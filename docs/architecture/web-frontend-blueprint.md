# go-trade-bot — Web Frontend Architecture

**Source**: `docs/prd/refactoring.md` §10, "Reversed decision (2026-09-06): Web dashboard is now in scope."
**Status**: Planning deliverable — no application code changed by this document.
**Relationship to the main blueprint**: this document extends `docs/architecture/refactoring-blueprint.md`
without modifying it. ADRs here continue that document's numbering (ADR-008 onward) to keep one coherent
decision log across both files.

---

## 1. Why This Costs Little — Verified, Not Assumed

The PRD's reversal note credits ADR-007 for making this cheap. Verified directly against the current
router registration (`cmd/api/main.go:37-72`, `NewServeMux`) rather than taken on faith — the full REST
surface today:

| Package | Routes |
|---|---|
| `strategy` | `POST /strategy`, `GET /strategy`, `GET /strategy/{id}`, `PUT /strategy/{id}`, `PATCH /strategy/{id}/status`, `PATCH /strategy/{id}/mode`, `POST /strategy/enqueue`, `GET /strategy/performance` |
| `signal` | `GET /signal` (with `?status=` filter), `GET /signal/{id}`, `POST /signal/close/{id}` |
| `account` | `POST /account`, `GET /account` |
| `broker` | `GET /broker/prices`, `GET /broker/klines` |
| `backtest` | `POST /backtest`, `POST /backtest/walkforward`, `GET /backtest`, `GET /backtest/{id}`, `POST/GET /backtest/{id}/montecarlo` |
| `optimize` | `POST /optimize`, `GET /optimize`, `GET /optimize/{id}`, `GET /optimize/{id}/results` |
| `performancehistory` | `GET /strategy/{id}/performance/history` |

Every capability the Phase 3/4 TUI specs (`docs/specs/phase-3/tui-*.md`, `docs/specs/phase-4/tui-*.md`)
expose — strategy management, live positions, backtests, walk-forward, Monte Carlo, hyperparameter
optimization, P&L history — already has a REST endpoint. **Zero new backend endpoints are required for
feature parity.** New backend work in this document is limited to: authentication middleware (§3), and,
optionally, an SSE push endpoint for real-time updates (§4) — both additive, neither touches existing
handler/usecase/repository code for the capabilities above.

---

## 2. Stack Choice

**Recommendation: React + TypeScript + Vite.**

| Option | Verdict |
|---|---|
| **React + Vite + TS** | **Chosen.** Deepest charting-library ecosystem (Recharts, visx, react-chartjs-2 all React-native), broadest community/AI-tooling support for a solo maintainer who will lean on Claude Code for frontend work occasionally, Vite gives a near-zero-config dev server and a `dist/` static build trivially consumable by `go:embed`. |
| Vue 3 + Vite | Legitimate alternative, similar build-tooling story to React via Vite. Passed over only because it doesn't clearly beat React on any axis that matters here (charting ecosystem is comparable-to-slightly-behind; team-of-one familiarity favors whichever the maintainer already knows, and React's AI-assistance depth is a genuine practical edge for solo maintenance). |
| Svelte / SvelteKit | Smaller runtime, less boilerplate — a real contender for "stay lean." Passed over for the same AI-tooling-depth reason as Vue, and because SvelteKit's file-based routing/SSR conveniences are wasted on a pure SPA-over-REST app (this project needs a router, not a meta-framework). |
| Angular | Rejected. Full DI/module ceremony, RxJS-first patterns, and a build/tooling footprint disproportionate to a several-page operator dashboard for one user. The `frontend-specialist` skill's convention list names Angular as an option for the *other* project's ecosystem (`go-base-project`) — that project's scale assumptions don't transfer here. |
| Vanilla JS / htmx + server-rendered fragments | Seriously considered as the "leanest possible" option, matching the "don't over-engineer a homelab tool" instinct. **Rejected** for one concrete reason: the entire point of this pivot is *real charts* (equity curves, drawdown, a parameter heatmap, P&L history) with interactivity (hover tooltips, zoom, live-updating series) — htmx's swap-based model is a poor fit for a chart that needs continuous, granular updates and client-side interaction state. Fighting the charting requirement inside htmx would cost more complexity than a small, disciplined React setup with one charting dependency. "Lean" is better achieved by keeping the *React app itself* minimal (no Redux/MobX, no component-library dependency, no CSS-in-JS runtime — plain CSS modules or Tailwind, one HTTP client wrapper, one charting library) than by dropping the framework entirely. |
| Next.js / Remix (React meta-frameworks) | Rejected. SSR/ISR, file-based API routes, and a Node.js server process at deploy time are all solving problems this project doesn't have — `cmd/api` already is the server; the frontend is a pure client-rendered SPA consuming it. Adding a Node runtime to the docker-compose stack would violate PRD §9's "do not introduce new stateful services" spirit for zero benefit.

