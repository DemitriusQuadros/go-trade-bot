package script

import (
	"context"

	strategyscript "go-trade-bot/app/strategies/script"
)

// inMemoryStateStore is a per-request, map-backed ScriptStateStore used by
// FastRerun (backend-08). It never touches the DB - a fast-rerun of an
// operator's unsaved editor text must not read or clobber the persisted
// script_state of the real strategy row it is previewing (AC#6).
type inMemoryStateStore struct {
	state map[string]map[string]interface{}
}

func newInMemoryStateStore() strategyscript.ScriptStateStore {
	return &inMemoryStateStore{state: map[string]map[string]interface{}{}}
}

func (s *inMemoryStateStore) key(strategyID uint, symbol string) string {
	return symbol
}

func (s *inMemoryStateStore) Load(_ context.Context, strategyID uint, symbol string) (map[string]interface{}, error) {
	if existing, ok := s.state[s.key(strategyID, symbol)]; ok {
		return existing, nil
	}
	return map[string]interface{}{}, nil
}

func (s *inMemoryStateStore) Save(_ context.Context, strategyID uint, symbol string, state map[string]interface{}) error {
	s.state[s.key(strategyID, symbol)] = state
	return nil
}
