package usecase

import "go-trade-bot/app/entities"

// DryRunAccount is the AccountUseCase the worker's dryrun SignalUseCase uses
// (fix-01). Reads (CanOpenOrder, GetDisponibleAmout) delegate to the real
// account so dryrun sizing mirrors what live would do, but the writes
// (DeductOrder, AddOrder) are no-ops: simulated fills must never move the
// real capital bookkeeping that live sizing depends on.
//
// Follow-up: a virtual dryrun balance (out of scope for fix-01).
type DryRunAccount struct {
	inner AccountUseCase
}

var _ AccountUseCase = DryRunAccount{}

// NewDryRunAccount wraps the real account usecase.
func NewDryRunAccount(inner AccountUseCase) DryRunAccount {
	return DryRunAccount{inner: inner}
}

// DeductOrder updates the virtual dry-run account balance and slots.
func (a DryRunAccount) DeductOrder(entryPrice float32) error {
	return a.inner.DeductOrder(entryPrice)
}

// AddOrder credits the returned capital and net profit (with fees deducted)
// back to the virtual dry-run account.
func (a DryRunAccount) AddOrder(exitPrice float32) error {
	return a.inner.AddOrder(exitPrice)
}

// GetDisponibleAmout reads the real account's per-order amount.
func (a DryRunAccount) GetDisponibleAmout() (float32, error) { return a.inner.GetDisponibleAmout() }

// CanOpenOrder reads the real account's order slots.
func (a DryRunAccount) CanOpenOrder() (bool, error) { return a.inner.CanOpenOrder() }

// GetAccount reads the real account (sizing basis only; never written).
func (a DryRunAccount) GetAccount() (entities.Account, error) { return a.inner.GetAccount() }
