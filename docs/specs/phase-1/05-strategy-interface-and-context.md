# Spec 05 — Strategy Interface, Context, Signal, ExecutionMode

## Overview

This spec freezes the `Strategy`/`Context`/`Signal`/`ExecutionMode` types (PRD §5, blueprint §3.1) that
every future strategy, the engine, and eventually the backtest engine (Phase 2) all depend on. Per ADR-005
and blueprint §6's "Scope creep in the Strategy/Context interface" risk, these signatures must not change
after the first strategy is ported (Spec 08) — this spec is the contract all downstream work is measured
against, including qa-specialist's Gherkin scenarios for each hook.

## Current Behavior (verified)

- No `Strategy` interface, `Context` struct, or `Signal` return type exists anywhere in the codebase today.
- The closest analogue is `IStrategyProcessor` (`app/handler/tasks/strategy/handler.go:25-27`): a
  single-method `Execute() error` interface with no per-candle granularity, no typed context, and no
  declarative order return — algorithms call `SignalUseCase.GenerateBuySignal`/`GenerateSellSignal`
  directly and imperatively from inside `Execute()`.
- State between cycles today is either (a) held in `internal/memcache` under ad-hoc string keys (grid's
  built levels, `grid/algorithm.go:15,61`) or (b) re-derived from scratch every cycle via
  `usecase.GetOpenSignal` (all three algorithms) — there is no unified `Context.Position` concept.

## Target Behavior

```go
// app/strategies/interface.go
package strategies

import "go-trade-bot/internal/exchange"

type ExecutionMode int

const (
    ModeBacktest ExecutionMode = iota
    ModeDryRun
    ModePaper
    ModeLive
)

func (m ExecutionMode) String() string        // "backtest" | "dryrun" | "paper" | "live"
func ParseExecutionMode(s string) (ExecutionMode, error) // inverse; error on unknown string

// Position is an in-memory, per-cycle read model built by app/engine from the current open
// Signal+Order (if any) for (strategyID, symbol). It is NOT the persisted app/entities.Position
// read-model slated for the TUI (Phase 3) — this is the lightweight type the engine hands to
// strategy hooks each cycle. See "Judgment call" note in Dependencies.
type Position struct {
    Symbol          string
    EntryPrice      float64
    Quantity        float64
    StopLossPrice   *float64 // nil if no resting stop (shouldn't happen post-Spec 03, but hooks must handle nil)
    StopLossOrderID string
    OpenedAt        time.Time
}

type Account struct {
    Available float64 // mirrors entities.Account.Amount at cycle-start read time
}

// Order is a strategy's *declared intent* (qty/price), not the exchange's response.
// The engine translates this into an exchange.PlaceOrderRequest.
type Order struct {
    Qty   float64
    Price float64
}

type Signal struct {
    Buy        *Order
    Sell       *Order
    StopLoss   *Order
    TakeProfit *Order
}

type Context struct {
    Candles    []exchange.Candle
    Position   *Position // nil if no open position for this symbol+strategy
    Account    Account
    Config     map[string]interface{}
    Indicators indicators.IndicatorProvider
    Price      float64 // most recent candle's Close
    Timeframe  string
    Symbol     string
    Mode       ExecutionMode
}

type Strategy interface {
    Name() string
    Before(ctx Context)
    ShouldLong(ctx Context) bool
    GoLong(ctx Context) Signal
    ShouldShort(ctx Context) bool
    GoShort(ctx Context) Signal
    UpdatePosition(ctx Context) *Signal
    After(ctx Context)
    Terminate(ctx Context)
}
```

### Hook contract — when the engine calls each, and what it does with the result

