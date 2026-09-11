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
	UpdatedAt     time.Time
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
