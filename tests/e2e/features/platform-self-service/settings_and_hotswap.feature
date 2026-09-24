Feature: Settings Management and Risk-Bearing Hot-Swap Safety
  As an administrator
  I want tiered settings updates with drain-then-swap safety for risk-bearing parameters
  So that system configuration changes never corrupt active trading cycles or exceed safety limits

  @REQ-PLATFORM-BE-05
  Scenario: Safe settings update executes immediately without draining
    Given the database is clean
    When I update settings via PUT to "/settings" with body:
      """
      {
        "webhook_url": "https://hooks.example.com/trade-events"
      }
      """
    Then the response status code should be 200
    And the response body should contain '"applied":true'
    And the active webhook notifier URL should be updated to "https://hooks.example.com/trade-events"

  @REQ-PLATFORM-BE-05
  Scenario: Risk-bearing settings update drains in-flight cycles before swapping credentials
    Given the database is clean
    And a strategy cycle is currently in-flight
    When I update settings via PUT to "/settings" with body:
      """
      {
        "broker_api_key": "new_key_123",
        "broker_api_secret": "new_secret_456"
      }
      """
    Then the in-flight cycle should be allowed to complete uninterrupted
    And the response status code should be 200
    And the exchange client should be atomically swapped to the new credentials

  @REQ-PLATFORM-BE-05
  Scenario: Mode transition to live requires explicit confirm_live flag
    Given the database is clean
    When I update settings via PUT to "/settings" with body:
      """
      {
        "mode": "live",
        "confirm_live": false
      }
      """
    Then the response status code should be 400
    And the response body should contain "confirm_live"
