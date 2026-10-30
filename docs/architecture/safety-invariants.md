# Safety Invariants & Risk Guards

Because TradeBot executes real financial trades with real capital, safety is engineered into the system architecture as non-negotiable invariants.

---

## 1. The Dual-Layer Mode Guard

A fundamental failure mode in algorithmic trading bots is an accidental configuration mismatch running simulated strategies against real accounts or live strategies on test accounts.

TradeBot prevents this via a **dual-layer independent gate**:

```
┌─────────────────────────────────────────────────────────────┐
│                      DUAL-LAYER GUARD                       │
├──────────────────────────────┬──────────────────────────────┤
│ Strategy.Mode (Per-Strategy) │ System ENV MODE (Process)    │
│   • live                     │   • live                     │
│   • paper                    │   • paper                    │
│   • dryrun                   │   • dryrun                   │
│   • backtest                 │   • backtest                 │
└──────────────────────────────┴──────────────────────────────┘
```

### Safety Rules:
1. **Both Must Agree**: A strategy configured with `mode: live` will run live **only if** the process-wide environment variable `MODE=live` is also set.
2. **Never Silently Downgrade**: If a strategy's requested mode exceeds the process mode ceiling (e.g., strategy is `live` but process is `dryrun`), the engine **refuses the cycle entirely** with a logged error and notification, rather than silently downgrading to paper trading.
3. **Explicit Live Confirmation**: A worker process will **refuse to boot** with `MODE=live` unless `CONFIRM_LIVE=true` is set.
4. **Testnet Consistency**: `MODE=live` requires `TESTNET=false`. A live worker process refuses to start if pointed at the testnet exchange adapter.

---

## 2. Real Exchange Stop-Loss Orders

Many open-source trading bots implement stop-losses by polling tickers locally in a loop and issuing a market sell when price drops below a threshold. If the bot crashes, loses network connectivity, or experiences high garbage collection latency, the position is left unprotected.

### TradeBot Implementation:
* When a strategy opens a long position in `live` mode, the engine executes the entry market order and **immediately places a native exchange-side `STOP_MARKET` order** on Binance.
* If the worker server loses power or restarts, the Binance matching engine continues to enforce the stop order independently.
* In `dryrun` mode, simulated resting stops are tracked via `SIM-STOP-...` records and evaluated across high/low bounds of closed candles before any cycle runs (`engine.SimulatedStopEvaluator`).

---

## 3. Daily Loss Limit (R-01)

To protect capital against adverse market regimes or unexpected indicator behavior, strategies support an automated daily loss limit:

* Configured via `max_daily_loss_usd` in the strategy parameters JSON.
* **Calculation**: At the start of each strategy cycle, the engine calculates cumulative realized profit and loss (PnL) from all trades closed since **00:00 UTC** of the latest candle's day.
* **Enforcement**: If cumulative realized losses exceed the limit ($\le -\text{limit}$), new entry hooks (`go_long`) are automatically blocked until the 00:00 UTC reset.
* **Exits Never Blocked**: Open positions can always exit cleanly via stop-loss or take-profit rules. The limit only inhibits new exposure.
* **Fail-Closed**: If the database query for daily realized PnL fails, the cycle errors and new entries are blocked by default.

---

## 4. Read-Only AI Agent Sandbox

The Autonomous AI Agent runtime (`cmd/agent`) operates under strict architectural sandboxing:

* **No Order Capabilities**: Injected exclusively with `exchange.ReadOnlyClient`. Any call to `PlaceOrder` or `CancelOrder` returns `ErrReadOnlyClient` at the Go interface level.
* **No Access to Credentials**: Settings and exchange API keys are completely stripped and masked from agent tools and MCP responses.
* **Cannot Alter Live Strategies**: AI agents cannot update productive or live strategies. All improvements must be created as **challengers**, pass the automated walk-forward deploy gate, and be explicitly approved by an operator.

---

## 5. Proposal Apply & Flatness Guarantee

When an operator approves a proposed code improvement:
* The change is enqueued as an `agent:apply_proposal` task.
* The applier verifies that the strategy is **flat** (no open positions). If a position is currently open, the apply task re-enqueues every minute until the position exits cleanly.
* The update acquires a distributed Redis lock (`strategy-cycle:<id>`) to guarantee that the trading worker is not mid-cycle while the script source is swapped.
