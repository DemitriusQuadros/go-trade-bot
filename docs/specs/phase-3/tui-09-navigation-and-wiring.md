# Spec TUI-09 — Navigation & Wiring (`cmd/console/pages/master.go`, `cmd/console/dependencies/dependencies.go`, `cmd/console/main.go`)

## Overview

Registers all six pages under `master.go`'s tab system with F1-F6 keybindings (in addition to the
existing `h`/`l` tab-cycling), rewires `Dependencies` to drop `*gorm.DB` entirely per ADR-007, and updates
`main.go`'s render loop to dispatch to six pages instead of three while preserving the "zero flickering"
requirement.

## Current Behavior (verified)

- `cmd/console/pages/master.go` (15 lines, entire file) — defines only the `Page` interface:
  `Set(header, tabPane, dependencies) Page`, `Render() ui.Drawable`, `StopSync()`, `StartSync()`. No tab
  registration logic lives here at all today — that's inlined directly in `main.go`.
- `cmd/console/main.go:25` — `widgets.NewTabPane("Status", "Performance", "Open Signals")`, three tabs.
- `cmd/console/main.go:34-46` (`renderTab` closure) — a `switch tabPane.ActiveTabIndex` with three cases,
  each calling `opensignal.StopSync()`/`StartSync()` explicitly since `OpenOrdersPage` is the only page
  with real sync behavior today (`status`/`performance`'s `StartSync`/`StopSync` are both no-ops per their
  own file listings).
- `cmd/console/main.go:52-67` — the event loop only handles three key IDs: `<Escape>` (quit), `h`
  (`FocusLeft`), `l` (`FocusRight`). No F-key handling, no per-page `HandleEvent` dispatch (`status.go`/
  `performance.go`/`openorders.go` have no `HandleEvent` method at all — `openorders.go:141-143` defines
  one but it's a no-op stub, never called from `main.go`'s event loop).
- `cmd/console/dependencies/dependencies.go:10-26` — `Dependencies{Cfg, Db *gorm.DB}`, `Init()` calls
  `db.NewDatabase(cfg)` directly (`:17`), panicking on connection failure (`:19`) rather than returning an
  error — the console currently **cannot start at all** if Postgres is unreachable, even though (post
  ADR-007) it will never need Postgres directly again.

## Target Behavior

```go
// cmd/console/dependencies/dependencies.go
package dependencies

import (
    "go-trade-bot/cmd/console/apiclient"
    "go-trade-bot/internal/configuration"
)

type Dependencies struct {
    Cfg *configuration.Configuration
    API *apiclient.Client // replaces Db *gorm.DB entirely — no gorm.io/gorm import remains anywhere
                           // under cmd/console/ after this spec (ADR-007's stated end-state).
}

func Init() *Dependencies {
    cfg := configuration.NewConfiguration()
    baseURL := cfg.APIBaseURL // proposed new Configuration field, default "http://localhost:8080"
                              // (tui-01 Acceptance Criterion #8) — see Judgment Call
    return &Dependencies{
        Cfg: cfg,
        API: apiclient.NewClient(baseURL),
    }
}
```

