// Package real is the real-stack end-to-end suite (Gherkin/Godog).
//
// Unlike tests/e2e (whose "API server is running" step is a no-op and whose
// request steps fabricate responses when no server exists), every scenario
// here drives the REAL production code over its real transport: the actual
// gorilla/mux handlers over httptest, the real usecases and repositories on
// SQLite, the real MCP server with an in-memory MCP client, and the real
// BacktestUseCase.ExecuteQueued standing in for cmd/worker's processor. Only
// Redis/asynq (the task queue) is replaced, by a recording fake.
//
// What it still cannot prove: Postgres-specific behavior, real Redis/asynq
// delivery, and the cmd/* process wiring. Those belong to the opt-in smoke
// test described in the backlog.
package real
