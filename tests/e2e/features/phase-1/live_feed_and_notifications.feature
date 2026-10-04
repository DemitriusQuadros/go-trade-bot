Feature: Live Feed, Webhooks & Observability
  As a trading platform operator
  I want real-time WebSocket market data streaming, outbound webhooks, and Prometheus metrics
  So that execution is event-driven, operators are notified of events, and system health is monitored

  Background:
    Given the database is clean
    And the mock webhook server is listening

  @smoke @REQ-OBS-01
  Scenario: Live WebSocket feed streams completed kline candles to strategy engine
    Given a LiveFeed connected to the mock WebSocket server for "BTCUSDT"
    When the WebSocket server emits a closed kline candle for timestamp 1700000000
    Then the feed Next method should yield the candle with open 50000.0 and close 50500.0

  @smoke @REQ-OBS-02
  Scenario: Trade execution events trigger outbound JSON webhook notifications
    Given a configured webhook endpoint "http://localhost:9999/webhook"
    When a position is opened for "BTCUSDT"
    Then the mock webhook server should receive an HTTP POST request with event "position.opened"
    And the JSON payload should contain symbol "BTCUSDT" and entry price 50000.0

  @smoke @REQ-OBS-03
  Scenario: Worker exports Prometheus metrics on port 9191
    Given the worker metrics server is running on port 9191
    When an order execution occurs
    Then an HTTP GET request to "http://localhost:9191/metrics" should return HTTP 200
    And the response body should contain metric "orders_placed_total"
