# Spec frontend-09 — Execution Log Page (`web/src/pages/ExecutionLog.tsx`)

## Overview

Web equivalent of the deleted TUI's Page 6 (`docs/specs/phase-3/tui-07-page-execution-log.md`) — the page
with, by a wide margin, the largest gap between desired feature and available backend surface of any page in
this spec set (a gap the TUI's own spec called out explicitly as "the largest open gap in this spec set,"
and nothing has closed it since — no `entities.ExecutionEvent` table, no unified `GET /events` endpoint
exists anywhere in the codebase, confirmed by grep). This spec makes an explicit design call on how to adapt
the TUI's poll-and-diff stopgap to a web-native pattern — see "Web-native redesign decision" below, read
before the rest of this spec.

## Current Behavior

- No web execution-log page exists. Feature-parity reference: `tui-07-page-execution-log.md`.
- `entities.StrategyExecution{ID, Status, Message, StrategyID, Strategy, ExecutedAt}` is persisted on every
  worker cycle but **no REST endpoint reads it back** — confirmed unchanged since the TUI spec's own
  verification pass (still zero route matches for "execution" across `app/handler/web/`, re-confirmed during
  this spec-writing pass).
- Four of the six PRD-named event types (signal generated, position opened, position closed at profit/loss)
  have **no dedicated backend event source** — only inferrable by polling `GET /signal` (all, unfiltered)
  and diffing against previously-seen state, exactly as the TUI's spec documented.
- `GET /signal` (no `status` param) returns every signal, open and closed, each with its `orders[]` —
  the diffing input this page (like the TUI) must use. Reconciled against `frontend-01`'s corrected
  `api/types.ts` (itself reconciled against `backend-04-api-consistency-fixes.md`): the response is
  snake_case `SignalResponseDTO[]`, and each signal carries `strategy_id` only — **no nested
  `strategy`/`Strategy` object with a display name**. This page's event synthesis (Target Behavior, below)
  must resolve a human-readable strategy name the same way `frontend-05` does: by cross-referencing
  `strategy_id` against a separately-fetched `GET /strategy` list, not by reading a nested path off the
  signal payload.

## Web-native redesign decision

The task brief for this spec explicitly invites either "feature-parity poll-and-diff, adapted to a web
polling/table view" or "a cleaner web-native approach... your call, justify it." **This spec chooses to keep
the same underlying poll-and-diff data strategy** (no new backend event table is invented — that remains
outside this docs-only task's scope, same as every other spec in this set that flags a backend gap without
resolving it) but changes **how the resulting event stream is presented and scaled**, in three concrete ways
that are genuinely web-native rather than a straight terminal-to-HTML port:

1. **Client-side event history persists across a page navigation (React state lives in a parent boundary /
   is lifted, or backed by `IndexedDB`/`sessionStorage`), not just an in-memory ring buffer that resets on
   every page mount.** The TUI's 500-event ring buffer reset to empty on every console restart (explicit
   Out of Scope in `tui-07`); a browser tab that never fully reloads (client-side routing, per this app's
   SPA nature) can reasonably keep accumulating a much larger buffer across the whole session, and
   `sessionStorage` persistence (cleared only on tab close) is a cheap, genuinely-better-than-parity
   improvement a terminal session couldn't offer for free.
2. **A paginated/virtualized infinite-scroll list, not a fixed-height terminal pane** — chosen over a plain
   unbounded `<table>` because the accumulated event history (per point 1, now potentially thousands of rows
   across a long session) needs windowed rendering to stay performant; `react-window`/`react-virtual`-style
   virtualization (exact library choice left open, matching the blueprint's own low-stakes styling
   deferrals elsewhere) renders only the visible slice.
3. **Full-text search across all fields plus CSV export** — both explicitly named in blueprint §7 as this
   page's "meaningfully more" web capabilities, and both are naturally web-native additions with no TUI
   equivalent to preserve parity with (`[/]`'s search was Message/Strategy/Symbol only, not "all fields";
   CSV export didn't exist in the TUI's keybinding set at all).

This is **not** a proposal to solve the underlying data-model gap (the TUI's own Judgment Call's "option 2":
a dedicated `entities.ExecutionEvent` table + `GET /events?since=` endpoint remains the architecturally
correct long-term fix, and is re-flagged below, not silently dropped).

## Target Behavior

### Layout — component composition

