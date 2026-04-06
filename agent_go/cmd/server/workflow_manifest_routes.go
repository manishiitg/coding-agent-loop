package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"mcp-agent-builder-go/agent_go/pkg/database"

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

// --- Migration endpoint ---

// handleMigrateWorkflowsToManifests reads workflow-mode presets and writes workflow.json for each.
func (api *StreamingAPI) handleMigrateWorkflowsToManifests(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	overwrite := r.URL.Query().Get("overwrite") == "true"

	// Load all presets
	presets, _, err := api.chatDB.ListPresetQueries(r.Context(), 1000, 0)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list presets: %v", err), http.StatusInternalServerError)
		return
	}

	type migrationResult struct {
		PresetID      string `json:"preset_id"`
		Label         string `json:"label"`
		WorkspacePath string `json:"workspace_path"`
		Status        string `json:"status"` // "migrated", "skipped", "error"
		Error         string `json:"error,omitempty"`
	}

	var results []migrationResult

	for _, preset := range presets {
		if preset.AgentMode != database.AgentModeWorkflow {
			continue // Skip non-workflow presets
		}

		if !preset.SelectedFolder.Valid || preset.SelectedFolder.String == "" {
			results = append(results, migrationResult{
				PresetID: preset.ID,
				Label:    preset.Label,
				Status:   "error",
				Error:    "no workspace folder set on preset",
			})
			continue
		}

		workspacePath := preset.SelectedFolder.String

		// Check if manifest already exists
		_, exists, err := ReadWorkflowManifest(r.Context(), workspacePath)
		if err != nil {
			results = append(results, migrationResult{
				PresetID:      preset.ID,
				Label:         preset.Label,
				WorkspacePath: workspacePath,
				Status:        "error",
				Error:         fmt.Sprintf("failed to read existing manifest: %v", err),
			})
			continue
		}
		if exists && !overwrite {
			results = append(results, migrationResult{
				PresetID:      preset.ID,
				Label:         preset.Label,
				WorkspacePath: workspacePath,
				Status:        "skipped",
				Error:         "manifest already exists (use ?overwrite=true to replace)",
			})
			continue
		}

		// Convert preset to manifest
		manifest, err := ManifestFromPreset(&preset)
		if err != nil {
			results = append(results, migrationResult{
				PresetID:      preset.ID,
				Label:         preset.Label,
				WorkspacePath: workspacePath,
				Status:        "error",
				Error:         fmt.Sprintf("failed to build manifest: %v", err),
			})
			continue
		}

		// Write manifest
		if err := WriteWorkflowManifest(r.Context(), workspacePath, manifest); err != nil {
			results = append(results, migrationResult{
				PresetID:      preset.ID,
				Label:         preset.Label,
				WorkspacePath: workspacePath,
				Status:        "error",
				Error:         fmt.Sprintf("failed to write manifest: %v", err),
			})
			continue
		}

		results = append(results, migrationResult{
			PresetID:      preset.ID,
			Label:         preset.Label,
			WorkspacePath: workspacePath,
			Status:        "migrated",
		})
	}

	// Count results
	migrated, skipped, errored := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case "migrated":
			migrated++
		case "skipped":
			skipped++
		case "error":
			errored++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"results":  results,
		"migrated": migrated,
		"skipped":  skipped,
		"errors":   errored,
		"total":    len(results),
	})
}

// --- Resolve manifest for execution ---

// ResolveWorkflowManifest tries to load a manifest for a given workspace path.
// If manifest exists, returns it. If not, falls back to loading from preset.
// This supports the gradual migration where some workflows have manifests and some don't.
func (api *StreamingAPI) ResolveWorkflowManifest(ctx context.Context, workspacePath string, presetQueryID string) (*WorkflowManifest, error) {
	// Try manifest first
	if workspacePath != "" {
		manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
		if err == nil && exists {
			return manifest, nil
		}
		if err != nil {
			log.Printf("[MANIFEST] Warning: error reading manifest from %s: %v (falling back to preset)", workspacePath, err)
		}
	}

	// Fall back to preset
	if presetQueryID == "" {
		return nil, fmt.Errorf("no manifest found and no preset_query_id provided")
	}

	preset, err := api.chatDB.GetPresetQuery(ctx, presetQueryID)
	if err != nil {
		return nil, fmt.Errorf("failed to load preset %s: %w", presetQueryID, err)
	}

	manifest, err := ManifestFromPreset(preset)
	if err != nil {
		return nil, fmt.Errorf("failed to convert preset to manifest: %w", err)
	}

	return manifest, nil
}

// --- Helper: resolve workspace path from preset ID ---

func (api *StreamingAPI) resolveWorkspacePathFromPreset(ctx context.Context, presetQueryID string) (string, error) {
	if presetQueryID == "" {
		return "", fmt.Errorf("preset_query_id is empty")
	}

	// Primary: look up workflow manifest by ID (file-backed, no DB dependency)
	workflows, err := DiscoverWorkflowManifests(ctx)
	if err == nil {
		for _, wf := range workflows {
			if wf.Manifest.ID == presetQueryID {
				return wf.WorkspacePath, nil
			}
		}
	}

	// Fallback: try DB preset (for backward compatibility with non-workflow presets)
	preset, err := api.chatDB.GetPresetQuery(ctx, presetQueryID)
	if err != nil {
		return "", fmt.Errorf("preset %s not found in manifests or DB", presetQueryID)
	}
	if !preset.SelectedFolder.Valid || preset.SelectedFolder.String == "" {
		return "", fmt.Errorf("preset %s has no selected_folder", presetQueryID)
	}
	return preset.SelectedFolder.String, nil
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
