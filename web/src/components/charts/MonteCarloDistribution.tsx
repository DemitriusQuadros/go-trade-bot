import React, { useEffect, useRef, useState } from 'react';
import {
  createChart,
  ColorType,
  CrosshairMode,
  HistogramSeries,
  Time,
} from 'lightweight-charts';

interface MonteCarloDistributionProps {
  distribution: Array<{ bin: number; count: number }>;
  mean: number;
  var95: number;
  ruinProb: number;
  height?: number;
}

export function MonteCarloDistribution({
  distribution,
  mean,
  var95,
  ruinProb,
  height = 220,
}: MonteCarloDistributionProps) {
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const [hoveredBin, setHoveredBin] = useState<{ bin: number; count: number } | null>(null);

  useEffect(() => {
    if (!chartContainerRef.current || !distribution || distribution.length === 0) return;

    // Sort distribution by bin ascending
    const sorted = [...distribution].sort((a, b) => a.bin - b.bin);

    // Map each bin to a synthetic sequential day timestamp (2020-01-01 + i days)
    // to guarantee strict chronological ordering required by lightweight-charts
    const baseDate = new Date(Date.UTC(2020, 0, 1)).getTime();
    const dayMs = 86400 * 1000;

    const timeToBinMap = new Map<number, { bin: number; count: number }>();
    const chartData = sorted.map((item, i) => {
      const timeSec = Math.floor((baseDate + i * dayMs) / 1000);
      timeToBinMap.set(timeSec, item);
      return {
        time: timeSec as Time,
        value: item.count,
        color: item.bin >= 0 ? '#22c55e' : '#ef4444',
      };
    });

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
          labelBackgroundColor: '#14532d',
        },
        horzLine: {
          color: '#22c55e',
          labelBackgroundColor: '#14532d',
        },
      },
      timeScale: {
        timeVisible: false,
        secondsVisible: false,
        borderColor: '#0a361b',
        tickMarkFormatter: (timeSec: number) => {
          const item = timeToBinMap.get(timeSec);
          return item ? `${item.bin.toFixed(1)}%` : '';
        },
      },
      rightPriceScale: {
        borderColor: '#0a361b',
        autoScale: true,
      },
      width: chartContainerRef.current.clientWidth,
      height,
    });

    const histogramSeries = chart.addSeries(HistogramSeries, {
      base: 0,
    });

    histogramSeries.setData(chartData);

    chart.subscribeCrosshairMove((param) => {
      if (!param || !param.time) {
        setHoveredBin(null);
        return;
      }
      const timeNum = typeof param.time === 'number' ? param.time : Number(param.time);
      const match = timeToBinMap.get(timeNum);
      setHoveredBin(match || null);
    });

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
  }, [distribution, height]);

  if (!distribution || distribution.length === 0) {
    return (
      <div className="flex items-center justify-center p-8 text-xs text-green-700 bg-black rounded-lg border border-green-900/30 font-mono">
        No Monte Carlo simulation data available.
      </div>
    );
  }

  return (
    <div className="w-full flex flex-col gap-3 font-mono">
      <div className="grid grid-cols-3 gap-2 text-center text-xs">
        <div className="p-2 bg-black/60 rounded border border-green-900/30">
          <span className="text-green-700 block text-[10px] uppercase">Mean Return</span>
          <span className={`font-mono font-bold ${mean >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
            {mean.toFixed(2)}%
          </span>
        </div>
        <div className="p-2 bg-black/60 rounded border border-green-900/30">
          <span className="text-green-700 block text-[10px] uppercase">95% VaR</span>
          <span className="font-mono font-bold text-amber-400">
            {var95.toFixed(2)}%
          </span>
        </div>
        <div className="p-2 bg-black/60 rounded border border-green-900/30">
          <span className="text-green-700 block text-[10px] uppercase">Ruin Probability</span>
          <span className={`font-mono font-bold ${ruinProb > 0.05 ? 'text-rose-400' : 'text-slate-200'}`}>
            {(ruinProb * 100).toFixed(2)}%
          </span>
        </div>
      </div>

      {hoveredBin && (
        <div className="text-[11px] text-green-400 flex items-center justify-between px-1">
          <span>Return Bin: <strong className="text-white">{hoveredBin.bin.toFixed(1)}%</strong></span>
          <span>Simulations: <strong className="text-white">{hoveredBin.count}</strong></span>
        </div>
      )}

      <div ref={chartContainerRef} className="w-full" style={{ height }} />
    </div>
  );
}
