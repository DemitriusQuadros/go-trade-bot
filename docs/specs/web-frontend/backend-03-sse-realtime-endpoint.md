# Spec backend-03 — SSE Realtime Endpoint (`GET /stream/dashboard`)

## Overview

Per ADR-010, real-time updates for the Dashboard/Positions views ship as a Phase-E polish item on top of an
initial polling-based frontend — but the contract is written now so the frontend's `useSSE` hook has a
stable target to build against later without a second round of API design. This spec defines a single,
consolidated SSE endpoint (one connection per browser tab, multiple event types), the server-side
poll-once/fan-out-to-N-clients broadcaster that backs it, and resolves the concrete problem the coordinator
flagged: browsers' native `EventSource` API cannot set custom headers, so `backend-01`'s
`Authorization: Bearer` scheme cannot be used as-is for this one endpoint.

## Current Behavior (verified)

- No streaming/push endpoint of any kind exists anywhere in `cmd/api` today (confirmed: no
  `text/event-stream` content type, no `http.Flusher` usage, no long-lived-connection handling in any
  handler file).
- `internal/feed.LiveFeed` (Phase 1) is the only real-time data mechanism in the codebase, and it lives
  entirely inside `cmd/worker` — `cmd/api` has no access to it and no existing mechanism to learn about
  price changes as they happen (confirmed by the web-frontend blueprint's own §5 audit, restated here since
  this spec is the first one to actually build against that gap).
- `app/handler/web/broker/handler.go` (`GET /broker/prices`, `GET /broker/klines`) already wraps
  `ExchangeClient.ListTickerPrices`/`ListKline` for one-shot REST reads — this spec's polling loop reuses
  the same `ExchangeClient` dependency already wired into `cmd/api`'s `fx` graph (`modules.ExchangeModule`,
  confirmed present in `cmd/api/main.go:41`), not a new exchange-access path.
- `app/repository/signal.SignalRepository.GetAllOpenSignals()` (Phase 1) already exists and is already
  exposed via `GET /signal?status=open` (Phase 3 backend-01) — this spec's position-P&L computation reuses
  it directly.

## Target Behavior

```
GET /stream/dashboard?token=<APIToken>
  Auth: bearer token via ?token= query parameter — see "Token-passing exception" below
  Content-Type: text/event-stream
  Response: an open, long-lived HTTP connection emitting Server-Sent Events, never a normal
    finite JSON body — no other Content-Type/status code semantics apply once the stream opens.
```

### Event types

```
event: price_update
data: {"symbol": "BTCUSDT", "price": 50123.45, "timestamp": "2026-09-06T12:00:00Z"}

event: position_update
data: {"signal_id": 12, "symbol": "BTCUSDT", "strategy_id": 3, "entry_price": 49800.0,
       "current_price": 50123.45, "unrealized_pnl": 12.34, "unrealized_pnl_pct": 0.65,
       "quantity": 0.01, "opened_at": "2026-09-06T10:00:00Z"}

event: heartbeat
data: {"timestamp": "2026-09-06T12:00:15Z"}
```
One `price_update` event per distinct symbol currently monitored by any enabled strategy, and one
`position_update` event per currently-open signal — **not** a single batched payload containing all symbols/
positions at once, so the frontend can update individual chart series/position cards independently without
re-rendering the entire view on every tick. `heartbeat` fires on a fixed interval (15s) regardless of
whether any price/position actually changed, purely so the client can detect a silently-dead connection
(see Reconnection below).

### Server-side broadcaster

```go
// cmd/api/webui/... or app/usecase/realtime/broadcaster.go (NEW — exact package TBD at
// implementation time; conceptually a usecase-layer component, not a handler)
type DashboardBroadcaster struct {
    // unexported: subscriber registry (map[chan Event]struct{} + mutex),
    // ExchangeClient, SignalRepository/SignalUseCase, StrategyRepository
    // (to resolve the current monitored-symbols set)
}

func NewDashboardBroadcaster(exchange exchange.ExchangeClient, signalRepo SignalRepository, strategyRepo StrategyRepository) *DashboardBroadcaster

// Run is the single upstream poll loop - started ONCE at cmd/api startup via
// an fx lifecycle hook, regardless of how many browser tabs are connected.
// Every `interval` tick: resolve the current distinct monitored-symbol set
// from all non-disabled strategies, fetch each via ExchangeClient.
// ListTickerPrices (one call per symbol, or a batched call if the adapter
// supports it - not specified further here, an implementation detail),
// fetch all open signals, compute unrealized P&L per position, and publish
// one event per symbol/position to every current subscriber.
func (b *DashboardBroadcaster) Run(ctx context.Context, interval time.Duration)

// Subscribe registers a new SSE client connection and returns a channel it
// should range over, plus an unsubscribe func the handler calls via defer
// when the client disconnects.
func (b *DashboardBroadcaster) Subscribe() (ch <-chan Event, unsubscribe func())
```

