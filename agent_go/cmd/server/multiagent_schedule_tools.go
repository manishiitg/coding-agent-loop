package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// createMultiAgentScheduleTools returns chat-mode tools for managing the current
// user's multi-agent (Chief-of-Staff) cron schedules stored in
// _users/<id>/multiagent-schedules.json. Mirrors createWorkflowScheduleTools but
// is scoped to the calling user and is cron-only (no calendar variant). All
// writes go through the schedule store server-side, so the agent must NEVER edit
// multiagent-schedules.json directly.
func createMultiAgentScheduleTools() []llmtypes.Tool {
	return []llmtypes.Tool{
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "list_multiagent_schedules",
				Description: "List this user's multi-agent (Chief-of-Staff) cron schedules. Each entry includes id, name, cron expression, timezone, enabled state, next run time (UTC), and the query that runs. Use this BEFORE creating a new schedule to avoid overlap and to discover the id needed by update/delete/trigger/get-runs.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"enabled_only": map[string]interface{}{
							"type":        "boolean",
							"description": "When true, only return schedules with enabled=true. Defaults to false (returns all).",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "create_multiagent_schedule",
				Description: "Create a new cron schedule for the multi-agent chat. The schedule runs the given query as a fresh multi-agent session on its cron cadence. Multi-agent schedules are cron-only (no calendar variant). Do NOT edit multiagent-schedules.json yourself; this tool writes it server-side.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Display name for the schedule (e.g. 'Daily morning briefing').",
						},
						"cron_expression": map[string]interface{}{
							"type":        "string",
							"description": "5-field cron expression (minute hour day-of-month month day-of-week). Examples: '0 9 * * *' (daily 9 AM), '*/30 * * * *' (every 30 min), '0 9 * * 1-5' (weekdays 9 AM), '0 0 1 * *' (first of month midnight).",
						},
						"timezone": map[string]interface{}{
							"type":        "string",
							"description": "Required IANA timezone (e.g. 'UTC', 'America/New_York', 'Asia/Kolkata'). Do not use abbreviations like EST, PST, or IST.",
						},
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The message/instruction the multi-agent chat executes on each scheduled run. Required.",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Optional human-readable note describing what this schedule does.",
						},
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether the schedule is active. Defaults to true.",
						},
					},
					Required: []string{"name", "cron_expression", "timezone", "query"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "update_multiagent_schedule",
				Description: "Update an existing multi-agent schedule. Only provided fields are changed; omitted fields keep their current values. To temporarily pause a schedule, set enabled=false (preserves the rest of the definition).",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"schedule_id": map[string]interface{}{
							"type":        "string",
							"description": "The schedule id to update (from list_multiagent_schedules).",
						},
						"name": map[string]interface{}{
							"type":        "string",
							"description": "New display name.",
						},
						"cron_expression": map[string]interface{}{
							"type":        "string",
							"description": "New 5-field cron expression.",
						},
						"timezone": map[string]interface{}{
							"type":        "string",
							"description": "New IANA timezone (e.g. 'UTC', 'America/New_York', 'Asia/Kolkata'). Do not use abbreviations like EST, PST, or IST.",
						},
						"query": map[string]interface{}{
							"type":        "string",
							"description": "New message/instruction to execute on each run. Cannot be set to empty.",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "New human-readable note.",
						},
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "Enable or disable the schedule.",
						},
					},
					Required: []string{"schedule_id"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "delete_multiagent_schedule",
				Description: "Permanently delete a multi-agent schedule by id. This cannot be undone.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"schedule_id": map[string]interface{}{
							"type":        "string",
							"description": "The schedule id to delete (from list_multiagent_schedules).",
						},
					},
					Required: []string{"schedule_id"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "trigger_multiagent_schedule",
				Description: "Manually run a multi-agent schedule immediately, outside its normal cron timing.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"schedule_id": map[string]interface{}{
							"type":        "string",
							"description": "The schedule id to trigger (from list_multiagent_schedules).",
						},
					},
					Required: []string{"schedule_id"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_multiagent_schedule_runs",
				Description: "View recent execution history for a multi-agent schedule, including status, duration, and errors.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"schedule_id": map[string]interface{}{
							"type":        "string",
							"description": "The schedule id (from list_multiagent_schedules).",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of runs to return. Defaults to 10.",
						},
					},
					Required: []string{"schedule_id"},
				},
			},
		},
	}
}

