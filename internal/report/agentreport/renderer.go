package agentreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"

	"go-trade-bot/internal/i18n"
)

// Metrics are DB-resolved performance figures for a Series.
type Metrics struct {
	Sharpe           float64
	MaxDrawdown      float64
	MaxDrawdownIsPct bool // false = absolute quote-currency amount (live data has no capital base)
	WinRatePct       float64
	ProfitFactor     float64 // +Inf when there are no losing trades
	TotalTrades      int
	NetPnL           float64
}

// Trade is one closed trade.
type Trade struct {
	Symbol     string
	EntryTime  time.Time
	ExitTime   time.Time
	EntryPrice float64
	ExitPrice  float64
	Quantity   float64
	Profit     float64
	ExitReason string
}

// EquityPoint is one point of an equity curve.
type EquityPoint struct {
	Time  time.Time
	Value float64
}

// Series is everything a data-bearing block can show for one Source.
type Series struct {
	Label string // e.g. "Backtest #7 · BTCUSDT" or "Strategy #3 live · last 30 days"
	// Ref, when set, lets the renderer build the source label in the render
	// locale (i18n-02 §3); Label is then only the EN fallback.
	Ref     *SeriesRef
	Metrics Metrics
	Equity  []EquityPoint
	Trades  []Trade // oldest first
}

// SeriesRef describes a Series' source for a localized label.
type SeriesRef struct {
	Kind string // SourceBacktestRun | SourceStrategyLive
	// ID is the backtest run id (backtest_run) or the strategy id
	// (strategy_live).
	ID     uint
	Symbol string    // backtest_run
	Start  time.Time // backtest_run
	End    time.Time // backtest_run
	Name   string    // strategy_live: the strategy name
	Days   int       // strategy_live
}

// DataSource resolves block references against the DB. Implemented by
// app/repository/agentplatform.ReportDataSource over existing tables.
type DataSource interface {
	BacktestRun(ctx context.Context, id uint) (Series, error)
	StrategyLive(ctx context.Context, strategyID uint, days int) (Series, error)
	// ScriptVersion returns a stored version's source; it must belong to
	// strategyID.
	ScriptVersion(ctx context.Context, strategyID, versionID uint) (string, error)
	// CurrentScript returns the strategy's current script source.
	CurrentScript(ctx context.Context, strategyID uint) (string, error)
	// PreviousScript returns the source of the version saved before the
	// current one ("" if there is none).
	PreviousScript(ctx context.Context, strategyID uint) (string, error)
}

// ReportMeta is the header/footer data for a report.
type ReportMeta struct {
	Title         string
	Severity      string
	AgentName     string
	RunID         uint
	CreatedAt     time.Time
	StrategyNames []string
	// Locale selects the chrome/label language and <html lang>; "" = en.
	// Model-written block text is rendered as is.
	Locale i18n.Locale
}

// Renderer validates and renders blocks (interface for DI/tests).
type Renderer interface {
	Validate(blocks []Block) error
	Render(ctx context.Context, meta ReportMeta, blocks []Block) (string, error)
	RenderLocales(ctx context.Context, meta ReportMeta, blocks []Block, locales []i18n.Locale) (map[i18n.Locale]string, error)
}

// HTMLRenderer is the html/template-based Renderer.
type HTMLRenderer struct {
	data DataSource
	tmpl *template.Template
}

// templateFuncs are placeholders so the template parses; every render
// clones the template and binds them to its locale (localizedFuncs).
var templateFuncs = template.FuncMap{
	"t":   func(string) string { return "" },
	"tf":  func(string, ...any) string { return "" },
	"sev": func(string) string { return "" },
}

func localizedFuncs(loc i18n.Locale) template.FuncMap {
	return template.FuncMap{
		"t":  func(key string) string { return Messages.T(loc, key) },
		"tf": func(key string, args ...any) string { return Messages.F(loc, key, args...) },
		"sev": func(sev string) string {
			if _, ok := Messages[i18n.EN]["severity."+sev]; ok {
				return Messages.T(loc, "severity."+sev)
			}
			return sev
		},
	}
}

// NewHTMLRenderer builds an HTMLRenderer over ds.
func NewHTMLRenderer(ds DataSource) *HTMLRenderer {
	return &HTMLRenderer{data: ds, tmpl: template.Must(template.New("agentreport").Funcs(templateFuncs).Parse(reportTemplate))}
}

// Validate implements Renderer.
func (r *HTMLRenderer) Validate(blocks []Block) error { return Validate(blocks) }

