package tasks

import (
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/configuration"
	"log"
	"time"

	"github.com/hibiken/asynq"
)

// TODO: Implement integration test with redis
type StrategyWorker struct {
	client *asynq.Client
}

func NewStrategyWorker(cfg *configuration.Configuration) StrategyWorker {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})

	return StrategyWorker{
		client: client,
	}
}

const (
	StrategyTask = "strategy:task:"
)

func (w StrategyWorker) EnqueueStrategyTask(strategy entities.Strategy) error {
	payload, err := json.Marshal(strategy)
	if err != nil {
		return err
	}
	task := StrategyTask + strategy.Name
	t1 := asynq.NewTask(task, payload)
	cycle := time.Duration(strategy.StrategyConfiguration.Cycle)
	time := cycle * time.Minute
	info, err := w.client.Enqueue(t1, asynq.ProcessIn(time))
	if err != nil {
		return err
	}
	log.Printf(" [*] Successfully enqueued task: %+v", info)
	return nil
}
