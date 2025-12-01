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
