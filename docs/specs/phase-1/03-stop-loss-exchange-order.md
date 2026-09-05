# Spec 03 — Stop-Loss as Real Exchange Order

## Overview

Stop-loss today is a software poll: every strategy cycle re-reads the ticker price and compares it against
`entryPrice * (1 - stopLossPct)` in memory. If the worker process is down when price crosses the threshold,
the stop never fires and the position is unprotected. This spec replaces that with a real `STOP_MARKET`
order submitted to the exchange at position-open time, and covers the three failure modes the PRD calls
out explicitly: submission failure, manual close while a stop rests, and restart reconciliation.

## Current Behavior (verified)

- `app/services/algorithm/grid/algorithm.go:84-112` (`monitore`) — reads `stopLossPct` from config, calls
  `p.broker.ListTickerPrices`, computes `pnl := (current - entryPrice) / entryPrice * 100`, and if
  `pnl <= -stopLossPct` calls `GenerateSellSignal`. This runs once per strategy cycle (1-60 minutes per
  `entities.Cycle`), not continuously.
- `app/services/algorithm/bollinger/algorithm.go:96-114` (`generateSell`) — same pattern: `pnl <=
  -stopLossPct` triggers a sell, computed from a fresh ticker read.
- `app/services/algorithm/scalping/algorithm.go:129-160` (`generateSell`) — identical pattern.
- In all three, the "stop-loss" is indistinguishable in code from the take-profit check — both are
  evaluated in the same `if pnl >= takeProfitPct || pnl <= -stopLossPct` branch, meaning there is currently
  no separate exchange-side artifact for either.
- `entities.Order.BrokerOrderID` exists but is always empty (see Spec 02) — there is no field currently
  used to track a resting stop order's exchange ID.
- No process-restart reconciliation logic exists anywhere — confirmed by absence of any "on startup, query
  open orders" code path in `cmd/worker/main.go` or `app/handler/tasks/strategy/handler.go`.

## Target Behavior

```go
// app/entities/signal.go — new field on Order (additive migration)
type Order struct {
    // ...existing fields...
    StopLossOrderID string // BrokerOrderID of the resting STOP_MARKET order; empty if none/not-yet-triggered/cancelled
}
```

**At position-open time** (inside `GenerateBuySignal`, immediately after the entry fill is recorded per
Spec 02 step 7):
1. Compute `stopPrice = fillPrice * (1 - stopLossPct/100)` using the strategy's configured `stop_loss_pct`
   (passed through `EntrySignal` — see Dependencies) and the **actual fill price**, not the pre-trade
   estimate.
2. Call `s.Exchange.PlaceOrder(ctx, exchange.PlaceOrderRequest{Symbol: e.Symbol, Side: exchange.SideSell,
   Type: exchange.OrderTypeStopMarket, Quantity: filledQty, StopPrice: stopPrice, ClientOrderID: <deterministic, see Spec 02>})`.
3. On success (`Status == OrderStatusNew`, i.e. resting), `Order.StopLossOrderID = OrderResult.BrokerOrderID`.
4. **On submission failure**: the entry position is **not** rolled back (the buy already filled — reversing
   it would itself be a real sell order with its own risk). Instead: log the failure, fire a
   `strategy.error` webhook with severity indicating "position opened WITHOUT stop-loss protection", and
   retry the `STOP_MARKET` submission once immediately (same cycle, same `ClientOrderID`) — this is the one
   case in Phase 1 where an immediate retry is acceptable, because a `STOP_MARKET` placement failure is a
   protective-order gap, not a risk of double-execution (retrying a stop placement can't cause an unwanted
   fill). If the retry also fails, the position is left open and unprotected, and the error is surfaced
   loudly (webhook + `strategy_panics_total`-style log level) rather than silently swallowed.

