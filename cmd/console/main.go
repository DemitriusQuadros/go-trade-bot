package main

import (
	"log"

	"go-trade-bot/cmd/console/dependencies"
	"go-trade-bot/cmd/console/pages"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

func main() {
	deps := dependencies.Init()
	StartDebugMetricsServer(deps.Cfg.Console.MetricsEnabled, deps.Cfg.Console.MetricsPort)

	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()

	header := widgets.NewParagraph()
	header.Border = true

	tabPane := widgets.NewTabPane(
		"Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log",
	)
	tabPane.Border = true
	tabPane.BorderStyle.Fg = ui.ColorGreen

	pageList := pages.RegisterPages(header, tabPane, deps)
	activePage := pageList[0]
	activePage.StartSync()

	ui.Render(header, tabPane, activePage.Render())
	SetRenderLoopHealthy(1)

	uiEvents := ui.PollEvents()

	for e := range uiEvents {
		SetRenderLoopHealthy(1)

		switch {
		case e.ID == "<Escape>" || e.ID == "q":
			activePage.StopSync()
			return
		case e.ID == "h" || e.ID == "<Left>":
			switchTab(tabPane, pageList, &activePage, tabPane.ActiveTabIndex-1)
		case e.ID == "l" || e.ID == "<Right>":
			switchTab(tabPane, pageList, &activePage, tabPane.ActiveTabIndex+1)
		case isFunctionKey(e.ID):
			targetIdx := functionKeyIndex(e.ID)
			if targetIdx >= 0 && targetIdx < len(pageList) {
				switchTab(tabPane, pageList, &activePage, targetIdx)
			}
		default:
			prevTab := tabPane.ActiveTabIndex
			if handler, ok := activePage.(pages.EventHandler); ok {
				_ = handler.HandleEvent(e)
				// Check if event triggered cross-page navigation
				if tabPane.ActiveTabIndex != prevTab {
					switchTab(tabPane, pageList, &activePage, tabPane.ActiveTabIndex)
				} else {
					ui.Render(header, tabPane, activePage.Render())
				}
			}
		}
	}
}

func isFunctionKey(id string) bool {
	switch id {
	case "<F1>", "<F2>", "<F3>", "<F4>", "<F5>", "<F6>":
		return true
	default:
		return false
	}
}

func functionKeyIndex(id string) int {
	switch id {
	case "<F1>":
		return 0
	case "<F2>":
		return 1
	case "<F3>":
		return 2
	case "<F4>":
		return 3
	case "<F5>":
		return 4
	case "<F6>":
		return 5
	default:
		return -1
	}
}

func switchTab(tabPane *widgets.TabPane, pageList []pages.Page, active *pages.Page, newIndex int) {
	if newIndex < 0 || newIndex >= len(pageList) {
		return
	}
	if *active != nil {
		(*active).StopSync()
	}
	tabPane.ActiveTabIndex = newIndex
	*active = pageList[newIndex]
	(*active).StartSync()
	ui.Clear()
	ui.Render(tabPane, (*active).Render())
}
