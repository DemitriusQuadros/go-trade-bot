# Spec frontend-01 — Scaffold & API Client (`web/`)

## Overview

Phase A (`docs/architecture/web-frontend-blueprint.md` §9). This spec is the foundation every other
`frontend-*` spec depends on: the Vite/React/TS project skeleton under `web/`, and `web/src/api/client.ts` —
the single typed HTTP boundary every page/hook uses to reach `cmd/api`. Per ADR-008/§2, this is a thin
hand-rolled `fetch` wrapper, not Axios/React Query — plain `useEffect` + polling (frontend-10's `usePolling`)
is the chosen data-fetching model for the initial phases.

## Current Behavior

- `web/` does not exist in the working tree (confirmed by directory listing) — this is a from-scratch
  scaffold, not a modification of existing frontend code (none exists; `cmd/console`, the only prior client,
  was deleted per ADR-011).
- `cmd/api/main.go:37-72` (`NewServeMux`) registers every REST route enumerated in the blueprint §1 table.
  None of them require an `Authorization` header today — `internal/configuration.Configuration` has no
  `APIToken` field yet (verified: `internal/configuration/configuration.go`'s `Configuration` struct lists
  `Broker, DB, Redis, Prometheus, Mode, ConfirmLive, Testnet, WebhookURL, APIBaseURL, DryRun, Console` — no
  `APIToken`). The backend-01 auth-middleware spec had not landed in `docs/specs/web-frontend/` at the time
  this spec was written — see "Reconciliation" below.
- **Reconciled against `docs/specs/web-frontend/backend-04-api-consistency-fixes.md`** (landed after this
  spec's first draft, closing the casing gap this spec's own first draft could only flag as a Judgment Call
  — see that section's history below, now resolved rather than open). `GET /strategy`, `GET /strategy/{id}`,
  `PATCH /strategy/{id}/status`, and `PATCH /strategy/{id}/mode` all now serialize through a new
  `StrategyResponseDTO` (`app/handler/web/strategy/response.go`) with `json:"snake_case"` tags on every
  field — backend-04's own audit found the casing leak was broader than the coordinator's original summary
  scoped (it hit both PATCH endpoints too, not just the two GETs). The same fix applies to `GET /signal` and
  `GET /signal/{id}` (unfiltered and both `?status=` filtered variants), now routed through a new
  `SignalResponseDTO`/`OrderResponseDTO` (`app/handler/web/signal/response.go`). No PascalCase key survives
  anywhere in either resource's response body. `StrategyResponseDTO` deliberately omits the vestigial
  `Algorithm` field; `SignalResponseDTO` omits the embedded `Strategy` object, exposing only `strategy_id`
  (matching `BacktestRunResponse`'s existing strategy-id-only precedent). `OrderResponseDTO.stop_loss_price`
  carries the resolved stop price directly, in snake_case — still resolving the gap the deleted TUI's
  `tui-04` spec flagged as unresolved, just via a properly-cased field now rather than the raw-entity field
  this spec's first draft had to document a PascalCase workaround around.

## Target Behavior

### Directory layout (per blueprint §"Directory Structure (full)")

```
web/
├── package.json
├── vite.config.ts
├── tsconfig.json
├── index.html
└── src/
    ├── main.tsx
    ├── App.tsx
    ├── api/
    │   ├── client.ts
    │   └── types.ts        # this spec's addition — shared response/request TS interfaces
    ├── auth/                # frontend-02
    ├── pages/                # frontend-03..09
    ├── components/           # frontend-10, frontend-11
    └── hooks/                # frontend-10
```

### `vite.config.ts` essentials

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // Every REST path this app calls (see api/client.ts's PATH constants)
      // is proxied to cmd/api in dev, so client code always uses relative
      // paths ("/strategy", "/account", ...) and needs zero URL branching
      // between dev and embedded-prod (same-origin under go:embed, §3).
      '/strategy': 'http://localhost:8080',
      '/signal': 'http://localhost:8080',
      '/account': 'http://localhost:8080',
      '/broker': 'http://localhost:8080',
      '/backtest': 'http://localhost:8080',
      '/optimize': 'http://localhost:8080',
      '/events': 'http://localhost:8080', // reserved for frontend-12's SSE endpoint
    },
  },
  build: {
    outDir: 'dist',
  },
});
```

### `tsconfig.json` essentials

- `"strict": true` — no implicit `any` anywhere in `api/types.ts`'s wire-shape interfaces; every field the
  backend can omit/null must be typed `| null` or `?`, not silently widened.
- `"target": "ES2020"`, `"module": "ESNext"`, `"moduleResolution": "bundler"` (Vite's recommended baseline).
- Path alias `"@/*": ["src/*"]` mirrored in `vite.config.ts`'s `resolve.alias`, used by every `frontend-*`
  spec's import examples (`import { api } from '@/api/client'`).

### `api/client.ts`

```ts
// web/src/api/client.ts
const TOKEN_STORAGE_KEY = 'gtb_api_token';

