# Spec backend-04 — gRPC ML Strategy Adapter

## Overview

PRD §4.2 (COULD priority) wants a `Strategy` implementation that delegates hooks to an external gRPC
process, future-proofing Python ML strategies without touching the Go engine. Per the PRD's own explicit
scoping, this phase's success signal is narrow: "gRPC adapter compiles and delegates a dummy strategy call
to a Python echo server" — not a real ML model integration. This spec defines the proto contract, the Go
client, and `app/strategies/mlgrpc/strategy.go` implementing the **frozen** `Strategy` interface (ADR-005)
without modifying it, plus error handling that deliberately reuses Phase 3's existing panic-recovery
mechanism rather than inventing a parallel one.

## Current Behavior (verified)

- `app/strategies/interface.go` (Phase 1, unchanged through Phases 2-3) defines `Strategy` with
  `Name/Before/ShouldLong/GoLong/ShouldShort/GoShort/UpdatePosition/After/Terminate` — this is the exact,
  unmodified interface this spec's adapter must implement. No changes to this file are needed or permitted.
- `go.mod` has `github.com/golang/protobuf` and `google.golang.org/protobuf` as **indirect** dependencies
  already (pulled in transitively by something else in the dependency graph) but **no** `google.golang.org/grpc`
  direct or indirect dependency exists — gRPC itself is a genuinely new dependency for this phase, confirmed
  by grep across `go.mod`.
- `app/engine/engine.go`'s `Run` method (Phase 1, extended in Phase 3's backend-04 spec with
  `strategy_panics_total`) already wraps the entire hook sequence in a `recover()` block that converts any
  panic from **any** `Strategy` implementation into a returned error, a `strategy.error` webhook (with a
  `"panic": true` payload marker per Phase 3 backend-04), and an incremented `strategy_panics_total{strategy}`
  counter — this mechanism is implementation-agnostic: it doesn't know or care whether the panicking
  `Strategy` is a native Go struct (Grid/Bollinger/Scalping) or a gRPC-delegating one. This is exactly the
  existing mechanism this spec's error handling reuses, per the coordinator's explicit instruction.
- No `internal/grpc/`, `app/strategies/mlgrpc/`, or `*.proto` file exists anywhere (fully greenfield).

## Target Behavior

```protobuf
// internal/grpc/strategy.proto (NEW)
syntax = "proto3";
package strategy;
option go_package = "go-trade-bot/internal/grpc/strategypb";

// Context mirrors app/strategies.Context's fields exactly (frozen shape,
// ADR-005) - this proto message is a wire-format transcription, not a
// redesign. Candles/Position/Account are flattened to proto-friendly shapes;
// Config (map[string]interface{} in Go) is passed as a JSON-encoded string
// field, since arbitrary interface{} values have no natural proto mapping
// and the strategy config JSON is already a JSON blob at rest
// (StrategyConfiguration.Configuration) - no new serialization format is
// introduced, just passed through.
message Candle {
    string symbol = 1;
    string timeframe = 2;
    int64 open_time_unix = 3;
    double open = 4;
    double high = 5;
    double low = 6;
    double close = 7;
    double volume = 8;
}

message Position {
    string symbol = 1;
    double entry_price = 2;
    double quantity = 3;
    optional double stop_loss_price = 4;
    string stop_loss_order_id = 5;
    int64 opened_at_unix = 6;
}

message StrategyContext {
    repeated Candle candles = 1;
    optional Position position = 2;
    double account_available = 3;
    string config_json = 4;      // JSON-encoded map[string]interface{}
    double price = 5;
    string timeframe = 6;
    string symbol = 7;
    string mode = 8;             // ExecutionMode.String() - "backtest"|"dryrun"|"paper"|"live"
}

message Order { double qty = 1; double price = 2; }

message Signal {
    optional Order buy = 1;
    optional Order sell = 2;
    optional Order stop_loss = 3;
    optional Order take_profit = 4;
}

message BoolResponse { bool value = 1; }
message SignalResponse { Signal signal = 1; }
message NullableSignalResponse { optional Signal signal = 1; } // null => "hold" (UpdatePosition's nil contract)
message Empty {}

// One RPC per Strategy hook - a direct 1:1 mapping, not a generic
// "call method X" RPC, so the .proto file itself documents the exact
// frozen contract a Python implementation must satisfy.
service MLStrategy {
    rpc Name(Empty) returns (NameResponse);
    rpc Before(StrategyContext) returns (Empty);
    rpc ShouldLong(StrategyContext) returns (BoolResponse);
    rpc GoLong(StrategyContext) returns (SignalResponse);
    rpc ShouldShort(StrategyContext) returns (BoolResponse);
    rpc GoShort(StrategyContext) returns (SignalResponse);
    rpc UpdatePosition(StrategyContext) returns (NullableSignalResponse);
    rpc After(StrategyContext) returns (Empty);
    rpc Terminate(StrategyContext) returns (Empty);
}
message NameResponse { string name = 1; }
```

