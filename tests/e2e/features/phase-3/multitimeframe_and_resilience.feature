Feature: Multi-Timeframe Feeds & Error Recovery Resilience
  As a resilient trading engine
  I want multi-timeframe candle distribution and strategy panic recovery
  So that strategies can analyze multiple timeframes and system crashes are prevented

  Background:
    Given the database is clean

  @smoke @REQ-P3-MTF-01
  Scenario: Multi-Timeframe feed delivers secondary timeframe candles to Context
    Given a strategy subscribed to primary timeframe "1m" and secondary timeframe "15m"
    When market candles are delivered for both timeframes
    Then the strategy Context should expose primary candles under "1m"
    And secondary candles under "15m" in CandlesByTimeframe

  @error @REQ-P3-RECOVERY-01
  Scenario: Engine recovers from strategy panic and triggers circuit breaker on repeated panics
    Given an active strategy for "ETHUSDT"
    When the strategy algorithm panics during cycle execution
    Then the engine should catch the panic safely without process crash
    And log the execution error
    When the strategy algorithm panics 3 consecutive times
    Then the strategy status should be automatically updated to "disabled"
