package main

import (
	dependencies "go-trade-bot/cmd/console/dependencies"
	"go-trade-bot/cmd/console/pages"
	"log"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

func main() {
	dependencies := dependencies.Init()
	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()

	header := widgets.NewParagraph()
	header.Text = "Press ESC to quit, Press h or l to switch tabs"
	header.SetRect(0, 0, 50, 1)
	header.Border = false
	header.TextStyle.Bg = ui.ColorRed

	tabPane := widgets.NewTabPane("Status", "Performance", "Open Signals")
	tabPane.SetRect(0, 1, 50, 2)
	tabPane.Border = true
	tabPane.BorderStyle.Fg = ui.ColorGreen

	status := pages.NewStatusPage().Set(header, tabPane, dependencies)
	performance := pages.NewPerformancePage().Set(header, tabPane, dependencies)
	opensignal := pages.NewOpenOrdersPage().Set(header, tabPane, dependencies)

	renderTab := func() {
		switch tabPane.ActiveTabIndex {
		case 0:
			opensignal.StopSync()
			ui.Render(status.Render())
		case 1:
			opensignal.StopSync()
			ui.Render(performance.Render())
		case 2:
			opensignal.StartSync()
			ui.Render(opensignal.Render())
		}
	}

	ui.Render(header, tabPane, status.Render())

	uiEvents := ui.PollEvents()

	for {
		e := <-uiEvents
		switch e.ID {
		case "<Escape>":
			return
		case "h":
			tabPane.FocusLeft()
			ui.Clear()
			ui.Render(header, tabPane)
			renderTab()
		case "l":
			tabPane.FocusRight()
			ui.Clear()
			ui.Render(header, tabPane)
			renderTab()
		}
	}
}
