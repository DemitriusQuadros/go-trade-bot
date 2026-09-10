package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/handler"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type UseCase interface {
	Save(ctx context.Context, strategy entities.Strategy) error
	Update(ctx context.Context, strategy entities.Strategy) error
	Enqueue(ctx context.Context) error
	GetAll(ctx context.Context) ([]entities.Strategy, error)
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
	GetPerformance(ctx context.Context) ([]entities.StrategyPerformance, error)
	UpdateStatus(ctx context.Context, id uint, status entities.StrategyStatus) (entities.Strategy, error)
	UpdateMode(ctx context.Context, id uint, mode string) (entities.Strategy, error)
	GetScriptVersions(ctx context.Context, strategyID uint) ([]entities.ScriptVersion, error)
	RevertScriptVersion(ctx context.Context, strategyID uint, versionID uint) error
}
type StrategyHandler struct {
	UseCase UseCase
}

func NewStrategyHandler(u UseCase) *StrategyHandler {
	return &StrategyHandler{
		UseCase: u,
	}
}

func (h *StrategyHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/strategy/{id}/versions",
			Action:  h.GetVersions,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/strategy/{id}/versions/{versionId}/revert",
			Action:  h.RevertVersion,
			Method:  http.MethodPost,
		},

		{
			Pattern: "/strategy",
			Action:  h.Post,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/strategy/enqueue",
			Action:  h.Enqueue,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/strategy/performance",
			Action:  h.GetPerformance,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/strategy",
			Action:  h.GetAll,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/strategy/{id:[0-9]+}/status",
			Action:  h.PatchStatus,
			Method:  http.MethodPatch,
		},
		{
			Pattern: "/strategy/{id:[0-9]+}/mode",
			Action:  h.PatchMode,
			Method:  http.MethodPatch,
		},
		{
			Pattern: "/strategy/{id}",
			Action:  h.Put,
			Method:  http.MethodPut,
		},
		{
			Pattern: "/strategy/{id}",
			Action:  h.GetById,
			Method:  http.MethodGet,
		},
	}
}

func (h *StrategyHandler) Post(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.Reader(r.Body))
	if err != nil {
		http.Error(w, "Invalid Body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var dto StrategyDto
	err = json.Unmarshal(body, &dto)

	if err != nil {
		http.Error(w, "Error converting body fields", http.StatusBadRequest)
		return
	}

	err = h.UseCase.Save(r.Context(), dto.ToModel())

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *StrategyHandler) Enqueue(w http.ResponseWriter, r *http.Request) {
	err := h.UseCase.Enqueue(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *StrategyHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	strategies, err := h.UseCase.GetAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToStrategyResponseList(strategies))
}

func (h *StrategyHandler) Put(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sId := vars["id"]
	id, err := strconv.Atoi(sId)

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.Reader(r.Body))
	if err != nil {
		http.Error(w, "Invalid Body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var dto StrategyDto
	err = json.Unmarshal(body, &dto)
	if err != nil {
		http.Error(w, "Error converting body fields", http.StatusBadRequest)
		return
	}

	strategy := dto.ToModel()
	strategy.ID = uint(id)

	err = h.UseCase.Update(r.Context(), strategy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *StrategyHandler) GetById(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	strategy, err := h.UseCase.GetByID(r.Context(), uint(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToStrategyResponse(strategy))
}

func (h *StrategyHandler) GetPerformance(w http.ResponseWriter, r *http.Request) {
	perfs, err := h.UseCase.GetPerformance(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(perfs)
}

type PatchStatusDTO struct {
	Status string `json:"status"`
}

func (h *StrategyHandler) PatchStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var dto PatchStatusDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, "Invalid Body", http.StatusBadRequest)
		return
	}

	strat, err := h.UseCase.UpdateStatus(r.Context(), uint(id), entities.StrategyStatus(dto.Status))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToStrategyResponse(strat))
}

type PatchModeDTO struct {
	Mode string `json:"mode"`
}

func (h *StrategyHandler) PatchMode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var dto PatchModeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, "Invalid Body", http.StatusBadRequest)
		return
	}

	strat, err := h.UseCase.UpdateMode(r.Context(), uint(id), dto.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToStrategyResponse(strat))
}

func (h *StrategyHandler) GetVersions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	versions, err := h.UseCase.GetScriptVersions(r.Context(), uint(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}

func (h *StrategyHandler) RevertVersion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	versionId, err := strconv.Atoi(mux.Vars(r)["versionId"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.UseCase.RevertScriptVersion(r.Context(), uint(id), uint(versionId)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Version reverted"})
}
