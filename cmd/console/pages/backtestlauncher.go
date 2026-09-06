package pages

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go-trade-bot/cmd/console/apiclient"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type BacktestLauncherPage struct {
	Header              *widgets.Paragraph
	TabPane             *widgets.TabPane
	Dependencies        *dependencies.Dependencies
	strategies          []apiclient.StrategyView
	selectedStrat       int
	timeframes          []string
	selectedTimeframe   int
	symbolInput         string
	startDate           time.Time
	endDate             time.Time
	activeField         int // 0: Strat, 1: Symbol, 2: Timeframe, 3: StartDate, 4: EndDate
	dryRun              bool
	slippagePct         float64
	feePct              float64
	fillDelayMs         int
	running             bool
	progressPct         int
	progressMsg         string
	recent              []apiclient.BacktestRunView
	errBanner           string
	isActive            bool
	mu                  sync.RWMutex
	OnBacktestCompleted func(result any)
}

func NewBacktestLauncherPage() *BacktestLauncherPage {
	now := time.Now()
	return &BacktestLauncherPage{
		timeframes:        []string{"1m", "5m", "15m", "30m", "1h", "1d"},
		selectedTimeframe: 0,
		symbolInput:       "BTCUSDT",
		startDate:         now.AddDate(-1, 0, 0),
		endDate:           now,
		slippagePct:       0.05,
		feePct:            0.10,
		fillDelayMs:       250,
	}
}

func (p *BacktestLauncherPage) Set(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = d
	return p
}

func (p *BacktestLauncherPage) PreselectStrategy(id uint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, s := range p.strategies {
		if s.ID == id {
			p.selectedStrat = i
			if len(s.MonitoredSymbols) > 0 {
				p.symbolInput = s.MonitoredSymbols[0]
			}
			break
		}
	}
}

func (p *BacktestLauncherPage) fetchStrategiesAndRecent() {
	if p.Dependencies == nil || p.Dependencies.API == nil {
		return
	}
	strats, err := p.Dependencies.API.ListStrategies(context.Background())
	if err == nil {
		p.strategies = strats
		if p.selectedStrat >= len(p.strategies) && len(p.strategies) > 0 {
			p.selectedStrat = 0
		}
		if len(p.strategies) > 0 && p.symbolInput == "" && len(p.strategies[p.selectedStrat].MonitoredSymbols) > 0 {
			p.symbolInput = p.strategies[p.selectedStrat].MonitoredSymbols[0]
		}
	}

	if len(p.strategies) > 0 && p.selectedStrat < len(p.strategies) {
		stratID := p.strategies[p.selectedStrat].ID
		runs, err := p.Dependencies.API.ListBacktests(context.Background(), stratID)
		if err == nil {
			p.recent = runs
		}
	}
}

func (p *BacktestLauncherPage) Render() ui.Drawable {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.fetchStrategiesAndRecent()

	termWidth, termHeight := getTerminalDimensions()

	headerHeight := 3
	tabHeight := 3
	footerHeight := 3
	bodyHeight := termHeight - headerHeight - tabHeight - footerHeight
	if bodyHeight < 15 {
		bodyHeight = 15
	}

	p.Header.SetRect(0, 0, termWidth, headerHeight)
	p.TabPane.SetRect(0, headerHeight, termWidth, headerHeight+tabHeight)

	startY := headerHeight + tabHeight
	endY := startY + bodyHeight

	// Form panel
	formHeight := 7
	formPanel := p.buildFormPanel(termWidth, formHeight)
	formPanel.SetRect(0, startY, termWidth, startY+formHeight)

	// Progress bar
	gaugeHeight := 3
	gauge := widgets.NewGauge()
	gauge.Title = "Backtest Execution"
	gauge.Percent = p.progressPct
	gauge.BarColor = ui.ColorGreen
	if p.running {
		gauge.Label = fmt.Sprintf("Running... %s", p.progressMsg)
	} else {
		gauge.Label = "Ready — Press [Enter] to run backtest"
	}
	gauge.SetRect(0, startY+formHeight, termWidth, startY+formHeight+gaugeHeight)

	// Recent backtests table
	recentTable := p.buildRecentTable()
	recentTable.SetRect(0, startY+formHeight+gaugeHeight, termWidth, endY)

	footer := widgets.NewParagraph()
	footer.Text = "[Tab] next field   [←/→] select option   [Enter] run backtest   [F1-F6] switch page"
	if p.errBanner != "" {
		footer.Text = fmt.Sprintf("[%s](fg:red)", p.errBanner)
	}
	footer.Border = false
	footer.SetRect(0, endY, termWidth, endY+footerHeight)

	comp := &launcherComposite{
		header:      p.Header,
		tabPane:     p.TabPane,
		form:        formPanel,
		gauge:       gauge,
		recentTable: recentTable,
		footer:      footer,
	}
	comp.Border = false
	comp.SetRect(0, 0, termWidth, termHeight)
	return comp
}

