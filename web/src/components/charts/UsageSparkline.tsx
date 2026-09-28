import React, { useMemo, useState } from 'react';
import { UsageDay } from '@/api/types';
import { formatTokens, formatUsd } from '@/lib/time';

// Daily agent spend as plain-SVG columns (no chart library - a 30-bar
// sparkline doesn't need one, and Recharts is being phased out). Colours
// come from Tailwind token classes (fill-primary etc.) so it follows the
// Console Pro theme in both light and dark mode. Missing days (no runs)
// are filled in as zero so the x-axis is a true calendar.
interface UsageSparklineProps {
  days: UsageDay[];
  span?: number; // number of calendar days to show, ending today (UTC)
  budget?: number; // daily budget; drawn as a dashed reference line when > 0
  height?: number;
}

function utcDayKey(d: Date): string {
  return d.toISOString().slice(0, 10);
}

export function UsageSparkline({ days, span = 30, budget = 0, height = 72 }: UsageSparklineProps) {
  const [hover, setHover] = useState<number | null>(null);

  const series = useMemo(() => {
    const byDay = new Map(days.map((d) => [d.day, d]));
    const out: UsageDay[] = [];
    const today = new Date();
    for (let i = span - 1; i >= 0; i--) {
      const d = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate() - i));
      const key = utcDayKey(d);
      out.push(byDay.get(key) ?? { day: key, input_tokens: 0, output_tokens: 0, cost_usd: 0, runs: 0 });
    }
    return out;
  }, [days, span]);

  const maxCost = Math.max(budget > 0 ? budget : 0, ...series.map((d) => d.cost_usd), 0.0001);
  const width = 600;
  const gap = 2;
  const barW = (width - gap * (series.length - 1)) / series.length;
  const total = series.reduce((sum, d) => sum + d.cost_usd, 0);
  const totalRuns = series.reduce((sum, d) => sum + d.runs, 0);
  const active = hover != null ? series[hover] : null;

  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between text-[11px]">
        <span className="text-muted-foreground">
          Last {span} days: <span className="font-mono text-foreground">{formatUsd(total)}</span> across{' '}
          <span className="font-mono text-foreground">{totalRuns}</span> run{totalRuns === 1 ? '' : 's'}
        </span>
        <span className="font-mono text-muted-foreground min-h-[1em]" aria-live="polite">
          {active
            ? `${active.day}: ${formatUsd(active.cost_usd)} · ${active.runs} run${active.runs === 1 ? '' : 's'} · ${formatTokens(
                active.input_tokens + active.output_tokens,
              )} tok`
            : ''}
        </span>
      </div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        className="w-full block"
        style={{ height }}
        role="img"
        aria-label={`Daily agent cost for the last ${span} days, total ${formatUsd(total)}`}
        onMouseLeave={() => setHover(null)}
      >
        {series.map((d, i) => {
          const h = d.cost_usd > 0 ? Math.max(2, (d.cost_usd / maxCost) * (height - 4)) : 1;
          const over = budget > 0 && d.cost_usd >= budget;
          return (
            <g key={d.day} onMouseEnter={() => setHover(i)}>
              {/* full-height hit area so thin bars are still easy to hover */}
              <rect x={i * (barW + gap)} y={0} width={barW} height={height} className="fill-transparent" />
              <rect
                x={i * (barW + gap)}
                y={height - h}
                width={barW}
                height={h}
                rx={1}
                className={
                  d.cost_usd === 0
                    ? 'fill-border'
                    : over
                      ? 'fill-destructive'
                      : hover === i
                        ? 'fill-primary'
                        : 'fill-primary/60'
                }
              />
            </g>
          );
        })}
        {budget > 0 && (
          <line
            x1={0}
            x2={width}
            y1={height - (budget / maxCost) * (height - 4)}
            y2={height - (budget / maxCost) * (height - 4)}
            className="stroke-warning"
            strokeWidth={1}
            strokeDasharray="4 3"
            vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      <div className="flex justify-between text-[10px] font-mono text-muted-foreground">
        <span>{series[0]?.day}</span>
        {budget > 0 && <span className="text-warning">- - daily budget {formatUsd(budget)}</span>}
        <span>today (UTC)</span>
      </div>
    </div>
  );
}
