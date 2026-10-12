package backtest_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/backtest"
	usecase "go-trade-bot/app/usecase/backtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAsync struct {
	run          entities.BacktestRun
	err          error
	gotWF        *usecase.WalkForwardRequest
	gotRun       *usecase.RunRequest
	markedFailed []uint
}

func (f *fakeAsync) Enqueue(_ context.Context, r usecase.RunRequest) (entities.BacktestRun, error) {
	f.gotRun = &r
	return f.run, f.err
}
func (f *fakeAsync) EnqueueWalkForward(_ context.Context, r usecase.WalkForwardRequest) (entities.BacktestRun, error) {
	f.gotWF = &r
	return f.run, f.err
}
func (f *fakeAsync) MarkEnqueueFailed(_ context.Context, id uint, _ error) error {
	f.markedFailed = append(f.markedFailed, id)
	return nil
}

type fakeTasks struct {
	ids []uint
	err error
}

func (f *fakeTasks) EnqueueBacktestTask(id uint) error { f.ids = append(f.ids, id); return f.err }

func asyncHandler(sync *mockUseCase, a handler.AsyncUseCase, tk handler.TaskEnqueuer) *handler.BacktestHandler {
	return handler.NewBacktestHandlerAsync(handler.AsyncParams{UseCase: sync, Async: a, Tasks: tk})
}

func post(h func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/backtest", bytes.NewBufferString(body)))
	return rec
}

func TestRunBacktest_AsyncReturns202QueuedAndEnqueuesTask(t *testing.T) {
	sync := &mockUseCase{runErr: errors.New("must not run synchronously")}
	a := &fakeAsync{run: entities.BacktestRun{ID: 42, StrategyID: 1, Symbol: "BTCUSDT", Status: entities.BacktestQueued}}
	tk := &fakeTasks{}
	h := asyncHandler(sync, a, tk)

	rec := post(h.RunBacktest, `{"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","async":true}`)

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "/api/backtest/42", rec.Header().Get("Location"))
	assert.Contains(t, rec.Body.String(), `"status":"queued"`)
	assert.Equal(t, []uint{42}, tk.ids)
	require.NotNil(t, a.gotRun)
	assert.Equal(t, "1h", a.gotRun.Timeframe)
}

func TestRunWalkForward_AsyncReturns202(t *testing.T) {
	a := &fakeAsync{run: entities.BacktestRun{ID: 9, IsWalkForward: true, Status: entities.BacktestQueued}}
	tk := &fakeTasks{}
	h := asyncHandler(&mockUseCase{runErr: errors.New("must not run synchronously")}, a, tk)

	rec := post(h.RunWalkForward, `{"strategy_id":1,"symbol":"BTCUSDT","train_months":3,"test_months":1,"async":true}`)

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"is_walk_forward":true`)
	assert.Equal(t, []uint{9}, tk.ids)
	require.NotNil(t, a.gotWF)
	assert.Equal(t, 3, a.gotWF.TrainMonths)
}

func TestRunBacktest_AsyncQueueFailureMarksRunFailedAnd503(t *testing.T) {
	a := &fakeAsync{run: entities.BacktestRun{ID: 5, Status: entities.BacktestQueued}}
	tk := &fakeTasks{err: errors.New("redis down")}
	h := asyncHandler(&mockUseCase{}, a, tk)

	rec := post(h.RunBacktest, `{"strategy_id":1,"symbol":"BTCUSDT","async":true}`)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, []uint{5}, a.markedFailed, "a run that could not be queued must not stay queued forever")
}

func TestRunBacktest_AsyncValidationErrorIsNotQueued(t *testing.T) {
	a := &fakeAsync{err: errors.New("no BTCUSDT 1m candles in range")}
	tk := &fakeTasks{}
	h := asyncHandler(&mockUseCase{}, a, tk)

	rec := post(h.RunBacktest, `{"strategy_id":1,"symbol":"BTCUSDT","async":true}`)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Empty(t, tk.ids)
}

func TestRunBacktest_AsyncWithoutQueueIs501NotSilentlySync(t *testing.T) {
	sync := &mockUseCase{runResult: entities.BacktestRun{ID: 1}}
	h := handler.NewBacktestHandler(sync)

	rec := post(h.RunBacktest, `{"strategy_id":1,"symbol":"BTCUSDT","async":true}`)

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestRunBacktest_WithoutAsyncStaysSynchronous(t *testing.T) {
	sync := &mockUseCase{runResult: entities.BacktestRun{ID: 1, Status: entities.BacktestDone}}
	a := &fakeAsync{}
	tk := &fakeTasks{}
	h := asyncHandler(sync, a, tk)

	rec := post(h.RunBacktest, `{"strategy_id":1,"symbol":"BTCUSDT"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, a.gotRun)
	assert.Empty(t, tk.ids)
}
