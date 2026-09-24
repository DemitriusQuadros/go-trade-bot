package modules

import (
	handler "go-trade-bot/app/handler/web/performancehistory"
	snapshot_repo "go-trade-bot/app/repository/performancesnapshot"
	strategy_repo "go-trade-bot/app/repository/strategy"
	usecase "go-trade-bot/app/usecase/performancehistory"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// PerformanceHistoryModule wires backend-05's GET
// /strategy/{id}/performance/history endpoint. cmd/api only needs the
// read-side use case - the daily snapshot job itself runs in cmd/worker
// (see cmd/worker/modules/performancehistory.go).
var PerformanceHistoryModule = fx.Module("performance_history",
	fx.Provide(
		func(db *gorm.DB) usecase.StrategyRepository {
			return strategy_repo.NewStrategyRepository(db)
		},
		func(db *gorm.DB) usecase.SnapshotRepository {
			return snapshot_repo.NewSnapshotRepository(db)
		},
		usecase.NewPerformanceHistoryUseCase,
		func(u *usecase.PerformanceHistoryUseCase) handler.UseCase { return u },
	),
)
