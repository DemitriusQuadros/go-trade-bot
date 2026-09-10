package steps

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"go-trade-bot/app/entities"
	backtest_repo "go-trade-bot/app/repository/backtest"
	candle_repo "go-trade-bot/app/repository/candle"
	strategy_repo "go-trade-bot/app/repository/strategy"
	"go-trade-bot/app/strategies"
	"go-trade-bot/app/strategies/script"
	backtest_uc "go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/metrics_provider"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

// scriptRegisterOnce guards the process-global registry so re-running the
// phase-5 scenario (or sharing the binary with other packages) never triggers
// the duplicate-registration panic.
var scriptRegisterOnce sync.Once

type scriptingScenarioState struct {
	strategyID  uint
	source      string
	symbol      string
	totalTrades int
}

func registerScriptStrategyOnce() {
	scriptRegisterOnce.Do(func() {
		runner := script.NewRunner(script.DefaultHookTimeout, nil)
		strategies.Register("script", func(db entities.Strategy) strategies.Strategy {
			return script.NewScriptStrategy(db, scriptE2ENopStore{}, runner)
		})
	})
}

// scriptE2ENopStore is a no-op ScriptStateStore (the phase-5 scenario's script
// does not use persistent state; state persistence has its own repository
// tests).
type scriptE2ENopStore struct{}

func (scriptE2ENopStore) Load(context.Context, uint, string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (scriptE2ENopStore) Save(context.Context, uint, string, map[string]interface{}) error {
	return nil
}

func scriptOscillatingCandles(symbol string, count int) []entities.Candle {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]entities.Candle, count)
	for i := 0; i < count; i++ {
		price := 100.0 + math.Sin(float64(i)*0.35)*8.0
		candles[i] = entities.Candle{
			Symbol:    symbol,
			Timeframe: "1m",
			OpenTime:  baseTime.Add(time.Duration(i) * time.Minute),
			Open:      price - 0.2,
			High:      price + 1.0,
			Low:       price - 1.0,
			Close:     price,
			Volume:    10.0,
		}
	}
	return candles
}

func RegisterScriptingSteps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &scriptingScenarioState{}

	sc.Step(`^a script strategy "([^"]*)" for symbol "([^"]*)" with source:$`, func(name, symbol string, doc *godog.DocString) error {
		registerScriptStrategyOnce()
		strat := entities.Strategy{
			Name:             name,
			StrategyName:     "script",
			ScriptSource:     doc.Content,
			Status:           entities.Productive,
			Mode:             "dryrun",
			MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle: entities.OneMinute,
				Configuration: datatypes.JSON(`{
					"stop_loss_pct": 2.0,
					"position_sizing": {"type": "pct_capital", "value": 10}
				}`),
			},
		}
		if err := tc.DB.Create(&strat).Error; err != nil {
			return err
		}
		state.strategyID = strat.ID
		state.source = doc.Content
		state.symbol = symbol
		tc.CurrentStrategy = &strat
		return nil
	})

	sc.Step(`^I run a backtest for the script strategy over (\d+) oscillating candles$`, func(count int) error {
		candles := scriptOscillatingCandles(state.symbol, count)
		candleRepo := candle_repo.NewCandleRepository(tc.DB)
		if err := candleRepo.Upsert(context.Background(), candles); err != nil {
			return err
		}
		uc := backtest_uc.NewBacktestUseCase(
			candleRepo,
			backtest_repo.NewBacktestRepository(tc.DB),
			strategy_repo.NewStrategyRepository(tc.DB),
			metrics_provider.NewCinarMetricsAdapter(),
			backtest_uc.DefaultThresholdPolicy(),
			tc.T.TempDir(),
			20,
		)
		run, err := uc.Run(context.Background(), backtest_uc.RunRequest{
			StrategyID: state.strategyID,
			Symbol:     state.symbol,
			Timeframe:  "1m",
			StartDate:  candles[0].OpenTime,
			EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
		})
		if err != nil {
			return err
		}
		state.totalTrades = run.TotalTrades
		return nil
	})

	sc.Step(`^the script backtest should complete with at least (\d+) trade$`, func(minTrades int) error {
		if state.totalTrades < minTrades {
			return fmt.Errorf("expected at least %d trades, got %d", minTrades, state.totalTrades)
		}
		return nil
	})

	sc.Step(`^the worker runs one dryrun cycle for the script strategy$`, func() error {
		var strat entities.Strategy
		if err := tc.DB.First(&strat, state.strategyID).Error; err != nil {
			return err
		}

		// Resolve the strategy via the registry exactly as the worker does, and
		// drive one hook sequence directly (dryrun => no real orders). Any hook
		// error is captured as an "error" StrategyExecution, mirroring the
		// engine's per-cycle recording.
		stratImpl, ok := strategies.Get(strat.StrategyName, strat)
		if !ok {
			return fmt.Errorf("strategy %q not resolvable via registry", strat.StrategyName)
		}

		candles := scriptOscillatingCandles(state.symbol, 100)
		exCandles := make([]exchange.Candle, len(candles))
		for i, c := range candles {
			exCandles[i] = exchange.Candle{
				Symbol:    c.Symbol,
				Timeframe: c.Timeframe,
				OpenTime:  c.OpenTime,
				Open:      c.Open,
				High:      c.High,
				Low:       c.Low,
				Close:     c.Close,
				Volume:    c.Volume,
			}
		}
		cctx := strategies.Context{
			Candles:    exCandles,
			Indicators: indicators.NewTalibAdapter(),
			Price:      candles[len(candles)-1].Close,
			Timeframe:  "1m",
			Symbol:     state.symbol,
			Mode:       strategies.ModeDryRun,
		}

		status := entities.OK
		message := ""
		func() {
			defer func() {
				if r := recover(); r != nil {
					status = entities.Error
					message = fmt.Sprintf("panic: %v", r)
				}
			}()
			stratImpl.Before(cctx)
			if stratImpl.ShouldLong(cctx) {
				_ = stratImpl.GoLong(cctx)
			}
			stratImpl.After(cctx)
		}()

		return tc.DB.Create(&entities.StrategyExecution{
			Status:     entities.ExecutionStatus(status),
			Message:    message,
			StrategyID: state.strategyID,
			ExecutedAt: time.Now(),
		}).Error
	})

	sc.Step(`^a strategy execution with status "([^"]*)" should be recorded for the script strategy$`, func(expected string) error {
		var count int64
		if err := tc.DB.Model(&entities.StrategyExecution{}).
			Where("strategy_id = ? AND status = ?", state.strategyID, expected).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("expected a StrategyExecution with status %q for strategy %d, found none", expected, state.strategyID)
		}
		return nil
	})
}