export class ApiError extends Error {
  constructor(public status: number, public body: string) {
    super(`HTTP ${status}: ${body}`);
  }
}

// Thrown for network-level failures (connection refused, DNS, timeout) —
// distinct from ApiError so callers can render "cmd/api unreachable" vs.
// "cmd/api rejected the request" differently, mirroring the deleted TUI's
// apiclient.ErrUnreachable / ErrAPI split (docs/specs/phase-3/tui-01-apiclient.md).
export class NetworkError extends Error {
  constructor(public cause: unknown) {
    super('Network request failed');
  }
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_STORAGE_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_STORAGE_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_STORAGE_KEY);
}

// onUnauthorized is set once by AuthGate (frontend-02) at app startup — the
// client has no router/state dependency of its own, it just notifies via
// this callback so AuthGate can clear the token and re-show the entry screen.
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
}

async function request<T>(method: string, path: string, body?: unknown, opts?: { signal?: AbortSignal }): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: opts?.signal,
    });
  } catch (err) {
    // AbortError (caller cancelled, e.g. usePolling unmount) is not a
    // NetworkError — rethrow as-is so callers can distinguish "I cancelled
    // this" from "the network failed."
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
    throw new NetworkError(err);
  }

  if (res.status === 401) {
    onUnauthorized?.();
    throw new ApiError(401, await res.text().catch(() => ''));
  }
  if (!res.ok) {
    throw new ApiError(res.status, await res.text().catch(() => ''));
  }
  if (res.status === 204 || res.status === 202) {
    // 202 bodies (e.g. POST /optimize) are still JSON-decoded below when
    // present — this early-return only covers the no-body 204 case.
    if (res.status === 204) return null as T;
  }
  const text = await res.text();
  if (!text) return null as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new Error(`Failed to parse response body from ${method} ${path}`);
  }
}

export const api = {
  get: <T>(path: string, opts?: { signal?: AbortSignal }) => request<T>('GET', path, undefined, opts),
  post: <T>(path: string, body?: unknown, opts?: { signal?: AbortSignal }) => request<T>('POST', path, body, opts),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
};
```

### `api/types.ts` — wire-shape interfaces (per blueprint §1's endpoint table, verified against live DTOs)

```ts
// web/src/api/types.ts

// --- Account -----------------------------------------------------------
export interface Account {
  id: number;
  amount: number;
  available_orders: number;
  currency: string;
  created_at: string;
  updated_at: string;
}
// NOTE: entities.Account's JSON tags are unverified (no explicit `json:` tags
// found on app/entities/account.go's fields) — field names above assume
// Go's default (capitalized-as-is) encoding/json behavior UNLESS GORM/json
// middleware lowercases them. Flagged: verify actual casing against a live
// `GET /account` response before Phase A implementation; do not assume.

// --- Strategy ------------------------------------------------------------
// Reconciled against backend-04-api-consistency-fixes.md: GET /strategy,
// GET /strategy/{id}, PATCH /strategy/{id}/status, and PATCH /strategy/{id}/mode
// all now serialize StrategyResponseDTO (app/handler/web/strategy/response.go),
// snake_case throughout, no PascalCase key present anywhere in the payload.
// The interface below mirrors that DTO directly — no client-side
// casing-normalization logic is needed anywhere in api/client.ts.
export type StrategyStatus = 'productive' | 'testing' | 'disabled';
export type StrategyMode = 'backtest' | 'dryrun' | 'paper' | 'live';

