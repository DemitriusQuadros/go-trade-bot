package settings

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/configuration"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockRepo struct{ mock.Mock }

func (m *mockRepo) Get(ctx context.Context) (*entities.Settings, error) {
	args := m.Called(ctx)
	s, _ := args.Get(0).(*entities.Settings)
	return s, args.Error(1)
}
func (m *mockRepo) Save(ctx context.Context, s *entities.Settings) error {
	args := m.Called(ctx, s)
	return args.Error(0)
}

type mockExchange struct{ mock.Mock }

func (m *mockExchange) SwapFromConfig(cfg *configuration.Configuration) error {
	args := m.Called(cfg)
	return args.Error(0)
}

type mockNotifier struct{ mock.Mock }

func (m *mockNotifier) Swap(cfg *configuration.Configuration) error {
	args := m.Called(cfg)
	return args.Error(0)
}

type mockGate struct {
	mock.Mock
	inFlight atomic.Int64
}

func (m *mockGate) SetDraining(d bool)   { m.Called(d) }
func (m *mockGate) InFlightCount() int64 { return m.inFlight.Load() }
func (m *mockGate) SetCeiling(mode strategies.ExecutionMode) {
	m.Called(mode)
}
func (m *mockGate) SetTestnet(t bool) { m.Called(t) }

func newMockGate(inFlight int64) *mockGate {
	g := &mockGate{}
	g.inFlight.Store(inFlight)
	return g
}

type mockWorkerClient struct{ mock.Mock }

func (m *mockWorkerClient) Apply(ctx context.Context, next entities.Settings, confirmLive bool) error {
	args := m.Called(ctx, next, confirmLive)
	return args.Error(0)
}

func baseSettings() entities.Settings {
	return entities.Settings{
		Mode: "dryrun", Testnet: true, WebhookURL: "https://old.example.com",
		BrokerApiKey: "oldkey", BrokerApiSecret: "oldsecret",
	}
}

func TestApply_SafeTierOnly_ImmediateNoGuard(t *testing.T) {
	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)

	current := baseSettings()
	repo.On("Get", mock.Anything).Return(&current, nil)

	next := current
	next.WebhookURL = "https://new.example.com"

	notif.On("Swap", mock.Anything).Return(nil)
	repo.On("Save", mock.Anything, &next).Return(nil)

	u := NewUseCase(repo, exch, notif, nil, nil)
	got, err := u.Apply(context.Background(), next, false)

	assert.NoError(t, err)
	assert.Equal(t, next, got)
	exch.AssertNotCalled(t, "SwapFromConfig", mock.Anything)
	notif.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestApply_RiskBearing_QuiescentSystem_SwapsImmediately(t *testing.T) {
	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)
	gate := newMockGate(0)

	current := baseSettings()
	repo.On("Get", mock.Anything).Return(&current, nil)

	next := current
	next.BrokerApiKey = "newkey"

	gate.On("SetDraining", true).Return()
	gate.On("SetDraining", false).Return()
	gate.On("SetCeiling", strategies.ModeDryRun).Return()
	gate.On("SetTestnet", true).Return()
	exch.On("SwapFromConfig", mock.Anything).Return(nil)
	notif.On("Swap", mock.Anything).Return(nil)
	repo.On("Save", mock.Anything, &next).Return(nil)

	u := NewUseCase(repo, exch, notif, gate, nil)
	got, err := u.Apply(context.Background(), next, false)

	assert.NoError(t, err)
	assert.Equal(t, next, got)
	exch.AssertExpectations(t)
	gate.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestApply_RiskBearing_DrainsBeforeSwapping(t *testing.T) {
	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)
	gate := newMockGate(1)

	current := baseSettings()
	repo.On("Get", mock.Anything).Return(&current, nil)

	next := current
	next.Testnet = false

	gate.On("SetDraining", true).Return()
	gate.On("SetDraining", false).Return()
	gate.On("SetCeiling", strategies.ModeDryRun).Return()
	gate.On("SetTestnet", false).Return()
	exch.On("SwapFromConfig", mock.Anything).Return(nil)
	notif.On("Swap", mock.Anything).Return(nil)
	repo.On("Save", mock.Anything, &next).Return(nil)

	// Simulate the in-flight cycle completing shortly after the drain begins.
	go func() {
		time.Sleep(50 * time.Millisecond)
		gate.inFlight.Store(0)
	}()

	u := NewUseCase(repo, exch, notif, gate, nil)
	got, err := u.Apply(context.Background(), next, false)

	assert.NoError(t, err)
	assert.Equal(t, next, got)
	exch.AssertExpectations(t)
}

