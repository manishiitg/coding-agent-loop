package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/fsutil"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"

	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

// resolveWorkflowLLMConfigForWorkspace reads the workflow.json manifest at workspacePath
// and extracts the LLM configuration for use in workflow execution.
//
//nolint:unused // retained for the workflow-manifest execution path while routes are being migrated.
func (api *StreamingAPI) resolveWorkflowLLMConfigForWorkspace(
	ctx context.Context,
	workspacePath string,
	userID string,
) (*todo_creation_human.AgentLLMConfig, *todo_creation_human.TieredLLMConfig, string, error) {
	// Try to load manifest from workspace
	manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
	if err == nil && exists && manifest.Capabilities.LLMConfig != nil {
		llmCfg := manifest.Capabilities.LLMConfig
		phaseLLM := workshopExtractLLM(llmCfg.PhaseLLM, llmCfg.Provider, llmCfg.ModelID)
		var tieredConfig *todo_creation_human.TieredLLMConfig
		if llmCfg.LLMAllocationMode == "tiered" && llmCfg.TieredConfig != nil {
			tieredConfig = &todo_creation_human.TieredLLMConfig{
				Tier1: &todo_creation_human.AgentLLMConfig{
					Provider:  llmCfg.TieredConfig.Tier1.Provider,
					ModelID:   llmCfg.TieredConfig.Tier1.ModelID,
					Fallbacks: workshopConvertFallbacks(llmCfg.TieredConfig.Tier1.Fallbacks),
				},
				Tier2: &todo_creation_human.AgentLLMConfig{
					Provider:  llmCfg.TieredConfig.Tier2.Provider,
					ModelID:   llmCfg.TieredConfig.Tier2.ModelID,
					Fallbacks: workshopConvertFallbacks(llmCfg.TieredConfig.Tier2.Fallbacks),
				},
				Tier3: &todo_creation_human.AgentLLMConfig{
					Provider:  llmCfg.TieredConfig.Tier3.Provider,
					ModelID:   llmCfg.TieredConfig.Tier3.ModelID,
					Fallbacks: workshopConvertFallbacks(llmCfg.TieredConfig.Tier3.Fallbacks),
				},
			}
		}
		return phaseLLM, tieredConfig, manifest.ID, nil
	}

	// Fallback to server defaults
	if api.provider != "" && api.model != "" {
		return &todo_creation_human.AgentLLMConfig{
			Provider: api.provider,
			ModelID:  api.model,
		}, nil, "", nil
	}

	return nil, nil, "", nil
}

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("WORKSPACE_API_URL"); url != "" {
		return url
	}
	return "http://127.0.0.1:8081"
}

// getWorkspaceDocsAbsPath returns the absolute filesystem path to the workspace docs root.
//
//nolint:unused // kept as a shared helper for upcoming absolute-path route cleanup.
func getWorkspaceDocsAbsPath() string {
	return fsutil.WorkspaceDocsRoot()
}

// listGroupSubdirs returns the names of immediate subdirectories under a workspace
// folder path, used to discover per-group folders inside an iteration (e.g. the
// "xspaces" / "excellence" / etc. dirs under runs/iteration-N/). Returns nil on
// any error or when the folder is empty.
func listGroupSubdirs(ctx context.Context, folderPath string) []string {
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}
	q := req.URL.Query()
	q.Add("folder", folderPath)
	q.Add("max_depth", "1")
	req.URL.RawQuery = q.Encode()

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var listing struct {
		Success bool                                `json:"success"`
		Data    virtualtools.WorkspaceFolderListing `json:"data"`
	}
	if err := json.Unmarshal(body, &listing); err != nil || !listing.Success {
		return nil
	}
	var groups []string
	for _, item := range listing.Data {
		if item.Type != "folder" {
			continue
		}
		name := filepath.Base(item.FilePath)
		groups = append(groups, name)
	}
	return groups
}

// workspacePathExists reports whether a workspace folder path resolves to a
// listing. Used to detect whether the old flat "runs/{iter}/logs" layout exists
// before falling back to the newer "runs/{iter}/{group}/logs" nesting.
func workspacePathExists(ctx context.Context, folderPath string) bool {
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return false
	}
	q := req.URL.Query()
	q.Add("folder", folderPath)
	q.Add("max_depth", "1")
	req.URL.RawQuery = q.Encode()

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	var listing struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return false
	}
	return listing.Success
}

// readFileFromWorkspace reads a file from the workspace API and returns its content as a string
// Returns (content, true, nil) if file exists, (empty, false, nil) if file doesn't exist (404), or (empty, false, error) on error
func readFileFromWorkspace(ctx context.Context, filePath string) (string, bool, error) {
	// URL-encode the filepath segments
	pathSegments := strings.Split(filePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Read file from workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, fmt.Errorf("failed to read response: %w", err)
	}

	// Check if file doesn't exist (404) - this is not an error
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil // File doesn't exist, but not an error
	}

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse workspace API response
	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", false, fmt.Errorf("failed to parse API response: %w", err)
	}

	if !apiResp.Success {
		return "", false, fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Check if file doesn't exist (API returns Success: true but with error/message)
	if strings.Contains(apiResp.Message, "File does not exist") ||
		strings.Contains(apiResp.Error, "File not found") {
		return "", false, nil // File doesn't exist, not an error
	}

	// Extract content from response
	var content string
	if fileContent, ok := apiResp.Data.(virtualtools.WorkspaceFileContent); ok {
		content = fileContent.Content
	} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		if c, ok := dataMap["content"].(string); ok {
			content = c
		}
	}

	if content == "" {
		// Debug logging to see actual response structure
		dataBytes, _ := json.Marshal(apiResp.Data)
		log.Printf("[DEBUG] readFileFromWorkspace: Failed to extract content from response. FilePath: %s, Response Data: %s", filePath, string(dataBytes))
		return "", false, fmt.Errorf("failed to extract content from API response")
	}

	return content, true, nil
}

