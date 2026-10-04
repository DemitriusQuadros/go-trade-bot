package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	worker "go-trade-bot/app/workers/backtest"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeUseCase struct {
	gotRunID uint
	err      error
}

func (f *fakeUseCase) ExecuteQueued(_ context.Context, runID uint) error {
	f.gotRunID = runID
	return f.err
}

func task(t *testing.T, runID uint) *asynq.Task {
	t.Helper()
	b, err := json.Marshal(worker.TaskPayload{RunID: runID})
	require.NoError(t, err)
	return asynq.NewTask(worker.BacktestTask, b)
}

func TestBacktestProcessor_PassesRunIDToUseCase(t *testing.T) {
	uc := &fakeUseCase{}
	require.NoError(t, NewBacktestProcessor(uc).ProcessTask(context.Background(), task(t, 42)))
	assert.Equal(t, uint(42), uc.gotRunID)
}

func TestBacktestProcessor_ReturnsUseCaseError(t *testing.T) {
	uc := &fakeUseCase{err: errors.New("boom")}
	err := NewBacktestProcessor(uc).ProcessTask(context.Background(), task(t, 7))
	assert.EqualError(t, err, "boom")
}

func TestBacktestProcessor_RejectsBadPayload(t *testing.T) {
	err := NewBacktestProcessor(&fakeUseCase{}).ProcessTask(context.Background(), asynq.NewTask(worker.BacktestTask, []byte("not json")))
	assert.Error(t, err)
}

type deadlineUseCase struct {
	remaining time.Duration
}

func (d *deadlineUseCase) ExecuteQueued(ctx context.Context, _ uint) error {
	dl, ok := ctx.Deadline()
	if !ok {
		return errors.New("no deadline on the run context")
	}
	d.remaining = time.Until(dl)
	return nil
}

func TestBacktestProcessor_UsesConfiguredTimeoutPerRun(t *testing.T) {
	uc := &deadlineUseCase{}
	p := NewBacktestProcessor(uc)

	minutes := 45
	p.SetTimeoutSource(func(context.Context) time.Duration { return time.Duration(minutes) * time.Minute })
	require.NoError(t, p.ProcessTask(context.Background(), task(t, 1)))
	assert.InDelta(t, (45 * time.Minute).Seconds(), uc.remaining.Seconds(), 5)

	// A later Settings change applies to the next run without a restart.
	minutes = 300
	require.NoError(t, p.ProcessTask(context.Background(), task(t, 2)))
	assert.InDelta(t, (300 * time.Minute).Seconds(), uc.remaining.Seconds(), 5)
}

func TestBacktestProcessor_FallsBackToDefaultTimeout(t *testing.T) {
	uc := &deadlineUseCase{}
	p := NewBacktestProcessor(uc) // no source wired
	require.NoError(t, p.ProcessTask(context.Background(), task(t, 1)))
	assert.InDelta(t, DefaultTimeout.Seconds(), uc.remaining.Seconds(), 5)

	p.SetTimeoutSource(func(context.Context) time.Duration { return 0 }) // read error / unset
	require.NoError(t, p.ProcessTask(context.Background(), task(t, 2)))
	assert.InDelta(t, DefaultTimeout.Seconds(), uc.remaining.Seconds(), 5)
}