// --- view models (everything here is either model text, escaped by
// html/template, or a number formatted by this package) ---

type kpiView struct {
	Label string
	Value string
	Tone  string // "", "pos", "neg"
}

type chartView struct {
	Path      string
	AreaPath  string
	MaxLabel  string
	MinLabel  string
	StartDate string
	EndDate   string
	Empty     bool
	Tone      string
}

type tradeView struct {
	Symbol  string
	Entry   string
	Exit    string
	EntryPx string
	ExitPx  string
	Qty     string
	Profit  string
	Tone    string
	Reason  string
}

type diffLineView struct {
	Kind string // "add" | "del" | "ctx" | "gap"
	Sign string
	Text string
}

type blockView struct {
	Type        string
	Paragraphs  []string
	Severity    string
	Title       string
	SourceLabel string
	KPIs        []kpiView
	Chart       chartView
	Trades      []tradeView
	TradesNote  string
	Diff        []diffLineView
	DiffNote    string
	Recs        []recommendationItem
	Columns     []string
	Rows        [][]string
}

type pageView struct {
	Lang       string
	Title      string
	Severity   string
	AgentName  string
	RunID      uint
	CreatedAt  string
	Strategies string
	Blocks     []blockView
}

// Render implements Renderer. It validates first, resolves every data
// reference through DataSource, and executes the template in meta.Locale.
func (r *HTMLRenderer) Render(ctx context.Context, meta ReportMeta, blocks []Block) (string, error) {
	if err := Validate(blocks); err != nil {
		return "", err
	}
	return r.render(ctx, r.data, meta, blocks)
}

// RenderLocales renders the same report once per locale (i18n-02 §3: the
// write-time snapshots). Every data reference is resolved once and shared by
// all locales, so the snapshots show identical numbers; only chrome/labels
// differ. An empty locales list means every supported locale.
func (r *HTMLRenderer) RenderLocales(ctx context.Context, meta ReportMeta, blocks []Block, locales []i18n.Locale) (map[i18n.Locale]string, error) {
	if err := Validate(blocks); err != nil {
		return nil, err
	}
	if len(locales) == 0 {
		locales = i18n.Supported
	}
	data := newMemoData(r.data)
	out := make(map[i18n.Locale]string, len(locales))
	for _, loc := range locales {
		m := meta
		m.Locale = loc
		html, err := r.render(ctx, data, m, blocks)
		if err != nil {
			return nil, err
		}
		out[loc] = html
	}
	return out, nil
}

