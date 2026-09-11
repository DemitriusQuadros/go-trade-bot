Feature: Monte Carlo Robustness Simulation
  As a risk manager
  I want to run Monte Carlo simulations on completed backtest trade logs
  So that I can assess the strategy's drawdowns and return distribution under trade order permutations

  Background:
    Given the database is clean
    And the API server is running

  @smoke @REQ-MC-001
  Scenario: Successfully run Monte Carlo simulation on a backtest run with trades
    Given a completed backtest run exists with id 10 and 15 trades
    When I send a POST request to "/backtest/10/montecarlo" with body:
      """
      {
        "iterations": 500
      }
      """
    Then the response status code should be 200
    And the response body should contain '"iterations":500'
    And the response body should contain '"sharpe_distribution"'
    And the response body should contain '"max_drawdown_distribution"'
    And the response body should contain '"total_return_distribution"'

  @error @REQ-MC-002
  Scenario: Reject Monte Carlo simulation on backtest run with fewer than 2 trades
    Given a completed backtest run exists with id 11 and 1 trades
    When I send a POST request to "/backtest/11/montecarlo" with body:
      """
      {
        "iterations": 500
      }
      """
    Then the response status code should be 422
    And the response body should contain "at least 2 trades required"

  @REQ-MC-003
  Scenario: Retrieve cached Monte Carlo simulation results
    Given a completed backtest run exists with id 12 with cached Monte Carlo results
    When I send a GET request to "/backtest/12/montecarlo"
    Then the response status code should be 200
    And the response body should contain '"iterations":1000'
    And the response body should contain '"sharpe_distribution"'
