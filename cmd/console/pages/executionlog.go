package pages

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go-trade-bot/cmd/console/apiclient"
	"go-trade-bot/cmd/console/dependencies"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type LogEvent struct {
	Timestamp time.Time
	Strategy  string
	Symbol    string
	EventType string // "opened", "closed_profit", "closed_loss", "signal_generated", "cycle_no_signal", "system_event"
	Price     float64
	Profit    float64
	Message   string
}

type ExecutionLogPage struct {
	Header       *widgets.Paragraph
	TabPane      *widgets.TabPane
	Dependencies *dependencies.Dependencies
	events       []LogEvent
	knownSignals map[uint]apiclient.SignalView
	filterType   string
	filterQuery  string
	filtering    bool
	searching    bool
	cancel       context.CancelFunc
	isActive     bool
	mu           sync.RWMutex
}

func NewExecutionLogPage() *ExecutionLogPage {
	return &ExecutionLogPage{
		knownSignals: make(map[uint]apiclient.SignalView),
		events:       make([]LogEvent, 0, 500),
	}
}

func (p *ExecutionLogPage) Set(header *widgets.Paragraph, tabPane *widgets.TabPane, d *dependencies.Dependencies) Page {
	p.Header = header
	p.TabPane = tabPane
	p.Dependencies = d
	return p
}

func (p *ExecutionLogPage) Render() ui.Drawable {
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

	filterBarHeight := 3
	filterBar := widgets.NewParagraph()
	filterTypeDisplay := "All types"
	if p.filterType != "" {
		filterTypeDisplay = p.filterType
	}
	searchDisplay := p.filterQuery
	if searchDisplay == "" {
		searchDisplay = "(none)"
	}
	filterBar.Title = "Filters"
	filterBar.Text = fmt.Sprintf("Filter: [ %s ]    Search: [ %s ]", filterTypeDisplay, searchDisplay)
	filterBar.BorderStyle.Fg = ui.ColorCyan
	filterBar.SetRect(0, startY, termWidth, startY+filterBarHeight)

	// Feed list widget
	feedList := widgets.NewList()
	feedList.Title = "Execution Log"
	feedList.SetRect(0, startY+filterBarHeight, termWidth, endY)

	var items []string
	// Filter and format items
	for i := len(p.events) - 1; i >= 0; i-- {
		ev := p.events[i]
		if p.filterType != "" && ev.EventType != p.filterType {
			continue
		}
		if p.filterQuery != "" {
			q := strings.ToLower(p.filterQuery)
			if !strings.Contains(strings.ToLower(ev.Strategy), q) &&
				!strings.Contains(strings.ToLower(ev.Symbol), q) &&
				!strings.Contains(strings.ToLower(ev.Message), q) {
				continue
			}
		}

		timeStr := ev.Timestamp.Format("15:04:05")
		var line string
		switch ev.EventType {
		case "opened":
			line = fmt.Sprintf("%s  %-12s  %-8s  [● position opened](fg:green)      $%.2f",
				timeStr, ev.Strategy, ev.Symbol, ev.Price)
		case "closed_profit":
			line = fmt.Sprintf("%s  %-12s  %-8s  [● position closed (profit)](fg:blue) $%.2f   +$%.2f",
				timeStr, ev.Strategy, ev.Symbol, ev.Price, ev.Profit)
		case "closed_loss":
			line = fmt.Sprintf("%s  %-12s  %-8s  [● position closed (loss)](fg:red)   $%.2f   -$%.2f",
				timeStr, ev.Strategy, ev.Symbol, ev.Price, -ev.Profit)
		case "signal_generated":
			line = fmt.Sprintf("%s  %-12s  %-8s  [● signal generated](fg:yellow)         $%.2f",
				timeStr, ev.Strategy, ev.Symbol, ev.Price)
		case "cycle_no_signal":
			line = fmt.Sprintf("%s  %-12s  %-8s  [● strategy cycle (no signal)](fg:gray)",
				timeStr, ev.Strategy, ev.Symbol)
		case "system_event":
			line = fmt.Sprintf("%s  %-12s  %-8s  [● system event: %s](fg:white)",
				timeStr, ev.Strategy, ev.Symbol, ev.Message)
		default:
			line = fmt.Sprintf("%s  %-12s  %-8s  ● %s", timeStr, ev.Strategy, ev.Symbol, ev.Message)
		}
		items = append(items, line)
	}

	if len(items) == 0 {
		items = []string{"(No log events recorded yet)"}
	}
	feedList.Rows = items
	feedList.TextStyle = ui.NewStyle(ui.ColorWhite)

	footer := widgets.NewParagraph()
	footer.Text = "[f] filter by type   [/] search   [Esc] clear filters   [F1-F6] switch page"
	footer.Border = false
	footer.SetRect(0, endY, termWidth, endY+footerHeight)

	comp := &executionLogComposite{
		header:    p.Header,
		tabPane:   p.TabPane,
		filterBar: filterBar,
		feedList:  feedList,
		footer:    footer,
	}
	comp.Border = false
	comp.SetRect(0, 0, termWidth, termHeight)
	return comp
}

