Feature: Candle Import Trigger & Scheduling API
  As a trader
  I want an API to trigger asynchronous candle imports and manage recurring import schedules
  So that historical market data can be ingested and maintained without using CLI commands

  @REQ-PLATFORM-BE-04
  Scenario: Asynchronous one-off candle import job trigger and status polling
    Given the database is clean
    When I trigger a candle import via POST to "/candles/import" with body:
      """
      {
        "symbols": ["BTCUSDT"],
        "timeframes": ["1m", "15m"],
        "from": "2024-01-01T00:00:00Z",
        "to": "2024-01-02T00:00:00Z"
      }
      """
    Then the response status code should be 202
    And the response body should contain "job_id"
    And the response body should contain "pending"
    When I poll the candle import job status for the created job
    Then the job status should become "completed"
    And the import result should contain imported candles for symbol "BTCUSDT"

  @REQ-PLATFORM-BE-04
  Scenario: Recurring candle import schedule CRUD
    Given the database is clean
    When I create a recurring import schedule via POST to "/candles/schedule" with body:
      """
      {
        "symbol": "ETHUSDT",
        "timeframe": "1h",
        "cron_spec": "0 1 * * *",
        "enabled": true
      }
      """
    Then the response status code should be 201
    And the response body should contain "id"
    When I send a GET request to "/candles/schedule"
    Then the response status code should be 200
    And the response body should contain "ETHUSDT"
    And the response body should contain "0 1 * * *"
