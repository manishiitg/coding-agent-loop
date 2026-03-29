package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func EmployeeRoutes(router *mux.Router) {
	apiRouter := router.PathPrefix("/employees").Subrouter()

	apiRouter.HandleFunc("", listEmployeesHandler()).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("", createEmployeeHandler()).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/assign-workflow", assignWorkflowEmployeeHandler()).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/{id}", getEmployeeHandler()).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/{id}", updateEmployeeHandler()).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/{id}", deleteEmployeeHandler()).Methods("DELETE", "OPTIONS")
}

func listEmployeesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		employees, err := readEmployeesFile()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		assignments, _ := readEmployeeWorkflowsFile()
		// Invert map once: employee_id → []workflow_path (O(m) instead of O(n*m))
		byEmployee := make(map[string][]string, len(assignments))
		for wfPath, empID := range assignments {
			byEmployee[empID] = append(byEmployee[empID], wfPath)
		}
		type employeeWithWorkflows struct {
			EmployeeFile
			WorkflowCount int      `json:"workflow_count"`
			Workflows     []string `json:"workflows"`
		}
		result := make([]employeeWithWorkflows, len(employees))
		for i, emp := range employees {
			normalized := normalizeEmployeeFile(emp)
			wfs := byEmployee[normalized.ID]
			result[i] = employeeWithWorkflows{
				EmployeeFile:  normalized,
				WorkflowCount: len(wfs),
				Workflows:     wfs,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"employees": result,
		})
	}
}

func createEmployeeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		var req struct {
			Name        string `json:"name"`
			Role        string `json:"role"`
			Status      string `json:"status"`
			AvatarColor string `json:"avatar_color"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		employees, err := readEmployeesFile()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)
		emp := normalizeEmployeeFile(EmployeeFile{
			ID:          uuid.New().String(),
			Name:        req.Name,
			Role:        req.Role,
			Status:      req.Status,
			AvatarColor: req.AvatarColor,
			Description: req.Description,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		employees = append(employees, emp)

		if err := writeEmployeesFile(employees); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(normalizeEmployeeFile(emp))
	}
}

func getEmployeeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		id := mux.Vars(r)["id"]
		employees, err := readEmployeesFile()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, emp := range employees {
			if emp.ID == id {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(normalizeEmployeeFile(emp))
				return
			}
		}
		http.Error(w, "Employee not found", http.StatusNotFound)
	}
}

func updateEmployeeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		id := mux.Vars(r)["id"]
		var req struct {
			Name        *string `json:"name"`
			Role        *string `json:"role"`
			Status      *string `json:"status"`
			AvatarColor *string `json:"avatar_color"`
			Description *string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		employees, err := readEmployeesFile()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		found := false
		for i := range employees {
			if employees[i].ID == id {
				if req.Name != nil {
					employees[i].Name = *req.Name
				}
				if req.Role != nil {
					employees[i].Role = *req.Role
					if req.Description == nil {
						employees[i].Description = *req.Role
					}
				}
				if req.Status != nil {
					employees[i].Status = *req.Status
				}
				if req.AvatarColor != nil {
					employees[i].AvatarColor = *req.AvatarColor
				}
				if req.Description != nil {
					employees[i].Description = *req.Description
					if req.Role == nil {
						employees[i].Role = *req.Description
					}
				}
				employees[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				found = true

				if err := writeEmployeesFile(employees); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(normalizeEmployeeFile(employees[i]))
				return
			}
		}
		if !found {
			http.Error(w, "Employee not found", http.StatusNotFound)
		}
	}
}

func deleteEmployeeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		id := mux.Vars(r)["id"]

		employees, err := readEmployeesFile()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		found := false
		var updated []EmployeeFile
		for _, emp := range employees {
			if emp.ID == id {
				found = true
				continue
			}
			updated = append(updated, emp)
		}
		if !found {
			http.Error(w, "Employee not found", http.StatusNotFound)
			return
		}
		if err := writeEmployeesFile(updated); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		assignments, _ := readEmployeeWorkflowsFile()
		changed := false
		for wfPath, empID := range assignments {
			if empID == id {
				delete(assignments, wfPath)
				changed = true
			}
		}
		if changed {
			_ = writeEmployeeWorkflowsFile(assignments)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func assignWorkflowEmployeeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		var req struct {
			WorkspacePath string  `json:"workspace_path"`
			EmployeeID    *string `json:"employee_id"` // null to unassign
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.WorkspacePath == "" {
			http.Error(w, "workspace_path is required", http.StatusBadRequest)
			return
		}

		assignments, err := readEmployeeWorkflowsFile()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if req.EmployeeID != nil && *req.EmployeeID != "" {
			assignments[req.WorkspacePath] = *req.EmployeeID
		} else {
			delete(assignments, req.WorkspacePath)
		}

		if err := writeEmployeeWorkflowsFile(assignments); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	}
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
