package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/services/algorithm/bollinger"
	"go-trade-bot/app/services/algorithm/grid"
	"go-trade-bot/app/services/algorithm/scalping"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/broker"
	"go-trade-bot/internal/metrics"
	"log"
	"time"

	"github.com/hibiken/asynq"
)

const (
	StrategyTask        = "strategy:execute:"
	total_strategy_task = "total_strategy_task"
)

type IStrategyProcessor interface {
	Execute() error
}

type StrategyProcessor struct {
	collector     *metrics.MetricsCollector
	worker        StrategyWorker
	repository    StrategyRepository
	broker        broker.Broker
	signalUseCase SignalUseCase
}

type StrategyWorker interface {
	EnqueueStrategyTask(strategy entities.Strategy) error
}

type StrategyRepository interface {
	SaveExecution(ctx context.Context, execution entities.StrategyExecution) error
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
	CountOpenSignals(ctx context.Context, strategy entities.Strategy) (int64, error)
}

type SignalUseCase interface {
	GenerateBuySignal(e usecase.EntrySignal) error
	GenerateSellSignal(e usecase.ExitSignal) error
	GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error)
}

func NewStrategyProcessor(collector *metrics.MetricsCollector, w StrategyWorker, r StrategyRepository, b broker.Broker, uc SignalUseCase) *StrategyProcessor {
	return &StrategyProcessor{
		collector:     collector,
		worker:        w,
		repository:    r,
		broker:        b,
		signalUseCase: uc,
	}
}

func (p *StrategyProcessor) HandleStrategyTask(ctx context.Context, t *asynq.Task) error {
	var strategy entities.Strategy

	if err := json.Unmarshal(t.Payload(), &strategy); err != nil {
		return err
	}

	nStrategy, err := p.repository.GetByID(ctx, strategy.ID)
	if err != nil {
		log.Printf("Error getting strategy by ID: %v", err)
		return nil
	}

	if nStrategy.Status != entities.Disabled {
		err := p.processStrategy(ctx, nStrategy)

		p.collector.IncrementCounter(total_strategy_task, map[string]string{
			"strategy": strategy.Name,
		})

		log.Printf("Strategy: %s Executed", strategy.Name)

		p.worker.EnqueueStrategyTask(nStrategy)

		message := "Strategy executed successfully"
		status := entities.ExecutionStatus(entities.OK)
		if err != nil {
			message = "Error executing strategy: " + err.Error()
			status = entities.ExecutionStatus(entities.Error)
		}
		p.repository.SaveExecution(ctx, entities.StrategyExecution{
			StrategyID: strategy.ID,
			Status:     status,
			Message:    message,
			ExecutedAt: time.Now(),
			Strategy:   strategy,
		})
	}

	return nil
}

func (p *StrategyProcessor) processStrategy(ctx context.Context, strategy entities.Strategy) error {
	var executor IStrategyProcessor

	switch strategy.Algorithm {
	case entities.Grid:
		executor = grid.NewGridProcessor(strategy, p.broker, p.signalUseCase)
	case entities.Scalping:
		executor = scalping.NewScalpingProcessor(strategy, p.broker, p.signalUseCase)
	case entities.Bollinger:
		executor = bollinger.NewBollingerProcessor(strategy, p.broker, p.signalUseCase)
	default:
		log.Printf("Unknown strategy algorithm: %s", strategy.Algorithm)
	}

	// validate open signals per strategy
	count, err := p.repository.CountOpenSignals(ctx, strategy)
	if err != nil {
		return err
	}

	if count < 4 {
		err := executor.Execute()
		if err != nil {
			log.Printf("Error executing strategy %s: %v", strategy.Name, err)
		}
	}

	return err
}
