# Spec TUI-02 — Page 1: Dashboard (`cmd/console/pages/dashboard.go`)

## Overview

Page 1 is the TUI's main/default view (PRD §4.5 Page 1): header bar with connection status, an account
summary panel, a monitored-pairs grid with braille-style sparklines and EMA20 color coding, an
active-strategies panel, and a bottom keybinding-hints bar. This replaces `status.go`'s role but is not a
1:1 port — `status.go` today renders only a strategy list and account list side by side with no header
content beyond a static "Press ESC to quit" line, no sparklines, no pairs grid, and no connection status.

## Current Behavior (verified — file being deleted)

- `cmd/console/pages/status.go` (55 lines) — `Render()` (`:32-47`) builds a `ui.NewGrid` with exactly two
  rows below the header/tab-pane: `components.StrategyList(d)` and `components.Account(d)`, each occupying
  1/3 width in a single `1.0/4`-height row. No pairs grid, no sparklines, no header content, no keybinding
  hints bar. `StartSync`/`StopSync` (`:49-55`) are both no-ops.
- `cmd/console/main.go:19-23` — the header is a static `widgets.Paragraph` ("Press ESC to quit, Press h or
  l to switch tabs") with a solid red background, shared identically across all three current pages, not a
  dynamic "bot name / time / connection status / balance" bar as PRD Page 1 requires.

## Target Behavior

```go
// cmd/console/pages/dashboard.go
package pages

type DashboardPage struct {
    Header       *widgets.Paragraph // now dynamic, rebuilt each render — see below
    TabPane      *widgets.TabPane
    Dependencies *dependencies.Dependencies // {Cfg, API *apiclient.Client}
    stop         chan struct{}              // for the pairs-grid sparkline refresh goroutine
}

func NewDashboardPage() *DashboardPage
func (p *DashboardPage) Set(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) Page
func (p *DashboardPage) Render() ui.Drawable
func (p *DashboardPage) StartSync() // starts a 1s ticker refreshing the pairs grid + connection dot
func (p *DashboardPage) StopSync()  // stops it (called when navigating away, matching openorders.go's
                                     // existing Stop-bool pattern for its own polling goroutine)
```

`Render()` assembles a `ui.NewGrid` with these rows (percentages are this spec's proposal, not dictated
by the PRD, sized to fit the wireframe below within a typical 120x35 terminal):

1. Header row (`1.0/20`): bot name, current time (updates every render tick), connection dot, account
   balance — all in one `widgets.Paragraph` with per-segment `ui.Color` runs (termui `Paragraph.Text`
   supports inline `[text](fg:color)` styling directives — used here for the colored dot and balance).
2. Tab-pane row (`1.0/20`), unchanged pattern from today.
3. Body row (remaining height), three columns:
   - Left (`1.0/4` width): `components.AccountSummary` (tui-08).
   - Center (`1.0/2` width): the monitored-pairs grid (this page's own logic, not delegated to a shared
     component, since it composites `ListTickerPrices` + `ListKlines` + `ComputeEMA20` per pair — see
     below).
   - Right (`1.0/4` width): `components.StrategyTableCompact` (tui-08).
4. Bottom row (`1.0/20`): static keybinding-hints `widgets.Paragraph` — `"[F1-F6] switch page  [q] quit"`.

### Monitored-pairs grid

The set of symbols shown is the **union of `MonitoredSymbols` across all strategies** returned by
`ListStrategies` (there is no dedicated "watchlist" concept anywhere in `app/entities` — this is a
judgment call, flagged below). For each symbol: `ListTickerPrices(symbol)` for last price + (last vs.
`GetKlines`'s previous-24h-ago close, for the 24h change % — no dedicated 24h-change endpoint exists;
computed client-side from the kline series) and `ListKlines(symbol, "1m", 60)` for the sparkline.

## ASCII Wireframe

```
┌ go-trade-bot ── 2026-08-31 14:32:07 ── ● CONNECTED ── Balance: $12,480.55 ──────────────────────────┐
├ [1 Dashboard] [2 Strategies] [3 Positions] [4 Backtest] [5 Results] [6 Log] ─────────────────────────┤
├ Account ─────────────┬ Monitored Pairs ─────────────────────────────────┬ Active Strategies ────────┤
│ Balance:   $12,480.55│ BTCUSDT   $61,204.10  +1.8%  ▁▂▃▅▆▇█▇▆▅▄▃▂▁▂▃▅▆▇ │ grid_btc     [LIVE] running│
│ Available: $ 9,120.00│ ETHUSDT   $ 3,015.42  -0.6%  ▇▆▅▄▃▂▁▂▃▅▆▇█▇▆▅▄▃▂ │ scalp_eth    [DRY]  running│
│ Daily P&L: +$142.30  │ SOLUSDT   $   142.87  +3.2%  ▂▃▅▆▇█▇▆▅▄▃▂▁▂▃▅▆▇ │ bollinger_sol[PAPER] paused│
│ Open Pos.: 3         │                                                    │ grid_ada     [BT]   error  │
│                      │  (green line/price = above EMA20, red = below)   │                            │
└──────────────────────┴────────────────────────────────────────────────┴────────────────────────────┘
[F1-F6] switch page   [q] quit
```

(Sparkline blocks shown here as ▁▂▃▅▆▇█ — the actual `widgets.Sparkline` character set; see tui-08's
judgment call on why this is not literal braille.)

## Acceptance Criteria

1. **Given** `cmd/api` is reachable, **when** Page 1 renders, **then** the header shows the current
   wall-clock time updated on every refresh tick, a green `●` connection dot, and the account balance from
   `GetAccount`.
2. **Given** `cmd/api` becomes unreachable mid-session (worker/API restart, network blip), **when** the
   next refresh tick's `Ping` fails, **then** the header dot turns red, and the account summary / pairs
   grid / strategies panel each continue showing their **last successfully fetched** values (tui-01
   Acceptance Criterion #3) rather than clearing — the operator sees stale-but-present data plus a clear
   "you're disconnected" signal, never a blank screen.
3. **Given** at least one strategy has `MonitoredSymbols` containing `"BTCUSDT"`, **when** the pairs grid
   renders, **then** `BTCUSDT` appears exactly once (deduplicated across strategies that might share a
   symbol) with its sparkline colored green if the latest close is above the computed EMA20, red if below.
4. **Given** the pairs grid is refreshing on a 1-second ticker (matching Page 3's live-update cadence for
   consistency), **when** `ui.Render` is called each tick, **then** only the pairs-grid widget's buffer
   region is touched — the PRD's "zero flickering on refresh" requirement means avoiding a full `ui.Clear()`
   + full-page `ui.Render()` every second; only tab-switches (`h`/`l` or `F1`-`F6`) do a full clear/redraw.
5. **Edge case — no strategies exist yet**: **given** `ListStrategies` returns an empty slice, **when**
   Page 1 renders, **then** the pairs grid shows "No monitored pairs (no strategies configured)" and the
   strategies panel shows "No strategies configured" (tui-08 Acceptance Criterion #5) — the page does not
   error out or panic on an empty fleet.
6. **Edge case — a single symbol's `ListTickerPrices`/`ListKlines` call fails while others succeed**:
   **when** the pairs grid renders, **then** only that symbol's row shows an inline error state
   (`"BTCUSDT   [error fetching price]"`), the remaining symbols render normally — one bad symbol/API
   hiccup does not blank the whole grid.

## Out of Scope

- Configurable/persisted watchlist independent of strategies' `MonitoredSymbols` — Judgment Call below.
- Mouse interaction — PRD explicitly states "no mouse required" for all pages.
- Sub-second refresh — 1s ticker matches Page 3's cadence and is fast enough for a 60-candle 1m sparkline
  to feel live without hammering `cmd/api`.

## Dependencies

- `tui-01-apiclient.md` — `Ping`, `GetAccount`, `ListStrategies`, `ListTickerPrices`, `ListKlines`.
- `tui-08-shared-components.md` — `AccountSummary`, `StrategyTableCompact`, `BuildSparkline`, `ComputeEMA20`.
- `tui-09-navigation-and-wiring.md` — `master.go`'s tab registration and F1-F6 keybindings that select
  this page.

## Judgment Call

**"Monitored pairs" (PRD: "Monitored pairs grid") is defined as the deduplicated union of every
strategy's `MonitoredSymbols`**, since no separate watchlist entity or endpoint exists anywhere in
`app/entities` or the blueprint's directory tree. If a dedicated operator-configurable watchlist
(independent of which symbols happen to have an active strategy) is actually intended by the PRD, that
requires new persistence and a new endpoint not scoped anywhere in Phases 1-3 — flagging for explicit
confirmation rather than assuming it during implementation.
