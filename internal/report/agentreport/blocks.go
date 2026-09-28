// Package agentreport validates and renders agent reports (agents-platform
// A-01 §5): a report is a list of typed blocks, rendered server-side into a
// single self-contained Console Pro HTML document.
//
// Safety model: every piece of model-supplied text reaches the output only
// through html/template's contextual auto-escaping - this package never
// wraps model input in template.HTML/JS/CSS/URL. Data-bearing blocks
// (kpi_grid, equity_chart, trade_table, code_diff by version id) carry only
// REFERENCES (ids); the numbers are resolved by the renderer from the DB via
// DataSource, and any numbers the model put in the block data are ignored.
package agentreport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Block is one typed report block as written by the model.
type Block struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Limits (A-01 §4.6).
const (
	MaxBlocks        = 40
	MaxRenderedBytes = 1 << 20 // 1 MB
	MaxTradeRows     = 50
	maxTextLen       = 20000
	maxTableCols     = 12
	maxTableRows     = 200
	maxRecItems      = 30
	maxLiveDays      = 365
	maxDiffSourceLen = 200000
)

// Block type names.
const (
	TypeSummary        = "summary"
	TypeCallout        = "callout"
	TypeKPIGrid        = "kpi_grid"
	TypeEquityChart    = "equity_chart"
	TypeTradeTable     = "trade_table"
	TypeCodeDiff       = "code_diff"
	TypeRecommendation = "recommendation"
	TypeTextTable      = "text_table"
)

// AllowedTypes lists every accepted block type, in documentation order.
var AllowedTypes = []string{
	TypeSummary, TypeCallout, TypeKPIGrid, TypeEquityChart, TypeTradeTable,
	TypeCodeDiff, TypeRecommendation, TypeTextTable,
}

// AllowedMetrics are the kpi_grid metric keys.
var AllowedMetrics = []string{"sharpe", "max_drawdown", "win_rate", "profit_factor", "total_trades", "net_pnl"}

// Source kinds for data-bearing blocks.
const (
	SourceBacktestRun  = "backtest_run"
	SourceStrategyLive = "strategy_live"
)

// Source is a reference to DB-resident data. The model supplies only these
// ids; values are resolved by the renderer.
type Source struct {
	Kind       string `json:"kind"`
	ID         uint   `json:"id,omitempty"`          // backtest_run
	StrategyID uint   `json:"strategy_id,omitempty"` // strategy_live
	Days       int    `json:"days,omitempty"`        // strategy_live, default 30
}

type summaryData struct {
	Text string `json:"text"`
}

type calloutData struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Text     string `json:"text"`
}

type kpiGridData struct {
	Source  Source   `json:"source"`
	Metrics []string `json:"metrics"`
}

type equityChartData struct {
	Source Source `json:"source"`
}

type tradeTableData struct {
	Source Source `json:"source"`
	Limit  int    `json:"limit"`
}

type codeDiffData struct {
	StrategyID    uint    `json:"strategy_id"`
	FromVersionID *uint   `json:"from_version_id,omitempty"`
	ToVersionID   *uint   `json:"to_version_id,omitempty"`
	FromSource    *string `json:"from_source,omitempty"`
	ToSource      *string `json:"to_source,omitempty"`
}

type recommendationItem struct {
	Title     string `json:"title"`
	Rationale string `json:"rationale"`
	Action    string `json:"action,omitempty"`
}

type recommendationData struct {
	Items []recommendationItem `json:"items"`
}

type textTableData struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// ParseBlocks decodes a raw JSON array of blocks.
func ParseBlocks(raw json.RawMessage) ([]Block, error) {
	var blocks []Block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("blocks must be a JSON array of {type, data} objects: %w", err)
	}
	return blocks, nil
}

// Validate checks every block's type and data shape without touching the
// DB. The error text is written for the model to act on.
func Validate(blocks []Block) error {
	if len(blocks) == 0 {
		return fmt.Errorf("a report needs at least one block")
	}
	if len(blocks) > MaxBlocks {
		return fmt.Errorf("a report may have at most %d blocks, got %d", MaxBlocks, len(blocks))
	}
	for i, b := range blocks {
		if err := validateBlock(b); err != nil {
			return fmt.Errorf("block %d (%q): %w", i, b.Type, err)
		}
	}
	return nil
}

func decodeStrict(data json.RawMessage, v any) error {
	if len(data) == 0 {
		return fmt.Errorf("missing data")
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("invalid data: %v", err)
	}
	return nil
}

func validateSource(s Source) error {
	switch s.Kind {
	case SourceBacktestRun:
		if s.ID == 0 {
			return fmt.Errorf("source kind %q requires a non-zero id", SourceBacktestRun)
		}
	case SourceStrategyLive:
		if s.StrategyID == 0 {
			return fmt.Errorf("source kind %q requires a non-zero strategy_id", SourceStrategyLive)
		}
		if s.Days < 0 || s.Days > maxLiveDays {
			return fmt.Errorf("source days must be between 1 and %d", maxLiveDays)
		}
	default:
		return fmt.Errorf("source.kind must be %q or %q, got %q", SourceBacktestRun, SourceStrategyLive, s.Kind)
	}
	return nil
}

