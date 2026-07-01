package server

import (
	"fmt"
	"strings"
)

const defaultWorkshopScheduleMessage = "Run the full workflow using run_full_workflow tool."

func workflowScheduleHasCalendarItemMessages(sched WorkflowSchedule) bool {
	for _, item := range sched.CalendarItems {
		if len(item.Messages) > 0 {
			return true
		}
	}
	return false
}

func workflowScheduleHasVersionedPrompt(sched WorkflowSchedule) bool {
	if scheduleModeOrDefault(sched.Mode) == "workshop" {
		return true
	}
	if len(sched.Messages) > 0 {
		return true
	}
	return workflowScheduleHasCalendarItemMessages(sched)
}

func workflowScheduleUsesRunMode(sched WorkflowSchedule) bool {
	return scheduledWorkshopExecutionMode(sched.WorkshopMode) == "run"
}

func defaultWorkflowPulseMessage(groupNames []string) string {
	normalized := normalizeScheduleGroupNames(groupNames)
	if len(normalized) == 0 {
		return defaultWorkshopScheduleMessage
	}

	var b strings.Builder
	b.WriteString("Do not ask for confirmation; proceed autonomously. ")
	if len(normalized) == 1 {
		b.WriteString(fmt.Sprintf("Run the full workflow for group_name %q by calling run_full_workflow(group_name=%q). ", normalized[0], normalized[0]))
	} else {
		b.WriteString(fmt.Sprintf("Run the full workflow for these group_names: %s. ", strings.Join(normalized, ", ")))
		b.WriteString("Call ")
		for i, groupName := range normalized {
			if i > 0 {
				b.WriteString(", then ")
			}
			b.WriteString(fmt.Sprintf("run_full_workflow(group_name=%q)", groupName))
		}
		b.WriteString(". ")
	}
	b.WriteString("Use default evaluation behavior so latest run evidence, reports, metrics, notifications, and cost tracking stay available. Stop after the configured group_names have run and report the run status plainly.")
	return b.String()
}

func defaultWorkflowPulseMessages(groupNames []string) []string {
	return []string{defaultWorkflowPulseMessage(groupNames)}
}

func legacyWorkflowPulseUIMigrationMessage(sched WorkflowSchedule, followupMessage string) string {
	removeSelfInstruction := fmt.Sprintf(
		"8. After saving files, call update_schedule(job_id=%q, mode=\"workshop\", workshop_mode=\"run\", messages=[%q]) so this migration prompt is removed from future fires. Preserve name, cadence, timezone, enabled state, and group_names by omitting those fields.",
		sched.ID,
		followupMessage,
	)
	return strings.Join([]string{
		"LEGACY PULSE UI MIGRATION",
		"",
		"This workflow.json originally had no schema_version, so this first Pulse turn is a one-time compatibility migration before the normal scheduled run.",
		"",
		"Do not run the workflow in this migration turn and do not ask for confirmation.",
		"1. Call get_reference_doc(kind=\"html-output\") before writing any .html file.",
		"2. If reports/report_plan.json exists or you touch reports/*.html, also call get_reference_doc(kind=\"report-plan\").",
		"3. Read existing workflow-owned review/report surfaces if present: builder/review.html, builder/review.md, builder/improve.html, builder/improve.md, reports/report_plan.json, and reports/**/*.html.",
		"4. Migrate legacy Markdown review/improve surfaces to self-contained HTML where needed, and refresh existing workflow-owned HTML to the current HTML output guidance: summary at top, dark-mode CSS, semantic badges/tables, and no external assets or CDNs.",
		"5. If you update builder/review.html, treat this migration as a report-only compatibility section. Do not add action items unless a separate current review command explicitly asks for them.",
		"6. Preserve existing content and unresolved findings. Update only the UI/report presentation needed for current Pulse output.",
		"7. Do not remove or change the normal Pulse run message shown in the next step.",
		removeSelfInstruction,
		"9. Stop after saving files and updating the schedule, then summarize what changed. The next scheduled message will run the workflow.",
	}, "\n")
}

