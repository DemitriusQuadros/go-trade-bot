package strategies

// Engine-owned Context.Config keys. These are populated by app/engine before
// any hook is called, never by a strategy or by user-authored strategy JSON
// - the leading underscore keeps them visually distinct from user config
// keys (e.g. "stop_loss_pct"). Exported here (not just as unexported
// constants inside app/engine) so a strategy package that needs one (Phase 1:
// only app/strategies/grid) doesn't hardcode a duplicate string literal
// across the package boundary.
//
// Per ADR-005, only a typed Context field is frozen - these Config keys are
// deliberately NOT promoted to typed Context fields, since exactly one
// strategy needs each of them today ("route through Config first, only
// promote to a typed field if two+ strategies need it").
const (
	// ConfigKeyCache holds the shared memcache.Cache instance (as `any`,
	// asserted back to memcache.Cache by the consumer) used for cross-cycle
	// strategy state - Grid's built-grid-levels cache is the only Phase 1
	// consumer.
	ConfigKeyCache = "_cache"

	// ConfigKey24hVolume holds a float64: the symbol's 24h volume, fetched
	// by the engine (replicating the pre-refactor internal/broker.Broker.
	// Get24hVolume computation) so Grid's volume_filter gate keeps its exact
	// current semantics without the strategy touching the exchange directly.
	ConfigKey24hVolume = "_24h_volume"

	// ConfigKeyLongTermCandles holds []exchange.Candle: a supplementary
	// 15m/50-candle fetch, fetched by the engine, so Scalping's uptrend
	// filter (originally a direct broker.ListKline(ctx, symbol, "15m", 50)
	// call inside the algorithm) keeps its exact cross-timeframe semantics
	// without the strategy touching the exchange directly. This resolves
	// Spec 07 Acceptance Criterion #6's flagged open question ("a narrow
	// read-only capability passed through Context, distinct from full
	// ExchangeClient access") using the same Config-injection pattern
	// already used for ConfigKey24hVolume.
	ConfigKeyLongTermCandles = "_long_term_candles"
)
