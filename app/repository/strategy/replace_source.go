package repository

import (
	"context"
	"errors"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrSourceChanged: the strategy's script_source no longer equals the
// proposal's base source (someone changed it since the proposal was made).
var ErrSourceChanged = errors.New("strategy script source changed since the proposal was created")

// ErrOpenPosition: the strategy has an open signal (not flat).
var ErrOpenPosition = errors.New("strategy has an open position")

// ReplaceResult reports what ReplaceScriptSource did besides the write.
type ReplaceResult struct {
	// StateCleared is true when the strategy's ScriptState rows were
	// deleted (only ever when it was flat at write time).
	StateCleared bool
}

// ReplaceScriptSource is the ONE write path for applying an operator-
// approved StrategyChangeProposal (agents-platform Phase B-01 §5). It is
// called only by app/usecase/proposal.Applier from cmd/agent's
// agent:apply_proposal task - never by any LLM tool (app/usecase/agent has
// no dependency on this package; a test asserts it).
//
// Inside one transaction it:
//   - re-reads the row (SELECT ... FOR UPDATE on Postgres) and requires its
//     script_source to still equal expectedBase (else ErrSourceChanged);
//   - counts open signals, and if requireFlat refuses with ErrOpenPosition;
//   - updates ONLY script_source and updated_at - never mode or status, so
//     a live/productive strategy stays exactly live/productive;
//   - writes a ScriptVersion for the new source;
//   - when the strategy is flat, deletes its ScriptState rows. That is safe
//     only because no position is open: persisted script state (e.g. an
//     entry bar counter or trailing-stop level) belongs to the OLD code's
//     position bookkeeping and would be misread by the new code; with no
//     position there is nothing for it to describe. With an open position
//     (only possible when requireFlat is false - non-live targets) the
//     state is kept so the new code can still see it.
//
// Residual risk (documented, accepted by the spec): the trading worker can
// open a position between the caller's flat check and this write. The
// transactional re-check narrows that window to the transaction itself;
// the caller additionally holds the strategy-cycle:<id> Redis lock the
// worker takes around every cycle, so a cycle and an apply never overlap.
// If a position is nonetheless opened by the old code, the new code manages
// it from its next cycle.
func (r StrategyRepository) ReplaceScriptSource(ctx context.Context, id uint, expectedBase, newSource string, requireFlat bool) (ReplaceResult, error) {
	var result ReplaceResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var current entities.Strategy
		if err := q.Select("id", "script_source").First(&current, id).Error; err != nil {
			return err
		}
		if current.ScriptSource != expectedBase {
			return ErrSourceChanged
		}

		var open int64
		if err := tx.Model(&entities.Signal{}).
			Where("strategy_id = ? AND status = ?", id, entities.Open).
			Count(&open).Error; err != nil {
			return err
		}
		if requireFlat && open > 0 {
			return ErrOpenPosition
		}

		now := time.Now()
		res := tx.Model(&entities.Strategy{}).
			Where("id = ? AND script_source = ?", id, expectedBase).
			Updates(map[string]any{"script_source": newSource, "updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrSourceChanged
		}

		if err := tx.Create(&entities.ScriptVersion{StrategyID: id, Source: newSource, CreatedAt: now}).Error; err != nil {
			return err
		}

		if open == 0 {
			if err := tx.Where("strategy_id = ?", id).Delete(&entities.ScriptState{}).Error; err != nil {
				return err
			}
			result.StateCleared = true
		}
		return nil
	})
	if err != nil {
		return ReplaceResult{}, err
	}
	return result, nil
}
