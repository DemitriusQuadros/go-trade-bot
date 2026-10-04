// Package candledata holds the asynq handlers for the candle-dataset reconciler.
package candledata

import (
	"context"
	"encoding/json"
	"fmt"

	usecase "go-trade-bot/app/usecase/candledata"

	"github.com/hibiken/asynq"
)

type TaskHandler struct{ svc *usecase.Service }

func NewTaskHandler(svc *usecase.Service) *TaskHandler { return &TaskHandler{svc: svc} }

// HandleChunk runs one planned chunk. The service decides whether asynq should
// retry (non-nil error) or not.
func (h *TaskHandler) HandleChunk(ctx context.Context, t *asynq.Task) error {
	var p struct {
		ChunkID uint `json:"chunk_id"`
	}
	if err := json.Unmarshal(t.Payload(), &p); err != nil || p.ChunkID == 0 {
		return fmt.Errorf("candledata: bad chunk payload: %v: %w", err, asynq.SkipRetry)
	}
	return h.svc.RunChunk(ctx, p.ChunkID)
}

// HandleSweep is the minute-by-minute self-healing pass.
func (h *TaskHandler) HandleSweep(ctx context.Context, _ *asynq.Task) error {
	return h.svc.Sweep(ctx)
}
