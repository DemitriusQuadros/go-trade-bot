import React, { useEffect, useRef, useMemo } from 'react';
import {
  createChart,
  ColorType,
  CrosshairMode,
  CandlestickSeries,
  LineSeries,
  createSeriesMarkers,
  SeriesMarker,
  Time,
} from 'lightweight-charts';
import { TraceRecord } from '@/api/types';

interface ExecutionTraceChartProps {
  trace: TraceRecord[];
  height?: number;
  onScrub?: (record: TraceRecord | null) => void;
}

// Visual distinct neon colors for continuous indicator overlays
const INDICATOR_COLORS = [
  '#38bdf8', // light blue
  '#f59e0b', // amber
  '#a855f7', // purple
  '#ec4899', // pink
  '#14b8a6', // teal
  '#eab308', // yellow
];

export function ExecutionTraceChart({
  trace,
  height = 420,
  onScrub,
}: ExecutionTraceChartProps) {
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const onScrubRef = useRef(onScrub);
  onScrubRef.current = onScrub;

  // Filter records that have valid candle data and sort chronologically
  const recordsWithCandle = useMemo(() => {
    if (!trace || trace.length === 0) return [];
    return trace
      .filter((r) => r.candle && typeof r.candle.c === 'number')
      .sort((a, b) => {
        const timeA = a.candle?.t ? a.candle.t : new Date(a.timestamp).getTime();
        const timeB = b.candle?.t ? b.candle.t : new Date(b.timestamp).getTime();
        return timeA - timeB;
      });
  }, [trace]);

  // Build time -> TraceRecord lookup map for scrubbing interaction
  const timeToRecordMap = useMemo(() => {
    const map = new Map<number, TraceRecord>();
    recordsWithCandle.forEach((rec) => {
      const sec = Math.floor(
        (rec.candle?.t ? rec.candle.t : new Date(rec.timestamp).getTime()) / 1000
      );
      map.set(sec, rec);
    });
    return map;
  }, [recordsWithCandle]);

  useEffect(() => {
    if (!chartContainerRef.current || recordsWithCandle.length === 0) return;

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

    // 1. Candlestick series
    const candleSeries = chart.addSeries(CandlestickSeries, {
      upColor: '#22c55e',
      downColor: '#ef4444',
      borderVisible: false,
      wickUpColor: '#22c55e',
      wickDownColor: '#ef4444',
    });

    const candleData = recordsWithCandle.map((r) => {
      const c = r.candle!;
      const timeSec = Math.floor(
        (c.t ? c.t : new Date(r.timestamp).getTime()) / 1000
      ) as Time;
      return {
        time: timeSec,
        open: c.o,
        high: c.h,
        low: c.l,
        close: c.c,
      };
    });

    candleSeries.setData(candleData);

    // 2. Buy/Sell/StopLoss/TakeProfit markers
    const markers: SeriesMarker<Time>[] = [];
    recordsWithCandle.forEach((r) => {
      const timeSec = Math.floor(
        (r.candle?.t ? r.candle.t : new Date(r.timestamp).getTime()) / 1000
      ) as Time;

      if (r.signal?.buy) {
        markers.push({
          time: timeSec,
          position: 'belowBar',
          color: '#22c55e',
          shape: 'arrowUp',
          text: `BUY ${r.signal.buy.qty}`,
        });
      }
      if (r.signal?.sell) {
        markers.push({
          time: timeSec,
          position: 'aboveBar',
          color: '#ef4444',
          shape: 'arrowDown',
          text: `SELL ${r.signal.sell.qty}`,
        });
      }
      if (r.signal?.stop_loss && !r.signal?.sell) {
        markers.push({
          time: timeSec,
          position: 'aboveBar',
          color: '#f59e0b',
          shape: 'circle',
          text: 'SL',
        });
      }
      if (r.signal?.take_profit && !r.signal?.sell) {
        markers.push({
          time: timeSec,
          position: 'aboveBar',
          color: '#38bdf8',
          shape: 'circle',
          text: 'TP',
        });
      }
    });

    if (markers.length > 0) {
      createSeriesMarkers(candleSeries, markers);
    }

    // 3. Continuous indicator overlays (e.g. ema, sma, bollinger, appearing in >= 70% of records)
    const indicatorCounts = new Map<string, number>();
    recordsWithCandle.forEach((r) => {
      r.indicators?.forEach((ind) => {
        const paramKey = ind.params
          ? Object.entries(ind.params)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([k, v]) => `${k}=${v}`)
              .join(',')
          : '';
        const key = `${ind.name}(${paramKey})`;
        indicatorCounts.set(key, (indicatorCounts.get(key) || 0) + 1);
      });
    });

    const continuousKeys: string[] = [];
    indicatorCounts.forEach((count, key) => {
      if (count >= recordsWithCandle.length * 0.7) {
        continuousKeys.push(key);
      }
    });

    continuousKeys.forEach((key, idx) => {
      const color = INDICATOR_COLORS[idx % INDICATOR_COLORS.length];
      const lineSeries = chart.addSeries(LineSeries, {
        color,
        lineWidth: 1,
        title: key,
      });

      const lineData: { time: Time; value: number }[] = [];
      recordsWithCandle.forEach((r) => {
        const matchingInd = r.indicators?.find((ind) => {
          const paramKey = ind.params
            ? Object.entries(ind.params)
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([k, v]) => `${k}=${v}`)
                .join(',')
            : '';
          return `${ind.name}(${paramKey})` === key;
        });

        if (matchingInd && typeof matchingInd.value === 'number') {
          const timeSec = Math.floor(
            (r.candle?.t ? r.candle.t : new Date(r.timestamp).getTime()) / 1000
          ) as Time;
          lineData.push({ time: timeSec, value: matchingInd.value });
        }
      });

      lineSeries.setData(lineData);
    });

    // 4. Interactive candle scrubbing
    chart.subscribeCrosshairMove((param) => {
      if (!param || !param.time) {
        return;
      }
      const timeNum = typeof param.time === 'number' ? param.time : Number(param.time);
      const matched = timeToRecordMap.get(timeNum);
      if (matched && onScrubRef.current) {
        onScrubRef.current(matched);
      }
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
  }, [recordsWithCandle, height, timeToRecordMap]);

  if (recordsWithCandle.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-xs text-green-700 bg-black rounded-lg border border-green-900/30 font-mono space-y-1">
        <span>No candlestick execution trace available for this run.</span>
        <span className="text-[11px] text-green-900">
          Traces are captured for script-based runs with cycle candle telemetry.
        </span>
      </div>
    );
  }

  return (
    <div className="w-full relative">
      <div ref={chartContainerRef} className="w-full" style={{ height }} />
    </div>
  );
}
