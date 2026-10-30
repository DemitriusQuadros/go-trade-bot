# Autonomous AI Agents Platform

TradeBot features an autonomous, multi-persona AI Agent Platform (`cmd/agent`) designed to assist operators in monitoring markets, analyzing performance, and iterating on trading algorithms.

---

## 1. Personas & Agent Architecture

Agents are defined as data entities in PostgreSQL (`agents` table) rather than hardcoded prompts. Each agent persona can be customized with:
* **Goal System Prompt**: Defines the persona's role (e.g., Risk Auditor, Mean-Reversion Specialist, Volatility Monitor).
* **Model Provider Override**: Select Claude 3.5 Sonnet, Gemini 1.5 Pro, or Gemini Flash per persona.
* **Granular Permissions**: Tagged tool permissions (`read`, `backtest`, `edit_testing`, `propose_live`, `notify`, `chain`).
* **Daily Budget ($ USD)**: Cost tracking that automatically halts model calls when daily expenditure reaches the persona cap.
* **Strategy Bindings**: Scopes the agent's read/write actions strictly to specific strategies.

---

## 2. Shared Strategy Memory & Journals

Agents and human operators collaborate over a persistent, per-strategy memory ledger:

```
[ Operator Notes in UI ] ──┐
                           ▼
                    [ Strategy Memory ] ◄── [ Agent write_journal ]
                           ▲
                           │
                  [ Agent read_memory ]
```

* **Deliberate Notes Only**: Unlike chat transcripts which contain verbose turn-by-turn history, shared memory holds high-signal findings, key support/resistance observations, and backtest conclusions.
* **Context Injection**: The latest 30 memory entries are injected into the agent's operating prompt on every execution.

---

## 3. Autonomous Triggers (Phase C)

Agents do not just wait for human chat prompts—they react autonomously to system events and market conditions:

### A. Cron Schedules
Run regular audits (e.g., daily PnL summaries every morning at 08:00 UTC).

### B. Trade & Risk Events
* `stoploss.hit`: Agent investigates market conditions when a strategy's stop order fires.
* `position.closed`: Agent reviews trade profitability and logs learnings to strategy memory.
* `drawdown`: Sweeper detects when cumulative realized PnL drops across a rolling time window.

### C. Market Volatility Rules
The 1-minute candle **Market Watcher** monitors live feeds and activates agents when:
* `pct_move`: Rapid price movement exceeds $X\%$ over a defined window (e.g., $\pm 4\%$ in 15 minutes).
* `volatility_spike`: Current ATR surges beyond $2\times$ the prior 24-hour ATR baseline.

### D. Chain Triggers
Agents can chain actions declaratively:
* Agent A (Market Scanner) spots an anomalous trend $\longrightarrow$ Triggers Agent B (Backtest Specialist) to test an adaptive strategy $\longrightarrow$ Triggers Agent C to author a proposal report.
* Guards prevent runaway execution: maximum chain depth of 3, cycle detection, and distributed cooldowns via Redis.

---

## 4. RunGuard & Safety Caps

Before **every single model call**, the `RunGuard` checks:
1. **Global Kill Switch**: If `Settings.AgentsPaused` is active, all agent executions halt immediately.
2. **Daily Persona Budget**: Prevents API token cost overruns.
3. **Iteration Cap**: Chat sessions cap at 8 iterations; unattended background runs cap at 24 iterations.
