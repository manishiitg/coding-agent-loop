package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	mcpagent "github.com/manishiitg/mcpagent/agent"
)

func (api *StreamingAPI) registerEmployeeManagementTools(underlyingAgent *mcpagent.Agent) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	tools := []struct {
		name        string
		description string
		params      map[string]interface{}
		exec        func(context.Context, map[string]interface{}) (string, error)
	}{
		{
			name:        "list_employees",
			description: "List the org employee registry and each employee's assigned workflow paths. Use this before changing employees when the user asks about org employees or workflow ownership.",
			params: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			exec: api.handleListEmployeesTool,
		},
		{
			name:        "create_employee",
			description: "Create a new org employee in the workspace employee registry. Use this when the user asks to add or create an employee from multi-agent chat. Name is the only required employee field. Do not use save_memory for org employee changes.",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Employee name.",
					},
					"avatar_color": map[string]interface{}{
						"type":        "string",
						"description": "Optional CSS hex color for the employee avatar.",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Optional status, defaults to active.",
					},
				},
				"required": []string{"name"},
			},
			exec: api.handleCreateEmployeeTool,
		},
		{
			name:        "update_employee",
			description: "Update an existing org employee's name, status, or avatar color in the workspace employee registry.",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Employee id from list_employees.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Optional new human name.",
					},
					"avatar_color": map[string]interface{}{
						"type":        "string",
						"description": "Optional CSS hex color.",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Optional employee status.",
					},
				},
				"required": []string{"id"},
			},
			exec: api.handleUpdateEmployeeTool,
		},
		{
			name:        "delete_employee",
			description: "Delete an org employee and remove their workflow assignments. Use only when the user explicitly asks to remove/delete an employee.",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Employee id from list_employees.",
					},
				},
				"required": []string{"id"},
			},
			exec: api.handleDeleteEmployeeTool,
		},
		{
			name:        "assign_workflow_employee",
			description: "Assign or unassign a workflow to an org employee so the org page reflects ownership.",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"workspace_path": map[string]interface{}{
						"type":        "string",
						"description": "Workflow path, for example Workflow/cost-reports.",
					},
					"employee_id": map[string]interface{}{
						"type":        "string",
						"description": "Employee id to assign. Omit or use an empty string to unassign.",
					},
				},
				"required": []string{"workspace_path"},
			},
			exec: api.handleAssignWorkflowEmployeeTool,
		},
	}

	for _, tool := range tools {
		if err := underlyingAgent.RegisterCustomTool(tool.name, tool.description, tool.params, tool.exec, "employee_tools"); err != nil {
			return fmt.Errorf("register %s: %w", tool.name, err)
		}
	}
	return nil
}

func (api *StreamingAPI) handleListEmployeesTool(ctx context.Context, args map[string]interface{}) (string, error) {
	employees, err := readEmployeesFile()
	if err != nil {
		return "", err
	}
	assignments, err := readEmployeeWorkflowsFile()
	if err != nil {
		return "", err
	}

	type employeeWithWorkflows struct {
		EmployeeFile
		Workflows []string `json:"workflows"`
	}
	byEmployee := map[string][]string{}
	for workflowPath, employeeID := range assignments {
		byEmployee[employeeID] = append(byEmployee[employeeID], workflowPath)
	}
	result := make([]employeeWithWorkflows, 0, len(employees))
	for _, employee := range employees {
		normalized := normalizeEmployeeFile(employee)
		result = append(result, employeeWithWorkflows{
			EmployeeFile: normalized,
			Workflows:    byEmployee[normalized.ID],
		})
	}
	return marshalEmployeeToolResult(map[string]interface{}{"employees": result})
}

