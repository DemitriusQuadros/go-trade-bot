package notifier_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEnqueuer struct {
	mu    sync.Mutex
	tasks []*asynq.Task
	opts  [][]asynq.Option
	err   error
	block bool // wait for ctx cancellation, like a hung Redis
}

func (f *fakeEnqueuer) EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.tasks = append(f.tasks, task)
	f.opts = append(f.opts, opts)
	return &asynq.TaskInfo{ID: "x"}, nil
}

func (f *fakeEnqueuer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tasks)
}

func TestAgentEventBridge_EnqueuesAgentEventOnAgentsQueue(t *testing.T) {
	enq := &fakeEnqueuer{}
	b := notifier.NewAgentEventBridge(enq, nil)
	ev := notifier.Event{Type: notifier.EventPositionClosed, StrategyID: 5, Symbol: "BTCUSDT", Mode: "dryrun",
		Data: map[string]any{"exit_reason": "simulated_stop_loss"}}
	require.NoError(t, b.Send(context.Background(), ev))
	b.Wait()

	require.Equal(t, 1, enq.count())
	assert.Equal(t, notifier.TaskAgentEvent, enq.tasks[0].Type())
	var p notifier.AgentEventPayload
	require.NoError(t, json.Unmarshal(enq.tasks[0].Payload(), &p))
	assert.Equal(t, notifier.EventPositionClosed, p.Event.Type)
	assert.Equal(t, uint(5), p.Event.StrategyID)
	assert.Equal(t, "simulated_stop_loss", p.Event.Data["exit_reason"])
	var opts []string
	for _, o := range enq.opts[0] {
		opts = append(opts, o.String())
	}
	assert.Contains(t, opts, `Queue("agents")`)
	assert.Contains(t, opts, "MaxRetry(3)")
	assert.Contains(t, opts, "Timeout(30s)")
}

func TestAgentEventBridge_SkipsBacktestEvents(t *testing.T) {
	enq := &fakeEnqueuer{}
	b := notifier.NewAgentEventBridge(enq, nil)
	require.NoError(t, b.Send(context.Background(), notifier.Event{Type: notifier.EventPositionOpened, Mode: "backtest"}))
	b.Wait()
	assert.Zero(t, enq.count())
}

func TestAgentEventBridge_FailuresAreCountedNeverReturned(t *testing.T) {
	var failures atomic.Int32
	b := notifier.NewAgentEventBridge(&fakeEnqueuer{err: errors.New("redis: connection refused")}, func(string) { failures.Add(1) })
	assert.NoError(t, b.Send(context.Background(), notifier.Event{Type: notifier.EventStrategyError, Mode: "live"}))
	b.Wait()
	assert.Equal(t, int32(1), failures.Load())
}

// A hung Redis must not block the caller: Send returns immediately and the
// background enqueue gives up at its own short timeout.
func TestAgentEventBridge_HungRedisDoesNotBlockSend(t *testing.T) {
	var failures atomic.Int32
	b := notifier.NewAgentEventBridge(&fakeEnqueuer{block: true}, func(string) { failures.Add(1) })
	b.SetEnqueueTimeout(50 * time.Millisecond)
	start := time.Now()
	for i := 0; i < 5; i++ {
		require.NoError(t, b.Send(context.Background(), notifier.Event{Type: notifier.EventPositionOpened, Mode: "live"}))
	}
	assert.Less(t, time.Since(start), 20*time.Millisecond, "Send must not wait on Redis")
	b.Wait()
	assert.Equal(t, int32(5), failures.Load())
}

func TestAgentEventBridge_SaturationDropsInsteadOfQueueing(t *testing.T) {
	var saturated atomic.Int32
	b := notifier.NewAgentEventBridge(&fakeEnqueuer{block: true}, func(reason string) {
		if reason == "saturated" {
			saturated.Add(1)
		}
	})
	b.SetEnqueueTimeout(200 * time.Millisecond)
	for i := 0; i < notifier.DefaultBridgeMaxInFlight+3; i++ {
		require.NoError(t, b.Send(context.Background(), notifier.Event{Type: notifier.EventPositionOpened, Mode: "live"}))
	}
	assert.Equal(t, int32(3), saturated.Load())
	b.Wait()
}

type stubSender struct {
	mu     sync.Mutex
	events []notifier.Event
	err    error
	panics bool
}

func (s *stubSender) Send(_ context.Context, e notifier.Event) error {
	if s.panics {
		panic("boom")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return s.err
}

func TestMultiNotifier_PrimaryResultOnlyAndAllSendersCalled(t *testing.T) {
	primary := &stubSender{err: errors.New("webhook 500")}
	secondary := &stubSender{}
	m := notifier.NewMultiNotifier(primary, &stubSender{panics: true}, secondary)
	err := m.Send(context.Background(), notifier.Event{Type: notifier.EventPositionClosed})
	assert.EqualError(t, err, "webhook 500", "the primary's result is what callers see")
	assert.Len(t, primary.events, 1)
	assert.Len(t, secondary.events, 1, "a panicking secondary does not stop the others")

	ok := notifier.NewMultiNotifier(&stubSender{}, &stubSender{err: errors.New("bridge down")})
	assert.NoError(t, ok.Send(context.Background(), notifier.Event{}), "a secondary's error never surfaces")
}
