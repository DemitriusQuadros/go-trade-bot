package main

import (
	"context"
	"log"
	"time"

	candledata_tasks "go-trade-bot/app/handler/tasks/candledata"
	candledata_worker "go-trade-bot/app/workers/candledata"
	config "go-trade-bot/internal/configuration"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
)

// RegisterCandleData starts the candle-dataset reconciler's own asynq server
// (queues candles_live and candles, CANDLE_DATA.CONCURRENCY workers) so a long
// backfill can never occupy the trading server's slots, and registers the
// minute-by-minute sweep (reap dead chunks, reconcile every dataset, re-enqueue
// pending work). The sweep is Unique so several worker replicas registering the
// same cron entry still run it once per tick.
func RegisterCandleData(
	lc fx.Lifecycle,
	cfg *config.Configuration,
	scheduler *asynq.Scheduler,
	h *candledata_tasks.TaskHandler,
) {
	redisOpt := asynq.RedisClientOpt{Addr: cfg.Redis.Addr}
	server := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: cfg.CandleData.Concurrency,
		// live top-ups get 3x the pull of deep backfill when both are waiting
		Queues: map[string]int{candledata_worker.QueueLive: 3, candledata_worker.QueueBackfill: 1},
	})

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			mux := asynq.NewServeMux()
			mux.HandleFunc(candledata_worker.TaskChunk, h.HandleChunk)
			mux.HandleFunc(candledata_worker.TaskSweep, h.HandleSweep)

			if _, err := scheduler.Register("* * * * *", asynq.NewTask(candledata_worker.TaskSweep, nil),
				asynq.Queue(candledata_worker.QueueLive), asynq.MaxRetry(0),
				asynq.Unique(50*time.Second), asynq.Timeout(5*time.Minute)); err != nil {
				log.Printf("candledata: failed to register sweep cron entry: %v", err)
			}
			if err := server.Start(mux); err != nil {
				return err
			}
			log.Printf("candledata: reconciler started (concurrency=%d, weight budget=%d/min)", cfg.CandleData.Concurrency, cfg.CandleData.WeightBudget)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			server.Shutdown()
			return nil
		},
	})
}
