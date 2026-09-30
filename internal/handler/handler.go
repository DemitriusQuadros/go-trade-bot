package handler

import "net/http"

// Configuration is one HTTP route a handler declares (cmd/api mounts it
// under /api).
type Configuration struct {
	Pattern string
	Method  string
	Action  func(http.ResponseWriter, *http.Request)
	// Capability is the internal/authz capability this route requires
	// (auth-01 §3), or authz.CapPublic. cmd/api's NewServeMux panics on an
	// empty one, so a new route can never ship unprotected by accident.
	Capability string
}
