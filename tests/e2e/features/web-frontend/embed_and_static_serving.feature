Feature: Embedded SPA and Static File Serving
  As a web user
  I want the embedded React SPA and static assets served same-origin
  So that I can use the web interface without a standalone static asset server

  @REQ-WEB-BE-02
  Scenario: Serving embedded index.html for root path
    Given the database is clean
    And the embedded SPA dist directory contains index.html
    When I send a GET request to "/"
    Then the response status code should be 200
    And the response header "Content-Type" should contain "text/html"

  @REQ-WEB-BE-02
  Scenario: Client-side routing fallback for deep paths
    Given the database is clean
    And the embedded SPA dist directory contains index.html
    When I send a GET request to "/strategies/5"
    Then the response status code should be 200
    And the response body should contain index.html shell content

  @REQ-WEB-BE-02
  Scenario: Unbuilt frontend placeholder returns 503 Service Unavailable
    Given the database is clean
    And the embedded SPA contains only placeholder file
    When I send a GET request to "/"
    Then the response status code should be 503
    And the response body should contain "Frontend not built"
