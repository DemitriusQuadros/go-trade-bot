package entities

import (
	"time"

	"go-trade-bot/internal/i18n"

	"gorm.io/datatypes"
)

// User is an app account (auth-01 §1). Every user sees the same shared bot;
// Role/Capabilities only decide what they may change. Roles and capability
// names live in internal/authz.
type User struct {
	ID uint `gorm:"primaryKey"`
	// Username is stored lowercased (case-insensitive unique), 3-32 chars of
	// [a-z0-9_.-].
	Username    string `gorm:"uniqueIndex;size:32;not null"`
	DisplayName string
	// Email is optional; unique when set (nil = unset, so several users may
	// have none).
	Email        *string `gorm:"uniqueIndex"`
	PasswordHash string  `json:"-"`
	Role         string
	Capabilities datatypes.JSONSlice[string] `gorm:"type:jsonb"`
	// DailyAgentBudgetUSD caps the user's agent chat spend per UTC day
	// (0 = unlimited; chat capability controls access).
	DailyAgentBudgetUSD float64 `gorm:"default:1"`
	// Locale is "en" | "es" | "pt-BR", or "" (the client decides).
	Locale      string
	Disabled    bool
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DefaultUserDailyAgentBudgetUSD is a new user's DailyAgentBudgetUSD.
const DefaultUserDailyAgentBudgetUSD = 1.00

// IsValidLocale reports whether l is an accepted User.Locale value: "" (the
// client decides) or a supported internal/i18n locale.
func IsValidLocale(l string) bool {
	if l == "" {
		return true
	}
	_, ok := i18n.Parse(l)
	return ok
}

// Session is a login session. Only the SHA-256 of the random token is
// stored; the raw token lives only in the gtb_session cookie.
type Session struct {
	ID         uint   `gorm:"primaryKey"`
	UserID     uint   `gorm:"index;not null"`
	TokenHash  string `gorm:"uniqueIndex;size:64;not null"`
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time `gorm:"index"`
	UserAgent  string    // truncated, informational
	IP         string    // truncated, informational
}

// UserUsage is a user's agent chat usage per UTC day (same pattern as
// AgentUsage; Day is UsageDay(t)).
type UserUsage struct {
	UserID  uint      `gorm:"primaryKey;autoIncrement:false"`
	Day     time.Time `gorm:"primaryKey;type:date"`
	Runs    int
	CostUSD float64
}
