# go-trade-bot — Product Requirements Document

**Full Backlog · No Fixed Timeline · Version 1.0 · August 2026**

---

## Table of Contents

1. [Product Overview](#1-product-overview)
2. [Goals & Success Metrics](#2-goals--success-metrics)
3. [User Stories](#3-user-stories)
4. [Feature List with MoSCoW Prioritization](#4-feature-list-with-moscow-prioritization)
5. [Strategy Creation: Jesse vs go-trade-bot](#5-strategy-creation-jesse-vs-go-trade-bot)
6. [Go Library Landscape](#6-go-library-landscape)
7. [Product Roadmap](#7-product-roadmap)
8. [Technical Considerations](#8-technical-considerations)
9. [Deployment & Validation Strategy](#9-deployment--validation-strategy)
10. [Open Questions & Assumptions](#10-open-questions--assumptions)

---

## 1. Product Overview

### One-liner

go-trade-bot is a self-hosted algorithmic cryptocurrency trading system built in Go that enables a solo developer to research, validate, and execute systematic trading strategies against Binance — progressing from backtesting through dry-run simulation to live capital deployment on a single codebase.

### Problem Statement

Systematic crypto trading requires four distinct capabilities that existing frameworks fail to deliver together: a rigorous backtest engine, a live execution pipeline, observability infrastructure, and a lightweight footprint compatible with a constrained homelab environment. Python-based frameworks like Jesse provide strong research capabilities but consume excessive memory and require a paid plugin for live trading. The current go-trade-bot codebase has solid infrastructure (Prometheus, REST API, asynq queue, DI with uber/fx) but operates as a paper-trading simulation — it never submits real orders to Binance, has no backtest engine, and requires recompilation to add new trading strategies.

### Solution

Evolve go-trade-bot into a full-cycle trading system by layering four capabilities onto its existing Go infrastructure:

1. A pluggable `Strategy` interface with lifecycle hooks that eliminates the hardcoded switch pattern
2. A unified execution engine with swappable data feeds — `ReplayFeed` for backtesting historical candles stored in Postgres, `LiveFeed` for production WebSocket streaming — ensuring zero divergence between backtest and live behavior
3. Four execution modes — Backtest, Dry-Run, Paper Trading, and Capital Real — each progressively closer to production risk
4. A btop-inspired TUI with real-time sparklines, backtest launcher, and results visualization, exposing all capabilities without leaving the terminal

### Primary User

Demitrius — a Go backend engineer who operates a personal homelab (WSL2, ~4GB available RAM) and wants to build and validate systematic trading strategies for personal capital deployment, without relying on third-party SaaS trading platforms or cloud compute.

### Stage Assumption

**Existing MVP with critical gaps.** Infrastructure is production-grade; core trading loop (real order execution, backtest engine, Strategy plugin system) is absent. This PRD describes the full backlog to reach a complete, self-sufficient trading system.

### Critical Finding

> ⚠️ **The bot is currently a paper trading system, not a live trading system.**
> `GenerateBuySignal()` and `GenerateSellSignal()` only write to the database. No `NewCreateOrderService` call exists anywhere in the codebase. The `Broker` struct only exposes `ListKline`, `ListTickerPrices`, and `Get24hVolume`. All strategies have status `"testing"` in the example configs.

---

## 2. Goals & Success Metrics

| Goal | Activation Signal | Completion Signal |
|---|---|---|
| Real order execution on Binance | `PlaceOrder` call succeeds in testnet | First live trade executed and settled |
| Strategy plugin system live | New strategy added without touching `handler.go` | 3+ strategies in registry, each independently togglable |
| Backtest engine operational | Bollinger strategy backtested over 12-month candle history | Walk-forward validated, HTML report generated |
| TUI reaches btop-level polish | Braille sparklines rendering live prices in Dashboard page | 6 pages navigable by keyboard, zero flickering |
| Strategy validation pipeline complete | Backtest → Dry-Run → Paper → Capital Real flow documented | First capital deployment following full validation pipeline |

---

## 3. User Stories

### Theme A — Strategy Development

- **As a developer**, I want to implement a new trading strategy by creating a Go struct that satisfies a `Strategy` interface, so that I can add algorithms without modifying the execution engine or recompiling shared code.
- **As a developer**, I want to run a backtest for any registered strategy via the TUI, selecting date range and parameters, so that I can evaluate historical performance before risking capital.
- **As a developer**, I want to view backtest results — Sharpe Ratio, Max Drawdown, Win Rate, Profit Factor, and an equity sparkline — directly in the terminal, with a detailed HTML report also generated, so that I can quickly assess viability and share results later.

### Theme B — Execution Pipeline

- **As a trader**, I want to run a strategy in Dry-Run mode against real live prices with simulated orders (configurable slippage and fees), so that I can validate real-world behavior without depending on a testnet.
- **As a trader**, I want to promote a strategy from Dry-Run to Paper Trading (Binance testnet) with one command, so that I can confirm execution fidelity before committing capital.
- **As a trader**, I want stop-loss and take-profit orders submitted as actual exchange orders — not polled in software — so that positions are protected even if the bot process restarts.
- **As a trader**, I want the bot to POST a webhook to a configurable URL on every trade event (open, close, drawdown alert, error), so that I can route notifications through n8n to any channel without changing bot code.

### Theme C — Monitoring & Observability

- **As an operator**, I want a btop-inspired TUI Dashboard page showing live price sparklines for all monitored pairs, account balance, active strategy count, and daily P&L, so that I can assess the system state at a glance from any terminal session.
- **As an operator**, I want to view all open positions with real-time P&L updated every second in the TUI, color-coded green/red, so that I can monitor exposure without opening a browser.

---

## 4. Feature List with MoSCoW Prioritization

### 4.1 Live Trading Core

| Feature | Priority | Description | Rationale |
|---|---|---|---|
| `PlaceOrder` / `CancelOrder` on Binance | **MUST** | Add `NewCreateOrderService` and `NewCancelOrderService` calls to `internal/broker/broker.go` using the existing `go-binance` SDK | Bot is currently paper-only. Every other feature depends on this. |
| Stop-loss as real exchange order | **MUST** | Submit `STOP_MARKET` order to Binance at position open time. Do not rely on software polling for SL execution. | Current SL is polled in memory — restarts lose the stop. |
| WebSocket live feed | **MUST** | Replace REST polling (asynq timer) with Binance kline WebSocket stream. Emit candles to strategy engine in real time. | REST polling has rate limits and latency; WebSocket is the correct pattern for live trading. |
| Broker ACL interface | **MUST** | Define `ExchangeClient` interface in `internal/exchange/`. No domain code imports `go-binance` directly. | Enables adding any exchange later by implementing one interface. |
| Generic webhook notifier | **MUST** | POST structured JSON payload to a configurable URL on trade events: `position.opened`, `position.closed`, `drawdown.alert`, `strategy.error`. | Decouples notification channel from bot. User routes via n8n. |

### 4.2 Strategy Plugin System

| Feature | Priority | Description | Rationale |
|---|---|---|---|
| Strategy interface with lifecycle hooks | **MUST** | Define `Strategy` interface: `Name()`, `Before()`, `ShouldLong()`, `GoLong()`, `ShouldShort()`, `GoShort()`, `UpdatePosition()`, `After()`, `Terminate()`. Engine calls hooks per candle. | Replaces hardcoded switch. Adding a strategy = one new file. |
| Strategy registry | **MUST** | Central `map[string]StrategyFactory` in `app/strategies/registry.go`. Handler resolves strategy by name. Engine never changes when new strategy is added. | Eliminates the `switch` case in `handler.go` permanently. |
| `IndicatorProvider` ACL | **MUST** | Define `IndicatorProvider` interface in `internal/indicators/`. Adapter wraps `go-talib` and `cinar/indicator/v2`. Strategies call indicators through the interface only. | Anti-Corruption Layer: swap implementations without touching strategy code. |
| `StrategyContext` struct | **MUST** | Pass `Candles`, `Position`, `Account`, `Config`, `Indicators` to every hook via a single `StrategyContext`. No global state. | Enables isolated unit testing of each strategy hook. |
| Port existing 3 algorithms to interface | **MUST** | Rewrite Bollinger, Grid, Scalping as `Strategy` interface implementations. Register in registry. Delete handler switch. | Validates the new architecture with existing known-working logic. |
| gRPC ML strategy adapter | **COULD** | `Strategy` interface implementation that delegates hooks to an external gRPC process. Enables Python ML models (scikit-learn, PyTorch) as strategies without modifying the Go engine. | Future-proofs ML integration. Engine stays pure Go; Python used only where it has ecosystem advantage. |

### 4.3 Backtest Engine

| Feature | Priority | Description | Rationale |
|---|---|---|---|
| Local candle storage (Postgres) | **MUST** | Schema: `candles(symbol, timeframe, open_time, open, high, low, close, volume)`. CLI command to import historical candles from Binance REST. | Prerequisite for backtest. Eliminates fresh REST fetches on every execution cycle. |
| Feed interface (`ReplayFeed` + `LiveFeed`) | **MUST** | `Feed` interface: `Next() (Candle, bool)`. `ReplayFeed` reads from Postgres sequentially. `LiveFeed` reads from Binance WebSocket. Engine receives candles through the interface only. | Single execution engine for both backtest and production. Zero parity risk. |
| Backtest execution engine | **MUST** | Loop over `ReplayFeed`, call strategy lifecycle hooks per candle, simulate order fills, track P&L and positions. No exchange calls. | Core capability for strategy validation before capital deployment. |
| Performance metrics calculator | **MUST** | After backtest: Sharpe Ratio (annualized), Max Drawdown, Win Rate, Profit Factor, Total Trades, Avg Trade Duration. Via `cinar/indicator` ACL wrapper. | Metrics are the decision gate for proceeding to Dry-Run. |
| HTML backtest report | **SHOULD** | Generate static HTML file with equity curve chart, drawdown chart, monthly P&L heatmap, and full trade list after each backtest run. Via `cinar/indicator` ACL. | Deep analysis beyond what fits in terminal. Shareable. |
| Walk-forward validation | **SHOULD** | Split historical data into rolling train/test windows. Run backtest on each window. Report out-of-sample performance separately to detect overfitting. | Without walk-forward, Sharpe from a single backtest is misleading. |
| Multi-timeframe candle support | **SHOULD** | Strategy can access candles at any registered timeframe (1m, 5m, 15m, 1h) via `StrategyContext`. Backtest generates higher-TF candles from 1m base. | Many effective strategies use multiple timeframes for signal confirmation. |
| Hyperparameter optimization | **COULD** | Run backtest across a parameter grid (e.g., RSI period 10–20, stop-loss 1–3%). Report best-performing configuration. Via `cinar/indicator` ACL. | Automates parameter search. Risk: overfitting if not paired with walk-forward. |

### 4.4 Execution Modes

> ⚠️ The four modes form a **mandatory validation pipeline**. No strategy should reach Capital Real without passing through the prior three stages.

| Mode | Data Source | Order Execution | Risk | Purpose |
|---|---|---|---|---|
| **Backtest** | Postgres (historical) | Simulated by engine | None | Strategy validation. Fast as possible. |
| **Dry-Run** | Binance WebSocket (live) | Simulated with configurable slippage + fees | None | Validate behavior on real prices without testnet dependency. |
| **Paper Trading** | Binance WebSocket (live) | Real orders to Binance Testnet | None | Confirm order execution fidelity, fills, slippage real. |
| **Capital Real** | Binance WebSocket (live) | Real orders to Binance Production | Full | Live trading with actual capital. Requires prior stages. |

### 4.5 TUI — btop-Inspired Interface

> All pages share: box-drawing characters for panels, braille-dot sparklines for time series, keyboard navigation (number keys or F1–F6), no mouse required, zero flickering on refresh, color-coded status indicators.

#### Page 1 — Dashboard (main view)
- **Header bar**: bot name, current time, connection status (green dot = WS connected, red = disconnected), account balance
- **Left panel**: Account summary — total balance, available margin, daily P&L (green/red), open positions count
- **Center panel**: Monitored pairs grid — each pair shows symbol, last price, 24h change %, and a braille sparkline of last 60 candles (1m). Color: green if price above EMA20, red if below.
- **Right panel**: Active strategies — name, mode badge (`LIVE` / `DRY` / `PAPER` / `BT`), status (running / paused / error)
- **Bottom bar**: keyboard shortcut hints

#### Page 2 — Strategies
- Full-width table: strategy name, algorithm type, monitored symbols, cycle, current mode, last execution time, last execution status, open signals count
- Selected row highlighted. Enter opens detail panel.
- **Detail panel** (overlay): config JSON preview, P&L since activation, total trades, win rate from live history
- **Key bindings**: `[e]` enable, `[d]` disable, `[b]` run backtest, `[r]` switch mode (cycle through Dry-Run → Paper → Live)

#### Page 3 — Open Positions
- One card per open position: symbol, strategy name, entry price, quantity, invested amount, current price (live, updated every second), unrealized P&L in $ and %, duration open
- P&L column: green if positive, red if negative, brightness scaled to magnitude
- Stop-loss and take-profit prices shown inline if set as exchange orders
- **Key bindings**: `[c]` manually close position (confirmation prompt)

#### Page 4 — Backtest Launcher
- Strategy selector: arrow keys to choose from registry list
- Parameter inputs: start date, end date, symbol (or All symbols from strategy config), timeframe
- Dry-Run config panel (visible only in Dry-Run mode): slippage %, fee %, fill delay ms
- `[Enter]` runs backtest. Progress bar with ETA appears. Results auto-navigate to Page 5 on completion.
- Recent backtests list below launcher: strategy, date range, Sharpe, result (pass/fail threshold), link to HTML report path

#### Page 5 — Backtest Results
- **Top row**: key metrics — Sharpe Ratio, Max Drawdown %, Win Rate %, Profit Factor, Total Trades, Total Return %
- **Center**: equity curve as full-width braille sparkline — time on X axis, portfolio value on Y
- **Below**: drawdown curve sparkline — shows underwater periods clearly
- **Trade log table**: open time, close time, symbol, entry price, exit price, P&L, duration, exit reason (TP / SL / Manual)
- `[h]` open HTML report in default browser. `[w]` run walk-forward validation. `[s]` save result to history.

#### Page 6 — Execution Log
- Live scrolling event feed — newest at top
- **Color coding**: green = position opened, yellow = signal generated, red = position closed at loss, blue = position closed at profit, gray = strategy cycle (no signal), white = system event
- Each event: timestamp, strategy name, symbol, event type, price, detail message
- **Filter bar**: `[f]` to filter by strategy or event type. `[/]` to text search.

---

## 5. Strategy Creation: Jesse vs go-trade-bot

### How Jesse Does It

Jesse uses Python's Abstract Base Class system. Every strategy is a Python class in its own directory under `strategies/`. The engine discovers it by name, instantiates it, and calls its methods via polymorphism on each candle. No engine code changes when a new strategy is added.

```python
# strategies/MyStrategy/__init__.py
from jesse.strategies import Strategy
import jesse.indicators as ta

class MyStrategy(Strategy):
    @property
    def rsi(self):
        return ta.rsi(self.candles, 14)

    def should_long(self) -> bool:
        return self.rsi < 30

    def go_long(self):
        qty = self.capital / self.price
        self.buy = qty, self.price
        self.stop_loss = qty, self.price * 0.98
        self.take_profit = qty, self.price * 1.04

    def update_position(self):
        # Update trailing stop dynamically
        self.stop_loss = self.position.qty, self.high - (self.atr * 2)

    def should_short(self) -> bool:
        return False

    def should_cancel_entry(self) -> bool:
        return True
```

**Key mechanics:**
- `self.candles` is a live numpy array updated by the engine
- `self.buy = (qty, price)` is declarative — the engine detects the change and submits the order
- Properties like `self.rsi` are computed lazily and cached per candle
- Engine calls `before()` → `_check()` → `after()` on every candle

### How go-trade-bot Will Do It

Go uses explicit interface satisfaction. A strategy is a struct in its own package under `app/strategies/`. It satisfies the `Strategy` interface and self-registers in the central registry via `init()`. The engine resolves it by name and calls hooks via interface dispatch. No switch statement, no handler changes.

```go
// app/strategies/rsi_oversold/strategy.go
package rsioversold

import "go-trade-bot/app/strategies"

func init() {
    strategies.Register("rsi_oversold", func() strategies.Strategy {
        return &RSIOversoldStrategy{}
    })
}

type RSIOversoldStrategy struct{}

func (s *RSIOversoldStrategy) Name() string { return "rsi_oversold" }

func (s *RSIOversoldStrategy) ShouldLong(ctx strategies.Context) bool {
    rsi := ctx.Indicators.RSI(ctx.Candles, 14)
    return rsi[len(rsi)-1] < 30
}

func (s *RSIOversoldStrategy) GoLong(ctx strategies.Context) strategies.Signal {
    return strategies.Signal{
        Buy: strategies.Order{
            Qty:   ctx.Account.Available / ctx.Price,
            Price: ctx.Price,
        },
        StopLoss: strategies.Order{
            Qty:   ctx.Account.Available / ctx.Price,
            Price: ctx.Price * 0.98,
        },
        TakeProfit: strategies.Order{
            Qty:   ctx.Account.Available / ctx.Price,
            Price: ctx.Price * 1.04,
        },
    }
}

func (s *RSIOversoldStrategy) UpdatePosition(ctx strategies.Context) *strategies.Signal {
    return nil // hold position
}

func (s *RSIOversoldStrategy) ShouldShort(ctx strategies.Context) bool { return false }
func (s *RSIOversoldStrategy) GoShort(ctx strategies.Context) strategies.Signal { return strategies.Signal{} }
func (s *RSIOversoldStrategy) Before(ctx strategies.Context)           {}
func (s *RSIOversoldStrategy) After(ctx strategies.Context)            {}
func (s *RSIOversoldStrategy) Terminate(ctx strategies.Context)        {}
```

### Key Differences

| Dimension | Jesse (Python) | go-trade-bot (Go) |
|---|---|---|
| Strategy discovery | File-based: engine scans `strategies/` directory by name | Registry-based: `init()` self-registers on import |
| Order declaration | Declarative: `self.buy = (qty, price)`. Engine detects diff and submits. | Return-based: `GoLong()` returns `Signal` struct. Engine processes it explicitly. |
| Hot reload | Yes — Python imports reload at runtime | No — requires recompile. Acceptable trade-off for type safety and performance. |
| Indicator access | Via module import: `import jesse.indicators as ta` | Via `IndicatorProvider` ACL: `ctx.Indicators.RSI(candles, 14)` |
| State between candles | Instance variables on `self` (no isolation) | Optional state field on strategy struct — explicit, testable |
| ML extension path | Native Python — same process | gRPC adapter — Python ML in separate process, called via interface |

### Proposed Core Interfaces

```go
// app/strategies/interface.go

type Strategy interface {
    Name() string
    Before(ctx Context)
    ShouldLong(ctx Context) bool
    GoLong(ctx Context) Signal
    ShouldShort(ctx Context) bool
    GoShort(ctx Context) Signal
    UpdatePosition(ctx Context) *Signal
    After(ctx Context)
    Terminate(ctx Context)
}

type Context struct {
    Candles    []Candle
    Position   *Position
    Account    Account
    Config     map[string]interface{}
    Indicators IndicatorProvider
    Price      float64
    Timeframe  string
    Symbol     string
    Mode       ExecutionMode // Backtest | DryRun | Paper | Live
}

type Signal struct {
    Buy        *Order
    Sell       *Order
    StopLoss   *Order
    TakeProfit *Order
}

type ExecutionMode int
const (
    ModeBacktest ExecutionMode = iota
    ModeDryRun
    ModePaper
    ModeLive
)
```

```go
// internal/indicators/interface.go — ACL (Anti-Corruption Layer)

type IndicatorProvider interface {
    RSI(candles []Candle, period int) []float64
    BollingerBands(candles []Candle, period int, stdDev float64) (upper, mid, lower []float64)
    EMA(candles []Candle, period int) []float64
    SMA(candles []Candle, period int) []float64
    MACD(candles []Candle, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist []float64)
    ATR(candles []Candle, period int) []float64
    // Add as needed — implementations hidden behind this interface
}
```

```go
// internal/exchange/interface.go — ACL (Anti-Corruption Layer)

type ExchangeClient interface {
    PlaceOrder(ctx context.Context, order PlaceOrderRequest) (OrderResult, error)
    CancelOrder(ctx context.Context, symbol, orderID string) error
    ListKline(ctx context.Context, symbol, interval string, limit int) ([]Candle, error)
    ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error)
    GetAccountBalance(ctx context.Context) (AccountBalance, error)
    SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error)
}
```

---

## 6. Go Library Landscape

> ⚠️ **ACL Rule**: All external libraries must be accessed exclusively through ACL interfaces defined in `internal/`. No domain or strategy code imports library packages directly. This enables swapping any implementation without touching strategy code.

| Library | Purpose | Stars / Activity | Role in bot |
|---|---|---|---|
| `github.com/markcheno/go-talib` | Technical indicators (TA-Lib port) | 938 ⭐ / stable | Already in use. Wrapped by `IndicatorProvider` ACL. Covers RSI, BBands, EMA, MACD for existing algorithms. |
| `github.com/cinar/indicator/v2` | Indicators + backtest framework + metrics + HTML report | 1,225 ⭐ / active 2026 | Primary source for backtest engine, Sharpe/Drawdown metrics, HTML report generation, and strategy composition patterns. All access via ACL adapters. |
| `github.com/adshao/go-binance/v2` | Binance REST + WebSocket SDK | Already in use | All Binance calls (`PlaceOrder`, WebSocket stream) via `ExchangeClient` ACL. No domain code imports this directly. |
| `github.com/gizak/termui/v3` | TUI rendering engine | Already in use | Powers all TUI pages. Braille sparklines via `Sparkline` widget. Keyboard navigation via `EventLoop`. |
| `gonum.org/v1/gonum/stat` | Statistics (correlation, regression) | 7k ⭐ / active | Walk-forward statistics, correlation analysis between strategy signals. Via `MetricsProvider` ACL. |

---

## 7. Product Roadmap

> Phases are ordered by dependency, not by calendar. Each phase is independently shippable and testable. No fixed timeline — implement at 5–10h/week pace.

### Phase 1 — Foundation: Live Trading That Actually Trades

**Goal**: Transform the bot from a paper tracker into a real trading system. Every feature in this phase is a prerequisite for everything else.

**Features:**
- [ ] Broker ACL (`ExchangeClient` interface + `BinanceAdapter`)
- [ ] `PlaceOrder` and `CancelOrder` on Binance (spot and futures)
- [ ] Stop-loss submitted as real `STOP_MARKET` exchange order
- [ ] WebSocket `LiveFeed` replacing REST polling
- [ ] Strategy interface with lifecycle hooks
- [ ] Strategy registry (self-registration via `init()`)
- [ ] `StrategyContext` struct (`Candles`, `Position`, `Account`, `Config`, `Indicators`)
- [ ] `IndicatorProvider` ACL wrapping `go-talib`
- [ ] Port Bollinger, Grid, Scalping to new Strategy interface
- [ ] Generic webhook notifier (POST to configurable URL)

**Success Signal**: First real order placed and settled on Binance production. Existing three strategies running through the new interface without behavioral change.

**Risk**: Introducing `PlaceOrder` without adequate testing can cause real financial loss. Mitigation: mandatory Paper Trading (testnet) phase for each strategy before enabling on production. Add `MODE` env var guard that hard-errors if set to `LIVE` without explicit `--confirm-live` flag.

---

### Phase 2 — Research: Backtest Engine + Strategy Validation Pipeline

**Goal**: Give the developer a research loop. No strategy reaches live capital without passing through this pipeline.

**Features:**
- [ ] Local candle storage schema in Postgres
- [ ] Candle import CLI (historical data from Binance REST)
- [ ] `Feed` interface: `Next() (Candle, bool)`
- [ ] `ReplayFeed`: reads from Postgres, simulates candle arrival
- [ ] Backtest execution engine (same engine as live, source = `ReplayFeed`)
- [ ] `MetricsProvider` ACL wrapping `cinar/indicator/v2` (Sharpe, Drawdown, Win Rate, Profit Factor)
- [ ] TUI Page 4 — Backtest Launcher
- [ ] TUI Page 5 — Backtest Results (metrics panel + equity sparkline)
- [ ] Walk-forward validation
- [ ] HTML report generation via `cinar/indicator` ACL
- [ ] Dry-Run mode (`LiveFeed` + simulated order fills with configurable slippage/fees)

**Success Signal**: All three existing strategies backtested over 12 months of candle data. Walk-forward validation results visible in TUI. HTML report opens in browser from `[h]` key. At least one strategy validated through full Backtest → Dry-Run → Paper Trading pipeline.

**Risk**: Backtest parity — if `ReplayFeed` and `LiveFeed` diverge in how they deliver candles, backtest results won't predict live behavior. Mitigation: `simulator_parity_test` that runs the same strategy on the same candle sequence through both feeds and asserts identical P&L.

---

### Phase 3 — Polish: TUI Completion + Position Management

**Goal**: Complete the btop-inspired TUI and add production-grade position management features.

**Features:**
- [ ] TUI Page 1 — Dashboard with braille sparklines (all monitored pairs)
- [ ] TUI Page 2 — Strategies (full management: enable, disable, switch mode)
- [ ] TUI Page 3 — Open Positions (real-time P&L, color-coded)
- [ ] TUI Page 6 — Execution Log (live event feed with filters)
- [ ] Position sizing strategies (fixed amount, % of capital)
- [ ] Multi-timeframe candle access in `StrategyContext`
- [ ] Strategy error handling and auto-recovery (restart on panic, webhook alert)
- [ ] Candle pipeline warm-up (pre-load N candles before strategy starts so indicators have valid values from the first signal)

**Success Signal**: All 6 TUI pages navigable by keyboard with zero flickering. Dashboard braille sparklines updating live. Strategy mode switching (Dry-Run → Paper → Live) triggerable from TUI without bot restart.

**Risk**: `termui/v3` has known limitations for complex layouts. If braille sparklines + real-time updates cause rendering issues, evaluate migration to `bubbletea` (Charm). The `Page` interface in `cmd/console/` makes migration tractable since all rendering is behind `Page.Render()`.

---

### Phase 4 — Scale: Optimization + ML Extension

**Goal**: Unlock non-linear strategy improvement through automated optimization and future-proof the architecture for ML-based strategies.

**Features:**
- [ ] Hyperparameter optimization (grid search over strategy config params, via `cinar/indicator` ACL)
- [ ] Optimization results page in TUI: parameter heatmap, best config highlighted
- [ ] Monte Carlo simulation (randomize trade order to test robustness)
- [ ] gRPC ML strategy adapter (`Strategy` interface implementation delegating to external Python process)
- [ ] Strategy performance history persistence (track live P&L per strategy over time in Postgres)
- [ ] TUI — P&L history sparkline per strategy (daily/weekly/monthly)

**Success Signal**: Hyperparameter optimization runs for Grid strategy, surfaces best RSI threshold and grid spacing combination. gRPC adapter compiles and delegates a dummy strategy call to a Python echo server — proving the interface works before any ML model is trained.

---

## 8. Technical Considerations

- **Feed interface is the architectural linchpin.** Every other component (backtest engine, live engine, Dry-Run engine) depends on it. Get this interface right in Phase 1 — changing it later breaks everything downstream.

- **No global mutable state in strategy code.** Jesse's singleton `StoreClass` is its biggest maintenance liability. In go-trade-bot, all state flows through `StrategyContext` — strategies are stateless by default, stateful only via explicit struct fields. This makes unit testing trivial.

- **ACL boundaries are non-negotiable.** `IndicatorProvider`, `ExchangeClient`, `MetricsProvider`, `NotificationSender` — these are the four external-facing interfaces. Nothing outside `internal/` imports library packages. Enforced at code review, not runtime.

- **Candle data volume.** 1 year of 1m candles for 10 pairs ≈ 5.2 million rows in Postgres. Index on `(symbol, timeframe, open_time)`. The existing Postgres instance handles this easily — no separate time-series DB needed.

- **Simulator parity test is mandatory** before Phase 2 is considered done. Same strategy, same candle sequence, `ReplayFeed` vs `LiveFeed` must produce identical P&L.

- **`MODE` guard**: engine checks `MODE` env var at startup. `LIVE` requires explicit `--confirm-live` flag. Prevents accidental production trading during development.

- **TUI migration path**: if `termui/v3` becomes a bottleneck, `bubbletea` (Charm) is the modern alternative. The `Page` interface in `cmd/console/` makes migration tractable.

- **gRPC ML adapter (Phase 4)** is designed but not implemented until a concrete ML strategy is ready. Define the proto file early so the interface is stable; implement the Go client when the Python model exists.

---

## 9. Deployment & Validation Strategy

**Homelab constraint**: 4GB WSL2 RAM available after BRUCE, n8n, Postgres, nginx. Bot stack target: <500MB idle. Do not introduce new stateful services — share existing Postgres and Redis instances.

**Branch-per-phase strategy**: Each phase lives on a feature branch. Phase N is merged to main only when its success signal is met. Never deploy a phase partially.

**Mandatory staging gate**:
- Backtest + Walk-Forward → minimum 2 weeks Dry-Run
- Dry-Run → minimum 4 weeks Paper Trading
- Paper Trading → Capital Real

These gates are enforced by `MODE` guard logic, not by discipline alone.

**Candle history import**: Before Phase 2 backtest runs, import minimum 2 years of 1m candles for all monitored symbols. Candle import is a one-time operation per symbol; ongoing candles accumulate via LiveFeed.

**Capital allocation rule**: First Capital Real deployment uses maximum R$1,500–2,000. Remaining capital is buffer for engineering errors. Scale capital only after 8 weeks of consistent positive performance with drawdown within backtest-predicted range.

**Observability from day one**: Prometheus metrics + Grafana dashboards must include: order execution latency, WebSocket reconnect count, strategy cycle execution time, and error rate per strategy. Alert on `error_rate > 0` for 5 minutes via webhook → n8n.

---

## 10. Open Questions & Assumptions

### Assumptions baked into this PRD

- Assumed Binance-only exchange for the full backlog. Broker ACL is designed for multi-exchange but no second implementation is scoped.
- Assumed `termui/v3` is sufficient for Phase 3 TUI. If braille sparklines cause flickering at the required refresh rate (1s), evaluate migration to `bubbletea` (Charm).
- Assumed `go-talib` covers existing strategy indicator needs. `cinar/indicator/v2` is introduced only through the ACL adapter.
- Assumed Paper Trading uses Binance Testnet.
- Assumed the gRPC ML adapter is Phase 4. If an ML strategy becomes the primary use case sooner, this moves to Phase 3.

### Questions to answer before Phase 2

- What is the minimum Sharpe Ratio threshold to proceed from Backtest to Dry-Run? Suggested starting point: Sharpe > 0.8, Max Drawdown < 20%.
- Which symbols will be the initial candle import set? Broader = more backtest options; narrower = faster import and less storage.
- Should the HTML report be generated on every backtest run, or only on explicit `[h]` key request?
- What is the acceptable Paper Trading duration for strategies with low signal frequency (e.g., Grid on 1h timeframe)?

### Won't Have (now)

- Bybit or other exchange support (interface ready, implementation not scoped)
- Web dashboard (TUI is the primary interface; REST API exists for programmatic access)
- Social or copy trading features
- Automated strategy discovery or genetic strategy generation