export interface Strategy {
  id: number;
  name: string;
  description: string;
  strategy_name: string;    // registry key — the field to display/filter by
  status: StrategyStatus;
  mode: StrategyMode;
  monitored_symbols: string[];
  cycle: number;             // minutes
  configuration: Record<string, unknown> | null; // raw JSONB, algorithm-specific
  created_at: string;
  updated_at: string;
}
// NOTE: StrategyResponseDTO deliberately omits the legacy `algorithm` field
// (vestigial per Spec 06's migration) — it will never appear in the payload,
// not merely absent by omission on this spec's part.

// --- Signal / Order (Positions) ------------------------------------------
// Reconciled against backend-04: GET /signal and GET /signal/{id} (unfiltered
// and both ?status= filtered variants) now serialize SignalResponseDTO /
// OrderResponseDTO (app/handler/web/signal/response.go), snake_case
// throughout. SignalResponseDTO omits the embedded Strategy object entirely
// (exposing only strategy_id), matching BacktestRunResponse's existing
// strategy-id-only precedent — code that previously needed signal.Strategy.Name
// must instead cross-reference strategy_id against the already-fetched
// Strategy[] list (frontend-04/05 already hold that list via usePolling).
export type SignalStatus = 'open' | 'closed';
export type MarginType = 'isolated' | 'cross';

export interface Order {
  id: number;
  signal_id: number;
  broker_order_id: string;
  stop_loss_order_id: string;
  stop_loss_price: number;  // resolved price, already populated server-side —
                             // resolves the deleted TUI's tui-04 "gap" (raw
                             // broker order ID only); confirmed present on
                             // app/entities/signal.go's Order struct today,
                             // now exposed under its correctly-cased key.
  entry_price: number;
  exit_price: number;
  quantity: number;
  invested_amount: number;
  margin_type: MarginType;
  entry_fee: number;
  exit_fee: number;
  leverage: number;
  executed_qty: number;
  is_closing: boolean;
  profit: number;
  created_at: string;
  updated_at: string;
}

export interface Signal {
  id: number;
  symbol: string;
  strategy_id: number;      // no nested `strategy` object — see note above
  status: SignalStatus;
  orders: Order[];
  created_at: string;
  updated_at: string;
}

// --- Broker (market data) -------------------------------------------------
// NOT covered by backend-04 (that spec's Gap 1 scoped strategy/signal only) —
// GET /broker/prices / GET /broker/klines are thin ExchangeClient proxies
// with their own internal/exchange.TickerPrice/Candle types, unaffected by
// this reconciliation pass. Casing here remains PascalCase (Go's default,
// no json: tags on internal/exchange/types.go's structs, verified
// unchanged) — flagged as the same class of inconsistency backend-04 fixed
// for strategy/signal, just not yet addressed for broker; not in scope for
// this reconciliation since backend-04 didn't touch it.
export interface TickerPrice {
  Symbol: string;
  Price: number;
}

export interface Candle {
  Symbol: string;
  Timeframe: string;
  OpenTime: string;
  Open: number;
  High: number;
  Low: number;
  Close: number;
  Volume: number;
}

// --- Backtest --------------------------------------------------------------
export interface RunBacktestRequest {
  strategy_id: number;
  symbol: string;
  timeframe: string;
  start_date: string; // RFC3339
  end_date: string;
  initial_capital?: number;
  fill_policy?: Record<string, unknown>;
}

export interface WalkForwardRequest {
  strategy_id: number;
  symbol: string;
  timeframe: string;
  start_date: string;
  end_date: string;
  initial_capital?: number;
  train_months: number;
  test_months: number;
  step_months: number;
  fill_policy?: Record<string, unknown>;
}

export interface EquityPoint { time: string; value: number }

export interface BacktestRun {
  id: number;
  strategy_id: number;
  symbol: string;
  start_date: string;
  end_date: string;
  is_walk_forward: boolean;
  sharpe: number;
  max_drawdown_pct: number;
  win_rate_pct: number;
  profit_factor: number | 'Infinity'; // see backend's math.IsInf special-case
  total_trades: number;
  total_return_pct: number;
  passed: boolean;
  html_report_path: string;
  equity_curve: EquityPoint[]; // reconciled against backend-04-api-consistency-fixes.md
                                 // Gap 2: BacktestRunResponse now always includes this field
                                 // (unconditionally, not gated behind includeTradeLog) —
                                 // see frontend-07/frontend-11 for the consumer-side fix
                                 // this replaces (client-side reconstruction from trade_log
                                 // is no longer needed). Empty array for pre-migration rows
                                 // whose MetricsJSON was never populated, never null.
  trade_log?: unknown; // only present when the backend's includeTradeLog flag is set (GET /backtest/{id})
  created_at: string;
}

