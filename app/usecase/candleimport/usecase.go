package candleimport

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	"go-trade-bot/app/repository/candleimport"
	worker "go-trade-bot/app/workers/candleimport"
	"go-trade-bot/internal/binancearchive"
	"go-trade-bot/internal/exchange"
)

type UseCase interface {
	Run(ctx context.Context, req entities.ImportRequest) ([]entities.ImportSummary, error)

	CreateJob(ctx context.Context, req entities.ImportRequest) (string, error)
	GetJob(ctx context.Context, id string) (*entities.ImportJob, error)
	CreateSchedule(ctx context.Context, sched *entities.ImportSchedule) error
	ListSchedules(ctx context.Context) ([]entities.ImportSchedule, error)
	UpdateSchedule(ctx context.Context, sched *entities.ImportSchedule) error
	DeleteSchedule(ctx context.Context, id uint) error
}

type candleImportUseCase struct {
	repo       candle.Repository
	importRepo candleimport.Repository
	worker     worker.Worker

	client     exchange.ExchangeClient
	httpClient *http.Client // used only by the ImportSourceArchive path (data.binance.vision)
}

// NewCandleImportUseCase depends on candle.Repository (the interface), not
// candle.CandleRepository (the concrete struct) - the fx graph only provides
// the interface (cmd/{api,worker}/modules/candle.go's CandleModule), and
// every other dependency here is already an interface. Depending on the
// concrete struct type was a startup-time fx wiring bug: "missing type:
// candle.CandleRepository" since fx does not treat "provides an interface"
// as satisfying "wants the concrete type" without an explicit fx.As, and
// nothing provided the concrete type either.
func NewCandleImportUseCase(repo candle.Repository, importRepo candleimport.Repository, worker worker.Worker, client exchange.ExchangeClient) UseCase {
	return &candleImportUseCase{
		repo:       repo,
		importRepo: importRepo,
		worker:     worker,
		client:     client,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (u *candleImportUseCase) Run(ctx context.Context, req entities.ImportRequest) ([]entities.ImportSummary, error) {
	if req.To.Before(req.From) {
		return nil, fmt.Errorf("to (%v) cannot be before from (%v)", req.To, req.From)
	}

	if req.MinHistory > 0 && req.To.Sub(req.From) < req.MinHistory {
		log.Printf("[WARN] requested range (%v) is shorter than min-history threshold (%v)", req.To.Sub(req.From), req.MinHistory)
	}

	var summaries []entities.ImportSummary
	var lastErr error

	for _, rawSymbol := range req.Symbols {
		symbol := strings.TrimSpace(rawSymbol)
		if symbol == "" {
			continue
		}

		for _, rawTf := range req.Timeframes {
			tf := strings.TrimSpace(rawTf)
			if tf == "" {
				continue
			}

			start := time.Now()
			var importedCount, gapsCount int
			var err error
			if req.Source == entities.ImportSourceArchive {
				importedCount, gapsCount, err = u.importSymbolTimeframeFromArchive(ctx, symbol, tf, req.From, req.To)
			} else {
				importedCount, gapsCount, err = u.importSymbolTimeframe(ctx, symbol, tf, req.From, req.To)
			}
			duration := time.Since(start).Seconds()

			if err != nil {
				log.Printf("[ERROR] failed import for %s/%s: %v", symbol, tf, err)
				lastErr = err
				continue
			}

			summaries = append(summaries, entities.ImportSummary{
				Symbol:          symbol,
				Timeframe:       tf,
				CandlesImported: importedCount,
				DurationSeconds: duration,
				GapsDetected:    gapsCount,
			})
		}
	}

	return summaries, lastErr
}

func (u *candleImportUseCase) importSymbolTimeframe(
	ctx context.Context,
	symbol, timeframe string,
	from, to time.Time,
) (int, int, error) {
	tfDuration, err := parseTimeframeDuration(timeframe)
	if err != nil {
		return 0, 0, err
	}

	// u.client (exchange.ExchangeClient) must ALSO implement
	// exchange.HistoricalKlineFetcher for a historical backfill to work at
	// all - see that interface's doc comment for why it's a separate,
	// narrower capability rather than folded into ExchangeClient itself.
	// Both real paths (the CLI's raw *BinanceAdapter and the fx-wired
	// *SwappableExchangeClient used by the web-triggered async job) satisfy
	// it; failing fast here with a clear message beats the previous silent
	// bug where ListKline(no start/end) quietly returned only the most
	// recent `limit` candles and the pagination loop's own bookkeeping
	// never caught that its fetch wasn't advancing.
	fetcher, ok := u.client.(exchange.HistoricalKlineFetcher)
	if !ok {
		return 0, 0, fmt.Errorf("exchange client (%T) does not support historical range fetches (ListKlineRange) required for candle import", u.client)
	}

	latestTime, err := u.repo.LatestOpenTime(ctx, symbol, timeframe)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to check latest open time: %w", err)
	}

	actualFrom := from
	if !latestTime.IsZero() && latestTime.After(from) {
		overlap := latestTime.Add(-10 * tfDuration)
		if overlap.After(from) {
			actualFrom = overlap
		}
	}

	totalImported := 0
	currentStart := actualFrom
	const batchLimit = 1000

	for currentStart.Before(to) {
		// Bound each request's end to `to` (or a full batch's worth of
		// duration, whichever is sooner) - this is what actually makes
		// currentStart's advancement below meaningful. Previously the fetch
		// ignored currentStart/batchEnd entirely and always asked for "the
		// most recent `limit` candles", so pagination past the first page
		// never happened no matter how the loop's bookkeeping advanced.
		batchEnd := currentStart.Add(time.Duration(batchLimit) * tfDuration)
		if batchEnd.After(to) {
			batchEnd = to
		}

		var klines []exchange.Candle
		var fetchErr error

		for retries := 0; retries < 3; retries++ {
			klines, fetchErr = fetcher.ListKlineRange(ctx, symbol, timeframe, currentStart, batchEnd, batchLimit)
			if fetchErr != nil {
				if strings.Contains(fetchErr.Error(), "429") || strings.Contains(fetchErr.Error(), "Rate limit") {
					log.Printf("[WARN] rate limited for %s/%s, backing off 5s...", symbol, timeframe)
					time.Sleep(5 * time.Second)
					continue
				}
				break
			}
			break
		}

		if fetchErr != nil {
			return totalImported, 0, fmt.Errorf("ListKlineRange failed for %s/%s: %w", symbol, timeframe, fetchErr)
		}

		if len(klines) == 0 {
			// No candles in [currentStart, batchEnd) - could be a real gap
			// (exchange downtime, symbol not yet listed) rather than "done",
			// so advance past this empty window instead of stopping, same
			// as the non-empty branch below would via maxOpenTime.
			currentStart = batchEnd
			continue
		}

		var entityCandles []entities.Candle
		var maxOpenTime time.Time

		for _, k := range klines {
			if k.OpenTime.Before(currentStart) {
				continue
			}
			if k.OpenTime.After(to) || k.OpenTime.Equal(to) {
				continue
			}

			entityCandles = append(entityCandles, entities.Candle{
				Symbol:    symbol,
				Timeframe: timeframe,
				OpenTime:  k.OpenTime,
				Open:      k.Open,
				High:      k.High,
				Low:       k.Low,
				Close:     k.Close,
				Volume:    k.Volume,
			})

			if k.OpenTime.After(maxOpenTime) {
				maxOpenTime = k.OpenTime
			}
		}

		if len(entityCandles) > 0 {
			if err := u.repo.Upsert(ctx, entityCandles); err != nil {
				return totalImported, 0, fmt.Errorf("upsert failed for %s/%s: %w", symbol, timeframe, err)
			}
			totalImported += len(entityCandles)
		}

		if maxOpenTime.IsZero() || maxOpenTime.Before(currentStart) {
			currentStart = currentStart.Add(time.Duration(batchLimit) * tfDuration)
		} else {
			currentStart = maxOpenTime.Add(tfDuration)
		}
	}

	gapsDetected := u.detectGaps(ctx, symbol, timeframe, from, to, tfDuration)
	return totalImported, gapsDetected, nil
}

// importSymbolTimeframeFromArchive bulk-imports from Binance's public
// data.binance.vision monthly kline archive instead of the live REST API -
// see entities.ImportSourceArchive's doc comment. Granularity is whole
// months: `from`/`to` are walked as month boundaries regardless of their
// time-of-day, since that's the archive's own granularity.
func (u *candleImportUseCase) importSymbolTimeframeFromArchive(
	ctx context.Context,
	symbol, timeframe string,
	from, to time.Time,
) (int, int, error) {
	tfDuration, err := parseTimeframeDuration(timeframe)
	if err != nil {
		return 0, 0, err
	}

	totalImported := 0
	fromMonth := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	toMonth := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)

	for month := fromMonth; !month.After(toMonth); month = month.AddDate(0, 1, 0) {
		candles, err := binancearchive.FetchMonth(ctx, u.httpClient, symbol, timeframe, month)
		if err != nil {
			return totalImported, 0, fmt.Errorf("archive fetch failed for %s/%s %s: %w", symbol, timeframe, month.Format("2006-01"), err)
		}
		if len(candles) == 0 {
			continue
		}
		if err := u.repo.Upsert(ctx, candles); err != nil {
			return totalImported, 0, fmt.Errorf("upsert failed for %s/%s %s: %w", symbol, timeframe, month.Format("2006-01"), err)
		}
		totalImported += len(candles)
	}

	gapsDetected := u.detectGaps(ctx, symbol, timeframe, from, to, tfDuration)
	return totalImported, gapsDetected, nil
}

