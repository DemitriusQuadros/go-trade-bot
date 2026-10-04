package agentreport

import (
	"context"
	"strings"
	"testing"

	"go-trade-bot/internal/i18n"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// i18n-02 test 1: every EN report label exists in ES and PT-BR.
func TestMessages_CatalogComplete(t *testing.T) {
	assert.Empty(t, Messages.MissingKeys())
	for _, loc := range i18n.Supported {
		for k := range Messages[loc] {
			_, ok := Messages[i18n.EN][k]
			assert.True(t, ok, "%s has key %q that EN lacks", loc, k)
		}
	}
	for _, m := range AllowedMetrics {
		_, ok := Messages[i18n.EN]["metric."+m]
		assert.True(t, ok, "metric %q has no label", m)
	}
}

// refData is fakeData with structured source refs, so labels localize.
type refData struct {
	fakeData
	calls *int
}

func (d refData) BacktestRun(ctx context.Context, id uint) (Series, error) {
	*d.calls++
	s, err := d.fakeData.BacktestRun(ctx, id)
	s.Ref = &SeriesRef{Kind: SourceBacktestRun, ID: id, Symbol: "BTCUSDT", Start: base, End: base.AddDate(0, 1, 0)}
	return s, err
}

func (d refData) StrategyLive(ctx context.Context, strategyID uint, days int) (Series, error) {
	*d.calls++
	s, err := d.fakeData.StrategyLive(ctx, strategyID, days)
	s.Ref = &SeriesRef{Kind: SourceStrategyLive, ID: strategyID, Name: "RSI Momentum", Days: days}
	return s, err
}

func representativeBlocks(t *testing.T) []Block {
	bt := map[string]any{"kind": "backtest_run", "id": 7}
	return []Block{
		block(t, TypeSummary, map[string]any{"text": "El drawdown se mantuvo bajo control esta semana."}),
		block(t, TypeCallout, map[string]any{"severity": "warning", "title": "Revisar stop-loss", "text": "Dos stops se activaron por ruido."}),
		block(t, TypeKPIGrid, map[string]any{"source": bt, "metrics": AllowedMetrics}),
		block(t, TypeEquityChart, map[string]any{"source": bt}),
		block(t, TypeTradeTable, map[string]any{"source": bt, "limit": 1}),
		block(t, TypeKPIGrid, map[string]any{"source": map[string]any{"kind": "strategy_live", "strategy_id": 3, "days": 14}, "metrics": []string{"max_drawdown", "net_pnl"}}),
		block(t, TypeCodeDiff, map[string]any{"strategy_id": 3, "from_version_id": 1}),
		block(t, TypeRecommendation, map[string]any{"items": []map[string]any{{"title": "Ampliar el stop", "rationale": "Ruido intradiario.", "action": "Backtest con stop de 2%"}}}),
	}
}

// i18n-02 §3: one snapshot per locale, data resolved once, labels and
// <html lang> per locale, model text unchanged. Goldens per locale.
func TestRenderLocales_SnapshotsPerLocale(t *testing.T) {
	calls := 0
	r := NewHTMLRenderer(refData{calls: &calls})
	out, err := r.RenderLocales(context.Background(), meta(), representativeBlocks(t), nil)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, 2, calls, "each data reference is resolved once for all locales")

	for loc, html := range out {
		assert.True(t, strings.HasPrefix(html, "<!DOCTYPE html>\n<html lang=\""+string(loc)+"\">"), loc)
		assert.Contains(t, html, "El drawdown se mantuvo bajo control esta semana.", "model text is not translated")
		assert.Contains(t, html, ">1.23<", "same DB value in every locale")
		assertGolden(t, "representative."+string(loc), html)
	}
	assert.Contains(t, out[i18n.EN], "Key metrics")
	assert.Contains(t, out[i18n.ES], "Métricas clave")
	assert.Contains(t, out[i18n.ES], "Generado por el agente Risk Monitor · ejecución #99")
	assert.Contains(t, out[i18n.PTBR], "Principais métricas")
	assert.Contains(t, out[i18n.PTBR], "RSI Momentum (estratégia #3) · ao vivo · últimos 14 dias")
	assert.NotContains(t, out[i18n.PTBR], "Key metrics")

	// Render with a single locale matches that locale's snapshot.
	m := meta()
	m.Locale = i18n.PTBR
	single, err := NewHTMLRenderer(refData{calls: new(int)}).Render(context.Background(), m, representativeBlocks(t))
	require.NoError(t, err)
	assert.Equal(t, out[i18n.PTBR], single)
}

func TestRenderLocales_ErrorsSurface(t *testing.T) {
	_, err := NewHTMLRenderer(fakeData{}).RenderLocales(context.Background(), meta(), []Block{
		{Type: TypeKPIGrid, Data: []byte(`{"source":{"kind":"backtest_run","id":8},"metrics":["sharpe"]}`)},
	}, nil)
	require.Error(t, err)
	_, err = NewHTMLRenderer(fakeData{}).RenderLocales(context.Background(), meta(), nil, nil)
	require.Error(t, err)
}
