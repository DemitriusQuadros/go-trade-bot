# Spec TUI-07 — Page 6: Execution Log (`cmd/console/pages/executionlog.go`)

## Overview

A live, scrolling, color-coded event feed (newest at top) with a filter bar (`[f]` filter by
strategy/event type, `[/]` text search). This page has the **largest gap between PRD intent and available
backend surface** of the six — see Judgment Call, which should be read before implementing this spec.

## Current Behavior (verified)

- No execution-log or event-feed page/concept exists anywhere in `cmd/console/` today.
- `entities.StrategyExecution{ID, Status, Message, StrategyID, Strategy, ExecutedAt}`
  (`app/entities/strategy.go:60-67`) is persisted via `StrategyRepository.SaveExecution` on every worker
  cycle (Phase 1 behavior, `app/handler/tasks/strategy/handler.go`), but **no REST endpoint reads it back**
  — confirmed by grep across `app/handler/web/`: no route pattern containing `execution` exists anywhere.
- `entities.Signal`/`entities.Order` lifecycle events (position opened, position closed at profit/loss,
  signal generated) have **no dedicated event log at all** — they're only ever visible as row-level state
  (`Signal.Status`, `Order.Profit`) via `GET /signal`, not as a chronological stream of "this happened at
  this timestamp" events distinct from the row's current state.
