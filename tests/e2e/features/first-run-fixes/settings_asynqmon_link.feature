Feature: Asynqmon Monitoring Link in Settings
  As a system administrator
  I want asynqmon_url included in settings endpoints
  So that worker task monitoring links can be configured and rendered in the web UI

  @REQ-FIRST-RUN-BE-04
  Scenario: GET settings returns asynqmon_url field
    Given the database is clean
    When I send a GET request to "/settings"
    Then the response status code should be 200
    And the response body should contain '"asynqmon_url"'

  @REQ-FIRST-RUN-BE-04
  Scenario: PUT settings updates asynqmon_url immediately
    Given the database is clean
    When I update settings via PUT to "/settings" with body:
      """
      {
        "asynqmon_url": "http://localhost:9191/tasks/monitoring"
      }
      """
    Then the response status code should be 200
    And the response body should contain '"applied":true'
    When I send a GET request to "/settings"
    Then the response status code should be 200
    And the response body should contain "http://localhost:9191/tasks/monitoring"
