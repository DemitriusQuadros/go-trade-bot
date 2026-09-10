import React from 'react';
import { TraceRecord } from '@/api/types';
import { Activity, ArrowDownRight, ArrowUpRight, Terminal, Info } from 'lucide-react';

interface TraceAnnotationPanelProps {
  record: TraceRecord | null;
}

export function TraceAnnotationPanel({ record }: TraceAnnotationPanelProps) {
  if (!record) {
    return (
      <div className="flex items-center gap-2 p-3 bg-black/60 border border-green-950/40 rounded-lg text-xs text-green-700/70 font-mono">
        <Info className="w-4 h-4 shrink-0 text-green-800" />
        <span>Hover over or click candles on the chart to inspect signals, indicators, and execution log at that tick.</span>
      </div>
    );
  }

  const hasCandle = !!record.candle;
  const candle = record.candle;
  const signal = record.signal;
  const hasSignal = signal && (signal.buy || signal.sell || signal.stop_loss || signal.take_profit);
  const indicators = record.indicators || [];
  const logs = record.log || [];

  return (
    <div className="p-3.5 bg-black/90 border border-green-900/40 rounded-lg font-mono text-xs space-y-3 shadow-inner">
      {/* Header bar: timestamp & OHLC summary if available */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-green-950/60 pb-2 text-[11px]">
        <div className="flex items-center gap-2">
          <span className="text-green-500 font-semibold">TICK @</span>
          <span className="text-green-300">
            {record.timestamp ? new Date(record.timestamp).toLocaleString() : '—'}
          </span>
        </div>
        {hasCandle && candle && (
          <div className="flex items-center gap-3 text-slate-300">
            <span>O: <span className="text-slate-100">{candle.o.toFixed(2)}</span></span>
            <span>H: <span className="text-emerald-400">{candle.h.toFixed(2)}</span></span>
            <span>L: <span className="text-rose-400">{candle.l.toFixed(2)}</span></span>
            <span>C: <span className="text-green-400 font-bold">{candle.c.toFixed(2)}</span></span>
            <span>V: <span className="text-slate-400">{candle.v.toFixed(2)}</span></span>
          </div>
        )}
      </div>

      {/* Signal Banner */}
      {hasSignal ? (
        <div className="flex flex-wrap items-center gap-2">
          {signal.buy && (
            <div className="flex items-center gap-1.5 px-2.5 py-1 bg-emerald-950/70 border border-emerald-500/50 text-emerald-400 rounded text-xs font-bold">
              <ArrowUpRight className="w-4 h-4" />
              <span>BUY {signal.buy.qty} @ ${signal.buy.price.toFixed(2)}</span>
            </div>
          )}
          {signal.sell && (
            <div className="flex items-center gap-1.5 px-2.5 py-1 bg-rose-950/70 border border-rose-500/50 text-rose-400 rounded text-xs font-bold">
              <ArrowDownRight className="w-4 h-4" />
              <span>SELL {signal.sell.qty} @ ${signal.sell.price.toFixed(2)}</span>
            </div>
          )}
          {signal.stop_loss && (
            <div className="flex items-center gap-1 px-2 py-0.5 bg-amber-950/60 border border-amber-600/40 text-amber-300 rounded text-[11px]">
              <span>SL: {signal.stop_loss.qty} @ ${signal.stop_loss.price.toFixed(2)}</span>
            </div>
          )}
          {signal.take_profit && (
            <div className="flex items-center gap-1 px-2 py-0.5 bg-blue-950/60 border border-blue-600/40 text-blue-300 rounded text-[11px]">
              <span>TP: {signal.take_profit.qty} @ ${signal.take_profit.price.toFixed(2)}</span>
            </div>
          )}
        </div>
      ) : (
        <div className="text-[11px] text-green-800 flex items-center gap-1.5">
          <span>Signal: <span className="text-green-700">None (Holding / Watching)</span></span>
        </div>
      )}

      {/* Indicators and Logs Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-1">
        {/* Computed Indicators */}
        <div className="space-y-1.5 bg-slate-950/60 p-2.5 rounded border border-green-950/30">
          <div className="flex items-center gap-1.5 text-green-500 text-[11px] font-bold uppercase tracking-wider">
            <Activity className="w-3.5 h-3.5" />
            <span>Indicators ({indicators.length})</span>
          </div>
          {indicators.length === 0 ? (
            <div className="text-slate-500 text-[11px]">No indicator calls recorded this cycle.</div>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {indicators.map((ind, idx) => {
                const paramStr = ind.params && Object.keys(ind.params).length > 0
                  ? Object.entries(ind.params).map(([k, v]) => `${k}:${v}`).join(',')
                  : '';
                return (
                  <span
                    key={idx}
                    className="inline-flex items-center gap-1 px-2 py-0.5 bg-green-950/30 border border-green-800/40 text-green-300 rounded text-[11px]"
                  >
                    <span className="text-green-500 uppercase">{ind.name}</span>
                    {paramStr && <span className="text-green-700">({paramStr})</span>}
                    <span className="text-white font-bold font-mono">: {typeof ind.value === 'number' ? ind.value.toFixed(2) : String(ind.value)}</span>
                  </span>
                );
              })}
            </div>
          )}
        </div>

        {/* Script Log Output */}
        <div className="space-y-1.5 bg-slate-950/60 p-2.5 rounded border border-green-950/30">
          <div className="flex items-center gap-1.5 text-green-500 text-[11px] font-bold uppercase tracking-wider">
            <Terminal className="w-3.5 h-3.5" />
            <span>Debug Log ({logs.length})</span>
          </div>
          {logs.length === 0 ? (
            <div className="text-slate-500 text-[11px]">No debug logs recorded this cycle.</div>
          ) : (
            <div className="space-y-1 max-h-32 overflow-y-auto pr-1">
              {logs.map((entry, idx) => (
                <div key={idx} className="flex items-start gap-1.5 text-[11px] leading-relaxed">
                  <span className="text-green-600 font-semibold">{entry.label}:</span>
                  <span className="text-slate-300 break-all">
                    {typeof entry.value === 'object' ? JSON.stringify(entry.value) : String(entry.value)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
