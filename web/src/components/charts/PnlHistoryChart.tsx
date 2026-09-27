import React, { useEffect, useRef } from 'react';
import { createChart, ColorType, HistogramSeries } from 'lightweight-charts';
import { getChartColors } from '@/lib/chartTheme';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';

export interface PnlHistoryPoint {
  periodStart: string;
  profit: number;
  winRate?: number;
}

interface PnlHistoryChartProps {
  points: PnlHistoryPoint[];
  summary?: string;
  height?: number;
}

export function PnlHistoryChart({
  points,
  summary,
  height = 220,
}: PnlHistoryChartProps) {
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const isDark = useIsDarkMode();

  useEffect(() => {
    if (!chartContainerRef.current || !points || points.length === 0) return;
    const c = getChartColors();

    const chart = createChart(chartContainerRef.current, {
      layout: {
        background: { type: ColorType.Solid, color: c.background },
        textColor: c.muted,
        fontFamily: 'monospace',
      },
      grid: {
        vertLines: { color: c.border },
        horzLines: { color: c.border },
      },
      timeScale: {
        timeVisible: true,
        borderColor: c.border,
      },
      rightPriceScale: {
        borderColor: c.border,
      },
      width: chartContainerRef.current.clientWidth,
      height,
    });

    const histogramSeries = chart.addSeries(HistogramSeries, {
      color: c.success,
    });

    const sortedData = [...points].sort((a, b) => new Date(a.periodStart).getTime() - new Date(b.periodStart).getTime());

    histogramSeries.setData(sortedData.map(p => ({
      time: Math.floor(new Date(p.periodStart).getTime() / 1000) as any,
      value: p.profit,
      color: p.profit >= 0 ? c.success : c.destructive,
    })));
    chart.timeScale().fitContent();

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
  }, [points, height, isDark]);

  if (!points || points.length === 0) {
    return (
      <div className="flex items-center justify-center p-8 text-xs text-muted-foreground bg-background rounded-lg border border-border/30 font-mono">
        No performance history recorded yet.
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
