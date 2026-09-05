# Spec 11 — Observability Phase 1 (Worker `/metrics` Fix + New Metrics)

## Overview

The blueprint's audit surfaced a pre-existing bug independent of the PRD: the worker never mounts a
`/metrics` HTTP handler, so every worker-side Prometheus metric registered today has been invisible despite
`prometheus.yml` scraping that exact target since inception. This spec fixes that gap and adds the new
Phase 1 metrics (`order_execution_duration_seconds`, `order_execution_errors_total`,
`websocket_reconnects_total`, `websocket_connected`) that Specs 02/04 depend on for their metric-related
acceptance criteria.

## Current Behavior (verified)

- `cmd/worker/main.go:25` — `var Registry = prometheus.NewRegistry()` is declared at package scope and
  **never referenced anywhere else in the file or package** (confirmed by search) — a dead variable that
  creates a second, unused Prometheus registry alongside the default global one that
  `internal/metrics/collector.go:59` actually registers into (`prometheus.MustRegister`, which registers
  into `prometheus.DefaultRegisterer`).
- `cmd/worker/main.go:79-97` (`StartMetricsServer`) — constructs an `asynqmon.New(...)` handler and mounts
  it via `r.PathPrefix(h.RootPath()).Handler(h)` on a `mux.Router` served at `:9191`. **No
  `promhttp.Handler()` is ever registered on this router or any other.** The `asynqmon.Options{
  PrometheusAddress: cfg.Prometheus.Address }` field configures Asynqmon to **read** metrics from
  Prometheus for its own dashboard graphs — it has no effect on whether the worker **exposes** metrics for
  Prometheus to scrape.
- `cmd/api/main.go:93` (referenced from blueprint audit) — **does** mount `promhttp.Handler()` correctly;
  this is the one process that already works, confirmed by contrast.
- `internal/metrics/collector.go:28-63` (`NewMetricsCollector`) — generic `MetricConfig`-driven
  Counter/Gauge/Histogram/Summary registration; already registers whatever configs are passed to it into
  the default registry, structurally correct and reusable as-is for the new Phase 1 metrics.
- `cmd/worker/modules/metrics.go` (referenced in blueprint, not separately reviewed line-by-line here) —
  the existing site where `MetricConfig`s like `total_strategy_task` are declared and passed into
  `NewMetricsCollector`.
- `internal/middleware/async_middleware.go:12-15,28-34` — registers/observes
  `asyn_total_task_execution` (counter, label `task`) and `asynq_total_task_duration` (histogram, label
  `task`) on every asynq task processed — these metrics exist in the collector's map but, per the gap
  above, have never been scraped successfully.
- `docs/grafana/status_page.json` — the only Grafana dashboard, 100% business metrics; confirmed to contain
  zero panels referencing `up`, `go_goroutines`, or any process-health signal.
- `prometheus.yml` — two scrape jobs (`go-app`, `go-worker`), no `rule_files`, no `alerting` block.

## Target Behavior

### `/metrics` mount fix

```go
// cmd/worker/main.go — StartMetricsServer, modified
func StartMetricsServer(cfg *config.Configuration) {
    h := asynqmon.New(asynqmon.Options{
        RootPath:          "/tasks/monitoring",
        RedisConnOpt:      asynq.RedisClientOpt{Addr: cfg.Redis.Addr},
        PrometheusAddress: cfg.Prometheus.Address,
    })

    r := mux.NewRouter()
    r.PathPrefix(h.RootPath()).Handler(h)
    r.Handle("/metrics", promhttp.Handler()) // NEW — the fix

    srv := &http.Server{Handler: r, Addr: ":9191"}
    go func() { srv.ListenAndServe() }()
}
```

The dead `var Registry = prometheus.NewRegistry()` at line 25 is **deleted** — `promhttp.Handler()` with no
argument serves `prometheus.DefaultGatherer`, matching what `internal/metrics/collector.go` already
registers into; no second registry is introduced, preserving ADR-006's "one registry" principle.

### New `MetricConfig` registrations (`cmd/worker/modules/metrics.go`)

```go
var Phase1Metrics = []metrics.MetricConfig{
    {
        Name: "order_execution_duration_seconds",
        Help: "Time to complete a PlaceOrder call, from request to response.",
        Type: metrics.Histogram,
        LabelNames: []string{"strategy", "side"}, // side: "buy" | "sell" | "stop_market"
        Buckets: prometheus.DefBuckets, // default buckets acceptable for Phase 1; revisit if order latency clusters outside them
    },
    {
        Name: "order_execution_errors_total",
        Help: "Count of PlaceOrder/CancelOrder calls that returned an error or rejection.",
        Type: metrics.Counter,
        LabelNames: []string{"strategy", "reason"}, // reason: "rejected" | "network_error" | "insufficient_balance" | "filter_violation"
    },
    {
        Name: "websocket_reconnects_total",
        Help: "Count of LiveFeed WebSocket reconnect attempts.",
        Type: metrics.Counter,
        LabelNames: []string{"symbol"},
    },
    {
        Name: "websocket_connected",
        Help: "1 if the LiveFeed WebSocket for this symbol is currently connected, 0 otherwise.",
        Type: metrics.Gauge,
        LabelNames: []string{"symbol"},
    },
}
```

### What triggers each metric update

