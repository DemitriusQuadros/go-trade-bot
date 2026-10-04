package modules_test

// agents-platform C-01 §2.1 / AC#3: the worker's trade events fan out to the
// existing webhook notifier AND the fire-and-forget agents bridge. The
// trading path (app/usecase/signal, app/engine) is untouched; these tests sit
// on the fan-out.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	signalusecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/cmd/worker/modules"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bridgeEnqueuer struct {
	mu    sync.Mutex
	tasks []*asynq.Task
	opts  [][]asynq.Option
	err   error
}

func (b *bridgeEnqueuer) EnqueueContext(_ context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return nil, b.err
	}
	b.tasks = append(b.tasks, task)
	b.opts = append(b.opts, opts)
	return &asynq.TaskInfo{ID: "t"}, nil
}

func (b *bridgeEnqueuer) events(t *testing.T) []notifier.Event {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []notifier.Event
	for _, task := range b.tasks {
		require.Equal(t, notifier.TaskAgentEvent, task.Type())
		var p notifier.AgentEventPayload
		require.NoError(t, json.Unmarshal(task.Payload(), &p))
		out = append(out, p.Event)
	}
	return out
}

type webhookSink struct {
	srv    *httptest.Server
	mu     sync.Mutex
	events []notifier.Event
}

func newWebhookSink(t *testing.T) *webhookSink {
	s := &webhookSink{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var e notifier.Event
		if json.Unmarshal(body, &e) == nil {
			s.mu.Lock()
			s.events = append(s.events, e)
			s.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *webhookSink) ofType(typ notifier.EventType) []notifier.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []notifier.Event
	for _, e := range s.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// runStopLossScenario drives entry + simulated stop-loss cycles through the
// real worker wiring with sender = production fan-out (real
// SwappableNotifier -> webhook sink, + bridge over enq).
func runStopLossScenario(t *testing.T, enq *bridgeEnqueuer) (*workerStack, *webhookSink, *notifier.AgentEventBridge) {
	t.Helper()
	sink := newWebhookSink(t)
	swappable, err := notifier.NewSwappableNotifier(&configuration.Configuration{WebhookURL: sink.srv.URL})
	require.NoError(t, err)
	bridge := notifier.NewAgentEventBridge(enq, nil)
	fanout := modules.NewTradeEventSender(swappable, bridge)

	s := newWorkerStackWithSender(t, "dryrun", strategies.ModeLive, false, false, func(capture *captureNotifier) notifier.NotificationSender {
		return notifier.NewMultiNotifier(fanout, capture)
	})

	fix01LongEnabled.Store(true)
	s.setClock(fix01T0)
	s.real.setMarket([]exchange.Candle{minuteCandle(-2, 99.5), minuteCandle(-1, 99.5)}, 100)
	s.cycle(t) // entry (require.NoError inside: the cycle succeeds)
	fix01LongEnabled.Store(false)

	s.setClock(fix01T0.Add(150 * time.Second))
	s.real.setMarket([]exchange.Candle{minuteCandle(0, 99), minuteCandle(1, 97), minuteCandle(2, 40)}, 97)
	s.cycle(t) // simulated stop fires
	bridge.Wait()
	return s, sink, bridge
}

func TestWorkerBridge_StopLossCloseReachesAgentsQueue(t *testing.T) {
	enq := &bridgeEnqueuer{}
	s, sink, _ := runStopLossScenario(t, enq)

	sigs := s.signals(t)
	require.Len(t, sigs, 1)
	assert.Equal(t, entities.Closed, sigs[0].Status)

	evs := enq.events(t)
	var closed []notifier.Event
	for _, e := range evs {
		if e.Type == notifier.EventPositionClosed {
			closed = append(closed, e)
		}
	}
	require.Len(t, closed, 1, "position.closed reaches the agents queue as agent:event")
	assert.Equal(t, signalusecase.ExitReasonSimulatedStopLoss, closed[0].Data["exit_reason"])
	assert.Equal(t, uint(42), closed[0].StrategyID)
	var opts []string
	for _, o := range enq.opts[0] {
		opts = append(opts, o.String())
	}
	assert.Contains(t, opts, `Queue("agents")`)

	assert.Eventually(t, func() bool { return len(sink.ofType(notifier.EventPositionClosed)) == 1 }, 3*time.Second, 20*time.Millisecond,
		"the existing webhook still receives the event")
}

func TestWorkerBridge_FailingEnqueuerNeverBreaksTheCycle(t *testing.T) {
	enq := &bridgeEnqueuer{err: errors.New("dial tcp: redis connection refused")}
	s, sink, _ := runStopLossScenario(t, enq) // cycles assert NoError internally

	sigs := s.signals(t)
	require.Len(t, sigs, 1)
	assert.Equal(t, entities.Closed, sigs[0].Status, "the stop-loss close still happened")
	assert.Len(t, s.notes.ofType(notifier.EventPositionClosed), 1, "every sender still received the event")
	assert.Eventually(t, func() bool { return len(sink.ofType(notifier.EventPositionClosed)) == 1 }, 3*time.Second, 20*time.Millisecond,
		"the webhook still receives the event with Redis down")
	assert.Empty(t, enq.events(t))
}

// SwappableNotifier.Swap keeps working behind the fan-out and does not swap
// the bridge.
func TestWorkerBridge_SwapStillWorksAndBridgeIsNotSwapped(t *testing.T) {
	first, second := newWebhookSink(t), newWebhookSink(t)
	swappable, err := notifier.NewSwappableNotifier(&configuration.Configuration{WebhookURL: first.srv.URL})
	require.NoError(t, err)
	enq := &bridgeEnqueuer{}
	bridge := notifier.NewAgentEventBridge(enq, nil)
	sender := modules.NewTradeEventSender(swappable, bridge)

	require.NoError(t, sender.Send(context.Background(), notifier.Event{Type: notifier.EventPositionOpened, Mode: "live"}))
	require.NoError(t, swappable.Swap(&configuration.Configuration{WebhookURL: second.srv.URL}))
	require.NoError(t, sender.Send(context.Background(), notifier.Event{Type: notifier.EventPositionClosed, Mode: "live"}))
	bridge.Wait()

	assert.Eventually(t, func() bool {
		return len(first.ofType(notifier.EventPositionOpened)) == 1 && len(second.ofType(notifier.EventPositionClosed)) == 1
	}, 3*time.Second, 20*time.Millisecond)
	assert.Empty(t, first.ofType(notifier.EventPositionClosed))
	assert.Len(t, enq.events(t), 2, "the bridge got both events across the swap")
}
