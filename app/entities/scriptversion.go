package entities

import "time"

// ScriptVersion stores historical versions of a script strategy's source code.
// Spec: docs/specs/strategy-scripting/phase-3-live-preview-and-authoring-polish.md
type ScriptVersion struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	StrategyID uint      `gorm:"index" json:"strategy_id"`
	Source     string    `gorm:"type:text" json:"source"`
	CreatedAt  time.Time `json:"created_at"`
}
