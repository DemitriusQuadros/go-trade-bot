// Package candledata persists candle datasets, the coverage ledger and the
// chunk queue (see app/usecase/candledata).
package candledata

import (
	"context"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChunkInput is a planned chunk to enqueue.
type ChunkInput struct {
	From     time.Time
	To       time.Time
	Purpose  entities.ChunkPurpose
	Priority int
}

// ChunkOutcome is what a finished chunk reports.
type ChunkOutcome struct {
	SourceUsed string
	RowCount   int
}

type Repository interface {
	// UpsertDataset creates the dataset or, if (symbol, timeframe) exists,
	// updates Start/KeepLive and returns the stored row in ds.
	UpsertDataset(ctx context.Context, ds *entities.CandleDataset) error
	GetDataset(ctx context.Context, id uint) (*entities.CandleDataset, error)
	ListDatasets(ctx context.Context) ([]entities.CandleDataset, error)
	SetPaused(ctx context.Context, id uint, paused bool) error
	// DeleteDataset removes the dataset, its ledger and chunks. Stored
	// candles are kept.
	DeleteDataset(ctx context.Context, id uint) error

	Segments(ctx context.Context, datasetID uint) ([]entities.CandleSegment, error)
	// ReplaceSegments atomically swaps the ledger rows lying fully inside
	// [from, to) for segs.
	ReplaceSegments(ctx context.Context, datasetID uint, from, to time.Time, segs []entities.CandleSegment) error

	// EnqueueChunks inserts chunks that are not already planned and returns
	// the ones it created (pending). A chunk is skipped when an identical one
	// exists (done chunks are reset to pending - the ledger no longer covers
	// them) or when an active chunk already contains its range.
	EnqueueChunks(ctx context.Context, datasetID uint, in []ChunkInput) ([]entities.CandleChunk, error)
	GetChunk(ctx context.Context, id uint) (*entities.CandleChunk, error)
	// ClaimChunk atomically moves pending|failed -> running for owner (failed
	// = a retryable failure; dead chunks are parked until RetryFailed); ok is
	// false when another worker got it first or it is not claimable.
	ClaimChunk(ctx context.Context, id uint, owner string, now time.Time) (chunk *entities.CandleChunk, ok bool, err error)
	CompleteChunk(ctx context.Context, id uint, out ChunkOutcome, now time.Time) error
	// FailChunk records the error; dead=true parks it until RetryFailed.
	FailChunk(ctx context.Context, id uint, errMsg string, dead bool, now time.Time) error
	// RetryFailed moves failed/dead chunks of the dataset back to pending
	// with attempts reset, returning them for re-enqueueing.
	RetryFailed(ctx context.Context, datasetID uint) ([]entities.CandleChunk, error)
	// Heartbeat renews a running chunk's lease. It reports false when the
	// chunk is no longer running under owner (reaped or finished).
	Heartbeat(ctx context.Context, id uint, owner string, now time.Time) (bool, error)
	// ListStale returns running chunks whose last heartbeat is older than
	// cutoff (their worker died or was redeployed).
	ListStale(ctx context.Context, cutoff time.Time, limit int) ([]entities.CandleChunk, error)
	// ListRunnable returns pending chunks of non-paused datasets, highest
	// priority first - what the sweeper (re-)enqueues.
	ListRunnable(ctx context.Context, limit int) ([]entities.CandleChunk, error)
	// AllChunkCounts is ChunkCounts across every dataset (metrics).
	AllChunkCounts(ctx context.Context) (map[entities.ChunkStatus]int64, error)
	ChunkCounts(ctx context.Context, datasetID uint) (map[entities.ChunkStatus]int64, error)
	ListChunks(ctx context.Context, datasetID uint, status entities.ChunkStatus, limit int) ([]entities.CandleChunk, error)
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) UpsertDataset(ctx context.Context, ds *entities.CandleDataset) error {
	var found []entities.CandleDataset
	if err := r.db.WithContext(ctx).Where("symbol = ? AND timeframe = ?", ds.Symbol, ds.Timeframe).Limit(1).Find(&found).Error; err != nil {
		return err
	}
	if len(found) == 0 {
		return r.db.WithContext(ctx).Create(ds).Error
	}
	existing := found[0]
	existing.Start, existing.KeepLive = ds.Start, ds.KeepLive
	if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return err
	}
	*ds = existing
	return nil
}

