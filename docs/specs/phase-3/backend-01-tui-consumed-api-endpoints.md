# Spec backend-01 — TUI-Consumed API Endpoints (Final Contracts)

## Overview

Per ADR-007, Phase 3's console TUI is a pure HTTP client of `cmd/api` — no direct DB/repository access.
This spec finalizes every endpoint contract the TUI's `apiclient` package depends on: two genuinely new
read endpoints (`GET /signal` filtering, `GET /strategy/performance`), the resolved PUT-vs-PATCH decision
for keypress-driven strategy actions, and the exact JSON request/response bodies for Phase 2's `/backtest*`
endpoints, which `docs/specs/phase-2/10-backtest-persistence-and-api.md` scoped structurally but left
several field-level shapes unspecified (`RunRequest`/`WalkForwardRequest` bodies were referenced by name
only). This is the contract the frontend-specialist agent's `tui-*.md` specs build against — written first,
per the coordinator's instruction.

## Current Behavior (verified)

- `app/handler/web/signal/handler.go:29-47` — routes are `POST /signal/close/{id}`, `GET /signal` (returns
  **all** signals via `UseCase.GetAll`, no status filter), `GET /signal/{id}`. No open-only filter exists.
- `app/repository/signal/repository.go:36` — `GetAllOpenSignals() ([]entities.Signal, error)` **already
  exists at the repository layer** but has no usecase or handler wiring (confirmed: no reference to it
  anywhere under `app/usecase` or `app/handler`).
- `app/handler/web/strategy/handler.go:32-60` — routes are `POST /strategy`, `POST /strategy/enqueue`,
  `GET /strategy`, `PUT /strategy/{id}`, `GET /strategy/{id}`. No performance-by-symbol endpoint exists.
- `app/repository/strategy/repository.go:57-69` — `GetStrategyPerformanceBySymbol(ctx) []entities.StrategyPerformance`
  **already exists** (raw SQL join across `orders`/`signals`/`strategies`, grouped by strategy+symbol,
  ordered by profit descending) but likewise has no usecase or handler wiring.
- `app/entities/strategyperformance.go` — `StrategyPerformance{Name, Symbol string; Profit float64; Trades int}`.
- `app/handler/web/strategy/handler.go:104-137` (`Put`) — requires a full `StrategyDto` body
  (`app/handler/web/strategy/dto.go`: `Name, Description, MonitoredSymbols, Status, Algorithm/StrategyName,
  Cycle, Configuration`) — every field must be resupplied even to toggle one of them.
- **Route-collision risk, verified against `gorilla/mux`'s registration-order matching**: `mux` matches
  routes in the order `Handlers()` returns them, not by specificity. `app/handler/web/strategy/handler.go:44-58`
  currently registers `GET /strategy` then `PUT /strategy/{id}` then `GET /strategy/{id}` — a naive addition
  of `GET /strategy/performance` **after** `GET /strategy/{id}` in this slice would be silently shadowed:
  `GET /strategy/performance` would match `GET /strategy/{id}` first, with `{id}="performance"`, and fail at
  `strconv.Atoi("performance")` (`handler.go:141`) with a `400 Invalid ID` instead of ever reaching the
  intended handler. The same risk applies to `/signal` if a literal-path `/signal/open` route were chosen
  instead of a query-parameter filter — this is precisely why this spec picks the query-parameter design
  below, not a new literal path segment.

## Target Behavior

### 1. `GET /signal?status=open` (query-parameter filter on the existing route — no new path)

```
GET /signal?status=open
  Auth: none (matches existing endpoints — no auth layer exists anywhere in this API today)
  Returns: 200, [] entities.Signal  (same shape as unfiltered GET /signal, just pre-filtered server-side)
  Errors: 400 if status is present but not "open" or "closed"
  Notes: status omitted => current behavior unchanged (all signals, both open and closed).
         Chosen over a literal `/signal/open` path specifically to avoid the mux registration-order
         collision risk with `/signal/{id}` documented above — a query parameter cannot collide with a
         path-segment pattern under any registration order.
```

`app/handler/web/signal/handler.go`'s `GetAll` gains a query-param branch:
```go
func (h *SignalHandler) GetAll(w http.ResponseWriter, r *http.Request) {
    status := r.URL.Query().Get("status")
    var (
        signals []entities.Signal
        err     error
    )
    switch status {
    case "":
        signals, err = h.UseCase.GetAll(r.Context())
    case "open":
        signals, err = h.UseCase.GetAllOpen(r.Context()) // NEW usecase method, wraps GetAllOpenSignals
    case "closed":
        signals, err = h.UseCase.GetAllClosed(r.Context()) // NEW, symmetric — see AC#3
    default:
        http.Error(w, "invalid status filter", http.StatusBadRequest)
        return
    }
    // ...existing error handling / JSON encoding unchanged...
}
```
`SignalUseCase.GetAllOpen` is a thin new method wrapping the already-existing
`SignalRepository.GetAllOpenSignals()` — the same pattern `GetAll`/`GetByID` already follow.

