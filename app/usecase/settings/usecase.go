// Package settings implements Spec backend-05 (ADR-016): tiered settings
// hot-swap with a drain-then-swap guard for risk-bearing fields.
//
// Deployment-topology note (flagged explicitly, since the spec's mechanism
// assumes a single in-memory process): cmd/api and cmd/worker are separate
// OS processes, each with its own independently-constructed
// exchange.SwappableExchangeClient / notifier.SwappableNotifier, and only
// cmd/worker runs the strategy execution loop (app/handler/tasks/strategy's
// StrategyProcessor, which owns the in-flight counter/admission gate the
// spec's drain sequence depends on). UseCase is instantiated once in each
// process:
//   - In cmd/worker, with a non-nil ProcessorGate (the real
//     StrategyProcessor) and a nil WorkerClient - it performs the actual
//     drain-then-swap against the objects that matter for live trading.
//   - In cmd/api, with a nil ProcessorGate (cmd/api runs no strategy cycles,
//     so there is nothing to drain there) and a non-nil WorkerClient that
//     forwards the same request synchronously to cmd/worker's internal
//     /internal/settings/apply endpoint (localhost, same host) so the
//     worker's copy of the Swappables/StrategyProcessor ceiling actually
//     gets updated too - not just cmd/api's own. The public PUT /settings
//     contract (status codes, timing, AC's) is observed at the cmd/api call
//     site, which blocks on the forwarded call for exactly this reason.
package settings

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/configuration"
)

var (
	// ErrConfirmLiveRequired is returned when a Mode->live transition is
	// requested without confirm_live:true (Spec backend-05 SS5, AC#7). Maps
	// to HTTP 400.
	ErrConfirmLiveRequired = errors.New("mode transition to live requires confirm_live=true")
	// ErrDrainTimeout is returned when in-flight cycles do not reach zero
	// within the drain deadline (AC#5). Maps to HTTP 503.
	ErrDrainTimeout = errors.New("drain_timeout")
	// ErrSwapFailed wraps a failure from the exchange/notifier swap step
	// itself, e.g. rejected credentials (AC#8). Maps to HTTP 502.
	ErrSwapFailed = errors.New("settings swap failed")
)

// drainTimeout/drainPollInterval are package vars (not consts) solely so
// tests can shrink them to exercise the timeout path deterministically and
// quickly rather than waiting out a real 30s deadline.
var (
	drainTimeout      = 30 * time.Second
	drainPollInterval = 200 * time.Millisecond
)

// Repository is the persistence port (app/repository/settings.Repository
// already satisfies this).
type Repository interface {
	Get(ctx context.Context) (*entities.Settings, error)
	Save(ctx context.Context, settings *entities.Settings) error
}

// ExchangeSwapper is satisfied by *exchange.SwappableExchangeClient.
type ExchangeSwapper interface {
	SwapFromConfig(cfg *configuration.Configuration) error
}

// NotifierSwapper is satisfied by *notifier.SwappableNotifier.
type NotifierSwapper interface {
	Swap(cfg *configuration.Configuration) error
}

// ProcessorGate is satisfied by *app/handler/tasks/strategy.StrategyProcessor
// - the drain-then-swap admission gate/in-flight counter this usecase
// orchestrates lives there. nil in processes that run no strategy cycles.
type ProcessorGate interface {
	SetDraining(draining bool)
	InFlightCount() int64
	SetCeiling(mode strategies.ExecutionMode)
	SetTestnet(testnet bool)
}

// WorkerClient forwards an already-validated Apply request to cmd/worker's
// internal settings endpoint so its process-local Swappables/ProcessorGate
// get updated too. nil inside cmd/worker's own UseCase instance (it IS the
// worker; forwarding to itself would recurse).
type WorkerClient interface {
	Apply(ctx context.Context, next entities.Settings, confirmLive bool) error
}

type UseCase struct {
	repo     Repository
	exchange ExchangeSwapper
	notifier NotifierSwapper
	gate     ProcessorGate // nil if this process runs no strategy cycles
	worker   WorkerClient  // nil if this process IS the worker
	mu       sync.Mutex
}

func NewUseCase(repo Repository, exchange ExchangeSwapper, notifier NotifierSwapper, gate ProcessorGate, worker WorkerClient) *UseCase {
	return &UseCase{repo: repo, exchange: exchange, notifier: notifier, gate: gate, worker: worker}
}

