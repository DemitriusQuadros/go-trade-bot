package indicators

import (
	"go-trade-bot/internal/exchange"

	"github.com/markcheno/go-talib"
)

// TalibAdapter is the only file in the repository permitted to import
// github.com/markcheno/go-talib (enforced by the same CI import-lint as
// Spec 01's go-binance restriction). It converts []exchange.Candle to the
// []float64 close-price (or high/low/close) slices go-talib expects
// internally - callers never touch raw float slices or know go-talib exists.
type TalibAdapter struct{}

func NewTalibAdapter() *TalibAdapter {
	return &TalibAdapter{}
}

func (a *TalibAdapter) RSI(candles []exchange.Candle, period int) []float64 {
	return talib.Rsi(closes(candles), period)
}

func (a *TalibAdapter) BollingerBands(candles []exchange.Candle, period int, stdDevUp, stdDevDown float64, maType MAType) (upper, mid, lower []float64) {
	return talib.BBands(closes(candles), period, stdDevUp, stdDevDown, toTalibMAType(maType))
}

func (a *TalibAdapter) EMA(candles []exchange.Candle, period int) []float64 {
	return talib.Ema(closes(candles), period)
}

func (a *TalibAdapter) SMA(candles []exchange.Candle, period int) []float64 {
	return talib.Sma(closes(candles), period)
}

func (a *TalibAdapter) MACD(candles []exchange.Candle, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist []float64) {
	return talib.Macd(closes(candles), fastPeriod, slowPeriod, signalPeriod)
}

func (a *TalibAdapter) ATR(candles []exchange.Candle, period int) []float64 {
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	closeVals := make([]float64, len(candles))
	for i, c := range candles {
		highs[i] = c.High
		lows[i] = c.Low
		closeVals[i] = c.Close
	}
	return talib.Atr(highs, lows, closeVals, period)
}

func closes(candles []exchange.Candle) []float64 {
	closeVals := make([]float64, len(candles))
	for i, c := range candles {
		closeVals[i] = c.Close
	}
	return closeVals
}

// opens, highs, lows, volumes are closes' siblings - added alongside the new
// (non-frozen) indicator methods below, which need OHLCV components closes()
// alone can't provide (e.g. Bop needs open, Obv needs volume).
func opens(candles []exchange.Candle) []float64 {
	vals := make([]float64, len(candles))
	for i, c := range candles {
		vals[i] = c.Open
	}
	return vals
}

func highs(candles []exchange.Candle) []float64 {
	vals := make([]float64, len(candles))
	for i, c := range candles {
		vals[i] = c.High
	}
	return vals
}

func lows(candles []exchange.Candle) []float64 {
	vals := make([]float64, len(candles))
	for i, c := range candles {
		vals[i] = c.Low
	}
	return vals
}

func volumes(candles []exchange.Candle) []float64 {
	vals := make([]float64, len(candles))
	for i, c := range candles {
		vals[i] = c.Volume
	}
	return vals
}

func toTalibMAType(m MAType) talib.MaType {
	if m == MATypeEMA {
		return talib.EMA
	}
	return talib.SMA
}

// --- Additional indicators (non-frozen, added post-ADR-005) ---
//
// These wrap the rest of go-talib's genuine technical/candle indicator
// surface (everything except the generic array-math helpers like Add/Sub/Sqrt
// and the two-independent-series statistic functions Beta/Correl - see
// IndicatorProvider's doc comment for the full rationale). Every MAType
// parameter go-talib exposes on these functions (Ma, Apo, Ppo, MacdExt,
// Stoch, StochF, StochRsi) is hardcoded to toTalibMAType(MATypeSMA) here,
// matching BollingerBands' Lua-side default before this change (the ind.*
// bollinger closure hardcodes indicators.MATypeSMA too) - exposing a MAType
// choice for every one of these would multiply the Lua binding surface for
// marginal benefit; SMA is the conventional default in TA-Lib itself.

func (a *TalibAdapter) DEMA(candles []exchange.Candle, period int) []float64 {
	return talib.Dema(closes(candles), period)
}

func (a *TalibAdapter) HTTrendline(candles []exchange.Candle) []float64 {
	return talib.HtTrendline(closes(candles))
}

