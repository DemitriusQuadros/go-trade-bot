# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**This file is kept in sync with the actual current state of the codebase, not with any design document's
aspirational state.** When specs and code disagree (it happens — this project is built almost entirely by
delegating implementation to background agents), this file describes what's actually there. See
`AGENTS.md` for how the project is developed.

**`docs/` now holds only `docs/grafana/`** — the PRDs, architecture blueprints, phase/feature specs, and
strategy-example payloads that used to live under `docs/prd/`, `docs/specs/`, `docs/architecture/`, and
`docs/strategy-examples/` have been removed.

## Commands

### Running the applications locally
```bash
go run cmd/api/main.go        # HTTP API server (port 8080) — also serves the embedded web frontend
go run cmd/worker/main.go     # Asynq task worker + monitoring UI (port 9191)
go run cmd/agent/main.go      # Agents runtime: "agents" asynq queue + agent cron scheduler, /metrics + Asynqmon on 9194
```

Or via Makefile:
```bash
make run-api-local
make run-worker-local
make run-agent-local
make web-dev          # Vite dev server for frontend development (hot reload, proxies API calls)
make web-build         # Build the React app and copy it into cmd/api/webui/dist for go:embed
```

There is no terminal UI anymore — `cmd/console` was built across Phases 3-4, then deleted entirely in favor
of a web frontend.

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
working directory by default, or from the path in `CONFIG_PATH` env var. `config.example.yml` was rewritten
with the multi-user auth work and documents `MODE`, `CONFIRM_LIVE`, `TESTNET`, `DRY_RUN.*`, `API_TOKEN`, `AUTH.*`
and `CF_ACCESS.*`; still check
`internal/configuration/configuration.go` for the authoritative current list before configuring a new
environment. It does now document the agent keys: `AGENT.{PROVIDER,ANTHROPIC_KEY,ANTHROPIC_MODEL,GEMINI_KEY,
GEMINI_MODEL}`, `AGENT_RUNTIME.{CONCURRENCY (default 2), METRICS_PORT (default "9194"), SYNC_INTERVAL
(default 30s)}` and `API_BASE_URL` (used for report deep links). `AGENT_RUNTIME.METRICS_PORT` deliberately
defaults to 9194, not 9193: 9193 is cmd/worker's loopback settings bridge (`INTERNAL_BRIDGE_ADDR`), and
cmd/agent runs on the same host (host networking in docker-compose).

## Architecture

A Binance algorithmic trading bot: two Go binaries (`cmd/api`, `cmd/worker`) sharing `app/`/`internal/`
packages, plus a React SPA (`web/`) embedded into `cmd/api`'s binary. Four backend phases and one frontend
pivot have landed; see `AGENTS.md` for how this project's agent-delegated workflow operates.

### Entry Points (`cmd/`)
- **api** — REST API server (`gorilla/mux`), port 8080. Strategy/account/signal/backtest/optimize/
  performance-history CRUD, real order execution, and serves the embedded web frontend (`cmd/api/webui/`,
  `go:embed`) with SPA fallback routing. Runs DB migrations on startup via GORM `AutoMigrate`. **Multi-user
  auth** (spec `docs/specs/multiuser/auth-01-backend.md`): users (`app/entities/user.go`, roles admin/friend/
  viewer + per-user capabilities `view|backtest|edit_drafts|agent_chat|approve_proposals|admin`), bcrypt
  passwords, HttpOnly `gtb_session` cookie sessions, `/api/auth/{login,logout,me}`, admin `/api/users*`.
  Every `handler.Configuration` MUST set `Capability` - `NewServeMux` panics on an empty/unknown one; the
  route→capability map is in the handlers themselves (`internal/authz`). Cookie-authenticated writes need
  `X-Requested-With: gtb` or a same-host `Origin` (CSRF). `API_TOKEN` (optional) is a bearer service token
  acting as admin; there is no `?token=` fallback any more. Auth is ON by default (`ALLOW_INSECURE_NO_AUTH`
  only when explicitly true). First admin: `AUTH.BOOTSTRAP_ADMIN_{USERNAME,PASSWORD}` when the users table
  is empty. Optional Cloudflare Access JWT check (`CF_ACCESS.{TEAM_DOMAIN,AUD}`, `internal/cfaccess`).
  Non-admins can only create/change `backtest`-mode, non-productive strategies - enforced in
  `app/usecase/strategy` from the request principal (no principal = system/agents = not guarded). Chat has a
  per-user daily budget (`UserUsage`, 409 `user_budget_exceeded`) and per-user transcripts (`mine=true`,
  per-user history replay). Deployment: `docs/deploy/cloudflare-tunnel.md` (expose only 8080). Every
  handler-declared route is mounted under `/api`
  (`NewServeMux` in `cmd/api/main.go`, an `apiRouter := router.PathPrefix("/api").Subrouter()`) — this
  keeps the backend's path space completely disjoint from the SPA's client-side routes, several of which
  share a bare name with a backend route (`/backtest`, `/strategy`, `/settings`, ...). Before this, a full
  page load/refresh on one of those SPA routes hit the backend's exact-match API route instead of falling
  through to `index.html`, because gorilla/mux matches in registration order and the API routes were
  registered before the SPA catch-all — refreshing `/backtest` in the browser returned the JSON run list
  instead of the page. `/metrics` (Prometheus scrape target) and the embedded static assets stay
  unprefixed. Frontend handler `Pattern` strings themselves stay bare (`/backtest`, `/script/repl`, ...);
  the `/api` prefix is added once, by the subrouter and by `web/src/api/client.ts`'s `request()` helper —
  don't hardcode it into an individual handler's `Pattern` or an individual frontend call site.
