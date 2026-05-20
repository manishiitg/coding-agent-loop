package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	baseevents "github.com/manishiitg/mcpagent/events"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
)

// loadStepProgress is a no-op — file-backed progress persistence is disabled.
// Returns nil to signal no existing progress (iteration-0 is always fresh).
func (hcpo *StepBasedWorkflowOrchestrator) loadStepProgress(ctx context.Context) (*StepProgress, error) {
	return nil, fmt.Errorf("no progress file")
}

// saveStepProgress is a no-op — file-backed progress persistence is disabled.
func (hcpo *StepBasedWorkflowOrchestrator) saveStepProgress(ctx context.Context, progress *StepProgress) error {
	return nil
}

// emitStepStartedEvent emits a step started event via step_progress_updated
func (hcpo *StepBasedWorkflowOrchestrator) emitStepStartedEvent(ctx context.Context, step PlanStepInterface, stepIndex int, stepPath string, isBranchStep bool) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	stepTitle := step.GetTitle()
	if stepTitle == "" {
		stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
	}
	stepId := step.GetID()
	if stepId == "" {
		stepId = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// Emit progress event with "start" status
	progress, err := hcpo.loadStepProgress(ctx)
	if err == nil && progress != nil {
		hcpo.emitStepProgressUpdatedEvent(ctx, progress, "start", stepId, "")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_progress_updated (start) for step %d: %s", stepIndex+1, stepTitle))
}

// emitStepFinishedEvent emits a step finished event via step_progress_updated
func (hcpo *StepBasedWorkflowOrchestrator) emitStepFinishedEvent(ctx context.Context, step PlanStepInterface, stepIndex int, stepPath string, isBranchStep bool) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	stepTitle := step.GetTitle()
	if stepTitle == "" {
		stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
	}
	stepId := step.GetID()
	if stepId == "" {
		stepId = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// Emit progress event with "end" status
	progress, err := hcpo.loadStepProgress(ctx)
	if err == nil && progress != nil {
		hcpo.emitStepProgressUpdatedEvent(ctx, progress, "end", stepId, "")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_progress_updated (end) for step %d: %s", stepIndex+1, stepTitle))
}

// emitStepProgressUpdatedEvent emits an event when step progress is updated
// status can be "start" (step started), "stop" (step stopped), "end" (step ended), "failed" (step failed), or empty (regular progress update)
// errorMsg is populated when status is "failed"
func (hcpo *StepBasedWorkflowOrchestrator) emitStepProgressUpdatedEvent(ctx context.Context, progress *StepProgress, status string, stepId string, errorMsg string) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	// Determine the current step ID
	var currentStepId string
	if stepId != "" {
		// Use provided step ID (for start/stop/end events)
		currentStepId = stepId
	} else if len(progress.CompletedStepIndices) > 0 {
		// Determine the last completed step (highest index in the completed list)
		lastCompletedStep := -1
		for _, idx := range progress.CompletedStepIndices {
			if idx > lastCompletedStep {
				lastCompletedStep = idx
			}
		}
		// Get step ID from the approved plan if available
		if lastCompletedStep >= 0 && hcpo.approvedPlan != nil && lastCompletedStep < len(hcpo.approvedPlan.Steps) {
			step := hcpo.approvedPlan.Steps[lastCompletedStep]
			currentStepId = step.GetID()
		}
	}

	eventData := &StepProgressUpdatedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
		},
		WorkspacePath: hcpo.GetWorkspacePath(),
		RunFolder:     hcpo.selectedRunFolder,
		CurrentStepId: currentStepId,
		Status:        status,
		Error:         errorMsg,
		// Include batch context for frontend batch progress tracking
		GroupName:   hcpo.currentGroupName,
		GroupIndex:  hcpo.currentGroupIdx,
		TotalGroups: hcpo.totalGroups,
	}

	// Add tier info when in tiered mode (for "start" events with a step ID)
	if hcpo.tierResolver != nil && currentStepId != "" && status == "start" {
		_, tier := hcpo.tierResolver.ResolveForExecution()
		eventData.UsedTier = int(tier)
		eventData.UsedTierLabel = TierLevelLabel(tier)
	}

	// Create unified event wrapper
	unifiedEvent := &baseevents.AgentEvent{
		Type:      events.StepProgressUpdated,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit step progress updated event: %v", err))
	} else {
		if status != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("📊 Emitted step progress updated event: status=%s, current_step_id=%s", status, currentStepId))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("📊 Emitted step progress updated event: current_step_id=%s", currentStepId))
		}
	}
}

