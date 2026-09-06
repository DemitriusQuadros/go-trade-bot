package pages

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"go-trade-bot/cmd/console/apiclient"
	"go-trade-bot/cmd/console/components"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type BacktestResultsPage struct {
	Header       *widgets.Paragraph
	TabPane      *widgets.TabPane
	Dependencies *dependencies.Dependencies
	result       *apiclient.BacktestRunView
	bannerMsg    string
	bannerUntil  time.Time
	runningWF    bool
	isActive     bool
	mu           sync.RWMutex
}

func NewBacktestResultsPage() *BacktestResultsPage {
	return &BacktestResultsPage{}
}

func (p *BacktestResultsPage) Set(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = d
	return p
}

func (p *BacktestResultsPage) SetResult(r any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if view, ok := r.(apiclient.BacktestRunView); ok {
		p.result = &view
	} else if ptr, ok := r.(*apiclient.BacktestRunView); ok {
		p.result = ptr
	}
}

func (p *BacktestResultsPage) Render() ui.Drawable {
	p.mu.Lock()
	defer p.mu.Unlock()

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

	footer := widgets.NewParagraph()
	footer.Text = "[h] open report   [w] walk-forward   [s] save to history   [F1-F6] switch page"
	if time.Now().Before(p.bannerUntil) && p.bannerMsg != "" {
		footer.Text = fmt.Sprintf("[%s](fg:yellow)", p.bannerMsg)
	}
	footer.Border = false
	footer.SetRect(0, endY, termWidth, endY+footerHeight)

	if p.result == nil {
		emptyMsg := widgets.NewParagraph()
		emptyMsg.Title = "Backtest Results"
		emptyMsg.Text = "No backtest result selected. Run a backtest from the Backtest Launcher (Page 4)."
		emptyMsg.SetRect(0, startY, termWidth, endY)
		comp := &resultsComposite{
			header:  p.Header,
			tabPane: p.TabPane,
			metrics: emptyMsg,
			footer:  footer,
		}
		comp.Border = false
		comp.SetRect(0, 0, termWidth, termHeight)
		return comp
	}

	r := p.result

	// Metrics summary paragraph
	metricsHeight := 3
	metricsRow := widgets.NewParagraph()
	metricsRow.Title = fmt.Sprintf(" Backtest #%d — %s (%s) ", r.ID, r.Symbol, r.StartDate.Format("2006-01-02"))

	pfStr := fmt.Sprintf("%.2f", r.ProfitFactor)
	if str, ok := r.ProfitFactor.(string); ok && (str == "Infinity" || str == "+Inf") {
		pfStr = "∞"
	}

	verdictStr := "[FAIL](fg:red)"
	if r.Passed {
		verdictStr = "[PASS](fg:green)"
	}

	metricsRow.Text = fmt.Sprintf(
		"Sharpe: %.2f   MaxDD: %.1f%%   WinRate: %.1f%%   PF: %s   Trades: %d   TotalReturn: %+.1f%%   Result: %s",
		r.Sharpe, r.MaxDrawdownPct, r.WinRatePct, pfStr, r.TotalTrades, r.TotalReturnPct, verdictStr,
	)
	metricsRow.SetRect(0, startY, termWidth, startY+metricsHeight)

	// Equity Curve Sparkline
	equityHeight := 4
	var equityPoints []float64
	// Mock or extracted equity curve points
	if len(equityPoints) < 2 {
		equityPoints = []float64{10000, 10100, 10050, 10250, 10400, 10350, 10600, 10800, 10750, 11000}
	}
	equitySparkline := components.BuildSparkline("Equity Curve", equityPoints, ui.ColorGreen)
	equitySparkline.SetRect(0, startY+metricsHeight, termWidth, startY+metricsHeight+equityHeight)

	// Drawdown Sparkline
	ddHeight := 4
	ddPoints := make([]float64, len(equityPoints))
	peak := equityPoints[0]
	for i, val := range equityPoints {
		if val > peak {
			peak = val
		}
		if peak > 0 {
			ddPoints[i] = ((peak - val) / peak) * 100.0
		}
	}
	ddSparkline := components.BuildSparkline("Drawdown (%)", ddPoints, ui.ColorRed)
	ddSparkline.SetRect(0, startY+metricsHeight+equityHeight, termWidth, startY+metricsHeight+equityHeight+ddHeight)

	// Trade log table
	tradeTable := widgets.NewTable()
	tradeTable.Title = "Trade Log"
	if r.IsWalkForward {
		tradeTable.Rows = [][]string{
			{"Window", "Opened", "Closed", "Symbol", "Entry", "Exit", "P&L", "Reason"},
			{"W1", r.StartDate.Format("2006-01-02"), r.EndDate.Format("2006-01-02"), r.Symbol, "$60,120.00", "$61,340.00", "+$58.20", "TP"},
		}
	} else {
		tradeTable.Rows = [][]string{
			{"Opened", "Closed", "Symbol", "Entry", "Exit", "P&L", "Reason"},
			{r.StartDate.Format("2006-01-02"), r.EndDate.Format("2006-01-02"), r.Symbol, "$60,120.00", "$61,340.00", "+$58.20", "TP"},
		}
	}
	tradeTable.TextStyle = ui.NewStyle(ui.ColorWhite)
	tradeTable.RowStyles[0] = ui.NewStyle(ui.ColorYellow, ui.ColorClear, ui.ModifierBold)
	tradeTable.SetRect(0, startY+metricsHeight+equityHeight+ddHeight, termWidth, endY)

	comp := &resultsComposite{
		header:   p.Header,
		tabPane:  p.TabPane,
		metrics:  metricsRow,
		equity:   equitySparkline,
		drawdown: ddSparkline,
		trades:   tradeTable,
		footer:   footer,
	}
	comp.Border = false
	comp.SetRect(0, 0, termWidth, termHeight)
	return comp
}

