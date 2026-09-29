import React, { useEffect, useRef } from 'react';
import { createChart, ColorType, CrosshairMode, LineSeries, UTCTimestamp } from 'lightweight-charts';
import { EquityPoint } from '@/api/types';
import { getChartColors } from '@/lib/chartTheme';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';

// Two equity curves on one lightweight-charts chart - the deploy gate's
// baseline (current code) vs candidate (new code) walk-forwards. Same setup
// as EquityCurveChart (token colours, same-second dedupe), plus a
// ResizeObserver so it follows the resizable agent dock.

function toSeriesData(points: EquityPoint[]) {
  const bySecond = new Map<number, number>();
  for (const p of points) {
    const t = new Date(p.time).getTime();
    if (Number.isNaN(t)) continue;
    bySecond.set(Math.floor(t / 1000), p.value);
  }
  return Array.from(bySecond.entries())
    .sort(([a], [b]) => a - b)
    .map(([time, value]) => ({ time: time as UTCTimestamp, value }));
}

interface EquityComparisonChartProps {
  baseline: EquityPoint[];
  candidate: EquityPoint[];
  height?: number;
  summary?: string;
}

export function EquityComparisonChart({ baseline, candidate, height = 160, summary }: EquityComparisonChartProps) {
  const ref = useRef<HTMLDivElement>(null);
  const isDark = useIsDarkMode();
  const enough = (baseline?.length ?? 0) >= 2 || (candidate?.length ?? 0) >= 2;

  useEffect(() => {
    const el = ref.current;
    if (!el || !enough) return;
    const c = getChartColors();
    const chart = createChart(el, {
      layout: { background: { type: ColorType.Solid, color: c.background }, textColor: c.muted, fontFamily: 'monospace' },
      grid: { vertLines: { color: c.border }, horzLines: { color: c.border } },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: { color: c.primary, labelBackgroundColor: c.primary },
        horzLine: { color: c.primary, labelBackgroundColor: c.primary },
      },
      timeScale: { timeVisible: true, secondsVisible: false, borderColor: c.border },
      rightPriceScale: { borderColor: c.border },
      width: el.clientWidth,
      height,
    });
    const base = chart.addSeries(LineSeries, { color: c.muted, lineWidth: 2, lineStyle: 2, title: 'Baseline' });
    const cand = chart.addSeries(LineSeries, { color: c.primary, lineWidth: 2, title: 'Candidate' });
    base.setData(toSeriesData(baseline ?? []));
    cand.setData(toSeriesData(candidate ?? []));
    chart.timeScale().fitContent();

    const resize = () => chart.applyOptions({ width: el.clientWidth });
    const observer = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(resize) : null;
    observer?.observe(el);
    return () => {
      observer?.disconnect();
      chart.remove();
    };
  }, [baseline, candidate, height, isDark, enough]);

  if (!enough) {
    return (
      <div className="flex items-center justify-center p-4 text-xs text-muted-foreground bg-background rounded border border-border/60 font-mono">
        Not enough data points to chart the gate runs.
      </div>
    );
  }

  return (
    <div className="w-full">
      {summary && <p className="sr-only">{summary}</p>}
      <div ref={ref} className="w-full overflow-hidden" style={{ height }} />
      <div className="mt-1 flex items-center gap-3 text-[10px] text-muted-foreground font-mono" aria-hidden="true">
        <span className="inline-flex items-center gap-1">
          <span className="inline-block w-3 border-t-2 border-dashed border-muted-foreground" /> baseline (current code)
        </span>
        <span className="inline-flex items-center gap-1">
          <span className="inline-block w-3 border-t-2 border-primary" /> candidate (new code)
        </span>
      </div>
    </div>
  );
}
