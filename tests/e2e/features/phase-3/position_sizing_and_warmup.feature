Feature: Position Sizing & Candle Warmup
  As a risk-managed trading engine
  I want configurable position sizing models and automatic candle warmup
  So that position sizes scale with risk and technical indicators are fully primed on startup

  Background:
    Given the database is clean

  @smoke @REQ-P3-SIZE-01
  Scenario: Fixed percentage position sizing allocates fixed equity fraction
    Given an account balance of 10000.0 USD
    And a strategy configured with sizing strategy "fixed_percentage" at 10.0%
    When position sizing is calculated for entry price 5000.0
    Then the target order position value should be 1000.0 USD

  @smoke @REQ-P3-SIZE-02
  Scenario: Risk-based position sizing calculates position based on stop-loss distance
    Given an account balance of 10000.0 USD
    And a strategy configured with sizing strategy "risk_based" risking 2.0% of capital
    And entry price 50.0 with stop loss price 45.0
    When position sizing is calculated
    Then the maximum risk amount should be 200.0 USD
    And the calculated order quantity should be 40.0 units

  @smoke @REQ-P3-SIZE-03
  Scenario: Volatility ATR position sizing scales inversely with market volatility
    Given an account balance of 10000.0 USD
    And a strategy configured with sizing strategy "volatility_atr"
    When market volatility ATR increases from 2.0 to 4.0
    Then the calculated position quantity should decrease by 50.0%

  @smoke @REQ-P3-WARMUP-01
  Scenario: Automatic candle warmup preloads historical candles on strategy boot
    Given 50 historical candles exist in database for "BTCUSDT" "1h"
    When the strategy engine initializes for "BTCUSDT" with warmup requirement 30 candles
    Then the strategy Context should be primed with 30 historical candles before first execution
