package entities

// StrategyPerformance is a live-computed, unbounded all-time aggregate view
// (never persisted) - the all-time answer to "how is this strategy doing".
// backend-05 adds StrategyPerformanceSnapshot (see strategyperformancesnapshot.go)
// as the orthogonal, actually-persisted, time-bucketed history capability;
// this type is unchanged in purpose.
//
// StrategyID is additive (backend-05): the pre-existing
// GetStrategyPerformanceBySymbol query does not select it, so it is left as
// its zero value there (GORM's Scan silently leaves unmatched struct fields
// at zero rather than erroring) - only the new GetPerformanceInRange query
// populates it, since backend-05's snapshot job needs the strategy's ID
// (not just its name) to persist a StrategyPerformanceSnapshot row.
type StrategyPerformance struct {
	StrategyID uint    `json:"strategy_id"`
	Name       string  `json:"name"`
	Symbol     string  `json:"symbol"`
	Profit     float64 `json:"profit"`
	Trades     int     `json:"trades"`
}
