// Package mlgrpc implements the strategies.Strategy interface (frozen,
// ADR-005 - unmodified by this package) by delegating every hook call to an
// external gRPC process over internal/grpc/strategy.proto's MLStrategy
// service. This is Phase 4's proof that Python ML strategies can plug into
// the engine without touching Go engine code - PRD's literal success signal
// is a dummy echo server round-trip, not a real model integration (see
// internal/grpc/testdata/echo_server.py for a manual/documentation-only
// reference implementation).
package mlgrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	pb "go-trade-bot/internal/grpc/strategypb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// defaultDummyAddr / defaultTimeout back the "mlgrpc_dummy" registry
	// entry (init(), below) - overridable via MLGRPC_DUMMY_ADDR for local
	// testing against internal/grpc/testdata/echo_server.py.
	defaultDummyAddr = "localhost:50051"
	defaultTimeout   = 2 * time.Second
)

// MLGrpcStrategy implements strategies.Strategy by translating each hook
// call into a synchronous gRPC round-trip against an external process. It
// does NOT implement its own error recovery - any RPC failure (unreachable,
// timeout, non-OK status) causes the method to PANIC, deliberately, so
// app/engine.Engine's existing recover()+strategy_panics_total+webhook
// mechanism handles it identically to a native Go strategy bug. This is the
// spec's central error-handling decision: an ML strategy that's merely slow
// under real-world network jitter registers as a "panic" alongside genuine
// bugs, which is the accepted simplicity trade-off for this dummy-echo-
// server-scoped phase.
type MLGrpcStrategy struct {
	client  pb.MLStrategyClient
	timeout time.Duration // per-call deadline, e.g. 2s
}

// NewMLGrpcStrategy builds an adapter around an already-constructed
// pb.MLStrategyClient. timeout <= 0 defaults to defaultTimeout.
func NewMLGrpcStrategy(client pb.MLStrategyClient, timeout time.Duration) *MLGrpcStrategy {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &MLGrpcStrategy{client: client, timeout: timeout}
}

// init self-registers under the Phase 1 registry pattern (Spec 06): a
// single fixed name is sufficient for Phase 4's dummy-echo-server scope - a
// real deployment would need one registered instance per configured Python
// model endpoint, which is explicitly out of scope here. The gRPC target
// address is read from MLGRPC_DUMMY_ADDR (defaulting to localhost:50051)
// since the registry's StrategyFactory signature (func() Strategy) takes no
// arguments, so per-strategy configuration can only come from the process
// environment, not from a caller-supplied parameter at Get() time.
//
// grpc.NewClient does not dial eagerly - it never blocks or errors here even
// if the target process isn't running; connection attempts happen lazily on
// the first RPC call, which is exactly what the "panic on any RPC failure"
// design (see MLGrpcStrategy's doc comment) expects: an unreachable server
// panics on the first hook call, not at registration/startup time.
func init() {
	strategies.Register("mlgrpc_dummy", func(_ entities.Strategy) strategies.Strategy {
		addr := os.Getenv("MLGRPC_DUMMY_ADDR")
		if addr == "" {
			addr = defaultDummyAddr
		}

		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			// A dial-configuration error (e.g. malformed address) is
			// distinct from an RPC-time failure - this is the one place
			// this package panics for a reason OTHER than an RPC call
			// failing, since there is no hook context to defer it to.
			panic(fmt.Sprintf("mlgrpc: failed to construct client for %q: %v", addr, err))
		}

		return NewMLGrpcStrategy(pb.NewMLStrategyClient(conn), defaultTimeout)
	})
}

func (s *MLGrpcStrategy) Name() string {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	resp, err := s.client.Name(ctx, &pb.Empty{})
	if err != nil {
		panicRPCFailure("Name", err)
	}
	return resp.GetName()
}

func (s *MLGrpcStrategy) Before(ctx strategies.Context) {
	rpcCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	if _, err := s.client.Before(rpcCtx, toProtoContext(ctx)); err != nil {
		panicRPCFailure("Before", err)
	}
}

func (s *MLGrpcStrategy) ShouldLong(ctx strategies.Context) bool {
	rpcCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	resp, err := s.client.ShouldLong(rpcCtx, toProtoContext(ctx))
	if err != nil {
		panicRPCFailure("ShouldLong", err)
	}
	return resp.GetValue()
}

func (s *MLGrpcStrategy) GoLong(ctx strategies.Context) strategies.Signal {
	rpcCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	resp, err := s.client.GoLong(rpcCtx, toProtoContext(ctx))
	if err != nil {
		panicRPCFailure("GoLong", err)
	}
	return fromProtoSignal(resp.GetSignal())
}

func (s *MLGrpcStrategy) ShouldShort(ctx strategies.Context) bool {
	rpcCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	resp, err := s.client.ShouldShort(rpcCtx, toProtoContext(ctx))
	if err != nil {
		panicRPCFailure("ShouldShort", err)
	}
	return resp.GetValue()
}

