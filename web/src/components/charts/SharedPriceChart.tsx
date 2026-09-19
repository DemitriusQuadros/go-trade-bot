import React, { useEffect, useRef, useMemo, useState } from 'react';
import {
  createChart,
  IChartApi,
  ISeriesApi,
  ColorType,
  CrosshairMode,
  CandlestickSeries,
  HistogramSeries,
  LineSeries,
  createSeriesMarkers,
  SeriesMarker,
  Time,
} from 'lightweight-charts';
import { TraceRecord } from '@/api/types';

interface SharedPriceChartProps {
  trace: TraceRecord[];
  height?: number;
  onScrub?: (record: TraceRecord | null) => void;
  symbol?: string;
  // When true, the container takes no inline height and instead fills
  // whatever box its CSS parent gives it (e.g. a flex-1 min-h-0 column) -
  // a ResizeObserver keeps the chart's actual pixel size in sync. `height`
  // is ignored in this mode; it remains the fixed-px behavior otherwise,
  // for callers like ScriptRepl.tsx that aren't in a fill layout.
  fill?: boolean;
}

// Below this many screen pixels per bar, a marker's qty text is dropped in
// favor of a bare arrow/dot - mirrors TradingView hiding its "+200"/"Sell"
// labels once bars get too dense to fit them without overlapping. Recomputed
// on every zoom/pan via subscribeVisibleLogicalRangeChange (Effect 4).
const MIN_PX_PER_BAR_FOR_MARKER_TEXT = 28;

// Verbatim copy of the old ExecutionTraceChart.tsx's array - kept manually
// synchronized with plotColorPalette in app/strategies/script/trace.go.
const INDICATOR_COLORS = ['#38bdf8', '#f59e0b', '#a855f7', '#ec4899', '#14b8a6', '#eab308'];

// r.candle.t is already Unix SECONDS (backend: exchange.Candle.OpenTime.Unix()) -
// lightweight-charts' Time type also wants Unix seconds, so it must be used
// as-is. Only the r.timestamp fallback (a JS Date-parsed ISO string) is in
// milliseconds and needs dividing. Dividing candle.t by 1000 a second time
// (a previous bug) collapses ~1000s windows into one integer, producing
// duplicate/non-ascending times that crash lightweight-charts' internal
// ordering assertion and take down the whole page (no error boundary wraps
// this component).
function toChartTimeSeconds(r: TraceRecord): number {
  if (typeof r.candle?.t === 'number') {
    return Math.floor(r.candle.t);
  }
  return Math.floor(new Date(r.timestamp).getTime() / 1000);
}

