# Spec TUI-03 — Page 2: Strategies (`cmd/console/pages/strategies.go`)

## Overview

Full-width strategy table with an Enter-triggered detail overlay and four single-keypress actions:
`[e]` enable, `[d]` disable, `[b]` run backtest, `[r]` cycle execution mode (Dry-Run → Paper → Live).
This page absorbs `performance.go`'s data ("strategy performance by symbol") into the detail overlay
rather than keeping it as a standalone page, per the blueprint's explicit Phase 3 file-list note.

## Current Behavior (verified — files being deleted)

- `cmd/console/pages/performance.go` (52 lines) — `Render()` (`:32-44`) renders only
  `components.Totalizers(d)`, a flat table with no per-strategy drill-down, no config preview, no P&L
  since activation, no win rate, no keybindings at all.
- No existing page provides a strategy table with algorithm/symbols/cycle/mode/last-execution columns —
  `strategylist.go`'s `getStrategies` (`:24-38`, being deleted per tui-08) renders only
  `"{ID} - {Name} - {Status}"`.
- No existing page has any keyboard-driven strategy mutation (`[e]`/`[d]`/`[b]`/`[r]`) — the current three
  pages are read-only views; `PUT /strategy/{id}` and `POST /strategy/enqueue` exist as REST endpoints
  (`app/handler/web/strategy/handler.go:39-53`) but nothing in `cmd/console` calls them today.

## Target Behavior

```go
// cmd/console/pages/strategies.go
package pages

type StrategiesPage struct {
    Header        *widgets.Paragraph
    TabPane       *widgets.TabPane
    Dependencies  *dependencies.Dependencies
    table         *widgets.Table
    strategies    []apiclient.StrategyView // index-aligned with table.Rows[1:] (row 0 is the header)
    selectedRow   int
    detailVisible bool
    detailData    *apiclient.StrategyPerformanceView
}

func (p *StrategiesPage) Render() ui.Drawable
// HandleEvent processes key events specific to this page: arrow keys move
// selectedRow, Enter toggles detailVisible (fetching GetStrategyPerformance
// on open), Esc closes the overlay, [e]/[d]/[b]/[r] act on the selected row.
func (p *StrategiesPage) HandleEvent(e ui.Event) error
```

### Keybinding semantics

- `[e]` — `UpdateStrategyStatus(id, "productive")`. No-op with an inline toast/error if already productive.
- `[d]` — `UpdateStrategyStatus(id, "disabled")`.
- `[b]` — navigates to Page 4 (Backtest Launcher) with the selected strategy pre-selected in its strategy
  selector (cross-page state handoff — see `tui-09` for how `master.go` passes this).
- `[r]` — cycles `Mode`: `"dryrun" → "paper" → "live" → "dryrun"`. **Resolved** by
  `docs/specs/phase-3/backend-01-tui-consumed-api-endpoints.md` §3: `PATCH /strategy/{id}/mode` persists
  the new value immediately, but it only takes **effect** at the strategy's next scheduled cycle (Phase
  1's `gateMode` re-reads `Strategy.Mode` fresh every cycle, per `backend-01`'s Target Behavior) — there is
  no mid-cycle interrupt. This page's UI must therefore distinguish "saved" from "active": see Acceptance
  Criterion #4a below.

### Detail overlay (Enter)

Rendered as a `ui.NewGrid`-based overlay drawn on top of (not replacing) the table — a bordered panel
roughly centered, covering ~60% of the terminal, containing:
- Config JSON preview: pretty-printed `StrategyView.Configuration` (raw `json.RawMessage`, indented via
  `json.MarshalIndent`) in a scrollable `widgets.Paragraph` or `widgets.List` (one line per JSON line).
- P&L since activation, total trades, win rate — from `GetStrategyPerformance(strategyID)` (net-new
  endpoint, tui-01).

## ASCII Wireframe

```
┌ go-trade-bot ── 2026-08-31 14:33:01 ── ● CONNECTED ── Balance: $12,480.55 ──────────────────────────┐
├ [1 Dashboard] [2 Strategies] [3 Positions] [4 Backtest] [5 Results] [6 Log] ─────────────────────────┤
│ Name        │ Algorithm │ Symbols        │ Cycle │ Mode  │ Last Exec           │ Status │ Open Sigs │
│─────────────┼───────────┼────────────────┼───────┼───────┼─────────────────────┼────────┼───────────│
│>grid_btc     │ grid      │ BTCUSDT        │ 5m    │ LIVE  │ 2026-08-31 14:30:02 │ ok     │ 2         │
│ scalp_eth    │ scalping  │ ETHUSDT        │ 1m    │ DRY   │ 2026-08-31 14:32:55 │ ok     │ 0         │
│ bollinger_sol│ bollinger │ SOLUSDT        │ 15m   │ PAPER │ 2026-08-31 14:15:00 │ error  │ 1         │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
[Enter] details  [e] enable  [d] disable  [b] backtest  [r] switch mode  [↑↓] navigate

     ┌ grid_btc — detail ─────────────────────────────────────┐
     │ Config:                                                │
     │ {                                                      │
     │   "grid_spacing_pct": 1.5,                              │
     │   "grid_levels": 10                                    │
     │ }                                                       │
     │                                                         │
     │ P&L since activation: +$318.40   Total trades: 47      │
     │ Win rate: 61.7%                                        │
     │                                       [Esc] close       │
     └─────────────────────────────────────────────────────────┘
```

