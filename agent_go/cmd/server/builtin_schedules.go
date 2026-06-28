package server

import (
	"os"
	"path/filepath"
	"strings"
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

const builtinOrgPulseID = "builtin-org-pulse"

const builtinOrgPulseQuery = `You are running the daily Org Pulse — the Chief of Staff's heartbeat over the whole org.

First, check whether anything has changed since your last Org Pulse (any workflow runs, new chats, or new outputs). If nothing has changed, write nothing and stop.

Otherwise, call get_reference_doc(kind="org-pulse") and follow it exactly. Start by backing up org-level artifacts per get_reference_doc(kind="backup-strategy") using pulse/backup.json and pulse/backup/status.json, same as workflow backup. Before writing or changing pulse/org-pulse.html, also call get_reference_doc(kind="org-html") and use its Org Pulse skeleton. Then read pulse/goals.html when it exists, review the org (each workflow's builder/improve.html verdict pills + headline, reports, knowledgebase, global learnings, plus recent conversations), measure workflows against the org goals, judge the org's endgame, generate grounded proposal-only recommendations for each goal (per-automation recs to that automation's builder/improve.html; org-level recs to the Recommendations section of pulse/goals.html), harvest what's worth keeping into your memory (curate and merge in your own words — never copy/import files), propose any promotions (a repeated ad-hoc task -> turn it into a workflow), and record everything — goal scorecard, org health, recommendations, what you harvested, and suggestion cards — in the single pulse/org-pulse.html log. If org publish is configured in pulse/publish.json and already verified in pulse/publish/status.json, re-publish pulse/goals.html + pulse/org-pulse.html per get_reference_doc(kind="publish-strategy") and update pulse/publish/status.json. Notify the user only on a decision-worthy change; stay silent on a steady day.`

// builtinOrgPulseMessages runs the daily Org Pulse as a message SEQUENCE — one
// focused turn per step in a single resumed session, the way workflow Pulse
// (runPostRunMonitor) does — instead of one giant single-turn prompt. Each step
// does ONE job and defers the detail to get_reference_doc(kind="org-pulse").
// builtinOrgPulseQuery above is kept as the single-turn Query fallback so nothing
// that references it breaks.
var builtinOrgPulseMessages = []string{
	`STEP 1/5 — FRESHNESS + BACKUP. You are running the daily Org Pulse, the Chief of Staff's heartbeat over the whole org. Call get_reference_doc(kind="org-pulse") and follow it; we go one step at a time — do ONLY this step, then stop. First check whether anything changed since your last Org Pulse: any workflow runs, new chats, new outputs, or edits to pulse/goals.html. If NOTHING changed, say "Org idle since last pulse — nothing to do" and STOP the whole pass; do not run the later steps. Otherwise back up org-level artifacts per get_reference_doc(kind="backup-strategy") using pulse/backup.json and pulse/backup/status.json (same config/status split as workflow backup; set up the zero-config local-git default if backup is unconfigured; skip the push when the source hash is already backed up). Report what changed and the backup result, then stop.`,
	`STEP 2/5 — EVIDENCE + GOALS. In one efficient batched sweep, read pulse/goals.html (the goal scorecard) plus each workflow's builder/improve.html verdict pills + headline, latest reports, knowledgebase, global learnings, recent conversations, and your own memory — follow get_reference_doc(kind="org-pulse") step 3 (curate, don't import; trust the per-workflow verdicts; drill into raw runs only on a surprise). Then measure goals: for each goal compare current vs baseline/target, assign on-track / at-risk / off-track / unknown with a one-sentence reason, and diagnose the gap blocking each goal. Evaluate workflow alignment only when goals exist. Report the goal scorecard with each goal's gap, then stop.`,
	`STEP 3/5 — GENERATE RECOMMENDATIONS. This is the proactive step: for each goal, propose grounded, prioritized recommendations to MOVE it — not just diagnose it. Follow get_reference_doc(kind="org-pulse") "Generate recommendations". Every rec is tied to a goal + evidence and ranked by impact/effort, and is PROPOSAL-ONLY — you recommend, the user/builder decides; never auto-apply a plan change. Think beyond the obvious: a new automation for an unserved goal, a different approach for a capped goal, cross-automation synergies, and promotions. Write per-automation improvement recs into that automation's builder/improve.html (a newest-first Chief of Staff recommendation card). Write org-level recs to the Recommendations section of pulse/goals.html per get_reference_doc(kind="org-html"). Report the recommendations you wrote and where, then stop.`,
	`STEP 4/5 — HARVEST + PROMOTIONS. Harvest what's worth keeping into your shared memory — curate and merge in your own words into the right entity/topic note, never copy or import files; synthesize cross-workflow insights; if a day produced nothing worth keeping, write nothing. Then spot promotions: review recent conversations for a repeated ad-hoc task shape and PROPOSE turning it into a workflow (name it, generalize the procedure, cite instances) — propose only, do not create it. Follow get_reference_doc(kind="org-pulse") steps 5-6. Report a one-sentence note of what you harvested and any promotion proposal, then stop.`,
	`STEP 5/5 — LOG + PUBLISH. Before writing, call get_reference_doc(kind="org-html") and use its Org Pulse skeleton. Prepend one dated entry to pulse/org-pulse.html (newest-first) covering the goal scorecard, workflow alignment delta, org-health one-liner, recommendations summary, what you harvested, and suggestion cards. If org publish is configured in pulse/publish.json and already verified in pulse/publish/status.json, re-publish pulse/goals.html + pulse/org-pulse.html per get_reference_doc(kind="publish-strategy") and update pulse/publish/status.json (never do the first/verifying publish unattended). Notify the user only on a decision-worthy change; stay silent on a steady day. Report the log entry and publish result, then stop.`,
}

// DefaultBuiltinSchedules returns the list of product-provided schedules that
// run for every user unless overridden.
func DefaultBuiltinSchedules() []WorkflowSchedule {
	return []WorkflowSchedule{
		{
			ID:             builtinAutoEnrichMemoryID,
			Name:           "Auto-enrich memory (every 3h)",
			Description:    "Distill new chat sessions into memory on a schedule. Uses a Go-side pre-check so no LLM runs when there is nothing to enrich.",
			CronExpression: "0 */3 * * *",
			Timezone:       "UTC",
			Enabled:        true,
			Mode:           "multi-agent",
			Query:          builtinAutoEnrichQuery,
		},
		{
			// Opt-in (Enabled:false): the user turns Org Pulse on via the chat
			// toggle / Scheduled Tasks popup, which writes a same-ID override with
			// enabled:true and their chosen cron. Default cadence is once a day; the
			// pass self-gates agentically (wakes, cheap "anything new?" check, exits
			// if not) rather than via a Go pre-fire check.
			ID:             builtinOrgPulseID,
			Name:           "Org Pulse (daily)",
			Description:    "The Chief of Staff's daily heartbeat: review every workflow's outcome, harvest reports/learnings into memory, and surface suggestions. Off by default; turn it on to opt in. Default once a day, editable.",
			CronExpression: "0 8 * * *",
			Timezone:       "UTC",
			Enabled:        false,
			Mode:           "multi-agent",
			// Org Pulse runs as a message SEQUENCE (one focused turn per step in one
			// resumed session, like workflow Pulse). Query is kept as the single-turn
			// fallback for anything that still reads it.
			Messages: builtinOrgPulseMessages,
			Query:    builtinOrgPulseQuery,
		},
	}
}

func FindDefaultBuiltinSchedule(scheduleID string) (WorkflowSchedule, bool) {
	for _, sched := range DefaultBuiltinSchedules() {
		if sched.ID == scheduleID {
			return sched, true
		}
	}
	return WorkflowSchedule{}, false
}

func IsDefaultBuiltinSchedule(scheduleID string) bool {
	_, ok := FindDefaultBuiltinSchedule(scheduleID)
	return ok
}

func IsSlashManagedBuiltinSchedule(scheduleID string) bool {
	return scheduleID == builtinOrgPulseID
}

// IsOrgPulseSchedule reports whether a multi-agent schedule IS the Org Pulse —
// either the canonical built-in (builtin-org-pulse) or a user-created duplicate.
// /pulse-setup is supposed to enable/override builtin-org-pulse, but an agent can
// instead materialize Org Pulse under a fresh id via create_multiagent_schedule
// (the on-disk shape we actually observe). Such a duplicate is still what the
// scheduler runs, so the effective "Org Pulse is on" state must account for it.
// We recognize duplicates by their highly specific Org Pulse signature.
func IsOrgPulseSchedule(sched WorkflowSchedule) bool {
	if sched.ID == builtinOrgPulseID {
		return true
	}
	hay := strings.ToLower(sched.Name + "\n" + sched.Description + "\n" + sched.Query)
	return strings.Contains(hay, "org pulse") || strings.Contains(hay, "org-pulse")
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
