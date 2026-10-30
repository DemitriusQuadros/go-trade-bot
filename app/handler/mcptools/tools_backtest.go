package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go-trade-bot/app/entities"
	backtestuc "go-trade-bot/app/usecase/backtest"
)

// WalkForwardQueue stores a queued walk-forward run and marks one that could
// not be handed to the worker as failed (app/usecase/backtest.BacktestUseCase).
// Walk-forward is the heaviest backtest, so over MCP it is asynchronous only.
type WalkForwardQueue interface {
	EnqueueWalkForward(ctx context.Context, req backtestuc.WalkForwardRequest) (entities.BacktestRun, error)
	MarkEnqueueFailed(ctx context.Context, runID uint, cause error) error
}

func walkForwardTools(q WalkForwardQueue, tasks BacktestTasks) []Tool {
	return []Tool{{
		Name:        "run_walk_forward",
		Description: "Start a walk-forward validation (rolling train/test windows, aggregate out-of-sample result) for an existing strategy. It is heavy, so it runs asynchronously in the worker: this returns backtest_id with status=queued immediately - call get_backtest with that id until status is done (results) or failed (error). Defaults: train 3 months, test 1 month, step = test months, capital 1000.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"strategy_id":{"type":"integer"},"symbol":{"type":"string"},"timeframe":{"type":"string","description":"e.g. 15m, 1h (default 1m)"},"start_date":{"type":"string","description":"RFC3339 or YYYY-MM-DD"},"end_date":{"type":"string","description":"RFC3339 or YYYY-MM-DD"},"initial_capital":{"type":"number"},"train_months":{"type":"integer"},"test_months":{"type":"integer"},"step_months":{"type":"integer"}},"required":["strategy_id","symbol","start_date","end_date"]}`),
		Write:       true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var in struct {
				StrategyID     uint    `json:"strategy_id"`
				Symbol         string  `json:"symbol"`
				Timeframe      string  `json:"timeframe"`
				StartDate      string  `json:"start_date"`
				EndDate        string  `json:"end_date"`
				InitialCapital float64 `json:"initial_capital"`
				TrainMonths    int     `json:"train_months"`
				TestMonths     int     `json:"test_months"`
				StepMonths     int     `json:"step_months"`
			}
			if err := decode("run_walk_forward", raw, &in); err != nil {
				return nil, err
			}
			if err := requireID("run_walk_forward", "strategy_id", in.StrategyID); err != nil {
				return nil, err
			}
			start, err := parseDay(in.StartDate)
			if err != nil {
				return nil, fmt.Errorf("run_walk_forward: start_date must be RFC3339 or YYYY-MM-DD")
			}
			end, err := parseDay(in.EndDate)
			if err != nil {
				return nil, fmt.Errorf("run_walk_forward: end_date must be RFC3339 or YYYY-MM-DD")
			}
			run, err := q.EnqueueWalkForward(ctx, backtestuc.WalkForwardRequest{
				StrategyID: in.StrategyID, Symbol: strings.ToUpper(strings.TrimSpace(in.Symbol)), Timeframe: strings.TrimSpace(in.Timeframe),
				StartDate: start, EndDate: end, InitialCapital: in.InitialCapital,
				TrainMonths: in.TrainMonths, TestMonths: in.TestMonths, StepMonths: in.StepMonths,
			})
			if err != nil {
				return nil, err
			}
			if err := tasks.EnqueueBacktestTask(run.ID); err != nil {
				_ = q.MarkEnqueueFailed(ctx, run.ID, err)
				return nil, fmt.Errorf("run_walk_forward: created run %d but failed to enqueue it: %w", run.ID, err)
			}
			return map[string]any{
				"backtest_id": run.ID, "status": "queued",
				"note": "runs asynchronously - call get_backtest with this id until status is done or failed",
			}, nil
		},
	}}
}

// BacktestTasks hands a queued run to the asynq worker.
type BacktestTasks interface {
	EnqueueBacktestTask(runID uint) error
}
