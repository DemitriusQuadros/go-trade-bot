package script

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"
)

// TraceRecord/IndicatorCall/LogEntry match blueprint §6.4, with one
// addendum (operator-confirmed 2026-09-09, resolving frontend-01's Judgment
// Call): the blueprint's original shape carried indicators/signal/log only,
// which cannot drive a candlestick chart on its own. Candle is added here
// rather than making the frontend fetch-and-zip candles against trace
// records by timestamp as a separate call - one response, one source of
// truth per candle, no risk of the two datasets drifting out of sync.
// This is the one shape shared by all three delivery paths (persisted on
// BacktestRun.ExecutionTraceJSON - backend-07; ephemeral in REPL/fast-rerun
// responses - backend-08; streamed in Phase 3 live preview).
//
// JSON shape note: Candle/Signal are marshaled via custom MarshalJSON/
// UnmarshalJSON below (traceRecordJSON) rather than embedding
// exchange.Candle/strategies.Signal directly - both of those Go types are
// frozen (ADR-005) and have no json tags of their own, so a direct
// `json:"candle"`/`json:"signal"` embed would serialize as PascalCase field
// names (Open/High/Low/...). The already-shipped frontend
// (web/src/api/types.ts's TraceCandle/TraceSignal) expects lowercase
// o/h/l/c/v/t and buy/sell/stop_loss/take_profit{qty,price} - the same
// shape app/strategies/script/bridge.go's buildCtxTable already uses for
// ctx.candles in Lua. This wire-format choice matches the frontend that was
// already built against this spec's contract without touching either frozen
// Go type.
type TraceRecord struct {
	Timestamp  time.Time
	Candle     exchange.Candle
	Indicators []IndicatorCall
	Signal     *strategies.Signal
	Log        []LogEntry
	Plots      []PlotPoint
}

type IndicatorCall struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params"`
	Value  float64        `json:"value"`
}

type LogEntry struct {
	Label string `json:"label"`
	Value any    `json:"value"`
}

// PlotPoint is authored fresh for the plot() Lua binding (unlike Candle/
// Signal, it has no frozen upstream Go type to shadow), so it gets json tags
// directly and needs no traceRecordJSON-style shadow type of its own.
type PlotPoint struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Color string  `json:"color"` // always populated on the wire - resolved before Record(), never empty
	// Overlay says this point belongs on the price pane itself (a moving
	// average, Bollinger band, VWAP - anything on the same y-scale as
	// price), like TradingView draws them over the candles, rather than in
	// the separate oscillator pane below (RSI, MACD, ATR - a different
	// scale that would otherwise get squashed onto/distort the price
	// autoscale). Auto-plots (see LogIndicatorCall) set this from a
	// hardcoded indicator-name classification; an explicit plot() call
	// defaults to false (oscillator pane) unless the script passes true as
	// its 4th argument.
	Overlay bool `json:"overlay"`
}

// traceCandleJSON is the wire shape for TraceRecord.Candle - matches
// bridge.go's buildCtxTable candle row shape and web/src/api/types.ts's
// TraceCandle.
type traceCandleJSON struct {
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
	V float64 `json:"v"`
	T int64   `json:"t"`
}

// traceOrderJSON/traceSignalJSON are the wire shapes for TraceRecord.Signal -
// matches web/src/api/types.ts's TraceSignal.
type traceOrderJSON struct {
	Qty   float64 `json:"qty"`
	Price float64 `json:"price"`
}

type traceSignalJSON struct {
	Buy        *traceOrderJSON `json:"buy,omitempty"`
	Sell       *traceOrderJSON `json:"sell,omitempty"`
	StopLoss   *traceOrderJSON `json:"stop_loss,omitempty"`
	TakeProfit *traceOrderJSON `json:"take_profit,omitempty"`
}

type traceRecordJSON struct {
	Timestamp  time.Time        `json:"timestamp"`
	Candle     *traceCandleJSON `json:"candle,omitempty"`
	Indicators []IndicatorCall  `json:"indicators"`
	Signal     *traceSignalJSON `json:"signal,omitempty"`
	Log        []LogEntry       `json:"log"`
	Plots      []PlotPoint      `json:"plots"`
}

func toOrderJSON(o *strategies.Order) *traceOrderJSON {
	if o == nil {
		return nil
	}
	return &traceOrderJSON{Qty: o.Qty, Price: o.Price}
}

