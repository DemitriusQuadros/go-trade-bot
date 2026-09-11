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
	delay := cycle * time.Minute
	info, err := w.client.Enqueue(t1, asynq.ProcessIn(delay))
	if err != nil {
		return err
	}
	log.Printf(" [*] Successfully enqueued task: %+v", info.ID)
	return nil
}

// EnqueueStrategyTaskWithDelay is Spec backend-05's small, additive sibling
// to EnqueueStrategyTask: a fixed caller-supplied delay rather than the
// strategy's own Cycle-derived one, used solely to re-schedule a cycle held
// at the drain admission gate (app/handler/tasks/strategy.HandleStrategyTask)
// a short time later, distinct from its normal cycle interval.
func (w StrategyWorker) EnqueueStrategyTaskWithDelay(strategy entities.Strategy, delay time.Duration) error {
	payload, err := json.Marshal(strategy)
	if err != nil {
		return err
	}
	task := StrategyTask + strategy.Name
	t1 := asynq.NewTask(task, payload)
	info, err := w.client.Enqueue(t1, asynq.ProcessIn(delay))
	if err != nil {
		return err
	}
	log.Printf(" [*] Successfully re-enqueued held task with drain delay: %+v", info.ID)
	return nil
}
