Feature: Risk Management & Stop-Loss Exchange Orders
  As a risk management system
  I want to maintain protective STOP_MARKET orders on the exchange
  So that positions are protected against adverse market movements even if the process restarts

  Background:
    Given the database is clean
    And the mock exchange server is active

  @smoke @REQ-RISK-01
  Scenario: Position open places automatic STOP_MARKET order on exchange
    Given an open strategy for "BTCUSDT" with stop loss percentage 2.0%
    When a BUY market order fills at price 50000.0 for quantity 0.1
    Then a "STOP_MARKET" sell order should be placed on the exchange with stop price 49000.0
    And the database order record should store the StopLossOrderID "EX-STOP-2001"

  @smoke @REQ-RISK-02
  Scenario: Position close cancels active resting stop-loss order before market sell
    Given an open signal for "BTCUSDT" with a resting stop-loss order "EX-STOP-2001"
    When a SELL signal is generated to close the position
    Then the system should call CancelOrder for "EX-STOP-2001" on the exchange first
    And subsequently submit a market SELL order to close the position
    And the database signal status should be updated to "Closed"

  @edge @REQ-RISK-03
  Scenario: Worker process restart reconciles unlinked resting stop-loss orders
    Given an open signal in the database for "ETHUSDT" with missing StopLossOrderID
    And the exchange has a active resting STOP_MARKET order for "ETHUSDT"
    When the worker process completes startup reconciliation
    Then the database order record should be updated with the matching exchange StopLossOrderID
