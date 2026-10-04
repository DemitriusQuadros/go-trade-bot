package modules

import (
	"go-trade-bot/app/engine"
	signalrepository "go-trade-bot/app/repository/signal"
	accountusecase "go-trade-bot/app/usecase/account"
	signalusecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/notifier"

	"go.uber.org/fx"
)

// EngineModule provides the worker's two execution engines (fix-01):
//
//   - *engine.Engine: the real engine, for effective live/paper cycles. Its
//     SignalUseCase places orders on the process's exchange client (the
//     production adapter, or the testnet adapter when Testnet=true). This
//     wiring is unchanged from before fix-01.
//   - DryRunEngine: the simulated engine, for effective dryrun cycles, built
//     by NewDryRunEngine around exchange.NewDryRunExchange (which wraps the
//     real client in exchange.NewReadOnlyClient).
//
// cmd/worker/main.go hands both to handler.NewStrategyProcessor, which routes
// each cycle by its effective mode.
var EngineModule = fx.Module("engine",
	fx.Provide(
		engine.NewEngine,
		NewDryRunEngine,
		func(s signalusecase.SignalUseCase) engine.SignalUseCase { return s },
		func(a *accountusecase.AccountUseCase) engine.AccountReader { return a },
	),
)

// DryRunEngine is the worker's simulated execution stack for effective
// dryrun cycles. Exchange and Stops are exposed for tests (clock control).
type DryRunEngine struct {
	Engine   *engine.Engine
	Exchange *exchange.DryRunExchange
	Stops    *engine.SimulatedStopEvaluator
}

// DryRunEngineParams are NewDryRunEngine's fx dependencies.
type DryRunEngineParams struct {
	fx.In

	Config     *configuration.Configuration
	Exchange   exchange.ExchangeClient // the real client; only ever used read-only here
	SignalRepo signalrepository.SignalRepository
	Account    *accountusecase.AccountUseCase
	Indicators indicators.IndicatorProvider
	Notifier   notifier.NotificationSender
	Cache      memcache.Cache
	Metrics    *metrics.MetricsCollector
	Sizer      signalusecase.PositionSizer
}

// NewDryRunEngine builds the dryrun stack:
//
//   - exchange: exchange.NewDryRunExchange(real) - reads delegate to a
//     ReadOnlyClient around the real client; orders are simulated (SIM- IDs)
//     with DryRun.SlippagePct.
//   - SignalUseCase: the ordinary signal usecase on that exchange, with
//     DryRun.FeePct (when set), no order_execution_* metrics (simulated
//     orders must not count as real ones), and a DryRunAccount that reads the
//     real account for sizing but never writes simulated P&L into it.
//     Virtual dryrun balance is a follow-up.
//   - Engine: the ordinary engine on that exchange/usecase, plus the
//     simulated stop-loss evaluator as its PreCycle hook.
func NewDryRunEngine(p DryRunEngineParams) DryRunEngine {
	dryEx := exchange.NewDryRunExchange(p.Exchange, p.Config.DryRun.SlippagePct, p.Metrics)

	signalUC := signalusecase.NewSignalUseCase(
		p.SignalRepo,
		signalusecase.NewDryRunAccount(p.Account),
		dryEx,
		p.Notifier,
		nil, // no real-order metrics for simulated orders
		p.Sizer,
	)
	if p.Config.DryRun.FeePct > 0 {
		signalUC.FeePct = p.Config.DryRun.FeePct
	}

	stops := engine.NewSimulatedStopEvaluator(signalUC, dryEx, p.SignalRepo)

	eng := engine.NewEngine(dryEx, p.Indicators, signalUC, p.Account, p.Notifier, p.Cache, p.Metrics)
	eng.PreCycle = stops

	return DryRunEngine{Engine: eng, Exchange: dryEx, Stops: stops}
}