func (r *HTMLRenderer) render(ctx context.Context, data DataSource, meta ReportMeta, blocks []Block) (string, error) {
	loc := i18n.ParseOr(string(meta.Locale), i18n.Default)
	page := pageView{
		Lang:       string(loc),
		Title:      meta.Title,
		Severity:   meta.Severity,
		AgentName:  meta.AgentName,
		RunID:      meta.RunID,
		CreatedAt:  meta.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"),
		Strategies: strings.Join(meta.StrategyNames, ", "),
	}
	for i, b := range blocks {
		v, err := resolve(ctx, data, loc, b)
		if err != nil {
			return "", fmt.Errorf("block %d (%q): %w", i, b.Type, err)
		}
		page.Blocks = append(page.Blocks, v)
	}

	tmpl, err := r.tmpl.Clone()
	if err != nil {
		return "", fmt.Errorf("render report: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Funcs(localizedFuncs(loc)).Execute(&buf, page); err != nil {
		return "", fmt.Errorf("render report: %w", err)
	}
	if buf.Len() > MaxRenderedBytes {
		return "", fmt.Errorf("rendered report is %d bytes, over the %d byte limit - use fewer or smaller blocks", buf.Len(), MaxRenderedBytes)
	}
	return buf.String(), nil
}

func series(ctx context.Context, data DataSource, s Source) (Series, error) {
	switch s.Kind {
	case SourceBacktestRun:
		return data.BacktestRun(ctx, s.ID)
	case SourceStrategyLive:
		days := s.Days
		if days <= 0 {
			days = 30
		}
		return data.StrategyLive(ctx, s.StrategyID, days)
	}
	return Series{}, fmt.Errorf("unknown source kind %q", s.Kind)
}

// sourceLabel is s's label in loc: built from s.Ref when the DataSource
// provided one, else the DataSource's own (EN) Label.
func sourceLabel(loc i18n.Locale, s Series) string {
	if s.Ref == nil {
		return s.Label
	}
	switch s.Ref.Kind {
	case SourceBacktestRun:
		return Messages.F(loc, "source.backtest", s.Ref.ID, s.Ref.Symbol,
			s.Ref.Start.UTC().Format("2006-01-02"), s.Ref.End.UTC().Format("2006-01-02"))
	case SourceStrategyLive:
		return Messages.F(loc, "source.strategy_live", s.Ref.Name, s.Ref.ID, s.Ref.Days)
	}
	return s.Label
}

func resolve(ctx context.Context, data DataSource, loc i18n.Locale, b Block) (blockView, error) {
	v := blockView{Type: b.Type}
	switch b.Type {
	case TypeSummary:
		var d summaryData
		_ = json.Unmarshal(b.Data, &d)
		v.Paragraphs = splitParagraphs(d.Text)
	case TypeCallout:
		var d calloutData
		_ = json.Unmarshal(b.Data, &d)
		v.Severity = d.Severity
		v.Title = d.Title
		v.Paragraphs = splitParagraphs(d.Text)
	case TypeKPIGrid:
		var d kpiGridData
		_ = json.Unmarshal(b.Data, &d)
		s, err := series(ctx, data, d.Source)
		if err != nil {
			return v, err
		}
		v.SourceLabel = sourceLabel(loc, s)
		for _, m := range d.Metrics {
			v.KPIs = append(v.KPIs, kpi(loc, m, s.Metrics))
		}
	case TypeEquityChart:
		var d equityChartData
		_ = json.Unmarshal(b.Data, &d)
		s, err := series(ctx, data, d.Source)
		if err != nil {
			return v, err
		}
		v.SourceLabel = sourceLabel(loc, s)
		v.Chart = buildChart(s.Equity)
	case TypeTradeTable:
		var d tradeTableData
		_ = json.Unmarshal(b.Data, &d)
		s, err := series(ctx, data, d.Source)
		if err != nil {
			return v, err
		}
		limit := d.Limit
		if limit <= 0 || limit > MaxTradeRows {
			limit = 20
		}
		v.SourceLabel = sourceLabel(loc, s)
		trades := s.Trades
		if len(trades) > limit {
			v.TradesNote = Messages.F(loc, "trades.note", limit, len(trades))
			trades = trades[len(trades)-limit:]
		}
		for _, t := range trades {
			v.Trades = append(v.Trades, tradeRow(t))
		}
	case TypeCodeDiff:
		var d codeDiffData
		_ = json.Unmarshal(b.Data, &d)
		from, to, label, err := diffSources(ctx, data, loc, d)
		if err != nil {
			return v, err
		}
		v.SourceLabel = label
		v.Diff, v.DiffNote = unifiedDiff(loc, from, to)
	case TypeRecommendation:
		var d recommendationData
		_ = json.Unmarshal(b.Data, &d)
		v.Recs = d.Items
	case TypeTextTable:
		var d textTableData
		_ = json.Unmarshal(b.Data, &d)
		v.Columns = d.Columns
		v.Rows = d.Rows
	}
	return v, nil
}

func diffSources(ctx context.Context, data DataSource, loc i18n.Locale, d codeDiffData) (from, to, label string, err error) {
	if d.FromSource != nil && d.ToSource != nil {
		return *d.FromSource, *d.ToSource, Messages.F(loc, "diff.source_proposed", d.StrategyID), nil
	}
	fromLabel, toLabel := Messages.T(loc, "diff.previous_version"), Messages.T(loc, "diff.current")
	if d.FromVersionID != nil {
		from, err = data.ScriptVersion(ctx, d.StrategyID, *d.FromVersionID)
		fromLabel = Messages.F(loc, "diff.version", *d.FromVersionID)
	} else {
		from, err = data.PreviousScript(ctx, d.StrategyID)
	}
	if err != nil {
		return "", "", "", err
	}
	if d.ToVersionID != nil {
		to, err = data.ScriptVersion(ctx, d.StrategyID, *d.ToVersionID)
		toLabel = Messages.F(loc, "diff.version", *d.ToVersionID)
	} else {
		to, err = data.CurrentScript(ctx, d.StrategyID)
	}
	if err != nil {
		return "", "", "", err
	}
	return from, to, Messages.F(loc, "diff.source_versions", d.StrategyID, fromLabel, toLabel), nil
}

func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	for _, p := range strings.Split(text, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fmtNum(v float64, decimals int) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	if math.IsInf(v, 1) {
		return "∞"
	}
	if math.IsInf(v, -1) {
		return "-∞"
	}
	return strconv.FormatFloat(v, 'f', decimals, 64)
}

func signedTone(v float64) string {
	switch {
	case v > 0:
		return "pos"
	case v < 0:
		return "neg"
	}
	return ""
}

func kpi(loc i18n.Locale, metric string, m Metrics) kpiView {
	k := kpiView{Label: Messages.T(loc, "metric."+metric)}
	switch metric {
	case "sharpe":
		k.Value = fmtNum(m.Sharpe, 2)
		k.Tone = signedTone(m.Sharpe)
	case "max_drawdown":
		if m.MaxDrawdownIsPct {
			k.Value = fmtNum(m.MaxDrawdown, 2) + "%"
		} else {
			k.Value = fmtNum(m.MaxDrawdown, 2)
		}
		if m.MaxDrawdown > 0 {
			k.Tone = "neg"
		}
	case "win_rate":
		k.Value = fmtNum(m.WinRatePct, 1) + "%"
	case "profit_factor":
		if m.TotalTrades == 0 {
			k.Value = Messages.T(loc, "value.na")
		} else if math.IsInf(m.ProfitFactor, 1) || m.ProfitFactor >= 1e15 {
			// No losing trades. The backtest usecase persists +Inf as
			// math.MaxFloat64 (Postgres can't store Inf); same sentinel rule
			// as app/handler/web/backtest/dto.go.
			k.Value = "∞"
			k.Tone = "pos"
		} else {
			k.Value = fmtNum(m.ProfitFactor, 2)
			if m.ProfitFactor > 1 {
				k.Tone = "pos"
			} else if m.ProfitFactor < 1 {
				k.Tone = "neg"
			}
		}
	case "total_trades":
		k.Value = strconv.Itoa(m.TotalTrades)
	case "net_pnl":
		k.Value = fmtNum(m.NetPnL, 2)
		k.Tone = signedTone(m.NetPnL)
	}
	return k
}

func tradeRow(t Trade) tradeView {
	const layout = "2006-01-02 15:04"
	tv := tradeView{
		Symbol:  t.Symbol,
		EntryPx: fmtNum(t.EntryPrice, 4),
		ExitPx:  fmtNum(t.ExitPrice, 4),
		Qty:     fmtNum(t.Quantity, 6),
		Profit:  fmtNum(t.Profit, 4),
		Tone:    signedTone(t.Profit),
		Reason:  t.ExitReason,
	}
	if !t.EntryTime.IsZero() {
		tv.Entry = t.EntryTime.UTC().Format(layout)
	}
	if !t.ExitTime.IsZero() {
		tv.Exit = t.ExitTime.UTC().Format(layout)
	}
	return tv
}

// Chart geometry (viewBox units).
const (
	chartW    = 640.0
	chartH    = 220.0
	chartPadX = 56.0
	chartPadY = 18.0
)

// buildChart turns an equity curve into SVG path strings. Only numbers
// produced here reach the SVG - never model input.
func buildChart(curve []EquityPoint) chartView {
	if len(curve) < 2 {
		return chartView{Empty: true}
	}
	minV, maxV := curve[0].Value, curve[0].Value
	for _, p := range curve {
		minV = math.Min(minV, p.Value)
		maxV = math.Max(maxV, p.Value)
	}
	if maxV == minV {
		maxV++
		minV--
	}
	plotW := chartW - 2*chartPadX
	plotH := chartH - 2*chartPadY
	var line strings.Builder
	var lastX float64
	for i, p := range curve {
		x := chartPadX + float64(i)/float64(len(curve)-1)*plotW
		y := chartPadY + (1-(p.Value-minV)/(maxV-minV))*plotH
		if i == 0 {
			fmt.Fprintf(&line, "M%.1f,%.1f", x, y)
		} else {
			fmt.Fprintf(&line, " L%.1f,%.1f", x, y)
		}
		lastX = x
	}
	area := fmt.Sprintf("%s L%.1f,%.1f L%.1f,%.1f Z", line.String(), lastX, chartH-chartPadY, chartPadX, chartH-chartPadY)
	tone := "pos"
	if curve[len(curve)-1].Value < curve[0].Value {
		tone = "neg"
	}
	return chartView{
		Path:      line.String(),
		AreaPath:  area,
		MaxLabel:  fmtNum(maxV, 2),
		MinLabel:  fmtNum(minV, 2),
		StartDate: curve[0].Time.UTC().Format("2006-01-02"),
		EndDate:   curve[len(curve)-1].Time.UTC().Format("2006-01-02"),
		Tone:      tone,
	}
}
