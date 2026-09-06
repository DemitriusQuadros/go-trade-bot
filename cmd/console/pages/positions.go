package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"go-trade-bot/cmd/console/apiclient"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type PositionsPage struct {
	Header       *widgets.Paragraph
	TabPane      *widgets.TabPane
	Dependencies *dependencies.Dependencies
	positions    []apiclient.SignalView
	prices       map[string]float64
	selectedCard int
	cancel       context.CancelFunc
	isActive     bool
	confirming   bool
	errBanner    string
	mu           sync.RWMutex
}

func NewPositionsPage() *PositionsPage {
	return &PositionsPage{
		prices: make(map[string]float64),
	}
}

func (p *PositionsPage) Set(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = d
	return p
}

func (p *PositionsPage) Render() ui.Drawable {
	p.mu.Lock()
	defer p.mu.Unlock()

	termWidth, termHeight := getTerminalDimensions()

	headerHeight := 3
	tabHeight := 3
	footerHeight := 3
	bodyHeight := termHeight - headerHeight - tabHeight - footerHeight
	if bodyHeight < 10 {
		bodyHeight = 10
	}

	p.Header.SetRect(0, 0, termWidth, headerHeight)
	p.TabPane.SetRect(0, headerHeight, termWidth, headerHeight+tabHeight)

	startY := headerHeight + tabHeight
	endY := startY + bodyHeight

	footer := widgets.NewParagraph()
	footer.Text = "[↑↓/←→] select card   [c] close position   [F1-F6] switch page"
	if p.errBanner != "" {
		footer.Text = fmt.Sprintf("[%s](fg:red)", p.errBanner)
	}
	footer.Border = false
	footer.SetRect(0, endY, termWidth, endY+footerHeight)

	if len(p.positions) == 0 {
		emptyMsg := widgets.NewParagraph()
		emptyMsg.Title = "Open Positions"
		emptyMsg.Text = "No open positions"
		emptyMsg.SetRect(0, startY, termWidth, endY)
		comp := &positionsComposite{
			header:  p.Header,
			tabPane: p.TabPane,
			cards:   []*widgets.Paragraph{emptyMsg},
			footer:  footer,
		}
		comp.Border = false
		comp.SetRect(0, 0, termWidth, termHeight)
		return comp
	}

	// Clamp selectedCard
	if p.selectedCard < 0 {
		p.selectedCard = 0
	}
	if p.selectedCard >= len(p.positions) {
		p.selectedCard = len(p.positions) - 1
	}

	// Layout cards in a 2-column grid
	cardWidth := termWidth / 2
	cardHeight := 7
	var cardWidgets []*widgets.Paragraph

	for i, sig := range p.positions {
		row := i / 2
		col := i % 2

		x1 := col * cardWidth
		x2 := x1 + cardWidth
		y1 := startY + row*cardHeight
		y2 := y1 + cardHeight
		if y2 > endY {
			y2 = endY
		}

		card := p.buildCard(sig, i == p.selectedCard)
		card.SetRect(x1, y1, x2, y2)
		cardWidgets = append(cardWidgets, card)
	}

	var confirmModal *widgets.Paragraph
	if p.confirming && p.selectedCard < len(p.positions) {
		sig := p.positions[p.selectedCard]
		confirmModal = p.buildConfirmModal(sig, termWidth, termHeight)
	}

	comp := &positionsComposite{
		header:       p.Header,
		tabPane:      p.TabPane,
		cards:        cardWidgets,
		footer:       footer,
		confirmModal: confirmModal,
	}
	comp.Border = false
	comp.SetRect(0, 0, termWidth, termHeight)
	return comp
}