### 1a. `entities.Order` gains `StopLossPrice` (resolved judgment call, sign-off 2026-09-05)

`tui-04-page-positions.md`'s Positions page needs to display the resting stop-loss price inline (PRD
§4.5 Page 3: "Stop-loss and take-profit prices shown inline if set as exchange orders"), but `Order` today
only persists `StopLossOrderID` (`app/entities/signal.go:36-38`) — the exchange order's ID, not its price.
`app/usecase/signal/usecase.go`'s `submitStopLoss` (line ~211) already computes the stop price locally at
submission time (`stopLossOrderID, stopLossPrice := s.submitStopLoss(...)`, line 154) but discards
`stopLossPrice` instead of persisting it.

**Decision**: add `StopLossPrice float32` to `entities.Order` (additive migration, `NULL`/`0` for historical
rows predating this field) and populate it at the same call site that already computes the value — no new
exchange API call, no polling, no rate-limit exposure. Rejected alternative: resolving the price via a live
`ExchangeClient.GetOrder` lookup per open position on every Positions-page refresh (PRD requires this to
update "every second") — that would mean one exchange API call per open position per second, a real
rate-limit risk with more than a handful of concurrent positions, for a value that's already computed and
thrown away today.

`GET /signal?status=open`'s response shape (and the unfiltered `GET /signal`) both gain this field
automatically once it's added to the entity — no separate endpoint change needed.

**`TakeProfitPrice` is explicitly NOT a backend field.** Unlike stop-loss, take-profit was never
implemented as a real exchange order in Phase 1 (Spec 03 covers stop-loss only) — it's a software-evaluated
exit condition inside each ported strategy's `UpdatePosition` hook, driven by a `take_profit_pct` key in
that strategy's `Configuration` JSON (confirmed: no `TakeProfit*` field exists anywhere under
`app/entities/`). `tui-04-page-positions.md`'s take-profit display should be computed **client-side** from
`EntryPrice * (1 + take_profit_pct/100)` using the strategy's already-fetched `Configuration` — the same
"derive on the client from raw data already in hand" pattern already accepted for `tui-02`'s EMA20
calculation. No backend change needed for this half of the display.

### 2. `GET /strategy/performance` (net-new route, registered before `/strategy/{id}`)

```
GET /strategy/performance
  Auth: none
  Returns: 200, [] { name: string, symbol: string, profit: number, trades: number }
           (JSON-tagged mirror of entities.StrategyPerformance — see AC#4 for field naming)
  Errors: 500 on repository failure
  Notes: MUST be registered before GET /strategy/{id} in StrategyHandler.Handlers()'s returned slice
         (see Current Behavior's collision risk) — this ordering requirement is the single most
         important implementation note in this spec.
```

`entities.StrategyPerformance` gains JSON tags (currently untagged, per Current Behavior — Go's default
JSON marshaling would emit `Name`/`Symbol`/`Profit`/`Trades` capitalized, which is inconsistent with every
other DTO in this codebase using `snake_case` `json:"..."` tags, e.g. `StrategyDto`). This spec adds:
```go
type StrategyPerformance struct {
    Name   string  `json:"name"`
    Symbol string  `json:"symbol"`
    Profit float64 `json:"profit"`
    Trades int     `json:"trades"`
}
```

### 3. PUT vs PATCH for keypress-driven strategy actions — resolved (judgment call)

**Decision: add narrow `PATCH /strategy/{id}/status` and `PATCH /strategy/{id}/mode` endpoints. Keep
`PUT /strategy/{id}` unchanged for full-object edits.**

