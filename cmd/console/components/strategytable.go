package components

import (
	"context"
	"fmt"
	"strings"

	"go-trade-bot/cmd/console/apiclient"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// ModeBadge returns the badge string and color for a strategy mode.
func ModeBadge(mode string) (string, ui.Color) {
	switch strings.ToLower(mode) {
	case "live":
		return "[LIVE]", ui.ColorRed
	case "paper":
		return "[PAPER]", ui.ColorYellow
	case "dryrun":
		return "[DRY]", ui.ColorCyan
	case "backtest":
		return "[BT]", ui.ColorBlue
	default:
		return fmt.Sprintf("[%s]", strings.ToUpper(mode)), ui.ColorWhite
	}
}

// StrategyTableCompact renders the compact active-strategies table for Page 1.
func StrategyTableCompact(ctx context.Context, api *apiclient.Client) ui.Drawable {
	if api == nil {
		p := widgets.NewParagraph()
		p.Title = "Active Strategies"
		p.Text = "API client not initialized"
		return p
	}

	strategies, err := api.ListStrategies(ctx)
	if err != nil {
		return Error(err)
	}

	if len(strategies) == 0 {
		p := widgets.NewParagraph()
		p.Title = "Active Strategies"
		p.Text = "No strategies configured"
		p.BorderStyle.Fg = ui.ColorYellow
		return p
	}

	table := widgets.NewTable()
	table.Title = "Active Strategies"
	table.Rows = make([][]string, len(strategies)+1)
	table.Rows[0] = []string{"Name", "Mode", "Status"}

	for i, s := range strategies {
		badge, _ := ModeBadge(s.Mode)
		statusText := s.Status
		if statusText == "productive" {
			statusText = "running"
		}
		table.Rows[i+1] = []string{s.Name, badge, statusText}
	}
	table.TextStyle = ui.NewStyle(ui.ColorWhite)
	table.RowStyles[0] = ui.NewStyle(ui.ColorYellow, ui.ColorClear, ui.ModifierBold)
	return table
}

// StrategyTableFull renders the full strategy management table for Page 2.
func StrategyTableFull(ctx context.Context, api *apiclient.Client) (*widgets.Table, []apiclient.StrategyView) {
	table := widgets.NewTable()
	table.Title = "Strategies"

	if api == nil {
		table.Rows = [][]string{
			{"Name", "Algorithm", "Symbols", "Cycle", "Mode", "Last Exec", "Status", "Open Sigs"},
			{"No API client available", "", "", "", "", "", "", ""},
		}
		return table, nil
	}

	strategies, err := api.ListStrategies(ctx)
	if err != nil {
		table.Rows = [][]string{
			{"Name", "Algorithm", "Symbols", "Cycle", "Mode", "Last Exec", "Status", "Open Sigs"},
			{fmt.Sprintf("Failed to load: %v", err), "", "", "", "", "", "", ""},
		}
		return table, nil
	}

	if len(strategies) == 0 {
		table.Rows = [][]string{
			{"Name", "Algorithm", "Symbols", "Cycle", "Mode", "Last Exec", "Status", "Open Sigs"},
			{"No strategies configured", "", "", "", "", "", "", ""},
		}
		return table, strategies
	}

	// Fetch open signals to count per strategy
	signals, _ := api.GetOpenSignals(ctx)
	openCounts := make(map[uint]int)
	for _, sig := range signals {
		openCounts[sig.StrategyID]++
	}

	table.Rows = make([][]string, len(strategies)+1)
	table.Rows[0] = []string{"Name", "Algorithm", "Symbols", "Cycle", "Mode", "Last Exec", "Status", "Open Sigs"}

	for i, s := range strategies {
		algo := s.StrategyName
		if algo == "" {
			algo = s.Algorithm
		}
		symbols := strings.Join(s.MonitoredSymbols, ", ")
		cycle := fmt.Sprintf("%dm", s.StrategyConfiguration.Cycle)
		mode := strings.ToUpper(s.Mode)

		lastExec := s.UpdatedAt.Format("2006-01-02 15:04:05")
		if s.UpdatedAt.IsZero() {
			lastExec = "-"
		}
		status := s.Status
		if status == "productive" {
			status = "ok"
		}
		openCount := fmt.Sprintf("%d", openCounts[s.ID])

		table.Rows[i+1] = []string{s.Name, algo, symbols, cycle, mode, lastExec, status, openCount}
	}

	table.TextStyle = ui.NewStyle(ui.ColorWhite)
	table.RowStyles[0] = ui.NewStyle(ui.ColorYellow, ui.ColorClear, ui.ModifierBold)
	return table, strategies
}
