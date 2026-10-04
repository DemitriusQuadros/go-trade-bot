package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/i18n"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func captureSystem(h *platformHarness, system *string) {
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		*system = req.System
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "ok"}, nil).Once()
}

// i18n-02 test 2: the language line follows the persona - from the request
// (chat) or from Settings.DefaultLocale (unattended runs).
func TestSystemPrompt_LanguageLine(t *testing.T) {
	const ptChat = "Always answer in Brazilian Portuguese (pt-BR). Keep code, tool arguments, symbols and identifiers unchanged."
	cases := []struct {
		name, trigger, reqLocale string
		defaultLoc               i18n.Locale
		want, notWant            string
	}{
		{"chat uses the user's locale", "chat_ui", "pt-BR", i18n.ES, ptChat, "Write report text"},
		{"chat without locale uses the default", "chat_ui", "", i18n.ES,
			"Always answer in Spanish (Latin America) (es). Keep code, tool arguments, symbols and identifiers unchanged.", "Write report text"},
		{"cron uses the default and adds the report line", "cron", "pt-BR", i18n.ES,
			"Always answer in Spanish (Latin America) (es). Keep code, tool arguments, symbols and identifiers unchanged. Write report text and notification messages in Spanish (Latin America).", "Brazilian"},
		{"event run", "event", "", i18n.PTBR, ptChat + " Write report text and notification messages in Brazilian Portuguese.", "Spanish"},
		{"no locale source means English", "chat_ui", "", "", "Always answer in English (en).", "Write report text"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newPlatformHarness(t)
			if c.defaultLoc != "" {
				h.uc.Locales = i18n.Static(c.defaultLoc)
			}
			var system string
			captureSystem(h, &system)
			_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: c.trigger, UserInput: "hi", Locale: c.reqLocale})
			require.NoError(t, err)
			assert.Contains(t, system, c.want)
			assert.NotContains(t, system, c.notWant)
			assert.Less(t, strings.Index(system, "You are the agent"), strings.Index(system, "Always answer in"), "right after the persona")
			assert.Less(t, strings.Index(system, "Always answer in"), strings.Index(system, "## Operating context"))
		})
	}
}

// i18n-02 test 3 (write side): write_report stores one snapshot per locale;
// RenderedHTML is the DefaultLocale one.
func TestWriteReport_StoresSnapshotPerLocale(t *testing.T) {
	h := newPlatformHarness(t)
	h.uc.Locales = i18n.Static(i18n.ES)
	blocks := []map[string]any{
		{"type": "summary", "data": map[string]any{"text": "Todo tranquilo."}},
		{"type": "kpi_grid", "data": map[string]any{"source": map[string]any{"kind": "backtest_run", "id": 7}, "metrics": []string{"sharpe", "win_rate"}}},
	}
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{writeReportCall(t, blocks, []uint{3})},
	}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "listo"}, nil).Once()

	_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "cron", UserInput: "report"})
	require.NoError(t, err)
	require.Len(t, h.platform.reports, 1)
	rep := h.platform.reports[0]

	var byLocale map[string]string
	require.NoError(t, json.Unmarshal(rep.RenderedHTMLByLocale, &byLocale))
	require.Len(t, byLocale, 3)
	assert.Contains(t, byLocale["en"], `<html lang="en">`)
	assert.Contains(t, byLocale["en"], "Key metrics")
	assert.Contains(t, byLocale["es"], `<html lang="es">`)
	assert.Contains(t, byLocale["es"], "Métricas clave")
	assert.Contains(t, byLocale["pt-BR"], "Principais métricas")
	for _, html := range byLocale {
		assert.Contains(t, html, "Todo tranquilo.")
		assert.Contains(t, html, ">1.50<")
	}
	assert.Equal(t, byLocale["es"], rep.RenderedHTML, "RenderedHTML is the DefaultLocale snapshot")
	assert.Equal(t, byLocale["pt-BR"], rep.HTMLForLocale("pt-BR"))
}
