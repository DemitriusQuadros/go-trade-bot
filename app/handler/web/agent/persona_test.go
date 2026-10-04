package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/agent"
	agentusecase "go-trade-bot/app/usecase/agent"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePersonas struct {
	agents   map[uint]entities.Agent
	guardErr error
	gotReq   agentusecase.RunRequest
	ran      bool
}

func (f *fakePersonas) ResolveAgent(_ context.Context, id *uint) (entities.Agent, error) {
	key := uint(1)
	if id != nil {
		key = *id
	}
	a, ok := f.agents[key]
	if !ok {
		return a, errors.New("agent not found")
	}
	return a, nil
}
func (f *fakePersonas) CheckGuard(context.Context, entities.Agent) error { return f.guardErr }
func (f *fakePersonas) Run(_ context.Context, req agentusecase.RunRequest) (entities.AgentRun, error) {
	f.ran, f.gotReq = true, req
	id := req.Agent.ID
	return entities.AgentRun{ID: 10, AgentID: &id, Trigger: req.Trigger, Status: entities.AgentRunOK, ResponseText: "hi", InputTokens: 12, OutputTokens: 3, CostUSD: 0.001}, nil
}
func (f *fakePersonas) AgentNames(context.Context) map[uint]string {
	out := map[uint]string{}
	for id, a := range f.agents {
		out[id] = a.Name
	}
	return out
}

func personaRouter(p handler.PersonaChat) *mux.Router {
	h := handler.NewAgentHandlerWithPersonas(&mockUseCase{}, &mockRepository{}, p)
	router := mux.NewRouter()
	for _, cfg := range h.Handlers() {
		router.HandleFunc(cfg.Pattern, cfg.Action).Methods(cfg.Method)
	}
	return router
}

func post(router *mux.Router, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewBufferString(body)))
	return rec
}

// A-02 §5 Chat: agent_id selects the persona; the response carries
// agent_id/agent_name and usage fields.
func TestSendMessage_WithAgentID(t *testing.T) {
	p := &fakePersonas{agents: map[uint]entities.Agent{1: {ID: 1, Name: "Copilot"}, 7: {ID: 7, Name: "Risk Monitor"}}}
	rec := post(personaRouter(p), `{"input":"hello","agent_id":7,"strategy_id":3}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, uint(7), p.gotReq.Agent.ID)
	assert.Equal(t, "chat_ui", p.gotReq.Trigger)
	assert.Equal(t, "hello", p.gotReq.DisplayInput)
	require.NotNil(t, p.gotReq.StrategyID)

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, float64(7), got["agent_id"])
	assert.Equal(t, "Risk Monitor", got["agent_name"])
	assert.Equal(t, float64(12), got["input_tokens"])
	assert.Equal(t, float64(3), got["output_tokens"])
	assert.Equal(t, 0.001, got["cost_usd"])
}

func TestSendMessage_DefaultAgentWhenOmitted(t *testing.T) {
	p := &fakePersonas{agents: map[uint]entities.Agent{1: {ID: 1, Name: "Copilot"}}}
	rec := post(personaRouter(p), `{"input":"hello"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Copilot", p.gotReq.Agent.Name)
}

// A paused/halted agent is a 409 with a clear message and no run.
func TestSendMessage_HaltedAgentIs409(t *testing.T) {
	p := &fakePersonas{agents: map[uint]entities.Agent{1: {ID: 1, Name: "Copilot"}}, guardErr: &agentusecase.HaltError{Reason: "agents are paused by the global kill switch"}}
	rec := post(personaRouter(p), `{"input":"hello"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "agent_unavailable", body["error"])
	assert.Contains(t, body["message"], "kill switch")
	assert.False(t, p.ran)
}

func TestSendMessage_UnknownAgent(t *testing.T) {
	p := &fakePersonas{agents: map[uint]entities.Agent{}}
	rec := post(personaRouter(p), `{"input":"hello","agent_id":99}`)
	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.False(t, p.ran)
}
