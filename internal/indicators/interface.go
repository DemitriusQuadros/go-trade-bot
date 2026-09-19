// Package indicators is the Anti-Corruption Layer boundary for technical
// indicator computation. No package outside internal/indicators may import
// github.com/markcheno/go-talib - every indicator call flows through the
// IndicatorProvider interface defined here.
package indicators

import "go-trade-bot/internal/exchange"

// MAType selects the moving-average basis used by BollingerBands. Exposed on
// the ACL (rather than hardcoded) because the current Bollinger strategy
// uses an EMA-smoothed middle band, not the PRD's naive SMA-implied default -
// see Spec 07 for the fidelity rationale.
type MAType int

const (
	MATypeSMA MAType = iota
	MATypeEMA
)

// IndicatorProvider wraps go-talib, scoped to what the three Phase 1
// strategies actually call (RSI, BollingerBands, EMA) plus MACD/SMA/ATR,
// which are part of the frozen interface for forward compatibility (ADR-005)
// even though no Phase 1 strategy calls them yet.
//
// Every method below RSI..ATR is FROZEN (ADR-005) - do not change their
// signatures. Everything from DEMA onward was added later to cover the rest
// of go-talib's genuine indicator surface (see internal/indicators/talib_adapter.go's
// header and docs/specs/strategy-scripting for the full ind.* Lua catalogue) -
// these are new additions, not part of the frozen set, and MAY gain params
// in a backward-compatible way (new methods only, never signature changes)
// if go-talib itself changes. Two go-talib functions are intentionally not
// wrapped: Beta and Correl, both of which compare two independent price
// series - there is no second candle series available anywhere in
// strategies.Context, so no sensible signature exists for them here or in
// the Lua ind.* bindings. MaVp (variable-period moving average) is also
// skipped - it requires a caller-supplied per-bar period array, which has no
// natural Lua-side source in a strategy script.
type IndicatorProvider interface {
	RSI(candles []exchange.Candle, period int) []float64
	BollingerBands(candles []exchange.Candle, period int, stdDevUp, stdDevDown float64, maType MAType) (upper, mid, lower []float64)
	EMA(candles []exchange.Candle, period int) []float64
	SMA(candles []exchange.Candle, period int) []float64
	MACD(candles []exchange.Candle, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist []float64)
	ATR(candles []exchange.Candle, period int) []float64

	// Overlap studies
	DEMA(candles []exchange.Candle, period int) []float64
	HTTrendline(candles []exchange.Candle) []float64
	KAMA(candles []exchange.Candle, period int) []float64
	MA(candles []exchange.Candle, period int) []float64
	MAMA(candles []exchange.Candle, fastLimit float64, slowLimit float64) (mama []float64, fama []float64)
	MidPoint(candles []exchange.Candle, period int) []float64
	MidPrice(candles []exchange.Candle, period int) []float64
	SAR(candles []exchange.Candle, acceleration float64, maximum float64) []float64
	SARExt(candles []exchange.Candle, startValue float64, offsetOnReverse float64, accelerationInitLong float64, accelerationLong float64, accelerationMaxLong float64, accelerationInitShort float64, accelerationShort float64, accelerationMaxShort float64) []float64
	T3(candles []exchange.Candle, period int, vFactor float64) []float64
	TEMA(candles []exchange.Candle, period int) []float64
	TRIMA(candles []exchange.Candle, period int) []float64
	WMA(candles []exchange.Candle, period int) []float64

	// Momentum
	ADX(candles []exchange.Candle, period int) []float64
	ADXR(candles []exchange.Candle, period int) []float64
	APO(candles []exchange.Candle, fastPeriod int, slowPeriod int) []float64
	Aroon(candles []exchange.Candle, period int) (down []float64, up []float64)
	AroonOsc(candles []exchange.Candle, period int) []float64
	BOP(candles []exchange.Candle) []float64
	CMO(candles []exchange.Candle, period int) []float64
	CCI(candles []exchange.Candle, period int) []float64
	DX(candles []exchange.Candle, period int) []float64
	MACDExt(candles []exchange.Candle, fastPeriod int, slowPeriod int, signalPeriod int) (macd []float64, signal []float64, hist []float64)
	MACDFix(candles []exchange.Candle, signalPeriod int) (macd []float64, signal []float64, hist []float64)
	MinusDI(candles []exchange.Candle, period int) []float64
	MinusDM(candles []exchange.Candle, period int) []float64
	MFI(candles []exchange.Candle, period int) []float64
	Mom(candles []exchange.Candle, period int) []float64
	PlusDI(candles []exchange.Candle, period int) []float64
	PlusDM(candles []exchange.Candle, period int) []float64
	PPO(candles []exchange.Candle, fastPeriod int, slowPeriod int) []float64
	ROCP(candles []exchange.Candle, period int) []float64
	ROC(candles []exchange.Candle, period int) []float64
	ROCR(candles []exchange.Candle, period int) []float64
	ROCR100(candles []exchange.Candle, period int) []float64
	Stoch(candles []exchange.Candle, fastKPeriod int, slowKPeriod int, slowDPeriod int) (k []float64, d []float64)
	StochF(candles []exchange.Candle, fastKPeriod int, fastDPeriod int) (k []float64, d []float64)
	StochRSI(candles []exchange.Candle, period int, fastKPeriod int, fastDPeriod int) (k []float64, d []float64)
	Trix(candles []exchange.Candle, period int) []float64
	UltOsc(candles []exchange.Candle, period1 int, period2 int, period3 int) []float64
	WillR(candles []exchange.Candle, period int) []float64

	// Volume
	AD(candles []exchange.Candle) []float64
	ADOsc(candles []exchange.Candle, fastPeriod int, slowPeriod int) []float64
	OBV(candles []exchange.Candle) []float64

	// Volatility
	NATR(candles []exchange.Candle, period int) []float64
	TRange(candles []exchange.Candle) []float64

	// Price transform
	AvgPrice(candles []exchange.Candle) []float64
	MedPrice(candles []exchange.Candle) []float64
	TypPrice(candles []exchange.Candle) []float64
	WclPrice(candles []exchange.Candle) []float64

	// Cycle (Hilbert Transform)
	HTDcPeriod(candles []exchange.Candle) []float64
	HTDcPhase(candles []exchange.Candle) []float64
	HTPhasor(candles []exchange.Candle) (inphase []float64, quadrature []float64)
	HTSine(candles []exchange.Candle) (sine []float64, leadSine []float64)
	HTTrendMode(candles []exchange.Candle) []float64

	// Statistic functions (Beta, Correl intentionally omitted - see type doc above)
	LinearReg(candles []exchange.Candle, period int) []float64
	LinearRegAngle(candles []exchange.Candle, period int) []float64
	LinearRegIntercept(candles []exchange.Candle, period int) []float64
	LinearRegSlope(candles []exchange.Candle, period int) []float64
	StdDev(candles []exchange.Candle, period int, nbDev float64) []float64
	TSF(candles []exchange.Candle, period int) []float64
	Var(candles []exchange.Candle, period int) []float64

	// Math window functions (candle-relevant subset of go-talib's math funcs)
	Max(candles []exchange.Candle, period int) []float64
	MaxIndex(candles []exchange.Candle, period int) []float64
	Min(candles []exchange.Candle, period int) []float64
	MinIndex(candles []exchange.Candle, period int) []float64
	MinMax(candles []exchange.Candle, period int) (min []float64, max []float64)
	MinMaxIndex(candles []exchange.Candle, period int) (minIdx []float64, maxIdx []float64)
	Sum(candles []exchange.Candle, period int) []float64
}
