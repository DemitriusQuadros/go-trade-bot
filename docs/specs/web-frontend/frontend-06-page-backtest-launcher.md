# Spec frontend-06 — Backtest Launcher Page (`web/src/pages/BacktestLauncher.tsx`)

## Overview

Phase C. Web equivalent of the deleted TUI's Page 4 (`docs/specs/phase-3/tui-05-page-backtest-launcher.md`):
strategy/symbol/timeframe/date-range form, a Dry-Run-friction-parameters panel, submit backtest or
walk-forward, recent-runs list. Per blueprint §7, the web version's meaningful improvement is a real
calendar date-range picker and symbol autocomplete sourced from the selected strategy's `MonitoredSymbols`,
replacing raw text-field date entry.

## Current Behavior

- No web backtest launcher exists. Feature-parity reference: `tui-05-page-backtest-launcher.md`.
- `POST /backtest` body: `{strategy_id, symbol, timeframe, start_date, end_date, initial_capital?,
  fill_policy?}` (verified against `app/handler/web/backtest/dto.go`'s `RunRequestDTO`) — synchronous,
  returns the full `BacktestRunResponse` on success. **Single `symbol` string only** — no batch/list
  variant, same constraint the deleted TUI's `tui-05` worked around with a client-side sequential loop
  (Acceptance Criterion #9 there).
- `POST /backtest/walkforward` body: `{strategy_id, symbol, timeframe, start_date, end_date,
  initial_capital?, train_months, test_months, step_months, fill_policy?}` (verified against
  `WalkForwardRequestDTO`) — **note**: field names are `train_months`/`test_months`/`step_months`, not the
  deleted TUI's assumed `train_window_days`/`test_window_days`/`step_days` — this spec corrects that
  divergence (the TUI's own spec was written before this shape settled and never got reconciled against it,
  since the TUI was deleted before that reconciliation happened).
- `GET /backtest?strategy_id=` lists recent runs for a strategy (required query param, `400` if omitted per
  the deleted TUI's `tui-01` Dependencies note on `backend-01`'s equivalent rule for the analogous
  `GET /optimize?strategy_id=`).
- No progress-reporting mechanism exists for either endpoint — both are long-running synchronous HTTP calls
  (Phase 2 Spec 10 AC#6: "may take multiple minutes"). No SSE/polling sub-resource exists for progress.

## Target Behavior

### Layout — component composition

```
BacktestLauncher.tsx
├── PageHeader — "Backtest Launcher"
├── LauncherForm
│    ├── StrategySelect — <select>, sourced from GET /strategy; pre-selected if the page was
│    │    entered via ?strategy_id= (frontend-04's "Run Backtest" row action handoff — this
│    │    spec's web equivalent of the TUI's [b]-keypress cross-page handoff, now a plain URL
│    │    query param instead of an in-memory field passed between page structs)
│    ├── SymbolAutocomplete — a real `<input list="...">` / combobox sourced from the selected
│    │    strategy's MonitoredSymbols (blueprint §7's named improvement over the TUI's raw text
│    │    field) — still free-text-overridable, not hard-validated against the exchange's live
│    │    symbol list (same non-goal `tui-05`'s Out of Scope named)
│    ├── TimeframeSelect — <select> (1m/5m/15m/1h/4h/1d), independent of the strategy's own Cycle
│    ├── DateRangePicker — a real calendar widget (component library choice left open, matching
│    │    the blueprint's "genuinely low-stakes" styling deferral — e.g. react-day-picker or a
│    │    hand-rolled two-<input type="date">s fallback if no extra dependency is wanted) —
│    │    blueprint §7's named improvement over the TUI's raw text-field date entry
│    ├── RunModeToggle — "Single run" | "Walk-forward" (a first-class toggle, resolving the
│    │    deleted TUI's tui-05 Judgment Call #1's awkward split where walk-forward had NO
│    │    Page-4 launch path at all and could only be triggered from an existing Page-5 result;
│    │    see Judgment Call below for why this spec chooses to fix that rather than preserve it)
│    ├── [Single run] DryRunConfigPanel — slippage %, fee %, fill-delay ms — visible per this
│    │    spec's form-level `DryRun` toggle (independent of the strategy's persisted Mode,
│    │    matching tui-05 AC#2's resolved ambiguity)
│    ├── [Walk-forward] WalkForwardConfigPanel — train_months / test_months / step_months inputs
│    │    (net-new relative to the TUI, which deferred these to backend defaults per tui-06's
│    │    "Phase 2 Spec 08's WalkForwardConfig defaults" judgment call — this spec exposes them
│    │    directly since a proper web form has room for it and the field names are already known)
│    └── Submit button — "Run Backtest" / "Run Walk-Forward" (label follows RunModeToggle)
├── RunProgressPanel — shown only while a run is in flight (see Target Behavior below)
└── RecentRunsList — table: Strategy | Symbol | Range | Sharpe | Result (PASS/FAIL badge) |
     Report link — one row per GET /backtest?strategy_id= entry, clicking a row navigates to
     /backtest/:id (BacktestResults, frontend-07)
```

### Progress representation (no server-side progress signal exists)

```tsx
// web/src/pages/BacktestLauncher.tsx (sketch)
async function handleSubmit(form: LauncherFormState) {
  setRunning(true);
  setProgressMessage(form.mode === 'walkforward' ? 'Running walk-forward…' : `Running ${form.symbol}…`);
  try {
    const result = form.mode === 'walkforward'
      ? await api.post<BacktestRun>('/backtest/walkforward', buildWalkForwardBody(form))
      : await api.post<BacktestRun>('/backtest', buildRunBody(form));
    navigate(`/backtest/${result.id}`); // auto-navigate to BacktestResults on completion,
                                          // matching the deleted TUI's PRD-cited requirement
  } catch (err) {
    if (err instanceof ApiError && err.status === 422) {
      setError(`No historical data for ${form.symbol}/${form.timeframe} — run cmd/candleimport first.`);
    } else if (err instanceof ApiError && err.status === 400) {
      setError('Invalid request — check the date range and form fields.');
    } else {
      setError(humanizeError(err));
    }
  } finally {
    setRunning(false);
  }
}
```

`RunProgressPanel` renders an **indeterminate** progress indicator (a CSS/SVG spinner or animated
indeterminate `<progress>` element — not a literal percentage), since neither `/backtest` nor
`/backtest/walkforward` reports incremental progress. This mirrors the deleted TUI's `tui-05` AC#4
"indeterminate/estimated fill, not a literal percentage" — the web version drops the TUI's elapsed-time
heuristic animation in favor of a plain honest spinner plus elapsed-time counter ("Running… 47s elapsed"),
judged clearer than fabricating a fake percentage bar (see Judgment Call).

The browser tab remains fully interactive during the request (unlike the TUI's termui render-loop
constraint that motivated its background-goroutine pattern) — `fetch` is naturally async, so no special
threading concern exists here; the operator can navigate to another page while a run is in flight, and
`RunProgressPanel`'s state simply lives in `BacktestLauncher`'s own component state, discarded if the
operator navigates away (no cross-page "run still going" indicator in v1 — see Out of Scope).

### "All symbols" sequential loop (carried forward from the TUI, same backend constraint)

```tsx
async function handleSubmitAllSymbols(form: LauncherFormState, symbols: string[]) {
  setRunning(true);
  let lastResult: BacktestRun | null = null;
  for (const [i, symbol] of symbols.entries()) {
    setProgressMessage(`Running symbol ${i + 1} of ${symbols.length}: ${symbol}…`);
    lastResult = await api.post<BacktestRun>('/backtest', { ...buildRunBody(form), symbol });
  }
  setRunning(false);
  if (lastResult) navigate(`/backtest/${lastResult.id}`); // RecentRunsList picks up all N rows on its own next refetch
}
```

## Acceptance Criteria

1. **Given** the strategy selector's value changes, **when** a new strategy is chosen, **then**
   `SymbolAutocomplete`'s default value updates to that strategy's first `MonitoredSymbols` entry (operator
   can still type a different value) — matching `tui-05` AC#1.
2. **Given** the form's own `DryRun` toggle is off, **when** the form renders, **then**
   `DryRunConfigPanel` is hidden — this is independent of the selected strategy's persisted `Mode`, matching
   `tui-05` AC#2's resolved ambiguity (the launcher's own toggle gates visibility, not the strategy's live
   mode).
3. **Given** `EndDate < StartDate` in the date-range picker, **when** the operator submits, **then** the form
   shows an inline validation error next to the date fields and makes **no** `POST` call — matching `tui-05`
   AC#3.
4. **Given** a completed single-run submission, **when** the response resolves, **then** the app navigates
   directly to `/backtest/<id>` using the response body's own data — no extra `GET /backtest/{id}`
   round-trip, matching `tui-05` AC#5.
5. **Given** the operator selects "All" for the symbol field (a strategy with multiple `MonitoredSymbols`),
   **when** the form submits, **then** one `POST /backtest` call fires **per symbol sequentially** (never a
   batch request), the progress panel shows "Running symbol N of M…", and on completion the app navigates to
   the **last** completed run's results page — matching `tui-05` AC#9 exactly (same backend single-symbol
   constraint applies to this web client too).
6. **Given** `POST /backtest` (or `/walkforward`) returns `422`, **when** the error is handled, **then** the
   form shows the specific "No historical data for X/Y — run cmd/candleimport first" message, distinct from
   the generic `400` validation-error copy — matching `tui-05` AC#10's error-class distinction.
7. **Given** `RecentRunsList` renders a row, **when** the "Result" column displays, **then** it shows a
   colored PASS/FAIL badge sourced from `BacktestRunResponse.passed` (server-computed boolean, never
   re-derived client-side from the raw Sharpe/drawdown numbers) — matching `tui-05` AC#6.
8. **Edge case — the page is entered via a `?strategy_id=` query param** (frontend-04's "Run Backtest" row
   action): **given** the param is present and resolves to a real strategy, **when** the page first renders,
   **then** `StrategySelect` starts on that strategy (not the first list entry) and `SymbolAutocomplete`
   defaults accordingly — web equivalent of `tui-05` AC#7's cross-page handoff.
9. **Edge case — no strategies exist**: **given** `GET /strategy` returns `[]`, **when** the page renders,
   **then** `StrategySelect` shows "No strategies available" (disabled), the submit button is disabled with
   an inline message, and no submission is possible — matching `tui-05` AC#8.
10. **Given** "Walk-forward" is selected on `RunModeToggle`, **when** the operator fills
    `WalkForwardConfigPanel` and submits, **then** `POST /backtest/walkforward` is called with
    `train_months`/`test_months`/`step_months` exactly as entered (not the deleted TUI's assumed
    `*_days` field names) — this spec's corrected field-name mapping per Current Behavior.
11. **Accessibility**: the calendar date-range picker is fully keyboard-operable (arrow keys move focus
    between days, `Enter` selects, `Escape` closes without discarding an already-confirmed range) — a real
    accessibility requirement any date-picker library choice must satisfy, not assumed for free; the
    indeterminate progress spinner has an `aria-live="polite"` region announcing state changes ("Running…",
    "Running symbol 2 of 3…", "Complete").

## Out of Scope

- Cancelling an in-flight run — no cancel/abort endpoint exists for either `/backtest` or
  `/backtest/walkforward`; same non-goal `tui-05`'s Out of Scope stated.
- Persisting/resuming a "run still in progress" indicator across a page navigation or browser refresh — the
  request is a plain in-flight `fetch`; navigating away or refreshing loses the in-page progress UI (the run
  itself continues server-side regardless and will show up in `RecentRunsList` once complete on a future
  visit). A more robust "job you can walk away from and check back on" model would require the async-job
  pattern backend-01 (optimization) introduced — not requested for `/backtest` itself and out of scope here.
- Exchange-symbol validation for `SymbolAutocomplete` — free-text override remains possible; no live
  exchange-symbol-list lookup.

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `api.post/get`, `RunBacktestRequest`/`WalkForwardRequest`/
  `BacktestRun` types.
- `frontend-04-page-strategies.md` — the "Run Backtest" row action's `?strategy_id=` handoff contract.
- `frontend-07-page-backtest-results.md` — the navigation target on run completion.
- `docs/specs/phase-3/tui-05-page-backtest-launcher.md` — feature/data-parity checklist; this spec corrects
  its walk-forward field-name assumption and resolves its Judgment Call #1 (see below).

## Judgment Calls (flagged for explicit reconciliation)

1. **Walk-forward is promoted to a first-class Phase-C launch option (`RunModeToggle`), not left as a
   result-only action the way the deleted TUI's `tui-05` Judgment Call #1 scoped it** (there, walk-forward
   was only reachable via Page 5's `[w]` action on an *already-completed* single-run result, since the PRD's
   literal Page 4 keybinding list never mentioned one). This spec deliberately does not preserve that
   awkward split: a browser form can trivially expose both run types side by side with no keybinding
   collision to avoid, and gating walk-forward behind "you must first run and view a regular backtest" is a
   worse web UX than it was ever a necessary TUI constraint. Flagging as a genuine scope decision (a new
   capability beyond strict TUI parity) rather than an oversight — confirm this is wanted before
   implementation, since `frontend-07`'s `[w]`-equivalent action on an existing result (see that spec) still
   also exists as a secondary path, so there are now two ways to launch a walk-forward run.
2. **Progress is shown as an indeterminate spinner plus an elapsed-time counter, not the deleted TUI's
   estimated-fill-bar animation.** `tui-05` AC#4 explicitly allowed "a fixed animation, or elapsed-time-based
   heuristic against the requested date range's typical duration." This spec picks the simpler of those two
   honest options rather than attempting to heuristically predict completion time from a small, un-vetted
   sample of past run durations that could easily mislead the operator (e.g. showing "80% done" right before
   a run that's actually going to take another 5 minutes) — flagging in case a "fake but reassuring" ETA
   estimate is preferred over honest indeterminacy.
