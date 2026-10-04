Feature: Real-time SSE Dashboard Event Stream
  As a web user
  I want real-time price and position updates over Server-Sent Events
  So that the dashboard reflects live market changes without client polling spam

  @REQ-WEB-BE-03
  Scenario: SSE stream connection with query param authorization emits price and heartbeat events
    Given the database is clean
    And API authorization token is configured as "secret_token_123"
    And active strategies monitoring symbol "BTCUSDT" exist
    When I connect to SSE stream at "/stream/dashboard?token=secret_token_123"
    Then the response status code should be 200
    And the response header "Content-Type" should be "text/event-stream"
    And the event stream should receive a "price_update" event for "BTCUSDT"
    And the event stream should receive a "heartbeat" event within interval

  @REQ-WEB-BE-03
  Scenario: Unauthenticated SSE stream connection is rejected
    Given the database is clean
    And API authorization token is configured as "secret_token_123"
    When I send a GET request to "/stream/dashboard" with no token parameter
    Then the response status code should be 401
