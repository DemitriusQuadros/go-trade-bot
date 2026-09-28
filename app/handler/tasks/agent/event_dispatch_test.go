package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	taskagent "go-trade-bot/app/handler/tasks/agent"
	"go-trade-bot/internal/cooldown"
	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

type dispatchEnv struct {
	agents   *fakeAgents
	settings *fakeKillSwitch
	enq      *fakeRunEnqueuer
	metrics  *counterMetrics
	clock    *fakeClock
	d        *taskagent.EventDispatcher
}

func newDispatchEnv(strategies map[uint]entities.Strategy) *dispatchEnv {
	e := &dispatchEnv{agents: newFakeAgents(), settings: &fakeKillSwitch{}, enq: &fakeRunEnqueuer{}, metrics: newCounterMetrics(), clock: &fakeClock{now: t0}}
	store := cooldown.NewMemoryStore()
	store.Now = e.clock.Now
	e.d = taskagent.NewEventDispatcher(e.agents, fakeStrategyStore{byID: strategies}, e.settings, store, e.enq, e.metrics)
	return e
}

func eventTask(t *testing.T, ev notifier.Event) *asynq.Task {
	b, err := json.Marshal(notifier.AgentEventPayload{Event: ev})
	require.NoError(t, err)
	return asynq.NewTask(taskagent.TaskAgentEvent, b)
}

func stopLossClose(strategyID uint) notifier.Event {
	return notifier.Event{Type: notifier.EventPositionClosed, Timestamp: t0, StrategyID: strategyID, Symbol: "BTCUSDT", Mode: "dryrun",
		Data: map[string]any{"exit_reason": "simulated_stop_loss", "profit": -12.5}}
}

func TestOccurrencesFromEvent(t *testing.T) {
	types := func(e notifier.Event) []string {
		var out []string
		for _, o := range taskagent.OccurrencesFromEvent(e) {
			out = append(out, o.Type)
		}
		return out
	}
	assert.Equal(t, []string{"position.closed", "stoploss.hit"}, types(stopLossClose(5)))
	assert.Equal(t, []string{"position.closed", "stoploss.hit"}, types(notifier.Event{Type: notifier.EventPositionClosed, StrategyID: 5, Data: map[string]any{"exit_reason": "stop_loss"}}))
	assert.Equal(t, []string{"position.closed"}, types(notifier.Event{Type: notifier.EventPositionClosed, StrategyID: 5, Data: map[string]any{"exit_reason": "take_profit"}}))
	assert.Equal(t, []string{"position.opened"}, types(notifier.Event{Type: notifier.EventPositionOpened, StrategyID: 5}))
	assert.Equal(t, []string{"strategy.error"}, types(notifier.Event{Type: notifier.EventStrategyError, StrategyID: 5}))
	assert.Equal(t, []string{"strategy.panic"}, types(notifier.Event{Type: notifier.EventStrategyError, StrategyID: 5, Data: map[string]any{"panic": true}}))
	assert.Empty(t, types(notifier.Event{Type: notifier.EventPositionOpened, StrategyID: 5, Mode: "backtest"}))
}

