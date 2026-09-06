# Spec TUI-01 — Console API Client (`cmd/console/apiclient/client.go`)

## Overview

ADR-007 (`docs/architecture/refactoring-blueprint.md` §5) replaces `cmd/console`'s entire data-access
path: today three pages and three components reach past `app/usecase`/`app/handler` straight into
`app/repository` against a live `*gorm.DB`. After this spec, `cmd/console` imports neither
`gorm.io/gorm`, `internal/db`, nor any `app/repository` package — every read and every write goes
through one `cmd/console/apiclient.Client` talking HTTP to `cmd/api` (`http://localhost:8080` by
default). This spec defines that client's full method set, derived page-by-page from PRD §4.5, plus its
degraded-mode behavior when `cmd/api` is unreachable.

**Reconciled against `docs/specs/phase-3/backend-01-tui-consumed-api-endpoints.md`**, which landed after
this spec's first draft (it did not exist in the working tree when drafting began — verified by an empty
`docs/specs/phase-3/` directory listing at that time). This revision updates every method whose shape
`backend-01` settled differently than this spec's own initial derivation from `app/entities/*.go` and
`app/handler/web/*/dto.go`; `backend-01` remains the authoritative source for anything not called out
explicitly below.

## Current Behavior (verified)

- `cmd/console/dependencies/dependencies.go:10-13` — `Dependencies{Cfg *configuration.Configuration, Db
  *gorm.DB}`. `Init()` (`dependencies.go:15-26`) calls `db.NewDatabase(cfg)` directly.
- `cmd/console/components/totalizers.go:21` — `repository.NewStrategyRepository(d.Db)` then
  `.GetStrategyPerformanceBySymbol(ctx)`, a raw-SQL join with no REST equivalent today.
- `cmd/console/components/account.go:17` — `repository.NewAccountRepository(d.Db).GetAccountByID(1)`.
  A `GET /account` REST endpoint already exists (`app/handler/web/account/handler.go:33-37`) and returns
  the same `entities.Account` shape — this call is a mechanical swap, not a gap.
- `cmd/console/components/strategylist.go:25` — `repository.NewStrategyRepository(d.Db).GetAll(ctx)`. A
  `GET /strategy` REST endpoint already exists (`app/handler/web/strategy/handler.go:44-48`) — mechanical
  swap.
- `cmd/console/pages/openorders.go:65,100,130` — instantiates `exchange.NewBinanceAdapter(p.Dependencies.Cfg)`
  directly (bypassing even the DB — a second, distinct layering violation from the ACL side) for live
  price polling, and `repository.NewSignalRepository(d.Db).GetAllOpenSignals()` for the open-position
  list. No `GET /signal/open` endpoint exists today — `GET /signal` (`app/handler/web/signal/handler.go:37-40`)
  returns **all** signals unfiltered (open and closed), which is unsuitable for Page 3's live P&L polling
  loop without excess payload and client-side filtering on every tick.
- `app/handler/web/broker/handler.go:29-38,44-53` — `GET /broker/prices?symbol=` and
  `GET /broker/klines?symbol=&interval=&limit=` already exist as thin, read-only `exchange.ExchangeClient`
  proxies. These are the correct replacement for `openorders.go`'s direct `exchange.NewBinanceAdapter`
  calls and for Page 1's sparkline/EMA20 data.
- No `app/handler/web/backtest/handler.go` exists yet in the working tree (confirmed by directory
  listing of `app/handler/web/`) — Phase 2 Spec 10 defines its target shape (`POST /backtest`,
  `GET /backtest/{id}`, `GET /backtest?strategy_id=`, `POST /backtest/walkforward`) but this is
  **design**, not verified-present code; treat Phase 4/5/6 page specs' backtest calls as depending on
  Phase 2 landing, not on code that exists today.
- No `GET /strategy/{id}/executions` or any execution-log/event-feed endpoint exists in
  `app/handler/web/strategy/handler.go` or anywhere else in the repo (confirmed by grep across
  `app/handler/web/`). Page 6 (Execution Log) has no backing endpoint at all today — see Judgment Call
  below.

## Target Behavior

