package candleimport

import (
	"context"
	"encoding/json"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/configuration"
	"github.com/hibiken/asynq"
)

const (
	TaskImportExecute = "candleimport:execute"
	TaskRecurringImport = "candleimport:recurring"
)

type Worker interface {
	EnqueueImport(ctx context.Context, jobID string, req entities.ImportRequest) error
}

type worker struct {
	client *asynq.Client
}

// NewWorker takes *configuration.Configuration and constructs its own
// *asynq.Client, matching app/workers/strategy.NewStrategyWorker's exact
// pattern - not a dependency-injected *asynq.Client, which nothing in
// cmd/api's or cmd/worker's fx graph ever provided (a real startup fx
// wiring bug: "missing type: *asynq.Client").
func NewWorker(cfg *configuration.Configuration) Worker {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})
	return &worker{client: client}
}

func (w *worker) EnqueueImport(ctx context.Context, jobID string, req entities.ImportRequest) error {
	payload, err := json.Marshal(map[string]interface{}{
		"job_id":  jobID,
		"request": req,
	})
	if err != nil {
		return err
	}

	task := asynq.NewTask(TaskImportExecute, payload)
	_, err = w.client.EnqueueContext(ctx, task)
	return err
}
