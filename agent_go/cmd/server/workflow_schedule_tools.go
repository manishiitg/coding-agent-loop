package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// createWorkflowScheduleTools returns chat-mode tools for managing workflow
// schedules stored in workflow.json manifests. Mirrors the workshop builder's
// schedule tools (interactive_workshop_manager.go) but adds workflow_path as
// an explicit argument since chat isn't scoped to a single workflow folder.
func createWorkflowScheduleTools() []llmtypes.Tool {
	return []llmtypes.Tool{
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "list_all_schedules",
				Description: "List every schedule across all workflows plus the current user's multi-agent schedules. Use this BEFORE creating a new schedule to check what's already firing at the same time and avoid overlap. Each entry includes cron expression, timezone, enabled state, next run time (UTC), mode, and source.",
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
				Name:        "list_workflow_schedules",
				Description: "List all cron schedules defined in a SINGLE workflow's workflow.json manifest. For a global view across all workflows, use list_all_schedules instead.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"workflow_path": map[string]interface{}{
							"type":        "string",
							"description": "Workspace-relative workflow path (e.g. 'Workflow/ICICI BANK PARSING').",
						},
					},
					Required: []string{"workflow_path"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "create_workflow_schedule",
				Description: "Create a new cron schedule on a workflow. The schedule will fire at the specified cron times and run the workflow's group(s). Use mode='workshop' with messages to drive runs via the LLM-driven workshop builder; use mode='multi-agent' with a query to drive runs via a multi-agent chat session. Default mode is 'workflow' (direct orchestrator).",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"workflow_path": map[string]interface{}{
							"type":        "string",
							"description": "Workspace-relative workflow path (e.g. 'Workflow/ICICI BANK PARSING').",
						},
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Display name for the schedule (e.g. 'Daily morning run').",
						},
						"cron_expression": map[string]interface{}{
							"type":        "string",
							"description": "5-field cron expression (minute hour day-of-month month day-of-week). Examples: '0 9 * * *' (daily 9 AM), '*/30 * * * *' (every 30 min), '0 0 * * 1' (weekly Monday midnight).",
						},
						"timezone": map[string]interface{}{
							"type":        "string",
							"description": "IANA timezone (e.g. 'America/New_York', 'Asia/Kolkata'). Defaults to 'UTC'.",
						},
						"group_names": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Variable group names to run (e.g. ['group-1']). Required for mode='workflow' or 'workshop'. Read variables.json to see available groups.",
						},
						"mode": map[string]interface{}{
							"type":        "string",
							"description": "Execution mode: 'workflow' (default, direct orchestrator), 'workshop' (LLM-driven via workshop builder), or 'multi-agent' (LLM-driven via multi-agent chat).",
							"enum":        []string{"workflow", "workshop", "multi-agent"},
						},
						"messages": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Required when mode='workshop'. Predefined message queue sent one-by-one to the workshop LLM. Example: ['Run the full workflow using run_full_workflow(group_name=\"group-1\")'].",
						},
						"workshop_mode": map[string]interface{}{
							"type":        "string",
							"description": "Workshop builder mode when mode='workshop'. Defaults to 'run'. Use 'optimizer' for runs that also generate learnings.",
							"enum":        []string{"run", "optimizer"},
						},
					},
					Required: []string{"workflow_path", "name", "cron_expression"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "update_workflow_schedule",
				Description: "Update an existing schedule. Only provided fields are changed; omitted fields keep their current values.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"job_id": map[string]interface{}{
							"type":        "string",
							"description": "The schedule ID to update (from list_workflow_schedules).",
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
							"description": "New IANA timezone.",
						},
						"group_names": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Replace the variable group names. Omit to keep current. Do not pass an empty array.",
						},
						"enabled": map[string]interface{}{
							"type":        "boolean",
							"description": "Enable or disable the schedule.",
						},
						"mode": map[string]interface{}{
							"type":        "string",
							"description": "Execution mode override.",
							"enum":        []string{"workflow", "workshop", "multi-agent"},
						},
						"messages": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Replace the workshop-mode message queue.",
						},
						"workshop_mode": map[string]interface{}{
							"type":        "string",
							"description": "Workshop builder mode override.",
							"enum":        []string{"run", "optimizer"},
						},
					},
					Required: []string{"job_id"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "delete_workflow_schedule",
				Description: "Permanently delete a schedule. This cannot be undone.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"job_id": map[string]interface{}{
							"type":        "string",
							"description": "The schedule ID to delete (from list_workflow_schedules).",
						},
					},
					Required: []string{"job_id"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "trigger_workflow_schedule",
				Description: "Manually trigger a schedule to run immediately, outside its normal cron timing. Returns the session ID of the triggered run.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"job_id": map[string]interface{}{
							"type":        "string",
							"description": "The schedule ID to trigger (from list_workflow_schedules).",
						},
					},
					Required: []string{"job_id"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_workflow_schedule_runs",
				Description: "View execution history for a specific schedule, including status, duration, and errors.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"job_id": map[string]interface{}{
							"type":        "string",
							"description": "The schedule ID (from list_workflow_schedules).",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of runs to return. Defaults to 10.",
						},
					},
					Required: []string{"job_id"},
				},
			},
		},
	}
}

