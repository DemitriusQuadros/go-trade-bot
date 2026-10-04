package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingEnqueuer mimics asynq's TaskID dedupe: a second task with an id
// already seen fails with ErrTaskIDConflict.
type recordingEnqueuer struct {
	mu    sync.Mutex
	tasks []*asynq.Task
	opts  [][]asynq.Option
	ids   map[string]bool
	err   error
}

func (r *recordingEnqueuer) EnqueueContext(_ context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	if r.ids == nil {
		r.ids = map[string]bool{}
	}
	for _, o := range opts {
		if o.Type() == asynq.TaskIDOpt {
			id := o.Value().(string)
			if r.ids[id] {
				return nil, asynq.ErrTaskIDConflict
			}
			r.ids[id] = true
		}
	}
	r.tasks = append(r.tasks, task)
	r.opts = append(r.opts, opts)
	return &asynq.TaskInfo{ID: "t"}, nil
}

func TestBuildChainPayload_DepthPathAndDetail(t *testing.T) {
	p, err := BuildChainPayload(ChainRequest{TargetAgentID: 2, SourceAgentID: 1, SourceRunID: 10, On: "report", ReportIDs: []uint{7}})
	require.NoError(t, err)
	assert.Equal(t, "chain", p.Trigger)
	assert.Equal(t, uint(2), p.AgentID)
	assert.Equal(t, 1, p.ChainDepth)
	assert.Equal(t, []uint{1}, p.ChainPath)
	require.NotNil(t, p.ParentRunID)
	assert.Equal(t, uint(10), *p.ParentRunID)
	var d map[string]any
	require.NoError(t, json.Unmarshal(p.Detail, &d))
	assert.Equal(t, float64(1), d["source_agent_id"])
	assert.Equal(t, float64(10), d["source_run_id"])
	assert.Equal(t, []any{float64(7)}, d["report_ids"])
	assert.Equal(t, "report", d["on"])

	// Depth: a run at depth 3 cannot chain further (depth 4 suppressed).
	_, err = BuildChainPayload(ChainRequest{TargetAgentID: 5, SourceAgentID: 4, SourceRunID: 13, SourceDepth: 3, SourcePath: []uint{1, 2, 3}})
	assert.True(t, IsChainSuppressed(err), "depth-4 chain must be suppressed: %v", err)
	_, err = BuildChainPayload(ChainRequest{TargetAgentID: 4, SourceAgentID: 3, SourceRunID: 12, SourceDepth: 2, SourcePath: []uint{1, 2}})
	assert.NoError(t, err, "depth 3 is allowed")

	// A -> B -> A: B's run (path [A]) trying to start A is suppressed.
	_, err = BuildChainPayload(ChainRequest{TargetAgentID: 1, SourceAgentID: 2, SourceRunID: 11, SourceDepth: 1, SourcePath: []uint{1}})
	assert.True(t, IsChainSuppressed(err))
	// Self-trigger is suppressed too.
	_, err = BuildChainPayload(ChainRequest{TargetAgentID: 2, SourceAgentID: 2, SourceRunID: 11})
	assert.True(t, IsChainSuppressed(err))
}

func TestChainLauncher_CountsAndDedupes(t *testing.T) {
	enq := &recordingEnqueuer{}
	l := NewChainLauncher(enq)
	fired, suppressed := 0, 0
	l.OnFired = func() { fired++ }
	l.OnSuppressed = func(string) { suppressed++ }

	req := ChainRequest{TargetAgentID: 2, SourceAgentID: 1, SourceRunID: 10, On: "success"}
	_, err := l.LaunchChain(context.Background(), req)
	require.NoError(t, err)
	_, err = l.LaunchChain(context.Background(), req)
	assert.True(t, IsChainSuppressed(err), "a redelivered parent cannot double-fire the same (target, source run)")
	_, err = l.LaunchChain(context.Background(), ChainRequest{TargetAgentID: 1, SourceAgentID: 2, SourceRunID: 11, SourceDepth: 1, SourcePath: []uint{1}})
	assert.True(t, IsChainSuppressed(err))
	assert.Equal(t, 1, fired)
	assert.Equal(t, 2, suppressed)
	require.Len(t, enq.tasks, 1)

	var opts []string
	for _, o := range enq.opts[0] {
		opts = append(opts, o.String())
	}
	assert.Contains(t, opts, `Queue("agents")`)
	assert.Contains(t, opts, `TaskID("agent-chain:2:10")`)

	enq.err = errors.New("redis down")
	_, err = l.LaunchChain(context.Background(), ChainRequest{TargetAgentID: 3, SourceAgentID: 1, SourceRunID: 10})
	assert.Error(t, err)
	assert.False(t, IsChainSuppressed(err))
}
