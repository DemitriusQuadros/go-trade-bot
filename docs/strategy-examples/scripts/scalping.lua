-- Scalping Strategy Example (Lua)
-- Fast-paced momentum or scalping logic based on RSI and EMA.

function should_long(ctx)
  local rsi = indicator("rsi", { period = 14 })
  local ema = indicator("ema", { period = 9 })
  -- Buy if RSI is oversold and price is above short-term EMA
  return rsi < 30 and ctx.price > ema
end

function go_long(ctx)
  return { buy = { price = ctx.price } }
end

function update_position(ctx)
  -- Tight stop-loss and take-profit for scalping
  if ctx.price >= ctx.position.entry_price * 1.005 then
    return { sell = { price = ctx.price } } -- take profit at +0.5%
  elseif ctx.price <= ctx.position.entry_price * 0.998 then
    return { sell = { price = ctx.price } } -- stop loss at -0.2%
  end
  return nil
end
