Feature: Legacy Strategy Archive-in-Place Migration and Deletion Security
  As a system maintainer
  I want legacy hand-coded strategies archived in-place as disabled rows
  So that historical signal data is preserved while preventing resurrection of deleted Go packages

  @REQ-PLATFORM-BE-02
  Scenario: Migration archives productive legacy strategies to disabled status
    Given the database is clean
    And existing strategy rows in database:
      | id | name        | strategy_name | status     |
      | 1  | Legacy Grid | grid          | productive |
      | 2  | Old Scalper | scalping      | disabled   |
      | 3  | New Engine  | template      | productive |
    When the database migration for legacy strategy archival runs
    Then strategy ID 1 status should be "disabled"
    And strategy ID 2 status should remain "disabled"
    And strategy ID 3 status should remain "productive"

  @REQ-PLATFORM-BE-02
  Scenario: Prevent resurrection of deleted legacy strategy packages
    Given the database is clean
    And a disabled strategy row with ID 10 exists for legacy strategy "grid"
    When I send a PUT request to "/strategy/10" with body:
      """
      {
        "name": "Legacy Grid",
        "description": "Attempt to reactivate deleted package",
        "strategy_name": "grid",
        "symbol": "BTCUSDT",
        "status": "productive",
        "cycle": 60
      }
      """
    Then the response status code should be 400
    And the response body should contain "Invalid strategy name"