| Hook | Called when | Engine action on return |
|---|---|---|
| `Before(ctx)` | Once per cycle, before any decision hook, after `Context` is fully populated (candles fetched/received, `Position` resolved from DB). | No return value — side-effect only (e.g. a strategy might log or update internal struct-field state here). Must not call `GenerateBuySignal`/`GenerateSellSignal` directly — strategies never touch the exchange or DB directly (see Dependencies). |
| `ShouldLong(ctx)` | Only if `ctx.Position == nil` (no open position for this symbol+strategy). | If `true`, engine calls `GoLong(ctx)` next in the same cycle. If `false`, engine proceeds to `ShouldShort`. |
| `GoLong(ctx)` | Immediately after `ShouldLong` returns `true`. | Engine reads `Signal.Buy` (required — engine treats a `GoLong` call that returns a `Signal{}` with `Buy == nil` as a strategy bug: logged as `strategy.error`, no order placed) and `Signal.StopLoss` (optional — if nil, engine computes stop price from the strategy's `stop_loss_pct` config per Spec 03 default; if present, engine uses the strategy-supplied price instead). Engine calls `usecase.SignalUseCase.GenerateBuySignal` with these values. `Signal.Sell`/`Signal.TakeProfit` are ignored on this path (a strategy accidentally setting them here is not an error, just unused). |
| `ShouldShort(ctx)` | Only if `ctx.Position == nil` and `ShouldLong` returned `false`. Phase 1 strategies (grid/bollinger/scalping) are long-only — see Out of Scope. | If `true`, engine calls `GoShort(ctx)`. Phase 1: no ported strategy will return `true` here; the hook exists for interface completeness and Phase 3+ short-capable strategies. |
| `GoShort(ctx)` | After `ShouldShort` returns `true`. | Phase 1: engine has no short-position execution path wired (spot-only exchange adapter, Spec 01) — if called, engine logs `strategy.error` "short positions not supported in Phase 1" and takes no action. This is intentional: the hook must exist on the interface (frozen contract) even though it's inert this phase. |
| `UpdatePosition(ctx)` | Only if `ctx.Position != nil` (a position is open) — called instead of the `ShouldLong`/`ShouldShort` pair. | Returns `*Signal` (nullable). `nil` means "hold, no action" (matches PRD's Jesse comparison: `return nil // hold position`). Non-nil: engine inspects `Sell` (triggers `GenerateSellSignal` — take-profit or discretionary exit) and `StopLoss` (if non-nil and different from `ctx.Position.StopLossPrice`, engine treats this as a trailing-stop adjustment request — Phase 1 note: the ported strategies in Spec 08 never return a modified `StopLoss` here, since trailing-stop is explicitly Out of Scope for Phase 1 per Spec 03; the engine must still not panic if a future strategy does, but no Phase 1 acceptance criteria exercise this path). |
| `After(ctx)` | Once per cycle, after the decision/update hook completes (regardless of whether a signal was acted on). | No return value — side-effect only, mirrors `Before`. |
| `Terminate(ctx)` | Called when a strategy is disabled (`entities.Strategy.Status == Disabled`) or the process is shutting down cleanly — **not** called on a panic/crash (see `app/engine`'s panic-recovery, which is Phase 3 scope per blueprint §4 Phase 3; Phase 1's engine lets a panic propagate and fail the cycle, recorded as `StrategyExecution{Status: Error}`, matching today's implicit behavior of an unhandled panic in `Execute()`). | No return value. Strategies use this to release any held resources (Phase 1 strategies hold no unmanaged resources, so this is a no-op for all three ported strategies). |

### Nil-handling rules (explicit, since Go's zero values are easy to get wrong here)

- `Context.Position` is `nil` exactly when there is no `Signal{Status: Open}` for `(strategy, symbol)` —
  engine builds this by calling the existing `SignalUseCase.GetOpenSignal` per cycle (unchanged call,
  new consumer).
- `Signal{}` (all four `*Order` fields nil) is a valid, meaningful return from `GoLong`/`GoShort` only in
  the sense of "engine logs error, does nothing" (see `GoLong` row above) — it is **not** a valid way to
  express "no action," because `ShouldLong`/`ShouldShort` already gate whether these hooks are called at
  all. A strategy should never need to return an all-nil `Signal` from `GoLong`.
- `UpdatePosition` returning `nil` **is** the valid "no action" expression (unlike `GoLong`).

## Acceptance Criteria

1. **Given** a `Context` built for a symbol with no open position, **when** the engine evaluates the hook
   sequence, **then** it calls `Before` → `ShouldLong` → (if false) `ShouldShort` → `After`, in that order,
   and never calls `UpdatePosition` in the same cycle.
2. **Given** a `Context` built for a symbol **with** an open position, **when** the engine evaluates the
   hook sequence, **then** it calls `Before` → `UpdatePosition` → `After`, and never calls
   `ShouldLong`/`GoLong`/`ShouldShort`/`GoShort` in the same cycle.
3. **Given** `ShouldLong` returns `true` and `GoLong` returns `Signal{Buy: &Order{Qty: 0.01, Price: 50000}}`
   (no explicit `StopLoss`), **when** the engine processes the signal, **then** it calls
   `GenerateBuySignal` and separately triggers the default stop-loss submission (Spec 03) using the
   strategy's configured `stop_loss_pct`, not a strategy-supplied stop price.
4. **Given** `GoLong` returns `Signal{}` (a strategy bug — no `Buy` set despite `ShouldLong` returning
   `true`), **when** the engine processes it, **then** no order is placed, a `strategy.error` webhook
   fires, and the cycle's `StrategyExecution` is recorded with `Status = Error`.
5. **Given** `UpdatePosition` returns `nil`, **when** the engine processes the cycle, **then** no
   `GenerateSellSignal` call occurs and the position remains open and unchanged.
6. **Given** `UpdatePosition` returns `&Signal{Sell: &Order{...}}`, **when** the engine processes it,
   **then** `GenerateSellSignal` is called (which per Spec 03 also cancels the resting stop-loss order
   first).
7. **Given** a strategy's `ShouldShort` returns `true` in Phase 1, **when** the engine reaches `GoShort`,
   **then** no exchange call is made, a `strategy.error` "short positions not supported" is logged/webhooked,
   and the cycle completes without a panic.
8. **Edge case — `Terminate` on disable, not on error**: **Given** a strategy's `entities.Strategy.Status`
   is updated to `Disabled` between cycles, **when** the next scheduled cycle is about to run, **then**
   `Terminate(ctx)` is called once and no further hooks run for that strategy until re-enabled (matches
   existing `HandleStrategyTask`'s `if nStrategy.Status != entities.Disabled` gate, `handler.go:78`, which
   already skips execution — this spec adds the `Terminate` call at the transition, which doesn't exist
   today).
9. **Edge case — hook panic**: **Given** any hook panics (e.g. a strategy divides by zero on an empty
   candle slice), **when** the engine's per-cycle call completes, **then** the panic is caught at the
   cycle boundary (not propagated to crash the worker process), recorded as `StrategyExecution{Status:
   Error}`, and `Terminate` is **not** called for this occurrence (per the table above — Phase 1 has no
   panic-recovery-then-restart semantics; that's Phase 3's `strategy_panics_total` work).

## Out of Scope

- Short-position execution (`GoShort` producing a real short sell/margin order) — Phase 1's `ExchangeClient`
  is spot-only (Spec 01); the hook exists on the frozen interface but is inert.
- Trailing stop-loss adjustment via `UpdatePosition`'s `StopLoss` field — Spec 03 explicitly defers this.
- Multi-timeframe `Context.Candles` (single timeframe only in Phase 1) — Phase 3.
- Panic-recovery-and-restart per strategy (`strategy_panics_total`) — Phase 3, per blueprint §4.

## Dependencies

- Spec 01 (Exchange ACL) — `Context.Candles` is `[]exchange.Candle`.
- Spec 07 (Indicator Provider) — `Context.Indicators` type.
- **Judgment call**: this spec introduces a `strategies.Position` type distinct from the
  `app/entities/position.go` "open-position read model" that the blueprint's directory tree lists without a
  clear phase/consumer. This spec treats `entities.Position` as a *separate*, Phase-3-oriented persisted
  read model for the TUI's Open Positions page, and `strategies.Position` as the lightweight in-memory type
  the engine builds fresh each cycle from `Signal`+`Order`. This split isn't explicitly settled in the
  blueprint and should be confirmed before Spec 06/08 implementation begins.
