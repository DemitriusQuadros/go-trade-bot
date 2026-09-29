package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/agent"
	agentrepo "go-trade-bot/app/repository/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Phase D-01 §1/§3 handler tests. The acceptance-criteria tests run against
// the real GORM repository on in-memory SQLite, so filtering and paging are
// exercised end to end; the parsing tests use mockRepository.

func newRunsDB(t *testing.T) (*gorm.DB, *agentrepo.GormRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.AgentRun{}))
	return db, agentrepo.NewGormRepository(db)
}

func seedRun(t *testing.T, repo *agentrepo.GormRepository, trigger string, strategyID *uint) entities.AgentRun {
	t.Helper()
	run, err := repo.CreateRun(context.Background(), entities.AgentRun{
		Trigger: trigger, Status: entities.AgentRunOK, StrategyID: strategyID, StartedAt: time.Now(),
	})
	require.NoError(t, err)
	return run
}

func getRuns(t *testing.T, repo handler.Repository, query string) (int, []handler.AgentRunResponse, []byte) {
	t.Helper()
	router := newRouter(&mockUseCase{}, repo)
	req := httptest.NewRequest(http.MethodGet, "/agent/runs"+query, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	var out []handler.AgentRunResponse
	if rr.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	}
	return rr.Code, out, rr.Body.Bytes()
}

func ids(runs []handler.AgentRunResponse) []uint {
	out := make([]uint, len(runs))
	for i, r := range runs {
		out[i] = r.ID
	}
	return out
}

func uintPtr(v uint) *uint { return &v }

// AC1: 3 chat runs + 2 cron runs for strategy 5; trigger=chat_ui returns
// exactly the 3 chat runs, newest first.
func TestListRuns_StrategyAndTriggerFilter(t *testing.T) {
	_, repo := newRunsDB(t)
	s5 := uintPtr(5)
	c1 := seedRun(t, repo, "chat_ui", s5)
	seedRun(t, repo, "cron", s5)
	c2 := seedRun(t, repo, "chat_ui", s5)
	seedRun(t, repo, "cron", s5)
	c3 := seedRun(t, repo, "chat_ui", s5)
	seedRun(t, repo, "chat_ui", uintPtr(6))

	code, runs, _ := getRuns(t, repo, "?strategy_id=5&trigger=chat_ui")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{c3.ID, c2.ID, c1.ID}, ids(runs))
}

// trigger=chat_ui,manual returns both kinds and nothing else.
func TestListRuns_MultipleTriggers(t *testing.T) {
	_, repo := newRunsDB(t)
	a := seedRun(t, repo, "chat_ui", nil)
	seedRun(t, repo, "cron", nil)
	b := seedRun(t, repo, "manual", nil)
	seedRun(t, repo, "mcp_tool", nil)

	code, runs, _ := getRuns(t, repo, "?trigger=chat_ui,manual")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{b.ID, a.ID}, ids(runs))
}

// AC2: 25 matching runs, limit=10 -> pages of 10, 10, 5 covering every run
// exactly once.
func TestListRuns_BeforeIDPagination(t *testing.T) {
	_, repo := newRunsDB(t)
	want := map[uint]bool{}
	for i := 0; i < 25; i++ {
		want[seedRun(t, repo, "chat_ui", nil).ID] = true
		seedRun(t, repo, "cron", nil) // interleaved non-matching runs
	}

	seen := map[uint]int{}
	var sizes []int
	query := "?trigger=chat_ui&limit=10"
	for page := 0; page < 5; page++ {
		code, runs, _ := getRuns(t, repo, query)
		require.Equal(t, http.StatusOK, code)
		if len(runs) == 0 {
			break
		}
		sizes = append(sizes, len(runs))
		for i, r := range runs {
			seen[r.ID]++
			if i > 0 {
				assert.Less(t, r.ID, runs[i-1].ID, "newest first")
			}
		}
		query = "?trigger=chat_ui&limit=10&before_id=" + uintToString(runs[len(runs)-1].ID)
	}
	assert.Equal(t, []int{10, 10, 5}, sizes)
	require.Len(t, seen, 25)
	for id, n := range seen {
		assert.True(t, want[id])
		assert.Equal(t, 1, n)
	}
}

// AC3: strategy_id=none returns the strategy-less chat run; strategy_id=5
// does not.
func TestListRuns_NoStrategy(t *testing.T) {
	_, repo := newRunsDB(t)
	free := seedRun(t, repo, "chat_ui", nil)
	scoped := seedRun(t, repo, "chat_ui", uintPtr(5))

	code, runs, _ := getRuns(t, repo, "?strategy_id=none&trigger=chat_ui")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{free.ID}, ids(runs))

	code, runs, _ = getRuns(t, repo, "?strategy_id=5")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{scoped.ID}, ids(runs))
}

