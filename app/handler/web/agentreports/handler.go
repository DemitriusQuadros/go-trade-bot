// Package agentreports is the REST transport for agent reports
// (agents-platform A-02 §5): list, detail, and the rendered HTML served for
// a sandboxed iframe.
package agentreports

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/agentplatform"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/handler"
	"go-trade-bot/internal/i18n"

	"github.com/gorilla/mux"
)

// UseCase is the slice of app/usecase/agentplatform this handler needs.
type UseCase interface {
	ListReports(ctx context.Context, f usecase.ReportFilter) ([]entities.AgentReport, error)
	GetReport(ctx context.Context, id uint) (entities.AgentReport, error)
	AgentNames(ctx context.Context) map[uint]string
}

// Handler serves /agent-reports.
type Handler struct {
	useCase UseCase
}

// NewAgentReportsHandler builds the handler.
func NewAgentReportsHandler(u UseCase) *Handler {
	return &Handler{useCase: u}
}

// Handlers implements the Route interface.
func (h *Handler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{Pattern: "/agent-reports", Method: http.MethodGet, Action: h.List, Capability: authz.CapView},
		{Pattern: "/agent-reports/{id:[0-9]+}", Method: http.MethodGet, Action: h.Get, Capability: authz.CapView},
		{Pattern: "/agent-reports/{id:[0-9]+}/html", Method: http.MethodGet, Action: h.HTML, Capability: authz.CapView},
	}
}

// ReportSummaryResponse is one list row.
type ReportSummaryResponse struct {
	ID          uint   `json:"id"`
	AgentID     uint   `json:"agent_id"`
	AgentName   string `json:"agent_name"`
	AgentRunID  *uint  `json:"agent_run_id"` // null for reports written over MCP (no run)
	StrategyIDs []uint `json:"strategy_ids"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Summary     string `json:"summary"`
	CreatedAt   string `json:"created_at"`
}

// ReportResponse is the detail view: summary fields + raw blocks.
type ReportResponse struct {
	ReportSummaryResponse
	Blocks json.RawMessage `json:"blocks"`
}

func toSummary(r entities.AgentReport, names map[uint]string) ReportSummaryResponse {
	ids := []uint(r.StrategyIDs)
	if ids == nil {
		ids = []uint{}
	}
	out := ReportSummaryResponse{
		ID: r.ID, AgentID: r.AgentID, AgentName: names[r.AgentID], StrategyIDs: ids, Title: r.Title,
		Severity: string(r.Severity), Summary: r.Summary, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
	if r.AgentRunID != 0 {
		id := r.AgentRunID
		out.AgentRunID = &id
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status, msg := http.StatusInternalServerError, err.Error()
	var ce *customerror.CustomError
	if errors.As(err, &ce) {
		status, msg = ce.Code, ce.Message
	}
	code := map[int]string{http.StatusBadRequest: "validation_error", http.StatusNotFound: "not_found"}[status]
	if code == "" {
		code = "internal_error"
	}
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func optUint(r *http.Request, name string) (*uint, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || v == 0 {
		return nil, &customerror.CustomError{Code: http.StatusBadRequest, Message: "invalid " + name}
	}
	u := uint(v)
	return &u, nil
}

// List is GET /agent-reports?agent_id=&strategy_id=&severity=&limit=&before_id=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	var f usecase.ReportFilter
	var err error
	if f.AgentID, err = optUint(r, "agent_id"); err != nil {
		writeError(w, err)
		return
	}
	if f.StrategyID, err = optUint(r, "strategy_id"); err != nil {
		writeError(w, err)
		return
	}
	if f.BeforeID, err = optUint(r, "before_id"); err != nil {
		writeError(w, err)
		return
	}
	limit, err := optUint(r, "limit")
	if err != nil {
		writeError(w, err)
		return
	}
	if limit != nil {
		f.Limit = int(*limit)
	}
	f.Severity = r.URL.Query().Get("severity")

	reps, err := h.useCase.ListReports(r.Context(), f)
	if err != nil {
		writeError(w, err)
		return
	}
	names := h.useCase.AgentNames(r.Context())
	out := make([]ReportSummaryResponse, 0, len(reps))
	for _, rep := range reps {
		out = append(out, toSummary(rep, names))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) load(w http.ResponseWriter, r *http.Request) (entities.AgentReport, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 32)
	if err != nil {
		writeError(w, &customerror.CustomError{Code: http.StatusBadRequest, Message: "invalid id"})
		return entities.AgentReport{}, false
	}
	rep, err := h.useCase.GetReport(r.Context(), uint(id))
	if err != nil {
		writeError(w, err)
		return entities.AgentReport{}, false
	}
	return rep, true
}

// Get is GET /agent-reports/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	rep, ok := h.load(w, r)
	if !ok {
		return
	}
	blocks := json.RawMessage(rep.BlocksJSON)
	if len(blocks) == 0 || !json.Valid(blocks) {
		blocks = json.RawMessage(`[]`)
	}
	writeJSON(w, http.StatusOK, ReportResponse{ReportSummaryResponse: toSummary(rep, h.useCase.AgentNames(r.Context())), Blocks: blocks})
}

// ContentSecurityPolicy locks the report down: no scripts, no network, only
// inline styles and data: images.
const ContentSecurityPolicy = "default-src 'none'; style-src 'unsafe-inline'; img-src data:"

// htmlOpenTag matches how internal/report/agentreport's template opens the
// document (<html lang="en|es|pt-BR">); ?theme= injects a class onto it.
var htmlOpenTag = regexp.MustCompile(`<html lang="([A-Za-z-]{1,16})">`)

// HTML is GET /agent-reports/{id}/html[?theme=dark|light][&lang=en|es|pt-BR]
// - the stored, server-rendered report. ?lang= serves that locale's
// write-time snapshot (i18n-02 §3), falling back to RenderedHTML (reports
// written before i18n-02, or an unknown lang). Without ?theme the report
// follows prefers-color-scheme.
func (h *Handler) HTML(w http.ResponseWriter, r *http.Request) {
	theme := r.URL.Query().Get("theme")
	if theme != "" && theme != "dark" && theme != "light" {
		writeError(w, &customerror.CustomError{Code: http.StatusBadRequest, Message: "theme must be dark or light"})
		return
	}
	rep, ok := h.load(w, r)
	if !ok {
		return
	}
	body := rep.RenderedHTML
	if loc, ok := i18n.Parse(r.URL.Query().Get("lang")); ok {
		body = rep.HTMLForLocale(string(loc))
	}
	if theme != "" {
		// theme is one of two constants and lang is captured from our own
		// template's tag - never request text - so this cannot inject markup.
		if m := htmlOpenTag.FindStringSubmatchIndex(body); m != nil {
			lang := body[m[2]:m[3]]
			body = body[:m[0]] + `<html lang="` + lang + `" class="` + theme + `">` + body[m[1]:]
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
