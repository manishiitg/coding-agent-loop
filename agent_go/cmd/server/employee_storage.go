package server

import (
	"context"
	"encoding/json"
	"sync"
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
	emp.Role = ""
	emp.Description = ""
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
	return "config/employees.json"
}

func employeeWorkflowsFilePath() string {
	return "config/employee-workflows.json"
}

// readEmployeesFile reads config/employees.json and returns the slice of employees.
// Returns an empty slice if the file does not exist.
func readEmployeesFile() ([]EmployeeFile, error) {
	data, exists, err := readFileFromWorkspace(context.Background(), employeesFilePath())
	if err != nil {
		return nil, err
	}
	if !exists {
		return []EmployeeFile{}, nil
	}
	var employees []EmployeeFile
	if err := json.Unmarshal([]byte(data), &employees); err != nil {
		return nil, err
	}
	return employees, nil
}

// writeEmployeesFile writes the employees slice to config/employees.json.
func writeEmployeesFile(employees []EmployeeFile) error {
	employeesMu.Lock()
	defer employeesMu.Unlock()
	sanitized := make([]EmployeeFile, len(employees))
	for i, emp := range employees {
		emp.Role = ""
		emp.Description = ""
		sanitized[i] = emp
	}
	data, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return err
	}
	return writeFileToWorkspace(context.Background(), employeesFilePath(), string(data))
}

// readEmployeeWorkflowsFile reads config/employee-workflows.json and returns
// a map of workflow_path → employee_id. Returns an empty map if the file does not exist.
func readEmployeeWorkflowsFile() (map[string]string, error) {
	data, exists, err := readFileFromWorkspace(context.Background(), employeeWorkflowsFilePath())
	if err != nil {
		return nil, err
	}
	if !exists {
		return map[string]string{}, nil
	}
	var assignments map[string]string
	if err := json.Unmarshal([]byte(data), &assignments); err != nil {
		return nil, err
	}
	return assignments, nil
}

// writeEmployeeWorkflowsFile writes the workflow_path → employee_id map to
// config/employee-workflows.json.
func writeEmployeeWorkflowsFile(assignments map[string]string) error {
	employeeWorkflowsMu.Lock()
	defer employeeWorkflowsMu.Unlock()
	data, err := json.MarshalIndent(assignments, "", "  ")
	if err != nil {
		return err
	}
	return writeFileToWorkspace(context.Background(), employeeWorkflowsFilePath(), string(data))
}