```go
// app/strategies/mlgrpc/strategy.go (NEW)
package mlgrpc

import (
    "context"
    "time"

    "go-trade-bot/app/strategies"
    pb "go-trade-bot/internal/grpc/strategypb"
)

// MLGrpcStrategy implements strategies.Strategy by translating each hook
// call into a synchronous gRPC round-trip against an external process. It
// does NOT implement its own error recovery - any RPC failure (unreachable,
// timeout, non-OK status) causes the method to PANIC, deliberately, so
// app/engine.Engine's existing recover()+strategy_panics_total+webhook
// mechanism (Phase 1/Phase 3 backend-04) handles it identically to a native
// Go strategy bug. This is the spec's central error-handling decision - see
// Acceptance Criteria for what this means concretely.
type MLGrpcStrategy struct {
    client  pb.MLStrategyClient
    timeout time.Duration // per-call deadline, e.g. 2s - see AC#3
}

func NewMLGrpcStrategy(client pb.MLStrategyClient, timeout time.Duration) *MLGrpcStrategy

func (s *MLGrpcStrategy) Name() string
func (s *MLGrpcStrategy) Before(ctx strategies.Context)
func (s *MLGrpcStrategy) ShouldLong(ctx strategies.Context) bool
func (s *MLGrpcStrategy) GoLong(ctx strategies.Context) strategies.Signal
func (s *MLGrpcStrategy) ShouldShort(ctx strategies.Context) bool
func (s *MLGrpcStrategy) GoShort(ctx strategies.Context) strategies.Signal
func (s *MLGrpcStrategy) UpdatePosition(ctx strategies.Context) *strategies.Signal
func (s *MLGrpcStrategy) After(ctx strategies.Context)
func (s *MLGrpcStrategy) Terminate(ctx strategies.Context)
```

Each method: (1) converts `strategies.Context` to `pb.StrategyContext` (a pure data-mapping function,
`toProtoContext`, shared across all nine methods), (2) calls the corresponding RPC with a
`context.WithTimeout(parentCtx, s.timeout)`, (3) on any error (`err != nil` from the gRPC call, including
`context.DeadlineExceeded`), **panics** with a descriptive message
(`fmt.Sprintf("mlgrpc: %s RPC failed: %v", "GoLong", err)`) rather than returning a zero-value/safe default —
this is the explicit design choice named in Target Behavior above.

`Register`ation follows the exact Phase 1 self-registration pattern (Spec 06): `app/strategies/mlgrpc/strategy.go`'s
`init()` calls `strategies.Register("mlgrpc_<model_name>", factory)` — though for Phase 4's dummy-echo-server
scope, a single fixed registration name (`"mlgrpc_dummy"`) is sufficient; a real deployment would need one
registered instance per configured Python model endpoint, which is out of scope here (see Out of Scope).

## Acceptance Criteria

1. **Given** a running Python gRPC echo server implementing the `MLStrategy` service (returning fixed/dummy
   responses — `ShouldLong` always `false`, `Name` returns a fixed string, etc.), **when** `MLGrpcStrategy`'s
   methods are called against it through the full `Engine.Run` hook sequence, **then** each RPC round-trips
   successfully and the engine completes a normal cycle exactly as it would for any native Go strategy —
   this is the PRD's literal Phase 4 success signal, and this spec's primary acceptance target.
2. **Given** the Python server is unreachable (not started, wrong address), **when** `ShouldLong` is called,
   **then** `MLGrpcStrategy.ShouldLong` panics, `Engine.Run`'s existing recover() block catches it,
   `strategy_panics_total{strategy="mlgrpc_dummy"}` increments, and a `strategy.error` webhook fires with
   `Data["panic"] == true` — verifying zero new error-handling code was needed in `Engine`, only reuse.
