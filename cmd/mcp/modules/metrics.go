package modules

import (
	"go.uber.org/fx"

	"github.com/prometheus/client_golang/prometheus"

	"go-trade-bot/internal/metrics"
)

// Metric config lists mirror cmd/worker/modules/metrics.go's - cmd/mcp
// reuses the exact same usecase constructors (SignalUseCase, BacktestUseCase,
// the script Runner) those metrics are emitted from, so the same
// MetricConfig set must be registered here or the calls silently no-op.
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

var MetricsModule = fx.Module("metrics",
	fx.Provide(func() *metrics.MetricsCollector {
		cfgs := []metrics.MetricConfig{}
		cfgs = append(cfgs, Phase1Metrics...)
		cfgs = append(cfgs, Phase2Metrics...)
		cfgs = append(cfgs, ScriptingMetrics...)
		return metrics.NewMetricsCollector(cfgs)
	}),
)
