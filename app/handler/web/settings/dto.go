package settings

import "go-trade-bot/app/entities"

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
	}
}
