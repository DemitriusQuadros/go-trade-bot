// Package settingsbridge defines the wire contract between cmd/api's public
// PUT /settings handler and cmd/worker's internal
// POST /internal/settings/apply endpoint (Spec backend-05, ADR-016).
//
// cmd/api and cmd/worker are separate OS processes, each holding its own
// process-local exchange.SwappableExchangeClient/notifier.SwappableNotifier
// and (worker-only) the strategy execution loop's in-flight
// counter/admission gate the drain-then-swap sequence depends on. The spec's
// mechanism assumes a single in-memory process; this internal HTTP bridge is
// the minimal glue that makes the same drain-then-swap guarantee actually
// hold for the process that runs live trading cycles (cmd/worker), while
// keeping the public PUT /settings contract (status codes, response shapes,
// AC's) exactly what the frontend/spec expect at the cmd/api call site -
// cmd/api blocks on this call for risk-bearing changes for exactly that
// reason (see app/usecase/settings.UseCase's package doc for the full
// rationale).
//
// Security (post-review fix): this endpoint accepts an unauthenticated write
// that can change broker credentials and flip Mode to "live" - it must never
// be reachable from anything but cmd/api on the same host. It is served on
// its own listener bound to configuration.Configuration.InternalBridgeAddr
// (default 127.0.0.1-only, a distinct port from cmd/worker's public :9191
// monitoring server, which binds all interfaces) - never mounted on :9191.
// InternalBridgeSecret, when configured, is additionally checked via
// InternalSecretHeader as defense-in-depth for deployments where cmd/api and
// cmd/worker might not share a host/network-namespace.
package settingsbridge

import "go-trade-bot/app/entities"

// DefaultWorkerBaseURL is the default address of cmd/worker's dedicated,
// loopback-only internal settings-apply listener (Spec backend-05) - a
// distinct listener/port from cmd/worker's public :9191 monitoring server,
// which binds all interfaces and must never serve this endpoint.
const DefaultWorkerBaseURL = "http://127.0.0.1:9193"

const ApplyPath = "/internal/settings/apply"

// InternalSecretHeader carries configuration.Configuration.InternalBridgeSecret
// on every request cmd/api sends to this endpoint, checked by cmd/worker
// before processing (defense-in-depth on top of the loopback bind).
const InternalSecretHeader = "X-Internal-Bridge-Secret"

// ApplyRequest is the already-merged, already-validated (at the cmd/api
// layer) target Settings state - cmd/worker only re-applies it against its
// own process-local Swappables/gate, it does not re-run field-merge logic.
type ApplyRequest struct {
	Settings    entities.Settings `json:"settings"`
	ConfirmLive bool              `json:"confirm_live"`
}

// ErrorResponse is returned (with a matching non-200 status) whenever
// cmd/worker's usecase.UseCase.Apply call fails.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// SuccessResponse is returned with HTTP 200 on a successful apply.
type SuccessResponse struct {
	Applied bool `json:"applied"`
}
