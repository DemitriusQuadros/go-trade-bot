package components

import (
	"context"
	"fmt"
	repository "go-trade-bot/app/repository/strategy"
	"go-trade-bot/cmd/console/dependencies"
	"strconv"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

func Totalizers(d *dependencies.Dependencies) ui.GridItem {
	return ui.NewRow(1.0,
		ui.NewCol(1.0, buildStrategyPerformanceBySymbol(d)),
	)
}

func buildStrategyPerformanceBySymbol(d *dependencies.Dependencies) ui.Drawable {
	strategyRepository := repository.NewStrategyRepository(d.Db)
	items := strategyRepository.GetStrategyPerformanceBySymbol(context.Background())

	if items == nil {
		p := widgets.NewParagraph()
		p.Text = "No strategy performance data available."
		p.SetRect(0, 0, 50, 10)
		p.Border = true
		p.TextStyle.Fg = ui.ColorYellow
		return p
	}

	table := widgets.NewTable()
	table.Rows = [][]string{{"Strategy", "Symbol", "Profit/Loss", "Trades"}}
	for _, i := range items {
		table.Rows = append(table.Rows, []string{
			i.Name,
			i.Symbol,
			fmt.Sprintf("%.2f", i.Profit),
			strconv.Itoa(i.Trades),
		})
	}
	table.TextStyle = ui.NewStyle(ui.ColorWhite)
	table.BorderStyle = ui.NewStyle(ui.ColorGreen)
	table.RowSeparator = true
	table.SetRect(0, 10, 50, 20)
	table.Border = true
	table.BorderStyle.Fg = ui.ColorCyan
	table.Title = "Strategy Performance by Symbol"
	table.TitleStyle = ui.NewStyle(ui.ColorYellow)
	table.TextAlignment = ui.AlignCenter

	return table
}
