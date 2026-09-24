-- Grid Strategy Example (Lua)
-- A simple grid trading strategy that places buy and sell orders at fixed intervals.

function before(ctx)
  -- Initialization or pre-calculation can happen here
end

function should_long(ctx)
  -- Example grid logic: if price drops below a certain threshold
  return ctx.price < 50000
end

function go_long(ctx)
  return { buy = { price = ctx.price } }
end

function update_position(ctx)
  -- Exit the position if the price hits our grid sell target (e.g. +1%)
  if ctx.price >= ctx.position.entry_price * 1.01 then
    return { sell = { price = ctx.price } }
  end
  return nil
end
