Feature: Hyperparameter Optimization, Monte Carlo & Performance History
  As an advanced quantitative trading platform
  I want hyperparameter optimization, Monte Carlo risk simulation, and performance snapshots
  So that optimal strategy parameters are discovered, tail risk is quantified, and performance history is tracked

  Background:
    Given the database is clean
    And the API server is running

  @smoke @REQ-P4-OPT-01
  Scenario: Launch hyperparameter grid search optimization via POST /api/optimize
    When I send a POST request to "/api/optimize" with body:
      """
      {
        "strategy_id": 1,
        "symbol": "BTCUSDT",
        "timeframe": "1h",
        "start_date": "2026-01-01T00:00:00Z",
        "end_date": "2026-02-01T00:00:00Z",
        "initial_capital": 10000.0,
        "param_grid": {
          "grid_levels": [4, 6, 8],
          "grid_spacing_pct": [0.5, 1.0, 1.5]
        }
      }
      """
    Then the response status code should be 202
    And the response body should contain '"status":"pending"'
    And the database should contain an optimization_run record for strategy ID 1

  @smoke @REQ-P4-OPT-02
  Scenario: Retrieve optimization run status via GET /api/optimize/{id}
    Given a persisted optimization_run record with ID 50 for strategy ID 1 with status "completed"
    When I send a GET request to "/api/optimize/50"
    Then the response status code should be 200
    And the response body should contain '"status":"completed"'

  @smoke @REQ-P4-MC-01
  Scenario: Monte Carlo Simulation evaluates trade resampling equity distribution
    Given a trade log with 20 historical trade return percentages
    When Monte Carlo simulation runs with 1000 resampled iterations
    Then the 5th percentile, 50th percentile, and 95th percentile drawdowns should be computed

  @smoke @REQ-P4-SNAP-01
  Scenario: Periodic strategy performance snapshot records daily metrics
    Given an active strategy with ID 1
    When a daily performance snapshot job executes for strategy ID 1
    Then a strategy_performance_snapshot record should be persisted with bucket "daily"
