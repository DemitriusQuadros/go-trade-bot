-- Bollinger Bands Strategy Example (Lua)
-- Mean reversion strategy using Bollinger Bands.

function should_long(ctx)
  -- Buy when price crosses below the lower band
  local lower_band = indicator("bollinger_lower", { period = 20, std_dev = 2.0 })
  return ctx.price < lower_band
end

function go_long(ctx)
  return { buy = { price = ctx.price } }
end

function update_position(ctx)
  -- Sell when price crosses the middle or upper band
  local middle_band = indicator("bollinger_middle", { period = 20, std_dev = 2.0 })
  if ctx.price > middle_band then
    return { sell = { price = ctx.price } }
  end
  return nil
end
