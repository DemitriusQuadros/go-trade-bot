# Spec 10 — Mode Guard & Execution Mode (ADR-002 Dual-Layer Guard)

## Overview

There is no concept of execution mode today — `entities.Strategy.Status` (`Productive`/`Testing`/`Disabled`)
is a lifecycle flag, not a risk tier, and "testing" strategies run against the exact same live Binance REST
client as "productive" ones (the only reason nothing bad has happened is that no order-placement code
exists yet — Spec 02 changes that). This spec implements ADR-002's two independent safety layers: a
per-strategy persisted `Mode` capped by a process-wide `MODE` env var + `--confirm-live` flag, and defines
the exact precedence/refusal logic as testable cases.

## Current Behavior (verified)

- `app/entities/strategy.go:24-30` — `StrategyStatus` (`Productive`/`Testing`/`Disabled`) is the only
  status-like field; no `Mode` field exists.
- `internal/configuration/configuration.go:10-36` — `Configuration` struct has `Broker`, `DB`, `Redis`,
  `Prometheus` only; no `Mode`, `ConfirmLive`, or `Testnet` field.
- `cmd/worker/main.go:99-117` (`main()`) — builds the `fx.App` and calls `app.Run()` unconditionally; no
  startup guard of any kind exists — the worker starts and begins processing strategy tasks regardless of
  any safety flag, because no such flag exists to check.
- `docs/strategy-examples/{grid,bollinger,scalping}.json` all have `"status": "testing"` — this is the PRD's
  own cited evidence that today's "testing" designation carries no actual execution-safety weight, since the
  same code path (all reads, no writes) runs for `"testing"` and `"productive"` alike.

## Target Behavior

```go
// app/strategies/interface.go (Spec 05) — ExecutionMode already defined there; repeated here for context
type ExecutionMode int
const (
    ModeBacktest ExecutionMode = iota // 0 — lowest risk tier
    ModeDryRun                        // 1
    ModePaper                         // 2
    ModeLive                          // 3 — highest risk tier
)
```

```go
// app/entities/strategy.go — new field (additive migration)
type Strategy struct {
    // ...existing fields...
    Mode string `gorm:"default:'dryrun'"` // "backtest" | "dryrun" | "paper" | "live" — stored as string,
                                            // parsed via strategies.ParseExecutionMode (Spec 05) at
                                            // Context-build time. New AND existing rows default to "dryrun"
                                            // via the migration (never "live").
}
```

```go
// internal/configuration/configuration.go — additive fields
type Configuration struct {
    // ...existing fields...
    Mode        string // process-wide ceiling, from MODE env var, e.g. "paper"
    ConfirmLive bool   // from --confirm-live CLI flag
    Testnet     bool   // whether the exchange adapter should target Binance testnet (Spec 01)
    WebhookURL  string // Spec 09
}
```

### Precedence & refusal logic

**Ordinal comparison**: `ModeBacktest(0) < ModeDryRun(1) < ModePaper(2) < ModeLive(3)`. The **effective**
mode for a given strategy execution is `min(strategy.Mode, process.MODE)` by this ordinal — i.e. the
process-wide `MODE` env var is a **ceiling**, never a floor. A strategy configured for `live` running on a
worker started with `MODE=paper` executes as if it were `paper` for that cycle (not refused outright,
downgraded) — **except** for the specific case of `MODE=live` itself, which has its own separate
`--confirm-live` gate (below), and except where downgrading silently could itself be dangerous (see
Acceptance Criterion #3's discussion of why downgrade, not refusal, is the chosen behavior, with a flagged
alternative).

**Startup gate** (`cmd/worker/main.go`, checked once before `app.Run()`):
1. Parse `MODE` env var via `configuration.Configuration.Mode` → `strategies.ParseExecutionMode`. Unknown/
   unset value defaults to `ModeDryRun` (the safest non-inert default — matches the per-strategy DB default).
2. If the parsed process-wide mode is `ModeLive` and `ConfirmLive == false` (i.e. `--confirm-live` flag not
   passed), the process **refuses to start**: prints a clear error to stderr and exits non-zero **before**
   the `fx.App` is even constructed — this is a hard startup failure, not a runtime-skipped cycle.
3. If `ModeLive` and `ConfirmLive == true`, the process starts normally with `MODE=live` as the ceiling.
4. Any mode below `ModeLive` (`backtest`/`dryrun`/`paper`) starts normally regardless of `--confirm-live`
   (the flag is only meaningful at the `live` ceiling).

**Per-cycle gate** (inside the engine, per strategy execution — Spec 05's `Context.Mode` is set to the
**effective** (capped) mode, not the raw `strategy.Mode`):
- If `strategy.Mode` (raw, uncapped) resolves to a **higher** ordinal than the process `MODE` ceiling — the
  scenario named explicitly in the task ("strategy.Mode=live, env MODE=paper") — the cycle is **not**
  silently downgraded to `paper` execution when the strategy's *own* setting is `live` specifically,
  because silently running a strategy the operator explicitly flagged for **real capital** at a lower risk
  tier without telling them is its own kind of surprising/dangerous behavior. **This spec resolves the
  scenario as: refuse the cycle entirely, log at error level, and fire a `strategy.error` webhook** (Spec
  09) identifying the mismatch — this is a deliberate carve-out from the general "cap, don't refuse" rule
  stated above, specifically for the `live` tier, mirroring the asymmetric caution the startup gate already
  applies to `live`. For any other combination (e.g. `strategy.Mode=paper`, `MODE=dryrun`), the general cap
  rule applies: execute at the lower (`dryrun`) tier without refusing.
