# Docker Compose Quickstart Guide

This guide walks through deploying the complete TradeBot stack using Docker Compose.

---

## 1. Prerequisites

* **Docker**: 24.0+
* **Docker Compose**: v2.20+
* **Git**: Installed and configured
* **Binance Account**: Spot API Key and Secret (or Testnet credentials)

---

## 2. Configuration Setup

Clone the repository and copy the example configuration:

```bash
# 1. Clone the repository
git clone https://github.com/DemitriusQuadros/go-trade-bot.git
cd go-trade-bot

# 2. Copy the sample configuration file
cp config.example.yml config.yml
```

Edit `config.yml` to set your credentials:

```yaml
# General Execution Mode
mode: "dryrun"          # dryrun | paper | live
confirm_live: false     # Must be true for live trading
testnet: false          # Set true when using Binance Spot Testnet

# Exchange Credentials
binance:
  api_key: "YOUR_BINANCE_API_KEY"
  secret_key: "YOUR_BINANCE_SECRET_KEY"

# Multi-User Authentication
auth:
  session_secret: "generate-a-secure-32-byte-hex-string"
  bootstrap_admin_username: "admin"
  bootstrap_admin_password: "ChooseAStrongPassword123!"

# LLM Agent Provider
agent:
  provider: "gemini"     # gemini | anthropic
  gemini_key: "YOUR_GEMINI_API_KEY"
  gemini_model: "gemini-1.5-pro-latest"
```

---

## 3. Starting the Stack

TradeBot provides a convenient `Makefile` to control the deployment:

```bash
# Build images and start all containers in detached mode
make up
```

This launches the full distributed topology:

| Container | Purpose | Port / Endpoint |
| :--- | :--- | :--- |
| `tradebot-api` | REST API & Web Dashboard | [http://localhost:8080](http://localhost:8080) |
| `tradebot-worker` | Strategy Execution & Reconciler | [http://localhost:9191/tasks/monitoring](http://localhost:9191/tasks/monitoring) |
| `tradebot-agent` | AI Agent Loop & Market Watcher | [http://localhost:9194/tasks/monitoring](http://localhost:9194/tasks/monitoring) |
| `tradebot-mcp` | In-Process MCP Server | stdio or internal bridge |
| `tradebot-postgres` | PostgreSQL 16 Database | localhost:5432 |
| `tradebot-redis` | Redis 7 Task Queue & Cache | localhost:6379 |
| `tradebot-prometheus` | Prometheus Metrics Collector | [http://localhost:9090](http://localhost:9090) |
| `tradebot-grafana` | Performance & Health Dashboards | [http://localhost:3000](http://localhost:3000) (admin / admin) |
| `tradebot-alertmanager` | Alerting Pipeline | [http://localhost:9093](http://localhost:9093) |

---

## 4. First-Time Login

1. Open your browser and navigate to [http://localhost:8080](http://localhost:8080).
2. You will be greeted by the **Console Pro** login gate.
3. Log in using your bootstrap admin credentials configured in `config.yml`:
   * **Username**: `admin`
   * **Password**: `ChooseAStrongPassword123!`
4. The database is initialized and migrated automatically on first boot.

---

## 5. Useful Management Commands

```bash
# View live aggregated container logs
make logs

# Stop all containers while preserving database and redis volumes
make down

# Complete teardown: stop containers and delete volumes
make clean
```