// deleteWorkspaceFile deletes a file from the workspace via the workspace API.
// Returns nil if the file doesn't exist (404) or was successfully deleted.
func deleteWorkspaceFile(ctx context.Context, configPath string) error {
	pathSegments := strings.Split(configPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath + "?confirm=true"
	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// writeFileToWorkspace writes content to a file in the workspace via the workspace API
func writeFileToWorkspace(ctx context.Context, filePath, content string) error {
	pathSegments := strings.Split(filePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	requestBodyJSON, err := json.Marshal(map[string]interface{}{"content": content})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// readProgressForFolder reads steps_done.json for a given folder and returns the progress
// Returns nil if the file doesn't exist or can't be read (non-fatal)
func readRunMetadata(ctx context.Context, metadataFilePath string) (*RunMetadata, error) {
	content, exists, err := readFileFromWorkspace(ctx, metadataFilePath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	var metadata RunMetadata
	if err := json.Unmarshal([]byte(content), &metadata); err != nil {
		return nil, nil
	}
	if metadata.StartedAt.IsZero() {
		metadata.StartedAt = metadata.CreatedAt
	}
	if metadata.DurationMs == nil && metadata.CompletedAt != nil && !metadata.StartedAt.IsZero() {
		durationMs := metadata.CompletedAt.Sub(metadata.StartedAt).Milliseconds()
		metadata.DurationMs = &durationMs
	}
	return &metadata, nil
}

// inferRunMetadata creates metadata for legacy run folders that don't have run_metadata.json.
// Uses the migrated costs store created_at as start time, progress.LastUpdated for completion.
func inferRunMetadata(ctx context.Context, workspacePath, folderName string, progress *StepProgress) *RunMetadata {
	if progress == nil {
		return nil
	}

	metadata := &RunMetadata{
		Status:      "running",
		TriggeredBy: "manual", // assume manual for legacy runs
	}

	if executionCosts, err := readAllRunTokenUsageFromCosts(ctx, workspacePath, orchestrator.CostScopeExecution); err == nil {
		if tokenUsage := executionCosts[folderName]; tokenUsage != nil && !tokenUsage.CreatedAt.IsZero() {
			metadata.CreatedAt = tokenUsage.CreatedAt
		}
	}

	// Fallback: use progress.LastUpdated as rough created_at if cost metadata didn't work
	if metadata.CreatedAt.IsZero() {
		metadata.CreatedAt = progress.LastUpdated
	}
	metadata.StartedAt = metadata.CreatedAt

	// Determine completion
	if progress.TotalSteps > 0 && len(progress.CompletedStepIndices) >= progress.TotalSteps {
		completedAt := progress.LastUpdated
		metadata.Status = "completed"
		metadata.CompletedAt = &completedAt
		durationMs := completedAt.Sub(metadata.StartedAt).Milliseconds()
		metadata.DurationMs = &durationMs
	}

	return metadata
}

func writeRunMetadata(ctx context.Context, metadataFilePath string, metadata *RunMetadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal run metadata: %w", err)
	}
	return writeFileToWorkspace(ctx, metadataFilePath, string(data))
}

func readProgressForFolder(ctx context.Context, stepsFilePath string) (*StepProgress, error) {
	// Use the generic file reading helper
	content, exists, err := readFileFromWorkspace(ctx, stepsFilePath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil // File doesn't exist, not an error
	}

	// Parse the JSON content
	var progress StepProgress
	if err := json.Unmarshal([]byte(content), &progress); err != nil {
		// If unable to parse JSON (e.g., merge conflicts, corrupted file), return nil (empty progress)
		// This allows the API to continue working even if the file is corrupted
		return nil, nil
	}
	return &progress, nil
}

// extractIterationFoldersFromTypedChildren extracts iteration folder names from typed WorkspaceFolderItem array
// Supports both top-level (iteration-X) and nested (iteration-X/group-Y) folders
func extractIterationFoldersFromTypedChildren(children []virtualtools.WorkspaceFolderItem, existingFolders []string) []string {
	for _, child := range children {
		// Check type field using typed struct
		isDir := child.Type == "folder"

		// Get name from FilePath field
		name := ""
		if child.FilePath != "" {
			// Extract relative path from runs folder (e.g., "Workflow/HDFC Personal Accounts/runs/iteration-3/group-1" -> "iteration-3/group-1")
			// Find "runs/" in the path and extract everything after it
			runsIndex := strings.Index(child.FilePath, "/runs/")
			if runsIndex >= 0 {
				relativePath := child.FilePath[runsIndex+6:] // Skip "/runs/"
				name = relativePath
			} else {
				// Fallback: extract last part
				parts := strings.Split(child.FilePath, "/")
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
							if groupChild.Type == "folder" {
								groupName := ""
								if groupChild.FilePath != "" {
									// Extract relative path
									runsIndex := strings.Index(groupChild.FilePath, "/runs/")
									if runsIndex >= 0 {
										groupName = groupChild.FilePath[runsIndex+6:]
									} else {
										parts := strings.Split(groupChild.FilePath, "/")
										if len(parts) > 0 {
											groupName = parts[len(parts)-1]
										}
									}
								}
								// Check if this is a group subfolder (nested under iteration-X)
								// Accepts both "group-X" format (backward compatibility) and display names (e.g., "production", "staging")
								if groupName != "" && strings.HasPrefix(groupName, name+"/") {
									// Any nested folder under iteration-X is considered a group folder
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
// Supports both top-level (iteration-X) and nested (iteration-X/group-Y or iteration-X/display-name) folders
func extractIterationFoldersFromChildren(children []interface{}, existingFolders []string) []string {
	for _, child := range children {
		if childMap, ok := child.(map[string]interface{}); ok {
			// Check type field
			isDir := false
			if t, ok := childMap["type"].(string); ok {
				isDir = (t == "folder")
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
									if t, ok := groupMap["type"].(string); ok {
										groupIsDir = (t == "folder")
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
										// Check if this is a group subfolder (nested under iteration-X)
										// Accepts both "group-X" format (backward compatibility) and display names (e.g., "production", "staging")
										if groupName != "" && strings.HasPrefix(groupName, name+"/") {
											// Any nested folder under iteration-X is considered a group folder
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

// ActiveWorkflowExecution is the backend half of the workflow/chat decoupling:
// workflow UI state lives here instead of being mirrored into chat session metadata.
type ActiveWorkflowExecution struct {
	QueryID       string    `json:"query_id"`
	SessionID     string    `json:"session_id"`
	PresetQueryID string    `json:"preset_query_id,omitempty"`
	PresetName    string    `json:"preset_name,omitempty"`
	WorkspacePath string    `json:"workspace_path"`
	RunFolder     string    `json:"run_folder,omitempty"`
	PhaseID       string    `json:"phase_id,omitempty"`
	PhaseName     string    `json:"phase_name,omitempty"`
	Status        string    `json:"status,omitempty"` // "running", "completed", "failed"
	UserID        string    `json:"user_id,omitempty"`
	Title         string    `json:"title,omitempty"`
	Query         string    `json:"query,omitempty"`
	TriggeredBy   string    `json:"triggered_by"` // "manual", "cron", "workflow_builder", "workflow_phase"
	StartedAt     time.Time `json:"started_at"`
	// Minimization state — frontend sets this when user minimizes a running workflow.
	IsMinimized      bool   `json:"is_minimized,omitempty"`
	MinimizedAt      int64  `json:"minimized_at,omitempty"` // unix ms
	CurrentStepID    string `json:"current_step_id,omitempty"`
	CurrentStepTitle string `json:"current_step_title,omitempty"`
}

// RunMetadataLLM captures which model was used for a specific role
type RunMetadataLLM struct {
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"model_id,omitempty"`
}

// RunMetadataModels captures the LLM configuration used for the run
type RunMetadataModels struct {
	AllocationMode string          `json:"allocation_mode,omitempty"` // "manual" or "tiered"
	ExecutionLLM   *RunMetadataLLM `json:"execution_llm,omitempty"`
	LearningLLM    *RunMetadataLLM `json:"learning_llm,omitempty"`
	PhaseLLM       *RunMetadataLLM `json:"phase_llm,omitempty"`
	Tier1          *RunMetadataLLM `json:"tier_1,omitempty"`
	Tier2          *RunMetadataLLM `json:"tier_2,omitempty"`
	Tier3          *RunMetadataLLM `json:"tier_3,omitempty"`
	TempOverride   *RunMetadataLLM `json:"temp_override,omitempty"`
	TempOverride2  *RunMetadataLLM `json:"temp_override_2,omitempty"`
}

// RunMetadata stores lifecycle information for a run folder
type RunMetadata struct {
	CreatedAt   time.Time          `json:"created_at"`
	StartedAt   time.Time          `json:"started_at,omitempty"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	DurationMs  *int64             `json:"duration_ms,omitempty"`
	Status      string             `json:"status"`                 // "running", "completed", "failed", "canceled"
	TriggeredBy string             `json:"triggered_by,omitempty"` // "manual", "cron", "workflow_builder"
	Models      *RunMetadataModels `json:"models,omitempty"`       // LLM config used for this run
}

// RunFolderInfo represents information about a single run folder
type RunFolderInfo struct {
	Name     string        `json:"name"`
	Progress *StepProgress `json:"progress,omitempty"` // Progress info if available
	Metadata *RunMetadata  `json:"metadata,omitempty"` // Lifecycle metadata
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
	RunMode           string `json:"run_mode"`                      // "use_same_run" or "create_new_runs_always"
	SelectedRunFolder string `json:"selected_run_folder,omitempty"` // If use_same_run and user selected specific folder
	ExecutionStrategy string `json:"execution_strategy"`            // "start_from_beginning", etc.
	ResumeFromStep    int    `json:"resume_from_step,omitempty"`    // 1-based step number to resume from
	PlanChangeAction  string `json:"plan_change_action,omitempty"`  // "keep_old_progress" or "delete_old_progress"

	// Variable group execution options (for batch execution with multiple groups)
	EnabledGroupNames []string `json:"enabled_group_names,omitempty"` // Group names to execute (if empty, uses groups' enabled flags)

	// Logging options
	SaveValidationResponses bool `json:"save_validation_responses,omitempty"` // If true, save validation responses and execution logs to workspace (default: true)

	// Workshop mode override (builder/optimizer/runner) — sent from frontend toggle
	WorkshopMode string `json:"workshop_mode,omitempty"`
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

// WorkflowUpdateRequest represents a workflow update request
type WorkflowUpdateRequest struct {
	PresetQueryID   string                                 `json:"preset_query_id"`
	WorkflowStatus  *string                                `json:"workflow_status,omitempty"`
	SelectedOptions *workflowtypes.WorkflowSelectedOptions `json:"selected_options,omitempty"`
	StepID          *string                                `json:"step_id,omitempty"` // Optional step ID for step-specific phase execution
}

// --- In-memory workflow runtime state (replaces DB workflows table) ---

// WorkflowRuntimeState holds ephemeral execution state for a workflow.
// This replaces the DB-backed workflows table. State doesn't survive server restarts
// (which is fine — workflow_status is only meaningful during active execution).
type WorkflowRuntimeState struct {
	ID              string                                 `json:"id"`
	PresetQueryID   string                                 `json:"preset_query_id"`
	WorkflowStatus  string                                 `json:"workflow_status"`
	SelectedOptions *workflowtypes.WorkflowSelectedOptions `json:"selected_options,omitempty"`
	CreatedAt       time.Time                              `json:"created_at"`
	UpdatedAt       time.Time                              `json:"updated_at"`
}

// workflowRuntimeStore is the in-memory store for workflow execution state.
// Key: preset_query_id → WorkflowRuntimeState
var workflowRuntimeStore = struct {
	sync.RWMutex
	m map[string]*WorkflowRuntimeState
}{m: make(map[string]*WorkflowRuntimeState)}

func getWorkflowRuntime(presetQueryID string) *WorkflowRuntimeState {
	workflowRuntimeStore.RLock()
	defer workflowRuntimeStore.RUnlock()
	return workflowRuntimeStore.m[presetQueryID]
}

func setWorkflowRuntime(state *WorkflowRuntimeState) {
	workflowRuntimeStore.Lock()
	defer workflowRuntimeStore.Unlock()
	workflowRuntimeStore.m[state.PresetQueryID] = state
}

func deleteWorkflowRuntime(presetQueryID string) {
	workflowRuntimeStore.Lock()
	defer workflowRuntimeStore.Unlock()
	delete(workflowRuntimeStore.m, presetQueryID)
}

// handleCreateWorkflow handles workflow creation (in-memory runtime state).
func (api *StreamingAPI) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req WorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.PresetQueryID == "" {
		http.Error(w, "preset_query_id is required", http.StatusBadRequest)
		return
	}

	// Check if already exists in memory
	if existing := getWorkflowRuntime(req.PresetQueryID); existing != nil {
		http.Error(w, "Workflow already exists for this preset query ID. Use update endpoint instead.", http.StatusConflict)
		return
	}

	status := workflowtypes.WorkflowStatusPreVerification
	if !req.HumanVerificationRequired {
		status = workflowtypes.WorkflowStatusPostVerification
	}

	now := time.Now()
	state := &WorkflowRuntimeState{
		ID:             fmt.Sprintf("wfrt_%d", now.UnixNano()),
		PresetQueryID:  req.PresetQueryID,
		WorkflowStatus: status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	setWorkflowRuntime(state)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"workflow": map[string]interface{}{
			"id":              state.ID,
			"preset_query_id": state.PresetQueryID,
			"workflow_status": state.WorkflowStatus,
			"created_at":      state.CreatedAt,
		},
		"message": "Workflow created successfully",
	})
}

// handleGetWorkflowStatus handles getting workflow status (in-memory runtime state).
func (api *StreamingAPI) handleGetWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	presetQueryID := r.URL.Query().Get("preset_query_id")
	if presetQueryID == "" {
		http.Error(w, "preset_query_id parameter is required", http.StatusBadRequest)
		return
	}

	state := getWorkflowRuntime(presetQueryID)
	if state == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"exists":  false,
			"message": "No workflow exists for this preset",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"exists":  true,
		"workflow": map[string]interface{}{
			"id":               state.ID,
			"preset_query_id":  state.PresetQueryID,
			"workflow_status":  state.WorkflowStatus,
			"selected_options": state.SelectedOptions,
			"created_at":       state.CreatedAt,
			"updated_at":       state.UpdatedAt,
		},
		"status": map[string]interface{}{
			"is_ready":              state.WorkflowStatus == workflowtypes.WorkflowStatusPostVerification,
			"requires_verification": state.WorkflowStatus == workflowtypes.WorkflowStatusPreVerification,
			"can_execute":           state.WorkflowStatus == workflowtypes.WorkflowStatusPostVerification,
		},
	})
}

// handleUpdateWorkflow handles workflow updates (in-memory runtime state).
func (api *StreamingAPI) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req WorkflowUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.PresetQueryID == "" {
		http.Error(w, "preset_query_id is required", http.StatusBadRequest)
		return
	}

	if req.WorkflowStatus == nil && req.SelectedOptions == nil {
		http.Error(w, "at least one field (workflow_status or selected_options) must be provided", http.StatusBadRequest)
		return
	}

	// Store step_id for execution
	if req.StepID != nil && *req.StepID != "" {
		api.workflowStepIDMux.Lock()
		if api.workflowStepIDs == nil {
			api.workflowStepIDs = make(map[string]string)
		}
		api.workflowStepIDs[req.PresetQueryID] = *req.StepID
		api.workflowStepIDMux.Unlock()
	}

	// Get or create in-memory state (upsert)
	state := getWorkflowRuntime(req.PresetQueryID)
	if state == nil {
		state = &WorkflowRuntimeState{
			ID:             fmt.Sprintf("wfrt_%d", time.Now().UnixNano()),
			PresetQueryID:  req.PresetQueryID,
			WorkflowStatus: workflowtypes.WorkflowStatusPreVerification,
			CreatedAt:      time.Now(),
		}
	}

	if req.WorkflowStatus != nil {
		state.WorkflowStatus = *req.WorkflowStatus
	}
	if req.SelectedOptions != nil {
		state.SelectedOptions = req.SelectedOptions
	}
	state.UpdatedAt = time.Now()
	setWorkflowRuntime(state)

	workflowResponse := map[string]interface{}{
		"id":              state.ID,
		"preset_query_id": state.PresetQueryID,
		"workflow_status": state.WorkflowStatus,
		"created_at":      state.CreatedAt,
		"updated_at":      state.UpdatedAt,
	}
	if state.SelectedOptions != nil {
		workflowResponse["selected_options"] = state.SelectedOptions
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"workflow": workflowResponse,
		"message":  "Workflow updated successfully",
	})
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

	resp, err := workspaceHTTPClient.Do(req)
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

		// Only read progress/metadata for the latest N iterations (most likely to be selected)
		if i < maxFoldersWithProgress {
			// Try to read steps_done.json for this folder
			stepsFilePath := workspacePath + "/runs/" + folderName + "/execution/steps_done.json"
			progress, err := readProgressForFolder(r.Context(), stepsFilePath)
			if err == nil && progress != nil {
				folderInfo.Progress = progress
			}

			// Try to read run_metadata.json
			metadataPath := workspacePath + "/runs/" + folderName + "/run_metadata.json"
			metadata, _ := readRunMetadata(r.Context(), metadataPath)

			if metadata == nil && progress != nil {
				// Fallback: infer metadata from progress and token_usage for legacy runs
				metadata = inferRunMetadata(r.Context(), workspacePath, folderName, progress)
				if metadata != nil {
					_ = writeRunMetadata(r.Context(), metadataPath, metadata)
				}
			} else if metadata != nil && metadata.Status == "running" && progress != nil && progress.TotalSteps > 0 && len(progress.CompletedStepIndices) >= progress.TotalSteps {
				completedAt := progress.LastUpdated
				metadata.Status = "completed"
				metadata.CompletedAt = &completedAt
				_ = writeRunMetadata(r.Context(), metadataPath, metadata)
			}

			if metadata != nil {
				folderInfo.Metadata = metadata
			}
		}

		folderInfos = append(folderInfos, folderInfo)
	}

	// Sort by created_at (most recent first) when metadata is available,
	// fallback to iteration number descending
	if len(folderInfos) > 0 {
		sort.Slice(folderInfos, func(i, j int) bool {
			mi := folderInfos[i].Metadata
			mj := folderInfos[j].Metadata
			if mi != nil && mj != nil {
				return mi.CreatedAt.After(mj.CreatedAt)
			}
			if mi != nil {
				return true
			}
			if mj != nil {
				return false
			}
			// Fallback: iteration number descending
			extractIteration := func(name string) int {
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
			return extractIteration(folderInfos[i].Name) > extractIteration(folderInfos[j].Name)
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

// VariableGroup represents a single set of variable values (matches controller type)
type VariableGroup struct {
	Name    string            `json:"name"` // Unique identifier and display label (e.g., "Production", "Staging")
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

	resp, err := workspaceHTTPClient.Do(req)
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

	resp, err := workspaceHTTPClient.Do(req)
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

	resp, err := workspaceHTTPClient.Do(req)
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

	createResp, err := workspaceHTTPClient.Do(createReq)
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

		createResp, err = workspaceHTTPClient.Do(createReq)
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

	// Write run_metadata.json with creation time
	triggeredBy := r.URL.Query().Get("triggered_by")
	if triggeredBy == "" {
		triggeredBy = "manual"
	}
	metadata := &RunMetadata{
		CreatedAt:   time.Now(),
		Status:      "running",
		TriggeredBy: triggeredBy,
	}
	metadataPath := folderPath + "/run_metadata.json"
	if err := writeRunMetadata(r.Context(), metadataPath, metadata); err != nil {
		// Non-fatal: log but don't fail the folder creation
		log.Printf("[WORKFLOW] Warning: failed to write run_metadata.json for %s: %v", newFolderName, err)
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

	resp, err := workspaceHTTPClient.Do(req)
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

	stepID := r.URL.Query().Get("step_id")
	if stepID == "" {
		http.Error(w, "step_id parameter is required", http.StatusBadRequest)
		return
	}

	// Construct learnings folder path: {workspacePath}/learnings/{stepID}
	learningsPath := fmt.Sprintf("%s/learnings/%s", workspacePath, stepID)

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

	resp, err := workspaceHTTPClient.Do(req)
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
			"message": fmt.Sprintf("Learnings folder for step %s does not exist (already deleted)", stepID),
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
		"message": fmt.Sprintf("Successfully deleted learnings for step %s", stepID),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// collectAllStepIDs recursively collects all step IDs from a plan, including branch steps
func collectAllStepIDs(steps []todo_creation_human.PlanStepInterface) []string {
	var stepIDs []string

	for _, step := range steps {
		if stepID := step.GetID(); stepID != "" {
			stepIDs = append(stepIDs, stepID)
		}

		// Handle conditional steps - collect branch step IDs
		if conditionalStep, ok := step.(*todo_creation_human.ConditionalPlanStep); ok {
			if len(conditionalStep.IfTrueSteps) > 0 {
				stepIDs = append(stepIDs, collectAllStepIDs(conditionalStep.IfTrueSteps)...)
			}
			if len(conditionalStep.IfFalseSteps) > 0 {
				stepIDs = append(stepIDs, collectAllStepIDs(conditionalStep.IfFalseSteps)...)
			}
		}

		// Handle todo_task steps - collect sub-agent step IDs from predefined_routes
		if todoTaskStep, ok := step.(*todo_creation_human.TodoTaskPlanStep); ok {
			// Collect sub-agent step IDs from predefined routes
			for _, route := range todoTaskStep.PredefinedRoutes {
				if route.SubAgentStep != nil {
					if subAgentStepID := route.SubAgentStep.GetID(); subAgentStepID != "" {
						stepIDs = append(stepIDs, subAgentStepID)
					}
				}
			}
		}
	}

	return stepIDs
}

// readLearningMetadataForStep reads learning metadata for a specific step ID
func readLearningMetadataForStep(ctx context.Context, workspacePath, stepID string) (map[string]interface{}, error) {
	metadataPath := workspacePath + "/learnings/" + stepID + "/.learning_metadata.json"

	// Use the generic file reading helper
	content, exists, err := readFileFromWorkspace(ctx, metadataPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil // File doesn't exist, not an error
	}

	// Parse metadata JSON into map
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(content), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse learning metadata: %w", err)
	}

	return metadata, nil
}

// handleGetAllStepLearnings handles getting learning metadata for all steps in a plan
func (api *StreamingAPI) handleGetAllStepLearnings(w http.ResponseWriter, r *http.Request) {
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

	// Read plan to get all step IDs
	plan, err := readPlanFromWorkspace(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Read step configs to get agent configs (use_code_execution_mode, lock_learnings, etc.)
	stepConfigs, err := readStepConfigFromWorkspace(r.Context(), workspacePath)
	if err != nil {
		// Log warning but continue - step configs may not exist
		log.Printf("Warning: Failed to read step configs: %v", err)
	}

	// Create a map from step ID to agent configs
	stepConfigMap := make(map[string]*todo_creation_human.AgentConfigs)
	for _, config := range stepConfigs {
		if config.ID != "" && config.AgentConfigs != nil {
			stepConfigMap[config.ID] = config.AgentConfigs
		}
	}

	// Collect all step IDs recursively
	stepIDs := collectAllStepIDs(plan.Steps)

	learningsMap := make(map[string]interface{})
	for _, stepID := range stepIDs {
		metadata, err := readLearningMetadataForStep(r.Context(), workspacePath, stepID)
		if err != nil {
			log.Printf("Warning: Failed to read learning metadata for step %s: %v", stepID, err)
			metadata = nil
		}

		// Merge step config data into metadata
		if agentConfigs, found := stepConfigMap[stepID]; found && agentConfigs != nil {
			if metadata == nil {
				metadata = make(map[string]interface{})
			}
			if agentConfigs.UseCodeExecutionMode != nil {
				metadata["use_code_execution_mode"] = *agentConfigs.UseCodeExecutionMode
			}
			if agentConfigs.LockLearnings != nil {
				metadata["lock_learnings"] = *agentConfigs.LockLearnings
			}
		}

		learningsMap[stepID] = metadata
	}

	// Check for global workflow-level learning metadata
	globalMetadata, err := readLearningMetadataForStep(r.Context(), workspacePath, "_global")
	if err == nil && globalMetadata != nil {
		// Merge global lock status from step config (stored under "_global" key)
		if agentConfigs, found := stepConfigMap["_global"]; found && agentConfigs != nil {
			if agentConfigs.LockLearnings != nil {
				globalMetadata["lock_learnings"] = *agentConfigs.LockLearnings
			}
		}
		learningsMap["_global"] = globalMetadata
	}

	response := map[string]interface{}{
		"success":   true,
		"learnings": learningsMap,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ============================================================================
// Plan and Step Config Backend API Handlers
// ============================================================================

// PlanStepUpdate represents partial updates to a plan step
// All fields are pointers so nil means "not updated" and non-nil means "update this field"
type PlanStepUpdate struct {
	// Common fields (shared by all step types)
	Title               *string                                    `json:"title,omitempty"`
	Description         *string                                    `json:"description,omitempty"`
	SuccessCriteria     *string                                    `json:"success_criteria,omitempty"`
	ContextDependencies *[]string                                  `json:"context_dependencies,omitempty"`
	ContextOutput       *todo_creation_human.FlexibleContextOutput `json:"context_output,omitempty"`

	// Regular step fields
	HasLoop         *bool   `json:"has_loop,omitempty"`
	LoopCondition   *string `json:"loop_condition,omitempty"`
	MaxIterations   *int    `json:"max_iterations,omitempty"`
	LoopDescription *string `json:"loop_description,omitempty"`

	// Conditional step fields
	HasCondition      *bool           `json:"has_condition,omitempty"`
	ConditionQuestion *string         `json:"condition_question,omitempty"`
	ConditionContext  *string         `json:"condition_context,omitempty"`
	IfTrueSteps       json.RawMessage `json:"if_true_steps,omitempty"`  // Will be converted to []PlanStepInterface
	IfFalseSteps      json.RawMessage `json:"if_false_steps,omitempty"` // Will be converted to []PlanStepInterface
	IfTrueNextStepID  *string         `json:"if_true_next_step_id,omitempty"`
	IfFalseNextStepID *string         `json:"if_false_next_step_id,omitempty"`

	// Routing/TodoTask step fields
	NextStepID *string `json:"next_step_id,omitempty"`

	// TodoTask step fields
	PredefinedRoutes *[]todo_creation_human.PlanOrchestrationRoute `json:"predefined_routes,omitempty"`
}

// PlanUpdateRequest represents a request to update a plan step
type PlanUpdateRequest struct {
	WorkspacePath string          `json:"workspace_path"`
	StepID        string          `json:"step_id"`
	Updates       *PlanStepUpdate `json:"updates,omitempty"`
}

// StepConfigUpdateRequest represents a request to update step config
type StepConfigUpdateRequest struct {
	WorkspacePath string                 `json:"workspace_path"`
	StepID        string                 `json:"step_id"`
	AgentConfigs  map[string]interface{} `json:"agent_configs"`
}

// BatchUpdateRequest represents a batch update request
type BatchUpdateRequest struct {
	WorkspacePath string       `json:"workspace_path"`
	Updates       []StepUpdate `json:"updates"`
}

// StepUpdate represents a single step update in batch request
type StepUpdate struct {
	StepID        string                 `json:"step_id"`
	PlanUpdates   *PlanStepUpdate        `json:"plan_updates,omitempty"`
	ConfigUpdates map[string]interface{} `json:"config_updates,omitempty"`
}

// DeleteStepRequest represents a request to delete a step
type DeleteStepRequest struct {
	WorkspacePath string `json:"workspace_path"`
	StepID        string `json:"step_id"`
}

// AddStepRequest represents a request to add a new step
type AddStepRequest struct {
	WorkspacePath     string                 `json:"workspace_path"`
	Step              map[string]interface{} `json:"step"`
	InsertAfterStepID string                 `json:"insert_after_step_id,omitempty"`
	ParentStepID      string                 `json:"parent_step_id,omitempty"`
	BranchType        string                 `json:"branch_type,omitempty"` // "if_true" or "if_false"
}

// readPlanFromWorkspace reads plan.json from workspace using workspace API
func readPlanFromWorkspace(ctx context.Context, workspacePath string) (*todo_creation_human.PlanningResponse, error) {
	planPath := workspacePath + "/planning/plan.json"

	// URL-encode the filepath segments
	pathSegments := strings.Split(planPath, "/")
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

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plan.json not found")
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

	// Extract content
	var planContent string
	if fileContent, ok := apiResp.Data.(virtualtools.WorkspaceFileContent); ok {
		planContent = fileContent.Content
	} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		if content, ok := dataMap["content"].(string); ok {
			planContent = content
		}
	}

	if planContent == "" {
		return nil, fmt.Errorf("failed to extract plan content from API response")
	}

	// Parse plan
	var plan todo_creation_human.PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan.json: %w", err)
	}

	return &plan, nil
}

// writePlanToWorkspace writes plan.json to workspace using workspace API
func writePlanToWorkspace(ctx context.Context, workspacePath string, plan *todo_creation_human.PlanningResponse) error {
	planPath := workspacePath + "/planning/plan.json"

	// Marshal plan to JSON
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(planPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Prepare request body
	requestBody := map[string]interface{}{
		"content": string(planJSON),
	}
	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Write file via workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// readStepConfigFromWorkspace reads step_config.json from workspace using workspace API
func readStepConfigFromWorkspace(ctx context.Context, workspacePath string) ([]todo_creation_human.StepConfig, error) {
	configPath := workspacePath + "/planning/step_config.json"

	// URL-encode the filepath segments
	pathSegments := strings.Split(configPath, "/")
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

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// File doesn't exist - return empty array
	if resp.StatusCode == http.StatusNotFound {
		return []todo_creation_human.StepConfig{}, nil
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

	// Extract content
	var configContent string
	if fileContent, ok := apiResp.Data.(virtualtools.WorkspaceFileContent); ok {
		configContent = fileContent.Content
	} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		if content, ok := dataMap["content"].(string); ok {
			configContent = content
		}
	}

	if configContent == "" {
		return []todo_creation_human.StepConfig{}, nil
	}

	// Parse step config
	configs, err := todo_creation_human.ParseStepConfigContent(configContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse step_config.json: %w", err)
	}

	return configs, nil
}

// writeStepConfigToWorkspace writes step_config.json to workspace using workspace API
func writeStepConfigToWorkspace(ctx context.Context, workspacePath string, configs []todo_creation_human.StepConfig) error {
	configPath := workspacePath + "/planning/step_config.json"

	// Create config file in object format
	configFile := todo_creation_human.StepConfigFile{
		Steps: configs,
	}

	// Marshal to JSON
	configJSON, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal step_config.json: %w", err)
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(configPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Prepare request body
	requestBody := map[string]interface{}{
		"content": string(configJSON),
	}
	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Write file via workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// findInStepsWithOffset recursively searches nested steps within a step, handling ConditionalPlanStep offsets correctly
func findInStepsWithOffset(step todo_creation_human.PlanStepInterface, targetID string, basePath []int, findInSteps func([]todo_creation_human.PlanStepInterface, string, []int) (todo_creation_human.PlanStepInterface, []int)) (todo_creation_human.PlanStepInterface, []int) {
	switch s := step.(type) {
	case *todo_creation_human.ConditionalPlanStep:
		// Check nested if_true_steps
		if found, foundPath := findInSteps(s.IfTrueSteps, targetID, basePath); found != nil {
			return found, foundPath
		}
		// Check nested if_false_steps with offset
		for k, nestedFalseStep := range s.IfFalseSteps {
			nestedFalseIndex := len(s.IfTrueSteps) + k
			nestedFalsePath := append(basePath, nestedFalseIndex)
			if nestedFalseStep.GetID() == targetID {
				return nestedFalseStep, nestedFalsePath
			}
			// Continue recursion for deeper nesting
			if found, foundPath := findInStepsWithOffset(nestedFalseStep, targetID, nestedFalsePath, findInSteps); found != nil {
				return found, foundPath
			}
		}
	case *todo_creation_human.TodoTaskPlanStep:
		for routeIdx, route := range s.PredefinedRoutes {
			if route.SubAgentStep != nil {
				if route.SubAgentStep.GetID() == targetID {
					return route.SubAgentStep, append(basePath, -4, routeIdx)
				}
				if found, foundPath := findInSteps([]todo_creation_human.PlanStepInterface{route.SubAgentStep}, targetID, basePath); found != nil {
					return found, foundPath
				}
			}
		}
	}
	return nil, nil
}

// findStepInPlan recursively finds a step by ID in the plan
// Returns the step and a path to it (indices for nested steps)
func findStepInPlan(plan *todo_creation_human.PlanningResponse, stepID string) (todo_creation_human.PlanStepInterface, []int) {
	var findInSteps func(steps []todo_creation_human.PlanStepInterface, targetID string, path []int) (todo_creation_human.PlanStepInterface, []int)

	findInSteps = func(steps []todo_creation_human.PlanStepInterface, targetID string, path []int) (todo_creation_human.PlanStepInterface, []int) {
		for i, step := range steps {
			currentPath := append(path, i)

			if step.GetID() == targetID {
				return step, currentPath
			}

			// Check nested steps based on step type
			switch s := step.(type) {
			case *todo_creation_human.ConditionalPlanStep:
				// Check if_true_steps
				if found, foundPath := findInSteps(s.IfTrueSteps, targetID, currentPath); found != nil {
					return found, foundPath
				}
				// Check if_false_steps with offset to avoid index overlap
				// False branch indices are offset by len(IfTrueSteps) to match updateNestedStepInPlanRecursive expectations
				for j, falseStep := range s.IfFalseSteps {
					falseIndex := len(s.IfTrueSteps) + j
					falsePath := append(currentPath, falseIndex)
					if falseStep.GetID() == targetID {
						return falseStep, falsePath
					}
					// Recursively check nested steps in false branch using findInSteps with offset handling
					// We need to search nested steps but ensure paths are built correctly
					if found, foundPath := findInStepsWithOffset(falseStep, targetID, falsePath, findInSteps); found != nil {
						return found, foundPath
					}
				}
			case *todo_creation_human.TodoTaskPlanStep:
				// Check predefined_routes
				for j, route := range s.PredefinedRoutes {
					if route.SubAgentStep != nil {
						if route.SubAgentStep.GetID() == targetID {
							return route.SubAgentStep, append(currentPath, -4, j) // -4 indicates predefined_routes
						}
						// Recursively check nested steps in sub_agent_step
						if found, foundPath := findInSteps([]todo_creation_human.PlanStepInterface{route.SubAgentStep}, targetID, currentPath); found != nil {
							return found, foundPath
						}
					}
				}
			}
		}
		return nil, nil
	}

	return findInSteps(plan.Steps, stepID, []int{})
}

// updateStepInPlan applies partial updates to a step in the plan
func updateStepInPlan(plan *todo_creation_human.PlanningResponse, stepID string, updates *PlanStepUpdate) error {
	if updates == nil {
		return fmt.Errorf("updates cannot be nil")
	}

	step, path := findStepInPlan(plan, stepID)
	if step == nil {
		return fmt.Errorf("step with ID %q not found", stepID)
	}

	// Apply updates based on step type
	var updatedStep todo_creation_human.PlanStepInterface
	switch s := step.(type) {
	case *todo_creation_human.RegularPlanStep:
		updated := *s // Copy the step
		applyCommonFields(&updated.CommonStepFields, updates)
		if updates.HasLoop != nil {
			updated.HasLoop = *updates.HasLoop
		}
		if updates.LoopCondition != nil {
			updated.LoopCondition = *updates.LoopCondition
		}
		if updates.MaxIterations != nil {
			updated.MaxIterations = *updates.MaxIterations
		}
		if updates.LoopDescription != nil {
			updated.LoopDescription = *updates.LoopDescription
		}
		updatedStep = &updated

	case *todo_creation_human.ConditionalPlanStep:
		updated := *s // Copy the step
		applyCommonFields(&updated.CommonStepFields, updates)
		if updates.ConditionQuestion != nil {
			updated.ConditionQuestion = *updates.ConditionQuestion
		}
		if updates.ConditionContext != nil {
			updated.ConditionContext = *updates.ConditionContext
		}
		if updates.IfTrueNextStepID != nil {
			updated.IfTrueNextStepID = *updates.IfTrueNextStepID
		}
		if updates.IfFalseNextStepID != nil {
			updated.IfFalseNextStepID = *updates.IfFalseNextStepID
		}
		// Handle nested steps
		if len(updates.IfTrueSteps) > 0 {
			var ifTrueArray []json.RawMessage
			if err := json.Unmarshal(updates.IfTrueSteps, &ifTrueArray); err != nil {
				return fmt.Errorf("failed to unmarshal if_true_steps array: %w", err)
			}
			steps, err := unmarshalStepsFromJSON(ifTrueArray)
			if err != nil {
				return fmt.Errorf("failed to unmarshal if_true_steps: %w", err)
			}
			updated.IfTrueSteps = steps
		}
		if len(updates.IfFalseSteps) > 0 {
			var ifFalseArray []json.RawMessage
			if err := json.Unmarshal(updates.IfFalseSteps, &ifFalseArray); err != nil {
				return fmt.Errorf("failed to unmarshal if_false_steps array: %w", err)
			}
			steps, err := unmarshalStepsFromJSON(ifFalseArray)
			if err != nil {
				return fmt.Errorf("failed to unmarshal if_false_steps: %w", err)
			}
			updated.IfFalseSteps = steps
		}
		updatedStep = &updated

	case *todo_creation_human.TodoTaskPlanStep:
		updated := *s // Copy the step
		// TodoTaskPlanStep only has ID and Title (no embedded CommonStepFields)
		if updates.Title != nil {
			updated.Title = *updates.Title
		}
		if updates.NextStepID != nil {
			updated.NextStepID = *updates.NextStepID
		}
		if updates.PredefinedRoutes != nil {
			updated.PredefinedRoutes = *updates.PredefinedRoutes
		}
		updatedStep = &updated

	default:
		return fmt.Errorf("unknown step type: %T", step)
	}

	// Update step in plan using path
	if len(path) == 1 {
		// Top-level step
		plan.Steps[path[0]] = updatedStep
	} else {
		// Nested step - need to navigate and update
		return updateNestedStepInPlan(plan, path, updatedStep)
	}

	return nil
}

// applyCommonFields applies common field updates to CommonStepFields
func applyCommonFields(common *todo_creation_human.CommonStepFields, updates *PlanStepUpdate) {
	if updates.Title != nil {
		common.Title = *updates.Title
	}
	if updates.Description != nil {
		common.Description = *updates.Description
	}
	if updates.SuccessCriteria != nil {
		common.SuccessCriteria = *updates.SuccessCriteria
	}
	if updates.ContextDependencies != nil {
		common.ContextDependencies = *updates.ContextDependencies
	}
	if updates.ContextOutput != nil {
		common.ContextOutput = *updates.ContextOutput
	}
}

// unmarshalStepFromJSON unmarshals a single step from JSON
func unmarshalStepFromJSON(data json.RawMessage) (todo_creation_human.PlanStepInterface, error) {
	// First, determine the step type
	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &typeCheck); err != nil {
		return nil, fmt.Errorf("failed to determine step type: %w", err)
	}

	switch typeCheck.Type {
	case "regular":
		var s todo_creation_human.RegularPlanStep
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal regular step: %w", err)
		}
		return &s, nil
	case "conditional":
		var s todo_creation_human.ConditionalPlanStep
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal conditional step: %w", err)
		}
		return &s, nil
	case "todo_task":
		var s todo_creation_human.TodoTaskPlanStep
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal todo_task step: %w", err)
		}
		return &s, nil
	default:
		return nil, fmt.Errorf("unknown step type: %s", typeCheck.Type)
	}
}

// unmarshalStepsFromJSON unmarshals an array of steps from JSON
func unmarshalStepsFromJSON(data []json.RawMessage) ([]todo_creation_human.PlanStepInterface, error) {
	steps := make([]todo_creation_human.PlanStepInterface, 0, len(data))
	for i, raw := range data {
		step, err := unmarshalStepFromJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal step at index %d: %w", i, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// updateNestedStepInPlan updates a nested step using its path
func updateNestedStepInPlan(plan *todo_creation_human.PlanningResponse, path []int, updatedStep todo_creation_human.PlanStepInterface) error {
	if len(path) < 2 {
		return fmt.Errorf("invalid path for nested step")
	}

	// Navigate to parent step
	parentStep := plan.Steps[path[0]]

	// Handle different step types
	switch s := parentStep.(type) {
	case *todo_creation_human.ConditionalPlanStep:
		if len(path) == 2 {
			// Direct child in branch
			branchIndex := path[1]
			// Determine which branch (need to check if_true or if_false)
			// For now, we'll need to search both branches
			// This is a limitation - we'd need to track branch type in path
			// For simplicity, try if_true first, then if_false
			if branchIndex < len(s.IfTrueSteps) {
				s.IfTrueSteps[branchIndex] = updatedStep
				return nil
			} else if branchIndex < len(s.IfTrueSteps)+len(s.IfFalseSteps) {
				s.IfFalseSteps[branchIndex-len(s.IfTrueSteps)] = updatedStep
				return nil
			}
			return fmt.Errorf("invalid branch index")
		}
		// Deeper nesting - recursively update
		return updateNestedStepInPlanRecursive(s.IfTrueSteps, s.IfFalseSteps, path[1:], updatedStep)
	case *todo_creation_human.TodoTaskPlanStep:
		if len(path) >= 3 && path[1] == -4 {
			// predefined_routes[path[2]].sub_agent_step
			routeIndex := path[2]
			if routeIndex >= 0 && routeIndex < len(s.PredefinedRoutes) {
				if len(path) == 3 {
					s.PredefinedRoutes[routeIndex].SubAgentStep = updatedStep
					return nil
				}
				// Deeper nesting in sub_agent_step
				return updateNestedStepInPlanRecursive([]todo_creation_human.PlanStepInterface{s.PredefinedRoutes[routeIndex].SubAgentStep}, nil, path[2:], updatedStep)
			}
		}
	}

	return fmt.Errorf("failed to update nested step")
}

// updateNestedStepInPlanRecursive recursively updates nested steps
func updateNestedStepInPlanRecursive(ifTrueSteps, ifFalseSteps []todo_creation_human.PlanStepInterface, path []int, updatedStep todo_creation_human.PlanStepInterface) error {
	if len(path) == 0 {
		return fmt.Errorf("empty path")
	}

	index := path[0]

	// Try if_true_steps first
	if index >= 0 && index < len(ifTrueSteps) {
		if len(path) == 1 {
			ifTrueSteps[index] = updatedStep
			return nil
		}
		// Deeper nesting
		step := ifTrueSteps[index]
		switch s := step.(type) {
		case *todo_creation_human.ConditionalPlanStep:
			return updateNestedStepInPlanRecursive(s.IfTrueSteps, s.IfFalseSteps, path[1:], updatedStep)
		case *todo_creation_human.TodoTaskPlanStep:
			if len(path) >= 3 && path[1] == -4 {
				routeIndex := path[2]
				if routeIndex >= 0 && routeIndex < len(s.PredefinedRoutes) {
					if len(path) == 3 {
						s.PredefinedRoutes[routeIndex].SubAgentStep = updatedStep
						return nil
					}
					return updateNestedStepInPlanRecursive([]todo_creation_human.PlanStepInterface{s.PredefinedRoutes[routeIndex].SubAgentStep}, nil, path[3:], updatedStep)
				}
			}
		}
	}

	// Try if_false_steps
	if ifFalseSteps != nil {
		adjustedIndex := index - len(ifTrueSteps)
		if adjustedIndex >= 0 && adjustedIndex < len(ifFalseSteps) {
			if len(path) == 1 {
				ifFalseSteps[adjustedIndex] = updatedStep
				return nil
			}
			// Deeper nesting
			step := ifFalseSteps[adjustedIndex]
			switch s := step.(type) {
			case *todo_creation_human.ConditionalPlanStep:
				return updateNestedStepInPlanRecursive(s.IfTrueSteps, s.IfFalseSteps, path[1:], updatedStep)
			case *todo_creation_human.TodoTaskPlanStep:
				if len(path) >= 3 && path[1] == -4 {
					routeIndex := path[2]
					if routeIndex >= 0 && routeIndex < len(s.PredefinedRoutes) {
						if len(path) == 3 {
							s.PredefinedRoutes[routeIndex].SubAgentStep = updatedStep
							return nil
						}
						return updateNestedStepInPlanRecursive([]todo_creation_human.PlanStepInterface{s.PredefinedRoutes[routeIndex].SubAgentStep}, nil, path[3:], updatedStep)
					}
				}
			}
		}
	}

	return fmt.Errorf("failed to find step at path")
}

// handleUpdatePlanStep handles updating a plan step
func (api *StreamingAPI) handleUpdatePlanStep(w http.ResponseWriter, r *http.Request) {
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

	var req PlanUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	if req.StepID == "" {
		http.Error(w, "step_id is required", http.StatusBadRequest)
		return
	}
	if req.Updates == nil {
		http.Error(w, "updates is required", http.StatusBadRequest)
		return
	}

	// Read plan
	plan, err := readPlanFromWorkspace(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Update step
	if err := updateStepInPlan(plan, req.StepID, req.Updates); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update step: %v", err), http.StatusBadRequest)
		return
	}

	// Write updated plan
	if err := writePlanToWorkspace(r.Context(), req.WorkspacePath, plan); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Find updated step to return
	updatedStep, _ := findStepInPlan(plan, req.StepID)
	if updatedStep == nil {
		http.Error(w, "Step not found after update", http.StatusInternalServerError)
		return
	}

	// Convert step to JSON for response
	stepJSON, err := json.Marshal(updatedStep)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal step: %v", err), http.StatusInternalServerError)
		return
	}

	var stepMap map[string]interface{}
	if err := json.Unmarshal(stepJSON, &stepMap); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unmarshal step: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Step updated successfully",
		"data": map[string]interface{}{
			"step": stepMap,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleUpdateStepConfig handles updating step config (agent_configs)
func (api *StreamingAPI) handleUpdateStepConfig(w http.ResponseWriter, r *http.Request) {
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

	var req StepConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	if req.StepID == "" {
		http.Error(w, "step_id is required", http.StatusBadRequest)
		return
	}

	// Read step configs
	configs, err := readStepConfigFromWorkspace(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read step configs: %v", err), http.StatusInternalServerError)
		return
	}

	// Find existing config or create new one
	existingIndex := -1
	for i, config := range configs {
		if config.ID == req.StepID {
			existingIndex = i
			break
		}
	}

	// Convert agent_configs map to AgentConfigs struct
	var agentConfigs *todo_creation_human.AgentConfigs
	if req.AgentConfigs != nil && len(req.AgentConfigs) > 0 {
		// Marshal to JSON and unmarshal to typed struct
		configJSON, err := json.Marshal(req.AgentConfigs)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to marshal agent_configs: %v", err), http.StatusBadRequest)
			return
		}
		var ac todo_creation_human.AgentConfigs
		if err := json.Unmarshal(configJSON, &ac); err != nil {
			http.Error(w, fmt.Sprintf("Failed to unmarshal agent_configs: %v", err), http.StatusBadRequest)
			return
		}
		agentConfigs = &ac
	}

	if existingIndex >= 0 {
		// Update existing config
		if agentConfigs != nil {
			// Merge with existing config (partial update)
			existingConfig := configs[existingIndex]
			if existingConfig.AgentConfigs != nil {
				// Deep merge - convert both to maps, merge, then convert back
				existingJSON, _ := json.Marshal(existingConfig.AgentConfigs)
				newJSON, _ := json.Marshal(agentConfigs)
				var existingMap, newMap map[string]interface{}
				json.Unmarshal(existingJSON, &existingMap)
				json.Unmarshal(newJSON, &newMap)
				// Merge maps
				for k, v := range newMap {
					if v != nil {
						existingMap[k] = v
					}
				}
				// Convert back to AgentConfigs
				mergedJSON, _ := json.Marshal(existingMap)
				json.Unmarshal(mergedJSON, &agentConfigs)
			}
			configs[existingIndex] = todo_creation_human.StepConfig{
				ID:           req.StepID,
				AgentConfigs: agentConfigs,
			}
		} else {
			// Remove config if agentConfigs is nil
			configs = append(configs[:existingIndex], configs[existingIndex+1:]...)
		}
	} else {
		// Add new config
		if agentConfigs != nil {
			configs = append(configs, todo_creation_human.StepConfig{
				ID:           req.StepID,
				AgentConfigs: agentConfigs,
			})
		}
	}

	// Write updated configs
	if err := writeStepConfigToWorkspace(r.Context(), req.WorkspacePath, configs); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write step config: %v", err), http.StatusInternalServerError)
		return
	}

	// If manually unlocking (lock_learnings = false), also clear auto-lock fields from metadata
	if agentConfigs != nil && agentConfigs.LockLearnings != nil && !*agentConfigs.LockLearnings {
		metadataPath := req.WorkspacePath + "/learnings/" + req.StepID + "/.learning_metadata.json"
		if content, exists, err := readFileFromWorkspace(r.Context(), metadataPath); err == nil && exists {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(content), &metadata); err == nil {
				metadata["auto_locked_at"] = ""
				metadata["auto_lock_reason"] = ""
				metadata["auto_lock_iteration"] = 0
				if metadataJSON, err := json.MarshalIndent(metadata, "", "  "); err == nil {
					_ = writeFileToWorkspace(r.Context(), metadataPath, string(metadataJSON))
				}
			}
		}
	}

	// Return updated config
	responseConfig := map[string]interface{}{}
	if agentConfigs != nil {
		configJSON, _ := json.Marshal(agentConfigs)
		json.Unmarshal(configJSON, &responseConfig)
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Step config updated successfully",
		"data": map[string]interface{}{
			"step_id":       req.StepID,
			"agent_configs": responseConfig,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleBatchUpdateSteps handles batch updating multiple steps
func (api *StreamingAPI) handleBatchUpdateSteps(w http.ResponseWriter, r *http.Request) {
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

	var req BatchUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	if len(req.Updates) == 0 {
		http.Error(w, "updates array cannot be empty", http.StatusBadRequest)
		return
	}

	// Read plan and configs
	plan, err := readPlanFromWorkspace(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read plan: %v", err), http.StatusInternalServerError)
		return
	}

	configs, err := readStepConfigFromWorkspace(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read step configs: %v", err), http.StatusInternalServerError)
		return
	}

	updatedStepsCount := 0
	updatedConfigsCount := 0
	var errors []map[string]interface{}

	// Process each update
	for _, update := range req.Updates {
		if update.StepID == "" {
			errors = append(errors, map[string]interface{}{
				"step_id": "",
				"error":   "step_id is required",
			})
			continue
		}

		// Update plan if needed
		if update.PlanUpdates != nil {
			if err := updateStepInPlan(plan, update.StepID, update.PlanUpdates); err != nil {
				errors = append(errors, map[string]interface{}{
					"step_id": update.StepID,
					"error":   fmt.Sprintf("failed to update plan: %v", err),
				})
			} else {
				updatedStepsCount++
			}
		}

		// Update config if needed
		if update.ConfigUpdates != nil && len(update.ConfigUpdates) > 0 {
			// Find or create config
			existingIndex := -1
			for i, config := range configs {
				if config.ID == update.StepID {
					existingIndex = i
					break
				}
			}

			// Convert to AgentConfigs
			configJSON, _ := json.Marshal(update.ConfigUpdates)
			var agentConfigs todo_creation_human.AgentConfigs
			if err := json.Unmarshal(configJSON, &agentConfigs); err != nil {
				errors = append(errors, map[string]interface{}{
					"step_id": update.StepID,
					"error":   fmt.Sprintf("failed to parse config updates: %v", err),
				})
			} else {
				if existingIndex >= 0 {
					// Merge with existing
					if configs[existingIndex].AgentConfigs != nil {
						existingJSON, _ := json.Marshal(configs[existingIndex].AgentConfigs)
						newJSON, _ := json.Marshal(&agentConfigs)
						var existingMap, newMap map[string]interface{}
						json.Unmarshal(existingJSON, &existingMap)
						json.Unmarshal(newJSON, &newMap)
						for k, v := range newMap {
							if v != nil {
								existingMap[k] = v
							}
						}
						mergedJSON, _ := json.Marshal(existingMap)
						json.Unmarshal(mergedJSON, &agentConfigs)
					}
					configs[existingIndex] = todo_creation_human.StepConfig{
						ID:           update.StepID,
						AgentConfigs: &agentConfigs,
					}
				} else {
					configs = append(configs, todo_creation_human.StepConfig{
						ID:           update.StepID,
						AgentConfigs: &agentConfigs,
					})
				}
				updatedConfigsCount++
			}
		}
	}

	// Write both files
	if updatedStepsCount > 0 {
		if err := writePlanToWorkspace(r.Context(), req.WorkspacePath, plan); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write plan: %v", err), http.StatusInternalServerError)
			return
		}
	}

	if updatedConfigsCount > 0 {
		if err := writeStepConfigToWorkspace(r.Context(), req.WorkspacePath, configs); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write step config: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Determine success status: true if at least some updates succeeded, even if some failed
	success := updatedStepsCount > 0 || updatedConfigsCount > 0 || len(errors) == 0
	message := "Batch update completed"
	if len(errors) > 0 {
		if updatedStepsCount > 0 || updatedConfigsCount > 0 {
			message = fmt.Sprintf("Batch update completed with %d error(s)", len(errors))
		} else {
			message = fmt.Sprintf("Batch update failed: %d error(s)", len(errors))
		}
	}

	response := map[string]interface{}{
		"success": success,
		"message": message,
		"data": map[string]interface{}{
			"updated_steps":   updatedStepsCount,
			"updated_configs": updatedConfigsCount,
		},
	}

	// Include errors in response if any occurred
	if len(errors) > 0 {
		response["data"].(map[string]interface{})["errors"] = errors
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDeleteStep handles deleting a step from plan and config
func (api *StreamingAPI) handleDeleteStep(w http.ResponseWriter, r *http.Request) {
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

	var req DeleteStepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	if req.StepID == "" {
		http.Error(w, "step_id is required", http.StatusBadRequest)
		return
	}

	// Read plan
	plan, err := readPlanFromWorkspace(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Find and remove step from plan
	// For simplicity, we'll search and remove from top-level steps only
	// Nested step deletion would require more complex logic
	removedFromPlan := false
	for i, step := range plan.Steps {
		if step.GetID() == req.StepID {
			plan.Steps = append(plan.Steps[:i], plan.Steps[i+1:]...)
			removedFromPlan = true
			break
		}
	}

	// Write updated plan if step was removed
	if removedFromPlan {
		if err := writePlanToWorkspace(r.Context(), req.WorkspacePath, plan); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write plan: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Read and update configs
	configs, err := readStepConfigFromWorkspace(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read step configs: %v", err), http.StatusInternalServerError)
		return
	}

	// Remove config if exists
	removedFromConfig := false
	for i, config := range configs {
		if config.ID == req.StepID {
			configs = append(configs[:i], configs[i+1:]...)
			removedFromConfig = true
			break
		}
	}

	// Write updated configs if config was removed
	if removedFromConfig {
		if err := writeStepConfigToWorkspace(r.Context(), req.WorkspacePath, configs); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write step config: %v", err), http.StatusInternalServerError)
			return
		}
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Step deleted successfully",
		"data": map[string]interface{}{
			"deleted_step_id": req.StepID,
			"deleted_config":  removedFromConfig,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAddStep handles adding a new step to the plan
func (api *StreamingAPI) handleAddStep(w http.ResponseWriter, r *http.Request) {
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

	var req AddStepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	if req.Step == nil {
		http.Error(w, "step is required", http.StatusBadRequest)
		return
	}

	// Read plan
	plan, err := readPlanFromWorkspace(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Remove agent_configs from step if present (validation)
	delete(req.Step, "agent_configs")

	// Determine step type and unmarshal to typed step
	stepType, ok := req.Step["type"].(string)
	if !ok {
		http.Error(w, "step must have 'type' field (regular, conditional, human_input, todo_task, or routing)", http.StatusBadRequest)
		return
	}

	// Marshal step map to JSON and unmarshal to typed step
	stepJSON, err := json.Marshal(req.Step)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal step: %v", err), http.StatusBadRequest)
		return
	}

	var newStep todo_creation_human.PlanStepInterface
	switch stepType {
	case "regular":
		var s todo_creation_human.RegularPlanStep
		if err := json.Unmarshal(stepJSON, &s); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse regular step: %v", err), http.StatusBadRequest)
			return
		}
		newStep = &s
	case "conditional":
		var s todo_creation_human.ConditionalPlanStep
		if err := json.Unmarshal(stepJSON, &s); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse conditional step: %v", err), http.StatusBadRequest)
			return
		}
		newStep = &s
	case "todo_task":
		var s todo_creation_human.TodoTaskPlanStep
		if err := json.Unmarshal(stepJSON, &s); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse todo_task step: %v", err), http.StatusBadRequest)
			return
		}
		newStep = &s
	default:
		http.Error(w, fmt.Sprintf("Unknown step type: %s", stepType), http.StatusBadRequest)
		return
	}

	// Insert step at appropriate position
	if req.InsertAfterStepID != "" {
		// Find step to insert after
		for i, step := range plan.Steps {
			if step.GetID() == req.InsertAfterStepID {
				// Insert after this step
				plan.Steps = append(plan.Steps[:i+1], append([]todo_creation_human.PlanStepInterface{newStep}, plan.Steps[i+1:]...)...)
				break
			}
		}
	} else if req.ParentStepID != "" {
		// Insert into nested step (conditional branch)
		parentStep, _ := findStepInPlan(plan, req.ParentStepID)
		if parentStep == nil {
			http.Error(w, fmt.Sprintf("Parent step %q not found", req.ParentStepID), http.StatusBadRequest)
			return
		}

		if conditionalStep, ok := parentStep.(*todo_creation_human.ConditionalPlanStep); ok {
			if req.BranchType == "if_true" {
				conditionalStep.IfTrueSteps = append(conditionalStep.IfTrueSteps, newStep)
			} else if req.BranchType == "if_false" {
				conditionalStep.IfFalseSteps = append(conditionalStep.IfFalseSteps, newStep)
			} else {
				http.Error(w, "branch_type must be 'if_true' or 'if_false' when parent_step_id is provided", http.StatusBadRequest)
				return
			}
		} else {
			http.Error(w, "Parent step must be a conditional step for nested insertion", http.StatusBadRequest)
			return
		}
	} else {
		// Add to end
		plan.Steps = append(plan.Steps, newStep)
	}

	// Write updated plan
	if err := writePlanToWorkspace(r.Context(), req.WorkspacePath, plan); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert step to JSON for response
	stepResponseJSON, err := json.Marshal(newStep)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal step: %v", err), http.StatusInternalServerError)
		return
	}

	var stepMap map[string]interface{}
	if err := json.Unmarshal(stepResponseJSON, &stepMap); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unmarshal step: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Step added successfully",
		"data": map[string]interface{}{
			"step": stepMap,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetExecutionLogs handles getting execution logs for a workflow run
func (api *StreamingAPI) handleGetExecutionLogs(w http.ResponseWriter, r *http.Request) {
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

	// Validate workspace path to prevent path traversal attacks
	cleanedWorkspacePath := filepath.Clean(workspacePath)
	if strings.Contains(cleanedWorkspacePath, "..") {
		http.Error(w, "Invalid workspace path", http.StatusBadRequest)
		return
	}

	runFolder := r.URL.Query().Get("run_folder")

	// Validate run folder to prevent path traversal
	if runFolder != "" && runFolder != "new" {
		cleanedRunFolder := filepath.Clean(runFolder)
		if strings.Contains(cleanedRunFolder, "..") {
			http.Error(w, "Invalid run folder", http.StatusBadRequest)
			return
		}
		runFolder = cleanedRunFolder
	}

	// Run folder layout used by the workflow runtime:
	//   runs/{iteration}/{group}/logs/        ← logs per group, e.g. runs/iteration-0/xspaces/logs/
	//   runs/{iteration}/{group}/execution/   ← output artifacts per group
	//
	// Callers today typically pass runFolder as just "iteration-N"; older single-group
	// workflows used to write directly under "runs/{iteration}/logs" and "runs/{iteration}/execution".
	// Support both: if runFolder points at an iteration and the naive paths don't exist,
	// pick the first group subdir automatically so iteration-0 / latest queries Just Work.
	// Callers that want a specific group can pass runFolder="iteration-N/{group}".
	var logsBasePath string
	var executionBasePath string
	resolvedGroup := ""
	if runFolder != "" && runFolder != "new" {
		logsBasePath = fmt.Sprintf("%s/runs/%s/logs", cleanedWorkspacePath, runFolder)
		executionBasePath = fmt.Sprintf("%s/runs/%s/execution", cleanedWorkspacePath, runFolder)

		// If the naive per-iteration paths don't exist but group subdirs do,
		// transparently resolve to the first group under this iteration.
		if !strings.Contains(runFolder, "/") {
			if !workspacePathExists(r.Context(), logsBasePath) && !workspacePathExists(r.Context(), executionBasePath) {
				iterationRoot := fmt.Sprintf("%s/runs/%s", cleanedWorkspacePath, runFolder)
				if groups := listGroupSubdirs(r.Context(), iterationRoot); len(groups) > 0 {
					resolvedGroup = groups[0]
					logsBasePath = fmt.Sprintf("%s/%s/logs", iterationRoot, resolvedGroup)
					executionBasePath = fmt.Sprintf("%s/%s/execution", iterationRoot, resolvedGroup)
				}
			}
		}
	} else {
		logsBasePath = fmt.Sprintf("%s/logs", cleanedWorkspacePath)
		executionBasePath = fmt.Sprintf("%s/execution", cleanedWorkspacePath)
	}
	_ = resolvedGroup // reserved for future multi-group response shape

	// Fetch workflow definition to get step titles and descriptions
	// We try to read planning/plan.json to map step IDs to human-readable titles
	// This uses generic parsing to handle different step types (regular, decision, etc.)
	planJsonPath := cleanedWorkspacePath + "/planning/plan.json"
	planContent, exists, _ := readFileFromWorkspace(r.Context(), planJsonPath)

	stepMetadata := make(map[string]map[string]string)
	if exists {
		var planDef struct {
			Steps []map[string]interface{} `json:"steps"`
		}
		if err := json.Unmarshal([]byte(planContent), &planDef); err == nil {
			populateStepMetadata(planDef.Steps, "", stepMetadata)
		}
	}

	// Typed response structure for folder listing
	type FolderListingResponse struct {
		Success bool                                `json:"success"`
		Message string                              `json:"message"`
		Error   string                              `json:"error"`
		Data    virtualtools.WorkspaceFolderListing `json:"data"`
	}

	// 1. Fetch Logs Folder Listing
	// List files in logs folder recursively (max_depth=3 to get step/validation.json and step/execution/exec.json)
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	q := req.URL.Query()
	q.Add("folder", logsBasePath)
	q.Add("max_depth", "3")
	req.URL.RawQuery = q.Encode()

	resp, err := workspaceHTTPClient.Do(req)
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

	var logsResp FolderListingResponse
	if resp.StatusCode == http.StatusOK {
		json.Unmarshal(body, &logsResp)
	}

	// 2. Fetch Execution Folder Listing (for artifacts and output content)
	// Max depth 2: step-N/file
	execReq, _ := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	execQ := execReq.URL.Query()
	execQ.Add("folder", executionBasePath)
	execQ.Add("max_depth", "2")
	execReq.URL.RawQuery = execQ.Encode()

	execResp, err := workspaceHTTPClient.Do(execReq)
	var execListingResp FolderListingResponse
	if err == nil {
		defer execResp.Body.Close()
		execBody, _ := io.ReadAll(execResp.Body)
		if execResp.StatusCode == http.StatusOK {
			json.Unmarshal(execBody, &execListingResp)
		}
	}

	if !logsResp.Success && !execListingResp.Success {
		response := map[string]interface{}{
			"success": true,
			"steps":   map[string]interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	stepsLogs := make(map[string]map[string]interface{})
	processedPaths := make(map[string]bool)

	getStepEntry := func(stepId string) map[string]interface{} {
		if _, exists := stepsLogs[stepId]; !exists {
			meta := stepMetadata[stepId]

			// If metadata not found, try stripping iteration suffix (e.g. "step-1-sub-agent-1-i-2" -> "step-1-sub-agent-1")
			if meta == nil {
				// Check for -i-{N} suffix
				if idx := strings.LastIndex(stepId, "-i-"); idx > 0 {
					baseId := stepId[:idx]
					// Verify it's followed by digits
					suffix := stepId[idx+3:]
					isDigit := true
					for _, c := range suffix {
						if c < '0' || c > '9' {
							isDigit = false
							break
						}
					}
					if isDigit && len(suffix) > 0 {
						meta = stepMetadata[baseId]
					}
				}
			}

			title := stepId
			desc := ""
			originalId := ""
			stepType := "regular"
			contextOutput := ""
			successCriteria := ""
			if meta != nil {
				if t := meta["title"]; t != "" {
					title = t
				}
				desc = meta["description"]
				originalId = meta["original_id"]
				if t := meta["type"]; t != "" {
					stepType = t
				}
				contextOutput = meta["context_output"]
				successCriteria = meta["success_criteria"]
			}

			stepsLogs[stepId] = map[string]interface{}{
				"step_id":             stepId,
				"original_id":         originalId,
				"type":                stepType,
				"title":               title,
				"description":         desc,
				"success_criteria":    successCriteria,
				"context_output":      contextOutput,
				"is_completed":        false,
				"output_content":      nil, // Will be populated if output file exists
				"artifacts":           []map[string]interface{}{},
				"validations":         []map[string]interface{}{},
				"executions":          []map[string]interface{}{},
				"decisions":           []map[string]interface{}{},
				"orchestration":       []map[string]interface{}{},
				"conditionals":        []map[string]interface{}{},
				"learnings":           []map[string]interface{}{},
				"archived_logs":       []map[string]interface{}{}, // Archived logs from previous runs
				"archived_executions": []map[string]interface{}{}, // Archived execution outputs from decision step routing
			}
		}
		return stepsLogs[stepId]
	}

	// Helper to process logs folder (validations, logs, status)
	var processLogsFolder func(items []virtualtools.WorkspaceFolderItem)
	processLogsFolder = func(items []virtualtools.WorkspaceFolderItem) {
		for _, item := range items {
			name := filepath.Base(item.FilePath)
			isDir := item.Type == "folder"

			if isDir && strings.HasPrefix(name, "step-") {
				stepId := name

				if len(item.Children) > 0 {
					for _, child := range item.Children {
						childName := filepath.Base(child.FilePath)
						childIsDir := child.Type == "folder"

						if !childIsDir && childName == "step_done.json" {
							logPath := child.FilePath
							if processedPaths[logPath] {
								continue
							}
							processedPaths[logPath] = true

							entry := getStepEntry(stepId)
							entry["is_completed"] = true
						}

						if !childIsDir && strings.HasPrefix(childName, "validation") && strings.HasSuffix(childName, ".json") {
							logPath := child.FilePath
							if processedPaths[logPath] {
								continue
							}
							processedPaths[logPath] = true

							entry := getStepEntry(stepId)
							validations, _ := entry["validations"].([]map[string]interface{})

							attempt := 1
							if childName != "validation.json" {
								fmt.Sscanf(childName, "validation-%d.json", &attempt)
							}

							content, exists, _ := readFileFromWorkspace(r.Context(), logPath)
							var validationData interface{} = nil
							if exists {
								json.Unmarshal([]byte(content), &validationData)
							}

							validations = append(validations, map[string]interface{}{
								"attempt":   attempt,
								"file_path": logPath,
								"content":   validationData,
							})

							sort.Slice(validations, func(i, j int) bool {
								v1, ok1 := validations[i]["attempt"].(int)
								v2, ok2 := validations[j]["attempt"].(int)
								if !ok1 || !ok2 {
									return false
								}
								return v1 < v2
							})

							entry["validations"] = validations
						}

						if !childIsDir && childName == "learning-execution.json" {
							logPath := child.FilePath
							if processedPaths[logPath] {
								continue
							}
							processedPaths[logPath] = true

							entry := getStepEntry(stepId)
							learningLogs, _ := entry["learnings"].([]map[string]interface{})

							content, exists, _ := readFileFromWorkspace(r.Context(), logPath)
							if exists {
								lines := strings.Split(content, "\n")
								for _, line := range lines {
									if strings.TrimSpace(line) == "" {
										continue
									}
									var logEntry map[string]interface{}
									if err := json.Unmarshal([]byte(line), &logEntry); err == nil {
										learningLogs = append(learningLogs, logEntry)
									}
								}
							}
							entry["learnings"] = learningLogs
						}

						if !childIsDir && childName == "conditional-evaluation.json" {
							logPath := child.FilePath
							if processedPaths[logPath] {
								continue
							}
							processedPaths[logPath] = true

							entry := getStepEntry(stepId)
							conditionals, _ := entry["conditionals"].([]map[string]interface{})

							content, exists, _ := readFileFromWorkspace(r.Context(), logPath)
							var condData map[string]interface{}
							if exists {
								json.Unmarshal([]byte(content), &condData)
							}

							conditionals = append(conditionals, condData)
							entry["conditionals"] = conditionals
						}

						if !childIsDir && childName == "decision-evaluation.json" {
							logPath := child.FilePath
							if processedPaths[logPath] {
								continue
							}
							processedPaths[logPath] = true

							entry := getStepEntry(stepId)
							decisions, _ := entry["decisions"].([]map[string]interface{})

							content, exists, _ := readFileFromWorkspace(r.Context(), logPath)
							var decisionData map[string]interface{}
							if exists {
								json.Unmarshal([]byte(content), &decisionData)
							}

							decisions = append(decisions, decisionData)
							entry["decisions"] = decisions
						}

						if !childIsDir && childName == "orchestration-execution.json" {
							logPath := child.FilePath
							if processedPaths[logPath] {
								continue
							}
							processedPaths[logPath] = true

							entry := getStepEntry(stepId)
							orchLogs, _ := entry["orchestration"].([]map[string]interface{})

							content, exists, _ := readFileFromWorkspace(r.Context(), logPath)
							if exists {
								lines := strings.Split(content, "\n")
								for _, line := range lines {
									if strings.TrimSpace(line) == "" {
										continue
									}
									var logEntry map[string]interface{}
									if err := json.Unmarshal([]byte(line), &logEntry); err == nil {
										orchLogs = append(orchLogs, logEntry)
									}
								}
							}
							entry["orchestration"] = orchLogs
						}

						// Handle todo-task-execution.json (similar to orchestration)
						if !childIsDir && childName == "todo-task-execution.json" {
							logPath := child.FilePath
							if processedPaths[logPath] {
								continue
							}
							processedPaths[logPath] = true

							entry := getStepEntry(stepId)
							todoTaskLogs, _ := entry["todo_task"].([]map[string]interface{})

							content, exists, _ := readFileFromWorkspace(r.Context(), logPath)
							if exists {
								lines := strings.Split(content, "\n")
								for _, line := range lines {
									if strings.TrimSpace(line) == "" {
										continue
									}
									var logEntry map[string]interface{}
									if err := json.Unmarshal([]byte(line), &logEntry); err == nil {
										todoTaskLogs = append(todoTaskLogs, logEntry)
									}
								}
							}
							entry["todo_task"] = todoTaskLogs
						}

						if childIsDir && childName == "execution" {
							if len(child.Children) > 0 {
								for _, execChild := range child.Children {
									execName := filepath.Base(execChild.FilePath)

									// Handle standard execution attempts
									if strings.HasPrefix(execName, "execution-attempt-") && strings.HasSuffix(execName, ".json") && !strings.Contains(execName, "-conversation") {
										execPath := execChild.FilePath
										if processedPaths[execPath] {
											continue
										}
										processedPaths[execPath] = true

										entry := getStepEntry(stepId)
										executions, _ := entry["executions"].([]map[string]interface{})

										var attempt, iteration int
										fmt.Sscanf(execName, "execution-attempt-%d-iteration-%d.json", &attempt, &iteration)

										// Fetch execution result content
										content, exists, _ := readFileFromWorkspace(r.Context(), execPath)
										var execData interface{} = nil
										if exists {
											if err := json.Unmarshal([]byte(content), &execData); err != nil {
												fmt.Printf("Error unmarshalling execution data for %s: %v\n", execPath, err)
											}
										}

										executions = append(executions, map[string]interface{}{
											"attempt":           attempt,
											"iteration":         iteration,
											"file_path":         execPath,
											"conversation_path": strings.Replace(execPath, ".json", "-conversation.json", 1),
											"content":           execData,
										})

										sort.Slice(executions, func(i, j int) bool {
											a1, ok1 := executions[i]["attempt"].(int)
											a2, ok2 := executions[j]["attempt"].(int)
											iter1, ok3 := executions[i]["iteration"].(int)
											iter2, ok4 := executions[j]["iteration"].(int)

											if !ok1 || !ok2 || !ok3 || !ok4 {
												return false
											}

											if a1 != a2 {
												return a1 < a2
											}
											return iter1 < iter2
										})

										entry["executions"] = executions
									} else if execName == "decision-execution.json" {
										// Handle decision execution result
										execPath := execChild.FilePath
										if processedPaths[execPath] {
											continue
										}
										processedPaths[execPath] = true

										entry := getStepEntry(stepId)
										executions, _ := entry["executions"].([]map[string]interface{})

										// Fetch execution result content
										content, exists, _ := readFileFromWorkspace(r.Context(), execPath)
										var execData interface{} = nil
										if exists {
											if err := json.Unmarshal([]byte(content), &execData); err != nil {
												fmt.Printf("Error unmarshalling decision execution data for %s: %v\n", execPath, err)
											}
										}

										executions = append(executions, map[string]interface{}{
											"attempt":           1, // Decision steps typically run once per iteration
											"iteration":         0,
											"file_path":         execPath,
											"conversation_path": "", // No separate conversation file for decision steps usually
											"content":           execData,
										})

										entry["executions"] = executions
									} else if execName == "learn_code_fast_path.json" {
										// Learn-code fast-path run: saved main.py executed directly without
										// invoking the LLM. Content shape is documented in
										// controller_learn_code.go:saveLearnCodeFastPathLog — includes
										// success/exit_code/output/error + mode marker "learn_code_fast_path".
										// Surface as a synthetic execution entry so the UI can render it
										// alongside LLM attempts; fast_path flag lets the frontend pick a
										// minimal renderer (no conversation file, no fix loop).
										execPath := execChild.FilePath
										if processedPaths[execPath] {
											continue
										}
										processedPaths[execPath] = true

										entry := getStepEntry(stepId)
										executions, _ := entry["executions"].([]map[string]interface{})

										content, exists, _ := readFileFromWorkspace(r.Context(), execPath)
										var fastPathData map[string]interface{}
										if exists {
											if err := json.Unmarshal([]byte(content), &fastPathData); err != nil {
												fmt.Printf("Error unmarshalling fast-path log for %s: %v\n", execPath, err)
											}
										}

										executions = append(executions, map[string]interface{}{
											"attempt":           0, // 0 signals "no LLM attempt" — saved script ran directly
											"iteration":         0,
											"fast_path":         true,
											"file_path":         execPath,
											"conversation_path": "",
											"content":           fastPathData,
										})

										sort.Slice(executions, func(i, j int) bool {
											a1, ok1 := executions[i]["attempt"].(int)
											a2, ok2 := executions[j]["attempt"].(int)
											if !ok1 || !ok2 {
												return false
											}
											return a1 < a2
										})

										entry["executions"] = executions
									}
								}
							}
						}

						if childIsDir && childName == "archived" {
							entry := getStepEntry(stepId)
							archivedLogs, _ := entry["archived_logs"].([]map[string]interface{})

							for _, timestampFolder := range child.Children {
								timestampName := filepath.Base(timestampFolder.FilePath)
								timestampIsDir := timestampFolder.Type == "folder"

								if !timestampIsDir {
									continue
								}

								archiveEntry := map[string]interface{}{
									"timestamp":   timestampName,
									"validations": []map[string]interface{}{},
									"executions":  []map[string]interface{}{},
									"learnings":   []map[string]interface{}{},
								}

								for _, archivedFile := range timestampFolder.Children {
									archivedFileName := filepath.Base(archivedFile.FilePath)
									archivedFilePath := archivedFile.FilePath

									if strings.HasPrefix(archivedFileName, "validation") && strings.HasSuffix(archivedFileName, ".json") {
										validations, _ := archiveEntry["validations"].([]map[string]interface{})
										content, exists, _ := readFileFromWorkspace(r.Context(), archivedFilePath)
										var validationData interface{} = nil
										if exists {
											json.Unmarshal([]byte(content), &validationData)
										}
										validations = append(validations, map[string]interface{}{
											"file_name": archivedFileName,
											"file_path": archivedFilePath,
											"content":   validationData,
										})
										archiveEntry["validations"] = validations
									} else if strings.HasPrefix(archivedFileName, "execution-attempt") && strings.HasSuffix(archivedFileName, ".json") {
										executions, _ := archiveEntry["executions"].([]map[string]interface{})
										content, exists, _ := readFileFromWorkspace(r.Context(), archivedFilePath)
										var execData interface{} = nil
										if exists {
											json.Unmarshal([]byte(content), &execData)
										}
										executions = append(executions, map[string]interface{}{
											"file_name": archivedFileName,
											"file_path": archivedFilePath,
											"content":   execData,
										})
										archiveEntry["executions"] = executions
									} else if strings.HasPrefix(archivedFileName, "learning") && strings.HasSuffix(archivedFileName, ".json") {
										learnings, _ := archiveEntry["learnings"].([]map[string]interface{})
										content, exists, _ := readFileFromWorkspace(r.Context(), archivedFilePath)
										var learningData interface{} = nil
										if exists {
											json.Unmarshal([]byte(content), &learningData)
										}
										learnings = append(learnings, map[string]interface{}{
											"file_name": archivedFileName,
											"file_path": archivedFilePath,
											"content":   learningData,
										})
										archiveEntry["learnings"] = learnings
									} else if archivedFileName == "todo-task-execution.json" {
										// Handle todo task archived logs (JSONL format)
										todoTaskLogs, _ := archiveEntry["todo_task"].([]map[string]interface{})
										if todoTaskLogs == nil {
											todoTaskLogs = []map[string]interface{}{}
										}
										content, exists, _ := readFileFromWorkspace(r.Context(), archivedFilePath)
										if exists {
											lines := strings.Split(content, "\n")
											for _, line := range lines {
												if strings.TrimSpace(line) == "" {
													continue
												}
												var logEntry map[string]interface{}
												if err := json.Unmarshal([]byte(line), &logEntry); err == nil {
													todoTaskLogs = append(todoTaskLogs, logEntry)
												}
											}
										}
										archiveEntry["todo_task"] = todoTaskLogs
									}
								}

								archivedLogs = append(archivedLogs, archiveEntry)
							}

							sort.Slice(archivedLogs, func(i, j int) bool {
								t1, _ := archivedLogs[i]["timestamp"].(string)
								t2, _ := archivedLogs[j]["timestamp"].(string)
								return t1 > t2
							})

							entry["archived_logs"] = archivedLogs
						}
					}
				}
			} else if isDir {
				if len(item.Children) > 0 {
					processLogsFolder(item.Children)
				}
			}
		}
	}

	// Helper to process execution folder (artifacts, outputs)
	var processExecutionFolder func(items []virtualtools.WorkspaceFolderItem)
	processExecutionFolder = func(items []virtualtools.WorkspaceFolderItem) {
		for _, item := range items {
			name := filepath.Base(item.FilePath)
			isDir := item.Type == "folder"

			// Case 1: Folder or File named after a step (e.g., execution/step-1/ or execution/step-1)
			if strings.HasPrefix(name, "step-") {
				stepId := name
				entry := getStepEntry(stepId)
				expectedOutputsStr, _ := entry["context_output"].(string)
				expectedOutputs := strings.Split(expectedOutputsStr, ",")

				if isDir {
					// It's a folder, process its children to find output files
					if len(item.Children) > 0 {
						for _, child := range item.Children {
							childName := filepath.Base(child.FilePath)
							childIsDir := child.Type == "folder"

							if childIsDir {
								continue
							}

							// Match against expected outputs
							isMatched := false
							for _, expected := range expectedOutputs {
								if expected != "" && childName == expected {
									isMatched = true
									break
								}
							}

							if isMatched {
								outputPath := child.FilePath
								if !processedPaths[outputPath] {
									processedPaths[outputPath] = true

									content, exists, _ := readFileFromWorkspace(r.Context(), outputPath)
									if exists {
										var outputData interface{}
										isJson := false
										if err := json.Unmarshal([]byte(content), &outputData); err == nil {
											isJson = true
										} else {
											outputData = content
										}
										entry["output_content"] = map[string]interface{}{
											"file_path": outputPath,
											"content":   outputData,
											"is_json":   isJson,
										}
									}
								}
							} else {
								// It's an artifact
								if !processedPaths[child.FilePath] {
									processedPaths[child.FilePath] = true
									artifacts, _ := entry["artifacts"].([]map[string]interface{})
									artifacts = append(artifacts, map[string]interface{}{
										"file_name": childName,
										"file_path": child.FilePath,
									})
									entry["artifacts"] = artifacts
								}
							}
						}
					}
				} else {
					// It's a FILE named after a step (e.g., execution/step-1)
					// Treat this file as the output content for this step
					if !processedPaths[item.FilePath] {
						processedPaths[item.FilePath] = true
						content, exists, _ := readFileFromWorkspace(r.Context(), item.FilePath)
						if exists {
							var outputData interface{}
							isJson := false
							if err := json.Unmarshal([]byte(content), &outputData); err == nil {
								isJson = true
							} else {
								outputData = content
							}
							entry["output_content"] = map[string]interface{}{
								"file_path": item.FilePath,
								"content":   outputData,
								"is_json":   isJson,
							}
						}
					}
				}
			} else if isDir && name == "archived" {
				// Process archived execution folders (execution/archived/run-{N}/step-{N}/)
				// Each run folder contains archived step folders from decision step routing
				for _, runFolder := range item.Children {
					runFolderName := filepath.Base(runFolder.FilePath)
					runFolderIsDir := runFolder.Type == "folder"

					if !runFolderIsDir || !strings.HasPrefix(runFolderName, "run-") {
						continue
					}

					// Extract run number from folder name (e.g., "run-1" -> "1")
					runNumber := strings.TrimPrefix(runFolderName, "run-")

					// Process each archived step folder within this run
					for _, archivedStepFolder := range runFolder.Children {
						archivedStepName := filepath.Base(archivedStepFolder.FilePath)
						archivedStepIsDir := archivedStepFolder.Type == "folder"

						if !archivedStepIsDir || !strings.HasPrefix(archivedStepName, "step-") {
							continue
						}

						// Get the step entry for this archived step
						stepId := archivedStepName
						entry := getStepEntry(stepId)

						// Initialize archived_executions array if not exists
						archivedExecutions, _ := entry["archived_executions"].([]map[string]interface{})
						if archivedExecutions == nil {
							archivedExecutions = []map[string]interface{}{}
						}

						// Create archive entry for this run
						archiveEntry := map[string]interface{}{
							"run_number": runNumber,
							"artifacts":  []map[string]interface{}{},
						}

						expectedOutputsStr, _ := entry["context_output"].(string)
						expectedOutputs := strings.Split(expectedOutputsStr, ",")

						// Process files in the archived step folder
						for _, archivedFile := range archivedStepFolder.Children {
							archivedFileName := filepath.Base(archivedFile.FilePath)
							archivedFileIsDir := archivedFile.Type == "folder"

							if archivedFileIsDir {
								continue
							}

							// Check if this file matches expected output
							isMatched := false
							for _, expected := range expectedOutputs {
								if expected != "" && archivedFileName == expected {
									isMatched = true
									break
								}
							}

							if isMatched {
								// This is the output content for this archived run
								content, exists, _ := readFileFromWorkspace(r.Context(), archivedFile.FilePath)
								if exists {
									var outputData interface{}
									isJson := false
									if err := json.Unmarshal([]byte(content), &outputData); err == nil {
										isJson = true
									} else {
										outputData = content
									}
									archiveEntry["output_content"] = map[string]interface{}{
										"file_path": archivedFile.FilePath,
										"content":   outputData,
										"is_json":   isJson,
									}
								}
							} else {
								// It's an artifact for this archived run
								artifacts, _ := archiveEntry["artifacts"].([]map[string]interface{})
								artifacts = append(artifacts, map[string]interface{}{
									"file_name": archivedFileName,
									"file_path": archivedFile.FilePath,
								})
								archiveEntry["artifacts"] = artifacts
							}
						}

						archivedExecutions = append(archivedExecutions, archiveEntry)
						entry["archived_executions"] = archivedExecutions
					}
				}
			} else if isDir {
				// Recurse into subfolders
				if len(item.Children) > 0 {
					processExecutionFolder(item.Children)
				}
			}
		}
	}

	if logsResp.Success {
		processLogsFolder(logsResp.Data)
	}
	if execListingResp.Success {
		processExecutionFolder(execListingResp.Data)
	}

	response := map[string]interface{}{
		"success": true,
		"steps":   stepsLogs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetCosts handles getting cost data (token usage) for workflow runs
func (api *StreamingAPI) handleGetCosts(w http.ResponseWriter, r *http.Request) {
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

	// Validate workspace path to prevent path traversal attacks
	cleanedWorkspacePath := filepath.Clean(workspacePath)
	if strings.Contains(cleanedWorkspacePath, "..") {
		http.Error(w, "Invalid workspace path", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loadWorkflowCosts(r.Context(), cleanedWorkspacePath))
}

// handleGetLogFile returns the content of a specific log file
func (api *StreamingAPI) handleGetLogFile(w http.ResponseWriter, r *http.Request) {
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

	filePath := r.URL.Query().Get("file_path")
	if filePath == "" {
		http.Error(w, "file_path parameter is required", http.StatusBadRequest)
		return
	}

	// Validate file path to prevent path traversal attacks
	cleanedPath := filepath.Clean(filePath)
	if strings.Contains(cleanedPath, "..") {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	// Restrict to allowed file types only (must be in logs or runs directories)
	allowedExtensions := map[string]string{
		".json":  "application/json",
		".txt":   "text/plain",
		".md":    "text/markdown",
		".log":   "text/plain",
		".csv":   "text/csv",
		".jsonl": "application/x-jsonlines",
		".yaml":  "text/yaml",
		".yml":   "text/yaml",
		".xml":   "application/xml",
		".sql":   "text/x-sql",
		".sh":    "text/x-shellscript",
	}

	ext := filepath.Ext(cleanedPath)
	contentType, allowed := allowedExtensions[ext]
	if !allowed {
		http.Error(w, "Unsupported file format. Allowed formats: .json, .txt, .md, .log, .csv, .jsonl, .yaml, .yml, .xml, .sql, .sh", http.StatusBadRequest)
		return
	}

	if !strings.Contains(cleanedPath, "/logs/") && !strings.Contains(cleanedPath, "/runs/") {
		http.Error(w, "File must be in logs or runs directory", http.StatusBadRequest)
		return
	}

	// Use existing readFileFromWorkspace utility which handles workspace API communication
	content, exists, err := readFileFromWorkspace(r.Context(), cleanedPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read file: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", contentType)

	w.Write([]byte(content))
}

// handleGetEvaluationReports handles getting evaluation reports for a workflow
// Returns evaluation_report.json files from evaluation/runs/*/ folders
func (api *StreamingAPI) handleGetEvaluationReports(w http.ResponseWriter, r *http.Request) {
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
	runFolder := r.URL.Query().Get("run_folder") // Optional: filter to specific run folder

	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	// Validate and clean workspace path
	cleanedWorkspacePath := filepath.Clean(workspacePath)
	if strings.Contains(cleanedWorkspacePath, "..") {
		http.Error(w, "Invalid workspace path", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loadWorkflowEvaluationReports(r.Context(), cleanedWorkspacePath, runFolder))
}

// populateStepMetadata recursively traverses the plan to build a mapping from step IDs/paths to human-readable metadata
func populateStepMetadata(steps []map[string]interface{}, prefix string, metadata map[string]map[string]string) {
	for i, step := range steps {
		id, _ := step["id"].(string)
		stepType, _ := step["type"].(string)
		title, _ := step["title"].(string)
		desc, _ := step["description"].(string)
		criteria, _ := step["success_criteria"].(string)
		if criteria == "" {
			criteria, _ = step["success_reasoning"].(string)
		}

		// Handle inner steps for complex types
		if inner, ok := step["orchestration_step"].(map[string]interface{}); ok {
			if desc == "" {
				if innerDesc, ok := inner["description"].(string); ok {
					desc = innerDesc
				}
			}
		}

		// Calculate step key (folder name pattern)
		var stepKey string
		resolvedType := stepType
		if prefix == "" {
			stepKey = fmt.Sprintf("step-%d", i+1)
		} else {
			// Prefix is like "step-1-true-" or "step-1-false-"
			stepKey = fmt.Sprintf("%s%d", prefix, i)
			if strings.Contains(prefix, "-true-") || strings.Contains(prefix, "-false-") {
				resolvedType = "branch"
			}
		}

		// Get context_output for the step (the output file name)
		// Handle both string and array formats
		var contextOutputs []string
		if co, ok := step["context_output"].(string); ok && co != "" {
			contextOutputs = []string{co}
		} else if coList, ok := step["context_output"].([]interface{}); ok {
			for _, co := range coList {
				if coStr, ok := co.(string); ok && coStr != "" {
					contextOutputs = append(contextOutputs, coStr)
				}
			}
		}

		meta := map[string]string{
			"title":            title,
			"description":      desc,
			"success_criteria": criteria,
			"original_id":      id,
			"type":             resolvedType,
			"context_output":   strings.Join(contextOutputs, ","), // Store as comma-separated string for simplicity in meta
		}

		// Store metadata by multiple keys to ensure it's found
		metadata[stepKey] = meta
		if id != "" {
			metadata[id] = meta
		}

		// Recurse into conditional/branch steps
		if trueSteps, ok := step["if_true_steps"].([]interface{}); ok {
			populateStepMetadata(convertToMapList(trueSteps), stepKey+"-true-", metadata)
			populateStepMetadata(convertToMapList(trueSteps), stepKey+"-if-true-", metadata)
		}
		if falseSteps, ok := step["if_false_steps"].([]interface{}); ok {
			populateStepMetadata(convertToMapList(falseSteps), stepKey+"-false-", metadata)
			populateStepMetadata(convertToMapList(falseSteps), stepKey+"-if-false-", metadata)
		}

		// Recurse into orchestration routes
		if routes, ok := step["orchestration_routes"].([]interface{}); ok {
			for j, r := range routes {
				if route, ok := r.(map[string]interface{}); ok {
					if subStep, ok := route["sub_agent_step"].(map[string]interface{}); ok {
						subAgentKey := fmt.Sprintf("%s-sub-agent-%d", stepKey, j+1)
						subId, _ := subStep["id"].(string)
						subCriteria, _ := subStep["success_criteria"].(string)
						subMeta := map[string]string{
							"title":            subStep["title"].(string),
							"description":      subStep["description"].(string),
							"success_criteria": subCriteria,
							"original_id":      subId,
							"type":             "sub-agent",
						}
						metadata[subAgentKey] = subMeta
						if subId != "" {
							metadata[subId] = subMeta
						}
					}
				}
			}
		}
	}
}

// convertToMapList converts a list of interfaces to a list of maps
func convertToMapList(items []interface{}) []map[string]interface{} {
	res := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			res = append(res, m)
		}
	}
	return res
}

// writeRawFileToWorkspace writes raw string content to a file in the workspace API
func writeRawFileToWorkspace(ctx context.Context, filePath string, content string) error {
	// URL-encode the filepath segments
	pathSegments := strings.Split(filePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Prepare request body
	requestBody := map[string]interface{}{
		"content": content,
	}
	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Write file via workspace API
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func listWorkspaceFolder(ctx context.Context, folderPath string, maxDepth int) (virtualtools.WorkspaceFolderListing, bool, error) {
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("folder", folderPath)
	q.Add("max_depth", strconv.Itoa(maxDepth))
	req.URL.RawQuery = q.Encode()

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Success bool                                `json:"success"`
		Message string                              `json:"message"`
		Error   string                              `json:"error"`
		Data    virtualtools.WorkspaceFolderListing `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, false, fmt.Errorf("failed to parse API response: %w", err)
	}
	if !apiResp.Success {
		return nil, false, fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	return apiResp.Data, true, nil
}

func collectWorkspaceFilePaths(items []virtualtools.WorkspaceFolderItem, out *[]string) {
	for _, item := range items {
		switch item.Type {
		case "file":
			*out = append(*out, item.FilePath)
		case "folder":
			collectWorkspaceFilePaths(item.Children, out)
		}
	}
}

func snapshotWorkspaceFolder(ctx context.Context, workspacePath, versionFolder, relativeFolder string) ([]string, error) {
	folderPath := workspacePath + "/" + relativeFolder
	listing, exists, err := listWorkspaceFolder(ctx, folderPath, 100)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	var filePaths []string
	collectWorkspaceFilePaths(listing, &filePaths)

	var filesSnapshot []string
	for _, fullPath := range filePaths {
		relPath := strings.TrimPrefix(fullPath, workspacePath+"/")
		if relPath == fullPath {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, fullPath)
		if err != nil {
			log.Printf("[WARN] Failed to read %s for versioning: %v", fullPath, err)
			continue
		}
		if !exists {
			continue
		}

		dstPath := versionFolder + "/" + relPath
		if err := writeRawFileToWorkspace(ctx, dstPath, content); err != nil {
			return nil, fmt.Errorf("failed to write version file %s: %w", relPath, err)
		}
		filesSnapshot = append(filesSnapshot, relPath)
	}

	return filesSnapshot, nil
}

func deleteWorkspaceFolder(ctx context.Context, folderPath string) error {
	pathSegments := strings.Split(folderPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	apiURL := getWorkspaceAPIURL() + "/api/folders/" + encodedPath + "?confirm=true"
	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func toStringSlice(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}

	res := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && s != "" {
			res = append(res, s)
		}
	}
	return res
}

// Config files that are versioned when publishing a workflow version
var versionedConfigFiles = []string{
	"workflow.json",
	"planning/plan.json",
	"planning/step_config.json",
	"planning/workflow_layout.json",
	"planning/step_override.json",
	"reports/report_plan.md",
	"variables/variables.json",
	"evaluation/evaluation_plan.json",
}

// Folder roots that are versioned recursively when publishing a workflow version
var versionedFolderRoots = []string{
	"learnings",
}

// handlePublishVersion creates a new numbered version snapshot of workflow config files
func (api *StreamingAPI) handlePublishVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req struct {
		WorkspacePath string `json:"workspace_path"`
		Label         string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	if req.Label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// List existing versions to find next version number
	versionsPath := req.WorkspacePath + "/versions"
	listURL := getWorkspaceAPIURL() + "/api/documents"
	listReq, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}
	q := listReq.URL.Query()
	q.Add("folder", versionsPath)
	q.Add("max_depth", "1")
	listReq.URL.RawQuery = q.Encode()

	listResp, err := workspaceHTTPClient.Do(listReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list versions: %v", err), http.StatusInternalServerError)
		return
	}
	defer listResp.Body.Close()

	nextVersion := 1
	if listResp.StatusCode == http.StatusOK {
		listBody, _ := io.ReadAll(listResp.Body)
		var listAPIResp virtualtools.WorkspaceAPIResponse
		if err := json.Unmarshal(listBody, &listAPIResp); err == nil && listAPIResp.Success {
			// Parse as typed folder listing
			if dataBytes, err := json.Marshal(listAPIResp.Data); err == nil {
				var folderListing virtualtools.WorkspaceFolderListing
				if err := json.Unmarshal(dataBytes, &folderListing); err == nil {
					// The first item is the versions folder itself; its children are v1, v2, etc.
					items := folderListing
					if len(folderListing) > 0 && len(folderListing[0].Children) > 0 {
						items = folderListing[0].Children
					}
					for _, item := range items {
						if item.Type != "folder" {
							continue
						}
						parts := strings.Split(item.FilePath, "/")
						name := parts[len(parts)-1]
						var v int
						if _, err := fmt.Sscanf(name, "v%d", &v); err == nil && v >= nextVersion {
							nextVersion = v + 1
						}
					}
				}
			}
		}
	} else {
		// versions folder doesn't exist yet — that's fine, start at v1
		io.ReadAll(listResp.Body)
	}

	versionFolder := fmt.Sprintf("%s/versions/v%d", req.WorkspacePath, nextVersion)
	var filesSnapshot []string

	// Copy each managed config file to the version folder.
	for _, relPath := range versionedConfigFiles {
		srcPath := req.WorkspacePath + "/" + relPath
		content, exists, err := readFileFromWorkspace(ctx, srcPath)
		if err != nil {
			log.Printf("[WARN] Failed to read %s for versioning: %v", srcPath, err)
			continue
		}
		if !exists {
			continue
		}

		dstPath := versionFolder + "/" + relPath
		if err := writeRawFileToWorkspace(ctx, dstPath, content); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write version file %s: %v", relPath, err), http.StatusInternalServerError)
			return
		}
		filesSnapshot = append(filesSnapshot, relPath)
	}

	// Copy managed folders recursively (for example learnings/) into the version folder.
	for _, folderRoot := range versionedFolderRoots {
		folderSnapshot, err := snapshotWorkspaceFolder(ctx, req.WorkspacePath, versionFolder, folderRoot)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to snapshot folder %s: %v", folderRoot, err), http.StatusInternalServerError)
			return
		}
		filesSnapshot = append(filesSnapshot, folderSnapshot...)
	}

	if len(filesSnapshot) == 0 {
		http.Error(w, "No config files found to version", http.StatusBadRequest)
		return
	}

	// Write version_meta.json
	meta := map[string]interface{}{
		"version":         nextVersion,
		"label":           req.Label,
		"created_at":      time.Now().UTC().Format(time.RFC3339),
		"files_snapshot":  filesSnapshot,
		"managed_files":   versionedConfigFiles,
		"managed_folders": versionedFolderRoots,
	}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	if err := writeRawFileToWorkspace(ctx, versionFolder+"/version_meta.json", string(metaJSON)); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write version metadata: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"version": map[string]interface{}{
			"version":     nextVersion,
			"label":       req.Label,
			"created_at":  meta["created_at"],
			"files_count": len(filesSnapshot),
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleListVersions returns all published versions for a workflow
func (api *StreamingAPI) handleListVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	versionsPath := workspacePath + "/versions"

	// List version subfolders
	listURL := getWorkspaceAPIURL() + "/api/documents"
	listReq, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}
	q := listReq.URL.Query()
	q.Add("folder", versionsPath)
	q.Add("max_depth", "1")
	listReq.URL.RawQuery = q.Encode()

	listResp, err := workspaceHTTPClient.Do(listReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list versions: %v", err), http.StatusInternalServerError)
		return
	}
	defer listResp.Body.Close()

	if listResp.StatusCode == http.StatusNotFound {
		// No versions folder - return empty list
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"versions": []interface{}{},
		})
		return
	}

	listBody, err := io.ReadAll(listResp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	var listAPIResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(listBody, &listAPIResp); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse response: %v", err), http.StatusInternalServerError)
		return
	}

	// Collect version folder names using typed folder listing
	var versionFolders []string
	if dataBytes, err := json.Marshal(listAPIResp.Data); err == nil {
		var folderListing virtualtools.WorkspaceFolderListing
		if err := json.Unmarshal(dataBytes, &folderListing); err == nil {
			// The first item is the versions folder itself; its children are v1, v2, etc.
			items := folderListing
			if len(folderListing) > 0 && len(folderListing[0].Children) > 0 {
				items = folderListing[0].Children
			}
			for _, item := range items {
				if item.Type != "folder" {
					continue
				}
				parts := strings.Split(item.FilePath, "/")
				name := parts[len(parts)-1]
				if strings.HasPrefix(name, "v") {
					versionFolders = append(versionFolders, name)
				}
			}
		}
	}

	// Read version_meta.json from each folder
	type versionInfo struct {
		Version    int    `json:"version"`
		Label      string `json:"label"`
		CreatedAt  string `json:"created_at"`
		FilesCount int    `json:"files_count"`
	}
	var versions []versionInfo

	for _, folder := range versionFolders {
		metaPath := versionsPath + "/" + folder + "/version_meta.json"
		content, exists, err := readFileFromWorkspace(ctx, metaPath)
		if err != nil || !exists {
			continue
		}

		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(content), &meta); err != nil {
			continue
		}

		v := versionInfo{
			Label:     fmt.Sprintf("%v", meta["label"]),
			CreatedAt: fmt.Sprintf("%v", meta["created_at"]),
		}
		if vNum, ok := meta["version"].(float64); ok {
			v.Version = int(vNum)
		}
		if snapshot, ok := meta["files_snapshot"].([]interface{}); ok {
			v.FilesCount = len(snapshot)
		}
		versions = append(versions, v)
	}

	// Sort newest first
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version > versions[j].Version
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"versions": versions,
	})
}

// handleRevertVersion restores config files from a published version
func (api *StreamingAPI) handleRevertVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req struct {
		WorkspacePath string `json:"workspace_path"`
		Version       int    `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" || req.Version < 1 {
		http.Error(w, "workspace_path and version (>= 1) are required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	versionFolder := fmt.Sprintf("%s/versions/v%d", req.WorkspacePath, req.Version)

	// Read version_meta.json to get file list
	metaPath := versionFolder + "/version_meta.json"
	metaContent, exists, err := readFileFromWorkspace(ctx, metaPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read version metadata: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, fmt.Sprintf("Version v%d not found", req.Version), http.StatusNotFound)
		return
	}

	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(metaContent), &meta); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse version metadata: %v", err), http.StatusInternalServerError)
		return
	}

	filesSnapshot, ok := meta["files_snapshot"].([]interface{})
	if !ok || len(filesSnapshot) == 0 {
		http.Error(w, "Version has no files to restore", http.StatusBadRequest)
		return
	}

	managedFiles := toStringSlice(meta["managed_files"])
	managedFolders := toStringSlice(meta["managed_folders"])
	snapshotSet := make(map[string]struct{}, len(filesSnapshot))
	for _, f := range filesSnapshot {
		if relPath, ok := f.(string); ok && relPath != "" {
			snapshotSet[relPath] = struct{}{}
		}
	}

	// Newer versions record which folder roots are managed by versioning. Clear them first so
	// restore does not leave behind stale learning files from newer iterations.
	for _, folderRoot := range managedFolders {
		if err := deleteWorkspaceFolder(ctx, req.WorkspacePath+"/"+folderRoot); err != nil {
			http.Error(w, fmt.Sprintf("Failed to clear folder %s: %v", folderRoot, err), http.StatusInternalServerError)
			return
		}
	}

	// Likewise, delete managed files that were absent in the snapshot so reverting restores the
	// exact versioned state instead of only overwriting files that still exist in the snapshot.
	for _, relPath := range managedFiles {
		if _, exists := snapshotSet[relPath]; exists {
			continue
		}
		if err := deleteWorkspaceFile(ctx, req.WorkspacePath+"/"+relPath); err != nil {
			http.Error(w, fmt.Sprintf("Failed to remove file %s: %v", relPath, err), http.StatusInternalServerError)
			return
		}
	}

	// Restore each file
	var filesRestored int
	for _, f := range filesSnapshot {
		relPath, ok := f.(string)
		if !ok {
			continue
		}

		srcPath := versionFolder + "/" + relPath
		content, exists, err := readFileFromWorkspace(ctx, srcPath)
		if err != nil || !exists {
			log.Printf("[WARN] Failed to read version file %s: exists=%v, err=%v", srcPath, exists, err)
			continue
		}

		dstPath := req.WorkspacePath + "/" + relPath
		if err := writeRawFileToWorkspace(ctx, dstPath, content); err != nil {
			http.Error(w, fmt.Sprintf("Failed to restore file %s: %v", relPath, err), http.StatusInternalServerError)
			return
		}
		filesRestored++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"files_restored": filesRestored,
	})
}

// handleDeleteVersion deletes a published version folder
func (api *StreamingAPI) handleDeleteVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	versionStr := r.URL.Query().Get("version")
	if workspacePath == "" || versionStr == "" {
		http.Error(w, "workspace_path and version parameters are required", http.StatusBadRequest)
		return
	}

	var versionNum int
	if _, err := fmt.Sscanf(versionStr, "%d", &versionNum); err != nil || versionNum < 1 {
		http.Error(w, "Invalid version number", http.StatusBadRequest)
		return
	}

	// Construct folder path
	folderPath := fmt.Sprintf("%s/versions/v%d", workspacePath, versionNum)

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

	resp, err := workspaceHTTPClient.Do(req)
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Version does not exist (already deleted)",
		})
		return
	}

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Workspace API returned status %d: %s", resp.StatusCode, string(body)), http.StatusInternalServerError)
		return
	}

	var apiResp virtualtools.WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse API response: %v", err), http.StatusInternalServerError)
		return
	}

	if !apiResp.Success {
		http.Error(w, fmt.Sprintf("Failed to delete version: %s", apiResp.Error), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Successfully deleted version v%d", versionNum),
	})
}
