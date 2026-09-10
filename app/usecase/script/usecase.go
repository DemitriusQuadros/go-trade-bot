// Package script (usecase layer) implements the REPL and fast-rerun endpoints
// (backend-08). It is deliberately distinct from app/strategies/script (the Lua
// engine), which it imports as strategyscript.
package script

import (
	"context"
	"fmt"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	strategyscript "go-trade-bot/app/strategies/script"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
)

// defaultWindow MUST equal app/engine/engine.go's candleWindow (100) so a
// REPL/fast-rerun cycle sees the same candle history depth a live/backtest
// cycle would.
const defaultWindow = 100

const replStrategyName = "repl"

// LiveCandleSource fetches the most recent candles for a symbol/interval (the
// exchange's ListKline). Adapted from internal/exchange.ExchangeClient.
type LiveCandleSource interface {
	ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error)
}

// HistoricalCandleSource fetches a window of candles ending at a point in time
// (the candle repository's RangeBefore).
type HistoricalCandleSource interface {
	RangeBefore(ctx context.Context, symbol, timeframe string, before time.Time, window int) ([]exchange.Candle, error)
}

// StrategyRepository is the narrow slice needed to load a strategy row's
// Configuration for fast-rerun.
type StrategyRepository interface {
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
}

// EvalRequest / EvalResponse back POST /api/script/repl. JSON field names match
// the already-shipped frontend (web/src/api/types.ts ReplRequest/ReplResponse).
type EvalRequest struct {
	Source        string `json:"source"`
	Symbol        string `json:"symbol"`
	Timeframe     string `json:"timeframe"`
	WindowCandles int    `json:"window_candles,omitempty"`
}

type EvalResponse struct {
	Result    any                          `json:"result"`
	Trace     []strategyscript.TraceRecord `json:"trace"`
	Error     string                       `json:"error,omitempty"`
	ErrorLine *int                         `json:"error_line,omitempty"`
}

// FastRerunRequest / FastRerunResponse back POST /api/script/fast-rerun.
type FastRerunRequest struct {
	StrategyID    uint       `json:"strategy_id"`
	Source        string     `json:"source"`
	Symbol        string     `json:"symbol"`
	Timeframe     string     `json:"timeframe"`
	EndTime       *time.Time `json:"end_time,omitempty"`
	WindowCandles int        `json:"window_candles,omitempty"`
}

type FastRerunResponse struct {
	Trace     []strategyscript.TraceRecord `json:"trace"`
	Error     string                       `json:"error,omitempty"`
	ErrorLine *int                         `json:"error_line,omitempty"`
}

// UseCase orchestrates the REPL and fast-rerun flows. It never holds a
// SignalUseCase reference by construction (AC#7): neither flow may ever place
// an order, real or simulated.
type UseCase struct {
	live         LiveCandleSource
	historical   HistoricalCandleSource
	strategyRepo StrategyRepository
	runner       *strategyscript.Runner
}

func NewUseCase(
	live LiveCandleSource,
	historical HistoricalCandleSource,
	strategyRepo StrategyRepository,
	runner *strategyscript.Runner,
) *UseCase {
	return &UseCase{live: live, historical: historical, strategyRepo: strategyRepo, runner: runner}
}

// Eval runs a single REPL snippet against a live candle window. A script-level
// error is captured into EvalResponse.Error (returns a nil Go error); only an
// infra failure (candle fetch) returns a real Go error.
func (u *UseCase) Eval(ctx context.Context, req EvalRequest) (EvalResponse, error) {
	window := req.WindowCandles
	if window <= 0 {
		window = defaultWindow
	}
	tf := req.Timeframe
	if tf == "" {
		tf = "1m"
	}

	candles, err := u.live.ListKline(ctx, req.Symbol, tf, window)
	if err != nil {
		return EvalResponse{}, fmt.Errorf("failed to fetch live candles for %s: %w", req.Symbol, err)
	}

	cctx := u.buildContext(req.Symbol, tf, candles, nil)

	var trace *strategyscript.TraceRecorder
	if len(candles) > 0 {
		last := candles[len(candles)-1]
		trace = strategyscript.NewTraceRecorder(last.OpenTime, last)
	} else {
		trace = strategyscript.NewTraceRecorder(time.Time{}, exchange.Candle{})
	}

	result, evalErr := u.runner.EvalREPL(replStrategyName, cctx, req.Source, trace)
	if evalErr != nil {
		// Script-level error: report via .Error, empty result/trace (AC#3).
		return EvalResponse{Result: nil, Trace: []strategyscript.TraceRecord{}, Error: evalErr.Error()}, nil
	}

	return EvalResponse{Result: result, Trace: []strategyscript.TraceRecord{trace.Record()}}, nil
}

