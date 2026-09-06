# Spec TUI-01 (Phase 4) — Page 7: Optimization Results (`cmd/console/pages/optimizeresults.go`)

## Overview

PRD §4.3 ("Hyperparameter optimization... report best-performing configuration") and §7 Phase 4
("Optimization results page in TUI: parameter heatmap, best config highlighted") describe a new page that
displays the outcome of a grid search across a strategy's config parameters — e.g. RSI period 10–20 crossed
with stop-loss 1–3% for the Grid strategy — as a 2D table where each cell is color-coded by a chosen
performance metric (default: Sharpe Ratio, matching Phase 2's `MetricsProvider`), with the single
best-performing cell visually highlighted distinctly from the rest of the heatmap coloring.

This is **additive** to Phase 3's six-page tab set (`docs/specs/phase-3/tui-09-navigation-and-wiring.md`),
not a replacement of any existing page. It becomes **Page 7**, bound to `<F7>`, appended after Page 6
(Execution Log) in `master.go`'s `RegisterPages` slice and `main.go`'s `widgets.NewTabPane` call.

**Final contract source of truth**: `docs/specs/phase-4/backend-01-optimization-api-endpoints.md` (not yet
present in the working tree at spec-writing time — `docs/specs/phase-4/` had no files before this one).
This spec defines its own expected request/response shapes below, modeled on Phase 3's
`backend-01-tui-consumed-api-endpoints.md` style and the blueprint's `app/usecase/optimize/usecase.go`
description (§4, Phase 4 file list) — **reconcile against the real backend spec before implementation if
shapes diverge.**

## Current Behavior (verified)

- No `cmd/console/pages/optimizeresults.go` exists (confirmed — `find cmd/console/pages` lists only
  `master.go`, `status.go`, `openorders.go`, `performance.go`, none of which are the Phase 3 six-page
  rewrite either; Phase 3's own `tui-*.md` specs have not been implemented in the working tree as of this
  writing — `cmd/console/apiclient/` does not exist, `dependencies.go` still holds `*gorm.DB`). This spec
  is written against Phase 3's **target** shapes (`apiclient.Client`, the `Page`/`EventHandler` interfaces,
  `dependencies.Dependencies{Cfg, API}`) as the assumed foundation, since Phase 4 is explicitly downstream
  of Phase 3 landing first.
- No `app/usecase/optimize/` package exists (confirmed by directory listing of `app/usecase/`) — this is a
  net-new Phase 4 backend deliverable per the blueprint (§4, Phase 4 New packages/files list), not
  something this TUI spec can treat as already-wired.
- No `POST /optimize` or equivalent endpoint exists anywhere in `app/handler/web/` (confirmed by grep for
  "optimiz" across `app/` — zero hits outside this spec-writing pass).
