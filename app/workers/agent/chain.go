package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/hibiken/asynq"
)

// MaxChainDepth bounds chained agent runs (agents-platform C-01 §4): the
// first chained run has depth 1, and a run at depth 3 can trigger nothing.
const MaxChainDepth = 3

// ChainOnTool is the Detail "on" value of a run started by the
// trigger_agent tool (the declarative values are entities.ChainOn*).
const ChainOnTool = "trigger_agent"

// ChainRequest describes one chained run to start: the target agent and
// the source run that triggers it. Both chain mechanisms - declarative
// ChainFrom (the agent:run processor) and the imperative trigger_agent tool
// - go through ChainLauncher.Launch, so they share every guard.
type ChainRequest struct {
	TargetAgentID uint
	SourceAgentID uint
	SourceRunID   uint
	SourceDepth   int    // the source run's ChainDepth
	SourcePath    []uint // the source run's ChainPath (agents before the source)
	On            string // entities.ChainOn* or ChainOnTool
	ReportIDs     []uint // reports the source run wrote
	Message       string // trigger_agent only: becomes the chained run's prompt
}

// ChainSuppressedError is returned when a guard refuses a chained run
// (depth, repeated agent, or an already-enqueued duplicate). It never fails
// the source run.
type ChainSuppressedError struct {
	Reason string
}

func (e *ChainSuppressedError) Error() string { return "chain suppressed: " + e.Reason }

// IsChainSuppressed reports whether err is a guard refusal.
func IsChainSuppressed(err error) bool {
	var s *ChainSuppressedError
	return errors.As(err, &s)
}

// BuildChainPayload applies the runtime chain guards and returns the
// agent:run payload: ChainDepth = source + 1 (<= MaxChainDepth), ChainPath
// = source path + source agent, and the target must not already be in that
// path (which also covers target == source). The API-level cycle check is a
// first line of defence; these checks are the guarantee.
func BuildChainPayload(req ChainRequest) (RunPayload, error) {
	if req.TargetAgentID == 0 || req.SourceAgentID == 0 || req.SourceRunID == 0 {
		return RunPayload{}, fmt.Errorf("chain: target agent, source agent and source run are required")
	}
	depth := req.SourceDepth + 1
	if depth > MaxChainDepth {
		return RunPayload{}, &ChainSuppressedError{Reason: fmt.Sprintf("chain depth %d exceeds the maximum of %d", depth, MaxChainDepth)}
	}
	path := make([]uint, 0, len(req.SourcePath)+1)
	path = append(path, req.SourcePath...)
	if len(path) == 0 || path[len(path)-1] != req.SourceAgentID {
		path = append(path, req.SourceAgentID)
	}
	for _, id := range path {
		if id == req.TargetAgentID {
			return RunPayload{}, &ChainSuppressedError{Reason: fmt.Sprintf("agent %d is already in the chain %v", req.TargetAgentID, path)}
		}
	}
	reportIDs := req.ReportIDs
	if reportIDs == nil {
		reportIDs = []uint{}
	}
	detail := map[string]any{
		"source_agent_id": req.SourceAgentID,
		"source_run_id":   req.SourceRunID,
		"report_ids":      reportIDs,
		"on":              req.On,
		"chain_path":      path,
	}
	if req.Message != "" {
		detail["message"] = req.Message
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return RunPayload{}, err
	}
	parent := req.SourceRunID
	return RunPayload{
		AgentID:     req.TargetAgentID,
		Trigger:     "chain",
		Detail:      b,
		Prompt:      req.Message,
		ChainDepth:  depth,
		ChainPath:   path,
		ParentRunID: &parent,
	}, nil
}

// ChainLauncher enqueues chained runs behind the shared guards.
// OnFired / OnSuppressed (may be nil) feed agent_triggers_fired_total and
// agent_trigger_suppressed_total{kind="chain"}.
type ChainLauncher struct {
	enq          TaskEnqueuer
	OnFired      func()
	OnSuppressed func(reason string)
}

// NewChainLauncher builds a launcher over enq.
func NewChainLauncher(enq TaskEnqueuer) *ChainLauncher {
	return &ChainLauncher{enq: enq}
}

// LaunchChain enqueues the chained run and returns its task id. A guard
// refusal is a *ChainSuppressedError (logged and counted here); any other
// error is an enqueue failure.
func (l *ChainLauncher) LaunchChain(ctx context.Context, req ChainRequest) (string, error) {
	p, err := BuildChainPayload(req)
	if err != nil {
		if IsChainSuppressed(err) {
			l.suppressed(req, err.Error())
		}
		return "", err
	}
	id, err := EnqueueRunWith(ctx, l.enq, p)
	if errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask) {
		sErr := &ChainSuppressedError{Reason: fmt.Sprintf("agent %d was already triggered by run %d", req.TargetAgentID, req.SourceRunID)}
		l.suppressed(req, sErr.Error())
		return "", sErr
	}
	if err != nil {
		return "", fmt.Errorf("chain: enqueue agent %d: %w", req.TargetAgentID, err)
	}
	if l.OnFired != nil {
		l.OnFired()
	}
	return id, nil
}

func (l *ChainLauncher) suppressed(req ChainRequest, reason string) {
	log.Printf("agent chain: run %d (agent %d) -> agent %d: %s", req.SourceRunID, req.SourceAgentID, req.TargetAgentID, reason)
	if l.OnSuppressed != nil {
		l.OnSuppressed(reason)
	}
}
