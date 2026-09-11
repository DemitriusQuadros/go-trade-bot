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
      <div className="p-8 text-center text-xs text-slate-500 bg-slate-900/50 rounded-lg border border-slate-800">
        No parameter combinations to display in heatmap.
      </div>
    );
  }

  const min = Math.min(...flat);
  const max = Math.max(...flat);
  const range = max - min || 1;

  const getColor = (val: number | null) => {
    if (val === null || isNaN(val)) return '#1f293d';
    const norm = Math.max(0, Math.min(1, (val - min) / range));
    const score = higherIsBetter ? norm : 1 - norm;

    // Gradient from dark red (0.0) -> muted slate (0.5) -> bright emerald (1.0)
    if (score < 0.5) {
      const t = score / 0.5;
      const r = Math.round(239 * (1 - t * 0.5));
      const g = Math.round(68 * (1 + t * 0.5));
      const b = Math.round(68 * (1 + t));
      return `rgba(${r}, ${g}, ${b}, 0.65)`;
    } else {
      const t = (score - 0.5) / 0.5;
      const r = Math.round(100 * (1 - t) + 16 * t);
      const g = Math.round(116 * (1 - t) + 185 * t);
      const b = Math.round(139 * (1 - t) + 129 * t);
      return `rgba(${r}, ${g}, ${b}, 0.85)`;
    }
  };

  return (
    <div className="overflow-x-auto p-2">
      <div className="flex flex-col gap-2 min-w-[400px]">
        {/* Heatmap header */}
        <div className="flex items-center justify-between text-xs text-slate-400 mb-1">
          <span>Y: {yAxisLabel} \ X: {xAxisLabel}</span>
          <div className="flex items-center gap-2">
            <span className="text-[10px]">Min: {formatValue(min)}</span>
            <div className="w-20 h-2 rounded bg-gradient-to-r from-red-500 via-slate-600 to-emerald-500" />
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
          <div className="p-2 text-xs font-semibold text-slate-400 text-center" />

          {/* X Axis column headers */}
          {xAxisValues.map((xVal, xi) => (
            <div
              key={`col-${xi}`}
              className="p-1.5 text-xs font-semibold text-slate-300 text-center bg-slate-900/60 rounded"
            >
              {xVal}
            </div>
          ))}

          {/* Rows */}
          {yAxisValues.map((yVal, yi) => (
            <React.Fragment key={`row-${yi}`}>
              {/* Y Axis row header */}
              <div className="p-2 text-xs font-semibold text-slate-300 flex items-center justify-end pr-3 bg-slate-900/60 rounded">
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
                      isBest ? 'ring-2 ring-blue-400 ring-offset-2 ring-offset-slate-950 font-bold text-white' : 'text-slate-100'
                    }`}
                    title={`Y: ${yVal}, X: ${xAxisValues[xi]} => ${val !== null ? formatValue(val) : 'N/A'}${isBest ? ' (BEST)' : ''}`}
                  >
                    {val !== null ? formatValue(val) : '—'}
                    {isBest && <span className="text-[9px] uppercase tracking-tighter text-blue-300">best</span>}
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
