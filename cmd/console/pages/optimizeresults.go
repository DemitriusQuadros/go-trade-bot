package pages

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"go-trade-bot/cmd/console/apiclient"
	"go-trade-bot/cmd/console/components"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

// rankMetrics is the fixed cycle order for the [m] keybinding, per tui-01's
// Acceptance Criterion #4: sharpe_ratio -> max_drawdown_pct -> win_rate_pct
// -> profit_factor -> sharpe_ratio.
var rankMetrics = []string{"sharpe_ratio", "max_drawdown_pct", "win_rate_pct", "profit_factor"}

// paramNameOptions is a small preset list of config field names an operator
// can sweep — this codebase's TUI has no free-text input anywhere else
// (symbol/strategy/timeframe fields are all cycled via Left/Right over a
// fixed option list, e.g. BacktestLauncherPage), so ParamX/ParamY names
// follow that same established convention rather than introducing the first
// free-text field.
var paramNameOptions = []string{"rsi_period", "stop_loss_pct", "take_profit_pct", "grid_spacing_pct", "grid_levels"}

var optimizeTimeframes = []string{"1m", "5m", "15m", "30m", "1h", "1d"}

// OptimizeForm holds the operator-editable inputs for a grid search request.
type OptimizeForm struct {
	Symbol             string
	Timeframe          string
	StartDate, EndDate time.Time
	ParamX, ParamY     string
	RangeX, RangeY     apiclient.ParamRange
	RankMetric         string // the metric RunOptimization is actually submitted with
}

// OptimizeResultsPage is Page 7 — parameter heatmap with the best-performing
// configuration highlighted, per PRD SS4.3/SS7 and spec tui-01 (Phase 4).
type OptimizeResultsPage struct {
	Header       *widgets.Paragraph
	TabPane      *widgets.TabPane
	Dependencies *dependencies.Dependencies

	strategies    []apiclient.StrategyView
	selectedStrat int
	activeField   int
	form          OptimizeForm

	running     bool
	runID       *uint
	runStatus   *apiclient.OptimizationRunView
	result      *apiclient.OptimizationResultsView
	axisXValues []float64
	axisYValues []float64
	// displayMetric is the metric currently used to color/rank the heatmap.
	// It starts equal to the metric the run was submitted with, but can be
	// cycled independently via [m] after a result is loaded (AC#4) — see
	// bestCellFor's doc comment for how that interacts with AC#2.
	displayMetric string

	errBanner string
	isActive  bool
	stop      chan struct{}
	mu        sync.RWMutex
}

func NewOptimizeResultsPage() *OptimizeResultsPage {
	now := time.Now()
	return &OptimizeResultsPage{
		form: OptimizeForm{
			Symbol:     "BTCUSDT",
			Timeframe:  optimizeTimeframes[1],
			StartDate:  now.AddDate(-1, 0, 0),
			EndDate:    now,
			ParamX:     paramNameOptions[0],
			ParamY:     paramNameOptions[1],
			RangeX:     apiclient.ParamRange{Min: 10, Max: 20, Step: 2},
			RangeY:     apiclient.ParamRange{Min: 1, Max: 3, Step: 0.5},
			RankMetric: rankMetrics[0],
		},
		displayMetric: rankMetrics[0],
	}
}

func (p *OptimizeResultsPage) Set(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = d
	return p
}

func (p *OptimizeResultsPage) fetchStrategies() {
	if p.Dependencies == nil || p.Dependencies.API == nil {
		return
	}
	strats, err := p.Dependencies.API.ListStrategies(context.Background())
	if err == nil {
		p.strategies = strats
		if p.selectedStrat >= len(p.strategies) && len(p.strategies) > 0 {
			p.selectedStrat = 0
		}
		if len(p.strategies) > 0 && p.selectedStrat < len(p.strategies) {
			if syms := p.strategies[p.selectedStrat].MonitoredSymbols; len(syms) > 0 {
				p.form.Symbol = syms[0]
			}
		}
	}
}

// countSteps computes how many discrete values a ParamRange sweeps over —
// the "N = product of each ParamRange's step count" figure named in AC#1.
func countSteps(r apiclient.ParamRange) int {
	if r.Step <= 0 || r.Max < r.Min {
		return 0
	}
	return int(math.Floor((r.Max-r.Min)/r.Step)) + 1
}

func axisValues(r apiclient.ParamRange) []float64 {
	n := countSteps(r)
	if n <= 0 {
		return nil
	}
	values := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		values = append(values, r.Min+float64(i)*r.Step)
	}
	return values
}