func fromOrderJSON(o *traceOrderJSON) *strategies.Order {
	if o == nil {
		return nil
	}
	return &strategies.Order{Qty: o.Qty, Price: o.Price}
}

func (t TraceRecord) MarshalJSON() ([]byte, error) {
	out := traceRecordJSON{
		Timestamp:  t.Timestamp,
		Indicators: t.Indicators,
		Log:        t.Log,
		Plots:      t.Plots,
	}
	if t.Indicators == nil {
		out.Indicators = []IndicatorCall{}
	}
	if t.Log == nil {
		out.Log = []LogEntry{}
	}
	if t.Plots == nil {
		out.Plots = []PlotPoint{}
	}
	out.Candle = &traceCandleJSON{
		O: t.Candle.Open,
		H: t.Candle.High,
		L: t.Candle.Low,
		C: t.Candle.Close,
		V: t.Candle.Volume,
		T: t.Candle.OpenTime.Unix(),
	}
	if t.Signal != nil {
		out.Signal = &traceSignalJSON{
			Buy:        toOrderJSON(t.Signal.Buy),
			Sell:       toOrderJSON(t.Signal.Sell),
			StopLoss:   toOrderJSON(t.Signal.StopLoss),
			TakeProfit: toOrderJSON(t.Signal.TakeProfit),
		}
	}
	return json.Marshal(out)
}

func (t *TraceRecord) UnmarshalJSON(data []byte) error {
	var in traceRecordJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	t.Timestamp = in.Timestamp
	t.Indicators = in.Indicators
	t.Log = in.Log
	t.Plots = in.Plots
	if in.Candle != nil {
		t.Candle = exchange.Candle{
			Open:     in.Candle.O,
			High:     in.Candle.H,
			Low:      in.Candle.L,
			Close:    in.Candle.C,
			Volume:   in.Candle.V,
			OpenTime: time.Unix(in.Candle.T, 0).UTC(),
		}
	}
	if in.Signal != nil {
		t.Signal = &strategies.Signal{
			Buy:        fromOrderJSON(in.Signal.Buy),
			Sell:       fromOrderJSON(in.Signal.Sell),
			StopLoss:   fromOrderJSON(in.Signal.StopLoss),
			TakeProfit: fromOrderJSON(in.Signal.TakeProfit),
		}
	}
	return nil
}

// TraceRecorder accumulates ONE TraceRecord's worth of data across a single
// hook-sequence cycle (Before->ShouldLong/ShouldShort->GoLong/GoShort/
// UpdatePosition->After). backend-04's ScriptStrategy constructs a fresh
// TraceRecorder per cycle (not per hook call) when running under a
// validation context (backtest/REPL/fast-rerun), and passes nil in
// production dryrun/paper/live - every call site in this package's
// bridge.go/indicators.go already treats a nil *TraceRecorder as a complete
// no-op (nil-check before every method call), so passing nil costs nothing
// beyond the branch.
type TraceRecorder struct {
	timestamp  time.Time
	candle     exchange.Candle
	indicators []IndicatorCall
	signal     *strategies.Signal
	log        []LogEntry
	plots      []PlotPoint
	// autoPlots/autoPlotOrder/autoPlotSigsByName back LogIndicatorCall's
	// auto-plot fallback (see its doc comment): one point per distinct
	// (ind.* name, param signature) pair actually called this cycle, keyed
	// by "name\x00sig" so calling the same indicator twice with DIFFERENT
	// params (e.g. ind.ema(9) and ind.ema(21) in the same cycle) plots both,
	// each claiming its own color-palette slot in true call order (matching
	// how an explicit plot() call claims a slot) - calling it twice with the
	// SAME params still collapses to one entry (a genuine duplicate, nothing
	// to disambiguate, last-value-wins is correct there). autoPlotSigsByName
	// tracks every distinct signature seen per bare name so Record() can
	// decide, per entry, whether to render under the bare name (only one
	// signature ever seen) or the disambiguated "name(sig)" form (more than
	// one) - emitted in Record() only for (disambiguated) names the script
	// didn't already plot() itself.
	autoPlots          map[string]autoPlotEntry
	autoPlotOrder      []string // composite "name\x00sig" keys, first-seen order
	autoPlotSigsByName map[string][]string
	plotNamesSeen      map[string]int // name -> palette slot index, first-seen order
	plotCapHit         bool           // true once the 13th distinct name has been dropped, guards the one-time warning
}

