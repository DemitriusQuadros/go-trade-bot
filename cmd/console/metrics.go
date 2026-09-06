package main

import (
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var renderLoopHealthyGauge = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "console_render_loop_healthy",
	Help: "1 if the TUI's render/event loop is actively processing, 0 if stalled.",
})

func init() {
	prometheus.MustRegister(renderLoopHealthyGauge)
}

// SetRenderLoopHealthy sets the render loop health gauge.
func SetRenderLoopHealthy(val float64) {
	renderLoopHealthyGauge.Set(val)
}

// StartDebugMetricsServer mounts /metrics on port if enabled is true.
func StartDebugMetricsServer(enabled bool, port string) {
	if !enabled {
		return
	}
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			log.Printf("[console] debug metrics server stopped: %v", err)
		}
	}()
}
