package tasks

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	worker "go-trade-bot/app/workers/backtest"

	"github.com/hibiken/asynq"
)

// BacktestUseCase is the narrow slice of app/usecase/backtest.BacktestUseCase
// this processor depends on.
type BacktestUseCase interface {
	ExecuteQueued(ctx context.Context, runID uint) error
}

// DefaultTimeout applies until a TimeoutSource is set, and whenever it
// returns a non-positive duration.
const DefaultTimeout = 2 * time.Hour

// TimeoutSource returns the per-run limit currently configured on the
// Settings page (Settings.BacktestTimeoutMinutes).
type TimeoutSource func(ctx context.Context) time.Duration

type BacktestProcessor struct {
	useCase BacktestUseCase
	mu      sync.RWMutex
	timeout TimeoutSource
}

func NewBacktestProcessor(u BacktestUseCase) *BacktestProcessor {
	return &BacktestProcessor{useCase: u}
}

// SetTimeoutSource wires where the per-run limit is read from. cmd/worker
// sets it once the settings repository exists.
func (p *BacktestProcessor) SetTimeoutSource(src TimeoutSource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeout = src
}

// runTimeout reads the limit fresh for every run so a Settings change
// applies to the next backtest without restarting the worker.
func (p *BacktestProcessor) runTimeout(ctx context.Context) time.Duration {
	p.mu.RLock()
	src := p.timeout
	p.mu.RUnlock()
	if src == nil {
		return DefaultTimeout
	}
	if d := src(ctx); d > 0 {
		return d
	}
	return DefaultTimeout
}

// ProcessTask implements asynq.Handler for worker.BacktestTask. ExecuteQueued
// has already persisted a "failed" status on the run before it returns an
// error; the task is enqueued with MaxRetry(0) so asynq never re-runs a
// replay that just failed, and the error is returned for its dead-letter
// accounting.
func (p *BacktestProcessor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload worker.TaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, p.runTimeout(ctx))
	defer cancel()

	if err := p.useCase.ExecuteQueued(runCtx, payload.RunID); err != nil {
		log.Printf("backtest run %d failed: %v", payload.RunID, err)
		return err
	}

	log.Printf("backtest run %d finished", payload.RunID)
	return nil
}