- PRD's six event-type color codes (green=position opened, yellow=signal generated, red=position closed at
  loss, blue=position closed at profit, gray=strategy cycle/no signal, white=system event) each map to a
  **different underlying data source or none at all**:

  | Event type | Data source today |
  |---|---|
  | strategy cycle (no signal) | `StrategyExecution{Status: "ok", Message: "no signal"}` — plausible, if `Message` is used this way; not confirmed against actual worker code in this pass |
  | system event | `StrategyExecution{Status: "error", ...}` — plausible for errors; no broader "system event" concept (e.g. worker restart, panic-recovery per blueprint §4 Phase 3's `strategy_panics_total` note) is exposed via any entity |
  | signal generated | **no entity** — a `Signal` row's `CreatedAt` implies "a signal was generated," but there's no distinct "generated" event separate from the row's existence |
  | position opened | **no entity** — inferred from a `Signal`'s first `Order` row's `CreatedAt`, not a dedicated event |
  | position closed at profit/loss | **no entity** — inferred from `Signal.Status` transitioning to `"closed"` plus the closing `Order.Profit`'s sign; nothing today timestamps *when* that transition happened distinct from `Order.UpdatedAt` |

## Target Behavior — proposed, pending backend reconciliation

```go
// cmd/console/apiclient/client.go (addition, also declared in tui-01)
func (c *Client) ListStrategyExecutions(ctx context.Context, since time.Time) ([]ExecutionEventView, error)
    // Proposed GET /strategy/executions?since=RFC3339 - covers ONLY the
    // "strategy cycle (no signal)" and "system event" rows honestly available
    // from entities.StrategyExecution today. Does NOT cover signal-generated/
    // position-opened/position-closed events - see Judgment Call.

type ExecutionEventView struct {
    Timestamp    time.Time
    StrategyName string
    Symbol       string // best-effort: StrategyExecution has no Symbol field today;
                         // proposed as a backend addition, or left empty client-side
    EventType    string // "cycle_no_signal" | "system_event" (this page's own two
                         // synthesizable types; the other four require the poll-and-
                         // diff approach below)
    Message      string
}
```

```go
// cmd/console/pages/executionlog.go
package pages

type ExecutionLogPage struct {
    Header       *widgets.Paragraph
    TabPane      *widgets.TabPane
    Dependencies *dependencies.Dependencies
    events       []LogEvent // merged, sorted newest-first, capped ring buffer (e.g. last 500)
    filterType   string     // "" = all, else one of the six PRD event-type keys
    filterQuery  string     // [/] search text, matched against Message/StrategyName/Symbol
    lastPollAt   time.Time
    knownSignals map[uint]entities_SignalSnapshot // for poll-and-diff synthesis, see below
}

// LogEvent is the page's own unified shape (superset of ExecutionEventView),
// combining backend-sourced StrategyExecution rows with client-synthesized
// signal-lifecycle events (see Judgment Call).
type LogEvent struct {
    Timestamp time.Time
    Strategy  string
    Symbol    string
    EventType string // one of the six PRD types
    Price     float64
    Message   string
}

func (p *ExecutionLogPage) Render() ui.Drawable
func (p *ExecutionLogPage) StartSync() // polls ListStrategyExecutions + GetOpenSignals/GetAllSignals every N seconds
func (p *ExecutionLogPage) HandleEvent(e ui.Event) error // [f] opens filter picker, [/] opens search input
```

### Client-side event synthesis (poll-and-diff)

Since no backend event log covers signal-generated/position-opened/position-closed today (see Current
Behavior), this page **synthesizes** those four event types by polling `GET /signal` (all signals, not
just open — needed to observe transitions into `"closed"`) on an interval (proposed: 5s, looser than
Page 3's 1s since this is a log, not a live-P&L display) and diffing against `knownSignals`:
- A signal ID seen for the first time with exactly one `Order` → **"position opened"** (green).
- A previously-open signal ID whose `Status` flipped to `"closed"` → **"position closed at
  profit"** (blue) if the closing order's `Profit > 0`, else **"position closed at loss"** (red).
- "Signal generated" (yellow) is conflated with "position opened" under this synthesis approach, since
  there's no entity distinguishing "a signal was generated" from "a signal immediately became a position"
  — flagged explicitly, see Judgment Call.

## ASCII Wireframe

```
┌ go-trade-bot ── 2026-08-31 14:44:20 ── ● CONNECTED ── Balance: $12,480.55 ──────────────────────────┐
├ [1 Dashboard] [2 Strategies] [3 Positions] [4 Backtest] [5 Results] [6 Log] ─────────────────────────┤
│ Filter: [All types ▾]   Search: [                              ]                                     │
│──────────────────────────────────────────────────────────────────────────────────────────────────────│
│ 14:44:18  grid_btc      BTCUSDT   ● position closed (profit)   $61,340.00   +$58.20                  │
│ 14:42:55  scalp_eth     ETHUSDT   ● signal generated           $3,015.42                              │
│ 14:41:02  bollinger_sol SOLUSDT   ● position closed (loss)     $141.20      -$12.40                  │
│ 14:40:30  grid_btc      BTCUSDT   ● strategy cycle (no signal)                                        │
│ 14:39:10  scalp_eth     ETHUSDT   ● position opened            $3,040.00                              │
│ 14:12:00  —             —         ● system event: worker restarted (panic recovery, strategy=grid_ada)│
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
[f] filter by strategy/type   [/] search   [F1-F6] switch page
```

(Color key, per PRD: green=opened, yellow=signal generated, red=closed-at-loss, blue=closed-at-profit,
gray=cycle/no-signal, white=system event — represented above as bullet markers since this is plain text.)

## Acceptance Criteria

1. **Given** `ListStrategyExecutions` returns rows with `Status: "ok"` and no associated signal activity,
   **when** the feed renders, **then** they appear as gray "strategy cycle (no signal)" entries.
2. **Given** `ListStrategyExecutions` returns a row with `Status: "error"`, **when** the feed renders,
   **then** it appears as a white "system event" entry showing the `Message` field verbatim.
3. **Given** the poll-and-diff logic (Target Behavior) observes a signal transitioning from absent to
   present with one order, **when** the next poll completes, **then** a green "position opened" event is
   synthesized with that timestamp being the **poll time**, not the signal's true `CreatedAt` — **edge
   case, flagged**: because this is poll-based (5s interval), the displayed timestamp for a synthesized
   event lags the true event time by up to one poll interval; the event's `Timestamp` field should use
   `Signal.CreatedAt`/`Order.UpdatedAt` (the true event time from the payload) rather than
   `time.Now()` at poll-detection time, to keep the feed's ordering and displayed times accurate even
   though *detection* is delayed.
4. **Given** `[f]` is pressed, **when** the filter picker opens, **then** it lists exactly the six PRD
   event types plus "All," and selecting one hides all `events` not matching `EventType`.
5. **Given** `[/]` is pressed and a search string is typed, **when** the feed re-renders, **then** it shows
   only events whose `Message`, `Strategy`, or `Symbol` contains the (case-insensitive) search string —
   combinable with an active type filter (both conditions AND'd together).
6. **Given** the event buffer reaches its cap (proposed 500 events), **when** a new event arrives,
   **then** the oldest event is dropped — this page does not attempt to be a complete historical audit
   log; that's the backend's `StrategyExecution`/`Signal` tables' job, queryable directly if a full
   history is ever needed outside the TUI.
7. **Edge case — `cmd/api` unreachable during a poll**: **when** a poll fails, **then** the feed keeps
   showing its existing buffered events (no clearing) and a small inline indicator (e.g. the header's
   existing connection dot, tui-02) reflects the disconnected state — consistent with every other page's
   degraded-mode behavior (tui-01 Acceptance Criterion #3).

## Out of Scope

- A true backend-persisted, unified event log table — this spec's poll-and-diff synthesis is an explicit
  stopgap (see Judgment Call), not a proposal to leave the underlying data model unaddressed forever.
- Exporting/searching historical events beyond the in-memory 500-event ring buffer's lifetime (i.e., after
  a console restart, the feed starts empty and rebuilds from that point forward) — no "load older events"
  pagination against `ListStrategyExecutions`/`GET /signal` history is built for this page.

## Dependencies

- `tui-01-apiclient.md` — `ListStrategyExecutions` (proposed net-new), reuses `GetOpenSignals`'s sibling
  "all signals" call (a `GET /signal` wrap, not `GetOpenSignals` itself, which is open-only).
- `docs/architecture/refactoring-blueprint.md` §4 Phase 3 — the `strategy_panics_total` counter and
  webhook-alert-on-panic-recovery behavior (`app/engine/engine.go` modification) is the closest existing
  design to a genuine "system event" source; if that panic-recovery path also wrote a
  `StrategyExecution{Status: "error"}` row (not currently specified either way), this page's white
  "system event" category would have a concrete data source rather than being purely aspirational.

## Judgment Call (the largest open gap in this spec set)

**Four of the six PRD event types have no dedicated backend event source today, and this spec's
poll-and-diff synthesis is a workaround, not a clean solution.** The honest options, in order of
increasing backend investment:

1. **Ship this spec's poll-and-diff approach** (documented above): zero new backend write paths, but
   fragile — misses events between polls if a signal opens *and* closes within one 5s window (a fast
   scalping strategy could plausibly do this), and "signal generated" is indistinguishable from "position
   opened" under this model since nothing marks the moment a signal is generated separately from a
   position being opened.
2. **Add a dedicated `entities.ExecutionEvent` table**, written at each of the four missing lifecycle
   points (`GenerateBuySignal`/`GenerateSellSignal` in `app/usecase/signal/usecase.go`, which already own
   exactly those four transition points) plus the existing `StrategyExecution` write sites, with one new
   `GET /events?since=` endpoint replacing this spec's two-source poll-and-diff. This is the architecturally
   clean answer but is real backend work not currently scoped in the blueprint's Phase 3 file list
   (`app/usecase/signal/usecase.go` is listed as `[MOD]` for a different reason — Spec 01's
   `ExchangeClient.PlaceOrder` call — and could absorb this in the same pass, but that's a scope decision
   for whoever owns Phase 1's signal usecase, not this TUI spec).

This spec adopts option 1 to keep Phase 3's TUI work unblocked by a backend change nobody has committed
to, but flags option 2 explicitly as the correct long-term fix, and recommends raising it with whoever
authors `backend-01-tui-consumed-api-endpoints.md` before Page 6 implementation actually starts, since
building the poll-and-diff logic is throwaway work if option 2 is adopted instead.