**This is the concrete mechanism behind ADR-010's "one upstream poll fans out to N connected SSE clients"**:
`Run`'s loop executes exactly once per `interval` tick no matter how many browsers are watching the
Dashboard — 1 open tab or 10, the exchange/DB load is identical, which is the entire efficiency argument for
building this over N independent per-tab polling loops.

### HTTP handler

```go
// app/handler/web/realtime/handler.go (NEW)
func (h *RealtimeHandler) StreamDashboard(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming unsupported", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    fmt.Fprintf(w, "retry: 5000\n\n") // browser's native auto-reconnect delay — see Reconnection
    flusher.Flush()

    ch, unsubscribe := h.broadcaster.Subscribe()
    defer unsubscribe()

    for {
        select {
        case event := <-ch:
            fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.JSON())
            flusher.Flush()
        case <-r.Context().Done(): // client disconnected (tab closed, network drop)
            return
        }
    }
}
```

### Token-passing exception (the specific problem the coordinator flagged)

**Decision**: `RequireAuth` (backend-01) accepts the token via **either** the `Authorization: Bearer`
header **or** a `?token=` query parameter — but the query-parameter path is **only ever exercised by this
one endpoint** in practice, since every other frontend API call goes through the shared `fetch` wrapper that
always sets the header. `extractBearerToken` (backend-01) checks the header first, falling back to the
query parameter only if no header is present.

**Why this is an acceptable, narrow exception, not a general auth weakening**: browsers' native
`EventSource` constructor has no API to set custom request headers — this is a documented, unfixable
platform limitation (not a library gap this project could route around by picking a different SSE client),
so a header-only scheme makes SSE simply impossible to authenticate from a standard browser `EventSource`.
The alternatives considered and rejected:
- **A short-lived, single-use "stream ticket"** (client first calls an authenticated `POST
  /stream/dashboard/ticket` to mint a one-time token, then opens `EventSource` with that ticket) — more
  correct (the long-lived `APIToken` itself never appears in a URL/logs), but meaningfully more
  implementation complexity (a ticket store with expiry) for a homelab single-operator system where the
  `APIToken` already lives in `localStorage` in plain text anyway (per the blueprint's own ADR-009
  consequence) — the marginal security gain over the simpler query-param approach is small relative to the
  added complexity, **but this trade-off is flagged explicitly for confirmation**, not asserted as
  obviously correct (see Out of Scope / Dependencies).
- **A cookie-based session** — would require `cmd/api` to manage session state and the frontend to switch
  its entire auth model from header-based to cookie-based for this one endpoint, inconsistent with every
  other route.
- **Accepted mitigation for the chosen query-param approach**: the token in a URL is a real, if minor,
  exposure (browser history, proxy/server access logs, `Referer` headers if the SSE URL is ever
  embedded/linked elsewhere) — mitigated by (a) this being a `GET` request the frontend constructs
  programmatically and never surfaces as a clickable/bookmarkable link, and (b) recommending `cmd/api`'s
  access logging (if ever added) redact the `token` query parameter specifically, called out as a follow-up
  note for whoever implements request logging, not solved by this spec.

### Reconnection / backoff — native, not custom

Browsers' `EventSource` **automatically reconnects** on a dropped connection, using the server-supplied
`retry: 5000` value (5 seconds) as its retry delay — no client-side reconnect logic needs to be written by
the frontend for the basic case. The frontend's `useSSE` hook's own responsibility (per the blueprint's
handoff to the concurrent `frontend-specialist` work) is narrower: detect a **silently stalled** connection
(no `heartbeat` event received within roughly `2x` the 15s interval) and surface a "reconnecting..." UI
state — `EventSource` itself doesn't expose a "haven't heard anything in a while" signal, only "the
connection was explicitly closed," so the heartbeat's existence is specifically what lets the frontend
detect the stalled-but-technically-still-open case.

## Acceptance Criteria

1. **Given** a strategy monitors `BTCUSDT` and `ETHUSDT` and has one open position on `BTCUSDT`, **when** a
   client connects to `GET /stream/dashboard?token=<valid>`, **then** it receives, on the next poll tick,
   two `price_update` events (one per symbol) and one `position_update` event.