func (a *TalibAdapter) KAMA(candles []exchange.Candle, period int) []float64 {
	return talib.Kama(closes(candles), period)
}

func (a *TalibAdapter) MA(candles []exchange.Candle, period int) []float64 {
	return talib.Ma(closes(candles), period, toTalibMAType(MATypeSMA))
}

func (a *TalibAdapter) MAMA(candles []exchange.Candle, fastLimit float64, slowLimit float64) (mama []float64, fama []float64) {
	return talib.Mama(closes(candles), fastLimit, slowLimit)
}

func (a *TalibAdapter) MidPoint(candles []exchange.Candle, period int) []float64 {
	return talib.MidPoint(closes(candles), period)
}

func (a *TalibAdapter) MidPrice(candles []exchange.Candle, period int) []float64 {
	return talib.MidPrice(highs(candles), lows(candles), period)
}

func (a *TalibAdapter) SAR(candles []exchange.Candle, acceleration float64, maximum float64) []float64 {
	return talib.Sar(highs(candles), lows(candles), acceleration, maximum)
}

func (a *TalibAdapter) SARExt(candles []exchange.Candle, startValue float64, offsetOnReverse float64, accelerationInitLong float64, accelerationLong float64, accelerationMaxLong float64, accelerationInitShort float64, accelerationShort float64, accelerationMaxShort float64) []float64 {
	return talib.SarExt(highs(candles), lows(candles), startValue, offsetOnReverse, accelerationInitLong, accelerationLong, accelerationMaxLong, accelerationInitShort, accelerationShort, accelerationMaxShort)
}

func (a *TalibAdapter) T3(candles []exchange.Candle, period int, vFactor float64) []float64 {
	return talib.T3(closes(candles), period, vFactor)
}

func (a *TalibAdapter) TEMA(candles []exchange.Candle, period int) []float64 {
	return talib.Tema(closes(candles), period)
}

func (a *TalibAdapter) TRIMA(candles []exchange.Candle, period int) []float64 {
	return talib.Trima(closes(candles), period)
}

func (a *TalibAdapter) WMA(candles []exchange.Candle, period int) []float64 {
	return talib.Wma(closes(candles), period)
}

func (a *TalibAdapter) ADX(candles []exchange.Candle, period int) []float64 {
	return talib.Adx(highs(candles), lows(candles), closes(candles), period)
}

func (a *TalibAdapter) ADXR(candles []exchange.Candle, period int) []float64 {
	return talib.AdxR(highs(candles), lows(candles), closes(candles), period)
}

func (a *TalibAdapter) APO(candles []exchange.Candle, fastPeriod int, slowPeriod int) []float64 {
	return talib.Apo(closes(candles), fastPeriod, slowPeriod, toTalibMAType(MATypeSMA))
}

func (a *TalibAdapter) Aroon(candles []exchange.Candle, period int) (down []float64, up []float64) {
	return talib.Aroon(highs(candles), lows(candles), period)
}

func (a *TalibAdapter) AroonOsc(candles []exchange.Candle, period int) []float64 {
	return talib.AroonOsc(highs(candles), lows(candles), period)
}

func (a *TalibAdapter) BOP(candles []exchange.Candle) []float64 {
	return talib.Bop(opens(candles), highs(candles), lows(candles), closes(candles))
}

func (a *TalibAdapter) CMO(candles []exchange.Candle, period int) []float64 {
	return talib.Cmo(closes(candles), period)
}

func (a *TalibAdapter) CCI(candles []exchange.Candle, period int) []float64 {
	return talib.Cci(highs(candles), lows(candles), closes(candles), period)
}

func (a *TalibAdapter) DX(candles []exchange.Candle, period int) []float64 {
	return talib.Dx(highs(candles), lows(candles), closes(candles), period)
}

func (a *TalibAdapter) MACDExt(candles []exchange.Candle, fastPeriod int, slowPeriod int, signalPeriod int) (macd []float64, signal []float64, hist []float64) {
	return talib.MacdExt(closes(candles), fastPeriod, toTalibMAType(MATypeSMA), slowPeriod, toTalibMAType(MATypeSMA), signalPeriod, toTalibMAType(MATypeSMA))
}

