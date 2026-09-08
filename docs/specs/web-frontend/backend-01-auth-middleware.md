# Spec backend-01 — Auth Middleware (`internal/middleware/auth_middleware.go`)

## Overview

ADR-009 (`docs/architecture/web-frontend-blueprint.md`) requires a single shared bearer token protecting
every `cmd/api` route before a browser-reachable frontend can be exposed. This spec makes that concrete:
exact placement in the existing middleware/routing chain, the 401 response shape the frontend's `AuthGate`
depends on, fail-fast-by-default token loading, and an explicit confirmation that this is `cmd/api`-only —
`cmd/worker` serves no web-facing routes beyond `/metrics`/Asynqmon and is unaffected.

## Current Behavior (verified)

- `cmd/api/main.go:74-98` (`NewHTTPServer`) wraps the `*mux.Router` with exactly one middleware today:
  `wrappedMux := middleware.ConfigMiddleware(cfg, collector)(router)`. `ConfigMiddleware`
  (`internal/middleware/middleware.go:19-39`) injects `cfg` into the request context and records
  `http_requests_total`/`http_request_duration_seconds` — it does **not** perform any authorization check.
- `cmd/api/main.go:100-110` (`NewServeMux`) builds the router by iterating every registered `Route`'s
  `Handlers()` (the `AsRoute`/`fx.Annotate(... fx.As(new(Route)) ...)` pattern, confirmed the sole
  registration path for `strategy`, `broker`, `account`, `signal`, `backtest`, `optimize`,
  `performancehistory` — **every** current API handler package goes through this one loop), then adds
  `router.Handle("/metrics", promhttp.Handler())` directly, outside that loop.
- No authentication of any kind exists anywhere in `cmd/api` today (confirmed — no `Authorization` header
  check, no API-key middleware, no session handling in any handler file across all seven packages).
- `internal/configuration/configuration.go:11-23` (`Configuration` struct, current) has `Broker, DB, Redis,
  Prometheus, Mode, ConfirmLive, Testnet, WebhookURL, APIBaseURL, DryRun, Console` — no `APIToken` field
  exists yet.
- `cmd/worker/main.go:210-224` (`StartMetricsServer`) mounts exactly two routes on `:9191`: Asynqmon's UI at
  `/tasks/monitoring` and `/metrics` via `promhttp.Handler()`. **No other route of any kind is served by
  `cmd/worker`** — confirmed by full-file read; there is no handler registration loop, no `Route` interface
  usage, nothing resembling `cmd/api`'s web-handler pattern anywhere in `cmd/worker`. This spec's middleware
  is therefore **`cmd/api`-only**, applied nowhere in `cmd/worker`.

## Target Behavior

```go
// internal/configuration/configuration.go — additive field
type Configuration struct {
    // ...existing fields...
    APIToken           string // shared bearer token (Spec backend-01); required unless AllowInsecure
    AllowInsecureNoAuth bool  // explicit, named opt-out — see "Fail-fast" below
}
```

```go
// internal/middleware/auth_middleware.go (NEW)
package middleware

import "net/http"

// RequireAuth wraps a single handler.Configuration's Action (NOT the whole
// router — see "Placement" below for why). Checks Authorization: Bearer
// <token> against cfg.APIToken using constant-time comparison
// (crypto/subtle.ConstantTimeCompare, avoiding a timing side-channel on an
// otherwise trivially-brute-forceable string compare).
func RequireAuth(cfg *configuration.Configuration, next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if cfg.AllowInsecureNoAuth {
            next(w, r)
            return
        }
        token := extractBearerToken(r) // also checks ?token= query param — see backend-03 for why
        if token == "" || !constantTimeEquals(token, cfg.APIToken) {
            writeUnauthorized(w)
            return
        }
        next(w, r)
    }
}

// writeUnauthorized is the single source of the 401 response shape - every
// rejected request across every route returns byte-identical structure.
func writeUnauthorized(w http.ResponseWriter) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusUnauthorized)
    w.Write([]byte(`{"error":"unauthorized","message":"missing or invalid Authorization header"}`))
}
```

### Placement — per-route wrapping, not a blanket outer middleware

**Decision**: `RequireAuth` wraps each individual `handler.Configuration.Action` at registration time inside
`NewServeMux`, not the whole `*mux.Router` the way `ConfigMiddleware` does. Concretely:

```go
// cmd/api/main.go — NewServeMux, modified
func NewServeMux(routes []Route, cfg *config.Configuration) *mux.Router {
    router := mux.NewRouter()
    for _, route := range routes {
        for _, h := range route.Handlers() {
            router.HandleFunc(h.Pattern, middleware.RequireAuth(cfg, h.Action)).Methods(h.Method)
        }
    }
    router.Handle("/metrics", promhttp.Handler()) // unauthenticated — unchanged, unwrapped
    // backend-02's SPA catch-all is added here too, also unwrapped — see that spec
    return router
}
```

**Why per-route, not a blanket wrapper around the whole mux**: the allowlist (`/metrics`, static frontend
assets) would otherwise require the middleware itself to pattern-match/enumerate which incoming paths are
"API" vs. "exempt" — fragile, and it grows stale the moment a new API route is added without updating an
allowlist regex. Per-route wrapping instead makes the allowlist **structural**: only handlers that came
through the `Route` interface (i.e., every real API endpoint, confirmed above to be the sole registration
path for all seven current handler packages) ever get wrapped; `/metrics` and the future SPA/static handler
(backend-02) are registered through separate code paths that never call `RequireAuth`, so they're exempt by
construction, not by an allowlist that could drift.

