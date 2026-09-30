import React, { useEffect, useRef } from 'react';
import { createChart, ColorType, CrosshairMode, LineSeries } from 'lightweight-charts';
import { EquityPoint } from '@/api/types';
import { getChartColors } from '@/lib/chartTheme';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';
import { chartLocalization } from '@/lib/format';
import { useLocale } from '@/i18n';
import { useT } from '@/i18n';

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
  const t = useT();
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const isDark = useIsDarkMode();
  const { locale } = useLocale();

  useEffect(() => {
    if (!chartContainerRef.current || !points || points.length < 2) return;
    const c = getChartColors();

    const chart = createChart(chartContainerRef.current, {
      localization: chartLocalization(),
      layout: {
        background: { type: ColorType.Solid, color: c.background },
        textColor: c.muted,
        fontFamily: 'monospace',
      },
      grid: {
        vertLines: { color: c.border },
        horzLines: { color: c.border },
      },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: {
          color: c.primary,
          labelBackgroundColor: c.primary,
        },
        horzLine: {
          color: c.primary,
          labelBackgroundColor: c.primary,
        },
      },
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
        borderColor: c.border,
      },
      rightPriceScale: {
        borderColor: c.border,
      },
      width: chartContainerRef.current.clientWidth,
      height,
    });

    const isNetPositive = points[points.length - 1].value >= (startingBalance || points[0].value);
    const color = isNetPositive ? c.success : c.destructive;

    const lineSeries = chart.addSeries(LineSeries, {
      color: color,
      lineWidth: 2,
      crosshairMarkerVisible: true,
      crosshairMarkerRadius: 4,
    });

    const sortedData = [...points].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());

    // lightweight-charts requires strictly ascending, non-repeating times -
    // two trades filled within the same wall-clock second (e.g. a sub-minute
    // timeframe backtest) would otherwise crash setData, so same-second
    // points are collapsed here (keeping the last value for that second).
    const bySecond = new Map<number, number>();
    for (const p of sortedData) {
      const sec = Math.floor(new Date(p.time).getTime() / 1000);
      bySecond.set(sec, p.value);
    }
    const dedupedData = Array.from(bySecond.entries())
      .sort(([a], [b]) => a - b)
      .map(([time, value]) => ({ time: time as any, value }));

    lineSeries.setData(dedupedData);
    chart.timeScale().fitContent();

    if (startingBalance !== undefined) {
      lineSeries.createPriceLine({
        price: startingBalance,
        color: c.muted,
        lineWidth: 1,
        lineStyle: 3,
        axisLabelVisible: true,
        title: t('charts.start'),
      });
    }

    const handleResize = () => {
      if (chartContainerRef.current) {
        chart.applyOptions({ width: chartContainerRef.current.clientWidth });
      }
    };
    window.addEventListener('resize', handleResize);
    // Also follow the container itself - e.g. inside the resizable agent
    // dock, whose width changes without a window resize.
    const observer = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(handleResize) : null;
    observer?.observe(chartContainerRef.current);

    return () => {
      window.removeEventListener('resize', handleResize);
      observer?.disconnect();
      chart.remove();
    };
  }, [points, startingBalance, height, isDark, locale, t]);

  if (!points || points.length < 2) {
    return (
      <div className="flex items-center justify-center p-8 text-xs text-muted-foreground bg-background rounded-lg border border-border/30 font-mono">
        {t('charts.noEquityData')}
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