func TestListRuns_AgentIDFilter(t *testing.T) {
	db, repo := newRunsDB(t)
	mine := seedRun(t, repo, "cron", nil)
	require.NoError(t, db.Model(&entities.AgentRun{}).Where("id = ?", mine.ID).Update("agent_id", 3).Error)
	seedRun(t, repo, "cron", nil)

	code, runs, _ := getRuns(t, repo, "?agent_id=3")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{mine.ID}, ids(runs))
}

func TestListRuns_LimitCappedAt100(t *testing.T) {
	repo := &mockRepository{}
	code, _, _ := getRuns(t, repo, "?limit=500")
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, repo.gotFilter)
	assert.Equal(t, 100, repo.gotFilter.Limit)

	code, _, _ = getRuns(t, repo, "?limit=100")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 100, repo.gotFilter.Limit)
}

func TestListRuns_ParsesEveryParam(t *testing.T) {
	repo := &mockRepository{}
	code, _, _ := getRuns(t, repo, "?limit=10&strategy_id=none&trigger=chat_ui,%20manual&agent_id=4&before_id=99")
	require.Equal(t, http.StatusOK, code)
	f := repo.gotFilter
	require.NotNil(t, f)
	assert.Equal(t, 10, f.Limit)
	assert.True(t, f.NoStrategy)
	assert.Nil(t, f.StrategyID)
	assert.Equal(t, []string{"chat_ui", "manual"}, f.Triggers)
	require.NotNil(t, f.AgentID)
	assert.Equal(t, uint(4), *f.AgentID)
	require.NotNil(t, f.BeforeID)
	assert.Equal(t, uint(99), *f.BeforeID)
}

func TestListRuns_InvalidTriggerIs400(t *testing.T) {
	repo := &mockRepository{}
	code, _, body := getRuns(t, repo, "?trigger=chat_ui,bogus")
	require.Equal(t, http.StatusBadRequest, code)
	var e map[string]string
	require.NoError(t, json.Unmarshal(body, &e))
	assert.Equal(t, "invalid_trigger", e["error"])
	assert.Contains(t, e["message"], "bogus")
	assert.Nil(t, repo.gotFilter, "repository must not be queried")
}

func TestListRuns_InvalidNumericParamsAre400(t *testing.T) {
	for _, q := range []string{"?agent_id=x", "?before_id=-1", "?strategy_id=abc", "?limit=0"} {
		code, _, _ := getRuns(t, &mockRepository{}, q)
		assert.Equal(t, http.StatusBadRequest, code, q)
	}
}

// §3 + AC4/AC5/AC6: refs on list and detail responses, clean input_summary.
func TestRunResponses_CarryRefsAndCleanInput(t *testing.T) {
	strategyID := uint(5)
	toolCalls := `[` +
		`{"tool":"create_challenger","args":{"strategy_id":5},"result":"{\"challenger_strategy_id\":12,\"champion_strategy_id\":5,\"created\":true}","timestamp":"2026-09-01T00:00:00Z"},` +
		`{"tool":"run_backtest","args":{"strategy_id":12},"result":"backtest_id=44 strategy_id=12\nsymbol=BTCUSDT\n","timestamp":"2026-09-01T00:00:01Z"},` +
		`{"tool":"run_backtest","args":{"strategy_id":12},"error":"no candles","timestamp":"2026-09-01T00:00:02Z"}` +
		`]`
	run := entities.AgentRun{
		ID: 1, Trigger: "chat_ui", Status: entities.AgentRunOK, StrategyID: &strategyID,
		InputSummary:  "[Context: the operator currently has strategy #5 open in the workbench editor. Assume questions refer to it unless stated otherwise.]\n\nmake a challenger",
		ToolCallsJSON: datatypes.JSON(toolCalls), StartedAt: time.Now(),
	}
	repo := &mockRepository{runs: []entities.AgentRun{run}, run: run}

	check := func(t *testing.T, resp handler.AgentRunResponse) {
		assert.Equal(t, "make a challenger", resp.InputSummary)
		require.Len(t, resp.ToolCalls, 3)
		assert.Equal(t, []handler.ToolRef{
			{Kind: "strategy", ID: 12, Role: "challenger"},
			{Kind: "strategy", ID: 5, Role: "champion"},
		}, resp.ToolCalls[0].Refs)
		assert.Equal(t, []handler.ToolRef{{Kind: "backtest", ID: 44}, {Kind: "strategy", ID: 12}}, resp.ToolCalls[1].Refs)
		assert.Equal(t, []handler.ToolRef{}, resp.ToolCalls[2].Refs)
	}

	code, runs, body := getRuns(t, repo, "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, runs, 1)
	check(t, runs[0])
	assert.Contains(t, string(body), `"refs":[]`, "an errored call serializes refs as [], never null")

	router := newRouter(&mockUseCase{}, repo)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/agent/runs/1", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var detail handler.AgentRunResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &detail))
	check(t, detail)
}

func uintToString(v uint) string {
	b, _ := json.Marshal(v)
	return string(b)
}