// multiAgentScheduleUserID resolves the user id the multi-agent schedule tools
// operate on. The multi-agent schedule store defaults to the "default" user when
// no multi-tenant identity is set.
func multiAgentScheduleUserID(currentUserID string) string {
	if strings.TrimSpace(currentUserID) == "" {
		return "default"
	}
	return currentUserID
}

// createMultiAgentScheduleExecutors wires the multi-agent schedule tools to the
// schedule store. All operations are scoped to currentUserID and write through
// the store (WriteMultiAgentSchedules), never by editing the JSON file directly.
func createMultiAgentScheduleExecutors(api *StreamingAPI, currentUserID string) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	userID := multiAgentScheduleUserID(currentUserID)

	return map[string]func(ctx context.Context, args map[string]interface{}) (string, error){
		"list_multiagent_schedules": func(ctx context.Context, args map[string]interface{}) (string, error) {
			enabledOnly, _ := args["enabled_only"].(bool)
			f, _, err := ReadMultiAgentSchedules(ctx, userID)
			if err != nil {
				return "", err
			}
			// Merge in product built-ins (Org Pulse, …) so the agent
			// sees their effective state — a same-ID entry in the user file wins, so
			// the user's enable/disable/cron override of a built-in is reflected here
			// exactly as the scheduler and the Scheduled Tasks UI compute it.
			var scheds []WorkflowSchedule
			for _, s := range MergeBuiltinSchedules(f.Schedules) {
				if enabledOnly && !s.Enabled {
					continue
				}
				scheds = append(scheds, s)
			}
			if len(scheds) == 0 {
				return "No multi-agent schedules found.", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Multi-agent schedules (%d) — times are UTC\n\n", len(scheds)))
			for _, s := range scheds {
				status := "disabled"
				if s.Enabled {
					status = "enabled"
				}
				nextRun := "unscheduled"
				if nr := getNextRunTime(s.CronExpression, s.Timezone); nr != nil {
					nextRun = nr.Format(time.RFC3339)
				}
				sb.WriteString(fmt.Sprintf("- **%s** (`%s`) — %s\n", s.Name, s.ID, status))
				sb.WriteString(fmt.Sprintf("  - cron: `%s` (%s) | next: %s\n", s.CronExpression, scheduleTimezoneOrDefault(s.Timezone), nextRun))
				if s.Description != "" {
					sb.WriteString(fmt.Sprintf("  - description: %s\n", s.Description))
				}
				if s.Query != "" {
					sb.WriteString(fmt.Sprintf("  - query: %s\n", s.Query))
				}
				if api.scheduler != nil {
					st := api.scheduler.GetRuntimeStateForUser(userID, s.ID)
					if st.LastStatus != "" {
						lastRun := ""
						if st.LastRunAt != nil {
							lastRun = " at " + st.LastRunAt.Format(time.RFC3339)
						}
						sb.WriteString(fmt.Sprintf("  - last run: %s%s\n", st.LastStatus, lastRun))
					}
				}
			}
			return sb.String(), nil
		},

		"create_multiagent_schedule": func(ctx context.Context, args map[string]interface{}) (string, error) {
			name, _ := args["name"].(string)
			cronExpr, _ := args["cron_expression"].(string)
			query, _ := args["query"].(string)
			if strings.TrimSpace(name) == "" || strings.TrimSpace(cronExpr) == "" || strings.TrimSpace(query) == "" {
				return "name, cron_expression, and query are required.", nil
			}
			timezone, _ := args["timezone"].(string)
			if err := ValidateScheduleTimezone(timezone); err != nil {
				return err.Error(), nil
			}
			if err := ValidateCronExpression(cronExpr); err != nil {
				return err.Error(), nil
			}
			enabled := true
			if raw, ok := args["enabled"]; ok && raw != nil {
				if b, ok2 := raw.(bool); ok2 {
					enabled = b
				}
			}
			description, _ := args["description"].(string)

			f, _, err := ReadMultiAgentSchedules(ctx, userID)
			if err != nil {
				return "", err
			}
			newSched := WorkflowSchedule{
				ID:             generateScheduleID(),
				Name:           name,
				Description:    description,
				ScheduleType:   "cron",
				CronExpression: cronExpr,
				Timezone:       timezone,
				Enabled:        enabled,
				Mode:           "multi-agent",
				Query:          query,
			}
			f.Schedules = append(f.Schedules, newSched)
			if err := WriteMultiAgentSchedules(ctx, userID, f); err != nil {
				return "", fmt.Errorf("failed to write multi-agent schedules: %w", err)
			}
			if api.scheduler != nil {
				if err := api.scheduler.ReloadMultiAgentSchedule(ctx, userID, newSched.ID); err != nil {
					return fmt.Sprintf("Schedule created (id: %s) but failed to activate: %v", newSched.ID, err), nil
				}
			}
			nextRunStr := "unknown"
			if nr := getNextRunTime(cronExpr, timezone); nr != nil {
				nextRunStr = nr.Format(time.RFC3339)
			}
			return fmt.Sprintf("Multi-agent schedule created and activated.\n- **ID**: `%s`\n- **Name**: %s\n- **Cron**: `%s`\n- **Timezone**: %s\n- **Next Run**: %s", newSched.ID, name, cronExpr, timezone, nextRunStr), nil
		},

		"update_multiagent_schedule": func(ctx context.Context, args map[string]interface{}) (string, error) {
			scheduleID, _ := args["schedule_id"].(string)
			if scheduleID == "" {
				return "schedule_id is required.", nil
			}
			if cronExpr, ok := args["cron_expression"].(string); ok && cronExpr != "" {
				if err := ValidateCronExpression(cronExpr); err != nil {
					return err.Error(), nil
				}
			}
			if tz, ok := args["timezone"].(string); ok && tz != "" {
				if err := ValidateScheduleTimezone(tz); err != nil {
					return err.Error(), nil
				}
			}

			f, _, err := ReadMultiAgentSchedules(ctx, userID)
			if err != nil {
				return "", err
			}
			idx := -1
			for i := range f.Schedules {
				if f.Schedules[i].ID == scheduleID {
					idx = i
					break
				}
			}
			if idx < 0 {
				// Built-in schedules (e.g. Org Pulse / builtin-org-pulse) are not
				// stored in the user file until the user first overrides one. Enabling
				// or tuning a built-in through this tool must materialize a same-ID
				// override so the merge in the scheduler AND the Scheduled Tasks UI
				// (MergeBuiltinSchedules) reports the effective state. Without this the
				// update silently fails ("not found") and the toggle stays off even
				// though the user opted in via /pulse-setup.
				if builtin, ok := FindDefaultBuiltinSchedule(scheduleID); ok {
					f.Schedules = append(f.Schedules, builtin)
					idx = len(f.Schedules) - 1
				} else {
					return fmt.Sprintf("multi-agent schedule %s not found.", scheduleID), nil
				}
			}
			sched := &f.Schedules[idx]
			if name, ok := args["name"].(string); ok && name != "" {
				sched.Name = name
			}
			if cronExpr, ok := args["cron_expression"].(string); ok && cronExpr != "" {
				sched.CronExpression = cronExpr
			}
			if tz, ok := args["timezone"].(string); ok && tz != "" {
				sched.Timezone = tz
			}
			if query, ok := args["query"].(string); ok {
				if strings.TrimSpace(query) == "" {
					return "query cannot be set to empty. Omit it to keep the current value.", nil
				}
				sched.Query = query
			}
			if desc, ok := args["description"].(string); ok {
				sched.Description = desc
			}
			if raw, ok := args["enabled"]; ok && raw != nil {
				if b, ok2 := raw.(bool); ok2 {
					sched.Enabled = b
				}
			}
			// Keep built-in content product-managed while preserving user scheduling knobs.
			*sched = NormalizeBuiltinSchedule(*sched)
			sched.Mode = "multi-agent"

			if err := WriteMultiAgentSchedules(ctx, userID, f); err != nil {
				return "", fmt.Errorf("failed to write multi-agent schedules: %w", err)
			}
			if api.scheduler != nil {
				if err := api.scheduler.ReloadMultiAgentSchedule(ctx, userID, scheduleID); err != nil {
					return fmt.Sprintf("Schedule updated but failed to reload: %v", err), nil
				}
			}
			nextRunStr := "unknown"
			if nr := getNextRunTime(sched.CronExpression, sched.Timezone); nr != nil {
				nextRunStr = nr.Format(time.RFC3339)
			}
			return fmt.Sprintf("Multi-agent schedule updated.\n- **ID**: `%s`\n- **Name**: %s\n- **Cron**: `%s`\n- **Enabled**: %v\n- **Next Run**: %s", sched.ID, sched.Name, sched.CronExpression, sched.Enabled, nextRunStr), nil
		},

		"delete_multiagent_schedule": func(ctx context.Context, args map[string]interface{}) (string, error) {
			scheduleID, _ := args["schedule_id"].(string)
			if scheduleID == "" {
				return "schedule_id is required.", nil
			}
			f, exists, err := ReadMultiAgentSchedules(ctx, userID)
			if err != nil {
				return "", err
			}
			if !exists {
				return fmt.Sprintf("multi-agent schedule %s not found.", scheduleID), nil
			}
			idx := -1
			for i := range f.Schedules {
				if f.Schedules[i].ID == scheduleID {
					idx = i
					break
				}
			}
			if idx < 0 {
				return fmt.Sprintf("multi-agent schedule %s not found.", scheduleID), nil
			}
			f.Schedules = append(f.Schedules[:idx], f.Schedules[idx+1:]...)
			if err := WriteMultiAgentSchedules(ctx, userID, f); err != nil {
				return "", fmt.Errorf("failed to write multi-agent schedules: %w", err)
			}
			if api.scheduler != nil {
				// Not found in the file now → ReloadMultiAgentSchedule removes the job.
				_ = api.scheduler.ReloadMultiAgentSchedule(ctx, userID, scheduleID)
			}
			return "Multi-agent schedule `" + scheduleID + "` deleted.", nil
		},

		"trigger_multiagent_schedule": func(ctx context.Context, args map[string]interface{}) (string, error) {
			scheduleID, _ := args["schedule_id"].(string)
			if scheduleID == "" {
				return "schedule_id is required.", nil
			}
			if api.scheduler == nil {
				return "", fmt.Errorf("scheduler not available")
			}
			if _, err := api.scheduler.TriggerMultiAgentNow(userID, scheduleID); err != nil {
				return "", err
			}
			return fmt.Sprintf("Multi-agent schedule triggered. Schedule ID: `%s`", scheduleID), nil
		},

		"get_multiagent_schedule_runs": func(ctx context.Context, args map[string]interface{}) (string, error) {
			scheduleID, _ := args["schedule_id"].(string)
			if scheduleID == "" {
				return "schedule_id is required.", nil
			}
			limit := 10
			if raw, ok := args["limit"]; ok && raw != nil {
				switch v := raw.(type) {
				case float64:
					limit = int(v)
				case int:
					limit = v
				}
			}
			if limit <= 0 {
				limit = 10
			}
			runs, total, err := ListMultiAgentScheduleRuns(ctx, userID, scheduleID, limit, 0)
			if err != nil {
				return "", err
			}
			if len(runs) == 0 {
				return "No runs found for this schedule.", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Runs for `%s` (showing %d of %d)\n\n", scheduleID, len(runs), total))
			for _, r := range runs {
				sb.WriteString(fmt.Sprintf("- **%s** — %s\n", r.StartedAt.Format(time.RFC3339), r.Status))
				if r.DurationMs != nil {
					sb.WriteString(fmt.Sprintf("  - duration: %dms\n", *r.DurationMs))
				}
				if r.SessionID != "" {
					sb.WriteString(fmt.Sprintf("  - session: `%s`\n", r.SessionID))
				}
				if r.Error != "" {
					sb.WriteString(fmt.Sprintf("  - error: %s\n", r.Error))
				}
			}
			return sb.String(), nil
		},
	}
}
