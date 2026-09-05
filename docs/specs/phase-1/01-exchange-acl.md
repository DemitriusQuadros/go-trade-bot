# Spec 01 — Exchange ACL (`internal/exchange`)

## Overview

Today every Binance interaction goes through `internal/broker.Broker`, a concrete struct that wraps
`*binance.Client` directly, and `go-binance` types leak into the usecase layer. This spec defines the
`ExchangeClient` interface — the single Anti-Corruption Layer boundary for all exchange interaction — plus
its two implementations (`BinanceAdapter` for production, `BinanceTestnetAdapter` for Paper Trading), and
the migration of every current call site off the concrete `Broker` type and off leaked `go-binance` types.
This is the foundation spec: specs 02-04 all depend on `ExchangeClient` existing and being wired in.

## Current Behavior (verified)

- `internal/broker/broker.go:12-21` — `Broker` struct holds `client *binance.Client` (unexported), constructed via `NewBroker(cfg *configuration.Configuration) Broker` (value receiver, value return — not a pointer).
- `internal/broker/broker.go:23-30` — `ListTickerPrices(ctx, symbol) ([]*binance.SymbolPrice, error)` — returns the **go-binance SDK type directly**.
- `internal/broker/broker.go:32-39` — `ListKline(ctx, symbol, interval, limit) ([]*binance.Kline, error)` — same leak.
- `internal/broker/broker.go:41-55` — `Get24hVolume(ctx, symbol) (float64, error)` — computed by calling `ListKline` internally and parsing `Volume` string field.
- `app/usecase/signal/usecase.go:10,40-42` — the usecase package imports `github.com/adshao/go-binance/v2` **only** to declare its own local `Broker` interface: `ListTickerPrices(ctx, symbol) ([]*binance.SymbolPrice, error)`. This is the leak the PRD calls out explicitly.
- `app/services/algorithm/grid/algorithm.go:19`, `bollinger/algorithm.go:19`, `scalping/algorithm.go:19` — each holds a `broker broker.Broker` field (the **concrete struct**, not an interface), constructed by `NewGridProcessor`/`NewBollingerProcessor`/`NewScalpingProcessor`.
- `cmd/api/modules/broker.go` and `cmd/worker/modules/broker.go` — `fx.Module` each providing `broker.NewBroker` as a constructor for the concrete type.
- No `PlaceOrder`, `CancelOrder`, `GetAccountBalance`, or `SubscribeKline` exists anywhere in the repository (confirmed by search for `NewCreateOrderService`, `NewCancelOrderService`, `NewGetAccountService`, `WsKline`).

## Target Behavior

```go
// internal/exchange/interface.go
package exchange

import "context"

type ExchangeClient interface {
    PlaceOrder(ctx context.Context, order PlaceOrderRequest) (OrderResult, error)
    CancelOrder(ctx context.Context, symbol, orderID string) error
    ListKline(ctx context.Context, symbol, interval string, limit int) ([]Candle, error)
    ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error)
    GetAccountBalance(ctx context.Context) (AccountBalance, error)
    SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error)
}
```

```go
// internal/exchange/types.go
package exchange

import "time"

type OrderSide string
const (
    SideBuy  OrderSide = "BUY"
    SideSell OrderSide = "SELL"
)

type OrderType string
const (
    OrderTypeMarket     OrderType = "MARKET"
    OrderTypeStopMarket OrderType = "STOP_MARKET"
)

type OrderStatus string
const (
    OrderStatusNew             OrderStatus = "NEW"             // resting (STOP_MARKET only)
    OrderStatusFilled          OrderStatus = "FILLED"
    OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
    OrderStatusRejected        OrderStatus = "REJECTED"
    OrderStatusExpired         OrderStatus = "EXPIRED"
    OrderStatusCanceled        OrderStatus = "CANCELED"
)

type PlaceOrderRequest struct {
    Symbol        string
    Side          OrderSide
    Type          OrderType
    Quantity      float64
    StopPrice     float64 // required iff Type == OrderTypeStopMarket; ignored otherwise
    ClientOrderID string  // caller-supplied idempotency key, see Spec 02 §Idempotency
}

type OrderResult struct {
    BrokerOrderID string
    ClientOrderID string
    Status        OrderStatus
    ExecutedQty   float64
    AvgFillPrice  float64 // 0 when Status == NEW (resting stop not yet triggered)
}

type Candle struct {
    Symbol    string
    Timeframe string
    OpenTime  time.Time
    Open, High, Low, Close, Volume float64
}

type TickerPrice struct {
    Symbol string
    Price  float64
}

type AccountBalance struct {
    Asset  string
    Free   float64
    Locked float64
}
```

