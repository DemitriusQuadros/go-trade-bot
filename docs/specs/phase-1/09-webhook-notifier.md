# Spec 09 — Generic Webhook Notifier (`internal/notifier`)

## Overview

No notification mechanism of any kind exists today — trade events are only visible via `log.Printf` and the
Prometheus/Grafana stack (which, per Spec 11, has its own pre-existing gaps). This spec defines
`NotificationSender`, the ACL for outbound trade-event notifications, its webhook-POST implementation, and
the exact payload schema per event type, so downstream specs (02, 03, 05) that already reference "fires a
`strategy.error` webhook" have a concrete contract to call into.

## Current Behavior (verified)

- No `internal/notifier` package, no webhook-POST code, no `WebhookURL` config field exists anywhere.
- `internal/configuration/configuration.go:10-36` — `Configuration` struct has no notification-related
  field at all.
- Error/event visibility today is entirely `log.Printf` calls scattered through
  `app/handler/tasks/strategy/handler.go` and each `algorithm.go`'s `Execute`/`RunXAlgorithm` methods —
  none of these reach any external system.

## Target Behavior

```go
// internal/notifier/interface.go
package notifier

import "context"

type EventType string
const (
    EventPositionOpened EventType = "position.opened"
    EventPositionClosed EventType = "position.closed"
    EventDrawdownAlert  EventType = "drawdown.alert"
    EventStrategyError  EventType = "strategy.error"
)

type Event struct {
    Type       EventType         `json:"type"`
    Timestamp  time.Time         `json:"timestamp"`
    StrategyID uint              `json:"strategy_id"`
    Strategy   string            `json:"strategy_name"`
    Symbol     string            `json:"symbol,omitempty"`
    Mode       string            `json:"mode"` // ExecutionMode.String(), Spec 05/10
    Message    string            `json:"message"`
    Data       map[string]any    `json:"data,omitempty"` // event-specific payload, see below
}

type NotificationSender interface {
    Send(ctx context.Context, event Event) error
}
```

`internal/notifier/webhook_notifier.go` implements `NotificationSender` as a single configurable HTTP POST
target (`configuration.Configuration.WebhookURL`), sending `Event` as a JSON body with
`Content-Type: application/json`.

### Payload `Data` shape per event type

| `Type` | `Data` fields |
|---|---|
| `position.opened` | `entry_price float64`, `quantity float64`, `broker_order_id string`, `stop_loss_price float64` |
| `position.closed` | `entry_price float64`, `exit_price float64`, `quantity float64`, `profit float64`, `exit_reason string` (`"take_profit"` \| `"stop_loss"` \| `"manual"` \| `"band_cross"` — matches Spec 08's per-strategy exit conditions) |
| `drawdown.alert` | `current_drawdown_pct float64`, `threshold_pct float64` — **Phase 1 note**: no drawdown-tracking logic exists yet (that's part of Phase 2's metrics engine); this event type is defined on the frozen interface now but Phase 1 has **no emitter** for it — see Out of Scope. |
| `strategy.error` | `error string`, `context string` (free-form — e.g. `"unprotected position detected on restart"` per Spec 03, `"strategy not found in registry"` per Spec 06) |

### Delivery guarantees

**Judgment call**: the PRD only specifies "POST a webhook... so that I can route notifications through
n8n" — it does not specify delivery guarantees. This spec adopts **fire-and-forget, asynchronous, at-most
one retry**:
- `Send` is called from a **non-blocking path** — the caller (e.g. `GenerateBuySignal` after a fill) does
  not wait for the HTTP round-trip to complete before returning; `WebhookNotifier` internally dispatches
  via a bounded worker goroutine (or a simple `go func() {...}()` with panic recovery) so a slow/unreachable
  webhook endpoint never delays the trading loop.
- On HTTP failure (non-2xx response or network error), **one retry** after a fixed 2-second delay; if the
  retry also fails, the event is logged (`log.Printf`) and dropped — no persistent outbox/queue in Phase 1.
  Rationale: webhook delivery is an observability aid, not a transactional requirement — the `Signal`/
  `Order`/`StrategyExecution` DB rows remain the durable record of what happened regardless of whether the
  webhook reached n8n.

## Acceptance Criteria

1. **Given** `configuration.Configuration.WebhookURL` is set, **when** `GenerateBuySignal` completes a fill
   (Spec 02), **then** `NotificationSender.Send` is called with `Type: EventPositionOpened` and `Data`
   populated from the actual fill (`entry_price`, `quantity`, `broker_order_id`) — not the strategy's
   pre-trade estimate.
2. **Given** the webhook endpoint returns HTTP `500` on the first attempt and `200` on the retry, **when**
   `Send` is called, **then** the event is considered delivered after the retry succeeds, and no further
   retries occur.
3. **Given** the webhook endpoint is unreachable for both the initial attempt and the retry, **when**
   `Send` is called, **then** the calling code path (e.g. `GenerateBuySignal`) is **not** blocked waiting
   for either attempt to resolve, and completes its own return value based purely on the trading operation's
   own success/failure — a webhook failure never causes a trading operation to be reported as failed.
4. **Given** `configuration.Configuration.WebhookURL` is empty/unset, **when** any code path calls
   `NotificationSender.Send`, **then** the call is a safe no-op (does not attempt an HTTP request to an
   empty URL, does not error) — this matters because Phase 1's success signal doesn't require webhook
   configuration to be mandatory for basic live trading to work.
5. **Given** a `strategy.error` event fires for a strategy-not-found condition (Spec 06), **when** the
   payload is inspected, **then** `Data.context` contains the specific diagnostic string
   `"strategy not found in registry"`, not a generic error message — payload specificity is required for
   the event to be actionable in n8n routing.
6. **Edge case — event during Backtest mode**: **given** `Context.Mode == ModeBacktest` (Spec 05/10),
   **when** a strategy would normally trigger a `position.opened` event, **then** **no webhook fires** —
   backtests run offline against historical data and must not spam a live notification channel; this
   applies to all four event types when `Mode == ModeBacktest` (Phase 1 has no backtest engine yet, so this
   criterion is forward-looking / a contract Phase 2's engine must honor, but the `NotificationSender`
   interface and its callers should be written now assuming this guard exists at the call site, not inside
   `WebhookNotifier` itself, since the notifier has no concept of `Mode`).
7. **Edge case — malformed webhook URL**: **given** `WebhookURL` is set to a syntactically invalid URL
   (e.g. missing scheme), **when** the worker starts, **then** this is caught as a configuration validation
   error at startup (fail fast, matching Spec 01 Acceptance Criterion #8's pattern), not discovered only
   when the first `Send` call silently fails at runtime.

## Out of Scope

- `drawdown.alert` emission logic — no drawdown tracking exists until Phase 2's metrics engine; the event
  type and payload shape are defined now (frozen interface) but nothing calls `Send` with this type in
  Phase 1.
- Persistent delivery queue / guaranteed-delivery semantics (e.g. an outbox table, retry-with-backoff
  beyond one retry) — explicitly deferred; Phase 1 accepts best-effort delivery.
- Alertmanager integration (blueprint §7.3, Phase 2) — that's Prometheus-alert-to-webhook, a distinct path
  from trade-event-to-webhook, though both are designed to converge on the same n8n endpoint eventually.

## Dependencies

- Spec 02 (Order Execution) — `position.opened`/`position.closed` events fire from within
  `GenerateBuySignal`/`GenerateSellSignal`.
- Spec 03 (Stop-Loss) — `strategy.error` events fire from the unprotected-position and reconciliation paths.
- Spec 06 (Strategy Registry) — `strategy.error` events fire from the not-found path.
