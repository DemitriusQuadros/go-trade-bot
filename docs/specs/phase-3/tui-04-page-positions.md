# Spec TUI-04 — Page 3: Open Positions (`cmd/console/pages/positions.go`)

## Overview

One card per open position with live P&L updated every second, color/brightness scaling by P&L
magnitude, inline stop-loss/take-profit display, and a `[c]` manual-close action with a confirmation
prompt. Replaces `openorders.go`'s role.

## Current Behavior (verified — file being deleted)

- `cmd/console/pages/openorders.go:39-56` (`Render`) builds a grid of one row per open signal, three
  columns (`strategy`, `invested`, `current`), each column a bare `widgets.Paragraph` — not the
  card-per-position layout PRD Page 3 describes (no entry price row, no explicit quantity/duration
  display beyond what's crammed into the "invested" paragraph, no SL/TP display at all).
- `openorders.go:94-118` — the live-price-update goroutine calls `exchangeClient.ListTickerPrices`
  directly (bypassing both the DB and any REST boundary — `exchangeClient` is instantiated at
  `openorders.go:65` via `exchange.NewBinanceAdapter(p.Dependencies.Cfg)`), computes P&L as
  `(price * qty) - (qty * entryPrice)` (`:107`), colors it red if negative else **blue** (`:110-113`,
  not green — a mismatch with PRD Page 3's "green if positive, red if negative" that this rebuild
  corrects), and calls `time.Sleep(1 * time.Second)` in an unbounded per-signal goroutine loop with no
  cancellation other than a shared `p.Stop` bool checked at the top of each iteration (`:97-99`) — one
  goroutine leaked per signal per page-visit until `Stop` is next checked, not immediately killed on
  `StopSync()`.
- `openorders.go:129-139` (`getOpenSignals`) — `repository.NewSignalRepository(d.Db).GetAllOpenSignals()`,
  direct DB access.
- No manual-close action exists on any current page — `POST /signal/close/{id}` exists as a REST endpoint
  (`app/handler/web/signal/handler.go:32-35`) but nothing calls it from `cmd/console` today.
- No stop-loss/take-profit display exists — `entities.Order.StopLossOrderID` (Spec 03, Phase 1) exists on
  the entity but is never read anywhere in `cmd/console`.

## Target Behavior

```go
// cmd/console/pages/positions.go
package pages

type PositionsPage struct {
    Header       *widgets.Paragraph
    TabPane      *widgets.TabPane
    Dependencies *dependencies.Dependencies
    positions    []apiclient.SignalView
    selectedCard int
    stop         chan struct{} // replaces openorders.go's shared bool with an explicit stop signal
    confirming   bool          // true while the [c] confirmation prompt is showing
}

func (p *PositionsPage) Render() ui.Drawable
func (p *PositionsPage) StartSync() // fetches GetOpenSignals + starts 1s ticker for ListTickerPrices per symbol
func (p *PositionsPage) StopSync()  // closes p.stop, guaranteed goroutine exit within one tick (<=1s)
func (p *PositionsPage) HandleEvent(e ui.Event) error // [c] on selected card -> confirmation -> CloseSignal
```

### P&L color/brightness scaling

PRD: "P&L column: green if positive, red if negative, brightness scaled to magnitude." termui's `ui.Color`
is a fixed 256-color-terminal palette (no arbitrary RGB brightness ramp in `ui.Style`), so "brightness
scaled to magnitude" is implemented as **discrete color steps** across `%` P&L magnitude, not continuous
brightness:

| `abs(PnL%)` | Positive | Negative |
|---|---|---|
| `< 1%` | `ui.ColorGreen` | `ui.ColorRed` |
| `1% - 5%` | `ui.ColorGreen` + bold (`ui.ModifierBold`) | `ui.ColorRed` + bold |
| `> 5%` | `ui.ColorGreen` + bold + `ui.ColorBlack` background (inverted, for max emphasis) | same pattern with red |

This 3-tier discretization is this spec's own proposal for approximating "brightness scaling" within
termui's actual color model — flagged as a judgment call since PRD doesn't specify exact thresholds.

## ASCII Wireframe

```
┌ go-trade-bot ── 2026-08-31 14:34:12 ── ● CONNECTED ── Balance: $12,480.55 ──────────────────────────┐
├ [1 Dashboard] [2 Strategies] [3 Positions] [4 Backtest] [5 Results] [6 Log] ─────────────────────────┤
│ ┌ Signal #142 — grid_btc — BTCUSDT ──────────────────┐ ┌ Signal #148 — scalp_eth — ETHUSDT ─────────┐│
│ │ Entry: $60,120.00   Qty: 0.05   Invested: $3,006.00 │ │ Entry: $3,040.00   Qty: 1.20  Inv: $3,648  ││
│ │ Current: $61,204.10                                 │ │ Current: $3,015.42                         ││
│ │ Unrealized P&L: +$54.21  (+1.80%)   Open: 2h 14m    │ │ Unrealized P&L: -$29.50 (-0.81%) Open: 8m  ││
│ │ SL: $58,900.00   TP: $63,000.00                     │ │ SL: (none set)   TP: (none set)            ││
│ └──────────────────────────────────────────────────────┘ └─────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
[↑↓/←→] select card   [c] close position   [F1-F6] switch page

                    ┌ Close position #142? ──────────────┐
                    │ This will submit a MARKET sell for  │
                    │ 0.05 BTCUSDT at current price.      │
                    │                                      │
                    │        [y] confirm    [n] cancel     │
                    └──────────────────────────────────────┘
```

## Acceptance Criteria

1. **Given** `GetOpenSignals` returns N open signals, **when** the page renders, **then** exactly N cards
   are shown, each with entry price, quantity, invested amount, current price, unrealized P&L in both $
   and %, and duration open (`time.Since(Signal.CreatedAt)`, formatted as `"2h 14m"`).
2. **Given** a position's unrealized P&L is positive, **when** the card renders, **then** the P&L text is
   green (never blue — explicitly correcting `openorders.go:112`'s bug noted in Current Behavior).
3. **Given** `Order.StopLossOrderID` is non-empty, **when** the card renders, **then** it shows the actual
   stop price inline (requires resolving the stop order's price — **gap**: `SignalView`/`entities.Order`
   stores `StopLossOrderID` as a broker order ID string, not a price; resolving it to a display price
   requires either the backend embedding the stop price in the `GET /signal/open` response payload, or a
   client-side `ExchangeClient.GetOrder` call — the latter is explicitly disallowed by ADR-007 since it
   would reach `internal/exchange` directly from `cmd/console`. This spec requires the **former**: flag to
   the concurrent backend spec that `SignalView`'s wire shape must include a resolved `StopLossPrice
   float64` field, not just the raw broker order ID).
4. **Given** the 1-second refresh ticker fires, **when** `ListTickerPrices` is called per distinct symbol
   across all open positions, **then** calls are deduplicated by symbol (two open BTCUSDT positions share
   one `ListTickerPrices("BTCUSDT")` call per tick, not two) to avoid redundant load on `cmd/api`.
5. **Given** `[c]` is pressed on the selected card, **when** the confirmation prompt appears, **then** no
   `CloseSignal` call is made until `[y]` is explicitly pressed — `[n]` or `[Esc]` dismisses with no side
   effect.
6. **Given** `[y]` confirms the close and `CloseSignal` succeeds, **when** the response returns, **then**
   the card is removed from the grid immediately (optimistic removal) rather than waiting for the next
   `GetOpenSignals` poll.
7. **Edge case — `CloseSignal` fails** (e.g. the position was already closed by the exchange's own
   stop-loss between the confirmation prompt and the keypress — a real race per Phase 1 Spec 03's
   `reconcileAlreadyStoppedPosition` path): **when** the call returns an error, **then** the card is
   **not** optimistically removed; an inline error appears on that card, and the next poll's
   `GetOpenSignals` result is the authority on whether the card should disappear.
8. **Edge case — `StopSync` called while goroutines are mid-`ListTickerPrices` call**: **when** the
   operator switches away from Page 3, **then** in-flight HTTP calls are allowed to complete but their
   results are discarded (checked against a closed `p.stop` channel before touching shared state) — no
   goroutine leak survives past the current tick, unlike `openorders.go`'s current per-signal
   goroutine-per-page-visit pattern (Current Behavior).

## Out of Scope

- Editing/adding a stop-loss or take-profit from this page — PRD Page 3 only mentions "shown inline if set
  as exchange orders," not an editor; no endpoint exists for placing a new protective order from the TUI.
- Partial position close (closing less than the full quantity) — `POST /signal/close/{id}` (existing
  endpoint) has no quantity parameter; a full close is the only supported action.

## Dependencies

- `tui-01-apiclient.md` — `GetOpenSignals`, `ListTickerPrices`, `CloseSignal`.
- `docs/architecture/refactoring-blueprint.md` §3.2 — `app/entities/position.go` [NEW] is named as "an
  open-position read model (derived from Signal+Order)" but does not exist in the working tree yet
  (confirmed absent). This page's `SignalView` is this spec's stand-in until that entity (and its
  corresponding API shape) lands; reconcile field names against it once available.

## Judgment Call

**`SignalView` must carry a resolved `StopLossPrice`/`TakeProfitPrice`, not raw broker order IDs** — see
Acceptance Criterion #3. This is a concrete, actionable ask for `backend-01-tui-consumed-api-endpoints.md`
that this spec surfaces but cannot resolve unilaterally, since resolving a broker order ID to its price
requires either a stored price at order-placement time (check whether `entities.Order` already has one —
it does not; `EntryPrice`/`ExitPrice` exist but no `StopPrice` field) or a live `GetOrder` lookup, which
only `cmd/api`/`cmd/worker` are allowed to perform per ADR-007.
