package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// --- List / Discovery ---

// handleListWorkflowManifests returns all workflows discovered from workspace manifests.
func (api *StreamingAPI) handleListWorkflowManifests(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	discovered, err := DiscoverWorkflowManifests(r.Context())
	if err != nil {
		log.Printf("[MANIFEST] Error discovering workflows: %v", err)
		http.Error(w, fmt.Sprintf("Failed to discover workflows: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"workflows": discovered,
		"total":     len(discovered),
	})
}

// --- Get single manifest ---

// handleGetWorkflowManifest returns the manifest for a specific workspace.
func (api *StreamingAPI) handleGetWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	manifest, exists, err := ReadWorkflowManifest(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at this workspace", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"manifest":       manifest,
		"workspace_path": workspacePath,
	})
}

// --- Create workflow with manifest ---

type CreateWorkflowManifestRequest struct {
	Label                     string                     `json:"label"`
	WorkspacePath             string                     `json:"workspace_path"`
	Capabilities              *WorkflowCapabilities      `json:"capabilities,omitempty"`
	ExecutionDefaults         *WorkflowExecutionDefaults `json:"execution_defaults,omitempty"`
	HumanVerificationRequired bool                       `json:"human_verification_required"`
}

func (api *StreamingAPI) handleCreateWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req CreateWorkflowManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}

	// Check if manifest already exists
	_, exists, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check existing manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, "workflow.json already exists at this workspace path", http.StatusConflict)
		return
	}

	// Build manifest
	manifest := NewWorkflowManifest(req.Label)
	if req.Capabilities != nil {
		manifest.Capabilities = *req.Capabilities
	}
	if req.ExecutionDefaults != nil {
		manifest.ExecutionDefs = *req.ExecutionDefaults
	}

	// Write manifest
	if err := WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write manifest: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"manifest":       manifest,
		"workspace_path": req.WorkspacePath,
	})
}

// --- Update manifest ---

type UpdateWorkflowManifestRequest struct {
	WorkspacePath     string                     `json:"workspace_path"`
	Label             *string                    `json:"label,omitempty"`
	Capabilities      *WorkflowCapabilities      `json:"capabilities,omitempty"`
	ExecutionDefaults *WorkflowExecutionDefaults `json:"execution_defaults,omitempty"`
	Ownership         *WorkflowOwnership         `json:"ownership,omitempty"`
	Schedules         *[]WorkflowSchedule        `json:"schedules,omitempty"`
	WorkshopMode      *string                    `json:"workshop_mode,omitempty"` // Standalone patch — avoids zeroing out other ExecutionDefaults fields
	RunRetentionCount *int                       `json:"run_retention_count,omitempty"`
	PostRunMonitor    *bool                      `json:"post_run_monitor,omitempty"` // Opt-in to the post-run monitor pass
}

func (api *StreamingAPI) handleUpdateWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req UpdateWorkflowManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}

	// Read existing manifest
	manifest, exists, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at this workspace path", http.StatusNotFound)
		return
	}

	// Apply partial updates
	if req.Label != nil {
		manifest.Label = *req.Label
	}
	if req.Capabilities != nil {
		manifest.Capabilities = *req.Capabilities
	}
	if req.ExecutionDefaults != nil {
		manifest.ExecutionDefs = *req.ExecutionDefaults
	}
	if req.Ownership != nil {
		manifest.Ownership = *req.Ownership
	}
	if req.Schedules != nil {
		manifest.Schedules = *req.Schedules
	}
	if req.WorkshopMode != nil {
		manifest.ExecutionDefs.WorkshopMode = *req.WorkshopMode
	}
	if req.RunRetentionCount != nil {
		manifest.RunRetentionCount = req.RunRetentionCount
	}
	if req.PostRunMonitor != nil {
		manifest.PostRunMonitor = req.PostRunMonitor
	}

	// Write updated manifest
	if err := WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write manifest: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"manifest":       manifest,
		"workspace_path": req.WorkspacePath,
	})
}

// --- Delete manifest ---

func (api *StreamingAPI) handleDeleteWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	// Check that manifest exists first
	manifest, exists, err := ReadWorkflowManifest(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at this workspace path", http.StatusNotFound)
		return
	}

	// Delete workflow.json
	if err := deleteWorkspaceFile(r.Context(), manifestPath(workspacePath)); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete manifest: %v", err), http.StatusInternalServerError)
		return
	}

	// Clean up in-memory runtime state
	deleteWorkflowRuntime(manifest.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Deleted workflow manifest for %s", workspacePath),
	})
}

// --- Delete workflow folder ---

type DeleteWorkflowFolderRequest struct {
	WorkspacePath string `json:"workspace_path"`
}

