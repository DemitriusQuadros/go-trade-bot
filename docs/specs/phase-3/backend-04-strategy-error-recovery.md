# Spec backend-04 — Strategy Error Recovery (`strategy_panics_total`)

## Overview

Blueprint §4 Phase 3 asks for "panic recovery per strategy cycle + webhook alert on recovery... increments
new `strategy_panics_total` counter." This spec's Current Behavior audit finds that Phase 1 **already
shipped panic recovery and webhook alerting** inside `Engine.Run` — the PRD's "restart on panic, webhook
alert" ask is functionally satisfied today except for one missing piece: nothing distinguishes a **panic**
from an ordinary returned error in metrics or in the `StrategyExecution`/webhook record. This spec's actual
scope is narrower than the blueprint's phrasing implies: add the distinguishing counter and message marker,
not build recovery from scratch.

## Current Behavior (verified)

- `app/engine/engine.go:83-89` (`Run`) **already** wraps the entire hook sequence in a `defer func() { if r
  := recover(); r != nil { ... } }()` block, converting any panic into a returned `error` and calling
  `e.notifyError` (a `strategy.error` webhook) — this is real, already-shipped panic recovery with webhook
  alerting, not a gap.
- `app/handler/tasks/strategy/handler.go:100-124` (`HandleStrategyTask`) calls `p.worker.EnqueueStrategyTask(nStrategy)`
  **unconditionally** (line 110, before the `if err != nil` branch), regardless of whether `processStrategy`
  returned an error — meaning a panicked cycle **already** doesn't halt the strategy's future execution; the
  next cycle is enqueued exactly as if the cycle had succeeded. The PRD's "restart on panic" is, in effect,
  already satisfied by this unconditional re-enqueue — there is no separate "restart" mechanism needed
  because the task queue was never designed to stop on a single cycle's failure.