// emitPreValidationCompletedEvent emits a pre-validation completed event
func (hcpo *StepBasedWorkflowOrchestrator) emitPreValidationCompletedEvent(ctx context.Context, step PlanStepInterface, stepIndex int, stepPath string, isBranchStep bool, workspaceResults *WorkspaceVerificationResult) {
	if !shouldEmitPreValidationCompletedEvent(workspaceResults) {
		return
	}

	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	stepTitle := step.GetTitle()
	if stepTitle == "" {
		stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
	}
	stepId := step.GetID()
	if stepId == "" {
		stepId = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// Convert FileCheckResult to FileCheckResultForEvent
	filesChecked := make([]FileCheckResultForEvent, 0, len(workspaceResults.FilesChecked))
	for _, fileCheck := range workspaceResults.FilesChecked {
		jsonChecks := make([]JSONCheckResultForEvent, 0, len(fileCheck.JSONChecks))
		for _, jsonCheck := range fileCheck.JSONChecks {
			jsonChecks = append(jsonChecks, JSONCheckResultForEvent{
				Path:      jsonCheck.Path,
				Passed:    jsonCheck.Passed,
				CheckType: jsonCheck.CheckType,
				ErrorMsg:  jsonCheck.ErrorMsg,
			})
		}
		filesChecked = append(filesChecked, FileCheckResultForEvent{
			FileName:   fileCheck.FileName,
			Exists:     fileCheck.Exists,
			IsJSON:     fileCheck.IsJSON,
			JSONChecks: jsonChecks,
		})
	}

	// Convert ValidationError to ValidationErrorForEvent
	errors := make([]ValidationErrorForEvent, 0, len(workspaceResults.Summary.Errors))
	for _, err := range workspaceResults.Summary.Errors {
		errors = append(errors, ValidationErrorForEvent{
			File:      err.File,
			Path:      err.Path,
			CheckType: err.CheckType,
			Expected:  err.Expected,
			Actual:    err.Actual,
			Message:   err.Message,
		})
	}

	preValidationEvent := &PreValidationCompletedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepID:        stepId,
		StepIndex:     stepIndex,
		StepTitle:     stepTitle,
		StepPath:      stepPath,
		IsBranchStep:  isBranchStep,
		OverallPass:   workspaceResults.OverallPass,
		TotalChecks:   workspaceResults.Summary.TotalChecks,
		PassedChecks:  workspaceResults.Summary.PassedChecks,
		FailedChecks:  workspaceResults.Summary.FailedChecks,
		FilesChecked:  filesChecked,
		Errors:        errors,
		RunFolder:     hcpo.selectedRunFolder,
		WorkspacePath: hcpo.GetWorkspacePath(),
	}

	agentEvent := &baseevents.AgentEvent{
		Type:      events.PreValidationCompleted,
		Timestamp: time.Now(),
		Data:      preValidationEvent,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit pre-validation completed event: %v", err))
	} else {
		status := "✅ PASSED"
		if !workspaceResults.OverallPass {
			status = "❌ FAILED"
		}
		hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted pre_validation_completed event for step %d: %s (%s - %d/%d checks passed)", stepIndex+1, stepTitle, status, workspaceResults.Summary.PassedChecks, workspaceResults.Summary.TotalChecks))
	}
}

func shouldEmitPreValidationCompletedEvent(workspaceResults *WorkspaceVerificationResult) bool {
	if workspaceResults == nil {
		return false
	}
	if !workspaceResults.OverallPass {
		return true
	}
	if len(workspaceResults.FilesChecked) > 0 {
		return true
	}
	summary := workspaceResults.Summary
	return summary.TotalChecks > 0 ||
		summary.PassedChecks > 0 ||
		summary.FailedChecks > 0 ||
		summary.SchemaErrors > 0 ||
		len(summary.Errors) > 0 ||
		len(summary.SchemaWarnings) > 0
}

func collectNestedArtifactFolderNames(step PlanStepInterface, names map[string]struct{}) {
	if step == nil {
		return
	}
	if id := strings.TrimSpace(step.GetID()); id != "" {
		names[id] = struct{}{}
	}

	switch s := step.(type) {
	case *ConditionalPlanStep:
		for _, nested := range s.IfTrueSteps {
			collectNestedArtifactFolderNames(nested, names)
		}
		for _, nested := range s.IfFalseSteps {
			collectNestedArtifactFolderNames(nested, names)
		}
	case *TodoTaskPlanStep:
		for _, route := range s.PredefinedRoutes {
			collectNestedArtifactFolderNames(route.SubAgentStep, names)
		}
	}
}

func (hcpo *StepBasedWorkflowOrchestrator) getArtifactFolderNamesForStep(stepNumber int) []string {
	if hcpo.approvedPlan == nil || stepNumber < 1 || stepNumber > len(hcpo.approvedPlan.Steps) {
		return nil
	}
	names := make(map[string]struct{})
	collectNestedArtifactFolderNames(hcpo.approvedPlan.Steps[stepNumber-1], names)
	result := make([]string, 0, len(names))
	for name := range names {
		if name != "" {
			result = append(result, name)
		}
	}
	return result
}

