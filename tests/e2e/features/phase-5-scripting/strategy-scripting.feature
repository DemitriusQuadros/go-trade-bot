Feature: Lua strategy scripting end-to-end (Phase 5)
  As a strategy author
  I want to create a Lua "script" strategy, backtest it, and run a dryrun cycle
  So that scripted strategies are a first-class, registry-resolved strategy type

  Scenario: Create, backtest, and dry-run a Lua script strategy
    Given a script strategy "RSI Dip Buyer" for symbol "BTCUSDT" with source:
      """
      function should_long(ctx)
        return ctx.price < 100
      end
      function go_long(ctx)
        return {buy = {price = ctx.price}}
      end
      function update_position(ctx)
        if ctx.price >= ctx.position.entry_price * 1.005 then
          return {sell = {price = ctx.price}}
        end
        return nil
      end
      """
    When I run a backtest for the script strategy over 200 oscillating candles
    Then the script backtest should complete with at least 1 trade
    When the worker runs one dryrun cycle for the script strategy
    Then a strategy execution with status "ok" should be recorded for the script strategy
