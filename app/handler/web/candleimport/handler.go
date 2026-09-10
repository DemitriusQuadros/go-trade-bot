package candleimport

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/candleimport"
	"go-trade-bot/internal/handler"
	"github.com/gorilla/mux"
)

type CandleImportHandler struct {
	UseCase candleimport.UseCase
}

func NewCandleImportHandler(uc candleimport.UseCase) *CandleImportHandler {
	return &CandleImportHandler{UseCase: uc}
}

func (h *CandleImportHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/candles/import",
			Action:  h.PostImport,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/candles/import/{job_id}",
			Action:  h.GetJob,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/candles/schedule",
			Action:  h.PostSchedule,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/candles/schedule",
			Action:  h.GetSchedules,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/candles/schedule/{id}",
			Action:  h.PatchSchedule,
			Method:  http.MethodPatch,
		},
		{
			Pattern: "/candles/schedule/{id}",
			Action:  h.DeleteSchedule,
			Method:  http.MethodDelete,
		},
	}
}

func (h *CandleImportHandler) PostImport(w http.ResponseWriter, r *http.Request) {
	var req entities.ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobID, err := h.UseCase.CreateJob(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": jobID,
		"status": string(entities.ImportJobPending),
	})
}

func (h *CandleImportHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["job_id"]

	job, err := h.UseCase.GetJob(r.Context(), jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	var res interface{}
	if len(job.ResultJSON) > 0 {
		json.Unmarshal(job.ResultJSON, &res)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id": job.ID,
		"status": string(job.Status),
		"result": res,
		"error":  job.ErrorMessage,
	})
}

func (h *CandleImportHandler) PostSchedule(w http.ResponseWriter, r *http.Request) {
	var sched entities.ImportSchedule
	if err := json.NewDecoder(r.Body).Decode(&sched); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sched.Enabled = true // default
	if err := h.UseCase.CreateSchedule(r.Context(), &sched); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sched)
}

func (h *CandleImportHandler) GetSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.UseCase.ListSchedules(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(schedules)
}

func (h *CandleImportHandler) PatchSchedule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 32)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	schedules, err := h.UseCase.ListSchedules(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	var target *entities.ImportSchedule
	for i := range schedules {
		if schedules[i].ID == uint(id) {
			target = &schedules[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "schedule not found", http.StatusNotFound)
		return
	}

	if enabled, ok := req["enabled"].(bool); ok {
		target.Enabled = enabled
	}
	if spec, ok := req["cron_spec"].(string); ok {
		target.CronSpec = spec
	}

	if err := h.UseCase.UpdateSchedule(r.Context(), target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(target)
}

func (h *CandleImportHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 32)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.UseCase.DeleteSchedule(r.Context(), uint(id)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