// C-01 AC#4.
func TestEventDispatch_StopLossHitScopeCooldownPauseKillSwitch(t *testing.T) {
	five := entities.Strategy{ID: 5, Name: "five", MonitoredSymbols: []string{"BTCUSDT"}}
	e := newDispatchEnv(map[uint]entities.Strategy{5: five, 6: {ID: 6}})
	trig := entities.AgentTriggers{Events: []entities.EventTrigger{{Type: "stoploss.hit"}}}
	bound := e.agents.add(entities.Agent{ID: 1, Name: "Bound"}, trig, 5)
	e.agents.add(entities.Agent{ID: 2, Name: "Other"}, trig, 6)                     // not bound to 5
	e.agents.add(entities.Agent{ID: 3, Name: "Paused", Paused: true}, trig, 5)      // paused
	e.agents.add(entities.Agent{ID: 4, Name: "ClosedOnly"}, entities.AgentTriggers{ // different trigger
		Events: []entities.EventTrigger{{Type: "position.opened"}}}, 5)

	require.NoError(t, e.d.ProcessTask(context.Background(), eventTask(t, stopLossClose(5))))
	runs := e.enq.runs()
	require.Len(t, runs, 1, "exactly one run: the bound, active agent with stoploss.hit")
	r := runs[0]
	assert.Equal(t, bound.ID, r.AgentID)
	assert.Equal(t, "event", r.Trigger)
	require.NotNil(t, r.StrategyID)
	assert.Equal(t, uint(5), *r.StrategyID)
	d := detailMap(t, r.Detail)
	assert.Equal(t, "stoploss.hit", d["event"])
	assert.Equal(t, float64(5), d["strategy_id"])
	assert.Equal(t, "BTCUSDT", d["symbol"])
	assert.Equal(t, "2026-05-01T12:00:00Z", d["occurred_at"])
	assert.Equal(t, "simulated_stop_loss", d["data"].(map[string]any)["exit_reason"])
	var opts []string
	for _, o := range e.enq.opts[0] {
		opts = append(opts, o.String())
	}
	assert.Contains(t, opts, `Queue("agents")`)
	assert.Equal(t, 1.0, e.metrics.get(taskagent.MetricTriggersFired+"/event"))

	// Second identical event inside the 15m default cooldown: suppressed.
	e.clock.Add(10 * time.Minute)
	require.NoError(t, e.d.ProcessTask(context.Background(), eventTask(t, stopLossClose(5))))
	assert.Equal(t, 1, e.enq.count())
	assert.Equal(t, 1.0, e.metrics.get(taskagent.MetricTriggersSuppressed+"/event"))

	// After the cooldown it fires again.
	e.clock.Add(6 * time.Minute)
	require.NoError(t, e.d.ProcessTask(context.Background(), eventTask(t, stopLossClose(5))))
	assert.Equal(t, 2, e.enq.count())

	// Kill switch on: nothing is dispatched.
	e.clock.Add(time.Hour)
	e.settings.set(true)
	require.NoError(t, e.d.ProcessTask(context.Background(), eventTask(t, stopLossClose(5))))
	assert.Equal(t, 2, e.enq.count())
}

// Events fire for challengers of bound champions and strategies the agent
// created (strategy_scope.go's definition), not only direct bindings.
func TestEventDispatch_ScopeIncludesChallengersAndCreated(t *testing.T) {
	champ, me := uint(5), uint(1)
	e := newDispatchEnv(map[uint]entities.Strategy{
		5: {ID: 5}, 7: {ID: 7, ChallengerOfID: &champ}, 8: {ID: 8, CreatedByAgentID: &me}, 9: {ID: 9},
	})
	e.agents.add(entities.Agent{ID: 1, Name: "A"}, entities.AgentTriggers{Events: []entities.EventTrigger{{Type: "position.opened"}}}, 5)
	for _, sid := range []uint{7, 8, 9} {
		_, err := e.d.Dispatch(context.Background(), taskagent.Occurrence{Type: "position.opened", StrategyID: sid, OccurredAt: t0})
		require.NoError(t, err)
	}
	var got []uint
	for _, r := range e.enq.runs() {
		got = append(got, *r.StrategyID)
	}
	assert.Equal(t, []uint{7, 8}, got)
}

func TestEventDispatch_FailsClosedOnUnreadableSettingsAndTruncatesData(t *testing.T) {
	e := newDispatchEnv(map[uint]entities.Strategy{5: {ID: 5}})
	e.agents.add(entities.Agent{ID: 1}, entities.AgentTriggers{Events: []entities.EventTrigger{{Type: "strategy.error"}}}, 5)
	e.settings.err = assert.AnError
	n, err := e.d.Dispatch(context.Background(), taskagent.Occurrence{Type: "strategy.error", StrategyID: 5})
	require.NoError(t, err)
	assert.Zero(t, n)

	e.settings.err = nil
	big := strings.Repeat("x", 5000)
	_, err = e.d.Dispatch(context.Background(), taskagent.Occurrence{Type: "strategy.error", StrategyID: 5, Data: map[string]any{"error": big, "reason": "lua_error"}})
	require.NoError(t, err)
	require.Equal(t, 1, e.enq.count())
	data := detailMap(t, e.enq.runs()[0].Detail)["data"].(map[string]any)
	assert.Equal(t, true, data["_truncated"])
	assert.Equal(t, "lua_error", data["reason"])
	assert.NotContains(t, data, "error")
}