// --- Optimization ----------------------------------------------------------
export type OptimizationStatus = 'pending' | 'running' | 'completed' | 'failed';

export interface ParamRange { min: number; max: number; step: number }

export interface CreateOptimizationRequest {
  strategy_id: number;
  symbol: string;
  timeframe: string;
  start_date: string;
  end_date: string;
  initial_capital?: number;
  param_grid: Record<string, ParamRange>;
}

export interface OptimizationStatusResponse {
  id: number;
  status: OptimizationStatus;
  progress: number;
  total_combinations: number;
  best_config: Record<string, number> | null;
  best_metrics: Record<string, unknown> | null;
  error_message: string | null;
}

export interface OptimizationGridPoint {
  params: Record<string, number>;
  metrics: Record<string, unknown> | null; // null-marked on a failed combination (backend-01 AC#7)
}

export interface OptimizationResults {
  id: number;
  best_config: Record<string, number>;
  best_metrics: Record<string, unknown>;
  grid: OptimizationGridPoint[];
}
```

### Base path resolution (dev vs. embedded-prod)

No `VITE_API_BASE` env branching is needed: `api/client.ts`'s `request()` always calls `fetch(path, ...)`
with a **relative** path (e.g. `/strategy`). In dev, `vite.config.ts`'s `server.proxy` forwards those paths
to `http://localhost:8080`. In embedded-prod (`go:embed`, per the blueprint §3), the SPA and `cmd/api` are
served from the same origin (`:8080`), so a relative path resolves correctly with zero configuration. This
is simpler than the blueprint's own illustrative snippet (`const BASE = import.meta.env.VITE_API_BASE ?? ''`)
— that pattern is kept available as a documented escape hatch (an optional `VITE_API_BASE` env var,
consumed by a thin `resolvePath()` helper) only if a future deployment ever splits the SPA and API across
origins, which is explicitly out of scope for this project (§3: "no separate frontend deployable").

## Acceptance Criteria

1. **Given** `npm --prefix web run dev` is running and `cmd/api` is running on `:8080`, **when** a component
   calls `api.get<Account>('/account')`, **then** the request is proxied to `cmd/api` and the parsed
   `Account` object is returned.
2. **Given** no token has been stored yet (`localStorage.getItem('gtb_api_token')` is `null`), **when**
   `api.get` is called, **then** the request is still sent (no client-side short-circuit) but without an
   `Authorization` header, and `cmd/api`'s middleware is expected to reject it with `401` once backend-01
   lands (reconciliation note below) — the client's job is only to attach the header when present, not to
   pre-validate.
3. **Given** `cmd/api` returns `401`, **when** `request()` processes the response, **then** `onUnauthorized`
   (registered by `AuthGate`, frontend-02) is invoked before the `ApiError` is thrown, so the caller's
   `catch` block and the global re-auth prompt both fire from a single failed call.
4. **Given** `cmd/api` is unreachable (connection refused), **when** any `api.*` call is made, **then** it
   rejects with `NetworkError` (not a raw `TypeError` from `fetch`), so callers can render a distinct
   "server unreachable" state vs. a "bad request" state.
5. **Given** a caller passes an `AbortSignal` (via `usePolling`, frontend-10) and aborts it before the
   response arrives, **when** `fetch` rejects with `AbortError`, **then** `request()` rethrows it unchanged
   (not wrapped as `NetworkError`) so `usePolling`'s cleanup logic can distinguish "I cancelled this" from
   "the network genuinely failed."
6. **Given** a `200` response whose body is not valid JSON, **when** `request()` parses it, **then** it
   throws a descriptive `Error` (not a silent `null`) — mirrors the deleted TUI's `tui-01` AC#7 for the same
   failure class.
7. **Edge case — 202/204 responses** (e.g. `POST /optimize` returns `202` with a JSON body; `POST
   /signal/close/{id}` returns `202` with no body per `app/handler/web/signal/handler.go:60`): **when**
   `request()` handles either, **then** a `204` returns `null` without attempting to parse a body, and a
   `202` with a non-empty body is parsed normally (the status-code branch only short-circuits body-parsing
   for the genuinely-empty `204` case).