func (a *TalibAdapter) MACDFix(candles []exchange.Candle, signalPeriod int) (macd []float64, signal []float64, hist []float64) {
	return talib.MacdFix(closes(candles), signalPeriod)
}

func (a *TalibAdapter) MinusDI(candles []exchange.Candle, period int) []float64 {
	return talib.MinusDI(highs(candles), lows(candles), closes(candles), period)
}

func (a *TalibAdapter) MinusDM(candles []exchange.Candle, period int) []float64 {
	return talib.MinusDM(highs(candles), lows(candles), period)
}

func (a *TalibAdapter) MFI(candles []exchange.Candle, period int) []float64 {
	return talib.Mfi(highs(candles), lows(candles), closes(candles), volumes(candles), period)
}

func (a *TalibAdapter) Mom(candles []exchange.Candle, period int) []float64 {
	return talib.Mom(closes(candles), period)
}

func (a *TalibAdapter) PlusDI(candles []exchange.Candle, period int) []float64 {
	return talib.PlusDI(highs(candles), lows(candles), closes(candles), period)
}

func (a *TalibAdapter) PlusDM(candles []exchange.Candle, period int) []float64 {
	return talib.PlusDM(highs(candles), lows(candles), period)
}

func (a *TalibAdapter) PPO(candles []exchange.Candle, fastPeriod int, slowPeriod int) []float64 {
	return talib.Ppo(closes(candles), fastPeriod, slowPeriod, toTalibMAType(MATypeSMA))
}

func (a *TalibAdapter) ROCP(candles []exchange.Candle, period int) []float64 {
	return talib.Rocp(closes(candles), period)
}

func (a *TalibAdapter) ROC(candles []exchange.Candle, period int) []float64 {
	return talib.Roc(closes(candles), period)
}

func (a *TalibAdapter) ROCR(candles []exchange.Candle, period int) []float64 {
	return talib.Rocr(closes(candles), period)
}

func (a *TalibAdapter) ROCR100(candles []exchange.Candle, period int) []float64 {
	return talib.Rocr100(closes(candles), period)
}

func (a *TalibAdapter) Stoch(candles []exchange.Candle, fastKPeriod int, slowKPeriod int, slowDPeriod int) (k []float64, d []float64) {
	return talib.Stoch(highs(candles), lows(candles), closes(candles), fastKPeriod, slowKPeriod, toTalibMAType(MATypeSMA), slowDPeriod, toTalibMAType(MATypeSMA))
}

func (a *TalibAdapter) StochF(candles []exchange.Candle, fastKPeriod int, fastDPeriod int) (k []float64, d []float64) {
	return talib.StochF(highs(candles), lows(candles), closes(candles), fastKPeriod, fastDPeriod, toTalibMAType(MATypeSMA))
}

func (a *TalibAdapter) StochRSI(candles []exchange.Candle, period int, fastKPeriod int, fastDPeriod int) (k []float64, d []float64) {
	return talib.StochRsi(closes(candles), period, fastKPeriod, fastDPeriod, toTalibMAType(MATypeSMA))
}

func (a *TalibAdapter) Trix(candles []exchange.Candle, period int) []float64 {
	return talib.Trix(closes(candles), period)
}

func (a *TalibAdapter) UltOsc(candles []exchange.Candle, period1 int, period2 int, period3 int) []float64 {
	return talib.UltOsc(highs(candles), lows(candles), closes(candles), period1, period2, period3)
}

func (a *TalibAdapter) WillR(candles []exchange.Candle, period int) []float64 {
	return talib.WillR(highs(candles), lows(candles), closes(candles), period)
}

func (a *TalibAdapter) AD(candles []exchange.Candle) []float64 {
	return talib.Ad(highs(candles), lows(candles), closes(candles), volumes(candles))
}

func (a *TalibAdapter) ADOsc(candles []exchange.Candle, fastPeriod int, slowPeriod int) []float64 {
	return talib.AdOsc(highs(candles), lows(candles), closes(candles), volumes(candles), fastPeriod, slowPeriod)
}

