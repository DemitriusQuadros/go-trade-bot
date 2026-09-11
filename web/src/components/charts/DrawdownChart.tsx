import React, { useEffect, useRef } from 'react';
import { createChart, ColorType, CrosshairMode, AreaSeries } from 'lightweight-charts';

export interface DrawdownPoint {
  time: string;
  drawdownPct: number;
}

interface DrawdownChartProps {
  points: DrawdownPoint[];
  summary?: string;
  height?: number;
}

export function DrawdownChart({
  points,
  summary,
  height = 200,
}: DrawdownChartProps) {
  const chartContainerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!chartContainerRef.current || !points || points.length < 2) return;

    const chart = createChart(chartContainerRef.current, {
      layout: {
        background: { type: ColorType.Solid, color: '#000000' },
        textColor: '#22c55e',
        fontFamily: 'monospace',
      },
      grid: {
        vertLines: { color: '#0a361b' },
        horzLines: { color: '#0a361b' },
      },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: { color: '#22c55e', labelBackgroundColor: '#22c55e' },
        horzLine: { color: '#22c55e', labelBackgroundColor: '#22c55e' },
      },
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
        borderColor: '#0a361b',
      },
      rightPriceScale: {
        borderColor: '#0a361b',
        autoScale: true,
      },
      width: chartContainerRef.current.clientWidth,
      height,
    });

    const areaSeries = chart.addSeries(AreaSeries, {
      lineColor: '#ef4444',
      topColor: 'rgba(239, 68, 68, 0.4)',
      bottomColor: 'rgba(239, 68, 68, 0.0)',
      lineWidth: 2,
    });

    const sortedData = [...points].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());

    // See EquityCurveChart.tsx for why same-second dedup is needed: trade
    // timestamps currently derive from DB insert wall-clock time, not
    // simulated candle time, so a fast backtest can produce several points
    // within the same wall-clock second.
    const bySecond = new Map<number, number>();
    for (const p of sortedData) {
      const sec = Math.floor(new Date(p.time).getTime() / 1000);
      bySecond.set(sec, p.drawdownPct);
    }
    const dedupedData = Array.from(bySecond.entries())
      .sort(([a], [b]) => a - b)
      .map(([time, value]) => ({ time: time as any, value }));

    areaSeries.setData(dedupedData);

    const handleResize = () => {
      if (chartContainerRef.current) {
        chart.applyOptions({ width: chartContainerRef.current.clientWidth });
      }
    };
    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('resize', handleResize);
      chart.remove();
    };
  }, [points, height]);

  if (!points || points.length < 2) {
    return (
      <div className="flex items-center justify-center p-8 text-xs text-green-700 bg-black rounded-lg border border-green-900/30 font-mono">
        Not enough data points to chart drawdown.
      </div>
    );
  }

  return (
    <div className="w-full">
      {summary && <p className="sr-only">{summary}</p>}
      <div ref={chartContainerRef} className="w-full" style={{ height }} />
    </div>
  );
}
