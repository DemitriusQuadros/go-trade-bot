Feature: Backtest REST API & Persistence
  As an API consumer
  I want HTTP endpoints to launch backtests, run walk-forward validation, and query historical results
  So that backtests can be triggered programmatically and results are persisted in the database

  Background:
    Given the database is clean
    And a strategy exists in the database for symbol "BTCUSDT"

  @smoke @REQ-API-01
  Scenario: Launch backtest via REST API POST /api/backtest
    When I send a POST request to "/api/backtest" with body:
      """
      {
        "strategy_id": 1,
        "symbol": "BTCUSDT",
        "start_date": "2026-01-01T00:00:00Z",
        "end_date": "2026-01-10T00:00:00Z"
      }
      """
    Then the response status code should be 200
    And the response body should contain '"symbol":"BTCUSDT"'
    And the database should contain a backtest_run record for strategy ID 1

  @smoke @REQ-API-02
  Scenario: Launch walk-forward validation via REST API POST /api/backtest/walkforward
    When I send a POST request to "/api/backtest/walkforward" with body:
      """
      {
        "strategy_id": 1,
        "symbol": "BTCUSDT",
        "start_date": "2026-01-01T00:00:00Z",
        "end_date": "2026-02-01T00:00:00Z"
      }
      """
    Then the response status code should be 200
    And the response body should contain '"is_walk_forward":true'

  @smoke @REQ-API-03
  Scenario: Retrieve backtest run by ID via GET /api/backtest/{id}
    Given a persisted backtest_run record with ID 100 for strategy ID 1
    When I send a GET request to "/api/backtest/100"
    Then the response status code should be 200
    And the response body should contain '"id":100'

  @smoke @REQ-API-04
  Scenario: List backtest runs by strategy ID via GET /api/backtest?strategy_id={id}
    Given 2 persisted backtest_run records exist for strategy ID 1
    When I send a GET request to "/api/backtest?strategy_id=1"
    Then the response status code should be 200
    And the response body should contain 2 backtest run items