- **worker** — Asynq async task processor that executes trading strategies on their configured cycles via
  the pluggable `Strategy` interface (see below). Serves the Asynqmon monitoring UI + `/metrics` at port
  9191. Refuses to start with `MODE=live` unless `CONFIRM_LIVE=true`, and unless `Testnet=false` (a live
  process must never point at the testnet exchange adapter). Holds the Redis **strategy-cycle lock**
  (`strategy-cycle:<id>`, `internal/lock.CycleKeyPrefix`, TTL 5 min) around every cycle (agents-platform
  Phase B - the only Phase B change in the trading worker, `processor.SetCycleLock` in `cmd/worker/main.go`):
  taken before the strategy row is read, released after the execution row is written. If an
  `agent:apply_proposal` holds it, the cycle is held and re-enqueued after 5 s (like the drain gate; if that
  re-enqueue fails the task errors so asynq retries it). A Redis error on acquire **fails open** (the cycle runs
  as before Phase B) because the apply side fails closed - it never writes without the lock.
  Its `notifier.NotificationSender` is a `notifier.MultiNotifier` (agents-platform C-01 §2.1,
  `cmd/worker/modules/notifier.go`): the `SwappableNotifier` webhook first (its result is the only one callers
  see; `Swap` still works) and then the **worker -> agents bridge** `notifier.AgentEventBridge`
  (`internal/notifier/agent_bridge.go`), which enqueues every non-backtest trade event as `agent:event`
  (queue `agents`, `MaxRetry(3)`, 30 s timeout) for cmd/agent. **The bridge is fire-and-forget**: `Send`
  hands the event to a bounded background goroutine (max 32 in flight, extra events dropped), the enqueue has
  a 2 s timeout, failures are only logged and counted (`agent_bridge_failures_total{reason}`), and it never
  returns an error or panics - a worker whose Redis is down keeps trading and keeps sending webhooks
  (`cmd/worker/modules/agent_bridge_fanout_test.go`). This is the only Phase C change in the worker;
  `app/usecase/signal` and `app/engine` are untouched.

- **agent** — the isolated agents-platform runtime (agents-platform Phase A). Consumes ONLY the asynq
  `agents` queue (`agent:run` tasks - manual runs enqueued by `POST /api/agents/{id}/run`, plus its own cron
  schedules from `asynq.PeriodicTaskManager` over a DB-backed provider that re-syncs every
  `AGENT_RUNTIME.SYNC_INTERVAL`), never the default queue cmd/worker serves. `agent:run` has `MaxRetry(0)`,
  a 10 min timeout, and `Unique(2m)` for cron runs. Serves `/metrics` on `AGENT_RUNTIME.METRICS_PORT`
  (`agent_runs_total`, `agent_run_duration_seconds`, `agent_cost_usd_total`, `agent_tokens_total`,
  `agent_blocked_order_attempts_total`). **Safety invariant: its only `exchange.ExchangeClient` is
  `exchange.ReadOnlyClient`** (`internal/exchange/readonly.go`; `PlaceOrder`/`CancelOrder` always return
  `ErrReadOnlyClient` and never reach the inner client) - `cmd/agent/modules/exchange.go` never provides the
  undecorated/swappable client, and `cmd/agent/main_test.go` asserts the fx graph can't resolve one. It
  ignores `MODE`/`CONFIRM_LIVE` entirely. Migrates the same entity list as cmd/api. Also serves
  `agent:apply_proposal` (Phase B, queue `agents`, `MaxRetry(3)`, 2 min timeout) - the ONLY code path that
  changes a live strategy's code, see "Agents platform (Phase B)" below. Phase C also serves `agent:event`,
  `agent:sweep_events` and runs the market watcher (see "Agents platform (Phase C-01)" below); metrics
  `agent_triggers_fired_total{kind}`, `agent_trigger_suppressed_total{kind}` (kind = event | market | chain)
  and the `agent_market_subscriptions` gauge.
- **mcp** — MCP server exposing the agent tool registry to external MCP clients (stdio or `--transport=http`).
  MCP tool calls act as the default "Copilot" agent's permissions; `notify` and `trigger_agent` are never exposed over MCP; no
  strategy writer lock is wired here (nil Lock).

Each entry point defines its own `modules/` directory with FX dependency injection modules. The `Migrate`
entity lists in `cmd/api/main.go`, `cmd/mcp/main.go` and `cmd/agent/main.go` must stay in sync; all three
also call `agentplatform.EnsureDefaultAgent` (seeds the one `IsDefault` "Copilot" persona) and
`proposal.EnsureGateConfig` (seeds the `DeployGateConfig` singleton, Phase B).

There is no more `cmd/backtest` or `cmd/candleimport` (one-shot CLIs from Phase 2) — both were removed once
their functionality was fully superseded by the API: backtests run via `POST /backtest`/`/backtest/walkforward`
(`app/handler/web/backtest/`), and candle imports via `POST /candles/import` plus the recurring
`/candles/schedule` CRUD (`app/handler/web/candleimport/`), neither of which the CLIs ever had an equivalent
for.

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
  `script`, `mlgrpc` (`grid`/`bollinger`/`scalping` and the old `template/` wizard package are gone —
  replaced by the Lua scripting system below). `mlgrpc/` delegates hooks to an external process over gRPC
  (`internal/grpc/`).
- **`app/strategies/script/`** — Every strategy is now a **Lua script** run in a sandboxed `gopher-lua`
  VM: `runner.go` (`Eval`, context-cancellable), `bridge.go` (`CallHook` — Context↔Lua value marshaling),
  `indicators.go` (`ind.*` closures over `internal/indicators.IndicatorProvider` — one lowercase closure per
  wrapped go-talib function, e.g. `ind.rsi`/`ind.adx`/`ind.stoch`/`ind.obv`/`ind.sar`; see that file's
  `bindIndicators` doc comment for the full catalogue), `trace.go`
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
  (simulated fills against `ReplayFeed`), `dryrun.go` (`NewDryRunDriver`, a LiveFeed-driven
  replay driver that nothing wires), `simulated_stop.go` (the worker's dryrun stop-loss evaluator, run via
  `Engine.PreCycle`), `montecarlo.go` (trade reordering for robustness testing). Has panic recovery per
  strategy cycle (`strategy_panics_total` metric + webhook alert).