```
PATCH /strategy/{id}/status
  Auth: none
  Body: { "status": "productive" | "testing" | "disabled" }
  Returns: 200, updated entities.Strategy
  Errors: 400 (invalid status value or malformed body), 404 (strategy not found)
  Side effects: none beyond persistence — does NOT re-enqueue or terminate; the existing
    HandleStrategyTask cycle (Phase 1) already checks Status == Disabled per-cycle and calls
    Terminate at that transition (Phase 1 Spec 05 AC#8) — this endpoint only needs to persist
    the new value, the existing polling/task logic handles the behavioral consequence.

PATCH /strategy/{id}/mode
  Auth: none
  Body: { "mode": "backtest" | "dryrun" | "paper" | "live" }
  Returns: 200, updated entities.Strategy
  Errors: 400 (unparseable mode — reuses strategies.ParseExecutionMode, Phase 1 Spec 10), 404
  Side effects: none beyond persistence — Phase 1's per-cycle gateMode (app/handler/tasks/strategy/handler.go)
    already re-evaluates the effective mode fresh every cycle by reading nStrategy.Mode from the DB, so a
    mode change here takes effect at the strategy's next scheduled cycle, never mid-cycle (this is the
    existing, un-changed answer to the blueprint's open question about immediate-vs-next-cycle mode
    switching — this spec does not attempt to make a switch take effect mid-cycle, since Phase 1's engine
    has no mechanism to interrupt an in-flight Engine.Run call, and building one is out of scope here).
```

**Rationale**: rejected the alternative (require the TUI to `PUT` the full `StrategyDto` for a one-key
toggle) for two reasons: (1) it forces `cmd/console/apiclient` to hold and resend the strategy's entire
`Configuration` JSON blob on every keypress, creating exactly the kind of read-modify-write race the
blueprint's own note about "assembling a full `StrategyDto` client-side" was worried about — if the TUI's
in-memory copy of a strategy is even slightly stale (another operator session, or a backtest-triggered
config change in some future phase) when a `PUT` full-replace fires, it silently clobbers the missed
change; (2) `PATCH` payloads map directly and unambiguously to the two specific fields Page 2's `[e]`/`[d]`/
`[r]` keybindings mutate — no other field is ever touched by a keypress action, so a full-replace endpoint
is semantically the wrong tool even before considering the race.

`app/usecase/strategy/usecase.go` gains:
```go
func (u StrategyUseCase) UpdateStatus(ctx context.Context, id uint, status entities.StrategyStatus) (entities.Strategy, error)
func (u StrategyUseCase) UpdateMode(ctx context.Context, id uint, mode string) (entities.Strategy, error)
```
Both fetch the current row by ID, mutate only the targeted field, re-run the **existing**
`validateStrategy`-equivalent checks that still apply (status/mode value validity; nothing else, since no
other field changed), and save — this is standard partial-update usecase shape, not a new pattern for this
codebase.

### 4. `/backtest*` request/response bodies — finalized (fills the gap in Phase 2 Spec 10)

Phase 2's `docs/specs/phase-2/10-backtest-persistence-and-api.md` names the four routes and references
`RunRequest`/`WalkForwardRequest` by type name only. This spec finalizes their JSON shapes since the TUI's
Backtest Launcher (Page 4) must construct these bodies exactly:

```
POST /backtest
  Body: {
    "strategy_id": number,
    "symbol": string,
    "timeframe": string,       // e.g. "1m" — must match an imported (symbol, timeframe) pair (Phase 2 Spec 01/02)
    "start_date": string,      // RFC3339
    "end_date": string         // RFC3339
  }
  Returns: 200, entities.BacktestRun (full — metrics, HTMLReportPath, Passed; see Phase 2 Spec 10 AC#1/#5
           for the ProfitFactor +Inf / "Infinity" serialization rule, unchanged here)
  Errors: 400 (strategy not found, symbol/timeframe not imported, invalid date range — end before start),
          422 (insufficient stored candle data to cover the requested range — distinct from 400 since the
          request is well-formed but the data prerequisite isn't met; the TUI should render this
          differently from a validation error, prompting "run cmd/candleimport first" rather than "fix
          your input")
  Side effects: triggers a synchronous backtest run (Phase 2 Spec 10 AC#6 — request blocks until complete)

POST /backtest/walkforward
  Body: {
    "strategy_id": number,
    "symbol": string,
    "timeframe": string,
    "total_range": { "from": string, "to": string },  // RFC3339, matches engine.TimeRange (Phase 2 Spec 08)
    "train_window_days": number,
    "test_window_days": number,
    "step_days": number
  }
  Returns: 200, entities.BacktestRun (IsWalkForward=true; TradeLogJSON embeds per-window breakdown per
           Phase 2 Spec 10 AC#7)
  Errors: 400, 422 (same categories as POST /backtest)

GET /backtest/{id}
  Returns: 200, entities.BacktestRun | 404

GET /backtest?strategy_id={id}
  Returns: 200, [] entities.BacktestRun (history list for Page 4's "recent backtests" panel — Phase 2
           Spec 10 didn't specify sort order: this spec fixes it as descending by CreatedAt, most recent
           first, matching how a "recent backtests" panel is expected to read)
  Errors: 400 if strategy_id is missing (this endpoint requires it — no unfiltered "all backtest runs ever"
          mode is exposed, since that has no clear TUI use case and would need pagination this spec doesn't
          otherwise need to define)
```

