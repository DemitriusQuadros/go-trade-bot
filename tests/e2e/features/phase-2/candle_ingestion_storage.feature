Feature: Candle Data Ingestion & Storage
  As a market data ingestion engine
  I want to store, import, and query historical OHLCV market candles
  So that backtesting and strategy warmups have accurate historical data

  Background:
    Given the database is clean

  @smoke @REQ-CANDLE-01
  Scenario: Store OHLCV candles with unique constraint enforcement
    When I save a candle for symbol "BTCUSDT" timeframe "1m" open time "2026-01-01T00:00:00Z" with OHLCV 50000.0, 50500.0, 49900.0, 50200.0, 10.5
    Then the database should contain 1 candle record for symbol "BTCUSDT"
    When I save a duplicate candle for symbol "BTCUSDT" timeframe "1m" open time "2026-01-01T00:00:00Z"
    Then the database should still contain 1 candle record for symbol "BTCUSDT"

  @smoke @REQ-CANDLE-02
  Scenario: Candle import CLI backfills missing candle history
    Given the market exchange API returns 5 historical candles for "ETHUSDT" "5m"
    When the candle import tool runs for symbol "ETHUSDT" timeframe "5m"
    Then the database should contain 5 candles for symbol "ETHUSDT" and timeframe "5m"

  @edge @REQ-CANDLE-03
  Scenario: Query candle range returns chronological data without gaps
    Given historical candles exist in the database for "SOLUSDT" "1h" from "2026-01-01T00:00:00Z" to "2026-01-01T05:00:00Z"
    When I query candle range for "SOLUSDT" "1h" between "2026-01-01T01:00:00Z" and "2026-01-01T04:00:00Z"
    Then 4 candles should be returned ordered by open time ascending