- `docs/specs/phase-2/06-metrics-provider-acl.md`'s `BacktestMetrics{SharpeRatio, MaxDrawdownPct,
  WinRatePct, ProfitFactor, TotalTrades, ...}` is the existing per-run metrics shape; optimization reuses
  this per-combination (blueprint: "reuses Phase 2's backtest engine per combination"), so this page's
  ranking metric is one of these four fields, not a new metric type.

## Target Behavior

### Assumed backend contract (flag: reconcile against `backend-01-optimization-api-endpoints.md`)

```
POST /optimize
  Body: {
    "strategy_id": 3,
    "symbol": "BTCUSDT",
    "timeframe": "5m",
    "start_date": "2025-08-01",
    "end_date": "2026-08-01",
    "param_grid": {
      "rsi_period":   {"from": 10, "to": 20, "step": 2},
      "stop_loss_pct": {"from": 1.0, "to": 3.0, "step": 0.5}
    },
    "rank_metric": "sharpe_ratio"   // "sharpe_ratio" | "max_drawdown_pct" | "win_rate_pct" | "profit_factor"
  }
  Returns: 202 {"optimization_id": 17}   // async — a full grid search is many sequential backtests
                                          // (blueprint: "reuses Phase 2's backtest engine per combination"),
                                          // each of which is itself already a synchronous multi-second-to-
                                          // minutes call per Spec 10 AC#6 — a grid of, say, 6x5=30
                                          // combinations could take 30x a single backtest's duration, which
                                          // is judged too long for a single synchronous HTTP round-trip
                                          // (unlike RunBacktest's own long-but-bounded synchronous design).
                                          // See Judgment Call #1.
  Errors: 400 (bad grid shape, e.g. step <= 0 or from > to), 422 (no historical data, same class as
          POST /backtest's 422 per backend-01 §4 AC#7)

GET /optimize/{id}
  Returns: 200 {
    "id": 17,
    "strategy_id": 3,
    "status": "running" | "completed" | "failed",
    "rank_metric": "sharpe_ratio",
    "param_grid": { ... echoed from request ... },
    "results": [
      {"params": {"rsi_period": 10, "stop_loss_pct": 1.0}, "metrics": {BacktestMetrics-shaped}},
      {"params": {"rsi_period": 10, "stop_loss_pct": 1.5}, "metrics": {...}},
      ...
    ],
    "best_index": 14   // index into results[] — resolved server-side, not re-derived client-side, so the
                        // TUI and any other consumer agree on tie-breaking rules the client shouldn't
                        // have to replicate
  }
  Errors: 404 (unknown id)

GET /optimize?strategy_id=3
  Returns: 200 [] {id, strategy_id, status, created_at, rank_metric}   // list view, for a future
           "recent optimization runs" panel — not required by this page's v1 wireframe below, but included
           for symmetry with GET /backtest?strategy_id= (tui-05); may be trimmed if unused.
```

### `cmd/console/apiclient` additions

```go
// cmd/console/apiclient/client.go additions
func (c *Client) RunOptimization(ctx context.Context, req RunOptimizationRequest) (OptimizationRunView, error)
    // POST /optimize — per the assumed contract above, this returns 202 with only an ID (async), NOT a
    // full result like RunBacktest's 200. Client uses the DEFAULT 3s timeout here (unlike RunBacktest's
    // 10-minute override, tui-01 Phase 3 AC#6) since this call only enqueues work, it doesn't wait for it.

func (c *Client) GetOptimization(ctx context.Context, id uint) (OptimizationRunView, error)
    // GET /optimize/{id} — polled by the page every 2s while status == "running" (see Acceptance Criteria).

type RunOptimizationRequest struct {
    StrategyID uint
    Symbol     string
    Timeframe  string
    StartDate  time.Time
    EndDate    time.Time
    ParamGrid  map[string]ParamRange // key = config field name, e.g. "rsi_period"
    RankMetric string                // "sharpe_ratio" | "max_drawdown_pct" | "win_rate_pct" | "profit_factor"
}

type ParamRange struct {
    From float64
    To   float64
    Step float64
}

type OptimizationRunView struct {
    ID         uint
    StrategyID uint
    Status     string // "running" | "completed" | "failed"
    RankMetric string
    ParamGrid  map[string]ParamRange
    Results    []OptimizationResultView
    BestIndex  int
}

type OptimizationResultView struct {
    Params  map[string]float64 // e.g. {"rsi_period": 14, "stop_loss_pct": 2.0}
    Metrics BacktestMetricsView // reuses tui-06 (Phase 3)'s existing metrics view shape — SharpeRatio,
                                // MaxDrawdownPct, WinRatePct, ProfitFactor, TotalTrades, TotalReturnPct
}
```

### Page struct

```go
// cmd/console/pages/optimizeresults.go
package pages

type OptimizeResultsPage struct {
    Header        *widgets.Paragraph
    TabPane       *widgets.TabPane
    Dependencies  *dependencies.Dependencies
    strategies    []apiclient.StrategyView
    selectedStrat int
    form          OptimizeForm      // strategy, symbol, timeframe, date range, two param ranges, rank metric
    running       bool
    runID         *uint
    result        *apiclient.OptimizationRunView
    axisX, axisY  string            // the two param_grid keys chosen for the 2D heatmap's rows/columns —
                                     // see Judgment Call #2 for the >2-parameter case
}

type OptimizeForm struct {
    StartDate, EndDate time.Time
    Symbol, Timeframe  string
    ParamX, ParamY     string       // param names, e.g. "rsi_period", "stop_loss_pct"
    RangeX, RangeY     apiclient.ParamRange
    RankMetric         string
}

func (p *OptimizeResultsPage) Render() ui.Drawable
func (p *OptimizeResultsPage) HandleEvent(e ui.Event) error // form nav/edit + [Enter] submit + [m] cycle
                                                              // rank metric + [F1-F7] page switch
func (p *OptimizeResultsPage) StopSync()
func (p *OptimizeResultsPage) StartSync() // starts the 2s poll-until-completed goroutine once runID is set
```

### Heatmap rendering (Judgment Call #3 — no true heatmap widget in `termui`)

`termui/v3` has no 2D color-mapped grid/heatmap widget (verified against the same widget survey
`tui-08-shared-components.md` (Phase 3) did for sparklines — `termui/v3.1.0`'s `widgets/` package contains
`Table`, `List`, `Gauge`, `BarChart`, `Sparkline`, `Plot`, `PieChart`, `Tree`, none of which natively support
per-cell background-color mapping from a continuous value). The realistic option, as this task's brief
anticipates, is a `widgets.Table` where:
- Rows = one axis's parameter values (e.g. `stop_loss_pct`: 1.0, 1.5, 2.0, 2.5, 3.0).
- Columns = the other axis's parameter values (e.g. `rsi_period`: 10, 12, 14, 16, 18, 20).
- Each cell's **text** is the rank metric's value formatted to 2 decimals (e.g. `"1.24"` for Sharpe).
- Each cell's **background color** is bucketed from the metric's min-max range across all `results[]` into
  a small fixed palette `termui` actually supports as cell styling (`table.RowStyles[i]` sets a whole row's
  style in `termui/v3`, not a true per-cell style — **per-cell color requires building the table's `Rows
  [][]string` content and driving color via `ui.Buffer`-level custom rendering, since `widgets.Table`'s own
  `RowStyles` map is keyed by row index, not `(row, col)`; achieving per-cell heatmap color therefore
  requires either (a) a custom `ui.Drawable` that manually draws cell backgrounds via `buf.SetCell` calls in
  a grid layout not routed through `widgets.Table` at all, or (b) accepting per-ROW coloring only (color the
  whole row by that row's best cell, a coarser approximation)**. This spec adopts **(a)** — a custom
  `Drawable` (`buildHeatmapGrid`, see below) — as the correct implementation for genuine per-cell coloring,
  since per-row coloring would defeat the entire point of a 2D heatmap (PRD explicitly asks for a grid, not
  a list). This mirrors `tui-08`'s own precedent of accepting a custom `Drawable` implementation (there, for
  Out-of-Scope braille sparklines) when the built-in widget set falls short of PRD's literal ask.

```go
// cmd/console/components/heatmap.go
package components

// BuildHeatmapGrid renders a 2D parameter grid as a color-coded table using
// direct ui.Buffer cell writes (bypassing widgets.Table's row-only styling
// limitation). cellValues[y][x] holds the rank-metric value for
// (yAxisValues[y], xAxisValues[x]); bestY/bestX mark the single
// best-performing cell for the distinct highlight border.
func BuildHeatmapGrid(
    xAxisLabel string, xAxisValues []float64,
    yAxisLabel string, yAxisValues []float64,
    cellValues [][]float64,
    bestY, bestX int,
) ui.Drawable

// heatmapColor buckets a normalized [0,1] value (this cell's value scaled
// against the observed min/max across the whole grid) into termui's fixed
// 256-color-unaware ANSI palette (termui/v3 exposes ui.Color as a uint16
// mapped to the terminal's 16/256-color table, not arbitrary RGB) — a 5-step
// bucket from ui.ColorRed (worst) through ui.ColorYellow (mid) to
// ui.ColorGreen (best), consistent with the existing red=bad/green=good
// convention used everywhere else in this spec set (e.g. Page 3's P&L
// coloring).
func heatmapColor(normalized float64) ui.Color
```

## ASCII Wireframe

```
┌ go-trade-bot ── 2026-09-05 10:12:44 ── ● CONNECTED ── Balance: $12,480.55 ──────────────────────────┐
├ [1 Dash][2 Strat][3 Pos][4 BT][5 Results][6 Log][7 Optimize] ────────────────────────────────────────┤
│ Strategy: [< grid_btc >]   Symbol: [BTCUSDT]   Timeframe: [5m]   Metric: [< Sharpe Ratio >]          │
│ Start: [2025-08-01]  End: [2026-08-01]                                                                │
│ Param X (cols): rsi_period      [10 .. 20 step 2]                                                    │
│ Param Y (rows): stop_loss_pct   [1.0 .. 3.0 step 0.5]                                                │
│ [Enter] Run optimization   [m] cycle rank metric                                                      │
│                                                                                                        │
│ Sharpe Ratio heatmap — grid_btc / BTCUSDT / 5m ───────────────────────────────────────────────────── │
│                     rsi_period=10   =12    =14    =16    =18    =20                                   │
│ stop_loss_pct=1.0        0.61      0.74   0.88   0.95   0.79   0.60                                   │
│ stop_loss_pct=1.5        0.70      0.91   1.10   1.18   0.97   0.75                                   │
│ stop_loss_pct=2.0        0.82      1.05  ┏1.24┓  1.21   1.02   0.81                                   │
│ stop_loss_pct=2.5        0.68      0.88   1.09   1.15   0.90   0.66                                   │
│ stop_loss_pct=3.0        0.51      0.66   0.80   0.83   0.71   0.49                                   │
│                     [worst 0.49 ░░]────gradient────[best 1.24 ▓▓]                                     │
│                                                                                                        │
│ Best configuration: rsi_period=14, stop_loss_pct=2.0 → Sharpe 1.24, MaxDD 12.1%, WinRate 59.4%        │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
[Enter] run   [m] rank metric   [F1-F7] switch page   [q] quit
```
The `┏1.24┓`-style box drawing around the single best cell (rather than merely a color tone shared with
several near-best cells) is the "best-performing configuration visually highlighted" requirement from PRD
§7 — color alone is insufficient per this project's own accessibility convention (WCAG "color is not the
only means of conveying information" — the border/bracket marker plus the "Best configuration:" summary
line below the grid both exist so the best cell is identifiable without relying on color perception.

## Acceptance Criteria

1. **Given** the operator fills the form and presses `[Enter]`, **when** `RunOptimization` returns `202`
   with an `optimization_id`, **then** the page shows a "Running… (N combinations)" status line (N =
   `product of each ParamRange's step count`, computed client-side from the submitted grid) and starts
   polling `GetOptimization` every 2s.
2. **Given** a poll returns `status == "completed"`, **when** the page processes it, **then** it stops
   polling, renders the heatmap grid via `BuildHeatmapGrid`, and shows the "Best configuration:" summary
   line derived from `results[BestIndex]` — never re-deriving "best" client-side from a `min`/`max` scan
   (the server's `BestIndex` and its tie-breaking rule are authoritative, per the same principle as
   `BacktestRunView.Passed` being a server-computed boolean, not a client-recomputed one, in Phase 3's
   `tui-06` AC#4).
3. **Given** a poll returns `status == "failed"`, **when** the page processes it, **then** it stops polling
   and shows an inline error (`components.Error`) instead of an empty/stale heatmap.
4. **Given** the rank metric is changed via `[m]` (cycling `sharpe_ratio → max_drawdown_pct → win_rate_pct →
   profit_factor → sharpe_ratio`) **after** a result is already loaded, **when** the cycle completes,
   **then** the heatmap re-colors and re-highlights the best cell using the newly selected metric's values
   from the **same already-fetched** `results[]` — no new `RunOptimization` call is needed, since
   `OptimizationResultView.Metrics` carries all four metrics per combination already (matching
   `BacktestMetricsView`'s existing full shape), only the display/ranking changes. Note: this re-ranking is
   necessarily **client-side** since the server's `BestIndex` is fixed to whichever `rank_metric` was
   submitted in the original request — flagged as a minor inconsistency with Acceptance Criterion #2's
   "never re-derive best client-side" rule, acceptable here because this is a display-only "what if I'd
   ranked by X instead" exploration, not the authoritative run result.
5. **Given** `MaxDrawdownPct` is the active rank metric (where **lower** is better, unlike the other three
   metrics where higher is better), **when** the heatmap colors cells, **then** the green/red gradient
   direction inverts accordingly (greenest = lowest drawdown) — a naive "highest value = greenest" rule
   would color the worst-performing cell green for this one metric, which is worse than showing no color at
   all.
6. **Edge case — grid produces zero valid combinations** (e.g. `from > to` after client-side validation
   somehow bypassed, or every combination hits a `422`-class data gap): **when** the page receives a
   `completed` result with an empty `results[]`, **then** it shows `"No valid results — check parameter
   ranges and historical data availability"` rather than rendering an empty/zero-row heatmap grid.
7. **Edge case — single-parameter grid** (operator leaves Param Y's range as a single value, degenerating
   to a 1D sweep): **when** `yAxisValues` has length 1, **then** `BuildHeatmapGrid` renders a single-row
   heatmap (still valid, not an error) — a 1D parameter sweep is a legitimate degenerate case of the 2D
   grid, not a distinct code path.
8. **Given** `<F7>` is pressed from any other page, **when** the event loop processes it, **then** it
   switches directly to this page, consistent with Phase 3's `tui-09` AC#2 pattern extended to a 7th key.

## Out of Scope

- Three-or-more-dimensional parameter grids in a single run — PRD's example (RSI period × stop-loss %) is
  explicitly 2D; this page's form only exposes two param-range fields. A strategy config with more tunable
  parameters would need either multiple 2D slices (fixing all other params at a default) or is simply not
  representable in this page's v1 — see Judgment Call #2.
- Monte Carlo simulation (PRD §7's separate Phase 4 bullet, `app/engine/montecarlo.go`) — no TUI surface is
  named for it anywhere in PRD §4.5's page list; out of scope for this spec entirely, may warrant its own
  future spec if a TUI surface is later requested.
- Cancelling an in-flight optimization run — no cancel endpoint is assumed in this spec's contract (same
  posture as Phase 3 `tui-05`'s treatment of `RunBacktest`).
- Persisting/re-viewing past optimization runs beyond the single `GET /optimize?strategy_id=` list
  mentioned in the assumed contract — no "recent optimizations" panel is included in the v1 wireframe
  above; can be added later following Page 4's "Recent Backtests" precedent if desired.

## Dependencies

- `docs/specs/phase-4/backend-01-optimization-api-endpoints.md` — **authoritative** source for
  `POST /optimize` / `GET /optimize/{id}` shapes; this spec's own contract guess above must be reconciled
  against it once it exists.
- `docs/specs/phase-3/tui-01-apiclient.md` — extended here with `RunOptimization`/`GetOptimization`; base
  `Client`/`ErrUnreachable`/`ErrAPI` patterns reused as-is.
- `docs/specs/phase-3/tui-05-page-backtest-launcher.md`, `tui-06-page-backtest-results.md` — form-layout
  and long-running-call UX precedent (progress messaging, error-class distinction for 400 vs 422).
- `docs/specs/phase-3/tui-08-shared-components.md` — `components.Error`; this spec's own new
  `components/heatmap.go` follows the same "stateless render function, no internal caching" convention
  documented there.
- `docs/specs/phase-3/tui-09-navigation-and-wiring.md` — `Page`/`EventHandler` interfaces, F-key dispatch
  pattern extended from F1-F6 to F1-F7.
- `docs/architecture/refactoring-blueprint.md` §4 Phase 4 — `app/usecase/optimize/usecase.go` backend
  package this page's endpoints assume exists.

## Judgment Calls (flagged for explicit reconciliation)

1. **`POST /optimize` is modeled as asynchronous (202 + poll), unlike `POST /backtest`'s synchronous 200.**
   PRD/blueprint give no explicit guidance on this. Rationale: a grid search is N sequential backtests
   (blueprint: "reuses Phase 2's backtest engine per combination"), and a single backtest is already
   long-running enough that Phase 3's `RunBacktest` needed a dedicated 10-minute client timeout override
   (Phase 3 `tui-01` AC#6) — multiplying that by a grid of even a modest 5×6=30 combinations makes a single
   synchronous HTTP call impractical (30x the single-run duration, no response until fully done, no way for
   the TUI to show incremental progress within that call). If the real backend spec instead makes this
   synchronous (matching `POST /backtest`'s existing precedent for consistency), this page's polling loop
   and `202`-handling logic collapse to a single blocking call plus the existing progress-bar pattern from
   `tui-05` — a smaller change than the reverse direction would be, so this spec's async-first guess is the
   safer default to build against pending confirmation.
2. **Only two parameters are optimizable per run (2D grid only), with no stated resolution for strategies
   that expose more than two tunable config fields.** PRD's own example is exactly 2D (RSI period ×
   stop-loss %), so this spec doesn't invent a >2D UI (e.g. small-multiples of 2D heatmaps sliced by a third
   parameter) since nothing in PRD or the blueprint asks for it — flagging as a real gap if a strategy with
   3+ meaningfully-interacting parameters needs optimization later.
3. **Per-cell heatmap coloring requires a custom `ui.Drawable`, not `widgets.Table`.** This is a rendering
   judgment call, not a contract one: `termui/v3.1.0`'s `widgets.Table.RowStyles` is keyed by row index only
   (verified against the same widget-survey approach `tui-08` used for sparklines), so true per-`(row,col)`
   background coloring needs direct `ui.Buffer.SetCell` writes in a new component rather than reusing the
   existing table widget. This is more implementation work than the other five pages' Table-based layouts,
   flagged explicitly since it's the one place this phase's TUI work meaningfully exceeds "assemble existing
   `termui` widgets."
