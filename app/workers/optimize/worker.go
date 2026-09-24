package optimize

import (
	"encoding/json"
	"log"

	"go-trade-bot/internal/configuration"

	"github.com/hibiken/asynq"
)

// OptimizeTask is this codebase's first async job task type (backend-01):
// unlike app/workers/strategy.StrategyTask (a per-strategy-name prefix, one
// task type per strategy), a single fixed task type is enough here since the
// payload just carries the OptimizationRun's ID - the run row itself already
// carries every parameter the job needs.
const OptimizeTask = "optimize:execute"

type TaskPayload struct {
	RunID uint `json:"run_id"`
}

type OptimizeWorker struct {
	client *asynq.Client
}

func NewOptimizeWorker(cfg *configuration.Configuration) OptimizeWorker {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})
	return OptimizeWorker{client: client}
}

// EnqueueOptimizeTask enqueues the grid search for immediate execution -
// unlike the strategy cycle task, there is no delay/re-enqueue loop: an
// optimization run executes once, start to finish (success or failure), and
// is never re-scheduled by this worker.
func (w OptimizeWorker) EnqueueOptimizeTask(runID uint) error {
	payload, err := json.Marshal(TaskPayload{RunID: runID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(OptimizeTask, payload)
	info, err := w.client.Enqueue(task)
	if err != nil {
		return err
	}
	log.Printf(" [*] Successfully enqueued optimization task: %+v", info.ID)
	return nil
}