func (p *PositionsPage) buildCard(sig apiclient.SignalView, isSelected bool) *widgets.Paragraph {
	pWidget := widgets.NewParagraph()
	pWidget.Title = fmt.Sprintf(" Signal #%d — %s — %s ", sig.ID, sig.Strategy.Name, sig.Symbol)

	if isSelected {
		pWidget.BorderStyle.Fg = ui.ColorCyan
	} else {
		pWidget.BorderStyle.Fg = ui.ColorWhite
	}

	var order apiclient.OrderView
	if len(sig.Orders) > 0 {
		order = sig.Orders[0]
	}

	entryPrice := float64(order.EntryPrice)
	qty := float64(order.Quantity)
	invested := float64(order.InvestedAmount)

	currentPrice := entryPrice
	if price, ok := p.prices[sig.Symbol]; ok && price > 0 {
		currentPrice = price
	}

	unrealizedPnL := (currentPrice - entryPrice) * qty
	pnlPct := 0.0
	if invested > 0 {
		pnlPct = (unrealizedPnL / invested) * 100.0
	}

	// Duration open
	duration := time.Since(sig.CreatedAt)
	hours := int(duration.Hours())
	mins := int(duration.Minutes()) % 60
	durationStr := fmt.Sprintf("%dh %dm", hours, mins)

	// SL display
	slStr := "(none set)"
	if order.StopLossPrice > 0 {
		slStr = fmt.Sprintf("$%.2f", order.StopLossPrice)
	}

	// TP display from strategy config
	tpStr := "(none set)"
	if len(sig.Strategy.StrategyConfiguration.Configuration) > 0 {
		var cfgMap map[string]any
		if err := json.Unmarshal(sig.Strategy.StrategyConfiguration.Configuration, &cfgMap); err == nil {
			if tpVal, ok := cfgMap["take_profit_pct"]; ok {
				if tpFloat, ok := tpVal.(float64); ok && tpFloat > 0 {
					tpPrice := entryPrice * (1 + tpFloat/100.0)
					tpStr = fmt.Sprintf("$%.2f", tpPrice)
				}
			}
		}
	}

	// Color scaling
	pnlColor := "fg:green"
	pnlSign := "+"
	if unrealizedPnL < 0 {
		pnlColor = "fg:red"
		pnlSign = ""
	}

	absPct := math.Abs(pnlPct)
	styleTag := pnlColor
	if absPct > 5.0 {
		styleTag = fmt.Sprintf("%s,mod:bold", pnlColor)
	} else if absPct > 1.0 {
		styleTag = fmt.Sprintf("%s,mod:bold", pnlColor)
	}

	line1 := fmt.Sprintf("Entry: $%.2f   Qty: %.4f   Invested: $%.2f", entryPrice, qty, invested)
	line2 := fmt.Sprintf("Current: $%.2f", currentPrice)
	line3 := fmt.Sprintf("Unrealized P&L: [%s$%.2f (%s%.2f%%)](%s)   Open: %s",
		pnlSign, unrealizedPnL, pnlSign, pnlPct, styleTag, durationStr)
	line4 := fmt.Sprintf("SL: %s   TP: %s", slStr, tpStr)

	pWidget.Text = fmt.Sprintf("%s\n%s\n%s\n%s", line1, line2, line3, line4)
	return pWidget
}

func (p *PositionsPage) buildConfirmModal(sig apiclient.SignalView, termWidth, termHeight int) *widgets.Paragraph {
	modal := widgets.NewParagraph()
	modal.Title = fmt.Sprintf(" Close position #%d? ", sig.ID)
	modal.BorderStyle.Fg = ui.ColorRed

	modalWidth := 50
	modalHeight := 8
	x1 := (termWidth - modalWidth) / 2
	y1 := (termHeight - modalHeight) / 2
	modal.SetRect(x1, y1, x1+modalWidth, y1+modalHeight)

	qty := 0.0
	if len(sig.Orders) > 0 {
		qty = float64(sig.Orders[0].Quantity)
	}

	modal.Text = fmt.Sprintf(
		"This will submit a MARKET sell for\n%.4f %s at current price.\n\n        [y] confirm    [n] cancel",
		qty, sig.Symbol,
	)
	return modal
}

type positionsComposite struct {
	ui.Block
	header       *widgets.Paragraph
	tabPane      *widgets.TabPane
	cards        []*widgets.Paragraph
	footer       *widgets.Paragraph
	confirmModal *widgets.Paragraph
}