- Refusing a cycle (the `live`-mismatch case) records `StrategyExecution{Status: Error, Message: "strategy
  configured for live mode but process MODE ceiling is <X>"}` and does **not** re-enqueue... — it **does**
  still re-enqueue for the next cycle (matching existing `HandleStrategyTask` behavior of always
  re-enqueuing non-disabled strategies, `handler.go:87`), since the mismatch might be corrected by the next
  worker restart with a different `MODE`.

### Interaction with `Testnet` (Spec 01)

- `ModePaper` **requires** `Testnet == true` (Spec 01 Acceptance Criterion #9 already specifies the inverse
  guard: `Testnet=true` + effective mode `live` is a startup config error). This spec adds the mirror case:
  effective mode `paper` with `Testnet == false` is **also** a startup config error — Paper Trading mode
  must never resolve to the production exchange adapter.
- `ModeLive` requires `Testnet == false`.
- `ModeBacktest`/`ModeDryRun` are agnostic to `Testnet` in Phase 1 (Backtest doesn't call `ExchangeClient`
  at all yet — Phase 2; Dry-Run per PRD §4.4 uses live WebSocket prices but simulated fills, so it doesn't
  call `PlaceOrder` either — see Out of Scope).

## Acceptance Criteria

1. **Given** `MODE=live` and `--confirm-live` is **not** passed, **when** `cmd/worker/main.go` starts,
   **then** the process exits non-zero before constructing the `fx.App`, with a stderr message naming the
   missing flag.
2. **Given** `MODE=live --confirm-live`, **when** the worker starts, **then** it starts normally and the
   effective ceiling is `ModeLive`.
3. **Given** a strategy with `Mode="paper"` and the process `MODE=dryrun`, **when** a cycle runs, **then**
   the effective `Context.Mode` is `ModeDryRun` (capped down) and the cycle executes normally at the
   `dryrun` tier — it is not refused.
4. **Given** a strategy with `Mode="live"` and the process `MODE=paper`, **when** a cycle runs, **then**
   the cycle is refused outright (not downgraded to `paper`), recorded as
   `StrategyExecution{Status: Error}`, and a `strategy.error` webhook fires identifying the mismatch.
5. **Given** all existing pre-migration `entities.Strategy` rows (no `Mode` value previously existed),
   **when** the migration runs, **then** every row's `Mode` is explicitly set to `"dryrun"` — never left
   `NULL` and never defaulted to `"live"`.
6. **Given** `configuration.Configuration.Testnet == false` and the effective mode resolves to `ModePaper`
   for some strategy, **when** the worker attempts to build that strategy's execution context, **then**
   this is treated as a startup/config validation failure (fail fast, matching Spec 01's pattern), not a
   silent execution against the production exchange.
7. **Edge case — unparseable `Mode` value**: **given** a `Strategy.Mode` value that doesn't match any of the
   four known strings (e.g. corrupted data, a future enum value from a newer code version read by an older
   binary), **when** `ParseExecutionMode` is called, **then** it returns an error, the cycle is refused
   (treated the same as Acceptance Criterion #4 — fail closed, not fail open to an assumed default), and a
   `strategy.error` webhook fires.
8. **Edge case — `MODE` env var unset entirely**: **given** the `MODE` environment variable is not set at
   all (e.g. a fresh `.env` before this feature was configured), **when** the worker starts, **then** it
   defaults to `ModeDryRun` (not `ModeLive`, not an error) — the safe default matches the per-strategy
   schema default from Acceptance Criterion #5, so an unconfigured process and an unconfigured strategy
   agree on the same safe tier.

## Out of Scope

- Dry-Run's simulated-fill engine (`app/engine/dryrun.go`) — Phase 2; this spec only defines the `Mode`
  value and gating, not what Dry-Run execution actually does once gated in.
- TUI-driven mode switching (`[r]` keybinding, `SwitchMode` use case) — Phase 3, per blueprint §4.
- Mid-cycle mode-change semantics (whether a change takes effect immediately or at the next cycle boundary)
  — explicitly listed as an open question in the blueprint (§8), not resolved by this spec; this spec only
  covers the mode as read fresh at the start of each cycle (which implicitly answers "at the next cycle
  boundary" for the read side, but doesn't address a change occurring *during* an in-flight cycle, since no
  mutation UI exists yet in Phase 1 to trigger that race in practice).

## Dependencies

- Spec 05 (Strategy Interface) — `ExecutionMode` type and `Context.Mode` field.
- Spec 01 (Exchange ACL) — `Testnet` interaction guards (Acceptance Criterion #9 there, #6 here).
- Spec 09 (Webhook Notifier) — mismatch/refusal events fire through `NotificationSender`.
- **Judgment call**: the asymmetric "refuse, don't downgrade" treatment specifically for a `live`-configured
  strategy under a lower process ceiling (Acceptance Criterion #4) is this spec's central interpretive
  decision — ADR-002 establishes the ceiling concept but doesn't specify refuse-vs-downgrade behavior at
  the boundary. Flagging for explicit sign-off, since the alternative (silent downgrade, matching the
  general rule) is simpler to implement and was seriously considered.