- **What's actually missing**: `e.notifyError` (`engine.go:273-287`) and the `StrategyExecution` row
  written by `HandleStrategyTask` (`handler.go:112-124`) treat a recovered panic **identically** to an
  ordinary returned error from a hook (e.g. `GoLong` returning `Signal{}` with no `Buy`, per Phase 1 Spec
  05 AC#4) — both produce the same `Status: Error` / generic error-string message shape. There is no
  `strategy_panics_total` metric anywhere (confirmed: not present in `internal/metrics/collector.go`'s
  registrations nor referenced in `engine.go`), and no way to distinguish "a strategy hook returned an
  expected error condition" from "a strategy hook crashed" when looking at Grafana or the
  `StrategyExecution` history — this distinction matters operationally (a rising panic rate signals a code
  bug needing a fix; a rising ordinary-error rate might just mean a strategy's config needs adjustment).

## Target Behavior

```go
// app/engine/engine.go — Run's recover() block, modified
const metricStrategyPanics = "strategy_panics_total"

func (e *Engine) Run(ctx context.Context, strategy strategies.Strategy, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("strategy %s panicked processing %s: %v", dbStrategy.Name, symbol, r)
            e.incrementPanicCounter(dbStrategy.Name)                 // NEW
            e.notifyError(dbStrategy, symbol, mode, err.Error())     // unchanged call, but see payload note below
        }
    }()
    // ...unchanged hook sequence...
}

func (e *Engine) incrementPanicCounter(strategyName string) {
    if e.Metrics == nil { // NEW field on Engine, mirrors SignalUseCase's existing *metrics.MetricsCollector field pattern
        return
    }
    e.Metrics.IncrementCounter(metricStrategyPanics, map[string]string{"strategy": strategyName})
}
```

`e.notifyError`'s `Data` payload (currently `map[string]any{"error": message, "context": "engine.Run"}`,
per Phase 1's existing code) gains a `"panic": true` field specifically on this call site — every other
`notifyError`/`errorEvent` call site in the codebase (Spec 05/06's not-found/mode-mismatch cases, `usecase.go`'s
order-failure cases) continues to omit this field (implicitly `false`/absent), so a webhook consumer (n8n)
can filter on `data.panic == true` to route panic alerts differently (e.g. higher urgency) from routine
trading errors without any other payload shape changing.

`internal/metrics/collector.go`'s `MetricConfig` registration (`cmd/worker/modules/metrics.go`) gains:
```go
{
    Name:       "strategy_panics_total",
    Help:       "Count of strategy hook panics recovered by the engine, by strategy name.",
    Type:       metrics.Counter,
    LabelNames: []string{"strategy"},
}
```

`docs/grafana/process_health.json` (Phase 2 Spec 11) gains a panel for this metric — cross-referenced, not
re-specified here, since that dashboard file's ownership is Phase 2's spec.

## Acceptance Criteria

1. **Given** a strategy's `ShouldLong` hook panics (e.g. a nil-pointer dereference on an unexpectedly empty
   candle slice), **when** `Engine.Run`'s deferred recover executes, **then**
   `strategy_panics_total{strategy="<name>"}` increments by exactly 1, and the existing webhook/error-return
   behavior (Phase 1, unchanged) still fires alongside it.
2. **Given** the same panic scenario, **when** the resulting webhook payload is inspected, **then**
   `Data["panic"] == true` is present, distinguishing it from a non-panic `strategy.error` event (e.g. the
   "strategy not found in registry" case, Phase 1 Spec 06) which has no `panic` key at all.
3. **Given** a strategy hook returns an ordinary error condition without panicking (e.g. `GoLong` returning
   `Signal{}` with no `Buy`, Phase 1 Spec 05 AC#4), **when** this occurs, **then**
   `strategy_panics_total` does **not** increment — only an actual `recover()` catch increments it, never a
   normal error-return path, preserving the operational distinction this spec exists to create.
4. **Given** a strategy panics on cycle N, **when** `HandleStrategyTask` completes processing cycle N,
   **then** the strategy is still re-enqueued for cycle N+1 (Phase 1's existing unconditional re-enqueue,
   `handler.go:110`, unmodified) — confirming this spec does not need to add any "restart" logic, since
   continuity already exists.
5. **Given** 3 consecutive cycles panic for the same strategy, **when** `strategy_panics_total` is queried,
   **then** it reads `3` for that strategy's label, monotonically increasing — no reset/decay logic is
   introduced (standard Prometheus counter semantics; a rate-based alert, per Phase 2 Spec 11's alerting
   rules, is the mechanism for detecting "panicking repeatedly," not the counter itself).
6. **Edge case — panic during `Terminate`**: **given** a strategy's `Terminate` hook itself panics (called
   on the `Disabled` transition, Phase 1 Spec 05 AC#8, invoked from `terminateIfRegistered` in
   `handler.go`, **not** from inside `Engine.Run`'s recover-wrapped scope), **when** this occurs, **then**
   this spec's recovery/counter mechanism does **not** cover it — `terminateIfRegistered`
   (`handler.go:198-207`) has no panic recovery of its own today, and adding it is flagged here as a small
   gap this spec does not close (out of scope, see below), since the blueprint's Phase 3 line item names
   only "per strategy cycle," which `Engine.Run` represents, not the separate `Terminate`-on-disable path.

## Out of Scope

- Adding panic recovery to `terminateIfRegistered`'s `Terminate` call (Acceptance Criterion #6) — flagged
  as a real, small gap, but not in the blueprint's literal Phase 3 scope ("per strategy cycle"); worth a
  follow-up ticket, not blocking this spec.
- Circuit-breaking (automatically disabling a strategy after N consecutive panics) — not requested by the
  PRD or blueprint; "restart on panic" (already satisfied, per Current Behavior) is the full extent of the
  automated-recovery ask. A human-driven response to a `strategy_panics_total` alert (Phase 2 Spec 11's
  alerting rules) is the intended operational loop, not fully automated remediation.
- Retry with backoff for a panicking cycle — the existing fixed-cycle-interval re-enqueue (unmodified) is
  the retry mechanism; no additional backoff is introduced for panics specifically.

## Dependencies

- Phase 1's `app/engine/engine.go` (already has the recover block this spec extends),
  `app/handler/tasks/strategy/handler.go` (already has the unconditional re-enqueue this spec relies on,
  unmodified), `internal/metrics/collector.go` (reused, generic `MetricConfig` pattern).
- Phase 2's `docs/specs/phase-2/11-observability-phase2.md` (`process_health.json` dashboard — this spec's
  new metric is a panel addition there, not re-specified in this file).
