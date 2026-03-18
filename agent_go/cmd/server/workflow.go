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
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/database"
	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("WORKSPACE_API_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
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

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

// aggregateGroupTokenUsage scans a parent iteration folder for group subfolders
// and aggregates their token_usage.json files into a combined view
// Returns nil if no group token usage files are found
func aggregateGroupTokenUsage(ctx context.Context, runFolderPath string) map[string]interface{} {
	// List contents of the run folder to find group subfolders
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}

	q := req.URL.Query()
	q.Add("folder", runFolderPath)
	q.Add("max_depth", "1")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
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

	// Parse folder listing response
	type FolderListingResponse struct {
		Success bool                                `json:"success"`
		Data    virtualtools.WorkspaceFolderListing `json:"data"`
	}

	var apiResp FolderListingResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil
	}

	if !apiResp.Success || len(apiResp.Data) == 0 {
		return nil
	}

	// Reserved folder names that are not group folders
	reservedFolders := map[string]bool{
		"logs":      true,
		"execution": true,
		"planning":  true,
		"learnings": true,
	}

	// Aggregate token usage from group subfolders
	aggregatedByModel := make(map[string]map[string]interface{})
	aggregatedByStepAndModel := make(map[string]map[string]map[string]interface{})
	groupsFound := []string{}

	for _, item := range apiResp.Data {
		isDir := item.Type == "folder"
		if !isDir {
			continue
		}

		// Extract folder name from filepath
		folderName := filepath.Base(item.FilePath)
		if reservedFolders[folderName] {
			continue
		}

		// Try to read token_usage.json from this group folder
		groupTokenPath := fmt.Sprintf("%s/%s/token_usage.json", runFolderPath, folderName)
		content, exists, _ := readFileFromWorkspace(ctx, groupTokenPath)
		if !exists {
			continue
		}

		var groupTokenUsage map[string]interface{}
		if err := json.Unmarshal([]byte(content), &groupTokenUsage); err != nil {
			continue
		}

		groupsFound = append(groupsFound, folderName)

		// Aggregate by_model data
		if byModel, ok := groupTokenUsage["by_model"].(map[string]interface{}); ok {
			for modelID, modelData := range byModel {
				if aggregatedByModel[modelID] == nil {
					aggregatedByModel[modelID] = make(map[string]interface{})
					// Copy initial values
					if modelMap, ok := modelData.(map[string]interface{}); ok {
						for k, v := range modelMap {
							aggregatedByModel[modelID][k] = v
						}
					}
				} else {
					// Aggregate numeric values
					if modelMap, ok := modelData.(map[string]interface{}); ok {
						aggregateTokenFields(aggregatedByModel[modelID], modelMap)
					}
				}
			}
		}

		// Aggregate by_step_and_model data with group prefix
		if byStepAndModel, ok := groupTokenUsage["by_step_and_model"].(map[string]interface{}); ok {
			for stepKey, stepData := range byStepAndModel {
				// Prefix step key with group name for clarity
				groupStepKey := fmt.Sprintf("%s/%s", folderName, stepKey)
				if aggregatedByStepAndModel[groupStepKey] == nil {
					aggregatedByStepAndModel[groupStepKey] = make(map[string]map[string]interface{})
				}
				if stepModels, ok := stepData.(map[string]interface{}); ok {
					for modelID, modelData := range stepModels {
						if aggregatedByStepAndModel[groupStepKey][modelID] == nil {
							aggregatedByStepAndModel[groupStepKey][modelID] = make(map[string]interface{})
							if modelMap, ok := modelData.(map[string]interface{}); ok {
								for k, v := range modelMap {
									aggregatedByStepAndModel[groupStepKey][modelID][k] = v
								}
							}
						}
					}
				}
			}
		}
	}

	if len(groupsFound) == 0 {
		return nil
	}

	// Build aggregated response
	result := map[string]interface{}{
		"by_model":          aggregatedByModel,
		"by_step_and_model": aggregatedByStepAndModel,
		"groups":            groupsFound,
		"aggregated":        true,
	}

	return result
}

// aggregateTokenFields aggregates numeric token fields from src into dst
func aggregateTokenFields(dst, src map[string]interface{}) {
	numericFields := []string{
		"input_tokens", "output_tokens", "cache_tokens", "cache_read_tokens", "cache_write_tokens",
		"reasoning_tokens", "llm_call_count", "context_window_usage",
		"input_cost", "output_cost", "reasoning_cost", "cache_cost", "cache_read_cost", "cache_write_cost", "total_cost",
	}

	for _, field := range numericFields {
		if srcVal, ok := src[field]; ok {
			switch v := srcVal.(type) {
			case float64:
				if dstVal, ok := dst[field].(float64); ok {
					dst[field] = dstVal + v
				} else {
					dst[field] = v
				}
			case int:
				if dstVal, ok := dst[field].(float64); ok {
					dst[field] = dstVal + float64(v)
				} else if dstVal, ok := dst[field].(int); ok {
					dst[field] = dstVal + v
				} else {
					dst[field] = v
				}
			}
		}
	}

	// Keep non-numeric fields from the first source (provider, model_context_window, etc.)
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
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
	return &metadata, nil
}

