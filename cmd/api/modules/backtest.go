package modules

import (
	handler "go-trade-bot/app/handler/web/backtest"
	backtest_repo "go-trade-bot/app/repository/backtest"
	candle_repo "go-trade-bot/app/repository/candle"
	strategy_repo "go-trade-bot/app/repository/strategy"
	usecase "go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/metrics_provider"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

var BacktestModule = fx.Module("backtest",
	fx.Provide(
		func(db *gorm.DB) usecase.BacktestRepository {
			return backtest_repo.NewBacktestRepository(db)
		},
		func(db *gorm.DB) usecase.StrategyRepository {
			return strategy_repo.NewStrategyRepository(db)
		},
		func() metrics_provider.MetricsProvider {
			return metrics_provider.NewCinarMetricsAdapter()
		},
		func(
			candleRepo candle_repo.Repository,
			backtestRepo usecase.BacktestRepository,
			strategyRepo usecase.StrategyRepository,
			metricsProvider metrics_provider.MetricsProvider,
			collector *metrics.MetricsCollector,
		) *usecase.BacktestUseCase {
			uc := usecase.NewBacktestUseCase(
				candleRepo,
				backtestRepo,
				strategyRepo,
				metricsProvider,
				usecase.DefaultThresholdPolicy(),
				"reports",
				20,
			)
			uc.SetMetricsCollector(collector)
			return uc
		},
		func(u *usecase.BacktestUseCase) handler.UseCase {
			return u
		},
	),
)
