package components

import (
	"context"
	repository "go-trade-bot/app/repository/strategy"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

func StrategyList(d *dependencies.Dependencies) *widgets.List {
	list := widgets.NewList()
	list.Title = "Strategies"
	list.Rows = getStrategies(d)
	list.TextStyle = ui.NewStyle(ui.ColorWhite)
	list.BorderStyle = ui.NewStyle(ui.ColorCyan)
	list.SetRect(0, 7, 50, 20)
	list.BorderStyle.Fg = ui.ColorCyan
	return list
}

func getStrategies(d *dependencies.Dependencies) []string {
	strategyRepository := repository.NewStrategyRepository(d.Db)

	strategies, err := strategyRepository.GetAll(context.Background())
	if err != nil {
		panic("Failed to get strategies: " + err.Error())
	}
	var strategyNames []string
	for _, strategy := range strategies {
		strategyNames = append(strategyNames, strategy.Name+" - "+string(strategy.Status))
	}
	return strategyNames
}
