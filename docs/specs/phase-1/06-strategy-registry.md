# Spec 06 — Strategy Registry (`app/strategies/registry.go`)

## Overview

The current dispatch mechanism is a hardcoded `switch` on a closed 3-value `Algorithm` enum
(`app/handler/tasks/strategy/handler.go:107-119`). This spec defines the self-registering registry that
replaces it, and — because the registry's existence forces a schema change — the migration path for
`entities.Strategy`'s `Algorithm` field to a registry-validated free string, including back-compat for the
three existing rows/configs that carry `algorithm: "grid"|"scalping"|"bollinger"`.

## Current Behavior (verified)

- `app/entities/strategy.go:9-15` — `Algorithm` is a Go `string` type with exactly three untyped consts:
  `Grid = "grid"`, `Scalping = "scalping"`, `Bollinger = "bollinger"`.
- `app/entities/strategy.go:69-76` — `IsValidAlgorithm(algo string) bool` switches on these three literal
  values.
- `app/usecase/strategy/usecase.go:90-108` (`validateStrategy`) — calls `IsValidAlgorithm` and rejects with
  `customerror.New(http.StatusBadRequest, "Invalid algorithm option")` if it fails. This is the **only**
  validation call site for the field today.
- `app/handler/web/strategy/dto.go:15,25` — `StrategyDto.Algorithm string` maps directly to
  `entities.Algorithm(s.Algorithm)` with no independent validation at the DTO layer (validation happens
  downstream in the usecase).
- `app/handler/tasks/strategy/handler.go:107-119` — `processStrategy` switches on `strategy.Algorithm`
  (three cases + `default: log.Printf("Unknown strategy algorithm...")`, which leaves `executor` as a nil
  interface and would panic on the subsequent `executor.Execute()` call — **this is a latent nil-pointer
  bug in the current code**, not something the new registry needs to preserve).
- `docs/strategy-examples/{grid,bollinger,scalping}.json` — each has `"algorithm": "grid"` /
  `"scalping"` / `"bollinger"` respectively, matching the enum values exactly.

## Target Behavior

```go
// app/strategies/registry.go
package strategies

import (
    "fmt"
    "sort"
    "sync"
)

type StrategyFactory func() Strategy

var (
    mu       sync.RWMutex
    registry = map[string]StrategyFactory{}
)

// Register is called from each strategy package's init(). Panics on a duplicate name —
// see Acceptance Criteria #4 for rationale.
func Register(name string, factory StrategyFactory) {
    mu.Lock()
    defer mu.Unlock()
    if _, exists := registry[name]; exists {
        panic(fmt.Sprintf("strategies: duplicate registration for %q", name))
    }
    registry[name] = factory
}

// Get resolves a strategy by name. Returns (nil, false) if not found — callers (the engine,
// validation logic) must handle the false case explicitly; Get never panics.
func Get(name string) (Strategy, bool) {
    mu.RLock()
    defer mu.RUnlock()
    factory, ok := registry[name]
    if !ok {
        return nil, false
    }
    return factory(), true
}

// Names returns all registered strategy names, sorted, for validation error messages and
// TUI Page 4's strategy selector list (Phase 3 consumer, defined here since it's a registry concern).
func Names() []string {
    mu.RLock()
    defer mu.RUnlock()
    names := make([]string, 0, len(registry))
    for n := range registry {
        names = append(names, n)
    }
    sort.Strings(names)
    return names
}
```

Each ported strategy package self-registers, matching the PRD's exact pattern:

```go
// app/strategies/grid/strategy.go
func init() {
    strategies.Register("grid", func() strategies.Strategy {
        return &GridStrategy{}
    })
}
```

Registered names for Phase 1 are exactly `"grid"`, `"bollinger"`, `"scalping"` — chosen to match the
existing enum's string values **exactly**, which is what makes the migration additive rather than a hard
rename (see Migration below).

### Schema/validation migration

1. **Additive column**: add `entities.Strategy.StrategyName string` (new GORM column, e.g.
   `strategy_name`) alongside the existing `Algorithm Algorithm` field — **do not** drop or rename
   `Algorithm` in the same migration.
2. **Backfill**: a one-time migration step sets `StrategyName = string(Algorithm)` for every existing row
   (trivial because the registry names were chosen to match the enum values 1:1).
3. **Validation cutover**: `app/usecase/strategy/usecase.go:106-108`'s
   `if !entities.IsValidAlgorithm(...)` check is replaced with:
   ```go
   if _, ok := strategies.Get(strategy.StrategyName); !ok {
       return customerror.New(http.StatusBadRequest,
           fmt.Sprintf("Invalid strategy name %q, must be one of: %s", strategy.StrategyName, strings.Join(strategies.Names(), ", ")))
   }
   ```
   Note this calls `strategies.Get` (which *constructs* a throwaway instance) purely to check registration;
   an `Exists(name string) bool` convenience function is acceptable as an alternative to avoid the
   unnecessary allocation, at implementer's discretion — not a frozen-interface concern.