```
ExecutionLog.tsx
├── PageHeader — "Execution Log", connection-status badge
├── FilterBar
│    ├── EventTypeFilter — multi-select checkboxes (not a single [f] cycle-picker like the TUI —
│    │    a web form can let the operator select several types at once, e.g. "show closes and
│    │    system events but hide routine no-signal cycles," which the TUI's single-select picker
│    │    could not express at all)
│    ├── StrategyFilter — <select>, sourced from distinct resolved strategy names seen in the
│    │    buffer (LogEvent.strategy — already resolved from strategy_id, per Current Behavior;
│    │    never a raw strategy_id or a nested-object field, since neither exists on the wire)
│    ├── SearchInput — full-text, debounced (300ms), matched against every field (message,
│    │    strategy, symbol, event type label) — blueprint §7's "full-text search across all
│    │    fields (not just filter-by-strategy/event-type)"
│    └── "Export CSV" button — exports the CURRENTLY FILTERED view, not always the full buffer
│         (a deliberate choice: exporting exactly what's on screen matches operator intent more
│         often than a hidden "export everything regardless of filters" behavior)
└── VirtualizedEventList — windowed rendering (frontend-10 or a dedicated small wrapper around
     a virtualization library), newest-first, one row per LogEvent:
     time | strategy | symbol | event type (colored dot + text label — never color alone) | message/price
```

### Client-side event synthesis (poll-and-diff, ported from the TUI with one behavioral change)

```ts
// web/src/pages/ExecutionLog.tsx (sketch)
interface LogEvent {
  id: string;          // synthetic, e.g. `${source}-${signalId ?? executionId}-${timestamp}`
  timestamp: string;   // the TRUE event time (signal.created_at / order.updated_at, both snake_case
                        // per SignalResponseDTO/OrderResponseDTO; a StrategyExecution row's
                        // executed_at once a backing endpoint exists — see Dependencies), NEVER
                        // poll-detection time — same correction the TUI's tui-07 AC#3 already made
  strategy: string;     // resolved display name, NOT read off the signal payload (SignalResponseDTO
                        // has no nested strategy object) — looked up from strategy_id against a
                        // separately-fetched GET /strategy list, same pattern as frontend-05
  symbol: string;
  eventType: 'position_opened' | 'position_closed_profit' | 'position_closed_loss' |
             'signal_generated' | 'cycle_no_signal' | 'system_event';
  price?: number;
  message: string;
}

// Polls GET /signal (all) + GET /strategy (for the strategy_id -> name lookup,
// same as frontend-05) + GET /strategy/executions (proposed net-new endpoint,
// see Dependencies — NOT confirmed to exist) every 5s, diffs against
// previously-seen signal ids/StrategyExecution ids, and appends newly-detected
// events to the persisted buffer (sessionStorage-backed, see Web-native
// redesign decision point 1).
```

### CSV export