`ConfigMiddleware` remains the **outer** wrapper (`NewHTTPServer`, unchanged call site):
`middleware.ConfigMiddleware(cfg, collector)(router)`. Request flow: `ConfigMiddleware` records metrics for
**every** request including rejected ones (so a spike in `401`s is visible in
`http_requests_total`/observable in Grafana per the existing process-health dashboard, Phase 2 Spec 11) →
`mux.Router` dispatch → per-route `RequireAuth` check → the real handler.

### Fail-fast token loading

`NewConfiguration()` (`internal/configuration/configuration.go:61-192`) gains, mirroring the existing
`log.Fatalf`-on-missing-required-value pattern already used for `BROKER.KEY`/`DB.HOST`/etc.:
```go
apiToken := viper.GetString("API_TOKEN")
allowInsecure := viper.GetBool("ALLOW_INSECURE_NO_AUTH")
if apiToken == "" && !allowInsecure {
    log.Fatalf("API_TOKEN is required (or set ALLOW_INSECURE_NO_AUTH=true to explicitly run without auth — NOT recommended)")
}
if allowInsecure {
    log.Printf("WARNING: ALLOW_INSECURE_NO_AUTH=true — cmd/api is serving every route with NO AUTHENTICATION. This must never be set outside a fully-isolated local dev environment.")
}
```
**Rationale**: matches this codebase's own established safety philosophy exactly (Phase 1 Spec 10's `MODE`/
`--confirm-live` guard: safe-by-default, unsafe only via an explicit, loudly-logged, named opt-in — never
"secure by default until someone forgets a config value, then silently insecure"). Fail-fast is the
default; local development without a token requires deliberately setting `ALLOW_INSECURE_NO_AUTH=true`, not
just omitting `API_TOKEN` by oversight.

## Acceptance Criteria

1. **Given** `APIToken` is configured and a request to `GET /strategy` arrives with
   `Authorization: Bearer <correct-token>`, **when** `RequireAuth` processes it, **then** the request
   reaches `StrategyHandler.GetAll` normally.
2. **Given** the same request with no `Authorization` header at all, **when** processed, **then** the
   response is `401` with the exact JSON body from `writeUnauthorized` — this exact shape is what the
   frontend's `api/client.ts` checks to decide whether to show `AuthGate` again.
3. **Given** a request with `Authorization: Bearer <wrong-token>`, **when** processed, **then** the response
   is `401`, identical in shape to Acceptance Criterion #2 — the frontend does not need to distinguish
   "missing" from "wrong" token, both mean "prompt for the token again."
4. **Given** `API_TOKEN` is unset and `ALLOW_INSECURE_NO_AUTH` is unset (or `false`), **when**
   `cmd/api` starts, **then** the process exits via `log.Fatalf` before the HTTP server binds — it never
   silently starts unauthenticated.
5. **Given** `ALLOW_INSECURE_NO_AUTH=true` is explicitly set, **when** `cmd/api` starts, **then** it starts
   normally, logs the loud warning shown above, and every route (including ones that would otherwise
   require a token) serves requests with no auth check.
6. **Given** `GET /metrics` is requested with no `Authorization` header, **when** processed, **then** it
   succeeds (`200`) — confirmed exempt by construction (registered outside the per-route wrapping loop),
   not by the middleware inspecting the path.
7. **Edge case — case-sensitivity/whitespace in the header**: **given** `Authorization: bearer
   <token>` (lowercase scheme) or `Authorization:Bearer  <token>` (extra whitespace), **when** parsed,
   **then** `extractBearerToken` handles both per the `Authorization` header's own case-insensitive-scheme,
   single-space-separator convention (standard HTTP semantics) — not stricter than what any standard HTTP
   client library would naturally produce.
8. **Edge case — token comparison timing**: **given** an attacker sends a token that matches the correct
   token's first N characters, **when** compared, **then** `constantTimeEquals` (via
   `crypto/subtle.ConstantTimeCompare`) takes the same time as a completely wrong guess — verified by
   confirming the implementation uses the constant-time primitive, not a plain `==`/`strings.Compare`.
9. **Edge case — `cmd/worker` is unaffected**: **given** `cmd/worker`'s `/metrics` and `/tasks/monitoring`
   endpoints, **when** requested, **then** they behave exactly as before this spec (no auth check
   introduced) — this spec makes zero changes to any file under `cmd/worker/`, confirmed explicitly since
   the coordinator asked this be stated, not just implied by omission.

## Out of Scope

- Any change to `cmd/worker`'s `/metrics`/Asynqmon exposure — explicitly confirmed unaffected (Current
  Behavior, Acceptance Criterion #9). Asynqmon's own UI is itself a control surface (can view/manage async
  tasks) with no auth of its own — noted here as a **residual risk**, not fixed by this spec, since the
  coordinator's task scoped this spec to `cmd/api` only.
- Token rotation tooling, expiry, or multi-token support — ADR-009 already scopes v1 to a single static
  token; rotation means editing config and restarting the process.
- Per-route granular permissions (e.g. read-only vs. trade-capable tokens) — a single token grants full
  access to every route; this matches the single-operator threat model ADR-009 is built for.
- Rate-limiting/lockout on repeated failed auth attempts — explicitly named as an accepted v1 gap in ADR-009.

## Dependencies

- `docs/architecture/web-frontend-blueprint.md` ADR-009 — this spec implements that decision.
- backend-02 (Embed and Static Serving) — its SPA catch-all route must also be registered outside the
  per-route `RequireAuth` loop, using the same "registered through a different code path" exemption pattern
  this spec establishes for `/metrics`.
- backend-03 (SSE Realtime Endpoint) — `extractBearerToken`'s query-param fallback (`?token=`) exists
  specifically for that spec's `EventSource` constraint; this spec defines the mechanism, backend-03
  justifies its use.
