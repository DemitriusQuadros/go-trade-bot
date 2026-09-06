# Spec TUI-08 — Shared Components (`cmd/console/components/{sparkline,accountsummary,strategytable}.go`)

## Overview

Three reusable widgets consumed by more than one of the six pages, replacing the three deleted
repository-coupled components. All three are built exclusively against `cmd/console/apiclient.Client`
(ADR-007) — none imports `gorm.io/gorm`, `internal/db`, or `app/repository`.

## Current Behavior (verified — files being deleted)

- `cmd/console/components/totalizers.go` (54 lines) — `buildStrategyPerformanceBySymbol` (`:20-54`)
  instantiates `repository.NewStrategyRepository(d.Db)` (`:21`) and calls
  `.GetStrategyPerformanceBySymbol(ctx)` (`:22`), rendering a `widgets.Table` with columns
  `Strategy | Symbol | Profit/Loss | Trades`.
- `cmd/console/components/account.go` (40 lines) — `Account(d)` (`:13-40`) instantiates
  `repository.NewAccountRepository(d.Db)` (`:17`), calls `.GetAccountByID(1)` (`:18`) — a **hardcoded
  account ID of 1**, and renders a `widgets.List` with `ID | Amount | Available | Currency | CreatedAt |
  UpdatedAt`.
- `cmd/console/components/strategylist.go` (38 lines) — `StrategyList(d)` (`:13-22`) and `getStrategies(d)`
  (`:24-38`) instantiate `repository.NewStrategyRepository(d.Db)` (`:25`), call `.GetAll(ctx)` (`:27`),
  `panic()` on error (`:29`) rather than returning it, and render a `widgets.List` of
  `"{ID} - {Name} - {Status}"` strings — no algorithm, symbols, cycle, mode, or last-execution info per
  row, which is why PRD Page 2 needs a proper table, not this list.
- `cmd/console/components/error.go` (17 lines, **kept, not deleted**) — `Error(err)` renders a red-background
  one-line `widgets.Paragraph`. Unchanged; every new component/page still uses this for inline panel errors.
- No sparkline component exists today. `openorders.go`'s live-price polling (`:94-118`) prints plain text
  (`"Current: $X PnL: $Y"`), no chart of any kind.

## Target Behavior

```go
// cmd/console/components/sparkline.go
package components

import (
    "github.com/gizak/termui/v3/widgets"
    "go-trade-bot/cmd/console/apiclient"
)

// BuildSparkline renders closes as a termui widgets.Sparkline. Judgment
// call: termui/v3.1.0's built-in Sparkline widget (verified in
// gizak/termui/v3@v3.1.0/widgets/sparkline.go:13) renders using 8-level
// block characters (▁▂▃▄▅▆▇█), NOT braille dots — termui has no built-in
// braille-canvas sparkline widget. PRD §4.5's "braille-dot sparklines" is
// therefore approximated with the block-style widget everywhere this spec
// set says "sparkline," unless a custom braille Drawable is built (see Out
// of Scope). This applies identically to Page 1's pair sparklines, Page 5's
// equity-curve/drawdown sparklines, and any other "sparkline" in PRD §4.5.
func BuildSparkline(title string, closes []float64, lineColor ui.Color) *widgets.SparklineGroup

// ComputeEMA20 is the client-side EMA used for Page 1's pair color coding
// (green above EMA20, red below) — see tui-01's Judgment Call #3 for why
// this lives here instead of behind an indicator endpoint.
func ComputeEMA20(closes []float64) []float64
```

```go
// cmd/console/components/accountsummary.go
package components

// AccountSummary replaces account.go's role. Fetches via apiclient (no
// hardcoded account ID — GET /account returns the single account row as
// the existing handler already does; the ID=1 assumption in the deleted
// component was never actually a multi-account concern since the schema
// has exactly one Account row in practice, but this component no longer
// bakes that assumption into the query itself since apiclient.GetAccount
// takes no ID parameter at all, matching the existing GET /account handler
// signature which also takes none — app/handler/web/account/handler.go:51
// calls h.UseCase.GetAccount() with no ID).
func AccountSummary(ctx context.Context, api *apiclient.Client) ui.Drawable
    // Renders: total balance, available margin (AvailableOrders), daily P&L
    // (green/red per sign), open positions count (from a supplementary
    // GetOpenSignals call — see Acceptance Criterion #3 for the "daily P&L"
    // judgment call, since entities.Account has no daily-P&L field).
```