func (r *repository) GetDataset(ctx context.Context, id uint) (*entities.CandleDataset, error) {
	var ds entities.CandleDataset
	if err := r.db.WithContext(ctx).First(&ds, id).Error; err != nil {
		return nil, err
	}
	return &ds, nil
}

func (r *repository) ListDatasets(ctx context.Context) ([]entities.CandleDataset, error) {
	var out []entities.CandleDataset
	err := r.db.WithContext(ctx).Order("symbol ASC, timeframe ASC").Find(&out).Error
	return out, err
}

func (r *repository) SetPaused(ctx context.Context, id uint, paused bool) error {
	res := r.db.WithContext(ctx).Model(&entities.CandleDataset{}).Where("id = ?", id).Update("paused", paused)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *repository) DeleteDataset(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("dataset_id = ?", id).Delete(&entities.CandleChunk{}).Error; err != nil {
			return err
		}
		if err := tx.Where("dataset_id = ?", id).Delete(&entities.CandleSegment{}).Error; err != nil {
			return err
		}
		return tx.Delete(&entities.CandleDataset{}, id).Error
	})
}

func (r *repository) Segments(ctx context.Context, datasetID uint) ([]entities.CandleSegment, error) {
	var out []entities.CandleSegment
	err := r.db.WithContext(ctx).Where("dataset_id = ?", datasetID).Order("range_from ASC").Find(&out).Error
	return out, err
}

func (r *repository) ReplaceSegments(ctx context.Context, datasetID uint, from, to time.Time, segs []entities.CandleSegment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("dataset_id = ? AND range_from >= ? AND range_to <= ?", datasetID, from, to).
			Delete(&entities.CandleSegment{}).Error; err != nil {
			return err
		}
		if len(segs) == 0 {
			return nil
		}
		for i := range segs {
			segs[i].ID = 0
			segs[i].DatasetID = datasetID
		}
		return tx.CreateInBatches(&segs, 500).Error
	})
}

func (r *repository) EnqueueChunks(ctx context.Context, datasetID uint, in []ChunkInput) ([]entities.CandleChunk, error) {
	var created []entities.CandleChunk
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, c := range in {
			var sameRows []entities.CandleChunk
			if err := tx.Where("dataset_id = ? AND range_from = ? AND range_to = ?", datasetID, c.From, c.To).Limit(1).Find(&sameRows).Error; err != nil {
				return err
			}
			if len(sameRows) > 0 {
				same := sameRows[0]
				if same.Status == entities.ChunkDone {
					if err := tx.Model(&same).Updates(map[string]any{"status": entities.ChunkPending, "attempts": 0, "last_error": "", "purpose": c.Purpose, "priority": c.Priority}).Error; err != nil {
						return err
					}
					same.Status = entities.ChunkPending
					created = append(created, same)
				}
				continue
			}
			var containing int64
			if err := tx.Model(&entities.CandleChunk{}).
				Where("dataset_id = ? AND status IN ? AND range_from <= ? AND range_to >= ?",
					datasetID, []entities.ChunkStatus{entities.ChunkPending, entities.ChunkRunning}, c.From, c.To).
				Count(&containing).Error; err != nil {
				return err
			}
			if containing > 0 {
				continue
			}
			chunk := entities.CandleChunk{DatasetID: datasetID, From: c.From, To: c.To, Purpose: c.Purpose, Priority: c.Priority, Status: entities.ChunkPending}
			if err := tx.Create(&chunk).Error; err != nil {
				return err
			}
			created = append(created, chunk)
		}
		return nil
	})
	return created, err
}

