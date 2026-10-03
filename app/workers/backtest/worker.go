package backtest

import (
	"encoding/json"
	"log"
	"time"

	"go-trade-bot/internal/configuration"

	"github.com/hibiken/asynq"
)

// BacktestTask runs one queued BacktestRun (B-02). Same shape as
// app/workers/optimize: one fixed task type, the payload is just the run's ID
// since the row already carries every parameter the job needs.
const BacktestTask = "backtest:execute"

// taskTimeout is only asynq's hard backstop. The limit that actually applies
// to a run is Settings.BacktestTimeoutMinutes (editable on the Settings
// page), enforced by BacktestProcessor when the run starts; this is just
// above the largest value that setting accepts so it never cuts a run short.
const taskTimeout = 25 * time.Hour

type TaskPayload struct {
	RunID uint `json:"run_id"`
}

type BacktestWorker struct {
	client *asynq.Client
}

func NewBacktestWorker(cfg *configuration.Configuration) BacktestWorker {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})
	return BacktestWorker{client: client}
}

// EnqueueBacktestTask enqueues the run for immediate execution. MaxRetry(0):
// ExecuteQueued records failures on the run row itself, and a retry of a
// half-finished replay would only repeat the same long job.
func (w BacktestWorker) EnqueueBacktestTask(runID uint) error {
	payload, err := json.Marshal(TaskPayload{RunID: runID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(BacktestTask, payload)
	info, err := w.client.Enqueue(task, asynq.MaxRetry(0), asynq.Timeout(taskTimeout))
	if err != nil {
		return err
	}
	log.Printf(" [*] Successfully enqueued backtest task: %+v", info.ID)
	return nil
}
