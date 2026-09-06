# Spec backend-06 — Console Observability (`/metrics` on a debug port)

## Overview

Blueprint §4 Phase 3 wants `cmd/console` to expose an optional `/metrics` endpoint on a debug port
(config-gated), plus a `go-console` Prometheus scrape job — closing the last "process with zero remote
visibility" gap named in blueprint §7.1. This is intentionally a small, additive spec: the console's TUI
rewrite itself (main.go's overall structure, `apiclient`, pages) belongs to the concurrent
`frontend-specialist` agent's `tui-*.md` specs; this spec only defines the metrics-endpoint contract that
work must accommodate.

## Current Behavior (verified)

- `cmd/console/main.go` (current, pre-ADR-007-rewrite) is a flat `func main()`: initializes
  `dependencies.Init()`, sets up `termui`, constructs the 3 existing pages directly, and runs a blocking
  `ui.PollEvents()` loop — no HTTP server, no metrics, no config-gated anything. Confirmed no
  `net/http`, `promhttp`, or port-binding code exists anywhere under `cmd/console/`.
- `prometheus.yml` (current) has exactly two scrape jobs, `go-app` and `go-worker` — no `go-console` entry.
- `internal/metrics/collector.go` (Phase 1, unchanged) is process-agnostic — any process can construct a
  `*MetricsCollector` and mount `promhttp.Handler()`; nothing about it is `cmd/api`/`cmd/worker`-specific.
  `cmd/api/main.go:93`'s existing `/metrics` mount (Phase 1, confirmed working) is the template this spec's
  console addition follows.

## Target Behavior

```go
// cmd/console/metrics.go (NEW, small — deliberately not folded into main.go so the
// frontend-specialist agent's main.go rewrite has a stable, independent piece to import
// rather than a merge conflict with this spec's addition)
package main

import (
    "net/http"

    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// StartDebugMetricsServer mounts /metrics on cfg.Console.MetricsPort if
// cfg.Console.MetricsEnabled is true. No-op otherwise - this is opt-in,
// since a TUI's debug port is a homelab-local convenience, not something
// every console invocation should bind by default (e.g. running the console
// twice against the same machine for testing shouldn't collide on a fixed
// port unless explicitly enabled).
func StartDebugMetricsServer(enabled bool, port string) {
    if !enabled {
        return
    }
    go func() {
        mux := http.NewServeMux()
        mux.Handle("/metrics", promhttp.Handler())
        http.ListenAndServe(":"+port, mux) // errors logged, not fatal - see AC#3
    }()
}
```

```go
// internal/configuration/configuration.go — additive fields
type Configuration struct {
    // ...existing fields...
    Console ConsoleConfig
}

type ConsoleConfig struct {
    MetricsEnabled bool   // default false
    MetricsPort    string // default "9192"
}
```

`cmd/console/main.go` (whatever shape the frontend-specialist's rewrite lands on) calls
`StartDebugMetricsServer(cfg.Console.MetricsEnabled, cfg.Console.MetricsPort)` once, early in startup,
alongside (not instead of) the existing `dependencies.Init()`/`apiclient` construction — this call is
independent of and has no data dependency on the TUI rewrite's internals, which is why this spec keeps it
in its own file rather than prescribing exactly where in the rewritten `main()` it's called.

### What gets exposed

Reusing the exact `internal/metrics.MetricsCollector` pattern (Phase 1/2's `MetricConfig`-driven
registration): `go_goroutines`, `process_resident_memory_bytes` (free via the default registry, matching
`cmd/api`/`cmd/worker`'s existing behavior) plus one console-specific gauge:
```go
{
    Name: "console_render_loop_healthy",
    Help: "1 if the TUI's render/event loop is actively processing, 0 if stalled.",
    Type: metrics.Gauge,
}
```
set to `1` on each iteration of the TUI's main event loop (wherever the frontend-specialist's rewrite lands
that loop) and left unset — read as `0`/stale by a `up`-style staleness check in Grafana — if the process
hangs. This spec defines the metric's existence and meaning; the exact call site inside the rewritten event
loop is the frontend-specialist's implementation detail, not fixed by this spec.

`prometheus.yml` gains:
```yaml
- job_name: 'go-console'
  static_configs:
    - targets: ['host.docker.internal:9192']
```
guarded by the same understanding as the config flag: this scrape job is only meaningful when
`MetricsEnabled=true` on at least one running console instance; an unreachable target when the console
isn't running (the common case — a TUI is not a long-lived daemon in the same sense as `api`/`worker`) shows
up as `up{job="go-console"} == 0` in Grafana, which is the **correct**, expected signal for "console isn't
currently open," not a false alarm — `docs/grafana/process_health.json`'s (Phase 2 Spec 11) `up{job=~"go-app
|go-worker|go-console"}` panel already anticipates this per-process pattern.

## Acceptance Criteria

1. **Given** `Console.MetricsEnabled = false` (the default), **when** the console starts, **then** no port
   is bound and no `/metrics` server runs — zero behavior change from today for any operator who doesn't
   opt in.
2. **Given** `Console.MetricsEnabled = true` and `Console.MetricsPort = "9192"`, **when** the console
   starts, **then** `GET http://localhost:9192/metrics` returns `200` with Prometheus exposition format
   including `go_goroutines` and `console_render_loop_healthy`.
3. **Given** the configured port is already in use (e.g. two console instances started with metrics enabled
   on the same machine), **when** `ListenAndServe` fails, **then** the error is logged and the TUI itself
   continues running normally — a metrics-port bind failure must never crash or block the actual terminal
   UI, since the UI is this process's primary purpose and the metrics endpoint is a debug convenience.
4. **Given** the TUI's event loop is actively processing (normal operation), **when** `/metrics` is
   scraped, **then** `console_render_loop_healthy` reads `1`.
5. **Edge case — console closed normally**: **given** an operator quits the TUI (`<Escape>`, per the
   existing keybinding), **when** the process exits, **then** the metrics HTTP server shuts down with it
   (it runs in a goroutine of the same process, per Target Behavior — no separate lifecycle to manage or
   leak).
6. **Edge case — `go-console` scrape job with no running console**: **given** no console instance is
   currently running with metrics enabled, **when** Prometheus scrapes the `go-console` job, **then** the
   scrape fails cleanly (`up{job="go-console"} == 0`) — this spec does not attempt to make the scrape
   target conditional or dynamic; a static, occasionally-unreachable target is the accepted, simple design
   for a TUI that isn't always running.

## Out of Scope

- Any TUI page/component work, `apiclient`, or `main.go`'s overall structure — entirely the
  `frontend-specialist` agent's `tui-*.md` specs; this spec only defines the metrics contract those specs
  must accommodate (one function call, one config struct).
- Alerting on `go-console` being down — explicitly wrong: an intermittently-down console is expected,
  normal behavior (it's a terminal session, not a daemon), so no alert rule (Phase 2 Spec 11's
  `alerting/rules.yml`) should fire on `up{job="go-console"} == 0` the way it correctly does for `go-app`/
  `go-worker`. If Phase 2's alert rules need a scoping fix to exclude `go-console` from the existing
  `ProcessDown` rule, that's a small follow-up to that spec's `rules.yml`, not addressed here.
- Multiple simultaneous console instances each needing a distinct port — a fixed configured port is
  sufficient for this project's single-operator homelab scope; port-per-instance negotiation is not needed.

## Dependencies

- Phase 1's `internal/metrics/collector.go` — reused unmodified.
- Phase 2's `docs/specs/phase-2/11-observability-phase2.md` (`process_health.json`'s per-process `up` panel
  already anticipates this addition).
- The concurrent `frontend-specialist` agent's Phase 3 TUI rewrite — this spec's `StartDebugMetricsServer`
  call must be wired into whatever `main()` shape that work produces; coordinate the call site, not the
  function's own implementation, which is fully specified here independent of that work.
