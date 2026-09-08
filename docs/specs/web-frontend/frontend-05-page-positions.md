# Spec frontend-05 — Positions Page (`web/src/pages/Positions.tsx`)

## Overview

Web equivalent of the deleted TUI's Page 3 (`docs/specs/phase-3/tui-04-page-positions.md`). **[Phase B]**:
read-only cards/table with live-polled P&L. **[Phase C]**: manual close action with a confirmation dialog.
Both phases covered in this one spec, labeled per Acceptance Criterion.

## Current Behavior

- No web positions page exists. Feature-parity reference: `tui-04-page-positions.md`.
- `GET /signal?status=open` returns open `SignalResponseDTO[]` (reconciled against `frontend-01`'s
  now-corrected `api/types.ts`, itself reconciled against `backend-04-api-consistency-fixes.md`), each with
  a nested `orders: OrderResponseDTO[]`, snake_case throughout. `OrderResponseDTO.stop_loss_price` (a
  `float32` on the underlying Go entity, exposed as `stop_loss_price` on the wire) is a real,
  already-populated field — this **resolves** the deleted TUI's `tui-04` biggest open gap (Judgment Call:
  "`SignalView` must carry a resolved `StopLossPrice`... requires backend embedding it"). The backend already
  does this; the web client does not need to work around a raw broker-order-ID-only shape the way the TUI
  spec anticipated it might have to.
