package server

import (
	"os"
	"path/filepath"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/fsutil"
)

// Built-in schedules are defined in Go and run for every user unless the user
// has overridden them by adding an entry with the same ID (typically
// enabled:false) to their _users/<id>/multiagent-schedules.json.
//
// Each built-in may also register a pre-fire check in PreFireChecks. The
// scheduler calls the check before starting a session; if the check returns
// false the cron tick is skipped entirely — no LLM session is spawned.

const builtinAutoEnrichMemoryID = "builtin-auto-enrich-memory"

const builtinAutoEnrichQuery = `Run scheduled Chief of Staff memory enrichment.

This built-in schedule is pre-gated by the scheduler: it only fires when one or more non-scheduled chat_history sessions have changed since their last enrichment marker. Call get_reference_doc(kind="memory-usage") if you need the memory policy, then call enrich_memory(delete_older_than_days: 7). Do not write reports, change schedules, or make workflow edits from this scheduled pass.`

const builtinOrgPulseID = "builtin-org-pulse"

const builtinOrgPulseQuery = `You are running the daily Org Pulse — the Chief of Staff's heartbeat over the whole org.

First, check whether anything has changed since your last Org Pulse (any workflow runs, new chats, or new outputs). If nothing has changed, write nothing and stop.

Otherwise, call get_reference_doc(kind="org-pulse") and follow it exactly. Start by backing up org-level artifacts per get_reference_doc(kind="backup-strategy") using pulse/backup.json and pulse/backup/status.json, same as workflow backup. Before writing or changing pulse/goals.html or pulse/org-pulse.html, also call get_reference_doc(kind="org-html") and use its skeletons. Then read pulse/goals.html when it exists, review the org (each workflow's builder/improve.html verdict pills + headline, existing Chief of Staff recommendation cards and their workflow-side data-status replies, reports, knowledgebase, global learnings, plus recent conversations), measure workflows against the org goals, update pulse/goals.html as the durable current scorecard with any concrete status/evidence/freshness changes, judge the org's endgame, audit whether each workflow has a complete high/medium/low LLM tier setup and whether schedules/overrides use an appropriate tier, with cost posture as supporting evidence only (report only — do not change any model/config), follow up on existing recommendations before creating new ones, generate grounded proposal-only recommendations for each goal only when no equivalent open rec already exists (per-automation recs to that automation's builder/improve.html; org-level recs to the Recommendations section of pulse/goals.html), harvest what's worth keeping into your memory (curate and merge in your own words — never copy/import files), propose any promotions (a repeated ad-hoc task -> turn it into a workflow), and record everything — goal scorecard, org health, LLM/cost audit, recommendation lifecycle updates, what you harvested, and suggestion cards — in the single pulse/org-pulse.html log. If org publish is configured in pulse/publish.json and already verified in pulse/publish/status.json, re-publish pulse/goals.html + pulse/org-pulse.html per get_reference_doc(kind="publish-strategy") and update pulse/publish/status.json. Notify the user with a daily Org Pulse digest using the org-pulse notification format; when Gmail/email is available, email is the default in-depth rendering: set email_subject, email_html (EMAIL-SAFE: inline styles only — Gmail strips <style>/<head>/class CSS, so do NOT paste pulse/org-pulse.html; build an inline-styled daily digest with workflow health, goal scorecard, LLM/cost audit, recommendations, next decisions, and links to the full report), and plain email_body on the same notify_user call; set email_to only when the org notification preference asks to replace the default To recipient; set email_cc only when the org notification preference asks for CC recipients. Do not stay silent on a steady day when workflows, chats, outputs, or goals changed; send a calm all-healthy digest.`

