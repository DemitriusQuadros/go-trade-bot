package agentreport

import (
	"context"
	"fmt"
)

// memoData caches a DataSource's answers for one RenderLocales call, so the
// per-locale snapshots of a report resolve every reference once and show
// the same data. Not safe for concurrent use (RenderLocales is sequential).
type memoData struct {
	inner DataSource
	cache map[string]memoEntry
}

type memoEntry struct {
	series Series
	text   string
	err    error
}

func newMemoData(inner DataSource) *memoData {
	return &memoData{inner: inner, cache: map[string]memoEntry{}}
}

func (m *memoData) get(key string, load func() memoEntry) memoEntry {
	if e, ok := m.cache[key]; ok {
		return e
	}
	e := load()
	m.cache[key] = e
	return e
}

func (m *memoData) BacktestRun(ctx context.Context, id uint) (Series, error) {
	e := m.get(fmt.Sprintf("bt:%d", id), func() memoEntry {
		s, err := m.inner.BacktestRun(ctx, id)
		return memoEntry{series: s, err: err}
	})
	return e.series, e.err
}

func (m *memoData) StrategyLive(ctx context.Context, strategyID uint, days int) (Series, error) {
	e := m.get(fmt.Sprintf("live:%d:%d", strategyID, days), func() memoEntry {
		s, err := m.inner.StrategyLive(ctx, strategyID, days)
		return memoEntry{series: s, err: err}
	})
	return e.series, e.err
}

func (m *memoData) ScriptVersion(ctx context.Context, strategyID, versionID uint) (string, error) {
	e := m.get(fmt.Sprintf("ver:%d:%d", strategyID, versionID), func() memoEntry {
		s, err := m.inner.ScriptVersion(ctx, strategyID, versionID)
		return memoEntry{text: s, err: err}
	})
	return e.text, e.err
}

func (m *memoData) CurrentScript(ctx context.Context, strategyID uint) (string, error) {
	e := m.get(fmt.Sprintf("cur:%d", strategyID), func() memoEntry {
		s, err := m.inner.CurrentScript(ctx, strategyID)
		return memoEntry{text: s, err: err}
	})
	return e.text, e.err
}

func (m *memoData) PreviousScript(ctx context.Context, strategyID uint) (string, error) {
	e := m.get(fmt.Sprintf("prev:%d", strategyID), func() memoEntry {
		s, err := m.inner.PreviousScript(ctx, strategyID)
		return memoEntry{text: s, err: err}
	})
	return e.text, e.err
}
