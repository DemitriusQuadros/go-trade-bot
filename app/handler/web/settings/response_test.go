package settings

import (
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
)

func TestMaskSecret(t *testing.T) {
	assert.Equal(t, "", MaskSecret(""))
	assert.Equal(t, "••••", MaskSecret("abcd"))
	assert.Equal(t, "••••", MaskSecret("ab"))
	assert.Equal(t, "••••7890", MaskSecret("abcdef1234567890"))
}

func TestIsMaskedPlaceholder(t *testing.T) {
	assert.True(t, IsMaskedPlaceholder("••••7890"))
	assert.True(t, IsMaskedPlaceholder("••••"))
	assert.False(t, IsMaskedPlaceholder(""))
	assert.False(t, IsMaskedPlaceholder("a-real-new-secret"))
}

func TestToSettingsResponse_NeverLeaksRawSecrets(t *testing.T) {
	s := entities.Settings{
		BrokerApiKey:           "supersecretkey1234",
		BrokerApiSecret:        "supersecretsecret5678",
		BrokerTestnetApiKey:    "",
		BrokerTestnetApiSecret: "shrt",
		Mode:                   "paper",
		Testnet:                true,
		WebhookURL:             "https://hooks.example.com",
		DryRunSlippagePct:      0.1,
		DryRunFeePct:           0.2,
		DryRunFillDelayMs:      500,
		PrometheusURL:          "http://localhost:9090",
		GrafanaURL:             "http://localhost:3000",
		AsynqmonURL:            "http://localhost:9191/tasks/monitoring",
	}

	resp := ToSettingsResponse(s)

	assert.NotContains(t, resp.BrokerApiKey, "supersecretkey1234")
	assert.Equal(t, "••••1234", resp.BrokerApiKey)
	assert.Equal(t, "••••5678", resp.BrokerApiSecret)
	assert.Equal(t, "", resp.BrokerTestnetApiKey)
	assert.Equal(t, "••••", resp.BrokerTestnetApiSecret)
	assert.Equal(t, "paper", resp.Mode)
	assert.True(t, resp.Testnet)
	assert.Equal(t, "https://hooks.example.com", resp.WebhookURL)
	assert.Equal(t, DryRunSettingsDTO{SlippagePct: 0.1, FeePct: 0.2, FillDelayMs: 500}, resp.DryRun)
	assert.Equal(t, "http://localhost:9090", resp.PrometheusURL)
	assert.Equal(t, "http://localhost:3000", resp.GrafanaURL)
	assert.Equal(t, "http://localhost:9191/tasks/monitoring", resp.AsynqmonURL)
}
