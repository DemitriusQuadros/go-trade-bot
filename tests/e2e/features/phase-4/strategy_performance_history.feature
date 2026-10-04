Feature: Strategy Performance History
  As a portfolio manager
  I want periodic time-bucketed snapshots of strategy performance
  So that I can monitor profit and loss history trends and render sparklines over time

  Background:
    Given the database is clean
    And the API server is running
    And a strategy exists in the database with id 1 for symbol "BTCUSDT"

  @smoke @REQ-HIST-001
  Scenario: Create periodic performance snapshots and query history series
    Given closed orders exist for strategy 1 on symbol "BTCUSDT" across multiple days
    When performance snapshot calculation is triggered for bucket "daily"
    Then performance snapshot rows should be persisted in the database for strategy 1
    When I send a GET request to "/strategy/1/performance/history?bucket=daily&symbol=BTCUSDT"
    Then the response status code should be 200
    And the response body should contain '"bucket":"daily"'
    And the response body should contain '"profit"'

  @REQ-HIST-002
  Scenario: Snapshot calculation is idempotent
    Given closed orders exist for strategy 1 on symbol "BTCUSDT" across multiple days
    When performance snapshot calculation is triggered for bucket "daily"
    And performance snapshot calculation is triggered again for bucket "daily"
    Then no duplicate performance snapshot rows should exist for the same period and bucket
