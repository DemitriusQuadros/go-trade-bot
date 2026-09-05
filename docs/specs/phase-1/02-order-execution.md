# Spec 02 — Order Execution (`GenerateBuySignal` / `GenerateSellSignal`)

## Overview

`SignalUseCase.GenerateBuySignal`/`GenerateSellSignal` currently only persist DB rows and adjust
`Account.Amount` — this is the PRD's headline finding. This spec wires real `PlaceOrder` calls into both
paths using the `ExchangeClient` from Spec 01, defines exact request/response handling including partial
fills, specifies what happens to `Signal`/`Order` rows on exchange rejection, and defines idempotency/retry
behavior for a financial operation where blind retries are dangerous.

## Current Behavior (verified)

- `app/usecase/signal/usecase.go:58-111` — `GenerateBuySignal(e EntrySignal)`: checks `CanOpenOrder()`,
  checks for an existing open signal (dedup), computes `investedAmount` from
  `AccountUseCase.GetDisponibleAmout()`, builds an `entities.Signal` with one `entities.Order` where
  `Quantity = investedAmount / e.EntryPrice`, `EntryFee = calculateEntryFee(investedAmount)` (hardcoded
  `0.1%`), calls `s.Repository.Create(signal)`, then `s.AccountUseCase.DeductOrder(investedAmount)`. **No
  exchange call anywhere in this path.**
- `app/usecase/signal/usecase.go:113-137` — `GenerateSellSignal(e ExitSignal)`: fetches the open signal,
  sets `Status = Closed`, sets `Orders[0].ExitPrice`, computes `ExitFee`/`Profit`, calls
  `s.Repository.Update(openSignal)`, then `s.AccountUseCase.AddOrder(...)`. **No exchange call anywhere.**
- `app/entities/signal.go:32-49` — `Order.BrokerOrderID string` and `Order.ExecutedQty float32` fields
  **already exist in the schema** but are never populated (always empty string / zero) — confirmed by
  search, no writer sets either field anywhere in the codebase today.
- `app/usecase/account/usecase.go:30-52` — `DeductOrder`/`AddOrder` mutate a single `Account{ID:1}` row's
  `Amount` and `AvailableOrders` counter directly; no linkage to any specific order or exchange fill.
- Callers: `app/services/algorithm/grid/algorithm.go:106,118,131`, `bollinger/algorithm.go:79,90,111`,
  `scalping/algorithm.go:71,93,152` all call `GenerateBuySignal`/`GenerateSellSignal` with a price computed
  from the strategy's own ticker/kline read (not from any exchange fill).

## Target Behavior

```go
// app/usecase/signal/usecase.go
type SignalUseCase struct {
    Repository     SignalRepository
    AccountUseCase AccountUseCase
    Exchange       exchange.ExchangeClient // replaces the local `Broker` interface (Spec 01)
}

func (s SignalUseCase) GenerateBuySignal(e EntrySignal) error
func (s SignalUseCase) GenerateSellSignal(e ExitSignal) error
```

**Buy path** (`GenerateBuySignal`):
1. `CanOpenOrder()` / dedup-check unchanged (existing behavior preserved).
2. Compute intended `investedAmount` and intended `Quantity` exactly as today (this becomes the
   *requested* quantity, not the *recorded* quantity).
3. Build a deterministic `ClientOrderID` (see Idempotency below) and call:
   `s.Exchange.PlaceOrder(ctx, exchange.PlaceOrderRequest{Symbol: e.Symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeMarket, Quantity: requestedQty, ClientOrderID: clientOrderID})`.
4. On success, the **recorded** `Order.Quantity` and `Order.EntryPrice` are taken from
   `OrderResult.ExecutedQty` / `OrderResult.AvgFillPrice` — **not** from `e.EntryPrice` (the strategy's
   pre-trade estimate). `Order.ExecutedQty` is populated with the same value as `Order.Quantity` (both
   fields already exist in the schema; this closes the "always zero" gap). `Order.BrokerOrderID =
   OrderResult.BrokerOrderID`.
