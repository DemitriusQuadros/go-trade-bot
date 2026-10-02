package candledata

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/candledata"
	"go-trade-bot/internal/authz"
)

type fakeMgr struct {
	status  *usecase.DatasetStatus
	calls   []string
	err     error
	created struct {
		symbol, tf string
		start      *time.Time
		live       bool
	}
}

func (f *fakeMgr) List(context.Context) ([]*usecase.DatasetStatus, error) {
	return []*usecase.DatasetStatus{f.status}, f.err
}
func (f *fakeMgr) Status(context.Context, uint) (*usecase.DatasetStatus, error) {
	return f.status, f.err
}
func (f *fakeMgr) Create(_ context.Context, s, tf string, start *time.Time, live bool) (*entities.CandleDataset, error) {
	f.created.symbol, f.created.tf, f.created.start, f.created.live = s, tf, start, live
	if f.err != nil {
		return nil, f.err
	}
	return &f.status.Dataset, nil
}
func (f *fakeMgr) Pause(context.Context, uint) error {
	f.calls = append(f.calls, "pause")
	return f.err
}
func (f *fakeMgr) Resume(context.Context, uint) error {
	f.calls = append(f.calls, "resume")
	return f.err
}
func (f *fakeMgr) RetryFailed(context.Context, uint) (int, error) {
	f.calls = append(f.calls, "retry")
	return 1, f.err
}
func (f *fakeMgr) Reconcile(context.Context, uint) (int, error) {
	f.calls = append(f.calls, "reconcile")
	return 1, f.err
}
func (f *fakeMgr) Delete(context.Context, uint) error {
	f.calls = append(f.calls, "delete")
	return f.err
}
func (f *fakeMgr) Chunks(_ context.Context, _ uint, st entities.ChunkStatus, _ int) ([]entities.CandleChunk, error) {
	f.calls = append(f.calls, "chunks:"+string(st))
	return []entities.CandleChunk{{ID: 7, Status: entities.ChunkDead, LastError: "boom"}}, f.err
}

func t0(d int) time.Time { return time.Date(2024, 1, d, 0, 0, 0, 0, time.UTC) }

func newFake() *fakeMgr {
	return &fakeMgr{status: &usecase.DatasetStatus{
		Dataset:        entities.CandleDataset{ID: 3, Symbol: "BTCUSDT", Timeframe: "1h", KeepLive: true},
		State:          usecase.StateConverging,
		Desired:        usecase.Range{From: t0(1), To: t0(5)},
		Loaded:         []usecase.Range{{From: t0(1), To: t0(3)}},
		Missing:        []usecase.Range{{From: t0(3), To: t0(5)}},
		DesiredCandles: 96, MissingCandles: 48,
		Chunks: map[entities.ChunkStatus]int64{entities.ChunkPending: 2, entities.ChunkDead: 1},
	}}
}

func router(m Manager) *mux.Router {
	h := &Handler{mgr: m}
	r := mux.NewRouter()
	for _, c := range h.Handlers() {
		r.HandleFunc(c.Pattern, c.Action).Methods(c.Method)
	}
	return r
}