2. **Given** two browser tabs are simultaneously connected, **when** the broadcaster's poll tick fires,
   **then** exactly **one** `ExchangeClient.ListTickerPrices` call occurs per monitored symbol (not two) —
   verifying the fan-out design's core efficiency claim.
3. **Given** a request to `GET /stream/dashboard` with no `token` query parameter and no `Authorization`
   header, **when** processed, **then** the response is `401` (backend-01's standard shape) **before** the
   `Content-Type: text/event-stream` header is ever written — the connection never upgrades to a stream for
   an unauthenticated request.
4. **Given** a valid token supplied via `?token=`, **when** the same request also happens to carry an
   `Authorization` header (e.g. a non-browser test client), **then** the header takes precedence
   (`extractBearerToken`'s documented check order) — confirming the query param is strictly a fallback, not
   a competing/ambiguous path.
5. **Given** an open connection, **when** 15 seconds elapse with no price/position change, **then** a
   `heartbeat` event is still emitted — the stream is never silent for longer than the heartbeat interval
   even during a genuinely quiet market.
6. **Given** a client disconnects (tab closed), **when** the server's next write attempt occurs, **then**
   `r.Context().Done()` fires, the handler returns, and `unsubscribe()` removes it from the broadcaster's
   subscriber set — a closed tab must not leak a goroutine/channel indefinitely.
7. **Edge case — zero monitored symbols / zero open positions**: **given** no strategies are currently
   enabled or none have `MonitoredSymbols`, **when** the poll tick fires, **then** only `heartbeat` events
   are emitted (no empty/zero-value `price_update`/`position_update` events) — an idle system produces an
   idle-but-alive stream, not synthetic empty events.
8. **Edge case — `ExchangeClient.ListTickerPrices` fails mid-poll** (e.g. transient Binance API error for
   one symbol): **when** this occurs, **then** the broadcaster skips publishing a `price_update` for that
   specific symbol on that tick only (stale-but-not-wrong is preferable to blocking every other symbol's
   update) and continues normally on the next tick — one symbol's transient failure must not stall the
   entire broadcaster loop or disconnect existing subscribers.

## Out of Scope

- The "stream ticket" short-lived-token alternative — named and rejected in favor of the simpler
  query-param approach. **Resolved by explicit user sign-off (2026-09-06)**: query-param token confirmed as
  the chosen approach, matching this spec's own recommendation — accepted given the already-established
  homelab/no-audit-trail threat model (ADR-009) and the absence of any reverse-proxy access logging in this
  project's current deployment that would actually persist the URL anywhere. Revisit if request logging or
  a reverse proxy is ever added in front of `cmd/api`.
- Redis pub/sub-based cross-process real push from `cmd/worker` (ADR-010's named future upgrade) — this
  spec builds only the `cmd/api`-side polling variant; the worker-side push architecture is a distinct,
  not-yet-scoped future spec.
- Per-symbol or per-strategy subscription filtering (a client only wanting `BTCUSDT` updates still receives
  every monitored symbol's events) — not requested; the frontend is expected to filter client-side, which
  is cheap at this data volume (a handful of symbols/positions for a homelab-scale system).
- Authenticated request logging / token redaction implementation — flagged as a follow-up note in the
  Token-passing section, not built here.

## Dependencies

- backend-01 (Auth Middleware) — `extractBearerToken`'s header-then-query-param fallback is defined there;
  this spec is the sole justification/consumer of the query-param path.
- backend-02 (Embed and Static Serving) — no direct dependency, but this endpoint's route
  (`/stream/dashboard`) must be registered in the same `NewServeMux` ordering discipline both specs
  establish (an exact path, registered before backend-02's `PathPrefix("/")` catch-all, and — unlike
  `/metrics`/the SPA handler — **through** the `RequireAuth` wrapping loop, since this route does require
  authentication, just via the query-param fallback rather than exclusively the header).
- Phase 1's `internal/exchange.ExchangeClient`, Phase 3's `GET /signal?status=open` equivalent
  (`SignalRepository.GetAllOpenSignals`) — reused directly by the broadcaster.
- **Judgment call**: the query-param token exception is this spec's central, most consequential decision —
  flagged twice in this document (Target Behavior and Out of Scope) because it's the one place this spec
  deliberately weakens the "always use the Authorization header" rule backend-01 otherwise establishes
  everywhere else, and the "ticket" alternative was seriously considered, not dismissed casually.
