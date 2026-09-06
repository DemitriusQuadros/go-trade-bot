# Spec TUI-06 — Page 5: Backtest Results (`cmd/console/pages/backtestresults.go`)

## Overview

Displays one completed `BacktestRunView`: a top metrics row, an equity-curve sparkline, a drawdown
sparkline, a trade-log table, and three keybindings (`[h]` open HTML report, `[w]` run walk-forward,
`[s]` save to history). Entirely new page — no prior console page rendered any backtest data.

## Current Behavior (verified)

- No backtest-results rendering exists anywhere in `cmd/console/` today.
- `docs/specs/phase-2/06-metrics-provider-acl.md:46-60` defines `BacktestMetrics{SharpeRatio,
  MaxDrawdownPct, WinRatePct, ProfitFactor, TotalTrades, AvgTradeDuration, TotalReturnPct,
  EquityCurve []EquityPoint}` and `EquityPoint{Time, Value}` — this is the metrics shape this page
  renders, once exposed via `entities.BacktestRun`/`GET /backtest/{id}` (Spec 10).
- `docs/specs/phase-2/10-backtest-persistence-and-api.md` AC#5 — `ProfitFactor = +Inf` is serialized as
  the literal JSON string `"Infinity"` in the API response, not a numeric `+Inf` — this page's
  `ProfitFactor` cell must special-case that string rather than assuming a `float64` unmarshal always
  succeeds.
- `entities.BacktestRun.TradeLogJSON` (Spec 10) stores `[]metrics_provider.TradeLogEntry` — for a
  walk-forward run, the same field additionally embeds `WalkForwardResult.Windows` as a sibling field
  (Spec 10 AC#7) — this page's trade-log table must handle both shapes (single-run flat list vs.
  walk-forward's windowed structure) or explicitly render walk-forward results differently (see
  Acceptance Criterion #7).

## Target Behavior

```go
// cmd/console/pages/backtestresults.go
package pages

type BacktestResultsPage struct {
    Header       *widgets.Paragraph
    TabPane      *widgets.TabPane
    Dependencies *dependencies.Dependencies
    result       *apiclient.BacktestRunView // set by RunBacktest's auto-navigation or GET /backtest/{id}
}

func (p *BacktestResultsPage) SetResult(r apiclient.BacktestRunView) // called by backtestlauncher.go
func (p *BacktestResultsPage) Render() ui.Drawable
func (p *BacktestResultsPage) HandleEvent(e ui.Event) error // [h]/[w]/[s]
```

### `[h]` — open HTML report

Calls the OS's default-browser opener (`open` on macOS, `xdg-open` on Linux, `start` on Windows — Go's
`os/exec` dispatched by `runtime.GOOS`) on `result.HTMLReportPath`. **Not** an HTTP call to `cmd/api` —
this is a local filesystem path opened by the console process itself, which implies the console and
`cmd/api` (and the report file) must share a filesystem (true for this project's single-host homelab
deployment per PRD §9, but worth flagging as an environment assumption that breaks under a networked
split deployment).

### `[w]` — run walk-forward

Calls `Client.RunWalkForward` using the current result's strategy/symbol/date-range as the base
parameters (the walk-forward-specific window/step-size config is either a fixed default or a small
follow-up prompt — this spec defers to Phase 2 Spec 08's `WalkForwardConfig` defaults rather than
building a second full parameter form here, since PRD Page 5 shows no such form). Same long-running/
progress-bar treatment as Page 4's `RunBacktest` (tui-05).

### `[s]` — save to history

See Judgment Call — this spec treats `[s]` as a **no-op confirmation toast**, not a new write call, since
Spec 10's `POST /backtest` already persists every run synchronously (AC#1).

## ASCII Wireframe

```
┌ go-trade-bot ── 2026-08-31 14:41:09 ── ● CONNECTED ── Balance: $12,480.55 ──────────────────────────┐
├ [1 Dashboard] [2 Strategies] [3 Positions] [4 Backtest] [5 Results] [6 Log] ─────────────────────────┤
│ Sharpe: 1.24   MaxDD: 14.2%   WinRate: 58.3%   PF: 2.10   Trades: 84   TotalReturn: +37.6%           │
│                                                                                                        │
│ Equity Curve ──────────────────────────────────────────────────────────────────────────────────────  │
│ ▁▁▂▂▃▃▄▄▃▃▄▅▅▆▆▅▅▆▇▇▆▆▇█████▇▇████████▇▇████████████████████████████████████████████████████████████ │
│                                                                                                        │
│ Drawdown ──────────────────────────────────────────────────────────────────────────────────────────  │
│ ▁▁▁▂▃▄▃▂▁▁▂▃▄▅▄▃▂▁▁▁▂▃▂▁▁▁▁▁▂▃▄▅▆▅▄▃▂▁▁▁▂▃▄▃▂▁▁▁▁▁▁▁▂▃▄▃▂▁▁▁▁▁▁▁▁▁▂▃▂▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁ │
│                                                                                                        │
│ Trade Log ──────────────────────────────────────────────────────────────────────────────────────────│
│ Opened              │ Closed              │ Symbol  │ Entry    │ Exit     │ P&L    │ Dur.  │ Reason  │
│ 2025-08-02 09:14    │ 2025-08-02 11:40    │ BTCUSDT │ 60120.00 │ 61340.00 │ +58.20 │ 2h26m │ TP      │
│ 2025-08-03 02:05    │ 2025-08-03 04:12    │ BTCUSDT │ 60980.00 │ 59900.00 │ -41.10 │ 2h07m │ SL      │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
[h] open report   [w] walk-forward   [s] save to history   [F1-F6] switch page
```

