package step_based_workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
)

// resolveRunFolder determines which run folder to use.
// Always runs in iteration-0. If iteration-0 already exists, it is moved to the
// next available iteration-N as a backup before creating a fresh iteration-0.
func (hcpo *StepBasedWorkflowOrchestrator) resolveRunFolder(ctx context.Context, workspacePath, runMode string) (string, error) {
	return hcpo.resolveRunFolderWithOptions(ctx, workspacePath, runMode, "")
}

// resolveRunFolderWithOptions always resolves to iteration-0.
// If iteration-0 exists and has content, it is moved to iteration-N (next available) as a backup.
// A fresh iteration-0 is then created for the new run.
func (hcpo *StepBasedWorkflowOrchestrator) resolveRunFolderWithOptions(ctx context.Context, workspacePath, runMode, selectedRunFolder string) (string, error) {
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	iteration0Path := fmt.Sprintf("%s/iteration-0", runsPath)

	// Check if iteration-0 exists — if so, back it up
	if hcpo.workspaceFileExists(ctx, iteration0Path) {
		backupName, err := hcpo.nextAvailableIteration(ctx, runsPath)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("Failed to determine backup iteration number: %v, will overwrite iteration-0", err))
		} else {
			backupPath := fmt.Sprintf("%s/%s", runsPath, backupName)
			hcpo.GetLogger().Info(fmt.Sprintf("Backing up iteration-0 -> %s", backupName))
			if err := hcpo.MoveWorkspaceFile(ctx, iteration0Path, backupPath); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("Failed to backup iteration-0 to %s: %v, will overwrite", backupName, err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("Backed up iteration-0 -> %s", backupName))
			}
		}
	}

	// Create fresh iteration-0
	if err := hcpo.createRunFolderStructure(ctx, iteration0Path); err != nil {
		return "", fmt.Errorf("failed to create iteration-0: %w", err)
	}

	hcpo.GetLogger().Info("Using iteration-0 for workflow execution")
	return "iteration-0", nil
}

// nextAvailableIteration finds the next available iteration-N name (N >= 1) under runsPath.
func (hcpo *StepBasedWorkflowOrchestrator) nextAvailableIteration(ctx context.Context, runsPath string) (string, error) {
	existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
	if err != nil {
		// If we can't list, start from 1
		return "iteration-1", nil
	}

	maxIter := 0
	re := regexp.MustCompile(`^iteration-(\d+)$`)
	for _, folder := range existingFolders {
		matches := re.FindStringSubmatch(folder)
		if len(matches) > 1 {
			var n int
			if _, err := fmt.Sscanf(matches[1], "%d", &n); err == nil && n > maxIter {
				maxIter = n
			}
		}
	}
	return fmt.Sprintf("iteration-%d", maxIter+1), nil
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

	// Builder-triggered single-step runs expect the same baseline execution tree as full
	// workflow runs. Create execution/ and execution/Downloads/ eagerly so browser-based
	// steps can inspect or move downloads before any step-specific folder is created.
	executionPath := filepath.Join(runPath, "execution")
	if err := createFolderViaAPI(ctx, executionPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create execution folder via API: %v (continuing)", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Created execution folder: %s", executionPath))
	}

	downloadsPath := filepath.Join(executionPath, "Downloads")
	if err := createFolderViaAPI(ctx, downloadsPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create execution Downloads folder via API: %v (continuing)", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Created execution Downloads folder: %s", downloadsPath))
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

// determineRunFolderForCleanup determines which run folder will be used (if any) without creating it.
// Always returns iteration-0 since that is the only folder workflows run in.
func (hcpo *StepBasedWorkflowOrchestrator) determineRunFolderForCleanup(ctx context.Context, workspacePath, runMode string) (string, bool, error) {
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	iteration0Path := fmt.Sprintf("%s/iteration-0", runsPath)

	if hcpo.workspaceFileExists(ctx, iteration0Path) {
		return "iteration-0", true, nil
	}
	// iteration-0 doesn't exist yet — nothing to clean
	return "", false, nil
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

// cleanupExecutionArtifactsForFreshStart cleans execution and validation artifacts based on run mode.
// This handles both new runs folder structure and old structure for backward compatibility.
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