func (r *repository) GetChunk(ctx context.Context, id uint) (*entities.CandleChunk, error) {
	var c entities.CandleChunk
	if err := r.db.WithContext(ctx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *repository) ClaimChunk(ctx context.Context, id uint, owner string, now time.Time) (*entities.CandleChunk, bool, error) {
	res := r.db.WithContext(ctx).Model(&entities.CandleChunk{}).
		Where("id = ? AND status IN ?", id, []entities.ChunkStatus{entities.ChunkPending, entities.ChunkFailed}).
		Updates(map[string]any{
			"status": entities.ChunkRunning, "lease_owner": owner, "heartbeat_at": now, "started_at": now,
			"attempts": gorm.Expr("attempts + 1"),
		})
	if res.Error != nil {
		return nil, false, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, false, nil
	}
	c, err := r.GetChunk(ctx, id)
	return c, err == nil, err
}

func (r *repository) CompleteChunk(ctx context.Context, id uint, out ChunkOutcome, now time.Time) error {
	return r.db.WithContext(ctx).Model(&entities.CandleChunk{}).Where("id = ?", id).Updates(map[string]any{
		"status": entities.ChunkDone, "source_used": out.SourceUsed, "row_count": out.RowCount,
		"last_error": "", "finished_at": now, "lease_owner": "",
	}).Error
}

func (r *repository) FailChunk(ctx context.Context, id uint, errMsg string, dead bool, now time.Time) error {
	status := entities.ChunkFailed
	if dead {
		status = entities.ChunkDead
	}
	return r.db.WithContext(ctx).Model(&entities.CandleChunk{}).Where("id = ?", id).Updates(map[string]any{
		"status": status, "last_error": errMsg, "finished_at": now, "lease_owner": "",
	}).Error
}

func (r *repository) RetryFailed(ctx context.Context, datasetID uint) ([]entities.CandleChunk, error) {
	var chunks []entities.CandleChunk
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("dataset_id = ? AND status IN ?", datasetID, []entities.ChunkStatus{entities.ChunkFailed, entities.ChunkDead}).
			Find(&chunks).Error; err != nil {
			return err
		}
		for i := range chunks {
			if err := tx.Model(&chunks[i]).Updates(map[string]any{"status": entities.ChunkPending, "attempts": 0, "last_error": ""}).Error; err != nil {
				return err
			}
			chunks[i].Status = entities.ChunkPending
		}
		return nil
	})
	return chunks, err
}

func (r *repository) ChunkCounts(ctx context.Context, datasetID uint) (map[entities.ChunkStatus]int64, error) {
	var rows []struct {
		Status entities.ChunkStatus
		N      int64
	}
	if err := r.db.WithContext(ctx).Model(&entities.CandleChunk{}).Select("status, COUNT(*) AS n").
		Where("dataset_id = ?", datasetID).Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[entities.ChunkStatus]int64{}
	for _, r := range rows {
		out[r.Status] = r.N
	}
	return out, nil
}

func (r *repository) ListChunks(ctx context.Context, datasetID uint, status entities.ChunkStatus, limit int) ([]entities.CandleChunk, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := r.db.WithContext(ctx).Where("dataset_id = ?", datasetID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var out []entities.CandleChunk
	err := q.Order("range_from ASC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *repository) Heartbeat(ctx context.Context, id uint, owner string, now time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&entities.CandleChunk{}).
		Where("id = ? AND status = ? AND lease_owner = ?", id, entities.ChunkRunning, owner).
		Update("heartbeat_at", now)
	return res.RowsAffected > 0, res.Error
}

func (r *repository) ListStale(ctx context.Context, cutoff time.Time, limit int) ([]entities.CandleChunk, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []entities.CandleChunk
	err := r.db.WithContext(ctx).
		Where("status = ? AND heartbeat_at < ?", entities.ChunkRunning, cutoff).
		Order("id ASC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *repository) ListRunnable(ctx context.Context, limit int) ([]entities.CandleChunk, error) {
	if limit <= 0 {
		limit = 200
	}
	var out []entities.CandleChunk
	err := r.db.WithContext(ctx).
		Where("status = ? AND dataset_id IN (?)", entities.ChunkPending,
			r.db.Model(&entities.CandleDataset{}).Select("id").Where("paused = ?", false)).
		Order("priority DESC, range_from ASC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *repository) AllChunkCounts(ctx context.Context) (map[entities.ChunkStatus]int64, error) {
	var rows []struct {
		Status entities.ChunkStatus
		N      int64
	}
	if err := r.db.WithContext(ctx).Model(&entities.CandleChunk{}).Select("status, COUNT(*) AS n").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[entities.ChunkStatus]int64{}
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, nil
}
