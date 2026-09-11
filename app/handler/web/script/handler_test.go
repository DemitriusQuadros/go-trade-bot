package script_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	handler "go-trade-bot/app/handler/web/script"
	scriptuc "go-trade-bot/app/usecase/script"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeUseCase struct {
	evalResp scriptuc.EvalResponse
	evalErr  error
	frResp   scriptuc.FastRerunResponse
	frErr    error
}

func (f *fakeUseCase) Eval(context.Context, scriptuc.EvalRequest) (scriptuc.EvalResponse, error) {
	return f.evalResp, f.evalErr
}
func (f *fakeUseCase) FastRerun(context.Context, scriptuc.FastRerunRequest) (scriptuc.FastRerunResponse, error) {
	return f.frResp, f.frErr
}

func post(h *handler.ScriptHandler, action func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/script/x", strings.NewReader(body))
	rec := httptest.NewRecorder()
	action(rec, req)
	return rec
}

// AC1 (via handler): a valid repl returns 200 with the result.
func TestRepl_Success(t *testing.T) {
	h := handler.NewScriptHandler(&fakeUseCase{evalResp: scriptuc.EvalResponse{Result: 4.0, Trace: nil}})
	rec := post(h, h.Repl, `{"source":"return 2+2","symbol":"BTCUSDT"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 4.0, resp["result"])
}

// AC3 (via handler): a script-level error is 200 with .error populated.
func TestRepl_ScriptError200(t *testing.T) {
	h := handler.NewScriptHandler(&fakeUseCase{evalResp: scriptuc.EvalResponse{Error: "syntax error"}})
	rec := post(h, h.Repl, `{"source":"return (","symbol":"BTCUSDT"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "syntax error")
}

// AC4: a missing symbol is a 400.
func TestRepl_MissingSymbol400(t *testing.T) {
	h := handler.NewScriptHandler(&fakeUseCase{})
	rec := post(h, h.Repl, `{"source":"return 1"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRepl_MalformedJSON400(t *testing.T) {
	h := handler.NewScriptHandler(&fakeUseCase{})
	rec := post(h, h.Repl, `{not json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRepl_InfraError500(t *testing.T) {
	h := handler.NewScriptHandler(&fakeUseCase{evalErr: errors.New("exchange unreachable")})
	rec := post(h, h.Repl, `{"source":"return 1","symbol":"BTCUSDT"}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestFastRerun_MissingSymbol400(t *testing.T) {
	h := handler.NewScriptHandler(&fakeUseCase{})
	rec := post(h, h.FastRerun, `{"strategy_id":1,"source":"x"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// A strategy-not-found lookup maps to 404.
func TestFastRerun_StrategyNotFound404(t *testing.T) {
	h := handler.NewScriptHandler(&fakeUseCase{frErr: errors.New("strategy 9 not found: record not found")})
	rec := post(h, h.FastRerun, `{"strategy_id":9,"source":"x","symbol":"BTCUSDT"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestFastRerun_ScriptError200(t *testing.T) {
	h := handler.NewScriptHandler(&fakeUseCase{frResp: scriptuc.FastRerunResponse{Error: "parse error"}})
	rec := post(h, h.FastRerun, `{"strategy_id":1,"source":"bad","symbol":"BTCUSDT"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "parse error")
}
