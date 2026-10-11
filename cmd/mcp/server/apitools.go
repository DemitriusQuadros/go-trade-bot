package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go-trade-bot/internal/configuration"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// REST-backed MCP tools. Each tool forwards to ONE cmd/api route over
// loopback with the service bearer (API_TOKEN), so request validation, DTOs,
// secret masking and error bodies are exactly what the web UI gets - nothing
// is re-implemented here.
//
// The table below IS the security boundary. The bearer is a synthetic admin,
// so the only thing standing between an MCP client and a route is whether
// that route appears here. Deliberately NOT listed (Tier 3, REST-only):
// PUT /settings, /account writes, /signal/close, strategy status/mode and any
// /strategy write, the agents kill switch, proposal approve/reject,
// /deploy-gate writes, /users, /webhook-targets writes, /auth. Strategy
// creation/edits go through the gated agent tools (save_strategy_script,
// deploy_to_testing), never through raw REST, which would not clamp mode to
// backtest/dryrun. apitools_test.go pins the exact set of non-GET routes.

// apiTool describes one forwarded route.
type apiTool struct {
	Name        string
	Description string
	Method      string
	Path        string   // e.g. "/candle-datasets/{id}/pause"
	Params      []string // the {names} in Path, all numeric ids
}

// maxAPIResponseBytes caps what one tool call returns to the model.
const maxAPIResponseBytes = 256 * 1024

var idPattern = regexp.MustCompile(`^[0-9]+$`)

func get(name, path, desc string, params ...string) apiTool {
	return apiTool{Name: name, Description: desc, Method: http.MethodGet, Path: path, Params: params}
}

func post(name, path, desc string, params ...string) apiTool {
	return apiTool{Name: name, Description: desc, Method: http.MethodPost, Path: path, Params: params}
}

const (
	queryNote = " Optional `query` holds the route's query-string filters."
	bodyNote  = " `body` is the JSON request body of the REST route."
)

// apiTools is the allowlist. Tier 1 = reads, Tier 2 = low-risk writes.
var apiTools = []apiTool{
	// --- Tier 1: reads -------------------------------------------------
	get("get_settings", "/settings", "Current settings (execution mode, testnet, dry-run fees/slippage, URLs, agents kill-switch state, backtest timeout). Secrets are masked."),
	get("get_account", "/account", "Account balances and order slots, including the dry-run and live accounts."+queryNote),
	get("list_signals", "/signal", "List trade signals (positions) with their orders. Filter with query `status` (open|closed)."+queryNote),
	get("get_signal", "/signal/{id}", "One signal with its orders, by id.", "id"),
	get("get_strategy_detail", "/strategy/{id}", "One strategy's full REST detail, including its `configuration` JSON (stop_loss_pct, position_sizing, max_daily_loss_usd, custom keys) which get_strategy omits.", "id"),
	get("get_strategy_performance", "/strategy/performance", "Per-strategy performance summary."+queryNote),
	get("get_strategy_performance_history", "/strategy/{id}/performance/history", "Daily P&L snapshots for one strategy."+queryNote, "id"),
	get("list_strategy_versions", "/strategy/{id}/versions", "Saved script versions of a strategy.", "id"),
	get("get_backtest_report", "/backtest/{id}/report", "The HTML/report payload of a finished backtest run.", "id"),
	get("get_monte_carlo", "/backtest/{id}/montecarlo", "Stored Monte Carlo robustness result for a backtest run.", "id"),
	get("get_prices", "/broker/prices", "Live ticker prices. Query `symbol` optional."+queryNote),
	get("get_klines", "/broker/klines", "Live exchange klines. Query `symbol`, `interval`, `limit`."+queryNote),
	get("list_agents", "/agents", "All agent personas with triggers, permissions, budget and bindings."),
	get("get_agent", "/agents/{id}", "One agent persona by id.", "id"),
	get("list_runs_of_agent", "/agents/{id}/runs", "Recent runs of one agent."+queryNote, "id"),
	get("get_agent_usage", "/agents/{id}/usage", "Daily token/cost usage of one agent.", "id"),
	get("get_kill_switch", "/agents/kill-switch", "Whether the global agents kill switch is on (read only)."),
	get("get_market_symbols", "/agents/market-symbols", "Symbols the agents' market watcher is subscribed to, with last price."),
	get("list_agent_runs", "/agent/runs", "Chat/agent run transcripts."+queryNote),
	get("get_agent_run", "/agent/runs/{id}", "One agent run with its tool calls.", "id"),
	get("list_strategy_memory", "/strategies/{id}/memory", "Shared memory entries (journal/findings/report refs) of a strategy.", "id"),
	get("get_agent_report", "/agent-reports/{id}", "One agent report's typed blocks.", "id"),
	get("list_candle_datasets", "/candle-datasets", "Candle datasets (symbol/timeframe/coverage/sync state)."),
	get("get_candle_dataset", "/candle-datasets/{id}", "One candle dataset with coverage and chunk counts.", "id"),
	get("list_candle_chunks", "/candle-datasets/{id}/chunks", "Backfill chunks of a dataset. Query `status`, `limit`."+queryNote, "id"),
	get("get_proposal", "/proposals/{id}", "One strategy change proposal with its gate evidence.", "id"),
	get("get_pending_proposal_count", "/proposals/pending-count", "Number of proposals awaiting an operator decision."),
	get("list_webhook_targets", "/webhook-targets", "Webhook targets (URLs and secrets masked)."),

	// --- Tier 2: low-risk writes ---------------------------------------
	post("run_monte_carlo", "/backtest/{id}/montecarlo", "Run a Monte Carlo trade-reordering analysis on a finished backtest run."+bodyNote, "id"),
	post("script_repl", "/script/repl", "Evaluate a Lua snippet against a candle window (no orders, no persistence)."+bodyNote),
	post("script_fast_rerun", "/script/fast-rerun", "Replay a Lua script over a bounded candle window and return the per-candle trace."+bodyNote),
	post("create_candle_dataset", "/candle-datasets", "Declare a candle dataset to keep (symbol, timeframe, optional start, keep_live); the reconciler backfills it."+bodyNote),
	post("pause_candle_dataset", "/candle-datasets/{id}/pause", "Pause a candle dataset's syncing.", "id"),
	post("resume_candle_dataset", "/candle-datasets/{id}/resume", "Resume a paused candle dataset.", "id"),
	post("retry_failed_candle_chunks", "/candle-datasets/{id}/retry-failed", "Re-queue a dataset's dead (permanently failed) chunks.", "id"),
	post("reconcile_candle_dataset", "/candle-datasets/{id}/reconcile", "Trigger an immediate reconcile pass for a dataset.", "id"),
	post("pause_agent", "/agents/{id}/pause", "Pause or resume one agent persona."+bodyNote+" Example: {\"paused\": true}.", "id"),
	post("run_agent", "/agents/{id}/run", "Enqueue a manual run of one agent (spends its model budget, under its normal guards)."+bodyNote, "id"),
}