// createWorkflowScheduleExecutors wires the chat tools to the same scheduler
// callback closures the workshop builder uses, so behavior stays identical.
// currentUserID scopes list_all_schedules' multi-agent visibility to the caller.
func createWorkflowScheduleExecutors(api *StreamingAPI, currentUserID string) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	cb := api.buildSchedulerCallbacks()

	stringSlice := func(raw interface{}) []string {
		arr, ok := raw.([]interface{})
		if !ok {
			return nil
		}
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}

	return map[string]func(ctx context.Context, args map[string]interface{}) (string, error){
		"list_all_schedules": func(ctx context.Context, args map[string]interface{}) (string, error) {
			enabledOnly, _ := args["enabled_only"].(bool)
			return formatGlobalSchedules(ctx, api, currentUserID, enabledOnly)
		},

		"list_workflow_schedules": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workflowPath, _ := args["workflow_path"].(string)
			if workflowPath == "" {
				return "workflow_path is required.", nil
			}
			return cb.ListSchedules(ctx, workflowPath)
		},

		"create_workflow_schedule": func(ctx context.Context, args map[string]interface{}) (string, error) {
			workflowPath, _ := args["workflow_path"].(string)
			name, _ := args["name"].(string)
			cronExpr, _ := args["cron_expression"].(string)
			if workflowPath == "" || name == "" || cronExpr == "" {
				return "workflow_path, name, and cron_expression are required.", nil
			}
			timezone, _ := args["timezone"].(string)
			groupNames := stringSlice(args["group_names"])
			mode, _ := args["mode"].(string)
			messages := stringSlice(args["messages"])
			workshopMode, _ := args["workshop_mode"].(string)

			if mode == "workshop" && len(messages) == 0 {
				return "messages is required when mode='workshop'.", nil
			}
			if mode != "multi-agent" && len(groupNames) == 0 {
				return "group_names is required for mode='workflow' or 'workshop'. Read variables.json and provide at least one group.", nil
			}
			return cb.CreateSchedule(ctx, workflowPath, name, cronExpr, timezone, groupNames, mode, messages, workshopMode)
		},

		"update_workflow_schedule": func(ctx context.Context, args map[string]interface{}) (string, error) {
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			name, _ := args["name"].(string)
			cronExpr, _ := args["cron_expression"].(string)
			timezone, _ := args["timezone"].(string)
			mode, _ := args["mode"].(string)
			workshopMode, _ := args["workshop_mode"].(string)

			setGroupNames := false
			var groupNames []string
			if raw, ok := args["group_names"]; ok && raw != nil {
				setGroupNames = true
				groupNames = stringSlice(raw)
				if len(groupNames) == 0 {
					return "group_names cannot be empty. Omit the argument to keep the current selection.", nil
				}
			}

			var enabled *bool
			if raw, ok := args["enabled"]; ok && raw != nil {
				if b, ok := raw.(bool); ok {
					enabled = &b
				}
			}

			var messages []string
			if raw, ok := args["messages"]; ok && raw != nil {
				messages = stringSlice(raw)
			}

			return cb.UpdateSchedule(ctx, jobID, name, cronExpr, timezone, groupNames, setGroupNames, enabled, mode, messages, workshopMode)
		},

		"delete_workflow_schedule": func(ctx context.Context, args map[string]interface{}) (string, error) {
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			if err := cb.DeleteSchedule(ctx, jobID); err != nil {
				return "", err
			}
			return "Schedule `" + jobID + "` deleted.", nil
		},

		"trigger_workflow_schedule": func(ctx context.Context, args map[string]interface{}) (string, error) {
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			return cb.TriggerSchedule(ctx, jobID)
		},

		"get_workflow_schedule_runs": func(ctx context.Context, args map[string]interface{}) (string, error) {
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			limit := 0
			if raw, ok := args["limit"]; ok && raw != nil {
				switch v := raw.(type) {
				case float64:
					limit = int(v)
				case int:
					limit = v
				}
			}
			return cb.GetScheduleRuns(ctx, jobID, limit)
		},
	}
}