// detectGaps checks stored candles for the (symbol, timeframe) in [from, to)
// for open-time gaps wider than one candle's duration. Shared by both the
// REST and archive import paths, which otherwise fetch identically-shaped
// entities.Candle rows through completely different mechanisms - gap
// detection just reads back whatever ended up in the DB, so it doesn't care
// which path put it there. Errors are logged, not propagated: a failed gap
// check shouldn't fail an otherwise-successful import.
func (u *candleImportUseCase) detectGaps(ctx context.Context, symbol, timeframe string, from, to time.Time, tfDuration time.Duration) int {
	storedCandles, err := u.repo.Range(ctx, symbol, timeframe, from, to)
	if err != nil {
		log.Printf("[WARN] gap check failed for %s/%s: %v", symbol, timeframe, err)
		return 0
	}

	gapsDetected := 0
	for i := 0; i < len(storedCandles)-1; i++ {
		diff := storedCandles[i+1].OpenTime.Sub(storedCandles[i].OpenTime)
		if diff > tfDuration {
			gapsDetected++
			log.Printf("[WARN] Gap detected for %s/%s between %v and %v (gap: %v)",
				symbol, timeframe, storedCandles[i].OpenTime, storedCandles[i+1].OpenTime, diff)
		}
	}
	return gapsDetected
}