Note `Init()` no longer panics on startup — `apiclient.NewClient` does not attempt a connection at
construction time (it's a plain HTTP client wrapper); reachability is only discovered lazily on first use
and surfaced via the connection-status dot (tui-02), not a hard boot-time failure. This is a deliberate
behavior change from today's `db.NewDatabase` panic-on-init, consistent with PRD Page 1's whole premise
that "disconnected" is a normal, displayable state, not a fatal one.

```go
// cmd/console/pages/master.go
package pages

// Page interface is UNCHANGED from today (kept exactly as-is — the blueprint's
// Appendix explicitly calls this out as "migration-tractable," confirming no
// interface break is needed for the 3->6 page expansion):
type Page interface {
    Set(header *widgets.Paragraph, tabPane *widgets.TabPane, dependencies *dependencies.Dependencies) Page
    Render() ui.Drawable
    StopSync()
    StartSync()
}

// HandleEvent is a NEW, optional-via-type-assertion interface — not every
// page needs it (Page 1/Dashboard has no page-specific keybindings beyond
// tab switching), but Pages 2/3/5/6 do. Rather than widen the base Page
// interface (forcing Dashboard/BacktestLauncher to implement a no-op
// HandleEvent), main.go type-asserts:
type EventHandler interface {
    HandleEvent(e ui.Event) error
}

// RegisterPages centralizes the 6-tab construction that today lives inline
// in main.go, returning the ordered slice main.go iterates.
func RegisterPages(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) []Page
```

```go
// cmd/console/main.go (rewritten render loop)
package main

func main() {
    d := dependencies.Init()
    // ... ui.Init(), header setup unchanged in shape (now dynamic per tui-02) ...

    tabPane := widgets.NewTabPane(
        "Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log",
    )
    pages := pages.RegisterPages(header, tabPane, d)
    activePage := pages[0]
    activePage.StartSync()
    ui.Render(header, tabPane, activePage.Render())

    uiEvents := ui.PollEvents()
    for e := range uiEvents {
        switch {
        case e.ID == "<Escape>" || e.ID == "q":
            return
        case e.ID == "h", e.ID == "<Left>":
            switchTab(tabPane, pages, &activePage, tabPane.ActiveTabIndex-1)
        case e.ID == "l", e.ID == "<Right>":
            switchTab(tabPane, pages, &activePage, tabPane.ActiveTabIndex+1)
        case isFunctionKey(e.ID): // "<F1>".."<F6>" -> index 0..5
            switchTab(tabPane, pages, &activePage, functionKeyIndex(e.ID))
        default:
            if handler, ok := activePage.(pages.EventHandler); ok {
                if err := handler.HandleEvent(e); err != nil {
                    // render an inline error banner without a full-page clear (see AC#3)
                }
            }
        }
    }
}

// switchTab stops the outgoing page's sync goroutines, starts the incoming
// page's, and does the ONE full ui.Clear() + full-page ui.Render() this
// codebase performs — every other refresh (per-second ticks, keypress
// handling within a page) must touch only the affected widget's buffer
// region, not clear the whole screen, per PRD's "zero flickering" requirement.
func switchTab(tabPane *widgets.TabPane, pages []Page, active *Page, newIndex int)
```

## Acceptance Criteria

1. **Given** the console starts with `cmd/api` unreachable, **when** `main()` runs, **then** it does not
   panic or exit — the TUI renders normally with every panel in its degraded/disconnected state (tui-01
   AC#2-3), correcting today's `dependencies.Init()` hard-panic-on-DB-unreachable behavior.
2. **Given** any of `<F1>` through `<F6>` is pressed, **when** the event loop processes it, **then** it
   switches directly to the corresponding page (index 0-5) regardless of the currently active tab — not
   just relative `h`/`l` stepping.
3. **Given** a page-specific keypress (e.g. `[e]` on Page 2) is handled via `EventHandler.HandleEvent`,
   **when** it triggers a re-render (e.g. after an optimistic status update), **then** only that page's
   own `Render()` output is re-drawn — `switchTab`'s full `ui.Clear()` is called **only** on an actual tab
   change, never on an in-page keypress, per PRD's zero-flickering requirement being interpreted here as
   "minimize full-screen redraws to tab-switch events only."
4. **Given** the previously active page has a running sync goroutine (e.g. Page 3's 1s ticker), **when**
   `switchTab` is called, **then** `StopSync()` is called on the outgoing page **before** `StartSync()` is
   called on the incoming page — preventing two pages' background goroutines from both hitting `cmd/api`
   concurrently after a tab switch (a resource-usage concern, not just a correctness one, given every page
   now shares one `apiclient.Client` and one `cmd/api` process).
5. **Given** `q` is added as a synonym for `<Escape>` (this spec's own addition — PRD's bottom-bar hint
   text across all six wireframes says `[q] quit`, not `[Esc] quit`), **when** either is pressed, **then**
   the application exits cleanly (`ui.Close()` deferred, matching today's pattern).
6. **Edge case — F-key not supported by the terminal emulator**: **given** some terminal emulators/
   multiplexers intercept function keys before they reach `termui`'s event stream (a known category of
   terminal-compatibility issue), **when** an F-key press doesn't arrive as a `ui.Event`, **then** `h`/`l`
   (and `<Left>`/`<Right>`) remain fully functional as the fallback navigation method — F-keys are additive,
   not a replacement for the existing relative-navigation scheme.

## Out of Scope

- Mouse-driven tab switching — PRD explicitly states no mouse required for any of the six pages.
- Persisting the last-active tab across console restarts — every session starts on Page 1/Dashboard.
- `fx`-based dependency injection for `cmd/console` (ADR-004's deferred question) — this spec keeps
  `Dependencies` hand-rolled (`Init()` returning a plain struct), since `{Cfg, API}` is a two-field struct
  cheap to wire either way (per ADR-004's own text); adopting `fx` here is a separate, optional follow-up
  not required by anything in this spec set.

## Dependencies

- `tui-01-apiclient.md` — `apiclient.Client`, `apiclient.NewClient`.
- `tui-02` through `tui-07` — the six `Page`-implementing structs this file registers.
- `docs/specs/phase-3/backend-06-console-observability.md` — defines `cmd/console/metrics.go`'s
  `StartDebugMetricsServer` (config-gated `/metrics` on port 9192, default disabled) and the
  `console_render_loop_healthy` gauge; `main.go`'s rewrite in this spec should call it alongside
  `dependencies.Init()`, gated by the same `Console.MetricsEnabled` config flag that spec defines — not
  duplicated here since `backend-06` owns that surface fully.
- `internal/configuration/configuration.go` — needs a new `APIBaseURL` field (see Judgment Call), which is
  a Phase 3 addition to a file otherwise unmodified by this spec set (it's mentioned as `[MOD]` in the
  blueprint's directory tree for unrelated Phase 1/2/3 fields — `Mode`, `WebhookURL`, `TestnetFlag`,
  `ConfirmLive` — `APIBaseURL` would be an additional field in that same modification pass).

## Judgment Call

**`internal/configuration.Configuration` needs a new `APIBaseURL` field** (or equivalent env var, e.g.
`API_BASE_URL`) for `dependencies.Init()` to construct `apiclient.NewClient` with a configurable target
rather than a hardcoded `"http://localhost:8080"` string baked into `cmd/console`. This field isn't named
anywhere in the blueprint's directory tree entry for `internal/configuration/configuration.go` (`[MOD] add
Mode, WebhookURL, TestnetFlag, ConfirmLive`), which predates ADR-007's console-rewrite scope existing at
all. Flagging as a small, mechanical addition this spec set needs but that the blueprint's own
`configuration.go` change-list doesn't yet enumerate — worth a one-line update to that section when this
phase's implementation actually lands.
