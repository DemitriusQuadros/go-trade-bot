# Spec backend-04 — API Consistency Fixes (Casing, Equity Curve, Report Serving)

## Overview

Three concrete gaps surfaced while the `frontend-specialist` agent built its specs against the real API,
each verified directly against current code (not taken on the coordinator's summary alone): a JSON-casing
inconsistency on the two oldest handler packages, a computed-but-never-persisted equity curve the backtest
results chart needs, and a report file path with no route that actually serves it. All three are additive
fixes — new DTOs, one new column, one new route — none require redesigning existing working code.

## Gap 1 — JSON Casing Inconsistency (`strategy`, `signal` handlers)

### Current Behavior (verified)

- `app/entities/strategy.go:32-53` (`Strategy`) and `app/entities/signal.go:21-53` (`Signal`, `Order`) have
  **zero `json:` struct tags** anywhere — confirmed by full-file read of both. Go's default JSON marshaling
  therefore emits the Go field names verbatim: `ID`, `Name`, `Description`, `StrategyName`, `Status`,
  `Mode`, `MonitoredSymbols`, `StrategyConfiguration`, `CreatedAt`, `UpdatedAt` for `Strategy`; `Symbol`,
  `StrategyID`, `Status`, `Orders`, and each `Order`'s `BrokerOrderID`/`StopLossOrderID`/`EntryPrice`/etc.
  for `Signal`.
- Four handler methods encode these raw entities directly with no DTO in between:
  `app/handler/web/strategy/handler.go:113-120` (`GetAll`), `:157-174` (`GetById`), `:191-213`
  (`PatchStatus` — the coordinator's summary didn't mention this one, but it has the identical bug:
  `json.NewEncoder(w).Encode(strat)` at line 212 on the raw `entities.Strategy` returned by
  `UseCase.UpdateStatus`), and `:219-241` (`PatchMode`, same bug at line 240). `app/handler/web/signal/handler.go:69-92`
  (`GetAll`, covering the unfiltered and both `?status=` filtered variants — all three share the same
  `json.NewEncoder(w).Encode(signals)` call at line 91) and `:94-111` (`GetById`, line 110).
- Every other handler package in the API already uses an explicit response DTO with `json:"snake_case"`
  tags: `app/handler/web/backtest/dto.go` (`BacktestRunResponse`), `app/entities/optimizationrun.go`
  (`json:"..."` tags directly on the entity, since `OptimizationRun` was built fresh in Phase 4 with tags
  from the start), `app/entities/backtestrun.go` (same — tagged from Phase 2), `app/entities/strategyperformancesnapshot.go`
  and the `StrategyPerformance` DTO (Phase 3/4, tagged). **`strategy` and `signal` are specifically the two
  oldest handler packages (Phase 1), predating the snake_case convention every later phase established.**
  The existing request-side `StrategyDto` (`app/handler/web/strategy/dto.go`) already uses snake_case for
  `POST`/`PUT` bodies — so today's API is inconsistent *within a single resource*: writing a strategy uses
  `snake_case`, reading it back uses `PascalCase`.

### Target Behavior

```go
// app/handler/web/strategy/response.go (NEW)
package handler

type StrategyConfigurationDTO struct {
    Cycle         int             `json:"cycle"`
    Configuration json.RawMessage `json:"configuration"`
}

// StrategyResponseDTO deliberately omits the legacy Algorithm field —
// vestigial per Spec 06's migration (kept in the DB for backfill history,
// never meaningful to a client going forward) — and flattens
// StrategyConfiguration's two fields to the top level's expected shape
// rather than nesting an extra layer the frontend would have to unwrap.
type StrategyResponseDTO struct {
    ID               uint                     `json:"id"`
    Name             string                   `json:"name"`
    Description      string                   `json:"description"`
    StrategyName     string                   `json:"strategy_name"`
    Status           string                   `json:"status"`
    Mode             string                   `json:"mode"`
    MonitoredSymbols []string                 `json:"monitored_symbols"`
    Cycle            int                      `json:"cycle"`
    Configuration    json.RawMessage          `json:"configuration"`
    CreatedAt        time.Time                `json:"created_at"`
    UpdatedAt        time.Time                `json:"updated_at"`
}

func ToStrategyResponse(s entities.Strategy) StrategyResponseDTO { /* field-by-field mapping */ }
func ToStrategyResponseList(strategies []entities.Strategy) []StrategyResponseDTO { /* maps each */ }
```

```go
// app/handler/web/signal/response.go (NEW)
package handler

type OrderResponseDTO struct {
    ID              uint      `json:"id"`
    SignalID        uint      `json:"signal_id"`
    BrokerOrderID   string    `json:"broker_order_id"`
    StopLossOrderID string    `json:"stop_loss_order_id"`
    StopLossPrice   float32   `json:"stop_loss_price"`
    EntryPrice      float32   `json:"entry_price"`
    ExitPrice       float32   `json:"exit_price"`
    Quantity        float32   `json:"quantity"`
    InvestedAmount  float32   `json:"invested_amount"`
    MarginType      string    `json:"margin_type"`
    EntryFee        float32   `json:"entry_fee"`
    ExitFee         float32   `json:"exit_fee"`
    Leverage        float32   `json:"leverage"`
    ExecutedQty     float32   `json:"executed_qty"`
    IsClosing       bool      `json:"is_closing"`
    Profit          float32   `json:"profit"`
    CreatedAt       time.Time `json:"created_at"`
    UpdatedAt       time.Time `json:"updated_at"`
}

// SignalResponseDTO omits the embedded Strategy object (Signal.Strategy),
// exposing only strategy_id — matching BacktestRunResponse's existing
// precedent of strategy_id-only, no nested strategy, for consistency
// across every list/detail response in the API.
type SignalResponseDTO struct {
    ID         uint                `json:"id"`
    Symbol     string              `json:"symbol"`
    StrategyID uint                `json:"strategy_id"`
    Status     string              `json:"status"`
    Orders     []OrderResponseDTO  `json:"orders"`
    CreatedAt  time.Time           `json:"created_at"`
    UpdatedAt  time.Time           `json:"updated_at"`
}

func ToSignalResponse(s entities.Signal) SignalResponseDTO { /* field-by-field mapping, maps Orders */ }
func ToSignalResponseList(signals []entities.Signal) []SignalResponseDTO { /* maps each */ }
```

All six affected call sites (`strategy` handler's `GetAll`/`GetById`/`PatchStatus`/`PatchMode`, `signal`
handler's `GetAll`/`GetById`) replace `json.NewEncoder(w).Encode(<raw entity>)` with
`json.NewEncoder(w).Encode(To...Response(...))`. No handler signature, route, method, or status code
changes — this is purely a response-body-shape fix.

**Why a DTO, not `json:` tags directly on the entities** (per the coordinator's explicit instruction,
restated here as the rationale): `entities.Strategy`/`entities.Signal`/`entities.Order` are GORM-mapped
domain types consumed by the repository/usecase/engine layers throughout the codebase — adding
presentation-layer JSON tags to them conflates two concerns this codebase already keeps separate everywhere
else (`StrategyDto` for requests, `BacktestRunResponse` for backtest responses). A response DTO is the
existing, established pattern; retrofitting tags onto the domain entities would be the inconsistent choice,
not the fix.

### Acceptance Criteria

1. **Given** a strategy row with `Name: "Grid Bot"`, `StrategyName: "grid"`, **when** `GET /strategy/{id}`
   is called, **then** the JSON body has a `"name"` key (not `"Name"`) and a `"strategy_name"` key (not
   `"StrategyName"`) — every key in the response is snake_case, with no PascalCase key present anywhere in
   the payload.
2. **Given** the same strategy, **when** the response is inspected, **then** it does **not** contain an
   `"algorithm"` key at all — confirming the deliberate omission of the vestigial field, not an oversight.
3. **Given** `PATCH /strategy/5/status` with `{"status": "disabled"}` succeeds, **when** the response body
   is inspected, **then** it is shaped identically to `GET /strategy/5`'s response (same `StrategyResponseDTO`,
   same snake_case keys) — closing the specific gap where `PatchStatus`/`PatchMode` were found to have the
   same bug as `GetAll`/`GetById`, not just the two the coordinator's summary named.
4. **Given** a signal with two orders, **when** `GET /signal/{id}` is called, **then** the response's
   `"orders"` array contains objects with `"entry_price"`, `"broker_order_id"`, etc. — not
   `"EntryPrice"`/`"BrokerOrderID"`.
5. **Given** `GET /signal?status=open`, **when** called, **then** the response array's shape matches
   `GET /signal` (unfiltered) and `GET /signal?status=closed` exactly — all three code paths route through
   the same `ToSignalResponseList`, so no filter variant can drift out of sync with the others.
6. **Edge case — `MonitoredSymbols`'s underlying type**: **given** `entities.Strategy.MonitoredSymbols` is
   `datatypes.JSONSlice[string]` (a GORM-specific wrapper type, not a plain `[]string`), **when**
   `ToStrategyResponse` maps it into `StrategyResponseDTO.MonitoredSymbols []string`, **then** the
   conversion produces a plain JSON array of strings (`["BTCUSDT","ETHUSDT"]`), not a nested/wrapped
   representation reflecting the GORM type's internal structure — verifying the DTO mapping actually
   unwraps this type, not just passes it through unchanged (a plain-`[]string`-typed DTO field forces the
   conversion; a naive pass-through would fail to compile only if the types truly mismatch, which is why
   this is called out as a check-worthy edge rather than assumed automatic).
7. **Edge case — empty `Configuration`**: **given** a strategy whose `StrategyConfiguration.Configuration`
   is empty/nil `datatypes.JSON`, **when** mapped to `StrategyResponseDTO.Configuration json.RawMessage`,
   **then** the response emits `"configuration":null` or `"configuration":{}` (a defined, non-crashing
   representation — exact choice left to implementation, but it must not panic on `json.RawMessage(nil)`'s
   marshaling, which Go's `encoding/json` handles as `null` by default, an acceptable outcome here).

---

## Gap 2 — Missing Equity Curve on the Backtest API

### Current Behavior (verified)

- `internal/metrics_provider/interface.go` — `BacktestMetrics.EquityCurve []EquityPoint` (already tagged
  `json:"equity_curve"`, `EquityPoint{Time time.Time \`json:"time"\`; Value float64 \`json:"value"\`}`) is
  computed by `internal/metrics_provider/cinar_adapter.go`'s `Compute` **every single time** a backtest or
  walk-forward window runs.
- `app/usecase/backtest/usecase.go:156-172` (`Run`) computes `metrics := u.metricsProvider.Compute(...)`,
  immediately uses `metrics` to build `report.BacktestReportInput{ Metrics: metrics, ... }` for HTML report
  generation (`internal/report/html_report.go:69-70` renders `run.Metrics.EquityCurve` into inline SVG for
  both the equity and drawdown charts) — and then **discards** the full `metrics` value. Only five scalar
  fields are copied onto the persisted `entities.BacktestRun` (`Sharpe`, `MaxDrawdownPct`, `WinRatePct`,
  `ProfitFactor`, `TotalTrades`, `TotalReturnPct` — confirmed at `usecase.go:187-203`); `EquityCurve` is not
  among them. The identical pattern repeats in `RunWalkForward` (`usecase.go:279-324`).
- `entities.BacktestRun` (`app/entities/backtestrun.go:9-40`) has no `EquityCurve`/`equity_curve` field of
  any kind — confirmed by full-file read.
- **Existing precedent for exactly this class of problem already exists in this codebase**:
  `app/entities/optimizationrun.go`'s own doc comment (lines 18-25) explains that `OptimizationRun` stores
  `BestMetricsJSON datatypes.JSON` — a JSON blob holding the full `metrics_provider.BacktestMetrics` —
  specifically **because** `BacktestMetrics.EquityCurve []EquityPoint` has no GORM column mapping and
  cannot be `gorm:"embedded"`. This is the exact same constraint `entities.BacktestRun` runs into; Phase 4's
  `OptimizationRun` already solved it the right way (a JSON blob column), `entities.BacktestRun` (Phase 2)
  predates that pattern and never retrofitted it.

### Target Behavior

```go
// app/entities/backtestrun.go — one new column, additive, mirrors OptimizationRun.BestMetricsJSON exactly
type BacktestRun struct {
    // ...existing fields, unchanged...

    // MetricsJSON persists the full metrics_provider.BacktestMetrics computed
    // for this run (including EquityCurve, which has no GORM column mapping
    // of its own — same constraint and same fix as OptimizationRun.BestMetricsJSON).
    // Pre-existing rows created before this column existed read back as an
    // empty/nil blob — ToRunResponse (below) returns an empty equity_curve
    // array for those, not an error.
    MetricsJSON datatypes.JSON `json:"metrics_json,omitempty" gorm:"type:jsonb"`
}
```

`app/usecase/backtest/usecase.go`'s `Run` and `RunWalkForward` both gain one additional marshal alongside
the existing `tradeLogBytes, _ := json.Marshal(tradeLog)` line:
```go
metricsBytes, _ := json.Marshal(metrics) // or wfResult.AggregateOOS for RunWalkForward
// ...
run := entities.BacktestRun{
    // ...existing fields...
    MetricsJSON: datatypes.JSON(metricsBytes),
}
```

```go
// app/handler/web/backtest/dto.go — BacktestRunResponse gains one field
type BacktestRunResponse struct {
    // ...existing fields, unchanged...
    EquityCurve []metrics_provider.EquityPoint `json:"equity_curve"`
}

func ToRunResponse(run entities.BacktestRun, includeTradeLog bool) BacktestRunResponse {
    // ...existing mapping, unchanged...
    if len(run.MetricsJSON) > 0 {
        var m metrics_provider.BacktestMetrics
        if err := json.Unmarshal(run.MetricsJSON, &m); err == nil {
            res.EquityCurve = m.EquityCurve
        }
    }
    return res
}
```

`res.EquityCurve` is populated **regardless of `includeTradeLog`** (unlike `TradeLog`, which is
conditionally included) — the equity curve is needed for the results chart on every `GET /backtest/{id}`
call, not gated behind the same flag that controls the much larger full trade list, which
`ToRunResponse`'s existing `includeTradeLog bool` parameter was introduced to keep out of the `GET
/backtest?strategy_id=` list response (per that endpoint's existing `includeTradeLog: false` call at
`handler.go:155` — the equity curve is small enough (one point per trade, typically dozens to low hundreds
of points) that this spec does **not** extend the same gating to it; it's always included.

### Acceptance Criteria

1. **Given** `AutoMigrate` runs with the new `MetricsJSON` column, **when** a fresh `POST /backtest`
   completes, **then** `GET /backtest/{id}`'s response includes a non-empty `"equity_curve"` array with one
   entry per trade (plus the leading starting-balance point `Compute` always emits), each entry having
   `"time"` and `"value"` keys.
2. **Given** the same run, **when** the equity curve's values are compared against the already-rendered
   HTML report's equity SVG (`internal/report/html_report.go`'s `renderEquitySVG`) for the same run,
   **then** they match exactly — both are sourced from the identical `metrics.EquityCurve` computed once
   per run, now persisted instead of discarded after the report is generated.
3. **Given** a `BacktestRun` row created **before** this migration (no `MetricsJSON` ever populated,
   `NULL`/empty column), **when** `GET /backtest/{id}` is called for it, **then** `"equity_curve": []` (an
   empty array, not `null` and not a `500` from a failed unmarshal) — pre-existing rows degrade gracefully,
   they don't become unreadable.
4. **Given** `GET /backtest?strategy_id=5` (the list endpoint, `includeTradeLog: false`), **when** called,
   **then** each entry in the list **still includes** its own `equity_curve` (per Target Behavior's "always
   included, not gated" rule) — this is a deliberate divergence from `TradeLog`'s gating behavior, tested
   explicitly so it isn't accidentally made consistent with the trade-log gating in a future edit.
5. **Given** a walk-forward run (`IsWalkForward: true`), **when** `GET /backtest/{id}` is called, **then**
   `equity_curve` reflects `wfResult.AggregateOOS.EquityCurve` (the concatenated out-of-sample equity curve
   across all windows, per Phase 2's walk-forward aggregation design) — not any single window's individual
   curve.
6. **Edge case — zero-trade backtest**: **given** a completed run with zero trades (a valid, previously-
   supported case per Phase 2's metrics spec), **when** `Compute` produces a flat equity curve at the
   starting balance, **then** `equity_curve` contains that flat-line representation (matching what the HTML
   report already renders for this case today), not an empty array — confirming the persisted data matches
   `Compute`'s actual existing zero-trade behavior exactly, not a new special case invented for this fix.

---

## Gap 3 — No Route Serves the HTML Report File

### Current Behavior (verified)

- `app/handler/web/backtest/handler.go:34-57` (`Handlers()`) registers exactly four routes: `POST
  /backtest`, `POST /backtest/walkforward`, `GET /backtest/{id}`, `GET /backtest` — plus, per this
  session's Phase 4 work, `POST`/`GET /backtest/{id}/montecarlo`. **No route matching `/backtest/{id}/report`
  or serving a file by path exists anywhere.**
- `entities.BacktestRun.HTMLReportPath` (e.g. `reports/Grid_Bot_BTCUSDT_42.html`) is a **relative
  filesystem path** on the `cmd/api` host — `internal/report/html_report.go:42-52` (`Generate`) constructs
  it via `filepath.Join(outputDir, fileName)` where `outputDir` defaults to `"reports"`
  (`app/usecase/backtest/usecase.go:98-99`) relative to `cmd/api`'s working directory. A browser has no way
  to resolve this path to anything — it isn't a URL, and nothing serves that directory over HTTP.
- `app/usecase/backtest/usecase.go`'s `pruneReports` (retention policy, Phase 2) calls `os.Remove(r.HTMLReportPath)`
  and sets `r.HTMLReportPath = ""` in the DB once a strategy's report count exceeds the retention limit —
  confirming `HTMLReportPath` being empty is an **expected, normal** state for older runs, not only a
  "never generated" signal.

### Target Behavior

```
GET /backtest/{id:[0-9]+}/report?token=<APIToken>
  Auth: bearer token via ?token= query parameter — same exception as backend-03's SSE endpoint,
    justified below (an <iframe src="..."> is a plain browser navigation, not a fetch() call the
    frontend controls headers for — identical constraint to EventSource, same fix).
  Returns: 200, Content-Type: text/html; charset=utf-8, the report file's raw bytes
  Errors: 404 if the BacktestRun doesn't exist, OR HTMLReportPath is empty (never generated / pruned),
    OR the file is unexpectedly missing from disk despite a non-empty path (manual deletion, disk
    issue) — all three cases return the same 404 with a JSON error body; the client cannot distinguish
    "never existed" from "existed but is gone now," and doesn't need to.
```

```go
// app/handler/web/backtest/handler.go — one new method + route
func (h *BacktestHandler) GetReport(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id, err := strconv.ParseUint(vars["id"], 10, 32)
    if err != nil {
        http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
        return
    }
    run, err := h.useCase.GetByID(r.Context(), uint(id))
    if err != nil {
        writeReportNotFound(w) // 404 JSON — see below
        return
    }
    if run.HTMLReportPath == "" {
        writeReportNotFound(w)
        return
    }
    content, err := os.ReadFile(run.HTMLReportPath)
    if err != nil {
        writeReportNotFound(w) // file missing from disk despite a recorded path — still 404, not 500
        return
    }
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write(content)
}

func writeReportNotFound(w http.ResponseWriter) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusNotFound)
    w.Write([]byte(`{"error":"report not found","message":"no HTML report is available for this backtest run"}`))
}
```

New route added to `Handlers()`: `{Pattern: "/backtest/{id:[0-9]+}/report", Action: h.GetReport, Method:
http.MethodGet}` — registered alongside the existing `/backtest/{id:[0-9]+}` pattern (both use the numeric
`{id:[0-9]+}` constraint already established by the Monte Carlo routes, avoiding any ambiguity with
`/backtest/{id}/montecarlo` or the bare `/backtest/{id}`).

**Path safety**: `run.HTMLReportPath` is never client-supplied — it's read from the database, which only
ever contains values `report.Generate` itself wrote via `filepath.Join(outputDir, fileName)` (Current
Behavior). The route takes no client-supplied path component beyond the numeric `{id}`, so there is no
path-traversal surface to defend against here beyond what already exists in `report.Generate`'s own
filename construction (out of scope for this spec — that code is unchanged).

**Token-passing exception, extending backend-03's precedent**: an `<iframe src="/backtest/5/report">`
element triggers a plain browser GET navigation — like `EventSource`, there is no API for a web page to
attach a custom `Authorization` header to an iframe's `src` navigation. This spec therefore extends
`RequireAuth`'s existing header-or-query-param fallback (already built for backend-03's SSE endpoint, not a
new mechanism) to this route as well. The frontend's `BacktestResults` page must construct the iframe's
`src` as `/backtest/{id}/report?token=<the stored token>` — the same pattern `frontend-01`'s API client
should already be using for the SSE connection, now reused for exactly one more route.

### Acceptance Criteria

1. **Given** a completed backtest run with a valid `HTMLReportPath` pointing at an existing file, **when**
   `GET /backtest/{id}/report?token=<valid>` is called, **then** the response is `200` with
   `Content-Type: text/html; charset=utf-8` and a body byte-identical to the file's on-disk contents.
2. **Given** a `BacktestRun` whose `HTMLReportPath` is empty (either never generated per Spec 07 AC#7's
   report-generation-failure case, or pruned per the retention policy), **when** the route is called,
   **then** the response is `404` with the `writeReportNotFound` JSON body — not a `500`, and not an
   attempt to `os.ReadFile("")`.
3. **Given** a `BacktestRun` with a non-empty `HTMLReportPath` but the file has been deleted from disk by
   something outside this application (manual cleanup, disk failure), **when** the route is called,
   **then** the response is still `404` with the identical body shape as Acceptance Criterion #2 — the
   client-visible behavior for "path recorded but file gone" and "no path recorded" is indistinguishable,
   by design.
4. **Given** a request for a non-existent `BacktestRun` ID entirely, **when** called, **then** the response
   is `404` — same status code and body shape as Acceptance Criteria #2/#3 (three different underlying
   causes, one observable client behavior).
5. **Given** the request omits `?token=` and has no `Authorization` header, **when** called, **then** the
   response is `401` (backend-01's standard shape) **before** any attempt to read `BacktestRun`/the file —
   an unauthenticated request never reaches the report-lookup logic at all.
6. **Edge case — `Content-Disposition`**: **given** the report is served, **when** the response headers are
   inspected, **then** no `Content-Disposition: attachment` header is set (the file must render **inline**
   in the `<iframe>`, not trigger a download prompt) — this is the specific header behavior that makes the
   `<iframe>` rendering approach work at all, called out explicitly since it's easy to get backwards.
7. **Edge case — large report file**: **given** a report with several thousand trades (Phase 2 Spec 07's
   own flagged "very large trade count" edge case, where the HTML file itself may be large), **when**
   served, **then** `os.ReadFile`'s whole-file-in-memory approach is accepted as sufficient for this
   phase's realistic homelab-scale report sizes (low megabytes at most) — streaming via `http.ServeFile`
   (which also handles range requests/conditional gets) is noted as a strictly-better alternative
   implementation worth using if convenient, but not a hard requirement this spec's acceptance criteria
   enforce.

## Out of Scope (all three gaps)

- Retrofitting `json:` tags onto `entities.Strategy`/`Signal`/`Order` directly — explicitly rejected per the
  coordinator's instruction; DTOs are the fix.
- Persisting per-window equity curves for a walk-forward run (only the aggregate OOS curve is persisted/
  exposed, matching how the rest of the walk-forward response already works, e.g. `Sharpe`/`MaxDrawdownPct`
  are aggregate-only too).
- `http.ServeFile`/range-request support for the report route — noted as a nice-to-have, not required
  (Acceptance Criterion #7).
- A "stream ticket" alternative to the query-param token exception for the report route — already decided
  against in backend-03 for the same reasoning; this spec reuses that decision rather than re-litigating it.

## Dependencies

- backend-01 (Auth Middleware) — `RequireAuth`'s header-or-query-param fallback, reused here for the report
  route exactly as backend-03 established it for SSE.
- backend-03 (SSE Realtime Endpoint) — precedent this spec's report-route auth exception directly extends,
  not a new mechanism.
- Phase 2's `app/usecase/backtest`, `internal/metrics_provider`, `internal/report`; Phase 4's
  `entities.OptimizationRun` (`BestMetricsJSON`) — the existing precedent Gap 2's `MetricsJSON` column
  mirrors exactly.
- This spec is a direct input to the `frontend-specialist` agent's reconciliation pass: `frontend-01`'s
  PascalCase-handling workaround should be replaced by consuming `StrategyResponseDTO`/`SignalResponseDTO`
  directly; `frontend-07`'s client-side equity-curve reconstruction should be replaced by reading
  `equity_curve` straight off `GET /backtest/{id}`; the `<iframe>` `src` should point at
  `GET /backtest/{id}/report?token=...`.