3. **Given** the Python server accepts the connection but hangs (never responds) on `GoLong`, **when** the
   configured `timeout` (e.g. 2s) elapses, **then** the call fails with `context.DeadlineExceeded`, which
   is treated identically to Acceptance Criterion #2's unreachable case (panic, same downstream handling) —
   a hung strategy must not stall the entire strategy-cycle asynq task indefinitely.
4. **Given** `strategies.Context.Config` contains a `map[string]interface{}` with nested structures (matching
   a real strategy's parsed JSON config), **when** `toProtoContext` serializes it to `config_json`, **then**
   the receiving Python side can `json.loads()` it back to an equivalent structure — round-trip fidelity
   through the JSON-string escape hatch, not a lossy conversion.
5. **Given** `UpdatePosition`'s RPC returns `NullableSignalResponse{signal: null}` (proto's explicit
   optional-unset, not an empty `Signal{}`), **when** `MLGrpcStrategy.UpdatePosition` processes the
   response, **then** it returns Go `nil` — preserving the frozen interface's "nil means hold" contract
   (Phase 1 Spec 05) exactly, not conflating "no signal" with "an empty signal."
6. **Edge case — partial context serialization failure**: **given** `Context.Position` is `nil` (no open
   position — the common case for `ShouldLong`/`GoLong` calls), **when** `toProtoContext` runs, **then**
   `StrategyContext.position` is left unset (proto3 `optional`, not a zero-valued `Position{}` message) —
   the Python side must be able to distinguish "no open position" from "a position at price/qty zero,"
   which a zero-valued message would incorrectly conflate.
7. **Edge case — `Name()` RPC failure**: **given** even the simplest RPC (`Name`, which the registry might
   call at startup/registration time rather than per-cycle) fails, **when** this occurs, **then** it panics
   identically to any other method (Target Behavior's blanket rule has no special case for "safer" hooks) —
   consistency of the error-handling rule across all nine methods is itself a property worth testing, not
   just spot-checking the trading-critical hooks.

## Out of Scope

- Any real ML model or trained strategy logic — explicitly deferred per PRD §8 ("implement the Go client
  when the Python model exists"); this spec's Python side is a dummy echo server only.
- Multiple simultaneous gRPC-backed strategies, connection pooling, or per-model endpoint configuration —
  Phase 4's scope is proving the mechanism works once, not productionizing a multi-model deployment.
- Retry logic for a failed RPC call — deliberately absent; a single failure panics immediately (Target
  Behavior), consistent with treating any ML-strategy unavailability as equivalent to a native strategy bug,
  which Phase 1's engine already doesn't retry within a cycle (the next scheduled cycle is the retry,
  exactly as for any other strategy error).
- TLS/authentication on the gRPC channel — a homelab-local process-to-process call (both sides on the same
  host/Docker network) does not need transport security for this phase's dummy-echo-server scope; revisit
  if the Python process ever runs on a separate untrusted host.
- Streaming RPCs (e.g. a persistent bidirectional stream instead of one round-trip per hook call) — simple
  unary RPCs per hook are sufficient for Phase 4's scope and keep the proto/adapter mapping direct and
  auditable against the frozen `Strategy` interface.

## Dependencies

- Phase 1's `app/strategies/interface.go` (unmodified — the whole point of this spec), `app/strategies/registry.go`
  (`Register`/`init()` pattern reused), `app/engine/engine.go`'s panic-recovery (Phase 1, extended in Phase 3
  backend-04) — reused entirely unmodified.
- New dependency: `google.golang.org/grpc` (direct), plus generated code from `internal/grpc/strategy.proto`
  via `protoc`/`buf` (build-time tooling, not a new runtime dependency beyond the grpc/protobuf libraries
  themselves).
- **Judgment call**: "panic on any RPC failure" (rather than, say, returning `false`/`nil` safe defaults and
  logging a warning) is this spec's central and most consequential decision, made specifically per the
  coordinator's instruction to reuse Phase 3's recovery mechanism rather than invent a new one. The
  trade-off: an ML strategy that's merely slow (but not actually wrong) under real-world network jitter will
  register as a "panic" in metrics/webhooks alongside genuine bugs, which is coarser than a
  dedicated "ML unavailable" signal might be — accepted here as the right simplicity trade-off for a
  dummy-echo-server-scoped phase, flagged for revisit once a real model is integrated and this distinction
  might start to matter operationally.
