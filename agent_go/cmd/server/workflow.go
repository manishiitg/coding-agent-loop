package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/database"
)

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("WORKSPACE_API_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
}

// getMapKeys returns all keys from a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// readProgressForFolder reads steps_done.json for a given folder and returns the progress
// Returns nil if the file doesn't exist or can't be read (non-fatal)
func readProgressForFolder(ctx context.Context, stepsFilePath string) (*StepProgress, error) {
	// URL-encode the filepath segments
	pathSegments := strings.Split(stepsFilePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Read file from workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second} // Shorter timeout for batch reads
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check if file doesn't exist (404) - this is not an error, just no progress yet
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // Return nil, nil to indicate file doesn't exist (not an error)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Extract content from response - workspace API returns file content in data.content
	var progress StepProgress
	if fileContent, ok := apiResp.Data.(virtualtools.WorkspaceFileContent); ok {
		// Parse the JSON content from typed struct
		if err := json.Unmarshal([]byte(fileContent.Content), &progress); err != nil {
			return nil, fmt.Errorf("failed to parse steps_done.json content: %w", err)
		}
		return &progress, nil
	} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		// Fallback: parse from map[string]interface{} (backward compatibility)
		if content, ok := dataMap["content"].(string); ok {
			// Parse the JSON content
			if err := json.Unmarshal([]byte(content), &progress); err != nil {
				return nil, fmt.Errorf("failed to parse steps_done.json content: %w", err)
			}
			return &progress, nil
		}
	}

	return nil, fmt.Errorf("unexpected response format from workspace API")
}

// extractIterationFoldersFromTypedChildren extracts iteration folder names from typed WorkspaceFolderItem array
// Supports both top-level (iteration-X) and nested (iteration-X/group-Y) folders
func extractIterationFoldersFromTypedChildren(children []virtualtools.WorkspaceFolderItem, existingFolders []string) []string {
	for _, child := range children {
		// Check for is_directory or type field using typed struct
		isDir := child.IsDirectory || child.IsDir || child.Type == "folder"

		// Get name from filepath or name field
		name := child.Name
		if name == "" && child.Filepath != "" {
			// Extract relative path from runs folder (e.g., "Workflow/HDFC Personal Accounts/runs/iteration-3/group-1" -> "iteration-3/group-1")
			// Find "runs/" in the path and extract everything after it
			runsIndex := strings.Index(child.Filepath, "/runs/")
			if runsIndex >= 0 {
				relativePath := child.Filepath[runsIndex+6:] // Skip "/runs/"
				name = relativePath
			} else {
				// Fallback: extract last part
				parts := strings.Split(child.Filepath, "/")
				if len(parts) > 0 {
					name = parts[len(parts)-1]
				}
			}
		}

		if isDir && name != "" {
			// Include iteration folders (both top-level and nested)
			// Top-level: iteration-X
			// Nested: iteration-X/group-Y
			if strings.HasPrefix(name, "iteration-") {
				// If this is a top-level iteration folder, check for nested group folders first
				if !strings.Contains(name, "/") {
					if len(child.Children) > 0 {
						// Check if this iteration has group subfolders
						hasGroups := false
						groupFolders := []string{}

						for _, groupChild := range child.Children {
							if groupChild.IsDirectory || groupChild.IsDir || groupChild.Type == "folder" {
								groupName := groupChild.Name
								if groupName == "" && groupChild.Filepath != "" {
									// Extract relative path
									runsIndex := strings.Index(groupChild.Filepath, "/runs/")
									if runsIndex >= 0 {
										groupName = groupChild.Filepath[runsIndex+6:]
									} else {
										parts := strings.Split(groupChild.Filepath, "/")
										if len(parts) > 0 {
											groupName = parts[len(parts)-1]
										}
									}
								}
								// Check if this is a group subfolder (starts with iteration-X/group-)
								if groupName != "" && strings.HasPrefix(groupName, name+"/") && strings.HasPrefix(strings.TrimPrefix(groupName, name+"/"), "group-") {
									hasGroups = true
									groupFolders = append(groupFolders, groupName)
								}
							}
						}

						// If iteration has group subfolders, only add the groups (not the parent)
						if hasGroups {
							existingFolders = append(existingFolders, groupFolders...)
						} else {
							// No groups found, add the parent iteration folder (backward compatibility)
							existingFolders = append(existingFolders, name)
						}
					} else {
						// No children, add the parent iteration folder
						existingFolders = append(existingFolders, name)
					}
				} else {
					// Nested folder (already a group folder) - add it directly
					existingFolders = append(existingFolders, name)
				}
			}
		}
	}
	return existingFolders
}

