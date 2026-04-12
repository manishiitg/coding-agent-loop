package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/gorilla/mux"
)

// Workflow running-session API
// ============================
// These endpoints expose the in-memory ActiveWorkflowExecution registry as a
// workflow-owned read/write surface, so the workflow UI doesn't have to read
// chat session metadata to find running workflows or track minimization
// state. Chat and workflow share no persistence; this is how they stay
// separate.

// runningWorkflowBySessionLocked returns the most recent execution for a
// given sessionID. api.activeWorkflowExecutionsMux must already be held.
func (api *StreamingAPI) runningWorkflowBySessionLocked(sessionID string) *ActiveWorkflowExecution {
	var best *ActiveWorkflowExecution
	for _, exec := range api.activeWorkflowExecutions {
		if exec == nil || exec.SessionID != sessionID {
			continue
		}
		if best == nil || exec.StartedAt.After(best.StartedAt) {
			best = exec
		}
	}
	return best
}

// registerRunningWorkflow inserts an execution into the in-memory registry
// and returns it. Intended for the workflow start paths that previously
// called trackActiveSession + wrote workflow_metadata into session.config.
func (api *StreamingAPI) registerRunningWorkflow(exec *ActiveWorkflowExecution) {
	if exec == nil || exec.QueryID == "" {
		return
	}
	if exec.StartedAt.IsZero() {
		exec.StartedAt = time.Now().UTC()
	}
	if exec.Status == "" {
		exec.Status = "running"
	}
	api.activeWorkflowExecutionsMux.Lock()
	api.activeWorkflowExecutions[exec.QueryID] = exec
	api.activeWorkflowExecutionsMux.Unlock()
}

// handleListRunningWorkflows returns all currently-running workflow
// executions for the caller, sorted by StartedAt descending.
// GET /api/workflow/running
func (api *StreamingAPI) handleListRunningWorkflows(w http.ResponseWriter, r *http.Request) {
	userID := GetUserIDFromContext(r.Context())

	api.activeWorkflowExecutionsMux.RLock()
	list := make([]ActiveWorkflowExecution, 0, len(api.activeWorkflowExecutions))
	for _, exec := range api.activeWorkflowExecutions {
		if exec == nil {
			continue
		}
		if userID != "" && exec.UserID != "" && exec.UserID != userID {
			continue
		}
		list = append(list, *exec)
	}
	api.activeWorkflowExecutionsMux.RUnlock()

	sort.Slice(list, func(i, j int) bool {
		return list[i].StartedAt.After(list[j].StartedAt)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"running": list,
	})
}

// handleGetRunningWorkflow returns the single execution mapped to session_id.
// GET /api/workflow/running/{session_id}
func (api *StreamingAPI) handleGetRunningWorkflow(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["session_id"]
	if sessionID == "" {
		http.Error(w, `{"error":"session_id is required"}`, http.StatusBadRequest)
		return
	}

	api.activeWorkflowExecutionsMux.RLock()
	exec := api.runningWorkflowBySessionLocked(sessionID)
	var out ActiveWorkflowExecution
	found := exec != nil
	if found {
		out = *exec
	}
	api.activeWorkflowExecutionsMux.RUnlock()

	if !found {
		http.Error(w, `{"error":"running workflow not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// updateRunningWorkflowRequest patches fields on a running workflow
// execution. Only the fields the frontend minimization flow needs are
// writable; unknown fields are ignored.
type updateRunningWorkflowRequest struct {
	Status           *string `json:"status,omitempty"`
	PhaseID          *string `json:"phase_id,omitempty"`
	PhaseName        *string `json:"phase_name,omitempty"`
	IsMinimized      *bool   `json:"is_minimized,omitempty"`
	MinimizedAt      *int64  `json:"minimized_at,omitempty"`
	CurrentStepID    *string `json:"current_step_id,omitempty"`
	CurrentStepTitle *string `json:"current_step_title,omitempty"`
}

// handleUpdateRunningWorkflow patches an execution in the registry.
// PATCH /api/workflow/running/{session_id}
func (api *StreamingAPI) handleUpdateRunningWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	sessionID := mux.Vars(r)["session_id"]
	if sessionID == "" {
		http.Error(w, `{"error":"session_id is required"}`, http.StatusBadRequest)
		return
	}

	var req updateRunningWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	api.activeWorkflowExecutionsMux.Lock()
	exec := api.runningWorkflowBySessionLocked(sessionID)
	if exec == nil {
		api.activeWorkflowExecutionsMux.Unlock()
		http.Error(w, `{"error":"running workflow not found"}`, http.StatusNotFound)
		return
	}
	if req.Status != nil {
		exec.Status = *req.Status
	}
	if req.PhaseID != nil {
		exec.PhaseID = *req.PhaseID
	}
	if req.PhaseName != nil {
		exec.PhaseName = *req.PhaseName
	}
	if req.IsMinimized != nil {
		exec.IsMinimized = *req.IsMinimized
	}
	if req.MinimizedAt != nil {
		exec.MinimizedAt = *req.MinimizedAt
	}
	if req.CurrentStepID != nil {
		exec.CurrentStepID = *req.CurrentStepID
	}
	if req.CurrentStepTitle != nil {
		exec.CurrentStepTitle = *req.CurrentStepTitle
	}
	out := *exec
	api.activeWorkflowExecutionsMux.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
