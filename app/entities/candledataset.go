package entities

import "time"

// CandleDataset declares "keep (Symbol, Timeframe) candles loaded from Start
// (nil = the symbol's listing date) up to the last closed candle". The
// reconciler (app/usecase/candledata) converges stored data toward it; there
// is no "import N months" operation. KeepLive datasets are also topped up after
// every candle close.
type CandleDataset struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Symbol    string     `gorm:"type:varchar(20);not null;uniqueIndex:idx_candle_dataset_sym_tf" json:"symbol"`
	Timeframe string     `gorm:"type:varchar(5);not null;uniqueIndex:idx_candle_dataset_sym_tf" json:"timeframe"`
	Start     *time.Time `json:"start"`
	KeepLive  bool       `json:"keep_live"`
	Paused    bool       `json:"paused"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// SegmentState is what the coverage ledger knows about a half-open time range.
type SegmentState string

const (
	// SegmentLoaded: every expected candle in the range is stored.
	SegmentLoaded SegmentState = "loaded"
	// SegmentKnownGap: an authoritative source proved the exchange emitted no
	// candle for the range (no trades / maintenance). Not retried.
	SegmentKnownGap SegmentState = "known_gap"
)

// CandleSegment is one row of the coverage ledger: [From, To) is verified as
// State, by Source (and SourceRef - file name + checksum for archives, so a
// corrected archive can be re-ingested). Segments are not merged in storage;
// candledata.Covered merges them in memory.
type CandleSegment struct {
	ID        uint         `gorm:"primaryKey"`
	DatasetID uint         `gorm:"not null;index:idx_candle_segment_dataset_from"`
	From      time.Time    `gorm:"column:range_from;not null;index:idx_candle_segment_dataset_from"`
	To        time.Time    `gorm:"column:range_to;not null"`
	State     SegmentState `gorm:"type:varchar(12);not null"`
	Source    string       `gorm:"type:varchar(32)"`
	SourceRef string       `gorm:"type:varchar(255)"`
	RowCount  int
	LoadedAt  time.Time
}

type ChunkStatus string

const (
	ChunkPending ChunkStatus = "pending"
	ChunkRunning ChunkStatus = "running"
	ChunkDone    ChunkStatus = "done"
	ChunkFailed  ChunkStatus = "failed"
	ChunkDead    ChunkStatus = "dead"
)

type ChunkPurpose string

const (
	PurposeBackfill ChunkPurpose = "backfill"
	PurposeTail     ChunkPurpose = "tail"
	PurposeRepair   ChunkPurpose = "repair"
)

// CandleChunk is one bounded unit of work (never spans a UTC month) carried by
// one asynq task. The chunk table is the source of truth for progress, so no
// state is lost when a worker dies. (DatasetID, From, To) is unique so
// re-planning never duplicates work.
type CandleChunk struct {
	ID          uint         `gorm:"primaryKey"`
	DatasetID   uint         `gorm:"not null;uniqueIndex:idx_candle_chunk_range;index"`
	From        time.Time    `gorm:"column:range_from;not null;uniqueIndex:idx_candle_chunk_range"`
	To          time.Time    `gorm:"column:range_to;not null;uniqueIndex:idx_candle_chunk_range"`
	Purpose     ChunkPurpose `gorm:"type:varchar(12);not null"`
	Priority    int          `gorm:"not null;default:0"`
	Status      ChunkStatus  `gorm:"type:varchar(10);not null;index"`
	Attempts    int
	LeaseOwner  string `gorm:"type:varchar(64)"`
	HeartbeatAt *time.Time
	LastError   string
	SourceUsed  string `gorm:"type:varchar(64)"`
	RowCount    int
	StartedAt   *time.Time
	FinishedAt  *time.Time
	CreatedAt   time.Time
}