// builtinOrgPulseMessages runs the daily Org Pulse as a message SEQUENCE — one
// focused turn per step in a single resumed session, the way workflow Pulse
// (runPostRunMonitor) does — instead of one giant single-turn prompt. Each step
// does ONE job and defers the detail to get_reference_doc(kind="org-pulse").
// builtinOrgPulseQuery above is kept as the single-turn Query fallback so nothing
// that references it breaks.
var builtinOrgPulseMessages = []string{
	`STEP 1/6 — FRESHNESS + BACKUP. You are running the daily Org Pulse, the Chief of Staff's heartbeat over the whole org. Call get_reference_doc(kind="org-pulse") and follow it; we go one step at a time — do ONLY this step, then stop. First check whether anything changed since your last Org Pulse: any workflow runs, new chats, new outputs, or edits to pulse/goals.html. If NOTHING changed, say "Org idle since last pulse — nothing to do" and STOP the whole pass; do not run the later steps. Otherwise back up org-level artifacts per get_reference_doc(kind="backup-strategy") using pulse/backup.json and pulse/backup/status.json (same config/status split as workflow backup; set up the zero-config local-git default if backup is unconfigured; skip the push when the source hash is already backed up). Report what changed and the backup result, then stop.`,
	`STEP 2/6 — EVIDENCE + GOALS. In one efficient batched sweep, read pulse/goals.html (the goal scorecard) plus each workflow's builder/improve.html verdict pills + headline, latest reports, knowledgebase, global learnings, recent conversations, and your own memory — follow get_reference_doc(kind="org-pulse") step 3 (curate, don't import; trust the per-workflow verdicts; drill into raw runs only on a surprise). Then measure goals: for each goal compare current vs baseline/target, assign on-track / at-risk / off-track / unknown with a one-sentence reason, and diagnose the gap blocking each goal. Before writing, call get_reference_doc(kind="org-html"), then update pulse/goals.html as the durable current scorecard when concrete evidence changes status, latest evidence, confidence, freshness/last-reviewed, or history. Evaluate workflow alignment only when goals exist. Report the goal scorecard, each goal's gap, and whether pulse/goals.html was updated, then stop.`,
	`STEP 3/6 — LLM + COST AUDIT (report-only). Main check: verify every workflow has a complete high/medium/low LLM tier setup, that each tier resolves to the expected provider/model, and that schedules or explicit overrides use the appropriate tier for the workflow's importance and current quality. In one batched sweep, inspect each workflow's workflow.json capabilities.llm_config / execution defaults / schedules, recent cost artifacts under costs/ and run folders, and any available report or Pulse evidence that names provider/model/tier. Treat llm_allocation_mode="coding_agent" or legacy "coding_plan" as complete via provider defaults even when tiered_config, pulse_llm, or auto_improve_llm are not persisted; resolve the effective provider defaults before classifying. For each workflow, classify tier setup as complete, missing-tier, override-mismatch, over-tiered, under-tiered, or unknown; use costs/tokens only as supporting evidence for whether the tier choice is wasteful or underpowered. Summarize which workflows use high/medium/low tiers or explicit models, where tier evidence is missing, where cost evidence is missing, and any unusual mismatch between task importance and model tier. This is reporting only: do NOT change workflow.json, prompts, plans, model settings, schedules, or secrets; do NOT run optimizers or fixes. Report the LLM/cost scorecard and the evidence paths you used, then stop.`,
	`STEP 4/6 — GENERATE RECOMMENDATIONS. This is the proactive step: for each goal, propose grounded, prioritized recommendations to MOVE it — not just diagnose it. Follow get_reference_doc(kind="org-pulse") "Generate recommendations". First read existing org-level recommendation cards in pulse/goals.html and workflow-level Chief of Staff cards in each builder/improve.html (` + "`.cos-rec`" + ` / data-cos-rec-id / data-status). For any existing open rec, report whether it is proposed, accepted, queued_auto_improve, in_progress, needs_evidence, blocked, done, or dismissed; update/follow up instead of duplicating. Every new rec is tied to a goal + evidence and ranked by impact/effort, and is PROPOSAL-ONLY — you recommend, the user/builder decides; never auto-apply a plan change. Think beyond the obvious: a new automation for an unserved goal, a different approach for a capped goal, cross-automation synergies, and promotions. Write per-automation improvement recs into that automation's builder/improve.html as structured Chief of Staff recommendation cards with stable data-cos-rec-id, data-status="proposed", data-priority, data-suggested-action, evidence, gap, and expected impact. Write org-level recs to the Recommendations section of pulse/goals.html per get_reference_doc(kind="org-html"). Report recommendations written, existing recommendations followed up, stale open decisions, and where each lives, then stop.`,
	`STEP 5/6 — HARVEST + PROMOTIONS. Harvest what's worth keeping into your shared memory — curate and merge in your own words into the right entity/topic note, never copy or import files; synthesize cross-workflow insights; if a day produced nothing worth keeping, write nothing. Then spot promotions: review recent conversations for a repeated ad-hoc task shape and PROPOSE turning it into a workflow (name it, generalize the procedure, cite instances) — propose only, do not create it. Follow get_reference_doc(kind="org-pulse") steps 7-8. Report a one-sentence note of what you harvested and any promotion proposal, then stop.`,
	`STEP 6/6 — LOG + PUBLISH + DAILY DIGEST. Before writing, call get_reference_doc(kind="org-html") and use its Org Pulse skeleton. Prepend one dated entry to pulse/org-pulse.html (newest-first) covering the goal scorecard, workflow alignment delta, org-health one-liner, LLM/model tier + cost audit, recommendations summary, what you harvested, and suggestion cards. If org publish is configured in pulse/publish.json and already verified in pulse/publish/status.json, re-publish pulse/goals.html + pulse/org-pulse.html per get_reference_doc(kind="publish-strategy") and update pulse/publish/status.json (never do the first/verifying publish unattended). Notify the user with one daily Org Pulse digest whenever this pass reached step 6; decision-worthy changes affect severity and ordering, but a steady healthy day still gets a calm all-healthy digest. When Gmail/email is available, email is the default in-depth rendering: set email_subject, email_html (EMAIL-SAFE: inline styles only — Gmail strips <style>/<head>/class CSS, so do NOT paste pulse/org-pulse.html; build an inline-styled digest with workflow health, goal scorecard, workflow alignment, LLM/cost audit, recommendations, harvested/promotions, decisions needed, and links to Goals/Pulse when published), and plain email_body on the same notify_user call; set email_to only when the org notification preference asks to replace the default To recipient; set email_cc only when the org notification preference asks for CC recipients. Report the log entry, LLM/cost summary, publish result, and notification result, then stop.`,
}

