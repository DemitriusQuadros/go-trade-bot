import { useEffect, useRef, useState, useCallback } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { getToken } from '@/api/client';
import { RealtimePriceEvent, RealtimePositionEvent } from '@/api/types';
import { QUERY_KEYS } from './queries';

interface RealtimeState {
  prices: Record<string, RealtimePriceEvent>;
  positions: Record<number, RealtimePositionEvent>;
  connected: boolean;
  lastHeartbeat: string | null;
}

const BACKOFF_SCHEDULE_MS = [1000, 2000, 5000, 10000, 30000];
const HEARTBEAT_TIMEOUT_MS = 35000;

export function useSSE(enabled = true): RealtimeState {
  const queryClient = useQueryClient();
  const [prices, setPrices] = useState<Record<string, RealtimePriceEvent>>({});
  const [positions, setPositions] = useState<Record<number, RealtimePositionEvent>>({});
  const [connected, setConnected] = useState(false);
  const [lastHeartbeat, setLastHeartbeat] = useState<string | null>(null);

  const attemptRef = useRef(0);

  const connect = useCallback(() => {
    if (!enabled) return;

    const token = getToken();
    const url = `/stream/dashboard${token ? `?token=${encodeURIComponent(token)}` : ''}`;
    let es: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout>;
    let heartbeatTimer: ReturnType<typeof setTimeout>;
    let cancelled = false;

    function resetHeartbeatWatchdog() {
      clearTimeout(heartbeatTimer);
      heartbeatTimer = setTimeout(() => {
        if (es) {
          es.close();
        }
        setConnected(false);
        if (!cancelled) scheduleReconnect();
      }, HEARTBEAT_TIMEOUT_MS);
    }

    function scheduleReconnect() {
      const delay = BACKOFF_SCHEDULE_MS[Math.min(attemptRef.current, BACKOFF_SCHEDULE_MS.length - 1)];
      attemptRef.current += 1;
      retryTimer = setTimeout(connectStream, delay);
    }

    function connectStream() {
      if (cancelled) return;
      try {
        es = new EventSource(url);

        es.onopen = () => {
          setConnected(true);
          attemptRef.current = 0;
          resetHeartbeatWatchdog();
        };

        es.addEventListener('price_update', (e: MessageEvent) => {
          resetHeartbeatWatchdog();
          try {
            const data: RealtimePriceEvent = JSON.parse(e.data);
            setPrices((prev) => ({ ...prev, [data.symbol]: data }));
          } catch {
            // ignore malformed json
          }
        });

        es.addEventListener('position_update', (e: MessageEvent) => {
          resetHeartbeatWatchdog();
          try {
            const data: RealtimePositionEvent = JSON.parse(e.data);
            setPositions((prev) => ({ ...prev, [data.signal_id]: data }));
            
            // Invalidate React Query caches
            queryClient.invalidateQueries({ queryKey: QUERY_KEYS.signals() });
            queryClient.invalidateQueries({ queryKey: QUERY_KEYS.account });
          } catch {
            // ignore malformed json
          }
        });
        
        es.addEventListener('strategy_update', (e: MessageEvent) => {
           resetHeartbeatWatchdog();
           // Invalidate strategies if their status changes
           queryClient.invalidateQueries({ queryKey: QUERY_KEYS.strategies });
        });

        es.addEventListener('heartbeat', (e: MessageEvent) => {
          resetHeartbeatWatchdog();
          try {
            const data = JSON.parse(e.data);
            setLastHeartbeat(data.timestamp || new Date().toISOString());
          } catch {
            setLastHeartbeat(new Date().toISOString());
          }
        });

        es.onerror = () => {
          es?.close();
          setConnected(false);
          clearTimeout(heartbeatTimer);
          if (!cancelled) {
            scheduleReconnect();
          }
        };
      } catch {
        setConnected(false);
        if (!cancelled) {
          scheduleReconnect();
        }
      }
    }

    connectStream();

    return () => {
      cancelled = true;
      clearTimeout(retryTimer);
      clearTimeout(heartbeatTimer);
      es?.close();
    };
  }, [enabled, queryClient]);

  useEffect(() => {
    const cleanup = connect();
    return () => {
      cleanup?.();
    };
  }, [connect]);

  return {
    prices,
    positions,
    connected,
    lastHeartbeat,
  };
}