// cleanupProgressFromStep removes completed step indices from targetStepIndex onward and cleans up branch steps
func (hcpo *StepBasedWorkflowOrchestrator) cleanupProgressFromStep(ctx context.Context, targetStepIndex int, progress *StepProgress) error {
	if progress == nil {
		return fmt.Errorf("progress is nil")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🧹 Cleaning up progress from step %d onward", targetStepIndex+1))

	// Remove completed indices from target step onward
	newCompletedIndices := make([]int, 0)
	for _, idx := range progress.CompletedStepIndices {
		if idx < targetStepIndex {
			newCompletedIndices = append(newCompletedIndices, idx)
		}
	}

	removedCount := len(progress.CompletedStepIndices) - len(newCompletedIndices)
	progress.CompletedStepIndices = newCompletedIndices

	// Clean up branch steps from target step onward
	if progress.BranchSteps != nil {
		branchStepsToRemove := make([]int, 0)
		for stepIdx := range progress.BranchSteps {
			if stepIdx >= targetStepIndex {
				branchStepsToRemove = append(branchStepsToRemove, stepIdx)
			}
		}
		for _, stepIdx := range branchStepsToRemove {
			delete(progress.BranchSteps, stepIdx)
		}
		if len(branchStepsToRemove) > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("🧹 Removed %d branch step progress entries from step %d onward", len(branchStepsToRemove), targetStepIndex+1))
		}
	}

	// Clean up validation failures from target step onward
	if progress.ValidationFailures != nil {
		pathsToRemove := make([]string, 0)
		for path := range progress.ValidationFailures {
			pathInfo := parseStepPath(path)
			// Reset if parent step is >= targetStepIndex+1 (1-based)
			if pathInfo.ParentStepNumber >= targetStepIndex+1 {
				pathsToRemove = append(pathsToRemove, path)
			}
		}
		for _, path := range pathsToRemove {
			delete(progress.ValidationFailures, path)
		}
		if len(pathsToRemove) > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("🧹 Removed %d validation failure entries from step %d onward", len(pathsToRemove), targetStepIndex+1))
		}
	}

	// Save updated progress
	if err := hcpo.saveStepProgress(ctx, progress); err != nil {
		return fmt.Errorf("failed to save progress after cleanup: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Progress cleanup completed: removed %d completed step indices from step %d onward. Remaining completed steps: %d", removedCount, targetStepIndex+1, len(progress.CompletedStepIndices)))

	return nil
}

// deleteStepProgress is a no-op — file-backed progress persistence is disabled.
func (hcpo *StepBasedWorkflowOrchestrator) deleteStepProgress(ctx context.Context) error {
	return nil
}

// initializeFreshProgress is a no-op — file-backed progress persistence is disabled.
func (hcpo *StepBasedWorkflowOrchestrator) initializeFreshProgress(ctx context.Context, newTotalSteps int) error {
	return nil
}

// archiveLogsFolder archives log files within a folder by moving them to an "archived/{timestamp}" subfolder
// This preserves logs for debugging while allowing clean re-execution
func (hcpo *StepBasedWorkflowOrchestrator) archiveLogsFolder(ctx context.Context, logsFolderPath string, folderName string) error {
	// Check if the logs folder exists
	exists, err := hcpo.CheckWorkspaceFileExists(ctx, logsFolderPath)
	if err != nil {
		return fmt.Errorf("failed to check if logs folder exists: %w", err)
	}

	if !exists {
		// Folder doesn't exist, nothing to archive
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Logs folder does not exist, skipping archive: %s", folderName))
		return nil
	}

	// List files in the logs folder
	files, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, logsFolderPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to list files in logs folder %s: %v", folderName, err))
		return nil // Don't fail - folder may be empty
	}

	// Filter for log files to archive (validation*.json, execution-attempt*.json, learning*.json, etc.)
	logFilePatterns := []string{"validation", "execution-attempt", "learning", "orchestration", "conditional", "decision"}
	filesToArchive := []string{}
	for _, file := range files {
		for _, pattern := range logFilePatterns {
			if strings.HasPrefix(file, pattern) && strings.HasSuffix(file, ".json") {
				filesToArchive = append(filesToArchive, file)
				break
			}
		}
	}

	if len(filesToArchive) == 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No log files to archive in %s", folderName))
		return nil
	}

	// Generate archive subfolder with timestamp
	timestamp := time.Now().Format("20060102-150405")
	archivedSubfolder := fmt.Sprintf("%s/archived/%s", logsFolderPath, timestamp)

	hcpo.GetLogger().Info(fmt.Sprintf("📦 Archiving %d log files from %s to archived/%s", len(filesToArchive), folderName, timestamp))

	// Move each file to the archived subfolder
	archivedCount := 0
	for _, file := range filesToArchive {
		sourcePath := fmt.Sprintf("%s/%s", logsFolderPath, file)
		destPath := fmt.Sprintf("%s/%s", archivedSubfolder, file)

		if err := hcpo.MoveWorkspaceFile(ctx, sourcePath, destPath); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive %s: %v", file, err))
			continue
		}
		archivedCount++
	}

	if archivedCount > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Archived %d/%d log files from %s", archivedCount, len(filesToArchive), folderName))
	}

	return nil
}