## Out of Scope

- Any UI — this spec is the scaffold and the API boundary only. `AuthGate`/pages/components are separate
  specs (`frontend-02` onward).
- Response caching, request deduplication, or a query-invalidation layer (React Query/SWR) — deliberately
  excluded per ADR-008/§2's "kept minimal" stance; revisit only if `usePolling`'s manual approach (frontend-10)
  becomes unwieldy.
- Retry/backoff logic on failed GETs — the deleted TUI's `apiclient` had this (`tui-01` AC#2/#5); this spec
  does not carry it forward by default, since a browser tab's polling loop (frontend-10) naturally retries on
  its next interval tick without needing per-call retry logic. Revisit if a specific page shows this causes
  visibly flaky behavior on transient network blips.

## Dependencies

- `docs/architecture/web-frontend-blueprint.md` §1 (endpoint inventory), §2 (stack choice), §3 (serving
  model — relative-path resolution depends on the same-origin `go:embed` deployment it describes).
- `docs/specs/web-frontend/backend-04-api-consistency-fixes.md` — **reconciled** (landed after Phase A/D
  specs were drafted). Closes this spec's own Judgment Call #1/#2 (below, now resolved rather than open) for
  `strategy`/`signal`; also the source of `BacktestRun.equity_curve` (Gap 2) and the `/backtest/{id}/report`
  route (Gap 3), consumed by `frontend-07`/`frontend-11`.
- `docs/specs/web-frontend/backend-01-auth-middleware.md` — **reconciled** (landed after this spec's first
  draft, during the same spec-writing pass). Two corrections vs. this spec's original guess: (1) the `401`
  response body is **JSON**, not plain text: `{"error":"unauthorized","message":"missing or invalid
  Authorization header"}` (verified, `writeUnauthorized`) — `request()`'s `catch (await res.text())` handling
  above still works unchanged (it stores the raw body string on `ApiError.body` regardless of content type),
  but callers that want to show `message` specifically should `JSON.parse(err.body).message` rather than
  displaying `err.body` verbatim; (2) the allowlist is enforced **structurally** (only specific routes are
  wrapped in `RequireAuth`), not via a path-regex the client needs to know about — confirmed `/metrics` and
  static assets are unauthenticated, matching this spec's original assumption, so no client-side change was
  needed there. Auth is applied **per-route**, and `extractBearerToken` also accepts a `?token=` query
  parameter as a fallback when no header is present (added specifically for `backend-03`'s SSE route,
  `frontend-12`) — this client's header-based approach is unaffected, since it always sends the header when
  a token is stored.

## Judgment Calls (flagged for explicit reconciliation)

1. **RESOLVED by `backend-04-api-consistency-fixes.md`.** This spec's original draft flagged that
   `Strategy`/`Signal`/`Order` mirrored Go's default PascalCase JSON encoding (raw `entities.*` structs, no
   `json:` tags) and proposed two options: accept the inconsistency, or retrofit tags onto the entities.
   Neither was chosen — backend-04 took the third path this spec's own text noted as the existing-pattern
   answer (a response DTO, matching `BacktestRunResponse`'s precedent) rather than tagging the domain
   entities directly, which the coordinator explicitly ruled out to avoid conflating the persistence and
   presentation layers. `api/types.ts`'s `Strategy`/`Signal`/`Order` interfaces above are now snake_case,
   matching `StrategyResponseDTO`/`SignalResponseDTO`/`OrderResponseDTO` exactly — no normalization logic
   lives in `api/client.ts`.
2. **`Account`'s exact field casing remains unverified** — backend-04's Gap 1 scoped `strategy`/`signal`
   only; `entities.Account` was not part of that audit and still has no confirmed `json:` tags. This
   judgment call stands, unresolved, exactly as originally flagged — resolve before implementation, since
   `Account` is the very first call `AuthGate` (`frontend-02`) makes.
3. **`TickerPrice`/`Candle` (broker/market-data types) remain PascalCase**, unaffected by backend-04's
   fix (which scoped `strategy`/`signal` handlers only, not `broker`). This is the same class of
   inconsistency backend-04 just closed for two other resources — flagging in case a future backend spec
   wants to extend the same DTO treatment to `app/handler/web/broker/handler.go`, but treating it as
   out of scope for this reconciliation pass, which is limited to what backend-04 actually shipped.
