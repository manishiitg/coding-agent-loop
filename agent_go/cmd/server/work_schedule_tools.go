package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/productschedule"
)

// registerWorkScheduleTools exposes the project-scoped subset Work needs:
// recurring one-message jobs. Definitions live in product.json and execution
// reuses ProductScheduleService, so there is no second scheduler.
func (api *StreamingAPI) registerWorkScheduleTools(registrar definitionToolRegistrar, userID, workspacePath string) error {
	if api.productSchedules == nil {
		return nil
	}
	raw, found, err := readFileFromWorkspace(context.Background(), filepath.ToSlash(filepath.Join(workspacePath, "product.json")))
	if err != nil || !found {
		return firstError(err, fmt.Errorf("Work project manifest not found"))
	}
	var manifest productProjectManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return err
	}
	if manifest.Product != "work" || strings.TrimSpace(manifest.ID) == "" {
		return fmt.Errorf("invalid Work project manifest")
	}
	projectID := manifest.ID
	register := func(name, description string, parameters map[string]interface{}, execute func(context.Context, map[string]interface{}) (string, error)) error {
		return registrar.RegisterCustomTool(name, description, parameters, execute, "work_schedule_tools")
	}
	resolveID := func(value string) string {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, projectScheduleJobPrefix) {
			return value
		}
		return projectScheduleJobID("work", projectID, value)
	}
	if err := register("list_project_schedules", "List this Work project's recurring message schedules and their latest run status.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{},
	}, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		jobs, err := api.productSchedules.JobsForUser(ctx, userID)
		if err != nil {
			return "", err
		}
		var out []ScheduledJobResponse
		for _, job := range jobs {
			if job.Profile.ID == "work" && job.ProjectID == projectID {
				runsWorkspace, _ := api.productSchedules.RunsWorkspace(ctx, job)
				out = append(out, api.productSchedules.jobResponse(job, runsWorkspace))
			}
		}
		encoded, err := json.MarshalIndent(map[string]interface{}{"schedules": out}, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}
	if err := register("create_project_schedule", "Create one recurring schedule for this Work project. It sends exactly one message into the project's Builder conversation; it never runs a workflow or route.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":            map[string]interface{}{"type": "string"},
			"message":         map[string]interface{}{"type": "string", "description": "The single instruction Work should perform at each occurrence."},
			"cron_expression": map[string]interface{}{"type": "string", "description": "Standard five-field cron expression."},
			"timezone":        map[string]interface{}{"type": "string", "description": "IANA timezone, for example Asia/Kolkata."},
			"enabled":         map[string]interface{}{"type": "boolean"},
		},
		"required": []string{"name", "message", "cron_expression", "timezone"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		name, _ := args["name"].(string)
		message, _ := args["message"].(string)
		cronExpression, _ := args["cron_expression"].(string)
		timezone, _ := args["timezone"].(string)
		enabled, hasEnabled := args["enabled"].(bool)
		if !hasEnabled {
			enabled = true
		}
		job, err := api.productSchedules.CreateProjectSchedule(ctx, userID, "work", projectID, productschedule.Schedule{
			Name: strings.TrimSpace(name), Messages: []string{strings.TrimSpace(message)}, CronExpression: strings.TrimSpace(cronExpression), Timezone: strings.TrimSpace(timezone), Enabled: enabled,
		})
		if err != nil {
			return "", err
		}
		runsWorkspace, _ := api.productSchedules.RunsWorkspace(ctx, job)
		encoded, err := json.MarshalIndent(api.productSchedules.jobResponse(job, runsWorkspace), "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}
	if err := register("update_project_schedule", "Update a Work project message schedule. Call list_project_schedules first and use its exact id.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string"}, "name": map[string]interface{}{"type": "string"}, "message": map[string]interface{}{"type": "string"},
			"cron_expression": map[string]interface{}{"type": "string"}, "timezone": map[string]interface{}{"type": "string"}, "enabled": map[string]interface{}{"type": "boolean"},
		},
		"required": []string{"id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		id, _ := args["id"].(string)
		job, err := api.productSchedules.UpdateProjectSchedule(ctx, userID, resolveID(id), func(schedule *productschedule.Schedule) {
			if value, ok := args["name"].(string); ok && strings.TrimSpace(value) != "" {
				schedule.Name = strings.TrimSpace(value)
			}
			if value, ok := args["message"].(string); ok && strings.TrimSpace(value) != "" {
				schedule.Messages = []string{strings.TrimSpace(value)}
			}
			if value, ok := args["cron_expression"].(string); ok && strings.TrimSpace(value) != "" {
				schedule.CronExpression = strings.TrimSpace(value)
			}
			if value, ok := args["timezone"].(string); ok && strings.TrimSpace(value) != "" {
				schedule.Timezone = strings.TrimSpace(value)
			}
			if value, ok := args["enabled"].(bool); ok {
				schedule.Enabled = value
			}
		})
		if err != nil {
			return "", err
		}
		runsWorkspace, _ := api.productSchedules.RunsWorkspace(ctx, job)
		encoded, err := json.MarshalIndent(api.productSchedules.jobResponse(job, runsWorkspace), "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}
	if err := register("delete_project_schedule", "Delete a Work project schedule. Call list_project_schedules first and use its exact id.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []string{"id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		id, _ := args["id"].(string)
		if err := api.productSchedules.DeleteProjectSchedule(ctx, userID, resolveID(id)); err != nil {
			return "", err
		}
		return "Schedule deleted.", nil
	}); err != nil {
		return err
	}
	if err := register("list_project_triggers", "List the authenticated webhook triggers stored in this Work project's product.json. Secrets are never returned.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{},
	}, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		triggers, err := api.productSchedules.projectWebhookConfigs(ctx, userID, "work", projectID)
		if err != nil {
			return "", err
		}
		out := make([]productWebhookResponse, 0, len(triggers))
		for _, trigger := range triggers {
			out = append(out, productWebhookDTO(trigger))
		}
		encoded, err := json.MarshalIndent(map[string]interface{}{"triggers": out}, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}
	if err := register("create_project_trigger", "Create an authenticated webhook trigger for this Work project. Each delivery sends the saved message into the Builder chat with a path to its JSON payload. Return the one-time secret to the user immediately.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"}, "message": map[string]interface{}{"type": "string"},
			"auth_mode": map[string]interface{}{"type": "string", "enum": []string{"bearer", "github"}}, "enabled": map[string]interface{}{"type": "boolean"},
		}, "required": []string{"name", "message"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		name, _ := args["name"].(string)
		message, _ := args["message"].(string)
		authMode, _ := args["auth_mode"].(string)
		enabled, ok := args["enabled"].(bool)
		if !ok {
			enabled = true
		}
		response, _, err := api.productSchedules.saveProductWebhookConfig(ctx, userID, productWebhookRequest{ProfileID: "work", ProjectID: projectID, Name: name, Message: message, AuthMode: authMode, Enabled: enabled}, "")
		if err != nil {
			return "", err
		}
		encoded, err := json.MarshalIndent(response, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}
	if err := register("update_project_trigger", "Update, enable, disable, or rotate a Work project webhook trigger. Call list_project_triggers first. A rotated secret is returned only once.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string"}, "name": map[string]interface{}{"type": "string"}, "message": map[string]interface{}{"type": "string"},
			"auth_mode": map[string]interface{}{"type": "string", "enum": []string{"bearer", "github"}}, "enabled": map[string]interface{}{"type": "boolean"}, "rotate_secret": map[string]interface{}{"type": "boolean"},
		}, "required": []string{"id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		id, _ := args["id"].(string)
		triggers, err := api.productSchedules.projectWebhookConfigs(ctx, userID, "work", projectID)
		if err != nil {
			return "", err
		}
		var current *productWebhookTrigger
		for i := range triggers {
			if triggers[i].ID == strings.TrimSpace(id) {
				current = &triggers[i]
				break
			}
		}
		if current == nil {
			return "", fmt.Errorf("trigger not found")
		}
		name, message, authMode, enabled := current.Name, current.Message, current.Webhook.AuthMode, current.Enabled
		if value, ok := args["name"].(string); ok && strings.TrimSpace(value) != "" {
			name = value
		}
		if value, ok := args["message"].(string); ok && strings.TrimSpace(value) != "" {
			message = value
		}
		if value, ok := args["auth_mode"].(string); ok && strings.TrimSpace(value) != "" {
			authMode = value
		}
		if value, ok := args["enabled"].(bool); ok {
			enabled = value
		}
		rotate, _ := args["rotate_secret"].(bool)
		response, _, err := api.productSchedules.saveProductWebhookConfig(ctx, userID, productWebhookRequest{ProfileID: "work", ProjectID: projectID, Name: name, Message: message, AuthMode: authMode, Enabled: enabled, RotateSecret: rotate}, current.ID)
		if err != nil {
			return "", err
		}
		encoded, err := json.MarshalIndent(response, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}
	if err := register("delete_project_trigger", "Delete a Work project webhook trigger. Call list_project_triggers first and use its exact id.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []string{"id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		id, _ := args["id"].(string)
		if err := api.productSchedules.deleteProductWebhookConfig(ctx, userID, "work", projectID, strings.TrimSpace(id)); err != nil {
			return "", err
		}
		return "Trigger deleted.", nil
	}); err != nil {
		return err
	}
	return register("trigger_project_schedule", "Run a Work project schedule now. Call list_project_schedules first and use its exact id.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []string{"id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		id, _ := args["id"].(string)
		sessionID, err := api.productSchedules.Trigger(ctx, userID, resolveID(id))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Schedule started in session %s.", sessionID), nil
	})
}
