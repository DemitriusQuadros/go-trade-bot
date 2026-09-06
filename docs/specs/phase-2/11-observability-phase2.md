# Spec 11 — Observability Phase 2 (Alertmanager, `process_health.json`, Feed/Backtest/Import Metrics)

## Overview

Phase 1 fixed the worker's `/metrics` mount gap and added order/WebSocket metrics; Phase 1 also left the
alerting path entirely unbuilt (blueprint §7.1: "there's nowhere for an alert to be evaluated or routed").
This spec adds the Phase 2 observability line items from blueprint §7.3/§4: an Alertmanager service,
`alerting/rules.yml`, a separate `docs/grafana/process_health.json` dashboard (per ADR-006), and the three
new metrics (`feed_candle_delay_seconds`, `candle_import_lag_seconds`, `backtest_run_duration_seconds`) —
specifying exactly what emits each and from where.

## Current Behavior (verified)

- `docker-compose.yml` has `prometheus`/`grafana` services but no `alertmanager` service (consistent with
  blueprint §7.1's "no Alertmanager service" finding — re-verified as still true; no changes to
  `docker-compose.yml` occurred during Phase 1's implementation).
- `prometheus.yml` (Phase 1, current) has the two scrape jobs (`go-app`, `go-worker`, now actually
  functional per Phase 1's `/metrics` fix) but still no `rule_files` or `alerting` block.
- `docs/grafana/` contains only `status_page.json` — no `process_health.json` yet (Phase 1's blueprint
  scoped this file to "Phase 1-3" in the directory tree annotation, but Phase 1's actual delivered scope,
  per the Phase 1 spec set, only covered the `/metrics` mount fix and new metric *registrations* —
  dashboard-file creation was correctly deferred, confirmed by its absence).
- `internal/metrics/collector.go` (Phase 1, unchanged) — the generic `MetricConfig`-driven wrapper is
  reused as-is for every new Phase 2 metric below, exactly as Phase 1's Spec 11 established.
- `app/engine/engine.go` and `app/usecase/signal/usecase.go` (Phase 1, current) have no
  `feed_candle_delay_seconds`, `candle_import_lag_seconds`, or `backtest_run_duration_seconds` emission
  anywhere — these are wholly new to Phase 2, as expected.

## Target Behavior

### New metrics

```go
var Phase2Metrics = []metrics.MetricConfig{
    {
        Name: "feed_candle_delay_seconds",
        Help: "Time between a candle's OpenTime and when the engine/driver processed it - the observable version of Feed/backtest parity risk.",
        Type: metrics.Gauge,
        LabelNames: []string{"symbol", "feed_type"}, // feed_type: "live" | "replay" | "dryrun"
    },
    {
        Name: "candle_import_lag_seconds",
        Help: "Time between the most recently stored candle's OpenTime and now, per (symbol, timeframe).",
        Type: metrics.Gauge,
        LabelNames: []string{"symbol", "timeframe"},
    },
    {
        Name: "backtest_run_duration_seconds",
        Help: "Wall-clock duration of a completed backtest or walk-forward run.",
        Type: metrics.Histogram,
        LabelNames: []string{"strategy", "is_walk_forward"},
        Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600}, // seconds - realistic for Phase 2's single-symbol scale
    },
}
```

### What triggers each metric update

| Metric | Update site | Trigger | Notes |
|---|---|---|---|
| `feed_candle_delay_seconds` | `internal/feed.LiveFeed.Next()` (Phase 1 file, additive change) and Spec 04's `ReplayDriver` | Every time a candle is delivered: `observed_delay = time.Since(candle.OpenTime)`. | For `feed_type="replay"`, this metric measures **processing** latency (how long the driver took per candle), not real staleness — reported anyway for symmetry and because a backtest that's unexpectedly slow per-candle is itself diagnostically useful (e.g. a pathological indicator computation). For `feed_type="live"`/`"dryrun"`, this is the actual real-world data-freshness signal blueprint §6's parity risk names. |
| `candle_import_lag_seconds` | `cmd/candleimport` (one-shot, per Spec 02) **and** periodically from `cmd/worker` (a lightweight background check, since the CLI itself is one-shot and its own metric would never be scraped per blueprint §7.2's "one-shot CLIs... not long-lived enough to be usefully scraped") | `cmd/worker` runs a low-frequency (e.g. every 5 minutes) check via `CandleRepository.LatestOpenTime` per configured (symbol, timeframe) pair and sets the gauge to `time.Since(latestOpenTime)`. | This is the one metric in this spec that is **not** emitted by the process actually doing the work (`cmd/candleimport`) but by a different long-lived process (`cmd/worker`) checking the *result* of that work — necessary because Prometheus can only scrape long-lived processes (blueprint §7.2), and this spec resolves that constraint by having the worker poll the DB state the importer left behind, rather than inventing a push-gateway (out of scope, see below). |
| `backtest_run_duration_seconds` | `app/usecase/backtest.BacktestUseCase.Run`/`RunWalkForward` (Spec 10) | Wraps the full orchestration (engine run + metrics + report + persistence) in a `time.Now()`/`time.Since` pair, observed on completion regardless of success/failure (mirrors Phase 1 Spec 11's `order_execution_duration_seconds` convention of timing both success and failure paths). | Since `cmd/backtest` (the CLI) is itself one-shot, this metric is only actually scraped when the run is triggered via `cmd/api`'s `POST /backtest` endpoint (Spec 10) — `cmd/api` is already a scraped, long-lived process (Phase 1 confirmed `/metrics` works there). A CLI-triggered run still calls the same `BacktestUseCase` code (Spec 10's shared-usecase design) and still calls `ObserveHistogram`, but that observation is lost when the one-shot process exits before any scrape occurs — documented as an accepted gap for CLI-triggered runs, consistent with blueprint §7.2's general one-shot-CLI treatment. |

### Alertmanager & rules

`docker-compose.yml` gains an `alertmanager` service (`prom/alertmanager`, same pattern as the existing
`prometheus`/`grafana` services — no new stateful volume beyond its own config). `alerting/rules.yml`:

```yaml
groups:
  - name: go-trade-bot
    rules:
      - alert: HighErrorRate
        expr: rate(order_execution_errors_total[5m]) > 0
        for: 5m
      - alert: WebSocketDisconnected
        expr: websocket_connected == 0
        for: 2m
      - alert: ProcessDown
        expr: up{job=~"go-app|go-worker"} == 0
        for: 1m
      - alert: FeedStale
        expr: feed_candle_delay_seconds{feed_type="live"} > 300
        for: 5m
      - alert: CandleImportStale
        expr: candle_import_lag_seconds > 172800  # 2 days
        for: 15m
```

`prometheus.yml` gains `rule_files: [alerting/rules.yml]` and an `alerting.alertmanagers` block pointing at
the new service. Alertmanager's own receiver config POSTs to the **same** `WebhookURL`
`internal/configuration.Configuration.WebhookURL` already defined in Phase 1 (Spec 09) — Prometheus alerts
and trade-event notifications converge on one path into n8n, per blueprint §7.3.

### `docs/grafana/process_health.json`

New dashboard, per ADR-006 — panels: per-process `up{job=~"go-app|go-worker"}` (console's `up` entry is
Phase 3, since `cmd/console` has no `/metrics` endpoint until then), goroutines/memory per process, asynq
queue depth (flagged as a **known gap** — blueprint §7.1 notes queue-depth metrics likely don't actually
exist despite Asynqmon's UI implying they might; if true, this panel ships empty/pending until a separate
fix, not blocking this spec), WS reconnects over time, order-latency p50/p95/p99, error rate per strategy,
**new in Phase 2**: feed lag (`feed_candle_delay_seconds`), candle import staleness
(`candle_import_lag_seconds`), backtest run duration distribution.

## Acceptance Criteria

1. **Given** the Alertmanager service is added to `docker-compose.yml` and `prometheus.yml`'s
   `alerting.alertmanagers` block points at it, **when** `make up` runs, **then** Prometheus's own
   `/api/v1/alertmanagers` endpoint reports the Alertmanager instance as reachable.
2. **Given** `order_execution_errors_total` has a non-zero rate for 5 continuous minutes (Phase 1's
   existing metric, now with an alert rule wired on top), **when** Prometheus evaluates
   `alerting/rules.yml`, **then** the `HighErrorRate` alert fires and Alertmanager POSTs to the configured
   `WebhookURL`.
3. **Given** `LiveFeed` delivers a candle whose `OpenTime` is 45 seconds in the past (normal real-time
   delivery lag), **when** the candle is processed, **then** `feed_candle_delay_seconds{feed_type="live"}`
   reflects approximately `45`, not `0` and not a stale/never-updated value.
4. **Given** `cmd/worker`'s periodic import-lag check runs and the most recent stored `BTCUSDT`/`1m` candle
   is 3 days old (import hasn't run recently), **when** the check executes, **then**
   `candle_import_lag_seconds{symbol="BTCUSDT",timeframe="1m"}` reflects approximately `259200` (3 days in
   seconds), and the `CandleImportStale` alert fires after its 15-minute `for` duration.
5. **Given** a backtest run triggered via `POST /backtest` takes 45 seconds to complete, **when** it
   finishes, **then** `backtest_run_duration_seconds{strategy="<name>",is_walk_forward="false"}` records an
   observation in the `[30,60)` bucket.
6. **Edge case — CLI-triggered backtest metric loss**: **given** a backtest is triggered via `cmd/backtest`
   (not the API), **when** it completes, **then** its `backtest_run_duration_seconds` observation is made
   in-process but is **not** expected to appear in Prometheus (the process exits before any scrape) — this
   is a documented, accepted limitation (Target Behavior's table), not a bug to chase; a test/acceptance
   check for this spec should assert the metric call happens (doesn't panic, uses correct labels) without
   asserting it's actually scraped, since that would require a running Prometheus instance mid-CLI-execution.
7. **Edge case — Alertmanager down**: **given** the `alertmanager` container is stopped/unreachable, **when**
   Prometheus attempts to send a firing alert, **then** Prometheus itself continues operating normally
   (alert delivery failure doesn't affect metric scraping or rule evaluation) — this is native Prometheus/
   Alertmanager behavior, verified here only to confirm no custom code introduces a tighter coupling.

## Out of Scope

- A push-gateway for one-shot CLI metrics (`cmd/candleimport`'s own process-level metrics, as opposed to the
  worker-polls-DB-state approach this spec adopts for `candle_import_lag_seconds`) — explicitly avoided as
  unnecessary infrastructure per blueprint §7.2's guidance; the polling approach is sufficient.
- `docs/grafana/process_health.json`'s console (`cmd/console`) `up` panel — Phase 3, once the console has a
  `/metrics` endpoint (blueprint §4 Phase 3).
- Fixing the asynq queue-depth metrics gap (blueprint §7.1's flagged uncertainty about whether these exist
  at all) — noted as a known gap in the dashboard panel description, not resolved by this spec.

## Dependencies

- Phase 1's `internal/metrics/collector.go`, `internal/notifier` (`WebhookURL` reuse for Alertmanager's
  receiver) — reused unmodified.
- Spec 02 (Candle Import CLI) — `candle_import_lag_seconds`'s underlying data (`LatestOpenTime`) depends on
  imports having run at all.
- Spec 04 (Backtest Engine), Spec 09 (Dry-Run) — `feed_candle_delay_seconds` is emitted from both
  `LiveFeed.Next()` (Phase 1 file, modified here) and `ReplayDriver` (Spec 04's new file).
- Spec 10 (Backtest Persistence and API) — `backtest_run_duration_seconds` wraps `BacktestUseCase.Run`.