func (api *StreamingAPI) handleDeleteWorkflowFolder(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace_path"))
	if workspacePath == "" {
		var req DeleteWorkflowFolderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			workspacePath = strings.TrimSpace(req.WorkspacePath)
		}
	}
	if workspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}

	manifest, exists, err := ReadWorkflowManifest(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at this workspace path", http.StatusNotFound)
		return
	}

	if err := deleteWorkspaceFolder(r.Context(), workspacePath); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete workflow folder: %v", err), http.StatusInternalServerError)
		return
	}

	deleteWorkflowRuntime(manifest.ID)

	api.workflowOrchestratorContextMux.Lock()
	if cancelFunc, ok := api.workflowOrchestratorContexts[manifest.ID]; ok {
		cancelFunc()
		delete(api.workflowOrchestratorContexts, manifest.ID)
	}
	api.workflowOrchestratorContextMux.Unlock()

	api.workflowStepIDMux.Lock()
	delete(api.workflowStepIDs, manifest.ID)
	api.workflowStepIDMux.Unlock()

	api.workflowObjectiveMux.Lock()
	delete(api.workflowObjectives, manifest.ID)
	api.workflowObjectiveMux.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"workspace_path": workspacePath,
		"message":        fmt.Sprintf("Deleted workflow folder %s", workspacePath),
	})
}

// --- Duplicate workflow ---

type DuplicateWorkflowManifestRequest struct {
	SourceWorkspacePath string `json:"source_workspace_path"`
	TargetWorkspacePath string `json:"target_workspace_path"`
	NewLabel            string `json:"new_label,omitempty"`
}

func (api *StreamingAPI) handleDuplicateWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req DuplicateWorkflowManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.SourceWorkspacePath == "" || req.TargetWorkspacePath == "" {
		http.Error(w, "source_workspace_path and target_workspace_path are required", http.StatusBadRequest)
		return
	}

	// Read source manifest
	srcManifest, exists, err := ReadWorkflowManifest(r.Context(), req.SourceWorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read source manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at source workspace path", http.StatusNotFound)
		return
	}

	// Check target doesn't already have a manifest
	_, targetExists, err := ReadWorkflowManifest(r.Context(), req.TargetWorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check target: %v", err), http.StatusInternalServerError)
		return
	}
	if targetExists {
		http.Error(w, "workflow.json already exists at target workspace path", http.StatusConflict)
		return
	}

	// Deep-copy and assign new identity
	newManifest := *srcManifest
	newManifest.ID = "wf_" + uuid.New().String()[:8]
	if req.NewLabel != "" {
		newManifest.Label = req.NewLabel
	} else {
		newManifest.Label = srcManifest.Label + " (copy)"
	}
	newManifest.CreatedAt = "" // Will be set by WriteWorkflowManifest
	newManifest.UpdatedAt = ""

	// Reset schedule IDs to avoid collisions
	for i := range newManifest.Schedules {
		newManifest.Schedules[i].ID = uuid.New().String()[:8]
	}

	// Write new manifest
	if err := WriteWorkflowManifest(r.Context(), req.TargetWorkspacePath, &newManifest); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write target manifest: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"manifest":       newManifest,
		"workspace_path": req.TargetWorkspacePath,
	})
}

// --- Resolve manifest for execution ---

// ResolveWorkflowManifest loads a manifest for a given workspace path.
func (api *StreamingAPI) ResolveWorkflowManifest(ctx context.Context, workspacePath string, presetQueryID string) (*WorkflowManifest, error) {
	if workspacePath != "" {
		manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
		if err == nil && exists {
			return manifest, nil
		}
		if err != nil {
			log.Printf("[MANIFEST] Warning: error reading manifest from %s: %v", workspacePath, err)
		}
	}

	return nil, fmt.Errorf("no workflow.json manifest found at workspace path %q", workspacePath)
}

// --- Helper: resolve workspace path from preset/workflow ID ---

func (api *StreamingAPI) resolveWorkspacePathFromPreset(ctx context.Context, presetQueryID string) (string, error) {
	if presetQueryID == "" {
		return "", fmt.Errorf("preset_query_id is empty")
	}

	// Look up workflow manifest by ID (file-backed, no DB dependency)
	workflows, err := DiscoverWorkflowManifests(ctx)
	if err == nil {
		for _, wf := range workflows {
			if wf.Manifest.ID == presetQueryID {
				return wf.WorkspacePath, nil
			}
		}
	}

	return "", fmt.Errorf("workflow %s not found in discovered manifests", presetQueryID)
}

// --- Helper: check if a setCORS-like helper already exists ---
// The existing workflow handlers use inline CORS headers. This centralizes it.
// For backward compatibility we keep inline CORS in existing handlers untouched.

// LoadManifestForExecution loads workflow defaults from manifest for use in execution bootstrap.
// Returns parsed capabilities that can be applied to the agent request.
func LoadManifestForExecution(ctx context.Context, workspacePath string) (*WorkflowCapabilities, bool, error) {
	manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !exists {
		return nil, false, err
	}
	return &manifest.Capabilities, true, nil
}
