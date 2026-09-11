package report

import (
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go-trade-bot/internal/metrics_provider"
)

type BacktestReportInput struct {
	RunID        uint
	StrategyName string
	Symbol       string
	StartDate    time.Time
	EndDate      time.Time
	Metrics      metrics_provider.BacktestMetrics
	Trades       []metrics_provider.TradeLogEntry
}

type MonthlyPnL struct {
	Month  string
	Profit float64
	Trades int
	Wins   int
}

type reportTemplateData struct {
	BacktestReportInput
	ProfitFactorDisplay string
	EquitySVG           template.HTML
	DrawdownSVG         template.HTML
	MonthlyPnL          []MonthlyPnL
	FormattedDuration   string
}

func Generate(outputDir string, run BacktestReportInput) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory %q: %w", outputDir, err)
	}

	sanitizedStrategy := strings.ReplaceAll(run.StrategyName, " ", "_")
	fileName := fmt.Sprintf("%s_%s_%d.html", sanitizedStrategy, run.Symbol, run.RunID)
	if run.RunID == 0 {
		fileName = fmt.Sprintf("%s_%s_%d.html", sanitizedStrategy, run.Symbol, time.Now().UnixNano())
	}
	filePath := filepath.Join(outputDir, fileName)

	// Format Profit Factor
	pfDisplay := fmt.Sprintf("%.2f", run.Metrics.ProfitFactor)
	if math.IsInf(run.Metrics.ProfitFactor, 1) {
		pfDisplay = "∞ (no losing trades)"
	} else if math.IsNaN(run.Metrics.ProfitFactor) || run.Metrics.TotalTrades == 0 {
		pfDisplay = "N/A"
	}

	// Format Duration
	durDisplay := run.Metrics.AvgTradeDuration.Round(time.Second).String()
	if run.Metrics.AvgTradeDuration == 0 {
		durDisplay = "N/A"
	}

	// Generate Charts
	equitySVG := renderEquitySVG(run.Metrics.EquityCurve)
	drawdownSVG := renderDrawdownSVG(run.Metrics.EquityCurve)

	// Calculate Monthly PnL
	monthlyPnL := calculateMonthlyPnL(run.Trades)

	data := reportTemplateData{
		BacktestReportInput: run,
		ProfitFactorDisplay: pfDisplay,
		EquitySVG:           template.HTML(equitySVG),
		DrawdownSVG:         template.HTML(drawdownSVG),
		MonthlyPnL:          monthlyPnL,
		FormattedDuration:   durDisplay,
	}

	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"float": func(a int) float64 { return float64(a) },
		"mul": func(a, b float64) float64 { return a * b },
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(reportHTMLTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create report file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("failed to execute report template: %w", err)
	}

	return filePath, nil
}

func calculateMonthlyPnL(trades []metrics_provider.TradeLogEntry) []MonthlyPnL {
	mMap := make(map[string]*MonthlyPnL)

	for _, t := range trades {
		date := t.ExitTime
		if date.IsZero() {
			date = t.EntryTime
		}
		if date.IsZero() {
			continue
		}
		key := date.Format("2006-01")
		if _, ok := mMap[key]; !ok {
			mMap[key] = &MonthlyPnL{Month: key}
		}
		mMap[key].Profit += t.Profit
		mMap[key].Trades++
		if t.Profit > 0 {
			mMap[key].Wins++
		}
	}

	keys := make([]string, 0, len(mMap))
	for k := range mMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]MonthlyPnL, len(keys))
	for i, k := range keys {
		result[i] = *mMap[k]
	}
	return result
}