type executionLogComposite struct {
	ui.Block
	header    *widgets.Paragraph
	tabPane   *widgets.TabPane
	filterBar *widgets.Paragraph
	feedList  *widgets.List
	footer    *widgets.Paragraph
}

func (c *executionLogComposite) Draw(buf *ui.Buffer) {
	if c.header != nil {
		c.header.Draw(buf)
	}
	if c.tabPane != nil {
		c.tabPane.Draw(buf)
	}
	if c.filterBar != nil {
		c.filterBar.Draw(buf)
	}
	if c.feedList != nil {
		c.feedList.Draw(buf)
	}
	if c.footer != nil {
		c.footer.Draw(buf)
	}
}

func (p *ExecutionLogPage) HandleEvent(e ui.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch e.ID {
	case "f":
		types := []string{"", "opened", "closed_profit", "closed_loss", "signal_generated", "cycle_no_signal", "system_event"}
		nextIdx := 0
		for i, t := range types {
			if t == p.filterType {
				nextIdx = (i + 1) % len(types)
				break
			}
		}
		p.filterType = types[nextIdx]

	case "/":
		if p.filterQuery == "" {
			p.filterQuery = "btc"
		} else if p.filterQuery == "btc" {
			p.filterQuery = "eth"
		} else {
			p.filterQuery = ""
		}

	case "<Escape>":
		p.filterType = ""
		p.filterQuery = ""
	}

	return nil
}

func (p *ExecutionLogPage) addEvent(ev LogEvent) {
	if len(p.events) >= 500 {
		p.events = p.events[1:]
	}
	p.events = append(p.events, ev)
}

func (p *ExecutionLogPage) pollAndDiff() {
	if p.Dependencies == nil || p.Dependencies.API == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 1. Poll signals
	signals, err := p.Dependencies.API.GetAllSignals(ctx)
	if err == nil {
		p.mu.Lock()
		for _, sig := range signals {
			oldSig, existed := p.knownSignals[sig.ID]
			price := 0.0
			profit := 0.0
			if len(sig.Orders) > 0 {
				price = float64(sig.Orders[0].EntryPrice)
				profit = float64(sig.Orders[0].Profit)
			}

			if !existed {
				p.addEvent(LogEvent{
					Timestamp: sig.CreatedAt,
					Strategy:  sig.Strategy.Name,
					Symbol:    sig.Symbol,
					EventType: "opened",
					Price:     price,
				})
			} else if oldSig.Status == "open" && sig.Status == "closed" {
				evType := "closed_loss"
				if profit >= 0 {
					evType = "closed_profit"
				}
				p.addEvent(LogEvent{
					Timestamp: sig.UpdatedAt,
					Strategy:  sig.Strategy.Name,
					Symbol:    sig.Symbol,
					EventType: evType,
					Price:     price,
					Profit:    profit,
				})
			}
			p.knownSignals[sig.ID] = sig
		}
		p.mu.Unlock()
	}

	// 2. Poll strategy executions
	execs, err := p.Dependencies.API.ListStrategyExecutions(ctx, time.Now().Add(-10*time.Second))
	if err == nil && len(execs) > 0 {
		p.mu.Lock()
		for _, ex := range execs {
			evType := "cycle_no_signal"
			if ex.EventType == "system_event" || strings.Contains(strings.ToLower(ex.Message), "error") {
				evType = "system_event"
			}
			p.addEvent(LogEvent{
				Timestamp: ex.Timestamp,
				Strategy:  ex.StrategyName,
				Symbol:    ex.Symbol,
				EventType: evType,
				Message:   ex.Message,
			})
		}
		p.mu.Unlock()
	}
}

func (p *ExecutionLogPage) StartSync() {
	p.StopSync()

	p.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.isActive = true
	p.mu.Unlock()

	go func() {
		p.pollAndDiff()

		p.mu.RLock()
		active := p.isActive
		p.mu.RUnlock()
		if active && ctx.Err() == nil {
			SafeRender(p.Render())
		}

		ticker := time.NewTicker(5 * time.Second)
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

				p.pollAndDiff()

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

func (p *ExecutionLogPage) StopSync() {
	p.mu.Lock()
	p.isActive = false
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.mu.Unlock()
}
