package tasks

import (
	"context"
	"encoding/json"
	"log"

	worker "go-trade-bot/app/workers/optimize"

	"github.com/hibiken/asynq"
)

// OptimizeUseCase is the narrow slice of app/usecase/optimize.OptimizeUseCase
// this processor depends on.
type OptimizeUseCase interface {
	Run(ctx context.Context, runID uint) error
}

type OptimizeProcessor struct {
	useCase OptimizeUseCase
}

func NewOptimizeProcessor(u OptimizeUseCase) *OptimizeProcessor {
	return &OptimizeProcessor{useCase: u}
}

// ProcessTask implements asynq.Handler for worker.OptimizeTask. Errors are
// returned (not swallowed) so asynq's own retry/dead-letter accounting sees
// them - the OptimizeUseCase.Run method itself already persists a "failed"
// status onto the OptimizationRun row before returning an error, so a
// caller polling GET /optimize/{id} sees the failure regardless of whether
// asynq additionally retries the task.
func (p *OptimizeProcessor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload worker.TaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return err
	}

	if err := p.useCase.Run(ctx, payload.RunID); err != nil {
		log.Printf("optimize run %d failed: %v", payload.RunID, err)
		return err
	}

	log.Printf("optimize run %d completed", payload.RunID)
	return nil
}
