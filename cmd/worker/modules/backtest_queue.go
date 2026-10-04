package modules

import (
	tasks "go-trade-bot/app/handler/tasks/backtest"
	backtest_usecase "go-trade-bot/app/usecase/backtest"

	"go.uber.org/fx"
)

// BacktestQueueModule wires B-02's asynchronous backtests on the worker
// side: the asynq task processor that actually runs
// BacktestUseCase.ExecuteQueued. cmd/api and cmd/mcp only enqueue.
var BacktestQueueModule = fx.Module("backtest_queue",
	fx.Provide(
		func(u *backtest_usecase.BacktestUseCase) tasks.BacktestUseCase { return u },
		tasks.NewBacktestProcessor,
	),
)