// FastRerun replays an unsaved script over a candle window (live if EndTime is
// nil, else historical), driving the full hook sequence against a real
// ScriptStrategy backed by a per-request in-memory state store. It NEVER calls
// a SignalUseCase or persists anything - position transitions are simulated
// in-memory purely to mirror the engine's Position gating so the produced
// signals match a real backtest over the same window.
func (u *UseCase) FastRerun(ctx context.Context, req FastRerunRequest) (FastRerunResponse, error) {
	window := req.WindowCandles
	if window <= 0 {
		window = defaultWindow
	}
	tf := req.Timeframe
	if tf == "" {
		tf = "1m"
	}

	var candles []exchange.Candle
	var err error
	if req.EndTime == nil {
		candles, err = u.live.ListKline(ctx, req.Symbol, tf, window)
	} else {
		candles, err = u.historical.RangeBefore(ctx, req.Symbol, tf, *req.EndTime, window)
	}
	if err != nil {
		return FastRerunResponse{}, fmt.Errorf("failed to fetch candles for %s: %w", req.Symbol, err)
	}

	dbStrat, err := u.strategyRepo.GetByID(ctx, req.StrategyID)
	if err != nil {
		return FastRerunResponse{}, fmt.Errorf("strategy %d not found: %w", req.StrategyID, err)
	}
	// Preserve the persisted row's Configuration but override the source with
	// the editor's unsaved text.
	dbStrat.ScriptSource = req.Source

	// Surface a genuine parse error as a response-level error (not a Go error);
	// per-hook runtime errors are failed-closed by ScriptStrategy and do not
	// surface here.
	if verr := u.runner.Validate(req.Source); verr != nil {
		return FastRerunResponse{Trace: []strategyscript.TraceRecord{}, Error: verr.Error()}, nil
	}

	strat := strategyscript.NewScriptStrategy(dbStrat, newInMemoryStateStore(), u.runner)
	traces := []strategyscript.TraceRecord{}
	strat.SetTraceSink(func(rec strategyscript.TraceRecord) {
		traces = append(traces, rec)
	})

	var position *strategies.Position
	for i, c := range candles {
		cctx := u.buildContextWindow(req.Symbol, tf, candles, i, position)

		strat.Before(cctx)
		if position == nil {
			if strat.ShouldLong(cctx) {
				sig := strat.GoLong(cctx)
				if sig.Buy != nil {
					entry := sig.Buy.Price
					if entry == 0 {
						entry = cctx.Price
					}
					position = &strategies.Position{
						Symbol:     req.Symbol,
						EntryPrice: entry,
						Quantity:   sig.Buy.Qty,
						OpenedAt:   c.OpenTime,
					}
				}
			} else if strat.ShouldShort(cctx) {
				_ = strat.GoShort(cctx)
			}
		} else {
			sig := strat.UpdatePosition(cctx)
			if sig != nil && sig.Sell != nil {
				position = nil
			}
		}
		strat.After(cctx)
	}

	return FastRerunResponse{Trace: traces}, nil
}

// buildContext builds a synthetic Context over the full candle slice (REPL).
func (u *UseCase) buildContext(symbol, tf string, candles []exchange.Candle, position *strategies.Position) strategies.Context {
	price := 0.0
	if len(candles) > 0 {
		price = candles[len(candles)-1].Close
	}
	return strategies.Context{
		Candles:    candles,
		Position:   position,
		Account:    strategies.Account{},
		Config:     map[string]interface{}{},
		Indicators: indicators.NewTalibAdapter(),
		Price:      price,
		Timeframe:  tf,
		Symbol:     symbol,
		Mode:       strategies.ModeDryRun,
	}
}

// buildContextWindow mirrors the engine's per-cycle candle window: the most
// recent up-to-defaultWindow candles ending at index i (inclusive).
func (u *UseCase) buildContextWindow(symbol, tf string, all []exchange.Candle, i int, position *strategies.Position) strategies.Context {
	start := i + 1 - defaultWindow
	if start < 0 {
		start = 0
	}
	win := all[start : i+1]
	return strategies.Context{
		Candles:    win,
		Position:   position,
		Account:    strategies.Account{},
		Config:     map[string]interface{}{},
		Indicators: indicators.NewTalibAdapter(),
		Price:      all[i].Close,
		Timeframe:  tf,
		Symbol:     symbol,
		Mode:       strategies.ModeDryRun,
	}
}