// deleteStepExecutionFolder deletes the execution folder for a specific step
// stepNumber is 1-based (e.g., step 1, step 2, etc.)
// This is used when resuming from a step or running a single step to ensure clean re-execution
// by removing any existing execution artifacts from previous runs
// Also deletes all branch step folders for this step (e.g., step-3-if-true-0, step-3-if-false-1, etc.)
// Also deletes decision step folder if it exists (e.g., step-8-decision)
// Also deletes all sub-agent step folders for this step
// (e.g., step-2-sub-agent-1, step-2-sub-agent-2, step-2-sub-login, etc.)
func (hcpo *StepBasedWorkflowOrchestrator) deleteStepExecutionFolder(ctx context.Context, stepNumber int) error {
	// Validate that run folder is set (required for building correct path)
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf(fmt.Sprintf("selectedRunFolder not set - run folder must be resolved before deleting execution folders"), nil)
	}

	// Build execution folder path: workspacePath/runs/{runFolder}/execution/step-{stepNumber}
	// Example: /workspace/runs/iteration-1/execution/step-3
	baseWorkspacePath := hcpo.GetWorkspacePath()
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	stepFolderPath := fmt.Sprintf("%s/step-%d", executionWorkspacePath, stepNumber)
	artifactFolderNames := hcpo.getArtifactFolderNamesForStep(stepNumber)

	hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleting execution folder for step %d: %s", stepNumber, stepFolderPath))

	// Use CleanupDirectory to delete the step folder recursively
	// This removes all files and subdirectories within the step's execution folder
	// CleanupDirectory handles the recursive deletion and depth-first directory removal
	if err := hcpo.CleanupDirectory(ctx, stepFolderPath, fmt.Sprintf("execution/step-%d", stepNumber)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete execution folder for step %d: %v", stepNumber, err))
		// Continue to try deleting branch step folders even if main folder deletion failed
	} else {
		// CleanupDirectory only deletes contents, not the root folder itself
		// Explicitly delete the root step folder after contents are cleaned
		if err := hcpo.DeleteWorkspaceFile(ctx, stepFolderPath); err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Step folder %s already deleted or doesn't exist", stepFolderPath))
			} else if strings.Contains(errStr, "directory not empty") {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step folder %s not empty after cleanup - may have remaining files", stepFolderPath))
			} else {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete step folder %s: %v", stepFolderPath, err))
			}
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Successfully deleted step folder: %s", stepFolderPath))
		}
	}

	// Also archive logs folder for this step: logs/step-{stepNumber}
	logsWorkspacePath := fmt.Sprintf("%s/logs", runWorkspacePath)
	stepLogsFolderPath := fmt.Sprintf("%s/step-%d", logsWorkspacePath, stepNumber)
	if err := hcpo.archiveLogsFolder(ctx, stepLogsFolderPath, fmt.Sprintf("step-%d", stepNumber)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive logs folder for step %d: %v", stepNumber, err))
		// Continue even if logs archiving failed
	}

	for _, folderName := range artifactFolderNames {
		if folderName == fmt.Sprintf("step-%d", stepNumber) {
			continue
		}
		folderPath := fmt.Sprintf("%s/%s", executionWorkspacePath, folderName)
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleting artifact folder for step %d: %s", stepNumber, folderPath))
		if err := hcpo.CleanupDirectory(ctx, folderPath, fmt.Sprintf("execution/%s", folderName)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete artifact folder %s: %v", folderName, err))
		} else if err := hcpo.DeleteWorkspaceFile(ctx, folderPath); err != nil {
			errStr := err.Error()
			if !strings.Contains(errStr, "not found") && !strings.Contains(errStr, "no such file") {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete artifact folder %s: %v", folderName, err))
			}
		}

		artifactLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, folderName)
		if err := hcpo.archiveLogsFolder(ctx, artifactLogsFolderPath, folderName); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive artifact logs folder %s: %v", folderName, err))
		}
	}

	// Also delete all branch step folders for this step (e.g., step-3-if-true-0, step-3-if-false-1, etc.)
	// This ensures that when resuming from a step before a conditional step, all branch executions are cleaned up
	branchStepPrefix := fmt.Sprintf("step-%d-if-", stepNumber)
	branchFoldersDeleted := 0
	branchFoldersFound := []string{}

	// Also delete decision step folder if it exists (e.g., step-8-decision)
	decisionStepFolder := fmt.Sprintf("step-%d-decision", stepNumber)
	decisionFoldersDeleted := 0

	// Also delete all sub-agent step folders for this step
	// (e.g., step-2-sub-agent-1, step-2-sub-agent-2, step-2-sub-login, etc.)
	// This ensures that when resuming from a step before an orchestration step, all sub-agent executions are cleaned up
	subAgentFoldersDeleted := 0
	subAgentFoldersFound := []string{}

	// Also delete all generic-agent step folders for this step.
	// These are created by todo_task steps for ad-hoc tasks via call_generic_agent
	genericAgentFoldersDeleted := 0
	genericAgentFoldersFound := []string{}

	// List all files/folders in the execution directory
	files, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, executionWorkspacePath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to list execution directory to find branch step folders: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Found %d items in execution directory", len(files)))

		// Find and delete all branch step folders, decision step folders, and sub-agent step folders that match the pattern
		for _, file := range files {
			// Check if this is a branch step folder for the current step
			// Pattern: step-{N}-if-true-{idx} or step-{N}-if-false-{idx}
			if strings.HasPrefix(file, branchStepPrefix) {
				branchFoldersFound = append(branchFoldersFound, file)
				branchFolderPath := fmt.Sprintf("%s/%s", executionWorkspacePath, file)
				hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleting branch step folder: %s", file))
				if err := hcpo.CleanupDirectory(ctx, branchFolderPath, fmt.Sprintf("execution/%s", file)); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete branch step folder %s: %v", file, err))
				} else {
					// CleanupDirectory only deletes contents, not the root folder itself
					// Explicitly delete the root branch folder after contents are cleaned
					if err := hcpo.DeleteWorkspaceFile(ctx, branchFolderPath); err != nil {
						errStr := err.Error()
						if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
							hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Branch step folder %s already deleted or doesn't exist", branchFolderPath))
						} else if strings.Contains(errStr, "directory not empty") {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Branch step folder %s not empty after cleanup - may have remaining files", branchFolderPath))
						} else {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete branch step folder %s: %v", branchFolderPath, err))
						}
					} else {
						branchFoldersDeleted++
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Successfully deleted branch step folder: %s", file))
					}
				}
				// Also archive corresponding branch step logs folder
				branchLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, file)
				if err := hcpo.archiveLogsFolder(ctx, branchLogsFolderPath, file); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive branch step logs folder %s: %v", file, err))
				}
			} else if file == decisionStepFolder {
				// Delete decision step folder
				decisionFolderPath := fmt.Sprintf("%s/%s", executionWorkspacePath, file)
				hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleting decision step folder: %s", file))
				if err := hcpo.CleanupDirectory(ctx, decisionFolderPath, fmt.Sprintf("execution/%s", file)); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete decision step folder %s: %v", file, err))
				} else {
					// CleanupDirectory only deletes contents, not the root folder itself
					// Explicitly delete the root decision folder after contents are cleaned
					if err := hcpo.DeleteWorkspaceFile(ctx, decisionFolderPath); err != nil {
						errStr := err.Error()
						if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
							hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Decision step folder %s already deleted or doesn't exist", decisionFolderPath))
						} else if strings.Contains(errStr, "directory not empty") {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Decision step folder %s not empty after cleanup - may have remaining files", decisionFolderPath))
						} else {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete decision step folder %s: %v", decisionFolderPath, err))
						}
					} else {
						decisionFoldersDeleted++
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Successfully deleted decision step folder: %s", file))
					}
				}
				// Also archive corresponding decision step logs folder
				decisionLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, file)
				if err := hcpo.archiveLogsFolder(ctx, decisionLogsFolderPath, file); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive decision step logs folder %s: %v", file, err))
				}
			} else if isSubAgentArtifactFolderForStep(file, stepNumber) {
				// Check if this is a sub-agent step folder for the current step
				// Pattern: step-{N}-sub-agent-{index} or step-{N}-sub-{routeId}
				subAgentFoldersFound = append(subAgentFoldersFound, file)
				subAgentFolderPath := fmt.Sprintf("%s/%s", executionWorkspacePath, file)
				hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleting sub-agent step folder: %s", file))
				if err := hcpo.CleanupDirectory(ctx, subAgentFolderPath, fmt.Sprintf("execution/%s", file)); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete sub-agent step folder %s: %v", file, err))
				} else {
					// CleanupDirectory only deletes contents, not the root folder itself
					// Explicitly delete the root sub-agent folder after contents are cleaned
					if err := hcpo.DeleteWorkspaceFile(ctx, subAgentFolderPath); err != nil {
						errStr := err.Error()
						if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
							hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Sub-agent step folder %s already deleted or doesn't exist", subAgentFolderPath))
						} else if strings.Contains(errStr, "directory not empty") {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Sub-agent step folder %s not empty after cleanup - may have remaining files", subAgentFolderPath))
						} else {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete sub-agent step folder %s: %v", subAgentFolderPath, err))
						}
					} else {
						subAgentFoldersDeleted++
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Successfully deleted sub-agent step folder: %s", file))
					}
				}
				// Also archive corresponding sub-agent step logs folder
				subAgentLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, file)
				if err := hcpo.archiveLogsFolder(ctx, subAgentLogsFolderPath, file); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive sub-agent step logs folder %s: %v", file, err))
				}
			} else if isGenericAgentArtifactFolderForStep(file, stepNumber) {
				// Check if this is a generic-agent step folder for the current step
				// Patterns include step-{N}-generic-* and the legacy stable-id generic-step-{N}-* form.
				genericAgentFoldersFound = append(genericAgentFoldersFound, file)
				genericAgentFolderPath := fmt.Sprintf("%s/%s", executionWorkspacePath, file)
				hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleting generic-agent step folder: %s", file))
				if err := hcpo.CleanupDirectory(ctx, genericAgentFolderPath, fmt.Sprintf("execution/%s", file)); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete generic-agent step folder %s: %v", file, err))
				} else {
					if err := hcpo.DeleteWorkspaceFile(ctx, genericAgentFolderPath); err != nil {
						errStr := err.Error()
						if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
							hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Generic-agent step folder %s already deleted or doesn't exist", genericAgentFolderPath))
						} else if strings.Contains(errStr, "directory not empty") {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Generic-agent step folder %s not empty after cleanup - may have remaining files", genericAgentFolderPath))
						} else {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete generic-agent step folder %s: %v", genericAgentFolderPath, err))
						}
					} else {
						genericAgentFoldersDeleted++
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Successfully deleted generic-agent step folder: %s", file))
					}
				}
				// Also archive corresponding generic-agent step logs folder
				genericAgentLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, file)
				if err := hcpo.archiveLogsFolder(ctx, genericAgentLogsFolderPath, file); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive generic-agent step logs folder %s: %v", file, err))
				}
			}
		}

		if len(branchFoldersFound) == 0 && decisionFoldersDeleted == 0 && len(subAgentFoldersFound) == 0 && len(genericAgentFoldersFound) == 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No branch step, decision step, sub-agent step, or generic-agent step folders found for step %d", stepNumber))
		}
	}

	if branchFoldersDeleted > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Deleted %d/%d branch step folder(s) for step %d: %v", branchFoldersDeleted, len(branchFoldersFound), stepNumber, branchFoldersFound))
	}
	if decisionFoldersDeleted > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Deleted decision step folder for step %d: %s", stepNumber, decisionStepFolder))
	}
	if subAgentFoldersDeleted > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Deleted %d/%d sub-agent step folder(s) for step %d: %v", subAgentFoldersDeleted, len(subAgentFoldersFound), stepNumber, subAgentFoldersFound))
	}
	if genericAgentFoldersDeleted > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Deleted %d/%d generic-agent step folder(s) for step %d: %v", genericAgentFoldersDeleted, len(genericAgentFoldersFound), stepNumber, genericAgentFoldersFound))
	}

	if err := hcpo.cleanupMessageSequenceStepPathsForStep(ctx, stepNumber); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup message_sequence execution folders for step %d: %v", stepNumber, err))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Deleted execution folder for step %d", stepNumber))
	return nil
}

