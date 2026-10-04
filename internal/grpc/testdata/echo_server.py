#!/usr/bin/env python3
"""
Dummy echo server for internal/grpc/strategy.proto's MLStrategy service.

This is documentation / manual-verification tooling only - no Go test in
this repository starts or calls this process (per the project's testing
constraint: no test may make a real network call or a real gRPC call to an
unreachable/external process; app/strategies/mlgrpc/strategy_test.go mocks
pb.MLStrategyClient instead).

Its purpose is to let a maintainer manually confirm backend-04's literal
PRD success signal end-to-end: "gRPC adapter compiles and delegates a dummy
strategy call to a Python echo server."

Usage:
    pip install grpcio grpcio-tools
    python -m grpc_tools.protoc \
        -I. --python_out=. --grpc_python_out=. strategy.proto
    python echo_server.py
    # then, from the Go side:
    MLGRPC_DUMMY_ADDR=localhost:50051 go run ./cmd/worker

Every RPC returns a fixed, deterministic response - never touches real
market data or a real model - matching the Go adapter's dummy-echo-server
scope (Out of Scope: "Any real ML model or trained strategy logic").
"""

import json
from concurrent import futures

import grpc

# Generated from strategy.proto via the grpc_tools.protoc command above -
# not checked in here since generation is a documented manual step, not a
# build-time dependency of this repository's Go code.
import strategy_pb2
import strategy_pb2_grpc


class DummyMLStrategy(strategy_pb2_grpc.MLStrategyServicer):
    def Name(self, request, context):
        return strategy_pb2.NameResponse(name="mlgrpc_dummy_python_echo")

    def Before(self, request, context):
        print(f"[Before] symbol={request.symbol} price={request.price} "
              f"config={json.loads(request.config_json or '{}')}")
        return strategy_pb2.Empty()

    def ShouldLong(self, request, context):
        # Fixed/dummy response - never actually decides to trade.
        return strategy_pb2.BoolResponse(value=False)

    def GoLong(self, request, context):
        return strategy_pb2.SignalResponse(signal=strategy_pb2.Signal())

    def ShouldShort(self, request, context):
        return strategy_pb2.BoolResponse(value=False)

    def GoShort(self, request, context):
        return strategy_pb2.SignalResponse(signal=strategy_pb2.Signal())

    def UpdatePosition(self, request, context):
        # Explicit "no signal" (hold) - the unset-optional case the Go
        # adapter must translate back to a Go nil, not an empty Signal{}.
        return strategy_pb2.NullableSignalResponse()

    def After(self, request, context):
        return strategy_pb2.Empty()

    def Terminate(self, request, context):
        return strategy_pb2.Empty()


def serve(address: str = "0.0.0.0:50051") -> None:
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    strategy_pb2_grpc.add_MLStrategyServicer_to_server(DummyMLStrategy(), server)
    server.add_insecure_port(address)
    server.start()
    print(f"mlgrpc dummy echo server listening on {address}")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