func legacyWorkflowPulseUIMigrationMessages(sched WorkflowSchedule) []string {
	followupMessage := defaultWorkflowPulseMessage(sched.GroupNames)
	return []string{
		legacyWorkflowPulseUIMigrationMessage(sched, followupMessage),
		followupMessage,
	}
}

func defaultMigratedWorkflowPulseMessages(manifest *WorkflowManifest, sched WorkflowSchedule) []string {
	if manifest != nil && manifest.schemaVersionMissing {
		return legacyWorkflowPulseUIMigrationMessages(sched)
	}
	return defaultWorkflowPulseMessages(sched.GroupNames)
}

func applyNewWorkflowScheduleDefaults(sched *WorkflowSchedule) {
	if sched == nil {
		return
	}
	if strings.TrimSpace(sched.Mode) == "" {
		sched.Mode = "workshop"
	}
	applyWorkflowScheduleWorkshopDefaults(sched)
}

func markWorkflowScheduleVersionCurrent(sched *WorkflowSchedule) {
	if sched == nil {
		return
	}
	sched.ScheduleVersion = CurrentWorkflowScheduleVersion
}

func applyWorkflowScheduleWorkshopDefaults(sched *WorkflowSchedule) {
	if sched == nil || strings.TrimSpace(sched.Mode) != "workshop" {
		return
	}
	if strings.TrimSpace(sched.WorkshopMode) == "" {
		sched.WorkshopMode = "run"
	}
	if workflowScheduleUsesRunMode(*sched) && len(sched.Messages) == 0 && !workflowScheduleHasCalendarItemMessages(*sched) {
		sched.Messages = defaultWorkflowPulseMessages(sched.GroupNames)
	}
}

func markWorkflowSchedulePromptCurrent(sched *WorkflowSchedule) {
	if sched == nil {
		return
	}
	if workflowScheduleHasVersionedPrompt(*sched) {
		sched.PromptVersion = CurrentWorkflowSchedulePromptVersion
		return
	}
	sched.PromptVersion = 0
}

type workflowScheduleMigrationSummary struct {
	ConvertedDirectToPulse  int
	StampedCurrent          int
	StaleWorkshopPrompts    int
	LegacyPulseUIMigrations int
}

func migrateWorkflowManifestSchedulesToCurrent(manifest *WorkflowManifest) (workflowScheduleMigrationSummary, bool) {
	var summary workflowScheduleMigrationSummary
	if manifest == nil {
		return summary, false
	}

	changed := false
	for i := range manifest.Schedules {
		sched := &manifest.Schedules[i]
		if sched.ScheduleVersion >= CurrentWorkflowScheduleVersion {
			if workflowSchedulePromptStale(*sched) {
				summary.StaleWorkshopPrompts++
			}
			continue
		}

		mode := strings.TrimSpace(sched.Mode)
		switch mode {
		case "", "workflow":
			sched.Mode = "workshop"
			sched.WorkshopMode = "run"
			if len(sched.Messages) == 0 && !workflowScheduleHasCalendarItemMessages(*sched) {
				sched.Messages = defaultMigratedWorkflowPulseMessages(manifest, *sched)
				if manifest.schemaVersionMissing {
					summary.LegacyPulseUIMigrations++
				}
			}
			markWorkflowSchedulePromptCurrent(sched)
			summary.ConvertedDirectToPulse++
			changed = true
		case "workshop":
			if workflowSchedulePromptStale(*sched) {
				summary.StaleWorkshopPrompts++
			}
		}

		markWorkflowScheduleVersionCurrent(sched)
		summary.StampedCurrent++
		changed = true
	}

	return summary, changed
}

func workflowSchedulePromptStale(sched WorkflowSchedule) bool {
	return workflowScheduleHasVersionedPrompt(sched) && sched.PromptVersion < CurrentWorkflowSchedulePromptVersion
}

