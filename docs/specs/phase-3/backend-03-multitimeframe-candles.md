# Spec backend-03 — Multi-Timeframe Candle Access (`internal/feed/multitimeframe.go`)

## Overview

Blueprint §4 Phase 3 wants multi-timeframe candle access "for `Context.Candles` multi-TF access." This spec
resolves the ADR-005 promotion question the coordinator asked about directly: **does two-or-more-strategies
need it yet?** The audit below finds the answer is still **no** — only Scalping needs raw multi-timeframe
candles today; Grid's 24h-volume need is a derived scalar, not raw candle access. Per ADR-005's own stated
threshold ("only promote to a typed field if two+ strategies need it"), this spec keeps multi-timeframe
access routed through `Context.Config`, but generalizes the mechanism from Scalping's hardcoded single-case
exception into a reusable capability, and — separately — builds the actual `1m → 5m/15m/1h` aggregation
logic that lets this work during a backtest without requiring every timeframe to be separately imported.

## Current Behavior (verified)

- `app/strategies/config_keys.go:28-37` — `ConfigKeyLongTermCandles` already exists, documented explicitly
  as "resolves Spec 07 Acceptance Criterion #6's flagged open question... using the same Config-injection
  pattern already used for `ConfigKey24hVolume`." This is Phase 1's own prior resolution of exactly this
  question for one specific case.
- `app/engine/engine.go:166-168` — the engine fetches this **hardcoded** to `"15m", 50` regardless of what
  timeframe a strategy might actually want: `e.Exchange.ListKline(ctx, symbol, "15m", 50)`. There is no
  generalized "fetch me timeframe X, N candles" mechanism — it's a single special case wired directly into
  `buildContext` for Scalping's specific need.
- `app/strategies/scalping/strategy.go` reads `ConfigKeyLongTermCandles` from `ctx.Config` (confirmed by
  Phase 1 spec cross-reference; the exact line wasn't re-verified byte-for-byte here since
  `config_keys.go`'s documentation already confirms Scalping is the sole consumer).
- `app/engine/engine.go:203-217` (`fetch24hVolume`) computes a **single scalar** (`float64` volume from one
  `1d`/1-candle fetch) — Grid's `Before`/build-phase logic (per Phase 1 Spec 08) consumes this as
  `ConfigKey24hVolume`, a `float64`, **never** as raw `[]exchange.Candle`. Grid does not need — and does not
  receive — candle-level access to any timeframe other than its own primary cycle interval.
- **Conclusion**: exactly **one** strategy (Scalping) needs raw candle access at a non-primary timeframe
  today. Grid's second-timeframe need is already fully satisfied by a derived scalar, not comparable to
  Scalping's need for the full `[]exchange.Candle` slice (Scalping computes its own EMA over those candles
  via `ctx.Indicators.EMA`, which requires the raw series, not a single number).
- `internal/feed/` (current) contains only `interface.go`, `live_feed.go` (Phase 1) — no aggregation logic
  of any kind exists. Phase 2's candle-import spec (`docs/specs/phase-2/02-candle-import-cli.md`) worked
  around the absence of aggregation by requiring **separate imports per timeframe** (`-timeframes
  1m,15m,1d`), explicitly deferring on-the-fly derivation to "Phase 3's `multitimeframe.go`" — this spec is
  where that deferral resolves.

## Target Behavior

### Decision: stay in `Context.Config`, generalize the mechanism (do not promote to a typed field)

Per ADR-005's literal threshold, one consumer does not justify widening the frozen `Context` struct. This
spec instead generalizes `ConfigKeyLongTermCandles` (a single hardcoded `15m`/`50` case) into a
**family-keyed** mechanism any strategy's config can request without an engine code change per new
timeframe need:

```go
// app/strategies/config_keys.go — addition
// ConfigKeyTimeframeCandles builds a Config key for an arbitrary supplementary
// timeframe, e.g. ConfigKeyTimeframeCandles("15m") == "_timeframe_candles_15m".
// Replaces the single hardcoded ConfigKeyLongTermCandles special-case with a
// generalized family - Scalping's port (Phase 1 code) is updated to read
// ConfigKeyTimeframeCandles("15m") instead of the old fixed key, with
// identical runtime behavior (same value, same shape, new key name).
func ConfigKeyTimeframeCandles(timeframe string) string {
    return "_timeframe_candles_" + timeframe
}
```