// getNextArchivalRunNumber returns the next archival run number for a step and increments the count
// stepNumber is 1-based (e.g., step 1, step 2, etc.)
// Returns the run number to use for archiving (1 for first archive, 2 for second, etc.)
func (hcpo *StepBasedWorkflowOrchestrator) getNextArchivalRunNumber(ctx context.Context, progress *StepProgress, stepNumber int) int {
	if progress.ArchivalCounts == nil {
		progress.ArchivalCounts = make(map[int]int)
	}
	// Get current count and increment
	currentCount := progress.ArchivalCounts[stepNumber]
	nextRunNumber := currentCount + 1
	progress.ArchivalCounts[stepNumber] = nextRunNumber
	return nextRunNumber
}

// archiveStepExecutionFolder archives the execution folder for a specific step instead of deleting it
// stepNumber is 1-based (e.g., step 1, step 2, etc.)
// runNumber is the archive run number (1 for first archive, 2 for second, etc.)
// Moves execution/step-{N}/ to execution/archived/run-{runNumber}/step-{N}/
// Also archives branch folders (step-{N}-if-*), decision folders (step-{N}-decision),
// and sub-agent folders (step-{N}-sub-agent-* / step-{N}-sub-*)
func (hcpo *StepBasedWorkflowOrchestrator) archiveStepExecutionFolder(ctx context.Context, stepNumber int, runNumber int) error {
	// Validate that run folder is set (required for building correct path)
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf("selectedRunFolder not set - run folder must be resolved before archiving execution folders")
	}

	// Build execution folder path: workspacePath/runs/{runFolder}/execution/step-{stepNumber}
	baseWorkspacePath := hcpo.GetWorkspacePath()
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	archiveBasePath := fmt.Sprintf("%s/archived/run-%d", executionWorkspacePath, runNumber)
	artifactFolderNames := hcpo.getArtifactFolderNamesForStep(stepNumber)

	hcpo.GetLogger().Info(fmt.Sprintf("📦 Archiving execution folder for step %d to run-%d", stepNumber, runNumber))

	// Helper function to archive a single folder
	archiveFolder := func(folderName string) error {
		sourcePath := fmt.Sprintf("%s/%s", executionWorkspacePath, folderName)
		destPath := fmt.Sprintf("%s/%s", archiveBasePath, folderName)

		// Check if source folder exists
		exists, err := hcpo.CheckWorkspaceFileExists(ctx, sourcePath)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check if folder exists: %s: %v", folderName, err))
			return nil // Continue with other folders
		}

		if !exists {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Folder does not exist, skipping archive: %s", folderName))
			return nil
		}

		// Move the folder to archive location
		if err := hcpo.MoveWorkspaceFile(ctx, sourcePath, destPath); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive folder %s: %v", folderName, err))
			return nil // Continue with other folders
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Archived folder: %s -> archived/run-%d/%s", folderName, runNumber, folderName))
		return nil
	}

	// Archive main step folder
	archiveFolder(fmt.Sprintf("step-%d", stepNumber))

	for _, folderName := range artifactFolderNames {
		if folderName == fmt.Sprintf("step-%d", stepNumber) {
			continue
		}
		archiveFolder(folderName)
	}

	// Also archive logs folder for this step
	logsWorkspacePath := fmt.Sprintf("%s/logs", runWorkspacePath)
	stepLogsFolderPath := fmt.Sprintf("%s/step-%d", logsWorkspacePath, stepNumber)
	if err := hcpo.archiveLogsFolder(ctx, stepLogsFolderPath, fmt.Sprintf("step-%d", stepNumber)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive logs folder for step %d: %v", stepNumber, err))
	}

	for _, folderName := range artifactFolderNames {
		if folderName == fmt.Sprintf("step-%d", stepNumber) {
			continue
		}
		artifactLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, folderName)
		if err := hcpo.archiveLogsFolder(ctx, artifactLogsFolderPath, folderName); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive artifact logs folder %s: %v", folderName, err))
		}
	}

	// Archive branch step folders, decision step folders, and sub-agent step folders
	branchStepPrefix := fmt.Sprintf("step-%d-if-", stepNumber)
	decisionStepFolder := fmt.Sprintf("step-%d-decision", stepNumber)

	// List all files/folders in the execution directory
	files, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, executionWorkspacePath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to list execution directory to find additional folders: %v", err))
	} else {
		for _, file := range files {
			if strings.HasPrefix(file, branchStepPrefix) {
				// Archive branch step folder
				archiveFolder(file)
				// Also archive corresponding branch step logs folder
				branchLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, file)
				if err := hcpo.archiveLogsFolder(ctx, branchLogsFolderPath, file); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive branch step logs folder %s: %v", file, err))
				}
			} else if file == decisionStepFolder {
				// Archive decision step folder
				archiveFolder(file)
				// Also archive corresponding decision step logs folder
				decisionLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, file)
				if err := hcpo.archiveLogsFolder(ctx, decisionLogsFolderPath, file); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive decision step logs folder %s: %v", file, err))
				}
			} else if isSubAgentArtifactFolderForStep(file, stepNumber) {
				// Archive sub-agent step folder
				archiveFolder(file)
				// Also archive corresponding sub-agent step logs folder
				subAgentLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, file)
				if err := hcpo.archiveLogsFolder(ctx, subAgentLogsFolderPath, file); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive sub-agent step logs folder %s: %v", file, err))
				}
			} else if isGenericAgentArtifactFolderForStep(file, stepNumber) {
				// Archive generic-agent step folder (created by todo_task steps via call_generic_agent)
				archiveFolder(file)
				// Also archive corresponding generic-agent step logs folder
				genericAgentLogsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, file)
				if err := hcpo.archiveLogsFolder(ctx, genericAgentLogsFolderPath, file); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive generic-agent step logs folder %s: %v", file, err))
				}
			}
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Archived execution folder for step %d to run-%d", stepNumber, runNumber))
	return nil
}