// extractIterationFoldersFromInterfaceArray extracts from array of interface{} (backward compatibility)
func extractIterationFoldersFromInterfaceArray(dataArray []interface{}, existingFolders []string) []string {
	for _, elem := range dataArray {
		if elemMap, ok := elem.(map[string]interface{}); ok {
			// Check if this element has children (the iteration folders)
			if children, ok := elemMap["children"].([]interface{}); ok {
				existingFolders = extractIterationFoldersFromChildren(children, existingFolders)
			}
		}
	}
	return existingFolders
}

// extractIterationFoldersFromChildren extracts iteration folder names from children array (interface{} version for backward compatibility)
// Supports both top-level (iteration-X) and nested (iteration-X/group-Y) folders
func extractIterationFoldersFromChildren(children []interface{}, existingFolders []string) []string {
	for _, child := range children {
		if childMap, ok := child.(map[string]interface{}); ok {
			// Check for is_directory or type field
			isDir := false
			if d, ok := childMap["is_directory"].(bool); ok {
				isDir = d
			} else if t, ok := childMap["type"].(string); ok {
				isDir = (t == "folder")
			} else if d, ok := childMap["is_dir"].(bool); ok {
				isDir = d
			}

			// Get name from filepath or name field
			name := ""
			if filepath, ok := childMap["filepath"].(string); ok {
				// Extract relative path from runs folder (e.g., "Workflow/HDFC Personal Accounts/runs/iteration-3/group-1" -> "iteration-3/group-1")
				runsIndex := strings.Index(filepath, "/runs/")
				if runsIndex >= 0 {
					relativePath := filepath[runsIndex+6:] // Skip "/runs/"
					name = relativePath
				} else {
					// Fallback: extract last part
					parts := strings.Split(filepath, "/")
					if len(parts) > 0 {
						name = parts[len(parts)-1]
					}
				}
			} else if n, ok := childMap["name"].(string); ok {
				name = n
			}

			if isDir && name != "" {
				// Include iteration folders (both top-level and nested)
				if strings.HasPrefix(name, "iteration-") {
					// If this is a top-level iteration folder, check for nested group folders first
					if !strings.Contains(name, "/") {
						childrenArray, hasChildren := childMap["children"].([]interface{})
						if hasChildren && len(childrenArray) > 0 {
							// Check if this iteration has group subfolders
							hasGroups := false
							groupFolders := []string{}

							for _, groupChild := range childrenArray {
								if groupMap, ok := groupChild.(map[string]interface{}); ok {
									groupIsDir := false
									if d, ok := groupMap["is_directory"].(bool); ok {
										groupIsDir = d
									} else if t, ok := groupMap["type"].(string); ok {
										groupIsDir = (t == "folder")
									} else if d, ok := groupMap["is_dir"].(bool); ok {
										groupIsDir = d
									}
									if groupIsDir {
										groupName := ""
										if groupFilePath, ok := groupMap["filepath"].(string); ok {
											runsIndex := strings.Index(groupFilePath, "/runs/")
											if runsIndex >= 0 {
												groupName = groupFilePath[runsIndex+6:]
											} else {
												parts := strings.Split(groupFilePath, "/")
												if len(parts) > 0 {
													groupName = parts[len(parts)-1]
												}
											}
										} else if n, ok := groupMap["name"].(string); ok {
											groupName = n
										}
										// Check if this is a group subfolder (starts with iteration-X/group-)
										if groupName != "" && strings.HasPrefix(groupName, name+"/") && strings.HasPrefix(strings.TrimPrefix(groupName, name+"/"), "group-") {
											hasGroups = true
											groupFolders = append(groupFolders, groupName)
										}
									}
								}
							}

							// If iteration has group subfolders, only add the groups (not the parent)
							if hasGroups {
								existingFolders = append(existingFolders, groupFolders...)
							} else {
								// No groups found, add the parent iteration folder (backward compatibility)
								existingFolders = append(existingFolders, name)
							}
						} else {
							// No children or empty children, add the parent iteration folder
							existingFolders = append(existingFolders, name)
						}
					} else {
						// Nested folder (already a group folder) - add it directly
						existingFolders = append(existingFolders, name)
					}
				}
			}
		}
	}
	return existingFolders
}