**While a position is open with a resting stop:**
- The engine (Spec 05) does not poll price against a software threshold anymore for stop-loss purposes —
  only take-profit remains a software-evaluated condition per cycle (the PRD only mandates SL as an
  exchange order; TP stays software-evaluated in Phase 1, matching PRD §4.1's scope).
- **Manual or algorithmic close while the stop still rests**: per Spec 02 step 2 of the sell path,
  `GenerateSellSignal` calls `CancelOrder(ctx, symbol, order.StopLossOrderID)` **before** placing the
  closing market sell. This is the mitigation for the race where both the stop and a manual close could
  fire concurrently and double-sell the position.
- If `CancelOrder` fails because the stop **already triggered** on the exchange between the last price read
  and the cancel attempt (Binance returns an "unknown order" / "order does not exist" error in this case),
  `GenerateSellSignal` treats this as: the position is already closed by the exchange-side stop. It queries
  `ExchangeClient` (via `ListKline`/ticker, or ideally a `GetOrder` status check — see Open Questions) to
  confirm, and if confirmed, updates the `Signal` to `Closed` using the **stop order's actual fill price**
  (not attempting a second sell). This is the specific edge case that justifies real exchange-side stops:
  the software layer must reconcile with, not fight, the exchange's own execution.

**Restart reconciliation** (worker process startup, before resuming normal cycle scheduling):
1. Query `repository.GetAllOpenSignalsWithStopOrders()` (new repository method — all `Signal{Status: Open}`
   rows whose `Order.StopLossOrderID != ""`).
2. For each, call a new `ExchangeClient` capability to check order status by ID (see Open Question below —
   this may require extending the `ExchangeClient` interface with a `GetOrder` method beyond what Spec 01
   currently defines, since Spec 01's interface as frozen doesn't include order-status lookup).
3. Three outcomes per open signal:
   - Stop order still resting (`NEW`) on the exchange → no action, tracking is consistent.
   - Stop order shows `FILLED` on the exchange but the local `Signal` is still `Open` → the position was
     closed by the stop while the worker was down. Update the `Signal` to `Closed` using the stop's actual
     fill price and fire a `position.closed` webhook (Spec 09) with a note that this was reconciled on
     restart, not observed live.
   - Stop order is missing entirely (cancelled externally, or never actually placed despite a
     `StopLossOrderID` being recorded — e.g. crash between placement and DB write) → the position is open
     and **unprotected**. Fire a `strategy.error` webhook immediately flagging "unprotected position
     detected on restart" and attempt to resubmit a fresh `STOP_MARKET` using the position's recorded entry
     price and the strategy's current `stop_loss_pct` config.

## Acceptance Criteria

1. **Given** a buy signal fills successfully, **when** `GenerateBuySignal` completes, **then** a
   `STOP_MARKET` order has been submitted with `StopPrice = fillPrice * (1 - stopLossPct/100)` and
   `Order.StopLossOrderID` is populated with the returned `BrokerOrderID`.
2. **Given** the `STOP_MARKET` submission fails on the first attempt, **when** the retry (step 4 above) also
   fails, **then** the position remains open in the DB, `Order.StopLossOrderID` remains empty, and a
   `strategy.error` webhook fires with a message identifying the symbol/strategy/unprotected condition.
3. **Given** an open position with a resting stop, **when** a take-profit condition triggers
   `GenerateSellSignal`, **then** `CancelOrder` is called with the recorded `StopLossOrderID` before the
   closing market sell is placed.
4. **Given** the stop order has already been filled by the exchange (price crossed the stop while the bot
   was mid-cycle evaluating take-profit), **when** `GenerateSellSignal`'s `CancelOrder` call fails with an
   "order does not exist" style error, **then** the code does **not** place a second market sell — it
   reconciles the `Signal` to `Closed` using the stop's fill data instead.
5. **Given** the worker restarts with 3 open signals in the DB, 2 of which have a `StopLossOrderID` still
   resting on the exchange and 1 of which has no matching order on the exchange, **when** the restart
   reconciliation routine runs, **then** the 2 consistent positions are left untouched, and the 1
   inconsistent position triggers both a `strategy.error` webhook and a resubmission attempt of a new
   `STOP_MARKET` order.
6. **Edge case — reconciliation finds the stop already filled during downtime**: **Given** a resting stop
   order shows `FILLED` status on restart while the local `Signal` is still `Open`, **when** reconciliation
   processes it, **then** the `Signal` is updated to `Closed` with `ExitPrice` taken from the stop order's
   fill data (not a fresh ticker read), and `AccountUseCase.AddOrder` is called exactly once for this
   reconciled close (must not double-credit if reconciliation runs twice due to a crash-loop).
7. **Edge case — StopPrice violates exchange filters**: **Given** the computed `stopPrice` fails Binance's
   `PERCENT_PRICE` or `PRICE_FILTER` exchange filter (e.g. `stop_loss_pct` configured absurdly high/low for
   the symbol's current volatility), **when** `PlaceOrder` rejects the `STOP_MARKET` request, **then** this
   is treated identically to acceptance criterion #2 (unprotected-position alert), not a silent skip.

## Out of Scope

- Take-profit as an exchange-side order (`TAKE_PROFIT_MARKET`) — PRD §4.1 only mandates stop-loss as a real
  order; take-profit remains software-evaluated in Phase 1.
- Trailing stop adjustment (`UpdatePosition` hook dynamically moving the stop) — mentioned in PRD §5's
  Jesse example but not in the Phase 1 MUST list; defer to Phase 3 position-management work.
- A dedicated `GetOrder`/order-status-lookup method's exact interface signature — flagged as an open
  question below since it extends Spec 01's frozen interface.

## Open Questions (raised by this spec, not yet settled)

- Spec 01 freezes `ExchangeClient` without a `GetOrder`/order-status-lookup method, but restart
  reconciliation (and the "stop already filled" race in `GenerateSellSignal`) both need one. **This should
  be resolved by adding `GetOrder(ctx, symbol, orderID string) (OrderResult, error)` to `ExchangeClient`
  before Spec 01 is implemented** — flagging this back to Spec 01 rather than treating `ExchangeClient` as
  already-final. (See judgment call disclosed in the task summary.)

## Dependencies

- Spec 01 (Exchange ACL) — and per the Open Question above, Spec 01's interface likely needs one additional
  method before this spec can be fully implemented.
- Spec 02 (Order Execution) — stop-loss submission is triggered from within `GenerateBuySignal`; cancellation
  is triggered from within `GenerateSellSignal`.