// IncrementValidationFailureCount increments the validation failure counter for a specific step path
func (hcpo *StepBasedWorkflowOrchestrator) IncrementValidationFailureCount(ctx context.Context, stepPath string) error {
	progress, err := hcpo.loadStepProgress(ctx)
	if err != nil {
		return err
	}

	if progress.ValidationFailures == nil {
		progress.ValidationFailures = make(map[string]int)
	}

	progress.ValidationFailures[stepPath]++
	hcpo.GetLogger().Info(fmt.Sprintf("📊 Incrementing validation failure count for %s: %d", stepPath, progress.ValidationFailures[stepPath]))

	return hcpo.saveStepProgress(ctx, progress)
}

// ResetValidationFailureCount resets the validation failure counter for a specific step path
func (hcpo *StepBasedWorkflowOrchestrator) ResetValidationFailureCount(ctx context.Context, stepPath string) error {
	progress, err := hcpo.loadStepProgress(ctx)
	if err != nil {
		return err
	}

	if progress.ValidationFailures != nil {
		delete(progress.ValidationFailures, stepPath)
		hcpo.GetLogger().Info(fmt.Sprintf("📊 Reset validation failure count for %s", stepPath))
		return hcpo.saveStepProgress(ctx, progress)
	}

	return nil
}

