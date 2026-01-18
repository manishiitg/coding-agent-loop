package step_based_workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// resolveRunFolder determines which run folder to use based on the run mode
// Returns the selected run folder name (e.g., "iteration-1", "iteration-2")
// Note: "iteration-same" is deprecated - all folders now use numbered iterations
func (hcpo *StepBasedWorkflowOrchestrator) resolveRunFolder(ctx context.Context, workspacePath, runMode string) (string, error) {
	return hcpo.resolveRunFolderWithOptions(ctx, workspacePath, runMode, "")
}

// resolveRunFolderWithOptions determines which run folder to use based on the run mode and optional pre-selected folder
// If selectedRunFolder is provided, it will be used directly (for frontend-provided options)
// Returns the selected run folder name (e.g., "iteration-1", "iteration-2")
func (hcpo *StepBasedWorkflowOrchestrator) resolveRunFolderWithOptions(ctx context.Context, workspacePath, runMode, selectedRunFolder string) (string, error) {
	runsPath := fmt.Sprintf("%s/runs", workspacePath)

	// Default to "use_same_run" if runMode is empty
	if runMode == "" {
		runMode = "use_same_run"
		hcpo.GetLogger().Info(fmt.Sprintf("📁 No run_mode specified, defaulting to 'use_same_run'"))
	}

	switch runMode {
	case "use_same_run":
		// If frontend provided a specific folder, use it directly
		if selectedRunFolder != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("📁 Using frontend-selected run folder: %s", selectedRunFolder))
			// Ensure the folder exists
			fullPath := fmt.Sprintf("%s/%s", runsPath, selectedRunFolder)
			exists := hcpo.workspaceFileExists(ctx, fullPath)
			if !exists {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Selected folder %s doesn't exist, creating it", selectedRunFolder))
				if err := hcpo.createRunFolderStructure(ctx, fullPath); err != nil {
					return "", err
				}
			}
			return selectedRunFolder, nil
		}

		// Check if runs directory exists
		exists := hcpo.workspaceFileExists(ctx, runsPath)
		if !exists {
			// Create iteration-1 run folder (not iteration-same)
			selectedFolder := "iteration-1"
			if err := hcpo.createRunFolderStructure(ctx, fmt.Sprintf("%s/%s", runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// List existing run folders
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err != nil || len(existingFolders) == 0 {
			// Create iteration-1 folder if none exist (not iteration-same)
			selectedFolder := "iteration-1"
			if err := hcpo.createRunFolderStructure(ctx, fmt.Sprintf("%s/%s", runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// Filter to only numbered iteration folders (iteration-N pattern)
		// Note: iteration-same is deprecated but still supported for backward compatibility
		var iterationFolders []string

		hcpo.GetLogger().Info(fmt.Sprintf("🔍 DEBUG: Found %d existing folders: %v", len(existingFolders), existingFolders))

		for _, folder := range existingFolders {
			// Include all iteration-* folders (both numbered and legacy "iteration-same")
			if strings.HasPrefix(folder, "iteration-") {
				iterationFolders = append(iterationFolders, folder)
			}
		}

		// Sort iteration folders by iteration number
		// Supports formats: "iteration-N", "YYYY-MM-DD-iteration-N", or "YYYY-MM-DD-initial"
		if len(iterationFolders) > 0 {
			sort.Slice(iterationFolders, func(i, j int) bool {
				// Extract iteration number from folder name
				extractIteration := func(name string) int {
					// Try to match "iteration-N" pattern (works for both "iteration-N" and "YYYY-MM-DD-iteration-N")
					re := regexp.MustCompile(`iteration-(\d+)$`)
					matches := re.FindStringSubmatch(name)
					if len(matches) > 1 {
						var num int
						if _, err := fmt.Sscanf(matches[1], "%d", &num); err == nil {
							return num
						}
					}
					// Legacy "iteration-same" treated as 0 (so it appears last after numbered iterations)
					if name == "iteration-same" {
						return 0
					}
					// If "initial" or no match, treat as -1 (lowest priority)
					if strings.HasSuffix(name, "-initial") {
						return -1
					}
					return -2 // Unknown format, put at end
				}

				iterI := extractIteration(iterationFolders[i])
				iterJ := extractIteration(iterationFolders[j])

				// Sort by iteration number (descending - highest iteration first)
				if iterI != iterJ {
					return iterI > iterJ
				}
				// If same iteration number, sort alphabetically
				return iterationFolders[i] > iterationFolders[j]
			})
		}

		// Limit to 10 most recent folders
		folderOptions := iterationFolders
		if len(folderOptions) > 10 {
			folderOptions = folderOptions[:10]
		}

		// If only one folder exists, use it directly
		if len(folderOptions) == 1 {
			hcpo.GetLogger().Info(fmt.Sprintf("📁 Using the only existing run folder: %s", folderOptions[0]))
			return folderOptions[0], nil
		}

		// Multiple folders exist - ask user to select which one to use
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Found %d existing run folders, presenting %d options to user", len(existingFolders), len(folderOptions)))

		// Ask user to select which run folder to use
		requestID := fmt.Sprintf("select_run_folder_%d", time.Now().UnixNano())

		// Build appropriate question based on number of folders
		var questionText string
		if len(existingFolders) == 1 {
			questionText = "Which run folder would you like to use?"
		} else if len(folderOptions) == len(existingFolders) {
			questionText = fmt.Sprintf("Found %d run folders. Which one would you like to use?", len(existingFolders))
		} else {
			questionText = fmt.Sprintf("Found %d run folders (showing %d most recent). Which one would you like to use?", len(existingFolders), len(folderOptions))
		}

		contextMsg := fmt.Sprintf("Found %d existing run folder(s). Which one would you like to use?\n\n", len(existingFolders))
		if len(existingFolders) > len(folderOptions) {
			contextMsg += fmt.Sprintf("**Note:** Showing %d most recent folders (sorted by iteration number).\n\n", len(folderOptions))
		}
		contextMsg += "**Available folders:**\n"
		for _, folder := range folderOptions {
			contextMsg += fmt.Sprintf("- %s\n", folder)
		}

		choice, err := hcpo.RequestMultipleChoiceFeedback(
			ctx,
			requestID,
			questionText,
			folderOptions,
			contextMsg,
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to get user selection for run folder: %v, defaulting to first option", err))
			// Default to first option (highest iteration number)
			return folderOptions[0], nil
		}

		// Parse the choice (format: "option0", "option1", etc.)
		// Extract the index from the choice string
		var selectedIndex int
		if _, err := fmt.Sscanf(choice, "option%d", &selectedIndex); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse folder choice '%s': %v, defaulting to first option", choice, err))
			return folderOptions[0], nil
		}

		// Validate index
		if selectedIndex < 0 || selectedIndex >= len(folderOptions) {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Invalid folder index %d (max: %d), defaulting to first option", selectedIndex, len(folderOptions)-1))
			return folderOptions[0], nil
		}

		selectedFolder := folderOptions[selectedIndex]
		hcpo.GetLogger().Info(fmt.Sprintf("✅ User selected run folder: %s", selectedFolder))
		return selectedFolder, nil

	case "create_new_runs_always":
		// Always create a new iteration folder with incremental number
		counter := 1
		for {
			selectedFolder := fmt.Sprintf("iteration-%d", counter)
			fullPath := fmt.Sprintf("%s/%s", runsPath, selectedFolder)

			exists := hcpo.workspaceFileExists(ctx, fullPath)
			if !exists {
				if err := hcpo.createRunFolderStructure(ctx, fullPath); err != nil {
					return "", err
				}
				return selectedFolder, nil
			}
			counter++
		}

	default:
		return "", fmt.Errorf("unknown run mode: %s", runMode)
	}
}

// workspaceFileExists checks if a file or directory exists in the workspace
func (hcpo *StepBasedWorkflowOrchestrator) workspaceFileExists(ctx context.Context, path string) bool {
	// Try to read the path directly (for files)
	_, err := hcpo.ReadWorkspaceFile(ctx, path)
	if err == nil {
		return true
	}

	// Check if it exists by listing parent directory (for both files and directories)
	parent := filepath.Dir(path)
	filename := filepath.Base(path)

	// List files and directories in parent using BaseOrchestrator method
	items, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, parent)
	if err == nil {
		for _, item := range items {
			if item == filename {
				return true
			}
		}
	}

	return false
}

func (hcpo *StepBasedWorkflowOrchestrator) isStepLearningsFolderEmpty(ctx context.Context, stepID string, stepIndex int, stepPath string) (bool, error) {
	// getLearningFolderPathByStepID now returns RELATIVE path - workspace functions auto-prepend workspacePath
	// In evaluation mode, learnings are stored in evaluation/learnings/
	stepLearningsPath := getLearningFolderPathByStepID("", stepID, stepPath, hcpo.isEvaluationMode)

	// Use readStepLearningFiles to check for learning files (it already excludes metadata)
	learningFiles, err := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
	if err != nil {
		// If folder doesn't exist or can't be read, assume empty (conservative approach - will use tempLLM)
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Step %s learnings folder does not exist or cannot be read: %s (will use tempLLM if available)", stepID, stepLearningsPath))
		return true, err
	}

	if len(learningFiles) == 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Step %s learnings folder has no learning files (only metadata or empty): %s (will use tempLLM if available)", stepID, stepLearningsPath))
		return true, nil
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %s learnings folder has %d learning file(s): %s (will use tempLLM if available)", stepID, len(learningFiles), stepLearningsPath))
	return false, nil
}

// listRunFolders lists existing run folder names
func (hcpo *StepBasedWorkflowOrchestrator) listRunFolders(ctx context.Context, runsPath string) ([]string, error) {
	// Use BaseOrchestrator's ListWorkspaceDirectories function
	return hcpo.BaseOrchestrator.ListWorkspaceDirectories(ctx, runsPath)
}

// createRunFolderStructure creates the basic structure for a run folder.
// Creates folders via Workspace API only (ensures consistency with list_workspace_files).
//
// The runPath can be in any format - it will be normalized by createFolderViaAPI.
// Typically runPath is already relative to workspace-docs root (e.g., "Workflow/ICICI.../runs/...").
//
// NOTE: Do NOT use os.MkdirAll here - see controller_execution.go for explanation.
func (hcpo *StepBasedWorkflowOrchestrator) createRunFolderStructure(ctx context.Context, runPath string) error {
	if runPath == "" {
		return fmt.Errorf("invalid run path: empty")
	}

	// Create run folder via Workspace API - normalization happens inside
	if err := createFolderViaAPI(ctx, runPath); err != nil {
		return fmt.Errorf("failed to create run folder: %w", err)
	}

	// Create knowledgebase folder at workspace root (shared across all runs) - only if enabled
	if hcpo.UseKnowledgebase() {
		workspacePath := hcpo.GetWorkspacePath()
		if err := createFolderViaAPI(ctx, KnowledgebaseFolderName, workspacePath); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create knowledgebase folder via API: %v (continuing)", err))
			// Don't fail - knowledgebase folder will be created when first file is written
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Created knowledgebase folder: %s/%s", workspacePath, KnowledgebaseFolderName))
		}
	} else {
		hcpo.GetLogger().Info("⏭️ Skipping knowledgebase folder creation (disabled in preset)")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Created run folder structure: %s", runPath))
	return nil
}

// determineRunFolderForCleanup determines which run folder will be used (if any) without creating it
// Returns: (runFolderName, shouldCleanSpecificFolder, error)
// - runFolderName: The folder name that will be used (empty if new folder will be created)
// - shouldCleanSpecificFolder: Whether we should clean a specific folder (true if reusing existing folder)
func (hcpo *StepBasedWorkflowOrchestrator) determineRunFolderForCleanup(ctx context.Context, workspacePath, runMode string) (string, bool, error) {
	runsPath := fmt.Sprintf("%s/runs", workspacePath)

	// Default to "use_same_run" if runMode is empty
	if runMode == "" {
		runMode = "use_same_run"
	}

	switch runMode {
	case "use_same_run":
		// Check if runs directory exists
		exists := hcpo.workspaceFileExists(ctx, runsPath)
		if !exists {
			// Will create "iteration-same" - no existing folder to clean
			return "", false, nil
		}

		// List existing run folders
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err != nil || len(existingFolders) == 0 {
			// Will create "iteration-same" - no existing folder to clean
			return "", false, nil
		}

		// Will reuse the latest folder - should clean it
		sort.Strings(existingFolders)
		return existingFolders[len(existingFolders)-1], true, nil

	case "create_new_runs_always":
		// Always creates new folder - no specific folder to clean
		return "", false, nil

	default:
		return "", false, fmt.Errorf("unknown run mode: %s", runMode)
	}
}

// shouldAskDeleteOldProgress determines if we should ask the "Delete old progress" question
// Returns true only when we're reusing an existing folder that might have old progress
func (hcpo *StepBasedWorkflowOrchestrator) shouldAskDeleteOldProgress(ctx context.Context, workspacePath, runMode string) bool {
	_, shouldClean, err := hcpo.determineRunFolderForCleanup(ctx, workspacePath, runMode)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to determine run folder for cleanup check: %v, defaulting to ask question", err))
		return true // Default to asking if we can't determine
	}
	return shouldClean
}

// cleanupExecutionArtifactsForFreshStart cleans execution and validation artifacts based on run mode
// This handles both new runs folder structure and old structure for backward compatibility
func (hcpo *StepBasedWorkflowOrchestrator) cleanupExecutionArtifactsForFreshStart(ctx context.Context, workspacePath, runMode string) {
	hcpo.GetLogger().Info(fmt.Sprintf("🧹 Starting cleanup of execution artifacts for fresh start (run_mode: %s)", runMode))

	// Check if a specific run folder was already selected (from frontend or earlier resolution)
	var runFolderName string
	var shouldCleanSpecificFolder bool

	if hcpo.selectedRunFolder != "" {
		// Use the already-selected run folder (from frontend options)
		runFolderName = hcpo.selectedRunFolder
		shouldCleanSpecificFolder = true
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Using already-selected run folder for cleanup: %s", runFolderName))
	} else {
		// Determine which run folder will be used (if any)
		var err error
		runFolderName, shouldCleanSpecificFolder, err = hcpo.determineRunFolderForCleanup(ctx, workspacePath, runMode)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to determine run folder for cleanup: %v, will only clean old structure", err))
			shouldCleanSpecificFolder = false
		}
	}

	// Clean specific run folder if we're reusing it
	if shouldCleanSpecificFolder && runFolderName != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Cleaning specific run folder: %s", runFolderName))
		runFolderPath := fmt.Sprintf("%s/runs/%s", workspacePath, runFolderName)

		// Clean execution directory in run folder (this will recursively delete all step-* subdirectories)
		executionDir := fmt.Sprintf("%s/execution", runFolderPath)
		if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup execution directory in run folder: %v", err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned up execution directory in run folder: %s (including all step-* subdirectories)", executionDir))
		}
		// Also clean logs directory in run folder
		logsDir := fmt.Sprintf("%s/logs", runFolderPath)
		if err := hcpo.CleanupDirectory(ctx, logsDir, "logs"); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup logs directory in run folder: %v", err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned up logs directory in run folder: %s", logsDir))
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📁 No specific run folder to clean (will create new folder or use new structure)"))
	}

	// Always clean old structure for backward compatibility
	hcpo.GetLogger().Info(fmt.Sprintf("🧹 Cleaning old structure for backward compatibility"))

	// Clean old execution directory
	oldExecutionDir := fmt.Sprintf("%s/execution", workspacePath)
	if err := hcpo.CleanupDirectory(ctx, oldExecutionDir, "execution"); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup old execution directory: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned up old execution directory: %s", oldExecutionDir))
	}
	// Also clean old logs directory
	oldLogsDir := fmt.Sprintf("%s/logs", workspacePath)
	if err := hcpo.CleanupDirectory(ctx, oldLogsDir, "logs"); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup old logs directory: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned up old logs directory: %s", oldLogsDir))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Completed cleanup of execution artifacts for fresh start"))
}
