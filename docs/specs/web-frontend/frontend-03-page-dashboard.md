# Spec frontend-03 — Dashboard Page (`web/src/pages/Dashboard.tsx`)

## Overview

Phase B. Web equivalent of the deleted TUI's Page 1 (`docs/specs/phase-3/tui-02-page-dashboard.md`): account
summary, monitored-pairs overview with real price charts, active-strategies summary. Per the blueprint §7
mapping, this is where the web version's payoff is most immediately visible — real interactive line charts
with hover tooltips replace block-character sparklines, and clicking a pair jumps straight into its detail
rather than the terminal's fixed-panel layout.

Read-only in this phase (Phase B) — no write actions live on this page in v1.

## Current Behavior

- No web dashboard exists; this is a from-scratch page. The reference for feature parity is the deleted
  TUI's Page 1 (`tui-02-page-dashboard.md`), read for **data/feature parity only**, not layout — a browser
  canvas affords a fundamentally different layout than a fixed-grid terminal UI (per this task's brief).
- The TUI's "monitored pairs" set was defined as the deduplicated union of every strategy's
  `MonitoredSymbols` (`tui-02`'s Judgment Call) — no dedicated watchlist entity/endpoint exists anywhere in
  `app/entities` today (still true, verified by the same grep this spec re-ran against the current
  `app/entities/` directory). This spec carries that same definition forward rather than inventing new
  backend surface.
- The TUI's "connection status" dot was redefined as console→cmd/api reachability, not literal Binance
  WebSocket state (`tui-01` Judgment Call #1) — same redefinition applies here: this page's own polling
  loop's success/failure is what "connected" means, with no path today to surface `cmd/worker`'s live-feed
  WS health specifically (that connection lives entirely inside `cmd/worker`, per `internal/feed.LiveFeed`,
  never exposed to `cmd/api`).

## Target Behavior

### Layout — component composition (not a terminal wireframe)

```
Dashboard.tsx
├── PageHeader             — connection status badge (frontend-10), page title
├── section.account-summary
│   └── AccountSummaryCard  — balance, available margin, open-positions count
│                             (daily P&L: see Judgment Call, same "N/A" honesty
│                             stance the deleted TUI's tui-08 AC#3 took)
├── section.monitored-pairs — CSS grid, responsive (auto-fill, minmax(320px, 1fr))
│   └── PairCard[]           one per deduplicated MonitoredSymbols entry:
│       ├── symbol + last price + 24h change % (colored per sign)
│       ├── PnlHistoryChart-style compact line (frontend-11's shared line-chart
│       │    primitive, small/sparkline-sized variant — NOT a Recharts
│       │    Sparkline component, since Recharts has none; a compact
│       │    <LineChart> with axes/grid hidden achieves the same visual
│       │    result while keeping exactly one charting primitive in the app)
│       └── onClick → navigate(`/strategies?symbol=${symbol}`) (drill-down,
│            per blueprint §7's "clickable monitored pairs jump straight into
│            that symbol's chart/strategy detail")
└── section.active-strategies
    └── StrategyTable (frontend-10, compact variant) — name, ModeBadge, status,
         last execution — read-only here, no [e]/[d]/[r] actions (Phase C)
```

### Data fetching

```tsx
// web/src/pages/Dashboard.tsx (sketch)
import { usePolling } from '@/hooks/usePolling';
import { api } from '@/api/client';
import type { Account, Strategy, TickerPrice, Candle } from '@/api/types';

export function Dashboard() {
  const { data: account, error: accountError, connected } = usePolling(
    () => api.get<Account>('/account'),
    { intervalMs: 5000 },
  );
  const { data: strategies } = usePolling(
    () => api.get<Strategy[]>('/strategy'),
    { intervalMs: 5000 },
  );

  const symbols = useMemo(
    () => Array.from(new Set((strategies ?? []).flatMap((s) => s.monitored_symbols))),
    [strategies],
  );

  // One usePolling instance per symbol (frontend-10's hook is per-resource,
  // not a batch primitive) — see Acceptance Criterion #4 for the
  // one-bad-symbol-doesn't-blank-the-grid requirement this implies.
  // ...
}
```

- `GET /account` and `GET /strategy` — 5s poll interval (matches the deleted TUI's Page 1 cadence closely
  enough; not literally 1s like the TUI's pairs-grid refresh, since a browser tab's visibility/backgrounding
  makes an aggressive fixed interval wasteful — see Judgment Call on interval choice).
- Per-symbol `GET /broker/prices?symbol=` (latest price) and `GET /broker/klines?symbol=&interval=1m&limit=60`
  (chart data) — each `PairCard` manages its own `usePolling` instance, independently erroring per symbol
  (mirrors the deleted TUI's `tui-02` AC#6: "one bad symbol/API hiccup does not blank the whole grid").
- 24h change % is computed client-side from the kline series' oldest vs. newest close (no dedicated
  24h-change endpoint exists — same gap the TUI's `tui-02` Target Behavior already documented and worked
  around identically).
- EMA20-based color coding (TUI's green-above/red-below convention) is **not carried forward as-is** — see
  Judgment Call: the web version has room for an actual price line chart with a visible EMA20 overlay line
  instead of collapsing the same information into a single sparkline color, which is a strictly richer
  presentation of the same underlying computation (`ComputeEMA20`, ported client-side unchanged from the
  TUI's formula).

## Acceptance Criteria

1. **Given** `cmd/api` is reachable, **when** the Dashboard mounts, **then** the connection-status badge
   shows "Connected," the account summary shows real balance/available-margin figures, and the monitored
   pairs grid renders one `PairCard` per deduplicated symbol across all strategies.
2. **Given** `cmd/api` becomes unreachable mid-session, **when** the next poll tick fails, **then** the
   connection-status badge switches to "Disconnected," and every card/table on the page continues showing
   its last successfully fetched data (no blanking) — matching the deleted TUI's `tui-01` AC#3 degraded-mode
   principle, now implemented via `usePolling`'s `error`/`data` split (frontend-10) rather than a bespoke
   per-page mechanism.
3. **Given** at least one strategy monitors `BTCUSDT`, **when** the pairs grid renders, **then** `BTCUSDT`
   appears in exactly one `PairCard` even if multiple strategies monitor it (deduplication, same rule as the
   deleted TUI's `tui-02` AC#3) — the EMA20-vs-close color/line-overlay reflects the latest 60-candle 1m
   series.
4. **Given** one symbol's `GET /broker/prices` or `GET /broker/klines` call fails while others succeed,
   **when** the grid renders, **then** only that symbol's card shows an inline error state (its own
   `usePolling` error, isolated per-card) — the rest of the grid renders normally.
5. **Given** the operator clicks a `PairCard`, **when** the click handler fires, **then** the app navigates
   to `/strategies?symbol=<SYMBOL>` (Strategies page, frontend-04, filtered/scrolled to strategies monitoring
   that symbol) — a materially deeper drill-down than the TUI could offer (no navigation model existed for a
   terminal grid cell).
6. **Edge case — no strategies exist yet**: **given** `GET /strategy` returns `[]`, **when** the page
   renders, **then** the monitored-pairs section shows an empty state ("No monitored pairs — create a
   strategy to start tracking symbols.", with a link/button to the Strategies page's create form once
   frontend-04's Phase C form exists) and the active-strategies section shows "No strategies configured" —
   matching the deleted TUI's `tui-02` AC#5 parity requirement, phrased for a web empty-state pattern
   (message + actionable link) rather than a static terminal string.
7. **Edge case — `Account` response casing** (per `frontend-01`'s flagged uncertainty): **given** the actual
   `GET /account` response's field casing hasn't been verified against a live call, **when** implementation
   begins, **then** this page's `AccountSummaryCard` must be checked against the real payload before
   shipping — the `Account` interface (`frontend-01`) is this spec's best-effort guess, not a confirmed
   contract.
8. **Accessibility**: every `PairCard`'s click target is a real `<button>`/`<a>` (not a `<div onClick>`),
   reachable and activatable via keyboard; the connection-status badge conveys state via text ("Connected"/
   "Disconnected"), not color alone; each chart has an accessible text alternative (a visually-hidden summary
   line, e.g. "BTCUSDT: $61,204.10, up 1.8% over the last hour") since Recharts' SVG output has no default
   screen-reader-friendly data table equivalent.

## Out of Scope

- A dedicated, persisted, operator-configurable watchlist independent of strategies' `MonitoredSymbols` —
  same explicit non-goal the deleted TUI's `tui-02` flagged; would require new backend persistence not
  scoped here.
- Real-time push (SSE) — Phase E (`frontend-12`). This page ships on `usePolling` first per ADR-010's
  sequencing.
- Daily P&L on the account summary — see Judgment Call; rendered honestly as unavailable rather than
  fabricated, matching the deleted TUI's `tui-08` AC#3 precedent.

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `api.get`, `Account`/`Strategy`/`TickerPrice`/`Candle` types.
- `frontend-10-shared-components.md` — `usePolling`, `StrategyTable` (compact variant), connection-status
  badge pattern.
- `frontend-11-charts.md` — the compact line-chart primitive used inside `PairCard` (a smaller-scale
  configuration of the same `EquityCurveChart`/`PnlHistoryChart`-family Recharts wrapper, not a new chart
  type).
- `docs/specs/phase-3/tui-02-page-dashboard.md`, `tui-08-shared-components.md` — feature/data-parity
  checklist (pairs-grid definition, EMA20 formula, connection-status redefinition), consulted for parity
  only, not layout.

## Judgment Calls (flagged for explicit reconciliation)

1. **EMA20 color-coding is upgraded to a visible overlay line on a real chart, not preserved as a
   single-color sparkline.** The TUI's binary green/red framing was a real information-density constraint of
   block-character rendering; a browser chart can show the actual EMA20 line alongside price with a legend,
   which is strictly more informative. This is a deliberate design improvement per this task's brief ("real
   charts... instead of TUI's block sparklines"), not a scope change to what's computed — `ComputeEMA20`'s
   formula carries over unchanged.
2. **Poll interval is 5s for account/strategy list, not the TUI's 1s.** The TUI's 1s cadence was chosen for a
   text terminal with negligible per-tick rendering cost; a browser tab re-rendering charts every second
   across N pair cards is comparatively expensive and, more importantly, imperceptible-quality-improvement
   over 5s for balance/strategy-list data that doesn't change that fast in practice. Per-symbol price ticks
   inside `PairCard` are proposed at a faster 2s interval as a middle ground — flagging both numbers as this
   spec's own proposal, not dictated by the blueprint, and easy to tune once Phase E's SSE work (frontend-12)
   makes the whole question moot for this page.
3. **Daily P&L remains unavailable ("N/A") on the account summary**, carrying forward the deleted TUI's
   `tui-08` AC#3 judgment call verbatim: `entities.Account` has no daily-P&L field, and computing it
   client-side from `GET /signal`'s full history on every poll tick is significant unnecessary payload for a
   number this spec chooses not to fabricate. Revisit if backend-01 (or a future spec) adds a dedicated
   daily-P&L field/endpoint.
