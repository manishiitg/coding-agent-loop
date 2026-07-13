package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
)

type activityStatusResponse struct {
	GeneratedAt       string                    `json:"generated_at"`
	RunningWorkflows  []activityRunningWorkflow `json:"running_workflows"`
	RunningSchedules  []activityRunningSchedule `json:"running_schedules"`
	WorkflowCount     int                       `json:"workflow_count"`
	ScheduleCount     int                       `json:"schedule_count"`
	IncludesSchedules []string                  `json:"includes_schedules"`
}

type activityRunningWorkflow struct {
	SessionID        string `json:"session_id"`
	QueryID          string `json:"query_id"`
	WorkspacePath    string `json:"workspace_path"`
	PresetName       string `json:"preset_name,omitempty"`
	Title            string `json:"title,omitempty"`
	Status           string `json:"status"`
	TriggeredBy      string `json:"triggered_by,omitempty"`
	StartedAt        string `json:"started_at"`
	CurrentStepID    string `json:"current_step_id,omitempty"`
	CurrentStepTitle string `json:"current_step_title,omitempty"`
}

type activityRunningSchedule struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	EntityType     string   `json:"entity_type"`
	Source         string   `json:"source"`
	WorkflowLabel  string   `json:"workflow_label,omitempty"`
	WorkspacePath  string   `json:"workspace_path,omitempty"`
	UserID         string   `json:"user_id,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	WorkshopMode   string   `json:"workshop_mode,omitempty"`
	GroupNames     []string `json:"group_names,omitempty"`
	SessionID      string   `json:"session_id,omitempty"`
	LastRunAt      string   `json:"last_run_at,omitempty"`
	NextRunAt      string   `json:"next_run_at,omitempty"`
	CronExpression string   `json:"cron_expression,omitempty"`
	Timezone       string   `json:"timezone,omitempty"`
	Query          string   `json:"query,omitempty"`
}

// registerActivityStatusTool gives multi-agent chat a read-only live status view.
// It mirrors the UI header's running workflow source and the scheduler runtime state.
func (api *StreamingAPI) registerActivityStatusTool(underlyingAgent *mcpagent.Agent, currentUserID string) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	params := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
	description := "Return a JSON snapshot of currently running workflow executions and currently running schedules. Use this when the user asks what workflows, background runs, cron jobs, or multi-agent schedules are running right now."

	return underlyingAgent.RegisterCustomTool(
		"get_activity_status",
		description,
		params,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return api.handleActivityStatusTool(ctx, currentUserID)
		},
		"activity_status",
	)
}

func (api *StreamingAPI) handleActivityStatusTool(ctx context.Context, currentUserID string) (string, error) {
	workflows := api.listRunningWorkflowExecutions(currentUserID)
	runningWorkflows := make([]activityRunningWorkflow, 0, len(workflows))
	for _, wf := range workflows {
		runningWorkflows = append(runningWorkflows, activityRunningWorkflow{
			SessionID:        wf.SessionID,
			QueryID:          wf.QueryID,
			WorkspacePath:    wf.WorkspacePath,
			PresetName:       wf.PresetName,
			Title:            wf.Title,
			Status:           wf.Status,
			TriggeredBy:      wf.TriggeredBy,
			StartedAt:        formatActivityTime(wf.StartedAt),
			CurrentStepID:    wf.CurrentStepID,
			CurrentStepTitle: wf.CurrentStepTitle,
		})
	}

	runningSchedules, err := api.listRunningScheduleActivities(ctx, currentUserID)
	if err != nil {
		return "", err
	}

	resp := activityStatusResponse{
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		RunningWorkflows:  runningWorkflows,
		RunningSchedules:  runningSchedules,
		WorkflowCount:     len(runningWorkflows),
		ScheduleCount:     len(runningSchedules),
		IncludesSchedules: []string{"workflow schedules", "current user's multi-agent schedules"},
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (api *StreamingAPI) listRunningScheduleActivities(ctx context.Context, currentUserID string) ([]activityRunningSchedule, error) {
	if api.scheduler == nil {
		return []activityRunningSchedule{}, nil
	}

	var out []activityRunningSchedule
	workflows, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workflow schedules: %w", err)
	}
	for _, wf := range workflows {
		if wf.Manifest == nil {
			continue
		}
		for _, sched := range wf.Manifest.Schedules {
			state := api.scheduler.GetRuntimeStateForWorkflow(wf.WorkspacePath, sched.ID)
			if !strings.EqualFold(state.LastStatus, "running") {
				continue
			}
			out = append(out, activityRunningSchedule{
				ID:             sched.ID,
				Name:           sched.Name,
				EntityType:     "workflow",
				Source:         wf.WorkspacePath,
				WorkflowLabel:  wf.Manifest.Label,
				WorkspacePath:  wf.WorkspacePath,
				Mode:           "workshop",
				WorkshopMode:   sched.WorkshopMode,
				GroupNames:     sched.GroupNames,
				SessionID:      state.LastSessionID,
				LastRunAt:      formatActivityPtrTime(state.LastRunAt),
				NextRunAt:      formatActivityPtrTime(state.NextRunAt),
				CronExpression: sched.CronExpression,
				Timezone:       scheduleTimezoneOrDefault(sched.Timezone),
			})
		}
	}

	if currentUserID != "" {
		if f, exists, err := ReadMultiAgentSchedules(ctx, currentUserID); err != nil {
			return nil, fmt.Errorf("failed to read multi-agent schedules: %w", err)
		} else if exists && f != nil {
			for _, sched := range MergeBuiltinSchedules(f.Schedules) {
				state := api.scheduler.GetRuntimeStateForUser(currentUserID, sched.ID)
				if !strings.EqualFold(state.LastStatus, "running") {
					continue
				}
				out = append(out, activityRunningSchedule{
					ID:             sched.ID,
					Name:           sched.Name,
					EntityType:     "multi-agent",
					Source:         "user:" + currentUserID,
					WorkspacePath:  "_users/" + currentUserID,
					UserID:         currentUserID,
					Mode:           "multi-agent",
					SessionID:      state.LastSessionID,
					LastRunAt:      formatActivityPtrTime(state.LastRunAt),
					NextRunAt:      formatActivityPtrTime(state.NextRunAt),
					CronExpression: sched.CronExpression,
					Timezone:       scheduleTimezoneOrDefault(sched.Timezone),
					Query:          sched.Query,
				})
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastRunAt > out[j].LastRunAt
	})
	return out, nil
}

func formatActivityTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatActivityPtrTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
