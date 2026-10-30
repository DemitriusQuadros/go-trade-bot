# Local Bare-Metal Development

This guide covers setting up a local development environment for building and contributing to TradeBot.

---

## 1. Prerequisites

* **Go**: 1.25.0+
* **Node.js**: 20+ with `npm`
* **PostgreSQL & Redis**: Running locally or via Docker
* **Make**: GNU Make

---

## 2. Infrastructure Setup

If you do not have native PostgreSQL and Redis instances installed, start them using Docker:

```bash
docker run -d --name gtb-postgres -p 5432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=tradebot postgres:16-alpine
docker run -d --name gtb-redis -p 6379:6379 redis:7-alpine
```

---

## 3. Running Services Locally

The application uses `Makefile` targets to run each binary with appropriate local flags:

### Terminal 1: REST API & Web Frontend Server
```bash
make run-api-local
```
Starts `cmd/api` on `http://localhost:8080`. It runs auto-migrations and serves the embedded web dashboard.

### Terminal 2: Trading Worker
```bash
make run-worker-local
```
Starts `cmd/worker` on `http://localhost:9191`. Serves Asynqmon monitoring and executes strategy cycles.

### Terminal 3: Agent Runtime
```bash
make run-agent-local
```
Starts `cmd/agent` on `http://localhost:9194`. Serves the agent Asynq queue and market watcher feeds.

### Terminal 4: Frontend Development Server (Hot Reload)
```bash
make web-dev
```
Runs the Vite development server on `http://localhost:5173`. Proxies `/api` requests to `localhost:8080` with Hot Module Replacement (HMR).

---

## 4. Building the Frontend for Go Embedding

The Go API binary embeds the production React build via `go:embed`. Before releasing or running `cmd/api` without Vite:

```bash
# Runs i18n checks, TypeScript compiler, Vite build, and copies assets to cmd/api/webui/dist
make web-build
```

---

## 5. Testing & Quality Assurance

### Run Unit and Integration Tests
```bash
# Test all backend packages
go test ./internal/... ./app/...

# Test with race condition detection
go test -race ./app/engine/... ./app/usecase/strategy/...
```

### Run Real-Stack E2E Test Suite
The `tests/e2e/real/` suite runs a real Gherkin/Godog test environment over SQLite, real HTTP handlers, and an in-memory MCP client:

```bash
go test -v ./tests/e2e/real/
```

### Validate Frontend Build & Translations
```bash
cd web
npm test
npm run build
```
The build script runs `check-i18n.mjs` first, asserting that all string keys match across `en.ts`, `pt-BR.ts`, and `es.ts`.
