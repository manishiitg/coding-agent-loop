package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"mcp-agent/agent_go/pkg/database"
)

// WorkflowRequest represents a workflow creation request
type WorkflowRequest struct {
	PresetQueryID             string `json:"preset_query_id"`
	HumanVerificationRequired bool   `json:"human_verification_required"`
}

// WorkflowExecuteRequest represents a workflow execution request (DEPRECATED - not used anymore)
type WorkflowExecuteRequest struct {
	PresetQueryID string `json:"preset_query_id"`
	Objective     string `json:"objective"`
	HumanResponse string `json:"human_response,omitempty"`
}

// WorkflowUpdateRequest represents a workflow update request
type WorkflowUpdateRequest struct {
	PresetQueryID   string                            `json:"preset_query_id"`
	WorkflowStatus  *string                           `json:"workflow_status,omitempty"`
	SelectedOptions *database.WorkflowSelectedOptions `json:"selected_options,omitempty"`
}

// handleCreateWorkflow handles workflow creation
func (api *StreamingAPI) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req WorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.PresetQueryID == "" {
		http.Error(w, "preset_query_id is required", http.StatusBadRequest)
		return
	}

	// Check if workflow already exists for this preset
	existingWorkflow, err := api.chatDB.GetWorkflowByPresetQueryID(r.Context(), req.PresetQueryID)
	if err != nil && !strings.Contains(err.Error(), "workflow not found for preset query") {
		http.Error(w, fmt.Sprintf("Failed to check existing workflow: %v", err), http.StatusInternalServerError)
		return
	}

	// If workflow already exists, return error
	if existingWorkflow != nil {
		http.Error(w, "Workflow already exists for this preset query ID. Use update endpoint instead.", http.StatusConflict)
		return
	}

	// Create new workflow
	status := database.WorkflowStatusPreVerification
	if !req.HumanVerificationRequired {
		status = database.WorkflowStatusPostVerification
	}
	createReq := &database.CreateWorkflowRequest{
		PresetQueryID:  req.PresetQueryID,
		WorkflowStatus: status,
	}

	workflow, err := api.chatDB.CreateWorkflow(r.Context(), createReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create workflow: %v", err), http.StatusInternalServerError)
		return
	}

	// Return success response
	response := map[string]interface{}{
		"success": true,
		"workflow": map[string]interface{}{
			"id":              workflow.ID,
			"preset_query_id": workflow.PresetQueryID,
			"workflow_status": workflow.WorkflowStatus,
			"created_at":      workflow.CreatedAt,
		},
		"message": "Workflow created successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetWorkflowStatus handles getting workflow status
func (api *StreamingAPI) handleGetWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	presetQueryID := r.URL.Query().Get("preset_query_id")
	if presetQueryID == "" {
		http.Error(w, "preset_query_id parameter is required", http.StatusBadRequest)
		return
	}

	// Get workflow from database
	workflow, err := api.chatDB.GetWorkflowByPresetQueryID(r.Context(), presetQueryID)
	if err != nil {
		if strings.Contains(err.Error(), "workflow not found for preset query") {
			// No workflow exists for this preset
			response := map[string]interface{}{
				"success": true,
				"exists":  false,
				"message": "No workflow exists for this preset",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to get workflow: %v", err), http.StatusInternalServerError)
		return
	}

	// Return workflow status
	response := map[string]interface{}{
		"success": true,
		"exists":  true,
		"workflow": map[string]interface{}{
			"id":               workflow.ID,
			"preset_query_id":  workflow.PresetQueryID,
			"workflow_status":  workflow.WorkflowStatus,
			"selected_options": workflow.SelectedOptions,
			"created_at":       workflow.CreatedAt,
			"updated_at":       workflow.UpdatedAt,
		},
		"status": map[string]interface{}{
			"is_ready":              workflow.WorkflowStatus == database.WorkflowStatusPostVerification,
			"requires_verification": workflow.WorkflowStatus == database.WorkflowStatusPreVerification,
			"can_execute":           workflow.WorkflowStatus == database.WorkflowStatusPostVerification,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleUpdateWorkflow handles workflow updates
func (api *StreamingAPI) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req WorkflowUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.PresetQueryID == "" {
		http.Error(w, "preset_query_id is required", http.StatusBadRequest)
		return
	}

	// Create update request with all provided fields
	updateReq := &database.UpdateWorkflowRequest{}

	if req.WorkflowStatus != nil {
		updateReq.WorkflowStatus = req.WorkflowStatus
	}

	if req.SelectedOptions != nil {
		updateReq.SelectedOptions = req.SelectedOptions
	}

	// Validate that at least one field is provided
	if updateReq.WorkflowStatus == nil && updateReq.SelectedOptions == nil {
		http.Error(w, "at least one field (workflow_status or selected_options) must be provided", http.StatusBadRequest)
		return
	}

	// Update workflow in database
	workflow, err := api.chatDB.UpdateWorkflow(r.Context(), req.PresetQueryID, updateReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update workflow: %v", err), http.StatusInternalServerError)
		return
	}

	// Return success response
	response := map[string]interface{}{
		"success": true,
		"workflow": map[string]interface{}{
			"id":              workflow.ID,
			"preset_query_id": workflow.PresetQueryID,
			"workflow_status": workflow.WorkflowStatus,
			"created_at":      workflow.CreatedAt,
			"updated_at":      workflow.UpdatedAt,
		},
		"message": "Workflow updated successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
