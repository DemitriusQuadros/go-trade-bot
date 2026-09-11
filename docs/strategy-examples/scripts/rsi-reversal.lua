-- RSI Mean-Reversion Strategy (Lua Strategy Script, gopher-lua sandboxed)
-- Hooks called by the execution loop:
-- before(ctx), should_long(ctx), go_long(ctx), should_short(ctx), go_short(ctx),
-- update_position(ctx), after(ctx), terminate(ctx)
--
-- ind.rsi(period) returns the LATEST RSI value as a single number. It RAISES
-- a Lua error (not nil) when there isn't enough candle history yet - always
-- read it through pcall so an early-cycle "not enough data" doesn't surface
-- as a hard strategy error every cycle until the candle window fills up.
--
-- go_long/go_short must return a table shaped {buy={qty=...}} / {sell={qty=...}}
-- - the field is `qty`, not `quantity`. (`quantity` is what an OPEN position's
-- size is called when reading ctx.position - a different field on purpose,
-- easy to mix up.)

local RSI_PERIOD = 14
local OVERSOLD = 30
local OVERBOUGHT = 70
local ORDER_QTY = 0.01

-- safe_rsi wraps ind.rsi in pcall: returns nil (instead of raising) when
-- there isn't yet enough candle history for the indicator to compute.
local function safe_rsi()
  local ok, val = pcall(ind.rsi, RSI_PERIOD)
  if ok then
    return val
  end
  return nil
end

function before(ctx)
  -- no per-cycle setup needed for this strategy
end

function should_long(ctx)
  local rsi_val = safe_rsi()
  if rsi_val and rsi_val < OVERSOLD then
    debug.log("rsi_oversold", rsi_val)
    return true
  end
  return false
end

function go_long(ctx)
  return {
    buy = { qty = ORDER_QTY, price = ctx.price },
  }
end

-- should_short/go_short are wired up (ScriptStrategy delegates to them same
-- as should_long/go_long, unlike the old wizard which stubbed the short
-- side) and left here so this script exercises all eight hooks, but note:
-- the exchange integration is spot-only today (see CLAUDE.md's safety
-- notes), so a sell-to-open here will not produce a real short position
-- end-to-end - treat this branch as illustrative/for dry-run inspection,
-- not as something that opens a real position you can later close.
function should_short(ctx)
  local rsi_val = safe_rsi()
  if rsi_val and rsi_val > OVERBOUGHT then
    debug.log("rsi_overbought", rsi_val)
    return true
  end
  return false
end

function go_short(ctx)
  return {
    sell = { qty = ORDER_QTY, price = ctx.price },
  }
end

-- update_position closes the open long position on RSI reversal back to the
-- midline - without this hook a position opened by go_long would never
-- close except via an exchange-level stop-loss (not set by this script),
-- which would make the strategy untestable as a round-trip. ctx.position
-- carries no long/short flag (spot-only exchange - every real open position
-- is a long), so this only ever needs to handle the one case.
function update_position(ctx)
  local rsi_val = safe_rsi()
  if not rsi_val or not ctx.position then
    return nil
  end

  if rsi_val >= 50 then
    -- ctx.position.quantity: the size of the position CURRENTLY OPEN
    -- (read), distinct from the `qty` field used above to REQUEST a new
    -- order.
    debug.log("rsi_reverted_close_long", rsi_val)
    return { sell = { qty = ctx.position.quantity, price = ctx.price } }
  end
  return nil
end

function after(ctx)
  -- no per-cycle teardown needed for this strategy
end

function terminate(ctx)
  debug.log("terminated", ctx.symbol)
end
