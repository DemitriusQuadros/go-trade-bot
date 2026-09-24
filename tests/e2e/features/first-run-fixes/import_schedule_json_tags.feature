Feature: Import Schedule JSON Field Tags and DTO Consistency
  As an API client
  I want import schedule endpoints to decode and encode snake_case JSON fields
  So that cron specifications and schedule attributes round-trip cleanly without dropping values

  @REQ-FIRST-RUN-BE-03
  Scenario: Create recurring import schedule correctly decodes cron_spec and returns snake_case keys
    Given the database is clean
    When I send a POST request to "/candles/schedule" with body:
      """
      {
        "symbol": "ETHUSDT",
        "timeframe": "1h",
        "cron_spec": "0 2 * * *",
        "enabled": true
      }
      """
    Then the response status code should be 201
    And the response body should contain '"cron_spec":"0 2 * * *"'
    And the response body should contain '"timeframe":"1h"'
    And the response body should not contain '"CronSpec"'

  @REQ-FIRST-RUN-BE-03
  Scenario: List recurring import schedules returns array with snake_case fields
    Given the database is clean
    And an import schedule exists for symbol "SOLUSDT" with cron_spec "0 0 * * *"
    When I send a GET request to "/candles/schedule"
    Then the response status code should be 200
    And the response body should contain '"cron_spec":"0 0 * * *"'
    And the response body should contain '"last_run_at"'
    And the response body should not contain '"LastRunAt"'
