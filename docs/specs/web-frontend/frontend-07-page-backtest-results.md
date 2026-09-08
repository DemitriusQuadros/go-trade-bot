# Spec frontend-07 — Backtest Results Page (`web/src/pages/BacktestResults.tsx`)

## Overview

Phase D. Web equivalent of the deleted TUI's Page 5 (`docs/specs/phase-3/tui-06-page-backtest-results.md`):
metrics row, equity curve, drawdown curve, trade log table, Monte Carlo distribution view, and the HTML
report — rendered inline via `<iframe>` per blueprint §7, instead of the TUI's "open in default browser."
This is one of the two pages (with `frontend-08`) that is this pivot's core payoff — the point at which no
chart in the app is a block-character approximation anymore.

## Current Behavior

- No web backtest results page exists. Feature-parity reference: `tui-06-page-backtest-results.md`.
- `GET /backtest/{id}` returns `BacktestRunResponse` (verified, `app/handler/web/backtest/dto.go`):
  `id, strategy_id, symbol, start_date, end_date, is_walk_forward, sharpe, max_drawdown_pct, win_rate_pct,
  profit_factor (number | "Infinity" string), total_trades, total_return_pct, passed, html_report_path,
  trade_log? (present only when includeTradeLog is requested), created_at`. **`profit_factor` is
  special-cased to the literal string `"Infinity"`** when the underlying value is `+Inf` or has been clamped
  to `math.MaxFloat64`/`>= 1e15` (verified in `ToRunResponse`) — this page's PF cell must handle both the
  numeric and string cases, matching `tui-06` AC#1.
- **Gap closed by `docs/specs/web-frontend/backend-04-api-consistency-fixes.md` (Gap 2), landed after this
  spec's first draft.** This spec's original draft found `BacktestRunResponse` didn't include an
  `equity_curve` field at all and proposed a client-side reconstruction workaround from `trade_log` — flagged
  at the time as "the single most important reconciliation item in this entire spec set." backend-04 resolves
  it the correct way (option (a) from that Judgment Call, not (b)): `entities.BacktestRun` gains a new
  `MetricsJSON datatypes.JSON` column (mirroring `OptimizationRun.BestMetricsJSON`'s existing precedent for
  the identical GORM-embedding constraint) persisting the full `metrics_provider.BacktestMetrics` computed
  during the run — previously discarded after producing the four scalar summaries, now durably saved.
  `BacktestRunResponse` gains an `equity_curve: EquityPoint[]` field, populated from `MetricsJSON` and
  **always included** on every `GET /backtest/{id}` and `GET /backtest?strategy_id=` call — unconditionally,
  not gated behind the existing `includeTradeLog` flag, since the series is small (one point per trade). For
  a walk-forward run, `equity_curve` reflects the aggregate out-of-sample curve
  (`wfResult.AggregateOOS.EquityCurve`), not any single window's curve. Pre-migration rows (created before
  `MetricsJSON` existed) return `equity_curve: []` rather than erroring. **This spec's `EquityCurveChart`/
  `DrawdownChart` (frontend-11) now consume `equity_curve` directly off the response — the client-side
  trade-log-reconstruction workaround described in this spec's earlier draft is removed entirely, not merely
  superseded.**
