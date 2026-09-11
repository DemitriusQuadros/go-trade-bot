Feature: gRPC ML Strategy Integration
  As an algorithmic trader
  I want to delegate strategy evaluation to an external ML model over gRPC
  So that I can leverage advanced models without embedding Python inside the Go trading engine

  Background:
    Given the database is clean
    And a mock gRPC ML strategy server is available

  @smoke @REQ-GRPC-001
  Scenario: Successfully delegate strategy lifecycle hooks to gRPC ML model
    Given a "grpc_ml" strategy is configured targeting the mock gRPC server
    When the strategy engine executes a cycle with current price 50000.0
    Then the gRPC server should receive the strategy context with market candles
    And the strategy should evaluate a "BUY" signal returned from the gRPC model

  @error @REQ-GRPC-002
  Scenario: Gracefully recover when gRPC ML model fails or times out
    Given the mock gRPC ML server is returning unavailable errors
    When the strategy engine executes a cycle
    Then the engine should recover from the gRPC failure
    And a strategy error event with panic flag should be recorded
