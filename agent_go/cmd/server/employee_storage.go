package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"mcp-agent-builder-go/agent_go/pkg/fsutil"
)

// EmployeeFile represents a single employee record stored in config/employees.json.
type EmployeeFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Role        string `json:"role,omitempty"`
	Status      string `json:"status,omitempty"`
	AvatarColor string `json:"avatar_color,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

const defaultEmployeeAvatarColor = "#6b7280"

func normalizeEmployeeFile(emp EmployeeFile) EmployeeFile {
	if emp.Role == "" && emp.Description != "" {
		emp.Role = emp.Description
	}
	if emp.Description == "" && emp.Role != "" {
		emp.Description = emp.Role
	}
	if emp.Status == "" {
		emp.Status = "active"
	}
	if emp.AvatarColor == "" {
		emp.AvatarColor = defaultEmployeeAvatarColor
	}
	return emp
}

var (
	employeesMu         sync.Mutex
	employeeWorkflowsMu sync.Mutex
)

func employeesFilePath() string {
	return filepath.Join(getWorkspaceDocsAbsPath(), "config", "employees.json")
}

func employeeWorkflowsFilePath() string {
	return filepath.Join(getWorkspaceDocsAbsPath(), "config", "employee-workflows.json")
}

// readEmployeesFile reads config/employees.json and returns the slice of employees.
// Returns an empty slice if the file does not exist.
func readEmployeesFile() ([]EmployeeFile, error) {
	data, err := os.ReadFile(employeesFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return []EmployeeFile{}, nil
		}
		return nil, err
	}
	var employees []EmployeeFile
	if err := json.Unmarshal(data, &employees); err != nil {
		return nil, err
	}
	return employees, nil
}

// writeEmployeesFile writes the employees slice to config/employees.json.
func writeEmployeesFile(employees []EmployeeFile) error {
	employeesMu.Lock()
	defer employeesMu.Unlock()
	return fsutil.WriteJSONAtomic(employeesFilePath(), employees, 0644)
}

// readEmployeeWorkflowsFile reads config/employee-workflows.json and returns
// a map of workflow_path → employee_id. Returns an empty map if the file does not exist.
func readEmployeeWorkflowsFile() (map[string]string, error) {
	data, err := os.ReadFile(employeeWorkflowsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var assignments map[string]string
	if err := json.Unmarshal(data, &assignments); err != nil {
		return nil, err
	}
	return assignments, nil
}

// writeEmployeeWorkflowsFile writes the workflow_path → employee_id map to
// config/employee-workflows.json.
func writeEmployeeWorkflowsFile(assignments map[string]string) error {
	employeeWorkflowsMu.Lock()
	defer employeeWorkflowsMu.Unlock()
	return fsutil.WriteJSONAtomic(employeeWorkflowsFilePath(), assignments, 0644)
}
