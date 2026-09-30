// Package auth is the /auth REST transport (auth-01 §4): login, logout and
// the current user. These are the only routes that do not require a
// session (capability authz.CapPublic); /auth/me and PATCH /auth/me check
// the principal themselves.
package auth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	authusecase "go-trade-bot/app/usecase/auth"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/handler"
	"go-trade-bot/internal/middleware"
)

// UseCase is the slice of app/usecase/auth this handler needs.
type UseCase interface {
	Login(ctx context.Context, username, password, ip, userAgent string) (authusecase.LoginResult, error)
	Logout(ctx context.Context, raw string) error
	Me(ctx context.Context, p authz.Principal) (authusecase.Me, error)
	UpdateMe(ctx context.Context, p authz.Principal, req authusecase.UpdateMeRequest) (authusecase.Me, error)
	SetupRequired(ctx context.Context) (bool, error)
}

// Handler serves /auth/*.
type Handler struct {
	useCase UseCase
	cfg     *configuration.Configuration
}

// NewAuthHandler builds the handler.
func NewAuthHandler(u UseCase, cfg *configuration.Configuration) *Handler {
	if cfg == nil {
		cfg = &configuration.Configuration{}
	}
	return &Handler{useCase: u, cfg: cfg}
}

// Handlers implements the Route interface (cmd/api adds /api).
func (h *Handler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{Pattern: "/auth/login", Method: http.MethodPost, Action: h.Login, Capability: authz.CapPublic},
		{Pattern: "/auth/logout", Method: http.MethodPost, Action: h.Logout, Capability: authz.CapPublic},
		{Pattern: "/auth/me", Method: http.MethodGet, Action: h.GetMe, Capability: authz.CapPublic},
		{Pattern: "/auth/me", Method: http.MethodPatch, Action: h.PatchMe, Capability: authz.CapPublic},
	}
}

// MeResponse is GET /auth/me and the login response's "user".
type MeResponse struct {
	ID                  uint     `json:"id"`
	Username            string   `json:"username"`
	DisplayName         string   `json:"display_name"`
	Email               *string  `json:"email"`
	Role                string   `json:"role"`
	Capabilities        []string `json:"capabilities"`
	Locale              string   `json:"locale"`
	DailyAgentBudgetUSD float64  `json:"daily_agent_budget_usd"`
	TodayAgentCostUSD   float64  `json:"today_agent_cost_usd"`
}

// ToMeResponse maps the usecase view.
func ToMeResponse(m authusecase.Me) MeResponse {
	caps := m.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return MeResponse{
		ID: m.ID, Username: m.Username, DisplayName: m.DisplayName, Email: m.Email, Role: m.Role,
		Capabilities: caps, Locale: m.Locale, DailyAgentBudgetUSD: m.DailyAgentBudgetUSD, TodayAgentCostUSD: m.TodayAgentCostUSD,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, v)
}

// ClientIP is the request's client address: Cf-Connecting-IP or the first
// X-Forwarded-For entry when AUTH.TRUST_PROXY is on, else RemoteAddr.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip := strings.TrimSpace(r.Header.Get("Cf-Connecting-IP")); ip != "" {
			return ip
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login serves POST /auth/login: 200 {"user": {...}} + the session cookie,
// 401 {"error":"invalid_credentials"} or 429 {"error":"rate_limited"}.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decode(r, &req); err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "validation_error", "invalid request json")
		return
	}
	res, err := h.useCase.Login(r.Context(), req.Username, req.Password, ClientIP(r, h.cfg.Auth.TrustProxy), r.UserAgent())
	if err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	middleware.SetSessionCookie(w, r, res.Token, time.Now().Add(authusecase.SessionTTL), h.cfg.Auth.TrustProxy)
	me, err := h.useCase.Me(r.Context(), authusecase.PrincipalFor(res.User, 0))
	if err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": ToMeResponse(me)})
}

// Logout serves POST /auth/logout: deletes the session, clears the cookie.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(middleware.SessionCookieName); err == nil {
		_ = h.useCase.Logout(r.Context(), c.Value)
	}
	middleware.ClearSessionCookie(w, r, h.cfg.Auth.TrustProxy)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) unauthorized(w http.ResponseWriter, r *http.Request) {
	setup, _ := h.useCase.SetupRequired(r.Context())
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"error": "unauthorized", "message": "sign in required", "setup_required": setup,
	})
}

// GetMe serves GET /auth/me: the current user, or 401 with
// "setup_required": true when there are no users yet.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	p, ok := authz.FromContext(r.Context())
	if !ok {
		h.unauthorized(w, r)
		return
	}
	me, err := h.useCase.Me(r.Context(), p)
	if err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToMeResponse(me))
}

type patchMeRequest struct {
	DisplayName     *string `json:"display_name"`
	Locale          *string `json:"locale"`
	CurrentPassword *string `json:"current_password"`
	NewPassword     *string `json:"new_password"`
}

// PatchMe serves PATCH /auth/me.
func (h *Handler) PatchMe(w http.ResponseWriter, r *http.Request) {
	p, ok := authz.FromContext(r.Context())
	if !ok {
		h.unauthorized(w, r)
		return
	}
	var req patchMeRequest
	if err := decode(r, &req); err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "validation_error", "invalid request json")
		return
	}
	me, err := h.useCase.UpdateMe(r.Context(), p, authusecase.UpdateMeRequest{
		DisplayName: req.DisplayName, Locale: req.Locale, CurrentPassword: req.CurrentPassword, NewPassword: req.NewPassword,
	})
	if err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToMeResponse(me))
}