```go
// cmd/console/apiclient/client.go
package apiclient

import (
    "context"
    "net/http"
    "time"
)

// Client is the sole HTTP boundary between cmd/console and the rest of the
// system (ADR-007). Every page/component in cmd/console depends on this
// interface (or the concrete *Client), never on app/repository or
// internal/exchange directly.
type Client struct {
    baseURL    string        // from Dependencies.Cfg, e.g. "http://localhost:8080"; env override API_BASE_URL
    httpClient *http.Client  // Timeout: 3s per call (see Acceptance Criteria)
    maxRetries int           // 2 retries, GET only, exponential backoff 200ms/400ms
}

func NewClient(baseURL string) *Client

// --- Connectivity -------------------------------------------------------

// Ping performs a lightweight GET /account and reports reachability only
// (ignores payload). Backs Page 1's header connection-status dot. See
// Judgment Call #1 below for why this is "console <-> cmd/api" connectivity,
// not literal Binance WebSocket status.
func (c *Client) Ping(ctx context.Context) error

// --- Account (Page 1) -----------------------------------------------------

func (c *Client) GetAccount(ctx context.Context) (AccountView, error) // GET /account

// --- Strategies (Page 1 right panel, Page 2) -------------------------------

func (c *Client) ListStrategies(ctx context.Context) ([]StrategyView, error) // GET /strategy
func (c *Client) GetStrategy(ctx context.Context, id uint) (StrategyView, error) // GET /strategy/{id}
func (c *Client) ListStrategyPerformance(ctx context.Context) ([]StrategyPerformanceView, error)
    // GET /strategy/performance — per backend-01 §2, this endpoint takes NO
    // query filter; it returns EVERY (strategy, symbol) performance row in
    // one call (StrategyRepository.GetStrategyPerformanceBySymbol's existing
    // shape, JSON-tagged snake_case per backend-01's entities.StrategyPerformance
    // update). Page 2's detail overlay filters the returned slice client-side
    // by StrategyView.Name to find the rows for the selected strategy — this
    // spec's earlier draft assumed a `?strategy_id=` filter param that
    // backend-01 does not define; corrected here.

// UpdateStrategyStatus flips enabled/disabled ([e]/[d] on Page 2).
// UpdateStrategyMode cycles Dry-Run -> Paper -> Live ([r] on Page 2).
// Per backend-01 §3 (resolved): both are narrow PATCH endpoints, not a
// full-object PUT — the blueprint §8 open question is now closed.
func (c *Client) UpdateStrategyStatus(ctx context.Context, id uint, status string) (StrategyView, error)
    // PATCH /strategy/{id}/status  body: {"status": "productive"|"testing"|"disabled"}
    // Returns the updated StrategyView so the caller can refresh its local
    // row without a separate GetStrategy round-trip.
func (c *Client) UpdateStrategyMode(ctx context.Context, id uint, mode string) (StrategyView, error)
    // PATCH /strategy/{id}/mode  body: {"mode": "backtest"|"dryrun"|"paper"|"live"}
    // backend-01 §3 AC#6/Target Behavior: this persists immediately but the
    // new mode only takes effect at the strategy's NEXT scheduled cycle
    // (Phase 1's gateMode re-reads Strategy.Mode fresh every cycle) — it is
    // NOT a mid-cycle interrupt. See tui-03's updated Acceptance Criteria.

// --- Market data (Page 1 pairs grid, Page 3 live price polling) -----------

func (c *Client) ListTickerPrices(ctx context.Context, symbol string) ([]TickerPriceView, error)
    // GET /broker/prices?symbol= — existing endpoint, mechanical wrap.
func (c *Client) ListKlines(ctx context.Context, symbol, interval string, limit int) ([]CandleView, error)
    // GET /broker/klines?symbol=&interval=&limit= — existing endpoint, mechanical wrap.
    // EMA20 for Page 1's color coding is computed CLIENT-SIDE from CandleView.Close
    // by cmd/console/components/sparkline.go, not fetched from a dedicated
    // indicator endpoint — see Judgment Call #3.

// --- Positions (Page 3) ----------------------------------------------------

func (c *Client) GetOpenSignals(ctx context.Context) ([]SignalView, error)
    // GET /signal?status=open — per backend-01 §1 (resolved): a query-param
    // filter on the EXISTING /signal route, not a new /signal/open path —
    // backend-01's Current Behavior section documents why a literal new path
    // segment was rejected (gorilla/mux registration-order collision risk
    // with /signal/{id}). This spec's earlier draft assumed the literal-path
    // form; corrected here. Replaces openorders.go:130's direct
    // repository.GetAllOpenSignals() call.
func (c *Client) GetAllSignals(ctx context.Context) ([]SignalView, error)
    // GET /signal (status omitted) — used by Page 6's poll-and-diff synthesis
    // (tui-07), which needs to observe closed signals too, not just open ones.
    // GET /signal?status=closed also exists (backend-01 AC#3) if a
    // closed-only view is ever needed instead of the full unfiltered set.
func (c *Client) CloseSignal(ctx context.Context, id uint) error
    // POST /signal/close/{id} — EXISTING endpoint (handler.go:34), client-side wiring only.

// --- Backtest (Pages 4/5) — depends on Phase 2 Spec 10 landing --------------

func (c *Client) RunBacktest(ctx context.Context, req RunBacktestRequest) (BacktestRunView, error)
    // POST /backtest — synchronous per Spec 10 AC#6; Client sets a LONG timeout
    // override for this one call (see Acceptance Criteria #6), not the default 3s.
    // Body per backend-01 §4 (finalized): {strategy_id, symbol, timeframe,
    // start_date, end_date} — single `symbol` string, NOT a list; there is no
    // backend-supported "All symbols" batch-run in one call (see tui-05's
    // updated handling of PRD's "or All symbols from strategy config" wording).
    // Distinguishes 422 (data prerequisite unmet — no imported candles for
    // the requested symbol/timeframe) from 400 (bad request shape) per
    // backend-01 AC#7 — callers must handle both, not just a generic error.
func (c *Client) RunWalkForward(ctx context.Context, req WalkForwardRequest) (BacktestRunView, error)
    // POST /backtest/walkforward — same long-timeout treatment, more so (Spec 10 AC#6: "multiple minutes").
    // Body per backend-01 §4 (finalized): {strategy_id, symbol, timeframe,
    // total_range: {from, to}, train_window_days, test_window_days, step_days}.
func (c *Client) GetBacktest(ctx context.Context, id uint) (BacktestRunView, error) // GET /backtest/{id}
func (c *Client) ListBacktests(ctx context.Context, strategyID uint) ([]BacktestRunView, error)
    // GET /backtest?strategy_id= — Page 4's "recent backtests" list.

// --- Execution log (Page 6) — NO KNOWN BACKING ENDPOINT, see Judgment Call #4 --

func (c *Client) ListStrategyExecutions(ctx context.Context, since time.Time) ([]ExecutionEventView, error)
    // Proposed GET /strategy/executions?since=RFC3339 — net-new, NOT in the
    // blueprint's known endpoint list (GET /signal/open, GET /strategy/performance,
    // PATCH /strategy/{id}/status). Flagged for reconciliation.
```