| Metric | Update site | Trigger | Label values |
|---|---|---|---|
| `order_execution_duration_seconds` | `app/usecase/signal/usecase.go` (`GenerateBuySignal`/`GenerateSellSignal`, Spec 02) | Wraps every `Exchange.PlaceOrder` call with a `time.Now()`/`time.Since` pair, observed **regardless of success or failure** (both a successful fill and a rejection took real wall-clock time worth measuring). | `strategy`: `entities.Strategy.Name`; `side`: `"buy"`, `"sell"`, or `"stop_market"` (Spec 03's stop submission is also timed under this metric, not a separate one). |
| `order_execution_errors_total` | Same call sites, `else` branch (error/rejection returned). | Incremented once per failed/rejected `PlaceOrder`/`CancelOrder` call. | `strategy`: strategy name; `reason`: derived from the `exchange.OrderResult.Status` or the error type (`network_error` for a transport-level error where no `OrderResult` was returned at all). |
| `websocket_reconnects_total` | `internal/feed/live_feed.go` (Spec 04) | Incremented on every reconnect **attempt** (not only successful ones) — a symbol stuck in a reconnect loop should show a climbing counter even before it succeeds. | `symbol` |
| `websocket_connected` | `internal/feed/live_feed.go` (Spec 04) | Set to `0` the instant a disconnect is detected; set to `1` only after the first candle is received post-reconnect. | `symbol` |

## Acceptance Criteria

1. **Given** the worker process is running with the fix applied, **when** `GET http://localhost:9191/metrics`
   is requested, **then** it returns a `200` with Prometheus exposition-format text including
   `total_strategy_task`, `asyn_total_task_execution`, `asynq_total_task_duration` (the three metrics that
   were already registered but never reachable) alongside standard Go/process collectors
   (`go_goroutines`, `process_resident_memory_bytes`) that come for free with the default registry.
2. **Given** the fix is applied, **when** `GET http://localhost:9191/tasks/monitoring` is requested,
   **then** the Asynqmon UI still responds normally — the fix is additive, it does not remove or break the
   existing route.
3. **Given** `prometheus.yml`'s existing `go-worker` scrape job (unchanged config, per blueprint §4 Phase 1
   note "none required beyond existing scrape jobs once worker /metrics is fixed"), **when** Prometheus
   next scrapes after the fix deploys, **then** the scrape succeeds (`up{job="go-worker"} == 1`) where it
   was previously scraping an endpoint with no metrics exposition (Asynqmon UI HTML is not Prometheus
   exposition format, so the prior scrapes were effectively failing/empty even if not returning a hard
   error).
4. **Given** a successful `PlaceOrder` call taking 340ms, **when** `GenerateBuySignal` completes, **then**
   `order_execution_duration_seconds{strategy="<name>", side="buy"}` records an observation of
   approximately `0.34`.
5. **Given** a `PlaceOrder` call that returns `OrderStatusRejected`, **when** the error path in
   `GenerateBuySignal` runs, **then** both `order_execution_duration_seconds` (observed, since time still
   elapsed) **and** `order_execution_errors_total{strategy="<name>", reason="rejected"}` (incremented) fire.
6. **Given** `LiveFeed` (Spec 04) experiences 3 consecutive reconnect attempts before succeeding, **when**
   the metrics are scraped mid-reconnect-loop, **then** `websocket_reconnects_total{symbol="<sym>"}` reads
   `3` and `websocket_connected{symbol="<sym>"}` reads `0`; after the 4th attempt succeeds and a candle is
   received, `websocket_connected` reads `1`.
7. **Edge case — metric name typo/registration-order bug**: **given** a metric is referenced by
   `collector.IncrementCounter`/`ObserveHistogram`/`SetGauge` (Spec 04's `LiveFeed`, Spec 02's order-latency
   code) **before** its `MetricConfig` has been added to the slice passed into `NewMetricsCollector`,
   **when** the call happens, **then** it silently no-ops (per `internal/metrics/collector.go:65-81`'s
   map-lookup-with-`ok` pattern — no panic, no error return) — this spec's Acceptance Criteria above are
   therefore **only verifiable if the `MetricConfig` registration lands in the same PR/commit as the
   metric-emitting code**; a test asserting the metric is non-zero after a triggering action is the
   practical way to catch a silent registration-order mistake, since the code won't fail loudly on its own.
8. **Edge case — dead `Registry` variable removal doesn't break anything**: **given** `var Registry =
   prometheus.NewRegistry()` is deleted from `cmd/worker/main.go:25`, **when** the package is compiled,
   **then** the build succeeds with no other reference to `Registry` anywhere in the codebase (confirmed by
   the current-behavior search already performed — this is a pure dead-code removal with zero call sites).

## Out of Scope

- `feed_candle_delay_seconds`, `candle_import_lag_seconds`, `backtest_run_duration_seconds` — Phase 2
  metrics per blueprint §7.2/§4.
- `strategy_panics_total` — Phase 3 per blueprint §4.
- `docs/grafana/process_health.json` dashboard creation, Alertmanager wiring, `alerting/rules.yml` — Phase 2
  per blueprint §7.3/§7.4; Phase 1 only needs the metrics to **exist and be scrapeable**, not yet visualized
  or alerted on.
- Console (`cmd/console`) `/metrics` endpoint — Phase 3 per blueprint §4.
- Asynq queue-depth/retry/dead-letter-count metrics (blueprint §7.1 notes these are likely never actually
  exported today despite Asynqmon's own UI implying they might be) — not in the Phase 1 metric list above;
  flagged in the blueprint as a gap but not scoped to this spec's acceptance criteria. If queue-depth
  visibility is wanted sooner, it should be added as an explicit Phase 1 line item, not assumed bundled here.

## Dependencies

- Spec 02 (Order Execution) — emits `order_execution_duration_seconds`/`order_execution_errors_total`.
- Spec 04 (WebSocket Live Feed) — emits `websocket_reconnects_total`/`websocket_connected`.
- None of Spec 11's `/metrics`-mount-fix work depends on any other Phase 1 spec — it can be implemented and
  merged first, independently, since it's a bug fix to existing (already-shipped) code.
