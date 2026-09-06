# Spec 05 — Simulator Parity Test (Phase 2 Completion Gate)

## Overview

Blueprint §6 names this a **mandatory phase-completion gate**, not an incidental test: "same strategy, same
candle sequence, `ReplayFeed` vs `LiveFeed`, asserting identical resulting P&L." This spec treats it as its
own deliverable because it exercises the exact seam Spec 04 introduces (`ReplayDriver` +
`SimulatedFillExchange`) against **both** feed implementations, and because a passing parity test is the
only concrete evidence that ADR-001's "one engine, shared across modes" claim is true rather than aspirational.

## Current Behavior (verified)

- No parity test exists (Phase 2 is greenfield; `app/engine/engine_test.go` from Phase 1 tests `Engine.Run`
  against mocked `SignalUseCase`/`AccountReader`/exchange interfaces for the live path only — it does not
  exercise `ReplayFeed`/`LiveFeed` at all, since neither existed as an `Engine` input in Phase 1).
- `internal/feed.LiveFeed` (Phase 1) is WebSocket-driven and cannot be pointed at a fixed, replayable candle
  sequence without a fake/mock `ExchangeClient.SubscribeKline` — this test requires exactly that: a
  test-only `ExchangeClient` implementation whose `SubscribeKline` emits a **predetermined, fixed candle
  sequence** (matching a `ReplayFeed`'s dataset exactly) down its returned channel, rather than a real
  WebSocket connection.

## Target Behavior

```go
// app/engine/simulator_parity_test.go (NEW)
package engine_test

// fixedSequenceExchange is a test double implementing exchange.ExchangeClient
// whose SubscribeKline emits a fixed, pre-loaded []exchange.Candle sequence
// down the returned channel (one send per call to a test-controlled
// "advance" method), and whose ListKline/PlaceOrder/etc. are otherwise
// identical in behavior to SimulatedFillExchange's Backtest-mode semantics
// (Spec 04) - the parity test's whole point is that the ONLY difference
// between the two runs is the Feed implementation (ReplayFeed vs a
// LiveFeed wrapping this fixture), not the fill/read semantics underneath.
type fixedSequenceExchange struct { /* ... */ }

func TestSimulatorParity(t *testing.T) {
    candles := loadFixture(t, "testdata/btcusdt_1m_1000candles.json") // shared fixture, both runs

    // Run A: ReplayDriver + ReplayFeed (Spec 03/04) over an in-memory
    // CandleRepository pre-seeded with `candles`.
    resultA := runBacktest(t, candles)

    // Run B: ReplayDriver + LiveFeed (Phase 1) wrapping fixedSequenceExchange,
    // fed the SAME `candles` slice in the SAME order via SubscribeKline.
    resultB := runLiveFeedReplay(t, candles)

    assertIdenticalTradeLogs(t, resultA.Trades, resultB.Trades)
    assertIdenticalPnL(t, resultA.TotalPnL, resultB.TotalPnL)
}
```

**What "identical" means precisely**: not just total P&L equality (which could hide offsetting errors) —
the full trade log (Spec 06's `TradeLogEntry` list: entry time, entry price, exit time, exit price, exit
reason, P&L per trade) must match **entry-for-entry, in order**. Total P&L equality alone is accepted as
insufficient evidence of parity and is explicitly called out as the wrong bar in Acceptance Criterion #4.

**Why `LiveFeed` needs a fixture wrapper rather than testing `ReplayFeed` against itself twice**: a test
that ran `ReplayFeed` through `ReplayDriver` twice and compared the results would trivially pass regardless
of whether `LiveFeed`'s single-candle push semantics (Phase 1 Spec 04: buffered channel, `Next()` blocks per
candle) interact correctly with `ReplayDriver`'s rolling-window accumulation (Spec 04, this phase) — the
parity test's entire value is in exercising the **actual** `LiveFeed` implementation's `Next()` contract
against the same driver that Backtest uses, with only the underlying candle **source** (WebSocket-fixture
vs. Postgres) differing.

## Acceptance Criteria

1. **Given** the same 1000-candle fixture, **when** run through `ReplayDriver{Feed: ReplayFeed}` and
   `ReplayDriver{Feed: LiveFeed-wrapping-fixture}` for the Bollinger strategy (chosen for this spec's
   canonical run since it has the simplest state — no cross-cycle cache, unlike Grid), **then** the two
   resulting trade logs are identical: same number of trades, same entry/exit timestamps, same entry/exit
   prices, same P&L per trade.
2. **Given** the same setup for the Grid strategy (chosen specifically **because** it has cross-cycle cache
   state via `ConfigKeyCache`/`internal/memcache`), **when** both runs execute, **then** results are still
   identical — this is the specific case that would catch a bug where `ReplayDriver` accidentally
   constructs a fresh `memcache.Cache` per candle for one feed type but a shared one for the other.
3. **Given** the same setup for the Scalping strategy (chosen because it needs `ConfigKeyLongTermCandles`,
   the 15m supplementary fetch), **when** both runs execute, **then** results are identical — this is the
   specific case that would catch a bug where the no-lookahead guarantee (Spec 04) holds for one feed
   source's `MarketDataSource` implementation but not the other's.
4. **Given** a deliberately-introduced parity bug (e.g. `ReplayFeed`'s `MarketDataSource` off-by-one on the
   window boundary), **when** the test suite runs against this broken code as a negative-control check
   during Spec 04's implementation review, **then** the test **fails** on trade-log mismatch even if total
   summed P&L happens to coincidentally match (validating Target Behavior's "total P&L alone is
   insufficient" design choice actually catches something total-P&L comparison would miss).
5. **Edge case — a stop-loss-triggering fixture**: **given** a fixture specifically constructed so a
   resting stop-loss triggers mid-sequence (not just take-profit/band-cross exits), **when** both runs
   execute, **then** the stop-triggered trade's exit price/reason match exactly between runs — this
   exercises Spec 04's "stop checked against subsequent candle's Low" logic identically regardless of feed
   source.
6. **Edge case — feed exhaustion mid-open-position**: **given** the fixture ends while a position is still
   open (no closing candle before the sequence runs out), **when** both runs reach feed exhaustion, **then**
   both leave the position open in their respective trade-log outputs identically (an "open at end of
   backtest" position is not force-closed by either driver) — this is a documented shared behavior, not
   left to differ between the two feed types by accident.

## Out of Scope

- Performance/latency comparison between the two feed types — this test is a **correctness** gate, not a
  benchmark; `LiveFeed`'s buffered-channel overhead vs. `ReplayFeed`'s in-memory slice iteration are
  expected to differ in speed, which is fine and not asserted on.
- Parity testing against **real** Binance WebSocket data (as opposed to the fixture wrapper) — that would
  require live network access during CI/test runs, which this repository's existing test conventions
  explicitly avoid (SQLite in-memory for repository tests, mocked interfaces for usecase/handler tests, per
  CLAUDE.md).

## Dependencies

- Spec 03 (ReplayFeed), Spec 04 (Backtest Engine: `ReplayDriver`, `SimulatedFillExchange`).
- Phase 1's `internal/feed.LiveFeed` (used here via a fixture-backed `ExchangeClient`, not modified).
- This spec is the **gate**, not a prerequisite, for the rest of Phase 2's completion — per the blueprint's
  own framing, it should be the last Phase 2 item to go green, exercising everything specs 01-04 built.
