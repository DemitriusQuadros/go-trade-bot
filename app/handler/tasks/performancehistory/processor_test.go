package tasks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	tasks "go-trade-bot/app/handler/tasks/performancehistory"
	"go-trade-bot/app/handler/tasks/performancehistory/mocks"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestSnapshotProcessor_ProcessTask_ComputesYesterdayWindow verifies the
// processor derives [yesterday 00:00, today 00:00) UTC at execution time,
// rather than requiring the caller to pass a window in the payload.
func TestSnapshotProcessor_ProcessTask_ComputesYesterdayWindow(t *testing.T) {
	useCase := mocks.NewUseCase(t)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)

	useCase.On("Snapshot", mock.Anything, entities.BucketDaily, yesterday, today).Return(nil)

	p := tasks.NewSnapshotProcessor(useCase)
	task := asynq.NewTask(tasks.SnapshotTask, nil)

	err := p.ProcessTask(context.Background(), task)
	require.NoError(t, err)
}

func TestSnapshotProcessor_ProcessTask_UseCaseError_Propagated(t *testing.T) {
	useCase := mocks.NewUseCase(t)
	useCase.On("Snapshot", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("db down"))

	p := tasks.NewSnapshotProcessor(useCase)
	task := asynq.NewTask(tasks.SnapshotTask, nil)

	err := p.ProcessTask(context.Background(), task)
	assert.Error(t, err)
}
