package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	baseevents "mcpagent/events"
)

// getStepsProgressPath returns the path to steps_done.json file in the run folder
func (hcpo *StepBasedWorkflowOrchestrator) getStepsProgressPath() (string, error) {
	if hcpo.selectedRunFolder == "" {
		return "", fmt.Errorf(fmt.Sprintf("selectedRunFolder not set - run folder must be resolved before accessing steps_done.json"), nil)
	}
	return fmt.Sprintf("%s/runs/%s/steps_done.json", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder), nil
}

// loadStepProgress loads progress from steps_done.json
func (hcpo *StepBasedWorkflowOrchestrator) loadStepProgress(ctx context.Context) (*StepProgress, error) {
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return nil, err
	}

	content, err := hcpo.ReadWorkspaceFile(ctx, progressPath)
	if err != nil {
		// File doesn't exist or error reading
		return nil, fmt.Errorf("failed to load step progress: %w", err)
	}

	var progress StepProgress
	if err := json.Unmarshal([]byte(content), &progress); err != nil {
		return nil, fmt.Errorf("failed to parse steps_done.json: %w", err)
	}

	// Backward compatibility: initialize BranchSteps if nil (old files won't have this field)
	if progress.BranchSteps == nil {
		progress.BranchSteps = make(map[int]BranchStepProgress)
	}

	// Backward compatibility: initialize ValidationFailures if nil
	if progress.ValidationFailures == nil {
		progress.ValidationFailures = make(map[string]int)
	}

	// Always initialize DecisionEvaluationCounts fresh (in-memory only, not persisted)
	// This ensures each new run starts with clean counts, preventing false infinite loop detection
	if progress.DecisionEvaluationCounts == nil {
		progress.DecisionEvaluationCounts = make(DecisionEvaluationCount)
	}

	return &progress, nil
}

// saveStepProgress saves progress to steps_done.json
func (hcpo *StepBasedWorkflowOrchestrator) saveStepProgress(ctx context.Context, progress *StepProgress) error {
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return err
	}

	progress.LastUpdated = time.Now()

	progressJSON, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal progress: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, progressPath, string(progressJSON)); err != nil {
		return fmt.Errorf("failed to write steps_done.json: %w", err)
	}

	// Emit step progress updated event for frontend dynamic updates
	hcpo.emitStepProgressUpdatedEvent(ctx, progress)

	return nil
}

// emitStepStartedEvent emits a step started event
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

	startedEvent := &StepStartedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepID:        stepId,
		StepIndex:     stepIndex,
		StepTitle:     stepTitle,
		StepPath:      stepPath,
		IsBranchStep:  isBranchStep,
		RunFolder:     hcpo.selectedRunFolder,
		WorkspacePath: hcpo.GetWorkspacePath(),
	}

	agentEvent := &baseevents.AgentEvent{
		Type:      baseevents.StepExecutionStart,
		Timestamp: time.Now(),
		Data:      startedEvent,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit step started event: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_started event for step %d: %s", stepIndex+1, stepTitle))
	}
}

// emitStepFinishedEvent emits a step finished event
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

	finishedEvent := &StepFinishedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepID:       stepId,
		StepIndex:    stepIndex,
		StepTitle:    stepTitle,
		StepPath:     stepPath,
		IsBranchStep: isBranchStep,
	}

	agentEvent := &baseevents.AgentEvent{
		Type:      baseevents.StepExecutionEnd,
		Timestamp: time.Now(),
		Data:      finishedEvent,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit step finished event: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_finished event for step %d: %s", stepIndex+1, stepTitle))
	}
}

