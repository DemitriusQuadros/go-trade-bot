package candleimport

import (
	"context"
	"encoding/json"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candleimport"
	usecase "go-trade-bot/app/usecase/candleimport"
	"github.com/hibiken/asynq"
)

type TaskHandler struct {
	repo    candleimport.Repository
	usecase usecase.UseCase
}

func NewTaskHandler(repo candleimport.Repository, uc usecase.UseCase) *TaskHandler {
	return &TaskHandler{repo: repo, usecase: uc}
}

func (h *TaskHandler) HandleImportExecute(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		JobID   string                 `json:"job_id"`
		Request entities.ImportRequest `json:"request"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return err
	}

	job, err := h.repo.GetJob(ctx, payload.JobID)
	if err != nil {
		return err
	}

	job.Status = entities.ImportJobRunning
	h.repo.UpdateJob(ctx, job)

	summaries, err := h.usecase.Run(ctx, payload.Request)
	
	now := time.Now()
	job.CompletedAt = &now

	if err != nil {
		job.Status = entities.ImportJobFailed
		job.ErrorMessage = err.Error()
	} else {
		job.Status = entities.ImportJobCompleted
	}

	if len(summaries) > 0 {
		resBytes, _ := json.Marshal(map[string]interface{}{
			"per_pair": summaries,
		})
		job.ResultJSON = resBytes
	}

	h.repo.UpdateJob(ctx, job)

	// Even if there's an error, we processed it, so don't return error to Asynq
	// otherwise Asynq will retry it. Return nil to mark task complete.
	return nil
}

func (h *TaskHandler) HandleRecurringImport(ctx context.Context, t *asynq.Task) error {
	var schedule entities.ImportSchedule
	if err := json.Unmarshal(t.Payload(), &schedule); err != nil {
		return err
	}

	req := entities.ImportRequest{
		Symbols:    []string{schedule.Symbol},
		Timeframes: []string{schedule.Timeframe},
		// Example: from 2 days ago to now
		From: time.Now().Add(-48 * time.Hour),
		To:   time.Now(),
	}

	// create a job for this
	_, err := h.usecase.CreateJob(ctx, req)
	return err
}
