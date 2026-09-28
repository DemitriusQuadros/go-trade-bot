package agent

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
