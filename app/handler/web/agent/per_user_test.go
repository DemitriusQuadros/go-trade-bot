package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/agent"
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/customerror"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Auth-01 §6: per-user chat budget, usage accounting, mine=true and the
// per-user history replay.

type fakeBudget struct {
	exceeded bool
	recorded []float64
	who      []uint
}

func (f *fakeBudget) CheckChatBudget(_ context.Context, p authz.Principal) error {
	if f.exceeded {
		return customerror.NewCoded(http.StatusConflict, "user_budget_exceeded",
			"your daily agent budget ($1.00) is used up; it resets at 00:00 UTC")
	}
	return nil
}

func (f *fakeBudget) RecordChatUsage(_ context.Context, p authz.Principal, cost float64) error {
	f.recorded = append(f.recorded, cost)
	f.who = append(f.who, p.UserID)
	return nil
}

// ctxPersonas records whether the run's context still carried a principal.
type ctxPersonas struct {
	fakePersonas
	runHadPrincipal bool
}

func (c *ctxPersonas) Run(ctx context.Context, req agentusecase.RunRequest) (entities.AgentRun, error) {
	_, c.runHadPrincipal = authz.FromContext(ctx)
	return c.fakePersonas.Run(ctx, req)
}

var friendP = authz.Principal{UserID: 42, Username: "ana", Role: authz.RoleFriend, Caps: authz.RolePreset(authz.RoleFriend)}
var adminP = authz.Principal{UserID: 1, Username: "root", Role: authz.RoleAdmin, Caps: authz.RolePreset(authz.RoleAdmin)}

func userRouter(repo *mockRepository, p handler.PersonaChat, b handler.UserBudget, who *authz.Principal) *mux.Router {
	h := handler.NewAgentHandlerWithUsers(&mockUseCase{}, repo, p, b)
	router := mux.NewRouter()
	for _, cfg := range h.Handlers() {
		action := cfg.Action
		router.HandleFunc(cfg.Pattern, func(w http.ResponseWriter, r *http.Request) {
			if who != nil {
				r = r.WithContext(authz.WithUser(r.Context(), *who))
			}
			action(w, r)
		}).Methods(cfg.Method)
	}
	return router
}

func TestChat_UserBudgetExceededIs409(t *testing.T) {
	p := &fakePersonas{agents: map[uint]entities.Agent{1: {ID: 1, Name: "Copilot"}}}
	b := &fakeBudget{exceeded: true}
	router := userRouter(&mockRepository{}, p, b, &friendP)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewBufferString(`{"input":"hi"}`)))
	require.Equal(t, http.StatusConflict, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "user_budget_exceeded", body["error"])
	assert.Equal(t, "your daily agent budget ($1.00) is used up; it resets at 00:00 UTC", body["message"])
	assert.False(t, p.ran)
}

func TestChat_UsageAccountingAndUserIDAndSystemTools(t *testing.T) {
	p := &ctxPersonas{fakePersonas: fakePersonas{agents: map[uint]entities.Agent{1: {ID: 1, Name: "Copilot"}}}}
	b := &fakeBudget{}
	router := userRouter(&mockRepository{}, p, b, &friendP)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewBufferString(`{"input":"hi"}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, p.gotReq.UserID)
	assert.Equal(t, uint(42), *p.gotReq.UserID, "AgentRun.UserID")
	assert.Equal(t, []float64{0.001}, b.recorded, "run cost added to UserUsage")
	assert.Equal(t, []uint{42}, b.who)
	assert.False(t, p.runHadPrincipal, "agent tools run as system")
}

func TestListRuns_Mine(t *testing.T) {
	repo := &mockRepository{}
	router := userRouter(repo, nil, nil, &friendP)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agent/runs?mine=true", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, repo.gotFilter.Owner)
	assert.Equal(t, uint(42), repo.gotFilter.Owner.UserID)
	assert.False(t, repo.gotFilter.Owner.IncludeUnowned)

	// Admins also get the service-token/legacy (NULL) rows.
	repo = &mockRepository{}
	router = userRouter(repo, nil, nil, &adminP)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agent/runs?mine=true", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, repo.gotFilter.Owner.IncludeUnowned)

	// Without mine, everyone's runs (Activity page).
	repo = &mockRepository{}
	router = userRouter(repo, nil, nil, &friendP)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agent/runs", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, repo.gotFilter.Owner)
}

func TestChat_HistoryReplayIsPerUser(t *testing.T) {
	repo := &mockRepository{runs: []entities.AgentRun{{ID: 5, Status: entities.AgentRunOK, InputSummary: "earlier", ResponseText: "ok"}}}
	p := &fakePersonas{agents: map[uint]entities.Agent{1: {ID: 1, Name: "Copilot"}}}
	router := userRouter(repo, p, &fakeBudget{}, &friendP)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewBufferString(`{"input":"hi","strategy_id":3}`)))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, repo.gotFilter)
	assert.Equal(t, []string{"chat_ui"}, repo.gotFilter.Triggers)
	require.NotNil(t, repo.gotFilter.Owner)
	assert.Equal(t, uint(42), repo.gotFilter.Owner.UserID)
	require.NotNil(t, repo.gotFilter.StrategyID)
	assert.Equal(t, uint(3), *repo.gotFilter.StrategyID)
	require.Len(t, p.gotReq.History, 1)
	assert.Equal(t, "earlier", p.gotReq.History[0].Input)
}
