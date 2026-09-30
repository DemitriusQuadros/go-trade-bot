package settings

import (
	"strings"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/i18n"
)

// PlatformSettingsUpdateRequestDTO mirrors
// web/src/api/types.ts's PlatformSettingsUpdateRequest. Secret fields are
// optional: omitted, empty, or a masked-placeholder-looking value all mean
// "keep the existing stored value" (Fix 1) - only a genuinely new-looking
// value overwrites.
type PlatformSettingsUpdateRequestDTO struct {
	BrokerApiKey           string            `json:"broker_api_key"`
	BrokerApiSecret        string            `json:"broker_api_secret"`
	BrokerTestnetApiKey    string            `json:"broker_testnet_api_key"`
	BrokerTestnetApiSecret string            `json:"broker_testnet_api_secret"`
	Mode                   string            `json:"mode"`
	ConfirmLive            bool              `json:"confirm_live"`
	Testnet                bool              `json:"testnet"`
	WebhookURL             string            `json:"webhook_url"`
	DryRun                 DryRunSettingsDTO `json:"dry_run"`
	PrometheusURL          string            `json:"prometheus_url"`
	GrafanaURL             string            `json:"grafana_url"`
	AsynqmonURL            string            `json:"asynqmon_url"`
	AgentsAsynqmonURL      string            `json:"agents_asynqmon_url"`
	// DefaultLocale (i18n-02 §1): "en" | "es" | "pt-BR". Omitted/empty keeps
	// the stored value (older clients don't send it); anything else is a
	// 400 invalid_locale (checked by the handler before MergeInto).
	DefaultLocale string `json:"default_locale"`
}

// resolveSecret implements the "omitted/empty/masked-placeholder means keep
// existing" rule for a single secret field.
func resolveSecret(requested, existing string) string {
	if requested == "" || IsMaskedPlaceholder(requested) {
		return existing
	}
	return requested
}

// MergeInto produces the final entities.Settings PUT /settings should apply:
// non-secret fields always take the request's value (they're not optional in
// the DTO), secret fields fall back to existing per resolveSecret.
func (r PlatformSettingsUpdateRequestDTO) MergeInto(existing entities.Settings) entities.Settings {
	return entities.Settings{
		ID:                     existing.ID,
		Mode:                   r.Mode,
		Testnet:                r.Testnet,
		WebhookURL:             r.WebhookURL,
		APIBaseURL:             existing.APIBaseURL,
		APIToken:               existing.APIToken,
		AllowInsecureNoAuth:    existing.AllowInsecureNoAuth,
		DryRunSlippagePct:      r.DryRun.SlippagePct,
		DryRunFeePct:           r.DryRun.FeePct,
		DryRunFillDelayMs:      r.DryRun.FillDelayMs,
		BrokerApiKey:           resolveSecret(r.BrokerApiKey, existing.BrokerApiKey),
		BrokerApiSecret:        resolveSecret(r.BrokerApiSecret, existing.BrokerApiSecret),
		BrokerTestnetApiKey:    resolveSecret(r.BrokerTestnetApiKey, existing.BrokerTestnetApiKey),
		BrokerTestnetApiSecret: resolveSecret(r.BrokerTestnetApiSecret, existing.BrokerTestnetApiSecret),
		PrometheusURL:          r.PrometheusURL,
		GrafanaURL:             r.GrafanaURL,
		AsynqmonURL:            r.AsynqmonURL,
		AgentsAsynqmonURL:      r.AgentsAsynqmonURL,
		// The agents kill switch is NOT settable through PUT /settings (any
		// agents_paused in the body is ignored): it is owned by
		// PUT /agents/kill-switch, so a normal settings save can never flip
		// it (and never has to assert confirm_live to do so).
		AgentsPaused:  existing.AgentsPaused,
		DefaultLocale: r.mergedDefaultLocale(existing.DefaultLocale),
	}
}

// mergedDefaultLocale is the DefaultLocale PUT /settings stores: the
// request's (canonicalized) value, or the existing one when omitted/invalid
// (the handler rejects invalid values before this runs).
func (r PlatformSettingsUpdateRequestDTO) mergedDefaultLocale(existing string) string {
	if loc, ok := i18n.Parse(r.DefaultLocale); ok {
		return string(loc)
	}
	return existing
}

// ValidDefaultLocale reports whether the request's default_locale is
// acceptable: omitted/empty or a supported locale.
func (r PlatformSettingsUpdateRequestDTO) ValidDefaultLocale() bool {
	if strings.TrimSpace(r.DefaultLocale) == "" {
		return true
	}
	_, ok := i18n.Parse(r.DefaultLocale)
	return ok
}