```go
// cmd/console/apiclient/errors.go
package apiclient

// ErrUnreachable wraps any network-level failure (connection refused, DNS,
// timeout) distinctly from a well-formed non-2xx HTTP response, so callers
// (pages) can distinguish "cmd/api is down" (show disconnected state, keep
// last-known-good data on screen) from "cmd/api returned 404/500" (show an
// inline error for that one panel, other panels keep working).
type ErrUnreachable struct{ Cause error }
func (e ErrUnreachable) Error() string
func (e ErrUnreachable) Unwrap() error

// ErrAPI wraps a non-2xx response with status code and body.
type ErrAPI struct{ StatusCode int; Body string }
func (e ErrAPI) Error() string
```

### View types vs. entities

`apiclient` defines its own `*View` structs (`AccountView`, `StrategyView`, `SignalView`, etc.) rather
than importing `app/entities` directly. This is a deliberate boundary, not incidental duplication:
`cmd/console` must not import `app/entities` (which itself imports `gorm.io/datatypes` and pulls in the
same dependency graph ADR-007 is trying to sever the console from). `apiclient`'s `*View` types are
plain structs with `json` tags mirroring the wire shape; field names and types otherwise track the
corresponding `entities.*`/`*Dto` structs 1:1 wherever a shape is already settled (`AccountDto`,
`StrategyDto`), and are this spec's own proposal where the backend endpoint is net-new
(`SignalView`, `StrategyPerformanceView`, `ExecutionEventView`).

