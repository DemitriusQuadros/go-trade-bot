import React, { useEffect, useRef } from 'react';
import { createChart, ColorType, CrosshairMode, AreaSeries } from 'lightweight-charts';
import { getChartColors, withAlpha } from '@/lib/chartTheme';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';
import { chartLocalization } from '@/lib/format';
import { useLocale } from '@/i18n';
import { useT } from '@/i18n';

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
        vertLine: { color: c.primary, labelBackgroundColor: c.primary },
        horzLine: { color: c.primary, labelBackgroundColor: c.primary },
      },
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
        borderColor: c.border,
      },
      rightPriceScale: {
        borderColor: c.border,
        autoScale: true,
      },
      width: chartContainerRef.current.clientWidth,
      height,
    });

    const areaSeries = chart.addSeries(AreaSeries, {
      lineColor: c.destructive,
      topColor: withAlpha(c.destructive, 0.4),
      bottomColor: withAlpha(c.destructive, 0),
      lineWidth: 2,
    });

    const sortedData = [...points].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime());

    // lightweight-charts requires strictly ascending, non-repeating times -
    // two trades filled within the same wall-clock second (e.g. a sub-minute
    // timeframe backtest) would otherwise crash setData, so same-second
    // points are collapsed here (keeping the last value for that second).
    const bySecond = new Map<number, number>();
    for (const p of sortedData) {
      const sec = Math.floor(new Date(p.time).getTime() / 1000);
      bySecond.set(sec, p.drawdownPct);
    }
    const dedupedData = Array.from(bySecond.entries())
      .sort(([a], [b]) => a - b)
      .map(([time, value]) => ({ time: time as any, value }));

    areaSeries.setData(dedupedData);
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
  }, [points, height, isDark, locale]);

  if (!points || points.length < 2) {
    return (
      <div className="flex items-center justify-center p-8 text-xs text-muted-foreground bg-background rounded-lg border border-border/30 font-mono">
        {t('charts.noDrawdownData')}
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
