Feature: Optimization Run Pre-Flight Candle Validation
  As a quantitative researcher
  I want optimization requests pre-validated for candle data presence before job creation
  So that optimization grid searches are rejected early when no candles exist in the requested date range

  @REQ-FIRST-RUN-BE-02
  Scenario: Optimization request with no candle data in date range returns 400 Bad Request
    Given the database is clean
    And a strategy with ID 1 exists with name "My Strategy" and strategy_name "template"
    When I send a POST request to "/api/optimize" with body:
      """
      {
        "strategy_id": 1,
        "symbol": "EMPTY_SYMBOL",
        "timeframe": "1m",
        "start_date": "2024-01-01T00:00:00Z",
        "end_date": "2024-01-02T00:00:00Z",
        "param_grid": {
          "period": [10, 20]
        }
      }
      """
    Then the response status code should be 400
    And the response body should contain "no candle data available"
    And no optimization run should be created in database for symbol "EMPTY_SYMBOL"

  @REQ-FIRST-RUN-BE-02
  Scenario: Optimization request succeeds when candle data exists in date range
    Given the database is clean
    And a strategy with ID 1 exists with name "My Strategy" and strategy_name "template"
    And historical candles exist for symbol "BTCUSDT" and timeframe "1m" in date range "2024-01-01T00:00:00Z" to "2024-01-02T00:00:00Z"
    When I send a POST request to "/api/optimize" with body:
      """
      {
        "strategy_id": 1,
        "symbol": "BTCUSDT",
        "timeframe": "1m",
        "start_date": "2024-01-01T00:00:00Z",
        "end_date": "2024-01-02T00:00:00Z",
        "param_grid": {
          "period": [10, 20]
        }
      }
      """
    Then the response status code should be 202
    And the response body should contain '"status":"pending"'
