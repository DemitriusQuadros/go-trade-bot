package modules

import (
	"go.uber.org/fx"

	"github.com/prometheus/client_golang/prometheus"

	"go-trade-bot/internal/metrics"
)

// Phase1Metrics are the new Spec 02/04/11 metrics: order execution latency
// and errors (emitted from app/usecase/signal, Spec 02/03), and WebSocket
// connectivity (emitted from internal/feed.LiveFeed, Spec 04). Per Spec 11's
// AC#7, these MetricConfigs must land in the same commit as the
// metric-emitting code, since an unregistered metric name silently no-ops
// rather than failing loudly.
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
	{
		Name:       "websocket_reconnects_total",
		Help:       "Count of LiveFeed WebSocket reconnect attempts.",
		Type:       metrics.Counter,
		LabelNames: []string{"symbol"},
	},
	{
		Name:       "websocket_connected",
		Help:       "1 if the LiveFeed WebSocket for this symbol is currently connected, 0 otherwise.",
		Type:       metrics.Gauge,
		LabelNames: []string{"symbol"},
	},
}

var Phase2Metrics = []metrics.MetricConfig{
	{
		Name:       "feed_candle_delay_seconds",
		Help:       "Time between a candle's OpenTime and when the engine/driver processed it - the observable version of Feed/backtest parity risk.",
		Type:       metrics.Gauge,
		LabelNames: []string{"symbol", "feed_type"}, // feed_type: "live" | "replay" | "dryrun"
	},
	{
		Name:       "candle_import_lag_seconds",
		Help:       "Time between the most recently stored candle's OpenTime and now, per (symbol, timeframe).",
		Type:       metrics.Gauge,
		LabelNames: []string{"symbol", "timeframe"},
	},
	{
		Name:       "backtest_run_duration_seconds",
		Help:       "Wall-clock duration of a completed backtest or walk-forward run.",
		Type:       metrics.Histogram,
		LabelNames: []string{"strategy", "is_walk_forward"},
		Buckets:    []float64{1, 5, 15, 30, 60, 120, 300, 600},
	},
}

var Phase3Metrics = []metrics.MetricConfig{
	{
		Name:       "strategy_panics_total",
		Help:       "Total count of strategy panics recovered by the engine.",
		Type:       metrics.Counter,
		LabelNames: []string{"strategy", "symbol"},
	},
}

// ScriptingMetrics are the strategy-scripting initiative's own metrics
// (docs/specs/strategy-scripting/backend-01-lua-runtime-sandbox.md). Emitted
// from app/strategies/script's Runner/CallHook.
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
		cfgs := []metrics.MetricConfig{
			{
				Name:       "http_requests_total",
				Help:       "Total of http requets received",
				Type:       metrics.Counter,
				LabelNames: []string{"path", "method"},
			},
			{
				Name:       "http_request_duration_seconds",
				Help:       "HTTP Requests duration",
				Type:       metrics.Histogram,
				LabelNames: []string{"path"},
				Buckets:    []float64{0.1, 0.3, 1.2, 5.0},
			},
			{
				Name:       "total_strategy_task",
				Help:       "Total of strategy tasks executed",
				Type:       metrics.Counter,
				LabelNames: []string{"strategy"},
			},
			// asyn_total_task_execution / asynq_total_task_duration (Spec 11
			// AC#1): emitted by internal/middleware.AsynqConfigMiddleware on
			// every asynq task since before this session's changes, but never
			// had a MetricConfig entry - per the collector's map-lookup-with-ok
			// pattern this meant both calls silently no-op'd. The "asyn" (not
			// "asynq") spelling in the first name is intentional, matching the
			// pre-existing constant in internal/middleware/async_middleware.go.
			{
				Name:       "asyn_total_task_execution",
				Help:       "Total of asynq tasks processed by the worker.",
				Type:       metrics.Counter,
				LabelNames: []string{"task"},
			},
			{
				Name:       "asynq_total_task_duration",
				Help:       "Duration of asynq task processing.",
				Type:       metrics.Histogram,
				LabelNames: []string{"task"},
				Buckets:    prometheus.DefBuckets,
			},
		}
		cfgs = append(cfgs, Phase1Metrics...)
		cfgs = append(cfgs, Phase2Metrics...)
		cfgs = append(cfgs, Phase3Metrics...)
		cfgs = append(cfgs, ScriptingMetrics...)
		return metrics.NewMetricsCollector(cfgs)
	}),
)
