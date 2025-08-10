package builder

import (
	"go-trade-bot/cmd/console/components"
	dependencies "go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
)

type Builder struct {
	Dependencies *dependencies.Dependencies
}

func New(dependencies *dependencies.Dependencies) *Builder {
	return &Builder{
		Dependencies: dependencies,
	}
}
func (b Builder) BuildConsole() ui.Drawable {
	grid := ui.NewGrid()
	termWidth, termHeight := ui.TerminalDimensions()
	grid.SetRect(0, 0, termWidth, termHeight)
	grid.Set(
		ui.NewRow(1.0/4,
			ui.NewCol(1.0/3, components.Title()),
			ui.NewCol(1.0/3, components.StrategyList(b.Dependencies)),
			ui.NewCol(1.0/3, components.Account(b.Dependencies)),
		),
		components.Totalizers(b.Dependencies),
	)
	return grid
}
