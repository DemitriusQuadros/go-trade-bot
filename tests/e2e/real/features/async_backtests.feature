Feature: Asynchronous backtests over the REST API
  Heavy backtests must not hold an HTTP request open. With "async": true the
  API stores a queued run, hands it to the worker queue and answers 202; the
  worker then fills the SAME run row.

  Background:
    Given a script strategy "Trader" exists

  Scenario: An async run is queued, then completed by the worker
    Given 10 days of BTCUSDT 1h candles exist from "2025-01-01"
    When I POST "/api/backtest" with JSON:
      """
      {"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","start_date":"2025-01-02T00:00:00Z","end_date":"2025-01-08T00:00:00Z","async":true}
      """
    Then the response status is 202
    And the response JSON "status" is "queued"
    And I remember the response id
    And the response header "Location" is "/api/backtest/{id}"
    And 1 backtest task is queued
    When I GET "/api/backtest/{id}"
    Then the response JSON "status" is "queued"
    When the worker processes the queue
    And I GET "/api/backtest/{id}"
    Then the response status is 200
    And the response JSON "status" is "done"
    And there is 1 backtest run stored

  Scenario: Without the async flag the run is still synchronous
    Given 10 days of BTCUSDT 1h candles exist from "2025-01-01"
    When I POST "/api/backtest" with JSON:
      """
      {"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","start_date":"2025-01-02T00:00:00Z","end_date":"2025-01-08T00:00:00Z"}
      """
    Then the response status is 200
    And the response JSON "status" is "done"
    And 0 backtest tasks are queued

  Scenario: A range with no candles is rejected before anything is queued
    Given 10 days of BTCUSDT 1h candles exist from "2025-01-01"
    When I POST "/api/backtest" with JSON:
      """
      {"strategy_id":1,"symbol":"ETHUSDT","timeframe":"1h","start_date":"2025-01-02T00:00:00Z","end_date":"2025-01-08T00:00:00Z","async":true}
      """
    Then the response status is 400
    And the response body contains "no ETHUSDT 1h candles"
    And there are 0 backtest runs stored
    And 0 backtest tasks are queued

  Scenario: When the queue is down the run is marked failed, not left queued forever
    Given 10 days of BTCUSDT 1h candles exist from "2025-01-01"
    And the task queue is down
    When I POST "/api/backtest" with JSON:
      """
      {"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","start_date":"2025-01-02T00:00:00Z","end_date":"2025-01-08T00:00:00Z","async":true}
      """
    Then the response status is 503
    And the only stored backtest run has status "failed"

  Scenario: A finished run's report is regenerated when its stored file was pruned
    Given 10 days of BTCUSDT 1h candles exist from "2025-01-01"
    When I POST "/api/backtest" with JSON:
      """
      {"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","start_date":"2025-01-02T00:00:00Z","end_date":"2025-01-08T00:00:00Z","async":true}
      """
    And I remember the response id
    And the worker processes the queue
    And I GET "/api/backtest/{id}/report"
    Then the response status is 200
    And the response body contains "<html"
    When the stored report files are deleted
    And I GET "/api/backtest/{id}/report"
    Then the response status is 200
    And the response body contains "<html"

  Scenario: An async walk-forward run is queued and the worker fills the same row
    Given 125 days of BTCUSDT 1h candles exist from "2025-01-01"
    When I POST "/api/backtest/walkforward" with JSON:
      """
      {"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","start_date":"2025-01-01T00:00:00Z","end_date":"2025-05-02T00:00:00Z","train_months":3,"test_months":1,"async":true}
      """
    Then the response status is 202
    And the response JSON "status" is "queued"
    And the response JSON "is_walk_forward" is "true"
    And I remember the response id
    When the worker processes the queue
    And I GET "/api/backtest/{id}"
    Then the response JSON "status" is "done"
    And the response JSON "is_walk_forward" is "true"
    And there is 1 backtest run stored
