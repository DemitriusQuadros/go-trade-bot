import React, { useEffect, useRef } from 'react';
import { createChart, ColorType, HistogramSeries } from 'lightweight-charts';

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

  useEffect(() => {
    if (!chartContainerRef.current || !points || points.length === 0) return;

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
      timeScale: {
        timeVisible: true,
        borderColor: '#0a361b',
      },
      rightPriceScale: {
        borderColor: '#0a361b',
      },
      width: chartContainerRef.current.clientWidth,
      height,
    });

    const histogramSeries = chart.addSeries(HistogramSeries, {
      color: '#22c55e',
    });

    const sortedData = [...points].sort((a, b) => new Date(a.periodStart).getTime() - new Date(b.periodStart).getTime());
    
    histogramSeries.setData(sortedData.map(p => ({
      time: Math.floor(new Date(p.periodStart).getTime() / 1000) as any,
      value: p.profit,
      color: p.profit >= 0 ? '#22c55e' : '#ef4444',
    })));

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

  if (!points || points.length === 0) {
    return (
      <div className="flex items-center justify-center p-8 text-xs text-green-700 bg-black rounded-lg border border-green-900/30 font-mono">
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
