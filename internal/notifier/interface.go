// Package notifier is the Anti-Corruption Layer for outbound trade-event
// notifications (Spec 09). WebhookNotifier is the sole implementation in
// Phase 1: a single configurable HTTP POST target.
package notifier

import (
	"context"
	"time"
)

type EventType string

const (
	EventPositionOpened EventType = "position.opened"
	EventPositionClosed EventType = "position.closed"
	EventDrawdownAlert  EventType = "drawdown.alert"
	EventStrategyError  EventType = "strategy.error"
)

// Event is the JSON payload POSTed to WebhookURL.
type Event struct {
	Type       EventType      `json:"type"`
	Timestamp  time.Time      `json:"timestamp"`
	StrategyID uint           `json:"strategy_id"`
	Strategy   string         `json:"strategy_name"`
	Symbol     string         `json:"symbol,omitempty"`
	Mode       string         `json:"mode"` // ExecutionMode.String(), Spec 05/10
	Message    string         `json:"message"`
	Data       map[string]any `json:"data,omitempty"` // event-specific payload, see docs/specs/phase-1/09
}

// NotificationSender is the ACL interface every trade-event emitter depends
// on. Callers are responsible for the Mode == ModeBacktest guard (Spec 09
// AC#6 "Edge case - event during Backtest mode") - this interface has no
// concept of Mode, so the guard lives at each call site, not here.
type NotificationSender interface {
	Send(ctx context.Context, event Event) error
}