func workflowSchedulePromptStatus(sched WorkflowSchedule) string {
	if !workflowScheduleHasVersionedPrompt(sched) {
		return "not versioned"
	}
	if workflowSchedulePromptStale(sched) {
		return fmt.Sprintf("legacy prompt version %d; current is %d", sched.PromptVersion, CurrentWorkflowSchedulePromptVersion)
	}
	return fmt.Sprintf("current prompt version %d", sched.PromptVersion)
}

func workflowScheduleRuntimeMessages(sctx *ScheduleContext) []string {
	messages := sctx.Schedule.Messages
	if len(messages) == 0 {
		messages = []string{defaultWorkshopScheduleMessage}
	}
	if upgradeMessage := workflowSchedulePromptUpgradeMessage(sctx, messages); upgradeMessage != "" {
		return []string{upgradeMessage}
	}
	return messages
}

func workflowSchedulePromptUpgradeMessage(sctx *ScheduleContext, legacyMessages []string) string {
	if sctx == nil || !workflowSchedulePromptStale(sctx.Schedule) {
		return ""
	}

	var b strings.Builder
	b.WriteString("SCHEDULE PROMPT UPGRADE REQUIRED\n\n")
	b.WriteString(fmt.Sprintf("This scheduled pulse is saved with prompt_version=%d, but the current workflow schedule prompt version is %d.\n", sctx.Schedule.PromptVersion, CurrentWorkflowSchedulePromptVersion))
	b.WriteString("Before doing this run's normal work, refresh the saved schedule prompt so future pulses use the latest workflow HTML/reporting and auto-improvement rules.\n\n")
	b.WriteString("Current schedule:\n")
	b.WriteString(fmt.Sprintf("- id: %s\n", sctx.Schedule.ID))
	b.WriteString(fmt.Sprintf("- name: %s\n", sctx.Schedule.Name))
	b.WriteString(fmt.Sprintf("- mode: %s\n", scheduleModeOrDefault(sctx.Schedule.Mode)))
	b.WriteString(fmt.Sprintf("- workshop_mode: %s\n", strings.TrimSpace(sctx.Schedule.WorkshopMode)))
	b.WriteString(fmt.Sprintf("- cron_expression: %s\n", sctx.Schedule.CronExpression))
	b.WriteString(fmt.Sprintf("- timezone: %s\n", scheduleTimezoneOrDefault(sctx.Schedule.Timezone)))
	b.WriteString(fmt.Sprintf("- group_names: %q\n\n", sctx.Schedule.GroupNames))

	b.WriteString("Upgrade steps:\n")
	b.WriteString("1. Call get_workflow_config and inspect this schedule.\n")
	b.WriteString("2. Call get_reference_doc(kind=\"workflow-tools\") for current schedule message rules.\n")
	b.WriteString("3. If this schedule is one of the /auto-improve run, harden, or replan-proposal pulses, call get_workflow_command_guidance(kind=\"auto-improve\") and use the latest RUN/HARDEN/REPLAN-PROPOSAL message requirements.\n")
	b.WriteString(fmt.Sprintf("4. Call update_schedule(job_id=%q, ...) to replace stale messages, description, mode, and workshop_mode as needed. Preserve cadence, timezone, enabled state, and group_names unless they are invalid or clearly drifted.\n", sctx.Schedule.ID))
	b.WriteString(fmt.Sprintf("5. Updating the messages marks this schedule with prompt_version=%d automatically. After the update, continue this fire using the refreshed instructions. Do not ask for confirmation unless a required value is missing.\n\n", CurrentWorkflowSchedulePromptVersion))

	b.WriteString("Legacy saved message intent for this fire. Use it only as background intent; prefer the current guidance above when rewriting and executing:\n")
	for i, msg := range legacyMessages {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, msg))
	}
	return b.String()
}
