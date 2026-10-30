# Model Context Protocol (MCP) Server

TradeBot includes a native **Model Context Protocol (MCP)** server (`cmd/mcp`), enabling external AI assistants such as **Claude Desktop**, **Cursor**, and **Antigravity** to interact directly with the trading system.

---

## 1. Architecture: In-Process Tool Execution

Unlike typical integrations that require exposing public HTTP endpoints and transmitting admin API tokens over the network, TradeBot's MCP server is an inbound adapter that calls application usecases **directly in-process**:

```
┌───────────────────────────────┐
│ External LLM (Claude, Cursor) │
└──────────────┬────────────────┘
               │ JSON-RPC (stdio / HTTP)
               ▼
┌───────────────────────────────┐
│       cmd/mcp (Server)        │
├───────────────────────────────┤
│ • Strict Input Validation     │
│ • Credential Stripping        │
│ • Compact LLM-Optimized DTOs  │
└──────────────┬────────────────┘
               │ Direct In-Process Go Calls
               ▼
┌───────────────────────────────┐
│     Application Usecases      │
└───────────────────────────────┘
```

---

## 2. Operator Tools Reference (33 Tools)

TradeBot provides 33 operator tools categorized across core domains:

### A. Strategy Prototyping & Inspection
* `get_strategy`: Fetch strategy parameters, configuration JSON, and Lua source.
* `list_strategies`: List all registered strategies across all execution modes.
* `script_repl`: Execute an ad-hoc Lua code snippet against live market context.
* `script_fast_rerun`: Replay a script against recent candles and return step-by-step trace records.
* `list_script_versions`: Inspect historical script versions and diffs.

### B. Backtesting & Quantitative Simulations
* `run_backtest`: Launch an asynchronous backtest simulation over a specified candle range.
* `get_backtest`: Poll backtest status, equity curve, Sharpe ratio, and drawdown metrics.
* `run_walk_forward`: Execute a walk-forward optimization across in-sample and out-of-sample windows.
* `run_monte_carlo`: Run trade order permutation simulations to analyze sequence risk.

### C. Operations & Live Risk
* `get_open_positions`: Inspect current open positions, unrealized PnL, and entry timestamps.
* `list_closed_signals`: Query trade history, exit reasons, and realized profits.
* `get_candle_coverage`: Check historical candle data depth before scheduling backtests.
* `get_scheduler_health`: Audit Asynq queue depths, latency, and worker health.

### D. Full Agent Lifecycle Management
* `create_agent` / `update_agent` / `delete_agent`: Manage persona system prompts, models, and budgets.
* `run_agent`: Manually enqueue an immediate agent execution.
* `pause_agent`: Pause or resume a specific agent persona.
* `set_kill_switch`: Activate the global emergency kill switch to halt all autonomous agents.

### E. Deep-Linked Performance Reports
* `get_backtest_report_url`: Generates browser-ready links to full HTML backtest audit dashboards.
* `get_agent_report_url`: Generates deep links to agent performance audits and proposal evidence.

---

## 3. Security & Information Protection

* **Zero Credential Leakage**: Tools such as `get_settings` and `list_webhook_targets` deliberately strip all passwords, secret keys, webhook URLs, and tokens before returning data to the LLM.
* **No Trading Orders via MCP**: Order submission tools are deliberately excluded from the MCP surface. An external AI assistant cannot place orders or liquidate positions.
* **Pinned Tool Allowlist**: The MCP tool write set is strictly pinned by automated tests (`TestWriteSetIsPinned` and `mcptools/ports.go`). Methods not explicitly declared on the port interface cannot be called.
