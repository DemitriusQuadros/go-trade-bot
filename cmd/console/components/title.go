package components

import (
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

func Title() *widgets.Paragraph {
	p := widgets.NewParagraph()
	p.Text = "TradeBot Console dashboard\nPress <Escape> to quit."
	p.SetRect(0, 0, 50, 7)
	p.Border = false
	p.TextStyle.Fg = ui.ColorRed
	return p
}
