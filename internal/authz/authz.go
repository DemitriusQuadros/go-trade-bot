// Package authz holds the multi-user authorization vocabulary (auth-01 §2/§3):
// capabilities, role presets and the acting user ("principal") carried in a
// request context.
//
// A context WITHOUT a principal is "system": agent tools, background tasks,
// cmd/agent and cmd/mcp. Usecases that apply per-user rules (the strategy
// draft-only guard) must treat a missing principal as system and not apply
// them - the agent write scope and the trading safety gates already apply.
package authz

import "context"

// Capabilities (auth-01 §2). admin implies every other capability.
const (
	CapView             = "view"
	CapBacktest         = "backtest"
	CapEditDrafts       = "edit_drafts"
	CapAgentChat        = "agent_chat"
	CapApproveProposals = "approve_proposals"
	CapAdmin            = "admin"
)

// AllCapabilities is every grantable capability, in display order.
var AllCapabilities = []string{CapView, CapBacktest, CapEditDrafts, CapAgentChat, CapApproveProposals, CapAdmin}

// Roles (auth-01 §2).
const (
	RoleAdmin  = "admin"
	RoleFriend = "friend"
	RoleViewer = "viewer"
)

// IsValidRole reports whether role is a known role.
func IsValidRole(role string) bool {
	switch role {
	case RoleAdmin, RoleFriend, RoleViewer:
		return true
	}
	return false
}

// IsValidCapability reports whether c is a known capability.
func IsValidCapability(c string) bool {
	for _, k := range AllCapabilities {
		if k == c {
			return true
		}
	}
	return false
}

// RolePreset returns the capabilities a role starts with. An unknown role
// gets nothing.
func RolePreset(role string) []string {
	switch role {
	case RoleAdmin:
		return append([]string(nil), AllCapabilities...)
	case RoleFriend:
		return []string{CapView, CapBacktest, CapEditDrafts, CapAgentChat, CapApproveProposals}
	case RoleViewer:
		return []string{CapView}
	}
	return []string{}
}

// NormalizeCapabilities drops unknown/duplicate entries and returns the rest
// in AllCapabilities order.
func NormalizeCapabilities(caps []string) []string {
	set := map[string]bool{}
	for _, c := range caps {
		if IsValidCapability(c) {
			set[c] = true
		}
	}
	out := make([]string, 0, len(set))
	for _, c := range AllCapabilities {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}

// HasCapability reports whether caps grants c (admin implies everything).
func HasCapability(caps []string, c string) bool {
	for _, k := range caps {
		if k == c || k == CapAdmin {
			return true
		}
	}
	return false
}

// Principal is the acting user of a request.
type Principal struct {
	// UserID is 0 for synthetic principals (the API_TOKEN service principal
	// and the ALLOW_INSECURE_NO_AUTH principal).
	UserID      uint
	Username    string
	DisplayName string
	Role        string
	Caps        []string
	Locale      string
	// SessionID is the session that authenticated the request (0 for
	// bearer/insecure principals).
	SessionID uint
	// Service marks a synthetic principal (API_TOKEN bearer or insecure mode).
	Service bool
}

// Has reports whether the principal holds capability c.
func (p Principal) Has(c string) bool { return HasCapability(p.Caps, c) }

// IsAdmin reports whether the principal holds the admin capability.
func (p Principal) IsAdmin() bool { return p.Has(CapAdmin) }

// UserIDPtr returns a pointer to UserID, or nil for synthetic principals.
func (p Principal) UserIDPtr() *uint {
	if p.UserID == 0 {
		return nil
	}
	id := p.UserID
	return &id
}

// ServicePrincipal is the synthetic admin principal behind
// Authorization: Bearer <API_TOKEN> (auth-01 §4).
func ServicePrincipal() Principal {
	return Principal{Username: "service", DisplayName: "service", Role: RoleAdmin, Caps: RolePreset(RoleAdmin), Service: true}
}

// InsecurePrincipal is the synthetic admin principal used when
// ALLOW_INSECURE_NO_AUTH=true.
func InsecurePrincipal() Principal {
	return Principal{Username: "insecure", DisplayName: "insecure (no auth)", Role: RoleAdmin, Caps: RolePreset(RoleAdmin), Service: true}
}

type ctxKey struct{}

// principalValue wraps the principal so WithoutUser can store an explicit
// "none" that shadows a parent context's principal.
type principalValue struct {
	p  Principal
	ok bool
}

// WithUser returns ctx carrying p as the acting user.
func WithUser(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, principalValue{p: p, ok: true})
}

// WithoutUser returns ctx with no acting user (system), shadowing any
// principal of the parent. The chat handler uses it before running agent
// tools, which act as system (auth-01 §3).
func WithoutUser(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, principalValue{})
}

// FromContext returns the acting user, or ok=false for system.
func FromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	v, _ := ctx.Value(ctxKey{}).(principalValue)
	return v.p, v.ok
}
