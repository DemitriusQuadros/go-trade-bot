package notifier

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

// Worker -> agents bridge (agents-platform C-01 §2.1). The trading worker
// fans every trade event out to its existing webhook notifier AND to this
// bridge, which enqueues it for cmd/agent as an agent:event task. The bridge
// is fire-and-forget: Send never blocks the caller on Redis and never
// returns an error.

// TaskAgentEvent is the asynq task type cmd/agent's event dispatcher serves.
const TaskAgentEvent = "agent:event"

// AgentEventQueue is the queue agent:event tasks go to - the same "agents"
// queue only cmd/agent consumes (app/workers/agent.Queue).
const AgentEventQueue = "agents"

// AgentEventPayload is the agent:event task payload.
type AgentEventPayload struct {
	Event Event `json:"event"`
}

// Bridge defaults.
const (
	DefaultBridgeEnqueueTimeout = 2 * time.Second
	// DefaultBridgeMaxInFlight bounds concurrent background enqueues; events
	// beyond it are dropped (and counted) rather than queued in memory.
	DefaultBridgeMaxInFlight = 32
	bridgeTaskTimeout        = 30 * time.Second
	bridgeMaxRetry           = 3
)

// TaskEnqueuer is satisfied by *asynq.Client.
type TaskEnqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// AgentEventBridge implements NotificationSender by enqueueing agent:event.
type AgentEventBridge struct {
	enq       TaskEnqueuer
	timeout   time.Duration
	sem       chan struct{}
	wg        sync.WaitGroup
	onFailure func(reason string)
}

// NewAgentEventBridge builds a bridge over enq. onFailure (may be nil) is
// called with "enqueue", "saturated", "marshal" or "panic" for every event
// that did not reach the queue - cmd/worker counts it in
// agent_bridge_failures_total.
func NewAgentEventBridge(enq TaskEnqueuer, onFailure func(reason string)) *AgentEventBridge {
	return &AgentEventBridge{
		enq:       enq,
		timeout:   DefaultBridgeEnqueueTimeout,
		sem:       make(chan struct{}, DefaultBridgeMaxInFlight),
		onFailure: onFailure,
	}
}

var _ NotificationSender = (*AgentEventBridge)(nil)

// SetEnqueueTimeout overrides the per-event enqueue timeout (tests).
func (b *AgentEventBridge) SetEnqueueTimeout(d time.Duration) {
	if d > 0 {
		b.timeout = d
	}
}

// AgentEventTaskOptions are the agent:event task options.
func AgentEventTaskOptions() []asynq.Option {
	return []asynq.Option{asynq.Queue(AgentEventQueue), asynq.MaxRetry(bridgeMaxRetry), asynq.Timeout(bridgeTaskTimeout)}
}

// Send hands the event to a background goroutine that enqueues it with a
// short timeout, and returns nil immediately. Backtest-mode events are
// skipped. It never blocks on Redis, never panics and never returns an
// error: a Redis outage only loses agent triggers, never a trading cycle.
func (b *AgentEventBridge) Send(_ context.Context, event Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			b.fail("panic", event, nil)
			err = nil
		}
	}()
	if b == nil || b.enq == nil || event.Mode == "backtest" {
		return nil
	}
	payload, mErr := json.Marshal(AgentEventPayload{Event: event})
	if mErr != nil {
		b.fail("marshal", event, mErr)
		return nil
	}
	select {
	case b.sem <- struct{}{}:
	default:
		b.fail("saturated", event, nil)
		return nil
	}
	b.wg.Add(1)
	go b.enqueue(event, payload)
	return nil
}

func (b *AgentEventBridge) enqueue(event Event, payload []byte) {
	defer b.wg.Done()
	defer func() { <-b.sem }()
	defer func() {
		if r := recover(); r != nil {
			b.fail("panic", event, nil)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	if _, err := b.enq.EnqueueContext(ctx, asynq.NewTask(TaskAgentEvent, payload), AgentEventTaskOptions()...); err != nil {
		b.fail("enqueue", event, err)
	}
}

// Wait blocks until every in-flight enqueue has finished (tests, shutdown).
func (b *AgentEventBridge) Wait() {
	b.wg.Wait()
}

func (b *AgentEventBridge) fail(reason string, event Event, err error) {
	log.Printf("agent bridge: %s event for strategy %d not forwarded to agents (%s): %v", event.Type, event.StrategyID, reason, err)
	if b.onFailure != nil {
		func() {
			defer func() { _ = recover() }()
			b.onFailure(reason)
		}()
	}
}

// MultiNotifier fans an event out to several senders in order. Every
// sender is always called; the returned error is the Primary's only, so a
// secondary (the agents bridge) can never change what callers observe.
type MultiNotifier struct {
	Primary     NotificationSender
	Secondaries []NotificationSender
}

// NewMultiNotifier builds a fan-out whose result is primary's.
func NewMultiNotifier(primary NotificationSender, secondaries ...NotificationSender) *MultiNotifier {
	return &MultiNotifier{Primary: primary, Secondaries: secondaries}
}

var _ NotificationSender = (*MultiNotifier)(nil)

// Send calls the primary, then every secondary (a secondary's error or
// panic is swallowed), and returns the primary's error.
func (m *MultiNotifier) Send(ctx context.Context, event Event) error {
	var err error
	if m.Primary != nil {
		err = m.Primary.Send(ctx, event)
	}
	for _, s := range m.Secondaries {
		if s == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("notifier: secondary sender panicked on %s: %v", event.Type, r)
				}
			}()
			_ = s.Send(ctx, event)
		}()
	}
	return err
}
