Feature: Trading Strategy Execution & Indicators
  As an automated trading engine
  I want to evaluate strategy algorithms using technical indicators
  So that accurate buy and sell signals are generated based on market conditions

  Background:
    Given the database is clean
    And the technical indicators provider is initialized

  @smoke @REQ-ALGO-01
  Scenario: Grid Strategy builds levels and generates buy signal when price hits lower level
    Given a "grid" strategy is initialized with configuration:
      | grid_levels      | 4    |
      | grid_spacing_pct | 1.0  |
      | rsi_period       | 14   |
      | rsi_buy_threshold| 40.0 |
    When the feed delivers candles with latest close 50000.0 and RSI 35.0
    Then the strategy grid levels should be constructed around price 50000.0
    When the next candle close drops to 49500.0
    Then the strategy should evaluate a "BUY" signal at price 49500.0

  @smoke @REQ-ALGO-02
  Scenario: Bollinger Bands Strategy generates buy signal on lower band cross
    Given a "bollinger" strategy is initialized with period 20 and stdDev 2.0
    When historical candle closes are delivered to the indicator provider
    And the lower band is calculated at 48000.0 and upper band at 52000.0
    When a candle closes at 47900.0
    Then the strategy should evaluate a "BUY" signal

  @smoke @REQ-ALGO-03
  Scenario: Scalping Strategy evaluates EMA crossover for buy signal
    Given a "scalping" strategy is initialized with fast EMA 9 and slow EMA 21
    When market candles result in fast EMA crossing above slow EMA
    And RSI is below overbought threshold 70.0
    Then the strategy should evaluate a "BUY" signal
