package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	candledata "go-trade-bot/app/usecase/candledata"
)

// --- script versions -----------------------------------------------------------

func versionTools(r VersionReader) []Tool {
	return []Tool{{
		Name:        "list_strategy_versions",
		Description: "Saved script versions of a strategy (id, saved_at, size), newest first. Pass version_id to get that version's full Lua source (e.g. to diff against get_strategy).",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"strategy_id":{"type":"integer"},"version_id":{"type":"integer","description":"optional - return this version's source"}},"required":["strategy_id"]}`),
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var in struct {
				StrategyID uint `json:"strategy_id"`
				VersionID  uint `json:"version_id"`
			}
			if err := decode("list_strategy_versions", raw, &in); err != nil {
				return nil, err
			}
			if err := requireID("list_strategy_versions", "strategy_id", in.StrategyID); err != nil {
				return nil, err
			}
			vs, err := r.GetScriptVersions(ctx, in.StrategyID)
			if err != nil {
				return nil, err
			}
			if in.VersionID != 0 {
				for _, v := range vs {
					if v.ID == in.VersionID {
						return map[string]any{"id": v.ID, "strategy_id": v.StrategyID, "saved_at": ts(v.CreatedAt), "source": v.Source}, nil
					}
				}
				return nil, fmt.Errorf("list_strategy_versions: version %d not found for strategy %d", in.VersionID, in.StrategyID)
			}
			type item struct {
				ID          uint   `json:"id"`
				SavedAt     string `json:"saved_at"`
				SourceBytes int    `json:"source_bytes"`
			}
			out := make([]item, 0, len(vs))
			for _, v := range vs {
				out = append(out, item{ID: v.ID, SavedAt: ts(v.CreatedAt), SourceBytes: len(v.Source)})
			}
			return map[string]any{"versions": out}, nil
		},
	}}
}

// --- monte carlo ---------------------------------------------------------------

func monteCarloTools(m MonteCarlo) []Tool {
	runSchema := json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"integer","description":"a finished backtest run id"}},"required":["run_id"]}`)
	return []Tool{
		{
			Name:        "get_monte_carlo",
			Description: "The stored Monte Carlo robustness result (Sharpe / max drawdown / total return distributions over shuffled trade order) for a backtest run. Errors if it was never computed - use run_monte_carlo.",
			InputSchema: runSchema,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					RunID uint `json:"run_id"`
				}
				if err := decode("get_monte_carlo", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("get_monte_carlo", "run_id", in.RunID); err != nil {
					return nil, err
				}
				return m.GetMonteCarlo(ctx, in.RunID)
			},
		},
		{
			Name:        "run_monte_carlo",
			Description: "Run a Monte Carlo trade-reordering analysis on a finished backtest run and store the result on it. Needs at least 2 trades. iterations is optional (server default; capped by the server).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"integer"},"iterations":{"type":"integer"}},"required":["run_id"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					RunID      uint `json:"run_id"`
					Iterations int  `json:"iterations"`
				}
				if err := decode("run_monte_carlo", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("run_monte_carlo", "run_id", in.RunID); err != nil {
					return nil, err
				}
				return m.RunMonteCarlo(ctx, in.RunID, in.Iterations)
			},
		},
	}
}

// --- market data ---------------------------------------------------------------

const (
	defaultKlines = 100
	maxKlines     = 500
)

func marketTools(m MarketData) []Tool {
	return []Tool{
		{
			Name:        "get_prices",
			Description: "Latest exchange ticker price for a symbol (e.g. BTCUSDT). Read-only market data.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string"}},"required":["symbol"]}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					Symbol string `json:"symbol"`
				}
				if err := decode("get_prices", raw, &in); err != nil {
					return nil, err
				}
				sym := strings.ToUpper(strings.TrimSpace(in.Symbol))
				if sym == "" {
					return nil, fmt.Errorf("get_prices: symbol is required")
				}
				prices, err := m.ListTickerPrices(ctx, sym)
				if err != nil {
					return nil, err
				}
				type item struct {
					Symbol string  `json:"symbol"`
					Price  float64 `json:"price"`
				}
				out := make([]item, 0, len(prices))
				for _, p := range prices {
					out = append(out, item{Symbol: p.Symbol, Price: p.Price})
				}
				return map[string]any{"prices": out}, nil
			},
		},
		{
			Name:        "get_klines",
			Description: "Recent exchange candles for a symbol and interval, oldest first, as rows [open_time, open, high, low, close, volume]. limit defaults to 100 (max 500). For history use the stored candles via get_candle_coverage / backtests.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string"},"interval":{"type":"string","description":"e.g. 1m, 5m, 15m, 1h, 4h, 1d"},"limit":{"type":"integer","description":"default 100, max 500"}},"required":["symbol","interval"]}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					Symbol   string `json:"symbol"`
					Interval string `json:"interval"`
					Limit    int    `json:"limit"`
				}
				if err := decode("get_klines", raw, &in); err != nil {
					return nil, err
				}
				sym := strings.ToUpper(strings.TrimSpace(in.Symbol))
				if sym == "" || strings.TrimSpace(in.Interval) == "" {
					return nil, fmt.Errorf("get_klines: symbol and interval are required")
				}
				candles, err := m.ListKline(ctx, sym, strings.TrimSpace(in.Interval), clamp(in.Limit, defaultKlines, maxKlines))
				if err != nil {
					return nil, err
				}
				rows := make([][]any, 0, len(candles))
				for _, c := range candles {
					rows = append(rows, []any{ts(c.OpenTime), c.Open, c.High, c.Low, c.Close, c.Volume})
				}
				return map[string]any{
					"symbol": sym, "interval": in.Interval,
					"columns": []string{"open_time", "open", "high", "low", "close", "volume"},
					"rows":    rows,
				}, nil
			},
		},
	}
}