`app/engine/engine.go`'s `buildContext` replaces its hardcoded `ListKline(ctx, symbol, "15m", 50)` call with
a data-driven loop reading a **new, strategy-authored config key** (not engine-owned, since only the
strategy's own config JSON knows which supplementary timeframes it needs):
```go
if timeframes, ok := config["_requested_timeframes"].([]interface{}); ok {
    for _, tfRaw := range timeframes {
        tf, _ := tfRaw.(string)
        candles, err := e.timeframeSource.Candles(ctx, symbol, tf, 50) // see below
        if err == nil {
            config[strategies.ConfigKeyTimeframeCandles(tf)] = candles
        }
    }
}
```
Wait — `_requested_timeframes` would need to be strategy-authored in the *user's* `Configuration` JSON,
which conflates user-facing config with an engine-internal request mechanism. Since Phase 1 already
established the pattern of the **engine deciding** what a specific named strategy needs (via
`ConfigKeyLongTermCandles`'s hardcoded injection, not a user-configurable field), this spec keeps that
precedent: **the engine still hardcodes which strategies need which supplementary timeframes**, but the
hardcoding lives in one small, explicit, engine-level lookup table rather than being buried inline in
`buildContext`, so adding the *next* strategy's supplementary-timeframe need is a one-line table entry, not
a new `buildContext` branch:
```go
// app/engine/timeframe_requirements.go (NEW, small)
var strategyTimeframeRequirements = map[string][]string{
    "scalping": {"15m"},
    // future entries: "some_new_strategy": {"1h", "4h"},
}
```
`buildContext` looks up `strategyTimeframeRequirements[dbStrategy.StrategyName]` and fetches+injects each
listed timeframe via `ConfigKeyTimeframeCandles`. This is the generalization: Scalping's port needs zero
further change (it already reads a Config key for its long-term candles; only the key name changes), and a
future second consumer needs a one-line table addition plus its own `ctx.Config[...]` read — **only if a
second consumer actually materializes should the promotion-to-typed-field conversation be reopened**, per
ADR-005.

### `internal/feed/multitimeframe.go` — real aggregation, for backtest fidelity

```go
package feed

// AggregateCandles derives higher-timeframe candles from a lower-timeframe
// (base) series - e.g. 15 consecutive 1m candles -> one 15m candle. Standard
// OHLCV aggregation: Open = first candle's Open, Close = last candle's Close,
// High = max(Highs), Low = min(Lows), Volume = sum(Volumes).
func AggregateCandles(base []exchange.Candle, targetTimeframe string) ([]exchange.Candle, error)
```

This is consumed by Spec 04 (Phase 2)'s `SimulatedFillExchange`/`MarketDataSource` (backtest mode): when a
requested `(symbol, timeframe)` pair has no directly-imported candles but the **base** timeframe (`1m`) is
available covering the same range, `MarketDataSource` derives the higher timeframe on the fly via
`AggregateCandles` instead of requiring Phase 2's importer to have separately fetched it. This **relaxes**
Phase 2 Spec 02's "timeframes must be explicitly imported" requirement for any timeframe that's a clean
multiple of an already-imported base timeframe (`5m`, `15m`, `1h` from `1m`) — Phase 2's importer remains
correct and unaffected (nothing in this spec requires changing it), it's simply no longer the *only* way to
get `15m` data into a backtest once this spec ships.

## Acceptance Criteria

1. **Given** `AggregateCandles` is called with 15 consecutive well-formed 1m candles, **when** aggregating
   to `"15m"`, **then** it returns exactly one candle with `Open` = the first input candle's `Open`,
   `Close` = the fifteenth's `Close`, `High`/`Low` = the max/min across all 15, `Volume` = the sum, and
   `OpenTime` = the first candle's `OpenTime`.
2. **Given** 32 consecutive 1m candles (not an exact multiple of 15), **when** aggregating to `"15m"`,
   **then** exactly 2 complete 15m candles are produced (covering the first 30 minutes) and the trailing 2
   incomplete minutes are **discarded**, not padded into a partial/short final candle — a partial candle
   would misrepresent the timeframe's actual OHLCV and must not be silently included.
3. **Given** a gap in the base 1m series (per Phase 2 Spec 02's gap tolerance), **when** aggregating across
   the gap, **then** `AggregateCandles` either skips the incomplete window spanning the gap (consistent with
   Acceptance Criterion #2's "no partial candles" rule) or produces a candle from fewer-than-expected
   source candles — this spec requires the **skip** behavior (never silently aggregate a gappy window as if
   it were complete), to avoid quietly understating volume/misrepresenting the range for a window with
   missing source data.
4. **Given** Scalping's port is updated to read `ConfigKeyTimeframeCandles("15m")` instead of the old
   `ConfigKeyLongTermCandles`, **when** a live cycle runs, **then** the injected value is identical in
   shape and content to what the old hardcoded mechanism provided — this is a pure rename/generalization,
   not a behavior change, verified by the existing Phase 1 characterization-test fixtures for Scalping
   (Phase 1 Spec 08) still passing unmodified.
5. **Given** the `strategyTimeframeRequirements` table has no entry for a given strategy (e.g. Grid,
   Bollinger), **when** `buildContext` runs for that strategy, **then** no supplementary timeframe fetch
   occurs at all — zero added REST/aggregation cost for strategies that don't need it, matching today's
   behavior exactly.
6. **Edge case — aggregation requested for a timeframe smaller than the base**: **given**
   `AggregateCandles` is called with a `targetTimeframe` smaller than the input candles' own interval (e.g.
   trying to derive `1m` from `15m` input, which is not a valid downsampling direction), **when** called,
   **then** it returns an error rather than producing nonsensical or duplicated candles — aggregation is
   strictly upsampling (coarser), never the reverse.
7. **Edge case — backtest with only 1m imported, strategy needs 1h**: **given** only `1m` candles are
   stored for a symbol and a strategy's requirement table entry needs `1h`, **when** the backtest engine
   (Phase 2 Spec 04's `MarketDataSource`) resolves the `1h` request, **then** it successfully derives `1h`
   candles via `AggregateCandles` (60 consecutive 1m candles per hour) without requiring a separate `1h`
   import — validating this spec's stated relaxation of Phase 2 Spec 02's import requirement.

## Out of Scope

- Promoting `Context.Candles` to a `map[string][]exchange.Candle` (multi-timeframe-native typed field) —
  explicitly not done, per this spec's central finding that the ADR-005 two-consumer threshold isn't met.
  If a second strategy needing raw multi-timeframe access is added in a future phase, **that** is the
  trigger to revisit this decision, not a change made preemptively here.
- Live-mode aggregation (deriving `15m` from `1m` for a *live* cycle) — live mode already fetches any
  interval directly from Binance via `ExchangeClient.ListKline(symbol, "15m", ...)`, which is simpler and
  more accurate than local aggregation for live data; `AggregateCandles` is motivated entirely by backtest
  data-availability, not live-path need.
- Sub-minute timeframes or non-clock-aligned custom intervals — Binance's own supported interval set (`1m,
  3m, 5m, 15m, 30m, 1h, ...`) is the only vocabulary this spec needs to support.

## Dependencies

- Phase 1's `app/strategies/config_keys.go`, `app/engine/engine.go` (both modified: key generalization,
  hardcoded-15m-fetch replaced by table-driven fetch).
- Phase 2's `docs/specs/phase-2/04-backtest-engine.md` (`MarketDataSource`) — this spec's `AggregateCandles`
  is consumed there for backtest-mode timeframe resolution.
- **Judgment call**: keeping the "which strategy needs which timeframe" mapping as an engine-internal table
  (`strategyTimeframeRequirements`) rather than a user-configurable field in the strategy's own
  `Configuration` JSON is a deliberate choice to preserve Phase 1's existing precedent
  (`ConfigKeyLongTermCandles` was never user-configurable either) — flagging this as worth confirming, since
  a user-facing "declare your own supplementary timeframes in config" design was considered and rejected in
  favor of consistency with the existing pattern, not because it's technically inferior.