// maxDistinctPlotNames is the soft cap from blueprint §1/ADR-026 - generous
// relative to TradingView's own free-tier 2-3 slot limit, protects chart
// readability and trace payload size, never fails a cycle.
const maxDistinctPlotNames = 12

// plotColorPalette is ExecutionTraceChart.tsx's INDICATOR_COLORS, mirrored
// server-side so a color is always resolved before the record leaves the Go
// process (frontend never has to guess a default) - see the "Reused
// palette, not shared code" note in backend-01-plot-binding-and-trace-plots.md
// for why this duplication is deliberate, not accidental drift.
// MUST match web/src/components/charts/ExecutionTraceChart.tsx's INDICATOR_COLORS.
var plotColorPalette = []string{
	"#38bdf8", "#f59e0b", "#a855f7", "#ec4899", "#14b8a6", "#eab308",
}

// NewTraceRecorder takes the cycle's current candle (Context.Candles' last
// element - the same candle the cycle is evaluating) so Record() can embed
// it in the TraceRecord without a separate frontend join step.
func NewTraceRecorder(timestamp time.Time, candle exchange.Candle) *TraceRecorder {
	return &TraceRecorder{timestamp: timestamp, candle: candle}
}

// LogIndicatorCall receives every value the ind.* closure actually pushed
// back to Lua (luaReturnAsValues), not just the first - a single-return
// indicator (rsi, ema, ...) auto-plots one line under its bare name exactly
// as before; a multi-return indicator (bollinger, macd, stoch, ...) auto-
// plots ONE line per returned value, named "<indicator>.<sublabel>" via
// multiIndicatorSubLabels below, so e.g. ind.bollinger(20, 2) draws
// bollinger.upper/bollinger.mid/bollinger.lower as three distinct series
// instead of collapsing to a single (upper-band-only) line.
func (t *TraceRecorder) LogIndicatorCall(cctx strategies.Context, name string, params map[string]any, values []float64) {
	if t == nil || len(values) == 0 {
		return
	}
	t.indicators = append(t.indicators, IndicatorCall{Name: name, Params: params, Value: values[0]})

	sig := paramSignature(params)
	labels := multiIndicatorSubLabels[name]
	if len(labels) != len(values) {
		// Single-return indicator (or a name/arity we don't have a sub-label
		// mapping for) - plot under the bare name, first value only, same as
		// the original single-value behavior.
		t.autoPlotValue(name, sig, values[0])
		return
	}
	for i, label := range labels {
		t.autoPlotValue(name+"."+label, sig, values[i])
	}
}

// autoPlotEntry is one (name, param signature) pair's auto-plot state.
// Color/slot is resolved immediately (see autoPlotValue) in true call
// order, matching how an explicit plot() call claims its slot - only the
// final display Name (bare vs. disambiguated) is decided later, in
// Record(), since that depends on how many distinct signatures the bare
// name ended up with by the end of the cycle.
type autoPlotEntry struct {
	name    string // bare indicator/sub-label name, e.g. "ema" or "bollinger.upper"
	sig     string
	value   float64
	overlay bool
	color   string
}

