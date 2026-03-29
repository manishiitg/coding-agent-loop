package server

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/database"
	"mcp-agent-builder-go/agent_go/pkg/workspace"

	"github.com/google/uuid"
)

type workflowPresetManifest struct {
	Preset        database.PresetQuery
	WorkspacePath string
	Manifest      *WorkflowManifest
}

func (api *StreamingAPI) listWorkflowPresetsForUser(ctx context.Context, currentUserID string) ([]database.PresetQuery, error) {
	if currentUserID != "" {
		presets, _, err := api.chatDB.ListPresetQueriesWithUser(ctx, 500, 0, currentUserID)
		return presets, err
	}
	presets, _, err := api.chatDB.ListPresetQueries(ctx, 500, 0)
	return presets, err
}

func (api *StreamingAPI) loadWorkflowPresetManifests(ctx context.Context, currentUserID string, presetQueryID string) ([]workflowPresetManifest, error) {
	presets, err := api.listWorkflowPresetsForUser(ctx, currentUserID)
	if err != nil {
		return nil, err
	}

	results := make([]workflowPresetManifest, 0, len(presets))
	for _, preset := range presets {
		if presetQueryID != "" && preset.ID != presetQueryID {
			continue
		}
		if preset.AgentMode != database.AgentModeWorkflow {
			continue
		}
		if !preset.SelectedFolder.Valid || strings.TrimSpace(preset.SelectedFolder.String) == "" {
			continue
		}

		workspacePath := strings.TrimSpace(preset.SelectedFolder.String)
		manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
		if err != nil || !found {
			if presetQueryID != "" {
				return nil, fmt.Errorf("workflow manifest not found at %s", workspacePath)
			}
			continue
		}

		results = append(results, workflowPresetManifest{
			Preset:        preset,
			WorkspacePath: workspacePath,
			Manifest:      manifest,
		})
	}

	if presetQueryID != "" && len(results) == 0 {
		return nil, fmt.Errorf("workflow preset %q not found", presetQueryID)
	}
	return results, nil
}

func (api *StreamingAPI) findWorkflowSchedule(ctx context.Context, currentUserID string, scheduleID string) (*workflowPresetManifest, int, error) {
	contexts, err := api.loadWorkflowPresetManifests(ctx, currentUserID, "")
	if err != nil {
		return nil, -1, err
	}
	for _, item := range contexts {
		for idx := range item.Manifest.Schedules {
			if item.Manifest.Schedules[idx].ID == scheduleID {
				found := item
				return &found, idx, nil
			}
		}
	}
	return nil, -1, fmt.Errorf("schedule %q not found", scheduleID)
}

func (api *StreamingAPI) listOrganizationWorkflowSummaries(ctx context.Context, currentUserID string) ([]map[string]interface{}, error) {
	presets, err := api.listWorkflowPresetsForUser(ctx, currentUserID)
	if err != nil {
		return nil, err
	}

	employees, err := api.chatDB.ListEmployees(ctx)
	if err != nil {
		return nil, err
	}
	employeeNames := make(map[string]string, len(employees))
	for _, emp := range employees {
		employeeNames[emp.ID] = emp.Name
	}

	wsClient := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithUserID(currentUserID),
	)

	workflows := make([]map[string]interface{}, 0, len(presets))
	for _, preset := range presets {
		if preset.AgentMode != database.AgentModeWorkflow {
			continue
		}

		summary := map[string]interface{}{
			"preset_query_id":    preset.ID,
			"label":              preset.Label,
			"employee_id":        nil,
			"employee_name":      nil,
			"workspace_path":     "",
			"schedule_count":     0,
			"latest_run_folder":  nil,
			"latest_output_path": nil,
			"latest_logs_path":   nil,
		}

		if preset.EmployeeID.Valid && preset.EmployeeID.String != "" {
			summary["employee_id"] = preset.EmployeeID.String
			if name := employeeNames[preset.EmployeeID.String]; name != "" {
				summary["employee_name"] = name
			}
		}

		if preset.SelectedFolder.Valid && strings.TrimSpace(preset.SelectedFolder.String) != "" {
			workspacePath := strings.TrimSpace(preset.SelectedFolder.String)
			summary["workspace_path"] = workspacePath

			if manifest, found, err := ReadWorkflowManifest(ctx, workspacePath); err == nil && found {
				summary["schedule_count"] = len(manifest.Schedules)
			}

			latestRunFolder := resolveLatestRunFolder(ctx, workspacePath, wsClient)
			if latestRunFolder != "" {
				summary["latest_run_folder"] = latestRunFolder
				summary["latest_output_path"] = filepath.ToSlash(filepath.Join(workspacePath, "runs", latestRunFolder, "execution"))
				summary["latest_logs_path"] = filepath.ToSlash(filepath.Join(workspacePath, "runs", latestRunFolder, "logs"))
			}
		}

		workflows = append(workflows, summary)
	}

	sort.Slice(workflows, func(i, j int) bool {
		li, _ := workflows[i]["label"].(string)
		lj, _ := workflows[j]["label"].(string)
		return strings.ToLower(li) < strings.ToLower(lj)
	})

	return workflows, nil
}

