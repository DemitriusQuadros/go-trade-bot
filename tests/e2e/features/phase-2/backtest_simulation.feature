Feature: Replay Feed & Backtest Simulation
  As a quantitative backtesting engine
  I want to stream historical market candles through strategy algorithms with simulated execution
  So that trading strategies can be evaluated accurately prior to live deployment

  Background:
    Given the database is clean

  @smoke @REQ-BT-01
  Scenario: ReplayFeed streams historical candles in chronological sequence
    Given 3 historical candles are stored in the database for "BTCUSDT" "1m"
    When a ReplayFeed is initialized for "BTCUSDT" "1m"
    Then calling Next on the ReplayFeed should yield 3 candles in open time order
    And the ReplayFeed should report exhausted when finished

  @smoke @REQ-BT-02
  Scenario: Backtest Engine runs strategy over historical feed and records equity curve
    Given historical candle data exists for "ETHUSDT" "5m"
    And a "grid" strategy is configured with initial capital 10000.0 USD
    When the backtest engine runs the strategy from start date "2026-01-01T00:00:00Z" to "2026-01-02T00:00:00Z"
    Then the backtest execution should complete successfully
    And the total trades count should be greater than or equal to 0
    And the final equity should be calculated

  @edge @REQ-BT-03
  Scenario: Simulator Parity enforces fee deduction and slippage on simulated fills
    Given a simulated exchange configured with fee rate 0.1% and slippage 0.05%
    When a BUY signal for 1.0 BTC at price 50000.0 is executed by the simulator
    Then the fill price should include slippage 50025.0
    And the total cost should include fee deduction 50.025 USD

  @smoke @REQ-BT-04
  Scenario: Dry-Run Mode evaluates signals in real-time without exchange order placement
    Given a strategy configured in execution mode "dryrun"
    When market candles are delivered in real-time
    Then simulated buy orders should be recorded in memory
    And no live exchange order placement requests should be dispatched
