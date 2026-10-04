package candledata

import (
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/candledata"
)

// maxRanges caps each range list in a response; a dataset with thousands of
// scattered holes would otherwise produce a huge payload.
const maxRanges = 200

type RangeDTO struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type ChunkCountsDTO struct {
	Pending int64 `json:"pending"`
	Running int64 `json:"running"`
	Done    int64 `json:"done"`
	Failed  int64 `json:"failed"`
	Dead    int64 `json:"dead"`
}

type DatasetResponse struct {
	ID             uint           `json:"id"`
	Symbol         string         `json:"symbol"`
	Timeframe      string         `json:"timeframe"`
	Start          *time.Time     `json:"start"`
	KeepLive       bool           `json:"keep_live"`
	Paused         bool           `json:"paused"`
	State          string         `json:"state"`
	Desired        *RangeDTO      `json:"desired"`
	Loaded         []RangeDTO     `json:"loaded"`
	KnownGaps      []RangeDTO     `json:"known_gaps"`
	Missing        []RangeDTO     `json:"missing"`
	Truncated      bool           `json:"ranges_truncated"`
	DesiredCandles int64          `json:"desired_candles"`
	MissingCandles int64          `json:"missing_candles"`
	ProgressPct    float64        `json:"progress_pct"`
	LagSeconds     float64        `json:"lag_seconds"`
	Chunks         ChunkCountsDTO `json:"chunks"`
	LastError      string         `json:"last_error"`
	ListingError   string         `json:"listing_error,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

func toRanges(rs []usecase.Range, truncated *bool) []RangeDTO {
	out := make([]RangeDTO, 0, len(rs))
	for i, r := range rs {
		if i == maxRanges {
			*truncated = true
			break
		}
		out = append(out, RangeDTO{From: r.From.UTC(), To: r.To.UTC()})
	}
	return out
}

func toResponse(st *usecase.DatasetStatus) DatasetResponse {
	r := DatasetResponse{
		ID: st.Dataset.ID, Symbol: st.Dataset.Symbol, Timeframe: st.Dataset.Timeframe, Start: st.Dataset.Start,
		KeepLive: st.Dataset.KeepLive, Paused: st.Dataset.Paused, State: string(st.State),
		DesiredCandles: st.DesiredCandles, MissingCandles: st.MissingCandles, LagSeconds: st.LagSeconds,
		LastError: st.LastError, ListingError: st.ListingError, CreatedAt: st.Dataset.CreatedAt,
		Chunks: ChunkCountsDTO{
			Pending: st.Chunks[entities.ChunkPending], Running: st.Chunks[entities.ChunkRunning], Done: st.Chunks[entities.ChunkDone],
			Failed: st.Chunks[entities.ChunkFailed], Dead: st.Chunks[entities.ChunkDead],
		},
	}
	r.Loaded = toRanges(st.Loaded, &r.Truncated)
	r.KnownGaps = toRanges(st.KnownGaps, &r.Truncated)
	r.Missing = toRanges(st.Missing, &r.Truncated)
	if !st.Desired.Empty() {
		r.Desired = &RangeDTO{From: st.Desired.From, To: st.Desired.To}
	}
	if st.DesiredCandles > 0 {
		r.ProgressPct = 100 * float64(st.DesiredCandles-st.MissingCandles) / float64(st.DesiredCandles)
	}
	return r
}

type ChunkResponse struct {
	ID         uint       `json:"id"`
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	Purpose    string     `json:"purpose"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	SourceUsed string     `json:"source_used"`
	RowCount   int        `json:"row_count"`
	LastError  string     `json:"last_error"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func toChunkResponse(c entities.CandleChunk) ChunkResponse {
	return ChunkResponse{ID: c.ID, From: c.From.UTC(), To: c.To.UTC(), Purpose: string(c.Purpose), Status: string(c.Status),
		Attempts: c.Attempts, SourceUsed: c.SourceUsed, RowCount: c.RowCount, LastError: c.LastError,
		StartedAt: c.StartedAt, FinishedAt: c.FinishedAt}
}
