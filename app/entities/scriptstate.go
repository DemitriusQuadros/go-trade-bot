package entities

import (
	"time"

	"gorm.io/datatypes"
)

// ScriptState is one (strategy_id, symbol)-scoped row of a Lua script's
// persistent runtime state (blueprint §3, ADR-019). One row is
// upserted every cycle regardless of whether the script wrote to state -
// simplest correct semantics, no dirty-tracking needed inside the sandbox.
// This is DISTINCT from Strategy.StrategyConfiguration.Configuration (the
// author's static, engine-read-only parameters, e.g. stop_loss_pct) - see
// the blueprint's "script_state becomes a second source of truth" risk;
// the two must never be conflated at the code level, and this entity has no
// relationship to StrategyConfiguration beyond sharing a StrategyID FK.
type ScriptState struct {
	ID         uint           `gorm:"primaryKey"`
	StrategyID uint           `gorm:"uniqueIndex:idx_script_state_strategy_symbol"`
	Symbol     string         `gorm:"uniqueIndex:idx_script_state_strategy_symbol"`
	StateJSON  datatypes.JSON `gorm:"type:jsonb"`
	UpdatedAt  time.Time
}
