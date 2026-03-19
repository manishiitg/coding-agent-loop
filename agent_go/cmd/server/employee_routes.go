package server

import (
	"encoding/json"
	"net/http"

	"mcp-agent-builder-go/agent_go/pkg/database"

	"github.com/gorilla/mux"
)

// EmployeeRoutes sets up employee API routes
func EmployeeRoutes(router *mux.Router, db database.Database) {
	apiRouter := router.PathPrefix("/api/employees").Subrouter()

	apiRouter.HandleFunc("", listEmployeesHandler(db)).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("", createEmployeeHandler(db)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/assign-workflow", assignWorkflowEmployeeHandler(db)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/{id}", getEmployeeHandler(db)).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/{id}", updateEmployeeHandler(db)).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/{id}", deleteEmployeeHandler(db)).Methods("DELETE", "OPTIONS")
}

func listEmployeesHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		employees, err := db.ListEmployees(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"employees": employees,
		})
	}
}

func createEmployeeHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		var emp database.Employee
		if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if emp.Name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		created, err := db.CreateEmployee(r.Context(), &emp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	}
}

func getEmployeeHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		id := mux.Vars(r)["id"]
		emp, err := db.GetEmployee(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if emp == nil {
			http.Error(w, "Employee not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(emp)
	}
}

func updateEmployeeHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		id := mux.Vars(r)["id"]
		var emp database.Employee
		if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		updated, err := db.UpdateEmployee(r.Context(), id, &emp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updated)
	}
}

func deleteEmployeeHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		id := mux.Vars(r)["id"]
		if err := db.DeleteEmployee(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func assignWorkflowEmployeeHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == "OPTIONS" {
			return
		}

		var req struct {
			PresetQueryID string  `json:"preset_query_id"`
			EmployeeID    *string `json:"employee_id"` // null to unassign
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.PresetQueryID == "" {
			http.Error(w, "preset_query_id is required", http.StatusBadRequest)
			return
		}

		sqlDB := db.GetDB()
		var err error
		if req.EmployeeID != nil && *req.EmployeeID != "" {
			_, err = sqlDB.ExecContext(r.Context(),
				`UPDATE preset_queries SET employee_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				*req.EmployeeID, req.PresetQueryID)
		} else {
			_, err = sqlDB.ExecContext(r.Context(),
				`UPDATE preset_queries SET employee_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				req.PresetQueryID)
		}
		if err != nil {
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