// emitLearnCodeScriptExecutionEvent emits a learn_code_script_execution event to the UI.
func (hcpo *StepBasedWorkflowOrchestrator) emitLearnCodeScriptExecutionEvent(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	scriptPath string,
	success bool,
	exitCode int,
	output string,
	errMsg string,
	fixIteration int,
	isSavedScript bool,
) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}
	stepTitle := step.GetTitle()
	if stepTitle == "" {
		stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
	}
	stepID := step.GetID()
	if stepID == "" {
		stepID = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// Read script content for UI display. Use workspace-relative path derived from scriptPath.
	docsRoot := GetPromptDocsRoot()
	scriptContent := ""
	if scriptPath != "" {
		scriptRelPath := strings.TrimPrefix(scriptPath, docsRoot+"/")
		scriptRelPath = strings.TrimPrefix(scriptRelPath, hcpo.GetWorkspacePath()+"/")
		if content, err := hcpo.ReadWorkspaceFile(ctx, scriptRelPath); err == nil {
			scriptContent = content
			hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Read script content for UI: %s (%d bytes)", scriptRelPath, len(content)))
		} else {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to read script content for UI (%s): %v", scriptRelPath, err))
		}
	}

	ev := &events.LearnCodeScriptExecutionEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "learn_code",
		},
		StepID:        stepID,
		StepIndex:     stepIndex,
		StepTitle:     stepTitle,
		StepPath:      stepPath,
		WorkspacePath: hcpo.GetWorkspacePath(),
		RunFolder:     hcpo.selectedRunFolder,
		ScriptPath:    scriptPath,
		ScriptContent: scriptContent,
		Success:       success,
		ExitCode:      exitCode,
		Output:        output,
		Error:         errMsg,
		FixIteration:  fixIteration,
		IsSavedScript: isSavedScript,
	}
	hcpo.GetLogger().Info(fmt.Sprintf(
		"🧭 [FIX_LEARN_CODE_UI] emitting learn_code_script_execution step=%s title=%q saved=%t fix_iter=%d success=%t exit=%d output_len=%d error_len=%d script_len=%d run_folder=%s",
		stepID, stepTitle, isSavedScript, fixIteration, success, exitCode, len(output), len(errMsg), len(scriptContent), hcpo.selectedRunFolder,
	))
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      events.LearnCodeScriptExecution,
		Timestamp: time.Now(),
		Data:      ev,
	})

	// Also emit a synthetic terminal snapshot so this step shows up
	// in Terminal view (no LLM call means no WithObservability →
	// otherwise the step would be invisible there).
	hcpo.emitLearnCodeTerminalSnapshot(ctx, stepID, stepTitle, scriptPath, scriptContent, output, errMsg, exitCode, success, isSavedScript, fixIteration)
}