## Acceptance Criteria

1. **Given** `cmd/api` is reachable and returns 200, **when** any `Client` method is called, **then** the
   response body is decoded into the corresponding `*View` type and returned with a `nil` error.
2. **Given** `cmd/api` is unreachable (connection refused — e.g. the API process is not running), **when**
   any `Client` method is called, **then** it returns `ErrUnreachable` after at most `maxRetries` (2)
   retries with exponential backoff (200ms, 400ms), never panics, and never blocks longer than
   `3 * (200+400)ms ≈ 1.8s` total for a default-timeout call.
3. **Given** `Client.Ping` fails with `ErrUnreachable`, **when** `dashboard.go` renders its header bar,
   **then** the connection-status indicator shows red/disconnected (PRD §4.5 Page 1) and every other panel
   on the page renders its **last successfully fetched** data (if any) rather than blanking to empty —
   graceful degradation, not a crash or a frozen blank screen.
4. **Given** `cmd/api` returns a non-2xx status (e.g. `404` from `GET /strategy/{id}` for a deleted
   strategy), **when** the calling page handles the returned `ErrAPI`, **then** it renders
   `components.Error(err)` (existing pattern, `cmd/console/components/error.go`, kept unchanged) scoped to
   that one panel — sibling panels on the same page continue rendering normally.
5. **Given** a GET request that times out after 3s with no response, **when** `Client` retries, **then**
   retries apply **only to GET methods** — `POST`/`PATCH` calls (`CloseSignal`, `UpdateStrategyStatus`,
   `RunBacktest`, etc.) are never automatically retried, to avoid double-submitting a write (e.g. closing
   a position twice, or triggering two backtest runs) on a slow-but-eventually-successful first attempt.
6. **Given** `RunBacktest`/`RunWalkForward` are long-running per Phase 2 Spec 10 AC#6 ("may take multiple
   minutes... synchronous"), **when** either is called, **then** `Client` uses a distinct, much longer
   timeout (e.g. 10 minutes) than the 3s default applied to every other method, and the calling page
   (`backtestlauncher.go`) is responsible for rendering the PRD's progress bar/ETA during that wait — the
   HTTP call itself has no way to report incremental progress (no SSE/WebSocket in scope, see Out of Scope).
7. **Edge case — malformed JSON response**: **given** `cmd/api` returns `200` with a body that fails to
   unmarshal into the expected `*View` type, **when** `Client` decodes it, **then** it returns a
   descriptive error (not a silent zero-value `*View`) so the calling page can distinguish "empty result"
   from "response I can't parse."
8. **Edge case — base URL configuration**: **given** no `API_BASE_URL` environment variable is set,
   **when** `apiclient.NewClient` is constructed via `dependencies.Init()`, **then** it defaults to
   `http://localhost:8080`, matching `cmd/api/main.go:66`'s hardcoded `:8080` listen address.

## Out of Scope

- Any streaming/push transport (WebSocket, SSE) from `cmd/api` to `cmd/console` — every method here is a
  discrete request/response HTTP call; pages achieve "live" updates via polling on a ticker (Page 3's
  "updated every second" requirement is a poll interval, not a push).