type globalScheduleEntry struct {
	source       string // "Workflow/<path>" or "user:<id>"
	mode         string
	sched        WorkflowSchedule
	nextRun      *time.Time
	lastStatus   string
	lastRunAt    *time.Time
}

// formatGlobalSchedules aggregates all workflow-manifest schedules and the
// current user's multi-agent schedules, sorts by next run time, and renders a
// compact text view so the chat can reason about cron overlap.
func formatGlobalSchedules(ctx context.Context, api *StreamingAPI, currentUserID string, enabledOnly bool) (string, error) {
	var entries []globalScheduleEntry

	workflows, err := DiscoverWorkflowManifests(ctx)
	if err == nil {
		for _, dw := range workflows {
			for _, sched := range dw.Manifest.Schedules {
				if enabledOnly && !sched.Enabled {
					continue
				}
				entry := globalScheduleEntry{
					source:  dw.WorkspacePath,
					mode:    scheduleModeOrDefault(sched.Mode),
					sched:   sched,
					nextRun: getNextRunTime(sched.CronExpression, sched.Timezone),
				}
				if api.scheduler != nil {
					st := api.scheduler.GetRuntimeState(sched.ID)
					entry.lastStatus = st.LastStatus
					entry.lastRunAt = st.LastRunAt
				}
				entries = append(entries, entry)
			}
		}
	}

	if currentUserID != "" {
		if f, exists, mErr := ReadMultiAgentSchedules(ctx, currentUserID); mErr == nil && exists {
			for _, sched := range f.Schedules {
				if enabledOnly && !sched.Enabled {
					continue
				}
				entry := globalScheduleEntry{
					source:  "user:" + currentUserID,
					mode:    "multi-agent",
					sched:   sched,
					nextRun: getNextRunTime(sched.CronExpression, sched.Timezone),
				}
				if api.scheduler != nil {
					st := api.scheduler.GetRuntimeState(sched.ID)
					entry.lastStatus = st.LastStatus
					entry.lastRunAt = st.LastRunAt
				}
				entries = append(entries, entry)
			}
		}
	}

	if len(entries) == 0 {
		return "No schedules found.", nil
	}

	sort.SliceStable(entries, func(i, j int) bool {
		switch {
		case entries[i].nextRun == nil && entries[j].nextRun == nil:
			return entries[i].sched.ID < entries[j].sched.ID
		case entries[i].nextRun == nil:
			return false
		case entries[j].nextRun == nil:
			return true
		}
		return entries[i].nextRun.Before(*entries[j].nextRun)
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## All schedules (%d) — sorted by next run\n\n", len(entries)))
	sb.WriteString("Use this view to spot overlap before creating new schedules. Times are UTC.\n\n")
	for _, e := range entries {
		status := "disabled"
		if e.sched.Enabled {
			status = "enabled"
		}
		nextRun := "unscheduled"
		if e.nextRun != nil {
			nextRun = e.nextRun.Format(time.RFC3339)
		}
		sb.WriteString(fmt.Sprintf("- **%s** (`%s`) — %s\n", e.sched.Name, e.sched.ID, status))
		sb.WriteString(fmt.Sprintf("  - source: `%s` | mode: `%s`\n", e.source, e.mode))
		sb.WriteString(fmt.Sprintf("  - cron: `%s` (%s) | next: %s\n", e.sched.CronExpression, scheduleTimezoneOrDefault(e.sched.Timezone), nextRun))
		if len(e.sched.GroupNames) > 0 {
			sb.WriteString(fmt.Sprintf("  - groups: %v\n", e.sched.GroupNames))
		}
		if e.lastStatus != "" {
			lastRun := ""
			if e.lastRunAt != nil {
				lastRun = " at " + e.lastRunAt.Format(time.RFC3339)
			}
			sb.WriteString(fmt.Sprintf("  - last run: %s%s\n", e.lastStatus, lastRun))
		}
	}
	return sb.String(), nil
}

func scheduleModeOrDefault(mode string) string {
	if mode == "" {
		return "workflow"
	}
	return mode
}

func scheduleTimezoneOrDefault(tz string) string {
	if tz == "" {
		return "UTC"
	}
	return tz
}
