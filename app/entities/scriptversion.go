package entities

import "time"

// ScriptVersion stores historical versions of a script strategy's source code.
// Spec: docs/specs/strategy-scripting/phase-3-live-preview-and-authoring-polish.md
type ScriptVersion struct {
	ID         uint `gorm:"primaryKey"`
	StrategyID uint `gorm:"index"`
	Source     string `gorm:"type:text"`
	CreatedAt  time.Time
}
