package tasks

import (
	"context"
	"log"
	"time"

	"go-trade-bot/app/entities"

	"github.com/hibiken/asynq"
)

// SnapshotTask is backend-05's daily periodic job type, driven by an
// asynq.Scheduler cron entry (cmd/worker/main.go) rather than an
// explicit per-call Enqueue - there is no "worker" client wrapper for this
// task the way app/workers/strategy or app/workers/optimize have one, since
// nothing ever enqueues it on demand; the scheduler is the sole producer.
const SnapshotTask = "performance:snapshot:daily"

// UseCase is the narrow slice of
// app/usecase/performancehistory.PerformanceHistoryUseCase this processor
// depends on.
type UseCase interface {
	Snapshot(ctx context.Context, bucket entities.PerformanceBucket, periodStart, periodEnd time.Time) error
}

type SnapshotProcessor struct {
	useCase UseCase
}

func NewSnapshotProcessor(u UseCase) *SnapshotProcessor {
	return &SnapshotProcessor{useCase: u}
}

// ProcessTask computes "yesterday" (the most recently completed full UTC
// day) at execution time and snapshots it. Parameterizing by the computed
// window (not "since last run") is what makes a manually-triggered backfill
// for a missed day safe (spec AC#7) - this processor's own cron-triggered
// invocation is just the common case of that same mechanism.
func (p *SnapshotProcessor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)

	if err := p.useCase.Snapshot(ctx, entities.BucketDaily, yesterday, today); err != nil {
		log.Printf("daily performance snapshot failed for %s: %v", yesterday.Format("2006-01-02"), err)
		return err
	}

	log.Printf("daily performance snapshot completed for %s", yesterday.Format("2006-01-02"))
	return nil
}