// DefaultBuiltinSchedules returns the list of product-provided schedules that
// run for every user unless overridden.
func DefaultBuiltinSchedules() []WorkflowSchedule {
	return []WorkflowSchedule{
		{
			ID:             builtinAutoEnrichMemoryID,
			Name:           "Auto-enrich memory (every 3h)",
			Description:    "Distill new Chief of Staff chats into memory on a schedule. Off by default; use /memory-setup to opt in and choose cadence.",
			CronExpression: "0 */3 * * *",
			Timezone:       "UTC",
			Enabled:        false,
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
	return scheduleID == builtinOrgPulseID || scheduleID == builtinAutoEnrichMemoryID
}

func SlashManagedBuiltinSetupCommand(scheduleID string) string {
	switch scheduleID {
	case builtinOrgPulseID:
		return "/pulse-setup"
	case builtinAutoEnrichMemoryID:
		return "/memory-setup"
	default:
		return ""
	}
}

func SlashManagedBuiltinScheduleLabel(scheduleID string) string {
	switch scheduleID {
	case builtinOrgPulseID:
		return "Daily Org Pulse"
	case builtinAutoEnrichMemoryID:
		return "automatic memory enrichment"
	default:
		return "this managed schedule"
	}
}

func SlashManagedBuiltinError(scheduleID string, action string) string {
	if scheduleID == builtinAutoEnrichMemoryID && action == "run" {
		return "Use /enrich-memory in Chief of Staff for a one-time run, or /memory-setup to configure automatic memory enrichment."
	}
	cmd := SlashManagedBuiltinSetupCommand(scheduleID)
	label := SlashManagedBuiltinScheduleLabel(scheduleID)
	if cmd == "" {
		return "Use the setup command in Chief of Staff to " + action + " " + label + "."
	}
	return "Use " + cmd + " in Chief of Staff to " + action + " " + label + "."
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

// NormalizeOrgPulseSchedule keeps Org Pulse runtime behavior on the current
// product-managed prompt sequence while preserving the user's scheduling knobs.
// Older same-ID overrides and duplicate user-created Org Pulse schedules can
// otherwise keep stale Queries/Messages forever and miss new required audit
// steps.
func NormalizeOrgPulseSchedule(sched WorkflowSchedule) WorkflowSchedule {
	if !IsOrgPulseSchedule(sched) {
		return sched
	}

	builtin, ok := FindDefaultBuiltinSchedule(builtinOrgPulseID)
	if !ok {
		return sched
	}

	normalized := builtin
	normalized.ID = sched.ID
	normalized.Enabled = sched.Enabled
	normalized.Mode = "multi-agent"
	normalized.TriggerPayload = sched.TriggerPayload
	normalized.GroupNames = sched.GroupNames
	normalized.ResumePrevious = sched.ResumePrevious

	if strings.TrimSpace(sched.Name) != "" {
		normalized.Name = sched.Name
	}
	if strings.TrimSpace(sched.Description) != "" {
		normalized.Description = sched.Description
	}
	if strings.TrimSpace(sched.ScheduleType) != "" {
		normalized.ScheduleType = sched.ScheduleType
	}
	if strings.TrimSpace(sched.CronExpression) != "" {
		normalized.CronExpression = sched.CronExpression
	}
	if strings.TrimSpace(sched.Timezone) != "" {
		normalized.Timezone = sched.Timezone
	}
	if len(sched.CalendarItems) > 0 {
		normalized.CalendarItems = make([]CalendarScheduleItem, 0, len(sched.CalendarItems))
		for _, item := range sched.CalendarItems {
			item.Messages = nil
			normalized.CalendarItems = append(normalized.CalendarItems, item)
		}
	}

	return normalized
}

func NormalizeAutoEnrichMemorySchedule(sched WorkflowSchedule) WorkflowSchedule {
	if sched.ID != builtinAutoEnrichMemoryID {
		return sched
	}

	builtin, ok := FindDefaultBuiltinSchedule(builtinAutoEnrichMemoryID)
	if !ok {
		return sched
	}

	normalized := builtin
	normalized.ID = sched.ID
	normalized.Enabled = sched.Enabled
	normalized.Mode = "multi-agent"
	normalized.TriggerPayload = sched.TriggerPayload
	normalized.GroupNames = sched.GroupNames
	normalized.ResumePrevious = sched.ResumePrevious

	if strings.TrimSpace(sched.Name) != "" {
		normalized.Name = sched.Name
	}
	if strings.TrimSpace(sched.Description) != "" {
		normalized.Description = sched.Description
	}
	if strings.TrimSpace(sched.ScheduleType) != "" {
		normalized.ScheduleType = sched.ScheduleType
	}
	if strings.TrimSpace(sched.CronExpression) != "" {
		normalized.CronExpression = sched.CronExpression
	}
	if strings.TrimSpace(sched.Timezone) != "" {
		normalized.Timezone = sched.Timezone
	}
	if len(sched.CalendarItems) > 0 {
		normalized.CalendarItems = make([]CalendarScheduleItem, 0, len(sched.CalendarItems))
		for _, item := range sched.CalendarItems {
			item.Messages = nil
			normalized.CalendarItems = append(normalized.CalendarItems, item)
		}
	}

	return normalized
}

func NormalizeBuiltinSchedule(sched WorkflowSchedule) WorkflowSchedule {
	sched = NormalizeOrgPulseSchedule(sched)
	sched = NormalizeAutoEnrichMemorySchedule(sched)
	return sched
}

// MergeBuiltinSchedules appends built-in schedules that the user has not
// overridden. Matching is by ID — a user entry with the same ID supplies the
// scheduling knobs (enabled, cron/timezone, calendar items). Recognized Org
// Pulse overrides/duplicates are normalized to the current product-managed
// content so stale persisted messages do not shadow new built-in steps.
func MergeBuiltinSchedules(userSchedules []WorkflowSchedule) []WorkflowSchedule {
	existing := make(map[string]struct{}, len(userSchedules))
	out := make([]WorkflowSchedule, 0, len(userSchedules)+len(DefaultBuiltinSchedules()))
	for _, s := range userSchedules {
		normalized := NormalizeBuiltinSchedule(s)
		existing[normalized.ID] = struct{}{}
		out = append(out, normalized)
	}
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
// folder.
func chatHistoryDirForUser(userID string) string {
	return filepath.Join(fsutil.WorkspaceDocsRoot(), "_users", userID, "chat_history")
}

func memoryEnrichmentMarkerPath(convPath string) string {
	if filepath.Base(convPath) == "conversation.json" {
		return filepath.Join(filepath.Dir(convPath), ".enriched")
	}
	return convPath + ".enriched"
}

func conversationNeedsMemoryEnrichment(convPath string) bool {
	convInfo, err := os.Stat(convPath)
	if err != nil || convInfo.IsDir() {
		return false
	}
	markInfo, err := os.Stat(memoryEnrichmentMarkerPath(convPath))
	if err != nil {
		return true
	}
	return convInfo.ModTime().After(markInfo.ModTime())
}

// hasChatsNeedingEnrichment returns true if at least one non-scheduled chat
// conversation is newer than its enrichment marker, or has no marker yet. It
// supports both legacy chat_history/<session>/conversation.json and the current
// date-bucket chat_history/YYYY-MM-DD/session-<id>-conversation.json layout.
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

		// Legacy layout: chat_history/<session-id>/conversation.json.
		if !chatHistoryIsScheduleSessionID(e.Name()) &&
			conversationNeedsMemoryEnrichment(filepath.Join(dir, e.Name(), "conversation.json")) {
			return true
		}

		// Current layout: chat_history/YYYY-MM-DD/session-<session-id>-conversation.json.
		matches, err := filepath.Glob(filepath.Join(dir, e.Name(), "session-*-conversation.json"))
		if err != nil {
			continue
		}
		for _, convPath := range matches {
			sessionID := chatHistorySessionIDFromFileName(filepath.Base(convPath))
			if sessionID == "" || chatHistoryIsScheduleSessionID(sessionID) {
				continue
			}
			if conversationNeedsMemoryEnrichment(convPath) {
				return true
			}
		}
	}
	return false
}
