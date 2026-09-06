package components

import (
	"context"
	"fmt"

	"go-trade-bot/cmd/console/apiclient"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// AccountSummary replaces account.go's role. Fetches account and open signals via apiclient.
func AccountSummary(ctx context.Context, api *apiclient.Client) ui.Drawable {
	if api == nil {
		p := widgets.NewParagraph()
		p.Title = "Account"
		p.Text = "API client not initialized"
		return p
	}

	acc, err := api.GetAccount(ctx)
	if err != nil {
		return Error(err)
	}

	openSignals, err := api.GetOpenSignals(ctx)
	openCount := 0
	if err == nil {
		openCount = len(openSignals)
	}

	p := widgets.NewParagraph()
	p.Title = "Account"
	p.Text = fmt.Sprintf(
		"Balance:   $%0.2f %s\nAvailable: %d orders\nDaily P&L: N/A (endpoint gap)\nOpen Pos.: %d",
		acc.Amount,
		acc.Currency,
		acc.AvailableOrders,
		openCount,
	)
	p.BorderStyle.Fg = ui.ColorCyan

	return p
}
