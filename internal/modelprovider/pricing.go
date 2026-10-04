package modelprovider

import "strings"

// modelPrice is a per-1M-token list price in USD.
type modelPrice struct {
	prefix string
	input  float64
	output float64
}

// priceTable is a small, static, HAND-MAINTAINED list-price table (USD per
// 1M tokens, input/output), matched by longest model-name prefix. It is not
// fetched from any vendor API - UPDATE IT BY HAND when vendors change
// prices or new model families ship. It exists only to enforce each agent's
// DailyBudgetUSD (agents-platform A-01 §3), so erring high is the safe
// direction: an over-estimate halts an agent early, an under-estimate lets
// it overspend.
var priceTable = []modelPrice{
	// Anthropic
	{prefix: "claude-opus", input: 15.00, output: 75.00},
	{prefix: "claude-sonnet", input: 3.00, output: 15.00},
	{prefix: "claude-haiku", input: 1.00, output: 5.00},
	{prefix: "claude-3-5-haiku", input: 0.80, output: 4.00},
	{prefix: "claude-3-haiku", input: 0.25, output: 1.25},
	// Google
	{prefix: "gemini-2.5-pro", input: 2.50, output: 15.00}, // >200k-context tier, conservative
	{prefix: "gemini-2.5-flash-lite", input: 0.10, output: 0.40},
	{prefix: "gemini-2.5-flash", input: 0.30, output: 2.50},
	{prefix: "gemini-2.0-flash", input: 0.15, output: 0.60},
	{prefix: "gemini-3", input: 4.00, output: 18.00},
	{prefix: "gemini", input: 2.50, output: 15.00},
}

// fallbackPrice is used for any model not matched above. Deliberately the
// most expensive tier in the table - never 0, because a zero price would
// silently make every budget infinite.
var fallbackPrice = modelPrice{prefix: "", input: 15.00, output: 75.00}

// DefaultModelFor returns the adapter's built-in default model for a
// provider name, or "" if unknown - used to price calls made with an empty
// model setting.
func DefaultModelFor(provider string) string {
	switch provider {
	case "anthropic":
		return defaultAnthropicModel
	case "gemini":
		return defaultGeminiModel
	}
	return ""
}

// EstimateCostUSD estimates the USD cost of u for the given provider/model
// using priceTable (see its doc comment - hand-maintained, conservative).
func EstimateCostUSD(provider, model string, u Usage) float64 {
	if model == "" {
		model = DefaultModelFor(provider)
	}
	price := lookupPrice(strings.ToLower(model))
	return float64(u.InputTokens)/1e6*price.input + float64(u.OutputTokens)/1e6*price.output
}

func lookupPrice(model string) modelPrice {
	best := fallbackPrice
	bestLen := -1
	for _, p := range priceTable {
		if strings.HasPrefix(model, p.prefix) && len(p.prefix) > bestLen {
			best = p
			bestLen = len(p.prefix)
		}
	}
	return best
}
