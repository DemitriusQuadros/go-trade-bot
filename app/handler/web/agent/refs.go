package agent

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Ref kinds (Phase D-01 §3). The frontend renders one card per ref.
const (
	RefKindStrategy     = "strategy"
	RefKindBacktest     = "backtest"
	RefKindReport       = "report"
	RefKindProposal     = "proposal"
	RefKindAgent        = "agent"
	RefKindMemory       = "memory"
	RefKindOptimization = "optimization"
)

// maxToolRefs bounds the refs of one tool call (list_backtests /
// list_strategies can print many ids).
const maxToolRefs = 20

// ToolRef is one entity a tool call produced or referenced. Role is only set
// where one call yields two refs of the same kind (challenger/champion,
// baseline/candidate) or to mark the strategy create_strategy created.
type ToolRef struct {
	Kind string `json:"kind"`
	ID   uint   `json:"id"`
	Role string `json:"role,omitempty"`
}

// jsonKeyRef maps a top-level key of a JSON-object tool result to a ref.
type jsonKeyRef struct {
	key, kind, role string
}

// objectKeyRefs is evaluated in this order, so the refs order is stable.
var objectKeyRefs = []jsonKeyRef{
	{"strategy_id", RefKindStrategy, ""},
	{"challenger_strategy_id", RefKindStrategy, "challenger"},
	{"champion_strategy_id", RefKindStrategy, "champion"},
	{"backtest_id", RefKindBacktest, ""},
	{"baseline_run_id", RefKindBacktest, "baseline"},
	{"candidate_run_id", RefKindBacktest, "candidate"},
	{"report_id", RefKindReport, ""},
	{"proposal_id", RefKindProposal, ""},
	{"agent_id", RefKindAgent, ""},
	{"memory_id", RefKindMemory, ""},
	{"optimization_run_id", RefKindOptimization, ""},
}

// gateKeyRefs are read from the object nested under "gate"
// (deploy_to_testing's GateResult).
var gateKeyRefs = []jsonKeyRef{
	{"baseline_run_id", RefKindBacktest, "baseline"},
	{"candidate_run_id", RefKindBacktest, "candidate"},
}

// optimizationTools may use the generic optimization_id / run_id keys.
var optimizationTools = map[string]bool{
	"list_optimizations":       true,
	"run_optimization":         true,
	"get_optimization_results": true,
}

// arrayIDKinds maps tools whose result is a JSON array of objects to the
// kind of each element's "id".
var arrayIDKinds = map[string]string{
	"list_proposals": RefKindProposal,
	"list_reports":   RefKindReport,
	"read_memory":    RefKindMemory,
}

// textRefPattern matches the machine-parseable `key=N` prefixes of the text
// tool results (tools.go: summarizeStrategyForModel,
// summarizeBacktestForModel, list_* lines, optimization summaries).
var textRefPattern = regexp.MustCompile(`(?:^|\s)(strategy_id|backtest_id|optimization_run_id)=(\d+)`)

var textKeyKinds = map[string]string{
	"strategy_id":         RefKindStrategy,
	"backtest_id":         RefKindBacktest,
	"optimization_run_id": RefKindOptimization,
}

// scriptSourceMarker starts the free-form Lua source in
// summarizeStrategyForModel's output; ids are never read past it.
const scriptSourceMarker = "\nscript_source:"

// ExtractToolRefs derives the structured refs of one persisted tool call from
// its result, at response time (nothing is persisted). It is pure and never
// returns nil: an errored call, an unparseable result or a result without ids
// yields [].
func ExtractToolRefs(tool, result string, isError bool) []ToolRef {
	c := refCollector{refs: []ToolRef{}, seen: map[ToolRef]bool{}}
	if isError {
		return c.refs
	}
	trimmed := strings.TrimSpace(result)
	switch {
	case strings.HasPrefix(trimmed, "{"):
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(trimmed), &obj) == nil {
			c.fromObject(tool, obj)
			return c.refs
		}
	case strings.HasPrefix(trimmed, "["):
		var arr []map[string]json.RawMessage
		if json.Unmarshal([]byte(trimmed), &arr) == nil {
			if kind, ok := arrayIDKinds[tool]; ok {
				for _, el := range arr {
					if id, ok := rawUint(el["id"]); ok {
						c.add(kind, id, "")
					}
				}
			}
			return c.refs
		}
	}
	c.fromText(tool, result)
	return c.refs
}

type refCollector struct {
	refs []ToolRef
	seen map[ToolRef]bool
}

func (c *refCollector) add(kind string, id uint, role string) {
	if id == 0 || len(c.refs) >= maxToolRefs {
		return
	}
	r := ToolRef{Kind: kind, ID: id, Role: role}
	if c.seen[r] {
		return
	}
	c.seen[r] = true
	c.refs = append(c.refs, r)
}

func (c *refCollector) fromObject(tool string, obj map[string]json.RawMessage) {
	for _, k := range objectKeyRefs {
		if id, ok := rawUint(obj[k.key]); ok {
			c.add(k.kind, id, k.role)
		}
	}
	if optimizationTools[tool] {
		for _, key := range []string{"optimization_id", "run_id"} {
			if id, ok := rawUint(obj[key]); ok {
				c.add(RefKindOptimization, id, "")
			}
		}
	}
	if gateRaw, ok := obj["gate"]; ok {
		var gate map[string]json.RawMessage
		if json.Unmarshal(gateRaw, &gate) == nil {
			for _, k := range gateKeyRefs {
				if id, ok := rawUint(gate[k.key]); ok {
					c.add(k.kind, id, k.role)
				}
			}
		}
	}
}

func (c *refCollector) fromText(tool, result string) {
	if i := strings.Index(result, scriptSourceMarker); i >= 0 {
		result = result[:i]
	}
	first := true
	for _, m := range textRefPattern.FindAllStringSubmatch(result, -1) {
		id, err := strconv.ParseUint(m[2], 10, 64)
		if err != nil {
			continue
		}
		kind := textKeyKinds[m[1]]
		role := ""
		// create_strategy's result is summarizeStrategyForModel of the new
		// strategy: its leading strategy_id is the one it created.
		if tool == "create_strategy" && kind == RefKindStrategy && first {
			role = "created"
			first = false
		}
		c.add(kind, uint(id), role)
	}
}

// rawUint decodes a positive JSON integer; anything else is not a ref.
func rawUint(raw json.RawMessage) (uint, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' {
		return 0, false
	}
	id, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}