**Kept minimal deliberately**: React + Vite + TypeScript + React Router + one charting library (§5) + a
thin hand-rolled `fetch` wrapper (no Axios, no React Query/SWR for the initial phases — plain `useEffect`
+ polling is sufficient at this data volume and avoids a dependency whose caching semantics would need
explaining; revisit only if manual polling logic gets genuinely unwieldy).

---

## 3. Serving & Deployment Model

**Recommendation: embed via `go:embed` into `cmd/api`'s binary. No separate frontend deployable.**

This matches the project's existing "one binary per concern" pattern (`cmd/api`, `cmd/worker`) and the
PRD's homelab constraints directly: no new container in `docker-compose.yml`, no
reverse-proxy/CORS configuration (same-origin, since the SPA and its API are served from the same `:8080`
process), no separate static-file host to keep running.

```
web/                              # frontend source — its own npm project, own package.json/vite.config
├── package.json
├── vite.config.ts
├── tsconfig.json
├── index.html
└── src/  ...                     # see §6 for full tree

cmd/api/
├── main.go                       # [MOD] mount webui.Handler() as the catch-all route
├── webui/
│   ├── embed.go                  # [NEW] //go:embed all:dist ; SPA fallback (serve index.html for
│   │                              #        any unmatched, non-API path so client-side routing works
│   │                              #        on a hard refresh of e.g. /strategies/5)
│   └── dist/                     # [NEW, gitignored] build output copied here by `make web-build`
└── modules/
    └── auth.go                   # [NEW] fx.Module providing the auth middleware (§4)
```

Build wiring: `make web-build` runs `npm --prefix web run build` then copies `web/dist/*` into
`cmd/api/webui/dist/` (a `Makefile` target, not a Go build step — Go's `go:embed` only needs the files to
exist on disk at `go build` time, matching how any other embed-based Go project handles a JS build step).
`make build`/`make up` gain a dependency on `web-build` running first. `web/dist/` and `cmd/api/webui/dist/`
are both gitignored — the frontend is built as part of CI/deploy, never committed.

