import React from 'react';

interface ParamHeatmapProps {
  xAxisLabel: string;
  xAxisValues: number[];
  yAxisLabel: string;
  yAxisValues: number[];
  cellValues: (number | null)[][]; // cellValues[yIndex][xIndex]
  bestY: number;
  bestX: number;
  higherIsBetter?: boolean;
  formatValue?: (n: number) => string;
}

export function ParamHeatmap({
  xAxisLabel,
  xAxisValues,
  yAxisLabel,
  yAxisValues,
  cellValues,
  bestY,
  bestX,
  higherIsBetter = true,
  formatValue = (n: number) => n.toFixed(2),
}: ParamHeatmapProps) {
  const flat = cellValues.flat().filter((v): v is number => v !== null && !isNaN(v));
  if (flat.length === 0) {
    return (
      <div className="p-8 text-center text-xs text-muted-foreground bg-card/50 rounded-lg border border-border">
        No parameter combinations to display in heatmap.
      </div>
    );
  }

  const min = Math.min(...flat);
  const max = Math.max(...flat);
  const range = max - min || 1;

  // Gradient from destructive (0.0) -> neutral border (0.5) -> success
  // (1.0), sourced from this theme's own tokens via CSS color-mix() rather
  // than hardcoded RGB literals (previously literal red-500/slate-500/
  // emerald-500 regardless of theme) - color-mix responds to the live
  // custom-property values directly, no JS re-read needed on toggle.
  const getColor = (val: number | null) => {
    if (val === null || isNaN(val)) return 'hsl(var(--secondary))';
    const norm = Math.max(0, Math.min(1, (val - min) / range));
    const score = higherIsBetter ? norm : 1 - norm;

    if (score < 0.5) {
      const t = Math.round((score / 0.5) * 100);
      return `color-mix(in srgb, hsl(var(--destructive)) ${100 - t}%, hsl(var(--border)) ${t}%)`;
    }
    const t = Math.round(((score - 0.5) / 0.5) * 100);
    return `color-mix(in srgb, hsl(var(--border)) ${100 - t}%, hsl(var(--success)) ${t}%)`;
  };

  return (
    <div className="overflow-x-auto p-2">
      <div className="flex flex-col gap-2 min-w-[400px]">
        {/* Heatmap header */}
        <div className="flex items-center justify-between text-xs text-muted-foreground mb-1">
          <span>Y: {yAxisLabel} \ X: {xAxisLabel}</span>
          <div className="flex items-center gap-2">
            <span className="text-[10px]">Min: {formatValue(min)}</span>
            <div className="w-20 h-2 rounded bg-gradient-to-r from-destructive via-secondary to-success" />
            <span className="text-[10px]">Max: {formatValue(max)}</span>
          </div>
        </div>

        {/* CSS Grid */}
        <div
          className="grid gap-1.5"
          style={{
            gridTemplateColumns: `auto repeat(${xAxisValues.length}, minmax(44px, 1fr))`,
          }}
        >
          {/* Top-left empty corner */}
          <div className="p-2 text-xs font-semibold text-muted-foreground text-center" />

          {/* X Axis column headers */}
          {xAxisValues.map((xVal, xi) => (
            <div
              key={`col-${xi}`}
              className="p-1.5 text-xs font-semibold text-foreground text-center bg-card/60 rounded"
            >
              {xVal}
            </div>
          ))}

          {/* Rows */}
          {yAxisValues.map((yVal, yi) => (
            <React.Fragment key={`row-${yi}`}>
              {/* Y Axis row header */}
              <div className="p-2 text-xs font-semibold text-foreground flex items-center justify-end pr-3 bg-card/60 rounded">
                {yVal}
              </div>

              {/* Cells */}
              {xAxisValues.map((_, xi) => {
                const val = cellValues[yi]?.[xi] ?? null;
                const isBest = yi === bestY && xi === bestX;

                return (
                  <div
                    key={`cell-${yi}-${xi}`}
                    style={{ backgroundColor: getColor(val) }}
                    className={`h-12 flex flex-col items-center justify-center rounded text-xs font-mono transition-all hover:scale-105 hover:z-10 cursor-default ${
                      isBest ? 'ring-2 ring-ring ring-offset-2 ring-offset-background font-bold text-success-foreground' : 'text-foreground'
                    }`}
                    title={`Y: ${yVal}, X: ${xAxisValues[xi]} => ${val !== null ? formatValue(val) : 'N/A'}${isBest ? ' (BEST)' : ''}`}
                  >
                    {val !== null ? formatValue(val) : '—'}
                    {isBest && <span className="text-[9px] uppercase tracking-tighter text-foreground">best</span>}
                  </div>
                );
              })}
            </React.Fragment>
          ))}
        </div>
      </div>
    </div>
  );
}
