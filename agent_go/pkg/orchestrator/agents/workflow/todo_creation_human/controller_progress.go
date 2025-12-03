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
		return "", fmt.Errorf("selectedRunFolder not set - run folder must be resolved before accessing steps_done.json")
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
		return nil, fmt.Errorf("failed to load step progress: %w", err)
	}

	var progress StepProgress
	if err := json.Unmarshal([]byte(content), &progress); err != nil {
		return nil, fmt.Errorf("failed to parse steps_done.json: %w", err)
	}

	// Backward compatibility: initialize BranchSteps if nil (old files won't have this field)
	if progress.BranchSteps == nil {
		progress.BranchSteps = make(map[int]BranchStepProgress)
		hcpo.GetLogger().Infof("📝 Initialized BranchSteps for backward compatibility")
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
		return fmt.Errorf("failed to marshal progress: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, progressPath, string(progressJSON)); err != nil {
		return fmt.Errorf("failed to write steps_done.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Saved step progress to %s", progressPath)

	// Emit step progress updated event for frontend dynamic updates
	hcpo.emitStepProgressUpdatedEvent(ctx, progress)

	return nil
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
		hcpo.GetLogger().Warnf("⚠️ Failed to emit step progress updated event: %v", err)
	} else {
		hcpo.GetLogger().Infof("📊 Emitted step progress updated event: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps)
	}
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
		return fmt.Errorf("failed to delete steps_done.json: %w", err)
	}

	hcpo.GetLogger().Infof("🗑️ Deleted step progress file: %s", progressPath)
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
		return fmt.Errorf("failed to initialize fresh progress: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Initialized fresh progress with %d total steps", newTotalSteps)
	return nil
}

// deleteStepExecutionFolder deletes the execution folder for a specific step
// stepNumber is 1-based (e.g., step 1, step 2, etc.)
// This is used when resuming from a step or running a single step to ensure clean re-execution
// by removing any existing execution artifacts from previous runs
func (hcpo *HumanControlledTodoPlannerOrchestrator) deleteStepExecutionFolder(ctx context.Context, stepNumber int) error {
	// Validate that run folder is set (required for building correct path)
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf("selectedRunFolder not set - run folder must be resolved before deleting execution folders")
	}

	// Build execution folder path: workspacePath/runs/{runFolder}/execution/step-{stepNumber}
	// Example: /workspace/runs/iteration-1/execution/step-3
	baseWorkspacePath := hcpo.GetWorkspacePath()
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	stepFolderPath := fmt.Sprintf("%s/step-%d", executionWorkspacePath, stepNumber)

	hcpo.GetLogger().Infof("🗑️ Deleting execution folder for step %d: %s", stepNumber, stepFolderPath)

	// Use CleanupDirectory to delete the step folder recursively
	// This removes all files and subdirectories within the step's execution folder
	// CleanupDirectory handles the recursive deletion and depth-first directory removal
	if err := hcpo.CleanupDirectory(ctx, stepFolderPath, fmt.Sprintf("execution/step-%d", stepNumber)); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to delete execution folder for step %d: %w", stepNumber, err)
		return err
	}

	hcpo.GetLogger().Infof("✅ Deleted execution folder for step %d", stepNumber)
	return nil
}
