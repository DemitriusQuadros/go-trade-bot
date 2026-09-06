# Spec backend-02 — Pluggable Position Sizing

## Overview

Blueprint §4 Phase 3 wants "position sizing strategies (fixed amount, % of capital)" to replace the inline
`ctx.Account.Available / ctx.Price` calculation duplicated in all three ported strategies. This spec makes a
finding that changes the shape of the solution: **the strategies' own computed `Qty` is already dead code**
— the engine discards it and computes the real invested amount independently. This means pluggable sizing
can be built entirely in the usecase/engine layer, touching zero frozen `app/strategies` types (`Strategy`,
`Context`, `Signal`, `ExecutionMode` — ADR-005) and zero ported strategy files.

## Current Behavior (verified)

- `app/strategies/grid/strategy.go:127-132` (`GoLong`): `qty = ctx.Account.Available / ctx.Price`, returns
  `strategies.Signal{Buy: &strategies.Order{Qty: qty, Price: ctx.Price}}`.
- `app/strategies/bollinger/strategy.go:49-52`: identical formula, identical shape.
- `app/strategies/scalping/strategy.go:62-64`: identical formula (using `latestClose` in place of
  `ctx.Price`, functionally the same value at that call site).
- **The finding**: `app/engine/engine.go:224-250` (`processGoLong`) builds `usecase.EntrySignal` from the
  returned `signal.Buy` value, but only reads `signal.Buy.Price` (line 237: `EntryPrice:
  float32(signal.Buy.Price)`) — **`signal.Buy.Qty` is never read anywhere in `processGoLong` or
  `EntrySignal`'s definition** (`app/usecase/signal/usecase.go:19-39`, confirmed no `Qty` field exists on
  `EntrySignal` at all). The actual invested amount is computed independently, inside
  `GenerateBuySignal` itself (`app/usecase/signal/usecase.go:120-121`):
  `investedAmount, _ := s.AccountUseCase.GetDisponibleAmout(); requestedQty := float64(investedAmount) / float64(e.EntryPrice)`.