```go
// cmd/console/components/strategytable.go
package components

// StrategyTable replaces both strategylist.go and totalizers.go. Two
// render modes selected by the caller (Page 1 needs the compact list-style
// view for its right panel; Page 2 needs the full table):
func StrategyTableCompact(ctx context.Context, api *apiclient.Client) ui.Drawable
    // Page 1 right panel: name, mode badge (LIVE/DRY/PAPER/BT), status
    // (running/paused/error) — one row per strategy, no performance data.
func StrategyTableFull(ctx context.Context, api *apiclient.Client) (*widgets.Table, []apiclient.StrategyView)
    // Page 2: name, algorithm type, monitored symbols, cycle, mode, last
    // execution time/status, open signals count. Returns the backing
    // []StrategyView slice alongside the table so strategies.go can map the
    // table's SelectedRow index back to a strategy ID for [e]/[d]/[b]/[r].
```

### Mode badge color mapping (used by both `StrategyTableCompact` and `StrategyTableFull`)

| Mode (`StrategyView.Mode`) | Badge text | Color |
|---|---|---|
| `"live"` | `LIVE` | `ui.ColorRed` (highest-stakes, draws the eye) |
| `"paper"` | `PAPER` | `ui.ColorYellow` |
| `"dryrun"` | `DRY` | `ui.ColorCyan` |
| `"backtest"` | `BT` | `ui.ColorBlue` |

This mapping is this spec's own proposal (PRD §4.5 names the four badges but not their colors) —
red-for-`LIVE` follows the general principle used elsewhere in the PRD (red = highest attention/risk).

## Acceptance Criteria

1. **Given** `apiclient.Client.ListStrategies` succeeds, **when** `StrategyTableFull` renders, **then**
   every row shows all eight PRD-specified columns (name, algorithm type, monitored symbols, cycle,
   current mode, last execution time, last execution status, open signals count) — algorithm type comes
   from `StrategyView.StrategyName` (the registry key, ADR-005), not the deprecated `Algorithm` enum field.
2. **Given** the API call underlying any of the three components fails (`ErrUnreachable` or `ErrAPI`),
   **when** the component renders, **then** it returns `components.Error(err)` in place of its normal
   widget — matching the existing `error.go` pattern, not a panic (fixing `strategylist.go:29`'s current
   `panic()` behavior, which the PRD's "no flickering... graceful" framing implicitly rules out for the
   rebuild).
3. **Edge case — "daily P&L" has no direct data source**: `entities.Account` (`app/entities/account.go`)
   has only `Amount`/`AvailableOrders`/`Currency`/timestamps — no running daily-P&L field. **Judgment
   call**: `AccountSummary` computes daily P&L client-side as
   `sum(SignalView.Orders[].Profit for orders closed today)`, derived from `GetOpenSignals` plus
   (**gap**, flagged) a currently-nonexistent "closed signals since midnight" query — `GET /signal/open`
   only returns open signals. Until a closed-signals-since endpoint exists, `AccountSummary` renders daily
   P&L as `"N/A (endpoint gap)"` rather than a fabricated number — this is a deliberate degraded-but-honest
   default, not a placeholder to silently ship.
4. **Given** `StrategyTableFull`'s selected row changes (arrow-key navigation, driven by the calling page,
   not this component), **when** the table re-renders, **then** the selected row is visibly highlighted
   (termui `widgets.Table.RowStyles[selectedIndex]`) — required for Page 2's "selected row highlighted"
   spec line.
5. **Given** zero strategies exist, **when** either `StrategyTableCompact` or `StrategyTableFull` renders,
   **then** it shows an explicit "No strategies configured" message rather than an empty/blank table body.

## Out of Scope

- A custom braille-canvas sparkline `Drawable` (true braille dot rendering, matching literal PRD wording)
  — accepted as a future enhancement; `widgets.Sparkline`'s block-character rendering is adopted as the
  pragmatic default for this phase (see Target Behavior's judgment call). Building a custom braille
  renderer means drawing directly into `ui.Buffer` cell-by-cell using the Unicode braille block
  (U+2800–U+28FF) to get 2x4 sub-cell resolution per character — a nontrivial `termui.Drawable`
  implementation, not a config flag on the existing widget.
- Any component-level caching/memoization beyond what the calling page already holds for degraded-mode
  display (tui-01 Acceptance Criterion #3) — components are stateless render functions called fresh each
  refresh tick.

## Dependencies

- `tui-01-apiclient.md` — `apiclient.Client` method set and `*View` types.
- `cmd/console/components/error.go` — kept unchanged, reused by all three new components.
- `app/entities/account.go`, `app/entities/strategy.go` — field-shape reference for `AccountView`/`StrategyView`.
