Feature: Template Strategy Engine and Strategy Template API
  As a quantitative trader
  I want a stateless condition-evaluator strategy engine and strategy template management endpoints
  So that I can create data-driven trading strategies without writing custom Go code

  @REQ-PLATFORM-BE-01 @REQ-PLATFORM-BE-03
  Scenario: Generic template strategy entry and exit evaluation
    Given the database is clean
    And a template strategy "RSI_Bollinger_Combo" exists for symbol "BTCUSDT" with rule definition:
      """
      {
        "entry": {
          "combinator": "AND",
          "conditions": [
            {
              "type": "indicator_threshold",
              "indicator": "rsi",
              "params": {"period": 14},
              "comparator": "<",
              "value": 30.0
            },
            {
              "type": "indicator_threshold",
              "indicator": "bollinger_lower",
              "params": {"period": 20, "std_dev": 2.0},
              "comparator": "<",
              "field": "price"
            }
          ]
        },
        "exit": {
          "combinator": "OR",
          "conditions": [
            {
              "type": "indicator_threshold",
              "indicator": "bollinger_upper",
              "params": {"period": 20, "std_dev": 2.0},
              "comparator": ">",
              "field": "price"
            }
          ],
          "take_profit_pct": 4.0
        }
      }
      """
    When the engine evaluates strategy "RSI_Bollinger_Combo" against current price 48000.0 and RSI 25.0
    Then ShouldLong should return true
    And GoLong produces a buy signal at price 48000.0

  @REQ-PLATFORM-BE-01
  Scenario: Insufficient candle history fails closed without strategy execution error
    Given the database is clean
    And a template strategy "RSI_50_Strategy" requires 50 candles for RSI
    When the engine evaluates the strategy with only 30 candles available
    Then ShouldLong should return false
    And no strategy execution error should be logged

  @REQ-PLATFORM-BE-01
  Scenario: Short-side entry is unconditionally disabled
    Given a template strategy "Short_Attempt" exists
    When ShouldShort is called for the template strategy
    Then it should return false unconditionally

  @REQ-PLATFORM-BE-03
  Scenario: Save-time validation enforces an exit path requirement
    Given the database is clean
    When I send a POST request to "/strategy" with body:
      """
      {
        "name": "No_Exit_Strategy",
        "description": "Invalid template without exit path",
        "strategy_name": "template",
        "symbol": "BTCUSDT",
        "cycle": 60,
        "configuration": {
          "template": {
            "entry": {
              "combinator": "AND",
              "conditions": [
                {
                  "type": "indicator_threshold",
                  "indicator": "rsi",
                  "comparator": "<",
                  "value": 30.0
                }
              ]
            },
            "exit": {
              "combinator": "OR",
              "conditions": []
            }
          }
        }
      }
      """
    Then the response status code should be 400
    And the response body should contain "exit path"

  @REQ-PLATFORM-BE-03
  Scenario: Plain-English summary endpoint and live preview
    Given the database is clean
    When I send a POST request to "/strategy/template/preview" with body:
      """
      {
        "rule_definition": {
          "entry": {
            "combinator": "AND",
            "conditions": [
              {
                "type": "indicator_threshold",
                "indicator": "rsi",
                "params": {"period": 14},
                "comparator": "<",
                "value": 30.0
              }
            ]
          },
          "exit": {
            "combinator": "OR",
            "conditions": [],
            "take_profit_pct": 4.0
          }
        }
      }
      """
    Then the response status code should be 200
    And the response body should contain "Buy when RSI(14) < 30"
    And the response body should contain "Exit at +4% take-profit"
