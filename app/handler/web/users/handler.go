// Package users is the admin user-management REST transport (auth-01 §5).
// Every route requires the admin capability.
package users

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	authusecase "go-trade-bot/app/usecase/auth"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/handler"
	"go-trade-bot/internal/middleware"

	"github.com/gorilla/mux"
)

// UseCase is the slice of app/usecase/auth this handler needs.
type UseCase interface {
	ListUsers(ctx context.Context) ([]authusecase.UserView, error)
	CreateUser(ctx context.Context, req authusecase.CreateUserRequest) (authusecase.UserView, error)
	UpdateUser(ctx context.Context, id uint, req authusecase.UpdateUserRequest) (authusecase.UserView, error)
	ResetPassword(ctx context.Context, id uint, password string) error
	DeleteUser(ctx context.Context, actor authz.Principal, id uint) error
}

// Handler serves /users*.
type Handler struct {
	useCase UseCase
}

// NewUsersHandler builds the handler.
func NewUsersHandler(u UseCase) *Handler { return &Handler{useCase: u} }

// Handlers implements the Route interface (cmd/api adds /api).
func (h *Handler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{Pattern: "/users", Method: http.MethodGet, Action: h.List, Capability: authz.CapAdmin},
		{Pattern: "/users", Method: http.MethodPost, Action: h.Create, Capability: authz.CapAdmin},
		{Pattern: "/users/{id:[0-9]+}", Method: http.MethodPut, Action: h.Update, Capability: authz.CapAdmin},
		{Pattern: "/users/{id:[0-9]+}", Method: http.MethodDelete, Action: h.Delete, Capability: authz.CapAdmin},
		{Pattern: "/users/{id:[0-9]+}/reset-password", Method: http.MethodPost, Action: h.ResetPassword, Capability: authz.CapAdmin},
	}
}

// UserResponse is one user (never includes the password hash).
type UserResponse struct {
	ID                  uint       `json:"id"`
	Username            string     `json:"username"`
	DisplayName         string     `json:"display_name"`
	Email               *string    `json:"email"`
	Role                string     `json:"role"`
	Capabilities        []string   `json:"capabilities"`
	DailyAgentBudgetUSD float64    `json:"daily_agent_budget_usd"`
	TodayAgentCostUSD   float64    `json:"today_agent_cost_usd"`
	Locale              string     `json:"locale"`
	Disabled            bool       `json:"disabled"`
	LastLoginAt         *time.Time `json:"last_login_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// ToUserResponse maps a usecase view.
func ToUserResponse(v authusecase.UserView) UserResponse {
	u := v.User
	caps := authz.NormalizeCapabilities(u.Capabilities)
	var last *time.Time
	if u.LastLoginAt != nil {
		t := u.LastLoginAt.UTC()
		last = &t
	}
	return UserResponse{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Role: u.Role, Capabilities: caps,
		DailyAgentBudgetUSD: u.DailyAgentBudgetUSD, TodayAgentCostUSD: v.TodayAgentCostUSD, Locale: u.Locale,
		Disabled: u.Disabled, LastLoginAt: last, CreatedAt: u.CreatedAt.UTC(), UpdatedAt: u.UpdatedAt.UTC(),
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

func pathID(r *http.Request) (uint, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 32)
	return uint(id), err == nil && id > 0
}

func badJSON(w http.ResponseWriter) {
	middleware.WriteJSONError(w, http.StatusBadRequest, "validation_error", "invalid request json")
}

// List serves GET /users.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	views, err := h.useCase.ListUsers(r.Context())
	if err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	out := make([]UserResponse, 0, len(views))
	for _, v := range views {
		out = append(out, ToUserResponse(v))
	}
	writeJSON(w, http.StatusOK, out)
}

type createRequest struct {
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Email       *string `json:"email"`
	Role        string  `json:"role"`
	Password    string  `json:"password"`
}

// Create serves POST /users (201).
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decode(r, &req); err != nil {
		badJSON(w)
		return
	}
	v, err := h.useCase.CreateUser(r.Context(), authusecase.CreateUserRequest{
		Username: req.Username, DisplayName: req.DisplayName, Email: req.Email, Role: req.Role, Password: req.Password,
	})
	if err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ToUserResponse(v))
}

type updateRequest struct {
	DisplayName         *string   `json:"display_name"`
	Email               *string   `json:"email"`
	Role                *string   `json:"role"`
	Capabilities        *[]string `json:"capabilities"`
	DailyAgentBudgetUSD *float64  `json:"daily_agent_budget_usd"`
	Disabled            *bool     `json:"disabled"`
}

// Update serves PUT /users/{id}. Omitted fields are left unchanged.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		middleware.WriteJSONError(w, http.StatusBadRequest, "validation_error", "invalid id")
		return
	}
	var req updateRequest
	if err := decode(r, &req); err != nil {
		badJSON(w)
		return
	}
	v, err := h.useCase.UpdateUser(r.Context(), id, authusecase.UpdateUserRequest{
		DisplayName: req.DisplayName, Email: req.Email, Role: req.Role, Capabilities: req.Capabilities,
		DailyAgentBudgetUSD: req.DailyAgentBudgetUSD, Disabled: req.Disabled,
	})
	if err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToUserResponse(v))
}

// ResetPassword serves POST /users/{id}/reset-password {password}.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		middleware.WriteJSONError(w, http.StatusBadRequest, "validation_error", "invalid id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		badJSON(w)
		return
	}
	if err := h.useCase.ResetPassword(r.Context(), id, req.Password); err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Delete serves DELETE /users/{id} (204); 400 for yourself or the last
// enabled admin.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		middleware.WriteJSONError(w, http.StatusBadRequest, "validation_error", "invalid id")
		return
	}
	actor, _ := authz.FromContext(r.Context())
	if err := h.useCase.DeleteUser(r.Context(), actor, id); err != nil {
		customerror.WriteJSON(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
