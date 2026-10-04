package agentplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/metrics_provider"
	"go-trade-bot/internal/report/agentreport"

	"gorm.io/gorm"
)

// ReportDataSource implements agentreport.DataSource directly over the
// existing backtest_runs / signals+orders / strategies / script_versions
// tables. It is read-only.
type ReportDataSource struct {
	db  *gorm.DB
	now func() time.Time
}

// NewReportDataSource builds a ReportDataSource.
func NewReportDataSource(db *gorm.DB) *ReportDataSource {
	return &ReportDataSource{db: db, now: time.Now}
}

var _ agentreport.DataSource = (*ReportDataSource)(nil)

// BacktestRun resolves metrics from the run's persisted columns, the equity
// curve from MetricsJSON and trades from TradeLogJSON.
func (s *ReportDataSource) BacktestRun(ctx context.Context, id uint) (agentreport.Series, error) {
	var run entities.BacktestRun
	if err := s.db.WithContext(ctx).First(&run, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return agentreport.Series{}, fmt.Errorf("backtest run %d not found", id)
		}
		return agentreport.Series{}, err
	}

	var trades []metrics_provider.TradeLogEntry
	if len(run.TradeLogJSON) > 0 {
		_ = json.Unmarshal(run.TradeLogJSON, &trades)
	}
	var m metrics_provider.BacktestMetrics
	if len(run.MetricsJSON) > 0 {
		_ = json.Unmarshal(run.MetricsJSON, &m)
	}

	out := agentreport.Series{
		Label: fmt.Sprintf("Backtest #%d · %s · %s → %s", run.ID, run.Symbol,
			run.StartDate.UTC().Format("2006-01-02"), run.EndDate.UTC().Format("2006-01-02")),
		Ref: &agentreport.SeriesRef{Kind: agentreport.SourceBacktestRun, ID: run.ID, Symbol: run.Symbol,
			Start: run.StartDate, End: run.EndDate},
		Metrics: agentreport.Metrics{
			Sharpe:           run.Sharpe,
			MaxDrawdown:      run.MaxDrawdownPct,
			MaxDrawdownIsPct: true,
			WinRatePct:       run.WinRatePct,
			ProfitFactor:     run.ProfitFactor,
			TotalTrades:      run.TotalTrades,
		},
	}
	if len(trades) > 0 {
		for _, t := range trades {
			out.Metrics.NetPnL += t.Profit
			out.Trades = append(out.Trades, agentreport.Trade{
				Symbol: t.Symbol, EntryTime: t.EntryTime, ExitTime: t.ExitTime, EntryPrice: t.EntryPrice,
				ExitPrice: t.ExitPrice, Quantity: t.Quantity, Profit: t.Profit, ExitReason: t.ExitReason,
			})
		}
	} else {
		out.Metrics.NetPnL = run.InitialCapital * run.TotalReturnPct / 100
	}
	for _, p := range m.EquityCurve {
		out.Equity = append(out.Equity, agentreport.EquityPoint{Time: p.Time, Value: p.Value})
	}
	return out, nil
}

