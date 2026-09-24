package script

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go-trade-bot/app/entities"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// maxStateJSONBytes is the soft size cap (blueprint §5) - a script.state
// table serialized to JSON larger than this is rejected at Save time, not
// silently truncated. Bounds one of the two "cheap to grow unboundedly"
// surfaces a script controls (the other is the fixed candleWindow cap on
// ctx.candles, which the script cannot influence at all).
const maxStateJSONBytes = 64 * 1024

// ScriptStateStore is the narrow interface ScriptStrategy (backend-04)
// depends on - deliberately NOT the full scriptstate.Repository interface,
// so ScriptStrategy's own tests can mock exactly this surface without
// pulling in GORM.
type ScriptStateStore interface {
	Load(ctx context.Context, strategyID uint, symbol string) (map[string]interface{}, error)
	Save(ctx context.Context, strategyID uint, symbol string, state map[string]interface{}) error
}

// scriptstateRepo is the narrow (Get/Upsert-only) slice of
// scriptstate.Repository this package needs. Kept local so the package does
// not have to import the concrete repository at construction - the fx
// interface-adapter (cmd/*/modules/script.go) bridges the concrete repo to
// this interface.
type scriptstateRepo interface {
	Get(ctx context.Context, strategyID uint, symbol string) (entities.ScriptState, error)
	Upsert(ctx context.Context, state entities.ScriptState) error
}

type dbScriptStateStore struct {
	repo scriptstateRepo
}

// NewScriptStateStore wraps a scriptstate repository behind the narrow
// ScriptStateStore interface.
func NewScriptStateStore(repo scriptstateRepo) ScriptStateStore {
	return &dbScriptStateStore{repo: repo}
}

func (s *dbScriptStateStore) Load(ctx context.Context, strategyID uint, symbol string) (map[string]interface{}, error) {
	row, err := s.repo.Get(ctx, strategyID, symbol)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return map[string]interface{}{}, nil // first cycle - empty state, not an error
		}
		return nil, fmt.Errorf("script state load failed for strategy %d/%s: %w", strategyID, symbol, err)
	}
	state := map[string]interface{}{}
	if len(row.StateJSON) > 0 {
		if err := json.Unmarshal(row.StateJSON, &state); err != nil {
			return nil, fmt.Errorf("script state unmarshal failed for strategy %d/%s: %w", strategyID, symbol, err)
		}
	}
	return state, nil
}

func (s *dbScriptStateStore) Save(ctx context.Context, strategyID uint, symbol string, state map[string]interface{}) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("script state marshal failed for strategy %d/%s: %w", strategyID, symbol, err)
	}
	if len(raw) > maxStateJSONBytes {
		return fmt.Errorf("script state for strategy %d/%s exceeds %d byte cap (got %d bytes) - reduce state size",
			strategyID, symbol, maxStateJSONBytes, len(raw))
	}
	return s.repo.Upsert(ctx, entities.ScriptState{
		StrategyID: strategyID,
		Symbol:     symbol,
		StateJSON:  datatypes.JSON(raw),
	})
}