## Acceptance Criteria

1. **Given** 5 signals exist, 2 open and 3 closed, **when** `GET /signal?status=open` is called, **then**
   the response contains exactly the 2 open signals, in the same shape `GET /signal` (unfiltered) would
   render them.
2. **Given** `GET /signal?status=bogus`, **when** called, **then** the response is `400` with a body
   identifying `status` as the invalid field — not a silent fallback to "all signals."
2a. **Given** a signal with an active `STOP_MARKET` order, **when** `submitStopLoss` succeeds, **then**
   `Order.StopLossPrice` is persisted with the same value used to place the exchange order — verified by
   comparing the persisted field against the price argument passed to `ExchangeClient.PlaceOrder`'s
   `StopPrice` field in the same call. **Given** a historical `Order` row from before this field existed,
   **when** it's read back, **then** `StopLossPrice` is `0`/zero-value, not an error — the TUI's Positions
   page (`tui-04`) must treat `0` as "no resolved stop price available" (e.g. for orders predating this
   migration), not render a misleading `$0.00` stop price.
3. **Given** `GET /signal?status=closed` — added for symmetry even though only `open` was explicitly
   requested by the blueprint — **when** called, **then** it returns exactly the closed signals; this
   confirms the filter branch handles both explicit values, not just `open`, avoiding a future asymmetric
   surprise if the TUI or another client wants closed-only later.
4. **Given** `GET /strategy/performance` is called, **when** the router matches the request, **then** it
   reaches `StrategyHandler`'s dedicated handler, **not** `GetById` with `id="performance"` — this is
   the acceptance criterion that directly tests the registration-order requirement from Current Behavior;
   a regression here would surface as a `400 Invalid ID` response instead of the performance data.
5. **Given** `PATCH /strategy/5/status` with `{"status": "disabled"}`, **when** processed, **then** only
   the `Status` column changes for strategy `5` — `Configuration`, `MonitoredSymbols`, `Mode`, and every
   other field remain byte-identical to their pre-request values (verified by comparing a full `GET
   /strategy/5` before and after, diffing only `status` and `updated_at`).
6. **Given** `PATCH /strategy/5/mode` with `{"mode": "paper"}` while `Testnet` config is `false`
   process-wide, **when** processed, **then** the `PATCH` itself **succeeds** (it only persists the DB
   value) — the Testnet/mode consistency guard (Phase 1 Spec 10 AC#6, enforced in
   `StrategyProcessor.gateMode`) is a **per-cycle execution-time** check, not a save-time validation, so
   this endpoint does not duplicate that logic; it would simply mean the strategy's next cycle is refused
   with a `strategy.error` webhook (Phase 1's existing behavior), which is the correct layering — the TUI
   should not be blocked from *setting* a mode value by a runtime execution-environment concern the API
   layer has no business enforcing at write time.
7. **Given** `POST /backtest` for a `(symbol, timeframe)` pair with zero imported candles, **when**
   processed, **then** the response is `422`, not `400` and not a `500` from an unhandled empty-`ReplayFeed`
   condition bubbling up as an unrelated panic/error.
8. **Given** `POST /backtest` with `end_date` before `start_date`, **when** processed, **then** the
   response is `400` before any engine work is attempted.
9. **Edge case — `GET /backtest?strategy_id=` omitted**: **given** the query parameter is absent entirely,
   **when** called, **then** the response is `400`, not an unfiltered dump of every backtest run for every
   strategy (explicitly rejected in Target Behavior above).

## Out of Scope

- Any authentication/authorization layer — matches the existing posture of every endpoint in this API.
- Pagination on `GET /backtest?strategy_id=` — accepted as unnecessary at Phase 2/3's realistic per-strategy
  run volume (dozens, not thousands).
- A `DELETE /backtest/{id}` endpoint (manually purging old runs beyond the automatic retention policy,
  Phase 2 Spec 07) — not requested by any Phase 3 TUI page description; add only if the frontend-specialist
  agent's Page 5 spec surfaces a concrete need.

## Dependencies

- Phase 1's `app/repository/signal.SignalRepository.GetAllOpenSignals`, `app/repository/strategy.StrategyRepository.GetStrategyPerformanceBySymbol` — both already exist, reused unmodified.
- Phase 1's `strategies.ParseExecutionMode` (Spec 06) — reused by `PATCH /strategy/{id}/mode`'s validation.
- Phase 2's `docs/specs/phase-2/10-backtest-persistence-and-api.md` — this spec finalizes, does not
  replace, that spec's route list and orchestration design.
