package script

import (
	"encoding/json"
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
	}
	if t.Indicators == nil {
		out.Indicators = []IndicatorCall{}
	}
	if t.Log == nil {
		out.Log = []LogEntry{}
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
}

// NewTraceRecorder takes the cycle's current candle (Context.Candles' last
// element - the same candle the cycle is evaluating) so Record() can embed
// it in the TraceRecord without a separate frontend join step.
func NewTraceRecorder(timestamp time.Time, candle exchange.Candle) *TraceRecorder {
	return &TraceRecorder{timestamp: timestamp, candle: candle}
}

func (t *TraceRecorder) LogIndicatorCall(cctx strategies.Context, name string, params map[string]any, value float64) {
	if t == nil {
		return
	}
	t.indicators = append(t.indicators, IndicatorCall{Name: name, Params: params, Value: value})
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

func (t *TraceRecorder) Record() TraceRecord {
	return TraceRecord{Timestamp: t.timestamp, Candle: t.candle, Indicators: t.indicators, Signal: t.signal, Log: t.log}
}
