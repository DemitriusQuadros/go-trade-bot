# Spec frontend-08 — Optimization Page (`web/src/pages/Optimization.tsx`)

## Overview

**[Phase C]**: trigger form. **[Phase D]**: `ParamHeatmap` results view, best-config highlight. Web
equivalent of the deleted TUI's Phase 4 Page 7 (`docs/specs/phase-4/tui-01-page-optimization-results.md`).
Unlike that TUI spec (which had to guess at the backend contract, since `backend-01-optimization-api-endpoints.md`
hadn't landed yet when it was written), this spec is written against the **now-finalized**
`docs/specs/phase-4/backend-01-optimization-api-endpoints.md` and the actual live `app/handler/web/optimize/dto.go`
— no contract-guessing judgment calls carry over from the TUI spec's own Judgment Call #1 (async-vs-sync);
that question is now settled (async, confirmed below).

## Current Behavior

- No web optimization page exists. Feature-parity reference: `tui-01-page-optimization-results.md` (Phase 4).
- `POST /optimize` body (verified, `CreateOptimizationRequestDTO`): `{strategy_id, symbol, timeframe,
  start_date, end_date, initial_capital?, param_grid: {[key]: {min, max, step}}}` — **field names are
  `min`/`max`/`step`**, not the TUI spec's guessed `from`/`to`/`step`; this spec corrects that divergence.
  Returns `202 {id, status: "pending", total_combinations}` — confirmed async, resolving the TUI's own
  Judgment Call #1 in favor of its "safer default" guess.
- `GET /optimize/{id}` (verified, `StatusResponse`): `{id, status, progress, total_combinations, best_config,
  best_metrics, error_message}` — `best_config`/`best_metrics` are `null` until `status === "completed"`,
  matching backend-01 AC#4 exactly.
- `GET /optimize/{id}/results` (verified, `ResultsResponse`): `{id, best_config, best_metrics, grid: [{params,
  metrics}]}` — `409` if the run isn't `"completed"` yet (distinguishing "not ready" from `404` "doesn't
  exist," per backend-01 AC#6).
- `GET /optimize?strategy_id=` lists recent runs (`400` if `strategy_id` omitted).
- The TUI spec's own Judgment Call #2 (2D-grid-only, no 3+ parameter support) is **not resolved** by any
  landed backend spec — `param_grid` is a `map[string]ParamRangeDTO` with no arity limit server-side, but
  the TUI's UI design chose exactly two axes for the heatmap. This spec inherits the same 2D-only UI
  constraint (see Out of Scope) since nothing in the backend contract changed that shape.

## Target Behavior

### Layout — component composition

```
Optimization.tsx
├── PageHeader — "Hyperparameter Optimization"
├── [Phase C] OptimizeForm
│    ├── StrategySelect, SymbolAutocomplete, TimeframeSelect, DateRangePicker (shared with
│    │    frontend-06's launcher form — same components, not reimplemented)
│    ├── RankMetricSelect — sharpe_ratio | max_drawdown_pct | win_rate_pct | profit_factor
│    ├── ParamRangeRow × 2 (Param X, Param Y) — each: param-name text input (free text, matching
│    │    the strategy's config JSON keys — no schema-driven field list exists server-side, same
│    │    non-goal the TUI's own spec accepted) + Min / Max / Step numeric inputs
│    └── Submit — "Run Optimization" → POST /optimize
├── [Phase C] RunStatusPanel — shown once a run is submitted or when the page is entered via
│    /optimize/:id for an in-progress run
│    ├── "Running… (N combinations)" with progress bar (progress / total_combinations — a REAL
│    │    percentage this time, unlike frontend-06's backtest launcher, since GET /optimize/{id}
│    │    actually reports progress/total_combinations)
│    └── polls GET /optimize/{id} every 2s while status === "running" (matching tui-01 AC#1)
├── [Phase D] ResultsPanel — shown once status === "completed"
│    ├── RankMetricSelect (re-rankable client-side post-hoc — see Target Behavior below)
│    ├── ParamHeatmap (frontend-11) — 2D grid, rows = Param Y values, columns = Param X values,
│    │    cell color = normalized rank-metric value, best cell visually bordered/highlighted
│    │    (never color-alone, per WCAG — matching the TUI's own accessibility framing)
│    └── "Best configuration: <params> → Sharpe X, MaxDD Y%, WinRate Z%" summary line, sourced
│         from GET /optimize/{id}/results's best_config/best_metrics (server-authoritative,
│         never re-derived client-side except for the re-ranking case below)
└── [Phase D, conditional] "Apply this config" button — blueprint §7's named improvement over the
     TUI ("a one-click 'apply this config' action that PATCHes the winning parameters onto the
     live strategy — awkward-to-impossible as a terminal action") — see Target Behavior below.
```

### Client-side re-ranking (carrying forward `tui-01` AC#4 verbatim)

```tsx
// web/src/pages/Optimization.tsx (sketch)
function reRank(grid: OptimizationGridPoint[], metric: RankMetric): { best: OptimizationGridPoint; ordered: OptimizationGridPoint[] } {
  const valid = grid.filter((p) => p.metrics !== null);
  const higherIsBetter = metric !== 'max_drawdown_pct'; // inverted direction for drawdown (lower = better)
  const sorted = [...valid].sort((a, b) => {
    const av = (a.metrics as any)[metric] as number;
    const bv = (b.metrics as any)[metric] as number;
    return higherIsBetter ? bv - av : av - bv;
  });
  return { best: sorted[0], ordered: sorted };
}
```

Switching `RankMetricSelect` **after** results have loaded re-colors/re-highlights `ParamHeatmap` using the
already-fetched `grid[]` — no new `POST /optimize` call — matching `tui-01` AC#4's explicit acceptance that
this is a display-only "what if I'd ranked by X" exploration, distinct from the server's authoritative
`best_config` (which stays tied to whatever `rank_metric` was actually optimized for, since `POST /optimize`'s
request body has no `rank_metric` field at all in the verified backend contract — see Judgment Call #1).

### "Apply this config" action

```tsx
async function applyConfig(strategyId: number, bestConfig: Record<string, number>) {
  // PATCHes the winning param values into the strategy's persisted Configuration JSON,
  // merged with (not replacing) any existing config keys the optimization didn't touch.
  const current = await api.get<Strategy>(`/strategy/${strategyId}`);
  const merged = { ...(current.Configuration ?? {}), ...bestConfig };
  await api.put(`/strategy/${strategyId}`, { ...toStrategyFormBody(current), configuration: merged });
}
```

## Acceptance Criteria

### [Phase C]

1. **Given** the operator fills the form (strategy, symbol, timeframe, dates, two param ranges, rank
   metric) and submits, **when** `POST /optimize` returns `202` with `total_combinations`, **then**
   `RunStatusPanel` shows "Running… (N combinations)" and begins polling `GET /optimize/{id}` every 2s.
2. **Given** a `param_grid` entry with `step: 0` is submitted, **when** `POST /optimize` returns `400`,
   **then** the form shows an inline validation error on that field — though this spec's form should also
   validate `step > 0` client-side **before** submitting, to avoid a guaranteed-400 round-trip (same
   principle as `frontend-04`'s JSON-validation-before-submit).
3. **Given** the combined grid size (`((max-min)/step + 1)` for each axis, multiplied) exceeds the backend's
   hard cap, **when** `POST /optimize` returns `413`, **then** the form shows a distinct
   "Grid too large — narrow the ranges or increase the step size" message (not the generic `400` copy),
   distinguishing this failure class per backend-01 AC#3.
4. **Given** the page is navigated to directly via `/optimize/:id` (e.g. a bookmarked/shared link, or a
   `RecentOptimizationsList` row click — see Out of Scope on whether that list ships in v1), **when** the
   page mounts, **then** it calls `GET /optimize/{id}` immediately and, if `status === "running"`, resumes
   polling exactly as if the run had just been submitted in this same session.

### [Phase D]

5. **Given** a poll returns `status === "completed"`, **when** the page processes it, **then** it stops
   polling, fetches `GET /optimize/{id}/results`, renders `ParamHeatmap`, and shows the "Best configuration:"
   summary derived from `results.best_config`/`results.best_metrics` — never re-deriving "best" via a
   client-side min/max scan on initial load, matching `tui-01` AC#2.
6. **Given** a poll returns `status === "failed"`, **when** the page processes it, **then** it stops polling
   and shows `error_message` inline instead of an empty/stale heatmap — matching `tui-01` AC#3.
7. **Given** the rank metric is changed via `RankMetricSelect` **after** results are loaded, **when** the
   selection changes, **then** the heatmap re-colors/re-highlights using the already-fetched `grid[]`,
   client-side, with no new network call — matching `tui-01` AC#4 (and its explicitly-accepted minor
   inconsistency with "never re-derive client-side," scoped only to this specific what-if exploration).
8. **Given** `max_drawdown_pct` is the active rank metric, **when** the heatmap colors cells, **then** the
   color gradient direction inverts (greenest = lowest drawdown) — matching `tui-01` AC#5's inversion rule.
9. **Edge case — zero valid results**: **given** every combination failed (e.g. a misconfigured
   `param_grid` key matching no real strategy config field) and `status === "completed"` with an empty
   `grid[]`, **when** the results panel renders, **then** it shows "No valid results — check parameter
   ranges and historical data availability" rather than an empty/zero-row heatmap — matching `tui-01` AC#6.
10. **Edge case — single-value axis** (Param Y's range degenerates to one value), **when** `ParamHeatmap`
    renders, **then** it renders a valid single-row heatmap, not an error — matching `tui-01` AC#7.
11. **Given** the operator clicks "Apply this config," **when** the merge-and-`PUT` flow (Target Behavior)
    completes, **then** the strategy's persisted `Configuration` is updated with the winning parameter
    values merged over the existing config, and a confirmation toast confirms the write — this is new
    capability beyond the TUI (blueprint §7's named improvement), gated behind a `ConfirmDialog` (frontend-10)
    since it mutates a live strategy's configuration.
12. **Accessibility**: `ParamHeatmap`'s cells are keyboard-focusable (each cell is a `<button>` or has
    `tabindex="0"` with a role, not a bare `<div>`), and each cell's accessible name states its exact value
    ("RSI period 14, Stop-loss 2.0%: Sharpe 1.24" — not just a colored square), satisfying the same
    color-is-not-the-only-signal principle the TUI's own spec already called out for its border-highlight
    treatment of the best cell.

## Out of Scope

- Cancelling an in-progress optimization run — no cancel endpoint exists, matching backend-01's explicit
  Out of Scope.
- 3+ dimensional parameter grids — the 2D-only UI constraint carries over unchanged from `tui-01`'s Judgment
  Call #2; the backend's `param_grid` map has no arity limit, but this page's form only exposes two
  `ParamRangeRow`s.
- A `RecentOptimizationsList` panel (mirroring `frontend-06`'s `RecentRunsList`) consuming
  `GET /optimize?strategy_id=` — the finalized backend contract supports it, and it's a natural addition, but
  it wasn't in the TUI's v1 wireframe either (`tui-01`'s own Out of Scope named it as a possible future
  addition "following Page 4's 'Recent Backtests' precedent if desired") — deferred here for the same reason,
  not blocking anything else in this spec.

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `api.post/get/put`, `CreateOptimizationRequest`/
  `OptimizationStatusResponse`/`OptimizationResults` types (corrected `min`/`max`/`step` field names).
- `frontend-06-page-backtest-launcher.md` — shared `StrategySelect`/`SymbolAutocomplete`/`TimeframeSelect`/
  `DateRangePicker` components.
- `frontend-11-charts.md` — `ParamHeatmap`.
- `frontend-10-shared-components.md` — `ConfirmDialog` (for "Apply this config").
- `docs/specs/phase-4/backend-01-optimization-api-endpoints.md` — **finalized**, authoritative contract this
  spec is built directly against (no guessing required, unlike the TUI's own spec history).
- `docs/specs/phase-4/tui-01-page-optimization-results.md` — feature/data-parity checklist; this spec
  resolves its Judgment Call #1 (now confirmed async) and corrects its `param_grid` field-name guess.

## Judgment Calls (flagged for explicit reconciliation)

1. **`POST /optimize`'s request body has no `rank_metric` field in the verified backend contract**
   (`CreateOptimizationRequestDTO` has `strategy_id, symbol, timeframe, start_date, end_date,
   initial_capital, param_grid` — no rank-metric selector). The deleted TUI's own spec assumed one
   (`"rank_metric": "sharpe_ratio"` in its guessed request body) — this appears to be a genuine divergence
   between what the TUI spec assumed and what actually landed. This spec's `RankMetricSelect` therefore
   **cannot** actually influence which combination the server marks as `best_config`/`best_metrics` — the
   server picks "best" by some fixed internal rule (unconfirmed which metric, or whether it's
   Sharpe-by-default) regardless of what the operator selects on the form. This spec's UI still shows the
   selector (useful for the client-side re-ranking exploration, Acceptance Criterion #7) but this is a
   materially different feature than "choose your ranking metric before running" — flagging as a real
   product-expectation mismatch worth confirming with whoever owns `app/usecase/optimize/` before shipping:
   either the form's rank-metric selector is relabeled to make clear it only affects post-hoc client-side
   exploration, or backend-02 (hyperparameter optimization orchestration) needs a `rank_metric` request field
   added.
2. **"Apply this config" performs a read-then-merge-then-`PUT`, not a dedicated PATCH endpoint.** No
   `PATCH /strategy/{id}/configuration` (or similar narrow endpoint) exists — this spec reuses the existing
   full-object `PUT /strategy/{id}`, which requires re-sending every other field unchanged (name, symbols,
   status, mode, cycle) alongside the merged configuration. This is workable but carries a real race risk
   (a concurrent edit to the strategy between the `GET` and the `PUT` could be silently overwritten) — flagged
   as acceptable for a single-operator homelab tool (matching this project's general risk posture, ADR-009)
   but worth knowing about explicitly rather than assuming the PUT is atomic with respect to other clients.
