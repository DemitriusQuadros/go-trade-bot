# REST API Reference

All backend API routes are mounted uniformly under the `/api` prefix by `cmd/api`.

---

## 1. Authentication & Security Headers

TradeBot supports two authentication mechanisms:

1. **HttpOnly Session Cookie (`gtb_session`)**: Used by the web dashboard upon logging in via `/api/auth/login`. State-changing requests (`POST`, `PUT`, `DELETE`) require the `X-Requested-With: gtb` header or a verified matching `Origin` header to mitigate CSRF attacks.
2. **Bearer Service Token (`API_TOKEN`)**: Passed in the `Authorization: Bearer <token>` header for headless API clients and administrative scripting.

---

## 2. Core Endpoint Catalogue

### Authentication & Users
| Method | Endpoint | Capability | Description |
| :--- | :--- | :--- | :--- |
| `POST` | `/api/auth/login` | Public | Authenticate username/password and receive session cookie. |
| `POST` | `/api/auth/logout` | Authenticated | Revoke session cookie. |
| `GET` | `/api/auth/me` | Authenticated | Return authenticated principal details and capabilities. |
| `GET` | `/api/users` | `admin` | List all registered user accounts. |
| `POST` | `/api/users` | `admin` | Create a new user account with assigned capabilities. |

### Strategy Management
| Method | Endpoint | Capability | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/strategies` | `view` | List all configured strategies. |
| `POST` | `/api/strategies` | `edit_drafts` | Create a new strategy (non-admins limited to `backtest` mode). |
| `GET` | `/api/strategies/{id}` | `view` | Fetch strategy parameters, configuration JSON, and Lua code. |
| `PUT` | `/api/strategies/{id}` | `edit_drafts` / `admin` | Update strategy parameters and Lua source. |
| `DELETE`| `/api/strategies/{id}` | `admin` | Delete a strategy. |

### Script Prototyping
| Method | Endpoint | Capability | Description |
| :--- | :--- | :--- | :--- |
| `POST` | `/api/script/repl` | `edit_drafts` | Evaluate an ad-hoc Lua snippet in live market context. |
| `POST` | `/api/script/fast-rerun` | `edit_drafts` | Replay script against 100 recent candles with cycle tracing. |

### Backtesting & Simulations
| Method | Endpoint | Capability | Description |
| :--- | :--- | :--- | :--- |
| `POST` | `/api/backtest` | `backtest` | Run historical backtest. Supports `{"async": true}` for queued runs. |
| `POST` | `/api/backtest/walkforward` | `backtest` | Run walk-forward optimization. Supports async execution. |
| `GET` | `/api/backtest/{id}` | `view` | Return backtest metrics, equity curve, and status. |
| `GET` | `/api/backtest/{id}/report`| `view` | Stream or render self-contained HTML performance report. |

### Candle Datasets (Reconciler)
| Method | Endpoint | Capability | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/candle-datasets` | `view` | List managed candle datasets, coverage %, and sync lag. |
| `POST` | `/api/candle-datasets` | `admin` | Declare new dataset (e.g., BTCUSDT 15m). |
| `POST` | `/api/candle-datasets/{id}/pause` | `admin` | Pause reconciler chunks for dataset. |
| `POST` | `/api/candle-datasets/{id}/resume` | `admin` | Resume reconciler chunks. |
| `POST` | `/api/candle-datasets/{id}/retry-failed` | `admin` | Re-enqueue dead chunks after failures. |
| `POST` | `/api/candle-datasets/{id}/reconcile` | `admin` | Trigger an immediate coverage sweep. |

### Strategy Change Proposals
| Method | Endpoint | Capability | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/proposals` | `view` | List pending/applied proposals with gate evidence. |
| `GET` | `/api/proposals/{id}` | `view` | Fetch proposal diff and walk-forward verification data. |
| `POST` | `/api/proposals/{id}/approve` | `approve_proposals` | Authorize live promotion (enqueues `agent:apply_proposal`). |
| `POST` | `/api/proposals/{id}/reject` | `approve_proposals` | Reject proposed candidate. |

### Autonomous Agents Platform
| Method | Endpoint | Capability | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/agents` | `view` | List all agent personas, triggers, and budgets. |
| `POST` | `/api/agents/{id}/run` | `agent_chat` | Enqueue immediate agent execution. |
| `PUT` | `/api/agents/kill-switch` | `admin` | Toggle the global agent execution kill switch. |
| `GET` | `/api/agents/market-symbols` | `view` | Return symbols currently tracked by the 1m market watcher. |

### Real-Time Streaming
| Method | Endpoint | Capability | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/stream/dashboard` | `view` | Server-Sent Events (SSE) stream of real-time ticker prices and positions. |