// autoPlotValue is the auto-plot fallback shared by every LogIndicatorCall
// sub-line: every indicator (or indicator sub-line) the script actually
// calls (ind.rsi, ind.bollinger, ...) shows on the chart automatically,
// using the same per-name checkbox toggle plot() already gets - the
// operator no longer has to remember a matching plot() call for each
// indicator they use, and an indicator the script never calls never appears
// (no dead RSI line left over from an earlier version of the script). An
// explicit plot() call under the exact same (possibly disambiguated) name
// always wins over this (see Record()): this is a fallback for indicators
// the script computes but doesn't bother plotting itself, not an override
// of intentional plot() calls.
//
// paramSig distinguishes calls to the same plotName with different
// arguments in the same cycle (e.g. ind.ema(9) vs ind.ema(21)): each
// distinct (plotName, paramSig) pair is keyed separately here and claims
// its own color-palette slot immediately, in call order - so two EMAs at
// different periods both survive to Record() (with two different colors),
// instead of the second call silently overwriting the first. A repeated
// call with the IDENTICAL signature reuses the same key, so it still
// collapses to one entry (nothing to disambiguate - a genuine duplicate,
// and last-value-wins is correct there).
func (t *TraceRecorder) autoPlotValue(plotName, paramSig string, value float64) {
	key := plotName + "\x00" + paramSig
	if _, seen := t.autoPlots[key]; !seen {
		t.autoPlotOrder = append(t.autoPlotOrder, key)
		if t.autoPlotSigsByName == nil {
			t.autoPlotSigsByName = make(map[string][]string)
		}
		t.autoPlotSigsByName[plotName] = append(t.autoPlotSigsByName[plotName], paramSig)
	}
	color, ok := t.resolvePlotSlot(key)
	if !ok {
		return // 13th+ distinct (name, params) pair this cycle: silently dropped, per ADR-026
	}
	if t.autoPlots == nil {
		t.autoPlots = make(map[string]autoPlotEntry)
	}
	t.autoPlots[key] = autoPlotEntry{name: plotName, sig: paramSig, value: value, overlay: overlayIndicators[plotName], color: color}
}

// disambiguatedPlotName returns the name a given (plotName, paramSig) entry
// should render under: the bare plotName if it was only ever called with one
// distinct param signature this cycle (preserves the original, simpler
// label for the common case), or "plotName(sig)" if the script called it
// more than once with different params - e.g. two EMAs with different
// periods become "ema(9)" and "ema(21)" instead of colliding on "ema".
func disambiguatedPlotName(plotName, paramSig string, distinctSigCount int) string {
	if distinctSigCount <= 1 || paramSig == "" {
		return plotName
	}
	return plotName + "(" + paramSig + ")"
}

// paramSignature renders an indicator call's positional numeric args
// (luaArgsAsParams' arg1, arg2, ... keys) as a stable, human-readable
// signature for disambiguatedPlotName, e.g. {"arg1": 9.0} -> "9" or
// {"arg1": 12.0, "arg2": 26.0, "arg3": 9.0} -> "12,26,9". Lexicographic key
// sort is safe here since every real ind.* closure takes at most a handful
// of positional args (macd's 3 is the max today) - well short of "arg10"
// ever sorting ahead of "arg2".
func paramSignature(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if f, ok := params[k].(float64); ok {
			parts = append(parts, formatParamNumber(f))
		}
	}
	return strings.Join(parts, ",")
}