// inferRunMetadata creates metadata for legacy run folders that don't have run_metadata.json.
// Uses token_usage.json created_at as start time, progress.LastUpdated for completion.
func inferRunMetadata(ctx context.Context, workspacePath, folderName string, progress *StepProgress) *RunMetadata {
	if progress == nil {
		return nil
	}

	metadata := &RunMetadata{
		Status:      "running",
		TriggeredBy: "manual", // assume manual for legacy runs
	}

	// Try to get created_at from token_usage.json
	tokenPaths := []string{
		workspacePath + "/runs/" + folderName + "/token_usage.json",
	}
	for _, tp := range tokenPaths {
		content, exists, err := readFileFromWorkspace(ctx, tp)
		if err != nil || !exists {
			continue
		}
		var tokenFile struct {
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
		}
		if err := json.Unmarshal([]byte(content), &tokenFile); err == nil && tokenFile.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, tokenFile.CreatedAt); err == nil {
				metadata.CreatedAt = t
			} else if t, err := time.Parse(time.RFC3339, tokenFile.CreatedAt); err == nil {
				metadata.CreatedAt = t
			}
		}
		break
	}

	// Fallback: use progress.LastUpdated as rough created_at if token_usage didn't work
	if metadata.CreatedAt.IsZero() {
		metadata.CreatedAt = progress.LastUpdated
	}

	// Determine completion
	if progress.TotalSteps > 0 && len(progress.CompletedStepIndices) >= progress.TotalSteps {
		completedAt := progress.LastUpdated
		metadata.Status = "completed"
		metadata.CompletedAt = &completedAt
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

// ActiveWorkflowExecution tracks a currently running workflow execution in memory
type ActiveWorkflowExecution struct {
	QueryID       string    `json:"query_id"`
	SessionID     string    `json:"session_id"`
	PresetQueryID string    `json:"preset_query_id,omitempty"`
	WorkspacePath string    `json:"workspace_path"`
	RunFolder     string    `json:"run_folder,omitempty"`
	TriggeredBy   string    `json:"triggered_by"` // "manual", "cron"
	StartedAt     time.Time `json:"started_at"`
}

// RunMetadataLLM captures which model was used for a specific role
type RunMetadataLLM struct {
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"model_id,omitempty"`
}

// RunMetadataModels captures the LLM configuration used for the run
type RunMetadataModels struct {
	AllocationMode string           `json:"allocation_mode,omitempty"` // "manual" or "tiered"
	ExecutionLLM   *RunMetadataLLM  `json:"execution_llm,omitempty"`
	LearningLLM    *RunMetadataLLM  `json:"learning_llm,omitempty"`
	PhaseLLM       *RunMetadataLLM  `json:"phase_llm,omitempty"`
	Tier1          *RunMetadataLLM  `json:"tier_1,omitempty"`
	Tier2          *RunMetadataLLM  `json:"tier_2,omitempty"`
	Tier3          *RunMetadataLLM  `json:"tier_3,omitempty"`
	TempOverride   *RunMetadataLLM  `json:"temp_override,omitempty"`
	TempOverride2  *RunMetadataLLM  `json:"temp_override_2,omitempty"`
}

// RunMetadata stores lifecycle information for a run folder
type RunMetadata struct {
	CreatedAt   time.Time          `json:"created_at"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	Status      string             `json:"status"`                    // "running", "completed"
	TriggeredBy string             `json:"triggered_by,omitempty"`    // "manual", "cron", "workflow_builder"
	Models      *RunMetadataModels `json:"models,omitempty"`          // LLM config used for this run
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

	// Temporary LLM for learning agents (optional, used when learnings already exist for a step)
	// If learnings exist for a step_id, use TempLearningLLM if configured
	// If no learnings exist (new learning), always use default LLM (step config → preset)
	TempLearningLLM *AgentLLMConfig `json:"temp_learning_llm,omitempty"`

	// Variable group execution options (for batch execution with multiple groups)
	EnabledGroupIDs []string `json:"enabled_group_ids,omitempty"` // Group IDs to execute (if empty, uses groups' enabled flags)

	// Logging options
	SaveValidationResponses bool `json:"save_validation_responses,omitempty"` // If true, save validation responses and execution logs to workspace (default: true)

	// Cleanup control
	SkipExecutionCleanup bool `json:"skip_execution_cleanup,omitempty"` // If true, skip deleting execution folders before running steps
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
		http.Error(w, fmt.Sprintf("Failed to update workflow: %v", err), http.StatusInternalServerError)
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
	stepsFilePath := workspacePath + "/runs/" + runFolder + "/execution/steps_done.json"

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
	GroupID     string            `json:"group_id"`     // e.g., "group-1", "group-2" (used as fallback for folder names)
	DisplayName string            `json:"display_name"` // Optional user-friendly name (e.g., "Production", "Staging")
	Values      map[string]string `json:"values"`
	Enabled     bool              `json:"enabled"`
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

		// Handle decision steps - DecisionPlanStep is now flattened, ID already collected above
		// No nested step to collect

		// Handle orchestration steps - collect orchestration step ID and sub-agent IDs
		if orchestrationStep, ok := step.(*todo_creation_human.OrchestrationPlanStep); ok {
			if orchestrationStep.OrchestrationStep != nil {
				if orchestrationStepID := orchestrationStep.OrchestrationStep.GetID(); orchestrationStepID != "" {
					stepIDs = append(stepIDs, orchestrationStepID)
				}
			}
			// Collect sub-agent step IDs from routes
			for _, route := range orchestrationStep.OrchestrationRoutes {
				if route.SubAgentStep != nil {
					if subAgentStepID := route.SubAgentStep.GetID(); subAgentStepID != "" {
						stepIDs = append(stepIDs, subAgentStepID)
					}
				}
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

	// Read step configs to get agent configs (use_code_execution_mode, learning_detail_level)
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
			if agentConfigs.UseToolSearchMode != nil {
				metadata["use_tool_search_mode"] = *agentConfigs.UseToolSearchMode
			}
			if agentConfigs.LearningDetailLevel != "" {
				metadata["learning_detail_level"] = agentConfigs.LearningDetailLevel
			}
			if agentConfigs.LockLearnings != nil {
				metadata["lock_learnings"] = *agentConfigs.LockLearnings
			}
		}

		learningsMap[stepID] = metadata
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
	Title                       *string                                    `json:"title,omitempty"`
	Description                 *string                                    `json:"description,omitempty"`
	SuccessCriteria             *string                                    `json:"success_criteria,omitempty"`
	ContextDependencies         *[]string                                  `json:"context_dependencies,omitempty"`
	ContextOutput *todo_creation_human.FlexibleContextOutput `json:"context_output,omitempty"`

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

	// Decision step fields
	DecisionStep               json.RawMessage `json:"decision_step,omitempty"` // Will be converted to PlanStepInterface
	DecisionEvaluationQuestion *string         `json:"decision_evaluation_question,omitempty"`

	// Orchestration step fields
	OrchestrationStep   json.RawMessage                               `json:"orchestration_step,omitempty"` // Will be converted to PlanStepInterface
	OrchestrationRoutes *[]todo_creation_human.PlanOrchestrationRoute `json:"orchestration_routes,omitempty"`
	NextStepID          *string                                       `json:"next_step_id,omitempty"`

	// TodoTask step fields
	PredefinedRoutes   *[]todo_creation_human.PlanOrchestrationRoute `json:"predefined_routes,omitempty"`
	EnableGenericAgent *bool                                         `json:"enable_generic_agent,omitempty"`
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

// readStepOverrideFromWorkspace reads step_override.json from workspace using workspace API
// Returns nil, nil if the file doesn't exist (no overrides configured)
func readStepOverrideFromWorkspace(ctx context.Context, workspacePath string) (*todo_creation_human.AgentConfigs, error) {
	overridePath := workspacePath + "/planning/step_override.json"

	// URL-encode the filepath segments
	pathSegments := strings.Split(overridePath, "/")
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

	// File doesn't exist - return nil (no overrides)
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
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
	var overrideContent string
	if fileContent, ok := apiResp.Data.(virtualtools.WorkspaceFileContent); ok {
		overrideContent = fileContent.Content
	} else if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		if content, ok := dataMap["content"].(string); ok {
			overrideContent = content
		}
	}

	if overrideContent == "" {
		return nil, nil
	}

	// Parse step override
	overrides, err := todo_creation_human.ParseStepOverrideContent(overrideContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse step_override.json: %w", err)
	}

	return overrides, nil
}

// writeStepOverrideToWorkspace writes step_override.json to workspace using workspace API
func writeStepOverrideToWorkspace(ctx context.Context, workspacePath string, overrides *todo_creation_human.AgentConfigs) error {
	overridePath := workspacePath + "/planning/step_override.json"

	// Marshal to JSON
	overrideJSON, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal step_override.json: %w", err)
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(overridePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Prepare request body
	requestBody := map[string]interface{}{
		"content": string(overrideJSON),
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

// deleteStepOverrideFromWorkspace deletes step_override.json from workspace using workspace API
func deleteStepOverrideFromWorkspace(ctx context.Context, workspacePath string) error {
	overridePath := workspacePath + "/planning/step_override.json"

	// URL-encode the filepath segments
	pathSegments := strings.Split(overridePath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Delete file via workspace API (confirm=true required by workspace API)
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath + "?confirm=true"
	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// 404 is fine - file doesn't exist
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
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
	case *todo_creation_human.DecisionPlanStep:
		// DecisionPlanStep now has flattened structure (no nested DecisionStep)
		// The step itself contains all fields - no nested steps to search
	case *todo_creation_human.OrchestrationPlanStep:
		if s.OrchestrationStep != nil {
			if s.OrchestrationStep.GetID() == targetID {
				return s.OrchestrationStep, append(basePath, -2)
			}
			if found, foundPath := findInSteps([]todo_creation_human.PlanStepInterface{s.OrchestrationStep}, targetID, basePath); found != nil {
				return found, foundPath
			}
		}
		for routeIdx, route := range s.OrchestrationRoutes {
			if route.SubAgentStep != nil {
				if route.SubAgentStep.GetID() == targetID {
					return route.SubAgentStep, append(basePath, -3, routeIdx)
				}
				if found, foundPath := findInSteps([]todo_creation_human.PlanStepInterface{route.SubAgentStep}, targetID, basePath); found != nil {
					return found, foundPath
				}
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
			case *todo_creation_human.DecisionPlanStep:
				// DecisionPlanStep now has flattened structure (no nested DecisionStep)
				// The step itself contains all fields - no nested steps to search
			case *todo_creation_human.OrchestrationPlanStep:
				// Check orchestration_step
				if s.OrchestrationStep != nil {
					if s.OrchestrationStep.GetID() == targetID {
						return s.OrchestrationStep, append(currentPath, -2) // -2 indicates orchestration_step
					}
					// Recursively check nested steps in orchestration_step
					if found, foundPath := findInSteps([]todo_creation_human.PlanStepInterface{s.OrchestrationStep}, targetID, currentPath); found != nil {
						return found, foundPath
					}
				}
				// Check orchestration_routes
				for j, route := range s.OrchestrationRoutes {
					if route.SubAgentStep != nil {
						if route.SubAgentStep.GetID() == targetID {
							return route.SubAgentStep, append(currentPath, -3, j) // -3 indicates orchestration_routes
						}
						// Recursively check nested steps in sub_agent_step
						if found, foundPath := findInSteps([]todo_creation_human.PlanStepInterface{route.SubAgentStep}, targetID, currentPath); found != nil {
							return found, foundPath
						}
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

	case *todo_creation_human.DecisionPlanStep:
		updated := *s // Copy the step
		// Decision steps now have flattened structure with all common fields
		if updates.Title != nil {
			updated.Title = *updates.Title
		}
		if updates.Description != nil {
			updated.Description = *updates.Description
		}
		if updates.SuccessCriteria != nil {
			updated.SuccessCriteria = *updates.SuccessCriteria
		}
		if updates.ContextDependencies != nil {
			updated.ContextDependencies = *updates.ContextDependencies
		}
		if updates.ContextOutput != nil {
			updated.ContextOutput = *updates.ContextOutput
		}
		if updates.DecisionEvaluationQuestion != nil {
			updated.DecisionEvaluationQuestion = *updates.DecisionEvaluationQuestion
		}
		if updates.IfTrueNextStepID != nil {
			updated.IfTrueNextStepID = *updates.IfTrueNextStepID
		}
		if updates.IfFalseNextStepID != nil {
			updated.IfFalseNextStepID = *updates.IfFalseNextStepID
		}
		updatedStep = &updated

	case *todo_creation_human.OrchestrationPlanStep:
		updated := *s // Copy the step
		// Orchestration steps only have ID and Title in common fields
		if updates.Title != nil {
			updated.Title = *updates.Title
		}
		if updates.NextStepID != nil {
			updated.NextStepID = *updates.NextStepID
		}
		if updates.OrchestrationRoutes != nil {
			updated.OrchestrationRoutes = *updates.OrchestrationRoutes
		}
		// Handle nested orchestration_step
		if len(updates.OrchestrationStep) > 0 {
			step, err := unmarshalStepFromJSON(updates.OrchestrationStep)
			if err != nil {
				return fmt.Errorf("failed to unmarshal orchestration_step: %w", err)
			}
			updated.OrchestrationStep = step
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
		if updates.EnableGenericAgent != nil {
			updated.EnableGenericAgent = *updates.EnableGenericAgent
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
	case "decision":
		var s todo_creation_human.DecisionPlanStep
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal decision step: %w", err)
		}
		return &s, nil
	case "orchestration":
		var s todo_creation_human.OrchestrationPlanStep
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal orchestration step: %w", err)
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
	case *todo_creation_human.DecisionPlanStep:
		// DecisionPlanStep now has flattened structure (no nested DecisionStep)
		// The step itself is updated directly at the parent level, no nested updates needed
		return fmt.Errorf("unexpected nested path for decision step - decision steps no longer have nested steps")
	case *todo_creation_human.OrchestrationPlanStep:
		if len(path) == 2 && path[1] == -2 {
			// orchestration_step
			s.OrchestrationStep = updatedStep
			return nil
		}
		if len(path) >= 3 && path[1] == -3 {
			// orchestration_routes[path[2]].sub_agent_step
			routeIndex := path[2]
			if routeIndex >= 0 && routeIndex < len(s.OrchestrationRoutes) {
				if len(path) == 3 {
					s.OrchestrationRoutes[routeIndex].SubAgentStep = updatedStep
					return nil
				}
				// Deeper nesting in sub_agent_step
				return updateNestedStepInPlanRecursive([]todo_creation_human.PlanStepInterface{s.OrchestrationRoutes[routeIndex].SubAgentStep}, nil, path[2:], updatedStep)
			}
		}
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
		case *todo_creation_human.DecisionPlanStep:
			// DecisionPlanStep now has flattened structure (no nested DecisionStep)
			return fmt.Errorf("unexpected nested path for decision step - decision steps no longer have nested steps")
		case *todo_creation_human.OrchestrationPlanStep:
			if s.OrchestrationStep != nil {
				return updateNestedStepInPlanRecursive([]todo_creation_human.PlanStepInterface{s.OrchestrationStep}, nil, path[1:], updatedStep)
			}
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
			case *todo_creation_human.DecisionPlanStep:
				// DecisionPlanStep now has flattened structure (no nested DecisionStep)
				return fmt.Errorf("unexpected nested path for decision step - decision steps no longer have nested steps")
			case *todo_creation_human.OrchestrationPlanStep:
				if s.OrchestrationStep != nil {
					return updateNestedStepInPlanRecursive([]todo_creation_human.PlanStepInterface{s.OrchestrationStep}, nil, path[1:], updatedStep)
				}
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

// handleGetStepOverride returns the current step_override.json content
func (api *StreamingAPI) handleGetStepOverride(w http.ResponseWriter, r *http.Request) {
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

	overrides, err := readStepOverrideFromWorkspace(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read step override: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to map for JSON response
	var agentConfigsMap map[string]interface{}
	if overrides != nil {
		configJSON, err := json.Marshal(overrides)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to marshal overrides: %v", err), http.StatusInternalServerError)
			return
		}
		json.Unmarshal(configJSON, &agentConfigsMap)
	}

	response := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"agent_configs": agentConfigsMap,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleUpdateStepOverride updates or deletes step_override.json
func (api *StreamingAPI) handleUpdateStepOverride(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		WorkspacePath string                 `json:"workspace_path"`
		AgentConfigs  map[string]interface{} `json:"agent_configs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}

	// If agent_configs is null/empty, delete the override file
	if req.AgentConfigs == nil || len(req.AgentConfigs) == 0 {
		if err := deleteStepOverrideFromWorkspace(r.Context(), req.WorkspacePath); err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete step override: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"success": true,
			"message": "Step overrides cleared",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Convert map to AgentConfigs struct
	configJSON, err := json.Marshal(req.AgentConfigs)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal agent_configs: %v", err), http.StatusBadRequest)
		return
	}
	var agentConfigs todo_creation_human.AgentConfigs
	if err := json.Unmarshal(configJSON, &agentConfigs); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unmarshal agent_configs: %v", err), http.StatusBadRequest)
		return
	}

	// Write step_override.json
	if err := writeStepOverrideToWorkspace(r.Context(), req.WorkspacePath, &agentConfigs); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write step override: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Step overrides updated successfully",
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
		http.Error(w, "step must have 'type' field (regular, conditional, decision, or orchestration)", http.StatusBadRequest)
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
	case "decision":
		var s todo_creation_human.DecisionPlanStep
		if err := json.Unmarshal(stepJSON, &s); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse decision step: %v", err), http.StatusBadRequest)
			return
		}
		newStep = &s
	case "orchestration":
		var s todo_creation_human.OrchestrationPlanStep
		if err := json.Unmarshal(stepJSON, &s); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse orchestration step: %v", err), http.StatusBadRequest)
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

	var logsBasePath string
	var executionBasePath string
	if runFolder != "" && runFolder != "new" {
		logsBasePath = fmt.Sprintf("%s/runs/%s/logs", cleanedWorkspacePath, runFolder)
		executionBasePath = fmt.Sprintf("%s/runs/%s/execution", cleanedWorkspacePath, runFolder)
	} else {
		logsBasePath = fmt.Sprintf("%s/logs", cleanedWorkspacePath)
		executionBasePath = fmt.Sprintf("%s/execution", cleanedWorkspacePath)
	}

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

	execResp, err := client.Do(execReq)
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

	// 1. Direct path: runs/{runFolder}/token_usage.json (for single group or group-specific folder)
	// 2. Group subfolders: runs/{runFolder}/{groupName}/token_usage.json (for parent iteration folder)
	var tokenUsage interface{} = nil
	tokenUsagePath := ""
	if runFolder != "" && runFolder != "new" {
		tokenUsagePath = fmt.Sprintf("%s/runs/%s/token_usage.json", cleanedWorkspacePath, runFolder)
	} else {
		// Try root workspace if no run folder
		tokenUsagePath = fmt.Sprintf("%s/token_usage.json", cleanedWorkspacePath)
	}

	if tokenUsagePath != "" {
		content, exists, _ := readFileFromWorkspace(r.Context(), tokenUsagePath)
		if exists {
			if err := json.Unmarshal([]byte(content), &tokenUsage); err != nil {
				fmt.Printf("Error unmarshalling token usage from %s: %v\n", tokenUsagePath, err)
			}
		} else if runFolder != "" && runFolder != "new" && !strings.Contains(runFolder, "/") {
			// Token usage not found at direct path and runFolder is a parent iteration folder (no "/")
			// Try to find and aggregate token_usage.json from group subfolders
			runFolderPath := fmt.Sprintf("%s/runs/%s", cleanedWorkspacePath, runFolder)
			aggregatedTokenUsage := aggregateGroupTokenUsage(r.Context(), runFolderPath)
			if aggregatedTokenUsage != nil {
				tokenUsage = aggregatedTokenUsage
			}
		}
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

	// Try to read token_usage.json for the run
	// With group folders, token_usage.json may be in:
	// 1. Direct path: runs/{runFolder}/token_usage.json (for single group or group-specific folder)
	// 2. Group subfolders: runs/{runFolder}/{groupName}/token_usage.json (for parent iteration folder)
	var tokenUsage interface{} = nil
	tokenUsagePath := ""
	if runFolder != "" && runFolder != "new" {
		tokenUsagePath = fmt.Sprintf("%s/runs/%s/token_usage.json", cleanedWorkspacePath, runFolder)
	} else {
		// Try root workspace if no run folder
		tokenUsagePath = fmt.Sprintf("%s/token_usage.json", cleanedWorkspacePath)
	}

	if tokenUsagePath != "" {
		content, exists, _ := readFileFromWorkspace(r.Context(), tokenUsagePath)
		if exists {
			if err := json.Unmarshal([]byte(content), &tokenUsage); err != nil {
				fmt.Printf("Error unmarshalling token usage from %s: %v\n", tokenUsagePath, err)
			}
		} else if runFolder != "" && runFolder != "new" && !strings.Contains(runFolder, "/") {
			// Token usage not found at direct path and runFolder is a parent iteration folder (no "/")
			// Try to find and aggregate token_usage.json from group subfolders
			runFolderPath := fmt.Sprintf("%s/runs/%s", cleanedWorkspacePath, runFolder)
			aggregatedTokenUsage := aggregateGroupTokenUsage(r.Context(), runFolderPath)
			if aggregatedTokenUsage != nil {
				tokenUsage = aggregatedTokenUsage
			}
		}
	}

	// Also try to read evaluation token_usage.json
	var evaluationTokenUsage interface{} = nil
	if runFolder != "" && runFolder != "new" {
		evalTokenUsagePath := fmt.Sprintf("%s/evaluation/runs/%s/token_usage.json", cleanedWorkspacePath, runFolder)
		content, exists, _ := readFileFromWorkspace(r.Context(), evalTokenUsagePath)
		if exists {
			if err := json.Unmarshal([]byte(content), &evaluationTokenUsage); err != nil {
				fmt.Printf("Error unmarshalling evaluation token usage from %s: %v\n", evalTokenUsagePath, err)
			}
		}
	}

	response := map[string]interface{}{
		"success":                true,
		"token_usage":            tokenUsage,
		"evaluation_token_usage": evaluationTokenUsage,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

	// Evaluation reports are in evaluation/runs/{targetRunFolder}/evaluation_report.json
	evaluationRunsPath := fmt.Sprintf("%s/evaluation/runs", cleanedWorkspacePath)

	type StepOutputContent struct {
		FilePath string      `json:"file_path"`
		Content  interface{} `json:"content"`
		IsJSON   bool        `json:"is_json"`
	}

	type EvaluationStepScore struct {
		StepID          string             `json:"step_id"`
		StepTitle       string             `json:"step_title"`
		Score           int                `json:"score"`
		MaxScore        int                `json:"max_score"`
		Reasoning       string             `json:"reasoning"`
		Evidence        string             `json:"evidence"`
		SuccessCriteria string             `json:"success_criteria"`
		ContextOutput   string             `json:"context_output,omitempty"`
		OutputContent   *StepOutputContent `json:"output_content,omitempty"`
	}

	type EvaluationReport struct {
		TargetRunFolder  string                `json:"target_run_folder"`
		GeneratedAt      string                `json:"generated_at"`
		TotalScore       int                   `json:"total_score"`
		MaxPossibleScore int                   `json:"max_possible_score"`
		ScorePercentage  float64               `json:"score_percentage"`
		StepScores       []EvaluationStepScore `json:"step_scores"`
		Summary          string                `json:"summary"`
	}

	type EvaluationReportEntry struct {
		RunFolder string           `json:"run_folder"`
		Report    EvaluationReport `json:"report"`
	}

	type EvaluationAggregate struct {
		TotalRuns         int     `json:"total_runs"`
		AverageScore      float64 `json:"average_score"`
		AveragePercentage float64 `json:"average_percentage"`
		HighestScore      int     `json:"highest_score"`
		LowestScore       int     `json:"lowest_score"`
		MaxPossibleScore  int     `json:"max_possible_score"`
	}

	type Response struct {
		Success        bool                    `json:"success"`
		Reports        []EvaluationReportEntry `json:"reports"`
		Aggregate      *EvaluationAggregate    `json:"aggregate,omitempty"`
		EvaluationPlan *string                 `json:"evaluation_plan,omitempty"`
		Error          string                  `json:"error,omitempty"`
	}

	// List evaluation run folders
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{Success: false, Error: fmt.Sprintf("Failed to create request: %v", err)})
		return
	}

	q := req.URL.Query()
	q.Add("folder", evaluationRunsPath)
	q.Add("max_depth", "1")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{Success: false, Error: fmt.Sprintf("Failed to list evaluation runs: %v", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No evaluation runs folder exists yet - still try to read evaluation plan
		var evaluationPlan *string
		evaluationPlanPath := fmt.Sprintf("%s/evaluation/evaluation_plan.json", cleanedWorkspacePath)
		planContent, exists, err := readFileFromWorkspace(r.Context(), evaluationPlanPath)
		if err == nil && exists {
			// Try to format the JSON if it's valid JSON
			var planJSON interface{}
			if err := json.Unmarshal([]byte(planContent), &planJSON); err == nil {
				// Re-marshal with indentation for pretty printing
				if formatted, err := json.MarshalIndent(planJSON, "", "  "); err == nil {
					formattedStr := string(formatted)
					evaluationPlan = &formattedStr
				} else {
					// If formatting fails, use original content
					evaluationPlan = &planContent
				}
			} else {
				// If not valid JSON, use as-is
				evaluationPlan = &planContent
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Success:        true,
			Reports:        []EvaluationReportEntry{},
			EvaluationPlan: evaluationPlan,
		})
		return
	}

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{Success: false, Error: fmt.Sprintf("Failed to list evaluation runs (status %d)", resp.StatusCode)})
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{Success: false, Error: fmt.Sprintf("Failed to read response: %v", err)})
		return
	}

	// Parse folder listing
	type FolderListingResponse struct {
		Success bool                                `json:"success"`
		Data    virtualtools.WorkspaceFolderListing `json:"data"`
	}

	var listResp FolderListingResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{Success: false, Error: fmt.Sprintf("Failed to parse folder listing: %v", err)})
		return
	}

	var reports []EvaluationReportEntry

	// Iterate through each run folder (e.g., "iteration-1/faisal", "iteration-2")
	for _, item := range listResp.Data {
		if item.Type != "folder" {
			continue
		}

		// Extract folder name (the last segment of the path)
		folderName := filepath.Base(item.FilePath)

		// If run_folder filter is specified, only include matching folders
		if runFolder != "" && !strings.HasPrefix(folderName, runFolder) && folderName != runFolder {
			continue
		}

		// Check for evaluation_report.json in this folder
		// Also check for nested group folders (e.g., iteration-1/faisal/evaluation_report.json)
		reportPaths := []string{
			fmt.Sprintf("%s/evaluation_report.json", item.FilePath),
		}

		// Also check subfolders for group-based evaluation runs
		subFolderURL := getWorkspaceAPIURL() + "/api/documents"
		subReq, _ := http.NewRequestWithContext(r.Context(), "GET", subFolderURL, nil)
		subQ := subReq.URL.Query()
		subQ.Add("folder", item.FilePath)
		subQ.Add("max_depth", "1")
		subReq.URL.RawQuery = subQ.Encode()

		subResp, err := client.Do(subReq)
		if err == nil && subResp.StatusCode == http.StatusOK {
			subBody, _ := io.ReadAll(subResp.Body)
			subResp.Body.Close()

			var subListResp FolderListingResponse
			if json.Unmarshal(subBody, &subListResp) == nil {
				for _, subItem := range subListResp.Data {
					if subItem.Type == "folder" {
						subFolderName := filepath.Base(subItem.FilePath)
						if runFolder != "" && !strings.Contains(subItem.FilePath, runFolder) {
							continue
						}
						reportPaths = append(reportPaths, fmt.Sprintf("%s/evaluation_report.json", subItem.FilePath))
						// Update folder name to include subfolder
						_ = subFolderName // Used below when creating entries
					}
				}
			}
		}

		// Try to read evaluation_report.json from each path
		for _, reportPath := range reportPaths {
			content, exists, err := readFileFromWorkspace(r.Context(), reportPath)
			if err != nil || !exists {
				continue
			}

			var report EvaluationReport
			if err := json.Unmarshal([]byte(content), &report); err != nil {
				continue
			}

			// Determine the run folder name for this report
			// Extract from path: evaluation/runs/{run_folder}/evaluation_report.json
			relPath := strings.TrimPrefix(reportPath, evaluationRunsPath+"/")
			runFolderName := strings.TrimSuffix(relPath, "/evaluation_report.json")

			reports = append(reports, EvaluationReportEntry{
				RunFolder: runFolderName,
				Report:    report,
			})
		}
	}

	// Sort reports by generated_at (most recent first)
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Report.GeneratedAt > reports[j].Report.GeneratedAt
	})

	// Calculate aggregate statistics if we have multiple reports
	var aggregate *EvaluationAggregate
	if len(reports) > 0 {
		totalScore := 0
		totalPercentage := 0.0
		highestScore := 0
		lowestScore := reports[0].Report.TotalScore
		maxPossible := 0

		for _, entry := range reports {
			totalScore += entry.Report.TotalScore
			totalPercentage += entry.Report.ScorePercentage

			if entry.Report.TotalScore > highestScore {
				highestScore = entry.Report.TotalScore
			}
			if entry.Report.TotalScore < lowestScore {
				lowestScore = entry.Report.TotalScore
			}
			if entry.Report.MaxPossibleScore > maxPossible {
				maxPossible = entry.Report.MaxPossibleScore
			}
		}

		aggregate = &EvaluationAggregate{
			TotalRuns:         len(reports),
			AverageScore:      float64(totalScore) / float64(len(reports)),
			AveragePercentage: totalPercentage / float64(len(reports)),
			HighestScore:      highestScore,
			LowestScore:       lowestScore,
			MaxPossibleScore:  maxPossible,
		}
	}

	// Read evaluation plan if it exists
	var evaluationPlan *string
	evaluationPlanPath := fmt.Sprintf("%s/evaluation/evaluation_plan.json", cleanedWorkspacePath)
	planContent, exists, err := readFileFromWorkspace(r.Context(), evaluationPlanPath)
	if err == nil && exists {
		// Try to format the JSON if it's valid JSON
		var planJSON interface{}
		if err := json.Unmarshal([]byte(planContent), &planJSON); err == nil {
			// Re-marshal with indentation for pretty printing
			if formatted, err := json.MarshalIndent(planJSON, "", "  "); err == nil {
				formattedStr := string(formatted)
				evaluationPlan = &formattedStr
			} else {
				// If formatting fails, use original content
				evaluationPlan = &planContent
			}
		} else {
			// If not valid JSON, use as-is
			evaluationPlan = &planContent
		}
	}

	response := Response{
		Success:        true,
		Reports:        reports,
		Aggregate:      aggregate,
		EvaluationPlan: evaluationPlan,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
		if inner, ok := step["decision_step"].(map[string]interface{}); ok {
			if desc == "" {
				if innerDesc, ok := inner["description"].(string); ok {
					desc = innerDesc
				}
			}
			if criteria == "" {
				if innerCriteria, ok := inner["success_criteria"].(string); ok {
					criteria = innerCriteria
				}
			}
		} else if inner, ok := step["orchestration_step"].(map[string]interface{}); ok {
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

		// Also check decision_step for inner step context_output
		if len(contextOutputs) == 0 {
			if inner, ok := step["decision_step"].(map[string]interface{}); ok {
				if co, ok := inner["context_output"].(string); ok && co != "" {
					contextOutputs = []string{co}
				} else if coList, ok := inner["context_output"].([]interface{}); ok {
					for _, co := range coList {
						if coStr, ok := co.(string); ok && coStr != "" {
							contextOutputs = append(contextOutputs, coStr)
						}
					}
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

		// Handle decision inner step
		if decisionStep, ok := step["decision_step"].(map[string]interface{}); ok {
			decisionKey := stepKey + "-decision"
			dTitle, _ := decisionStep["title"].(string)
			dDesc, _ := decisionStep["description"].(string)
			dId, _ := decisionStep["id"].(string)
			dCriteria, _ := decisionStep["success_criteria"].(string)

			// Extract context_output for inner decision step
			var dContextOutputs []string
			if co, ok := decisionStep["context_output"].(string); ok && co != "" {
				dContextOutputs = []string{co}
			} else if coList, ok := decisionStep["context_output"].([]interface{}); ok {
				for _, co := range coList {
					if coStr, ok := co.(string); ok && coStr != "" {
						dContextOutputs = append(dContextOutputs, coStr)
					}
				}
			}

			dMeta := map[string]string{
				"title":            dTitle,
				"description":      dDesc,
				"success_criteria": dCriteria,
				"original_id":      dId,
				"type":             "decision-inner",
				"context_output":   strings.Join(dContextOutputs, ","),
			}
			metadata[decisionKey] = dMeta
			if dId != "" {
				metadata[dId] = dMeta
			}
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

// Config files that are versioned when publishing a workflow version
var versionedConfigFiles = []string{
	"planning/plan.json",
	"planning/step_config.json",
	"planning/workflow_layout.json",
	"planning/step_override.json",
	"variables/variables.json",
	"evaluation/evaluation_plan.json",
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

	client := &http.Client{Timeout: 30 * time.Second}
	listResp, err := client.Do(listReq)
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

	// Copy each config file to the version folder
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

	if len(filesSnapshot) == 0 {
		http.Error(w, "No config files found to version", http.StatusBadRequest)
		return
	}

	// Write version_meta.json
	meta := map[string]interface{}{
		"version":        nextVersion,
		"label":          req.Label,
		"created_at":     time.Now().UTC().Format(time.RFC3339),
		"files_snapshot": filesSnapshot,
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

	client := &http.Client{Timeout: 30 * time.Second}
	listResp, err := client.Do(listReq)
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
