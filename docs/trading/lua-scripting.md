# Lua Strategy Scripting Guide

Every trading strategy in TradeBot is written in standard Lua and executed within a sandboxed `gopher-lua` runtime (`app/strategies/script/`).

This provides fast iteration, dynamic script updates without recompiling Go binaries, hot-reloading in the web IDE, and complete safety isolation.

---

## 1. Strategy Lifecycle Hooks

A Lua strategy defines four standard lifecycle functions:

```lua
-- 1. Called once when the strategy is loaded or initialized
function init(ctx)
    ctx:set_cycle_interval("15m")
end

-- 2. Evaluated on each closed candle when there is NO open position
function go_long(ctx)
    -- Calculate entry conditions
    return {
        action = "buy",
        qty = 0.01,
        stop_loss = { price = ctx.price * 0.98 },
        take_profit = { price = ctx.price * 1.05 }
    }
end

-- 3. Evaluated on each closed candle when a position IS open
function update_position(ctx)
    -- Calculate exit conditions
    if some_exit_condition then
        return { action = "sell", reason = "trend_exhaustion" }
    end
    return nil
end

-- 4. Called when the strategy is stopped or disabled
function terminate(ctx)
    -- Optional cleanup logic
end
```

---

## 2. The Execution Context (`ctx`)

On every cycle, the Go engine marshals current market conditions into the `ctx` table:

| Field | Type | Description |
| :--- | :--- | :--- |
| `ctx.price` | `number` | Latest close price of the asset. |
| `ctx.candles` | `table` | Array of candle objects `{open, high, low, close, volume, time}`. |
| `ctx.symbol` | `string` | The traded pair (e.g., `"BTCUSDT"`). |
| `ctx.position` | `table` or `nil` | Active position: `{side: "long", entry_price, qty, entry_time}`. |
| `ctx.balance` | `number` | Available quote currency balance (e.g., USDT balance). |

---

## 3. Persistent Cross-Cycle State (`state.*`)

Because strategy cycles can be redelivered to different worker instances or across worker restarts, **global Lua variables do not persist across cycles**.

Instead, TradeBot provides the `state` API, backed by PostgreSQL (`script_state` table):

```lua
function go_long(ctx)
    local counter = state.get("consecutive_losses") or 0
    if counter >= 3 then
        -- Skip entry if 3 consecutive losses occurred
        return nil
    end
    -- ...
end

function update_position(ctx)
    if ctx.price > state.get("trailing_high") or 0 then
        state.set("trailing_high", ctx.price)
    end
end
```

---

## 4. Built-In Technical Indicators (`ind.*`)

TradeBot wraps **TA-Lib** functions as native Lua closures under the `ind.*` namespace. All indicator functions accept the candle array and numeric parameters:

### Momentum & Trend Indicators
```lua
-- RSI (Relative Strength Index)
local rsi = ind.rsi(ctx.candles, 14)

-- Moving Averages
local sma20 = ind.sma(ctx.candles, 20)
local ema50 = ind.ema(ctx.candles, 50)

-- MACD (Moving Average Convergence Divergence)
-- Returns macd, signal, hist tables
local macd, signal, hist = ind.macd(ctx.candles, 12, 26, 9)

-- Bollinger Bands
-- Returns upper, middle, lower tables
local upper, middle, lower = ind.bollinger(ctx.candles, 20, 2.0, 2.0)

-- Average Directional Index (ADX) & ATR
local adx = ind.adx(ctx.candles, 14)
local atr = ind.atr(ctx.candles, 14)

-- Stochastic Oscillator
local slowk, slowd = ind.stoch(ctx.candles, 5, 3, 3)
```

---

## 5. Prototyping: REPL & Fast Rerun

The web dashboard strategy IDE provides interactive prototyping tools backed by dedicated REST endpoints:

* **Interactive REPL (`POST /api/script/repl`)**: Execute ad-hoc Lua snippets against latest live market context to inspect indicator output and debug syntax immediately.
* **Fast Rerun (`POST /api/script/fast-rerun`)**: Rapidly simulates the strategy against a bounded in-memory buffer of recent candles (100 candles), producing detailed step-by-step `TraceRecord` execution logs and indicator plots without launching a full backtest task.
