package modules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/settings"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/settingsbridge"
)

// workerSettingsClient implements usecase.WorkerClient by forwarding an
// already-validated Apply call to cmd/worker's internal, loopback-only
// POST /internal/settings/apply endpoint (see internal/settingsbridge's
// package doc for the full cross-process rationale, and why this must never
// point at cmd/worker's public :9191 monitoring server). This is what makes
// cmd/worker's own SwappableExchangeClient/SwappableNotifier/StrategyProcessor
// actually observe a settings change made through cmd/api's public
// PUT /settings, not just cmd/api's own process-local copies.
type workerSettingsClient struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewWorkerSettingsClient's timeout (35s) is deliberately longer than the
// settings usecase's own 30s drain deadline (Spec backend-05 SS4), so a
// legitimate drain-timeout response from the worker has time to actually
// come back as a clean 503 rather than this client timing out first and
// masking it as a generic connection error.
//
// baseURL/secret are derived from cfg.InternalBridgeAddr/InternalBridgeSecret
// (internal/configuration) rather than settingsbridge's default constant, so
// an operator running cmd/api and cmd/worker on separate hosts can point
// this at the right address via config.yml/env without a code change.
func NewWorkerSettingsClient(cfg *configuration.Configuration) usecase.WorkerClient {
	baseURL := settingsbridge.DefaultWorkerBaseURL
	if cfg.InternalBridgeAddr != "" {
		baseURL = "http://" + cfg.InternalBridgeAddr
	}
	return &workerSettingsClient{
		baseURL: baseURL,
		secret:  cfg.InternalBridgeSecret,
		http:    &http.Client{Timeout: 35 * time.Second},
	}
}

func (c *workerSettingsClient) Apply(ctx context.Context, next entities.Settings, confirmLive bool) error {
	payload, err := json.Marshal(settingsbridge.ApplyRequest{Settings: next, ConfirmLive: confirmLive})
	if err != nil {
		return fmt.Errorf("%w: encoding request: %v", usecase.ErrSwapFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+settingsbridge.ApplyPath, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: building request: %v", usecase.ErrSwapFailed, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		req.Header.Set(settingsbridge.InternalSecretHeader, c.secret)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: worker unreachable: %v", usecase.ErrSwapFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	var body settingsbridge.ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return usecase.ErrConfirmLiveRequired
	case http.StatusServiceUnavailable:
		return usecase.ErrDrainTimeout
	default:
		return fmt.Errorf("%w: worker responded %d: %s", usecase.ErrSwapFailed, resp.StatusCode, body.Message)
	}
}
