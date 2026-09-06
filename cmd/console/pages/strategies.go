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
	Header              *widgets.Paragraph
	TabPane             *widgets.TabPane
	Dependencies        *dependencies.Dependencies
	strategies          []apiclient.StrategyView
	selectedRow         int
	detailVisible       bool
	detailLoading       bool
	detailData          *apiclient.StrategyPerformanceView
	errBanner           string
	errBannerUntil      time.Time
	pendingModes        map[uint]string // mode changes pending next cycle
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
	if p.detailVisible && p.selectedRow < len(p.strategies) {
		selectedStrat := p.strategies[p.selectedRow]
		overlay = p.buildDetailOverlay(selectedStrat, termWidth, termHeight)
	}

	comp := &strategiesComposite{
		header:  p.Header,
		tabPane: p.TabPane,
		table:   table,
		footer:  footer,
		overlay: overlay,
	}
	comp.Border = false
	comp.SetRect(0, 0, termWidth, termHeight)
	return comp
}

func (p *StrategiesPage) buildDetailOverlay(strat apiclient.StrategyView, termWidth, termHeight int) *widgets.Paragraph {
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
		return overlay
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

	overlay.Text = fmt.Sprintf("Config:\n%s\n\n%s\n\n[Esc] close", configStr, perfSummary)
	return overlay
}

type strategiesComposite struct {
	ui.Block
	header  *widgets.Paragraph
	tabPane *widgets.TabPane
	table   *widgets.Table
	footer  *widgets.Paragraph
	overlay *widgets.Paragraph
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
}

func (p *StrategiesPage) HandleEvent(e ui.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.detailVisible {
		if e.ID == "<Escape>" || e.ID == "<Enter>" {
			p.detailVisible = false
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
				p.mu.Unlock()
				SafeRender(p.Render())
			}()
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
				p.mu.Unlock()
				SafeRender(p.Render())
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
				p.mu.Unlock()
				SafeRender(p.Render())
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
				p.mu.Unlock()
				SafeRender(p.Render())
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

func (p *StrategiesPage) StartSync() {}
func (p *StrategiesPage) StopSync()  {}
