package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"

	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

const (
	defaultRunRetentionCount = 5
	maxRunRetentionCount     = 50
)

// resolveRunFolderWithOptions always resolves to iteration-0 under the workflow's
// runs/ tree. The previous iteration-0 (workflow + eval) is rotated to the same
// numbered backup so workflow run N and its eval at evaluation/runs/iteration-N
// stay paired by construction. workflow.json run_retention_count controls backup
// retention for both trees, defaulting to 5 when omitted.
func (hcpo *StepBasedWorkflowOrchestrator) resolveRunFolderWithOptions(ctx context.Context, workspacePath, runMode, selectedRunFolder string) (string, error) {
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	evalRunsPath := fmt.Sprintf("%s/evaluation/runs", workspacePath)
	runRetentionCount := hcpo.resolveRunRetentionCount(ctx)
	if err := hcpo.rotatePairedIterationZero(ctx, runsPath, evalRunsPath, runRetentionCount); err != nil {
		return "", err
	}
	hcpo.GetLogger().Info(fmt.Sprintf("Using iteration-0 for workflow execution (keeping %d backup iteration(s))", runRetentionCount))
	return "iteration-0", nil
}

func (hcpo *StepBasedWorkflowOrchestrator) resolveRunRetentionCount(ctx context.Context) int {
	content, err := hcpo.ReadWorkspaceFile(ctx, "workflow.json")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("Could not read workflow.json for run retention, using default %d: %v", defaultRunRetentionCount, err))
		return defaultRunRetentionCount
	}

	var manifest struct {
		RunRetentionCount *int `json:"run_retention_count,omitempty"`
	}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("Could not parse workflow.json run_retention_count, using default %d: %v", defaultRunRetentionCount, err))
		return defaultRunRetentionCount
	}
	if manifest.RunRetentionCount == nil {
		return defaultRunRetentionCount
	}
	if *manifest.RunRetentionCount < 1 || *manifest.RunRetentionCount > maxRunRetentionCount {
		hcpo.GetLogger().Warn(fmt.Sprintf("Invalid workflow.json run_retention_count=%d, using default %d", *manifest.RunRetentionCount, defaultRunRetentionCount))
		return defaultRunRetentionCount
	}
	return *manifest.RunRetentionCount
}

// rotatePairedIterationZero rotates iteration-0 in both the workflow runs tree
// and the eval runs tree to the SAME backup name. This keeps run N and its
// eval N paired, so Pulse reviewers and report viewers can resolve both halves
// from one index.
//
// The next backup name is computed across both trees (max+1 of any iteration-N
// in either), so the chosen name is fresh in both. If only one of the iteration-0
// folders exists, only that one is moved — the other tree just doesn't get a
// new backup at this index.
//
// A fresh workflow iteration-0 is then created. Eval iteration-0 is created
// lazily by ExecuteEvaluationOnly when an eval run actually starts.
func (hcpo *StepBasedWorkflowOrchestrator) rotatePairedIterationZero(ctx context.Context, runsPath, evalRunsPath string, keep int) error {
	workflowIter0 := fmt.Sprintf("%s/iteration-0", runsPath)
	evalIter0 := fmt.Sprintf("%s/iteration-0", evalRunsPath)

	workflowExists := hcpo.workspaceFileExists(ctx, workflowIter0)
	evalExists := hcpo.workspaceFileExists(ctx, evalIter0)

	if workflowExists || evalExists {
		backupName := hcpo.nextAvailableIterationAcross(ctx, runsPath, evalRunsPath)
		workflowBackup := fmt.Sprintf("%s/%s", runsPath, backupName)
		workflowMoved := false

		if workflowExists {
			hcpo.GetLogger().Info(fmt.Sprintf("Backing up %s/iteration-0 -> %s", runsPath, backupName))
			if err := hcpo.MoveWorkspaceFile(ctx, workflowIter0, workflowBackup); err != nil {
				return fmt.Errorf("refusing to rotate runs: failed to back up workflow iteration-0 to %s: %w", backupName, err)
			}
			workflowMoved = true
		}
		if evalExists {
			evalBackup := fmt.Sprintf("%s/%s", evalRunsPath, backupName)
			hcpo.GetLogger().Info(fmt.Sprintf("Backing up %s/iteration-0 -> %s (paired with workflow)", evalRunsPath, backupName))
			if err := hcpo.MoveWorkspaceFile(ctx, evalIter0, evalBackup); err != nil {
				if workflowMoved {
					if rollbackErr := hcpo.MoveWorkspaceFile(ctx, workflowBackup, workflowIter0); rollbackErr != nil {
						return fmt.Errorf("failed to back up eval iteration-0 to %s: %v; workflow backup rollback also failed: %w", backupName, err, rollbackErr)
					}
				}
				return fmt.Errorf("refusing to rotate runs: failed to back up eval iteration-0 to %s: %w", backupName, err)
			}
		}
	}

	hcpo.pruneOldIterations(ctx, runsPath, keep)
	hcpo.pruneOldIterations(ctx, evalRunsPath, keep)

	if err := hcpo.createRunFolderStructure(ctx, workflowIter0); err != nil {
		return fmt.Errorf("failed to create workflow iteration-0: %w", err)
	}
	return nil
}