// --- candle datasets -----------------------------------------------------------

type rangeView struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type chunkCounts struct {
	Pending int64 `json:"pending"`
	Running int64 `json:"running"`
	Done    int64 `json:"done"`
	Failed  int64 `json:"failed"`
	Dead    int64 `json:"dead"`
}

type datasetView struct {
	ID             uint        `json:"id"`
	Symbol         string      `json:"symbol"`
	Timeframe      string      `json:"timeframe"`
	Start          string      `json:"start,omitempty"`
	KeepLive       bool        `json:"keep_live"`
	Paused         bool        `json:"paused"`
	State          string      `json:"state"`
	DesiredCandles int64       `json:"desired_candles"`
	MissingCandles int64       `json:"missing_candles"`
	ProgressPct    float64     `json:"progress_pct"`
	LagSeconds     float64     `json:"lag_seconds"`
	Chunks         chunkCounts `json:"chunks"`
	LastError      string      `json:"last_error,omitempty"`
	ListingError   string      `json:"listing_error,omitempty"`
	// Ranges are only filled by get_candle_dataset (capped).
	Desired   *rangeView  `json:"desired,omitempty"`
	Loaded    []rangeView `json:"loaded,omitempty"`
	KnownGaps []rangeView `json:"known_gaps,omitempty"`
	Missing   []rangeView `json:"missing,omitempty"`
	Truncated bool        `json:"ranges_truncated,omitempty"`
}

const maxRanges = 50

func rangesView(rs []candledata.Range, truncated *bool) []rangeView {
	out := make([]rangeView, 0, len(rs))
	for i, r := range rs {
		if i == maxRanges {
			*truncated = true
			break
		}
		out = append(out, rangeView{From: ts(r.From), To: ts(r.To)})
	}
	return out
}

func toDatasetView(st *candledata.DatasetStatus, withRanges bool) datasetView {
	v := datasetView{
		ID: st.Dataset.ID, Symbol: st.Dataset.Symbol, Timeframe: st.Dataset.Timeframe, Start: tsPtr(st.Dataset.Start),
		KeepLive: st.Dataset.KeepLive, Paused: st.Dataset.Paused, State: string(st.State),
		DesiredCandles: st.DesiredCandles, MissingCandles: st.MissingCandles, LagSeconds: st.LagSeconds,
		LastError: st.LastError, ListingError: st.ListingError,
		Chunks: chunkCounts{
			Pending: st.Chunks[entities.ChunkPending], Running: st.Chunks[entities.ChunkRunning], Done: st.Chunks[entities.ChunkDone],
			Failed: st.Chunks[entities.ChunkFailed], Dead: st.Chunks[entities.ChunkDead],
		},
	}
	if st.DesiredCandles > 0 {
		v.ProgressPct = 100 * float64(st.DesiredCandles-st.MissingCandles) / float64(st.DesiredCandles)
	}
	if withRanges {
		v.Loaded = rangesView(st.Loaded, &v.Truncated)
		v.KnownGaps = rangesView(st.KnownGaps, &v.Truncated)
		v.Missing = rangesView(st.Missing, &v.Truncated)
		if !st.Desired.Empty() {
			v.Desired = &rangeView{From: ts(st.Desired.From), To: ts(st.Desired.To)}
		}
	}
	return v
}

var datasetIDSchema = json.RawMessage(`{"type":"object","properties":{"dataset_id":{"type":"integer"}},"required":["dataset_id"]}`)

