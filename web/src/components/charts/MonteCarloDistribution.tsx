import React from 'react';
import { MonteCarloDistributionStats } from '@/api/types';

interface MonteCarloDistributionProps {
  totalReturn: MonteCarloDistributionStats;
  sharpe: MonteCarloDistributionStats;
  maxDrawdown: MonteCarloDistributionStats;
  iterations: number;
}

// Was built against a histogram (bin/count) shape the backend has never
// actually produced - RunMonteCarlo (app/engine/montecarlo.go) reorders
// the trade log N times and reports percentile summary stats per metric,
// not per-iteration raw values, so a bar histogram was never renderable
// from what /backtest/{id}/montecarlo actually returns (the API call
// itself succeeded every time; nothing ever reached the screen). A range
// bar - Min/Max as the track ends, a shaded P5-P95 band, ticks at Median
// and Mean - is what this data can actually support, and it's a more
// standard way to show a percentile spread than a fabricated histogram
// would have been anyway.
export function MonteCarloDistribution({ totalReturn, sharpe, maxDrawdown, iterations }: MonteCarloDistributionProps) {
  return (
    <div className="w-full flex flex-col gap-5 font-mono">
      <div className="text-[11px] text-muted-foreground">
        {iterations.toLocaleString()} reorderings of this run's own trade log - same trades, shuffled sequence,
        recomputed each time. Shows how sensitive the result is to the order trades happened to land in.
      </div>
      <RangeMetric label="Total Return" unit="%" stats={totalReturn} invert={false} />
      <RangeMetric label="Sharpe Ratio" unit="" stats={sharpe} invert={false} />
      <RangeMetric label="Max Drawdown" unit="%" stats={maxDrawdown} invert />
    </div>
  );
}

// invert: for Max Drawdown, smaller is better - the "good" end of the
// track is the left (Min) side instead of the right, so the mean/median
// color-coding flips accordingly (a value near this metric's Min is good).
function RangeMetric({
  label,
  unit,
  stats,
  invert,
}: {
  label: string;
  unit: string;
  stats: MonteCarloDistributionStats;
  invert: boolean;
}) {
  const span = stats.Max - stats.Min || 1;
  const pct = (v: number) => ((v - stats.Min) / span) * 100;
  const fmt = (v: number) => `${v.toFixed(2)}${unit}`;

  const meanIsGood = invert ? stats.Mean <= (stats.Min + stats.Max) / 2 : stats.Mean >= 0;

  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between">
        <span className="text-[11px] uppercase tracking-wide text-muted-foreground">{label}</span>
        <span className={`text-sm font-bold ${meanIsGood ? 'text-success' : 'text-destructive'}`}>
          {fmt(stats.Mean)} <span className="text-muted-foreground font-normal text-[10px]">mean</span>
        </span>
      </div>

      <div className="relative h-2.5 rounded-full bg-secondary">
        {/* P5-P95 band */}
        <div
          className="absolute inset-y-0 rounded-full bg-primary/30"
          style={{ left: `${pct(stats.P5)}%`, width: `${Math.max(pct(stats.P95) - pct(stats.P5), 1)}%` }}
        />
        {/* Median tick */}
        <div
          className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-0.5 h-4 bg-foreground rounded-full"
          style={{ left: `${pct(stats.Median)}%` }}
          title={`Median: ${fmt(stats.Median)}`}
        />
        {/* Mean marker */}
        <div
          className={`absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-2 h-2 rounded-full ring-2 ring-background ${
            meanIsGood ? 'bg-success' : 'bg-destructive'
          }`}
          style={{ left: `${pct(stats.Mean)}%` }}
          title={`Mean: ${fmt(stats.Mean)}`}
        />
      </div>

      <div className="flex items-center justify-between text-[10px] text-muted-foreground">
        <span>Min {fmt(stats.Min)}</span>
        <span>P5 {fmt(stats.P5)}</span>
        <span>Median {fmt(stats.Median)}</span>
        <span>P95 {fmt(stats.P95)}</span>
        <span>Max {fmt(stats.Max)}</span>
      </div>
    </div>
  );
}
