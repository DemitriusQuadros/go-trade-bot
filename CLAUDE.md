# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**This file is kept in sync with the actual current state of the codebase, not with any design document's
aspirational state.** When specs and code disagree (it happens — this project is built almost entirely by
delegating implementation to background agents), this file describes what's actually there. See
`AGENTS.md` for how the project is developed and where the design documents live.

## Commands

### Running the applications locally
```bash
go run cmd/api/main.go        # HTTP API server (port 8080) — also serves the embedded web frontend
go run cmd/worker/main.go     # Asynq task worker + monitoring UI (port 9191)
```

Or via Makefile:
```bash
make run-api-local
make run-worker-local
make web-dev          # Vite dev server for frontend development (hot reload, proxies API calls)
make web-build         # Build the React app and copy it into cmd/api/webui/dist for go:embed
```

There is no terminal UI anymore — `cmd/console` was built across Phases 3-4, then deleted entirely in favor
of a web frontend (see `docs/prd/refactoring.md` §10 and `docs/architecture/web-frontend-blueprint.md`).

### Infrastructure (Docker)
```bash
make up       # Build and start all containers (Redis, PostgreSQL, Prometheus, Grafana, Alertmanager)
make down     # Stop containers, keep volumes
make logs     # Tail logs
make clean    # Remove containers, volumes, images
```

### Tests
```bash
go test ./...                          # Run all Go tests
go test ./app/usecase/strategy/...     # Run tests in a specific package
go test -run TestStrategyUseCase_Save ./app/usecase/strategy/...  # Run a single test
go test ./... -race                    # Race detector — skip by default, this has stalled agents in this
                                        # project before; run scoped to specific packages with real
                                        # concurrency (app/engine, internal/feed, app/usecase/optimize)
                                        # rather than the whole suite
```

E2E tests (Gherkin/Godog) live in `tests/e2e/`, organized by phase (`tests/e2e/features/phase-{1,2,3,4}/`).

There is no frontend test suite yet.

### Configuration
Copy `config.example.yml` to `config.yml` and fill in credentials. The app reads `config.yml` from the
working directory by default, or from the path in `CONFIG_PATH` env var. **`config.example.yml` is stale**
— it only documents the original `BROKER`/`DB`/`REDIS`/`PROMETHEUS` keys and is missing everything added
across Phases 1-4 and the web frontend pivot: `MODE`, `CONFIRM_LIVE`, `TESTNET`, `BROKER.TESTNET_KEY`/
`TESTNET_SECRET`, `WEBHOOK_URL`, `DRY_RUN.{SLIPPAGE_PCT,FEE_PCT,FILL_DELAY}`, and `API_TOKEN` (auth). Check
`internal/configuration/configuration.go` for the authoritative current list before configuring a new
environment.

## Architecture

A Binance algorithmic trading bot: two Go binaries (`cmd/api`, `cmd/worker`) sharing `app/`/`internal/`
packages, plus a React SPA (`web/`) embedded into `cmd/api`'s binary. Four backend phases and one frontend
pivot have landed; see `docs/architecture/refactoring-blueprint.md` and
`docs/architecture/web-frontend-blueprint.md` for the full history and reasoning, or `AGENTS.md` for how
this project's spec-driven, agent-delegated workflow operates.

### Entry Points (`cmd/`)
- **api** — REST API server (`gorilla/mux`), port 8080. Strategy/account/signal/backtest/optimize/
  performance-history CRUD, real order execution, and serves the embedded web frontend (`cmd/api/webui/`,
  `go:embed`) with SPA fallback routing. Runs DB migrations on startup via GORM `AutoMigrate`. Every route
  except `/metrics` and static asset serving requires `Authorization: Bearer <API_TOKEN>`
  (`internal/middleware/auth_middleware.go`).
- **worker** — Asynq async task processor that executes trading strategies on their configured cycles via
  the pluggable `Strategy` interface (see below). Serves the Asynqmon monitoring UI + `/metrics` at port
  9191. Refuses to start with `MODE=live` unless `CONFIRM_LIVE=true`, and unless `Testnet=false` (a live
  process must never point at the testnet exchange adapter).