// nextAvailableIterationAcross returns "iteration-{N+1}" where N is the highest
// iteration-N found across all supplied runs paths. Used to pick a backup name
// that is fresh in BOTH workflow runs/ and evaluation/runs/, keeping the paired
// rotation aligned.
func (hcpo *StepBasedWorkflowOrchestrator) nextAvailableIterationAcross(ctx context.Context, paths ...string) string {
	maxIter := 0
	re := regexp.MustCompile(`^iteration-(\d+)$`)
	for _, p := range paths {
		folders, err := hcpo.listRunFolders(ctx, p)
		if err != nil {
			continue
		}
		for _, folder := range folders {
			matches := re.FindStringSubmatch(folder)
			if len(matches) > 1 {
				var n int
				if _, err := fmt.Sscanf(matches[1], "%d", &n); err == nil && n > maxIter {
					maxIter = n
				}
			}
		}
	}
	return fmt.Sprintf("iteration-%d", maxIter+1)
}

// pruneOldIterations deletes backup iteration folders (iteration-1, iteration-2, ...)
// keeping only the most recent `keep` backups. iteration-0 is never deleted.
func (hcpo *StepBasedWorkflowOrchestrator) pruneOldIterations(ctx context.Context, runsPath string, keep int) {
	existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
	if err != nil {
		return
	}

	// Collect backup iteration numbers (skip iteration-0)
	type iterEntry struct {
		num  int
		name string
	}
	var backups []iterEntry
	re := regexp.MustCompile(`^iteration-(\d+)$`)
	for _, folder := range existingFolders {
		matches := re.FindStringSubmatch(folder)
		if len(matches) > 1 {
			var n int
			if _, scanErr := fmt.Sscanf(matches[1], "%d", &n); scanErr == nil && n > 0 {
				backups = append(backups, iterEntry{num: n, name: folder})
			}
		}
	}

	if len(backups) <= keep {
		return
	}

	// Sort descending by iteration number — keep the highest N
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].num > backups[j].num
	})

	for _, entry := range backups[keep:] {
		folderPath := fmt.Sprintf("%s/%s", runsPath, entry.name)
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Pruning old iteration backup: %s", entry.name))
		if delErr := hcpo.DeleteWorkspaceFile(ctx, folderPath); delErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete old iteration %s: %v", entry.name, delErr))
		}
	}
}

// nextAvailableIteration finds the next available iteration-N name (N >= 1) under runsPath.
//
//nolint:unused // kept for future non-destructive iteration allocation work.
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
	// getLearningFolderPathByStepID returns a RELATIVE path — workspace functions auto-prepend workspacePath
	stepLearningsPath := getLearningFolderPathByStepID("", stepID, stepPath, hcpo.isEvaluationMode)

	// Use readStepLearningFiles to check for learning files (it already excludes metadata)
	learningFiles, err := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
	if err != nil {
		// If folder doesn't exist or can't be read, assume empty so selection falls back conservatively.
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Step %s learnings folder does not exist or cannot be read: %s (treating as empty for execution model selection)", stepID, stepLearningsPath))
		return true, err
	}

	if len(learningFiles) == 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Step %s learnings folder has no learning files (only metadata or empty): %s", stepID, stepLearningsPath))
		return true, nil
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %s learnings folder has %d learning file(s): %s", stepID, len(learningFiles), stepLearningsPath))
	return false, nil
}

// listRunFolders lists existing run folder names
func (hcpo *StepBasedWorkflowOrchestrator) listRunFolders(ctx context.Context, runsPath string) ([]string, error) {
	// Use BaseOrchestrator's ListWorkspaceDirectories function
	return hcpo.BaseOrchestrator.ListWorkspaceDirectories(ctx, runsPath)
}

// createRunFolderStructure creates the basic structure for a run folder.
// Creates folders via Workspace API only (ensures consistency with workspace listings).
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
		if err := createFolderViaAPI(ctx, filepath.Join(KnowledgebaseFolderName, KnowledgebaseContextFolderName), workspacePath); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create knowledgebase context folder via API: %v (continuing)", err))
		}
		// Seed knowledgebase/notes/_index.json so the first agent read sees a valid registry.
		// Pre-existing files are left untouched. kbShape is retained for config compatibility
		// but runtime behavior is always notes-only (see workflowtypes.ResolveKBShape).
		kbShape := hcpo.KBShape()
		if err := InitKBGraphFiles(ctx, hcpo.BaseOrchestrator, workspacePath, kbShape); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to initialize KB files: %v (continuing)", err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Initialized KB files (shape=%s) in %s/%s/", workflowtypes.ResolveKBShape(kbShape), workspacePath, KnowledgebaseFolderName))
		}
	} else {
		hcpo.GetLogger().Info("⏭️ Skipping knowledgebase folder creation (disabled in preset)")
	}

	// Create db folder at workspace root (always enabled, no preset toggle).
	// Structured JSON data shared across all runs and groups. See DBFolderName in controller_execution.go.
	{
		workspacePath := hcpo.GetWorkspacePath()
		if err := createFolderViaAPI(ctx, DBFolderName, workspacePath); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create db folder via API: %v (continuing)", err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Created db folder: %s/%s", workspacePath, DBFolderName))
		}
		if err := createFolderViaAPI(ctx, filepath.Join(DBFolderName, DBAssetsFolderName), workspacePath); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create db assets folder via API: %v (continuing)", err))
		}
	}

	// Create soul folder at workspace root (builder's long-term memory, always enabled).
	// Only the workflow interactive builder writes here. See SoulFolderName in controller_execution.go.
	{
		workspacePath := hcpo.GetWorkspacePath()
		if err := createFolderViaAPI(ctx, SoulFolderName, workspacePath); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create soul folder via API: %v (continuing)", err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Created soul folder: %s/%s", workspacePath, SoulFolderName))
		}
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
