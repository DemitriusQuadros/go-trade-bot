package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EvaluationPrompt is the fixed input for a cron/manual run that carries no
// operator prompt (A-02 §2).
const EvaluationPrompt = "Evaluate each of your bound strategies: review recent performance, open positions and the shared memory. " +
	"Record findings with write_journal. If anything is noteworthy, write a report with write_report and, if you have " +
	"the notify permission and it is important, notify the operator. Be concise."

// BuildScheduledInput composes the user input for a cron/manual run: the
// evaluation prompt, plus the operator's instruction when one was given.
func BuildScheduledInput(operatorPrompt string) string {
	if operatorPrompt == "" {
		return EvaluationPrompt
	}
	return EvaluationPrompt + "\n\nOperator request: " + operatorPrompt
}

// TriggerInput is what BuildTriggerInput needs to phrase a triggered run's
// instruction (C-01 §5).
type TriggerInput struct {
	Trigger       string          // "cron" | "manual" | "event" | "market" | "chain"
	Detail        json.RawMessage // the run's trigger detail
	Prompt        string          // manual: operator request; chain via trigger_agent: the caller's message
	SourceAgent   string          // chain: the source agent's name ("" if unknown)
	BoundStrategy bool            // market: at least one bound strategy trades the symbol
}

const eventInstruction = "Investigate the cause using the tools and the shared memory, record a finding with write_journal, " +
	"and write a report (write_report) and notify only if action is warranted. Be concise."

// BuildTriggerInput composes the user input for an unattended run from its
// trigger kind and detail. cron/manual keep BuildScheduledInput.
func BuildTriggerInput(in TriggerInput) string {
	var d map[string]any
	_ = json.Unmarshal(in.Detail, &d)
	num := func(k string) float64 {
		f, _ := d[k].(float64)
		return f
	}
	str := func(k string) string {
		s, _ := d[k].(string)
		return s
	}
	switch in.Trigger {
	case "event":
		msg := fmt.Sprintf("Strategy #%d emitted %s", uint(num("strategy_id")), str("event"))
		if sym := str("symbol"); sym != "" {
			msg += " on " + sym
		}
		if at := str("occurred_at"); at != "" {
			msg += " at " + at
		}
		if data, ok := d["data"]; ok && data != nil {
			if b, err := json.Marshal(data); err == nil && string(b) != "{}" {
				msg += ". Event data: " + string(b)
			}
		}
		return msg + ".\n" + eventInstruction
	case "market":
		sym, window := str("symbol"), int(num("window_minutes"))
		var msg string
		if str("kind") == "volatility_spike" {
			msg = fmt.Sprintf("%s volatility spiked: the ATR over the last %dm is %.2fx the prior-24h baseline (threshold %.2fx); last price %v at %s.",
				sym, window, num("observed_multiplier"), num("threshold"), d["price"], str("at"))
		} else {
			msg = fmt.Sprintf("%s moved %+.2f%% in %dm (threshold %.2f%%); last price %v at %s.",
				sym, num("observed_pct"), window, num("threshold"), d["price"], str("at"))
		}
		if !in.BoundStrategy {
			msg += " None of your bound strategies trade this symbol: treat it as a macro signal and assess its impact on the strategies you are responsible for."
		}
		return msg + "\nAssess what this means for your bound strategies (open positions, recent behaviour, shared memory). " + eventInstruction
	case "chain":
		source := in.SourceAgent
		if source == "" {
			source = fmt.Sprintf("#%d", uint(num("source_agent_id")))
		}
		var reports []string
		if ids, ok := d["report_ids"].([]any); ok {
			for _, id := range ids {
				if f, ok := id.(float64); ok {
					reports = append(reports, fmt.Sprintf("%d", uint(f)))
				}
			}
		}
		msg := fmt.Sprintf("Agent %s finished run #%d and produced reports [%s] - read them with list_reports and continue from its findings.",
			source, uint(num("source_run_id")), strings.Join(reports, ", "))
		if in.Prompt != "" {
			msg += "\nMessage from " + source + ": " + in.Prompt
		}
		return msg + "\nRecord what you conclude with write_journal; write a report and notify only if action is warranted."
	default:
		return BuildScheduledInput(in.Prompt)
	}
}
