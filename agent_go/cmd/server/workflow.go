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

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
	"mcp-agent/agent_go/pkg/database"
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
func extractIterationFoldersFromTypedChildren(children []virtualtools.WorkspaceFolderItem, existingFolders []string) []string {
	for i, child := range children {
		// Check for is_directory or type field using typed struct
		isDir := child.IsDirectory || child.IsDir || child.Type == "folder"

		// Get name from filepath or name field
		name := child.Name
		if name == "" && child.Filepath != "" {
			// Extract folder name from filepath (e.g., "Workflow/HDFC Personal Accounts/runs/iteration-3" -> "iteration-3")
			parts := strings.Split(child.Filepath, "/")
			if len(parts) > 0 {
				name = parts[len(parts)-1]
			}
		}

		fmt.Printf("[handleGetRunFolders] Child %d: name=%s, is_dir=%v, filepath=%s\n", i, name, isDir, child.Filepath)

		if isDir && name != "" {
			// Only include iteration folders (iteration-N pattern)
			if strings.HasPrefix(name, "iteration-") {
				fmt.Printf("[handleGetRunFolders] Adding iteration folder: %s\n", name)
				existingFolders = append(existingFolders, name)
			} else {
				fmt.Printf("[handleGetRunFolders] Skipping non-iteration folder: %s\n", name)
			}
		}
	}
	return existingFolders
}

// extractIterationFoldersFromInterfaceArray extracts from array of interface{} (backward compatibility)
func extractIterationFoldersFromInterfaceArray(dataArray []interface{}, existingFolders []string) []string {
	for i, elem := range dataArray {
		if elemMap, ok := elem.(map[string]interface{}); ok {
			// Check if this element has children (the iteration folders)
			if children, ok := elemMap["children"].([]interface{}); ok {
				fmt.Printf("[handleGetRunFolders] Element %d has %d children\n", i, len(children))
				existingFolders = extractIterationFoldersFromChildren(children, existingFolders)
			}
		}
	}
	return existingFolders
}