func (api *StreamingAPI) listOrganizationWorkflowSchedules(ctx context.Context, currentUserID string, presetQueryID string) ([]ScheduledJobResponse, error) {
	contexts, err := api.loadWorkflowPresetManifests(ctx, currentUserID, presetQueryID)
	if err != nil {
		return nil, err
	}

	jobs := make([]ScheduledJobResponse, 0)
	for _, item := range contexts {
		for _, sched := range item.Manifest.Schedules {
			state := ScheduleRuntimeState{}
			if api.scheduler != nil {
				state = api.scheduler.GetRuntimeState(sched.ID)
			}
			jobs = append(jobs, buildJobResponse(item.WorkspacePath, item.Manifest, sched, state))
		}
	}

	sort.Slice(jobs, func(i, j int) bool {
		return strings.ToLower(jobs[i].Name) < strings.ToLower(jobs[j].Name)
	})

	return jobs, nil
}

func (api *StreamingAPI) createOrganizationWorkflowSchedule(ctx context.Context, currentUserID string, presetQueryID string, schedule WorkflowSchedule) (*ScheduledJobResponse, error) {
	contexts, err := api.loadWorkflowPresetManifests(ctx, currentUserID, presetQueryID)
	if err != nil {
		return nil, err
	}
	if len(contexts) == 0 {
		return nil, fmt.Errorf("workflow preset %q not found", presetQueryID)
	}

	item := contexts[0]
	schedule.ID = uuid.New().String()
	item.Manifest.Schedules = append(item.Manifest.Schedules, schedule)

	if err := WriteWorkflowManifest(ctx, item.WorkspacePath, item.Manifest); err != nil {
		return nil, err
	}
	if api.scheduler != nil && schedule.Enabled {
		if err := api.scheduler.LoadSchedule(buildScheduleContext(item.WorkspacePath, item.Manifest, schedule)); err != nil {
			return nil, err
		}
	}

	state := ScheduleRuntimeState{}
	if api.scheduler != nil {
		state = api.scheduler.GetRuntimeState(schedule.ID)
	}
	resp := buildJobResponse(item.WorkspacePath, item.Manifest, schedule, state)
	return &resp, nil
}

func (api *StreamingAPI) updateOrganizationWorkflowSchedule(ctx context.Context, currentUserID string, scheduleID string, update func(*WorkflowSchedule) error) (*ScheduledJobResponse, error) {
	item, idx, err := api.findWorkflowSchedule(ctx, currentUserID, scheduleID)
	if err != nil {
		return nil, err
	}

	if err := update(&item.Manifest.Schedules[idx]); err != nil {
		return nil, err
	}

	if err := WriteWorkflowManifest(ctx, item.WorkspacePath, item.Manifest); err != nil {
		return nil, err
	}
	if api.scheduler != nil {
		if err := api.scheduler.ReloadSchedule(ctx, item.WorkspacePath, scheduleID); err != nil {
			return nil, err
		}
	}

	state := ScheduleRuntimeState{}
	if api.scheduler != nil {
		state = api.scheduler.GetRuntimeState(scheduleID)
	}
	resp := buildJobResponse(item.WorkspacePath, item.Manifest, item.Manifest.Schedules[idx], state)
	return &resp, nil
}

func (api *StreamingAPI) deleteOrganizationWorkflowSchedule(ctx context.Context, currentUserID string, scheduleID string) error {
	item, idx, err := api.findWorkflowSchedule(ctx, currentUserID, scheduleID)
	if err != nil {
		return err
	}

	item.Manifest.Schedules = append(item.Manifest.Schedules[:idx], item.Manifest.Schedules[idx+1:]...)
	if err := WriteWorkflowManifest(ctx, item.WorkspacePath, item.Manifest); err != nil {
		return err
	}
	if api.scheduler != nil {
		if err := api.scheduler.RemoveJob(scheduleID); err != nil {
			return err
		}
	}
	return nil
}