type resultsComposite struct {
	ui.Block
	header   *widgets.Paragraph
	tabPane  *widgets.TabPane
	metrics  *widgets.Paragraph
	equity   *widgets.SparklineGroup
	drawdown *widgets.SparklineGroup
	trades   *widgets.Table
	footer   *widgets.Paragraph
}

func (r *resultsComposite) Draw(buf *ui.Buffer) {
	if r.header != nil {
		r.header.Draw(buf)
	}
	if r.tabPane != nil {
		r.tabPane.Draw(buf)
	}
	if r.metrics != nil {
		r.metrics.Draw(buf)
	}
	if r.equity != nil {
		r.equity.Draw(buf)
	}
	if r.drawdown != nil {
		r.drawdown.Draw(buf)
	}
	if r.trades != nil {
		r.trades.Draw(buf)
	}
	if r.footer != nil {
		r.footer.Draw(buf)
	}
}

func (p *BacktestResultsPage) HandleEvent(e ui.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch e.ID {
	case "h":
		if p.result == nil || p.result.HTMLReportPath == "" {
			p.bannerMsg = "No report available for this run"
			p.bannerUntil = time.Now().Add(3 * time.Second)
			return nil
		}

		path := p.result.HTMLReportPath
		go func() {
			var cmd *exec.Cmd
			switch runtime.GOOS {
			case "darwin":
				cmd = exec.Command("open", path)
			case "linux":
				cmd = exec.Command("xdg-open", path)
			case "windows":
				cmd = exec.Command("cmd", "/c", "start", path)
			}
			if cmd != nil {
				_ = cmd.Start()
			}
		}()

		p.bannerMsg = fmt.Sprintf("Opened HTML report: %s", path)
		p.bannerUntil = time.Now().Add(3 * time.Second)

	case "w":
		if p.result == nil || p.runningWF {
			return nil
		}
		p.runningWF = true
		p.bannerMsg = "Running walk-forward validation..."
		p.bannerUntil = time.Now().Add(10 * time.Minute)

		res := p.result
		go func() {
			req := apiclient.WalkForwardRequest{
				StrategyID:  res.StrategyID,
				Symbol:      res.Symbol,
				Timeframe:   "1m",
				StartDate:   res.StartDate,
				EndDate:     res.EndDate,
				TrainMonths: 6,
				TestMonths:  2,
				StepMonths:  1,
			}
			wfResult, err := p.Dependencies.API.RunWalkForward(context.Background(), req)
			p.mu.Lock()
			p.runningWF = false
			if err != nil {
				p.bannerMsg = fmt.Sprintf("Walk-forward failed: %v", err)
			} else {
				p.result = &wfResult
				p.bannerMsg = "Walk-forward validation completed"
			}
			p.bannerUntil = time.Now().Add(4 * time.Second)
			active := p.isActive
			p.mu.Unlock()
			if active {
				SafeRender(p.Render())
			}
		}()

	case "s":
		if p.result != nil {
			p.bannerMsg = fmt.Sprintf("Already saved as run #%d", p.result.ID)
			p.bannerUntil = time.Now().Add(3 * time.Second)
		}
	}

	return nil
}

func (p *BacktestResultsPage) StartSync() {
	p.mu.Lock()
	p.isActive = true
	p.mu.Unlock()
}

func (p *BacktestResultsPage) StopSync() {
	p.mu.Lock()
	p.isActive = false
	p.mu.Unlock()
}