func do(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestEveryRouteDeclaresAValidCapability_AndWritesNeedAdmin(t *testing.T) {
	for _, c := range (&Handler{}).Handlers() {
		assert.True(t, authz.IsValidRouteCapability(c.Capability), c.Pattern)
		if c.Method != http.MethodGet {
			assert.Equal(t, authz.CapAdmin, c.Capability, c.Method+" "+c.Pattern)
		}
	}
}

func TestListAndGet_ShapeAndProgress(t *testing.T) {
	r := router(newFake())

	w := do(r, http.MethodGet, "/candle-datasets/3", "")
	require.Equal(t, http.StatusOK, w.Code)
	var got DatasetResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "converging", got.State)
	assert.InDelta(t, 50.0, got.ProgressPct, 0.001)
	assert.Equal(t, int64(2), got.Chunks.Pending)
	assert.Equal(t, int64(1), got.Chunks.Dead)
	require.Len(t, got.Missing, 1)
	assert.NotNil(t, got.Desired)

	w = do(r, http.MethodGet, "/candle-datasets", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list []DatasetResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Len(t, list, 1)
}

func TestCreate_NormalisesAndDefaultsKeepLive(t *testing.T) {
	f := newFake()
	r := router(f)

	w := do(r, http.MethodPost, "/candle-datasets", `{"symbol":" btcusdt ","timeframe":"15m","start":"2021-03-05"}`)

	require.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "BTCUSDT", f.created.symbol)
	assert.True(t, f.created.live, "keep_live defaults to true")
	require.NotNil(t, f.created.start)
	assert.Equal(t, time.Date(2021, 3, 5, 0, 0, 0, 0, time.UTC), *f.created.start)

	w = do(r, http.MethodPost, "/candle-datasets", `{"symbol":"BTCUSDT","timeframe":"1h","keep_live":false}`)
	require.Equal(t, http.StatusCreated, w.Code)
	assert.False(t, f.created.live)
	assert.Nil(t, f.created.start)

	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodPost, "/candle-datasets", `{"symbol":"BTCUSDT","timeframe":"1h","start":"yesterday"}`).Code)
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodPost, "/candle-datasets", `{not json`).Code)
}

func TestActions_PauseResumeRetryReconcileDelete(t *testing.T) {
	f := newFake()
	r := router(f)
	for _, tc := range []struct{ path, call string }{
		{"/candle-datasets/3/pause", "pause"}, {"/candle-datasets/3/resume", "resume"},
		{"/candle-datasets/3/retry-failed", "retry"}, {"/candle-datasets/3/reconcile", "reconcile"},
	} {
		w := do(r, http.MethodPost, tc.path, "")
		require.Equal(t, http.StatusOK, w.Code, tc.path)
		assert.Equal(t, tc.call, f.calls[len(f.calls)-1])
		var got DatasetResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), "returns the refreshed status")
		assert.Equal(t, uint(3), got.ID)
	}
	assert.Equal(t, http.StatusNoContent, do(r, http.MethodDelete, "/candle-datasets/3", "").Code)
	assert.Equal(t, "delete", f.calls[len(f.calls)-1])
}

func TestChunks_ReadOnlyListWithStatusFilter(t *testing.T) {
	f := newFake()
	w := do(router(f), http.MethodGet, "/candle-datasets/3/chunks?status=dead", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "chunks:dead", f.calls[0])
	var got []ChunkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "boom", got[0].LastError)
}

func TestErrorMapping(t *testing.T) {
	f := newFake()
	r := router(f)

	f.err = usecase.ErrInvalid{Msg: "bad tf"}
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodPost, "/candle-datasets", `{"symbol":"BTCUSDT","timeframe":"1w"}`).Code)
	f.err = gorm.ErrRecordNotFound
	assert.Equal(t, http.StatusNotFound, do(r, http.MethodGet, "/candle-datasets/99", "").Code)
	f.err = assert.AnError
	assert.Equal(t, http.StatusInternalServerError, do(r, http.MethodPost, "/candle-datasets/3/pause", "").Code)
	f.err = nil
	assert.Equal(t, http.StatusBadRequest, do(r, http.MethodGet, "/candle-datasets/abc", "").Code)
}

func TestRangesAreCapped(t *testing.T) {
	f := newFake()
	for i := 0; i < 300; i++ {
		from := t0(1).Add(time.Duration(i) * 2 * time.Hour)
		f.status.Missing = append(f.status.Missing, usecase.Range{From: from, To: from.Add(time.Hour)})
	}
	w := do(router(f), http.MethodGet, "/candle-datasets/3", "")
	var got DatasetResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Len(t, got.Missing, maxRanges)
	assert.True(t, got.Truncated)
}