```ts
function exportCsv(events: LogEvent[]): void {
  const header = 'timestamp,strategy,symbol,event_type,price,message\n';
  const rows = events.map((e) =>
    [e.timestamp, e.strategy, e.symbol, e.eventType, e.price ?? '', `"${e.message.replace(/"/g, '""')}"`].join(','),
  );
  const blob = new Blob([header + rows.join('\n')], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `execution-log-${new Date().toISOString()}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}
```

## Acceptance Criteria

1. **Given** the poll-and-diff logic observes a signal transitioning from absent to present with exactly one
   order, **when** the next poll completes, **then** a "position opened" event is synthesized using
   `signal.created_at` as its `timestamp` (the true event time), not the poll-detection wall-clock time —
   matching `tui-07` AC#3's correction.
2. **Given** a previously-open signal's `status` flips to `"closed"`, **when** the next poll detects it,
   **then** a "position closed at profit" or "position closed at loss" event is synthesized based on the
   closing order's `profit` sign — matching `tui-07`'s synthesis rule (Target Behavior).
3. **Given** the operator selects multiple event types in `EventTypeFilter` (e.g. both closed-at-loss and
   system-event), **when** the list re-renders, **then** it shows the union of matching events — a strictly
   more expressive filter than the TUI's single-select `[f]` picker, per this spec's web-native redesign.
4. **Given** a search string is entered, **when** the list re-renders (after the 300ms debounce), **then**
   it shows only events matching the search **and** the active type/strategy filters (AND'd together) —
   matching `tui-07` AC#5's combinability rule, extended to "all fields" per the redesign decision.
5. **Given** the operator clicks "Export CSV," **when** the export fires, **then** the downloaded file
   contains exactly the currently-filtered/searched rows, in the same newest-first order shown on screen —
   not the full unfiltered buffer.
6. **Given** the accumulated event buffer grows large (thousands of rows across a long session), **when**
   the list renders, **then** only the visible window's rows are actually mounted in the DOM (virtualized) —
   scrolling remains smooth regardless of total buffer size, a genuine improvement over the TUI's fixed
   500-event cap (this page's cap, if any, is a pragmatic memory/sessionStorage-size limit, proposed at
   5,000 events, not a rendering-performance limit).
7. **Given** `cmd/api` becomes unreachable during a poll, **when** the poll fails, **then** the list keeps
   showing its existing buffered events (no clearing) and the connection-status badge reflects disconnected
   — matching `tui-07` AC#7's degraded-mode principle.
8. **Given** the operator navigates away from the Execution Log page and back within the same browser tab
   session, **when** the page remounts, **then** the previously-accumulated event buffer is restored from
   `sessionStorage` rather than starting empty — the concrete implementation of this spec's Web-native
   redesign decision point 1.
9. **Edge case — tab closed and reopened**: **given** the operator closes the browser tab entirely and later
   reopens `:8080`, **when** the Execution Log page is visited, **then** the buffer starts empty and rebuilds
   forward from that point — `sessionStorage` (not `localStorage`) is the deliberate choice here, since a
   fresh session with no history is the honest state (this page is explicitly not a historical audit log;
   the backend tables are, if queried directly, matching the TUI's own stated non-goal).
10. **Accessibility**: each event row's colored dot has an adjacent text label naming the event type (never
    color-only), the filter checkboxes are each independently labeled, and new events arriving via polling
    do **not** aggressively steal focus or trigger a screen-reader announcement storm — an `aria-live="polite"`
    region announces only a periodic summary ("3 new events") rather than reading every row aloud as it
    streams in.

## Out of Scope

- A true backend-persisted, unified event log table (`entities.ExecutionEvent` + `GET /events?since=`) — this
  remains the architecturally correct long-term fix (per the TUI's own Judgment Call option 2) but is
  backend work outside this docs-only task's scope; this spec's poll-and-diff approach is knowingly a
  stopgap, not a proposal to leave the data model unaddressed forever.
- Server-side pagination/historical replay beyond the client's own accumulated session buffer — no
  "load older events from before this session" capability, since no backend endpoint exists to serve that
  (the proposed `GET /strategy/executions?since=` is itself unconfirmed to exist).
- Distinguishing "signal generated" from "position opened" as genuinely separate events — same unresolved
  conflation the TUI's spec flagged (nothing in the data model marks the moment a signal is generated
  separately from a position being opened).

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `api.get`, `Signal`/`Strategy` types (snake_case, reconciled
  against `backend-04-api-consistency-fixes.md`; `Signal` has `strategy_id` only, no nested `strategy`
  object — this page's synthesis logic resolves the display name via the `Strategy[]` list, same pattern
  as `frontend-05`).
- `frontend-05-page-positions.md` — the `strategy_id`-to-name resolution pattern this page reuses.
- `frontend-10-shared-components.md` — `usePolling` (adapted here for a diff-and-append pattern rather than
  a plain "replace with latest" pattern — this page's own hook variant, not the base `usePolling` unchanged).
- `docs/specs/phase-3/tui-07-page-execution-log.md` — feature/data-parity checklist and the poll-and-diff
  synthesis rules ported directly.
- **Proposed net-new endpoint** `GET /strategy/executions?since=` — same status as when the TUI's own spec
  proposed it: **not confirmed to exist**, not scoped in any landed backend spec found during this
  spec-writing pass (re-verified: still zero grep hits for "execution" as a route pattern). Without it, the
  "strategy cycle (no signal)" and "system event" categories have **no data source at all** for this web
  page either (not just the TUI) — the poll-and-diff logic above can only actually synthesize the
  signal-lifecycle-derived event types (opened/closed-profit/closed-loss) from `GET /signal` alone. Flagging
  this as a still-open, still-unresolved gap this spec inherits unchanged from its TUI predecessor, not a
  new problem this rewrite introduces.

## Judgment Calls (flagged for explicit reconciliation)

1. **Poll-and-diff (not a from-scratch unified event backend) was kept as this spec's data strategy**,
   with the redesign effort spent instead on presentation/scale (virtualized infinite list, richer
   multi-select filtering, full-text search, CSV export, session-persistent buffer) rather than on closing
   the underlying data-model gap. Justification: this docs-only task's scope is the frontend; inventing a new
   `entities.ExecutionEvent` table and its write sites (`GenerateBuySignal`/`GenerateSellSignal` in
   `app/usecase/signal/usecase.go`, per the TUI spec's own proposed option 2) is backend work requiring a
   decision from whoever owns that usecase, not something this frontend spec can resolve unilaterally.
   Flagging clearly, as instructed, since "propose a cleaner approach if you think it fits" was explicitly
   invited — this spec's answer is "the cleaner approach is presentation-layer, the data-layer fix is still
   owed and still not this task's to make."
2. **`sessionStorage` (not `localStorage`) backs the event buffer.** `localStorage` would survive a tab
   close/reopen, which sounds like a nice-to-have, but silently accumulating a stale, ever-growing execution
   history across days/weeks of separate sessions without any TTL/rotation policy risks unbounded storage
   growth and, worse, showing the operator "recent" events that are actually days old with no visual
   distinction — `sessionStorage`'s natural clear-on-tab-close behavior sidesteps that entirely at the cost
   of losing history on tab close, which this spec judges to be the correct trade-off for a page that's
   explicitly not a historical audit log. Flagging as a real trade-off, not an obviously-correct default.
