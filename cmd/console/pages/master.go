package pages

import (
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type Page interface {
	Set(header *widgets.Paragraph, tabPane *widgets.TabPane, dependencies *dependencies.Dependencies) Page
	Render() ui.Drawable
	StopSync()
	StartSync()
}
