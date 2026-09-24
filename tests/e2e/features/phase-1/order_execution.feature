Feature: Financial Order Execution & Fills
  As a signal processing engine
  I want to submit real market orders to the exchange and record fills accurately
  So that user account balances and order records match actual exchange fills

  Background:
    Given the database is clean
    And the mock exchange server is active

  @smoke @REQ-ORDER-01
  Scenario: Market BUY order execution creates signal and order with actual exchange fill
    Given an active strategy for "BTCUSDT" with balance 1000.0 USD
    And the exchange mock will accept market BUY orders with fill price 50000.0 and executed qty 0.02
    When a BUY signal is generated for "BTCUSDT" at estimated price 49900.0
    Then a market order should be placed on the exchange with ClientOrderID
    And the database should contain a signal with status "Open"
    And the database order record should have BrokerOrderID "EX-ORD-1001"
    And the database order executed quantity should be 0.02 at entry price 50000.0

  @edge @REQ-ORDER-02
  Scenario: Partial fill correctly updates order quantity and deducts actual spent capital
    Given an active strategy for "ETHUSDT" with balance 2000.0 USD
    And the exchange mock will execute a partial BUY fill for requested 1.0 ETH with executed qty 0.6 at price 3000.0
    When a BUY signal is generated for "ETHUSDT" requesting quantity 1.0
    Then the database order quantity should equal actual executed quantity 0.6
    And the account balance should be deducted by spent capital 1800.0 USD

  @error @REQ-ORDER-03
  Scenario: Exchange rejection does not create signal or order records
    Given an active strategy for "SOLUSDT"
    And the exchange mock will reject orders with error "INSUFFICIENT_BALANCE"
    When a BUY signal is generated for "SOLUSDT"
    Then the buy signal processing should fail with an error
    And no signal row should be persisted in the database
    And the account balance should remain unchanged

  @invariants @REQ-ORDER-04
  Scenario: Idempotent order placement prevents duplicate fills
    Given an exchange order was already placed with ClientOrderID "gtb-1-BUY-100"
    When another order request arrives with the same ClientOrderID "gtb-1-BUY-100"
    Then the exchange adapter returns the existing order result without placing a second order
