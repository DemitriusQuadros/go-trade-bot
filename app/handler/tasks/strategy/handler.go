package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/services/algorithm/grid"
	"go-trade-bot/app/services/algorithm/heikenashi"
	"go-trade-bot/app/services/algorithm/volume"
	"go-trade-bot/internal/metrics"
	"log"

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
	collector *metrics.MetricsCollector
	worker    StrategyWorker
}

type StrategyWorker interface {
	EnqueueStrategyTask(strategy entities.Strategy) error
}

func NewStrategyProcessor(collector *metrics.MetricsCollector, w StrategyWorker) *StrategyProcessor {
	return &StrategyProcessor{
		collector: collector,
		worker:    w,
	}
}

func (p *StrategyProcessor) HandleStrategyTask(ctx context.Context, t *asynq.Task) error {
	var strategy entities.Strategy

	if err := json.Unmarshal(t.Payload(), &strategy); err != nil {
		return err
	}

	processStrategy(strategy)

	p.collector.IncrementCounter(total_strategy_task, map[string]string{
		"strategy": strategy.Name,
	})

	log.Printf("Strategy: %s Executed", strategy.Name)

	p.worker.EnqueueStrategyTask(strategy)
	return nil
}

func processStrategy(strategy entities.Strategy) {
	var executor IStrategyProcessor

	switch strategy.Algorithm {
	case entities.Grid:
		executor = grid.NewGridProcessor(strategy)
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
}