func datasetTools(m DatasetManager) []Tool {
	idTool := func(name, desc string, write bool, act func(ctx context.Context, id uint) error) Tool {
		return Tool{
			Name: name, Description: desc, InputSchema: datasetIDSchema, Write: write,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					DatasetID uint `json:"dataset_id"`
				}
				if err := decode(name, raw, &in); err != nil {
					return nil, err
				}
				if err := requireID(name, "dataset_id", in.DatasetID); err != nil {
					return nil, err
				}
				if act != nil {
					if err := act(ctx, in.DatasetID); err != nil {
						return nil, err
					}
				}
				st, err := m.Status(ctx, in.DatasetID)
				if err != nil {
					return nil, err
				}
				return toDatasetView(st, act == nil), nil
			},
		}
	}
	return []Tool{
		{
			Name:        "list_candle_datasets",
			Description: "Candle datasets the platform keeps (symbol, timeframe, state, progress %, missing candles, sync lag, chunk counts, last error). Coverage ranges are in get_candle_dataset.",
			InputSchema: json.RawMessage(emptyObject),
			Run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				all, err := m.List(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]datasetView, 0, len(all))
				for _, st := range all {
					out = append(out, toDatasetView(st, false))
				}
				return map[string]any{"datasets": out}, nil
			},
		},
		idTool("get_candle_dataset", "One candle dataset with its desired/loaded/known-gap/missing ranges (each capped at 50) and chunk counts.", false, nil),
		{
			Name:        "list_candle_chunks",
			Description: "Backfill chunks of a candle dataset (range, purpose, status, attempts, source used, rows, last error). Filter by status (pending|running|done|failed|dead); limit defaults to 50 (max 200).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"dataset_id":{"type":"integer"},"status":{"type":"string"},"limit":{"type":"integer"}},"required":["dataset_id"]}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					DatasetID uint   `json:"dataset_id"`
					Status    string `json:"status"`
					Limit     int    `json:"limit"`
				}
				if err := decode("list_candle_chunks", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("list_candle_chunks", "dataset_id", in.DatasetID); err != nil {
					return nil, err
				}
				limit := clamp(in.Limit, 50, 200)
				cs, err := m.Chunks(ctx, in.DatasetID, entities.ChunkStatus(in.Status), limit)
				if err != nil {
					return nil, err
				}
				type item struct {
					ID         uint   `json:"id"`
					From       string `json:"from"`
					To         string `json:"to"`
					Purpose    string `json:"purpose"`
					Status     string `json:"status"`
					Attempts   int    `json:"attempts"`
					SourceUsed string `json:"source_used,omitempty"`
					RowCount   int    `json:"row_count"`
					LastError  string `json:"last_error,omitempty"`
					FinishedAt string `json:"finished_at,omitempty"`
				}
				out := make([]item, 0, len(cs))
				for _, c := range cs {
					out = append(out, item{
						ID: c.ID, From: ts(c.From), To: ts(c.To), Purpose: string(c.Purpose), Status: string(c.Status),
						Attempts: c.Attempts, SourceUsed: c.SourceUsed, RowCount: c.RowCount,
						LastError: truncate(c.LastError, 300), FinishedAt: tsPtr(c.FinishedAt),
					})
				}
				return map[string]any{"chunks": out, "limit": limit}, nil
			},
		},
		{
			Name:        "create_candle_dataset",
			Description: "Declare a candle dataset to keep (symbol e.g. BTCUSDT, timeframe e.g. 1h). start (RFC3339 or YYYY-MM-DD) is optional - absent means from the symbol's listing. keep_live defaults to true. The reconciler then backfills it in the background.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"symbol":{"type":"string"},"timeframe":{"type":"string"},"start":{"type":"string"},"keep_live":{"type":"boolean"}},"required":["symbol","timeframe"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					Symbol    string  `json:"symbol"`
					Timeframe string  `json:"timeframe"`
					Start     *string `json:"start"`
					KeepLive  *bool   `json:"keep_live"`
				}
				if err := decode("create_candle_dataset", raw, &in); err != nil {
					return nil, err
				}
				var start *time.Time
				if in.Start != nil && strings.TrimSpace(*in.Start) != "" {
					t, err := parseDay(*in.Start)
					if err != nil {
						return nil, fmt.Errorf("create_candle_dataset: start must be RFC3339 or YYYY-MM-DD")
					}
					start = &t
				}
				keepLive := in.KeepLive == nil || *in.KeepLive
				ds, err := m.Create(ctx, strings.ToUpper(strings.TrimSpace(in.Symbol)), strings.TrimSpace(in.Timeframe), start, keepLive)
				if err != nil {
					return nil, err
				}
				st, err := m.Status(ctx, ds.ID)
				if err != nil {
					return nil, err
				}
				return toDatasetView(st, false), nil
			},
		},
		idTool("pause_candle_dataset", "Pause a candle dataset's syncing.", true, m.Pause),
		idTool("resume_candle_dataset", "Resume a paused candle dataset.", true, m.Resume),
		idTool("retry_failed_candle_chunks", "Re-queue a dataset's dead (permanently failed) chunks.", true, func(ctx context.Context, id uint) error { _, err := m.RetryFailed(ctx, id); return err }),
		idTool("reconcile_candle_dataset", "Trigger an immediate reconcile pass for a dataset (plans and enqueues missing chunks).", true, func(ctx context.Context, id uint) error { _, err := m.Reconcile(ctx, id); return err }),
	}
}

func parseDay(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	return t.UTC(), err
}