- **backtest**, **candleimport** — smaller one-shot CLIs (Phase 2) for running a backtest headlessly and
  importing historical candle data from Binance, respectively.

Each entry point defines its own `modules/` directory with FX dependency injection modules.

### Application Layer (`app/`)

Clean architecture — dependencies flow inward: `handler → usecase → repository → (entities/DB)`.

- **`app/entities/`** — GORM-mapped domain types: `Strategy` (has `StrategyName` resolved against the
  strategy registry, and `Mode`/`Status` as separate concepts — `Mode` is the risk tier, `Status` is
  enabled/disabled/testing), `Signal`/`Order` (real exchange order IDs, `StopLossPrice`), `Account`,
  `Candle`, `BacktestRun` (includes a persisted `equity_curve` and, additively, `ExecutionTraceJSON` —
  the per-cycle `[]script.TraceRecord`, left empty for walk-forward runs rather than aggregated across
  windows), `OptimizationRun`, `StrategyPerformanceSnapshot`, `ScriptState` (DB-backed per-strategy script
  state, see `app/strategies/script/` below).
- **`app/repository/`** — GORM data access, one package per domain (`strategy/`, `signal/`, `account/`,
  `candle/`, `backtest/`, `optimize/`, `performancesnapshot/`).
- **`app/usecase/`** — Business logic. Each usecase defines its own interfaces for its dependencies,
  enabling mock-based testing without infrastructure.
