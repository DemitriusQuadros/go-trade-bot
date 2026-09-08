import React, { useEffect, useRef } from 'react';
import { createChart, ColorType, CrosshairMode, LineSeries } from 'lightweight-charts';
import { EquityPoint } from '@/api/types';

interface EquityCurveChartProps {
  points: EquityPoint[];
  startingBalance?: number;
  summary?: string;
  height?: number;
}

export function EquityCurveChart({
  points,
  startingBalance,
  summary,
  height = 280,
}: EquityCurveChartProps) {
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
        vertLine: {
          color: '#22c55e',
          labelBackgroundColor: '#22c55e',
        },
        horzLine: {
          color: '#22c55e',
          labelBackgroundColor: '#22c55e',
        },
      },
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
        borderColor: '#0a361b',
      },
      rightPriceScale: {
        borderColor: '#0a361b',
      },
      width: chartContainerRef.current.clientWidth,
      height,
    });

    const isNetPositive = points[points.length - 1].value >= (startingBalance || points[0].value);
    const color = isNetPositive ? '#22c55e' : '#ef4444';

    const lineSeries = chart.addSeries(LineSeries, {
      color: color,
      lineWidth: 2,
      crosshairMarkerVisible: true,
      crosshairMarkerRadius: 4,
    });

    const sortedData = [...points].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());
    
    lineSeries.setData(sortedData.map(p => ({
      time: Math.floor(new Date(p.time).getTime() / 1000) as any,
      value: p.value,
    })));

    if (startingBalance !== undefined) {
      lineSeries.createPriceLine({
        price: startingBalance,
        color: '#22c55e',
        lineWidth: 1,
        lineStyle: 3,
        axisLabelVisible: true,
        title: 'Start',
      });
    }

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
  }, [points, startingBalance, height]);

  if (!points || points.length < 2) {
    return (
      <div className="flex items-center justify-center p-8 text-xs text-green-700 bg-black rounded-lg border border-green-900/30 font-mono">
        Not enough data points to chart equity curve.
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
