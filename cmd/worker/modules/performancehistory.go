package modules

import (
	tasks "go-trade-bot/app/handler/tasks/performancehistory"
	snapshot_repo "go-trade-bot/app/repository/performancesnapshot"
	strategy_repo "go-trade-bot/app/repository/strategy"
	usecase "go-trade-bot/app/usecase/performancehistory"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// PerformanceHistoryModule wires backend-05's daily snapshot job: the asynq
// task processor that runs Snapshot() when cmd/worker's asynq.Scheduler
// fires the daily cron entry (main.go).
var PerformanceHistoryModule = fx.Module("performance_history",
	fx.Provide(
		func(db *gorm.DB) usecase.StrategyRepository {
			return strategy_repo.NewStrategyRepository(db)
		},
		func(db *gorm.DB) usecase.SnapshotRepository {
			return snapshot_repo.NewSnapshotRepository(db)
		},
		usecase.NewPerformanceHistoryUseCase,
		func(u *usecase.PerformanceHistoryUseCase) tasks.UseCase { return u },
		tasks.NewSnapshotProcessor,
	),
)