func (a *TalibAdapter) OBV(candles []exchange.Candle) []float64 {
	return talib.Obv(closes(candles), volumes(candles))
}

func (a *TalibAdapter) NATR(candles []exchange.Candle, period int) []float64 {
	return talib.Natr(highs(candles), lows(candles), closes(candles), period)
}

func (a *TalibAdapter) TRange(candles []exchange.Candle) []float64 {
	return talib.TRange(highs(candles), lows(candles), closes(candles))
}

func (a *TalibAdapter) AvgPrice(candles []exchange.Candle) []float64 {
	return talib.AvgPrice(opens(candles), highs(candles), lows(candles), closes(candles))
}

func (a *TalibAdapter) MedPrice(candles []exchange.Candle) []float64 {
	return talib.MedPrice(highs(candles), lows(candles))
}

func (a *TalibAdapter) TypPrice(candles []exchange.Candle) []float64 {
	return talib.TypPrice(highs(candles), lows(candles), closes(candles))
}

func (a *TalibAdapter) WclPrice(candles []exchange.Candle) []float64 {
	return talib.WclPrice(highs(candles), lows(candles), closes(candles))
}

func (a *TalibAdapter) HTDcPeriod(candles []exchange.Candle) []float64 {
	return talib.HtDcPeriod(closes(candles))
}

func (a *TalibAdapter) HTDcPhase(candles []exchange.Candle) []float64 {
	return talib.HtDcPhase(closes(candles))
}

func (a *TalibAdapter) HTPhasor(candles []exchange.Candle) (inphase []float64, quadrature []float64) {
	return talib.HtPhasor(closes(candles))
}

func (a *TalibAdapter) HTSine(candles []exchange.Candle) (sine []float64, leadSine []float64) {
	return talib.HtSine(closes(candles))
}

func (a *TalibAdapter) HTTrendMode(candles []exchange.Candle) []float64 {
	return talib.HtTrendMode(closes(candles))
}

func (a *TalibAdapter) LinearReg(candles []exchange.Candle, period int) []float64 {
	return talib.LinearReg(closes(candles), period)
}

func (a *TalibAdapter) LinearRegAngle(candles []exchange.Candle, period int) []float64 {
	return talib.LinearRegAngle(closes(candles), period)
}

func (a *TalibAdapter) LinearRegIntercept(candles []exchange.Candle, period int) []float64 {
	return talib.LinearRegIntercept(closes(candles), period)
}

func (a *TalibAdapter) LinearRegSlope(candles []exchange.Candle, period int) []float64 {
	return talib.LinearRegSlope(closes(candles), period)
}

func (a *TalibAdapter) StdDev(candles []exchange.Candle, period int, nbDev float64) []float64 {
	return talib.StdDev(closes(candles), period, nbDev)
}

func (a *TalibAdapter) TSF(candles []exchange.Candle, period int) []float64 {
	return talib.Tsf(closes(candles), period)
}

func (a *TalibAdapter) Var(candles []exchange.Candle, period int) []float64 {
	return talib.Var(closes(candles), period)
}

func (a *TalibAdapter) Max(candles []exchange.Candle, period int) []float64 {
	return talib.Max(closes(candles), period)
}

func (a *TalibAdapter) MaxIndex(candles []exchange.Candle, period int) []float64 {
	return talib.MaxIndex(closes(candles), period)
}

func (a *TalibAdapter) Min(candles []exchange.Candle, period int) []float64 {
	return talib.Min(closes(candles), period)
}

func (a *TalibAdapter) MinIndex(candles []exchange.Candle, period int) []float64 {
	return talib.MinIndex(closes(candles), period)
}

func (a *TalibAdapter) MinMax(candles []exchange.Candle, period int) (min []float64, max []float64) {
	return talib.MinMax(closes(candles), period)
}

func (a *TalibAdapter) MinMaxIndex(candles []exchange.Candle, period int) (minIdx []float64, maxIdx []float64) {
	return talib.MinMaxIndex(closes(candles), period)
}

func (a *TalibAdapter) Sum(candles []exchange.Candle, period int) []float64 {
	return talib.Sum(closes(candles), period)
}