func (p *BacktestLauncherPage) buildFormPanel(termWidth, height int) *widgets.Paragraph {
	pWidget := widgets.NewParagraph()
	pWidget.Title = "Backtest Launcher Form"
	pWidget.BorderStyle.Fg = ui.ColorCyan

	stratName := "(No strategies available)"
	if len(p.strategies) > 0 && p.selectedStrat < len(p.strategies) {
		stratName = p.strategies[p.selectedStrat].Name
	}

	tf := p.timeframes[p.selectedTimeframe]
	startStr := p.startDate.Format("2006-01-02")
	endStr := p.endDate.Format("2006-01-02")

	f0Style := "fg:white"
	if p.activeField == 0 {
		f0Style = "fg:black,bg:cyan"
	}
	f1Style := "fg:white"
	if p.activeField == 1 {
		f1Style = "fg:black,bg:cyan"
	}
	f2Style := "fg:white"
	if p.activeField == 2 {
		f2Style = "fg:black,bg:cyan"
	}
	f3Style := "fg:white"
	if p.activeField == 3 {
		f3Style = "fg:black,bg:cyan"
	}
	f4Style := "fg:white"
	if p.activeField == 4 {
		f4Style = "fg:black,bg:cyan"
	}

	line1 := fmt.Sprintf("Strategy: [ < %s > ](%s)    Symbol: [ %s ](%s)    Timeframe: [ < %s > ](%s)",
		stratName, f0Style, p.symbolInput, f1Style, tf, f2Style)
	line2 := fmt.Sprintf("Start Date: [ %s ](%s)       End Date: [ %s ](%s)",
		startStr, f3Style, endStr, f4Style)

	dryRunInfo := fmt.Sprintf("Dry-Run Config: Slippage: %.2f%%   Fee: %.2f%%   Fill Delay: %dms",
		p.slippagePct, p.feePct, p.fillDelayMs)

	pWidget.Text = fmt.Sprintf("%s\n%s\n\n%s", line1, line2, dryRunInfo)
	return pWidget
}

func (p *BacktestLauncherPage) buildRecentTable() *widgets.Table {
	table := widgets.NewTable()
	table.Title = "Recent Backtests"
	table.Rows = [][]string{
		{"Strategy", "Range", "Sharpe", "Result", "Report"},
	}

	if len(p.recent) == 0 {
		table.Rows = append(table.Rows, []string{"No recent backtests", "-", "-", "-", "-"})
		return table
	}

	stratMap := make(map[uint]string)
	for _, s := range p.strategies {
		stratMap[s.ID] = s.Name
	}

	for i, r := range p.recent {
		sName := stratMap[r.StrategyID]
		if sName == "" {
			sName = fmt.Sprintf("strategy_%d", r.StrategyID)
		}
		rangeStr := fmt.Sprintf("%s..%s", r.StartDate.Format("2006-01-02"), r.EndDate.Format("2006-01-02"))
		sharpeStr := fmt.Sprintf("%.2f", r.Sharpe)
		resultStr := "FAIL"
		resStyle := ui.NewStyle(ui.ColorRed)
		if r.Passed {
			resultStr = "PASS"
			resStyle = ui.NewStyle(ui.ColorGreen)
		}
		table.Rows = append(table.Rows, []string{sName, rangeStr, sharpeStr, resultStr, r.HTMLReportPath})
		table.RowStyles[i+1] = resStyle
	}

	table.TextStyle = ui.NewStyle(ui.ColorWhite)
	table.RowStyles[0] = ui.NewStyle(ui.ColorYellow, ui.ColorClear, ui.ModifierBold)
	return table
}