## Acceptance Criteria

1. **Given** a `BacktestRunView` with `ProfitFactor` serialized as the string `"Infinity"` (Spec 10 AC#5),
   **when** the metrics row renders the PF cell, **then** it displays the literal text `∞` (or `"Inf"`)
   rather than throwing a parse error or displaying `"0.00"`.
2. **Given** `EquityCurve` has fewer than 2 points (a run so short it produced almost no data), **when**
   the equity-curve sparkline renders, **then** it shows a flat/minimal line rather than erroring on an
   empty or single-element slice passed to `BuildSparkline` (tui-08).
3. **Given** the drawdown sparkline, **when** it renders, **then** its values are computed client-side from
   `EquityCurve` as `(peak_so_far - value) / peak_so_far * 100` at each point (the API response carries
   `EquityCurve`, not a separately-serialized drawdown series per Spec 06's shape) — this is presentation
   math, not a new metric, and is safe to compute in `cmd/console` unlike EMA20's ACL-boundary concern
   (tui-01 Judgment Call #3 doesn't apply here since drawdown-from-equity is arithmetic, not an indicator
   library call).
4. **Given** `Passed == false`, **when** the metrics row renders, **then** the overall verdict is visually
   distinct (e.g. a red `"FAIL"` badge alongside the raw numbers) — matching Page 4's recent-backtests list
   treatment (tui-05 Acceptance Criterion #6) for visual consistency between the two pages.
5. **Given** `[h]` is pressed and `HTMLReportPath == ""` (Spec 10 AC#4: report generation can fail without
   failing the whole run), **when** the keybinding handler runs, **then** it shows an inline
   `"No report available for this run"` message rather than attempting to open an empty path.
6. **Given** `[w]` is pressed, **when** `RunWalkForward` completes, **then** the page's `result` is replaced
   with the new walk-forward `BacktestRunView` (`IsWalkForward: true`) and re-renders in place — no
   navigation away from Page 5.
7. **Edge case — walk-forward result's trade log table**: **given** `result.IsWalkForward == true`,
   **when** the trade-log section renders, **then** it shows the **aggregate** `TestMetrics`-derived trade
   log (concatenated across windows, per Spec 10 AC#7 / Spec 08 AC's window-concatenation rule) with an
   additional leftmost "Window" column identifying which walk-forward window each trade belongs to —
   distinguishing this from a flat single-run trade log, which has no such column.
8. **Edge case — `[s]` pressed**: **when** pressed, **then** a toast/inline message
   `"Already saved as run #<ID>"` appears — no HTTP call is made (see Judgment Call).

## Out of Scope

- In-TUI equity curve zooming/panning or exact-value tooltips — the sparkline is a fixed-resolution
  overview, matching every other sparkline's presentation elsewhere in this spec set.
- Exporting the trade log (CSV, etc.) from within the TUI — not named in PRD Page 5's keybinding list.

## Dependencies

- `tui-01-apiclient.md` — `RunWalkForward`, `GetBacktest`.
- `tui-05-page-backtest-launcher.md` — the primary entry point via auto-navigation on run completion.
- `tui-08-shared-components.md` — `BuildSparkline` for equity curve and drawdown.
- Phase 2 Spec 06 (`docs/specs/phase-2/06-metrics-provider-acl.md`), Spec 08 (walk-forward), Spec 10
  (persistence/API) — the full metrics/trade-log/walk-forward shape this page renders.
- `docs/specs/phase-3/backend-01-tui-consumed-api-endpoints.md` §4 — finalized `POST /backtest/walkforward`
  request body (`total_range`/`train_window_days`/`test_window_days`/`step_days`) that `[w]`'s handler must
  construct; note `backend-01` does not address the `[s]` "save to history" semantics question below, so
  that judgment call remains open even after reconciliation.

## Judgment Call

**`[s]` "save result to history" is treated as a no-op confirmation, not a new write call.** PRD Page 5's
exact wording ("`[s]` save result to history") predates Phase 2 Spec 10's resolved design, in which
`POST /backtest` already persists a `BacktestRun` row synchronously on every completed run (AC#1) — there
is no "unsaved" intermediate state a result can be in by the time it's rendered on Page 5 at all. If the
PRD's intent was instead something like "pin/star/mark this result as a reference baseline" (a genuinely
new piece of state, distinct from mere persistence), that requires a new field on `BacktestRun` (e.g.
`Pinned bool`) and a corresponding endpoint — not scoped here, and worth confirming with the concurrent
backend spec author before treating `[s]` as fully resolved.
