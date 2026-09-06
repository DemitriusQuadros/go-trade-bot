package pages

import (
	"bytes"
	"context"
	"encoding/json"
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

type StrategiesPage struct {
	Header         *widgets.Paragraph
	TabPane        *widgets.TabPane
	Dependencies   *dependencies.Dependencies
	strategies     []apiclient.StrategyView
	selectedRow    int
	detailVisible  bool
	detailLoading  bool
	detailData     *apiclient.StrategyPerformanceView
	errBanner      string
	errBannerUntil time.Time
	pendingModes   map[uint]string // mode changes pending next cycle

	// P&L history sparkline (spec tui-02, Phase 4) — independent
	// loading/error state from detailData/detailLoading above, per AC#2's
	// per-panel-independent-loading principle.
	historyData    []apiclient.PerformanceHistoryPointView // nil while loading/before first fetch
	historyLoading bool
	historyErr     error
	historyBucket  string // "daily" | "weekly" | "monthly", default "daily"
	historySymbol  string // defaults to MonitoredSymbols[0] on overlay open

	isActive            bool
	mu                  sync.RWMutex
	OnBacktestRequested func(strategyID uint)
}

func NewStrategiesPage() *StrategiesPage {
	return &StrategiesPage{
		pendingModes: make(map[uint]string),
	}
}

func (p *StrategiesPage) Set(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = d
	return p
}

func (p *StrategiesPage) Render() ui.Drawable {
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

	table, strats := components.StrategyTableFull(context.Background(), p.Dependencies.API)
	p.strategies = strats
	table.SetRect(0, startY, termWidth, endY)

	// Clamp selected row
	if len(p.strategies) > 0 {
		if p.selectedRow < 0 {
			p.selectedRow = 0
		}
		if p.selectedRow >= len(p.strategies) {
			p.selectedRow = len(p.strategies) - 1
		}
		// Highlight selected row in table
		table.RowStyles[p.selectedRow+1] = ui.NewStyle(ui.ColorBlack, ui.ColorCyan, ui.ModifierBold)
	}

	footer := widgets.NewParagraph()
	footer.Text = "[Enter] details  [e] enable  [d] disable  [b] backtest  [r] switch mode  [↑/↓] navigate"
	if time.Now().Before(p.errBannerUntil) && p.errBanner != "" {
		footer.Text = fmt.Sprintf("[%s](fg:red)", p.errBanner)
	}
	footer.Border = false
	footer.SetRect(0, endY, termWidth, endY+footerHeight)

	var overlay *widgets.Paragraph
	var historySection ui.Drawable
	var historyFooter *widgets.Paragraph
	if p.detailVisible && p.selectedRow < len(p.strategies) {
		selectedStrat := p.strategies[p.selectedRow]
		overlay, historySection, historyFooter = p.buildDetailOverlay(selectedStrat, termWidth, termHeight)
	}

	comp := &strategiesComposite{
		header:        p.Header,
		tabPane:       p.TabPane,
		table:         table,
		footer:        footer,
		overlay:       overlay,
		history:       historySection,
		historyFooter: historyFooter,
	}
	comp.Border = false
	comp.SetRect(0, 0, termWidth, termHeight)
	return comp
}

// buildDetailOverlay returns the base config/perf overlay panel plus two
// additional drawables for the P&L history sparkline section (spec tui-02,
// Phase 4): the sparkline (or its loading/error/empty placeholder), and a
// small footer line with the latest-bucket P&L readout and keybinding hints.
// These are separate widgets (not appended into overlay.Text) so the history
// section can render/refresh independently of the config+lifetime-P&L block,
// per Acceptance Criterion #2's per-panel-independent-loading principle.
func (p *StrategiesPage) buildDetailOverlay(strat apiclient.StrategyView, termWidth, termHeight int) (*widgets.Paragraph, ui.Drawable, *widgets.Paragraph) {
	overlay := widgets.NewParagraph()
	overlay.Title = fmt.Sprintf(" %s — detail ", strat.Name)
	overlay.BorderStyle.Fg = ui.ColorGreen

	overlayWidth := int(float64(termWidth) * 0.65)
	if overlayWidth < 60 {
		overlayWidth = 60
	}
	overlayHeight := int(float64(termHeight) * 0.65)
	if overlayHeight < 15 {
		overlayHeight = 15
	}

	x1 := (termWidth - overlayWidth) / 2
	y1 := (termHeight - overlayHeight) / 2
	overlay.SetRect(x1, y1, x1+overlayWidth, y1+overlayHeight)

	if p.detailLoading {
		overlay.Text = "\n Loading strategy details and performance..."
		return overlay, nil, nil
	}

	configStr := "(no configuration)"
	if len(strat.StrategyConfiguration.Configuration) > 0 && string(strat.StrategyConfiguration.Configuration) != "null" {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, strat.StrategyConfiguration.Configuration, "", "  "); err == nil {
			configStr = pretty.String()
		} else {
			configStr = string(strat.StrategyConfiguration.Configuration)
		}
	}

	perfSummary := "P&L since activation: N/A   Total trades: 0\nWin rate: N/A"
	if p.detailData != nil {
		winRate := "N/A"
		perfSummary = fmt.Sprintf("P&L since activation: $%+0.2f   Total trades: %d\nWin rate: %s",
			p.detailData.Profit, p.detailData.Trades, winRate)
	}

	overlay.Text = fmt.Sprintf("Config:\n%s\n\n%s", configStr, perfSummary)

	// History section rect: reserve the bottom of the overlay's inner area
	// for the sparkline + summary/hint line, below the config/perf text
	// above — the overlay's own fixed ~65%-of-terminal height already leaves
	// headroom for this (spec tui-02 Target Behavior, "Overlay layout
	// addition"), no resize needed.
	sparklineHeight := 4
	footerHeight := 1
	histX1 := overlay.Inner.Min.X
	histX2 := overlay.Inner.Max.X
	histY2 := overlay.Inner.Max.Y
	footerY1 := histY2 - footerHeight
	sparkY1 := footerY1 - sparklineHeight

	historySection := p.buildHistorySection()
	historySection.SetRect(histX1, sparkY1, histX2, footerY1)

	historyFooter := p.buildHistoryFooter(strat)
	historyFooter.SetRect(histX1, footerY1, histX2, histY2)

	return overlay, historySection, historyFooter
}

