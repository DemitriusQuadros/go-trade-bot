import { useState, useEffect } from 'react';
import { TraceRecord } from '@/api/types';


export function useLivePreview(strategyId: number | null, enabled: boolean) {
  const [records, setRecords] = useState<TraceRecord[]>([]);

  useEffect(() => {
    if (!strategyId || !enabled) {
      setRecords([]);
      return;
    }

    const token = localStorage.getItem('auth_token');
    const url = new URL(`/api/stream/script-preview/${strategyId}`, (import.meta as any).env.VITE_API_URL || window.location.origin);
    if (token) url.searchParams.set('token', token);

    const es = new EventSource(url.toString());
    es.onmessage = (e) => {
      if (e.data === 'null') return;
      try {
        const record = JSON.parse(e.data) as TraceRecord;
        setRecords((prev) => [...prev, record].slice(-100)); // keep last 100
      } catch (err) {
        console.error('Failed to parse SSE', err);
      }
    };
    es.onerror = () => {
      console.error('SSE error in preview stream');
    };

    return () => es.close();
  }, [strategyId, enabled]);

  return records;
}
