package strategies

import (
	"fmt"
	"sort"
	"sync"

	"go-trade-bot/app/entities"
)

// StrategyFactory builds a fresh Strategy instance for a specific persisted
// strategy row. The dbStrategy argument (backend-04) lets a factory read
// per-strategy persisted fields it needs to construct the instance - the
// script strategy reads dbStrategy.ScriptSource/ID/Name; native Go strategies
// (grid/bollinger/scalping/mlgrpc) ignore it. Each ported strategy package
// self-registers a factory from its init() (or, for factories needing
// fx-constructed dependencies, via an explicit Register call at startup).
type StrategyFactory func(dbStrategy entities.Strategy) Strategy

var (
	mu       sync.RWMutex
	registry = map[string]StrategyFactory{}
)

// Register is called from each strategy package's init(). Panics on a
// duplicate name: a silent overwrite would mean one strategy is unreachable
// with no error, which is worse for a solo maintainer than a startup crash
// pointing directly at the bug (Spec 06 AC#4).
func Register(name string, factory StrategyFactory) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("strategies: duplicate registration for %q", name))
	}
	registry[name] = factory
}

// Get resolves a strategy by name, constructing a fresh instance for the
// given persisted strategy row. Returns (nil, false) if not found - callers
// (the engine, validation logic) must handle the false case explicitly; Get
// never panics.
func Get(name string, dbStrategy entities.Strategy) (Strategy, bool) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := registry[name]
	if !ok {
		return nil, false
	}
	return factory(dbStrategy), true
}

// Exists reports whether name is registered, without constructing an
// instance (avoids the throwaway allocation Get's construct-to-check pattern
// would otherwise cost on every validation call).
func Exists(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := registry[name]
	return ok
}

// Names returns all registered strategy names, sorted, for validation error
// messages and (Phase 3) the TUI's strategy selector list.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// resetForTest clears the registry. Test-only helper (unexported) so
// registry_test.go can verify Register's duplicate-panic and Get's
// not-found behavior without cross-test pollution or depending on the real
// grid/bollinger/scalping packages being imported.
func resetForTest() {
	mu.Lock()
	defer mu.Unlock()
	registry = map[string]StrategyFactory{}
}
