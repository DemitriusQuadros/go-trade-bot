package settings

import (
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
)

func TestMergeInto_NewSecret_Overwrites(t *testing.T) {
	existing := entities.Settings{ID: 1, BrokerApiKey: "oldkey"}
	req := PlatformSettingsUpdateRequestDTO{BrokerApiKey: "brand-new-key"}

	merged := req.MergeInto(existing)
	assert.Equal(t, "brand-new-key", merged.BrokerApiKey)
}

func TestMergeInto_EmptySecret_KeepsExisting(t *testing.T) {
	existing := entities.Settings{ID: 1, BrokerApiKey: "oldkey"}
	req := PlatformSettingsUpdateRequestDTO{BrokerApiKey: ""}

	merged := req.MergeInto(existing)
	assert.Equal(t, "oldkey", merged.BrokerApiKey)
}

func TestMergeInto_MaskedPlaceholderSecret_KeepsExisting(t *testing.T) {
	existing := entities.Settings{ID: 1, BrokerApiSecret: "oldsecret5678"}
	// Simulate the frontend round-tripping GetSettings' masked value back on
	// PUT without the user having actually changed it.
	masked := MaskSecret(existing.BrokerApiSecret)
	req := PlatformSettingsUpdateRequestDTO{BrokerApiSecret: masked}

	merged := req.MergeInto(existing)
	assert.Equal(t, "oldsecret5678", merged.BrokerApiSecret)
}

func TestMergeInto_NonSecretFieldsAlwaysTakeRequestValue(t *testing.T) {
	existing := entities.Settings{ID: 1, Mode: "dryrun", Testnet: true, WebhookURL: "https://old"}
	req := PlatformSettingsUpdateRequestDTO{
		Mode:          "paper",
		Testnet:       false,
		WebhookURL:    "https://new",
		PrometheusURL: "http://prom",
		GrafanaURL:    "http://grafana",
		AsynqmonURL:   "http://localhost:9191/tasks/monitoring",
		DryRun:        DryRunSettingsDTO{SlippagePct: 1, FeePct: 2, FillDelayMs: 3},
	}

	merged := req.MergeInto(existing)
	assert.Equal(t, "paper", merged.Mode)
	assert.False(t, merged.Testnet)
	assert.Equal(t, "https://new", merged.WebhookURL)
	assert.Equal(t, "http://prom", merged.PrometheusURL)
	assert.Equal(t, "http://grafana", merged.GrafanaURL)
	assert.Equal(t, "http://localhost:9191/tasks/monitoring", merged.AsynqmonURL)
	assert.Equal(t, 1.0, merged.DryRunSlippagePct)
	assert.Equal(t, 2.0, merged.DryRunFeePct)
	assert.Equal(t, 3, merged.DryRunFillDelayMs)
}

func TestMergeInto_PreservesIDAndOutOfScopeFields(t *testing.T) {
	existing := entities.Settings{ID: 42, APIBaseURL: "http://localhost:8080", APIToken: "tok", AllowInsecureNoAuth: true}
	req := PlatformSettingsUpdateRequestDTO{}

	merged := req.MergeInto(existing)
	assert.Equal(t, uint(42), merged.ID)
	assert.Equal(t, "http://localhost:8080", merged.APIBaseURL)
	assert.Equal(t, "tok", merged.APIToken)
	assert.True(t, merged.AllowInsecureNoAuth)
}
