package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/services/algorithm/grid"
	"go-trade-bot/app/services/algorithm/heikenashi"
	"go-trade-bot/app/services/algorithm/scalping"
	"go-trade-bot/app/services/algorithm/volume"
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
}

type SignalUseCase interface {
	GenerateBuySignal(symbol string, strategyId uint, price float32, quantity float32) error
	GenerateSellSignal(symbol string, strategyId uint, price float32) error
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

	err := p.processStrategy(strategy)

	p.collector.IncrementCounter(total_strategy_task, map[string]string{
		"strategy": strategy.Name,
	})

	log.Printf("Strategy: %s Executed", strategy.Name)

	p.worker.EnqueueStrategyTask(strategy)

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
	return nil
}

func (p *StrategyProcessor) processStrategy(strategy entities.Strategy) error {
	var executor IStrategyProcessor

	switch strategy.Algorithm {
	case entities.Grid:
		executor = grid.NewGridProcessor(strategy, p.broker, p.signalUseCase)
	case entities.Scalping:
		executor = scalping.NewScalpingProcessor(strategy, p.broker, p.signalUseCase)
	case entities.Heikenashi:
		executor = heikenashi.NewHeikenashiProcessor(strategy)
	case entities.Volume:
		executor = volume.NewVolumeProcessor(strategy)
	default:
		log.Printf("Unknown strategy algorithm: %s", strategy.Algorithm)
	}

	err := executor.Execute()
	if err != nil {
		log.Printf("Error executing strategy %s: %v", strategy.Name, err)
	}

	return err
}