- `trade_log` (when requested) is `[]TradeLogEntry` (verified, `internal/metrics_provider/interface.go`):
  `symbol, entry_time, entry_price, exit_time, exit_price, quantity, profit, exit_reason`. For a
  walk-forward run, `TradeLogJSON` embeds a richer wrapped shape (`{trades, windows}` per Phase 2's
  `WalkForwardTradeLogPayload`, referenced by the deleted TUI's `tui-06` Current Behavior) — this page's
  trade-log table must handle both shapes.
- `POST /backtest/{id}/montecarlo` (body `{iterations?}`, default 1000, cap 10000) and
  `GET /backtest/{id}/montecarlo` (cached result, `404` if never computed) — verified against
  `docs/specs/phase-4/backend-03-monte-carlo-simulation.md`, which **is** a finalized/landed backend spec
  (unlike the auth/embed/SSE specs this task set flags elsewhere as pending). Returns `MonteCarloResult`:
  `iterations, original_metrics: BacktestMetrics, sharpe_distribution: DistributionStats,
  max_drawdown_distribution: DistributionStats, total_return_distribution: DistributionStats` where
  `DistributionStats = {mean, median, min, max, p5, p95}`.
- **Gap closed by `backend-04` (Gap 3).** `html_report_path` is a local filesystem path
  (e.g. `reports/Grid_Bot_BTCUSDT_42.html`), written to disk by `cmd/api`'s own process — this spec's earlier
  draft found no route served it over HTTP (the deleted TUI's `tui-06` opened it via the OS's
  default-browser opener, `os/exec`, precisely because no such route existed) and flagged this as a required
  backend addition (Judgment Call #2). backend-04 adds `GET /backtest/{id:[0-9]+}/report`, which reads
  `HTMLReportPath` off the DB row and streams the file's raw bytes with `Content-Type: text/html;
  charset=utf-8` (no `Content-Disposition: attachment`, so it renders inline rather than downloading).
  `404` (with a JSON error body) covers all three "no report available" cases uniformly — never generated,
  pruned by the retention policy, or missing from disk despite a recorded path — the client cannot and does
  not need to distinguish them. **Auth for this route uses the same `?token=` query-param fallback
  `backend-03` established for the SSE endpoint**, since an `<iframe src="...">` is a plain browser
  navigation with no way to attach an `Authorization` header — see Target Behavior's `ReportPanel` section
  for the exact `src` construction this requires.

## Target Behavior

### Layout — component composition

```
BacktestResults.tsx
├── PageHeader — "Backtest Results — <Strategy> / <Symbol>", PASS/FAIL badge (colored, from
│    `passed`), IsWalkForward badge if applicable
├── MetricsRow — Sharpe | MaxDD% | WinRate% | ProfitFactor (∞ if "Infinity") | Trades | TotalReturn%
│    (six stat tiles, not a terminal text line — each with a label + value + optional trend color)
├── section.charts (two-column on wide viewports, stacked on narrow)
│    ├── EquityCurveChart (frontend-11) — fed directly from `equity_curve` on the GET /backtest/{id}
│    │    response (backend-04 Gap 2) — no client-side reconstruction from trade_log
│    └── DrawdownChart (frontend-11) — computed client-side as (peak_so_far - value) / peak_so_far
│         * 100 at each point of that SAME server-provided equity series — same arithmetic-not-a-new-
│         metric approach the deleted TUI's tui-06 AC#3 used, now fed from the backend's own
│         authoritative EquityCurve rather than an approximation
├── Toolbar — "Run Monte Carlo" button, "Run Walk-Forward" button (if !is_walk_forward), "Open
│    Report" button/link
├── [conditional] MonteCarloPanel — shown after "Run Monte Carlo" completes (or on mount if
│    GET /backtest/{id}/montecarlo already has a cached result) — MonteCarloDistribution
│    (frontend-11) rendered three times (Sharpe / MaxDD / TotalReturn), each as a small
│    box-plot-style summary (mean/median/min/max/p5/p95)
├── TradeLogTable — sortable, paginated (client-side pagination is sufficient at typical trade
│    counts — hundreds, not tens of thousands); walk-forward runs get an additional leftmost
│    "Window" column (matching tui-06 AC#7); CSV export button (blueprint §7: "the TUI spec's own
│    ... this pivot resolves" framing doesn't literally ask for CSV here, but frontend-09's Log
│    page sets the CSV-export precedent this page's table reuses the same utility for)
└── [conditional] ReportPanel — <iframe> embedding the HTML report, src built per Target Behavior's
     "Report iframe src" section below (GET /backtest/{id}/report?token=..., backend-04 Gap 3)
```

### Equity/drawdown chart data (no reconstruction — direct consumption)

```tsx
// web/src/pages/BacktestResults.tsx (sketch)
// run: BacktestRun (frontend-01's api/types.ts), already includes equity_curve
// unconditionally per backend-04 Gap 2 — no derivation step needed here.
const drawdownPoints = useMemo(() => {
  let peak = -Infinity;
  return run.equity_curve.map((p) => {
    peak = Math.max(peak, p.value);
    return { time: p.time, drawdownPct: peak > 0 ? ((peak - p.value) / peak) * 100 : 0 };
  });
}, [run.equity_curve]);
```

### Report iframe `src`

```tsx
// web/src/pages/BacktestResults.tsx (sketch)
import { getToken } from '@/api/client';

function reportSrc(backtestId: number): string {
  // An <iframe src="..."> is a plain browser navigation — it cannot carry an
  // Authorization header, the identical constraint frontend-12's useSSE hook
  // hits with EventSource. backend-04 extends backend-03's ?token= fallback
  // (already built into RequireAuth) to this route for exactly that reason —
  // this is NOT a new auth mechanism, it's the same one reused a second time.
  return `/backtest/${backtestId}/report?token=${encodeURIComponent(getToken() ?? '')}`;
}

// <ReportPanel>: <iframe src={reportSrc(run.id)} title="Backtest HTML report" />
```

`ReportPanel` only renders this `<iframe>` when `html_report_path !== ""` (Acceptance Criterion #10, below,
unchanged) — a `404` from the report route itself (e.g. the file was pruned between the list rendering and
the click, a real race under the existing retention policy) is handled by the `<iframe>`'s own `onError`-
adjacent fallback: since `<iframe>` has no clean cross-origin-safe way to inspect a same-origin child's HTTP
status from the parent in all cases, this spec's fallback is a short client-side existence check
(`api.get` against the same URL is not viable — it isn't JSON — so a `HEAD`-style probe or simply trusting
`html_report_path !== ""` as computed at the moment the page loaded is accepted as sufficient; a genuinely
stale link is a rare race, not a common-path failure to over-engineer around).

### Trade log shape handling

```ts
// web/src/pages/BacktestResults.tsx (sketch)
interface FlatTradeLog { trades: TradeLogEntry[] }
interface WalkForwardTradeLog { trades: (TradeLogEntry & { window: number })[]; windows: unknown[] }

function normalizeTradeLog(raw: unknown, isWalkForward: boolean): (TradeLogEntry & { window?: number })[] {
  if (isWalkForward && raw && typeof raw === 'object' && 'trades' in (raw as object)) {
    return (raw as WalkForwardTradeLog).trades;
  }
  return (raw as TradeLogEntry[]) ?? [];
}
```

### Monte Carlo trigger

```tsx
async function runMonteCarlo(backtestId: number, iterations = 1000) {
  setMcLoading(true);
  try {
    const result = await api.post<MonteCarloResult>(`/backtest/${backtestId}/montecarlo`, { iterations });
    setMcResult(result);
  } catch (err) {
    if (err instanceof ApiError && err.status === 422) {
      setMcError('This run has fewer than 2 trades — Monte Carlo needs at least 2 to reorder.');
    } else {
      setMcError(humanizeError(err));
    }
  } finally {
    setMcLoading(false);
  }
}
```

## Acceptance Criteria

1. **Given** `profit_factor` is the literal string `"Infinity"`, **when** `MetricsRow` renders the PF tile,
   **then** it displays `∞`, not a parse error or `"0.00"` — matching `tui-06` AC#1.
2. **Given** `equity_curve` has fewer than 2 points (a pre-migration run whose `MetricsJSON` was never
   populated, or a genuinely tiny run), **when** `EquityCurveChart` renders, **then** it shows a flat/minimal
   line (or an explicit "not enough data to chart" note) rather than erroring on a degenerate input —
   matching `tui-06` AC#2's principle, now applied to the backend's own directly-served series.
3. **Given** `equity_curve`, **when** `DrawdownChart` renders, **then** its values are computed client-side
   as `(peak_so_far - value) / peak_so_far * 100` at each point — matching `tui-06` AC#3's arithmetic
   exactly, against the server-provided series (backend-04 Gap 2), not a client-side reconstruction.
3a. **Given** a fresh `GET /backtest/{id}` for a completed run, **when** the response is inspected,
   **then** `equity_curve` is present and non-empty, sourced from the persisted `MetricsJSON` column — the
   same series `internal/report/html_report.go`'s HTML report renders as inline SVG for the same run, per
   backend-04 Gap 2 AC#2 (both read from the identical `metrics.EquityCurve` computed once per run).
3b. **Given** a walk-forward run (`is_walk_forward: true`), **when** `equity_curve` is read, **then** it
   reflects the aggregate out-of-sample curve across all windows (`wfResult.AggregateOOS.EquityCurve`), not
   any single window's individual curve — matching backend-04 Gap 2 AC#5.
4. **Given** `passed === false`, **when** `PageHeader`'s badge renders, **then** it's visually distinct
   (e.g. red "FAIL" badge) — matching `tui-06` AC#4 and `frontend-06`'s `RecentRunsList` treatment for visual
   consistency across both pages.
5. **Given** the operator clicks "Run Monte Carlo," **when** `POST /backtest/{id}/montecarlo` resolves,
   **then** `MonteCarloPanel` renders three `MonteCarloDistribution` views (Sharpe/MaxDD/TotalReturn), each
   showing `mean/median/min/max/p5/p95` — direct consumption of `backend-03`'s finalized `MonteCarloResult`
   shape.
6. **Given** `POST /backtest/{id}/montecarlo` returns `422` (fewer than 2 trades), **when** the error is
   handled, **then** the panel shows the specific "fewer than 2 trades" message rather than a generic error —
   distinguishing this failure class the same way `frontend-06` distinguishes `422` from `400`.
7. **Given** the page mounts and `GET /backtest/{id}/montecarlo` already has a cached result (a prior
   `POST` was made for this run), **when** the initial fetch resolves with `200`, **then**
   `MonteCarloPanel` renders immediately without requiring the operator to click "Run Monte Carlo" again —
   this is new behavior beyond the TUI (which had no Monte Carlo UI at all, per `backend-03`'s own explicit
   "no dedicated Phase 4 TUI page" note) and is this page's own reasonable extension of the finalized
   backend contract.
8. **Given** `!is_walk_forward` and the operator clicks "Run Walk-Forward," **when** the action fires,
   **then** it calls `POST /backtest/walkforward` using this run's `strategy_id`/`symbol`/date-range as base
   parameters (matching `tui-06`'s "current result's parameters" approach) and, on completion, **replaces**
   this page's displayed result in place with the new walk-forward run (no navigation away) — matching
   `tui-06` AC#6.
9. **Edge case — walk-forward trade log**: **given** `is_walk_forward === true`, **when**
   `TradeLogTable` renders, **then** it shows the aggregate concatenated trade log with an additional
   leftmost "Window" column — matching `tui-06` AC#7.
10. **Given** `html_report_path === ""` (report generation failed without failing the whole run, per Phase 2
    Spec 10 AC#4), **when** the Toolbar renders, **then** the "Open Report" button/link is disabled with a
    "No report available for this run" tooltip/inline note rather than attempting to construct an
    `<iframe src>` against an empty path — matching `tui-06` AC#5's principle, translated to a
    disabled-control pattern instead of a keybinding no-op message.
10a. **Given** `html_report_path !== ""`, **when** `ReportPanel` renders, **then** the `<iframe>`'s `src` is
    `/backtest/{id}/report?token=<stored token>` (Target Behavior's `reportSrc`) and the report's content
    renders **inline** in the page, not as a downloaded file — verifying `backend-04`'s `GetReport` handler's
    lack of a `Content-Disposition: attachment` header actually produces inline rendering, per that spec's
    own AC#6.
10b. **Edge case — the report route itself returns `404`** (e.g. the run's report was pruned by the
    retention policy after this page loaded but before the `<iframe>` request fires): **when** this occurs,
    **then** the `<iframe>` shows the backend's own `404` JSON error body rendered as plain text inside the
    frame — an acceptable, if inelegant, degraded state for this rare race (see Target Behavior's note on why
    a more graceful client-side pre-check isn't built for this edge case in v1).
11. **Accessibility**: `MetricsRow`'s six stat tiles are each labeled with visible text (not icon-only), the
    trade log table has proper `<th scope="col">` headers for screen readers, and the equity/drawdown charts
    each carry a short text summary (e.g. "Equity grew from $1,000 to $1,376 over 84 trades, peak drawdown
    14.2%") alongside the SVG chart for non-visual access.

## Out of Scope

- Exporting the trade log in any format other than CSV (already covered as an extension of frontend-09's
  export utility) — no PDF/Excel export.
- In-app equity-curve zooming/panning beyond Recharts' default hover-tooltip interactivity (frontend-11
  defines exactly what interactivity ships) — no custom brush/zoom control in v1.
- A "save/pin as reference baseline" feature — the deleted TUI's `tui-06` Judgment Call left `[s]` as a
  no-op confirmation since every run already persists synchronously; this spec does not add a `Pinned`
  concept either, for the same reason (`POST /backtest` already durably saves every run — there's no
  "unsaved" state to rescue).

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `api.get/post`, `getToken`, `BacktestRun` type (now includes
  `equity_curve` directly, per backend-04 Gap 2 — no longer flagged as a gap).
- `frontend-06-page-backtest-launcher.md` — the primary entry point via navigation on run completion.
- `frontend-11-charts.md` — `EquityCurveChart`, `DrawdownChart`, `MonteCarloDistribution` (reconciled: these
  now consume `equity_curve` directly, no reconstruction step).
- `docs/specs/phase-4/backend-03-monte-carlo-simulation.md` — **finalized**, `MonteCarloResult`/
  `DistributionStats` shapes consumed directly.
- `docs/specs/phase-3/tui-06-page-backtest-results.md` — feature/data-parity checklist.
- `docs/specs/web-frontend/backend-03-sse-realtime-endpoint.md` — source of the `?token=` query-param auth
  fallback this spec's report `<iframe>` reuses (`backend-04` extends it to the report route rather than
  inventing a new mechanism).
- `docs/specs/web-frontend/backend-04-api-consistency-fixes.md` — **reconciled** (landed after this spec's
  first draft, closing both gaps this spec's Judgment Calls originally flagged): Gap 2 (`equity_curve` on
  `GET /backtest/{id}`) and Gap 3 (`GET /backtest/{id}/report`). Both Judgment Calls below are resolved, not
  open.

## Judgment Calls (flagged for explicit reconciliation)

1. **RESOLVED by `backend-04` Gap 2.** This spec's original draft found `GET /backtest/{id}` didn't expose
   an equity curve at all and proposed either a new backend field (option (a)) or accepting a client-side
   trade-log reconstruction as "close enough" (option (b)), flagging it as the single most important
   reconciliation item in this entire spec set. Option (a) was the one shipped: a new `MetricsJSON` column
   (mirroring `OptimizationRun.BestMetricsJSON`'s already-established pattern for the same GORM-embedding
   constraint) persists the full computed `BacktestMetrics`, and `BacktestRunResponse.equity_curve` is
   populated from it — the exact `metrics.EquityCurve` the HTML report itself renders, not an approximation.
   The reconstruction workaround this spec's earlier draft described is removed from Target Behavior above.
2. **RESOLVED by `backend-04` Gap 3.** This spec's original draft flagged that no route served
   `html_report_path` over HTTP, blocking the `<iframe>` requirement entirely, and named this a required
   (not optional) backend addition. `GET /backtest/{id:[0-9]+}/report` now exists, reusing `backend-03`'s
   `?token=` query-param auth fallback (the same `EventSource`-can't-set-headers constraint applies
   identically to an `<iframe>`'s plain-navigation `src`). This spec's `reportSrc()` helper (Target Behavior)
   is the concrete client-side implementation of that fix.
