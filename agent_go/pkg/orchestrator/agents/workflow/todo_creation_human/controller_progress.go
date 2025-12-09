package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcpagent/events"
)

// getStepsProgressPath returns the path to steps_done.json file in the run folder
func (hcpo *HumanControlledTodoPlannerOrchestrator) getStepsProgressPath() (string, error) {
	if hcpo.selectedRunFolder == "" {
		return "", fmt.Errorf(fmt.Sprintf("selectedRunFolder not set - run folder must be resolved before accessing steps_done.json"), nil)
	}
	return fmt.Sprintf("%s/runs/%s/steps_done.json", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder), nil
}

// loadStepProgress loads progress from steps_done.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadStepProgress(ctx context.Context) (*StepProgress, error) {
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return nil, err
	}

	content, err := hcpo.ReadWorkspaceFile(ctx, progressPath)
	if err != nil {
		// File doesn't exist or error reading
		return nil, fmt.Errorf(fmt.Sprintf("failed to load step progress: %w", err), nil)
	}

	var progress StepProgress
	if err := json.Unmarshal([]byte(content), &progress); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse steps_done.json: %w", err), nil)
	}

	// Backward compatibility: initialize BranchSteps if nil (old files won't have this field)
	if progress.BranchSteps == nil {
		progress.BranchSteps = make(map[int]BranchStepProgress)
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Initialized BranchSteps for backward compatibility"))
	}

	return &progress, nil
}

// saveStepProgress saves progress to steps_done.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) saveStepProgress(ctx context.Context, progress *StepProgress) error {
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return err
	}

	progress.LastUpdated = time.Now()

	progressJSON, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to marshal progress: %w", err), nil)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, progressPath, string(progressJSON)); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write steps_done.json: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Saved step progress to %s", progressPath))

	// Emit step progress updated event for frontend dynamic updates
	hcpo.emitStepProgressUpdatedEvent(ctx, progress)

	return nil
}

