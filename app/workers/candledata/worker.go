// Package candledata is the asynq side of the candle-dataset reconciler:
// task names, queues and the chunk enqueuer.
package candledata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go-trade-bot/internal/configuration"

	"github.com/hibiken/asynq"
)

const (
	TaskChunk = "candledata:chunk"
	TaskSweep = "candledata:sweep"

	// QueueLive carries live top-ups and hole repairs; QueueBackfill carries
	// deep history. Both are served by a dedicated asynq server in cmd/worker,
	// never the trading server.
	QueueLive     = "candles_live"
	QueueBackfill = "candles"

	// LivePriority is the chunk priority from which a chunk goes to QueueLive.
	LivePriority = 50

	chunkMaxRetry = 5
	chunkTimeout  = 20 * time.Minute
)

// Enqueuer implements candledata.ChunkEnqueuer. The task id makes re-enqueueing
// a chunk that is already queued a harmless no-op, which is what lets the
// sweeper blindly re-enqueue every pending chunk.
type Enqueuer struct{ client *asynq.Client }

func NewEnqueuer(cfg *configuration.Configuration) *Enqueuer {
	return &Enqueuer{client: asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})}
}

func ChunkTaskID(chunkID uint) string { return fmt.Sprintf("candle-chunk:%d", chunkID) }

func (e *Enqueuer) EnqueueChunk(ctx context.Context, chunkID uint, priority int) error {
	payload, err := json.Marshal(map[string]uint{"chunk_id": chunkID})
	if err != nil {
		return err
	}
	queue := QueueBackfill
	if priority >= LivePriority {
		queue = QueueLive
	}
	_, err = e.client.EnqueueContext(ctx, asynq.NewTask(TaskChunk, payload),
		asynq.Queue(queue), asynq.TaskID(ChunkTaskID(chunkID)),
		asynq.MaxRetry(chunkMaxRetry), asynq.Timeout(chunkTimeout))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}