## Acceptance Criteria

1. **Given** `ListStrategies` returns N strategies, **when** the page renders, **then** the table has
   exactly N data rows plus one header row, and the currently `selectedRow` is visibly highlighted
   (tui-08 Acceptance Criterion #4).
2. **Given** the selected row's strategy has `Status == "disabled"`, **when** `[e]` is pressed, **then**
   `UpdateStrategyStatus(id, "productive")` is called and, on success, the table's local `StrategyView`
   copy is optimistically updated to `Status: "productive"` immediately (not waiting for the next full
   `ListStrategies` poll) so the keypress feels instant.
3. **Given** the write in Acceptance Criterion #2 fails (`ErrAPI` or `ErrUnreachable`), **when** the
   optimistic update would otherwise apply, **then** it is **not** applied — the row reverts to/stays at
   its last-known-good `Status`, and an inline error is shown (e.g. a one-line red banner at the bottom of
   the table for ~3s) rather than silently desyncing the displayed state from the server's actual state.
4. **Given** `[r]` is pressed on a strategy currently in `"live"` mode, **when** the mode cycles, **then**
   it wraps to `"dryrun"`, never skipping straight back to `"live"` — the cycle order is strictly
   `dryrun → paper → live → dryrun`, matching PRD's stated direction ("cycle through Dry-Run → Paper →
   Live").
4a. **Given** `UpdateStrategyMode` succeeds, **when** the table re-renders, **then** the Mode column shows
   the **new** persisted value immediately (optimistic, per Acceptance Criterion #2's pattern) but with a
   distinguishing marker (e.g. `"PAPER*"` or a dim/pending style) until the strategy's `LastExecutionTime`
   (from the next `ListStrategies` poll) advances past the moment the `PATCH` was sent — signaling "saved,
   takes effect next cycle" rather than implying the switch is live immediately, per `backend-01`'s
   resolved next-cycle-boundary semantics.
5. **Given** Enter is pressed on a selected row, **when** `GetStrategyPerformance` is in flight, **then**
   the overlay shows a "Loading…" placeholder rather than a blank/frozen panel, replaced with real data
   (or an inline error) once the call resolves.
6. **Edge case — `[b]` pressed with the detail overlay open**: **when** `[b]` is pressed while
   `detailVisible == true`, **then** it is ignored (the overlay's own `[Esc]` must close it first) — `[b]`
   only navigates from the base table view, preventing an accidental navigation away while reading detail
   data.
7. **Edge case — empty `Configuration` JSON**: **given** a strategy whose `Configuration` is an empty
   object `{}` or `null`, **when** the detail overlay renders the config preview, **then** it shows
   `"(no configuration)"` rather than a raw `null` token or a JSON parse error.

## Out of Scope

- Editing configuration values from within the detail overlay — PRD Page 2 describes the overlay as a
  read-only "preview," not an editor; a full strategy editor is not scoped to any of the six named pages.
- Deleting a strategy — no `[Del]`/`[x]` keybinding is named in PRD §4.5 Page 2; `DELETE /strategy/{id}`
  doesn't even exist as a REST endpoint today (confirmed absent from `app/handler/web/strategy/handler.go`'s
  route list).

## Dependencies

- `tui-01-apiclient.md` — `ListStrategies`, `GetStrategyPerformance`, `UpdateStrategyStatus`,
  `UpdateStrategyMode`.
- `tui-08-shared-components.md` — `StrategyTableFull` for the base table construction.
- `tui-05-page-backtest-launcher.md` — receiving end of the `[b]` cross-page navigation handoff.

## Judgment Call — resolved

Immediate vs. next-cycle-boundary mode switching (blueprint §8's open question) is now resolved by
`backend-01-tui-consumed-api-endpoints.md` §3: the write is immediate, the effect is next-cycle. This
page's remaining judgment call is purely presentational — Acceptance Criterion #4a's "pending" marker
convention is this spec's own proposal for surfacing that distinction to the operator, since PRD §4.5
Page 2 doesn't describe any such visual state; flagged in case a simpler "just show the new value, no
pending marker" treatment is preferred instead.
