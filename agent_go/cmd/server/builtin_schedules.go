package server

import (
	"os"
	"path/filepath"
)

// Built-in schedules are defined in Go and run for every user unless the user
// has overridden them by adding an entry with the same ID (typically
// enabled:false) to their _users/<id>/multiagent-schedules.json.
//
// Each built-in may also register a pre-fire check in PreFireChecks. The
// scheduler calls the check before starting a session; if the check returns
// false the cron tick is skipped entirely — no LLM session is spawned.

const builtinAutoEnrichMemoryID = "builtin-auto-enrich-memory"

const builtinAutoEnrichQuery = `Check for chats needing memory enrichment, then act:

1. Count sessions that have no .enriched marker, or whose conversation.json is newer than the marker. Run (via execute_shell_command):

    c=0
    for sid in $(ls _users/default/chat_history/ 2>/dev/null); do
      conv=_users/default/chat_history/$sid/conversation.json
      mark=_users/default/chat_history/$sid/.enriched
      [ -f "$conv" ] || continue
      if [ ! -f "$mark" ] || [ "$conv" -nt "$mark" ]; then c=$((c+1)); fi
    done
    echo $c

2. If the count is 0, reply "No new chats to enrich — skipping." and STOP. Do not call any other tools.
3. If the count is > 0, call enrich_memory() with no arguments.`

// DefaultBuiltinSchedules returns the list of product-provided schedules that
// run for every user unless overridden.
func DefaultBuiltinSchedules() []WorkflowSchedule {
	return []WorkflowSchedule{
		{
			ID:              builtinAutoEnrichMemoryID,
			Name:            "Auto-enrich memory (every 3h)",
			Description:     "Distill new chat sessions into memory on a schedule. Uses a Go-side pre-check so no LLM runs when there is nothing to enrich.",
			CronExpression:  "0 */3 * * *",
			Timezone:        "UTC",
			Enabled:         true,
			Mode:            "multi-agent",
			Query:           builtinAutoEnrichQuery,
			ScheduleVersion: CurrentMultiAgentScheduleVersion,
			PromptVersion:   CurrentMultiAgentSchedulePromptVersion,
		},
	}
}

// MergeBuiltinSchedules appends built-in schedules that the user has not
// overridden. Matching is by ID — a user entry with the same ID always wins,
// so the user can disable a built-in (enabled:false) or tweak cron/timezone
// by adding a matching entry to their multiagent-schedules.json.
func MergeBuiltinSchedules(userSchedules []WorkflowSchedule) []WorkflowSchedule {
	existing := make(map[string]struct{}, len(userSchedules))
	for _, s := range userSchedules {
		existing[s.ID] = struct{}{}
	}
	out := make([]WorkflowSchedule, 0, len(userSchedules)+len(DefaultBuiltinSchedules()))
	out = append(out, userSchedules...)
	for _, b := range DefaultBuiltinSchedules() {
		if _, overridden := existing[b.ID]; !overridden {
			out = append(out, b)
		}
	}
	return out
}

// PreFireCheck is a gating function called by the scheduler before a built-in
// schedule fires. Returning false skips this tick entirely — no LLM session.
type PreFireCheck func(userID string) bool

// PreFireChecks maps built-in schedule IDs to their gating functions.
var PreFireChecks = map[string]PreFireCheck{
	builtinAutoEnrichMemoryID: hasChatsNeedingEnrichment,
}

// chatHistoryDirForUser returns the filesystem path to a user's chat_history
// folder. The server runs with workspace-docs/ as a sibling, so relative paths
// resolve correctly.
func chatHistoryDirForUser(userID string) string {
	return filepath.Join("workspace-docs", "_users", userID, "chat_history")
}

// hasChatsNeedingEnrichment returns true if at least one session folder has
// a conversation.json that is newer than its .enriched marker, or no marker
// at all. Returns false when every session has already been enriched and has
// not grown since.
func hasChatsNeedingEnrichment(userID string) bool {
	dir := chatHistoryDirForUser(userID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		conv := filepath.Join(dir, e.Name(), "conversation.json")
		convInfo, err := os.Stat(conv)
		if err != nil {
			continue
		}
		mark := filepath.Join(dir, e.Name(), ".enriched")
		markInfo, err := os.Stat(mark)
		if err != nil {
			return true
		}
		if convInfo.ModTime().After(markInfo.ModTime()) {
			return true
		}
	}
	return false
}