// RunFolderInfo represents information about a single run folder
type RunFolderInfo struct {
	Name     string        `json:"name"`
	Progress *StepProgress `json:"progress,omitempty"` // Progress info if available
}

// RunFoldersResponse represents the response for listing run folders
type RunFoldersResponse struct {
	Folders      []RunFolderInfo `json:"folders"` // Changed from []string to []RunFolderInfo
	TotalCount   int             `json:"total_count"`
	ShowingCount int             `json:"showing_count"`
}

// StepProgress represents the progress of workflow execution
type StepProgress struct {
	CompletedStepIndices []int                      `json:"completed_step_indices"`
	TotalSteps           int                        `json:"total_steps"`
	LastUpdated          time.Time                  `json:"last_updated"`
	BranchSteps          map[int]BranchStepProgress `json:"branch_steps,omitempty"`
}

// BranchStepProgress tracks which branch was executed for conditional steps
type BranchStepProgress struct {
	BranchExecuted string   `json:"branch_executed"`
	CompletedSteps []string `json:"completed_steps"`
}

// ExecutionOptions represents user-selected execution options from frontend
type ExecutionOptions struct {
	RunMode                 string `json:"run_mode"`                             // "use_same_run" or "create_new_runs_always"
	SelectedRunFolder       string `json:"selected_run_folder,omitempty"`        // If use_same_run and user selected specific folder
	ExecutionStrategy       string `json:"execution_strategy"`                   // "start_from_beginning", "fast_execute_all", etc.
	ResumeFromStep          int    `json:"resume_from_step,omitempty"`           // 1-based step number to resume from
	FastExecuteEndStep      int    `json:"fast_execute_end_step,omitempty"`      // 0-based last step for fast execute range
	PlanChangeAction        string `json:"plan_change_action,omitempty"`         // "keep_old_progress" or "delete_old_progress"
	AllStepsCompletedAction string `json:"all_steps_completed_action,omitempty"` // "fast_execute_again" or "skip_execution"

	// Temporary LLM overrides (optional, overrides step-level configs for this execution only)
	// Only applies to execution agents (not validation or learning agents)
	// Cascading fallback: tempLLM1 → tempLLM2 → step LLM (on validation failures)
	TempOverrideLLM  *AgentLLMConfig `json:"temp_override_llm,omitempty"`  // First override LLM (used on first attempt)
	TempOverrideLLM2 *AgentLLMConfig `json:"temp_override_llm2,omitempty"` // Second override LLM (used on second attempt if tempLLM1 fails)

	// Fallback behavior when validation fails
	FallbackToOriginalLLMOnFailure bool `json:"fallback_to_original_llm_on_failure,omitempty"` // If true, use original LLM instead of temp override when validation fails

	// Learning behavior when tempLLM is active (per-model control)
	SkipLearningWhenTempLLM1 bool `json:"skip_learning_when_temp_llm1,omitempty"` // If true, skip learning phases when tempLLM1 is used (default: false, learning runs)
	SkipLearningWhenTempLLM2 bool `json:"skip_learning_when_temp_llm2,omitempty"` // If true, skip learning phases when tempLLM2 is used (default: false, learning runs)

	// Variable group execution options (for batch execution with multiple groups)
	EnabledGroupIDs []string `json:"enabled_group_ids,omitempty"` // Group IDs to execute (if empty, uses groups' enabled flags)

	// Logging options
	SaveValidationResponses bool `json:"save_validation_responses,omitempty"` // If true, save validation responses and execution logs to workspace (default: true)
}

