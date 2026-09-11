package engine

// strategyTimeframeRequirements lists any supplementary non-primary timeframes
// required by a strategy (e.g. Scalping needs 15m candles for its trend filter).
var strategyTimeframeRequirements = map[string][]string{
	"scalping": {"15m"},
}
