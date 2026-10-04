package report

import (
	"bytes"
	"testing"

	"go-trade-bot/internal/i18n"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// i18n-02 test 1: the backtest report catalog is complete.
func TestMessages_CatalogComplete(t *testing.T) {
	assert.Empty(t, Messages.MissingKeys())
}

func TestRender_Locales(t *testing.T) {
	in := BacktestReportInput{StrategyName: "S", Symbol: "BTCUSDT"}
	var en, es bytes.Buffer
	require.NoError(t, Render(&en, in, i18n.EN))
	require.NoError(t, Render(&es, in, i18n.ES))
	assert.Contains(t, en.String(), `<html lang="en">`)
	assert.Contains(t, en.String(), "Backtest Report: S")
	assert.Contains(t, en.String(), "No trades executed in this backtest run.")
	assert.Contains(t, es.String(), `<html lang="es">`)
	assert.Contains(t, es.String(), "No se ejecutaron operaciones en este backtest.")
	assert.Contains(t, es.String(), "Curva de capital plana")
	assert.NotContains(t, es.String(), "Win Rate")
}
