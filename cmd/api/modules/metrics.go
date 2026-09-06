package modules

import (
	"go.uber.org/fx"

	"github.com/prometheus/client_golang/prometheus"

	"go-trade-bot/internal/metrics"
)

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
			// order_execution_* are also needed here (not just in
			// cmd/worker): POST /signal/close/{id} calls
			// SignalUseCase.GenerateSellSignal, which places a real order
			// (Spec 02) and records these metrics (Spec 11).
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
		cfgs = append(cfgs, Phase2Metrics...)
		cfgs = append(cfgs, Phase3Metrics...)
		return metrics.NewMetricsCollector(cfgs)
	}),
)
