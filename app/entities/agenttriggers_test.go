package entities_test

import (
	"encoding/json"
	"testing"
	"time"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// C-01 AC#1: pre-C-01 rows (`events: ["position.closed"]`, `chain_from:
// [3]`) still load, and re-encode in the new shape.
func TestAgentTriggers_OldFormatLoadsAndRoundTrips(t *testing.T) {
	old := `{"cron":["0 * * * *"],"events":["position.closed","stoploss.hit"],"chain_from":[3,4]}`
	a := entities.Agent{Triggers: datatypes.JSON(old)}
	got := a.ParsedTriggers()
	assert.Equal(t, []string{"0 * * * *"}, got.Cron)
	assert.Equal(t, []entities.EventTrigger{{Type: "position.closed"}, {Type: "stoploss.hit"}}, got.Events)
	assert.Equal(t, []entities.ChainTrigger{{AgentID: 3, On: "report"}, {AgentID: 4, On: "report"}}, got.ChainFrom)

	var strict entities.AgentTriggers
	require.NoError(t, json.Unmarshal([]byte(old), &strict))
	assert.Equal(t, got, strict)

	b, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, `{"cron":["0 * * * *"],"events":[{"type":"position.closed"},{"type":"stoploss.hit"}],
		"chain_from":[{"agent_id":3,"on":"report"},{"agent_id":4,"on":"report"}]}`, string(b))
	var again entities.AgentTriggers
	require.NoError(t, json.Unmarshal(b, &again))
	assert.Equal(t, got, again)
}

func TestAgentTriggers_NewFormatRoundTrip(t *testing.T) {
	in := entities.AgentTriggers{
		Cron:      []string{"*/5 * * * *"},
		Events:    []entities.EventTrigger{{Type: "drawdown", ThresholdPct: 5, WindowHours: 24, CooldownMinutes: 240}},
		Market:    []entities.MarketRule{{ID: "r1", Symbol: "BTCUSDT", Kind: "pct_move", WindowMinutes: 60, ThresholdPct: 3, CooldownMinutes: 60}},
		ChainFrom: []entities.ChainTrigger{{AgentID: 2, On: "notify"}},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out entities.AgentTriggers
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
	assert.Equal(t, entities.TriggerSummary{Cron: 1, Events: 1, Market: 1, ChainFrom: 1}, out.Summary())

	// Mixed old/new elements in one list are accepted.
	require.NoError(t, json.Unmarshal([]byte(`{"events":["position.opened",{"type":"no_signal","window_hours":6}],"chain_from":[5,{"agent_id":6,"on":"success"}]}`), &out))
	assert.Equal(t, []entities.EventTrigger{{Type: "position.opened"}, {Type: "no_signal", WindowHours: 6}}, out.Events)
	assert.Equal(t, []entities.ChainTrigger{{AgentID: 5, On: "report"}, {AgentID: 6, On: "success"}}, out.ChainFrom)
}

// A malformed element fails strict decoding (API -> 400) but never wipes the
// rest of a stored row (the cron schedule survives a bad market rule).
func TestAgentTriggers_LenientVsStrict(t *testing.T) {
	raw := `{"cron":["0 * * * *"],"market":[{"id":"ok","symbol":"BTCUSDT","kind":"pct_move","window_minutes":5,"threshold_pct":1,"cooldown_minutes":60},{"symbol":42}],"events":[true]}`
	var strict entities.AgentTriggers
	assert.Error(t, json.Unmarshal([]byte(raw), &strict))
	lenient := entities.ParseTriggersLenient([]byte(raw))
	assert.Equal(t, []string{"0 * * * *"}, lenient.Cron)
	require.Len(t, lenient.Market, 1)
	assert.Equal(t, "ok", lenient.Market[0].ID)
	assert.Empty(t, lenient.Events)
	assert.Equal(t, entities.AgentTriggers{}, entities.ParseTriggersLenient(nil))
	assert.Equal(t, entities.AgentTriggers{}, entities.ParseTriggersLenient([]byte("{}")))
}

func TestTriggerDefaults(t *testing.T) {
	assert.Equal(t, 15*time.Minute, entities.EventTrigger{Type: "position.closed"}.EffectiveCooldown())
	assert.Equal(t, 240*time.Minute, entities.EventTrigger{Type: "drawdown"}.EffectiveCooldown())
	assert.Equal(t, 240*time.Minute, entities.EventTrigger{Type: "no_signal"}.EffectiveCooldown())
	assert.Equal(t, 24, entities.EventTrigger{Type: "drawdown"}.EffectiveWindowHours())
	assert.Equal(t, 0, entities.EventTrigger{Type: "no_signal"}.EffectiveWindowHours())
	assert.Equal(t, 60*time.Minute, entities.MarketRule{}.EffectiveCooldown())
	assert.Equal(t, 5*time.Minute, entities.MarketRule{CooldownMinutes: 2}.EffectiveCooldown())
}