export function SharedPriceChart({ trace, height = 600, onScrub, symbol, fill = false }: SharedPriceChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const candleSeriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null);
  const volumeSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null);
  const plotSeriesRef = useRef<Map<string, ISeriesApi<'Line'>>>(new Map());
  const [hiddenPlots, setHiddenPlots] = useState<Set<string>>(new Set());
  const [legendRecord, setLegendRecord] = useState<TraceRecord | null>(null);
  const [showMarkerText, setShowMarkerText] = useState(true);
  const onScrubRef = useRef(onScrub);
  onScrubRef.current = onScrub;

  const recordsWithCandle = useMemo(() => {
    if (!trace || trace.length === 0) return [];
    return trace
      .filter((r) => r.candle && typeof r.candle.c === 'number')
      .sort((a, b) => toChartTimeSeconds(a) - toChartTimeSeconds(b));
  }, [trace]);

  const timeToRecordMap = useMemo(() => {
    const map = new Map<number, TraceRecord>();
    recordsWithCandle.forEach((rec) => {
      map.set(toChartTimeSeconds(rec), rec);
    });
    return map;
  }, [recordsWithCandle]);
  const timeToRecordMapRef = useRef(timeToRecordMap);
  timeToRecordMapRef.current = timeToRecordMap;

  // First-seen color per plot name (defensive client-side fallback - the
  // primary color source is PlotPoint.Color from the wire, always non-empty
  // per backend-01's AC#6). Shared by the chart-building effect and the
  // legend overlay so both agree on which color goes with which name.
  const colorForName = useMemo(() => {
    let colorSlot = 0;
    const map = new Map<string, string>();
    trace.forEach((r) =>
      r.plots?.forEach((p) => {
        if (!map.has(p.name)) {
          map.set(p.name, p.color || INDICATOR_COLORS[colorSlot++ % INDICATOR_COLORS.length]);
        }
      })
    );
    return map;
  }, [trace]);

  // Effect 1: create the chart ONCE per mount and never recreate it on
  // resize (a ResizeObserver below keeps its pixel size in sync instead) -
  // this is what makes the chart survive both data-source swaps AND layout
  // resizes (e.g. a fill-mode flex column changing size) without resetting
  // zoom/pan.
  useEffect(() => {
    if (!containerRef.current) return;
    const chart = createChart(containerRef.current, {
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
        // TradingView's default bar spacing (~8-10px) reads as noticeably
        // "fatter" candles than lightweight-charts' own default (6px) -
        // this is most of the "candles look too small" gap at a glance.
        barSpacing: 10,
      },
      rightPriceScale: {
        borderColor: '#0a361b',
        autoScale: true,
        // Reserve headroom at the bottom of the price pane for the volume
        // histogram overlay (see 'volume' price scale below) so volume bars
        // never distort the candle autoscale.
        scaleMargins: { top: 0.08, bottom: 0.2 },
      },
      // Initial size only - the ResizeObserver below (which also fires once
      // immediately on observe) takes over from here, so a fill-mode
      // container whose clientHeight is 0 before layout settles just gets
      // corrected on the observer's first callback rather than staying wrong.
      width: containerRef.current.clientWidth,
      height: containerRef.current.clientHeight || height,
    });
    chartRef.current = chart;
    candleSeriesRef.current = chart.addSeries(CandlestickSeries, {
      upColor: '#22c55e',
      downColor: '#ef4444',
      borderVisible: false,
      wickUpColor: '#22c55e',
      wickDownColor: '#ef4444',
    });

    // Volume histogram, low-opacity, pinned to the bottom ~20% of the price
    // pane via its own overlay price scale - the same "own scale, shared
    // pane" trick as the indicator pane, just without a hard border since
    // it's meant to read as background context, not a separate chart.
    volumeSeriesRef.current = chart.addSeries(HistogramSeries, {
      priceScaleId: 'volume',
      priceFormat: { type: 'volume' },
      lastValueVisible: false,
      priceLineVisible: false,
    });
    chart.priceScale('volume').applyOptions({
      scaleMargins: { top: 0.8, bottom: 0 },
      visible: false,
    });

    chart.subscribeCrosshairMove((param) => {
      const timeNum = param && param.time ? (typeof param.time === 'number' ? param.time : Number(param.time)) : null;
      const matched = timeNum !== null ? timeToRecordMapRef.current.get(timeNum) ?? null : null;
      setLegendRecord(matched);
      if (onScrubRef.current) {
        onScrubRef.current(matched);
      }
    });

    // Effect 4 (inline, same chart instance): recompute whether marker text
    // fits. subscribeVisibleLogicalRangeChange fires on every zoom/pan -
    // bars-in-view vs container width gives an approximate px-per-bar,
    // avoiding a persistent MutationObserver just to read barSpacing.
    chart.timeScale().subscribeVisibleLogicalRangeChange((range) => {
      if (!range || !containerRef.current) return;
      const barsInView = range.to - range.from;
      if (barsInView <= 0) return;
      const pxPerBar = containerRef.current.clientWidth / barsInView;
      setShowMarkerText(pxPerBar >= MIN_PX_PER_BAR_FOR_MARKER_TEXT);
    });

    // ResizeObserver instead of a window 'resize' listener - a fill-mode
    // container (flex-1 min-h-0) changes size on layout events (e.g. the
    // left panel's content growing/shrinking) that never fire a window
    // resize, so watching the container's own box is the only way both
    // fixed-height and fill-mode callers stay correctly sized.
    const resizeObserver = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      const { width: w, height: h } = entry.contentRect;
      if (w > 0 && h > 0) {
        chart.resize(w, h);
      }
    });
    resizeObserver.observe(containerRef.current);

    return () => {
      resizeObserver.disconnect();
      chart.remove();
      chartRef.current = null;
      candleSeriesRef.current = null;
      volumeSeriesRef.current = null;
      plotSeriesRef.current.clear();
    };
  }, []);

  // Effect 2: push new data into EXISTING series via .setData(), keyed by
  // plot name for stable series identity - the actual fix for "switching
  // tabs doesn't reset my view."
  useEffect(() => {
    const chart = chartRef.current;
    const candleSeries = candleSeriesRef.current;
    const volumeSeries = volumeSeriesRef.current;
    if (!chart || !candleSeries) return;

    const candleData = recordsWithCandle.map((r) => {
      const c = r.candle!;
      return {
        time: toChartTimeSeconds(r) as Time,
        open: c.o,
        high: c.h,
        low: c.l,
        close: c.c,
      };
    });
    candleSeries.setData(candleData);

    if (volumeSeries) {
      volumeSeries.setData(
        recordsWithCandle.map((r) => {
          const c = r.candle!;
          return {
            time: toChartTimeSeconds(r) as Time,
            value: c.v,
            color: c.c >= c.o ? 'rgba(34, 197, 94, 0.35)' : 'rgba(239, 68, 68, 0.35)',
          };
        })
      );
    }

    // Recompute marker-text density right now too, not just on the next
    // pan/zoom (Effect 1's subscribeVisibleLogicalRangeChange) - on first
    // load the chart auto-fits all bars into view without firing that
    // subscription, so relying on it alone left markers showing text at
    // a density that should have hidden it until the user first touched
    // the chart. `effectiveShowText` (not the possibly-stale `showMarkerText`
    // state) is what actually drives the markers built below.
    let effectiveShowText = showMarkerText;
    const visibleRange = chart.timeScale().getVisibleLogicalRange();
    if (visibleRange && containerRef.current) {
      const barsInView = visibleRange.to - visibleRange.from;
      if (barsInView > 0) {
        const pxPerBar = containerRef.current.clientWidth / barsInView;
        effectiveShowText = pxPerBar >= MIN_PX_PER_BAR_FOR_MARKER_TEXT;
        if (effectiveShowText !== showMarkerText) {
          setShowMarkerText(effectiveShowText);
        }
      }
    }

    const markers: SeriesMarker<Time>[] = [];
    recordsWithCandle.forEach((r) => {
      const timeSec = toChartTimeSeconds(r) as Time;
      // Compact "+qty"/"-qty" text (TradingView's own convention) when bars
      // are wide enough apart to fit it (see showMarkerText / Effect 1's
      // subscribeVisibleLogicalRangeChange) - otherwise bare shape+color,
      // so a high-frequency strategy doesn't turn into label soup. Full
      // qty/price detail is always available via the crosshair-driven
      // TraceAnnotationPanel (onScrub) regardless of this setting.
      if (r.signal?.buy) {
        markers.push({
          time: timeSec,
          position: 'belowBar',
          color: '#22c55e',
          shape: 'arrowUp',
          text: effectiveShowText ? `+${r.signal.buy.qty}` : undefined,
        });
      }
      if (r.signal?.sell) {
        markers.push({
          time: timeSec,
          position: 'aboveBar',
          color: '#ef4444',
          shape: 'arrowDown',
          text: effectiveShowText ? `-${r.signal.sell.qty}` : undefined,
        });
      }
      if (r.signal?.stop_loss && !r.signal?.sell) {
        markers.push({
          time: timeSec,
          position: 'aboveBar',
          color: '#f59e0b',
          shape: 'circle',
          text: effectiveShowText ? 'SL' : undefined,
        });
      }
      if (r.signal?.take_profit && !r.signal?.sell) {
        markers.push({
          time: timeSec,
          position: 'aboveBar',
          color: '#38bdf8',
          shape: 'circle',
          text: effectiveShowText ? 'TP' : undefined,
        });
      }
    });
    // createSeriesMarkers is idempotent-safe to call again with a fresh
    // array (lightweight-charts replaces the marker set) - unlike line
    // series, there's no "stable marker identity" concept to preserve.
    createSeriesMarkers(candleSeries, markers);

    // plot() series: one LineSeries per distinct PlotPoint.name seen across
    // `trace`, keyed by name in plotSeriesRef so re-running this effect
    // with a NEW trace array reuses the same ISeriesApi object for a name
    // that was already present, rather than removing and re-adding it.
    const namesInNewTrace = new Set<string>();
    trace.forEach((r) => r.plots?.forEach((p) => namesInNewTrace.add(p.name)));

    for (const [name, series] of plotSeriesRef.current) {
      if (!namesInNewTrace.has(name)) {
        chart.removeSeries(series);
        plotSeriesRef.current.delete(name);
      }
    }

    // plot() series (RSI, MACD, etc.) get a dedicated SECOND PANE below the
    // candles - a real sub-chart with its own border and price scale, not
    // an overlay squeezed into a margin of the price pane. Value ranges
    // like RSI's 0-100 never touch the candle series' autoscale this way.
    // paneIndex 1 is created implicitly by addSeries on first use.
    namesInNewTrace.forEach((name) => {
      let series = plotSeriesRef.current.get(name);
      if (!series) {
        series = chart.addSeries(
          LineSeries,
          {
            color: colorForName.get(name),
            lineWidth: 1,
            title: name,
            visible: !hiddenPlots.has(name),
          },
          1
        );
        plotSeriesRef.current.set(name, series);
      }
      const lineData = recordsWithCandle
        .flatMap((r) =>
          (r.plots || [])
            .filter((p) => p.name === name)
            .map((p) => ({ time: toChartTimeSeconds(r) as Time, value: p.value }))
        );
      series.setData(lineData);
    });

    // Size the indicator pane to ~1/4 of the price pane's height (stretch
    // factor is relative, not absolute) - only meaningful once pane 1
    // actually exists, which happens on the first addSeries(..., 1) above.
    const panes = chart.panes();
    if (panes.length > 1) {
      panes[0].setStretchFactor(3);
      panes[1].setStretchFactor(1);
      // Pane 1's own 'right' price scale is a separate namespace from pane
      // 0's - match the theme border color instead of the library default.
      chart.priceScale('right', 1).applyOptions({ borderColor: '#0a361b' });
    }
    // hiddenPlots intentionally excluded: visibility is handled by Effect 3
    // below as a pure client-side toggle, not re-derived from data updates.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [trace, recordsWithCandle, showMarkerText, colorForName]);

  // Effect 3: legend visibility toggle - pure client-side .applyOptions, no
  // data refetch, no chart recreate.
  useEffect(() => {
    plotSeriesRef.current.forEach((series, name) => {
      series.applyOptions({ visible: !hiddenPlots.has(name) });
    });
  }, [hiddenPlots]);

  const plotNames = useMemo(() => {
    const names = new Set<string>();
    trace.forEach((r) => r.plots?.forEach((p) => names.add(p.name)));
    return Array.from(names);
  }, [trace]);

  // Overlaid top-left legend, TradingView-style: symbol + O/H/L/C tracking
  // the crosshair, falling back to the latest candle when the mouse isn't
  // over the chart (TV never shows a blank legend). Painted ON the canvas
  // instead of taking its own layout row, so it costs zero chart height.
  const legendSource = legendRecord ?? recordsWithCandle[recordsWithCandle.length - 1] ?? null;
  const legendCandle = legendSource?.candle;

  return (
    <div className={fill ? 'w-full h-full min-h-0 relative flex flex-col' : 'w-full relative'}>
      <div
        ref={containerRef}
        className={fill ? 'w-full flex-1 min-h-0' : 'w-full'}
        style={fill ? undefined : { height }}
      />
      {/* max-w constrains this to the chart's own width so the inner
          flex-wrap actually has a boundary to wrap against - an absolutely
          positioned box with no width limit sizes to its content and never
          wraps, which is what let the OHLC row run off the right edge of a
          narrow chart instead of dropping to a second line. */}
      <div className="pointer-events-none absolute top-2 left-2 max-w-[calc(100%-1rem)] z-10 font-mono text-[11px] leading-relaxed bg-black/60 rounded px-2 py-1 backdrop-blur-sm">
        <div className="flex items-center gap-2 flex-wrap">
          {symbol && <span className="text-green-300 font-bold">{symbol}</span>}
          {legendCandle && (
            <>
              <span className="text-green-700">O</span>
              <span className={legendCandle.c >= legendCandle.o ? 'text-green-400' : 'text-red-400'}>
                {legendCandle.o.toFixed(2)}
              </span>
              <span className="text-green-700">H</span>
              <span className={legendCandle.c >= legendCandle.o ? 'text-green-400' : 'text-red-400'}>
                {legendCandle.h.toFixed(2)}
              </span>
              <span className="text-green-700">L</span>
              <span className={legendCandle.c >= legendCandle.o ? 'text-green-400' : 'text-red-400'}>
                {legendCandle.l.toFixed(2)}
              </span>
              <span className="text-green-700">C</span>
              <span className={legendCandle.c >= legendCandle.o ? 'text-green-400' : 'text-red-400'}>
                {legendCandle.c.toFixed(2)}
              </span>
            </>
          )}
        </div>
        {plotNames.length > 0 && (
          <div className="flex items-center gap-2 flex-wrap mt-0.5">
            {plotNames.map((name) => {
              const point = legendSource?.plots?.find((p) => p.name === name);
              return (
                <span key={name} style={{ color: colorForName.get(name) }}>
                  {name}
                  {point ? `: ${point.value.toFixed(2)}` : ''}
                </span>
              );
            })}
          </div>
        )}
      </div>
      {plotNames.length > 0 && (
        <div className="flex flex-wrap gap-2 mt-1 px-1 text-[11px] font-mono">
          {plotNames.map((name) => (
            <label key={name} className="flex items-center gap-1 cursor-pointer select-none text-green-500">
              <input
                type="checkbox"
                checked={!hiddenPlots.has(name)}
                onChange={() =>
                  setHiddenPlots((prev) => {
                    const next = new Set(prev);
                    if (next.has(name)) {
                      next.delete(name);
                    } else {
                      next.add(name);
                    }
                    return next;
                  })
                }
              />
              <span>{name}</span>
            </label>
          ))}
        </div>
      )}
    </div>
  );
}