- **`app/workers/strategy/`** — Asynq client wrapper that enqueues a strategy for its next cycle.
- **Agents platform (Phase A)** — agents are configurable persona rows, not code:
  - `app/entities/agentplatform.go`: `Agent` (goal prompt, provider/model override, permissions, cron
    triggers, webhook target ids, daily budget, paused, `IsDefault`), `AgentStrategyBinding`,
    `StrategyMemoryEntry` (memory shared PER STRATEGY across all agents + the operator), `AgentReport`
    (typed blocks + server-rendered HTML snapshot), `WebhookTarget`, `AgentUsage` (per agent per UTC day,
    incl. `BudgetAlertSent` dedupe flag). `AgentRun` gained `AgentID`/`TriggerDetail`/`ChainDepth`/
    `ParentRunID`/tokens/`CostUSD`; `Settings` gained `AgentsPaused` (global kill switch).
  - `app/repository/agentplatform/`: one repo for all of the above (+ `ReportDataSource`, the DB-backed
    `agentreport.DataSource`). Mock in `mocks/` is hand-generated in mockery style (mockery's embedded
    go1.24 parser can't load this go1.25 module).
  - `app/usecase/agent/`: `AgentUseCase.Run(RunRequest)` is the persona-aware loop; `RunToolLoop` is a thin
    wrapper using the default agent. System prompt = house rules + persona + operating context + shared
    memory (last 30 `journal`/`finding`/`report_ref` entries per context strategy, ~24k char cap) +
    authoring doc. Chat turns are NOT written to memory (they live in the chat transcript, i.e. the
    `chat_ui` runs); memory is deliberate notes only - operator notes (incl. the chat UI's "Save to
    notes", `POST /strategies/{id}/memory` -> `journal`), `write_journal` findings and report refs. Legacy
    `chat_user`/`chat_agent` rows stay in the DB, visible to `read_memory` but not the prompt or the notes panel. Tools are tagged with a
    permission and filtered per persona, and re-checked at dispatch. `RunGuard` (`guard.go`) re-checks the
    kill switch / agent pause / daily budget before EVERY model call (fails closed). New tools
    (`tools_platform.go`): `read_memory`, `write_journal`, `list_reports`, `write_report` (always granted)
    and `notify` (permission `notify`, max 10 per run) - none can touch `entities.Strategy`, orders or
    settings. `write_report`'s schema is a per-block-type `oneOf` (each branch pins `type` with a one-value
    `enum` and lists the required `data` props) plus an example in the description; a block that puts its
    fields next to `type` instead of under `data` is accepted by moving them into `data`
    (`agentreport.DecodeBlocksLenient`, logged as `agent: debug:`). `get_candle_coverage {symbol?}`
    (`tools_coverage.go`, permission `read`) lists stored candles per (symbol, timeframe) from
    `CandleRepository.Coverage` (one grouped query; `AgentUseCase.Coverage`, set by `WirePhaseB`), and the
    system prompt tells the model to call it before backtesting.
  - Tool-loop cap (fix-02 B1): `chat_ui`/`mcp_tool` default to 8 iterations (`MaxToolLoopIterations`),
    every other trigger (cron, manual, event, market, chain) to 24 (`UnattendedMaxToolLoopIterations`);
    `Agent.MaxIterations` (REST `max_iterations`, 0 = trigger default, else 4..50) overrides both. At the cap
    the model gets ONE more turn with no tools and the user message "Iteration limit reached - give your
    final answer now, no more tool calls." (guard re-checked first); if it answers with text the run is `ok`
    with `AgentRun.HitIterationCap` (REST `hit_iteration_cap`), otherwise `error`.
  - House rules: `AgentInstruction` ID 1 is seeded empty by every `Migrate` (api/mcp/agent,
    `EnsureInstruction`, idempotent); `GetInstruction` uses `Limit(1).Find`, so a missing row is empty
    content with no gorm "record not found" log. `save_strategy_script` (gate unchanged) now takes a one-writer lock per existing strategy;
    and refuses to update any productive or live-mode strategy on every path (chat, cron/manual, MCP) -
    live changes only ever land via an operator-approved proposal (Phase B).
  - `app/usecase/agentplatform/`: management usecase behind the REST API (validation, next_run_at, usage,
    webhook targets, memory, kill switch).
  - `app/handler/tasks/agent/`: `agent:run` processor + `CronProvider` (one schedule per (agent, cron spec)
    for agents that are not paused and have >=1 binding; nothing while the kill switch is on; fails closed).
  - `app/workers/agent/`: `agent:run` enqueue helper (queue `agents`) and, since Phase C, `ChainLauncher`
    (the shared chain guards).
  - REST (all under `/api`): `app/handler/web/agents/` (`/agents` CRUD, `/agents/{id}/pause|run|runs|usage`,
    `PUT /agents/kill-switch`, `/strategies/{id}/memory`), `app/handler/web/agentreports/`
    (`/agent-reports`, `/{id}`, `/{id}/html` with a `default-src 'none'` CSP, `?token=` and `?theme=`),
    `app/handler/web/webhooktargets/` (CRUD + `/test`; url/secret masked). `POST /agent/runs` accepts
    `agent_id`; a paused/halted persona is a 409. The kill switch is written ONLY by
    `PUT /agents/kill-switch`: `GET /settings` shows `agents_paused` read-only and `PUT /settings` ignores it
    (the settings repo's `Save` omits the column).
- **Agents platform (Phase B-01: challengers, deploy gate, proposals)** — spec
  `docs/specs/agents-platform/phase-b-01-backend-auto-improve.md`:
  - Data: `Strategy.ChallengerOfID` (a challenger = dryrun/testing clone of a live or productive champion) and
    `Strategy.CreatedByAgentID` (additive, keeps agent-created strategies in that agent's scope) - both set only
    at creation; `StrategyRepository.Update` omits them (a full-column Save would otherwise unlink a challenger on
    every edit). `entities.StrategyChangeProposal` (`promote_challenger` | `gate_failed_change`; pending ->
    approved -> applied, or rejected/superseded/failed; `LastFlatCheckAt`), `entities.DeployGateConfig` singleton
    (ID 1: min Sharpe delta 0, max DD ratio 1.10, min trades 20, min PF 1.0, lookback 6 / train 3 / test 1 months,
    timeframe "" = the strategy's cycle interval), `AgentUsage.StrategiesCreated`, `BacktestRun.CandidateSourceHash`.
    Repo: `app/repository/proposal/`. `strategy_response` DTO has `challenger_of_id`.
  - `BacktestUseCase.RunWalkForwardForStrategy(ctx, strat, req)` runs walk-forward on an in-memory (unsaved)
    strategy; `RunWalkForward` = load by id -> delegate. Runs persist under `strat.ID`. `Run` and
    `RunWalkForward*` return a 400 `customerror` when (symbol, timeframe, range) has zero candles, e.g.
    `no BTCUSDT 15m candles in 2024-06-01..2024-12-01; available: 1h 2021-01-01..2026-09-20 (36267), ...`
    (fix-02 B2; `RunEphemeral` is unchanged).
  - Deploy gate: `app/usecase/agent/deploygate` (pure `Evaluate`: trades >= min, Sharpe >= baseline + delta,
    maxDD <= baseline x ratio (0 baseline DD -> candidate must be 0), PF >= min (+Inf passes), NaN fails; a
    0-trade baseline counts as Sharpe 0/DD 0) and `gate_runner.go` (`GateRunner`: two walk-forwards over the
    last `LookbackMonths` on the first monitored symbol - baseline = current source, candidate = new source -
    then `Evaluate` on the persisted runs; fewer than 90% of the expected candles -> failing
    `insufficient_history` check, whose detail ends with `; available: <coverage>` when the candle counter
    also implements `CandleCoverageReader`). Thresholds come ONLY from `DeployGateConfig`; tool args can't carry numbers.
  - Tools (`tools_phaseb.go`): `create_challenger` (edit_testing; champion must be bound + live/productive; one
    active challenger per champion, auto-bound, finding on the champion), `deploy_to_testing` (edit_testing;
    non-live, non-productive, in-scope targets; daily `MaxAutoDeploysPerDay` cap (0 = disabled) via a
    conditional counter; writer lock; Lua compile check; gate; pass -> `StrategyUseCase.Update` (+ScriptVersion)
    after re-checking the target didn't change/go live during the gate; fail -> pending `gate_failed_change`
    proposal), `propose_promotion` (propose_live; >= 7 days challenger age or rationale `EARLY:`; forward-test
    evidence from closed signals since the challenger's creation + a recorded, non-blocking gate run;
    supersedes older pending proposals for the champion; warning notification with
    `<APIBaseURL>/agents/proposals/<id>`), `create_strategy` (create_strategy; testing + backtest/dryrun,
    auto-bound, max 3 per agent per UTC day in `AgentUsage.StrategiesCreated`), `list_proposals` and
    `get_deploy_gate_config` (always granted). `save_strategy_script` now only UPDATES backtest-mode drafts
    (dryrun -> "use deploy_to_testing (gated)"); its create path is unchanged (records `CreatedByAgentID`).
    Evidence JSON: `{"gate": {passed, checks:[{name,passed,candidate,baseline,threshold,detail}], baseline_run_id,
    candidate_run_id}, "gate_context": {...}, "forward_test": {since, age_days, challenger:{trades,win_rate_pct,
    net_pnl,max_adverse}, champion:{...}}}`; non-finite floats are the strings "+Inf"/"-Inf"/"NaN".
  - Write scope (`strategy_scope.go`): bound strategies ∪ challengers of bound champions ∪ strategies the agent
    created. Every strategy-writing tool refuses anything else (fails closed without the platform repo). This
    applies to chat and MCP too (the default Copilot has no bindings by default).
  - REST (`app/handler/web/proposals/`, cmd/api): `GET /proposals?status=a,b&strategy_id=&agent_id=&limit=
    &before_id=` (strategy_id matches target OR challenger), `GET /proposals/pending-count`, `GET /proposals/{id}`,
    `POST /proposals/{id}/approve|reject` (`{"note"?}`, pending only, both return the detail DTO; approve with a
    changed target source -> 409 `{"error":"superseded"}`; approve enqueues `agent:apply_proposal`, and reverts to
    pending if the enqueue fails), `GET|PUT /deploy-gate` (ratios > 0, min_trades >= 1, months >= 1,
    lookback >= train + test, known timeframe).
  - Apply (`app/usecase/proposal.Applier`, cmd/agent `agent:apply_proposal`): approved only; target source !=
    base -> superseded; promotions and live/productive targets wait for flat (no open signal) - each re-check is a
    NEW task `ProcessIn(1m)`, failing with "never flat" 7 days after approval; a `gate_failed_change` on a
    non-live, non-productive target applies immediately. The write is `StrategyRepository.ReplaceScriptSource`
    under the `strategy-cycle:<id>` lock: one transaction re-checks base source + open signals, updates ONLY
    `script_source`/`updated_at` (never Mode/Status), writes a ScriptVersion and clears ScriptState when flat.
    Then: status applied, operator-authored finding memory entry, info notification, and (promotions) the
    challenger is disabled. Residual race (documented in `replace_source.go`): a cycle whose lock TTL expired
    could still open a position concurrently; the new code then manages it.
  - `app/usecase/agent` must never import `app/repository/strategy` / `app/usecase/proposal`
    (`phaseb_isolation_test.go` walks its imports), so no LLM tool can reach `ReplaceScriptSource`.
- **Agents platform (Phase C-01: event, market and chain triggers)** — spec
  `docs/specs/agents-platform/phase-c-01-backend-triggers.md`. Phase C only STARTS agent runs; every
  triggered run is an ordinary unattended run (`AgentRun.Trigger` = `event` | `market` | `chain`, detail in
  `TriggerDetail`) under every Phase A/B guard (RunGuard kill switch/pause/budget, permissions, write scope,
  live/productive refusal, deploy gate). `isScheduledTrigger` treats every trigger except `chat_ui`/`mcp_tool`
  as unattended.
  - Schema (`app/entities/agenttriggers.go`): `AgentTriggers{Cron, Events []EventTrigger, Market
    []MarketRule, ChainFrom []ChainTrigger}` - the REST `triggers` contract. Its `UnmarshalJSON` also accepts
    the pre-C forms (`events: ["position.closed"]` -> `{type}`, `chain_from: [3]` -> `{agent_id: 3, on:
    "report"}`); `Agent.ParsedTriggers()` is lenient (a bad element is skipped, the rest survives). Validation
    (400) and default-filling live in `app/usecase/agentplatform/triggers.go`; a self-chain or a ChainFrom cycle
    across all agents is 400 `{"error":"chain_cycle","message":"chain cycle: A → B → A"}` (the frontend matches
    the code). `AgentResponse.trigger_summary` counts each kind; responses always carry all four arrays.
  - Event types: `position.opened`, `position.closed`, `stoploss.hit` (derived: a stop-loss `exit_reason`,
    incl. `simulated_stop_loss`), `strategy.error`, `strategy.panic` (derived: `Data["panic"]`), and the swept
    `drawdown` / `no_signal`. Events fire only for strategies in the agent's scope
    (`agentusecase.StrategyInScope`, the same definition as the Phase B write scope).
  - Dispatch (`app/handler/tasks/agent/event_dispatch.go`, cmd/agent `agent:event`): skip paused agents and
    everything while the kill switch is on (unreadable settings = on); cooldown via Redis `SET NX PX`
    (`internal/cooldown`, key `agent-trigger:<agentID>:<type>:<strategyID>`, default 15 min, 240 for swept
    types) - also dedupes across replicas; enqueues `agent:run` with `StrategyID` and detail `{event,
    strategy_id, symbol, occurred_at, data (truncated)}`.
  - Sweeper (`sweeper.go`, `agent:sweep_events`, scheduled `*/5 * * * *` by the `CronProvider` only while a
    non-paused agent has a swept trigger): `drawdown` = peak-to-trough of cumulative realized PnL (sum of
    `Order.Profit`, the performance-snapshot basis) over signals closed in `window_hours`
    (`SignalRepository.ListClosedBetween`), as % of the first closed trade's invested amount; `no_signal` =
    last opened signal (or `CreatedAt`) older than `window_hours`, skipping disabled and backtest-mode
    strategies. Both go through the dispatcher's cooldown/enqueue path.
  - Market watcher (`market_watcher.go`, fx lifecycle in cmd/agent): every `SYNC_INTERVAL` recomputes the
    symbols from non-paused agents' `Market` rules (none while the kill switch is on), keeps one 1m
    `feed.NewLiveFeed` over the **read-only** client per symbol, closes unwatched ones, backfills with
    `ListKline` (max 1000) on subscribe and after a reconnect gap, buffers largest-window + 24h closed candles,
    and evaluates each closed candle: `pct_move` = signed `(close/close[-window] - 1)*100` fires on its
    absolute value; `volatility_spike` = window ATR / prior-24h ATR (`internal/indicators` ATR; with < 24h of
    history it uses what it has, min 60 candles). Cooldown key `agent-market:<agentID>:<ruleID>`. Detail
    `{rule_id, symbol, kind, window_minutes, observed_pct | observed_multiplier, threshold, price, at,
    monitored_by_bound_strategy}`; `StrategyID` only when exactly one bound strategy monitors the symbol. It
    writes a status snapshot to Redis (`internal/marketstatus`, key `agent-runtime:market-status`, TTL 15 min)
    that cmd/api serves as `GET /api/agents/market-symbols` -> `{"symbols":[{symbol, watchers, last_price,
    last_candle_at}], "runtime_seen_at": RFC3339|null}`.
  - Chains: declarative `ChainFrom` (`chain_dispatch.go`, run by the `agent:run` processor after an ok run;
    `on` = `report` (the run wrote >= 1 report), `notify` (>= 1 successful notify) or `success`; the run
    outcome comes from the non-persisted `AgentRun.ReportIDs`/`NotificationsSent`) and the imperative
    `trigger_agent {agent_id, message}` tool (`app/usecase/agent/tools_chain.go`, new permission `chain`,
    max 3 per run; target must exist, not be paused, not be the caller or already in the chain). Both go
    through `app/workers/agent.ChainLauncher` - the guards: `ChainDepth <= 3`, no agent twice in
    `ChainPath`, and a deterministic asynq `TaskID` `agent-chain:<target>:<sourceRun>` (24 h retention) so a
    redelivered parent can't double-fire. A refusal is logged + `agent_trigger_suppressed_total{kind="chain"}`
    and never fails the parent run. Chain detail `{source_agent_id, source_run_id, report_ids, on, chain_path
    (+ message for trigger_agent, on = "trigger_agent")}`. `RunPayload` gained `StrategyID`, `ChainPath`,
    `ParentRunID`; the run DTO gained `chain_depth` and `parent_run_id`.
  - Prompts (`app/usecase/agent/prompts.go` `BuildTriggerInput`) phrase event/market/chain runs; the operating
    context shows the trigger detail and, for chains, depth and path.

There is no more `app/services/algorithm/` (deleted in Phase 1 — that's where Grid/Bollinger/Scalping used
to live as hardcoded switch cases) and no more `internal/broker/` (replaced by `internal/exchange/`, below).

### Internal / Infrastructure (`internal/`)
- **`exchange/`** — `ExchangeClient` ACL interface wrapping `go-binance`. `BinanceAdapter` (production) and
  `BinanceTestnetAdapter` (Paper Trading mode). **No code outside this package should import
  `go-binance` directly.**
- **`feed/`** — `Feed` interface with `LiveFeed` (WebSocket) and `ReplayFeed` (Postgres-backed, for
  backtesting) implementations, plus `multitimeframe.go` for 1m→5m/15m/1h aggregation.
- **`indicators/`** — `IndicatorProvider` ACL wrapping `go-talib`. `RSI`/`BollingerBands`/`EMA`/`SMA`/`MACD`/
  `ATR` are frozen (ADR-005); every other genuine go-talib technical/candle indicator (overlap studies,
  momentum, volume, volatility, price transform, Hilbert Transform cycle, statistic, and window-math
  functions — ~69 methods) is also wrapped, all taking `[]exchange.Candle` plus numeric params and
  returning `[]float64` (single or multi-return). `Beta`/`Correl` (need two independent price series) and
  `MaVp` (needs a per-bar variable period array) are the only genuine indicators NOT wrapped — no sensible
  signature exists for them given `strategies.Context` carries exactly one candle series. All MAType
  parameters on the added methods (`Ma`/`Apo`/`Ppo`/`MacdExt`/`Stoch`/`StochF`/`StochRsi`) are hardcoded to
  `MATypeSMA` in the adapter — see `talib_adapter.go`'s "Additional indicators" section.
- **`metrics_provider/`** — Sharpe/Drawdown/WinRate/ProfitFactor computation, wrapping `cinar/indicator/v2`.
- **`notifier/`** — Generic webhook notifier for trade/error events (`WEBHOOK_URL`), plus the agents'
  multi-target notifier (`agent_notifier.go`: generic/discord/slack/telegram formatters, link-only, async
  delivery with one retry, never logs target URLs or the telegram token), plus (Phase C) `MultiNotifier`
  and the fire-and-forget worker -> agents `AgentEventBridge` (`agent:event`).
- **`report/`** — HTML backtest report generation.
- **`grpc/`** — `strategy.proto` + generated stubs for the `mlgrpc` strategy adapter.
- **`configuration/`** — Viper-based config loader. Keys map to `config.yml`/env vars. See its source for
  the authoritative current field list.
- **`memcache/`** — Thread-safe in-memory key-value store, still used by the Grid strategy for cross-cycle
  state (injected via `Context.Config["_cache"]`, not a package-level global).
- **`report/agentreport/`** — agent report renderer: typed blocks (`summary`, `callout`, `kpi_grid`,
  `equity_chart`, `trade_table`, `code_diff`, `recommendation`, `text_table`) validated and rendered with
  `html/template` only into one self-contained Console Pro HTML document (tokens copied from
  `web/src/index.css` - keep in sync by hand). Data-bearing blocks carry ids; values are resolved from the
  DB, never from model-supplied numbers. Golden files in `testdata/` (`go test ./internal/report/agentreport
  -update` to regenerate).
- **`cooldown/`** — agent-trigger cooldown/dedupe store (Phase C): Redis `SET NX PX` (`RedisStore`) and an
  in-memory variant with an injectable clock for tests. Redis test only with `REDIS_TEST_ADDR`.
- **`marketstatus/`** — the market watcher's Redis status snapshot (written by cmd/agent, read by cmd/api for
  `GET /api/agents/market-symbols`).
- **`lock/`** — one-writer-per-strategy lock for agent runs: Redis (`SET NX PX` + compare-and-delete Lua,
  key `agent:strategy-lock:<id>`, TTL 15 min; the same type with `CycleKeyPrefix` is the Phase B
  `strategy-cycle:<id>` lock shared by cmd/worker and `agent:apply_proposal`) and an in-memory variant. The Redis test runs only with
  `REDIS_TEST_ADDR` set.
- **`cronspec/`** — agent cron validation/next-fire (robfig/cron standard parser, UTC).
- **`modelprovider/`** — LLM ACL (Anthropic/Gemini). `CompletionResult.Usage` carries tokens;
  `pricing.go`'s `EstimateCostUSD` uses a HAND-MAINTAINED price table (conservative fallback, never 0);
  `ConfigProviderFactory` serves per-persona provider/model overrides.
- **`customerror/`** — `CustomError{Code, Message}` — errors carry HTTP status codes.
- **`metrics/`** — Prometheus counter/gauge/histogram wrapper (`MetricsCollector`). The worker's `/metrics`
  endpoint on `:9191` was, for a long time, silently unreachable (mounted the wrong routes) despite
  `prometheus.yml` scraping it — fixed, but worth knowing if metrics ever look mysteriously empty again.
- **`middleware/`** — HTTP and Asynq middleware: config/metrics injection, and `auth_middleware.go`
  (session cookie / service bearer → `authz.Principal` in the request context, capability check, CSRF).

### Strategy Execution Loop
1. Strategy saved via API → persisted to DB → immediately enqueued as an asynq task.
2. Worker's `HandleStrategyTask` picks it up → fetches fresh strategy from DB → resolves the strategy by
   `StrategyName` via the registry → the engine builds a `Context`, gates the effective `ExecutionMode`
   (per-strategy `Mode` capped by the process-wide `MODE` env var — refuses the cycle rather than silently
   downgrading if a `live`-configured strategy exceeds a lower ceiling) → runs the strategy's hooks →
   records `StrategyExecution` → re-enqueues for next cycle.
3. Order routing is by **effective** mode (fix-01; `StrategyProcessor.engineFor` in
   `app/handler/tasks/strategy/handler.go`, two engines wired in `cmd/worker/modules/engine.go`):
   - `live` → the real engine: `GenerateBuySignal`/`GenerateSellSignal` place **real orders** on the process
     `ExchangeClient` and submit a real exchange-side `STOP_MARKET` stop-loss at position-open time — not a
     software-polled stop. `paper` uses the same real engine, whose client is the testnet adapter
     (`gateMode` refuses paper without `Testnet=true`).
   - `dryrun` → the simulated engine (`modules.NewDryRunEngine`): `exchange.DryRunExchange`
     (`internal/exchange/dryrun.go`) wraps the real client in `exchange.NewReadOnlyClient`, so market data is
     real but any order call that reached the real client would get `ErrReadOnlyClient`. MARKET orders fill
     at the latest ticker ± `DRY_RUN.SLIPPAGE_PCT` (fees from `DRY_RUN.FEE_PCT` via `SignalUseCase.FeePct`);
     order IDs are `SIM-…`. The stop-loss is a **simulated resting stop** persisted on the Order row
     (`StopLossOrderID` `SIM-STOP-…`, `StopLossPrice`); `engine.SimulatedStopEvaluator` (the dryrun engine's
     `PreCycle` hook) checks closed candles since the entry before any hook runs and, on `Low ≤ stop`, closes
     the position at stop − slippage through the normal `GenerateSellSignal` stop-reconciliation path (exit
     reason `simulated_stop_loss`). `Order.SimStopEvaluatedAt` is the persisted watermark so a candle is never
     evaluated twice. Dryrun Signal/Order rows are still written (`Signal.Mode = "dryrun"`); the real account
     row is read for sizing but never written by simulated fills (`signal.DryRunAccount`; a virtual balance
     is a follow-up); simulated orders count in `dryrun_simulated_orders_total{side,type}`, never in
     `order_execution_*`. The worker logs `worker: order routing live=… paper=… dryrun=…` at startup.
   - `backtest` → refused by the worker (no hook runs, NOT re-enqueued, one `strategy.error`
     notification); backtests only run through the backtest API.
   - `GenerateSellSignal` refuses to close a `SIM-` position on a real client (e.g. `POST
     /api/signal/close/{id}` from `cmd/api`) or a real position on the simulator.
   - **Strategy-supplied exits** (all modes): a script's `stop_loss = {price}` from `go_long` is the
     STOP_MARKET price and wins over `stop_loss_pct`; if it isn't below the fill price it falls back to
     `stop_loss_pct` and notifies (`SignalUseCase.submitStopLoss`). `take_profit = {price}` is persisted as
     `Order.TakeProfitPrice` (only if above the fill) and enforced by the **engine**, not the exchange: at
     the start of each cycle, once `ctx.price` (latest close) reaches it, the position is closed at market
     (exit reason `take_profit`) and the hooks are skipped. A sell from `update_position` has exit reason
     `strategy_exit`. The real reason is stored in `Order.ExitReason`; backtest trade logs use it (older
     rows fall back to guessing from the profit sign). `ctx.position.side` is always `"long"` (spot only).
     Before this fix a script's stop price and take-profit were ignored and `ctx.position.side` was nil,
     so scripts exiting on `side == "long"` never closed (strategy #6: 1 trade in 2.7 years, now 196).
4. Strategies with `status = "disabled"` are skipped and NOT re-enqueued (their `Terminate` hook fires once).

### Frontend (`web/`)
React + TypeScript + Vite SPA, built with `make web-build` and embedded into `cmd/api`'s binary via
`go:embed` (`cmd/api/webui/`). Talks to `cmd/api` over the same REST surface described above (all under
`/api`), plus a consolidated SSE stream (`GET /api/stream/dashboard`) for live prices/positions.

**Two modes (agents-platform Phase D, specs `phase-d-01`/`phase-d-02`)**: a header Agent|Code toggle
(`Ctrl+Shift+.`). Agent mode is `/agent?strategy=&agent=` (`pages/AgentMode.tsx`: persona/strategy rails,
full-screen transcript, shared `AgentNotesPanel` memory). Code mode is every other route, with the chat as the
resizable right dock `components/domain/AgentDock.tsx` (`Ctrl+.`), which replaced the floating
`AgentCopilotWidget` (deleted). Both views render one `context/ChatSessionContext.tsx` transcript per context
(`general` | `strategy:N`), hydrated from `GET /api/agent/runs?trigger=chat_ui&strategy_id=N|none&before_id=`.
Tool results render as rich cards (`components/domain/agentchat/cards/`: Backtest/Gate/CodeChange/Report/
Proposal) driven by each tool call's server-computed `refs` (`app/handler/web/agent/refs.go`
`ExtractToolRefs`, response-time only, never persisted; `web/src/lib/toolRefs.ts` mirrors it as a fallback -
keep the two in sync when a tool's result format changes). `GET /agent/runs` also takes `trigger`
(comma list), `agent_id`, `before_id`, `strategy_id=none`; `limit` is clamped to 100; run DTOs strip the
`[Context: ...]` marker from `input_summary`.

**Design system**: "Console Pro" (color tokens, typography, component conventions, chart/code-editor
theming) — check existing components for conventions before adding or restyling any UI; the standalone
design-system doc has been removed. Auth is the same-origin session cookie (`context/AuthContext.tsx`,
`can(cap)`; SSE and report iframes need no token); every request sends `X-Requested-With: gtb`. UI hides or
disables what the user's capabilities don't allow, but the backend is the source of truth.

**Translation (EN / ES / PT-BR, specs `docs/specs/multiuser/i18n-01/02`)**: every UI string lives in
`web/src/i18n/locales/{en,es,pt-BR}.ts` (`en` is the source; the others are typed `typeof en`, so a missing
key fails `tsc`); use `useT()`/`t('area.key', vars)` and `t.enum(...)` for API enum labels - never raw JSX
text. `npm run build` runs `web/scripts/check-i18n.mjs` first (missing keys, placeholder mismatches, raw JSX
literals). Numbers/dates go through `web/src/lib/format.ts` (locale-aware; `formatUtcDay` stays UTC; charts
use `chartLocalization()`). Locale: `/auth/me` `locale` → `localStorage gtb_locale` → browser. Backend:
`internal/i18n` (`en|es|pt-BR`), `Settings.DefaultLocale` (`default_locale`, admin) for unattended runs,
reports and webhooks; chat replies follow the user's locale (system-prompt language line). Agent reports
store one snapshot per locale (`AgentReport.RenderedHTMLByLocale`, served by `/html?lang=`; old reports
fall back to `RenderedHTML`). Label catalogs: `internal/report/agentreport/messages.go`,
`internal/notifier/messages.go`, `internal/report/messages.go`.

**Known inconsistency, not yet resolved**: the frontend was originally built with a plain `fetch` wrapper,
no React Query, and Recharts for charts (deliberate decisions at the time). A later, unreviewed "redesign"
commit introduced `@tanstack/react-query`, Tailwind, Shadcn-style components, and `lightweight-charts`
without removing the old approach — `package.json` now has **both** `recharts` and
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
Algorithm-specific parameters are stored as JSONB in `Strategy.StrategyConfiguration.Configuration`.

### Monitoring
- Prometheus scrapes the API and worker; Alertmanager is also in the docker-compose stack (Phase 2).
  Grafana dashboards are in `docs/grafana/`.
- Asynqmon UI available at `http://localhost:9191/tasks/monitoring` when the worker is running, and at
  `http://localhost:9194/tasks/monitoring` from cmd/agent (`NewMonitoringHandler` in
  `cmd/agent/modules/runtime.go`, same Redis, every queue incl. `agents`). Both links are display-only
  settings: `asynqmon_url` and `agents_asynqmon_url` (`Settings.AgentsAsynqmonURL`, default empty) in
  GET/PUT `/settings`.
- `docker-compose.yml` has an `agent` service (cmd/agent, `mem_limit: 512m`, `restart: unless-stopped`,
  host networking like `mcp`).
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

### Known follow-ups from the agents platform (Phase A/B)
- `save_strategy_script` refuses productive/live strategies on every path (chat, cron/manual, MCP) and, since
  Phase B, only updates backtest-mode drafts; live changes go through challenger -> proposal -> operator approval.
- Phase B scope is strict for chat too: the default Copilot can only change strategies bound to it or that it
  created, so chat edits of operator-created drafts now need a binding.
- The deploy gate needs >= 90% candle coverage over the lookback (6 months by default) at the gate timeframe;
  with the known ~1000-candle import limit most gates fail with `insufficient_history` (-> proposal) until
  history is imported.
- Budget alerts dedupe per agent per UTC day across processes via `AgentUsage.BudgetAlertSent`.
- The model price table in `internal/modelprovider/pricing.go` must be updated by hand.
- Phase C: `notifier.EventDrawdownAlert` (`drawdown.alert`) is still never emitted by the worker; `drawdown`
  triggers come only from cmd/agent's sweeper. The market watcher backfills at most 1000 1m candles
  (`ListKline`), so a `volatility_spike` rule evaluates against a shorter-than-24h baseline (min 60 candles)
  until enough live candles accumulate, and windows > ~999 min need live candles before `pct_move` works.
  No market rule fires while its symbol's subscription is down. A trigger whose enqueue fails after its
  cooldown key was taken is lost for that cooldown (logged) rather than risking a double run.

## Safety notes (this executes real trades with real money)

- `Mode` (per-strategy) and the process-wide `MODE` env var form a dual-layer guard — both default to the
  safest tier (`dryrun`) and both must independently agree before a strategy can execute live.
- `MODE=live` requires `CONFIRM_LIVE=true` at worker startup, and requires `Testnet=false`.
- Never let a strategy's effective mode silently downgrade past `live` — the engine refuses the cycle
  instead, by design (see `app/handler/tasks/strategy/handler.go`'s `gateMode`).
- Agent triggers (Phase C) only enqueue `agent:run` tasks. The worker's side is the fire-and-forget
  `AgentEventBridge` behind a `MultiNotifier` - it can't block or fail a trading cycle; nothing else in the
  trading path changed. Runaway protection: per-trigger cooldowns (Redis `SET NX PX`), chain depth <= 3, no
  agent twice in a chain, one chain run per (target, source run); the budget guard still bounds cost.
- `cmd/agent` must only ever get `exchange.ReadOnlyClient`. No agent tool can place/cancel orders or touch
  settings/credentials, and no agent tool can change a live-mode or productive strategy's source, mode or
  status. The strategy-writing tools are `save_strategy_script` (creates; updates backtest drafts),
  `deploy_to_testing` (non-live/non-productive, in-scope, behind the Go deploy gate), `create_challenger`
  (dryrun/testing clones) and `create_strategy` - all force Mode backtest/dryrun and Status testing.
- The ONLY path that changes a live strategy's code is `agent:apply_proposal` (cmd/agent) after an operator
  `POST /api/proposals/{id}/approve` over authenticated REST; it writes `script_source` only (never Mode or
  Status), under the `strategy-cycle:<id>` lock, when the strategy is flat. Deploy gate numbers are computed in
  Go from persisted backtest runs with thresholds from `DeployGateConfig` - the model can't supply either.
- Only effective-`live` (real client) and `paper` (testnet client) worker cycles can place exchange orders.
  Effective-`dryrun` cycles run on `exchange.DryRunExchange`, which reaches the real client only through
  `exchange.ReadOnlyClient`; effective-`backtest` cycles are refused and not re-enqueued (fix-01). Don't
  route dryrun to the real engine or give the dryrun stack an unwrapped client.
