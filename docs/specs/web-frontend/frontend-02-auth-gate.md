# Spec frontend-02 — AuthGate & App Shell (`web/src/auth/AuthGate.tsx`, `web/src/App.tsx`)

## Overview

Phase A, second half: the "unlock" screen (blueprint §4) and the router shell that decides whether to show
it. This is the last Phase A piece before the "one trivial authenticated page" success signal (blueprint §9
Phase A: "a browser hitting :8080 gets prompted for a token, and once entered, successfully calls one real
authenticated endpoint"). Built against `frontend-01`'s `api/client.ts`.

## Current Behavior

- No auth concept exists anywhere in this codebase's HTTP surface today — every Phase 1-4 endpoint spec
  states "Auth: none" (confirmed by grep across `docs/specs/phase-*/backend-*.md` and every handler in
  `app/handler/web/`, none of which reference an `Authorization` header or any token check).
- `docs/specs/web-frontend/backend-01-auth-middleware.md` — **not yet present** in the working tree at
  spec-writing time (verified: empty `docs/specs/web-frontend/` directory before this spec set was
  written). This spec is designed against the blueprint's own ADR-009 description; see Dependencies for the
  exact reconciliation points.

## Target Behavior

### Token lifecycle (per blueprint §4/ADR-009 and its "Open Questions" localStorage recommendation)

- **Storage**: `localStorage`, key `gtb_api_token` (matches `frontend-01`'s `api/client.ts` constant) — no
  cookie, no `sessionStorage` (a token entered once should survive a tab close/reopen; this is a homelab
  single-operator tool, not a shared-kiosk context where session-only storage would be safer).
- **No expiry/rotation UI in v1** (ADR-009 Consequences, explicit) — the token is valid until the operator
  clears it or the backend's configured `APIToken` value changes (which invalidates every stored copy
  simultaneously, requiring every browser tab to re-enter it — an accepted operational cost per ADR-009).

### Validation strategy: **fail on first real API call, not a dedicated validate-on-entry round-trip**

Two options exist for "does entering a wrong token show an error immediately":
1. A dedicated lightweight validation call (e.g. `GET /account`) fired the moment the operator submits the
   token entry form, before ever showing the app shell.
2. No dedicated validation call — store the token optimistically, render the app shell, and let whichever
   real API call the first page makes surface the `401` naturally.

**This spec adopts (1)**, using `GET /account` (the same "lightweight, always-available" choice the deleted
TUI's `apiclient.Ping` made for connectivity checks, `docs/specs/phase-3/tui-01-apiclient.md`) as an explicit
verification call on submit — not because (2) is unworkable, but because a wrong-token error appearing
*after* the operator has already watched the app shell flash in (only to be yanked back to the entry screen
by a delayed 401 from whatever the Dashboard's first `useEffect` fires) is a materially worse first
impression than a single extra round-trip on the entry form itself. This is this spec's own UX judgment
call, not dictated by the blueprint — flagged below.

```tsx
// web/src/auth/AuthGate.tsx
import { useState, type ReactNode } from 'react';
import { api, setToken, getToken, clearToken, setUnauthorizedHandler, ApiError, NetworkError } from '@/api/client';
import type { Account } from '@/api/types';

interface AuthGateProps {
  children: ReactNode;
}

export function AuthGate({ children }: AuthGateProps) {
  const [unlocked, setUnlocked] = useState(() => getToken() !== null);
  const [tokenInput, setTokenInput] = useState('');
  const [validating, setValidating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Registered once — any 401 from ANY subsequent api.* call anywhere in the
  // app (not just this form) clears the token and drops back to the entry
  // screen. See Acceptance Criterion #5.
  setUnauthorizedHandler(() => {
    clearToken();
    setUnlocked(false);
    setError('Session expired or token no longer valid — please re-enter it.');
  });

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!tokenInput.trim()) {
      setError('Token is required.');
      return;
    }
    setValidating(true);
    setError(null);
    setToken(tokenInput.trim()); // set BEFORE the validation call so the
                                  // Authorization header is actually attached
    try {
      await api.get<Account>('/account');
      setUnlocked(true);
    } catch (err) {
      clearToken();
      if (err instanceof ApiError && err.status === 401) {
        // backend-01 (reconciled): body is JSON {"error":"unauthorized","message":"..."},
        // not plain text — display the parsed message when present, falling back to a
        // generic string if parsing ever fails (e.g. against a future non-JSON 401 source).
        const message = (() => {
          try { return JSON.parse(err.body).message as string; } catch { return null; }
        })();
        setError(message ?? 'Invalid token. Check the value in your config.yml / API_TOKEN and try again.');
      } else if (err instanceof NetworkError) {
        setError('Cannot reach the server. Is cmd/api running?');
      } else {
        setError('Unexpected error validating the token. See console for details.');
      }
    } finally {
      setValidating(false);
    }
  }

  if (unlocked) return <>{children}</>;

  return (
    <main className="auth-gate" aria-labelledby="auth-gate-title">
      <form onSubmit={handleSubmit}>
        <h1 id="auth-gate-title">go-trade-bot</h1>
        <label htmlFor="token-input">API token</label>
        <input
          id="token-input"
          type="password"
          autoComplete="off"
          value={tokenInput}
          onChange={(e) => setTokenInput(e.target.value)}
          disabled={validating}
          aria-describedby={error ? 'auth-gate-error' : undefined}
          aria-invalid={error ? true : undefined}
        />
        {error && <p id="auth-gate-error" role="alert">{error}</p>}
        <button type="submit" disabled={validating}>
          {validating ? 'Verifying…' : 'Unlock'}
        </button>
      </form>
    </main>
  );
}
```

### `App.tsx` — router shell

```tsx
// web/src/App.tsx
import { BrowserRouter, Routes, Route, Navigate, NavLink } from 'react-router-dom';
import { AuthGate } from '@/auth/AuthGate';
import { clearToken } from '@/api/client';
import { Dashboard } from '@/pages/Dashboard';
import { Strategies } from '@/pages/Strategies';
import { Positions } from '@/pages/Positions';
import { BacktestLauncher } from '@/pages/BacktestLauncher';
import { BacktestResults } from '@/pages/BacktestResults';
import { Optimization } from '@/pages/Optimization';
import { ExecutionLog } from '@/pages/ExecutionLog';

const NAV_ITEMS = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/strategies', label: 'Strategies' },
  { to: '/positions', label: 'Positions' },
  { to: '/backtest', label: 'Backtest' },
  { to: '/optimize', label: 'Optimize' },
  { to: '/log', label: 'Execution Log' },
];

export function App() {
  return (
    <AuthGate>
      <BrowserRouter>
        <div className="app-shell">
          <nav aria-label="Primary">
            {NAV_ITEMS.map((item) => (
              <NavLink key={item.to} to={item.to} end={item.end}>
                {item.label}
              </NavLink>
            ))}
            <button
              type="button"
              onClick={() => {
                clearToken();
                window.location.reload(); // simplest correct way to fully reset
                                           // AuthGate's `unlocked` state, given
                                           // it lives outside the router tree
              }}
            >
              Log out
            </button>
          </nav>
          <main>
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/strategies" element={<Strategies />} />
              <Route path="/strategies/:id" element={<Strategies />} />
              <Route path="/positions" element={<Positions />} />
              <Route path="/backtest" element={<BacktestLauncher />} />
              <Route path="/backtest/:id" element={<BacktestResults />} />
              <Route path="/optimize" element={<Optimization />} />
              <Route path="/optimize/:id" element={<Optimization />} />
              <Route path="/log" element={<ExecutionLog />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </main>
        </div>
      </BrowserRouter>
    </AuthGate>
  );
}
```

`AuthGate` wraps `BrowserRouter`, not the reverse — the token gate is a pre-router concern (no route inside
the app should be reachable, even by direct URL entry, before a token exists), matching the blueprint's
framing of `AuthGate` as "renders *before* a token exists" rather than being one route among many.

### Reload-on-hard-refresh interaction with `go:embed`'s SPA fallback

Per blueprint §3, `webui.Handler()` serves `index.html` for any unmatched non-API, non-static path so
client-side routes survive a hard refresh (e.g. `GET /strategies/5` directly in the address bar). This means
`AuthGate`'s `unlocked` state (derived from `localStorage` at mount, not from an in-memory-only flag) is
exactly right for this scenario: a hard refresh on `/strategies/5` re-mounts the whole React tree, but
`getToken() !== null` still returns `true` if a token was previously stored, so the operator lands directly
back on the strategies detail route without re-entering the token — a real UX requirement this design must
preserve, not an incidental side effect.

## Acceptance Criteria

1. **Given** no token is stored (`localStorage.getItem('gtb_api_token') === null`), **when** the app first
   mounts, **then** `AuthGate` renders the token-entry form and none of `App.tsx`'s routed pages/`nav` render
   at all — not even in a disabled/greyed-out state.
2. **Given** the operator submits a blank token, **when** the form's `handleSubmit` runs, **then** it shows
   "Token is required." and makes no API call — client-side validation before any network round-trip.
3. **Given** the operator submits a token that `cmd/api` rejects with `401`, **when** the validation call
   resolves, **then** the token is cleared from `localStorage` (not left stored-but-rejected), an inline
   error appears next to the input (`role="alert"`, read by screen readers immediately per the accessibility
   checklist), and the entry form remains visible — no partial "half-authenticated" state.
4. **Given** the operator submits a token that `cmd/api` accepts (`GET /account` returns `200`), **when**
   validation resolves, **then** `unlocked` flips to `true` and the full app shell (nav + router) renders
   immediately, with the token now attached to every subsequent `api.*` call via `frontend-01`'s
   `Authorization` header logic.
5. **Given** the app is already unlocked and the operator is mid-session, **when** any page's API call
   later receives a `401` (e.g. the operator's token was rotated server-side, or the stored token expired
   under a future revision that adds expiry), **then** `onUnauthorized` fires, the token is cleared, and the
   UI drops back to the entry screen with a "Session expired…" message — this must work from **any** page,
   not just ones that explicitly know about `AuthGate`, since the handler is registered globally against
   `api/client.ts`.
6. **Given** the operator clicks "Log out," **when** the handler runs, **then** the token is cleared and the
   page reloads, landing back on the entry screen with the input field empty (not pre-filled with the old,
   now-cleared token).
7. **Given** a token was stored in a previous session, **when** the operator loads `:8080/strategies/5`
   directly (hard refresh / bookmarked deep link), **then** `AuthGate` reads the stored token, skips the
   entry screen entirely, and `App.tsx`'s router renders the `/strategies/:id` route directly — no
   redirect-to-dashboard-then-navigate-back detour.
8. **Edge case — `cmd/api` unreachable during validation**: **given** the network request in `handleSubmit`
   throws `NetworkError`, **when** the catch block runs, **then** the error message reads "Cannot reach the
   server. Is cmd/api running?" (distinct from the 401 message) and the token is still cleared (an
   unreachable server means the token was never actually confirmed valid — storing it "optimistically"
   would let the operator wrongly believe they're authenticated once the server comes back, when in fact a
   real backend restart might have rejected it).
9. **Accessibility**: the token input has an associated `<label>`, the submit button is reachable and
   activatable via keyboard alone (native `<button type="submit">`, no `onClick`-only div), and the error
   message is announced via `role="alert"` without requiring focus to move to it.

## Out of Scope

- Multi-user accounts, password reset, "remember me" toggles — ADR-009 explicitly rejects per-user auth for
  this single-operator context.
- Token rotation UI — ADR-009 Consequences: rotating means editing `config.yml`/`API_TOKEN` and restarting
  `cmd/api`; every browser tab independently re-authenticates via Acceptance Criterion #5's flow the next
  time it makes a call.
- Rate-limiting/lockout on repeated failed token attempts — ADR-009 explicit non-goal for this phase.
- httpOnly-cookie-based token storage — the blueprint's own "Open Questions" section leaves this open and
  recommends localStorage for v1; this spec follows that recommendation, not the cookie alternative.

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `api/client.ts`'s `getToken`/`setToken`/`clearToken`/
  `setUnauthorizedHandler`/`ApiError`/`NetworkError`, used directly.
- `docs/specs/web-frontend/backend-01-auth-middleware.md` — **reconciled** (landed during this spec-writing
  pass): (a) allowlist is `/metrics` and static assets, structurally enforced (only specific handlers are
  wrapped in `RequireAuth`) — matches this spec's assumption, no change needed; (b) a wrong-but-well-formed
  token does return `401` via `writeUnauthorized`, with a JSON body `{"error":"unauthorized","message":"..."}`
  — `handleSubmit`'s error branch now parses that `message` field directly (see Target Behavior); (c) no
  server-side token expiry exists (the token is valid until `cfg.APIToken` itself changes) — Acceptance
  Criterion #5's "session expired" framing is retained as a general umbrella message covering both a genuine
  future-expiry scenario and a mid-session server-side token rotation, since both present identically to the
  client (a previously-good token suddenly starts 401ing).
- `react-router-dom` — assumed dependency per the blueprint's "React Router" mention (§2); exact version
  left as a Phase A implementation-time choice (blueprint's own "Open Questions" precedent for low-stakes
  version pinning).

## Judgment Calls (flagged for explicit reconciliation)

1. **Validate-on-submit (a dedicated `GET /account` call before showing the app shell) was chosen over
   fail-on-first-real-call.** The blueprint's own text doesn't specify which; this spec's rationale (avoiding
   a flash-then-yank UX) is stated above. If backend-01 lands with a cheaper dedicated health/ping endpoint
   (rather than reusing `GET /account`, which does real DB work), swapping the validation call is a
   one-line change localized to `handleSubmit` — flagging in case a lighter-weight endpoint is preferred
   for this specific check.
2. **Logout is implemented as `clearToken()` + `window.location.reload()`**, not a React-state-only reset.
   This is simpler than plumbing `AuthGate`'s `unlocked` state down through a context consumed by a nav
   button, at the cost of a full page reload on logout (acceptable — logout is a rare, deliberate action, not
   a hot path where reload latency matters).
