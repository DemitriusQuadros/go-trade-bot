Feature: API Consistency Fixes, Equity Curve & Report Serving
  As a web developer
  I want standardized snake_case JSON DTO responses, backtest equity curves, and HTML report serving
  So that the web application renders consistent data structures and interactive reports

  @REQ-WEB-BE-04
  Scenario: Strategy and signal endpoints return snake_case JSON keys
    Given the database is clean
    And a strategy with ID 1 exists with name "My Strategy" and strategy_name "template"
    When I send a GET request to "/strategy/1"
    Then the response status code should be 200
    And the response body should contain '"strategy_name"'
    And the response body should contain '"monitored_symbols"'
    And the response body should not contain '"StrategyName"'

  @REQ-WEB-BE-04
  Scenario: Backtest run response includes persisted equity curve
    Given the database is clean
    And a backtest run with ID 10 exists with persisted equity curve data
    When I send a GET request to "/backtest/10"
    Then the response status code should be 200
    And the response body should contain '"equity_curve"'

  @REQ-WEB-BE-04
  Scenario: Serving HTML report file for completed backtest run
    Given the database is clean
    And API authorization token is configured as "secret_token_123"
    And a backtest run with ID 10 has an HTML report file at "reports/test_report.html"
    When I send a GET request to "/backtest/10/report?token=secret_token_123"
    Then the response status code should be 200
    And the response header "Content-Type" should contain "text/html"
