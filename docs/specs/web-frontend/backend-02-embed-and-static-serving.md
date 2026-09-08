# Spec backend-02 — Embed & Static Serving (`cmd/api/webui`)

## Overview

Per the web-frontend blueprint §3/ADR-008, the built React SPA is embedded via `go:embed` into the
`cmd/api` binary and served same-origin alongside the REST API. This spec defines the embed package, the
SPA-fallback routing that lets client-side routing (React Router) survive a hard page refresh on a deep
path like `/strategies/5`, exactly how this coexists with `gorilla/mux`'s existing API route registration
without either shadowing the other, and what happens when `cmd/api` is built without a real frontend bundle
present (a real scenario during backend-only development).

## Current Behavior (verified)

- `cmd/api/main.go:100-110` (`NewServeMux`) registers every API route via the `Route`/`AsRoute` loop, then
  adds `router.Handle("/metrics", promhttp.Handler())`. **No catch-all or static-file route of any kind
  exists today** — an unmatched path currently returns gorilla/mux's default `404 page not found`.
- No `cmd/api/webui` directory, no frontend build output, no `web/` directory exists anywhere in the repo
  yet (this phase is greenfield for the frontend itself).
- `Makefile` (current, per `git status` showing it modified this session by other work) has targets for
  `run-api-local`, `run-worker-local`, `run-console`, `up`/`down`/`logs`/`clean` (per CLAUDE.md) — no
  frontend build step exists.
- Confirmed (per backend-01) gorilla/mux matches routes **in registration order**, not by specificity —
  this is the property this spec's catch-all registration order depends on.

## Target Behavior