func (s *MLGrpcStrategy) GoShort(ctx strategies.Context) strategies.Signal {
	rpcCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	resp, err := s.client.GoShort(rpcCtx, toProtoContext(ctx))
	if err != nil {
		panicRPCFailure("GoShort", err)
	}
	return fromProtoSignal(resp.GetSignal())
}

// UpdatePosition preserves the frozen interface's "nil means hold" contract
// exactly: NullableSignalResponse.Signal is nil (proto3 explicit optional-
// unset, not an empty *pb.Signal) when the Python side declines to signal,
// and that nil is propagated as a Go nil, not conflated with an empty
// strategies.Signal{}.
func (s *MLGrpcStrategy) UpdatePosition(ctx strategies.Context) *strategies.Signal {
	rpcCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	resp, err := s.client.UpdatePosition(rpcCtx, toProtoContext(ctx))
	if err != nil {
		panicRPCFailure("UpdatePosition", err)
	}

	if resp.GetSignal() == nil {
		return nil
	}
	sig := fromProtoSignal(resp.GetSignal())
	return &sig
}

func (s *MLGrpcStrategy) After(ctx strategies.Context) {
	rpcCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	if _, err := s.client.After(rpcCtx, toProtoContext(ctx)); err != nil {
		panicRPCFailure("After", err)
	}
}

func (s *MLGrpcStrategy) Terminate(ctx strategies.Context) {
	rpcCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	if _, err := s.client.Terminate(rpcCtx, toProtoContext(ctx)); err != nil {
		panicRPCFailure("Terminate", err)
	}
}

// panicRPCFailure is the single choke point for this package's "panic on
// any RPC failure" rule (including context.DeadlineExceeded from the
// s.timeout deadline) - every one of the nine Strategy methods routes
// through here so the panic message format and the rule itself never drift
// between methods.
func panicRPCFailure(rpc string, err error) {
	panic(fmt.Sprintf("mlgrpc: %s RPC failed: %v", rpc, err))
}

// toProtoContext converts strategies.Context to pb.StrategyContext - a pure
// data-mapping function shared across all nine hook methods above.
func toProtoContext(ctx strategies.Context) *pb.StrategyContext {
	candles := make([]*pb.Candle, len(ctx.Candles))
	for i, c := range ctx.Candles {
		candles[i] = &pb.Candle{
			Symbol:       c.Symbol,
			Timeframe:    c.Timeframe,
			OpenTimeUnix: c.OpenTime.Unix(),
			Open:         c.Open,
			High:         c.High,
			Low:          c.Low,
			Close:        c.Close,
			Volume:       c.Volume,
		}
	}

	// configJson defaults to "{}" (not "" / omitted) so the Python side can
	// always json.loads() it unconditionally, even when Context.Config is
	// nil - an empty object round-trips cleanly, an empty string does not.
	configJSON := "{}"
	if len(ctx.Config) > 0 {
		if b, err := json.Marshal(ctx.Config); err == nil {
			configJSON = string(b)
		}
	}

	pbCtx := &pb.StrategyContext{
		Candles:          candles,
		AccountAvailable: ctx.Account.Available,
		ConfigJson:       configJSON,
		Price:            ctx.Price,
		Timeframe:        ctx.Timeframe,
		Symbol:           ctx.Symbol,
		Mode:             ctx.Mode.String(),
	}

	// Edge case (AC#6): a nil Context.Position must leave pbCtx.Position
	// unset (nil), not a zero-valued *pb.Position{} - a zero-valued message
	// would incorrectly read on the Python side as "a position at
	// price/qty zero" rather than "no open position".
	if ctx.Position != nil {
		pbCtx.Position = &pb.Position{
			Symbol:          ctx.Position.Symbol,
			EntryPrice:      ctx.Position.EntryPrice,
			Quantity:        ctx.Position.Quantity,
			StopLossPrice:   ctx.Position.StopLossPrice,
			StopLossOrderId: ctx.Position.StopLossOrderID,
			OpenedAtUnix:    ctx.Position.OpenedAt.Unix(),
		}
	}

	return pbCtx
}

func fromProtoSignal(s *pb.Signal) strategies.Signal {
	if s == nil {
		return strategies.Signal{}
	}
	return strategies.Signal{
		Buy:        fromProtoOrder(s.GetBuy()),
		Sell:       fromProtoOrder(s.GetSell()),
		StopLoss:   fromProtoOrder(s.GetStopLoss()),
		TakeProfit: fromProtoOrder(s.GetTakeProfit()),
	}
}

func fromProtoOrder(o *pb.Order) *strategies.Order {
	if o == nil {
		return nil
	}
	return &strategies.Order{Qty: o.GetQty(), Price: o.GetPrice()}
}
