Feature: Strategy Management & Safety
  As a trading system administrator
  I want to create, configure, and manage trading strategies safely
  So that invalid or unsafe strategies are prevented from executing

  Background:
    Given the database is clean
    And the API server is running

  @smoke @REQ-STRAT-01
  Scenario: Successfully create a valid trading strategy via API
    When I send a POST request to "/api/strategies" with body:
      """
      {
        "symbol": "BTCUSDT",
        "algorithm": "grid",
        "status": "testing",
        "configuration": {
          "cycle": 1,
          "grid_levels": 5,
          "grid_spacing_pct": 1.0,
          "volume_filter": 0.0,
          "stop_loss_pct": 2.0,
          "take_profit_pct": 1.5,
          "rsi_period": 14,
          "rsi_buy_threshold": 30.0,
          "rsi_sell_threshold": 70.0
        }
      }
      """
    Then the response status code should be 201
    And the response body should contain '"algorithm":"grid"'
    And the database should contain a strategy for symbol "BTCUSDT" with algorithm "grid"

  @error @REQ-STRAT-01
  Scenario: Reject creation of strategy with unregistered algorithm
    When I send a POST request to "/api/strategies" with body:
      """
      {
        "symbol": "ETHUSDT",
        "algorithm": "unknown_algo",
        "status": "testing",
        "configuration": {
          "cycle": 1
        }
      }
      """
    Then the response status code should be 400
    And the database should not contain a strategy for symbol "ETHUSDT"

  @smoke @REQ-STRAT-02
  Scenario: Prevent starting a strategy in live mode without live confirmation
    Given the application process mode is "paper"
    When I attempt to configure process mode to "live" without "--confirm-live"
    Then the mode guard validation should fail with a startup error

  @edge @REQ-STRAT-03
  Scenario: Prevent configuring live execution mode with testnet enabled
    Given the strategy execution mode is configured as "live"
    And the exchange configuration has "Testnet" set to true
    When the exchange client initialization runs
    Then startup should fail with a configuration conflict error
