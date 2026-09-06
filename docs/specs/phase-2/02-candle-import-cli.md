# Spec 02 — Candle Import CLI (`cmd/candleimport`)

## Overview

The backtest engine (Spec 04) needs historical candles already sitting in Postgres before it can run — no
strategy touches live Binance REST during a backtest. This spec defines `cmd/candleimport/main.go`, a
one-shot CLI that pulls historical klines via the already-existing `exchange.ExchangeClient.ListKline`
(Phase 1's `BinanceAdapter`) and writes them through Spec 01's `CandleRepository.Upsert`. It must be
idempotent/resumable (a solo maintainer running this against 2 years of history across several symbols will
interrupt it, deliberately or not) and — per this spec's resolution of a design gap surfaced while
cross-checking Spec 04 — must support importing **more than one timeframe per symbol**, since Grid's 24h-
volume gate and Scalping's 15m-uptrend gate both need timeframes distinct from a strategy's primary cycle
interval to be backtestable at all.

## Current Behavior (verified)

- No `cmd/candleimport` directory exists (confirmed: `cmd/` has only `api`, `worker`, `console`).
- `exchange.ExchangeClient.ListKline(ctx, symbol, interval string, limit int) ([]Candle, error)` already
  exists and is implemented by `internal/exchange/binance_adapter.go` (Phase 1) — this CLI is a **new
  consumer** of an existing, tested interface, not new exchange-integration work. `limit` is capped by
  Binance's API (1000 klines per REST call for spot `klines` endpoint) — the importer must page through
  multiple calls to cover a 2-year range, which the existing single-call `ListKline` signature does not do
  for the caller automatically.
- `app/engine/engine.go:166-168`'s `fetch24hVolume`/`ConfigKeyLongTermCandles` calls
  `e.Exchange.ListKline(ctx, symbol, "1d", 1)` and `e.Exchange.ListKline(ctx, symbol, "15m", 50)`
  respectively, **in addition to** the primary cycle-interval window — confirming (per Spec 01's Out of
  Scope note) that Grid and Scalping each depend on a second timeframe beyond their configured
  `StrategyConfiguration.Cycle`.

## Target Behavior

```go
// cmd/candleimport/main.go — flags
//   -symbols       comma-separated list, e.g. "BTCUSDT,ETHUSDT,SOLUSDT" (required)
//   -timeframes    comma-separated list, e.g. "1m,15m,1d" (required — see Acceptance Criterion #6
//                  for why this can't default to a single inferred value)
//   -from          RFC3339 start date (required)
//   -to            RFC3339 end date (default: now)
//   -min-history   duration, default "17520h" (2 years, PRD §9) — the CLI warns (not fails) if the
//                  resolved [from, to) range is shorter than this, since a deliberate short-range
//                  re-import (e.g. backfilling a gap) is a legitimate use of the same tool
```

For each `(symbol, timeframe)` pair:
1. Call `CandleRepository.LatestOpenTime(ctx, symbol, timeframe)`. If non-zero and greater than `-from`,
   resume from `max(-from, latestOpenTime - overlapWindow)` rather than `-from` directly — `overlapWindow`
   is a fixed 10-candle lookback (re-fetching the last 10 already-imported candles is cheap and covers the
   case where the previous run's last written batch was incomplete at crash time; `Upsert`'s idempotency
   (Spec 01 AC#2) makes the overlap harmless).
2. Page through `ExchangeClient.ListKline` in batches (batch size = the exchange's max, e.g. 1000),
   advancing the requested window by `batch_size * timeframe_duration` each call, until reaching `-to`.
3. `Upsert` each batch immediately after fetching it (not buffered until the entire range completes) — so
   an interruption after N of M batches leaves N batches durably written, resumable per step 1.
4. Log a structured completion line per `(symbol, timeframe)`: `{"symbol":"...", "timeframe":"...",
   "candles_imported": N, "duration_seconds": D, "gaps_detected": G}` (blueprint §7.2's guidance that
   one-shot CLIs get structured logs, not Prometheus instrumentation, plus Spec 11's
   `candle_import_lag_seconds` gauge — see Spec 11 for where that metric is actually emitted from).
5. **Gap detection**: after import, walk the stored candles for the range and flag any `OpenTime` gap wider
   than one `timeframe` duration (e.g. a missing 1m candle) — Binance's kline endpoint can return sparse
   data during low-liquidity periods or exchange downtime; a gap is logged as a warning (`gaps_detected`
   count, plus the specific gap ranges to stderr) but does **not** fail the import — a backtest can still
   run against slightly gapped data, and Spec 04's `ReplayFeed` must tolerate gaps (see Spec 03).

## Acceptance Criteria

1. **Given** `-symbols BTCUSDT -timeframes 1m -from 2024-01-01T00:00:00Z -to 2024-01-02T00:00:00Z`, **when**
   the import runs successfully, **then** `CandleRepository.Count(ctx, "BTCUSDT", "1m")` returns `1440`
   (one day of 1-minute candles), assuming no exchange-side gaps.
2. **Given** an import is interrupted (process killed) after writing candles through `2024-01-01T12:00`,
   **when** the same command is re-run with the same flags, **then** it resumes from approximately `11:50`
   (the 10-candle overlap window) rather than re-importing the full range from `00:00`, and the final
   candle count matches Acceptance Criterion #1's expectation exactly (no duplicates, per `Upsert`'s
   idempotency).
3. **Given** `-timeframes 1m,15m,1d` for a single symbol, **when** the import completes, **then**
   `CandleRepository.Count` returns independently-correct counts for all three timeframes — this is the
   acceptance criterion that validates the multi-timeframe requirement (Current Behavior) is actually
   satisfied, not just accepted as a flag with single-timeframe behavior underneath.
4. **Given** the requested `[from, to)` range is shorter than `-min-history` (e.g. importing 1 month), **when**
   the import runs, **then** it completes successfully and logs a warning about the range being short of the
   PRD §9 2-year minimum — it does not refuse to run (a maintainer legitimately re-importing a small gap
   should not be blocked by a policy meant for the *initial* full-history import).
5. **Given** Binance returns a sparse kline response with a detectable gap (e.g. missing candles between
   `05:00` and `05:03` for a `1m` request), **when** gap detection runs post-import, **then** the gap is
   logged with its exact `[start, end)` range and the import still exits `0` (success) — a gap is a data
   characteristic, not an import failure.
6. **Edge case — invalid symbol**: **given** `-symbols FAKEUSDT` (a symbol Binance doesn't recognize),
   **when** `ListKline` returns an error for that symbol, **then** the CLI logs the failure for that
   specific symbol and **continues** processing the remaining symbols in the `-symbols` list (one bad
   symbol must not abort a multi-symbol batch import), exiting non-zero overall only if every symbol failed.
7. **Edge case — rate limiting**: **given** Binance responds with a `429`/rate-limit error mid-page, **when**
   this occurs, **then** the importer backs off (fixed delay, e.g. 5s, sufficient for a one-shot batch tool
   — no need for the exponential-backoff sophistication `LiveFeed`'s reconnect logic uses, since this is a
   finite, human-supervised run, not an unattended long-lived connection) and retries the same batch rather
   than skipping it or aborting the whole import.

## Out of Scope

- Real-time/incremental "keep candles current" behavior beyond simple re-run-to-resume — PRD §9 explicitly
  distinguishes one-time import from ongoing accumulation ("Candle import is a one-time operation per
  symbol; ongoing candles accumulate via LiveFeed"). Whether `LiveFeed`'s already-received-but-not-yet-
  persisted candles should also be written to the `candles` table for continuous backtest-data freshness is
  a design question for **Phase 3** (once the TUI's Backtest Launcher wants "backtest through yesterday"
  without a manual re-import) — Phase 2's importer is explicitly a manual/scheduled batch tool, not a
  continuous sync.
- Deriving `15m`/`1d` candles from imported `1m` data instead of fetching each timeframe independently from
  Binance — accepted as simpler and consistent with Phase 3's `multitimeframe.go` being the intended future
  home for on-the-fly aggregation (see Spec 01 Out of Scope).

## Dependencies

- Spec 01 (Candle Storage) — `CandleRepository.{Upsert,LatestOpenTime,Count}`.
- Phase 1's `internal/exchange.ExchangeClient.ListKline` (already implemented, no changes needed here).
- **Judgment call**: requiring `-timeframes` as an explicit, required flag (no inferred default) is this
  spec's most consequential decision — it surfaces the Grid/Scalping cross-timeframe dependency as an
  operator-visible import requirement rather than a latent gap only discovered when a backtest run fails
  partway through for lack of `15m`/`1d` data. Flagging for explicit sign-off since it changes the "just
  import 1m and go" mental model a reader of PRD §4.3 alone might expect.
