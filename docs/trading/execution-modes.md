# Execution Modes & Order Routing

TradeBot supports four distinct execution modes designed to transition strategies safely from initial concept to live capital allocation:

```
┌─────────────────────────────────────────────────────────────┐
│                    STRATEGY LIFECYCLE                       │
│                                                             │
│  [ Backtest ] ──► [ Dry-Run ] ──► [ Paper ] ──► [ Live ]     │
│   Historical       Simulated        Testnet       Real      │
│   Replay Feed      Live Tickers     Exchange      Exchange  │
└─────────────────────────────────────────────────────────────┘
```

---

## The Four Execution Modes

### 1. `backtest`
* **Purpose**: Historical simulation over stored candle datasets.
* **Feed**: `feed.ReplayFeed` (PostgreSQL-backed historical series).
* **Execution**: Orders fill instantaneously on candle closes. Realized PnL, equity curves, maximum drawdown, Sharpe ratio, and execution trace logs are persisted to `backtest_runs`.
* **Worker Guard**: The live trading worker **refuses** backtest-mode strategies (`strategy.error`). Backtests are triggered exclusively via the Backtest API or MCP tools.

### 2. `dryrun`
* **Purpose**: Forward paper-trading against real live market data without touching exchange accounts.
* **Feed**: `feed.LiveFeed` (Live Binance WebSocket stream).
* **Execution**: Fills are simulated locally by `exchange.DryRunExchange` against real-time ticker prices.
  * **Slippage & Fees**: Modeled with configurable parameters (`DRY_RUN.SLIPPAGE_PCT` and `DRY_RUN.FEE_PCT`).
  * **Simulated Resting Stops**: Persisted as `SIM-STOP-...` records and evaluated across candle highs and lows before each cycle.
  * **Safety**: Reaches the exchange client only through `exchange.ReadOnlyClient`. Any accidental order call is blocked at the Go interface level.

### 3. `paper`
* **Purpose**: Validation against the official Binance Spot Testnet matching engine.
* **Feed**: Binance Spot Testnet WebSocket and REST endpoints.
* **Execution**: Uses the real execution engine, but points to `BinanceTestnetAdapter`. Orders, fills, and stop orders are placed on the Binance Testnet order book.
* **Safety Guard**: `paper` mode requires `TESTNET=true`. It fails to boot if pointed at production credentials.

### 4. `live`
* **Purpose**: Production trading with real capital.
* **Feed**: Production Binance WebSocket stream.
* **Execution**: Direct interaction with Binance Spot matching engines via `BinanceAdapter`.
  * **Native Stop-Loss**: Position opens place immediate exchange-side `STOP_MARKET` orders.
  * **Startup Requirements**: Must have `MODE=live`, `CONFIRM_LIVE=true`, and `TESTNET=false`.

---

## Order Routing & Mode Resolution

The trading worker resolves the **effective execution mode** per strategy:

```go
// Worker evaluates effective mode ceiling
effectiveMode := resolveEffectiveMode(strategy.Mode, env.MODE)
```

| Strategy `Mode` | Env `MODE` | Result |
| :--- | :--- | :--- |
| `live` | `live` (with `CONFIRM_LIVE=true`) | Runs **Live** on real Binance Spot |
| `live` | `dryrun` | **Refused** (fails closed; does not downgrade) |
| `paper` | `paper` (with `TESTNET=true`) | Runs on Binance Testnet |
| `dryrun` | `dryrun` or higher | Runs **Dry-Run** (simulated orders) |
| `backtest` | Any | **Refused by worker** (runs via API only) |

---

## Simulator Parity Guarantee

A common problem in algorithmic trading is "simulation drift," where a strategy performs differently in backtests than it does live due to differences in order handling logic.

TradeBot eliminates this by driving all modes through the **same execution engine (`app/engine/engine.go`)**:
* Indicator values, cycle hooks, daily loss limits, and sizing calculations are 100% identical.
* Verified continuously by `app/engine/simulator_parity_test.go`.
