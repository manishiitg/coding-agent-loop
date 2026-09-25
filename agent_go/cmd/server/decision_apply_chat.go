package server

import (
	"fmt"
	"strings"
)

// When the user answers a decision in Needs you, the UI sends the message
// returned here to the workflow's Builder chat, so the answer is applied right
// away where the user can watch, instead of waiting for the next run's
// pre-run drain. Typed decisions reuse the scheduler's own apply instructions,
// so both paths behave the same; the drain remains the fallback when the chat
// message is not sent.
func decisionApplyChatMessage(input ReportHumanInput) string {
	if !strings.EqualFold(strings.TrimSpace(input.Status), "answered") {
		return ""
	}
	id := strings.TrimSpace(input.ID)
	answer := strings.TrimSpace(input.SelectedOptionID)
	if answer == "" && strings.TrimSpace(input.Note) != "" {
		answer = "a written answer"
	}
	if id == "" || answer == "" {
		return ""
	}
	intro := fmt.Sprintf("I just answered the decision %q in %s (answer: %s). Apply it now, here in this chat.", id, input.WorkspacePath, answer)
	switch scheduledDecisionApplyMode(input) {
	case "external_wait":
		return ""
	case "no_change", "direct_apply", "targeted_fixer":
		var parts []string
		for _, turn := range scheduledDecisionPreflightTurns([]ReportHumanInput{input}) {
			parts = append(parts, turn.query)
		}
		if len(parts) == 0 {
			return ""
		}
		return intro + "\n\n" + strings.Join(parts, "\n\n")
	default:
		// Older decisions carry no apply type, so the unattended drain never
		// applies them. Here the user answered in person and is watching.
		return intro + fmt.Sprintf(`

Read it with get_human_input_request(workspace_path=%q, input_id=%q). Its context says what happens for each answer. Honor the answer exactly, including a rejection or a deferral.
- Approved: make the change with the normal typed Builder tools, re-read the changed artifact to confirm it landed, then call mark_human_input_consumed with an outcome_summary that says in plain words what changed.
- Rejected: call mark_human_input_consumed with an outcome_summary that the user declined and nothing changed.
- Deferred, or anything you cannot apply safely right now: change nothing, leave it answered, and tell me in one or two sentences why.
Do not run the workflow, back up, publish or notify. Keep your reply short and plain.`, input.WorkspacePath, id)
	}
}
