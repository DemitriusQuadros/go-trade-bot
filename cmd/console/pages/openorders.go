package pages

import (
	"context"
	"fmt"
	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/signal"
	"go-trade-bot/cmd/console/components"
	"go-trade-bot/cmd/console/dependencies"
	"go-trade-bot/internal/broker"
	"strconv"
	"time"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type OpenOrdersPage struct {
	Header       *widgets.Paragraph
	TabPane      *widgets.TabPane
	Dependencies *dependencies.Dependencies
	Stop         bool
}

func NewOpenOrdersPage() *OpenOrdersPage {
	return &OpenOrdersPage{}
}

func (p *OpenOrdersPage) Set(
	header *widgets.Paragraph,
	tabPane *widgets.TabPane,
	dependencies *dependencies.Dependencies,
) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = dependencies
	return p
}

func (p *OpenOrdersPage) Render() ui.Drawable {
	open := ui.NewGrid()
	termWidth, termHeight := ui.TerminalDimensions()

	open.SetRect(0, 0, termWidth, termHeight)
	items := []ui.GridItem{}
	items = append(items, ui.NewRow(1.0/16, p.Header))
	items = append(items, ui.NewRow(1.0/16, p.TabPane))
	items = append(items, p.renderOpenSignals()...)

	interfaceItems := make([]interface{}, len(items))
	for i, v := range items {
		interfaceItems[i] = v
	}
	open.Set(interfaceItems...)

	return open
}

func (p *OpenOrdersPage) renderOpenSignals() []ui.GridItem {
	broker := broker.NewBroker(p.Dependencies.Cfg)
	signals, err := p.getOpenSignals()
	if err != nil {
		return []ui.GridItem{
			ui.NewRow(1.0, ui.NewCol(1.0, components.Error(err))),
		}
	}

	items := []ui.GridItem{}
	for _, signal := range signals {
		strategy := widgets.NewParagraph()
		strategy.Text = fmt.Sprintf(
			"ID - %d - %s - %s - %s", signal.ID, signal.Strategy.Name, string(signal.Status), signal.Symbol,
		)
		strategy.TextStyle.Fg = ui.ColorGreen

		invested := widgets.NewParagraph()
		invested.Text = fmt.Sprintf(
			"Invested: %s - Qtd: %.2f - Entry Price: %.2f",
			fmt.Sprintf("$%.2f", signal.Orders[0].InvestedAmount),
			signal.Orders[0].Quantity,
			signal.Orders[0].EntryPrice,
		)
		invested.TextStyle.Fg = ui.ColorGreen
		current := widgets.NewParagraph()
		go func() {
			for {
				if p.Stop {
					return
				}
				prices, err := broker.ListTickerPrices(context.TODO(), signal.Symbol)
				if err != nil {
					current.Text = "Error fetching price: " + err.Error()
					ui.Render(current)
					return
				}
				priceFloat, err := strconv.ParseFloat(prices[0].Price, 32)
				if err != nil {
					current.Text = "Error parsing price: " + err.Error()
					ui.Render(current)
					return
				}
				pnl := (float32(priceFloat) * signal.Orders[0].Quantity) - (signal.Orders[0].Quantity * signal.Orders[0].EntryPrice)
				current.Text = "Current: $" + prices[0].Price + " PnL: $" + fmt.Sprintf("%.2f", pnl)
				if pnl < 0 {
					current.TextStyle.Fg = ui.ColorRed
				} else {
					current.TextStyle.Fg = ui.ColorBlue
				}

				ui.Render(current)
				time.Sleep(1 * time.Second)
			}
		}()

		items = append(items, ui.NewRow(0.4/4,
			ui.NewCol(1.0/3, strategy),
			ui.NewCol(1.0/3, invested),
			ui.NewCol(1.0/3, current),
		))
	}
	return items
}

func (p *OpenOrdersPage) getOpenSignals() ([]entities.Signal, error) {
	r := repository.NewSignalRepository(p.Dependencies.Db)

	signals, err := r.GetAllOpenSignals()
	if err != nil {
		return nil, err
	}

	return signals, nil

}

func (p *OpenOrdersPage) HandleEvent(event interface{}) error {
	return nil
}

func (p *OpenOrdersPage) StopSync() {
	p.Stop = true
}

func (p *OpenOrdersPage) StartSync() {
	p.Stop = false
	p.renderOpenSignals()
}
