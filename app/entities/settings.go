package entities

import (
	"time"

	"go-trade-bot/internal/configuration"
)

type Settings struct {
	ID                     uint   `gorm:"primaryKey"`
	Mode                   string // execution-mode ceiling
	ConfirmLive            bool
	Testnet                bool
	WebhookURL             string
	APIBaseURL             string
	APIToken               string
	AllowInsecureNoAuth    bool
	DryRunSlippagePct      float64
	DryRunFeePct           float64
	DryRunFillDelayMs      int
	BrokerApiKey           string
	BrokerApiSecret        string
	BrokerTestnetApiKey    string
	BrokerTestnetApiSecret string
	// PrometheusURL/GrafanaURL are pure display data (PRD SS4 "Monitoring
	// links") - never read by any Go process's memory, only ever persisted
	// and returned as-is by GET /settings for the frontend to render as
	// external links (Spec backend-05 SS1's risk tier table).
	PrometheusURL string
	GrafanaURL    string
	AsynqmonURL   string
	// AgentsAsynqmonURL is cmd/agent's Asynqmon UI (fix-02 B5, default
	// empty; the UI suggests http://localhost:9194/tasks/monitoring).
	// Display-only, like AsynqmonURL.
	AgentsAsynqmonURL string
	// AgentsPaused is the global agents kill switch (agents-platform A-01):
	// when true no agent run (cron, manual or chat) starts, and in-flight
	// runs halt before their next model call. Never read by any trading
	// path - it only gates app/usecase/agent.
	AgentsPaused bool
	// DefaultLocale ("en" | "es" | "pt-BR", i18n-02 §1) is the language of
	// shared, unattended output: cron/event/market/chain agent runs, their
	// reports and webhook notifications. "" reads as "en". Written by the
	// admin-only PUT /settings.
	DefaultLocale string `gorm:"size:8;default:en"`
	// BacktestTimeoutMinutes is how long one asynchronous backtest may run
	// before the worker cancels it and records it failed (B-02). 0 reads as
	// DefaultBacktestTimeoutMinutes. Edited on the Settings page (admin) and
	// read by cmd/worker at the start of each run, so a change applies to
	// the next backtest without a restart.
	BacktestTimeoutMinutes int `gorm:"default:120"`
	UpdatedAt              time.Time
}

const (
	DefaultBacktestTimeoutMinutes = 120
	MinBacktestTimeoutMinutes     = 5
	MaxBacktestTimeoutMinutes     = 24 * 60
)

// BacktestTimeout is the effective per-run limit: the stored value, or the
// default when unset (0) or out of range.
func (s Settings) BacktestTimeout() time.Duration {
	m := s.BacktestTimeoutMinutes
	if m < MinBacktestTimeoutMinutes || m > MaxBacktestTimeoutMinutes {
		m = DefaultBacktestTimeoutMinutes
	}
	return time.Duration(m) * time.Minute
}

// ToConfiguration builds a *configuration.Configuration carrying just the
// fields exchange.NewExchangeClientFromConfig / notifier.NewWebhookNotifier
// consult, so a Settings row can be fed straight into
// SwappableExchangeClient.SwapFromConfig / SwappableNotifier.Swap (Spec
// backend-05 SS3) without those packages depending on entities.Settings
// directly.
func (s Settings) ToConfiguration() *configuration.Configuration {
	return &configuration.Configuration{
		Mode:        s.Mode,
		ConfirmLive: s.ConfirmLive,
		Testnet:     s.Testnet,
		WebhookURL:  s.WebhookURL,
		Broker: configuration.Broker{
			ApiKey:           s.BrokerApiKey,
			ApiSecret:        s.BrokerApiSecret,
			TestnetApiKey:    s.BrokerTestnetApiKey,
			TestnetApiSecret: s.BrokerTestnetApiSecret,
		},
		DryRun: configuration.DryRunConfig{
			SlippagePct: s.DryRunSlippagePct,
			FeePct:      s.DryRunFeePct,
			FillDelay:   time.Duration(s.DryRunFillDelayMs) * time.Millisecond,
		},
	}
}