`internal/exchange/binance_adapter.go` wraps `*binance.Client` (production base URL) and is the **only file
in the repository permitted to import `github.com/adshao/go-binance/v2`** other than
`binance_testnet_adapter.go`. `binance_testnet_adapter.go` implements the identical `ExchangeClient`
interface against Binance's testnet base URL (`https://testnet.binance.vision` for spot) — used exclusively
in `ModePaper`.

All current call sites are re-pointed at the interface:
- `app/usecase/signal/usecase.go` — local `Broker` interface (lines 40-42) is **deleted**; the `go-binance`
  import (line 10) is removed entirely; `SignalUseCase` gains an `exchange.ExchangeClient` field.
- `app/strategies/grid`, `bollinger`, `scalping` (ported per Spec 08) depend on `exchange.ExchangeClient`
  via `Context.Indicators`/exchange access is routed through the engine, not held directly by strategy
  structs — strategies never hold an `ExchangeClient` reference themselves (see Spec 05).
- `cmd/api/modules/broker.go` and `cmd/worker/modules/broker.go` are deleted; replaced by
  `cmd/api/modules/exchange.go` / `cmd/worker/modules/exchange.go`, each an `fx.Module` providing
  `exchange.ExchangeClient` — bound to `BinanceAdapter` or `BinanceTestnetAdapter` based on
  `configuration.Configuration.Testnet` (see Spec 10 for how `Mode`/`Testnet` interact).

## Acceptance Criteria

1. **Given** `internal/exchange/interface.go` is compiled, **when** any file outside `internal/exchange/`
   imports `github.com/adshao/go-binance/v2`, **then** a CI lint step (depguard or equivalent import-check)
   fails the build (see blueprint §6 Risk: ACL boundary erosion).
2. **Given** `configuration.Configuration.Testnet == false`, **when** `cmd/worker/modules/exchange.go`'s
   provider function runs, **then** it returns a `*BinanceAdapter` pointed at the production Binance base URL.
3. **Given** `configuration.Configuration.Testnet == true`, **when** the same provider function runs,
   **then** it returns a `*BinanceTestnetAdapter` pointed at the testnet base URL, using testnet-specific
   API credentials from config (not the production `Broker.ApiKey`/`ApiSecret`).
4. **Given** `BinanceAdapter.ListTickerPrices(ctx, "BTCUSDT")` is called, **when** the underlying
   `go-binance` call succeeds, **then** it returns `[]exchange.TickerPrice` (not `[]*binance.SymbolPrice`)
   with `Price` parsed to `float64` (not left as `string`, unlike the current `broker.go` callers which
   each independently call `strconv.ParseFloat`).
5. **Given** the underlying Binance REST call returns an error (e.g. rate limit `429`, or network timeout),
   **when** any `ExchangeClient` method is called, **then** the adapter returns `(zero-value, error)` — it
   does **not** log-and-swallow the error the way `broker.go:26,37,44` currently does with bare `fmt.Println(err)`
   before returning it; callers are the single place errors are logged.
6. **Given** `app/usecase/signal/usecase.go` after migration, **when** its source is inspected, **then** it
   contains zero references to any `github.com/adshao/go-binance/v2` type or the `internal/broker` package.
7. **Given** the three strategy packages under `app/strategies/{grid,bollinger,scalping}` after porting
   (Spec 08), **when** their source is inspected, **then** none import `internal/broker` or hold a
   `broker.Broker`-typed field — all exchange access flows through `Context` as built by `app/engine`.
8. **Edge case — partial config**: **Given** `configuration.Configuration.Broker.ApiKey` is empty at
   startup, **when** `cmd/worker/main.go` builds the `fx` graph, **then** the exchange module provider
   fails fast with a descriptive error (not a nil-pointer panic inside a later `PlaceOrder` call).
9. **Edge case — testnet/production mismatch**: **Given** `Testnet == true` but `Mode` (Spec 10) resolves
   to `live`, **when** the worker starts, **then** startup fails with an explicit configuration-conflict
   error (`live` mode must never resolve to a testnet adapter — this is a distinct guard from the
   `MODE`/`--confirm-live` check in Spec 10, and both must pass independently).

## Out of Scope

- `SubscribeKline`'s actual WebSocket implementation — interface method is defined here, but the
  implementation and reconnect logic belong to Spec 04 (`internal/feed`'s `LiveFeed` is the consumer;
  `BinanceAdapter.SubscribeKline` itself is a thin wrapper over `go-binance`'s `WsKlineServe`, detailed in
  Spec 04, not here).
- Futures endpoints (leverage, margin type, position mode) — PRD §4.1 says "spot and futures" but the
  existing `entities.MarginType` (`Isolated`/`Cross`) is never actually used to select an endpoint anywhere
  in current code; Phase 1 targets spot only. Futures support is an open question, not scoped here.
- Multi-exchange abstraction beyond Binance (explicitly out per PRD §10 and blueprint §6 scope-creep list).

## Dependencies

None — this is the first Phase 1 spec; specs 02, 03, 04 all depend on this one.
