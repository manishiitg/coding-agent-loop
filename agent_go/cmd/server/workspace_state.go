package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	orchtypes "mcp-agent-builder-go/agent_go/pkg/orchestrator/types"
)

// WorkspaceStateResponse is the consolidated response containing all workspace data
type WorkspaceStateResponse struct {
	Success bool           `json:"success"`
	Data    *WorkspaceState `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// WorkspaceState contains all workspace-related data in a single structure
type WorkspaceState struct {
	// Run folders and progress
	RunFolders       []RunFolderInfo  `json:"run_folders"`
	SelectedProgress *StepProgress    `json:"selected_progress,omitempty"`

	// Variables manifest
	VariablesManifest *VariablesManifest `json:"variables_manifest,omitempty"`

	// Workflow phases
	Phases []orchtypes.WorkflowPhase `json:"phases"`
}

// handleLoadWorkspaceState handles loading all workspace state in a single request
func (api *StreamingAPI) handleLoadWorkspaceState(w http.ResponseWriter, r *http.Request) {
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

	selectedFolder := r.URL.Query().Get("selected_folder") // Optional
	fmt.Printf("[DEBUG] handleLoadWorkspaceState: workspacePath=%s, selectedFolder=%s\n", workspacePath, selectedFolder)

	ctx := r.Context()
	state := &WorkspaceState{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []string

	// Load run folders
	wg.Add(1)
	go func() {
		defer wg.Done()
		folders, err := api.loadRunFoldersInternal(ctx, workspacePath)
		if err != nil {
			mu.Lock()
			errors = append(errors, fmt.Sprintf("run_folders: %v", err))
			mu.Unlock()
			return
		}
		mu.Lock()
		state.RunFolders = folders
		mu.Unlock()
	}()

	// Load variables manifest
	wg.Add(1)
	go func() {
		defer wg.Done()
		manifest, err := api.loadVariablesManifestInternal(ctx, workspacePath)
		if err != nil {
			mu.Lock()
			errors = append(errors, fmt.Sprintf("variables: %v", err))
			mu.Unlock()
			return
		}
		mu.Lock()
		state.VariablesManifest = manifest
		mu.Unlock()
	}()

	// Load selected folder progress if specified
	if selectedFolder != "" && selectedFolder != "new" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			progress, err := api.loadProgressInternal(ctx, workspacePath, selectedFolder)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("progress: %v", err))
				mu.Unlock()
				return
			}
			mu.Lock()
			state.SelectedProgress = progress
			mu.Unlock()
		}()
	}

	// Load phases (synchronous, fast)
	state.Phases = orchtypes.GetWorkflowConstants().Phases

	// Wait for all goroutines to complete
	wg.Wait()

	// Check if there were any errors
	if len(errors) > 0 {
		response := WorkspaceStateResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to load some data: %v", errors),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPartialContent) // 206 - some data loaded successfully
		json.NewEncoder(w).Encode(response)
		return
	}

	// Success - return all data
	response := WorkspaceStateResponse{
		Success: true,
		Data:    state,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// loadRunFoldersInternal loads run folders (extracted from handleGetRunFolders)
func (api *StreamingAPI) loadRunFoldersInternal(ctx context.Context, workspacePath string) ([]RunFolderInfo, error) {
	// This is the core logic from handleGetRunFolders
	// Returns just the folders array without HTTP handling
	folders, err := api.getRunFoldersFromWorkspace(ctx, workspacePath)
	if err != nil {
		return []RunFolderInfo{}, nil // Return empty on error, not nil
	}
	return folders, nil
}

// loadVariablesManifestInternal loads variables manifest (extracted from handleGetVariableGroups)
func (api *StreamingAPI) loadVariablesManifestInternal(ctx context.Context, workspacePath string) (*VariablesManifest, error) {
	variablesPath := workspacePath + "/variables/variables.json"
	content, exists, err := readFileFromWorkspace(ctx, variablesPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil // No variables file - return nil, not error
	}

	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	return &manifest, nil
}

// loadProgressInternal loads progress for a specific folder (extracted from handleGetProgress)
func (api *StreamingAPI) loadProgressInternal(ctx context.Context, workspacePath, folderName string) (*StepProgress, error) {
	// Use the same file and helper function as the folder loading logic
	stepsFilePath := workspacePath + "/runs/" + folderName + "/execution/steps_done.json"
	fmt.Printf("[DEBUG] loadProgressInternal: workspacePath=%s, folderName=%s, stepsFilePath=%s\n", workspacePath, folderName, stepsFilePath)
	progress, err := readProgressForFolder(ctx, stepsFilePath)
	if err != nil {
		return nil, err
	}
	if progress == nil {
		return nil, nil // No progress file - return nil, not error
	}

	return progress, nil
}

// getRunFoldersFromWorkspace gets run folders by calling workspace API
// Extracted from handleGetRunFolders for reuse
func (api *StreamingAPI) getRunFoldersFromWorkspace(ctx context.Context, workspacePath string) ([]RunFolderInfo, error) {
	runsPath := workspacePath + "/runs"

	// List folders from workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
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
		return nil, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check if runs folder doesn't exist (404)
	if resp.StatusCode == http.StatusNotFound {
		return []RunFolderInfo{}, nil // Empty list, not error
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("workspace API returned status %d", resp.StatusCode)
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	if !apiResp.Success {
		return []RunFolderInfo{}, nil // Empty list, not error
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

	// Build folder info list
	folderInfos := make([]RunFolderInfo, 0, len(existingFolders))
	for _, folderName := range existingFolders {
		folderInfos = append(folderInfos, RunFolderInfo{Name: folderName})
	}

	// Sort by iteration number descending (highest first), then by group name ascending within the same iteration
	// Supports both formats: iteration-X and iteration-X/group-Y
	sort.Slice(folderInfos, func(i, j int) bool {
		iterI := extractIterationNumber(folderInfos[i].Name)
		iterJ := extractIterationNumber(folderInfos[j].Name)
		if iterI != iterJ {
			return iterI > iterJ
		}
		// Same iteration: sort group names ascending so alphabetically early names (e.g. "atul") aren't pushed to the end
		return folderInfos[i].Name < folderInfos[j].Name
	})

	// Limit to 50 most recent folders/groups
	const maxFolders = 50
	if len(folderInfos) > maxFolders {
		folderInfos = folderInfos[:maxFolders]
	}

	// Load progress for all iterations/groups
	// This ensures progress is shown for all folders in the dropdown
	for i := range folderInfos {
		stepsFilePath := workspacePath + "/runs/" + folderInfos[i].Name + "/execution/steps_done.json"
		if progress, err := readProgressForFolder(ctx, stepsFilePath); err == nil && progress != nil {
			folderInfos[i].Progress = progress
		}
	}

	return folderInfos, nil
}

// extractIterationNumber extracts the iteration number from a folder name.
// Supports both "iteration-X" and "iteration-X/group-Y" formats.
func extractIterationNumber(name string) int {
	re := regexp.MustCompile(`iteration-(\d+)`)
	matches := re.FindStringSubmatch(name)
	if len(matches) > 1 {
		var num int
		if _, err := fmt.Sscanf(matches[1], "%d", &num); err == nil {
			return num
		}
	}
	return -1
}