- **`app/handler/web/`** — HTTP handlers implementing the `Route` interface. Response DTOs
  (`response.go` alongside each handler) are the norm — raw entities are not JSON-encoded directly for
  `strategy`/`signal` (they have no `json` tags; encoding them raw produces PascalCase keys, inconsistent
  with every other endpoint's snake_case DTOs).
- **`app/handler/tasks/`** — Asynq task handlers: `strategy/` (runs one strategy cycle via the engine),
  `optimize/` (hyperparameter grid search), `performancehistory/` (daily P&L snapshot cron job via
  `asynq.Scheduler`).
- **`app/strategies/`** — The pluggable strategy system. `Strategy`/`Context`/`Signal`/`ExecutionMode` are
  **frozen** (do not change their signatures — see `interface.go`'s own header comment). `StrategyFactory`
  (`registry.go`) takes the full `entities.Strategy` DB row, not just a name string, so a factory can read
  per-strategy persisted fields (the script factory reads `ScriptSource`/`ID`/`Name`). Registered names:
  `script`, `mlgrpc` (`grid`/`bollinger`/`scalping` and the old `template/` wizard package are gone — see
  `docs/specs/strategy-scripting/`, backend-01 through backend-08). `mlgrpc/` delegates hooks to an
  external process over gRPC (`internal/grpc/`).
- **`app/strategies/script/`** — Every strategy is now a **Lua script** run in a sandboxed `gopher-lua`
  VM: `runner.go` (`Eval`, context-cancellable), `bridge.go` (`CallHook` — Context↔Lua value marshaling),
  `indicators.go` (`ind.*` closures over `internal/indicators.IndicatorProvider`), `trace.go`
  (`TraceRecorder`/`TraceRecord`, one record per candle including `Candle exchange.Candle`), `state_store.go`
  (DB-backed persisted state, see `app/repository/scriptstate/` below — deliberately not in-memory/memcache,
  since asynq can redeliver a strategy's next cycle to a different worker replica), `repl.go` (backs the
  REPL/fast-rerun endpoints), and `strategy.go` (`ScriptStrategy`, the `Strategy`-interface adapter that
  delegates every hook to `CallHook`; fail-closed — a Lua runtime error in a hook yields an empty signal for
  that cycle rather than propagating, mirroring how a native strategy panic is handled).
- **`app/entities/strategy.go`** — the old closed `Algorithm` enum / `IsValidAlgorithm` are gone
  (backend-05); `StrategyName` is now the only field identifying which registry entry to resolve. This is
  an API contract break with no back-compat shim: API clients must send `strategy_name`, not `algorithm`.
- **`app/repository/scriptstate/`** — GORM repository for the `script_state` table (`entities/scriptstate.go`)
  backing `app/strategies/script/state_store.go`'s DB-backed persistence.
- **`app/usecase/script/`** and **`app/handler/web/script/`** — REPL and fast-rerun support (backend-08).
  Routes: `POST /api/script/repl` (evaluate an ad-hoc snippet — a script-level error is still HTTP 200 with
  the response's `Error` field set; only infra failures are 500) and `POST /api/script/fast-rerun` (replay a
  script against a bounded in-memory candle window sized to the engine's `candleWindow` constant
  (`app/engine/engine.go`, currently 100), producing one `TraceRecord` per candle regardless of mid-loop
  script errors).
- **`app/engine/`** — The single execution engine (`engine.go`) driving all `ExecutionMode`s by injecting a
  different `Feed`/exchange combination, not by branching engine logic: `backtest.go`/`replay_driver.go`
  (simulated fills against `ReplayFeed`), `dryrun.go` (live prices, simulated fills), `montecarlo.go` (trade
  reordering for robustness testing). Has panic recovery per strategy cycle (`strategy_panics_total` metric
  + webhook alert).
- **`app/workers/strategy/`** — Asynq client wrapper that enqueues a strategy for its next cycle.

There is no more `app/services/algorithm/` (deleted in Phase 1 — that's where Grid/Bollinger/Scalping used
to live as hardcoded switch cases) and no more `internal/broker/` (replaced by `internal/exchange/`, below).

### Internal / Infrastructure (`internal/`)
- **`exchange/`** — `ExchangeClient` ACL interface wrapping `go-binance`. `BinanceAdapter` (production) and
  `BinanceTestnetAdapter` (Paper Trading mode). **No code outside this package should import
  `go-binance` directly.**
- **`feed/`** — `Feed` interface with `LiveFeed` (WebSocket) and `ReplayFeed` (Postgres-backed, for
  backtesting) implementations, plus `multitimeframe.go` for 1m→5m/15m/1h aggregation.
- **`indicators/`** — `IndicatorProvider` ACL wrapping `go-talib`.
- **`metrics_provider/`** — Sharpe/Drawdown/WinRate/ProfitFactor computation, wrapping `cinar/indicator/v2`.
- **`notifier/`** — Generic webhook notifier for trade/error events.
- **`report/`** — HTML backtest report generation.
- **`grpc/`** — `strategy.proto` + generated stubs for the `mlgrpc` strategy adapter.
- **`configuration/`** — Viper-based config loader. Keys map to `config.yml`/env vars. See its source for
  the authoritative current field list (config.example.yml is stale, see Commands above).
- **`memcache/`** — Thread-safe in-memory key-value store, still used by the Grid strategy for cross-cycle
  state (injected via `Context.Config["_cache"]`, not a package-level global).
- **`customerror/`** — `CustomError{Code, Message}` — errors carry HTTP status codes.
- **`metrics/`** — Prometheus counter/gauge/histogram wrapper (`MetricsCollector`). The worker's `/metrics`
  endpoint on `:9191` was, for a long time, silently unreachable (mounted the wrong routes) despite
  `prometheus.yml` scraping it — fixed, but worth knowing if metrics ever look mysteriously empty again.
- **`middleware/`** — HTTP and Asynq middleware: config/metrics injection, and (new) `auth_middleware.go`
  for the web frontend's bearer-token auth.

### Strategy Execution Loop
1. Strategy saved via API → persisted to DB → immediately enqueued as an asynq task.
2. Worker's `HandleStrategyTask` picks it up → fetches fresh strategy from DB → resolves the strategy by
   `StrategyName` via the registry → the engine builds a `Context`, gates the effective `ExecutionMode`
   (per-strategy `Mode` capped by the process-wide `MODE` env var — refuses the cycle rather than silently
   downgrading if a `live`-configured strategy exceeds a lower ceiling) → runs the strategy's hooks →
   records `StrategyExecution` → re-enqueues for next cycle.
3. `GenerateBuySignal`/`GenerateSellSignal` place **real orders** via `ExchangeClient` (this is not a
   paper-trading simulator) and submit a real `STOP_MARKET` stop-loss order at position-open time — not a
   software-polled stop.
4. Strategies with `status = "disabled"` are skipped and NOT re-enqueued (their `Terminate` hook fires once).

### Frontend (`web/`)
React + TypeScript + Vite SPA, built with `make web-build` and embedded into `cmd/api`'s binary via
`go:embed` (`cmd/api/webui/`). Talks to `cmd/api` over the same REST surface described above, plus a
consolidated SSE stream (`GET /stream/dashboard`) for live prices/positions. Auth token lives in
`localStorage`, attached as `Authorization: Bearer <token>` (query-param fallback for the SSE endpoint and
the backtest HTML report iframe, since browsers can't attach custom headers to those requests).

**Known inconsistency, not yet resolved**: the frontend was originally built matching
`docs/specs/web-frontend/frontend-*.md` (plain `fetch` wrapper, no React Query, Recharts for charts — these
were deliberate decisions, see ADR-008 in the web-frontend blueprint). A later, unreviewed "redesign"
commit introduced `@tanstack/react-query`, Tailwind, Shadcn-style components, and `lightweight-charts`
without updating the specs or removing the old approach — `package.json` now has **both** `recharts` and
`lightweight-charts` installed, and chart components are split between the two (`MonteCarloDistribution.tsx`
still uses Recharts; `EquityCurveChart`/`PnlHistoryChart`/`DrawdownChart` use lightweight-charts). Pick one
and finish the migration before this drifts further. That same commit also left over a dozen throwaway
patch scripts (`fix_dashboard*.js`, `refactor*.{js,py}`, `rewrite.js`, `cleanup.js`, `fix.py`, `fix_opt*.js`)
committed in the repo root — these aren't part of the application and should be removed.

### Testing Patterns
- Mocks are generated with `testify/mock` and live in `mocks/` subdirectories alongside the package they mock.
- Repository tests use SQLite in-memory (`gorm.io/driver/sqlite`) to avoid needing a real Postgres instance.
- Usecase and handler tests use mocked interfaces only — no DB or exchange required.
- E2E tests (`tests/e2e/`, Gherkin/Godog) exist for all four backend phases. None exist yet for the web
  frontend.

### Strategy Algorithm Configuration
Algorithm-specific parameters are stored as JSONB in `Strategy.StrategyConfiguration.Configuration`. See
`docs/strategy-examples/` for reference payloads (grid, bollinger, scalping).

### Monitoring
- Prometheus scrapes the API and worker; Alertmanager is also in the docker-compose stack (Phase 2).
  Grafana dashboards are in `docs/grafana/`.
- Asynqmon UI available at `http://localhost:9191/tasks/monitoring` when the worker is running.
- `docker-compose.yml`'s `worker` service (there was none before backend-06) has `mem_limit: 512m` as a
  backstop against a runaway Lua script (ADR-022 — there's no per-script memory ceiling at the Go/Lua
  level) plus `restart: unless-stopped` so an OOM-kill doesn't permanently take the worker down. **512m is
  a placeholder** pending a real 24h steady-state RSS baseline — revisit and size to ~2x observed RSS once
  that measurement exists.

### Known follow-ups from the strategy-scripting cutover (backend-01 through backend-08)
- One strategy row in the reachable dev Postgres is still `strategy_name = "template"` (`id=1`, "RSI
  Momentum", `status=productive`, `mode=dryrun`) — post-cutover this is no longer registry-resolvable and
  needs a manual ops fix (disable or migrate it) before deploying this branch.
- The `algorithm` API field/column removal (see `app/entities/strategy.go` above) has no back-compat
  mapping — any external client still sending `algorithm` instead of `strategy_name` will break.

## Safety notes (this executes real trades with real money)

- `Mode` (per-strategy) and the process-wide `MODE` env var form a dual-layer guard — both default to the
  safest tier (`dryrun`) and both must independently agree before a strategy can execute live.
- `MODE=live` requires `CONFIRM_LIVE=true` at worker startup, and requires `Testnet=false`.
- Never let a strategy's effective mode silently downgrade past `live` — the engine refuses the cycle
  instead, by design (see `app/handler/tasks/strategy/handler.go`'s `gateMode`).