// extractIterationFoldersFromChildren extracts iteration folder names from children array (interface{} version for backward compatibility)
func extractIterationFoldersFromChildren(children []interface{}, existingFolders []string) []string {
	for i, child := range children {
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
				// Extract folder name from filepath (e.g., "Workflow/HDFC Personal Accounts/runs/iteration-3" -> "iteration-3")
				parts := strings.Split(filepath, "/")
				if len(parts) > 0 {
					name = parts[len(parts)-1]
				}
			} else if n, ok := childMap["name"].(string); ok {
				name = n
			}

			fmt.Printf("[handleGetRunFolders] Child %d: name=%s, is_dir=%v, filepath=%v\n", i, name, isDir, childMap["filepath"])

			if isDir && name != "" {
				// Only include iteration folders (iteration-N pattern)
				if strings.HasPrefix(name, "iteration-") {
					fmt.Printf("[handleGetRunFolders] Adding iteration folder: %s\n", name)
					existingFolders = append(existingFolders, name)
				} else {
					fmt.Printf("[handleGetRunFolders] Skipping non-iteration folder: %s\n", name)
				}
			}
		} else {
			fmt.Printf("[handleGetRunFolders] Child %d is not a map, type: %T\n", i, child)
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

	// Log the path being requested for debugging
	fmt.Printf("[handleGetRunFolders] Requesting runs folder at path: %s\n", runsPath)

	// List folders from workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("folder", runsPath)
	q.Add("max_depth", "1")
	req.URL.RawQuery = q.Encode()

	// Log the full URL being called
	fmt.Printf("[handleGetRunFolders] Calling workspace API: %s\n", req.URL.String())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[handleGetRunFolders] Error calling workspace API: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to call workspace API: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	// Log the response status and body (first 500 chars)
	bodyPreview := string(body)
	if len(bodyPreview) > 500 {
		bodyPreview = bodyPreview[:500] + "..."
	}
	fmt.Printf("[handleGetRunFolders] Workspace API response - Status: %d, Body: %s\n", resp.StatusCode, bodyPreview)

	// Check if runs folder doesn't exist (404)
	if resp.StatusCode == http.StatusNotFound {
		// No runs folder - return empty list
		fmt.Printf("[handleGetRunFolders] Runs folder not found (404), returning empty list\n")
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
		fmt.Printf("[handleGetRunFolders] Workspace API returned error status %d: %s\n", resp.StatusCode, string(body))
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
		return
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		fmt.Printf("[handleGetRunFolders] Failed to parse API response: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to parse API response: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Printf("[handleGetRunFolders] Parsed API response - Success: %v, Message: %s\n", apiResp.Success, apiResp.Message)

	if !apiResp.Success {
		// Treat error as no folders
		fmt.Printf("[handleGetRunFolders] API returned success=false, treating as no folders. Error: %s\n", apiResp.Error)
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

	// Log the data structure for debugging
	if apiResp.Data != nil {
		if dataBytes, err := json.Marshal(apiResp.Data); err == nil {
			dataPreview := string(dataBytes)
			if len(dataPreview) > 1000 {
				dataPreview = dataPreview[:1000] + "..."
			}
			fmt.Printf("[handleGetRunFolders] API response data: %s\n", dataPreview)
		}
	}

	// Handle different response structures using typed structs
	// Case 1: Data is a WorkspaceFolderListing (array of folder items)
	var folderListing virtualtools.WorkspaceFolderListing
	if dataBytes, err := json.Marshal(apiResp.Data); err == nil {
		if err := json.Unmarshal(dataBytes, &folderListing); err == nil && len(folderListing) > 0 {
			fmt.Printf("[handleGetRunFolders] Data is a WorkspaceFolderListing with %d top-level items\n", len(folderListing))
			// Extract iteration folders from the first item's children (the runs folder)
			if len(folderListing) > 0 && len(folderListing[0].Children) > 0 {
				fmt.Printf("[handleGetRunFolders] First item has %d children\n", len(folderListing[0].Children))
				existingFolders = extractIterationFoldersFromTypedChildren(folderListing[0].Children, existingFolders)
			}
		} else {
			// Fallback: try to parse as array of interface{} (backward compatibility)
			if dataArray, ok := apiResp.Data.([]interface{}); ok {
				fmt.Printf("[handleGetRunFolders] Data is an array with %d elements (fallback parsing)\n", len(dataArray))
				existingFolders = extractIterationFoldersFromInterfaceArray(dataArray, existingFolders)
			} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
				fmt.Printf("[handleGetRunFolders] Data is a map, checking for children (fallback parsing)...\n")
				if children, ok := dataMap["children"].([]interface{}); ok {
					fmt.Printf("[handleGetRunFolders] Found %d children in response\n", len(children))
					existingFolders = extractIterationFoldersFromChildren(children, existingFolders)
				} else {
					fmt.Printf("[handleGetRunFolders] No 'children' array found in data map. Available keys: %v\n", getMapKeys(dataMap))
				}
			} else {
				fmt.Printf("[handleGetRunFolders] Data is not a recognized format, type: %T\n", apiResp.Data)
			}
		}
	}

	fmt.Printf("[handleGetRunFolders] Extracted %d iteration folders: %v\n", len(existingFolders), existingFolders)

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
				fmt.Printf("[handleGetRunFolders] Loaded progress for %s: %d/%d steps completed\n",
					folderName, len(progress.CompletedStepIndices), progress.TotalSteps)
			} else {
				fmt.Printf("[handleGetRunFolders] No progress found for %s (file may not exist yet)\n", folderName)
			}
		} else {
			fmt.Printf("[handleGetRunFolders] Skipping progress read for %s (not in latest %d)\n", folderName, maxFoldersWithProgress)
		}

		folderInfos = append(folderInfos, folderInfo)
	}

	// Sort folder infos by iteration number (descending - highest first)
	if len(folderInfos) > 0 {
		sort.Slice(folderInfos, func(i, j int) bool {
			extractIteration := func(name string) int {
				re := regexp.MustCompile(`iteration-(\d+)$`)
				matches := re.FindStringSubmatch(name)
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