func validateBlock(b Block) error {
	switch b.Type {
	case TypeSummary:
		var d summaryData
		if err := decodeStrict(b.Data, &d); err != nil {
			return err
		}
		if strings.TrimSpace(d.Text) == "" {
			return fmt.Errorf("summary.text is required")
		}
		if len(d.Text) > maxTextLen {
			return fmt.Errorf("summary.text exceeds %d characters", maxTextLen)
		}
	case TypeCallout:
		var d calloutData
		if err := decodeStrict(b.Data, &d); err != nil {
			return err
		}
		if d.Severity != "info" && d.Severity != "warning" && d.Severity != "critical" {
			return fmt.Errorf("callout.severity must be info, warning or critical")
		}
		if strings.TrimSpace(d.Text) == "" && strings.TrimSpace(d.Title) == "" {
			return fmt.Errorf("callout needs a title or text")
		}
		if len(d.Text) > maxTextLen {
			return fmt.Errorf("callout.text exceeds %d characters", maxTextLen)
		}
	case TypeKPIGrid:
		var d kpiGridData
		if err := decodeStrict(b.Data, &d); err != nil {
			return err
		}
		if err := validateSource(d.Source); err != nil {
			return err
		}
		if len(d.Metrics) == 0 {
			return fmt.Errorf("kpi_grid.metrics must list at least one of %s", strings.Join(AllowedMetrics, ", "))
		}
		for _, m := range d.Metrics {
			if !contains(AllowedMetrics, m) {
				return fmt.Errorf("unknown kpi_grid metric %q; allowed: %s", m, strings.Join(AllowedMetrics, ", "))
			}
		}
	case TypeEquityChart:
		var d equityChartData
		if err := decodeStrict(b.Data, &d); err != nil {
			return err
		}
		return validateSource(d.Source)
	case TypeTradeTable:
		var d tradeTableData
		if err := decodeStrict(b.Data, &d); err != nil {
			return err
		}
		if err := validateSource(d.Source); err != nil {
			return err
		}
		if d.Limit < 0 || d.Limit > MaxTradeRows {
			return fmt.Errorf("trade_table.limit must be between 1 and %d", MaxTradeRows)
		}
	case TypeCodeDiff:
		var d codeDiffData
		if err := decodeStrict(b.Data, &d); err != nil {
			return err
		}
		if d.StrategyID == 0 {
			return fmt.Errorf("code_diff.strategy_id is required")
		}
		usesSource := d.FromSource != nil || d.ToSource != nil
		usesVersion := d.FromVersionID != nil || d.ToVersionID != nil
		if usesSource && usesVersion {
			return fmt.Errorf("code_diff takes either from_version_id/to_version_id or from_source/to_source, not both")
		}
		if usesSource && (d.FromSource == nil || d.ToSource == nil) {
			return fmt.Errorf("code_diff with sources needs both from_source and to_source")
		}
		if usesSource && (len(*d.FromSource) > maxDiffSourceLen || len(*d.ToSource) > maxDiffSourceLen) {
			return fmt.Errorf("code_diff sources may be at most %d characters each", maxDiffSourceLen)
		}
	case TypeRecommendation:
		var d recommendationData
		if err := decodeStrict(b.Data, &d); err != nil {
			return err
		}
		if len(d.Items) == 0 || len(d.Items) > maxRecItems {
			return fmt.Errorf("recommendation.items must have 1..%d items", maxRecItems)
		}
		for i, it := range d.Items {
			if strings.TrimSpace(it.Title) == "" {
				return fmt.Errorf("recommendation.items[%d].title is required", i)
			}
		}
	case TypeTextTable:
		var d textTableData
		if err := decodeStrict(b.Data, &d); err != nil {
			return err
		}
		if len(d.Columns) == 0 || len(d.Columns) > maxTableCols {
			return fmt.Errorf("text_table.columns must have 1..%d entries", maxTableCols)
		}
		if len(d.Rows) > maxTableRows {
			return fmt.Errorf("text_table may have at most %d rows", maxTableRows)
		}
		for i, row := range d.Rows {
			if len(row) != len(d.Columns) {
				return fmt.Errorf("text_table.rows[%d] has %d cells, expected %d", i, len(row), len(d.Columns))
			}
		}
	default:
		allowed := append([]string(nil), AllowedTypes...)
		sort.Strings(allowed)
		return fmt.Errorf("unknown block type %q; allowed types: %s", b.Type, strings.Join(allowed, ", "))
	}
	return nil
}

// FirstSummary returns the text of the first summary block, or "".
func FirstSummary(blocks []Block) string {
	for _, b := range blocks {
		if b.Type == TypeSummary {
			var d summaryData
			if json.Unmarshal(b.Data, &d) == nil {
				return d.Text
			}
		}
	}
	return ""
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
