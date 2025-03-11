package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"log"

	"github.com/hibiken/asynq"
)

const StrategyTask = "strategy:execute:"

type StrategyProcessor struct{}

func (p *StrategyProcessor) HandleStrategyTask(ctx context.Context, t *asynq.Task) error {
	var strategy entities.Strategy

	if err := json.Unmarshal(t.Payload(), &strategy); err != nil {
		return err
	}
	log.Printf("Executando estratégia: %s", strategy.Name)

	return nil
}
