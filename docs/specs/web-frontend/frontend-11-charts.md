# Spec frontend-11 — Charts (`web/src/components/charts/*.tsx`)

## Overview

The `components/charts/` family consumed by multiple pages: `EquityCurveChart`, `DrawdownChart`,
`ParamHeatmap`, `MonteCarloDistribution`, `PnlHistoryChart`. Written as a dedicated spec per this task's
instruction, since these are this pivot's core payoff (blueprint §7: "no chart in the app is a
block-character/braille approximation anymore") and are each consumed by more than one page. Per ADR-008/§2
and §6, **Recharts** is the chosen library for every time-series chart; the parameter heatmap is a
hand-rolled CSS-grid component (no second charting dependency) — this spec makes that heatmap decision
concrete.

## Current Behavior

- No charts exist anywhere in this codebase's frontend surface (the deleted TUI's `tui-08` `BuildSparkline`
  was a block-character `termui.widgets.Sparkline`, explicitly not a "real chart" by any definition; it is
  being replaced, not ported).
- Recharts is not yet a dependency of `web/` (scaffolded in `frontend-01`, but no chart library is added
  there — this spec is where it's introduced).

## Target Behavior

### Shared conventions across every chart in this family

- **Library**: `recharts` (single version pinned in `web/package.json`, exact version left to
  Phase A/D implementation time per the blueprint's own "genuinely low-stakes" deferral on chart-library
  version pinning).
- **Responsive container**: every chart is wrapped in Recharts' `<ResponsiveContainer>` so it fills its
  parent's width — no fixed pixel widths, satisfying this project's "wide content scrolls in its own
  container, page body never scrolls horizontally" constraint by simply not being wide in the first place.
- **Tooltip**: every chart uses Recharts' `<Tooltip>` with a custom `content` renderer showing exact
  value + formatted timestamp — the "hover for exact price/time" capability blueprint §7 names as the
  Dashboard's specific improvement over the TUI, generalized here to every chart in the app.
- **Accessibility text alternative**: every chart component accepts an optional `summary?: string` prop
  rendered as a visually-hidden (`sr-only`) `<p>` immediately before the chart's SVG — satisfying each
  consuming page's Accessibility Acceptance Criterion (`frontend-03`, `frontend-07`) without duplicating the
  same pattern ad hoc per page.
- **Color**: green/red for positive/negative framing (equity gains, P&L) follows this project's established
  convention (Positions page, Backtest Results' `TotalReturnPct`) — exact hex values deferred to the
  dataviz palette this project should adopt at implementation time, not hardcoded in this spec.

### `EquityCurveChart`

```tsx
// web/src/components/charts/EquityCurveChart.tsx
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';

export interface EquityPoint { time: string; value: number }

interface EquityCurveChartProps {
  points: EquityPoint[];
  startingBalance?: number; // for a reference horizontal line, if provided
  summary?: string;
  height?: number; // default 280; a smaller value (e.g. 120) plus hidden axes
                     // produces the compact variant frontend-03's PairCard uses
}

export function EquityCurveChart({ points, startingBalance, summary, height = 280 }: EquityCurveChartProps) {
  if (points.length < 2) {
    return <p className="chart-empty-state">Not enough data to chart an equity curve.</p>; // tui-06 AC#2 parity
  }
  return (
    <figure>
      {summary && <p className="sr-only">{summary}</p>}
      <ResponsiveContainer width="100%" height={height}>
        <LineChart data={points}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="time" tickFormatter={formatTime} />
          <YAxis tickFormatter={formatCurrency} />
          <Tooltip formatter={formatCurrency} labelFormatter={formatTime} />
          <Line type="monotone" dataKey="value" stroke="var(--color-equity-line)" dot={false} />
        </LineChart>
      </ResponsiveContainer>
    </figure>
  );
}
```

**Data source note (reconciled)**: this spec's earlier draft flagged that `points` here was not guaranteed
to be the backend's own `BacktestMetrics.EquityCurve` — at that time, `GET /backtest/{id}` didn't expose the
field, so `frontend-07` reconstructed an approximate series from `trade_log` client-side. That gap is closed
by `docs/specs/web-frontend/backend-04-api-consistency-fixes.md` (Gap 2): `GET /backtest/{id}`'s
`equity_curve` field is now populated server-side (from a new `MetricsJSON` column persisting the full
computed `BacktestMetrics`, unconditionally included on every response) and `frontend-07` passes it straight
through as `EquityCurveChart`'s `points` prop with no reconstruction step. `EquityCurveChart` itself was
always agnostic to where `points` came from (it just renders whatever `EquityPoint[]` it's given), so this
component's own code is unchanged by the reconciliation — only the caller (`frontend-07`) and the accuracy
guarantee changed: `points` is now the backend's authoritative series, not an approximation.

### `DrawdownChart`

```tsx
// web/src/components/charts/DrawdownChart.tsx
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';

export interface DrawdownPoint { time: string; drawdownPct: number } // always <= 0 by convention

interface DrawdownChartProps {
  points: DrawdownPoint[];
  summary?: string;
  height?: number;
}

export function DrawdownChart({ points, summary, height = 200 }: DrawdownChartProps) {
  // Rendered as a filled AreaChart (not a bare Line) specifically because a
  // drawdown series' visual "depth below zero" reads more immediately as a
  // filled area than a thin line — a small, deliberate chart-type choice
  // distinct from EquityCurveChart's plain Line, chosen for what the data
  // means (an area you dipped into) rather than reusing the same chart type
  // everywhere for consistency's own sake.
  if (points.length < 2) {
    return <p className="chart-empty-state">Not enough data to chart drawdown.</p>;
  }
  return (
    <figure>
      {summary && <p className="sr-only">{summary}</p>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={points}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="time" tickFormatter={formatTime} />
          <YAxis tickFormatter={(v) => `${v}%`} />
          <Tooltip formatter={(v: number) => `${v.toFixed(2)}%`} labelFormatter={formatTime} />
          <Area type="monotone" dataKey="drawdownPct" stroke="var(--color-danger)" fill="var(--color-danger-fill)" />
        </AreaChart>
      </ResponsiveContainer>
    </figure>
  );
}
```

### `PnlHistoryChart`

```tsx
// web/src/components/charts/PnlHistoryChart.tsx
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell } from 'recharts';

export interface PnlHistoryPoint { periodStart: string; periodEnd: string; profit: number; trades: number }

interface PnlHistoryChartProps {
  points: PnlHistoryPoint[];
  bucket: 'daily' | 'weekly' | 'monthly';
  summary?: string;
  height?: number;
  // Multi-series overlay (blueprint §7's "multi-strategy overlay/comparison") is
  // explicitly deferred per frontend-04's Judgment Call #1 — this component's
  // v1 signature takes exactly one strategy's series, not `points: Record<string, PnlHistoryPoint[]>`.
}

export function PnlHistoryChart({ points, bucket, summary, height = 220 }: PnlHistoryChartProps) {
  // Rendered as a BAR chart, not a line — per-bucket P&L is a discrete
  // realized-or-not-per-period value (sparse rows, no zero-profit row for
  // empty buckets per backend-05 AC#5), which reads more honestly as
  // discrete bars than an interpolated line implying continuity between
  // sparse points that may be far apart in time.
  if (points.length === 0) {
    return <p className="chart-empty-state">No history yet.</p>; // tui-02 (Phase 4) AC#4 parity
  }
  return (
    <figure>
      {summary && <p className="sr-only">{summary}</p>}
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={points}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="periodStart" tickFormatter={(t) => formatByBucket(t, bucket)} />
          <YAxis tickFormatter={formatCurrency} />
          <Tooltip formatter={formatCurrency} labelFormatter={(t) => formatByBucket(t, bucket)} />
          <Bar dataKey="profit">
            {points.map((p, i) => (
              <Cell key={i} fill={p.profit >= 0 ? 'var(--color-success)' : 'var(--color-danger)'} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </figure>
  );
}
```

### `MonteCarloDistribution`

```tsx
// web/src/components/charts/MonteCarloDistribution.tsx
export interface DistributionStats { mean: number; median: number; min: number; max: number; p5: number; p95: number }

interface MonteCarloDistributionProps {
  label: string; // "Sharpe Ratio" | "Max Drawdown %" | "Total Return %"
  stats: DistributionStats;
  originalValue: number; // the backtest's own OriginalMetrics value for this metric, for comparison
  format?: (n: number) => string;
}

// Rendered as a hand-drawn horizontal box-plot-style summary using a small
// set of Recharts primitives (a single-category BarChart with a custom
// "floating bar" from p5 to p95, a ReferenceLine at median, and a scatter
// point at originalValue) rather than reaching for a dedicated box-plot
// library. Recharts has no native box-plot chart type, but unlike the
// heatmap case (below), a floating-bar-plus-reference-line composition is
// straightforward to build from existing Recharts primitives (BarChart with
// a computed [p5, p95] range dataKey, ReferenceLine, ReferenceDot) — this
// does NOT require the same "hand-roll outside Recharts entirely" escape
// hatch the heatmap needs, so it stays a Recharts-based component like its
// siblings in this file, just a more composed one.
export function MonteCarloDistribution({ label, stats, originalValue, format = String }: MonteCarloDistributionProps) {
  // ...
}
```

### `ParamHeatmap` — the hand-rolled CSS-grid decision, made concrete

Per blueprint §6: "Recharts has no native heatmap primitive. Rather than adding a second charting dependency
... for exactly one chart type, the optimization parameter grid ... is rendered as a plain CSS-grid of
colored cells." This spec is where that decision becomes an actual component contract, superseding the
deleted TUI's own `tui-01` (Phase 4) approach of hand-rolling a `termui.Drawable` via direct `ui.Buffer`
cell writes (necessary there because `widgets.Table.RowStyles` is row-indexed only) — a browser's native
`display: grid` gives genuine per-cell styling for free, with none of the TUI's row-vs-cell workaround
needed.

**Why CSS grid over other options considered:**
- **A second charting library just for one heatmap** (e.g. `visx`, `nivo`) — rejected per blueprint §6's own
  reasoning: disproportionate dependency weight for exactly one chart type in a 4-5-chart-type app.
- **An HTML `<table>` with per-cell inline `background-color`** — technically also viable (a `<table>` is
  semantically more "this is tabular data" than a `<div>` grid), and this spec's Judgment Call below flags
  this as the honest runner-up: a `<table>` gives free row/column header semantics for screen readers
  (`<th scope="row">`/`<th scope="col">`) that a bare CSS-grid `<div>` structure has to hand-roll via ARIA
  (`role="grid"`, `role="row"`, `role="gridcell"`, `aria-rowindex`/`aria-colindex`). **This spec chooses CSS
  grid over `<table>`** specifically because the best-cell border-highlight treatment (a box-shadow/border
  spanning a single cell without disturbing neighboring cells' borders) and the color-gradient-with-a-legend
  layout are both meaningfully easier to compose with `display: grid`'s explicit row/column track sizing than
  with `<table>`'s border-collapse model — but this is a close call, not an obvious one, and is flagged
  explicitly (Judgment Call) since the accessibility tree implications differ between the two approaches.

```tsx
// web/src/components/charts/ParamHeatmap.tsx
interface ParamHeatmapProps {
  xAxisLabel: string;
  xAxisValues: number[];
  yAxisLabel: string;
  yAxisValues: number[];
  cellValues: (number | null)[][]; // cellValues[y][x]; null = failed combination (backend-01 AC#7)
  bestY: number;
  bestX: number;
  higherIsBetter: boolean; // false for max_drawdown_pct — inverts the color gradient direction
  formatValue?: (n: number) => string; // default: n.toFixed(2)
}

export function ParamHeatmap({
  xAxisLabel, xAxisValues, yAxisLabel, yAxisValues, cellValues, bestY, bestX, higherIsBetter, formatValue = (n) => n.toFixed(2),
}: ParamHeatmapProps) {
  const flat = cellValues.flat().filter((v): v is number => v !== null);
  const min = Math.min(...flat);
  const max = Math.max(...flat);

  function colorFor(value: number | null): string {
    if (value === null) return 'var(--color-cell-failed)'; // distinct hatched/grey treatment, not
                                                             // silently blank — the operator must be
                                                             // able to see a combination errored out
    const normalized = max === min ? 0.5 : (value - min) / (max - min);
    const scaled = higherIsBetter ? normalized : 1 - normalized;
    return interpolateRedYellowGreen(scaled); // small hand-rolled interpolation function — red (0) →
                                                // yellow (0.5) → green (1), continuous, NOT the
                                                // deleted TUI's discrete 5-bucket ANSI palette (a
                                                // browser can do real continuous color, unlike
                                                // termui's fixed 16/256-color model)
  }

  return (
    <figure role="group" aria-label={`${xAxisLabel} by ${yAxisLabel} heatmap`}>
      <div
        className="param-heatmap"
        style={{
          display: 'grid',
          gridTemplateColumns: `auto repeat(${xAxisValues.length}, 1fr)`,
        }}
      >
        <div />
        {xAxisValues.map((x) => <div key={x} className="param-heatmap__col-label">{x}</div>)}
        {yAxisValues.map((y, yi) => (
          <>
            <div key={`label-${y}`} className="param-heatmap__row-label">{y}</div>
            {xAxisValues.map((x, xi) => {
              const value = cellValues[yi][xi];
              const isBest = yi === bestY && xi === bestX;
              return (
                <button
                  key={`${y}-${x}`}
                  type="button"
                  className={`param-heatmap__cell${isBest ? ' param-heatmap__cell--best' : ''}`}
                  style={{ backgroundColor: colorFor(value) }}
                  aria-label={`${xAxisLabel} ${x}, ${yAxisLabel} ${y}: ${value === null ? 'failed' : formatValue(value)}${isBest ? ' (best)' : ''}`}
                  tabIndex={0}
                >
                  {value === null ? '×' : formatValue(value)}
                </button>
              );
            })}
          </>
        ))}
      </div>
      <div className="param-heatmap__legend">
        <span>worst {formatValue(higherIsBetter ? min : max)}</span>
        <div className="param-heatmap__legend-gradient" />
        <span>best {formatValue(higherIsBetter ? max : min)}</span>
      </div>
    </figure>
  );
}
```

The single best cell gets a **border/outline treatment** (`.param-heatmap__cell--best`, e.g. a thick
contrasting border) **in addition to** its color, and its `aria-label` explicitly appends "(best)" — directly
porting the deleted TUI's own accessibility framing ("color alone is insufficient... the border/bracket
marker plus the 'Best configuration:' summary line both exist so the best cell is identifiable without
relying on color perception," `tui-01` Phase 4 spec) rather than treating it as newly invented here.

## Acceptance Criteria

1. **Given** `EquityCurveChart` receives 2+ points, **when** it renders, **then** hovering any point on the
   line shows an exact `$value` and formatted timestamp via the `Tooltip`.
2. **Given** `EquityCurveChart` receives fewer than 2 points, **when** it renders, **then** it shows the
   "Not enough data" message rather than attempting to render a degenerate/single-point line — matching
   `tui-06` AC#2's principle.
3. **Given** `PnlHistoryChart` receives a mix of positive and negative buckets, **when** it renders, **then**
   each bar is individually colored green or red per its own sign (via `<Cell>`), not a single fixed series
   color — a bar chart with per-bar coloring is the direct reason `Cell` overrides are used instead of a
   single `fill` prop on `<Bar>`.
4. **Given** `ParamHeatmap` receives a grid where one combination's `cellValues[y][x] === null` (a failed
   combination, backend-01 AC#7), **when** it renders, **then** that cell shows a distinct "failed" visual
   treatment (a `×` glyph plus a non-gradient fill color) and its `aria-label` states "failed" — a failed
   combination is never silently rendered as if it were a real (possibly falsely-extreme) metric value.
5. **Given** `higherIsBetter={false}` (the `max_drawdown_pct` ranking case), **when** `ParamHeatmap` colors
   cells, **then** the lowest raw value gets the "best" (greenest) color, not the highest — matching `tui-01`
   Phase 4 AC#5's inversion rule, now implemented as continuous interpolation rather than the TUI's discrete
   bucketing.
6. **Given** the heatmap's Y axis has exactly one value (a degenerate 1D sweep), **when** it renders,
   **then** it produces a single-row grid without erroring — matching `tui-01` Phase 4 AC#7.
7. **Given** every `ParamHeatmap` cell is keyboard-focused via `Tab`, **when** a screen reader announces the
   focused cell, **then** it reads the full `aria-label` ("RSI period 14, Stop-loss 2.0%: Sharpe 1.24") —
   verifying the accessibility contract Target Behavior describes is actually wired, not just documented.
8. **Given** any chart component's `summary` prop is provided, **when** the component renders, **then** the
   summary text is present in the DOM as a visually-hidden (not `display: none` — screen-reader-only,
   `.sr-only` clipping pattern) element immediately preceding the chart.

## Out of Scope

- Zoom/pan/brush interactivity beyond Recharts' default hover tooltip — no chart in this app ships a
  `<Brush>` or custom zoom control in v1 (matching `frontend-06`'s and `frontend-07`'s own Out of Scope
  entries for their respective chart consumers).
- Exporting a chart as an image (PNG/SVG download) — not requested by any page spec; CSV export of
  underlying tabular data (where applicable, e.g. `frontend-09`'s execution log) is a separate, already-
  specified capability, not a chart-image export.
- Multi-series overlay on `PnlHistoryChart` (blueprint §7's cross-strategy comparison) — explicitly deferred,
  same as `frontend-04`'s Judgment Call #1.

## Dependencies

- `docs/architecture/web-frontend-blueprint.md` §6 — the Recharts-for-time-series /
  hand-rolled-CSS-grid-for-heatmap decision this spec implements directly.
- `frontend-07-page-backtest-results.md` — `EquityCurveChart`/`DrawdownChart`/`MonteCarloDistribution`
  consumer, including the flagged equity-curve data-source gap.
- `frontend-08-page-optimization.md` — `ParamHeatmap` consumer.
- `frontend-04-page-strategies.md` — `PnlHistoryChart` consumer.
- `frontend-03-page-dashboard.md` — the compact-variant line chart inside `PairCard` (a smaller-height
  configuration of `EquityCurveChart`/a shared line-chart primitive, not a sixth distinct chart type).
- `docs/specs/phase-4/tui-01-page-optimization-results.md` — the accessibility framing (border-plus-label,
  not color-alone) ported directly for `ParamHeatmap`'s best-cell treatment.

## Judgment Calls (flagged for explicit reconciliation)

1. **`ParamHeatmap` uses a CSS-grid `<div>` structure with hand-rolled ARIA grid roles (via
   `role="group"`/button cells with descriptive `aria-label`s), not a semantic HTML `<table>`.** As discussed
   in Target Behavior, an actual `<table>` with `<th scope="row">`/`<th scope="col">` gives row/column header
   association to screen readers "for free" via native table semantics, which this spec's button-grid
   approach instead achieves by baking both axis values into each cell's own `aria-label` (more verbose per
   cell, but avoids needing `role="grid"`/`role="row"`/`role="gridcell"` ARIA scaffolding, which is easy to
   get subtly wrong). This is a genuine, debatable trade-off — flagging explicitly rather than asserting the
   chosen approach is obviously superior; an accessibility review at implementation time should confirm this
   choice actually reads well in a real screen reader (VoiceOver/NVDA) before treating it as final.
2. **Color interpolation is continuous (a real red→yellow→green RGB/HSL blend), not the deleted TUI's
   discrete 5-bucket ANSI palette.** This is a strict visual upgrade a browser affords for free (arbitrary
   RGB) that a 16/256-color terminal could never do — flagged only to note this is a deliberate divergence
   from the TUI's literal presentation, not an oversight in "porting" the TUI's heatmap logic; the underlying
   ranking/normalization math is otherwise unchanged.