func (u *UseCase) Get(ctx context.Context) (entities.Settings, error) {
	s, err := u.repo.Get(ctx)
	if err != nil {
		return entities.Settings{}, err
	}
	return *s, nil
}

// riskBearingChanged reports whether any risk-bearing field (Spec
// backend-05 SS1) differs between current and requested settings.
func riskBearingChanged(current, next entities.Settings) bool {
	return current.BrokerApiKey != next.BrokerApiKey ||
		current.BrokerApiSecret != next.BrokerApiSecret ||
		current.BrokerTestnetApiKey != next.BrokerTestnetApiKey ||
		current.BrokerTestnetApiSecret != next.BrokerTestnetApiSecret ||
		current.Testnet != next.Testnet ||
		current.Mode != next.Mode
}

// Apply is the single entry point PUT /settings calls (Spec backend-05 SS4).
// confirmLive is the request's own confirm_live flag - ConfirmLive is not an
// independently-settable stored field (SS1), it only ever gates a Mode->live
// transition, re-asserted here exactly like assertModeGuard does at boot.
func (u *UseCase) Apply(ctx context.Context, next entities.Settings, confirmLive bool) (entities.Settings, error) {
	if next.Mode == strategies.ModeLive.String() && !confirmLive {
		return entities.Settings{}, ErrConfirmLiveRequired
	}

	current, err := u.repo.Get(ctx)
	if err != nil {
		return entities.Settings{}, err
	}

	if riskBearingChanged(*current, next) {
		return u.applyRiskBearing(ctx, next, confirmLive)
	}
	return u.applySafe(ctx, next, confirmLive)
}

// applySafe hot-swaps safe-tier fields immediately, with no guard (AC#1).
func (u *UseCase) applySafe(ctx context.Context, next entities.Settings, confirmLive bool) (entities.Settings, error) {
	cfg := next.ToConfiguration()
	if err := u.notifier.Swap(cfg); err != nil {
		return entities.Settings{}, fmt.Errorf("%w: %v", ErrSwapFailed, err)
	}

	if u.worker != nil {
		// Best-effort: a safe-tier field is cosmetic even if this
		// synchronous fan-out to the worker fails (e.g. worker momentarily
		// unreachable) - the worker picks up the persisted value on its own
		// next settings change anyway. Not fatal to this request.
		_ = u.worker.Apply(ctx, next, confirmLive)
	}

	if err := u.repo.Save(ctx, &next); err != nil {
		return entities.Settings{}, err
	}
	return next, nil
}

// applyRiskBearing runs the drain-then-swap sequence (Spec backend-05 SS3/4).
func (u *UseCase) applyRiskBearing(ctx context.Context, next entities.Settings, confirmLive bool) (entities.Settings, error) {
	u.mu.Lock() // AC#9: serialize overlapping risk-bearing requests
	defer u.mu.Unlock()

	if u.gate != nil {
		u.gate.SetDraining(true)
		defer u.gate.SetDraining(false)

		deadline := time.Now().Add(drainTimeout)
		for u.gate.InFlightCount() > 0 {
			if time.Now().After(deadline) {
				return entities.Settings{}, ErrDrainTimeout
			}
			time.Sleep(drainPollInterval)
		}
	}

	cfg := next.ToConfiguration()
	if err := u.exchange.SwapFromConfig(cfg); err != nil {
		return entities.Settings{}, fmt.Errorf("%w: %v", ErrSwapFailed, err)
	}
	if err := u.notifier.Swap(cfg); err != nil {
		return entities.Settings{}, fmt.Errorf("%w: %v", ErrSwapFailed, err)
	}

	if u.gate != nil {
		if mode, err := strategies.ParseExecutionMode(next.Mode); err == nil {
			u.gate.SetCeiling(mode)
		}
		u.gate.SetTestnet(next.Testnet)
	}

	if u.worker != nil {
		// Risk-bearing: the worker's own copy of the Swappables/gate is the
		// one that actually matters for live trading safety, so its result
		// is propagated to the caller, not swallowed - a 503/502 here must
		// surface as the same to the original PUT /settings caller, and
		// persistence below must not happen if it fails.
		if err := u.worker.Apply(ctx, next, confirmLive); err != nil {
			return entities.Settings{}, err
		}
	}

	if err := u.repo.Save(ctx, &next); err != nil {
		return entities.Settings{}, err
	}
	return next, nil
}
