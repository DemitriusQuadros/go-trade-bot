package agent

import (
	"encoding/json"

	"go-trade-bot/app/entities"
)

// toolCallResponse mirrors app/usecase/agent's private toolCallRecord shape
// (Tool/Args/Result/Error/Timestamp) - it is the actual shape persisted to
// AgentRun.ToolCallsJSON, not the frontend spec's originally-assumed
// {tool,args,result_summary} shape. The frontend renders whatever this DTO
// exposes, so this is the one place the two are kept in sync.
type toolCallResponse struct {
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args,omitempty"`
	Result    string          `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// AgentRunResponse is the snake_case DTO for entities.AgentRun - raw
// entities are never JSON-encoded directly in this codebase (no json tags,
// PascalCase keys), matching every other app/handler/web/* response.
type AgentRunResponse struct {
	ID           uint               `json:"id"`
	Provider     string             `json:"provider"`
	Model        string             `json:"model"`
	Trigger      string             `json:"trigger"`
	Status       string             `json:"status"`
	ErrorMessage string             `json:"error_message,omitempty"`
	InputSummary string             `json:"input_summary"`
	ResponseText string             `json:"response_text,omitempty"`
	ToolCalls    []toolCallResponse `json:"tool_calls"`
	StrategyID   *uint              `json:"strategy_id,omitempty"`
	StartedAt    string             `json:"started_at"`
	FinishedAt   string             `json:"finished_at,omitempty"`
}

func ToRunResponse(run entities.AgentRun) AgentRunResponse {
	var calls []toolCallResponse
	if len(run.ToolCallsJSON) > 0 {
		// Best-effort: a malformed/legacy ToolCallsJSON blob must never fail
		// the whole response - the rest of the run (status, summary, links)
		// is still useful to the operator even if the tool-call detail
		// can't be decoded.
		_ = json.Unmarshal(run.ToolCallsJSON, &calls)
	}
	if calls == nil {
		calls = []toolCallResponse{}
	}

	resp := AgentRunResponse{
		ID:           run.ID,
		Provider:     run.Provider,
		Model:        run.Model,
		Trigger:      run.Trigger,
		Status:       string(run.Status),
		ErrorMessage: run.ErrorMessage,
		InputSummary: run.InputSummary,
		ResponseText: run.ResponseText,
		ToolCalls:    calls,
		StrategyID:   run.StrategyID,
	}
	if !run.StartedAt.IsZero() {
		resp.StartedAt = run.StartedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if !run.FinishedAt.IsZero() {
		resp.FinishedAt = run.FinishedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return resp
}

func ToRunListResponse(runs []entities.AgentRun) []AgentRunResponse {
	out := make([]AgentRunResponse, len(runs))
	for i, r := range runs {
		out[i] = ToRunResponse(r)
	}
	return out
}
