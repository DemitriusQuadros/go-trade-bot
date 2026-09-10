package settings

import (
	"strings"

	"go-trade-bot/app/entities"
)

// maskedPlaceholderPrefix is what MaskSecret prepends to a non-empty secret's
// last 4 characters (Spec backend-05 SS/blueprint SS3.4: masked-display only,
// never the real value). PutSettings uses this exact prefix to recognize a
// round-tripped placeholder on write (a field that "looks like" this format,
// or is empty/omitted, means "keep the existing stored value").
const maskedPlaceholderPrefix = "••••" // "••••"

// MaskSecret renders secret for display: "" for an unset secret, "" is never
// returned for a non-empty one - it's always "••••" + the last 4 characters,
// or just "••••" if the secret is 4 characters or shorter (so no meaningful
// fragment of a short secret ever leaks).
func MaskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return maskedPlaceholderPrefix
	}
	return maskedPlaceholderPrefix + secret[len(secret)-4:]
}

// IsMaskedPlaceholder reports whether v looks like something MaskSecret
// produced, rather than a genuinely new secret value the caller intends to
// set. Used by PutSettings/MergeInto to implement "omitted or masked means
// keep the existing value; anything else overwrites."
func IsMaskedPlaceholder(v string) bool {
	return strings.HasPrefix(v, maskedPlaceholderPrefix)
}

// DryRunSettingsDTO mirrors web/src/api/types.ts's DryRunSettings.
type DryRunSettingsDTO struct {
	SlippagePct float64 `json:"slippage_pct"`
	FeePct      float64 `json:"fee_pct"`
	FillDelayMs int     `json:"fill_delay_ms"`
}

// SettingsResponseDTO mirrors web/src/api/types.ts's PlatformSettings.
// Secrets are always masked - GetSettings must never encode the raw
// entities.Settings struct directly (Fix 1).
type SettingsResponseDTO struct {
	BrokerApiKey           string            `json:"broker_api_key"`
	BrokerApiSecret        string            `json:"broker_api_secret"`
	BrokerTestnetApiKey    string            `json:"broker_testnet_api_key"`
	BrokerTestnetApiSecret string            `json:"broker_testnet_api_secret"`
	Mode                   string            `json:"mode"`
	Testnet                bool              `json:"testnet"`
	WebhookURL             string            `json:"webhook_url"`
	DryRun                 DryRunSettingsDTO `json:"dry_run"`
	PrometheusURL          string            `json:"prometheus_url"`
	GrafanaURL             string            `json:"grafana_url"`
}

func ToSettingsResponse(s entities.Settings) SettingsResponseDTO {
	return SettingsResponseDTO{
		BrokerApiKey:           MaskSecret(s.BrokerApiKey),
		BrokerApiSecret:        MaskSecret(s.BrokerApiSecret),
		BrokerTestnetApiKey:    MaskSecret(s.BrokerTestnetApiKey),
		BrokerTestnetApiSecret: MaskSecret(s.BrokerTestnetApiSecret),
		Mode:                   s.Mode,
		Testnet:                s.Testnet,
		WebhookURL:             s.WebhookURL,
		DryRun: DryRunSettingsDTO{
			SlippagePct: s.DryRunSlippagePct,
			FeePct:      s.DryRunFeePct,
			FillDelayMs: s.DryRunFillDelayMs,
		},
		PrometheusURL: s.PrometheusURL,
		GrafanaURL:    s.GrafanaURL,
	}
}

// PlatformSettingsUpdateResponseDTO mirrors
// web/src/api/types.ts's PlatformSettingsUpdateResponse.
type PlatformSettingsUpdateResponseDTO struct {
	Settings SettingsResponseDTO `json:"settings"`
	Applied  bool                `json:"applied"`
}

// DrainTimeoutErrorBody mirrors web/src/api/types.ts's DrainTimeoutErrorBody.
type DrainTimeoutErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
