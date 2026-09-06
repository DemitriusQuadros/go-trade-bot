# Spec TUI-02 (Phase 4) — P&L History Sparkline in Page 2's Detail Overlay (`cmd/console/pages/strategies.go`)

## Overview

PRD §7 Phase 4: "TUI — P&L history sparkline per strategy (daily/weekly/monthly)", backed by "Strategy
performance history persistence (track live P&L per strategy over time in Postgres)". This is **not a new
page**. It is an addition to the existing detail overlay defined in
`docs/specs/phase-3/tui-03-page-strategies.md` (Page 2 — Strategies), which today (per that spec's Target
Behavior) already shows a config JSON preview plus "P&L since activation, total trades, win rate" as flat
numbers with no time-series view at all. This spec adds a sparkline widget to that same overlay, next to
those existing fields, plus a granularity toggle.

**Reconciled against `docs/specs/phase-4/backend-05-strategy-performance-history.md` (now final) on
2026-09-05.** That spec landed after this one's first draft; two real divergences were found and are fixed
below rather than left as a guess: (1) the query parameter is `bucket=`, not `granularity=`, and `symbol=`
is a **required** parameter (`400` if omitted) — the endpoint is scoped to a `(strategy, symbol)` pair, not
the strategy as a whole; (2) the response includes `period_end` alongside `period_start`, not just the
latter. Both are reflected in the Target Behavior below. The required `symbol` parameter also surfaces a
real design gap this draft hadn't addressed — see the new "Symbol selection" subsection.

## Current Behavior (verified)

