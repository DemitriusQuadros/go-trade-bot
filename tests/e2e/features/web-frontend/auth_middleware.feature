Feature: Auth Middleware and Security Gateway
  As an application operator
  I want single bearer token authorization on API endpoints
  So that unauthorized requests to the trading control plane are rejected

  @REQ-WEB-BE-01
  Scenario: Authorized API request with valid Bearer token succeeds
    Given the database is clean
    And API authorization token is configured as "secret_token_123"
    When I send a GET request to "/strategy" with header "Authorization" set to "Bearer secret_token_123"
    Then the response status code should be 200

  @REQ-WEB-BE-01
  Scenario: Unauthorized API request returns consistent 401 JSON error
    Given the database is clean
    And API authorization token is configured as "secret_token_123"
    When I send a GET request to "/strategy" with no Authorization header
    Then the response status code should be 401
    And the response body should contain '"error":"unauthorized"'
    And the response body should contain "missing or invalid Authorization header"

  @REQ-WEB-BE-01
  Scenario: Metrics endpoint remains accessible without authorization
    Given the database is clean
    And API authorization token is configured as "secret_token_123"
    When I send a GET request to "/metrics" with no Authorization header
    Then the response status code should be 200
