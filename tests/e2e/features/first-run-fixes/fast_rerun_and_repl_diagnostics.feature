Feature: Fast-Rerun & REPL Candle Data Diagnostics
  As a script author
  I want fast-rerun and REPL endpoints to report candle data availability explicitly and support unsaved script previews
  So that I can test scripts before saving and distinguish zero-candle windows from zero-signal executions

  @REQ-FIRST-RUN-BE-01
  Scenario: Fast-rerun preview for an unsaved strategy with strategy_id 0 succeeds
    Given the database is clean
    And historical candles exist for symbol "BTCUSDT" and timeframe "1m"
    When I send a POST request to "/api/script/fast-rerun" with body:
      """
      {
        "strategy_id": 0,
        "symbol": "BTCUSDT",
        "timeframe": "1m",
        "source": "let rsi = ind.rsi(14); if (rsi < 30) { signal.buy(); }"
      }
      """
    Then the response status code should be 200
    And the response body should contain '"data_available":true'

  @REQ-FIRST-RUN-BE-01
  Scenario: Fast-rerun returns data_available false when no candles exist for requested symbol
    Given the database is clean
    When I send a POST request to "/api/script/fast-rerun" with body:
      """
      {
        "strategy_id": 0,
        "symbol": "UNKNOWN_PAIR",
        "timeframe": "1m",
        "source": "signal.buy();"
      }
      """
    Then the response status code should be 200
    And the response body should contain '"data_available":false'

  @REQ-FIRST-RUN-BE-01
  Scenario: Fast-rerun returns 404 for a non-zero strategy_id that does not exist in database
    Given the database is clean
    When I send a POST request to "/api/script/fast-rerun" with body:
      """
      {
        "strategy_id": 999,
        "symbol": "BTCUSDT",
        "timeframe": "1m",
        "source": "signal.buy();"
      }
      """
    Then the response status code should be 404

  @REQ-FIRST-RUN-BE-01
  Scenario: REPL evaluation returns data_available false when zero candles exist
    Given the database is clean
    When I send a POST request to "/api/script/repl" with body:
      """
      {
        "symbol": "UNKNOWN_PAIR",
        "timeframe": "1m",
        "source": "ind.rsi(14)"
      }
      """
    Then the response status code should be 200
    And the response body should contain '"data_available":false'
