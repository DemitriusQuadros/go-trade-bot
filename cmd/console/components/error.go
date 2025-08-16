package components

import (
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

func Error(err error) *widgets.Paragraph {
	errWidget := widgets.NewParagraph()
	errWidget.Text = "Failed to load: " + err.Error()
	errWidget.SetRect(0, 0, 50, 1)
	errWidget.Border = false
	errWidget.TextStyle.Bg = ui.ColorRed

	return errWidget

}
