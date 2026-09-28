package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go-trade-bot/app/repository/candle"
	"go-trade-bot/internal/modelprovider"
)

// CandleCoverageReader is the slice of app/repository/candle that
// get_candle_coverage and the deploy gate's insufficient_history message use
// (fix-02 B2). symbol == "" returns every symbol.
type CandleCoverageReader interface {
	Coverage(ctx context.Context, symbol string) ([]candle.Coverage, error)
}

const getCandleCoverageToolName = "get_candle_coverage"

var getCandleCoverageSchema = json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string","description":"e.g. BTCUSDT; omit to list every symbol"}}}`)

// getCandleCoverageTool reports which (symbol, timeframe, date range) the
// platform has stored candles for, so the model picks a backtest range that
// actually has data (run #49 backtested a range with zero candles).
func (u AgentUseCase) getCandleCoverageTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name: getCandleCoverageToolName,
			Description: "List the stored candle history per symbol and timeframe: first and last candle date and candle count. " +
				"Call this before run_backtest / run_optimization and pick a timeframe and date range that has data. " +
				"Omit symbol to list every symbol.",
			InputSchema: getCandleCoverageSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if u.Coverage == nil {
				return "", fmt.Errorf("%s: candle coverage is not available in this process", getCandleCoverageToolName)
			}
			var in struct {
				Symbol string `json:"symbol"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", fmt.Errorf("%s: invalid args: %w", getCandleCoverageToolName, err)
				}
			}
			symbol := strings.ToUpper(strings.TrimSpace(in.Symbol))
			cov, err := u.Coverage.Coverage(ctx, symbol)
			if err != nil {
				return "", fmt.Errorf("%s: %w", getCandleCoverageToolName, err)
			}
			if len(cov) == 0 {
				if symbol != "" {
					return fmt.Sprintf("no candles stored for %s - import history first (POST /api/candles/import)", symbol), nil
				}
				return "no candles stored for any symbol - import history first (POST /api/candles/import)", nil
			}
			var b strings.Builder
			for _, c := range cov {
				fmt.Fprintf(&b, "symbol=%s timeframe=%s from=%s to=%s candles=%d\n",
					c.Symbol, c.Timeframe, c.From.Format("2006-01-02T15:04:05Z"), c.To.Format("2006-01-02T15:04:05Z"), c.Count)
			}
			return b.String(), nil
		},
	}
}

// coverageSuffix is "; available: <coverage>" for symbol, or "" when no
// reader is wired or it fails (best-effort context for error messages).
func coverageSuffix(ctx context.Context, reader any, symbol string) string {
	r, ok := reader.(CandleCoverageReader)
	if !ok || r == nil {
		return ""
	}
	cov, err := r.Coverage(ctx, symbol)
	if err != nil {
		return ""
	}
	return "; available: " + candle.FormatCoverage(cov)
}
