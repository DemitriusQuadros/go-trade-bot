# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Running the applications locally
```bash
go run cmd/api/main.go        # HTTP API server (port 8080)
go run cmd/worker/main.go     # Asynq task worker + monitoring UI (port 9191)
go run cmd/console/main.go    # Terminal UI dashboard
```

Or via Makefile:
```bash
make run-api-local
make run-worker-local
make run-console
```

### Infrastructure (Docker)
```bash
make up       # Build and start all containers (Redis, PostgreSQL, Prometheus, Grafana)
make down     # Stop containers, keep volumes
make logs     # Tail logs
make clean    # Remove containers, volumes, images
```

### Tests
```bash
go test ./...                          # Run all tests
go test ./app/usecase/strategy/...     # Run tests in a specific package
go test -run TestStrategyUseCase_Save ./app/usecase/strategy/...  # Run a single test
```

### Configuration
Copy `config.example.yml` to `config.yml` and fill in credentials. The app reads `config.yml` from the working directory by default, or from the path in `CONFIG_PATH` env var.

## Architecture

This is a Binance trading bot with three separate entry points sharing common `app/` and `internal/` packages.

### Entry Points (`cmd/`)
- **api** — REST API server using `gorilla/mux`, port 8080. Handles strategy CRUD, account, signal management. Runs DB migrations on startup via GORM `AutoMigrate`.
- **worker** — Asynq async task processor that executes trading strategies on their configured cycles. Also serves the Asynqmon monitoring UI at port 9191.
- **console** — Terminal dashboard (termui) with three tabs: Status, Performance, and Open Signals.

Each entry point defines its own `modules/` directory with FX dependency injection modules (configuration, db, broker, strategy, signal, account, cache, metrics).

### Application Layer (`app/`)

Follows clean architecture — dependencies flow inward:

```
handler → usecase → repository → (entities/DB)
                 → services/algorithm → (broker, signal usecase, cache)
```

- **`app/entities/`** — GORM-mapped domain types: `Strategy`, `Signal`, `Order`, `Account`, `StrategyExecution`. `Strategy` embeds `StrategyConfiguration` with a `Configuration datatypes.JSON` field for algorithm-specific parameters.
- **`app/repository/`** — GORM data access. Each domain has its own package (`strategy/`, `signal/`, `account/`).
- **`app/usecase/`** — Business logic. Each usecase defines its own interfaces for its dependencies (repository, broker, etc.), enabling mock-based testing without infrastructure.
- **`app/handler/web/`** — HTTP handlers implementing the `Route` interface (`Handlers() []handler.Configuration`). Each handler takes a `UseCase` interface, not a concrete type.
- **`app/handler/tasks/strategy/`** — Asynq task handler that dispatches to algorithm processors and re-enqueues the strategy for its next cycle.
- **`app/services/algorithm/`** — Three algorithm implementations: `grid/`, `scalping/`, `bollinger/`. Each implements `Execute() error`.
- **`app/workers/strategy/`** — Asynq client wrapper that serializes a `Strategy` and enqueues it with a delay equal to its `Cycle` in minutes.

### Strategy Execution Loop
1. Strategy saved via API → persisted to DB → immediately enqueued as an asynq task.
2. Worker's `HandleStrategyTask` picks it up → fetches fresh strategy from DB → runs algorithm → records `StrategyExecution` → re-enqueues for next cycle.
3. Algorithm processors call `SignalUseCase.GenerateBuySignal` / `GenerateSellSignal` which persist `Signal` + `Order` records and adjust `Account` balance.
4. Strategies with `status = "disabled"` are skipped but NOT re-enqueued.

### Internal / Infrastructure (`internal/`)
- **`broker/`** — Thin Binance API wrapper (`ListTickerPrices`, `ListKline`, `Get24hVolume`).
- **`configuration/`** — Viper-based config loader. Keys map to `config.yml` structure (`BROKER.KEY`, `DB.HOST`, etc.).
- **`memcache/`** — Thread-safe in-memory key-value store used by the Grid algorithm to persist built grid levels between cycles.
- **`customerror/`** — `CustomError{Code, Message}` — errors carry HTTP status codes so handlers can respond correctly.
- **`metrics/`** — Prometheus counter/gauge wrapper (`MetricsCollector`).
- **`middleware/`** — HTTP and Asynq middleware that injects config and metrics.

### Testing Patterns
- Mocks are generated with `testify/mock` and live in `mocks/` subdirectories alongside the package they mock.
- Repository tests use SQLite in-memory (`gorm.io/driver/sqlite`) to avoid needing a real Postgres instance.
- Usecase and handler tests use mocked interfaces only — no DB or broker required.

### Strategy Algorithm Configuration
Algorithm-specific parameters are stored as JSONB in `Strategy.StrategyConfiguration.Configuration`. See `docs/strategy-examples/` for reference payloads for each algorithm (grid, bollinger, scalping).

### Monitoring
- Prometheus scrapes the API and worker. Grafana dashboards are in `docs/grafana/`.
- Asynqmon UI available at `http://localhost:9191/tasks/monitoring` when the worker is running.