- **`Signal` has no nested `strategy`/`Strategy` object** — `SignalResponseDTO` exposes `strategy_id` only
  (matching `BacktestRunResponse`'s existing strategy-id-only precedent, per `backend-04` Gap 1). This page's
  `PositionCard` header (Target Behavior, below) needs the strategy's display name, which is **not** on the
  signal payload at all — it must be resolved by cross-referencing `signal.strategy_id` against the
  separately-fetched `Strategy[]` list (the same `GET /strategy` list `frontend-03`/`frontend-04` already
  poll), not by reading a nested `signal.strategy.name`/`signal.Strategy.Name` path that doesn't exist on the
  wire.
- `POST /signal/close/{id}` returns `202 Accepted` with no body (`app/handler/web/signal/handler.go:60`) —
  confirmed synchronous-enough for a direct request/response click-to-close flow, no polling-for-completion
  needed.
- The TUI's live P&L polling bug (coloring negative P&L blue instead of red, `openorders.go:110-113`) is
  **not applicable** here — this is a from-scratch implementation with no legacy color logic to inherit.

## Target Behavior

### Layout — component composition

```
Positions.tsx
├── PageHeader — "Open Positions", connection-status badge
├── section.positions-grid — CSS grid, responsive (auto-fill, minmax(340px, 1fr)),
│    one PositionCard per open Signal
│    └── PositionCard (frontend-10)
│         ├── header: Signal #<id> — <strategyName> — <symbol> (strategyName resolved by the
│         │    PAGE, not the card: Positions.tsx looks up signal.strategy_id in its own
│         │    separately-fetched Strategy[] list — GET /strategy, same list frontend-03/04
│         │    already poll — and passes the resolved name down as a plain prop; SignalResponseDTO
│         │    carries no nested strategy object to read a name off of directly)
│         ├── Entry price / Quantity / Invested amount (from orders[0], the entry order)
│         ├── Current price (from a per-symbol usePolling(ListTickerPrices), deduplicated
│         │    across cards sharing a symbol — see Target Behavior below)
│         ├── Unrealized P&L in $ and %, colored green (>=0) / red (<0), font-weight/size
│         │    scaled by |P&L%| magnitude across 3 discrete tiers (same 3-tier discretization
│         │    the deleted TUI's tui-04 proposed, ported directly — this is presentation logic
│         │    independent of terminal-color-palette constraints, so it carries over verbatim
│         │    rather than being "upgraded"; a browser COULD do continuous color interpolation
│         │    instead, but the 3-tier scheme reads clearly and needs no new design work)
│         ├── Duration open ("2h 14m", computed from signal.created_at)
│         ├── SL / TP row — order.stop_loss_price formatted as currency if > 0, else "(none set)"
│         │    (take-profit price: no equivalent field exists on entities.Order today — see
│         │    Judgment Call, same "(none set)" honest-default treatment)
│         └── [Phase C] "Close Position" button → opens ConfirmDialog
└── [Phase C] ConfirmDialog (frontend-10)
     "Close position #<id>? This will submit a MARKET sell for <qty> <symbol> at current price."
     [Cancel] [Confirm]
```

### Deduplicated per-symbol price polling

```tsx
// web/src/pages/Positions.tsx (sketch)
const { data: signals } = usePolling(() => api.get<Signal[]>('/signal?status=open'), { intervalMs: 3000 });
const { data: strategies } = usePolling(() => api.get<Strategy[]>('/strategy'), { intervalMs: 5000 });

const distinctSymbols = useMemo(
  () => Array.from(new Set((signals ?? []).map((s) => s.symbol))),
  [signals],
);

// strategy_id -> display name lookup, since SignalResponseDTO carries no
// nested strategy object (backend-04 Gap 1) — resolved here, once, from the
// separately-fetched Strategy[] list, and passed to each PositionCard as a
// plain `strategyName` prop rather than each card reading a nested path.
const strategyNameById = useMemo(
  () => new Map((strategies ?? []).map((s) => [s.id, s.name])),
  [strategies],
);

// One usePolling per distinct symbol, shared across every PositionCard for
// that symbol via a small symbol -> price lookup map passed down as props —
// avoids N redundant GET /broker/prices calls for N cards on the same
// symbol, mirroring the deleted TUI's tui-04 AC#4 deduplication requirement.
const pricesBySymbol = useSymbolPrices(distinctSymbols); // small local hook, see frontend-10
```

### Close-position flow [Phase C]

```tsx
async function handleClose(signalId: number) {
  try {
    await api.post(`/signal/close/${signalId}`);
    setSignals((prev) => prev.filter((s) => s.id !== signalId)); // optimistic removal
  } catch (err) {
    // Do NOT remove the card — next poll's GET /signal?status=open is authoritative.
    showInlineCardError(signalId, humanizeError(err));
  }
}
```

## Acceptance Criteria

### [Phase B]

1. **Given** `GET /signal?status=open` returns N signals, **when** the page renders, **then** exactly N
   `PositionCard`s are shown, each with entry price, quantity, invested amount, current price, unrealized
   P&L in both $ and %, and duration open — matching the deleted TUI's `tui-04` AC#1 field set.
2. **Given** a position's unrealized P&L is positive, **when** the card renders, **then** the P&L text is
   green; negative renders red — explicitly correct from the start (no legacy blue-color bug to fix, unlike
   the TUI's own history).
3. **Given** `order.stop_loss_price > 0`, **when** the card renders, **then** it shows the actual dollar stop
   price inline directly from the API response — no client-side broker-order-ID resolution needed, since
   the gap the deleted TUI's `tui-04` flagged is already closed server-side (see Current Behavior).
4. **Given** two open positions share the same symbol (e.g. two `BTCUSDT` signals from different
   strategies), **when** the 3s price-poll tick fires, **then** exactly one `GET /broker/prices?symbol=BTCUSDT`
   call is made, and both cards read from the same resolved price — deduplication requirement carried
   forward from `tui-04` AC#4.
5. **Edge case — zero open positions**: **given** `GET /signal?status=open` returns `[]`, **when** the page
   renders, **then** it shows an empty state ("No open positions.") rather than a blank grid — a case the
   deleted TUI's spec didn't explicitly enumerate but is a necessary web-page completeness requirement.
6. **Edge case — a single symbol's price poll fails while others succeed**: **when** the poll tick resolves,
   **then** only cards for that symbol show a "price unavailable" inline state (last-known price kept
   visible, dimmed, with a small warning icon) — the rest of the grid continues updating normally, matching
   the general per-resource degraded-mode principle used throughout this spec set.

### [Phase C]

7. **Given** the operator clicks "Close Position" on a card, **when** `ConfirmDialog` opens, **then** no
   `POST /signal/close/{id}` call is made until "Confirm" is explicitly clicked — "Cancel" or `Escape`
   dismisses with zero side effects, matching `tui-04` AC#5's "no call until explicit confirm" rule.
8. **Given** the operator confirms and `POST /signal/close/{id}` returns `202`, **when** the response
   resolves, **then** the card is removed from the grid immediately (optimistic removal), not waiting for
   the next `GET /signal?status=open` poll — matching `tui-04` AC#6.
9. **Given** `POST /signal/close/{id}` fails** (e.g. the position was already closed by the exchange's own
   stop-loss between the confirm dialog opening and the click — the same race `tui-04` AC#7 named), **when**
   the call rejects, **then** the card is **not** removed; an inline error appears on that specific card, and
   the next poll's `GET /signal?status=open` result is authoritative on whether it should disappear.
10. **Accessibility**: `ConfirmDialog` traps focus, is dismissible via `Escape`, and returns focus to the
    triggering "Close Position" button on close/cancel — per the shared `ConfirmDialog` component's contract
    (frontend-10); the confirmation copy explicitly states the action is a market order at current price (not
    a limit order), matching the deleted TUI's exact wording so the operator isn't surprised by execution
    semantics.

## Out of Scope

- Editing/adding a stop-loss or take-profit from this page — no endpoint exists for placing a new protective
  order via this API surface; same non-goal the deleted TUI's `tui-04` stated.
- Partial position close (less than full quantity) — `POST /signal/close/{id}` has no quantity parameter,
  same limitation carried forward.
- Real-time push (SSE) for live P&L — Phase E (`frontend-12`); this page ships on `usePolling` first.
- Drilling into a position's originating Signal/Order full history as a separate page — the blueprint §7
  table names this as a "meaningfully more" web capability ("clickable row drills into the originating
  Signal/Order history"); this spec's card already surfaces every `Order` field it has, so a dedicated
  drill-down view would need to expose data not otherwise visible (e.g. multiple orders per signal if a
  strategy scales in) — deferred as a candidate follow-up, not built here, since v1's card already shows
  everything the single-entry-order case has.

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `api.get/post`, `Signal`/`Order`/`Strategy`/`TickerPrice` types
  (snake_case, reconciled against `backend-04-api-consistency-fixes.md`; `Signal` has `strategy_id` only,
  no nested `strategy` object).
- `frontend-10-shared-components.md` — `PositionCard`, `ConfirmDialog`, `usePolling`, the proposed
  per-symbol price-deduplication hook.
- `docs/specs/phase-3/tui-04-page-positions.md` — feature/data-parity checklist, and the resolved
  `StopLossPrice` gap noted above.

## Judgment Calls (flagged for explicit reconciliation)

1. **Take-profit price has no equivalent field on `entities.Order` today** (only `StopLossOrderID`/
   `StopLossPrice` exist; no `TakeProfitOrderID`/`TakeProfitPrice`). The deleted TUI's own wireframe showed a
   `TP: $63,000.00` example value but its spec text never actually resolved where that number would come
   from — this spec surfaces the same gap explicitly rather than silently dropping the TP row: it renders
   "(none set)" unconditionally until a backend field exists, which is honest but means the TP row is
   currently dead weight in the UI. Flagging as a real product gap worth raising, not asserting a fix within
   this docs-only task's scope.
2. **The 3-tier P&L color/weight discretization is ported directly from the TUI rather than upgraded to
   continuous color interpolation**, even though a browser CSS `color: hsl(...)` gradient could trivially do
   continuous scaling unlike termui's fixed palette. Kept as discrete tiers for this v1 because it's a proven,
   already-designed scheme requiring no new design work; continuous interpolation is a low-risk future
   polish item, not blocking anything else in this spec set.