// bucketLimitFor returns how many buckets to request per granularity,
// matching the wireframe's "last 30d"/"last 12w"/"last 6m" convention.
func bucketLimitFor(bucket string) int {
	switch bucket {
	case "weekly":
		return 12
	case "monthly":
		return 6
	default:
		return 30
	}
}

func bucketRangeLabel(bucket string) string {
	switch bucket {
	case "weekly":
		return fmt.Sprintf("last %dw", bucketLimitFor(bucket))
	case "monthly":
		return fmt.Sprintf("last %dm", bucketLimitFor(bucket))
	default:
		return fmt.Sprintf("last %dd", bucketLimitFor(bucket))
	}
}

func bucketPeriodLabel(bucket string) string {
	switch bucket {
	case "weekly":
		return "this week"
	case "monthly":
		return "this month"
	default:
		return "today"
	}
}

// buildHistorySection renders the sparkline itself, or the appropriate
// loading/error/empty placeholder — AC#2 (independent loading), AC#4 (empty
// slice gets "No history yet", never an empty sparkline call), AC#5 (a
// failed fetch shows components.Error scoped to just this section).
func (p *StrategiesPage) buildHistorySection() ui.Drawable {
	title := fmt.Sprintf("P&L History — %s (%s, %s)", p.historySymbol, p.historyBucket, bucketRangeLabel(p.historyBucket))

	if p.historyLoading {
		para := widgets.NewParagraph()
		para.Title = title
		para.Text = "Loading…"
		return para
	}
	if p.historyErr != nil {
		errWidget := components.Error(p.historyErr)
		errWidget.Title = title
		return errWidget
	}
	if len(p.historyData) == 0 {
		para := widgets.NewParagraph()
		para.Title = title
		para.Text = "No history yet"
		return para
	}

	profits := make([]float64, len(p.historyData))
	for i, pt := range p.historyData {
		profits[i] = pt.Profit
	}
	return components.BuildSparkline(title, profits, ui.ColorGreen)
}