func formatParamNumber(f float64) string {
	if f == math.Trunc(f) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// multiIndicatorSubLabels maps a multi-return ind.* name to the labels used
// for its individual auto-plot lines, in the exact order its closure pushes
// Lua return values (see indicators.go's bindIndicators) - keep these two
// files in sync if a multi-return indicator's closure changes its push
// order or a new multi-return indicator is added.
var multiIndicatorSubLabels = map[string][]string{
	"bollinger":   {"upper", "mid", "lower"},
	"macd":        {"macd", "signal", "hist"},
	"macdext":     {"macd", "signal", "hist"},
	"macdfix":     {"macd", "signal", "hist"},
	"mama":        {"mama", "fama"},
	"aroon":       {"down", "up"},
	"stoch":       {"k", "d"},
	"stochf":      {"k", "d"},
	"stochrsi":    {"k", "d"},
	"htphasor":    {"inphase", "quadrature"},
	"htsine":      {"sine", "leadsine"},
	"minmax":      {"min", "max"},
	"minmaxindex": {"minidx", "maxidx"},
}

// overlayIndicators names every ind.* auto-plot line (bare name for a
// single-return indicator, "<name>.<sublabel>" for a multi-return one, see
// multiIndicatorSubLabels) whose values share the candles' own price scale -
// a moving average or band sits right on top of the candles on a real
// trading platform, unlike an oscillator (RSI, MACD, ATR) whose
// 0-100/unbounded range would otherwise squash onto or distort the price
// pane's autoscale. Drives LogIndicatorCall's auto-plot pane placement (see
// PlotPoint.Overlay) - keep in sync with indicators.go's bindIndicators if a
// new price-scale indicator is added there. Everything NOT listed here
// defaults to false (oscillator pane), which is correct for every
// bounded/ratio/index-valued indicator (rsi, macd.*, stoch.*, adx, obv,
// maxindex, minmaxindex.*, ...).
var overlayIndicators = map[string]bool{
	"sma":             true,
	"ema":             true,
	"dema":            true,
	"tema":            true,
	"trima":           true,
	"wma":             true,
	"kama":            true,
	"t3":              true,
	"ma":              true,
	"httrendline":     true,
	"midpoint":        true,
	"midprice":        true,
	"sar":             true,
	"sarext":          true,
	"avgprice":        true,
	"medprice":        true,
	"typprice":        true,
	"wclprice":        true,
	"linearreg":       true,
	"tsf":             true,
	"max":             true,
	"min":             true,
	"bollinger.upper": true,
	"bollinger.mid":   true,
	"bollinger.lower": true,
	"mama.mama":       true,
	"mama.fama":       true,
	"minmax.min":      true,
	"minmax.max":      true,
}

func (t *TraceRecorder) LogEntry(label string, value any) {
	if t == nil {
		return
	}
	t.log = append(t.log, LogEntry{Label: label, Value: value})
}

func (t *TraceRecorder) SetSignal(s *strategies.Signal) {
	if t == nil {
		return
	}
	t.signal = s
}

// resolvePlotSlot assigns (or reuses) name's color-palette slot. Shared by
// LogPlot (explicit plot() calls) and LogIndicatorCall (the ind.* auto-plot
// fallback) so the two never disagree on a name's color and both count
// against the same maxDistinctPlotNames budget (ADR-026): the first 12
// distinct names seen this cycle - from either source - get a slot; the
// 13th+ is refused (ok=false) with a one-time "plot_limit_exceeded" log
// entry (guarded by plotCapHit so it doesn't spam on every subsequent
// over-cap call within the same cycle).
func (t *TraceRecorder) resolvePlotSlot(name string) (color string, ok bool) {
	if t.plotNamesSeen == nil {
		t.plotNamesSeen = make(map[string]int)
	}
	slot, seen := t.plotNamesSeen[name]
	if !seen {
		if len(t.plotNamesSeen) >= maxDistinctPlotNames {
			if !t.plotCapHit {
				t.plotCapHit = true
				t.log = append(t.log, LogEntry{Label: "plot_limit_exceeded", Value: name})
			}
			return "", false // 13th+ distinct name this cycle: silently dropped, per ADR-026
		}
		slot = len(t.plotNamesSeen)
		t.plotNamesSeen[name] = slot
	}
	return plotColorPalette[slot%len(plotColorPalette)], true
}

// LogPlot records one plot(name, value, color, overlay) call - every call,
// not deduplicated (unlike LogIndicatorCall's auto-plot map, an explicit
// plot() call is the script author's own choice to make, so it is never
// second-guessed here). An empty color resolves via resolvePlotSlot, so a
// PlotPoint's Color is always non-empty on the wire. overlay puts the point
// on the price pane instead of the oscillator pane - see PlotPoint.Overlay.
func (t *TraceRecorder) LogPlot(name string, value float64, color string, overlay bool) {
	if t == nil {
		return
	}
	slotColor, ok := t.resolvePlotSlot(name)
	if !ok {
		return
	}
	if color == "" {
		color = slotColor
	}
	t.plots = append(t.plots, PlotPoint{Name: name, Value: value, Color: color, Overlay: overlay})
}

func (t *TraceRecorder) Record() TraceRecord {
	plots := t.plots
	if len(t.autoPlots) > 0 {
		explicitlyPlotted := make(map[string]bool, len(t.plots))
		for _, p := range t.plots {
			explicitlyPlotted[p.Name] = true
		}
		for _, key := range t.autoPlotOrder {
			entry, ok := t.autoPlots[key]
			if !ok {
				continue // dropped at call time (cap hit) - see autoPlotValue
			}
			distinct := len(t.autoPlotSigsByName[entry.name])
			finalName := disambiguatedPlotName(entry.name, entry.sig, distinct)
			if explicitlyPlotted[finalName] {
				continue // the script's own plot() call under this name wins
			}
			plots = append(plots, PlotPoint{Name: finalName, Value: entry.value, Color: entry.color, Overlay: entry.overlay})
		}
	}
	return TraceRecord{Timestamp: t.timestamp, Candle: t.candle, Indicators: t.indicators, Signal: t.signal, Log: t.log, Plots: plots}
}