// emitStepStartedEvent emits a step started event
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitStepStartedEvent(ctx context.Context, step TodoStep, stepIndex int, stepPath string, isBranchStep bool) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	stepTitle := step.Title
	if stepTitle == "" {
		stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
	}
	stepId := step.ID
	if stepId == "" {
		stepId = fmt.Sprintf("step-%d", stepIndex+1)
	}

	startedEvent := &events.StepStartedEvent{
		BaseEventData: events.BaseEventData{
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

	agentEvent := &events.AgentEvent{
		Type:      events.StepExecutionStart,
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitStepFinishedEvent(ctx context.Context, step TodoStep, stepIndex int, stepPath string, isBranchStep bool) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	stepTitle := step.Title
	if stepTitle == "" {
		stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
	}
	stepId := step.ID
	if stepId == "" {
		stepId = fmt.Sprintf("step-%d", stepIndex+1)
	}

	finishedEvent := &events.StepFinishedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepID:       stepId,
		StepIndex:    stepIndex,
		StepTitle:    stepTitle,
		StepPath:     stepPath,
		IsBranchStep: isBranchStep,
	}

	agentEvent := &events.AgentEvent{
		Type:      events.StepExecutionEnd,
		Timestamp: time.Now(),
		Data:      finishedEvent,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit step finished event: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_finished event for step %d: %s", stepIndex+1, stepTitle))
	}
}

// emitStepProgressUpdatedEvent emits an event when step progress is updated
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitStepProgressUpdatedEvent(ctx context.Context, progress *StepProgress) {
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

	// Convert BranchStepProgress to events.BranchStepProgress
	branchSteps := make(map[int]events.BranchStepProgress)
	for k, v := range progress.BranchSteps {
		branchSteps[k] = events.BranchStepProgress{
			BranchExecuted: v.BranchExecuted,
			CompletedSteps: v.CompletedSteps,
		}
	}

	eventData := &events.StepProgressUpdatedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		CompletedStepIndices: progress.CompletedStepIndices,
		TotalSteps:           progress.TotalSteps,
		WorkspacePath:        hcpo.GetWorkspacePath(),
		RunFolder:            hcpo.selectedRunFolder,
		LastCompletedStep:    lastCompletedStep,
		BranchSteps:          branchSteps,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
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

// cleanupProgressFromStep removes completed step indices from targetStepIndex onward and cleans up branch steps
func (hcpo *HumanControlledTodoPlannerOrchestrator) cleanupProgressFromStep(ctx context.Context, targetStepIndex int, progress *StepProgress) error {
	if progress == nil {
		return fmt.Errorf(fmt.Sprintf("progress is nil"), nil)
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

	// Save updated progress
	if err := hcpo.saveStepProgress(ctx, progress); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to save progress after cleanup: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Progress cleanup completed: removed %d completed step indices from step %d onward. Remaining completed steps: %d", removedCount, targetStepIndex+1, len(progress.CompletedStepIndices)))

	return nil
}

// deleteStepProgress deletes steps_done.json file
func (hcpo *HumanControlledTodoPlannerOrchestrator) deleteStepProgress(ctx context.Context) error {
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return err
	}

	if err := hcpo.DeleteWorkspaceFile(ctx, progressPath); err != nil {
		// Ignore error if file doesn't exist
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return fmt.Errorf(fmt.Sprintf("failed to delete steps_done.json: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleted step progress file: %s", progressPath))
	return nil
}

// initializeFreshProgress creates a new steps_done.json with the new total steps and empty completed indices
func (hcpo *HumanControlledTodoPlannerOrchestrator) initializeFreshProgress(ctx context.Context, newTotalSteps int) error {
	freshProgress := &StepProgress{
		CompletedStepIndices: []int{},
		TotalSteps:           newTotalSteps,
		LastUpdated:          time.Now(),
		BranchSteps:          make(map[int]BranchStepProgress),
	}

	if err := hcpo.saveStepProgress(ctx, freshProgress); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to initialize fresh progress: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Initialized fresh progress with %d total steps", newTotalSteps))
	return nil
}

// deleteStepExecutionFolder deletes the execution folder for a specific step
// stepNumber is 1-based (e.g., step 1, step 2, etc.)
// This is used when resuming from a step or running a single step to ensure clean re-execution
// by removing any existing execution artifacts from previous runs
// Also deletes all branch step folders for this step (e.g., step-3-if-true-0, step-3-if-false-1, etc.)
func (hcpo *HumanControlledTodoPlannerOrchestrator) deleteStepExecutionFolder(ctx context.Context, stepNumber int) error {
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
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete execution folder for step %d: %w", stepNumber, err))
		// Continue to try deleting branch step folders even if main folder deletion failed
	}

	// Also delete all branch step folders for this step (e.g., step-3-if-true-0, step-3-if-false-1, etc.)
	// This ensures that when resuming from a step before a conditional step, all branch executions are cleaned up
	branchStepPrefix := fmt.Sprintf("step-%d-if-", stepNumber)
	branchFoldersDeleted := 0
	branchFoldersFound := []string{}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Searching for branch step folders with prefix '%s' in execution directory", branchStepPrefix))

	// List all files/folders in the execution directory
	files, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, executionWorkspacePath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to list execution directory to find branch step folders: %w", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Found %d items in execution directory", len(files)))

		// Find and delete all branch step folders that match the pattern
		for _, file := range files {
			// Check if this is a branch step folder for the current step
			// Pattern: step-{N}-if-true-{idx} or step-{N}-if-false-{idx}
			if strings.HasPrefix(file, branchStepPrefix) {
				branchFoldersFound = append(branchFoldersFound, file)
				branchFolderPath := fmt.Sprintf("%s/%s", executionWorkspacePath, file)
				hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleting branch step folder: %s", file))
				if err := hcpo.CleanupDirectory(ctx, branchFolderPath, fmt.Sprintf("execution/%s", file)); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete branch step folder %s: %w", file, err))
				} else {
					branchFoldersDeleted++
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Successfully deleted branch step folder: %s", file))
				}
			}
		}

		if len(branchFoldersFound) == 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No branch step folders found for step %d (prefix: %s)", stepNumber, branchStepPrefix))
		}
	}

	if branchFoldersDeleted > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Deleted %d/%d branch step folder(s) for step %d: %v", branchFoldersDeleted, len(branchFoldersFound), stepNumber, branchFoldersFound))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Deleted execution folder for step %d", stepNumber))
	return nil
}