func totalCombinations(form OptimizeForm) int {
	return countSteps(form.RangeX) * countSteps(form.RangeY)
}

func (p *OptimizeResultsPage) Render() ui.Drawable {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.fetchStrategies()

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

	formHeight := 8
	formPanel := p.buildFormPanel()
	formPanel.SetRect(0, startY, termWidth, startY+formHeight)

	resultArea := p.buildResultArea(termWidth)
	resultArea.SetRect(0, startY+formHeight, termWidth, endY)

	footer := widgets.NewParagraph()
	footer.Text = "[Tab] next field  [←/→] adjust  [Enter] run  [m] cycle rank metric  [F1-F7] switch page"
	if p.errBanner != "" {
		footer.Text = fmt.Sprintf("[%s](fg:red)", p.errBanner)
	}
	footer.Border = false
	footer.SetRect(0, endY, termWidth, endY+footerHeight)

	comp := &optimizeComposite{
		header:     p.Header,
		tabPane:    p.TabPane,
		form:       formPanel,
		resultArea: resultArea,
		footer:     footer,
	}
	comp.Border = false
	comp.SetRect(0, 0, termWidth, termHeight)
	return comp
}

func (p *OptimizeResultsPage) buildFormPanel() *widgets.Paragraph {
	panel := widgets.NewParagraph()
	panel.Title = "Optimization Form"
	panel.BorderStyle.Fg = ui.ColorCyan

	stratName := "(No strategies available)"
	if len(p.strategies) > 0 && p.selectedStrat < len(p.strategies) {
		stratName = p.strategies[p.selectedStrat].Name
	}

	styleFor := func(field int) string {
		if p.activeField == field {
			return "fg:black,bg:cyan"
		}
		return "fg:white"
	}

	line1 := fmt.Sprintf("Strategy: [ < %s > ](%s)   Symbol: [ %s ](%s)   Timeframe: [ < %s > ](%s)   Metric: [ < %s > ]",
		stratName, styleFor(0), p.form.Symbol, styleFor(1), p.form.Timeframe, styleFor(2), p.displayMetric)
	line2 := fmt.Sprintf("Start: [%s]  End: [%s]", p.form.StartDate.Format("2006-01-02"), p.form.EndDate.Format("2006-01-02"))
	line3 := fmt.Sprintf("Param X (cols): [ < %s > ](%s)   [%.4g .. %.4g step %.4g](%s)",
		p.form.ParamX, styleFor(3), p.form.RangeX.Min, p.form.RangeX.Max, p.form.RangeX.Step, styleFor(4))
	line4 := fmt.Sprintf("Param Y (rows): [ < %s > ](%s)   [%.4g .. %.4g step %.4g](%s)",
		p.form.ParamY, styleFor(5), p.form.RangeY.Min, p.form.RangeY.Max, p.form.RangeY.Step, styleFor(6))

	status := "Ready — Press [Enter] to run optimization"
	if p.running {
		status = "Running…"
		if p.runStatus != nil {
			status = fmt.Sprintf("Running… (%d/%d combinations)", p.runStatus.Progress, p.runStatus.TotalCombinations)
		} else {
			status = fmt.Sprintf("Running… (%d combinations)", totalCombinations(p.form))
		}
	}

	panel.Text = fmt.Sprintf("%s\n%s\n%s\n%s\n\n%s", line1, line2, line3, line4, status)
	return panel
}

func (p *OptimizeResultsPage) buildResultArea(termWidth int) ui.Drawable {
	switch {
	case p.running:
		msg := widgets.NewParagraph()
		msg.Title = "Optimization"
		msg.Text = "Running grid search — this may take a while…"
		return msg

	case p.runStatus != nil && p.runStatus.Status == "failed":
		errMsg := p.runStatus.ErrorMessage
		if errMsg == "" {
			errMsg = "optimization run failed"
		}
		return components.Error(errors.New(errMsg))

	case p.result != nil:
		return p.buildHeatmapArea(termWidth)

	default:
		msg := widgets.NewParagraph()
		msg.Title = "Optimization"
		msg.Text = "Fill the form and press [Enter] to run a hyperparameter optimization grid search."
		return msg
	}
}

