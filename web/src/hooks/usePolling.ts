// web/src/hooks/usePolling.ts
import { useEffect, useRef, useState, useCallback } from 'react';
import { NetworkError } from '@/api/client';

interface UsePollingOptions {
  intervalMs: number;
  enabled?: boolean;
}

export interface UsePollingResult<T> {
  data: T | null;
  error: Error | null;
  connected: boolean;
  loading: boolean;
  refetch: () => void;
}

export function usePolling<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  { intervalMs, enabled = true }: UsePollingOptions
): UsePollingResult<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);
  const hasLoadedOnce = useRef(false);

  const tick = useCallback(
    async (signal: AbortSignal) => {
      try {
        const result = await fetcher(signal);
        if (signal.aborted) return;
        setData(result);
        setError(null);
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return;
        setError(err as Error);
      } finally {
        if (!hasLoadedOnce.current) {
          hasLoadedOnce.current = true;
          setLoading(false);
        }
      }
    },
    [fetcher]
  );

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    tick(controller.signal);
    const id = setInterval(() => tick(controller.signal), intervalMs);
    return () => {
      controller.abort();
      clearInterval(id);
    };
  }, [enabled, intervalMs, tick]);

  return {
    data,
    error,
    connected: !(error instanceof NetworkError),
    loading,
    refetch: () => tick(new AbortController().signal),
  };
}