func (c *positionsComposite) Draw(buf *ui.Buffer) {
	if c.header != nil {
		c.header.Draw(buf)
	}
	if c.tabPane != nil {
		c.tabPane.Draw(buf)
	}
	for _, card := range c.cards {
		if card != nil {
			card.Draw(buf)
		}
	}
	if c.footer != nil {
		c.footer.Draw(buf)
	}
	if c.confirmModal != nil {
		c.confirmModal.Draw(buf)
	}
}

func (p *PositionsPage) HandleEvent(e ui.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.confirming {
		switch e.ID {
		case "y":
			if p.selectedCard < len(p.positions) {
				sigID := p.positions[p.selectedCard].ID
				p.confirming = false
				// Optimistic removal
				removedIndex := p.selectedCard
				p.positions = append(p.positions[:removedIndex], p.positions[removedIndex+1:]...)
				if p.selectedCard >= len(p.positions) && p.selectedCard > 0 {
					p.selectedCard = len(p.positions) - 1
				}

				go func() {
					err := p.Dependencies.API.CloseSignal(context.Background(), sigID)
					p.mu.Lock()
					if err != nil {
						p.errBanner = fmt.Sprintf("Failed to close signal: %v", err)
					}
					active := p.isActive
					p.mu.Unlock()

					if err != nil {
						p.fetchSignals(context.Background())
						if active {
							SafeRender(p.Render())
						}
					}
				}()
			}
		case "n", "<Escape>":
			p.confirming = false
		}
		return nil
	}

	switch e.ID {
	case "<Left>", "h":
		if p.selectedCard > 0 {
			p.selectedCard--
		}
	case "<Right>", "l":
		if p.selectedCard < len(p.positions)-1 {
			p.selectedCard++
		}
	case "<Up>", "k":
		if p.selectedCard >= 2 {
			p.selectedCard -= 2
		}
	case "<Down>", "j":
		if p.selectedCard+2 < len(p.positions) {
			p.selectedCard += 2
		}
	case "c":
		if len(p.positions) > 0 {
			p.confirming = true
		}
	}
	return nil
}

func (p *PositionsPage) fetchSignals(ctx context.Context) {
	if p.Dependencies == nil || p.Dependencies.API == nil || ctx.Err() != nil {
		return
	}
	sigs, err := p.Dependencies.API.GetOpenSignals(ctx)
	if err == nil && ctx.Err() == nil {
		p.mu.Lock()
		p.positions = sigs
		p.mu.Unlock()
	}
}

func (p *PositionsPage) fetchPrices(ctx context.Context) {
	if p.Dependencies == nil || p.Dependencies.API == nil || ctx.Err() != nil {
		return
	}
	p.mu.RLock()
	symSet := make(map[string]struct{})
	for _, sig := range p.positions {
		symSet[sig.Symbol] = struct{}{}
	}
	p.mu.RUnlock()

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	for sym := range symSet {
		if callCtx.Err() != nil {
			return
		}
		prices, err := p.Dependencies.API.ListTickerPrices(callCtx, sym)
		if err == nil && len(prices) > 0 && callCtx.Err() == nil {
			p.mu.Lock()
			p.prices[sym] = prices[0].Price
			p.mu.Unlock()
		}
	}
}

func (p *PositionsPage) StartSync() {
	p.StopSync()

	p.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.isActive = true
	p.mu.Unlock()

	go func() {
		p.fetchSignals(ctx)
		p.fetchPrices(ctx)

		p.mu.RLock()
		active := p.isActive
		p.mu.RUnlock()
		if active && ctx.Err() == nil {
			SafeRender(p.Render())
		}

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.mu.RLock()
				active := p.isActive
				p.mu.RUnlock()
				if !active || ctx.Err() != nil {
					return
				}

				p.fetchSignals(ctx)
				p.fetchPrices(ctx)

				p.mu.RLock()
				active = p.isActive
				p.mu.RUnlock()
				if active && ctx.Err() == nil {
					SafeRender(p.Render())
				}
			}
		}
	}()
}

func (p *PositionsPage) StopSync() {
	p.mu.Lock()
	p.isActive = false
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.mu.Unlock()
}
