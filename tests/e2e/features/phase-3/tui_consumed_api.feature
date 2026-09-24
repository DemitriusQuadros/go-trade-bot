Feature: TUI Consumed API Endpoints
  As a TUI console interface
  I want dedicated HTTP REST API endpoints
  So that open signals, account totals, and strategy performance metrics can be rendered efficiently

  Background:
    Given the database is clean
    And the API server is running

  @smoke @REQ-P3-API-01
  Scenario: Query open signals via GET /api/signals/open
    Given an open signal exists in the database for "BTCUSDT"
    When I send a GET request to "/api/signals/open"
    Then the response status code should be 200
    And the response body should contain '"symbol":"BTCUSDT"'
    And the response body should contain '"status":"open"'

  @smoke @REQ-P3-API-02
  Scenario: Query account summary via GET /api/account
    Given an account record exists with total balance 5000.0 USD
    When I send a GET request to "/api/account"
    Then the response status code should be 200
    And the response body should contain balance 5000.0

  @smoke @REQ-P3-API-03
  Scenario: Query strategy performance metrics via GET /api/strategies/1/performance
    Given a strategy with ID 1 exists with recorded performance win rate 75.0%
    When I send a GET request to "/api/strategies/1/performance"
    Then the response status code should be 200
    And the response body should contain '"win_rate_pct":75'
