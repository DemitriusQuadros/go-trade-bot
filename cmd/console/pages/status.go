package pages

import (
	"go-trade-bot/cmd/console/components"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type StatusPage struct {
	Header       *widgets.Paragraph
	TabPane      *widgets.TabPane
	Dependencies *dependencies.Dependencies
}

func NewStatusPage() *StatusPage {
	return &StatusPage{}
}

func (p *StatusPage) Set(
	header *widgets.Paragraph,
	tabPane *widgets.TabPane,
	dependencies *dependencies.Dependencies,
) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = dependencies
	return p
}

func (p *StatusPage) Render() ui.Drawable {
	status := ui.NewGrid()
	termWidth, termHeight := ui.TerminalDimensions()

	status.SetRect(0, 0, termWidth, termHeight)
	status.Set(
		ui.NewRow(1.0/16, p.Header),
		ui.NewRow(1.0/16, p.TabPane),
		ui.NewRow(1.0/4,
			ui.NewCol(1.0/3, components.StrategyList(p.Dependencies)),
			ui.NewCol(1.0/3, components.Account(p.Dependencies)),
		),
	)

	return status
}

func (p *StatusPage) HandleEvent(event interface{}) error {
	return nil
}

func (p *StatusPage) StartSync() {
	return
}

func (p *StatusPage) StopSync() {
	return
}