func (api *StreamingAPI) handleCreateEmployeeTool(ctx context.Context, args map[string]interface{}) (string, error) {
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		return "", fmt.Errorf("name is required")
	}

	employees, err := readEmployeesFile()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	employee := normalizeEmployeeFile(EmployeeFile{
		ID:          uuid.New().String(),
		Name:        name,
		Status:      strings.TrimSpace(stringArg(args, "status")),
		AvatarColor: strings.TrimSpace(stringArg(args, "avatar_color")),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	employees = append(employees, employee)
	if err := writeEmployeesFile(employees); err != nil {
		return "", err
	}
	return marshalEmployeeToolResult(map[string]interface{}{"employee": employee})
}

func (api *StreamingAPI) handleUpdateEmployeeTool(ctx context.Context, args map[string]interface{}) (string, error) {
	id := strings.TrimSpace(stringArg(args, "id"))
	if id == "" {
		return "", fmt.Errorf("id is required")
	}

	employees, err := readEmployeesFile()
	if err != nil {
		return "", err
	}
	for i := range employees {
		if employees[i].ID != id {
			continue
		}
		if value, ok := optionalStringArg(args, "name"); ok {
			employees[i].Name = strings.TrimSpace(value)
		}
		if value, ok := optionalStringArg(args, "avatar_color"); ok {
			employees[i].AvatarColor = strings.TrimSpace(value)
		}
		if value, ok := optionalStringArg(args, "status"); ok {
			employees[i].Status = strings.TrimSpace(value)
		}
		employees[i].Role = ""
		employees[i].Description = ""
		employees[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		employees[i] = normalizeEmployeeFile(employees[i])
		if err := writeEmployeesFile(employees); err != nil {
			return "", err
		}
		return marshalEmployeeToolResult(map[string]interface{}{"employee": employees[i]})
	}
	return "", fmt.Errorf("employee not found: %s", id)
}

func (api *StreamingAPI) handleDeleteEmployeeTool(ctx context.Context, args map[string]interface{}) (string, error) {
	id := strings.TrimSpace(stringArg(args, "id"))
	if id == "" {
		return "", fmt.Errorf("id is required")
	}

	employees, err := readEmployeesFile()
	if err != nil {
		return "", err
	}
	updated := make([]EmployeeFile, 0, len(employees))
	found := false
	for _, employee := range employees {
		if employee.ID == id {
			found = true
			continue
		}
		updated = append(updated, employee)
	}
	if !found {
		return "", fmt.Errorf("employee not found: %s", id)
	}
	if err := writeEmployeesFile(updated); err != nil {
		return "", err
	}

	assignments, err := readEmployeeWorkflowsFile()
	if err == nil {
		changed := false
		for workflowPath, employeeID := range assignments {
			if employeeID == id {
				delete(assignments, workflowPath)
				changed = true
			}
		}
		if changed {
			if err := writeEmployeeWorkflowsFile(assignments); err != nil {
				return "", err
			}
		}
	}

	return marshalEmployeeToolResult(map[string]interface{}{"deleted": true, "id": id})
}

func (api *StreamingAPI) handleAssignWorkflowEmployeeTool(ctx context.Context, args map[string]interface{}) (string, error) {
	workspacePath := strings.TrimSpace(stringArg(args, "workspace_path"))
	if workspacePath == "" {
		return "", fmt.Errorf("workspace_path is required")
	}

	assignments, err := readEmployeeWorkflowsFile()
	if err != nil {
		return "", err
	}
	employeeID := strings.TrimSpace(stringArg(args, "employee_id"))
	if employeeID == "" {
		delete(assignments, workspacePath)
	} else {
		if err := ensureEmployeeExists(employeeID); err != nil {
			return "", err
		}
		assignments[workspacePath] = employeeID
	}
	if err := writeEmployeeWorkflowsFile(assignments); err != nil {
		return "", err
	}
	return marshalEmployeeToolResult(map[string]interface{}{
		"workspace_path": workspacePath,
		"employee_id":    employeeID,
		"assigned":       employeeID != "",
	})
}

func ensureEmployeeExists(employeeID string) error {
	employees, err := readEmployeesFile()
	if err != nil {
		return err
	}
	for _, employee := range employees {
		if employee.ID == employeeID {
			return nil
		}
	}
	return fmt.Errorf("employee not found: %s", employeeID)
}

func stringArg(args map[string]interface{}, key string) string {
	value, _ := args[key].(string)
	return value
}

func optionalStringArg(args map[string]interface{}, key string) (string, bool) {
	value, ok := args[key]
	if !ok || value == nil {
		return "", false
	}
	s, ok := value.(string)
	return s, ok
}

func marshalEmployeeToolResult(value interface{}) (string, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}