func (p *OptimizeResultsPage) buildHeatmapArea(termWidth int) ui.Drawable {
	if len(p.result.Grid) == 0 {
		msg := widgets.NewParagraph()
		msg.Title = "Optimization Results"
		msg.Text = "No valid results — check parameter ranges and historical data availability"
		return msg
	}

	cellValues := make([][]float64, len(p.axisYValues))
	for yi := range p.axisYValues {
		cellValues[yi] = make([]float64, len(p.axisXValues))
	}
	for _, item := range p.result.Grid {
		if item.Metrics == nil {
			continue
		}
		yi, xi, ok := p.locateCell(item.Params)
		if !ok {
			continue
		}
		cellValues[yi][xi] = metricValue(*item.Metrics, p.displayMetric)
	}

	bestY, bestX := p.bestCellFor(p.displayMetric)
	invert := p.displayMetric == "max_drawdown_pct"

	grid := components.BuildHeatmapGrid(p.form.ParamX, p.axisXValues, p.form.ParamY, p.axisYValues, cellValues, bestY, bestX, invert)

	title := widgets.NewParagraph()
	stratName := ""
	if len(p.strategies) > 0 && p.selectedStrat < len(p.strategies) {
		stratName = p.strategies[p.selectedStrat].Name
	}
	title.Title = fmt.Sprintf("%s heatmap — %s / %s / %s", metricLabel(p.displayMetric), stratName, p.form.Symbol, p.form.Timeframe)

	summary := widgets.NewParagraph()
	summary.Border = false
	if bestY >= 0 && bestX >= 0 && bestY < len(cellValues) && bestX < len(cellValues[bestY]) {
		var bestMetrics apiclient.BacktestMetricsView
		for _, item := range p.result.Grid {
			if item.Metrics == nil {
				continue
			}
			if yi, xi, ok := p.locateCell(item.Params); ok && yi == bestY && xi == bestX {
				bestMetrics = *item.Metrics
				break
			}
		}
		summary.Text = fmt.Sprintf("Best configuration: %s=%g, %s=%g -> Sharpe %.2f, MaxDD %.1f%%, WinRate %.1f%%",
			p.form.ParamX, p.axisXValues[bestX], p.form.ParamY, p.axisYValues[bestY],
			bestMetrics.SharpeRatio, bestMetrics.MaxDrawdownPct, bestMetrics.WinRatePct)
	}

	return &heatmapComposite{title: title, grid: grid, summary: summary}
}

// locateCell maps a grid item's Params (e.g. {"rsi_period": 14,
// "stop_loss_pct": 2.0}) onto (rowIndex, colIndex) in the rendered heatmap.
func (p *OptimizeResultsPage) locateCell(params map[string]float64) (yi, xi int, ok bool) {
	xv, xok := params[p.form.ParamX]
	yv, yok := params[p.form.ParamY]
	if !xok || !yok {
		return 0, 0, false
	}
	xi = indexOfClosest(p.axisXValues, xv)
	yi = indexOfClosest(p.axisYValues, yv)
	if xi < 0 || yi < 0 {
		return 0, 0, false
	}
	return yi, xi, true
}

func indexOfClosest(values []float64, target float64) int {
	for i, v := range values {
		if math.Abs(v-target) < 1e-9 {
			return i
		}
	}
	return -1
}

// bestCellFor resolves the single best-performing cell for the given metric.
//
// When metric matches the metric the run was actually submitted with, the
// server's authoritative BestConfig (never re-derived client-side, per AC#2
// and this project's existing BacktestRunView.Passed precedent) is used to
// locate the cell. For any OTHER metric selected via [m], this is
// necessarily a client-side scan over the already-fetched grid (AC#4 —
// explicitly flagged there as a minor, accepted inconsistency with AC#2,
// since it's a display-only "what if I'd ranked by X instead" exploration).
func (p *OptimizeResultsPage) bestCellFor(metric string) (int, int) {
	if p.result == nil {
		return -1, -1
	}
	if metric == p.form.RankMetric && len(p.result.BestConfig) > 0 {
		if yi, xi, ok := p.locateCell(p.result.BestConfig); ok {
			return yi, xi
		}
	}

	invert := metric == "max_drawdown_pct"
	bestY, bestX := -1, -1
	var bestVal float64
	first := true
	for _, item := range p.result.Grid {
		if item.Metrics == nil {
			continue
		}
		yi, xi, ok := p.locateCell(item.Params)
		if !ok {
			continue
		}
		v := metricValue(*item.Metrics, metric)
		better := first
		if !first {
			if invert {
				better = v < bestVal
			} else {
				better = v > bestVal
			}
		}
		if better {
			bestVal = v
			bestY, bestX = yi, xi
			first = false
		}
	}
	return bestY, bestX
}

