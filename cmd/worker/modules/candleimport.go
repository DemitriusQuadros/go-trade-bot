package modules

import (
	repository "go-trade-bot/app/repository/candleimport"
	usecase "go-trade-bot/app/usecase/candleimport"
	worker "go-trade-bot/app/workers/candleimport"
	taskHandler "go-trade-bot/app/handler/tasks/candleimport"

	"go.uber.org/fx"
)

// CandleImportModule only provides constructors. Route registration and the
// recurring-schedule bootstrap happen in cmd/worker/main.go's
// RegisterHandlers, alongside every other task type (strategy, optimize,
// performance-snapshot) - this package previously had its own fx.Invoke
// assuming a shared, fx-provided *asynq.ServeMux existed for any module to
// register onto independently. That pattern doesn't exist anywhere else in
// this codebase: RegisterHandlers constructs its own asynq.NewServeMux()
// locally inside an OnStart hook and wires every task type onto it by hand.
// The old fx.Invoke here failed at startup ("missing type: *asynq.ServeMux")
// since nothing ever provided one.
var CandleImportModule = fx.Module("candleimport",
	fx.Provide(
		repository.NewRepository,
		worker.NewWorker,
		usecase.NewCandleImportUseCase,
		taskHandler.NewTaskHandler,
	),
)