// StrategyLive computes figures from the strategy's CLOSED signals whose
// last update falls in the trailing window. Each closed signal is one
// trade; its P&L is the sum of its orders' Profit. There is no capital base
// for live data, so max drawdown is an absolute quote-currency amount
// (peak-to-trough of cumulative realized P&L), and Sharpe is the per-trade
// Sharpe (mean/stddev of trade P&L * sqrt(n)) - labelled as such by the
// renderer's source label.
func (s *ReportDataSource) StrategyLive(ctx context.Context, strategyID uint, days int) (agentreport.Series, error) {
	if days <= 0 {
		days = 30
	}
	var strat entities.Strategy
	if err := s.db.WithContext(ctx).Select("id", "name").First(&strat, strategyID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return agentreport.Series{}, fmt.Errorf("strategy %d not found", strategyID)
		}
		return agentreport.Series{}, err
	}

	since := s.now().Add(-time.Duration(days) * 24 * time.Hour)
	var signals []entities.Signal
	if err := s.db.WithContext(ctx).Preload("Orders").
		Where("strategy_id = ? AND status = ? AND updated_at >= ?", strategyID, entities.Closed, since).
		Order("updated_at ASC").Limit(5000).Find(&signals).Error; err != nil {
		return agentreport.Series{}, err
	}

	out := agentreport.Series{
		Label: fmt.Sprintf("%s (strategy #%d) · live · last %d days · per-trade Sharpe, drawdown in quote currency", strat.Name, strategyID, days),
		Ref:   &agentreport.SeriesRef{Kind: agentreport.SourceStrategyLive, ID: strategyID, Name: strat.Name, Days: days},
	}
	var profits []float64
	var grossWin, grossLoss, equity, peak, maxDD float64
	wins := 0
	for _, sig := range signals {
		orders := append([]entities.Order(nil), sig.Orders...)
		sort.Slice(orders, func(i, j int) bool { return orders[i].CreatedAt.Before(orders[j].CreatedAt) })
		var profit, qty, entryPx, exitPx float64
		for _, o := range orders {
			profit += float64(o.Profit)
			if !o.IsClosing {
				qty += float64(o.Quantity)
				if entryPx == 0 {
					entryPx = float64(o.EntryPrice)
				}
			}
			if o.ExitPrice != 0 {
				exitPx = float64(o.ExitPrice)
			}
		}
		profits = append(profits, profit)
		if profit > 0 {
			wins++
			grossWin += profit
		} else if profit < 0 {
			grossLoss += -profit
		}
		equity += profit
		if equity > peak {
			peak = equity
		}
		maxDD = math.Max(maxDD, peak-equity)
		out.Equity = append(out.Equity, agentreport.EquityPoint{Time: sig.UpdatedAt, Value: equity})
		out.Trades = append(out.Trades, agentreport.Trade{
			Symbol: sig.Symbol, EntryTime: sig.CreatedAt, ExitTime: sig.UpdatedAt, EntryPrice: entryPx,
			ExitPrice: exitPx, Quantity: qty, Profit: profit, ExitReason: "closed",
		})
	}

	n := len(profits)
	out.Metrics = agentreport.Metrics{TotalTrades: n, NetPnL: equity, MaxDrawdown: maxDD}
	if n > 0 {
		out.Metrics.WinRatePct = float64(wins) / float64(n) * 100
		switch {
		case grossLoss > 0:
			out.Metrics.ProfitFactor = grossWin / grossLoss
		case grossWin > 0:
			out.Metrics.ProfitFactor = math.Inf(1)
		}
		mean := equity / float64(n)
		var variance float64
		for _, p := range profits {
			variance += (p - mean) * (p - mean)
		}
		if n > 1 {
			if sd := math.Sqrt(variance / float64(n-1)); sd > 0 {
				out.Metrics.Sharpe = mean / sd * math.Sqrt(float64(n))
			}
		}
	}
	return out, nil
}

// ScriptVersion returns a stored version's source, which must belong to
// strategyID.
func (s *ReportDataSource) ScriptVersion(ctx context.Context, strategyID, versionID uint) (string, error) {
	var v entities.ScriptVersion
	err := s.db.WithContext(ctx).Where("id = ? AND strategy_id = ?", versionID, strategyID).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("script version %d not found for strategy %d", versionID, strategyID)
	}
	return v.Source, err
}

// CurrentScript returns the strategy's current script source.
func (s *ReportDataSource) CurrentScript(ctx context.Context, strategyID uint) (string, error) {
	var strat entities.Strategy
	err := s.db.WithContext(ctx).Select("id", "script_source").First(&strat, strategyID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("strategy %d not found", strategyID)
	}
	return strat.ScriptSource, err
}

// PreviousScript returns the second-newest stored version (the newest is
// the current source, since every save records a version), or "".
func (s *ReportDataSource) PreviousScript(ctx context.Context, strategyID uint) (string, error) {
	var versions []entities.ScriptVersion
	if err := s.db.WithContext(ctx).Where("strategy_id = ?", strategyID).Order("id DESC").Limit(2).Find(&versions).Error; err != nil {
		return "", err
	}
	if len(versions) < 2 {
		return "", nil
	}
	return versions[1].Source, nil
}
