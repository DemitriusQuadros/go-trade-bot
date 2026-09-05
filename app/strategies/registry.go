package strategies

import (
	"fmt"
	"sort"
	"sync"
)

// StrategyFactory builds a fresh Strategy instance. Each ported strategy
// package self-registers a factory from its init(), matching the PRD's
// exact pattern (SS5).
type StrategyFactory func() Strategy

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

// Get resolves a strategy by name. Returns (nil, false) if not found -
// callers (the engine, validation logic) must handle the false case
// explicitly; Get never panics.
func Get(name string) (Strategy, bool) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := registry[name]
	if !ok {
		return nil, false
	}
	return factory(), true
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