- `docs/specs/phase-3/tui-03-page-strategies.md`'s detail overlay (Target Behavior, "Detail overlay
  (Enter)" section) renders exactly: config JSON preview, then "P&L since activation, total trades, win
  rate" as plain text — all three sourced from a single `GetStrategyPerformance(strategyID)` call
  (`apiclient.StrategyPerformanceView`, filtered client-side from `ListStrategyPerformance`'s full slice per
  Phase 3 `tui-01`'s corrected Target Behavior). None of this is time-bucketed — it is a running lifetime
  total, matching `entities.StrategyPerformance{Name, Symbol string; Profit float64; Trades int}`'s current
  shape (verified: `app/entities/strategyperformance.go`, four fields, no timestamp, no bucket granularity).
- No history/time-series persistence for strategy P&L exists anywhere in the codebase today — confirmed by
  grep (`strategyperformance` matches only the single entity file and its repository's raw-SQL
  `GetStrategyPerformanceBySymbol` join, both computing an all-time aggregate, never a per-day/week/month
  row).
- `docs/architecture/refactoring-blueprint.md` §4 Phase 4 confirms this gap explicitly: "New packages/files
  — `app/repository/strategyperformance/` extension for daily/weekly/monthly P&L history persistence
  (entity `app/entities/strategyperformance.go` already exists — extend, don't recreate)" and "Modified
  files — `app/entities/strategyperformance.go` — verify existing shape supports time-bucketed history;
  extend if it's currently a single running total" — the blueprint's own text confirms today's shape is in
  fact "currently a single running total" with no time dimension, matching the grep finding above.
- Phase 3's `tui-08-shared-components.md` already defines `BuildSparkline(title string, closes []float64,
  lineColor ui.Color) *widgets.SparklineGroup` for exactly this rendering need (closes-over-time as an
  8-level block-character sparkline), reused for Page 1's price sparklines and Page 5's equity-curve/
  drawdown sparklines — this spec's P&L history sparkline is a fourth caller of the same existing component,
  not a new rendering primitive.

## Target Behavior

### Final backend contract (`backend-05-strategy-performance-history.md`)

```
GET /strategy/{id}/performance/history?symbol={symbol}&bucket=daily|weekly|monthly&limit={n}
  Returns: 200 [] {
    "period_start": "2026-08-01",   // bucket window start
    "period_end":   "2026-08-01",   // bucket window end (equal to period_start for bucket=daily)
    "profit": 42.10,                // net P&L realized within this bucket
    "trades": 6                     // trade count within this bucket, for symmetry with the existing
                                     // lifetime Trades field, though not rendered by this spec's sparkline
  }
  Errors: 400 (invalid bucket value, symbol missing, limit out of [1,365]), 404 (strategy not found)
  Notes: `symbol` is REQUIRED — the endpoint is scoped to one (strategy, symbol) pair, not the strategy as
         a whole (see "Symbol selection" below for what this means for a multi-symbol strategy). Rows are
         sparse — a bucket with zero closed trades has no row at all, not a zero-profit row (backend-05
         AC#5) — the client must not assume one row per calendar day/week/month in range.
```

### Symbol selection (resolved gap — `backend-05` requires `symbol`, this draft originally didn't)

A `Strategy` monitors a list of symbols (`MonitoredSymbols []string`), but performance history is scoped to
one `(strategy, symbol)` pair per `backend-05`'s design (confirmed: its own AC#6 states a multi-symbol
strategy gets independent snapshot rows per symbol, never a cross-symbol aggregate). This spec resolves the
gap minimally rather than building a full per-symbol switcher: the sparkline section defaults to
`nStrategy.MonitoredSymbols[0]` (the first configured symbol) and, **only if more than one symbol is
configured**, appends a `[y]` keybinding (chosen to avoid colliding with the existing `[g]` granularity
toggle) that cycles through `MonitoredSymbols` the same way `[g]` cycles bucket size — same stale-response
guard (Acceptance Criterion #8 below) applies per-symbol-change too. The section's title line always names
the active symbol (`"P&L History — BTCUSDT (daily, last 30d)"`) so it's never ambiguous which symbol is
shown. A single-symbol strategy (the common case for all three Phase 1 strategies in this codebase's
example configs) never shows the `[y]` hint at all, since there's nothing to cycle.

### `cmd/console/apiclient` additions

```go
// cmd/console/apiclient/client.go additions
func (c *Client) GetStrategyPerformanceHistory(
    ctx context.Context, strategyID uint, symbol, bucket string, limit int,
) ([]PerformanceHistoryPointView, error)
    // GET /strategy/{id}/performance/history?symbol=&bucket=&limit=

type PerformanceHistoryPointView struct {
    PeriodStart time.Time
    PeriodEnd   time.Time
    Profit      float64
    Trades      int
}
```

### Overlay struct changes

```go
// cmd/console/pages/strategies.go — StrategiesPage gains three fields
type StrategiesPage struct {
    // ...unchanged fields from Phase 3 tui-03...
    detailData    *apiclient.StrategyPerformanceView
    historyData   []apiclient.PerformanceHistoryPointView // NEW — nil while loading/before first fetch
    historyBucket string                                   // NEW — "daily" | "weekly" | "monthly", default "daily"
    historySymbol string                                   // NEW — defaults to MonitoredSymbols[0] on overlay open
}
```

`HandleEvent` gains two new keybindings, both active only while `detailVisible == true`:
- `[g]` — cycles `historyBucket`: `"daily" → "weekly" → "monthly" → "daily"`, re-fetching
  `GetStrategyPerformanceHistory` with the new bucket size each time (not client-side re-bucketing of a
  fixed daily fetch — see Judgment Call #1 for why this is a fresh server call per toggle rather than one
  fetch-daily-then-aggregate-client-side approach).
- `[y]` — cycles `historySymbol` through `nStrategy.MonitoredSymbols`, re-fetching with the new symbol; only
  registered/shown in the keybinding hint when `len(MonitoredSymbols) > 1` (see "Symbol selection" above).

A keybinding rather than a mouse-driven toggle, consistent with PRD §4.5's blanket "no mouse required"
requirement restated at the top of every TUI spec in this project.

### Overlay layout addition

The sparkline is inserted between the existing "P&L since activation / total trades / win rate" line and
the overlay's bottom border, growing the overlay's fixed height (Phase 3 `tui-03`'s "~60% of the terminal"
sizing already leaves headroom for one additional `widgets.SparklineGroup` row — no other field is removed
or resized to make room, since PRD names this as an addition, not a replacement of any Phase 3 field).

## ASCII Wireframe

Shows only the modified detail overlay from Phase 3 `tui-03`'s wireframe — the base table and its
keybinding row above are unchanged from that spec and are not repeated here.

```
     ┌ grid_btc — detail ──────────────────────────────────────────────┐
     │ Config:                                                         │
     │ {                                                                │
     │   "grid_spacing_pct": 1.5,                                       │
     │   "grid_levels": 10                                             │
     │ }                                                                │
     │                                                                  │
     │ P&L since activation: +$318.40   Total trades: 47               │
     │ Win rate: 61.7%                                                 │
     │                                                                  │
     │ P&L History — BTCUSDT (daily, last 30d) ── [g] toggle bucket     │
     │ ▂▃▁▄▅▃▆▇▅▄▆████▇▆▅▇█▇▆▅▄▆▇█▆▅▄▃▅                                 │
     │ +$42.10 today                                                   │
     │                                                          [Esc] close │
     └──────────────────────────────────────────────────────────────────┘
```

`grid_btc` above monitors a single symbol, so the `[y]` symbol-cycle hint is correctly absent per the rule
in "Symbol selection." A strategy monitoring multiple symbols (e.g. a hypothetical `scalping_multi`
watching `BTCUSDT` and `ETHUSDT`) would instead show `── [g] toggle bucket  [y] toggle symbol ──` in that
same hint line.

## Acceptance Criteria

1. **Given** the detail overlay opens (`Enter` on a selected row, per Phase 3 `tui-03`'s existing flow),
   **when** `detailVisible` transitions to `true`, **then** `historySymbol` is initialized to
   `nStrategy.MonitoredSymbols[0]`, `historyBucket` to `"daily"`, and `GetStrategyPerformanceHistory` is
   called with those values **in addition to** the existing `GetStrategyPerformance` call — both fire
   concurrently (two goroutines or one `errgroup`-style join), not sequentially, so the overlay's total
   "Loading…" time is `max()` of the two calls rather than their sum.
2. **Given** the history fetch is in flight, **when** the overlay renders before it resolves, **then** the
   sparkline section shows its own independent "Loading…" placeholder — it does **not** block the config
   preview or the lifetime P&L/trades/win-rate fields from rendering as soon as `GetStrategyPerformance`
   (the other, independent call) resolves. This follows the same per-panel-independent-loading principle as
   Phase 3 `tui-01` AC#3 (degraded mode is per-data-source, not all-or-nothing for the whole overlay).
3. **Given** `[g]` is pressed while the overlay is open, **when** `historyBucket` cycles to the next value,
   **then** a new `GetStrategyPerformanceHistory` call is issued with the new bucket size (same
   `historySymbol`), the sparkline section shows "Loading…" again during that call (not a stale sparkline
   from the previous bucket size), and the section's title line updates to reflect the new bucket (e.g.
   "P&L History — BTCUSDT (weekly, last 12w)").
3a. **Given** a strategy monitors 2+ symbols and `[y]` is pressed while the overlay is open, **when**
   `historySymbol` cycles to the next entry in `MonitoredSymbols`, **then** a new
   `GetStrategyPerformanceHistory` call is issued with the new symbol (same `historyBucket`), the sparkline
   shows "Loading…" during that call, and the title line's symbol updates accordingly. **Given** a strategy
   monitors exactly 1 symbol, **when** the overlay renders, **then** the `[y]` hint is not shown in the
   keybinding line and the key has no bound action (nothing to cycle to).
4. **Given** `GetStrategyPerformanceHistory` returns an empty slice (a strategy activated too recently to
   have any completed buckets yet), **when** the sparkline section renders, **then** it shows
   `"No history yet"` rather than calling `BuildSparkline` with an empty slice (Phase 3 `tui-06` AC#2
   already established the precedent that `BuildSparkline`/its callers must handle degenerate small
   inputs — fewer than 2 points — without erroring; a fully empty slice is the more extreme version of that
   same case and gets its own explicit message rather than relying on the sparkline component's flat-line
   fallback, since a flat line at zero could be misread as "P&L is exactly zero" rather than "no data
   exists").
5. **Given** the history fetch fails (`ErrAPI` or `ErrUnreachable`), **when** the overlay renders,
   **then** the sparkline section shows `components.Error(err)` scoped to just that section — the config
   preview and lifetime P&L fields (sourced from the separate `GetStrategyPerformance` call) continue
   rendering normally if that other call succeeded, per the same per-panel-independence principle as
   Acceptance Criterion #2.
6. **Given** the sparkline renders successfully, **when** the line directly below it renders the
   most-recent bucket's value (`"+$42.10 today"` / `"+$42.10 this week"` / `"+$42.10 this month"` per the
   active granularity), **then** the value is colored green if `>= 0` and red if `< 0` — matching this
   project's existing P&L color convention used on Page 3 (Open Positions) and Page 5 (Backtest Results'
   `TotalReturnPct`).
7. **Given** the overlay is closed (`[Esc]`) and later reopened on a **different** strategy row, **when**
   it reopens, **then** both `historyBucket` resets to `"daily"` and `historySymbol` resets to the newly
   selected strategy's `MonitoredSymbols[0]` — neither persists across strategy rows or overlay closes,
   each overlay open is a fresh session, matching Phase 3 `tui-03`'s existing behavior of always
   re-fetching `GetStrategyPerformance` fresh on every `Enter` rather than caching across opens.
8. **Edge case — bucket or symbol cycling while a fetch from the previous toggle is still in flight**:
   **given** `[g]` or `[y]` is pressed twice in rapid succession (in any combination) before the first
   new-value fetch resolves, **when** the first (now-stale) response arrives, **then** it is discarded
   rather than overwriting `historyData` with stale data — the page tracks the `(bucket, symbol)` pair the
   in-flight request was made with and only applies a response if it matches the **current**
   `(historyBucket, historySymbol)` at resolution time (a standard stale-response guard, since `termui`'s
   render loop has no built-in request cancellation/generation-token mechanism to rely on instead).

## Out of Scope

- Editing/annotating the P&L history (e.g. marking a specific day as anomalous) — PRD names this as a
  read-only sparkline, no interaction beyond the granularity toggle.
- A dedicated full-page P&L history view (e.g. a larger chart with axis labels/tooltips) — PRD's exact
  wording is "P&L history sparkline per strategy," matching this spec's compact-widget-in-existing-overlay
  scope, not a new page. If a larger drill-down view is later wanted, it would be a new spec, not an
  extension of this one.
- Exporting P&L history data (CSV, etc.) — not named anywhere in PRD's Phase 4 bullet.
- Client-side re-bucketing (e.g. deriving weekly buckets from a cached daily fetch instead of a fresh server
  call) — see Judgment Call #1.

## Dependencies

- `docs/specs/phase-4/backend-05-strategy-performance-history.md` — **authoritative**, and now reconciled
  (2026-09-05): `symbol=`+`bucket=` params, `period_end` field, and the multi-symbol AC#6 that motivated
  this spec's `[y]` symbol-cycling addition are all reflected above.
- `docs/specs/phase-3/tui-03-page-strategies.md` — the existing detail overlay this spec extends; all of
  that spec's other Acceptance Criteria (loading state, empty-config edge case, `[b]`-while-open no-op,
  etc.) remain in force unchanged.
- `docs/specs/phase-3/tui-08-shared-components.md` — `BuildSparkline`, reused as-is with no changes to its
  own signature; this spec is purely a new call site.
- `docs/specs/phase-3/tui-01-apiclient.md` — extended here with `GetStrategyPerformanceHistory`; base
  `Client`/`ErrUnreachable`/`ErrAPI` patterns reused as-is.
- `docs/architecture/refactoring-blueprint.md` §4 Phase 4 — `app/repository/strategyperformance/` extension
  and `app/entities/strategyperformance.go` time-bucketing this endpoint assumes exists server-side.

## Judgment Calls (flagged for explicit reconciliation)

1. **Granularity toggle re-fetches from the server on every `[g]` press, rather than fetching once (e.g.
   daily, 90 days) and re-bucketing client-side into weekly/monthly views.** Rationale: the existing
   `GetStrategyPerformance` (lifetime totals) precedent in this spec set treats the server as the source of
   truth for aggregation (Phase 3 `tui-06` AC#2's "never re-derive `best` client-side" principle, and this
   phase's sibling `tui-01-page-optimization-results.md` AC#2's identical stance on `BestIndex`) — bucketing
   by ISO week and calendar month has enough edge-case subtlety (month boundaries, week-start convention)
   that duplicating it client-side risks disagreeing with whatever the backend's own bucketing does for
   other consumers of the same data. The cost is one extra HTTP round-trip per toggle press (a rare,
   deliberate user action, not a per-tick poll) — judged acceptable. **Confirmed correct against the final
   `backend-05` spec**: `bucket=daily|weekly|monthly` is a first-class server-side parameter with its own
   snapshot rows per bucket size, not a client-derived view over daily data — this judgment call stands
   as originally reasoned, no rewrite needed.
2. **The most-recent-bucket summary line ("+$42.10 today/this week/this month") is this spec's own addition
   beyond PRD's literal wording**, which only asks for "a P&L history sparkline" with no mention of a
   companion current-period readout. Included because a sparkline alone has no axis labels or numeric
   scale in `termui`'s block-character rendering (per `tui-08`'s own established limitation for every other
   sparkline in this spec set), so without a numeric anchor the operator has no way to read an actual dollar
   value off the chart, only relative shape — flagged as an enhancement beyond the letter of the PRD line,
   not a deviation from its intent.
