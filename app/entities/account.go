package entities

import "time"

type AccountMode string

const (
	AccountModeDryRun AccountMode = "dryrun"
	AccountModeLive   AccountMode = "live"
	AccountModePaper  AccountMode = "paper"
)

type Account struct {
	ID              int64       `gorm:"primaryKey" json:"id"`
	Mode            AccountMode `gorm:"type:varchar(20);not null;default:'dryrun';uniqueIndex" json:"mode"`
	Amount          float32     `gorm:"not null" json:"amount"`
	InitialAmount   float32     `gorm:"not null;default:10000" json:"initial_amount"`
	LockedAmount    float32     `gorm:"not null;default:0" json:"locked_amount"`
	MaxAllocation   float32     `gorm:"not null;default:0" json:"max_allocation"`
	AvailableOrders int64       `gorm:"not null" json:"available_orders"`
	Currency        string      `gorm:"not null" json:"currency"`
	LastSyncedAt    *time.Time  `json:"last_synced_at,omitempty"`
	CreatedAt       time.Time   `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time   `gorm:"autoUpdateTime" json:"updated_at"`
}