// emitLearnCodeTerminalSnapshot builds a synthetic terminal pane
// snapshot for a learn-code step run and emits it as a
// StreamingChunkEvent with kind=terminal. The orchestrator's terminal
// store picks it up exactly the same way an adapter-emitted terminal
// chunk would, so the step appears in the Terminal view alongside
// LLM-driven steps.
func (hcpo *StepBasedWorkflowOrchestrator) emitLearnCodeTerminalSnapshot(
	ctx context.Context,
	stepID, stepTitle, scriptPath, scriptContent, output, errMsg string,
	exitCode int,
	success, isSavedScript bool,
	fixIteration int,
) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}
	var b strings.Builder
	source := "saved"
	if !isSavedScript {
		source = "fresh"
	}
	fmt.Fprintf(&b, "$ python %s (source=%s fix_iter=%d)\n", scriptPath, source, fixIteration)
	if stepID != "" || stepTitle != "" {
		fmt.Fprintf(&b, "↳ step %s (learn_code)\n", firstNonEmptyStr(stepID, stepTitle))
	}
	if scriptContent != "" {
		b.WriteString("--- script ---\n")
		b.WriteString(truncateForTerminal(scriptContent, 4000))
		b.WriteString("\n--- output ---\n")
	}
	if strings.TrimSpace(output) != "" {
		b.WriteString(truncateForTerminal(output, 8000))
		if !strings.HasSuffix(output, "\n") {
			b.WriteByte('\n')
		}
	}
	if strings.TrimSpace(errMsg) != "" {
		fmt.Fprintf(&b, "[stderr] %s\n", truncateForTerminal(errMsg, 4000))
	}
	status := "✓"
	if !success {
		status = "✗"
	}
	fmt.Fprintf(&b, "[done · %s · exit=%d]\n", status, exitCode)

	chunkEv := &baseevents.StreamingChunkEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"kind":      "terminal",
				"source":    "learn_code",
				"step_id":   stepID,
				"replace":   true,
				"transport": "learn_code",
			},
		},
		Content:    b.String(),
		ChunkIndex: 0,
		IsToolCall: false,
	}
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      "streaming_chunk",
		Timestamp: time.Now(),
		Data:      chunkEv,
	})

	// Follow up with a streaming_end so the terminals store flips the
	// pane to "completed" and the frontend's close (X) button appears.
	// Without this the pane stays in the "running" state forever — the
	// step has no LLM call to drive a natural end-of-stream signal.
	endEv := &baseevents.StreamingEndEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"kind":      "terminal",
				"source":    "learn_code",
				"step_id":   stepID,
				"transport": "learn_code",
			},
		},
	}
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      "streaming_end",
		Timestamp: time.Now(),
		Data:      endEv,
	})
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateForTerminal(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…[truncated]"
}