func metricValue(m apiclient.BacktestMetricsView, metric string) float64 {
	switch metric {
	case "max_drawdown_pct":
		return m.MaxDrawdownPct
	case "win_rate_pct":
		return m.WinRatePct
	case "profit_factor":
		if f, ok := m.ProfitFactor.(float64); ok {
			return f
		}
		return math.Inf(1) // "Infinity" (zero losing trades) sorts as best
	default: // "sharpe_ratio"
		return m.SharpeRatio
	}
}

func metricLabel(metric string) string {
	switch metric {
	case "max_drawdown_pct":
		return "Max Drawdown"
	case "win_rate_pct":
		return "Win Rate"
	case "profit_factor":
		return "Profit Factor"
	default:
		return "Sharpe Ratio"
	}
}

type optimizeComposite struct {
	ui.Block
	header     *widgets.Paragraph
	tabPane    *widgets.TabPane
	form       *widgets.Paragraph
	resultArea ui.Drawable
	footer     *widgets.Paragraph
}

func (c *optimizeComposite) Draw(buf *ui.Buffer) {
	if c.header != nil {
		c.header.Draw(buf)
	}
	if c.tabPane != nil {
		c.tabPane.Draw(buf)
	}
	if c.form != nil {
		c.form.Draw(buf)
	}
	if c.resultArea != nil {
		c.resultArea.Draw(buf)
	}
	if c.footer != nil {
		c.footer.Draw(buf)
	}
}

type heatmapComposite struct {
	ui.Block
	title   *widgets.Paragraph
	grid    ui.Drawable
	summary *widgets.Paragraph
}

func (h *heatmapComposite) SetRect(x1, y1, x2, y2 int) {
	h.Block.SetRect(x1, y1, x2, y2)
	titleHeight := 2
	summaryHeight := 2
	gridHeight := (y2 - y1) - titleHeight - summaryHeight
	if gridHeight < 3 {
		gridHeight = 3
	}
	h.title.SetRect(x1, y1, x2, y1+titleHeight)
	h.grid.SetRect(x1, y1+titleHeight, x2, y1+titleHeight+gridHeight)
	h.summary.SetRect(x1, y1+titleHeight+gridHeight, x2, y1+titleHeight+gridHeight+summaryHeight)
}

func (h *heatmapComposite) Draw(buf *ui.Buffer) {
	if h.title != nil {
		h.title.Draw(buf)
	}
	if h.grid != nil {
		h.grid.Draw(buf)
	}
	if h.summary != nil {
		h.summary.Draw(buf)
	}
}

func (p *OptimizeResultsPage) HandleEvent(e ui.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	const numFields = 7 // strategy, symbol, timeframe, paramX name, rangeX, paramY name, rangeY

	switch e.ID {
	case "<Tab>":
		p.activeField = (p.activeField + 1) % numFields
	case "<Left>", "h":
		p.adjustField(-1)
	case "<Right>", "l":
		p.adjustField(1)
	case "m":
		p.cycleDisplayMetric()
	case "<Enter>":
		p.submit()
	}

	return nil
}

func (p *OptimizeResultsPage) adjustField(dir int) {
	switch p.activeField {
	case 0: // strategy
		if len(p.strategies) == 0 {
			return
		}
		p.selectedStrat = (p.selectedStrat + dir + len(p.strategies)) % len(p.strategies)
		if syms := p.strategies[p.selectedStrat].MonitoredSymbols; len(syms) > 0 {
			p.form.Symbol = syms[0]
		}
	case 1: // symbol (cycle among the selected strategy's monitored symbols)
		if len(p.strategies) == 0 || p.selectedStrat >= len(p.strategies) {
			return
		}
		syms := p.strategies[p.selectedStrat].MonitoredSymbols
		if len(syms) < 2 {
			return
		}
		idx := 0
		for i, s := range syms {
			if s == p.form.Symbol {
				idx = i
				break
			}
		}
		idx = (idx + dir + len(syms)) % len(syms)
		p.form.Symbol = syms[idx]
	case 2: // timeframe
		idx := indexOfString(optimizeTimeframes, p.form.Timeframe)
		idx = (idx + dir + len(optimizeTimeframes)) % len(optimizeTimeframes)
		p.form.Timeframe = optimizeTimeframes[idx]
	case 3: // param X name
		idx := indexOfString(paramNameOptions, p.form.ParamX)
		idx = (idx + dir + len(paramNameOptions)) % len(paramNameOptions)
		p.form.ParamX = paramNameOptions[idx]
	case 4: // range X step (widens/narrows the sweep step)
		p.form.RangeX.Step = clampStep(p.form.RangeX.Step + float64(dir)*0.5)
	case 5: // param Y name
		idx := indexOfString(paramNameOptions, p.form.ParamY)
		idx = (idx + dir + len(paramNameOptions)) % len(paramNameOptions)
		p.form.ParamY = paramNameOptions[idx]
	case 6: // range Y step
		p.form.RangeY.Step = clampStep(p.form.RangeY.Step + float64(dir)*0.5)
	}
}

