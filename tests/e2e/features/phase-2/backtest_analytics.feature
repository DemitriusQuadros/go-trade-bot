Feature: Performance Metrics & Analytics
  As a quantitative trader
  I want quantitative performance metrics, walk-forward validation, and HTML reports
  So that I can evaluate risk-adjusted returns and prevent strategy overfitting

  Background:
    Given the database is clean

  @smoke @REQ-METRIC-01
  Scenario: Calculate quantitative performance metrics from backtest trades
    Given a completed backtest trade log with 10 profitable trades and 5 losing trades
    When the metrics provider calculates performance analytics
    Then the Sharpe ratio should be calculated
    And the Win Rate percentage should be 66.67%
    And the Profit Factor should be greater than 1.0
    And the Max Drawdown percentage should be computed

  @smoke @REQ-METRIC-02
  Scenario: Walk-Forward Validation splits data into in-sample and out-of-sample windows
    Given historical candle data spanning 60 days for "BTCUSDT"
    When Walk-Forward Validation runs with 4 in-sample and out-of-sample window splits
    Then in-sample strategy parameters should be tested against out-of-sample windows
    And an overall walk-forward pass or fail verdict should be returned

  @smoke @REQ-METRIC-03
  Scenario: Generate standalone HTML report upon backtest completion
    Given a completed backtest run for strategy ID 1
    When the HTML report generator builds the report artifact
    Then a standalone HTML file should be created at the output path
    And the HTML content should include summary table and trade metrics
