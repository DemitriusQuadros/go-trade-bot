# Spec TUI-05 — Page 4: Backtest Launcher (`cmd/console/pages/backtestlauncher.go`)

## Overview

Strategy selector, parameter inputs (date range, symbol, timeframe), a Dry-Run-only config panel, a
progress bar with ETA, and a recent-backtests list. This is a wholly new page — none of the three
existing console pages had any backtest-related UI. It depends entirely on Phase 2's backtest engine and
persistence work landing first (see Dependencies); as of this spec, `app/handler/web/backtest/` does not
exist in the working tree.

## Current Behavior (verified)

- No backtest-related file exists anywhere under `cmd/console/` today (confirmed by directory listing).
- No `app/handler/web/backtest/handler.go` exists yet (confirmed absent from `app/handler/web/`'s
  directory listing) — Phase 2 Spec 10 defines its target shape but it is not present/verified-working
  code as of this spec.
- `app/strategies/registry.go` [NEW, per blueprint §3.2] does not exist yet either — the strategy
  selector's data source (`Registry()` keys) is itself a Phase 1 deliverable (Spec 06) that this page
  depends on transitively via `ListStrategies` (existing strategies already carry a valid
  `StrategyName`), not directly on the registry package (`cmd/console` never imports `app/strategies`,
  consistent with ADR-007 — the selector lists **configured strategies** via `GET /strategy`, not raw
  registry keys via a hypothetical `GET /strategies/registry` endpoint that doesn't exist and isn't
  needed for this page's stated purpose).

## Target Behavior

```go
// cmd/console/pages/backtestlauncher.go
package pages

type BacktestLauncherPage struct {
    Header          *widgets.Paragraph
    TabPane         *widgets.TabPane
    Dependencies    *dependencies.Dependencies
    strategies      []apiclient.StrategyView
    selectedStrat   int
    form            LauncherForm // start date, end date, symbol, timeframe, dry-run params
    running         bool
    progressMessage string // "Running... (no server-reported progress, see AC#4)"
    recent          []apiclient.BacktestRunView
    preselectedID   *uint // set by Page 2's [b] handoff (tui-03) — pre-selects a strategy on entry
}

type LauncherForm struct {
    StartDate     time.Time
    EndDate       time.Time
    Symbol        string // single symbol only — see Judgment Call update below (backend-01 has no
                          // "All symbols" batch-run request shape)
    Timeframe     string // e.g. "1m", "5m", "1h" - independent of the strategy's own Cycle
    DryRun        bool   // toggles visibility of the panel below
    SlippagePct   float64
    FeePct        float64
    FillDelayMs   int
}

func (p *BacktestLauncherPage) Render() ui.Drawable
func (p *BacktestLauncherPage) HandleEvent(e ui.Event) error // arrow keys: selector nav / field edit;
                                                               // [Enter]: submit run; [Tab]: next field
```

### Run submission flow

`[Enter]` (with a strategy selected and valid form fields) calls `Client.RunBacktest` (or `RunWalkForward`
if a walk-forward toggle is set — not shown in PRD's Page 4 description, so **not included** as a Page 4
control per this spec; walk-forward is Page 5's `[w]` action on an *existing* result, not a Page 4 launch
option — see Judgment Call). Because `RunBacktest` is synchronous over HTTP (Phase 2 Spec 10 AC#6) and can
take from seconds to low minutes, the page:
1. Sets `running = true`, renders an indeterminate/estimated progress bar (`widgets.Gauge`) immediately.
2. Issues `RunBacktest` in a goroutine (the render loop must not block on the HTTP call — termui's
   `ui.PollEvents()` loop needs to keep processing input, e.g. allowing the operator to still switch tabs
   away while a backtest runs in the background).
3. On completion, sets `running = false` and auto-navigates to Page 5 with the resulting `BacktestRunView`
   (PRD: "Results auto-navigate to Page 5 on completion").
4. On error, shows an inline error and stays on Page 4 (does not navigate to a nonexistent result).

## ASCII Wireframe

```
┌ go-trade-bot ── 2026-08-31 14:36:40 ── ● CONNECTED ── Balance: $12,480.55 ──────────────────────────┐
├ [1 Dashboard] [2 Strategies] [3 Positions] [4 Backtest] [5 Results] [6 Log] ─────────────────────────┤
│ Strategy: [< grid_btc >]                Symbol: [BTCUSDT]        Timeframe: [< 1m >]                │
│ Start:    [2025-08-01]                  End:    [2026-08-01]                                        │
│                                                                                                        │
│ ┌ Dry-Run Config (Mode = Dry-Run only) ──────────────────────────────────────────────────────────┐   │
│ │ Slippage %: [0.05]     Fee %: [0.10]     Fill Delay (ms): [250]                                 │   │
│ └────────────────────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                                        │
│ [Enter] Run backtest                                                                                  │
│ ████████████████████░░░░░░░░░░░░░░  Running grid_btc over 12mo of BTCUSDT 1m data...                 │
│                                                                                                        │
│ Recent Backtests ─────────────────────────────────────────────────────────────────────────────────── │
│ Strategy    │ Range                   │ Sharpe │ Result │ Report                                      │
│ grid_btc    │ 2024-08-01..2025-08-01  │ 1.24   │ PASS   │ /reports/grid_btc_20260830.html            │
│ scalp_eth   │ 2025-01-01..2025-07-01  │ 0.41   │ FAIL   │ /reports/scalp_eth_20260828.html           │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

## Acceptance Criteria

1. **Given** the strategy selector shows all strategies from `ListStrategies`, **when** arrow keys move
   the selection, **then** the Symbol field's default value updates to the newly-selected strategy's first
   `MonitoredSymbols` entry (operator can still override it, e.g. typing `"All"`).
2. **Given** the selected strategy's persisted `Mode != "dryrun"`, **when** the page renders, **then** the
   Dry-Run config panel (slippage/fee/fill-delay) is hidden — PRD: "visible only in Dry-Run mode." Note
   this refers to the **backtest run's simulated mode**, not necessarily the strategy's own persisted
   `Mode` field; this spec treats the launcher's own `form.DryRun` toggle (independent of the strategy's
   live `Mode`) as the actual visibility condition, since a `Live`-mode strategy can still be backtested
   with Dry-Run-style friction parameters for research purposes — flagged as a judgment call since PRD's
   phrasing is ambiguous about which "mode" gates the panel.
3. **Given** `[Enter]` is pressed with `EndDate < StartDate`, **when** the form validates, **then** the
   run is not submitted and an inline validation error appears next to the date fields (never a silent
   no-op or a submitted request that the backend would reject).
4. **Given** `RunBacktest` is in flight, **when** the progress bar renders, **then** it shows an
   **indeterminate/estimated** fill (e.g. a fixed animation, or elapsed-time-based heuristic against the
   requested date range's typical duration) rather than a literal server-reported percentage — **gap**:
   Phase 2 Spec 10's synchronous `POST /backtest` has no progress-reporting mechanism (no SSE/polling
   sub-resource); a true ETA requires either the backend adding one (out of scope for Spec 10) or the TUI
   estimating from historical run durations for similar date ranges. This spec adopts the latter as a
   non-blocking approximation, explicitly not a literal percentage-complete signal.
5. **Given** a completed run, **when** the page auto-navigates to Page 5, **then** it passes the resulting
   `BacktestRunView` directly (no extra `GetBacktest` round-trip needed, since `RunBacktest`'s response
   already contains the full result per Spec 10 AC#1).
6. **Given** the recent-backtests list, **when** it renders each row's "Result" column, **then** it shows
   `PASS`/`FAIL` from `BacktestRunView.Passed` (Spec 10's threshold-policy boolean), colored green/red
   respectively — not a raw boolean or the numeric Sharpe/Drawdown values alone.
7. **Edge case — Page 2's `[b]` handoff arrives with a strategy pre-selected**: **given**
   `preselectedID != nil` on page entry, **when** the page first renders, **then** the strategy selector
   starts on that strategy (not index 0) and the Symbol field defaults accordingly — matching PRD's
   implied workflow of drilling from a strategy row straight into launching its backtest.
8. **Edge case — no strategies exist**: **given** `ListStrategies` returns empty, **when** the page
   renders, **then** the strategy selector shows "No strategies available" and `[Enter]` is a no-op with
   an inline message rather than attempting to submit a run with no strategy ID.
9. **Given** the operator selects `"All"` in the Symbol field for a strategy with multiple
   `MonitoredSymbols`, **when** `[Enter]` submits, **then** the page runs one `RunBacktest` call **per
   symbol sequentially** (not a single request with a list) — `backend-01-tui-consumed-api-endpoints.md`
   §4's finalized `POST /backtest` body takes exactly one `symbol` string, with no batch/list variant. The
   progress bar reflects "Running symbol 2 of 3…" during this sequential loop, and the page auto-navigates
   to Page 5 showing the **last** completed run once all symbols finish, with the recent-backtests list
   (unchanged, single-run-per-row) picking up all N resulting rows on its next refresh.
10. **Given** `RunBacktest` returns `422` (per `backend-01` AC#7 — no imported candle data for the
    requested symbol/timeframe), **when** the page handles the response, **then** it shows a distinct
    message ("No historical data for BTCUSDT/1m — run `cmd/candleimport` first") rather than the generic
    validation-error treatment used for `400` responses (Acceptance Criterion #3) — these are different
    failure classes and should read differently to the operator.

## Out of Scope

- A walk-forward toggle on the launch form itself — PRD Page 4's description lists only single-run
  parameters; walk-forward is described under Page 5's `[w]` keybinding as acting on an *existing* result,
  not as a Page 4 launch mode. See Judgment Call.
- Symbol/timeframe autocomplete or validation against the exchange's actual listed symbols — inputs are
  free-text/selector-from-known-strategy-symbols only; no live exchange-symbol-list lookup.
- Cancelling an in-flight backtest run — `RunBacktest`'s synchronous HTTP call has no cancel/abort
  endpoint (Spec 10 doesn't define one); once submitted, the operator waits for completion or failure.

## Dependencies

- `tui-01-apiclient.md` — `ListStrategies`, `RunBacktest`, `RunWalkForward`, `ListBacktests`.
- Phase 2 Spec 10 (`docs/specs/phase-2/10-backtest-persistence-and-api.md`) — the entire backend surface
  this page depends on; **this page cannot be implemented before Phase 2 lands**, per the blueprint's own
  Phase 3 gap note (§2.5: "wiring the Backtest Launcher/Results pages to a backtest engine that doesn't
  exist yet — hard dependency on §2.3").
- `docs/specs/phase-3/backend-01-tui-consumed-api-endpoints.md` §4 — finalized `POST /backtest`/
  `POST /backtest/walkforward` request bodies this page's form must construct exactly, including the
  `422` vs `400` error distinction (Acceptance Criterion #10).
- `tui-03-page-strategies.md` — `[b]` keybinding's cross-page handoff into `preselectedID`.
- `tui-06-page-backtest-results.md` — the auto-navigation target on run completion.

## Judgment Calls

1. **Walk-forward is not exposed as a Page 4 launch option**, only as Page 5's `[w]` action on a completed
   single-run result (matching PRD's literal Page 4/Page 5 keybinding split: Page 4 has no `[w]` mentioned,
   Page 5 does). This means launching a walk-forward validation always requires first running (or having
   previously run) a regular backtest and viewing its result, then pressing `[w]` there — there is no direct
   "launch a walk-forward from scratch" path on Page 4. If that's not the intended operator workflow, this
   should be raised as a PRD clarification rather than assumed during implementation.
2. **"All symbols from strategy config" (PRD's exact Page 4 wording) is implemented as a client-side
   sequential loop over `RunBacktest`, one call per symbol** — `backend-01`'s finalized `POST /backtest`
   body (§4) has no list/batch symbol field, only a single `symbol` string. This spec's Acceptance
   Criterion #9 is the concrete resolution, but it's worth flagging that this makes an "All symbols" run
   for a strategy with, say, 5 monitored symbols take 5x as long (each a separate synchronous HTTP call,
   Spec 10 AC#6) with no aggregate progress/result view beyond "the last one completed, check the recent
   list for the rest" — a materially worse experience than a single batch endpoint would provide. Flagging
   as a candidate follow-up (a batch `POST /backtest/batch` endpoint) rather than blocking this phase on it.