func clampStep(v float64) float64 {
	if v < 0.5 {
		return 0.5
	}
	return v
}

func indexOfString(values []string, target string) int {
	for i, v := range values {
		if v == target {
			return i
		}
	}
	return 0
}

// cycleDisplayMetric implements the [m] keybinding: sharpe_ratio ->
// max_drawdown_pct -> win_rate_pct -> profit_factor -> sharpe_ratio (AC#4).
// Before a run, it also updates the metric that will be SUBMITTED with the
// next RunOptimization call; after a result is loaded, it only changes the
// DISPLAY ranking (see bestCellFor) — no new API call is made.
func (p *OptimizeResultsPage) cycleDisplayMetric() {
	idx := indexOfString(rankMetrics, p.displayMetric)
	idx = (idx + 1) % len(rankMetrics)
	p.displayMetric = rankMetrics[idx]
	if p.result == nil {
		p.form.RankMetric = p.displayMetric
	}
}

func (p *OptimizeResultsPage) submit() {
	if p.running {
		return
	}
	if len(p.strategies) == 0 {
		p.errBanner = "No strategies available to optimize"
		return
	}
	if countSteps(p.form.RangeX) <= 0 || countSteps(p.form.RangeY) <= 0 {
		p.errBanner = "Validation error: parameter ranges must have from <= to and step > 0"
		return
	}

	selected := p.strategies[p.selectedStrat]
	req := apiclient.RunOptimizationRequest{
		StrategyID: selected.ID,
		Symbol:     p.form.Symbol,
		Timeframe:  p.form.Timeframe,
		StartDate:  p.form.StartDate,
		EndDate:    p.form.EndDate,
		ParamGrid: map[string]apiclient.ParamRange{
			p.form.ParamX: p.form.RangeX,
			p.form.ParamY: p.form.RangeY,
		},
	}

	p.form.RankMetric = p.displayMetric
	p.axisXValues = axisValues(p.form.RangeX)
	p.axisYValues = axisValues(p.form.RangeY)
	p.running = true
	p.runID = nil
	p.runStatus = nil
	p.result = nil
	p.errBanner = ""
	p.stopPolling()

	go func() {
		ctx := context.Background()
		view, err := p.Dependencies.API.RunOptimization(ctx, req)
		p.mu.Lock()
		if err != nil {
			p.running = false
			p.errBanner = fmt.Sprintf("Failed to start optimization: %v", err)
			active := p.isActive
			p.mu.Unlock()
			if active {
				SafeRender(p.Render())
			}
			return
		}
		id := view.ID
		p.runID = &id
		p.runStatus = &view
		active := p.isActive
		p.mu.Unlock()
		if active {
			SafeRender(p.Render())
		}

		p.startPolling(id)
	}()
}

func (p *OptimizeResultsPage) startPolling(id uint) {
	p.mu.Lock()
	stop := make(chan struct{})
	p.stop = stop
	p.mu.Unlock()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ctx := context.Background()
			view, err := p.Dependencies.API.GetOptimization(ctx, id)
			if err != nil {
				continue
			}

			p.mu.Lock()
			p.runStatus = &view
			done := view.Status == "completed" || view.Status == "failed"
			active := p.isActive
			p.mu.Unlock()
			if active {
				SafeRender(p.Render())
			}

			if !done {
				continue
			}

			if view.Status == "failed" {
				p.mu.Lock()
				p.running = false
				active := p.isActive
				p.mu.Unlock()
				if active {
					SafeRender(p.Render())
				}
				return
			}

			results, err := p.Dependencies.API.GetOptimizationResults(ctx, id)
			p.mu.Lock()
			p.running = false
			if err != nil {
				p.errBanner = fmt.Sprintf("Failed to fetch optimization results: %v", err)
			} else {
				p.result = &results
			}
			active = p.isActive
			p.mu.Unlock()
			if active {
				SafeRender(p.Render())
			}
			return
		}
	}
}

func (p *OptimizeResultsPage) stopPolling() {
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
}

func (p *OptimizeResultsPage) StartSync() {
	p.mu.Lock()
	p.isActive = true
	p.mu.Unlock()
}

func (p *OptimizeResultsPage) StopSync() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.isActive = false
	p.stopPolling()
}
