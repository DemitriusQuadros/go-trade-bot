package candleimport

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	"go-trade-bot/app/repository/candleimport"
	worker "go-trade-bot/app/workers/candleimport"
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

	client exchange.ExchangeClient
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
			importedCount, gapsCount, err := u.importSymbolTimeframe(ctx, symbol, tf, req.From, req.To)
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
		var klines []exchange.Candle
		var fetchErr error

		for retries := 0; retries < 3; retries++ {
			klines, fetchErr = u.client.ListKline(ctx, symbol, timeframe, batchLimit)
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
			return totalImported, 0, fmt.Errorf("ListKline failed for %s/%s: %w", symbol, timeframe, fetchErr)
		}

		if len(klines) == 0 {
			break
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

	storedCandles, err := u.repo.Range(ctx, symbol, timeframe, from, to)
	if err != nil {
		return totalImported, 0, nil
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

	return totalImported, gapsDetected, nil
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
