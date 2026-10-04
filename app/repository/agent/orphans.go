package agent

import (
	"context"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
)

// OrphanedRunMessage is the error recorded on a run that never finished.
const OrphanedRunMessage = "interrupted: the agent runtime stopped or timed out before this run finished"

// MarkOrphanedRuns marks runs that started before startedBefore and never
// finished as errors. A run can be left unfinished if its process died or
// (before the detached-persist fix) its task deadline expired mid-save; such
// rows otherwise show as in progress forever. Pre-"running"-status rows are
// covered too: they were created as "ok" with a zero finished_at.
//
// startedBefore must be older than the longest possible live run (the
// agent:run task timeout) so that a restart of one cmd/agent replica never
// marks another replica's in-flight run.
func MarkOrphanedRuns(ctx context.Context, db *gorm.DB, startedBefore time.Time) (int64, error) {
	zeroCutoff := time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC)
	res := db.WithContext(ctx).Model(&entities.AgentRun{}).
		Where("started_at < ?", startedBefore).
		Where("status = ? OR (status = ? AND finished_at < ?)", entities.AgentRunRunning, entities.AgentRunOK, zeroCutoff).
		Updates(map[string]any{
			"status":        entities.AgentRunError,
			"error_message": OrphanedRunMessage,
			"finished_at":   time.Now(),
		})
	return res.RowsAffected, res.Error
}
