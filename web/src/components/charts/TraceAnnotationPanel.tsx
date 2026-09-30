import React from 'react';
import { TraceRecord } from '@/api/types';
import { Activity, ArrowDownRight, ArrowUpRight, Terminal, Info } from 'lucide-react';
import { formatDateTime, formatNumber } from '@/lib/format';
import { useT } from '@/i18n';

interface TraceAnnotationPanelProps {
  record: TraceRecord | null;
}

export function TraceAnnotationPanel({ record }: TraceAnnotationPanelProps) {
  const t = useT();
  if (!record) {
    return (
      <div className="flex items-center gap-2 p-3 bg-background/60 border border-border/40 rounded-lg text-xs text-muted-foreground/70 font-mono">
        <Info className="w-4 h-4 shrink-0 text-muted-foreground" />
        <span>{t('charts.traceHint')}</span>
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
    <div className="p-3.5 bg-background/90 border border-border/40 rounded-lg font-mono text-xs space-y-3 shadow-inner">
      {/* Header bar: timestamp & OHLC summary if available */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 pb-2 text-[11px]">
        <div className="flex items-center gap-2">
          <span className="text-foreground font-semibold">{t('charts.tickAt')}</span>
          <span className="text-foreground">
            {formatDateTime(record.timestamp, { seconds: true })}
          </span>
        </div>
        {hasCandle && candle && (
          <div className="flex items-center gap-3 text-foreground">
            <span>O: <span className="text-foreground">{formatNumber(candle.o, { digits: 2 })}</span></span>
            <span>H: <span className="text-success">{formatNumber(candle.h, { digits: 2 })}</span></span>
            <span>L: <span className="text-destructive">{formatNumber(candle.l, { digits: 2 })}</span></span>
            <span>C: <span className="text-foreground font-bold">{formatNumber(candle.c, { digits: 2 })}</span></span>
            <span>V: <span className="text-muted-foreground">{formatNumber(candle.v, { digits: 2 })}</span></span>
          </div>
        )}
      </div>

      {/* Signal Banner */}
      {hasSignal ? (
        <div className="flex flex-wrap items-center gap-2">
          {signal.buy && (
            <div className="flex items-center gap-1.5 px-2.5 py-1 bg-success/15 border border-success/40 text-success rounded text-xs font-bold">
              <ArrowUpRight className="w-4 h-4" />
              <span>{t('charts.buyAt', { qty: signal.buy.qty, price: formatNumber(signal.buy.price, { digits: 2 }) })}</span>
            </div>
          )}
          {signal.sell && (
            <div className="flex items-center gap-1.5 px-2.5 py-1 bg-destructive/15 border border-destructive/40 text-destructive rounded text-xs font-bold">
              <ArrowDownRight className="w-4 h-4" />
              <span>{t('charts.sellAt', { qty: signal.sell.qty, price: formatNumber(signal.sell.price, { digits: 2 }) })}</span>
            </div>
          )}
          {signal.stop_loss && (
            <div className="flex items-center gap-1 px-2 py-0.5 bg-warning/15 border border-warning/40 text-warning rounded text-[11px]">
              <span>{t('charts.slAt', { qty: signal.stop_loss.qty, price: formatNumber(signal.stop_loss.price, { digits: 2 }) })}</span>
            </div>
          )}
          {signal.take_profit && (
            <div className="flex items-center gap-1 px-2 py-0.5 bg-card/60 border border-border/40 text-foreground rounded text-[11px]">
              <span>{t('charts.tpAt', { qty: signal.take_profit.qty, price: formatNumber(signal.take_profit.price, { digits: 2 }) })}</span>
            </div>
          )}
        </div>
      ) : (
        <div className="text-[11px] text-muted-foreground flex items-center gap-1.5">
          <span>{t('charts.signalLabel')} <span className="text-muted-foreground">{t('charts.signalNone')}</span></span>
        </div>
      )}

      {/* Indicators and Logs Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-1">
        {/* Computed Indicators */}
        <div className="space-y-1.5 bg-background/60 p-2.5 rounded border border-border/30">
          <div className="flex items-center gap-1.5 text-foreground text-[11px] font-bold uppercase tracking-wider">
            <Activity className="w-3.5 h-3.5" />
            <span>{t('charts.indicators', { count: indicators.length })}</span>
          </div>
          {indicators.length === 0 ? (
            <div className="text-muted-foreground text-[11px]">{t('charts.noIndicators')}</div>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {indicators.map((ind, idx) => {
                const paramStr = ind.params && Object.keys(ind.params).length > 0
                  ? Object.entries(ind.params).map(([k, v]) => `${k}:${v}`).join(',')
                  : '';
                return (
                  <span
                    key={idx}
                    className="inline-flex items-center gap-1 px-2 py-0.5 bg-card/30 border border-border/40 text-foreground rounded text-[11px]"
                  >
                    <span className="text-foreground uppercase">{ind.name}</span>
                    {paramStr && <span className="text-muted-foreground">({paramStr})</span>}
                    <span className="text-white font-bold font-mono">: {typeof ind.value === 'number' ? formatNumber(ind.value, { digits: 2 }) : String(ind.value)}</span>
                  </span>
                );
              })}
            </div>
          )}
        </div>

        {/* Script Log Output */}
        <div className="space-y-1.5 bg-background/60 p-2.5 rounded border border-border/30">
          <div className="flex items-center gap-1.5 text-foreground text-[11px] font-bold uppercase tracking-wider">
            <Terminal className="w-3.5 h-3.5" />
            <span>{t('charts.debugLog', { count: logs.length })}</span>
          </div>
          {logs.length === 0 ? (
            <div className="text-muted-foreground text-[11px]">{t('charts.noDebugLogs')}</div>
          ) : (
            <div className="space-y-1 max-h-32 overflow-y-auto pr-1">
              {logs.map((entry, idx) => (
                <div key={idx} className="flex items-start gap-1.5 text-[11px] leading-relaxed">
                  <span className="text-muted-foreground font-semibold">{entry.label}:</span>
                  <span className="text-foreground break-all">
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