// emitDecisionEvaluatedEvent emits a decision evaluated event with structured response
// decisionResponse is from the workflow package (conditional_agent.go) - same package, so we can use it directly
func (hcpo *StepBasedWorkflowOrchestrator) emitDecisionEvaluatedEvent(ctx context.Context, step PlanStepInterface, stepIndex int, stepPath string, decisionResponse *DecisionResponse) {
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

	// Convert workflow DecisionResponse to event DecisionResponseEvent
	// Since DecisionResponse is in the same package, we can access fields directly
	eventDecisionResponse := DecisionResponseEvent{
		Result:    decisionResponse.Result,
		Reasoning: decisionResponse.Reasoning,
	}

	evaluatedEvent := &DecisionEvaluatedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepID:    stepId,
		StepIndex: stepIndex,
		StepTitle: stepTitle,
		StepPath:  stepPath,
		DecisionQuestion: func() string {
			if decisionStep, ok := step.(*DecisionPlanStep); ok {
				return decisionStep.DecisionEvaluationQuestion
			}
			return ""
		}(),
		DecisionResponse: eventDecisionResponse,
		RunFolder:        hcpo.selectedRunFolder,
		WorkspacePath:    hcpo.GetWorkspacePath(),
	}

	agentEvent := &baseevents.AgentEvent{
		Type:      events.DecisionEvaluated,
		Timestamp: time.Now(),
		Data:      evaluatedEvent,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit decision evaluated event: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted decision_evaluated event for step %d: %s (result=%t)", stepIndex+1, stepTitle, decisionResponse.Result))
	}
}

// emitStepProgressUpdatedEvent emits an event when step progress is updated
func (hcpo *StepBasedWorkflowOrchestrator) emitStepProgressUpdatedEvent(ctx context.Context, progress *StepProgress) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	// Determine the last completed step (highest index in the completed list)
	lastCompletedStep := -1
	if len(progress.CompletedStepIndices) > 0 {
		for _, idx := range progress.CompletedStepIndices {
			if idx > lastCompletedStep {
				lastCompletedStep = idx
			}
		}
	}

	// Get step ID and title from the approved plan if available
	var lastCompletedStepId string
	var lastCompletedStepTitle string
	if lastCompletedStep >= 0 && hcpo.approvedPlan != nil && lastCompletedStep < len(hcpo.approvedPlan.Steps) {
		step := hcpo.approvedPlan.Steps[lastCompletedStep]
		lastCompletedStepId = step.GetID()
		lastCompletedStepTitle = step.GetTitle()
	}

	// Use local BranchStepProgress (already in same package)
	branchSteps := progress.BranchSteps

	eventData := &StepProgressUpdatedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
		},
		CompletedStepIndices:   progress.CompletedStepIndices,
		TotalSteps:             progress.TotalSteps,
		WorkspacePath:          hcpo.GetWorkspacePath(),
		RunFolder:              hcpo.selectedRunFolder,
		LastCompletedStep:      lastCompletedStep,
		LastCompletedStepId:    lastCompletedStepId,
		LastCompletedStepTitle: lastCompletedStepTitle,
		BranchSteps:            branchSteps,
		ValidationFailures:     progress.ValidationFailures,
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
		hcpo.GetLogger().Info(fmt.Sprintf("📊 Emitted step progress updated event: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps))
	}
}

// emitPreValidationCompletedEvent emits a pre-validation completed event
func (hcpo *StepBasedWorkflowOrchestrator) emitPreValidationCompletedEvent(ctx context.Context, step PlanStepInterface, stepIndex int, stepPath string, isBranchStep bool, workspaceResults *WorkspaceVerificationResult) {
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

// deleteStepProgress deletes steps_done.json file
func (hcpo *StepBasedWorkflowOrchestrator) deleteStepProgress(ctx context.Context) error {
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return err
	}

	if err := hcpo.DeleteWorkspaceFile(ctx, progressPath); err != nil {
		// Ignore error if file doesn't exist
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return fmt.Errorf("failed to delete steps_done.json: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleted step progress file: %s", progressPath))
	return nil
}

