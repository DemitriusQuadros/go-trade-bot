package modules

import (
	"fmt"
	"log"
	"os"

	candledata_tasks "go-trade-bot/app/handler/tasks/candledata"
	candle_repo "go-trade-bot/app/repository/candle"
	candledata_repo "go-trade-bot/app/repository/candledata"
	usecase "go-trade-bot/app/usecase/candledata"
	candledata_worker "go-trade-bot/app/workers/candledata"
	"go-trade-bot/internal/candlesource"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/weightlimit"

	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

// CandleDataModule wires the candle-dataset reconciler (docs/specs/candle-data).
// Task registration, the dedicated asynq server and the sweep cron are in
// cmd/worker/candledata.go.
var CandleDataModule = fx.Module("candledata",
	fx.Provide(
		candledata_repo.NewRepository,
		candledata_worker.NewEnqueuer,
		NewCandleDataService,
		candledata_tasks.NewTaskHandler,
	),
)

// NewCandleDataService assembles the source chain archive_monthly ->
// archive_daily -> rest. The REST source needs the exchange client to implement
// exchange.HistoricalKlineFetcher (the swappable/real adapters do); without it
// the chain simply has no REST fallback.
func NewCandleDataService(
	cfg *configuration.Configuration,
	store candledata_repo.Repository,
	candles candle_repo.Repository,
	client exchange.ExchangeClient,
	enq *candledata_worker.Enqueuer,
	collector *metrics.MetricsCollector,
) *usecase.Service {
	fetcher, _ := client.(exchange.HistoricalKlineFetcher)
	if fetcher == nil {
		log.Printf("candledata: exchange client %T has no ListKlineRange; REST fallback disabled", client)
	}
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr})
	limiter := weightlimit.NewWeightLimiter(weightlimit.NewRedisStore(rdb), cfg.CandleData.WeightBudget)

	sources := []candlesource.Source{
		candlesource.NewArchiveSource(false, nil),
		candlesource.NewArchiveSource(true, nil),
		candlesource.NewRESTSource(fetcher, limiter),
	}
	runner := usecase.NewRunner(sources, candles)
	host, _ := os.Hostname()
	svc := usecase.NewService(store, runner, candlesource.NewListingResolver(nil, fetcher), enq, fmt.Sprintf("%s-%d", host, os.Getpid()))
	svc.Metrics = candleMetrics{collector}
	return svc
}

// candleMetrics adapts the process MetricsCollector to candledata.Metrics.
type candleMetrics struct{ c *metrics.MetricsCollector }

func (m candleMetrics) Inc(n string, l map[string]string) { m.c.IncrementCounter(n, l) }
func (m candleMetrics) Set(n string, l map[string]string, v float64) {
	m.c.SetGauge(n, l, v)
}
func (m candleMetrics) Observe(n string, l map[string]string, v float64) {
	m.c.ObserveHistogram(n, l, v)
}
