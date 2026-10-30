# Metrics, Observability & Grafana

TradeBot provides comprehensive observability across all four services via standard **Prometheus** metrics, **Grafana** dashboards, and **Alertmanager** rules.

---

## 1. Scrape Targets

Prometheus scrapes metrics from three primary HTTP endpoints:

| Service | Port | Path | Purpose |
| :--- | :--- | :--- | :--- |
| `cmd/api` | `8080` | `/metrics` | HTTP request rates, latency, auth failures, and SSE active streams. |
| `cmd/worker` | `9191` | `/metrics` | Strategy cycle durations, order fills, simulated stops, and reconciler lag. |
| `cmd/agent` | `9194` | `/metrics` | Agent loop executions, token usage, LLM API costs, and trigger fires. |

---

## 2. Core Prometheus Metrics

### Trading Engine & Order Execution
* `order_execution_duration_seconds`: Histogram measuring round-trip exchange execution latency.
* `order_execution_errors_total{reason="..."}`: Counter of failed order submissions.
* `dryrun_simulated_orders_total{side="buy|sell", type="..."}`: Counter of simulated paper orders.
* `strategy_panics_total{strategy="..."}`: Counter of caught Lua runtime panics.

### Candle Dataset Reconciler
* `candle_sync_lag_seconds{symbol="...", timeframe="..."}`: Gauge measuring time gap between the latest closed exchange candle and stored database records.
* `candle_missing_candles{dataset_id="..."}`: Total missing candles identified in un-reconciled chunks.
* `candle_chunks{status="pending|running|dead"}`: Gauge tracking chunk task distribution.
* `candle_source_fallbacks_total{source="..."}`: Counter tracking fallback transitions from monthly archives $\to$ daily archives $\to$ REST.

### Autonomous AI Agents
* `agent_runs_total{agent="...", trigger="cron|event|market|chain|chat_ui"}`: Counter of completed agent runs.
* `agent_run_duration_seconds`: Duration histogram of LLM tool-calling loops.
* `agent_cost_usd_total{agent="..."}`: Cumulative API cost in USD.
* `agent_tokens_total{agent="...", type="prompt|completion"}`: Token consumption tracking.
* `agent_triggers_fired_total{kind="event|market|chain"}`: Rate of reactive trigger dispatches.
* `agent_market_subscriptions`: Gauge of active 1m WebSocket symbol streams watched by the agent runtime.

---

## 3. Grafana Dashboards

Pre-configured production dashboards are located in `docs/grafana/`:

* **`docs/grafana/process_health.json`**: Monitors CPU, memory RSS baselines, Go goroutine counts, and Asynq queue processing throughput across `cmd/api`, `cmd/worker`, and `cmd/agent`.
* **`docs/grafana/status_page.json`**: Trading operations dashboard tracking active live strategies, open position values, daily realized PnL, stop-loss trigger counts, and candle synchronization lag.

To import them:
1. Log in to Grafana at [http://localhost:3000](http://localhost:3000) (`admin` / `admin`).
2. Navigate to **Dashboards $\longrightarrow$ Import**.
3. Upload `process_health.json` and `status_page.json` or paste their contents.
