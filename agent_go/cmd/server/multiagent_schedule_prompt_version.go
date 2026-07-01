package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

const CurrentMultiAgentScheduleFileVersion = 1
const CurrentMultiAgentScheduleVersion = 1
const CurrentMultiAgentSchedulePromptVersion = 1

func multiAgentScheduleFileSchemaVersionMissing(content string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return false
	}
	_, hasSchemaVersion := raw["schema_version"]
	return !hasSchemaVersion
}

func multiAgentScheduleHasVersionedPrompt(sched WorkflowSchedule) bool {
	return strings.TrimSpace(sched.Query) != ""
}

func markMultiAgentScheduleVersionCurrent(sched *WorkflowSchedule) {
	if sched == nil {
		return
	}
	sched.ScheduleVersion = CurrentMultiAgentScheduleVersion
}

func markMultiAgentSchedulePromptCurrent(sched *WorkflowSchedule) {
	if sched == nil {
		return
	}
	if multiAgentScheduleHasVersionedPrompt(*sched) {
		sched.PromptVersion = CurrentMultiAgentSchedulePromptVersion
		return
	}
	sched.PromptVersion = 0
}

func multiAgentSchedulePromptStale(sched WorkflowSchedule) bool {
	return multiAgentScheduleHasVersionedPrompt(sched) && sched.PromptVersion < CurrentMultiAgentSchedulePromptVersion
}

func multiAgentSchedulePromptStatus(sched WorkflowSchedule) string {
	if !multiAgentScheduleHasVersionedPrompt(sched) {
		return "not versioned"
	}
	if multiAgentSchedulePromptStale(sched) {
		return fmt.Sprintf("legacy prompt version %d; current is %d", sched.PromptVersion, CurrentMultiAgentSchedulePromptVersion)
	}
	return fmt.Sprintf("current prompt version %d", sched.PromptVersion)
}

func multiAgentScheduleRuntimeQuery(sctx *ScheduleContext) string {
	if sctx == nil {
		return ""
	}
	query := strings.TrimSpace(sctx.Schedule.Query)
	if query == "" {
		return ""
	}
	if upgradeQuery := multiAgentSchedulePromptUpgradeQuery(sctx, query); upgradeQuery != "" {
		return upgradeQuery
	}
	return query
}

func multiAgentSchedulePromptUpgradeQuery(sctx *ScheduleContext, legacyQuery string) string {
	if sctx == nil || !multiAgentSchedulePromptStale(sctx.Schedule) {
		return ""
	}

	userID := strings.TrimSpace(sctx.UserID)
	if userID == "" {
		userID = "default"
	}
	filePath := multiAgentSchedulesPath(userID)

	var b strings.Builder
	b.WriteString("CHIEF OF STAFF SCHEDULE PROMPT UPGRADE REQUIRED\n\n")
	b.WriteString(fmt.Sprintf("This Chief-of-Staff scheduled task is saved with prompt_version=%d, but the current multi-agent schedule prompt version is %d.\n", sctx.Schedule.PromptVersion, CurrentMultiAgentSchedulePromptVersion))
	b.WriteString("Before doing this run's normal work, refresh the saved schedule query so future scheduled fires use the latest Chief-of-Staff schedule-management rules.\n\n")
	b.WriteString("Current schedule:\n")
	b.WriteString(fmt.Sprintf("- id: %s\n", sctx.Schedule.ID))
	b.WriteString(fmt.Sprintf("- name: %s\n", sctx.Schedule.Name))
	b.WriteString(fmt.Sprintf("- mode: %s\n", strings.TrimSpace(sctx.Schedule.Mode)))
	b.WriteString(fmt.Sprintf("- cron_expression: %s\n", sctx.Schedule.CronExpression))
	b.WriteString(fmt.Sprintf("- timezone: %s\n", scheduleTimezoneOrDefault(sctx.Schedule.Timezone)))
	b.WriteString(fmt.Sprintf("- user_id: %s\n", userID))
	b.WriteString(fmt.Sprintf("- schedule_file: %s\n\n", filePath))

	b.WriteString("Upgrade steps:\n")
	b.WriteString("1. Call get_reference_doc(kind=\"schedule-management\") before editing the schedule file.\n")
	b.WriteString(fmt.Sprintf("2. Read %s, find this schedule by id, and preserve its cadence, enabled state, user scope, capabilities, and core user intent.\n", filePath))
	b.WriteString(fmt.Sprintf("3. Rewrite the saved query only as needed to match the current Chief-of-Staff schedule guidance. Keep mode=\"multi-agent\", set schedule_version=%d, set prompt_version=%d, and keep schema_version=%d at the file root.\n", CurrentMultiAgentScheduleVersion, CurrentMultiAgentSchedulePromptVersion, CurrentMultiAgentScheduleFileVersion))
	b.WriteString("4. Save the JSON file with valid syntax. Do not ask for confirmation unless a required value is missing.\n")
	b.WriteString("5. After saving, continue this fire using the refreshed instructions you just wrote.\n\n")

	b.WriteString("Legacy saved query intent for this fire. Use it as background intent, but prefer the current schedule-management guidance when rewriting and executing:\n")
	b.WriteString(legacyQuery)
	return b.String()
}

type multiAgentScheduleMigrationSummary struct {
	StampedFiles    int
	StampedCurrent  int
	StalePrompts    int
	LegacyFileRoots int
}

func migrateMultiAgentScheduleFileToCurrent(f *MultiAgentScheduleFile) (multiAgentScheduleMigrationSummary, bool) {
	var summary multiAgentScheduleMigrationSummary
	if f == nil {
		return summary, false
	}

	changed := false
	if f.schemaVersionMissing {
		f.SchemaVersion = CurrentMultiAgentScheduleFileVersion
		summary.LegacyFileRoots++
		summary.StampedFiles++
		changed = true
	} else if f.SchemaVersion < CurrentMultiAgentScheduleFileVersion {
		f.SchemaVersion = CurrentMultiAgentScheduleFileVersion
		summary.StampedFiles++
		changed = true
	}

	for i := range f.Schedules {
		sched := &f.Schedules[i]
		if strings.TrimSpace(sched.Mode) == "" {
			sched.Mode = "multi-agent"
			changed = true
		}
		if sched.ScheduleVersion < CurrentMultiAgentScheduleVersion {
			sched.ScheduleVersion = CurrentMultiAgentScheduleVersion
			summary.StampedCurrent++
			changed = true
		}
		if multiAgentSchedulePromptStale(*sched) {
			summary.StalePrompts++
		}
	}

	return summary, changed
}
