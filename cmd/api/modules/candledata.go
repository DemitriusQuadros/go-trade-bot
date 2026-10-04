package modules

import (
	"os"

	candle_repo "go-trade-bot/app/repository/candle"
	candledata_repo "go-trade-bot/app/repository/candledata"
	usecase "go-trade-bot/app/usecase/candledata"
	candledata_worker "go-trade-bot/app/workers/candledata"
	"go-trade-bot/internal/candlesource"
	"go-trade-bot/internal/exchange"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// CandleDataModule provides the candle-dataset management service for the REST
// API. The API only plans and enqueues (and adopts existing candles on create);
// chunks are executed by cmd/worker, so the service has no source runner here.
var CandleDataModule = fx.Module("candledata",
	fx.Provide(
		candledata_repo.NewRepository,
		candledata_worker.NewEnqueuer,
		NewCandleDataService,
	),
)

func NewCandleDataService(
	store candledata_repo.Repository,
	enq *candledata_worker.Enqueuer,
	client exchange.ExchangeClient,
	db *gorm.DB,
) *usecase.Service {
	fetcher, _ := client.(exchange.HistoricalKlineFetcher)
	host, _ := os.Hostname()
	svc := usecase.NewService(store, nil, candlesource.NewListingResolver(nil, fetcher), enq, "api-"+host)
	svc.Scanner = candle_repo.NewCandleRepository(db)
	return svc
}