`webui.Handler()` (a small `http.Handler`) is mounted in `NewServeMux` (`cmd/api/main.go:100-110`) as the
**last** registered route (gorilla/mux matches in registration order, per the precedent already established
in `docs/specs/phase-3/backend-01-tui-consumed-api-endpoints.md`'s route-collision findings) — a catch-all
`PathPrefix("/")` that serves static assets by path and falls back to `index.html` for anything that isn't a
real file (client-side-routed paths) and isn't already claimed by an API route registered earlier.

---

## 4. Authentication — Non-Negotiable, In Scope

**The current API has zero authentication anywhere** (every Phase 1-4 endpoint spec says "Auth: none").
That was an acceptable risk surface for a TUI reachable only via shell access to the host. It stops being
acceptable the moment a browser on the home network — or a phone, or (if a reverse-proxy rule is ever
misconfigured) the open internet — can reach an endpoint that places real orders or flips a strategy to
`live` mode. This is treated as a hard requirement of this pivot, not a follow-up.

**Recommendation: a single shared bearer token, checked by middleware.** Not per-user accounts, not OAuth —
those solve a multi-tenant problem this single-operator homelab doesn't have.

```go
// internal/middleware/auth_middleware.go (NEW)
package middleware

// AuthMiddleware rejects any request without a valid `Authorization: Bearer
// <token>` header matching configuration.Configuration.APIToken. Applied to
// every route except the explicit allowlist below.
func AuthMiddleware(cfg *configuration.Configuration) func(http.Handler) http.Handler
```

- `internal/configuration.Configuration` gains `APIToken string` (env var `API_TOKEN`, generated once by
  the operator — e.g. `openssl rand -hex 32` — and placed in `config.yml`/the environment, never
  committed).
- **Allowlist (unauthenticated)**: `/metrics` (Prometheus scraping doesn't support bearer-token auth without
  extra scrape-config plumbing; goroutine/latency/error-rate exposure alone is lower-risk than trade
  execution — flagged as a call worth the user's explicit sign-off, not asserted as obviously correct) and
  the static frontend assets themselves (the SPA shell must load *before* it can prompt for a token — the
  token check applies to API calls the loaded app makes, not to serving `index.html`/JS/CSS).
- **Web UI login flow**: a minimal "unlock" screen (`AuthGate` component, §6) — operator pastes the token
  once, the app stores it (`localStorage`) and attaches `Authorization: Bearer <token>` to every subsequent
  `fetch` call via the shared API client wrapper. No session/expiry UI in v1 (see Consequences in ADR-009).
- **`cmd/console` compatibility**: moot — the TUI was removed entirely (§8), so there is no second client
  needing this header.

**Explicit trade-off, stated plainly**: this design has no per-action audit trail ("who did X" is
meaningless for one operator), no token rotation UI (rotating means editing `config.yml` and restarting
`cmd/api`), and no lockout/rate-limiting on repeated failed attempts (a homelab-scale risk acceptance, not
an oversight). If this bot's control plane is ever exposed beyond the home network, **TLS terminated by the
existing nginx** (already in this stack per CLAUDE.md) is a mandatory companion to this token scheme — a
bearer token sent over plain HTTP across an untrusted network is trivially sniffable. This document
recommends the token scheme *assuming* home-network-only or VPN-gated access, and flags direct internet
exposure without TLS as an explicit non-goal, not a supported configuration.

---

## 5. Real-Time Data Strategy

**Recommendation: polling first (matches the TUI's existing model, zero new backend work), with a clear,
additive path to Server-Sent Events (SSE) for the Dashboard/Positions views in a later phase — not
WebSocket.**

- **Why not WebSocket**: nothing in this data flow needs the browser to push data *to* the server in
  real time — every write (close a position, switch a mode, trigger a backtest) is a discrete action,
  naturally a REST call. WebSocket's bidirectional framing is solving a problem this app doesn't have; SSE
  (one-directional, server→client, plain HTTP, native `EventSource` in every browser, no extra client
  library) is the simpler tool for "push price/position updates as they change."
- **Why not push-based real-time from day one**: today, nothing anywhere fans out price/position changes as
  events — `internal/feed.LiveFeed`'s candle stream lives entirely inside the `cmd/worker` process and is
  never exposed to `cmd/api`. Building true push requires either (a) `cmd/api` polling Postgres/the exchange
  itself on an interval and fanning out to connected browser tabs via SSE (cheap, still centralizes N
  browser tabs' worth of polling into one upstream poll — a real win even though it's "polling under the
  hood"), or (b) `cmd/worker` publishing price/position-change events via Redis pub/sub (already a shared
  dependency in this stack, so no new stateful service per PRD §9) for `cmd/api` to subscribe to and
  re-emit as SSE — genuine push, more moving parts.
- **Recommended sequencing**: ship the whole frontend on plain interval polling first (§9 Phase B/C) — this
  alone is a UX improvement over the TUI (real charts, richer layout) even without solving real-time. Add
  SSE via option (a) — `cmd/api`-side polling, fanned out — as a Phase E polish item once the core app is
  useful and real usage reveals whether 1-2s staleness on Positions/Dashboard actually bothers the operator
  in practice. Option (b) (Redis pub/sub, true push) is named as a future upgrade path, not built in this
  round.

---

## 6. Charting

**Recommendation: Recharts** for every time-series chart (equity curve, drawdown curve, P&L history), plus
a **hand-rolled CSS-grid component** (no additional library) for the optimization parameter heatmap.

- **Recharts over Chart.js**: Chart.js is canvas-based and imperative — using it from React means fighting
  an imperative API through a ref-based wrapper. Recharts is React-native and declarative
  (`<LineChart><Line dataKey="equity" /></LineChart>`), which is a materially better fit for a
  React-based app and faster to build/maintain for a solo developer.
- **Recharts over D3 directly**: D3 gives full control at a much higher implementation cost — appropriate
  for a data-viz product, not proportionate to a homelab trading dashboard's 4-5 chart types.
- **Heatmap**: Recharts has no native heatmap primitive. Rather than adding a second charting dependency
  (e.g. `visx`) for exactly one chart type, the optimization parameter grid (backend-01/02, Phase 4) is
  rendered as a plain CSS-grid of colored cells — a heatmap is fundamentally a colored grid, and a small
  hand-rolled color-interpolation function (red→yellow→green across the Sharpe-ratio range) is simpler to
  build, style, and maintain than introducing a whole library for one component.

Chart inventory and source data (all already available via the audited REST surface, §1):
| Chart | Data source |
|---|---|
| Equity curve, drawdown curve | `GET /backtest/{id}` → `BacktestMetrics.EquityCurve` |
| P&L history (time-bucketed) | `GET /strategy/{id}/performance/history` |
| Optimization parameter heatmap | `GET /optimize/{id}/results` → `grid` |
| Monte Carlo distribution (histogram/box-plot) | `GET/POST /backtest/{id}/montecarlo` |
| Live price sparkline (Dashboard) | `GET /broker/klines` (polling or future SSE) |

---

## Directory Structure (full)

```
go-trade-bot/
├── web/
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── index.html
│   └── src/
│       ├── main.tsx
│       ├── App.tsx                        # router shell
│       ├── api/
│       │   └── client.ts                  # typed fetch wrapper; attaches Authorization header
│       ├── auth/
│       │   └── AuthGate.tsx                # token entry screen, localStorage persistence
│       ├── pages/
│       │   ├── Dashboard.tsx
│       │   ├── Strategies.tsx
│       │   ├── Positions.tsx
│       │   ├── BacktestLauncher.tsx
│       │   ├── BacktestResults.tsx
│       │   ├── Optimization.tsx
│       │   └── ExecutionLog.tsx
│       ├── components/
│       │   ├── charts/
│       │   │   ├── EquityCurveChart.tsx
│       │   │   ├── DrawdownChart.tsx
│       │   │   ├── ParamHeatmap.tsx
│       │   │   ├── MonteCarloDistribution.tsx
│       │   │   └── PnlHistoryChart.tsx
│       │   ├── StrategyTable.tsx
│       │   ├── ModeBadge.tsx
│       │   ├── PositionCard.tsx
│       │   └── ConfirmDialog.tsx
│       └── hooks/
│           ├── usePolling.ts
│           └── useSSE.ts                  # added in the real-time polish phase (§5/§9 Phase E)
├── cmd/
│   ├── api/
│   │   ├── main.go                        # [MOD]
│   │   ├── webui/
│   │   │   ├── embed.go                   # [NEW]
│   │   │   └── dist/                      # [NEW, gitignored, build artifact]
│   │   └── modules/
│   │       └── auth.go                    # [NEW]
│   └── (console/ removed entirely — see §8)
├── internal/
│   ├── middleware/
│   │   └── auth_middleware.go             # [NEW]
│   └── configuration/configuration.go     # [MOD] add APIToken field
└── Makefile                               # [MOD] add web-build target
```

---

## 7. Page/Feature Mapping — TUI → Web

| TUI page (Phase 3/4 spec) | Web equivalent | Where the web version does meaningfully more |
|---|---|---|
| Page 1 — Dashboard (`tui-02-page-dashboard.md`) | `Dashboard.tsx` | Real interactive line charts (hover for exact price/time) instead of braille sparklines; clickable monitored pairs jump straight into that symbol's chart/strategy detail; live balance via polling→SSE (§5) instead of a fixed terminal refresh rate. |
| Page 2 — Strategies (`tui-03-page-strategies.md`) | `Strategies.tsx` | Sortable/filterable table (TUI has a fixed column order); a real syntax-highlighted, editable JSON config panel in a modal (vs. terminal preview-only); multi-select bulk enable/disable. |
| Page 3 — Positions (`tui-04-page-positions.md`) | `Positions.tsx` | Real-time P&L via SSE (once shipped); clickable row drills into the originating Signal/Order history; manual close via a proper confirm modal instead of a keypress-plus-terminal-prompt. |
| Page 4 — Backtest Launcher (`tui-05-page-backtest-launcher.md`) | `BacktestLauncher.tsx` | A real calendar date-range picker and symbol autocomplete (sourced from the strategy's `MonitoredSymbols`) instead of raw text-field date entry. |
| Page 5 — Backtest Results (`tui-06-page-backtest-results.md`) | `BacktestResults.tsx` | Real equity-curve/drawdown charts (Recharts) instead of braille sparklines; a sortable/paginated trade table; the HTML report rendered inline (`<iframe>`) instead of "open in default browser." |
| Page 6 — Execution Log (`tui-07-page-execution-log.md`) | `ExecutionLog.tsx` | Full-text search across all fields (not just filter-by-strategy/event-type); virtualized infinite scroll instead of a fixed terminal pane; CSV export. |
| Phase 4 — Optimization Results (`tui-01-page-optimization-results.md`) | `Optimization.tsx` | A true 2D color heatmap with hover-for-exact-metrics (the TUI spec's own "Judgment Call #3: no true heatmap widget in termui" is exactly the limitation this pivot resolves); a one-click "apply this config" action that `PATCH`es the winning parameters onto the live strategy — awkward-to-impossible as a terminal action. |
| Phase 4 — P&L History sparkline (`tui-02-pnl-history-sparkline.md`) | Folded into `Strategies.tsx`'s detail view, or a standalone `PerformanceHistory` panel | A full interactive chart with a daily/weekly/monthly toggle and multi-strategy overlay/comparison — the TUI spec scopes this to a single embedded sparkline per strategy; the web version can compare strategies side by side on one chart. |

---

## 8. `cmd/console` Disposition

**Superseded (2026-09-06): removed entirely, not kept frozen.** This section originally recommended
keeping the TUI as a zero-browser emergency fallback (rationale below, kept for historical record). The
user overrode that recommendation directly and asked for the TUI to be removed from the project outright.
`cmd/console/` (all pages, components, `apiclient`, `dependencies`, tests) has been deleted; the `termui/v3`
dependency is removed from `go.mod`; `Makefile`'s `run-console` target and `CLAUDE.md`'s references are
removed. Nothing outside `cmd/console/` imported it or `termui` (verified before deletion), so this was a
clean removal with no follow-on changes required elsewhere in the codebase. §9's phased build plan and every
other section of this document are unaffected — the web frontend was always designed as the sole primary
interface going forward regardless of the TUI's fate.

<details>
<summary>Original recommendation (superseded, kept for record)</summary>

Original recommendation: keep, frozen at its current (post-Phase-3/4) feature set — do not retire, and do
not build further TUI phases going forward.

Rationale that was given:
- It was a fully working, already-tested sunk investment (Phases 1-4's TUI specs were implemented) —
  deleting a working tool for no functional reason seemed like pure waste.
- It had genuine, distinct value as a **zero-browser emergency fallback**: checking strategy status or
  force-disabling a runaway strategy via an SSH session from a phone terminal app, when a proper browser
  isn't convenient or the web frontend/reverse-proxy is itself down, is a real operational scenario for a
  live-trading system.
- It cost nothing to freeze — ADR-007 already made it a pure `apiclient` HTTP consumer with no direct DB
  access, so it had zero coupling to any backend change this document proposes.

This tradeoff was overridden by explicit user decision — the operational value of an SSH fallback was
judged not worth maintaining a second UI surface at all, even a frozen one.

</details>

---

## 9. Phased Build Plan

Lighter-weight than Phases 1-4's specs — a sequence, not a full spec-per-item. Each phase should still get
its own `docs/specs/web-frontend/` spec file(s) when implementation starts, following the same template as
prior phases; this section only orders the work.

```
Phase A — Foundation (highest integration risk, do first)
  - internal/middleware/auth_middleware.go + Configuration.APIToken
  - Minimal Vite/React/TS scaffold in web/
  - go:embed wiring (cmd/api/webui) + Makefile web-build target
  - AuthGate token-entry screen
  - One trivial authenticated page proving build -> embed -> serve -> auth end to end
  Success signal: a browser hitting :8080 gets prompted for a token, and once entered, successfully
  calls one real authenticated endpoint (e.g. GET /account).

Phase B — Read-only core views
  - Dashboard, Strategies (list + detail, no edit), Positions (read-only) - all on interval polling
  Success signal: feature-parity read access to everything the TUI's Dashboard/Strategies/Positions
  pages show, in a browser.

Phase C — Write actions
  - Strategy create/edit, PATCH status/mode, manual position close, backtest/walk-forward trigger,
    optimization trigger
  Success signal: every write action the TUI exposes has a working web equivalent.

Phase D — Backtest/Optimization/Charts polish (the payoff this pivot was for)
  - Recharts equity/drawdown/P&L-history charts, ParamHeatmap, Monte Carlo distribution view,
    inline HTML report iframe
  Success signal: no chart in the app is a block-character/braille approximation anymore.

Phase E — Real-time polish
  - cmd/api-side polling-and-fan-out SSE endpoint for Dashboard/Positions
  Success signal: Dashboard/Positions update without a manual refresh or client-side poll interval,
  across multiple simultaneously open browser tabs sharing one upstream poll.
```

---

## 10. Architecture Decision Records

```
ADR-008: Web frontend stack is React + TypeScript + Vite, embedded via go:embed into cmd/api
  Decision: build the frontend as a Vite-bundled React/TS SPA under web/, served as static assets
    embedded into the cmd/api binary via go:embed, same-origin with the REST API.
  Alternatives: Vue/Svelte (comparable technical fit, passed over on ecosystem/AI-tooling-depth
    grounds for a solo maintainer); Angular (disproportionate ceremony); vanilla JS/htmx (poor fit
    for the real-charting requirement that motivated this pivot); Next.js/Remix (solves SSR/API-route
    problems this project doesn't have, and would require a Node runtime in docker-compose).
  Rationale: matches the project's existing "one binary per concern" pattern, needs the deepest
    charting-library ecosystem available, and keeps the dependency footprint minimal by choosing a
    build tool (Vite) over a framework (Next.js) for what is fundamentally an SPA over an existing API.
  Consequences: adds a Node/npm build step to CI/deploy that didn't exist before (Makefile gains a
    web-build target); the frontend and backend are versioned/deployed together as one binary, which
    is a feature (one artifact, one docker-compose service) not a constraint at this project's scale.

ADR-009: Authentication is a single shared bearer token, not per-user accounts
  Decision: internal/middleware.AuthMiddleware checks Authorization: Bearer <token> against a single
    configured Configuration.APIToken for every route except /metrics and static asset serving.
  Alternatives: full OAuth/session-based multi-user auth (solves a problem this single-operator
    homelab doesn't have); no auth at all (rejected outright per this task's explicit constraint -
    unacceptable for a browser-reachable, trade-capable control plane); mTLS/client-certificate auth
    (more robust but meaningfully more operational overhead - cert issuance/rotation - for a homelab
    single-operator context than the risk reduction justifies over a long random bearer token behind
    a home network/VPN boundary).
  Rationale: minimum viable protection against "anyone on the network can place trades," proportionate
    to a one-operator system, cheap to implement (one middleware, one config field) and cheap to
    operate (no user database, no password reset flow, no session store).
  Consequences: no audit trail, no token rotation UI, no rate-limiting/lockout in v1 - all explicitly
    accepted trade-offs for this context (see §4). Direct internet exposure without a TLS-terminating
    reverse proxy in front is an explicit non-goal, not a supported deployment mode. (The cmd/console
    compatibility consequence originally noted here is moot - the TUI was removed entirely, §8.)

ADR-010: Real-time updates start as polling; SSE is an additive future upgrade, not WebSocket
  Decision: ship the initial frontend on the same interval-polling model the TUI already uses; add a
    server-side-polling-fanned-out-via-SSE endpoint for Dashboard/Positions only after the core app is
    built and real usage shows staleness is actually a problem.
  Alternatives: WebSocket (bidirectional framing this app's data flow doesn't need - every write is a
    discrete REST action); build true push (Redis pub/sub from cmd/worker) from day one (more moving
    parts, deferred until proven necessary, consistent with this project's established pattern of not
    front-loading infrastructure - see the main blueprint's "Building a generic multi-exchange
    abstraction now" scope-creep precedent).
  Rationale: polling ships a usable, chart-rich app fastest; SSE-over-server-side-polling is a cheap,
    purely additive upgrade later that doesn't require touching cmd/worker at all, since it only
    changes how cmd/api serves data it can already query.
  Consequences: initial UX has the same staleness characteristics as the TUI it's replacing (a known,
    accepted gap during Phases A-D); Phase E's SSE upgrade requires zero changes to cmd/worker/LiveFeed.

ADR-011: cmd/console (TUI) is removed entirely, not retained (supersedes this ADR's original decision)
  Original decision (2026-09-06, superseded same day): keep the Phase 3/4 TUI as-is, frozen, updated only
    for the mandatory auth-token compatibility fix (ADR-009's consequence).
  Superseding decision: cmd/console/ deleted outright, along with the termui/v3 dependency and its
    Makefile/CLAUDE.md references. Overridden directly by the user rather than by a change in technical
    circumstances - the original rationale (SSH emergency fallback value, zero marginal cost to freeze)
    wasn't wrong, it was simply outweighed by a preference to not maintain a second UI surface at all.
  Alternatives considered at the time: keep frozen (the original decision, now superseded); keep with
    active feature investment (never seriously considered - explicitly rejected in the original ADR text).
  Rationale for the override: verified before deletion that nothing outside cmd/console/ imported it or
    termui/v3, so removal was clean with zero follow-on changes needed elsewhere in the codebase - the
    "zero marginal cost to keep" argument cuts both ways; it was also zero marginal cost to remove.
  Consequences: no SSH-reachable fallback exists anymore if the web frontend or its reverse proxy/host is
    down. Accepted as a reasonable tradeoff for a single-operator homelab system by explicit user decision;
    if this gap is ever felt in practice, revisit rather than silently reintroducing a TUI.
```

---

## Open Questions Requiring Sign-Off

```
Open question: Should /metrics remain unauthenticated once AuthMiddleware ships?
  Blocks: internal/middleware/auth_middleware.go's exact allowlist.
  Recommendation in this doc: yes, leave it open (Prometheus scrape-config friction vs. modest
    exposure of goroutine/latency/error-rate data) - but this is a judgment call, not asserted as
    obviously correct, and should be confirmed before Phase A implementation.

Open question: Token storage in the browser - localStorage (simple, vulnerable to XSS if one is ever
  introduced) vs. an httpOnly cookie (better XSS posture, but requires cmd/api to set/manage cookies
  rather than the SPA handling the token entirely client-side, adding a small amount of backend
  complexity to Phase A).
  Blocks: web/src/auth/AuthGate.tsx's implementation and internal/middleware/auth_middleware.go's
    token-extraction logic (header vs. cookie).
  Recommendation in this doc: localStorage for v1, given this is a from-scratch React SPA with no
    existing XSS-risk surface (no third-party script injection points) - revisit if the app ever adds
    a plugin/extension model.

Open question: Exact chart library version/pinning and whether Tailwind or plain CSS Modules is used
  for styling - a genuinely low-stakes implementation detail this document deliberately leaves open
  rather than over-specifying at the architecture stage.
  Blocks: nothing architectural; resolve at Phase A implementation time.
```