func renderEquitySVG(curve []metrics_provider.EquityPoint) string {
	if len(curve) < 2 {
		return `<svg viewBox="0 0 600 200" width="100%" height="200"><line x1="50" y1="100" x2="550" y2="100" stroke="#3b82f6" stroke-width="2"/><text x="300" y="90" fill="#9ca3af" text-anchor="middle" font-size="12">Flat Equity Curve</text></svg>`
	}

	minVal := curve[0].Value
	maxVal := curve[0].Value
	for _, p := range curve {
		if p.Value < minVal {
			minVal = p.Value
		}
		if p.Value > maxVal {
			maxVal = p.Value
		}
	}

	if maxVal == minVal {
		maxVal += 10
		minVal -= 10
	}

	width := 600.0
	height := 200.0
	padX := 50.0
	padY := 30.0

	plotWidth := width - 2*padX
	plotHeight := height - 2*padY

	var pathBuilder strings.Builder
	for i, p := range curve {
		x := padX + (float64(i)/float64(len(curve)-1))*plotWidth
		normalizedY := (p.Value - minVal) / (maxVal - minVal)
		y := padY + (1.0-normalizedY)*plotHeight

		if i == 0 {
			pathBuilder.WriteString(fmt.Sprintf("M %.2f,%.2f", x, y))
		} else {
			pathBuilder.WriteString(fmt.Sprintf(" L %.2f,%.2f", x, y))
		}
	}

	return fmt.Sprintf(`
	<svg viewBox="0 0 %.0f %.0f" width="100%%" height="200" style="background:#1e293b;border-radius:8px;">
		<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#334155" stroke-width="1"/>
		<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#334155" stroke-width="1"/>
		<text x="%.0f" y="%.0f" fill="#94a3b8" font-size="10" text-anchor="end">%.2f</text>
		<text x="%.0f" y="%.0f" fill="#94a3b8" font-size="10" text-anchor="end">%.2f</text>
		<path d="%s" fill="none" stroke="#10b981" stroke-width="2"/>
	</svg>`,
		width, height,
		padX, padY, width-padX, padY,
		padX, height-padY, width-padX, height-padY,
		padX-5, padY+4, maxVal,
		padX-5, height-padY+4, minVal,
		pathBuilder.String(),
	)
}

func renderDrawdownSVG(curve []metrics_provider.EquityPoint) string {
	if len(curve) < 2 {
		return `<svg viewBox="0 0 600 150" width="100%" height="150"><line x1="50" y1="30" x2="550" y2="30" stroke="#ef4444" stroke-width="2"/><text x="300" y="75" fill="#9ca3af" text-anchor="middle" font-size="12">0% Drawdown</text></svg>`
	}

	drawdowns := make([]float64, len(curve))
	peak := curve[0].Value
	maxDD := 0.0

	for i, p := range curve {
		if p.Value > peak {
			peak = p.Value
		}
		if peak > 0 {
			dd := (peak - p.Value) / peak * 100.0
			drawdowns[i] = dd
			if dd > maxDD {
				maxDD = dd
			}
		}
	}

	if maxDD == 0 {
		maxDD = 10.0
	}

	width := 600.0
	height := 150.0
	padX := 50.0
	padY := 25.0

	plotWidth := width - 2*padX
	plotHeight := height - 2*padY

	var pathBuilder strings.Builder
	for i, dd := range drawdowns {
		x := padX + (float64(i)/float64(len(drawdowns)-1))*plotWidth
		y := padY + (dd/maxDD)*plotHeight

		if i == 0 {
			pathBuilder.WriteString(fmt.Sprintf("M %.2f,%.2f", x, y))
		} else {
			pathBuilder.WriteString(fmt.Sprintf(" L %.2f,%.2f", x, y))
		}
	}

	return fmt.Sprintf(`
	<svg viewBox="0 0 %.0f %.0f" width="100%%" height="150" style="background:#1e293b;border-radius:8px;">
		<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#334155" stroke-width="1"/>
		<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#334155" stroke-width="1"/>
		<text x="%.0f" y="%.0f" fill="#94a3b8" font-size="10" text-anchor="end">0.0%%</text>
		<text x="%.0f" y="%.0f" fill="#94a3b8" font-size="10" text-anchor="end">-%.1f%%</text>
		<path d="%s" fill="none" stroke="#ef4444" stroke-width="2"/>
	</svg>`,
		width, height,
		padX, padY, width-padX, padY,
		padX, height-padY, width-padX, height-padY,
		padX-5, padY+4,
		padX-5, height-padY+4, maxDD,
		pathBuilder.String(),
	)
}

const reportHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Backtest Report - {{.StrategyName}} ({{.Symbol}})</title>
	<style>
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
			background-color: #0f172a;
			color: #f8fafc;
			margin: 0;
			padding: 24px;
		}
		.container {
			max-width: 1100px;
			margin: 0 auto;
		}
		.header {
			border-bottom: 1px solid #334155;
			padding-bottom: 16px;
			margin-bottom: 24px;
		}
		.header h1 {
			margin: 0 0 8px 0;
			color: #38bdf8;
		}
		.meta {
			color: #94a3b8;
			font-size: 14px;
		}
		.grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
			gap: 16px;
			margin-bottom: 24px;
		}
		.card {
			background-color: #1e293b;
			padding: 16px;
			border-radius: 8px;
			border: 1px solid #334155;
		}
		.card .label {
			color: #94a3b8;
			font-size: 12px;
			text-transform: uppercase;
			margin-bottom: 4px;
		}
		.card .value {
			font-size: 20px;
			font-weight: bold;
		}
		.positive { color: #10b981; }
		.negative { color: #ef4444; }
		.section {
			background-color: #1e293b;
			padding: 20px;
			border-radius: 8px;
			border: 1px solid #334155;
			margin-bottom: 24px;
		}
		.section h2 {
			margin: 0 0 16px 0;
			font-size: 18px;
			color: #e2e8f0;
		}
		table {
			width: 100%;
			border-collapse: collapse;
			font-size: 14px;
		}
		th, td {
			padding: 10px 12px;
			text-align: left;
			border-bottom: 1px solid #334155;
		}
		th {
			color: #94a3b8;
			font-weight: 600;
		}
		tr:hover {
			background-color: #334155;
		}
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h1>Backtest Report: {{.StrategyName}}</h1>
			<div class="meta">
				Symbol: <strong>{{.Symbol}}</strong> | 
				Range: <strong>{{.StartDate.Format "2006-01-02"}}</strong> to <strong>{{.EndDate.Format "2006-01-02"}}</strong>
			</div>
		</div>

		<div class="grid">
			<div class="card">
				<div class="label">Total Return</div>
				<div class="value {{if ge .Metrics.TotalReturnPct 0.0}}positive{{else}}negative{{end}}">
					{{printf "%.2f" .Metrics.TotalReturnPct}}%
				</div>
			</div>
			<div class="card">
				<div class="label">Sharpe Ratio</div>
				<div class="value">{{printf "%.2f" .Metrics.SharpeRatio}}</div>
			</div>
			<div class="card">
				<div class="label">Max Drawdown</div>
				<div class="value negative">{{printf "%.2f" .Metrics.MaxDrawdownPct}}%</div>
			</div>
			<div class="card">
				<div class="label">Win Rate</div>
				<div class="value">{{printf "%.1f" .Metrics.WinRatePct}}%</div>
			</div>
			<div class="card">
				<div class="label">Profit Factor</div>
				<div class="value">{{.ProfitFactorDisplay}}</div>
			</div>
			<div class="card">
				<div class="label">Total Trades</div>
				<div class="value">{{.Metrics.TotalTrades}}</div>
			</div>
			<div class="card">
				<div class="label">Avg Duration</div>
				<div class="value">{{.FormattedDuration}}</div>
			</div>
		</div>

		<div class="section">
			<h2>Equity Curve</h2>
			{{.EquitySVG}}
		</div>

		<div class="section">
			<h2>Drawdown Underwater Curve</h2>
			{{.DrawdownSVG}}
		</div>

		{{if .MonthlyPnL}}
		<div class="section">
			<h2>Monthly Performance</h2>
			<table>
				<thead>
					<tr>
						<th>Month</th>
						<th>Trades</th>
						<th>Win Rate</th>
						<th>Profit ($)</th>
					</tr>
				</thead>
				<tbody>
					{{range .MonthlyPnL}}
					<tr>
						<td>{{.Month}}</td>
						<td>{{.Trades}}</td>
						<td>{{printf "%.1f" (mul (div (float .Wins) (float .Trades)) 100.0)}}%</td>
						<td class="{{if ge .Profit 0.0}}positive{{else}}negative{{end}}">{{printf "%.2f" .Profit}}</td>
					</tr>
					{{end}}
				</tbody>
			</table>
		</div>
		{{end}}

		<div class="section">
			<h2>Trade Log ({{len .Trades}} trades)</h2>
			{{if .Trades}}
			<table>
				<thead>
					<tr>
						<th>#</th>
						<th>Entry Time</th>
						<th>Exit Time</th>
						<th>Entry Price</th>
						<th>Exit Price</th>
						<th>Qty</th>
						<th>Profit ($)</th>
						<th>Reason</th>
					</tr>
				</thead>
				<tbody>
					{{range $i, $t := .Trades}}
					<tr>
						<td>{{add $i 1}}</td>
						<td>{{$t.EntryTime.Format "2006-01-02 15:04"}}</td>
						<td>{{if not $t.ExitTime.IsZero}}{{$t.ExitTime.Format "2006-01-02 15:04"}}{{else}}Open{{end}}</td>
						<td>{{printf "%.2f" $t.EntryPrice}}</td>
						<td>{{printf "%.2f" $t.ExitPrice}}</td>
						<td>{{printf "%.4f" $t.Quantity}}</td>
						<td class="{{if ge $t.Profit 0.0}}positive{{else}}negative{{end}}">{{printf "%.2f" $t.Profit}}</td>
						<td>{{$t.ExitReason}}</td>
					</tr>
					{{end}}
				</tbody>
			</table>
			{{else}}
			<p style="color:#94a3b8;">No trades executed in this backtest run.</p>
			{{end}}
		</div>
	</div>
</body>
</html>`