5. If `OrderResult.Status == OrderStatusRejected` or `ExecutedQty == 0`: **no `Signal`/`Order` row is
   created**, `AccountUseCase.DeductOrder` is **not** called, and the error is returned to the caller
   (which logs it as a failed strategy cycle per `app/handler/tasks/strategy/handler.go`'s existing
   `StrategyExecution` error-status recording) plus triggers a `strategy.error` webhook (Spec 09).
6. If `ExecutedQty < requestedQty` (partial fill — see Acceptance Criteria #3), the `Signal`/`Order` row
   **is** created, but sized to the actual filled quantity; `AccountUseCase.DeductOrder` deducts
   `ExecutedQty * AvgFillPrice` (actual spent capital), not the originally intended `investedAmount`.
7. Immediately after a successful buy fill, `GenerateBuySignal` submits the stop-loss `STOP_MARKET` order
   per Spec 03 — this happens inside the same method, in the same DB transaction boundary as the `Signal`
   create (see Spec 03 for full detail; this spec only establishes that the buy path is where it's triggered).

**Sell path** (`GenerateSellSignal`):
1. Fetch open signal unchanged.
2. If the position has a resting stop-loss order (`Order.BrokerOrderID` for the stop leg — see Spec 03),
   **cancel it first** via `s.Exchange.CancelOrder(ctx, symbol, stopOrderID)` before submitting the closing
   market order (prevents the stop firing concurrently with a manual/algorithmic close — see Spec 03
   Acceptance Criteria for the race this avoids).
3. Call `PlaceOrder` with `Side: exchange.SideSell, Type: exchange.OrderTypeMarket, Quantity:
   openSignal.Orders[0].Quantity` (the actual held quantity from step 4 of the buy path, not a re-derived
   estimate).
4. `Order.ExitPrice` is set from `OrderResult.AvgFillPrice` (actual fill), not `e.ExitPrice` (caller's
   pre-trade estimate) — same principle as the buy path.
5. If the sell `PlaceOrder` call fails, the `Signal` row is **not** updated to `Closed` — it remains `Open`
   so a subsequent cycle retries the close; a `strategy.error` webhook fires immediately regardless.

## Idempotency & Retry

- **`ClientOrderID` scheme**: `fmt.Sprintf("gtb-%d-%s-%d", strategyID, side, signalID_or_timestamp)` — for
  buys, `signalID` doesn't exist yet at call time, so use a nanosecond timestamp bucketed to the current
  strategy cycle (the worker already knows the cycle boundary); for sells, use the existing `Signal.ID`.
  Binance deduplicates orders with the same `ClientOrderID` within its retention window, so a retried
  `PlaceOrder` call with the same ID after a network timeout cannot double-execute.
- **No automatic blind retry** on `PlaceOrder` failure. A financial order call that returns an ambiguous
  error (timeout, connection reset — result unknown, not a clean rejection) must **not** be retried with a
  *new* `ClientOrderID`, since that risks a double fill. Instead: on ambiguous failure, `GenerateBuySignal`
  returns the error immediately (no retry within the call), and the **next scheduled strategy cycle**
  re-attempts the buy from scratch using `GetOpenOrderByClientOrderID` reconciliation is deferred to Spec 03
  §restart reconciliation for the stop-loss leg specifically; for the entry order itself, Phase 1 accepts
  "fail the cycle, try again next cycle" as sufficient given cycles run on the order of 1-60 minutes and a
  missed entry is a lost opportunity, not a lost position.
- A clean `OrderStatusRejected` response (e.g. insufficient balance, `LOT_SIZE` filter violation) is
  **never** retried automatically — it indicates a config or sizing problem that will recur identically.

## Acceptance Criteria

1. **Given** a strategy triggers a buy signal for `BTCUSDT` with sufficient account balance, **when**
   `GenerateBuySignal` is called, **then** `Exchange.PlaceOrder` is invoked with `Side=BUY, Type=MARKET`,
   and on a `FILLED` response the resulting `Order.BrokerOrderID`, `Order.Quantity`, `Order.EntryPrice`,
   and `Order.ExecutedQty` all reflect the exchange response, not the caller's `EntrySignal.EntryPrice`.
2. **Given** the exchange rejects the order (e.g. `OrderStatusRejected`, insufficient balance), **when**
   `GenerateBuySignal` processes the response, **then** no `Signal` row is created, `Repository.Create` is
   never called, `AccountUseCase.DeductOrder` is never called, and the method returns a non-nil error.
3. **Given** the exchange fills only 60% of the requested quantity (`ExecutedQty = 0.6 * requestedQty`,
   `Status = PARTIALLY_FILLED` or `FILLED` at reduced size for a market order), **when**
   `GenerateBuySignal` processes the response, **then** the created `Order.Quantity` equals the actual
   `ExecutedQty` (not the originally requested quantity), and `AccountUseCase.DeductOrder` deducts only the
   capital actually spent (`ExecutedQty * AvgFillPrice`).
4. **Given** an open signal exists for `ETHUSDT` with a resting stop-loss order, **when**
   `GenerateSellSignal` is called (algorithmic take-profit trigger), **then** `CancelOrder` is called for
   the resting stop **before** the closing `PlaceOrder(SideSell)` call, and both calls use the symbol from
   the open signal.
5. **Given** `GenerateSellSignal`'s closing `PlaceOrder` call fails, **when** the error is returned,
   **then** the `Signal.Status` remains `Open` in the database (verified via `Repository.Update` not being
   called with `Status = Closed`), so the position is not silently lost from tracking.
6. **Given** two worker processes (or two retried calls) invoke `GenerateBuySignal` for the same strategy
   cycle with the same generated `ClientOrderID`, **when** both reach `PlaceOrder`, **then** the exchange
   returns the same `BrokerOrderID` for both (idempotent dedup), and only one `Signal` row is ultimately
   persisted (dedup enforced by the existing `GetOpenSignals` check plus the shared `ClientOrderID`).
7. **Edge case — zero balance race**: **Given** `CanOpenOrder()` returns `true` but the account balance
   changed between the check and the `PlaceOrder` call (e.g. another strategy consumed it), **when** the
   exchange rejects for insufficient balance, **then** behavior matches Acceptance Criterion #2 (no orphan
   `Signal` row) — the DB-side `CanOpenOrder` check is advisory, the exchange response is authoritative.
8. **Edge case — ambiguous network failure**: **Given** `PlaceOrder` returns a context-deadline-exceeded
   error (fill status unknown), **when** `GenerateBuySignal` handles it, **then** it does **not** retry
   with a new `ClientOrderID` within the same call, returns the error, and relies on the next scheduled
   cycle (with dedup via `GetOpenSignals`) to avoid a double entry.

## Out of Scope

- Limit orders / order-book-aware execution — Phase 1 uses `MARKET` orders only, matching current
  strategies' "act on current price" behavior.
- Futures leverage/margin order parameters (see Spec 01 Out of Scope).
- Dry-Run simulated fills (slippage/fee configuration) — Phase 2 (`app/engine/dryrun.go`).
- Reconciliation of entry orders across a bot restart mid-flight — only the stop-loss leg gets restart
  reconciliation in Phase 1 (Spec 03); entry-order reconciliation is an open question for a later phase.

## Dependencies

- Spec 01 (Exchange ACL) — `ExchangeClient` interface and `PlaceOrderRequest`/`OrderResult` types must exist.
- Spec 03 (Stop-Loss) — the buy path's stop-loss submission step is specified in detail there; this spec
  only establishes the trigger point.
