# Spec frontend-12 — Real-Time Updates via SSE (`web/src/hooks/useSSE.ts`)

## Overview

Phase E, per blueprint §5/ADR-010. The additive real-time polish phase: a `useSSE` hook, its
reconnection/backoff behavior, and which pages/components switch from polling to SSE consumption. Ships
**only after** Phases A-D are built and real usage shows polling staleness is actually a problem — this spec
documents the design so it's ready to build then, not a claim that it's needed immediately.

## Current Behavior

- No SSE (or any push-based transport) exists anywhere in this codebase. `cmd/api` has no `EventSource`-
  compatible endpoint, no WebSocket server, nothing beyond plain request/response HTTP.
- Every `frontend-03` through `frontend-09` page spec is built on `usePolling` (`frontend-10`) as their sole
  data-freshness mechanism — this spec is additive to that, not a replacement of it (see Target Behavior's
  fallback requirement).
- `docs/specs/web-frontend/backend-03-sse-realtime-endpoint.md` — **reconciled** (landed during this
  spec-writing pass, after this spec's first draft). Real contract confirmed below, replacing this spec's
  original `/events`-with-`account`/`positions`/`prices`-events guess.

## Target Behavior

### Confirmed endpoint contract (per `backend-03-sse-realtime-endpoint.md`)

```
GET /stream/dashboard?token=<APIToken>
  Auth: bearer token via ?token= query parameter — a documented, deliberate exception to
    backend-01's Authorization-header-only rule, added specifically because EventSource cannot
    set custom request headers. backend-01's extractBearerToken checks the header FIRST and only
    falls back to ?token= if no header is present, so this exception is scoped narrowly (see
    Judgment Call #1's update below).
  Content-Type: text/event-stream

  event: price_update
  data: {"symbol": "BTCUSDT", "price": 50123.45, "timestamp": "2026-09-06T12:00:00Z"}

  event: position_update
  data: {"signal_id": 12, "symbol": "BTCUSDT", "strategy_id": 3, "entry_price": 49800.0,
         "current_price": 50123.45, "unrealized_pnl": 12.34, "unrealized_pnl_pct": 0.65,
         "quantity": 0.01, "opened_at": "2026-09-06T10:00:00Z"}

  event: heartbeat
  data: {"timestamp": "2026-09-06T12:00:15Z"}
```

One `price_update` per distinct monitored symbol and one `position_update` per open signal, **not** a single
batched payload — this spec's `useSSE`/`useLiveData` design (below) is updated to treat each event as a
per-resource delta requiring client-side accumulation into a map (keyed by `symbol`/`signal_id`), not a
full-replace of a single `data` blob, which is a real correction from this spec's original draft's assumption
of one array/object per event. `heartbeat` fires every 15s regardless of data changes specifically so the
client can detect a silently-dead connection (a proxy/load-balancer buffering the stream without actually
closing it, the same failure mode this spec's Target Behavior originally flagged as a risk `onerror` alone
won't catch) — `useSSE` is updated below to treat "no `heartbeat` in >2x its interval" as a synthetic error
triggering the same backoff-and-retry path as a real `onerror`.

There is **no dedicated `account` event** in the confirmed contract — account balance is not part of this
endpoint's scope (only prices and positions are, matching blueprint §9 Phase E's literal "Dashboard/Positions"
naming, where "Dashboard" turns out to mean this page's price data specifically, not its account summary).
`AccountSummaryCard` (`frontend-03`) therefore stays on `usePolling` permanently in this phase, not
`useLiveData` — correcting this spec's own Target Behavior table below.

### `useSSE` hook

```ts
// web/src/hooks/useSSE.ts
import { useEffect, useRef, useState } from 'react';
import { getToken } from '@/api/client';

interface UseSSEOptions<T> {
  eventName: string;         // e.g. "account", "positions", "prices"
  parse?: (raw: string) => T; // default: JSON.parse
  enabled?: boolean;
}

interface UseSSEResult<T> {
  data: T | null;
  connected: boolean; // true once the EventSource's readyState is OPEN; false on error/reconnect-pending
}

const BACKOFF_SCHEDULE_MS = [1000, 2000, 5000, 10000, 30000]; // capped exponential backoff, ceiling 30s
const HEARTBEAT_INTERVAL_MS = 15000; // matches backend-03's confirmed 15s heartbeat cadence
const HEARTBEAT_TIMEOUT_MS = HEARTBEAT_INTERVAL_MS * 2; // no heartbeat in 30s => treat as dead

// Single shared connection to GET /stream/dashboard — per backend-03's confirmed
// contract, ALL THREE event types (price_update, position_update, heartbeat) ride
// the same EventSource, so useSSE (unlike this spec's original per-eventName-
// connection draft) opens exactly one stream and demuxes by event name
// internally. eventName selects which accumulated map this hook instance returns.
export function useSSE<T>({ eventName, parse = JSON.parse, enabled = true }: UseSSEOptions<T>): UseSSEResult<T> {
  const [data, setData] = useState<T | null>(null);
  const [connected, setConnected] = useState(false);
  const attemptRef = useRef(0);

  useEffect(() => {
    if (!enabled) return;
    let es: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout>;
    let heartbeatTimer: ReturnType<typeof setTimeout>;
    let cancelled = false;

    function resetHeartbeatWatchdog() {
      clearTimeout(heartbeatTimer);
      heartbeatTimer = setTimeout(() => {
        // No heartbeat (or any event) in 2x the expected interval — the
        // connection is silently dead (e.g. a buffering proxy). EventSource
        // itself won't fire onerror for this case, so this watchdog is the
        // only way to detect it.
        es?.close();
        setConnected(false);
        if (!cancelled) scheduleReconnect();
      }, HEARTBEAT_TIMEOUT_MS);
    }

    function scheduleReconnect() {
      const delay = BACKOFF_SCHEDULE_MS[Math.min(attemptRef.current, BACKOFF_SCHEDULE_MS.length - 1)];
      attemptRef.current += 1;
      retryTimer = setTimeout(connect, delay);
    }

    function connect() {
      // Token via ?token= query param — backend-01's extractBearerToken checks
      // the Authorization header first and falls back to this param only when
      // absent, per backend-03's confirmed, narrowly-scoped exception (see
      // Judgment Call #1).
      es = new EventSource(`/stream/dashboard?token=${encodeURIComponent(getToken() ?? '')}`);
      es.addEventListener(eventName, (e: MessageEvent) => {
        setData(parse(e.data));
        resetHeartbeatWatchdog();
      });
      es.addEventListener('heartbeat', () => resetHeartbeatWatchdog());
      es.onopen = () => {
        attemptRef.current = 0;
        setConnected(true);
        resetHeartbeatWatchdog();
      };
      es.onerror = () => {
        setConnected(false);
        es?.close();
        clearTimeout(heartbeatTimer);
        if (!cancelled) scheduleReconnect();
      };
    }

    connect();
    return () => {
      cancelled = true;
      es?.close();
      clearTimeout(retryTimer);
      clearTimeout(heartbeatTimer);
    };
  }, [eventName, enabled, parse]);

  return { data, connected };
}
```

**Per-resource accumulation, not full-replace**: since `price_update`/`position_update` each carry one
symbol/signal's delta (not a full array), the actual page-level consumer (not shown in this generic hook)
must accumulate events into a `Map<string, PriceUpdate>`/`Map<number, PositionUpdate>` keyed by
`symbol`/`signal_id` respectively, merging each incoming event into the existing map rather than replacing
it wholesale — a correction from this spec's original single-blob assumption, reflected in the updated
Target Behavior table below.

### Polling-to-SSE handoff pattern (per page)

Rather than each page choosing one exclusive mode, this spec proposes a small composing hook that prefers
SSE when connected and silently falls back to the existing `usePolling` result otherwise — no page-level
`if (sseSupported)` branching, and no page is ever left with zero data source if SSE is unavailable
(matches blueprint §5's explicit "additive, not a replacement" framing):

```ts
// web/src/hooks/useLiveData.ts (proposed, composes the two existing hooks)
export function useLiveData<T>(
  pollingFetcher: (signal: AbortSignal) => Promise<T>,
  sseEventName: string,
  opts: { pollIntervalMs: number; sseEnabled: boolean },
): { data: T | null; connected: boolean; source: 'sse' | 'polling' } {
  const polling = usePolling(pollingFetcher, { intervalMs: opts.pollIntervalMs, enabled: !opts.sseEnabled || true });
  const sse = useSSE<T>({ eventName: sseEventName, enabled: opts.sseEnabled });

  if (opts.sseEnabled && sse.connected && sse.data !== null) {
    return { data: sse.data, connected: true, source: 'sse' };
  }
  return { data: polling.data, connected: polling.connected, source: 'polling' };
}
```

**Note**: keeping `usePolling` running (`enabled: !opts.sseEnabled || true` — i.e., always enabled) even
while SSE is connected is a deliberate redundancy, not a bug — it guarantees a page never goes fully dark if
SSE silently stalls without firing `onerror` (a known real-world SSE failure mode behind certain
proxies/load balancers that buffer responses). This redundancy is acceptable specifically because Phase E's
whole premise is "the server-side poll is already centralized" (blueprint §5) — the client falling back to
its own independent poll on an SSE hiccup is a small amount of duplicate work, not a full loss of the
Phase E efficiency win, since it's a fallback path, not the steady-state behavior.

### Which pages/components switch

Per blueprint §5/§9 Phase E ("SSE endpoint for Dashboard/Positions only"):

| Page/component | Switches to `useLiveData` | Stays polling-only |
|---|---|---|
| `frontend-03` Dashboard — `AccountSummaryCard` | | ✅ — **corrected**: `backend-03`'s confirmed contract has no `account` event at all; balance is out of this endpoint's scope entirely, not merely deferred |
| `frontend-03` Dashboard — `PairCard` prices | ✅ (`price_update`, accumulated per-symbol) | |
| `frontend-05` Positions — `PositionCard` list + P&L | ✅ (`position_update`, accumulated per-`signal_id`; note this event carries a server-computed `unrealized_pnl`/`unrealized_pnl_pct` directly, so the client no longer needs to combine a separate price feed with client-side P&L math the way the polling path does) | |
| `frontend-04` Strategies (list, detail, history) | | ✅ — no strategy-mutation event is proposed in backend-03's scope; writes are infrequent, discrete, and already get optimistic-update instant feedback (frontend-04's own Acceptance Criteria), so polling staleness here is a non-issue the way live P&L staleness is |
| `frontend-06`/`frontend-07` Backtest pages | | ✅ — one-shot/completed-run data, not a live stream by nature |
| `frontend-08` Optimization | | ✅ — already has its own 2s progress-poll loop specific to one in-flight run, not a candidate for a general fan-out stream |
| `frontend-09` Execution Log | | ✅ — deliberately poll-and-diff by design (`frontend-09`'s own spec), not folded into this SSE endpoint's scope |

## Acceptance Criteria

1. **Given** `backend-03`'s SSE endpoint is available and the browser's `EventSource` connects successfully,
   **when** an `account` event arrives, **then** `useLiveData`'s `source` is `'sse'` and the Dashboard's
   `AccountSummaryCard` updates from the pushed payload without waiting for its own poll interval to elapse.
2. **Given** the SSE connection drops (network blip, server restart), **when** `EventSource.onerror` fires,
   **then** `connected` flips to `false`, and `useLiveData` falls back to `polling`'s `data`/`connected`
   immediately (no gap where the page shows neither) — the underlying `usePolling` instance was never
   stopped, so its most recent value is already available to fall back to instantly.
3. **Given** the SSE connection is down, **when** reconnection is attempted, **then** it follows the capped
   exponential backoff schedule (`1s, 2s, 5s, 10s, 30s, 30s, ...`) rather than either a tight retry loop
   (hammering `cmd/api`) or a single fixed long interval (slow to recover from a brief blip).
4. **Given** the SSE connection successfully re-opens after a backoff period, **when** `onopen` fires,
   **then** the backoff counter resets to the schedule's first step (`1s`) for any *future* disconnect —
   a fresh disconnect is never penalized with an inherited long backoff from a prior, unrelated outage.
5. **Given** a component using `useLiveData` unmounts, **when** cleanup runs, **then** the `EventSource` is
   closed and any pending backoff `setTimeout` is cleared — no reconnect attempt fires after unmount.
6. **Given** multiple browser tabs are open simultaneously (blueprint §9 Phase E's stated success signal:
   "across multiple simultaneously open browser tabs sharing one upstream poll"), **when** each tab opens its
   own `EventSource` connection, **then** all tabs receive the same pushed data from the server's single
   upstream poll — this is a backend-03 behavior this spec's client-side hook doesn't implement itself, but
   depends on and should be verified against once backend-03 lands (the client makes no assumption about how
   many *other* tabs exist; each tab's `useSSE` instance is independent).
7. **Edge case — token becomes invalid mid-stream** (e.g. rotated server-side): **given** the SSE connection
   receives a `401`-equivalent rejection (however `backend-03` signals it over an `EventSource`, which has no
   native way to surface HTTP status codes to JS beyond `onerror` firing) **when** this occurs, **then**
   `useSSE` treats it identically to any other connection error (backoff-and-retry) — it does **not** attempt
   to distinguish "auth failure, stop retrying" from "transient network failure, keep retrying" in this v1,
   since `EventSource`'s API gives no clean signal to tell them apart without a custom protocol addition (see
   Judgment Call #2). A token that's genuinely dead will simply keep failing every retry, silently falling
   back to (and staying on) `polling`'s own `onUnauthorized` handling (`frontend-02`) instead — which already
   correctly surfaces the re-auth prompt.

## Out of Scope

- WebSocket — rejected outright per ADR-010; nothing in this spec revisits that decision.
- `cmd/worker`-side Redis pub/sub for genuine push (blueprint §5's option (b)) — named as a future upgrade
  path, not built in this phase.
- Any UI control for the operator to manually toggle "prefer SSE vs. polling" — the fallback in
  `useLiveData` is fully automatic and invisible; no settings panel exposes this choice.
- Applying SSE to Strategies/Backtest/Optimization/Execution-Log pages — explicitly out of scope per the
  blueprint's own Phase E framing ("Dashboard/Positions views... in a later phase"), and this spec's
  Target Behavior table states the reasoning per page.

## Dependencies

- `frontend-03-page-dashboard.md`, `frontend-05-page-positions.md` — the two page specs whose `usePolling`
  call sites are upgraded to `useLiveData` in this phase.
- `frontend-10-shared-components.md` — `usePolling`, reused as the fallback data source, unmodified.
- `frontend-02-auth-gate.md` — `getToken()`/`onUnauthorized`, reused for the query-param token attachment
  and for the ultimate re-auth fallback path (Acceptance Criterion #7).
- `docs/specs/web-frontend/backend-03-sse-realtime-endpoint.md` — **reconciled** (landed during this
  spec-writing pass). Confirmed: single endpoint `GET /stream/dashboard`, three event types
  (`price_update`/`position_update`/`heartbeat` at a 15s cadence) on one shared connection, `?token=` query
  param as a documented, narrowly-scoped fallback to `backend-01`'s header-based auth (header checked
  first). No `account` event exists — corrected in the Target Behavior table above.

## Judgment Calls (flagged for explicit reconciliation)

1. **RESOLVED by `backend-03`**: token is passed as an `EventSource` URL query param (`?token=...`), exactly
   as this spec's original draft anticipated would be necessary given `EventSource`'s inability to set custom
   headers. `backend-03`'s own spec confirms it considered and rejected a short-lived-ticket alternative (a
   `POST /stream/dashboard/ticket` mint-then-connect flow) as "more moving parts" for a single-operator
   homelab context — the plain long-lived-token-in-query-param approach was chosen instead, with the same
   known risk this spec's original draft flagged (token in server access logs/browser history) explicitly
   accepted rather than engineered around. `useSSE`'s `connect()` above uses the raw stored token directly,
   matching this confirmed design — no ticket-minting step needed.
2. **`useSSE` still cannot cleanly distinguish an auth failure from a transient network failure on
   `EventSource.onerror`** — `backend-03`'s spec does not introduce a custom sub-protocol for this either
   (confirmed: no `event: auth_error` or equivalent is defined). This spec's Acceptance Criterion #7 fallback
   (treat every error identically, let a genuinely-dead token surface via the REST-polling fallback path's
   existing `onUnauthorized` handling instead) remains the correct v1 answer, now confirmed as the only
   answer available given the finalized contract, not merely this spec's own placeholder guess.
3. **New**: the heartbeat-watchdog mechanism (`HEARTBEAT_TIMEOUT_MS`, added during reconciliation) is this
   spec's own addition, not specified by `backend-03` itself — that spec defines the heartbeat's existence
   and 15s server-side cadence but leaves client-side dead-connection detection as the consumer's
   responsibility (implied by its own text: "`EventSource` itself doesn't expose a 'haven't heard anything in
   a while' signal, only 'the connection formally closed'"). This spec's 2x-interval timeout (30s) is a
   reasonable but arbitrary choice — tune if real-world proxy/load-balancer buffering behavior in this
   project's actual deployment (nginx, per CLAUDE.md) turns out to need a different margin.
