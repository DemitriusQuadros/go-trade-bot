package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	tasks "go-trade-bot/app/handler/tasks/optimize"
	"go-trade-bot/app/handler/tasks/optimize/mocks"
	worker "go-trade-bot/app/workers/optimize"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestOptimizeProcessor_ProcessTask_Success(t *testing.T) {
	useCase := mocks.NewOptimizeUseCase(t)
	useCase.On("Run", mock.Anything, uint(42)).Return(nil)

	p := tasks.NewOptimizeProcessor(useCase)

	payload, _ := json.Marshal(worker.TaskPayload{RunID: 42})
	task := asynq.NewTask(worker.OptimizeTask, payload)

	err := p.ProcessTask(context.Background(), task)
	require.NoError(t, err)
}

func TestOptimizeProcessor_ProcessTask_UseCaseError_Propagated(t *testing.T) {
	useCase := mocks.NewOptimizeUseCase(t)
	useCase.On("Run", mock.Anything, uint(7)).Return(errors.New("grid search blew up"))

	p := tasks.NewOptimizeProcessor(useCase)

	payload, _ := json.Marshal(worker.TaskPayload{RunID: 7})
	task := asynq.NewTask(worker.OptimizeTask, payload)

	err := p.ProcessTask(context.Background(), task)
	assert.Error(t, err)
}

func TestOptimizeProcessor_ProcessTask_InvalidPayload(t *testing.T) {
	useCase := mocks.NewOptimizeUseCase(t)
	p := tasks.NewOptimizeProcessor(useCase)

	task := asynq.NewTask(worker.OptimizeTask, []byte("not json"))
	err := p.ProcessTask(context.Background(), task)
	assert.Error(t, err)
	useCase.AssertNotCalled(t, "Run", mock.Anything, mock.Anything)
}