type launcherComposite struct {
	ui.Block
	header      *widgets.Paragraph
	tabPane     *widgets.TabPane
	form        *widgets.Paragraph
	gauge       *widgets.Gauge
	recentTable *widgets.Table
	footer      *widgets.Paragraph
}

func (l *launcherComposite) Draw(buf *ui.Buffer) {
	if l.header != nil {
		l.header.Draw(buf)
	}
	if l.tabPane != nil {
		l.tabPane.Draw(buf)
	}
	if l.form != nil {
		l.form.Draw(buf)
	}
	if l.gauge != nil {
		l.gauge.Draw(buf)
	}
	if l.recentTable != nil {
		l.recentTable.Draw(buf)
	}
	if l.footer != nil {
		l.footer.Draw(buf)
	}
}

func (p *BacktestLauncherPage) HandleEvent(e ui.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch e.ID {
	case "<Tab>":
		p.activeField = (p.activeField + 1) % 5
	case "<Left>", "h":
		if p.activeField == 0 && len(p.strategies) > 0 {
			if p.selectedStrat > 0 {
				p.selectedStrat--
			} else {
				p.selectedStrat = len(p.strategies) - 1
			}
			if len(p.strategies[p.selectedStrat].MonitoredSymbols) > 0 {
				p.symbolInput = p.strategies[p.selectedStrat].MonitoredSymbols[0]
			}
		} else if p.activeField == 2 {
			if p.selectedTimeframe > 0 {
				p.selectedTimeframe--
			} else {
				p.selectedTimeframe = len(p.timeframes) - 1
			}
		}
	case "<Right>", "l":
		if p.activeField == 0 && len(p.strategies) > 0 {
			if p.selectedStrat < len(p.strategies)-1 {
				p.selectedStrat++
			} else {
				p.selectedStrat = 0
			}
			if len(p.strategies[p.selectedStrat].MonitoredSymbols) > 0 {
				p.symbolInput = p.strategies[p.selectedStrat].MonitoredSymbols[0]
			}
		} else if p.activeField == 2 {
			if p.selectedTimeframe < len(p.timeframes)-1 {
				p.selectedTimeframe++
			} else {
				p.selectedTimeframe = 0
			}
		}
	case "<Enter>":
		if p.running {
			return nil
		}
		if len(p.strategies) == 0 {
			p.errBanner = "No strategies available to run"
			return nil
		}
		if p.endDate.Before(p.startDate) {
			p.errBanner = "Validation error: End date cannot be before Start date"
			return nil
		}

		selected := p.strategies[p.selectedStrat]
		tf := p.timeframes[p.selectedTimeframe]
		p.running = true
		p.progressPct = 20
		p.progressMsg = fmt.Sprintf("%s on %s", selected.Name, p.symbolInput)
		p.errBanner = ""

		go func() {
			req := apiclient.RunBacktestRequest{
				StrategyID: selected.ID,
				Symbol:     p.symbolInput,
				Timeframe:  tf,
				StartDate:  p.startDate,
				EndDate:    p.endDate,
			}

			runRes, err := p.Dependencies.API.RunBacktest(context.Background(), req)
			p.mu.Lock()
			p.running = false
			if err != nil {
				var apiErr apiclient.ErrAPI
				if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
					p.errBanner = fmt.Sprintf("No historical data for %s/%s — run cmd/candleimport first", req.Symbol, req.Timeframe)
				} else {
					p.errBanner = fmt.Sprintf("Backtest failed: %v", err)
				}
				p.progressPct = 0
				active := p.isActive
				p.mu.Unlock()
				if active {
					SafeRender(p.Render())
				}
				return
			}

			p.progressPct = 100
			active := p.isActive
			p.mu.Unlock()
			if active {
				SafeRender(p.Render())
			}

			if p.OnBacktestCompleted != nil {
				p.OnBacktestCompleted(runRes)
			}
		}()
	}

	return nil
}

func (p *BacktestLauncherPage) StartSync() {
	p.mu.Lock()
	p.isActive = true
	p.mu.Unlock()
}

func (p *BacktestLauncherPage) StopSync() {
	p.mu.Lock()
	p.isActive = false
	p.mu.Unlock()
}
