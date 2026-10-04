package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	taskagent "go-trade-bot/app/handler/tasks/agent"
	proposalusecase "go-trade-bot/app/usecase/proposal"
	agentworker "go-trade-bot/app/workers/agent"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubApplier struct {
	res proposalusecase.ApplyResult
	err error
	ids []uint
}

func (s *stubApplier) Apply(_ context.Context, id uint) (proposalusecase.ApplyResult, error) {
	s.ids = append(s.ids, id)
	return s.res, s.err
}

type recEnqueuer struct {
	mu     sync.Mutex
	ids    []uint
	delays []time.Duration
	err    error
}

func (r *recEnqueuer) EnqueueApplyProposal(_ context.Context, id uint, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
	r.delays = append(r.delays, d)
	return r.err
}

func applyTask(t *testing.T, id uint) *asynq.Task {
	b, err := json.Marshal(agentworker.ApplyProposalPayload{ProposalID: id})
	require.NoError(t, err)
	return asynq.NewTask(taskagent.TaskApplyProposal, b)
}

// Not flat yet: a NEW task is scheduled (ProcessIn 1 minute), the current
// one succeeds - asynq's retry counter is never used for waiting.
func TestApplyProcessor_RescheduleEnqueuesNewTask(t *testing.T) {
	a := &stubApplier{res: proposalusecase.ApplyResult{Outcome: proposalusecase.OutcomeRescheduled, RetryIn: time.Minute}}
	e := &recEnqueuer{}
	require.NoError(t, taskagent.NewApplyProcessor(a, e).ProcessTask(context.Background(), applyTask(t, 7)))
	assert.Equal(t, []uint{7}, a.ids)
	assert.Equal(t, []uint{7}, e.ids)
	assert.Equal(t, []time.Duration{time.Minute}, e.delays)
}

func TestApplyProcessor_TerminalOutcomesDoNotReschedule(t *testing.T) {
	for _, o := range []proposalusecase.Outcome{proposalusecase.OutcomeApplied, proposalusecase.OutcomeSkipped, proposalusecase.OutcomeSuperseded, proposalusecase.OutcomeFailed} {
		e := &recEnqueuer{}
		require.NoError(t, taskagent.NewApplyProcessor(&stubApplier{res: proposalusecase.ApplyResult{Outcome: o}}, e).ProcessTask(context.Background(), applyTask(t, 7)))
		assert.Empty(t, e.ids, o)
	}
}

func TestApplyProcessor_ErrorsAreRetried(t *testing.T) {
	e := &recEnqueuer{}
	err := taskagent.NewApplyProcessor(&stubApplier{err: errors.New("db down")}, e).ProcessTask(context.Background(), applyTask(t, 7))
	assert.Error(t, err)
	assert.Empty(t, e.ids)

	e = &recEnqueuer{err: errors.New("redis down")}
	err = taskagent.NewApplyProcessor(&stubApplier{res: proposalusecase.ApplyResult{Outcome: proposalusecase.OutcomeRescheduled, RetryIn: time.Minute}}, e).
		ProcessTask(context.Background(), applyTask(t, 7))
	assert.Error(t, err, "if the next check can't be scheduled, asynq retries this task")
}

func TestApplyProcessor_InvalidPayloadDropped(t *testing.T) {
	a := &stubApplier{}
	require.NoError(t, taskagent.NewApplyProcessor(a, &recEnqueuer{}).ProcessTask(context.Background(), asynq.NewTask(taskagent.TaskApplyProposal, []byte("{"))))
	assert.Empty(t, a.ids)
}

func TestApplyProposalOptions(t *testing.T) {
	task, err := agentworker.NewApplyProposalTask(3)
	require.NoError(t, err)
	assert.Equal(t, agentworker.TaskApplyProposal, task.Type())
	assert.JSONEq(t, `{"proposal_id":3}`, string(task.Payload()))
	assert.Len(t, agentworker.ApplyProposalOptions(0), 3)
	assert.Len(t, agentworker.ApplyProposalOptions(time.Minute), 4)
}
