package pages

import (
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// Page interface defining lifecycle and render methods for all TUI pages.
type Page interface {
	Set(header *widgets.Paragraph, tabPane *widgets.TabPane, dependencies *dependencies.Dependencies) Page
	Render() ui.Drawable
	StopSync()
	StartSync()
}

// EventHandler is an optional interface implemented by pages that process page-specific key events.
type EventHandler interface {
	HandleEvent(e ui.Event) error
}

// RegisterPages centralizes the 6-tab construction and returns the ordered slice of pages.
func RegisterPages(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) []Page {
	p1 := NewDashboardPage().Set(header, tabPane, d)
	p2 := NewStrategiesPage().Set(header, tabPane, d)
	p3 := NewPositionsPage().Set(header, tabPane, d)
	p4 := NewBacktestLauncherPage().Set(header, tabPane, d)
	p5 := NewBacktestResultsPage().Set(header, tabPane, d)
	p6 := NewExecutionLogPage().Set(header, tabPane, d)

	// Cross-page wiring:
	// Page 2 [b] navigates to Page 4 with preselected strategy
	if stratPage, ok := p2.(*StrategiesPage); ok {
		if btPage, ok := p4.(*BacktestLauncherPage); ok {
			stratPage.OnBacktestRequested = func(strategyID uint) {
				btPage.PreselectStrategy(strategyID)
				tabPane.ActiveTabIndex = 3 // Index 3 is Backtest
			}
		}
	}

	// Page 4 auto-navigates to Page 5 on completion
	if btPage, ok := p4.(*BacktestLauncherPage); ok {
		if resPage, ok := p5.(*BacktestResultsPage); ok {
			btPage.OnBacktestCompleted = func(result any) {
				resPage.SetResult(result)
				tabPane.ActiveTabIndex = 4 // Index 4 is Results
			}
		}
	}

	return []Page{p1, p2, p3, p4, p5, p6}
}

// getTerminalDimensions safely queries terminal dimensions without panicking if ui.Init() wasn't called.
func getTerminalDimensions() (int, int) {
	defer func() {
		_ = recover()
	}()
	w, h := ui.TerminalDimensions()
	if w > 0 && h > 0 {
		return w, h
	}
	return 120, 35
}

// SafeRender safely renders drawables without panicking if ui.Init() was not called (e.g. In tests).
func SafeRender(items ...ui.Drawable) {
	defer func() {
		_ = recover()
	}()
	ui.Render(items...)
}