func parseTimeframeDuration(tf string) (time.Duration, error) {
	switch tf {
	case "1m":
		return time.Minute, nil
	case "3m":
		return 3 * time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "30m":
		return 30 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "2h":
		return 2 * time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "8h":
		return 8 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	case "3d":
		return 3 * 24 * time.Hour, nil
	case "1w":
		return 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe %q", tf)
	}
}

func (u *candleImportUseCase) CreateJob(ctx context.Context, req entities.ImportRequest) (string, error) {
	jobID := fmt.Sprintf("candleimport:%d", time.Now().UnixNano())
	job := &entities.ImportJob{
		ID:        jobID,
		Status:    entities.ImportJobPending,
		CreatedAt: time.Now(),
	}
	if err := u.importRepo.CreateJob(ctx, job); err != nil {
		return "", err
	}
	if err := u.worker.EnqueueImport(ctx, jobID, req); err != nil {
		return "", err
	}
	return jobID, nil
}

func (u *candleImportUseCase) GetJob(ctx context.Context, id string) (*entities.ImportJob, error) {
	return u.importRepo.GetJob(ctx, id)
}

func (u *candleImportUseCase) CreateSchedule(ctx context.Context, sched *entities.ImportSchedule) error {
	sched.CreatedAt = time.Now()
	return u.importRepo.CreateSchedule(ctx, sched)
}

func (u *candleImportUseCase) ListSchedules(ctx context.Context) ([]entities.ImportSchedule, error) {
	return u.importRepo.ListSchedules(ctx)
}

func (u *candleImportUseCase) UpdateSchedule(ctx context.Context, sched *entities.ImportSchedule) error {
	return u.importRepo.UpdateSchedule(ctx, sched)
}

func (u *candleImportUseCase) DeleteSchedule(ctx context.Context, id uint) error {
	return u.importRepo.DeleteSchedule(ctx, id)
}
