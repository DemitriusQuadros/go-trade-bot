package modules

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/fx"

	taskagent "go-trade-bot/app/handler/tasks/agent"
	"go-trade-bot/internal/metrics"
)

// Metric config lists mirror cmd/mcp/modules/metrics.go (the same usecase
// constructors emit them here) plus the agent runtime metrics (A-02 §1).
var Phase1Metrics = []metrics.MetricConfig{
	{
		Name:       "order_execution_duration_seconds",
		Help:       "Time to complete a PlaceOrder call, from request to response.",
		Type:       metrics.Histogram,
		LabelNames: []string{"strategy", "side"},
		Buckets:    prometheus.DefBuckets,
	},
	{
		Name:       "order_execution_errors_total",
		Help:       "Count of PlaceOrder/CancelOrder calls that returned an error or rejection.",
		Type:       metrics.Counter,
		LabelNames: []string{"strategy", "reason"},
	},
}

var Phase2Metrics = []metrics.MetricConfig{
	{
		Name:       "backtest_run_duration_seconds",
		Help:       "Wall-clock duration of a completed backtest or walk-forward run.",
		Type:       metrics.Histogram,
		LabelNames: []string{"strategy", "is_walk_forward"},
		Buckets:    []float64{1, 5, 15, 30, 60, 120, 300, 600},
	},
}

var ScriptingMetrics = []metrics.MetricConfig{
	{
		Name:       "script_runtime_errors_total",
		Help:       "Total count of Lua script runtime errors (timeout, lua_error, panic), by strategy and reason.",
		Type:       metrics.Counter,
		LabelNames: []string{"strategy", "reason"},
	},
}

var AgentRuntimeMetrics = []metrics.MetricConfig{
	{
		Name:       taskagent.MetricRunsTotal,
		Help:       "Agent runs executed by cmd/agent, by agent, trigger and final status.",
		Type:       metrics.Counter,
		LabelNames: []string{"agent", "trigger", "status"},
	},
	{
		Name:       taskagent.MetricRunDuration,
		Help:       "Wall-clock duration of an agent run.",
		Type:       metrics.Histogram,
		LabelNames: []string{"agent"},
		Buckets:    []float64{1, 5, 15, 30, 60, 120, 300, 600},
	},
	{
		Name:       taskagent.MetricCostUSDTotal,
		Help:       "Estimated model cost (USD) of agent runs executed by cmd/agent.",
		Type:       metrics.Counter,
		LabelNames: []string{"agent"},
	},
	{
		Name:       taskagent.MetricTokensTotal,
		Help:       "Model tokens consumed by agent runs executed by cmd/agent, by direction (input|output).",
		Type:       metrics.Counter,
		LabelNames: []string{"agent", "direction"},
	},
	{
		Name:       taskagent.MetricTriggersFired,
		Help:       "Agent runs started by an event, market or chain trigger (agents-platform C-01), by kind.",
		Type:       metrics.Counter,
		LabelNames: []string{"kind"},
	},
	{
		Name:       taskagent.MetricTriggersSuppressed,
		Help:       "Agent triggers suppressed by a cooldown (event, market) or a chain guard (chain: depth, repeated agent, duplicate), by kind.",
		Type:       metrics.Counter,
		LabelNames: []string{"kind"},
	},
	{
		Name:       taskagent.MetricMarketSubscriptions,
		Help:       "Symbols cmd/agent's market watcher holds a 1m kline subscription for.",
		Type:       metrics.Gauge,
		LabelNames: []string{},
	},
	{
		Name:       taskagent.MetricBlockedOrders,
		Help:       "Order placement/cancellation attempts blocked by cmd/agent's read-only exchange client (should always be 0).",
		Type:       metrics.Counter,
		LabelNames: []string{"op"},
	},
}

var MetricsModule = fx.Module("metrics",
	fx.Provide(func() *metrics.MetricsCollector {
		cfgs := []metrics.MetricConfig{}
		cfgs = append(cfgs, Phase1Metrics...)
		cfgs = append(cfgs, Phase2Metrics...)
		cfgs = append(cfgs, ScriptingMetrics...)
		cfgs = append(cfgs, AgentRuntimeMetrics...)
		return metrics.NewMetricsCollector(cfgs)
	}),
)
