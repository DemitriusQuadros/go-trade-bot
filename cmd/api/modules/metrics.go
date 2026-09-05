package modules

import (
	"go.uber.org/fx"

	"github.com/prometheus/client_golang/prometheus"

	"go-trade-bot/internal/metrics"
)

var MetricsModule = fx.Module("metrics",
	fx.Provide(func() *metrics.MetricsCollector {
		return metrics.NewMetricsCollector([]metrics.MetricConfig{
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
		})
	}),
)
