package pages

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go-trade-bot/cmd/console/apiclient"
	"go-trade-bot/cmd/console/components"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type DashboardPage struct {
	Header       *widgets.Paragraph
	TabPane      *widgets.TabPane
	Dependencies *dependencies.Dependencies
	stop         chan struct{}
	mu           sync.RWMutex

	lastAccount    apiclient.AccountView
	lastStrategies []apiclient.StrategyView
	connected      bool
	gridWidget     *widgets.Paragraph
}

func NewDashboardPage() *DashboardPage {
	return &DashboardPage{
		connected: false,
	}
}

func (p *DashboardPage) Set(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = d
	return p
}

func (p *DashboardPage) updateHeader() {
	if p.Header == nil {
		return
	}
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	statusStr := "[● DISCONNECTED](fg:red)"
	if p.connected {
		statusStr = "[● CONNECTED](fg:green)"
	}
	balStr := fmt.Sprintf("Balance: $%.2f", p.lastAccount.Amount)
	p.Header.Text = fmt.Sprintf(" go-trade-bot ── %s ── %s ── %s ", nowStr, statusStr, balStr)
	p.Header.TextStyle.Bg = ui.ColorClear
	p.Header.Border = true
}

func (p *DashboardPage) Render() ui.Drawable {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.fetchDataOnce(context.Background())
	p.updateHeader()

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

	// Left column: Account Summary
	leftWidth := termWidth / 4
	if leftWidth < 25 {
		leftWidth = 25
	}

	// Right column: Compact strategies
	rightWidth := termWidth / 4
	if rightWidth < 25 {
		rightWidth = 25
	}

	// Center column: Monitored pairs
	centerWidth := termWidth - leftWidth - rightWidth
	if centerWidth < 30 {
		centerWidth = 30
	}

	startY := headerHeight + tabHeight
	endY := startY + bodyHeight

	accountSummary := components.AccountSummary(context.Background(), p.Dependencies.API)
	if pWidget, ok := accountSummary.(*widgets.Paragraph); ok {
		pWidget.SetRect(0, startY, leftWidth, endY)
	}

	pairsGrid := p.buildPairsGrid(centerWidth, bodyHeight)
	pairsGrid.SetRect(leftWidth, startY, leftWidth+centerWidth, endY)

	stratTable := components.StrategyTableCompact(context.Background(), p.Dependencies.API)
	if sWidget, ok := stratTable.(*widgets.Paragraph); ok {
		sWidget.SetRect(leftWidth+centerWidth, startY, termWidth, endY)
	} else if tWidget, ok := stratTable.(*widgets.Table); ok {
		tWidget.SetRect(leftWidth+centerWidth, startY, termWidth, endY)
	}

	footer := widgets.NewParagraph()
	footer.Text = "[F1-F6] switch page   [q] quit   [h/l] navigate tabs"
	footer.Border = false
	footer.SetRect(0, endY, termWidth, endY+footerHeight)

	grid := ui.NewGrid()
	grid.SetRect(0, 0, termWidth, termHeight)

	comp := &dashboardComposite{
		header:     p.Header,
		tabPane:    p.TabPane,
		account:    accountSummary,
		pairs:      pairsGrid,
		strategies: stratTable,
		footer:     footer,
	}
	comp.Border = false
	comp.SetRect(0, 0, termWidth, termHeight)
	return comp
}

type dashboardComposite struct {
	ui.Block
	header     *widgets.Paragraph
	tabPane    *widgets.TabPane
	account    ui.Drawable
	pairs      ui.Drawable
	strategies ui.Drawable
	footer     *widgets.Paragraph
}

func (d *dashboardComposite) Draw(buf *ui.Buffer) {
	if d.header != nil {
		d.header.Draw(buf)
	}
	if d.tabPane != nil {
		d.tabPane.Draw(buf)
	}
	if d.account != nil {
		d.account.Draw(buf)
	}
	if d.pairs != nil {
		d.pairs.Draw(buf)
	}
	if d.strategies != nil {
		d.strategies.Draw(buf)
	}
	if d.footer != nil {
		d.footer.Draw(buf)
	}
}

func (p *DashboardPage) fetchDataOnce(ctx context.Context) {
	if p.Dependencies == nil || p.Dependencies.API == nil {
		p.connected = false
		return
	}

	err := p.Dependencies.API.Ping(ctx)
	if err != nil {
		p.connected = false
	} else {
		p.connected = true
	}

	if acc, err := p.Dependencies.API.GetAccount(ctx); err == nil {
		p.lastAccount = acc
	}
	if strats, err := p.Dependencies.API.ListStrategies(ctx); err == nil {
		p.lastStrategies = strats
	}
}

func (p *DashboardPage) buildPairsGrid(width, height int) *widgets.Paragraph {
	pWidget := widgets.NewParagraph()
	pWidget.Title = "Monitored Pairs"
	pWidget.BorderStyle.Fg = ui.ColorCyan

	if len(p.lastStrategies) == 0 {
		pWidget.Text = "No monitored pairs (no strategies configured)"
		return pWidget
	}

	// Union symbols
	symbolSet := make(map[string]struct{})
	var symbols []string
	for _, s := range p.lastStrategies {
		for _, sym := range s.MonitoredSymbols {
			sym = strings.TrimSpace(sym)
			if sym != "" {
				if _, ok := symbolSet[sym]; !ok {
					symbolSet[sym] = struct{}{}
					symbols = append(symbols, sym)
				}
			}
		}
	}

	if len(symbols) == 0 {
		pWidget.Text = "No monitored pairs configured"
		return pWidget
	}

	var lines []string
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for _, sym := range symbols {
		prices, err := p.Dependencies.API.ListTickerPrices(ctx, sym)
		if err != nil || len(prices) == 0 {
			lines = append(lines, fmt.Sprintf("%-10s [error fetching price]", sym))
			continue
		}

		lastPrice := prices[0].Price
		klines, err := p.Dependencies.API.ListKlines(ctx, sym, "1m", 60)
		changePct := 0.0
		isAboveEMA := true

		if err == nil && len(klines) > 0 {
			firstClose := klines[0].Close
			if firstClose > 0 {
				changePct = ((lastPrice - firstClose) / firstClose) * 100.0
			}
			closes := make([]float64, len(klines))
			for i, k := range klines {
				closes[i] = k.Close
			}
			ema := components.ComputeEMA20(closes)
			if len(ema) > 0 && lastPrice < ema[len(ema)-1] {
				isAboveEMA = false
			}
		}

		changeSign := "+"
		if changePct < 0 {
			changeSign = ""
		}

		colorTag := "fg:green"
		if !isAboveEMA {
			colorTag = "fg:red"
		}

		line := fmt.Sprintf("[%s](%s)   [$%.2f](%s)   [%s%.1f%%](%s)",
			sym, colorTag, lastPrice, colorTag, changeSign, changePct, colorTag)
		lines = append(lines, line)
	}

	lines = append(lines, "\n(green = above EMA20, red = below)")
	pWidget.Text = strings.Join(lines, "\n")
	return pWidget
}

func (p *DashboardPage) StartSync() {
	p.StopSync()
	p.stop = make(chan struct{})

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				p.mu.Lock()
				p.fetchDataOnce(context.Background())
				p.updateHeader()
				p.mu.Unlock()
				SafeRender(p.Render())
			}
		}
	}()
}

func (p *DashboardPage) StopSync() {
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
}