// AgentLLMConfig represents LLM configuration for an agent (matches controller type)
type AgentLLMConfig struct {
	Provider string `json:"provider,omitempty"` // e.g., "openai", "bedrock", "openrouter", "vertex", "anthropic"
	ModelID  string `json:"model_id,omitempty"` // e.g., "gpt-4o", "claude-3-5-sonnet-20241022"
}

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
	StepID          *string                           `json:"step_id,omitempty"` // Optional step ID for step-specific phase execution
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
		http.Error(w, fmt.Sprintf("Invalid request body: %w", err), http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf("Failed to check existing workflow: %w", err), http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf("Failed to create workflow: %w", err), http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf("Failed to get workflow: %w", err), http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf("Invalid request body: %w", err), http.StatusBadRequest)
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

	// Store step_id in a temporary map for retrieval during execution
	// We'll pass it through workflowOptions when executing
	if req.StepID != nil && *req.StepID != "" {
		// Store step_id in a map keyed by preset_query_id for retrieval during execution
		// This is a temporary storage - step_id is only used during phase execution
		api.workflowStepIDMux.Lock()
		if api.workflowStepIDs == nil {
			api.workflowStepIDs = make(map[string]string)
		}
		api.workflowStepIDs[req.PresetQueryID] = *req.StepID
		api.workflowStepIDMux.Unlock()
	}

	// Update workflow in database
	workflow, err := api.chatDB.UpdateWorkflow(r.Context(), req.PresetQueryID, updateReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update workflow: %w", err), http.StatusInternalServerError)
		return
	}

	// Return success response
	workflowResponse := map[string]interface{}{
		"id":              workflow.ID,
		"preset_query_id": workflow.PresetQueryID,
		"workflow_status": workflow.WorkflowStatus,
		"created_at":      workflow.CreatedAt,
		"updated_at":      workflow.UpdatedAt,
	}

	// Include selected_options if present
	if workflow.SelectedOptions != nil {
		workflowResponse["selected_options"] = workflow.SelectedOptions
	}

	response := map[string]interface{}{
		"success":  true,
		"workflow": workflowResponse,
		"message":  "Workflow updated successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetRunFolders handles listing available run folders for a workspace
func (api *StreamingAPI) handleGetRunFolders(w http.ResponseWriter, r *http.Request) {
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

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	// Build path to runs folder
	runsPath := workspacePath + "/runs"

	// List folders from workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	// Add query parameters
	// Use max_depth=2 to list nested folders (iteration-X/group-Y)
	q := req.URL.Query()
	q.Add("folder", runsPath)
	q.Add("max_depth", "2")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to call workspace API: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	// Check if runs folder doesn't exist (404)
	if resp.StatusCode == http.StatusNotFound {
		// No runs folder - return empty list
		response := RunFoldersResponse{
			Folders:      []RunFolderInfo{},
			TotalCount:   0,
			ShowingCount: 0,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
		return
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse API response: %v", err), http.StatusInternalServerError)
		return
	}

	if !apiResp.Success {
		// Treat error as no folders
		response := RunFoldersResponse{
			Folders:      []RunFolderInfo{},
			TotalCount:   0,
			ShowingCount: 0,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Extract folder names from response data
	existingFolders := []string{} // Initialize as empty slice, not nil

	// Handle different response structures using typed structs
	// Case 1: Data is a WorkspaceFolderListing (array of folder items)
	var folderListing virtualtools.WorkspaceFolderListing
	if dataBytes, err := json.Marshal(apiResp.Data); err == nil {
		if err := json.Unmarshal(dataBytes, &folderListing); err == nil && len(folderListing) > 0 {
			// Extract iteration folders from the first item's children (the runs folder)
			if len(folderListing) > 0 && len(folderListing[0].Children) > 0 {
				existingFolders = extractIterationFoldersFromTypedChildren(folderListing[0].Children, existingFolders)
			}
		} else {
			// Fallback: try to parse as array of interface{} (backward compatibility)
			if dataArray, ok := apiResp.Data.([]interface{}); ok {
				existingFolders = extractIterationFoldersFromInterfaceArray(dataArray, existingFolders)
			} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
				if children, ok := dataMap["children"].([]interface{}); ok {
					existingFolders = extractIterationFoldersFromChildren(children, existingFolders)
				}
			}
		}
	}

	// Build folder info - read progress for all displayed iterations (max 10)
	folderInfos := make([]RunFolderInfo, 0, len(existingFolders))
	maxFoldersWithProgress := 10 // Read progress for all displayed iterations

	for i, folderName := range existingFolders {
		folderInfo := RunFolderInfo{
			Name: folderName,
		}

		// Only read progress for the latest N iterations (most likely to be selected)
		if i < maxFoldersWithProgress {
			// Try to read steps_done.json for this folder
			stepsFilePath := workspacePath + "/runs/" + folderName + "/steps_done.json"
			progress, err := readProgressForFolder(r.Context(), stepsFilePath)
			if err == nil && progress != nil {
				folderInfo.Progress = progress
			}
		}

		folderInfos = append(folderInfos, folderInfo)
	}

	// Sort folder infos by iteration number (descending - highest first)
	// Supports both formats: iteration-X and iteration-X/group-Y
	if len(folderInfos) > 0 {
		sort.Slice(folderInfos, func(i, j int) bool {
			extractIteration := func(name string) int {
				// Try nested format first: iteration-X/group-Y
				re := regexp.MustCompile(`iteration-(\d+)/`)
				matches := re.FindStringSubmatch(name)
				if len(matches) > 1 {
					var num int
					if _, err := fmt.Sscanf(matches[1], "%d", &num); err == nil {
						return num
					}
				}
				// Fallback to top-level format: iteration-X
				re = regexp.MustCompile(`iteration-(\d+)$`)
				matches = re.FindStringSubmatch(name)
				if len(matches) > 1 {
					var num int
					if _, err := fmt.Sscanf(matches[1], "%d", &num); err == nil {
						return num
					}
				}
				return -1
			}

			iterI := extractIteration(folderInfos[i].Name)
			iterJ := extractIteration(folderInfos[j].Name)

			if iterI != iterJ {
				return iterI > iterJ
			}
			return folderInfos[i].Name > folderInfos[j].Name
		})
	}

	// Limit to 10 most recent folders
	totalCount := len(folderInfos)
	maxFolders := 10
	if len(folderInfos) > maxFolders {
		folderInfos = folderInfos[:maxFolders]
	}

	response := RunFoldersResponse{
		Folders:      folderInfos,
		TotalCount:   totalCount,
		ShowingCount: len(folderInfos),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetProgress handles getting execution progress (steps_done.json) for a run folder
func (api *StreamingAPI) handleGetProgress(w http.ResponseWriter, r *http.Request) {
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

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	runFolder := r.URL.Query().Get("run_folder")
	if runFolder == "" {
		http.Error(w, "run_folder parameter is required", http.StatusBadRequest)
		return
	}

	// Build path to steps_done.json
	stepsFilePath := workspacePath + "/runs/" + runFolder + "/steps_done.json"

	// URL-encode the filepath segments
	pathSegments := strings.Split(stepsFilePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Read file from workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to call workspace API: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	// Check if file doesn't exist (404)
	if resp.StatusCode == http.StatusNotFound {
		// No progress file - return empty progress
		response := map[string]interface{}{
			"exists":   false,
			"progress": nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
		return
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse API response: %v", err), http.StatusInternalServerError)
		return
	}

	if !apiResp.Success {
		// File doesn't exist or error reading
		response := map[string]interface{}{
			"exists":   false,
			"progress": nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Extract content from response - workspace API returns file content in data.content
	var progress StepProgress
	if fileContent, ok := apiResp.Data.(virtualtools.WorkspaceFileContent); ok {
		// Parse the JSON content from typed struct
		if err := json.Unmarshal([]byte(fileContent.Content), &progress); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse steps_done.json content: %v", err), http.StatusInternalServerError)
			return
		}
	} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		// Fallback: parse from map[string]interface{} (backward compatibility)
		if content, ok := dataMap["content"].(string); ok {
			// Parse the JSON content
			if err := json.Unmarshal([]byte(content), &progress); err != nil {
				http.Error(w, fmt.Sprintf("Failed to parse steps_done.json content: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	response := map[string]interface{}{
		"exists":   true,
		"progress": progress,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// VariableGroup represents a single set of variable values (matches controller type)
type VariableGroup struct {
	GroupID string            `json:"group_id"`
	Values  map[string]string `json:"values"`
	Enabled bool              `json:"enabled"`
}

// Variable represents a single variable definition
type Variable struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description"`
}

// VariablesManifest represents the variables.json structure
type VariablesManifest struct {
	Objective      string          `json:"objective"`
	Variables      []Variable      `json:"variables"`
	Groups         []VariableGroup `json:"groups,omitempty"`
	ExtractionDate string          `json:"extraction_date"`
}

// VariableGroupsResponse represents the response for getting variable groups
type VariableGroupsResponse struct {
	Success  bool               `json:"success"`
	Manifest *VariablesManifest `json:"manifest,omitempty"`
	Error    string             `json:"error,omitempty"`
}

// handleGetVariableGroups handles getting variable groups from variables.json
func (api *StreamingAPI) handleGetVariableGroups(w http.ResponseWriter, r *http.Request) {
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

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	// Build path to variables.json
	variablesPath := workspacePath + "/variables/variables.json"

	// URL-encode the filepath segments
	pathSegments := strings.Split(variablesPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Read file from workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to call workspace API: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	// Check if file doesn't exist (404)
	if resp.StatusCode == http.StatusNotFound {
		response := VariableGroupsResponse{
			Success:  true,
			Manifest: nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
		return
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse API response: %v", err), http.StatusInternalServerError)
		return
	}

	if !apiResp.Success {
		response := VariableGroupsResponse{
			Success: false,
			Error:   apiResp.Error,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Extract content from response
	var manifest VariablesManifest
	if fileContent, ok := apiResp.Data.(virtualtools.WorkspaceFileContent); ok {
		if err := json.Unmarshal([]byte(fileContent.Content), &manifest); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse variables.json content: %v", err), http.StatusInternalServerError)
			return
		}
	} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		if content, ok := dataMap["content"].(string); ok {
			if err := json.Unmarshal([]byte(content), &manifest); err != nil {
				http.Error(w, fmt.Sprintf("Failed to parse variables.json content: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	response := VariableGroupsResponse{
		Success:  true,
		Manifest: &manifest,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleUpdateVariableGroups handles updating variable groups in variables.json
func (api *StreamingAPI) handleUpdateVariableGroups(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, PUT, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" && r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	// Parse request body
	var manifest VariablesManifest
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Write updated manifest to variables.json
	variablesPath := workspacePath + "/variables/variables.json"

	// Marshal manifest to JSON
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal manifest: %v", err), http.StatusInternalServerError)
		return
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(variablesPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Prepare request body with Content field (workspace API expects {"content": "...", "commit_message": "..."})
	requestBody := map[string]interface{}{
		"content": string(manifestJSON),
	}
	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal request body: %v", err), http.StatusInternalServerError)
		return
	}

	// Write file via workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(r.Context(), "PUT", apiURL, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to call workspace API: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Variable groups updated successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCreateRunFolder handles creating a new run folder (iteration)
func (api *StreamingAPI) handleCreateRunFolder(w http.ResponseWriter, r *http.Request) {
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

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	// Build path to runs folder
	runsPath := workspacePath + "/runs"

	// First, list existing folders to find the next iteration number
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("folder", runsPath)
	q.Add("max_depth", "2")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to call workspace API: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	// Extract existing folders from response
	existingFolders := []string{}
	if resp.StatusCode == http.StatusOK {
		var apiResp virtualtools.WorkspaceAPIResponse
		if err := json.Unmarshal(body, &apiResp); err == nil && apiResp.Success {
			// Extract iteration folders from response
			var folderListing virtualtools.WorkspaceFolderListing
			if dataBytes, err := json.Marshal(apiResp.Data); err == nil {
				if err := json.Unmarshal(dataBytes, &folderListing); err == nil && len(folderListing) > 0 {
					if len(folderListing) > 0 && len(folderListing[0].Children) > 0 {
						existingFolders = extractIterationFoldersFromTypedChildren(folderListing[0].Children, existingFolders)
					}
				} else {
					// Fallback: try to parse as array of interface{}
					if dataArray, ok := apiResp.Data.([]interface{}); ok {
						existingFolders = extractIterationFoldersFromInterfaceArray(dataArray, existingFolders)
					} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
						if children, ok := dataMap["children"].([]interface{}); ok {
							existingFolders = extractIterationFoldersFromChildren(children, existingFolders)
						}
					}
				}
			}
		}
	}

	// Find the next available iteration number
	// Extract all iteration numbers from existing folders
	maxIteration := 0
	iterationRe := regexp.MustCompile(`iteration-(\d+)`)
	for _, folder := range existingFolders {
		// Handle both formats: iteration-X and iteration-X/group-Y
		matches := iterationRe.FindStringSubmatch(folder)
		if len(matches) > 1 {
			var num int
			if _, err := fmt.Sscanf(matches[1], "%d", &num); err == nil {
				if num > maxIteration {
					maxIteration = num
				}
			}
		}
	}

	// Create new iteration folder with next number
	newIterationNumber := maxIteration + 1
	newFolderName := fmt.Sprintf("iteration-%d", newIterationNumber)
	folderPath := runsPath + "/" + newFolderName

	// Create folder via workspace API (POST /api/folders)
	createFolderURL := getWorkspaceAPIURL() + "/api/folders"
	createFolderReqBody := map[string]interface{}{
		"folder_path": folderPath,
	}
	reqBodyJSON, err := json.Marshal(createFolderReqBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal request body: %v", err), http.StatusInternalServerError)
		return
	}

	createReq, err := http.NewRequestWithContext(r.Context(), "POST", createFolderURL, strings.NewReader(string(reqBodyJSON)))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create folder request: %v", err), http.StatusInternalServerError)
		return
	}
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := client.Do(createReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to call workspace API to create folder: %v", err), http.StatusInternalServerError)
		return
	}
	defer createResp.Body.Close()

	createBody, err := io.ReadAll(createResp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read create folder response: %v", err), http.StatusInternalServerError)
		return
	}

	if createResp.StatusCode == http.StatusConflict {
		// Folder already exists - try next number
		// This shouldn't happen, but handle it gracefully
		newIterationNumber++
		newFolderName = fmt.Sprintf("iteration-%d", newIterationNumber)
		folderPath = runsPath + "/" + newFolderName

		// Retry with new number
		createFolderReqBody["folder_path"] = folderPath
		reqBodyJSON, _ = json.Marshal(createFolderReqBody)
		createReq, _ = http.NewRequestWithContext(r.Context(), "POST", createFolderURL, strings.NewReader(string(reqBodyJSON)))
		createReq.Header.Set("Content-Type", "application/json")

		createResp, err = client.Do(createReq)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to call workspace API to create folder: %v", err), http.StatusInternalServerError)
			return
		}
		defer createResp.Body.Close()
		createBody, _ = io.ReadAll(createResp.Body)
	}

	if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", createResp.StatusCode, string(createBody)), http.StatusInternalServerError)
		return
	}

	// Parse workspace API response
	var createApiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(createBody, &createApiResp); err != nil {
		// Even if parsing fails, if status was OK/Created, folder was likely created
		if createResp.StatusCode == http.StatusCreated || createResp.StatusCode == http.StatusOK {
			// Return success with folder name
			response := map[string]interface{}{
				"success":     true,
				"folder_name": newFolderName,
				"message":     "Folder created successfully",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to parse API response: %v", err), http.StatusInternalServerError)
		return
	}

	if !createApiResp.Success {
		http.Error(w, fmt.Sprintf("Failed to create folder: %s", createApiResp.Error), http.StatusInternalServerError)
		return
	}

	// Success response
	response := map[string]interface{}{
		"success":     true,
		"folder_name": newFolderName,
		"message":     "Folder created successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDeleteRunFolder handles deleting a run folder (iteration)
func (api *StreamingAPI) handleDeleteRunFolder(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	runFolder := r.URL.Query().Get("run_folder")
	if runFolder == "" {
		http.Error(w, "run_folder parameter is required", http.StatusBadRequest)
		return
	}

	// Validate run folder name (security: prevent path traversal)
	if strings.Contains(runFolder, "..") || strings.Contains(runFolder, "/") || strings.Contains(runFolder, "\\") {
		http.Error(w, "Invalid run folder name", http.StatusBadRequest)
		return
	}

	// Construct folder path: {workspacePath}/runs/{runFolder}
	folderPath := workspacePath + "/runs/" + runFolder

	// URL-encode the folder path segments
	pathSegments := strings.Split(folderPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Delete folder via workspace API
	apiURL := getWorkspaceAPIURL() + "/api/folders/" + encodedPath + "?confirm=true"
	req, err := http.NewRequestWithContext(r.Context(), "DELETE", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to call workspace API: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode == http.StatusNotFound {
		// Folder doesn't exist - return success (idempotent)
		response := map[string]interface{}{
			"success": true,
			"message": "Folder does not exist (already deleted)",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
		return
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse API response: %v", err), http.StatusInternalServerError)
		return
	}

	if !apiResp.Success {
		http.Error(w, fmt.Sprintf("Failed to delete folder: %s", apiResp.Error), http.StatusInternalServerError)
		return
	}

	// Success response
	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Successfully deleted iteration folder: %s", runFolder),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleDeleteStepLearnings handles deleting learnings for a specific step
func (api *StreamingAPI) handleDeleteStepLearnings(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	stepNumberStr := r.URL.Query().Get("step_number")
	if stepNumberStr == "" {
		http.Error(w, "step_number parameter is required", http.StatusBadRequest)
		return
	}

	// Validate step number (must be a positive integer)
	var stepNumber int
	if _, err := fmt.Sscanf(stepNumberStr, "%d", &stepNumber); err != nil || stepNumber < 1 {
		http.Error(w, "step_number must be a positive integer", http.StatusBadRequest)
		return
	}

	// Construct learnings folder path: {workspacePath}/learnings/step-{stepNumber}
	learningsPath := fmt.Sprintf("%s/learnings/step-%d", workspacePath, stepNumber)

	// URL-encode the folder path segments
	pathSegments := strings.Split(learningsPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Delete folder via workspace API
	apiURL := getWorkspaceAPIURL() + "/api/folders/" + encodedPath + "?confirm=true"
	req, err := http.NewRequestWithContext(r.Context(), "DELETE", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to call workspace API: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode == http.StatusNotFound {
		// Folder doesn't exist - return success (idempotent)
		response := map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Learnings folder for step %d does not exist (already deleted)", stepNumber),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
		return
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse API response: %v", err), http.StatusInternalServerError)
		return
	}

	if !apiResp.Success {
		http.Error(w, fmt.Sprintf("Failed to delete learnings folder: %s", apiResp.Error), http.StatusInternalServerError)
		return
	}

	// Success response
	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Successfully deleted learnings for step %d", stepNumber),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
