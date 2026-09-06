Feature: Hyperparameter Optimization
  As a trading researcher
  I want to optimize strategy hyperparameters over parameter ranges
  So that I can identify the most robust configuration without manually running individual backtests

  Background:
    Given the database is clean
    And the API server is running
    And a strategy exists in the database with id 1 for symbol "BTCUSDT"

  @smoke @REQ-OPT-001
  Scenario: Successfully enqueue a hyperparameter optimization grid search
    When I send a POST request to "/optimize" with body:
      """
      {
        "strategy_id": 1,
        "symbol": "BTCUSDT",
        "timeframe": "15m",
        "start_date": "2026-01-01T00:00:00Z",
        "end_date": "2026-01-31T00:00:00Z",
        "initial_capital": 1000.0,
        "param_grid": {
          "rsi_period": { "min": 10, "max": 14, "step": 2 },
          "stop_loss_pct": { "min": 1.0, "max": 3.0, "step": 1.0 }
        }
      }
      """
    Then the response status code should be 202
    And the response body should contain '"status":"pending"'
    And the response body should contain '"total_combinations":9'
    And the database should contain an optimization run for strategy 1 with status "pending"

  @error @REQ-OPT-002
  Scenario: Reject optimization request with non-positive step
    When I send a POST request to "/optimize" with body:
      """
      {
        "strategy_id": 1,
        "symbol": "BTCUSDT",
        "timeframe": "15m",
        "start_date": "2026-01-01T00:00:00Z",
        "end_date": "2026-01-31T00:00:00Z",
        "param_grid": {
          "rsi_period": { "min": 10, "max": 20, "step": 0 }
        }
      }
      """
    Then the response status code should be 400
    And the response body should contain "step must be positive"

  @error @REQ-OPT-003
  Scenario: Reject optimization request exceeding maximum combinations limit
    When I send a POST request to "/optimize" with body:
      """
      {
        "strategy_id": 1,
        "symbol": "BTCUSDT",
        "timeframe": "15m",
        "start_date": "2026-01-01T00:00:00Z",
        "end_date": "2026-01-31T00:00:00Z",
        "param_grid": {
          "param_a": { "min": 1, "max": 100, "step": 1 },
          "param_b": { "min": 1, "max": 100, "step": 1 }
        }
      }
      """
    Then the response status code should be 413
    And the response body should contain "grid size exceeds limit"

  @REQ-OPT-004
  Scenario: Poll optimization job progress
    Given an optimization run with id 5 exists in status "running" with progress 15 of 30
    When I send a GET request to "/optimize/5"
    Then the response status code should be 200
    And the response body should contain '"status":"running"'
    And the response body should contain '"progress":15'
    And the response body should contain '"total_combinations":30'

  @REQ-OPT-005
  Scenario: Retrieve completed optimization results grid
    Given an optimization run with id 6 exists in status "completed" with 4 grid combinations
    When I send a GET request to "/optimize/6/results"
    Then the response status code should be 200
    And the response body should contain '"best_config"'
    And the response body should contain '"grid"'

  @error @REQ-OPT-006
  Scenario: Conflict when requesting results of an in-progress optimization
    Given an optimization run with id 7 exists in status "running" with progress 5 of 20
    When I send a GET request to "/optimize/7/results"
    Then the response status code should be 409
    And the response body should contain "optimization is still running"

  @REQ-OPT-007
  Scenario: List optimization runs for a strategy
    Given an optimization run with id 8 exists for strategy 1
    When I send a GET request to "/optimize?strategy_id=1"
    Then the response status code should be 200
    And the response body should contain '"strategy_id":1'