4. **DTO cutover**: `app/handler/web/strategy/dto.go` gains a `StrategyName string` JSON field; the
   deprecated `Algorithm string` field is kept accepted (mapped straight to `StrategyName` if
   `StrategyName` is empty and `Algorithm` is present) for exactly one deprecation window (see Open
   Question) so existing API clients / `docs/strategy-examples/*.json` configs don't break immediately.
5. **`entities.Algorithm` type and its three consts are kept, not deleted, in Phase 1** — they become
   vestigial (unused by validation) rather than removed, to avoid a breaking schema change in the same
   phase that also introduces live trading. Formal removal is a Phase 2+ cleanup, tracked as a follow-up,
   not a Phase 1 acceptance criterion.

## Acceptance Criteria

1. **Given** `app/strategies/grid`, `app/strategies/bollinger`, `app/strategies/scalping` are all imported
   (transitively, via the worker's `main.go` blank-importing each package for its `init()` side effect),
   **when** the process starts, **then** `strategies.Names()` returns exactly `["bollinger", "grid",
   "scalping"]` (sorted).
2. **Given** a `StrategyDto` with `strategy_name: "grid"`, **when** `validateStrategy` runs, **then**
   validation passes (registry lookup succeeds).
3. **Given** a `StrategyDto` with `strategy_name: "nonexistent"`, **when** `validateStrategy` runs, **then**
   it returns a `400`-coded `customerror` whose message lists the valid registered names.
4. **Given** two packages both call `strategies.Register("grid", ...)` (a packaging/naming bug introduced
   by a future contributor), **when** the process starts (both `init()`s run at package-load time), **then**
   the process panics immediately at startup with a message identifying the duplicate name — this is a
   fail-fast choice: a silent overwrite would mean one strategy is unreachable with no error, which is
   worse for a solo maintainer than a startup crash pointing directly at the bug.
5. **Given** an existing `entities.Strategy` row created before this migration (`Algorithm = "grid"`,
   `StrategyName = ""`), **when** the backfill migration runs, **then** `StrategyName` is set to `"grid"`
   for that row, and it passes validation identically to a strategy created after the migration with
   `strategy_name: "grid"` set directly.
6. **Given** a `StrategyDto` payload from before the DTO cutover — i.e. only `algorithm: "bollinger"` is
   present, no `strategy_name` field at all (matching `docs/strategy-examples/bollinger.json`'s current
   shape verbatim) — **when** `ToModel()` runs during the deprecation window, **then** the resulting
   `entities.Strategy.StrategyName` is `"bollinger"` and the save succeeds without requiring the caller to
   update their payload.
7. **Edge case — `strategies.Get` on an unregistered name at runtime** (e.g. a DB row's `StrategyName`
   references a strategy that was later un-registered/renamed by a code change): **given**
   `app/handler/tasks/strategy/handler.go`'s `processStrategy` calls `strategies.Get(nStrategy.StrategyName)`
   and it returns `(nil, false)`, **when** this happens, **then** the cycle is recorded as
   `StrategyExecution{Status: Error, Message: "strategy ... not found in registry"}` and a `strategy.error`
   webhook fires — this explicitly replaces the current code's latent nil-pointer-panic behavior in the
   `default:` case of the old switch (see Current Behavior) with a handled error path.
8. **Edge case — empty `StrategyName` on save**: **given** a `StrategyDto` with neither `strategy_name` nor
   the deprecated `algorithm` field set, **when** `validateStrategy` runs, **then** it fails with the same
   "Strategy has to have a name"-style `400` the current code already returns for an empty `Algorithm`
   (`usecase.go:102-104`), adapted to reference `strategy_name`.

## Out of Scope

- Removing/renaming the `entities.Algorithm` field and its consts — deferred past Phase 1 (see Migration
  step 5).
- Hot-reload of strategy registration without a recompile — PRD §5 explicitly accepts this Go/Jesse
  difference; not in scope for any phase covered by this PRD.
- The gRPC ML strategy's self-registration (`app/strategies/mlgrpc`) — Phase 4.

## Open Questions (raised by this spec)

- How long is the `algorithm` → `strategy_name` DTO deprecation window (Migration step 4)? This spec
  assumes "at least through Phase 1, revisit in Phase 2" but the blueprint's open-questions section (§8)
  doesn't address this specific migration; flagging for explicit decision alongside the other open
  questions already carried over from the PRD.

## Dependencies

- Spec 05 (Strategy Interface) — `StrategyFactory func() Strategy` depends on the frozen `Strategy` type.
- Spec 08 (Strategy Porting) — the three `init()` registrations are implemented as part of porting each
  strategy; this spec defines the registry mechanism they call into.
