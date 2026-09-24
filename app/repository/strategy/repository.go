package repository

import (
	"context"
	"go-trade-bot/app/entities"
	"time"

	"gorm.io/gorm"
)

type StrategyRepository struct {
	db *gorm.DB
}

func NewStrategyRepository(db *gorm.DB) StrategyRepository {
	return StrategyRepository{
		db: db,
	}
}

func (r StrategyRepository) Save(ctx context.Context, strategy entities.Strategy) (entities.Strategy, error) {
	err := r.db.WithContext(ctx).Create(&strategy).Error
	return strategy, err
}

func (r StrategyRepository) GetByID(ctx context.Context, id uint) (entities.Strategy, error) {
	var strategy entities.Strategy
	err := r.db.WithContext(ctx).First(&strategy, id).Error
	return strategy, err
}

func (r StrategyRepository) GetAll(ctx context.Context) ([]entities.Strategy, error) {
	var strategies []entities.Strategy
	err := r.db.WithContext(ctx).Find(&strategies).Error
	return strategies, err
}

// Update saves an existing strategy row. CreatedAt is deliberately omitted:
// the incoming entities.Strategy always carries a zero CreatedAt (the
// PUT /strategy/{id} DTO has no created_at field - callers only ever set
// name/description/script_source/etc), and GORM's Save() on a struct with a
// non-zero primary key issues a full-column UPDATE, including zero-valued
// fields, unless told otherwise. Without this Omit, every single edit
// silently wiped the row's real creation date to 0001-01-01.
func (r StrategyRepository) Update(ctx context.Context, strategy entities.Strategy) error {
	return r.db.WithContext(ctx).Omit("CreatedAt").Save(&strategy).Error
}

// Delete removes a strategy and every row that references it, in one
// transaction - a hard delete, not a soft/flagged one (no entity in this
// project has a DeletedAt column), matching the "keep the database small"
// requirement this cascade was built for. Order rows have no direct
// StrategyID column (only SignalID), so they're found via the strategy's
// signal IDs first. AgentRun rows are included deliberately: the AI
// copilot's chat history for a strategy has no reason to outlive the
// strategy itself, and would otherwise accumulate as orphaned rows
// referencing a StrategyID that no longer resolves to anything (Backend
// Spec 02's StrategyID is a plain uint, not a DB foreign key, so nothing
// would even complain about the dangling reference if this were skipped).
//
// Deliberately NOT wrapped in the safety guard (Status/open-signals check) -
// that's app/usecase/strategy.StrategyUseCase.Delete's job; this method
// trusts its caller and only handles "make it all go away, correctly and
// atomically."
func (r StrategyRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var signalIDs []uint
		if err := tx.Model(&entities.Signal{}).Where("strategy_id = ?", id).Pluck("id", &signalIDs).Error; err != nil {
			return err
		}
		if len(signalIDs) > 0 {
			if err := tx.Where("signal_id IN ?", signalIDs).Delete(&entities.Order{}).Error; err != nil {
				return err
			}
		}

		for _, del := range []func(*gorm.DB) *gorm.DB{
			func(tx *gorm.DB) *gorm.DB { return tx.Where("strategy_id = ?", id).Delete(&entities.Signal{}) },
			func(tx *gorm.DB) *gorm.DB { return tx.Where("strategy_id = ?", id).Delete(&entities.StrategyExecution{}) },
			func(tx *gorm.DB) *gorm.DB { return tx.Where("strategy_id = ?", id).Delete(&entities.BacktestRun{}) },
			func(tx *gorm.DB) *gorm.DB { return tx.Where("strategy_id = ?", id).Delete(&entities.OptimizationRun{}) },
			func(tx *gorm.DB) *gorm.DB {
				return tx.Where("strategy_id = ?", id).Delete(&entities.StrategyPerformanceSnapshot{})
			},
			func(tx *gorm.DB) *gorm.DB { return tx.Where("strategy_id = ?", id).Delete(&entities.ScriptState{}) },
			func(tx *gorm.DB) *gorm.DB { return tx.Where("strategy_id = ?", id).Delete(&entities.ScriptVersion{}) },
			func(tx *gorm.DB) *gorm.DB { return tx.Where("strategy_id = ?", id).Delete(&entities.AgentRun{}) },
		} {
			if err := del(tx).Error; err != nil {
				return err
			}
		}

		return tx.Delete(&entities.Strategy{}, id).Error
	})
}

func (r StrategyRepository) SaveExecution(ctx context.Context, execution entities.StrategyExecution) error {
	return r.db.WithContext(ctx).Create(&execution).Error
}

func (r StrategyRepository) CountOpenSignals(ctx context.Context, strategy entities.Strategy) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entities.Signal{}).
		Where("strategy_id = ? AND status = ?", strategy.ID, entities.Open).
		Count(&count).Error
	return count, err
}

func (r StrategyRepository) GetStrategyPerformanceBySymbol(ctx context.Context) []entities.StrategyPerformance {
	var performances []entities.StrategyPerformance
	r.db.WithContext(ctx).Raw(`
			select st.name Name, s.symbol Symbol, coalesce(sum(profit),0) Profit, count(*) Trades
			from orders o
			join signals s on o.signal_id  = s.id 
			join strategies st  on st.id = s.strategy_id 
			group by st.name, s.symbol
			order by profit desc
		`).Scan(&performances)

	return performances
}

// GetPerformanceInRange mirrors GetStrategyPerformanceBySymbol's existing
// join/aggregate query, with a WHERE o.updated_at (the close time - Order
// rows use UpdatedAt as their "closed at" timestamp today, per
// GenerateSellSignal setting Orders[0].UpdatedAt at close time) filter added
// - backend-05's daily snapshot job's data source. Unlike the all-time
// method, this one also selects st.id so the caller can persist a
// StrategyPerformanceSnapshot row keyed by StrategyID, not just strategy
// name.
func (r StrategyRepository) GetPerformanceInRange(ctx context.Context, from, to time.Time) ([]entities.StrategyPerformance, error) {
	var performances []entities.StrategyPerformance
	err := r.db.WithContext(ctx).Raw(`
			select st.id strategy_id, st.name Name, s.symbol Symbol, coalesce(sum(profit),0) Profit, count(*) Trades
			from orders o
			join signals s on o.signal_id = s.id
			join strategies st on st.id = s.strategy_id
			where o.updated_at >= ? and o.updated_at < ?
			group by st.id, st.name, s.symbol
			order by profit desc
		`, from, to).Scan(&performances).Error

	return performances, err
}

func (r StrategyRepository) SaveScriptVersion(ctx context.Context, v entities.ScriptVersion) error {
	return r.db.WithContext(ctx).Create(&v).Error
}

func (r StrategyRepository) GetScriptVersions(ctx context.Context, strategyID uint) ([]entities.ScriptVersion, error) {
	var versions []entities.ScriptVersion
	err := r.db.WithContext(ctx).Where("strategy_id = ?", strategyID).Order("created_at desc").Find(&versions).Error
	return versions, err
}