// initializeFreshProgress creates a new steps_done.json with the new total steps and empty completed indices
func (hcpo *StepBasedWorkflowOrchestrator) initializeFreshProgress(ctx context.Context, newTotalSteps int) error {
	freshProgress := &StepProgress{
		CompletedStepIndices:     []int{},
		TotalSteps:               newTotalSteps,
		LastUpdated:              time.Now(),
		BranchSteps:              make(map[int]BranchStepProgress),
		ValidationFailures:       make(map[string]int),
		DecisionEvaluationCounts: make(DecisionEvaluationCount),
	}

	if err := hcpo.saveStepProgress(ctx, freshProgress); err != nil {
		return fmt.Errorf("failed to initialize fresh progress: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Initialized fresh progress with %d total steps", newTotalSteps))
	return nil
}

// archiveLogsFolder archives (renames) a logs folder instead of deleting it
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

	// Generate archive name with timestamp
	timestamp := time.Now().Format("20060102-150405")
	archivedFolderName := fmt.Sprintf("%s-archived-%s", folderName, timestamp)

	// Build archived path (same parent directory)
	// Extract parent directory from logsFolderPath
	lastSlash := strings.LastIndex(logsFolderPath, "/")
	if lastSlash == -1 {
		return fmt.Errorf("invalid logs folder path: %s", logsFolderPath)
	}
	parentDir := logsFolderPath[:lastSlash]
	archivedFolderPath := fmt.Sprintf("%s/%s", parentDir, archivedFolderName)

	hcpo.GetLogger().Info(fmt.Sprintf("📦 Archiving logs folder: %s -> %s", folderName, archivedFolderName))

	// Use MoveWorkspaceFile to rename the folder
	if err := hcpo.MoveWorkspaceFile(ctx, logsFolderPath, archivedFolderPath); err != nil {
		return fmt.Errorf("failed to archive logs folder %s: %w", folderName, err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Successfully archived logs folder: %s -> %s", folderName, archivedFolderName))
	return nil
}

// deleteStepExecutionFolder deletes the execution folder for a specific step
// stepNumber is 1-based (e.g., step 1, step 2, etc.)
// This is used when resuming from a step or running a single step to ensure clean re-execution
// by removing any existing execution artifacts from previous runs
// Also deletes all branch step folders for this step (e.g., step-3-if-true-0, step-3-if-false-1, etc.)
// Also deletes decision step folder if it exists (e.g., step-8-decision)
// Also deletes all sub-agent step folders for this step (e.g., step-2-sub-agent-1, step-2-sub-agent-2, etc.)
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

	// Also delete all branch step folders for this step (e.g., step-3-if-true-0, step-3-if-false-1, etc.)
	// This ensures that when resuming from a step before a conditional step, all branch executions are cleaned up
	branchStepPrefix := fmt.Sprintf("step-%d-if-", stepNumber)
	branchFoldersDeleted := 0
	branchFoldersFound := []string{}

	// Also delete decision step folder if it exists (e.g., step-8-decision)
	decisionStepFolder := fmt.Sprintf("step-%d-decision", stepNumber)
	decisionFoldersDeleted := 0

	// Also delete all sub-agent step folders for this step (e.g., step-2-sub-agent-1, step-2-sub-agent-2, etc.)
	// This ensures that when resuming from a step before an orchestration step, all sub-agent executions are cleaned up
	subAgentStepPrefix := fmt.Sprintf("step-%d-sub-agent-", stepNumber)
	subAgentFoldersDeleted := 0
	subAgentFoldersFound := []string{}

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
			} else if strings.HasPrefix(file, subAgentStepPrefix) {
				// Check if this is a sub-agent step folder for the current step
				// Pattern: step-{N}-sub-agent-{index}
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
			}
		}

		if len(branchFoldersFound) == 0 && decisionFoldersDeleted == 0 && len(subAgentFoldersFound) == 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No branch step, decision step, or sub-agent step folders found for step %d", stepNumber))
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

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Deleted execution folder for step %d", stepNumber))
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