```go
// cmd/api/webui/embed.go (NEW)
package webui

import (
    "embed"
    "io/fs"
    "net/http"
    "strings"
)

// dist holds the Vite build output, copied here by `make web-build` before
// `go build` runs. `all:dist` (not a bare `dist/*` pattern) so a leading-dot
// placeholder file (see "Missing build" below) is included even when no real
// build has been produced yet — go:embed requires at least one matching file
// or the build fails outright, which the placeholder exists to prevent.
//
//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler serving the embedded SPA: real files by
// exact path, falling back to index.html for any path that isn't a real
// file (client-side-routed paths like /strategies/5), and a small inline
// "not built yet" page if even index.html isn't present in the embedded FS
// (see AC#5-6).
func Handler() http.Handler {
    sub, err := fs.Sub(distFS, "dist")
    if err != nil {
        return http.HandlerFunc(notBuiltHandler) // should be unreachable given all:dist always
                                                   // finds *something*, but fails closed to the
                                                   // friendly fallback rather than panicking if it ever is
    }
    fileServer := http.FileServer(http.FS(sub))

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if _, err := sub.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
            // Real, embedded file (e.g. /assets/index-abc123.js) — Vite
            // fingerprints filenames, so these are safe to cache aggressively.
            w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
            fileServer.ServeHTTP(w, r)
            return
        }
        serveIndexOrFallback(w, sub) // SPA fallback / "not built" page — see below
    })
}
```

### SPA fallback + coexistence with `gorilla/mux`

`webui.Handler()` is registered **last**, as a `PathPrefix("/")` catch-all, after every API route and
`/metrics`:

```go
// cmd/api/main.go — NewServeMux, extended (continuing from backend-01's version)
func NewServeMux(routes []Route, cfg *config.Configuration) *mux.Router {
    router := mux.NewRouter()
    for _, route := range routes {
        for _, h := range route.Handlers() {
            router.HandleFunc(h.Pattern, middleware.RequireAuth(cfg, h.Action)).Methods(h.Method)
        }
    }
    router.Handle("/metrics", promhttp.Handler())
    router.PathPrefix("/").Handler(webui.Handler()) // MUST be last — see rationale below
    return router
}
```

**Why this ordering is safe, not fragile**: gorilla/mux tries routes in registration order and stops at the
first match. Every real API route is an **exact path** (`/strategy`, `/backtest/{id:[0-9]+}`, etc.) —
`PathPrefix("/")` only ever gets reached for a request that didn't match any earlier exact route, which is
exactly and only the set of paths this handler is meant to serve (real static assets and client-routed SPA
paths). This is the same registration-order principle backend-01/Phase-3's `backend-01` spec already
established for avoiding path collisions (e.g. `/strategy/performance` vs `/strategy/{id}`) — applied here
at the router-wide level instead of within one handler package.

`serveIndexOrFallback` serves `index.html` with `Cache-Control: no-cache` (the app shell itself must always
be revalidated — the browser should never serve a stale `index.html` referencing since-deleted fingerprinted
asset files after a new deploy) unless `index.html` isn't present in the embedded FS at all, in which case
it serves a small inline (not filesystem-read) HTML string:
```html
<html><body style="font-family:monospace;padding:2rem">
<h1>Frontend not built</h1>
<p>Run <code>make web-build</code> then rebuild cmd/api.</p>
</body></html>
```
with `503 Service Unavailable` (the API itself is fine; the *frontend* specifically isn't available — a
distinguishable status from a real routing failure).

### Missing/empty build at compile time

`go:embed`'s patterns must match at least one file or the build fails outright (a Go language constraint,
not a design choice) — so `cmd/api/webui/dist/` cannot be `.gitignore`d in its entirety, or a fresh clone
would fail to `go build cmd/api` before ever running `make web-build`. This spec resolves it with:
- `cmd/api/webui/dist/.placeholder` — a tiny, **checked-into-git** file (not gitignored) whose only purpose
  is to guarantee `go:embed all:dist` always matches at least one file, even immediately after a fresh
  clone with no frontend build ever run.
- `.gitignore` gains `cmd/api/webui/dist/*` **with** `!cmd/api/webui/dist/.placeholder` (a negated
  exclusion), so real build output (`index.html`, `assets/*.js`, `assets/*.css`) is never committed, but the
  placeholder always is.
- `make web-build` (below) does not need to preserve or delete `.placeholder` specially — copying a real
  build's `index.html`/`assets/` alongside it is harmless (the fallback path only triggers on a **failed**
  `sub.Open("index.html")`, so its presence once a real build exists is inert).

```makefile
# Makefile — new target
web-build:
	cd web && npm ci && npm run build
	rm -rf cmd/api/webui/dist/assets cmd/api/webui/dist/index.html
	cp -r web/dist/* cmd/api/webui/dist/
```
`make build` (or whatever the project's top-level build target is called) gains a dependency on
`web-build` running first, so a normal full build always produces a real, embedded frontend — the
placeholder-only fallback is specifically for backend-only development iterations where running the full
npm toolchain isn't wanted for every `go build`.

## Acceptance Criteria

1. **Given** a real frontend build exists in `cmd/api/webui/dist/` (post `make web-build`), **when**
   `GET /` is requested, **then** `index.html` is served with `Cache-Control: no-cache`.
2. **Given** the same build, **when** `GET /assets/index-abc123.js` is requested (a real, fingerprinted
   Vite output file), **then** it's served with `Cache-Control: public, max-age=31536000, immutable`.
3. **Given** the same build, **when** `GET /strategies/5` is requested (a client-side-only React Router
   path with no corresponding file in `dist/`), **then** `index.html` is served (not a `404`) — the SPA's
   own router then renders the correct view client-side.
4. **Given** `GET /strategy` (a real registered API route) is requested, **when** processed, **then** it is
   handled by `StrategyHandler.GetAll` (subject to backend-01's auth check), **never** falling through to
   `webui.Handler()` — confirming the registration-order coexistence actually holds for a real, exact-path
   API route, not just for the fallback case.
5. **Given** `cmd/api/webui/dist/` contains only the checked-in `.placeholder` file (fresh clone, no
   `make web-build` ever run), **when** `GET /` is requested, **then** the response is `503` with the
   inline "Frontend not built" HTML — not a Go panic, not an unhandled `embed.FS` error surfacing as a
   generic `500`.
6. **Given** the same fresh-clone state, **when** `go build ./cmd/api` is run directly (skipping
   `web-build`), **then** it **compiles successfully** — confirming `.placeholder`'s entire reason for
   existing (satisfying `go:embed`'s "at least one matching file" requirement) actually works.
7. **Edge case — path traversal**: **given** a request for `GET /../../etc/passwd` or similar, **when**
   processed, **then** `http.FileServer`'s standard path-cleaning (Go's stdlib `net/http` already sanitizes
   `..` segments before reaching the handler) prevents any access outside the embedded `dist` subtree —
   this spec relies on and does not need to reimplement that standard-library protection, called out here
   only to confirm it wasn't accidentally bypassed by the custom `sub.Open` existence-check wrapping.
8. **Edge case — asset with a name colliding with an API route** (e.g. a hypothetical future static asset
   literally named `strategy`): **given** this collision, **when** requested, **then** the exact-path API
   route (registered earlier) wins, per gorilla/mux's registration-order matching — flagged as a
   theoretical edge worth naming even though Vite's fingerprinted-filename convention makes a real
   collision extremely unlikely in practice (asset filenames are hashed, not literal resource names).

## Out of Scope

- A separate CDN/static-hosting deployment path — explicitly rejected in the blueprint (§3) in favor of
  single-binary embedding; not designed here as an alternative.
- Server-side rendering or pre-rendering of the SPA shell — the app is client-rendered only; `index.html`
  is a static shell in all cases.
- Content-Security-Policy or other security headers beyond caching — not requested by this phase's scope;
  a reasonable follow-up once the frontend's actual asset-loading pattern (inline scripts, external fonts,
  etc.) is known.
- Hot-module-reload / dev-server proxying from `cmd/api` (i.e., running Vite's own dev server against a
  live-reloading `cmd/api` during frontend development) — frontend development runs `vite dev` directly
  (its own dev server, separate port, proxying API calls to `:8080`) per whatever the `frontend-specialist`
  agent's own spec establishes; this spec only covers the **production embed** path.

## Dependencies

- backend-01 (Auth Middleware) — `RequireAuth` and the SPA catch-all must be registered in the order
  backend-01 already established (API routes, then `/metrics`, then this spec's catch-all last), and the
  catch-all must remain **outside** the per-route auth wrapping loop (static assets and the SPA shell are
  unauthenticated per ADR-009 — only the *data* the SPA fetches after loading requires a token).
- The concurrent `frontend-specialist` agent's Vite build configuration — this spec assumes a standard Vite
  `dist/` output shape (`index.html` at the root, fingerprinted assets under `assets/`); if that agent's
  build configuration differs (e.g. a different output directory), this spec's `embed.go` path references
  need reconciling against their actual `vite.config.ts`.
