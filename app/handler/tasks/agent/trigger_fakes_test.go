package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agentplatform"
	agentworker "go-trade-bot/app/workers/agent"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

// Stateful fakes shared by the C-01 trigger tests (dispatch, sweeper,
// market watcher, chains).

type fakeAgents struct {
	mu       sync.Mutex
	agents   []entities.Agent
	bindings map[uint][]uint
}

func newFakeAgents() *fakeAgents { return &fakeAgents{bindings: map[uint][]uint{}} }

func (f *fakeAgents) add(a entities.Agent, t entities.AgentTriggers, bound ...uint) entities.Agent {
	f.mu.Lock()
	defer f.mu.Unlock()
	a.Triggers = agentplatform.MarshalTriggers(t)
	f.agents = append(f.agents, a)
	f.bindings[a.ID] = bound
	return a
}

func (f *fakeAgents) setTriggers(id uint, t entities.AgentTriggers) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.agents {
		if f.agents[i].ID == id {
			f.agents[i].Triggers = agentplatform.MarshalTriggers(t)
		}
	}
}

func (f *fakeAgents) ListAgents(context.Context) ([]entities.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]entities.Agent(nil), f.agents...), nil
}

func (f *fakeAgents) ListBindingsByAgent(_ context.Context, id uint) ([]entities.AgentStrategyBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []entities.AgentStrategyBinding
	for _, s := range f.bindings[id] {
		out = append(out, entities.AgentStrategyBinding{AgentID: id, StrategyID: s})
	}
	return out, nil
}

func (f *fakeAgents) GetAgent(_ context.Context, id uint) (entities.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.agents {
		if a.ID == id {
			return a, nil
		}
	}
	return entities.Agent{}, errors.New("not found")
}

type fakeStrategyStore struct{ byID map[uint]entities.Strategy }

func (f fakeStrategyStore) GetByID(_ context.Context, id uint) (entities.Strategy, error) {
	s, ok := f.byID[id]
	if !ok {
		return entities.Strategy{}, errors.New("not found")
	}
	return s, nil
}

func (f fakeStrategyStore) GetAll(context.Context) ([]entities.Strategy, error) {
	var out []entities.Strategy
	for _, s := range f.byID {
		out = append(out, s)
	}
	return out, nil
}

type fakeKillSwitch struct {
	mu     sync.Mutex
	paused bool
	err    error
}

func (f *fakeKillSwitch) set(p bool) { f.mu.Lock(); f.paused = p; f.mu.Unlock() }
func (f *fakeKillSwitch) Get(context.Context) (*entities.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return &entities.Settings{AgentsPaused: f.paused}, nil
}

// fakeRunEnqueuer records agent:run payloads and, like asynq, rejects a
// second task with an already-used TaskID.
type fakeRunEnqueuer struct {
	mu       sync.Mutex
	payloads []agentworker.RunPayload
	opts     [][]asynq.Option
	ids      map[string]bool
}

func (f *fakeRunEnqueuer) EnqueueContext(_ context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range opts {
		if o.Type() == asynq.TaskIDOpt {
			if f.ids == nil {
				f.ids = map[string]bool{}
			}
			id := o.Value().(string)
			if f.ids[id] {
				return nil, asynq.ErrTaskIDConflict
			}
			f.ids[id] = true
		}
	}
	var p agentworker.RunPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return nil, err
	}
	f.payloads = append(f.payloads, p)
	f.opts = append(f.opts, opts)
	return &asynq.TaskInfo{ID: "task"}, nil
}

func (f *fakeRunEnqueuer) runs() []agentworker.RunPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agentworker.RunPayload(nil), f.payloads...)
}

func (f *fakeRunEnqueuer) count() int { return len(f.runs()) }

type counterMetrics struct {
	mu     sync.Mutex
	counts map[string]float64
	gauges map[string]float64
}

func newCounterMetrics() *counterMetrics {
	return &counterMetrics{counts: map[string]float64{}, gauges: map[string]float64{}}
}

func (c *counterMetrics) IncrementCounter(name string, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[name+"/"+labels["kind"]]++
}

func (c *counterMetrics) SetGauge(name string, _ map[string]string, v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gauges[name] = v
}

func (c *counterMetrics) get(key string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[key]
}

func (c *counterMetrics) gauge(name string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gauges[name]
}

// fakeClock is a settable clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

func detailMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}