// inputSchema builds the JSON schema for t.
func (t apiTool) inputSchema() json.RawMessage {
	props := map[string]any{}
	required := []string{}
	for _, p := range t.Params {
		props[p] = map[string]any{"type": "integer", "description": "numeric id"}
		required = append(required, p)
	}
	if t.Method == http.MethodGet {
		props["query"] = map[string]any{
			"type":                 "object",
			"description":          "optional query-string filters",
			"additionalProperties": map[string]any{"type": []string{"string", "number", "boolean"}},
		}
	} else {
		props["body"] = map[string]any{"type": "object", "description": "JSON request body"}
	}
	schema := map[string]any{"type": "object", "properties": props, "required": required}
	b, _ := json.Marshal(schema)
	return b
}

// apiForwarder calls cmd/api for the tools above.
type apiForwarder struct {
	baseURL string
	token   string
	client  *http.Client
}

func newAPIForwarder(cfg *configuration.Configuration) *apiForwarder {
	return &apiForwarder{
		baseURL: strings.TrimRight(cfg.MCPAPIURL, "/"),
		token:   cfg.APIToken,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// call executes t with the model-supplied args. A non-2xx response comes
// back as an error so the MCP result is flagged IsError with the API's own
// message.
func (f *apiForwarder) call(ctx context.Context, t apiTool, rawArgs json.RawMessage) (string, error) {
	args := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(rawArgs)) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("%s: arguments must be a JSON object: %w", t.Name, err)
		}
	}

	path := t.Path
	for _, p := range t.Params {
		raw, ok := args[p]
		if !ok {
			return "", fmt.Errorf("%s: missing required argument %q", t.Name, p)
		}
		val := strings.Trim(strings.TrimSpace(string(raw)), `"`)
		if !idPattern.MatchString(val) {
			return "", fmt.Errorf("%s: %q must be a numeric id, got %s", t.Name, p, string(raw))
		}
		path = strings.ReplaceAll(path, "{"+p+"}", val)
	}

	target := f.baseURL + "/api" + path
	var body io.Reader
	if t.Method == http.MethodGet {
		if raw, ok := args["query"]; ok && string(raw) != "null" {
			q, err := queryFromJSON(raw)
			if err != nil {
				return "", fmt.Errorf("%s: %w", t.Name, err)
			}
			if enc := q.Encode(); enc != "" {
				target += "?" + enc
			}
		}
	} else {
		raw := []byte(`{}`)
		if b, ok := args["body"]; ok && string(b) != "null" {
			raw = b
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, t.Method, target, body)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.Name, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "gtb")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: could not reach the API at %s (is cmd/api running? set MCP.API_URL): %w", t.Name, f.baseURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("%s: reading response: %w", t.Name, err)
	}
	text := string(data)
	if len(data) > maxAPIResponseBytes {
		text = string(data[:maxAPIResponseBytes]) + "\n[truncated: response exceeded 256 KiB; narrow it with query filters]"
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("%s: HTTP %d: %s", t.Name, resp.StatusCode, strings.TrimSpace(text))
	}
	return text, nil
}

// queryFromJSON turns {"a":"x","n":3,"b":true} into url.Values.
func queryFromJSON(raw json.RawMessage) (url.Values, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("query must be an object of scalar values: %w", err)
	}
	q := url.Values{}
	for k, v := range m {
		switch x := v.(type) {
		case string:
			q.Set(k, x)
		case float64:
			q.Set(k, strconv.FormatFloat(x, 'f', -1, 64))
		case bool:
			q.Set(k, strconv.FormatBool(x))
		case nil:
		default:
			return nil, fmt.Errorf("query value for %q must be a string, number or boolean", k)
		}
	}
	return q, nil
}

// registerAPITools adds the REST-backed tools. A name already taken by an
// agent tool is skipped (and logged) rather than silently replacing it.
func registerAPITools(s *mcp.Server, cfg *configuration.Configuration, taken map[string]bool) {
	f := newAPIForwarder(cfg)
	for _, tool := range apiTools {
		tool := tool
		if taken[tool.Name] {
			log.Printf("mcp: REST tool %q skipped: name already used by an agent tool", tool.Name)
			continue
		}
		s.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.inputSchema(),
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			out, err := f.call(ctx, tool, req.Params.Arguments)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil
		})
	}
}
