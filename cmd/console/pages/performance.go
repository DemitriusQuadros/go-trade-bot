package pages

import (
	"go-trade-bot/cmd/console/components"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type PerformancePage struct {
	Header       *widgets.Paragraph
	TabPane      *widgets.TabPane
	Dependencies *dependencies.Dependencies
}

func NewPerformancePage() *PerformancePage {
	return &PerformancePage{}
}

func (p *PerformancePage) Set(
	header *widgets.Paragraph,
	tabPane *widgets.TabPane,
	dependencies *dependencies.Dependencies,
) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = dependencies
	return p
}

func (p *PerformancePage) Render() ui.Drawable {
	performance := ui.NewGrid()
	termWidth, termHeight := ui.TerminalDimensions()

	performance.SetRect(0, 0, termWidth, termHeight)
	performance.Set(
		ui.NewRow(1.0/16, p.Header),
		ui.NewRow(1.0/16, p.TabPane),
		components.Totalizers(p.Dependencies),
	)

	return performance
}

func (p *PerformancePage) HandleEvent(event interface{}) error {
	return nil
}

func (p *PerformancePage) StartSync() {
	return
}

func (p *PerformancePage) StopSync() {
	return
}
