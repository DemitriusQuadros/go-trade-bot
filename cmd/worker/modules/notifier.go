package modules

import (
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
)

// MetricAgentBridgeFailures counts trade events the worker -> agents bridge
// could not enqueue (agents-platform C-01 §2.1), by reason.
const MetricAgentBridgeFailures = "agent_bridge_failures_total"

// AgentBridgeMetrics registers the bridge's failure counter.
var AgentBridgeMetrics = []metrics.MetricConfig{
	{
		Name:       MetricAgentBridgeFailures,
		Help:       "Trade events the worker could not forward to cmd/agent's agents queue (agent:event), by reason. Trading is never affected.",
		Type:       metrics.Counter,
		LabelNames: []string{"reason"},
	},
}

// NotifierModule provides the NotificationSender ACL (Spec 09). An empty
// WebhookURL yields a WebhookNotifier whose Send is a safe no-op - webhook
// configuration is optional for basic live trading to work.
//
// Spec backend-05 (ADR-016): wrapped in *notifier.SwappableNotifier so
// WebhookURL changes hot-swap immediately with no drain guard (it's a
// safe-tier field - worst case of a mid-flight swap is one notification
// using the old URL). fx also provides the concrete *notifier.SwappableNotifier
// type directly so the settings usecase can be injected with it.
//
// Agents platform C-01 §2.1: the NotificationSender every trading call site
// receives is a MultiNotifier - the SwappableNotifier first (its result is
// the only one callers see, exactly as before), then the fire-and-forget
// AgentEventBridge that enqueues agent:event for cmd/agent. The bridge is
// never swapped by Swap, never blocks on Redis and never returns an error, so
// a worker whose Redis is down keeps trading and keeps sending webhooks.
var NotifierModule = fx.Module("notifier",
	fx.Provide(
		notifier.NewSwappableNotifier,
		NewAgentEventBridge,
		NewTradeEventSender,
	),
)

// NewAgentEventBridge builds the bridge over its own asynq client.
func NewAgentEventBridge(cfg *configuration.Configuration, collector *metrics.MetricsCollector) *notifier.AgentEventBridge {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})
	return notifier.NewAgentEventBridge(client, func(reason string) {
		if collector != nil {
			collector.IncrementCounter(MetricAgentBridgeFailures, map[string]string{"reason": reason})
		}
	})
}

// NewTradeEventSender is the worker's NotificationSender: webhook first,
// agents bridge second.
func NewTradeEventSender(s *notifier.SwappableNotifier, bridge *notifier.AgentEventBridge) notifier.NotificationSender {
	return notifier.NewMultiNotifier(s, bridge)
}