- `app/usecase/account/usecase.go:54-60` (`GetDisponibleAmout`): `return account.Amount /
  float32(account.AvailableOrders), nil` — a single, **global, per-account** sizing rule ("split remaining
  balance evenly across remaining open-order slots"), applied identically regardless of which strategy is
  buying. This is the actual sizing policy in effect today, and it is not configurable per strategy at all.
- Conclusion: the three strategies' `qty := ctx.Account.Available / ctx.Price` lines compute a value that
  is silently discarded, then the engine/usecase layer independently recomputes the real quantity via a
  completely different, hardcoded formula. This is dead code and a duplicated-but-inert calculation, not a
  case where three different quantities are actually being used.

## Target Behavior

Since sizing is already effectively an engine/usecase-layer decision (not a strategy-layer one, in
practice), this spec makes it **explicitly** so and **pluggable**, without touching
`app/strategies/interface.go` or any ported strategy file:

```go
// app/usecase/signal/sizing.go (NEW)
package usecase

type SizingType string

const (
    SizingFixedAmount SizingType = "fixed_amount" // Value = absolute currency amount to invest per entry
    SizingPctCapital  SizingType = "pct_capital"   // Value = percentage (0-100) of available balance
)

type PositionSizingConfig struct {
    Type  SizingType
    Value float64
}

// PositionSizer computes the amount to invest for one entry, given the
// account's current available balance. Falls back to Phase 1's existing
// AccountUseCase.GetDisponibleAmout() behavior when no PositionSizingConfig
// is supplied (nil) - see AC#5, this preserves all three shipped example
// configs' behavior unchanged.
type PositionSizer interface {
    Size(cfg *PositionSizingConfig, available float64) (investedAmount float64, err error)
}

func NewDefaultPositionSizer() PositionSizer // fixed_amount / pct_capital implementation, below
```

```go
// EntrySignal gains one new field (non-frozen type - this is an internal
// usecase struct, not part of app/strategies' ADR-005-frozen surface):
type EntrySignal struct {
    // ...existing fields...
    PositionSizing *PositionSizingConfig // nil => existing GetDisponibleAmout() behavior (AC#5)
}
```

`app/engine/engine.go`'s `processGoLong` gains a helper mirroring the existing `strategyStopLossPct`
pattern (`engine.go:289-296`) exactly:
```go
func strategyPositionSizing(dbStrategy entities.Strategy) *usecase.PositionSizingConfig {
    var config map[string]interface{}
    if err := json.Unmarshal(dbStrategy.StrategyConfiguration.Configuration, &config); err != nil {
        return nil
    }
    sizing, ok := config["position_sizing"].(map[string]interface{})
    if !ok {
        return nil
    }
    // parse "type"/"value" keys into usecase.PositionSizingConfig, returning nil on any malformed shape
    // (fail open to the default behavior, not a hard error - a strategy's config is user-authored JSON
    // and a typo here shouldn't break trading, just fall back silently to today's behavior)
}
```
`entry.PositionSizing = strategyPositionSizing(dbStrategy)` is set alongside the existing
`entry.StopLossPct = strategyStopLossPct(dbStrategy)` line in `processGoLong`.

`app/usecase/signal/usecase.go`'s `GenerateBuySignal` replaces its current unconditional
`s.AccountUseCase.GetDisponibleAmout()` call with:
```go
available, _ := s.AccountUseCase.GetDisponibleAmout() // still needed as the fallback / as the ceiling
                                                          // for fixed_amount's "can't invest more than
                                                          // available" clamp (see AC#2)
investedAmount, err := s.Sizer.Size(e.PositionSizing, float64(available))
```
where `s.Sizer PositionSizer` is a new `SignalUseCase` field, wired at DI-construction time (`cmd/*/modules`)
to `NewDefaultPositionSizer()`.

### Per-type computation

- `fixed_amount`: `investedAmount = min(Value, available)` — never invests more than the account actually
  has, regardless of the configured fixed amount (protects against a stale/misconfigured large fixed value
  after the account balance has drawn down).
- `pct_capital`: `investedAmount = available * (Value / 100)`. `Value` outside `[0, 100]` is a configuration
  error (see Acceptance Criteria).

## Acceptance Criteria

1. **Given** a strategy with no `position_sizing` key in its `Configuration` JSON (all three shipped
   example configs, unchanged), **when** `GenerateBuySignal` runs, **then** `investedAmount` matches
   exactly what `AccountUseCase.GetDisponibleAmout()` alone would have produced pre-this-spec — zero
   behavioral change for any strategy that doesn't opt in.
2. **Given** `position_sizing: {"type": "fixed_amount", "value": 500}` and `available = 300`, **when**
   sizing runs, **then** `investedAmount = 300` (clamped to available), not `500` (never invest more than
   the account has, regardless of configuration).
3. **Given** `position_sizing: {"type": "fixed_amount", "value": 100}` and `available = 300`, **when**
   sizing runs, **then** `investedAmount = 100` exactly (below the ceiling, uses the configured value
   as-is).
4. **Given** `position_sizing: {"type": "pct_capital", "value": 10}` and `available = 1000`, **when**
   sizing runs, **then** `investedAmount = 100`.
5. **Given** a strategy's `Configuration` JSON has a malformed `position_sizing` value (e.g. `"position_sizing":
   "not an object"`), **when** `strategyPositionSizing` parses it, **then** it returns `nil` (fails open to
   the default `GetDisponibleAmout()` behavior per Acceptance Criterion #1) rather than causing
   `GenerateBuySignal` to error out and skip an otherwise-valid trading signal over a config typo.
6. **Edge case — `pct_capital` value out of range**: **given** `position_sizing: {"type": "pct_capital",
   "value": 150}`, **when** sizing runs, **then** `Size` returns an error (not a silent clamp to 100, and
   not an over-100%-of-capital investment) — this is a genuine configuration error the operator should be
   told about explicitly (via the same error-surfacing path `GenerateBuySignal` already uses for other
   failures), unlike the malformed-JSON case above which fails open because it's ambiguous whether the
   field was even intended to be read.
7. **Edge case — zero available balance**: **given** `available = 0` under any sizing type, **when**
   sizing runs, **then** `investedAmount = 0` and the existing `AccountUseCase.CanOpenOrder()` gate
   (unchanged, called earlier in `GenerateBuySignal`) is what actually prevents the trade — `PositionSizer`
   itself doesn't need its own "can afford" guard duplicate.

## Out of Scope

- Volatility-based or ATR-based position sizing — not requested by the blueprint's Phase 3 line item
  ("fixed amount, % of capital" only); `internal/indicators.ATR` exists (Phase 1, unused) but wiring it into
  sizing is a natural future extension, not built here.
- Per-symbol sizing overrides within a single strategy (today's `position_sizing` config is one value for
  the whole strategy, applied identically across all `MonitoredSymbols`) — not requested.
- Retroactively changing how existing open positions were sized — this spec only affects new entries after
  the config is added.

## Dependencies

- Phase 1's `app/engine/engine.go` (`strategyStopLossPct`'s pattern is directly mirrored),
  `app/usecase/signal/usecase.go`, `app/usecase/account/usecase.go` — all modified per Target Behavior, none
  of which are frozen types.
- **Judgment call**: this spec's central claim — that `Qty` returned by `GoLong`/`UpdatePosition` is dead
  code today — is the load-bearing fact that makes "no `Context`/`Signal` changes needed" true. If a future
  reviewer finds a code path that *does* read `signal.Buy.Qty` (one wasn't found in this audit), this
  spec's approach would need revisiting; flagging this explicitly as the one fact worth double-checking
  before implementation, since the entire design rests on it.
