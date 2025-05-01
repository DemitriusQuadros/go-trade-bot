package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/metrics"
	"log"

	"github.com/hibiken/asynq"
)

const (
	StrategyTask        = "strategy:execute:"
	total_strategy_task = "total_strategy_task"
)

type StrategyProcessor struct {
	collector *metrics.MetricsCollector
}

func NewStrategyProcessor(collector *metrics.MetricsCollector) *StrategyProcessor {
	return &StrategyProcessor{
		collector: collector,
	}
}

func (p *StrategyProcessor) HandleStrategyTask(ctx context.Context, t *asynq.Task) error {
	var strategy entities.Strategy

	if err := json.Unmarshal(t.Payload(), &strategy); err != nil {
		return err
	}

	p.collector.IncrementCounter(total_strategy_task, map[string]string{
		"strategy": strategy.Name,
	})

	log.Printf("Executando estratégia: %s", strategy.Name)

	return nil
}