- Authentication/authorization headers — matches the project's current no-auth posture on every existing
  endpoint (Phase 2 Spec 10's own Out of Scope makes the same call).
- Client-side response caching beyond "last successfully fetched value held in the calling page's own
  state for degraded-mode display" (Acceptance Criterion #3) — no generic cache layer inside `apiclient`
  itself.

## Dependencies

- `docs/specs/phase-3/backend-01-tui-consumed-api-endpoints.md` — **not yet present at spec-writing time**;
  supersedes this spec's request/response shapes wherever they diverge.
- Phase 2 Spec 10 (`docs/specs/phase-2/10-backtest-persistence-and-api.md`) — `POST /backtest`,
  `GET /backtest/{id}`, `GET /backtest?strategy_id=`, `POST /backtest/walkforward` request/response shapes.
- `app/handler/web/account/dto.go`, `app/handler/web/strategy/dto.go`, `app/entities/signal.go`,
  `app/entities/strategy.go` — existing wire-shape references for `AccountView`/`StrategyView`/`SignalView`.
- Blueprint ADR-007 consequences (§5) — the two confirmed net-new endpoints (`GET /signal/open`,
  `GET /strategy/performance`) and the one open shape question (`PATCH /strategy/{id}/status` vs.
  full `PUT /strategy/{id}`).

## Judgment Calls (flagged for explicit reconciliation)

1. **"Connection status" (PRD Page 1) is redefined as console→cmd/api reachability, not literal Binance
   WebSocket state.** The PRD's exact wording is "green dot = WS connected, red = disconnected." Under
   ADR-007, `cmd/console` has no direct relationship to Binance's WebSocket at all — that connection lives
   in `internal/feed/live_feed.go` inside `cmd/worker`. There is no endpoint today (or planned in the
   blueprint) that surfaces `cmd/worker`'s live-feed WS health to `cmd/api`, let alone to the console. This
   spec proposes the pragmatic substitute: the dot reflects whether `cmd/console` can reach `cmd/api` at
   all (`Client.Ping`). If the PRD's original intent — surfacing the *worker's* WS health specifically —
   is still wanted, that requires a new `cmd/api` endpoint exposing worker health (out of scope here) and
   should be raised with the concurrent backend spec author.
2. **RESOLVED by `backend-01` §3**: Page 2's `[e]`/`[d]`/`[r]` keypresses go through narrow
   `PATCH /strategy/{id}/status` and `PATCH /strategy/{id}/mode` endpoints, not `PUT /strategy/{id}`'s
   full-object replace. `backend-01` additionally resolves blueprint §8's separate immediate-vs-next-cycle
   mode-switch question: a mode change persists immediately but only takes effect at the strategy's next
   scheduled cycle (Phase 1's `gateMode` re-reads `Strategy.Mode` fresh per cycle) — this spec's earlier
   draft of `tui-03`'s Judgment Call, which proposed "immediate" as this spec's own default absent backend
   confirmation, is superseded by this resolved answer; see `tui-03`'s updated Acceptance Criteria.
3. **EMA20 (Page 1's pair-color-coding rule) is computed in `cmd/console`, not fetched from an indicator
   endpoint.** `internal/indicators` (the `IndicatorProvider` ACL wrapping `go-talib`) is explicitly an
   internal package no outside caller should reach into, and no REST endpoint exposes indicator
   computation today. EMA is a well-known, cheap client-side formula
   (`EMA_t = Close_t * k + EMA_{t-1} * (1-k)`, `k = 2/(N+1)`) computed directly from `CandleView.Close`
   values already fetched via `GET /broker/klines` for the sparkline itself — no extra API surface needed.
   Flagging because this is presentation-layer duplication of logic that conceptually belongs to the
   `IndicatorProvider` ACL; acceptable here since the TUI's requirement (a single EMA20 for color, not a
   trading decision) is materially lower-stakes than a strategy's `ShouldLong` computation.
4. **Page 6 (Execution Log) has no backing endpoint anywhere in the blueprint or the known concurrent
   backend endpoint list.** Neither `GET /signal/open` nor `GET /strategy/performance` nor the
   `PATCH /strategy/{id}/status` open question covers a chronological, mixed-event-type feed (position
   opened/closed, signal generated, strategy cycle with no signal, system events) as PRD §4.5 Page 6
   requires. This spec proposes a net-new `GET /strategy/executions?since=` endpoint backed by
   `entities.StrategyExecution` (already persisted via `StrategyRepository.SaveExecution`, but with **no
   REST read path today** — confirmed by grep) as a partial answer, but a `StrategyExecution` row alone
   cannot represent "position opened," "position closed at profit/loss," or "signal generated" — those are
   `Signal`/`Order` lifecycle events with no execution-log framing today. Full resolution likely requires
   either a new dedicated event-log table written at each of those lifecycle points, or client-side
   synthesis by polling `GET /signal` (all) + `GET /strategy/executions` and diffing state between polls.
   This is the single biggest open gap in this spec set — see `tui-07-page-execution-log.md` for the
   detailed proposal and its own explicit caveats.
