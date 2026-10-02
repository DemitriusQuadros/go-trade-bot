package authz

import "fmt"

// CapPublic marks the handful of routes that need no session: the /auth
// login/logout/me routes (auth-01 §4). It is an explicit, non-empty value so
// NewServeMux's "every route has a capability" check still holds.
const CapPublic = "public"

// Route -> capability map (auth-01 §3). The capability is declared on each
// route's handler.Configuration.Capability, next to its pattern; this table
// is the policy those declarations follow:
//
//	view               every GET not listed below, incl. /stream/* (SSE),
//	                   /agent-reports/* (incl. /html) and /backtest/{id}/report
//	admin (GET)        /settings, /webhook-targets, /deploy-gate, /users
//	backtest           POST /backtest, POST /backtest/walkforward,
//	                   POST /backtest/{id}/montecarlo, POST /optimize,
//	                   POST /script/repl, POST /script/fast-rerun
//	edit_drafts        POST /strategy, PUT /strategy/{id}, DELETE /strategy/{id},
//	                   POST /strategy/{id}/versions/{versionId}/revert,
//	                   POST /strategies/{id}/memory, DELETE /backtest/{id}
//	agent_chat         POST /agent/runs
//	approve_proposals  POST /proposals/{id}/approve|reject
//	admin              every other write: settings, deploy gate, kill switch,
//	                   agent persona CRUD/pause/run, strategy status/mode,
//	                   strategy enqueue, signal close, candle datasets,
//	                   webhook targets (incl. /test), POST /account, /users*
//	public             /auth/login, /auth/logout, GET|PATCH /auth/me
//	                   (the handlers check the session themselves)
//
// Anything new defaults to admin for writes and view for GETs.

// IsValidRouteCapability reports whether c may be declared on a route.
func IsValidRouteCapability(c string) bool {
	return c == CapPublic || IsValidCapability(c)
}

// MustRouteCapability panics when a route declares no (or an unknown)
// capability - so a new route can never ship unprotected by accident.
func MustRouteCapability(method, pattern, c string) {
	if c == "" {
		panic(fmt.Sprintf("authz: route %s %s has no capability", method, pattern))
	}
	if !IsValidRouteCapability(c) {
		panic(fmt.Sprintf("authz: route %s %s has unknown capability %q", method, pattern, c))
	}
}