func TestApply_RiskBearing_DrainTimeout_ReturnsErrorAndDoesNotSwapOrPersist(t *testing.T) {
	// Shrink the drain constants so this test exercises the real timeout
	// codepath deterministically and fast, rather than waiting out a real
	// 30s deadline.
	origTimeout, origPoll := drainTimeout, drainPollInterval
	drainTimeout = 50 * time.Millisecond
	drainPollInterval = 5 * time.Millisecond
	defer func() { drainTimeout, drainPollInterval = origTimeout, origPoll }()

	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)
	gate := newMockGate(1) // never reaches zero

	current := baseSettings()
	repo.On("Get", mock.Anything).Return(&current, nil)

	next := current
	next.Testnet = false

	gate.On("SetDraining", true).Return()
	gate.On("SetDraining", false).Return()

	u := NewUseCase(repo, exch, notif, gate, nil)
	_, err := u.Apply(context.Background(), next, false)

	assert.ErrorIs(t, err, ErrDrainTimeout)
	exch.AssertNotCalled(t, "SwapFromConfig", mock.Anything)
	notif.AssertNotCalled(t, "Swap", mock.Anything)
	repo.AssertNotCalled(t, "Save", mock.Anything, mock.Anything)
	gate.AssertExpectations(t) // SetDraining(true) then SetDraining(false) via defer
}

func TestApply_RiskBearing_SwapFails_DoesNotPersist(t *testing.T) {
	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)
	gate := newMockGate(0)

	current := baseSettings()
	repo.On("Get", mock.Anything).Return(&current, nil)

	next := current
	next.BrokerApiKey = "bad-key"

	gate.On("SetDraining", true).Return()
	gate.On("SetDraining", false).Return()
	exch.On("SwapFromConfig", mock.Anything).Return(assert.AnError)

	u := NewUseCase(repo, exch, notif, gate, nil)
	_, err := u.Apply(context.Background(), next, false)

	assert.ErrorIs(t, err, ErrSwapFailed)
	repo.AssertNotCalled(t, "Save", mock.Anything, mock.Anything)
	notif.AssertNotCalled(t, "Swap", mock.Anything)
}

func TestApply_ModeLiveWithoutConfirmLive_RejectedBeforeDrain(t *testing.T) {
	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)
	gate := newMockGate(0)

	current := baseSettings()
	// repo.Get must never even be consulted - the confirm-live check is a
	// cheap early guard checked before anything else (AC#7).
	next := current
	next.Mode = "live"

	u := NewUseCase(repo, exch, notif, gate, nil)
	_, err := u.Apply(context.Background(), next, false)

	assert.ErrorIs(t, err, ErrConfirmLiveRequired)
	repo.AssertNotCalled(t, "Get", mock.Anything)
	gate.AssertNotCalled(t, "SetDraining", mock.Anything)
}

func TestApply_ModeLiveWithConfirmLive_Allowed(t *testing.T) {
	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)
	gate := newMockGate(0)

	current := baseSettings()
	repo.On("Get", mock.Anything).Return(&current, nil)

	next := current
	next.Mode = "live"
	next.Testnet = false

	gate.On("SetDraining", true).Return()
	gate.On("SetDraining", false).Return()
	gate.On("SetCeiling", strategies.ModeLive).Return()
	gate.On("SetTestnet", false).Return()
	exch.On("SwapFromConfig", mock.Anything).Return(nil)
	notif.On("Swap", mock.Anything).Return(nil)
	repo.On("Save", mock.Anything, &next).Return(nil)

	u := NewUseCase(repo, exch, notif, gate, nil)
	_, err := u.Apply(context.Background(), next, true)
	assert.NoError(t, err)
}

func TestApply_ForwardsToWorkerClient_OnRiskBearingChange(t *testing.T) {
	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)
	worker := new(mockWorkerClient)

	current := baseSettings()
	repo.On("Get", mock.Anything).Return(&current, nil)

	next := current
	next.BrokerApiKey = "newkey"

	exch.On("SwapFromConfig", mock.Anything).Return(nil)
	notif.On("Swap", mock.Anything).Return(nil)
	worker.On("Apply", mock.Anything, next, false).Return(nil)
	repo.On("Save", mock.Anything, &next).Return(nil)

	// No gate (as in cmd/api's own instance) - drain is a no-op, but the
	// worker forwarding call must still happen and its result must gate
	// persistence.
	u := NewUseCase(repo, exch, notif, nil, worker)
	_, err := u.Apply(context.Background(), next, false)

	assert.NoError(t, err)
	worker.AssertExpectations(t)
}

func TestApply_WorkerClientFails_OnRiskBearingChange_DoesNotPersist(t *testing.T) {
	repo := new(mockRepo)
	exch := new(mockExchange)
	notif := new(mockNotifier)
	worker := new(mockWorkerClient)

	current := baseSettings()
	repo.On("Get", mock.Anything).Return(&current, nil)

	next := current
	next.BrokerApiKey = "newkey"

	exch.On("SwapFromConfig", mock.Anything).Return(nil)
	notif.On("Swap", mock.Anything).Return(nil)
	worker.On("Apply", mock.Anything, next, false).Return(ErrDrainTimeout)

	u := NewUseCase(repo, exch, notif, nil, worker)
	_, err := u.Apply(context.Background(), next, false)

	assert.ErrorIs(t, err, ErrDrainTimeout)
	repo.AssertNotCalled(t, "Save", mock.Anything, mock.Anything)
}
