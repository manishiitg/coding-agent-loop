package server

import (
	"strings"
)

// Built-in schedules are defined in Go and run for every user unless the user
// has overridden them by adding an entry with the same ID (typically
// enabled:false) to their _users/<id>/multiagent-schedules.json.
//
// Each built-in may also register a pre-fire check in PreFireChecks. The
// scheduler calls the check before starting a session; if the check returns
// false the cron tick is skipped entirely — no LLM session is spawned.

const builtinOrgPulseID = "builtin-org-pulse"
const deprecatedAutoEnrichMemoryID = "builtin-auto-enrich-memory"

const builtinOrgPulseQuery = `You are running the daily Org Pulse — the Chief of Staff's read-only alignment check over the whole org.

The user controls cadence by schedule. Do not skip the pass just because the org looks steady; write a calm steady-state read when there is nothing notable.

Call get_reference_doc(kind="org-pulse") and follow it exactly. Start by backing up org-level artifacts per get_reference_doc(kind="backup-strategy") using pulse/backup.json and pulse/backup/status.json. Read pulse/goals.html and targeted current evidence from each workflow, then report whether each org goal is on-track, at-risk, off-track, or unknown and whether each workflow is aligned, supporting, unaligned, or missing measurement. Workflow files are strictly read-only. Do not create recommendations, questions, promotions, fixes, plan changes, workflow runs, model audits, or cost audits. Before updating pulse/goals.html or pulse/org-pulse.html, call get_reference_doc(kind="org-html"). Keep pulse/goals.html as the durable current scorecard and prepend a plain-language alignment entry to pulse/org-pulse.html. If org publishing is already configured and verified, re-publish those two pages and update pulse/publish/status.json. Notify the user with one factual daily digest containing goal status, workflow alignment, stale or missing evidence, and links to the published pages when available. Send a calm all-healthy digest on steady days.`

// builtinOrgPulseMessages runs the daily Org Pulse as a message SEQUENCE — one
// focused turn per step in a single resumed session, the way workflow Pulse
// (runPostRunMonitor) does — instead of one giant single-turn prompt. Each step
// does ONE job and defers the detail to get_reference_doc(kind="org-pulse").
// builtinOrgPulseQuery above is kept as the single-turn Query fallback so nothing
// that references it breaks.
var builtinOrgPulseMessages = []string{
	`STEP 1/3 — BACKUP. You are running the daily Org Pulse, the Chief of Staff's read-only alignment check over the whole org. Call get_reference_doc(kind="org-pulse") and follow it; do only this step, then stop. Back up org-level artifacts per get_reference_doc(kind="backup-strategy") using pulse/backup.json and pulse/backup/status.json. Report the backup result, then stop.`,
	`STEP 2/3 — GOALS + WORKFLOW ALIGNMENT. In one efficient batched sweep, read pulse/goals.html and targeted current evidence from every workflow as defined by get_reference_doc(kind="org-pulse"). For each explicit org goal, compare current evidence with baseline, target, and due date; classify it on-track, at-risk, off-track, or unknown with one plain-language reason. Classify every workflow aligned, supporting, unaligned, or missing measurement and cite its freshest evidence. If there are no explicit org goals, say so and do not invent them. Workflow files are strictly read-only: do not create recommendations, questions, promotions, fixes, plan changes, runs, LLM audits, or cost audits. Call get_reference_doc(kind="org-html"), then update pulse/goals.html only when scorecard evidence, status, confidence, freshness, or alignment changed. Report the scorecard and alignment table, then stop.`,
	`STEP 3/3 — LOG + PUBLISH + DAILY DIGEST. Call get_reference_doc(kind="org-html"), then prepend one compact, dated, plain-language entry to pulse/org-pulse.html covering goal status, workflow alignment, changes since the prior pass, and stale or missing evidence. Do not include recommendations, decisions, task promotions, LLM/model audits, cost audits, or proposed fixes. If org publishing is configured in pulse/publish.json and already verified in pulse/publish/status.json, re-publish pulse/goals.html and pulse/org-pulse.html per get_reference_doc(kind="publish-strategy") and update pulse/publish/status.json. Notify the user with one factual daily Org Pulse digest; a steady healthy day still gets a calm all-healthy digest. When email is available, use an inline-styled email plus a plain-text body containing the same goal and alignment facts and links when published. Report the log, publish, and notification results, then stop.`,
}

// DefaultBuiltinSchedules returns the list of product-provided schedules that
// run for every user unless overridden.
func DefaultBuiltinSchedules() []WorkflowSchedule {
	return []WorkflowSchedule{
		{
			// Opt-in (Enabled:false): the user turns Org Pulse on via the chat
			// toggle / Scheduled Tasks popup, which writes a same-ID override with
			// enabled:true and their chosen cron. Default cadence is once a day; the
			// pass self-gates agentically (wakes, cheap "anything new?" check, exits
			// if not) rather than via a Go pre-fire check.
			ID:             builtinOrgPulseID,
			Name:           "Org Pulse (daily)",
			Description:    "The Chief of Staff's daily read-only check of org goals and workflow alignment. Off by default; turn it on to opt in. Default once a day, editable.",
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

func SlashManagedBuiltinSetupCommand(scheduleID string) string {
	switch scheduleID {
	case builtinOrgPulseID:
		return "/pulse-setup"
	default:
		return ""
	}
}

func SlashManagedBuiltinScheduleLabel(scheduleID string) string {
	switch scheduleID {
	case builtinOrgPulseID:
		return "Daily Org Pulse"
	default:
		return "this managed schedule"
	}
}

func SlashManagedBuiltinError(scheduleID string, action string) string {
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
	if mode := strings.TrimSpace(strings.ToLower(sched.Mode)); mode != "" && mode != "multi-agent" {
		return false
	}
	name := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(sched.Name), "-", " "))
	name = strings.Join(strings.Fields(name), " ")
	return name == "org pulse" || name == "daily org pulse" || name == "daily org pulse scan"
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

func NormalizeBuiltinSchedule(sched WorkflowSchedule) WorkflowSchedule {
	sched = NormalizeOrgPulseSchedule(sched)
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
		if s.ID == deprecatedAutoEnrichMemoryID {
			continue
		}
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
var PreFireChecks = map[string]PreFireCheck{}
