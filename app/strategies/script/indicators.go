package script

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/indicators"
)

// bindIndicators exposes internal/indicators.IndicatorProvider as the Lua
// `ind` global table: ind.rsi(period), ind.bollinger(period, stddev_up[,
// stddev_down]) -> upper, mid, lower (three return values, Lua-idiomatic
// multi-return, not a table), ind.ema(period), ind.sma(period),
// ind.macd(fast, slow, signal) -> macd, signal, hist, ind.atr(period).
//
// Beyond that original six, every other genuine go-talib indicator is also
// exposed here, lowercase and un-prefixed like the originals: ad, adosc,
// adx, adxr, apo, aroon, aroonosc, avgprice, bop, cci, cmo, dema, dx,
// htdcperiod, htdcphase, htphasor, htsine, httrendline, httrendmode, kama,
// linearreg, linearregangle, linearregintercept, linearregslope, ma,
// macdext, macdfix, mama, max, maxindex, medprice, mfi, midpoint, midprice,
// min, minindex, minmax, minmaxindex, minusdi, minusdm, mom, natr, obv,
// plusdi, plusdm, ppo, roc, rocp, rocr, rocr100, sar, sarext, stddev, stoch,
// stochf, stochrsi, sum, t3, tema, trange, trima, trix, tsf, typprice,
// ultosc, var, wclprice, willr, wma - see docs/specs/strategy-scripting for
// the full per-indicator signature reference. Two go-talib functions are
// deliberately NOT exposed: beta and correl, since both compare two
// independent price series and strategies.Context only ever carries one
// candle series (cctx.Candles) - there's no second series for a script to
// pass in. MaVp (variable-period moving average) is also skipped - it wants
// a caller-supplied per-bar period array, which has no natural Lua-side
// source in a strategy script. Every multi-return indicator (bollinger,
// macd, aroon, stoch, stochf, stochrsi, macdext, macdfix, mama, htphasor,
// htsine, minmax, minmaxindex) returns Lua-idiomatic multiple values, not a
// table, matching bollinger/macd's existing convention. Every MAType
// parameter go-talib exposes (ma, apo, ppo, macdext, stoch, stochf,
// stochrsi) is hardcoded to indicators.MATypeSMA here, same as bollinger's
// existing closure - see internal/indicators/talib_adapter.go's "Additional
// indicators" section for the rationale.
// Every closure closes over cctx.Candles for the CURRENT cycle only - a
// fresh table is bound per CallHook invocation, matching the "fresh LState
// per hook" design (no cross-cycle closure reuse).
func bindIndicators(L *lua.LState, cctx strategies.Context, trace *TraceRecorder) {
	ind := L.NewTable()

	L.SetField(ind, "rsi", L.NewFunction(safeIndicatorClosure(cctx, trace, "rsi", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.RSI(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("rsi(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "bollinger", L.NewFunction(safeIndicatorClosure(cctx, trace, "bollinger", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		stdDevUp := L.CheckNumber(2)
		stdDevDown := stdDevUp
		if L.GetTop() >= 3 {
			stdDevDown = L.CheckNumber(3)
		}
		upper, mid, lower := cctx.Indicators.BollingerBands(cctx.Candles, period, float64(stdDevUp), float64(stdDevDown), indicators.MATypeSMA)
		if len(upper) == 0 || len(mid) == 0 || len(lower) == 0 {
			return 0, fmt.Errorf("bollinger(%d): no data", period)
		}
		L.Push(lua.LNumber(upper[len(upper)-1]))
		L.Push(lua.LNumber(mid[len(mid)-1]))
		L.Push(lua.LNumber(lower[len(lower)-1]))
		return 3, nil
	})))

	L.SetField(ind, "ema", L.NewFunction(safeIndicatorClosure(cctx, trace, "ema", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.EMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("ema(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "sma", L.NewFunction(safeIndicatorClosure(cctx, trace, "sma", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.SMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("sma(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "macd", L.NewFunction(safeIndicatorClosure(cctx, trace, "macd", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fast := L.CheckInt(1)
		slow := L.CheckInt(2)
		signal := L.CheckInt(3)
		macd, sig, hist := cctx.Indicators.MACD(cctx.Candles, fast, slow, signal)
		if len(macd) == 0 || len(sig) == 0 || len(hist) == 0 {
			return 0, fmt.Errorf("macd(%d,%d,%d): no data", fast, slow, signal)
		}
		L.Push(lua.LNumber(macd[len(macd)-1]))
		L.Push(lua.LNumber(sig[len(sig)-1]))
		L.Push(lua.LNumber(hist[len(hist)-1]))
		return 3, nil
	})))

	L.SetField(ind, "atr", L.NewFunction(safeIndicatorClosure(cctx, trace, "atr", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.ATR(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("atr(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "dema", L.NewFunction(safeIndicatorClosure(cctx, trace, "dema", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.DEMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("dema(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "httrendline", L.NewFunction(safeIndicatorClosure(cctx, trace, "httrendline", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.HTTrendline(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("httrendline(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "kama", L.NewFunction(safeIndicatorClosure(cctx, trace, "kama", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.KAMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("kama(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "ma", L.NewFunction(safeIndicatorClosure(cctx, trace, "ma", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.MA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("ma(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "mama", L.NewFunction(safeIndicatorClosure(cctx, trace, "mama", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fastLimit := L.CheckNumber(1)
		slowLimit := L.CheckNumber(2)
		mama, fama := cctx.Indicators.MAMA(cctx.Candles, float64(fastLimit), float64(slowLimit))
		if len(mama) == 0 || len(fama) == 0 {
			return 0, fmt.Errorf("mama(%v,%v): no data", fastLimit, slowLimit)
		}
		L.Push(lua.LNumber(mama[len(mama)-1]))
		L.Push(lua.LNumber(fama[len(fama)-1]))
		return 2, nil
	})))

	L.SetField(ind, "midpoint", L.NewFunction(safeIndicatorClosure(cctx, trace, "midpoint", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.MidPoint(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("midpoint(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "midprice", L.NewFunction(safeIndicatorClosure(cctx, trace, "midprice", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.MidPrice(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("midprice(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "sar", L.NewFunction(safeIndicatorClosure(cctx, trace, "sar", func(L *lua.LState, cctx strategies.Context) (int, error) {
		acceleration := L.CheckNumber(1)
		maximum := L.CheckNumber(2)
		values := cctx.Indicators.SAR(cctx.Candles, float64(acceleration), float64(maximum))
		if len(values) == 0 {
			return 0, fmt.Errorf("sar(%v,%v): no data", acceleration, maximum)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "sarext", L.NewFunction(safeIndicatorClosure(cctx, trace, "sarext", func(L *lua.LState, cctx strategies.Context) (int, error) {
		startValue := L.CheckNumber(1)
		offsetOnReverse := L.CheckNumber(2)
		accelerationInitLong := L.CheckNumber(3)
		accelerationLong := L.CheckNumber(4)
		accelerationMaxLong := L.CheckNumber(5)
		accelerationInitShort := L.CheckNumber(6)
		accelerationShort := L.CheckNumber(7)
		accelerationMaxShort := L.CheckNumber(8)
		values := cctx.Indicators.SARExt(cctx.Candles, float64(startValue), float64(offsetOnReverse), float64(accelerationInitLong), float64(accelerationLong), float64(accelerationMaxLong), float64(accelerationInitShort), float64(accelerationShort), float64(accelerationMaxShort))
		if len(values) == 0 {
			return 0, fmt.Errorf("sarext(%v,%v,%v,%v,%v,%v,%v,%v): no data", startValue, offsetOnReverse, accelerationInitLong, accelerationLong, accelerationMaxLong, accelerationInitShort, accelerationShort, accelerationMaxShort)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "t3", L.NewFunction(safeIndicatorClosure(cctx, trace, "t3", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		vFactor := L.CheckNumber(2)
		values := cctx.Indicators.T3(cctx.Candles, period, float64(vFactor))
		if len(values) == 0 {
			return 0, fmt.Errorf("t3(%d,%v): no data", period, vFactor)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "tema", L.NewFunction(safeIndicatorClosure(cctx, trace, "tema", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.TEMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("tema(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "trima", L.NewFunction(safeIndicatorClosure(cctx, trace, "trima", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.TRIMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("trima(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "wma", L.NewFunction(safeIndicatorClosure(cctx, trace, "wma", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.WMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("wma(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "adx", L.NewFunction(safeIndicatorClosure(cctx, trace, "adx", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.ADX(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("adx(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "adxr", L.NewFunction(safeIndicatorClosure(cctx, trace, "adxr", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.ADXR(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("adxr(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "apo", L.NewFunction(safeIndicatorClosure(cctx, trace, "apo", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fastPeriod := L.CheckInt(1)
		slowPeriod := L.CheckInt(2)
		values := cctx.Indicators.APO(cctx.Candles, fastPeriod, slowPeriod)
		if len(values) == 0 {
			return 0, fmt.Errorf("apo(%d,%d): no data", fastPeriod, slowPeriod)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "aroon", L.NewFunction(safeIndicatorClosure(cctx, trace, "aroon", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		down, up := cctx.Indicators.Aroon(cctx.Candles, period)
		if len(down) == 0 || len(up) == 0 {
			return 0, fmt.Errorf("aroon(%d): no data", period)
		}
		L.Push(lua.LNumber(down[len(down)-1]))
		L.Push(lua.LNumber(up[len(up)-1]))
		return 2, nil
	})))

	L.SetField(ind, "aroonosc", L.NewFunction(safeIndicatorClosure(cctx, trace, "aroonosc", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.AroonOsc(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("aroonosc(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "bop", L.NewFunction(safeIndicatorClosure(cctx, trace, "bop", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.BOP(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("bop(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "cmo", L.NewFunction(safeIndicatorClosure(cctx, trace, "cmo", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.CMO(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("cmo(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "cci", L.NewFunction(safeIndicatorClosure(cctx, trace, "cci", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.CCI(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("cci(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "dx", L.NewFunction(safeIndicatorClosure(cctx, trace, "dx", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.DX(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("dx(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "macdext", L.NewFunction(safeIndicatorClosure(cctx, trace, "macdext", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fastPeriod := L.CheckInt(1)
		slowPeriod := L.CheckInt(2)
		signalPeriod := L.CheckInt(3)
		macd, signal, hist := cctx.Indicators.MACDExt(cctx.Candles, fastPeriod, slowPeriod, signalPeriod)
		if len(macd) == 0 || len(signal) == 0 || len(hist) == 0 {
			return 0, fmt.Errorf("macdext(%d,%d,%d): no data", fastPeriod, slowPeriod, signalPeriod)
		}
		L.Push(lua.LNumber(macd[len(macd)-1]))
		L.Push(lua.LNumber(signal[len(signal)-1]))
		L.Push(lua.LNumber(hist[len(hist)-1]))
		return 3, nil
	})))

	L.SetField(ind, "macdfix", L.NewFunction(safeIndicatorClosure(cctx, trace, "macdfix", func(L *lua.LState, cctx strategies.Context) (int, error) {
		signalPeriod := L.CheckInt(1)
		macd, signal, hist := cctx.Indicators.MACDFix(cctx.Candles, signalPeriod)
		if len(macd) == 0 || len(signal) == 0 || len(hist) == 0 {
			return 0, fmt.Errorf("macdfix(%d): no data", signalPeriod)
		}
		L.Push(lua.LNumber(macd[len(macd)-1]))
		L.Push(lua.LNumber(signal[len(signal)-1]))
		L.Push(lua.LNumber(hist[len(hist)-1]))
		return 3, nil
	})))

	L.SetField(ind, "minusdi", L.NewFunction(safeIndicatorClosure(cctx, trace, "minusdi", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.MinusDI(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("minusdi(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "minusdm", L.NewFunction(safeIndicatorClosure(cctx, trace, "minusdm", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.MinusDM(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("minusdm(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "mfi", L.NewFunction(safeIndicatorClosure(cctx, trace, "mfi", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.MFI(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("mfi(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "mom", L.NewFunction(safeIndicatorClosure(cctx, trace, "mom", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.Mom(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("mom(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "plusdi", L.NewFunction(safeIndicatorClosure(cctx, trace, "plusdi", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.PlusDI(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("plusdi(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "plusdm", L.NewFunction(safeIndicatorClosure(cctx, trace, "plusdm", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.PlusDM(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("plusdm(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "ppo", L.NewFunction(safeIndicatorClosure(cctx, trace, "ppo", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fastPeriod := L.CheckInt(1)
		slowPeriod := L.CheckInt(2)
		values := cctx.Indicators.PPO(cctx.Candles, fastPeriod, slowPeriod)
		if len(values) == 0 {
			return 0, fmt.Errorf("ppo(%d,%d): no data", fastPeriod, slowPeriod)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "rocp", L.NewFunction(safeIndicatorClosure(cctx, trace, "rocp", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.ROCP(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("rocp(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "roc", L.NewFunction(safeIndicatorClosure(cctx, trace, "roc", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.ROC(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("roc(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "rocr", L.NewFunction(safeIndicatorClosure(cctx, trace, "rocr", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.ROCR(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("rocr(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "rocr100", L.NewFunction(safeIndicatorClosure(cctx, trace, "rocr100", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.ROCR100(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("rocr100(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "stoch", L.NewFunction(safeIndicatorClosure(cctx, trace, "stoch", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fastKPeriod := L.CheckInt(1)
		slowKPeriod := L.CheckInt(2)
		slowDPeriod := L.CheckInt(3)
		k, d := cctx.Indicators.Stoch(cctx.Candles, fastKPeriod, slowKPeriod, slowDPeriod)
		if len(k) == 0 || len(d) == 0 {
			return 0, fmt.Errorf("stoch(%d,%d,%d): no data", fastKPeriod, slowKPeriod, slowDPeriod)
		}
		L.Push(lua.LNumber(k[len(k)-1]))
		L.Push(lua.LNumber(d[len(d)-1]))
		return 2, nil
	})))

	L.SetField(ind, "stochf", L.NewFunction(safeIndicatorClosure(cctx, trace, "stochf", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fastKPeriod := L.CheckInt(1)
		fastDPeriod := L.CheckInt(2)
		k, d := cctx.Indicators.StochF(cctx.Candles, fastKPeriod, fastDPeriod)
		if len(k) == 0 || len(d) == 0 {
			return 0, fmt.Errorf("stochf(%d,%d): no data", fastKPeriod, fastDPeriod)
		}
		L.Push(lua.LNumber(k[len(k)-1]))
		L.Push(lua.LNumber(d[len(d)-1]))
		return 2, nil
	})))

	L.SetField(ind, "stochrsi", L.NewFunction(safeIndicatorClosure(cctx, trace, "stochrsi", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		fastKPeriod := L.CheckInt(2)
		fastDPeriod := L.CheckInt(3)
		k, d := cctx.Indicators.StochRSI(cctx.Candles, period, fastKPeriod, fastDPeriod)
		if len(k) == 0 || len(d) == 0 {
			return 0, fmt.Errorf("stochrsi(%d,%d,%d): no data", period, fastKPeriod, fastDPeriod)
		}
		L.Push(lua.LNumber(k[len(k)-1]))
		L.Push(lua.LNumber(d[len(d)-1]))
		return 2, nil
	})))

	L.SetField(ind, "trix", L.NewFunction(safeIndicatorClosure(cctx, trace, "trix", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.Trix(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("trix(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "ultosc", L.NewFunction(safeIndicatorClosure(cctx, trace, "ultosc", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period1 := L.CheckInt(1)
		period2 := L.CheckInt(2)
		period3 := L.CheckInt(3)
		values := cctx.Indicators.UltOsc(cctx.Candles, period1, period2, period3)
		if len(values) == 0 {
			return 0, fmt.Errorf("ultosc(%d,%d,%d): no data", period1, period2, period3)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "willr", L.NewFunction(safeIndicatorClosure(cctx, trace, "willr", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.WillR(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("willr(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "ad", L.NewFunction(safeIndicatorClosure(cctx, trace, "ad", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.AD(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("ad(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "adosc", L.NewFunction(safeIndicatorClosure(cctx, trace, "adosc", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fastPeriod := L.CheckInt(1)
		slowPeriod := L.CheckInt(2)
		values := cctx.Indicators.ADOsc(cctx.Candles, fastPeriod, slowPeriod)
		if len(values) == 0 {
			return 0, fmt.Errorf("adosc(%d,%d): no data", fastPeriod, slowPeriod)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "obv", L.NewFunction(safeIndicatorClosure(cctx, trace, "obv", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.OBV(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("obv(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "natr", L.NewFunction(safeIndicatorClosure(cctx, trace, "natr", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.NATR(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("natr(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "trange", L.NewFunction(safeIndicatorClosure(cctx, trace, "trange", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.TRange(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("trange(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "avgprice", L.NewFunction(safeIndicatorClosure(cctx, trace, "avgprice", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.AvgPrice(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("avgprice(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "medprice", L.NewFunction(safeIndicatorClosure(cctx, trace, "medprice", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.MedPrice(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("medprice(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "typprice", L.NewFunction(safeIndicatorClosure(cctx, trace, "typprice", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.TypPrice(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("typprice(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "wclprice", L.NewFunction(safeIndicatorClosure(cctx, trace, "wclprice", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.WclPrice(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("wclprice(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "htdcperiod", L.NewFunction(safeIndicatorClosure(cctx, trace, "htdcperiod", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.HTDcPeriod(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("htdcperiod(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "htdcphase", L.NewFunction(safeIndicatorClosure(cctx, trace, "htdcphase", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.HTDcPhase(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("htdcphase(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "htphasor", L.NewFunction(safeIndicatorClosure(cctx, trace, "htphasor", func(L *lua.LState, cctx strategies.Context) (int, error) {

		inphase, quadrature := cctx.Indicators.HTPhasor(cctx.Candles)
		if len(inphase) == 0 || len(quadrature) == 0 {
			return 0, fmt.Errorf("htphasor(): no data")
		}
		L.Push(lua.LNumber(inphase[len(inphase)-1]))
		L.Push(lua.LNumber(quadrature[len(quadrature)-1]))
		return 2, nil
	})))

	L.SetField(ind, "htsine", L.NewFunction(safeIndicatorClosure(cctx, trace, "htsine", func(L *lua.LState, cctx strategies.Context) (int, error) {

		sine, leadSine := cctx.Indicators.HTSine(cctx.Candles)
		if len(sine) == 0 || len(leadSine) == 0 {
			return 0, fmt.Errorf("htsine(): no data")
		}
		L.Push(lua.LNumber(sine[len(sine)-1]))
		L.Push(lua.LNumber(leadSine[len(leadSine)-1]))
		return 2, nil
	})))

	L.SetField(ind, "httrendmode", L.NewFunction(safeIndicatorClosure(cctx, trace, "httrendmode", func(L *lua.LState, cctx strategies.Context) (int, error) {

		values := cctx.Indicators.HTTrendMode(cctx.Candles)
		if len(values) == 0 {
			return 0, fmt.Errorf("httrendmode(): no data")
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "linearreg", L.NewFunction(safeIndicatorClosure(cctx, trace, "linearreg", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.LinearReg(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("linearreg(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "linearregangle", L.NewFunction(safeIndicatorClosure(cctx, trace, "linearregangle", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.LinearRegAngle(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("linearregangle(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "linearregintercept", L.NewFunction(safeIndicatorClosure(cctx, trace, "linearregintercept", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.LinearRegIntercept(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("linearregintercept(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "linearregslope", L.NewFunction(safeIndicatorClosure(cctx, trace, "linearregslope", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.LinearRegSlope(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("linearregslope(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "stddev", L.NewFunction(safeIndicatorClosure(cctx, trace, "stddev", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		nbDev := L.CheckNumber(2)
		values := cctx.Indicators.StdDev(cctx.Candles, period, float64(nbDev))
		if len(values) == 0 {
			return 0, fmt.Errorf("stddev(%d,%v): no data", period, nbDev)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "tsf", L.NewFunction(safeIndicatorClosure(cctx, trace, "tsf", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.TSF(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("tsf(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "var", L.NewFunction(safeIndicatorClosure(cctx, trace, "var", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.Var(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("var(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "max", L.NewFunction(safeIndicatorClosure(cctx, trace, "max", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.Max(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("max(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "maxindex", L.NewFunction(safeIndicatorClosure(cctx, trace, "maxindex", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.MaxIndex(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("maxindex(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "min", L.NewFunction(safeIndicatorClosure(cctx, trace, "min", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.Min(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("min(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "minindex", L.NewFunction(safeIndicatorClosure(cctx, trace, "minindex", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.MinIndex(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("minindex(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "minmax", L.NewFunction(safeIndicatorClosure(cctx, trace, "minmax", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		min, max := cctx.Indicators.MinMax(cctx.Candles, period)
		if len(min) == 0 || len(max) == 0 {
			return 0, fmt.Errorf("minmax(%d): no data", period)
		}
		L.Push(lua.LNumber(min[len(min)-1]))
		L.Push(lua.LNumber(max[len(max)-1]))
		return 2, nil
	})))

	L.SetField(ind, "minmaxindex", L.NewFunction(safeIndicatorClosure(cctx, trace, "minmaxindex", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		minIdx, maxIdx := cctx.Indicators.MinMaxIndex(cctx.Candles, period)
		if len(minIdx) == 0 || len(maxIdx) == 0 {
			return 0, fmt.Errorf("minmaxindex(%d): no data", period)
		}
		L.Push(lua.LNumber(minIdx[len(minIdx)-1]))
		L.Push(lua.LNumber(maxIdx[len(maxIdx)-1]))
		return 2, nil
	})))

	L.SetField(ind, "sum", L.NewFunction(safeIndicatorClosure(cctx, trace, "sum", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.Sum(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("sum(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetGlobal("ind", ind)
}

// safeIndicatorClosure is THE required defensive wrapper (blueprint §0.3):
// go-talib (wrapped by the real IndicatorProvider) panics on
// insufficient-candle input (confirmed against
// internal/indicators/talib_adapter_test.go's
// TestRSI_InsufficientCandles_PassesThroughUnchanged, which actually asserts
// a panic via assert.Panics). Every indicator closure gets its own
// recover()->L.RaiseError conversion so a Go-side panic reached via a
// Lua-invoked closure becomes a proper Lua-catchable error, never an
// unrecovered panic unwinding through gopher-lua's call stack (which would
// otherwise be indistinguishable from a process crash - see blueprint §5).
func safeIndicatorClosure(
	cctx strategies.Context,
	trace *TraceRecorder,
	name string,
	fn func(L *lua.LState, cctx strategies.Context) (nret int, err error),
) lua.LGFunction {
	return func(L *lua.LState) (n int) {
		defer func() {
			if r := recover(); r != nil {
				L.RaiseError("ind.%s: %v", name, r)
			}
		}()
		params := luaArgsAsParams(L)
		nret, err := fn(L, cctx)
		if err != nil {
			L.RaiseError("ind.%s: %v", name, err)
		}
		if trace != nil {
			trace.LogIndicatorCall(cctx, name, params, luaReturnAsValues(L, nret))
		}
		return nret
	}
}

// luaArgsAsParams captures the closure's positional call args (already on
// the Lua stack when the closure runs) as a params map for the trace, keyed
// positionally (arg1, arg2, ...) since indicator closures take positional
// numeric args, not named ones.
func luaArgsAsParams(L *lua.LState) map[string]any {
	top := L.GetTop()
	params := make(map[string]any, top)
	for i := 1; i <= top; i++ {
		v := L.Get(i)
		if n, ok := v.(lua.LNumber); ok {
			params[fmt.Sprintf("arg%d", i)] = float64(n)
		}
	}
	return params
}

// luaReturnAsValues captures every numeric value the closure just pushed
// back to Lua, in push order - a single-return indicator yields a
// one-element slice, a multi-return one (bollinger, macd, stoch, ...)
// yields all of them, so LogIndicatorCall can auto-plot every sub-line
// (upper/mid/lower, macd/signal/hist, ...) instead of only the first.
func luaReturnAsValues(L *lua.LState, nret int) []float64 {
	if nret == 0 {
		return nil
	}
	top := L.GetTop()
	values := make([]float64, 0, nret)
	for i := top - nret + 1; i <= top; i++ {
		v := L.Get(i)
		if n, ok := v.(lua.LNumber); ok {
			values = append(values, float64(n))
		}
	}
	return values
}

// bindDebugLog exposes debug.log(label, value) - a project-authored table,
// NOT gopher-lua's real (unopened) debug stdlib. No-op write path when
// trace is nil (production dryrun/paper/live), per blueprint §6.4.
func bindDebugLog(L *lua.LState, trace *TraceRecorder) {
	debugTable := L.NewTable()
	L.SetField(debugTable, "log", L.NewFunction(func(L *lua.LState) int {
		label := L.CheckString(1)
		value := L.Get(2)
		if trace != nil {
			trace.LogEntry(label, fromLuaValue(value))
		}
		return 0
	}))
	L.SetGlobal("debug", debugTable)
}