// buildHistoryFooter renders the most-recent-bucket P&L readout plus the
// [g]/[y] keybinding hints (AC#6, "Symbol selection" — [y] only shown for
// multi-symbol strategies).
func (p *StrategiesPage) buildHistoryFooter(strat apiclient.StrategyView) *widgets.Paragraph {
	footer := widgets.NewParagraph()
	footer.Border = false

	summary := ""
	if !p.historyLoading && p.historyErr == nil && len(p.historyData) > 0 {
		latest := p.historyData[len(p.historyData)-1]
		color := "green"
		if latest.Profit < 0 {
			color = "red"
		}
		summary = fmt.Sprintf("[+$%.2f %s](fg:%s)   ", latest.Profit, bucketPeriodLabel(p.historyBucket), color)
	}

	hint := "[g] toggle bucket"
	if len(strat.MonitoredSymbols) > 1 {
		hint += "  [y] toggle symbol"
	}

	footer.Text = fmt.Sprintf("%s%s   [Esc] close", summary, hint)
	return footer
}

type strategiesComposite struct {
	ui.Block
	header        *widgets.Paragraph
	tabPane       *widgets.TabPane
	table         *widgets.Table
	footer        *widgets.Paragraph
	overlay       *widgets.Paragraph
	history       ui.Drawable
	historyFooter *widgets.Paragraph
}

func (s *strategiesComposite) Draw(buf *ui.Buffer) {
	if s.header != nil {
		s.header.Draw(buf)
	}
	if s.tabPane != nil {
		s.tabPane.Draw(buf)
	}
	if s.table != nil {
		s.table.Draw(buf)
	}
	if s.footer != nil {
		s.footer.Draw(buf)
	}
	if s.overlay != nil {
		s.overlay.Draw(buf)
	}
	if s.history != nil {
		s.history.Draw(buf)
	}
	if s.historyFooter != nil {
		s.historyFooter.Draw(buf)
	}
}

func (p *StrategiesPage) HandleEvent(e ui.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.detailVisible {
		switch e.ID {
		case "<Escape>", "<Enter>":
			p.detailVisible = false
		case "g":
			p.cycleHistoryBucket()
		case "y":
			p.cycleHistorySymbol()
		}
		return nil
	}

	switch e.ID {
	case "<Up>", "k":
		if p.selectedRow > 0 {
			p.selectedRow--
		}
	case "<Down>", "j":
		if p.selectedRow < len(p.strategies)-1 {
			p.selectedRow++
		}
	case "<Enter>":
		if len(p.strategies) > 0 && p.selectedRow < len(p.strategies) {
			p.detailVisible = true
			p.detailLoading = true
			selected := p.strategies[p.selectedRow]

			// AC#7: every overlay open is a fresh session — bucket/symbol
			// never persist across strategy rows or prior overlay closes.
			p.historyBucket = "daily"
			p.historySymbol = ""
			if len(selected.MonitoredSymbols) > 0 {
				p.historySymbol = selected.MonitoredSymbols[0]
			}
			p.historyLoading = true
			p.historyData = nil
			p.historyErr = nil

			// AC#1: GetStrategyPerformance and GetStrategyPerformanceHistory
			// fire concurrently (two goroutines), not sequentially, so the
			// overlay's total loading time is max() of the two, not their sum.
			go func() {
				perfs, err := p.Dependencies.API.ListStrategyPerformance(context.Background())
				p.mu.Lock()
				p.detailLoading = false
				if err == nil {
					for _, perf := range perfs {
						if perf.Name == selected.Name {
							p.detailData = &perf
							break
						}
					}
				}
				active := p.isActive
				p.mu.Unlock()
				if active {
					SafeRender(p.Render())
				}
			}()

			go p.fetchHistory(selected.ID, "daily", p.historySymbol)
		}
	case "e":
		if len(p.strategies) > 0 && p.selectedRow < len(p.strategies) {
			selected := p.strategies[p.selectedRow]
			go func() {
				_, err := p.Dependencies.API.UpdateStrategyStatus(context.Background(), selected.ID, "productive")
				p.mu.Lock()
				if err != nil {
					p.errBanner = fmt.Sprintf("Failed to enable strategy: %v", err)
					p.errBannerUntil = time.Now().Add(3 * time.Second)
				}
				active := p.isActive
				p.mu.Unlock()
				if active {
					SafeRender(p.Render())
				}
			}()
		}
	case "d":
		if len(p.strategies) > 0 && p.selectedRow < len(p.strategies) {
			selected := p.strategies[p.selectedRow]
			go func() {
				_, err := p.Dependencies.API.UpdateStrategyStatus(context.Background(), selected.ID, "disabled")
				p.mu.Lock()
				if err != nil {
					p.errBanner = fmt.Sprintf("Failed to disable strategy: %v", err)
					p.errBannerUntil = time.Now().Add(3 * time.Second)
				}
				active := p.isActive
				p.mu.Unlock()
				if active {
					SafeRender(p.Render())
				}
			}()
		}
	case "r":
		if len(p.strategies) > 0 && p.selectedRow < len(p.strategies) {
			selected := p.strategies[p.selectedRow]
			nextMode := "dryrun"
			switch strings.ToLower(selected.Mode) {
			case "dryrun":
				nextMode = "paper"
			case "paper":
				nextMode = "live"
			case "live":
				nextMode = "dryrun"
			}
			go func() {
				_, err := p.Dependencies.API.UpdateStrategyMode(context.Background(), selected.ID, nextMode)
				p.mu.Lock()
				if err != nil {
					p.errBanner = fmt.Sprintf("Failed to update mode: %v", err)
					p.errBannerUntil = time.Now().Add(3 * time.Second)
				} else {
					p.pendingModes[selected.ID] = nextMode
				}
				active := p.isActive
				p.mu.Unlock()
				if active {
					SafeRender(p.Render())
				}
			}()
		}
	case "b":
		if len(p.strategies) > 0 && p.selectedRow < len(p.strategies) {
			selected := p.strategies[p.selectedRow]
			if p.OnBacktestRequested != nil {
				p.OnBacktestRequested(selected.ID)
			}
		}
	}

	return nil
}

