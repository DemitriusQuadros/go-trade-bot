# Strategy Authoring Reference

Generated from `app/strategies/interface.go` and `app/strategies/script/indicators.go` by `internal/docgen` (Backend Spec 05, `ai-strategy-agent`). Do not hand-edit - run `make gen-agent-docs` after changing either source file.

## The Strategy contract

Every strategy is a Lua script evaluated against a fixed lifecycle-hook contract. `Strategy`/`Context`/`Signal`/`ExecutionMode` (`app/strategies/interface.go`) are frozen - the hook signatures below never change.

### Hooks

- `Name() string`
- `Before(ctx Context)`
- `ShouldLong(ctx Context) bool`
- `GoLong(ctx Context) Signal`
- `ShouldShort(ctx Context) bool`
- `GoShort(ctx Context) Signal`
- `UpdatePosition(ctx Context) *Signal`
- `After(ctx Context)`
- `Terminate(ctx Context)`

### Context fields

Every hook receives a `Context` with exactly these fields:

- `Candles []exchange.Candle`
- `Position *Position` - nil if no open position for this symbol+strategy
- `Account Account`
- `Config map[string]interface{}`
- `Indicators indicators.IndicatorProvider`
- `Price float64` - most recent candle's Close
- `Timeframe string`
- `Symbol string`
- `Mode ExecutionMode`

### Signal fields

`GoLong`/`GoShort`/`UpdatePosition` return a `Signal`:

- `Buy *Order`
- `Sell *Order`
- `StopLoss *Order`
- `TakeProfit *Order`

### Execution mode

ExecutionMode is the four-tier risk ladder every strategy execution runs under (PRD SS4.4, Spec 10). Ordinal order matters: ModeBacktest(0) < ModeDryRun(1) < ModePaper(2) < ModeLive(3).

## Indicator catalogue (75 methods)

Every `ind.*` closure available to a Lua script, bound fresh per cycle over the current candle window:

bindIndicators exposes internal/indicators.IndicatorProvider as the Lua `ind` global table: ind.rsi(period), ind.bollinger(period, stddev_up[, stddev_down]) -> upper, mid, lower (three return values, Lua-idiomatic multi-return, not a table), ind.ema(period), ind.sma(period), ind.macd(fast, slow, signal) -> macd, signal, hist, ind.atr(period). Beyond that original six, every other genuine go-talib indicator is also exposed here, lowercase and un-prefixed like the originals: ad, adosc, adx, adxr, apo, aroon, aroonosc, avgprice, bop, cci, cmo, dema, dx, htdcperiod, htdcphase, htphasor, htsine, httrendline, httrendmode, kama, linearreg, linearregangle, linearregintercept, linearregslope, ma, macdext, macdfix, mama, max, maxindex, medprice, mfi, midpoint, midprice, min, minindex, minmax, minmaxindex, minusdi, minusdm, mom, natr, obv, plusdi, plusdm, ppo, roc, rocp, rocr, rocr100, sar, sarext, stddev, stoch, stochf, stochrsi, sum, t3, tema, trange, trima, trix, tsf, typprice, ultosc, var, wclprice, willr, wma - see docs/specs/strategy-scripting for the full per-indicator signature reference. Two go-talib functions are deliberately NOT exposed: beta and correl, since both compare two independent price series and strategies.Context only ever carries one candle series (cctx.Candles) - there's no second series for a script to pass in. MaVp (variable-period moving average) is also skipped - it wants a caller-supplied per-bar period array, which has no natural Lua-side source in a strategy script. Every multi-return indicator (bollinger, macd, aroon, stoch, stochf, stochrsi, macdext, macdfix, mama, htphasor, htsine, minmax, minmaxindex) returns Lua-idiomatic multiple values, not a table, matching bollinger/macd's existing convention. Every MAType parameter go-talib exposes (ma, apo, ppo, macdext, stoch, stochf, stochrsi) is hardcoded to indicators.MATypeSMA here, same as bollinger's existing closure - see internal/indicators/talib_adapter.go's "Additional indicators" section for the rationale. Every closure closes over cctx.Candles for the CURRENT cycle only - a fresh table is bound per CallHook invocation, matching the "fresh LState per hook" design (no cross-cycle closure reuse).

- `ind.ad` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.adosc` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.adx` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.adxr` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.apo` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.aroon` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.aroonosc` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.atr` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.avgprice` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.bollinger` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.bop` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.cci` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.cmo` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.dema` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.dx` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.ema` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.htdcperiod` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.htdcphase` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.htphasor` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.htsine` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.httrendline` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.httrendmode` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.kama` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.linearreg` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.linearregangle` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.linearregintercept` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.linearregslope` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.ma` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.macd` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.macdext` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.macdfix` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.mama` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.max` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.maxindex` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.medprice` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.mfi` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.midpoint` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.midprice` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.min` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.minindex` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.minmax` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.minmaxindex` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.minusdi` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.minusdm` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.mom` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.natr` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.obv` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.plusdi` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.plusdm` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.ppo` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.roc` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.rocp` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.rocr` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.rocr100` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.rsi` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.sar` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.sarext` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.sma` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.stddev` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.stoch` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.stochf` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.stochrsi` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.sum` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.t3` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.tema` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.trange` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.trima` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.trix` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.tsf` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.typprice` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.ultosc` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.var` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.wclprice` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.willr` - see `app/strategies/script/indicators.go` for its exact signature.
- `ind.wma` - see `app/strategies/script/indicators.go` for its exact signature.

## Config conventions

### Valid `cycle_minutes` values

- `1` (OneMinute)
- `5` (FiveMinutes)
- `10` (TenMinutes)
- `15` (FifteenMinutes)
- `30` (ThirtyMinutes)
- `60` (OneHour)

### Valid `status` values

A script authored by the agent is always saved with `status = "testing"` - the agent has no channel to request any other status. For reference, the full enum is:

- `productive` (Productive)
- `testing` (Testing)
- `disabled` (Disabled)

### JSON key convention

Every tool input/output uses snake_case keys (`strategy_id`, `script_source`, `cycle_minutes`), matching every other REST DTO in this codebase.