// fetchHistory calls GetStrategyPerformanceHistory for the given
// (strategyID, bucket, symbol) and applies the result only if that pair
// still matches the page's CURRENT (historyBucket, historySymbol) at
// resolution time — a stale-response guard for rapid [g]/[y] presses in
// succession (AC#8). Must be invoked via `go p.fetchHistory(...)`, not
// called directly while holding p.mu (it performs its own locking only
// around state writes, not around the network call itself).
func (p *StrategiesPage) fetchHistory(strategyID uint, bucket, symbol string) {
	if p.Dependencies == nil || p.Dependencies.API == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	data, err := p.Dependencies.API.GetStrategyPerformanceHistory(ctx, strategyID, symbol, bucket, bucketLimitFor(bucket))

	p.mu.Lock()
	if bucket == p.historyBucket && symbol == p.historySymbol {
		p.historyLoading = false
		p.historyErr = err
		if err == nil {
			p.historyData = data
		} else {
			p.historyData = nil
		}
	}
	active := p.isActive
	p.mu.Unlock()
	if active {
		SafeRender(p.Render())
	}
}

// cycleHistoryBucket implements the [g] keybinding (AC#3): daily -> weekly
// -> monthly -> daily, re-fetching from the server on every toggle rather
// than re-bucketing a cached daily fetch client-side (Judgment Call #1).
// Must be called while p.mu is already held (invoked from HandleEvent).
func (p *StrategiesPage) cycleHistoryBucket() {
	if p.selectedRow >= len(p.strategies) {
		return
	}
	selected := p.strategies[p.selectedRow]

	switch p.historyBucket {
	case "daily":
		p.historyBucket = "weekly"
	case "weekly":
		p.historyBucket = "monthly"
	default:
		p.historyBucket = "daily"
	}
	p.historyLoading = true
	p.historyData = nil
	p.historyErr = nil

	go p.fetchHistory(selected.ID, p.historyBucket, p.historySymbol)
}

// cycleHistorySymbol implements the [y] keybinding (AC#3a) — only active
// (and only shown in the keybinding hint) when the selected strategy
// monitors more than one symbol. Must be called while p.mu is already held.
func (p *StrategiesPage) cycleHistorySymbol() {
	if p.selectedRow >= len(p.strategies) {
		return
	}
	selected := p.strategies[p.selectedRow]
	syms := selected.MonitoredSymbols
	if len(syms) < 2 {
		return // nothing to cycle to
	}

	idx := 0
	for i, s := range syms {
		if s == p.historySymbol {
			idx = i
			break
		}
	}
	p.historySymbol = syms[(idx+1)%len(syms)]
	p.historyLoading = true
	p.historyData = nil
	p.historyErr = nil

	go p.fetchHistory(selected.ID, p.historyBucket, p.historySymbol)
}

func (p *StrategiesPage) StartSync() {
	p.mu.Lock()
	p.isActive = true
	p.mu.Unlock()
}

func (p *StrategiesPage) StopSync() {
	p.mu.Lock()
	p.isActive = false
	p.mu.Unlock()
}
