package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
)

// ExecutionManager centralizes all execution lifecycle decisions
// It handles:
// - Mapping ExecutionStrategy to CleanupScope
// - Applying cleanup (folders + progress)
// - Preparing execution for batch groups
type ExecutionManager struct {
	orchestrator *HumanControlledTodoPlannerOrchestrator
}

// NewExecutionManager creates a new ExecutionManager
func NewExecutionManager(orch *HumanControlledTodoPlannerOrchestrator) *ExecutionManager {
	return &ExecutionManager{
		orchestrator: orch,
	}
}

// ============================================================================
// PHASE 1: DECIDE - Prepare execution setup based on options
// ============================================================================

// PrepareExecution analyzes execution options and returns ExecutionSetup
// This is the SINGLE place that maps ExecutionStrategy -> CleanupScope
func (em *ExecutionManager) PrepareExecution(
	ctx context.Context,
	opts *ExecutionOptions,
	existingProgress *StepProgress,
	totalSteps int,
	runFolder string,
) (*ExecutionSetup, error) {
	orch := em.orchestrator

	setup := &ExecutionSetup{
		RunFolder: runFolder,
		Context: &ExecutionContext{
			FastExecuteEndStep: -1, // Default: not set
		},
		Cleanup: CleanupScope{
			NewTotalSteps: totalSteps,
		},
		StartFromStep: 0,
	}

	// Handle nil opts - default to resume if progress exists, fresh start otherwise
	if opts == nil {
		if existingProgress != nil && len(existingProgress.CompletedStepIndices) > 0 {
			setup.Mode = ExecutionModeResume
			setup.StartFromStep = findNextIncompleteStep(existingProgress)
			orch.GetLogger().Infof("📋 No options provided, defaulting to resume from step %d", setup.StartFromStep+1)
		} else {
			setup.Mode = ExecutionModeFresh
			setup.Cleanup = CleanupScope{
				InitFreshProgress: true,
				NewTotalSteps:     totalSteps,
			}
			orch.GetLogger().Infof("📋 No options provided, defaulting to fresh start")
		}
		return setup, nil
	}

	// Map strategy to mode and cleanup
	switch opts.ExecutionStrategy {

	// === Fresh Start Strategies ===

	case ExecutionStrategyStartFromBeginning:
		setup.Mode = ExecutionModeFresh
		setup.Cleanup = CleanupScope{
			DeleteProgress:    true,
			InitFreshProgress: true,
			CleanAllSteps:     true,
			NewTotalSteps:     totalSteps,
		}

	case ExecutionStrategyStartFromBeginningNoHuman:
		setup.Mode = ExecutionModeFresh
		setup.Cleanup = CleanupScope{
			DeleteProgress:    true,
			InitFreshProgress: true,
			CleanAllSteps:     true,
			NewTotalSteps:     totalSteps,
		}
		setup.Context.SkipHumanInput = true

	// === Fast Execute Strategies ===

	case ExecutionStrategyFastExecuteAll:
		setup.Mode = ExecutionModeFastExecute
		setup.Cleanup = CleanupScope{
			DeleteProgress:    true,
			InitFreshProgress: true,
			CleanAllSteps:     true,
			NewTotalSteps:     totalSteps,
		}
		setup.Context.FastExecuteMode = true
		setup.Context.FastExecuteEndStep = totalSteps - 1
		setup.Context.SkipHumanInput = true

	case ExecutionStrategyFastExecuteRange:
		endStep := opts.FastExecuteEndStep
		if endStep <= 0 {
			endStep = totalSteps - 1
		}
		setup.Mode = ExecutionModeFastExecuteRange
		setup.Cleanup = CleanupScope{
			DeleteProgress:    true,
			InitFreshProgress: true,
			CleanAllSteps:     true, // Clean all for simplicity
			NewTotalSteps:     totalSteps,
		}
		setup.Context.FastExecuteMode = true
		setup.Context.FastExecuteEndStep = endStep
		setup.Context.SkipHumanInput = true

	// === Resume Strategies ===

	case ExecutionStrategyResumeFromStep:
		resumeStep := opts.ResumeFromStep // 1-based
		if resumeStep <= 0 {
			resumeStep = 1
		}
		setup.Mode = ExecutionModeResumeFromStep
		setup.StartFromStep = resumeStep - 1 // Convert to 0-based
		setup.Cleanup = CleanupScope{
			UpdateProgress: true,
			CleanFromStep:  resumeStep, // Delete step-N and all after
			NewTotalSteps:  totalSteps,
		}

	case ExecutionStrategyResumeFromStepNoHuman:
		resumeStep := opts.ResumeFromStep // 1-based
		if resumeStep <= 0 {
			resumeStep = 1
		}
		setup.Mode = ExecutionModeResumeFromStep
		setup.StartFromStep = resumeStep - 1 // Convert to 0-based
		setup.Cleanup = CleanupScope{
			UpdateProgress: true,
			CleanFromStep:  resumeStep, // Delete step-N and all after
			NewTotalSteps:  totalSteps,
		}
		setup.Context.SkipHumanInput = true

	case ExecutionStrategyFastResumeFromStep:
		resumeStep := opts.ResumeFromStep // 1-based
		if resumeStep <= 0 {
			resumeStep = 1
		}
		setup.Mode = ExecutionModeResumeFromStep
		setup.StartFromStep = resumeStep - 1
		setup.Cleanup = CleanupScope{
			UpdateProgress: true,
			CleanFromStep:  resumeStep,
			NewTotalSteps:  totalSteps,
		}
		setup.Context.FastExecuteMode = true
		setup.Context.FastExecuteEndStep = totalSteps - 1
		setup.Context.SkipHumanInput = true

	// === Single Step Execution ===

	case ExecutionStrategyRunSingleStep:
		targetStep := opts.ResumeFromStep // 1-based
		if targetStep <= 0 {
			targetStep = 1
		}
		setup.Mode = ExecutionModeSingleStep
		setup.StartFromStep = targetStep - 1 // Convert to 0-based
		setup.Cleanup = CleanupScope{
			CleanSpecificStep: targetStep, // Only delete step-N
			NewTotalSteps:     totalSteps,
		}
		setup.Context.RunSingleStepOnly = true
		setup.Context.SingleStepTarget = targetStep - 1

	// === Default (Resume) ===

	default:
		// Unknown or empty strategy - default to resume behavior
		if existingProgress != nil && len(existingProgress.CompletedStepIndices) > 0 {
			setup.Mode = ExecutionModeResume
			setup.StartFromStep = findNextIncompleteStep(existingProgress)
		} else {
			setup.Mode = ExecutionModeFresh
			setup.Cleanup = CleanupScope{
				InitFreshProgress: true,
				NewTotalSteps:     totalSteps,
			}
		}
		orch.GetLogger().Warnf("⚠️ Unknown execution strategy '%s', defaulting to mode: %s", opts.ExecutionStrategy, setup.Mode)
	}

	orch.GetLogger().Infof("📋 Prepared execution: mode=%s, startFrom=%d, cleanup=%s",
		setup.Mode, setup.StartFromStep+1, em.GetCleanupDescription(setup.Cleanup))

	return setup, nil
}

// PrepareForBatchGroup prepares execution for a specific group in batch mode
// Each group gets its own run folder and fresh execution state
func (em *ExecutionManager) PrepareForBatchGroup(
	ctx context.Context,
	groupID string,
	runFolder string,
	totalSteps int,
	variableValues map[string]string,
	isNewFolder bool, // true if folder was just created
	baseExecCtx *ExecutionContext, // Base context to inherit settings from
) (*ExecutionSetup, error) {
	orch := em.orchestrator

	// Clone base context or create new one
	var execCtx *ExecutionContext
	if baseExecCtx != nil {
		execCtx = &ExecutionContext{
			SkipHumanInput:     baseExecCtx.SkipHumanInput,
			FastExecuteMode:    baseExecCtx.FastExecuteMode,
			FastExecuteEndStep: baseExecCtx.FastExecuteEndStep,
			RunSingleStepOnly:  baseExecCtx.RunSingleStepOnly,
			SingleStepTarget:   baseExecCtx.SingleStepTarget,
		}
	} else {
		execCtx = &ExecutionContext{
			FastExecuteEndStep: -1,
		}
	}

	// Check if we're resuming from a specific step (from execution options)
	// Only use ResumeFromStep if the strategy is actually a resume strategy
	resumeStep := 0
	if orch.executionOptions != nil {
		strategy := orch.executionOptions.ExecutionStrategy
		isResumeStrategy := strategy == ExecutionStrategyResumeFromStep ||
			strategy == ExecutionStrategyResumeFromStepNoHuman ||
			strategy == ExecutionStrategyFastResumeFromStep ||
			strategy == ExecutionStrategyRunSingleStep

		if isResumeStrategy && orch.executionOptions.ResumeFromStep > 0 {
			resumeStep = orch.executionOptions.ResumeFromStep
			orch.GetLogger().Infof("🔍 Batch group cleanup: detected resume from step %d (strategy: %s)", resumeStep, strategy)
		} else if orch.executionOptions.ResumeFromStep > 0 {
			orch.GetLogger().Infof("🔍 Batch group cleanup: ResumeFromStep=%d but strategy=%s is not a resume strategy, ignoring ResumeFromStep",
				orch.executionOptions.ResumeFromStep, strategy)
		}
	}

	// Determine cleanup scope
	cleanup := CleanupScope{
		DeleteProgress:    !isNewFolder, // Only delete if folder existed
		InitFreshProgress: true,         // Always initialize fresh progress
		NewTotalSteps:     totalSteps,
	}

	// If resuming from a specific step, clean from that step instead of all steps
	if resumeStep > 0 && !isNewFolder {
		cleanup.CleanFromStep = resumeStep // Delete step-N and all after
		cleanup.UpdateProgress = true      // Update progress to remove steps >= resumeStep
		orch.GetLogger().Infof("🔧 Batch group cleanup: will clean from step %d onwards (preserving steps 1-%d)", resumeStep, resumeStep-1)
	} else if !isNewFolder {
		// If folder exists and not resuming, clean all steps
		cleanup.CleanAllSteps = true
	}

	// Determine start step and mode: if resuming, use resume step; otherwise start from beginning
	startFromStep := 0
	executionMode := ExecutionModeFresh // Default: each group starts fresh
	if resumeStep > 0 {
		startFromStep = resumeStep - 1              // Convert to 0-based (step 3 -> index 2)
		executionMode = ExecutionModeResumeFromStep // Use resume mode when resuming
		orch.GetLogger().Infof("🔧 Batch group execution: will start from step %d (0-based index: %d) in resume mode", resumeStep, startFromStep)
	}

	setup := &ExecutionSetup{
		Mode:           executionMode,
		GroupID:        groupID,
		RunFolder:      runFolder,
		VariableValues: variableValues,
		Context:        execCtx,
		StartFromStep:  startFromStep, // Start from resume step if resuming, otherwise from beginning
		Cleanup:        cleanup,
	}

	orch.GetLogger().Infof("📋 Prepared batch group '%s': folder=%s, isNew=%v, cleanup=%s",
		groupID, runFolder, isNewFolder, em.GetCleanupDescription(setup.Cleanup))

	return setup, nil
}

// ============================================================================
// PHASE 2: EXECUTE - Apply cleanup and prepare for execution
// ============================================================================

// ApplyCleanup performs all cleanup based on CleanupScope
// This should be called BEFORE starting execution
func (em *ExecutionManager) ApplyCleanup(ctx context.Context, setup *ExecutionSetup) error {
	orch := em.orchestrator
	scope := setup.Cleanup

	if !scope.HasCleanup() {
		orch.GetLogger().Infof("✅ No cleanup needed for mode: %s", setup.Mode)
		return nil
	}

	orch.GetLogger().Infof("🧹 Applying cleanup: %s", em.GetCleanupDescription(scope))

	// Ensure run folder is set (required for all cleanup operations)
	if setup.RunFolder == "" {
		return fmt.Errorf("run folder not set - cannot apply cleanup")
	}

	// Temporarily set the run folder on orchestrator for cleanup operations
	// (This is needed because low-level functions use hcpo.selectedRunFolder)
	previousRunFolder := orch.selectedRunFolder
	orch.selectedRunFolder = setup.RunFolder
	defer func() {
		// Restore if we're not in batch mode (batch mode keeps the new folder)
		if setup.GroupID == "" {
			// For single execution, we want to keep the folder set
			// Only restore if explicitly told to
		}
	}()

	// 1. Delete progress file if requested
	if scope.DeleteProgress {
		if err := orch.deleteStepProgress(ctx); err != nil {
			orch.GetLogger().Warnf("⚠️ Failed to delete progress: %v (continuing)", err)
		} else {
			orch.GetLogger().Infof("🗑️ Deleted steps_done.json")
		}
	}

	// 2. Handle execution folder cleanup
	if scope.CleanAllSteps {
		// Delete entire execution/ folder
		executionDir := fmt.Sprintf("%s/runs/%s/execution", orch.GetWorkspacePath(), setup.RunFolder)
		if err := orch.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
			orch.GetLogger().Warnf("⚠️ Failed to clean all steps: %v (continuing)", err)
		} else {
			orch.GetLogger().Infof("🗑️ Cleaned entire execution/ folder")
		}
	} else if scope.CleanFromStep > 0 {
		// Delete step-N through step-Total
		cleanedCount := 0
		for stepNum := scope.CleanFromStep; stepNum <= scope.NewTotalSteps; stepNum++ {
			if err := orch.deleteStepExecutionFolder(ctx, stepNum); err != nil {
				orch.GetLogger().Warnf("⚠️ Failed to delete step %d: %v (continuing)", stepNum, err)
			} else {
				cleanedCount++
			}
		}
		orch.GetLogger().Infof("🗑️ Cleaned %d step folders (step-%d to step-%d)",
			cleanedCount, scope.CleanFromStep, scope.NewTotalSteps)
	} else if scope.CleanSpecificStep > 0 {
		// Delete only specific step
		if err := orch.deleteStepExecutionFolder(ctx, scope.CleanSpecificStep); err != nil {
			orch.GetLogger().Warnf("⚠️ Failed to delete step %d: %v (continuing)", scope.CleanSpecificStep, err)
		} else {
			orch.GetLogger().Infof("🗑️ Cleaned step-%d folder", scope.CleanSpecificStep)
		}
	}

	// 3. Update existing progress if needed (remove steps >= StartFromStep)
	if scope.UpdateProgress && setup.StartFromStep > 0 {
		progress, err := orch.loadStepProgress(ctx)
		if err == nil && progress != nil {
			newCompleted := []int{}
			for _, idx := range progress.CompletedStepIndices {
				if idx < setup.StartFromStep {
					newCompleted = append(newCompleted, idx)
				}
			}
			removedCount := len(progress.CompletedStepIndices) - len(newCompleted)
			progress.CompletedStepIndices = newCompleted
			if err := orch.saveStepProgress(ctx, progress); err != nil {
				orch.GetLogger().Warnf("⚠️ Failed to update progress: %v", err)
			} else {
				orch.GetLogger().Infof("📝 Updated progress: removed %d steps >= step-%d",
					removedCount, setup.StartFromStep+1)
			}
		}
	}

	// 4. Initialize fresh progress if needed
	if scope.InitFreshProgress {
		if err := orch.initializeFreshProgress(ctx, scope.NewTotalSteps); err != nil {
			return fmt.Errorf("failed to initialize progress: %w", err)
		}
		orch.GetLogger().Infof("📝 Initialized fresh progress with %d steps", scope.NewTotalSteps)
	}

	// Restore previous run folder only if explicitly needed
	_ = previousRunFolder // Suppress unused warning

	orch.GetLogger().Infof("✅ Cleanup completed for mode: %s", setup.Mode)
	return nil
}

// ApplyExecutionContext applies the ExecutionSetup context to the orchestrator
// This sets the orchestrator flags based on the resolved setup
func (em *ExecutionManager) ApplyExecutionContext(setup *ExecutionSetup) {
	orch := em.orchestrator

	if setup.Context == nil {
		return
	}

	// Apply context flags to orchestrator
	orch.SetSkipHumanInput(setup.Context.SkipHumanInput)
	orch.SetFastExecuteMode(setup.Context.FastExecuteMode, setup.Context.FastExecuteEndStep)
	orch.SetRunSingleStepMode(setup.Context.RunSingleStepOnly, setup.Context.SingleStepTarget)

	// Set run folder
	if setup.RunFolder != "" {
		orch.selectedRunFolder = setup.RunFolder
	}

	// Set variable values for batch execution
	if setup.VariableValues != nil {
		orch.variableValues = setup.VariableValues
	}

	orch.GetLogger().Infof("🔧 Applied execution context: skipHuman=%v, fastMode=%v, singleStep=%v",
		setup.Context.SkipHumanInput, setup.Context.FastExecuteMode, setup.Context.RunSingleStepOnly)
}

// ============================================================================
// HELPERS
// ============================================================================

// GetCleanupDescription returns a human-readable description of the cleanup scope
func (em *ExecutionManager) GetCleanupDescription(scope CleanupScope) string {
	if !scope.HasCleanup() {
		return "none"
	}

	var parts []string

	// Progress cleanup
	if scope.DeleteProgress {
		parts = append(parts, "delete_progress")
	}
	if scope.InitFreshProgress {
		parts = append(parts, fmt.Sprintf("init_progress(%d steps)", scope.NewTotalSteps))
	}
	if scope.UpdateProgress {
		parts = append(parts, "update_progress")
	}

	// Folder cleanup
	if scope.CleanAllSteps {
		parts = append(parts, "clean_all_steps")
	} else if scope.CleanFromStep > 0 {
		parts = append(parts, fmt.Sprintf("clean_steps_%d_to_%d", scope.CleanFromStep, scope.NewTotalSteps))
	} else if scope.CleanSpecificStep > 0 {
		parts = append(parts, fmt.Sprintf("clean_step_%d", scope.CleanSpecificStep))
	}

	return strings.Join(parts, ", ")
}

// findNextIncompleteStep finds the next step that hasn't been completed
func findNextIncompleteStep(progress *StepProgress) int {
	if progress == nil || progress.TotalSteps == 0 {
		return 0
	}

	// Create a set of completed indices
	completedSet := make(map[int]bool)
	for _, idx := range progress.CompletedStepIndices {
		completedSet[idx] = true
	}

	// Find first incomplete step
	for i := 0; i < progress.TotalSteps; i++ {
		if !completedSet[i] {
			return i
		}
	}

	// All steps complete - return total (will be handled by caller)
	return progress.TotalSteps
}

// BuildExecutionContextFromSetup creates an ExecutionContext from setup
// Useful when you need the context without applying to orchestrator
func (em *ExecutionManager) BuildExecutionContextFromSetup(setup *ExecutionSetup) *ExecutionContext {
	if setup == nil || setup.Context == nil {
		return &ExecutionContext{
			FastExecuteEndStep: -1,
		}
	}
	return setup.Context
}

// ============================================================================
// CONVENIENCE METHODS - For gradual migration from scattered cleanup calls
// ============================================================================

// CleanupForSingleStep handles cleanup when running a single step
// This is a convenience method for the common pattern in controller.go
func (em *ExecutionManager) CleanupForSingleStep(ctx context.Context, targetStep int, runFolder string) error {
	orch := em.orchestrator

	if runFolder == "" {
		return fmt.Errorf("run folder not set - cannot cleanup for single step")
	}

	// Set run folder temporarily for cleanup
	previousRunFolder := orch.selectedRunFolder
	orch.selectedRunFolder = runFolder
	defer func() {
		orch.selectedRunFolder = previousRunFolder
	}()

	orch.GetLogger().Infof("🗑️ Cleaning up for single step execution: step-%d", targetStep)

	// Only delete the specific step folder
	if err := orch.deleteStepExecutionFolder(ctx, targetStep); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to delete step %d folder: %v", targetStep, err)
		return err
	}

	orch.GetLogger().Infof("✅ Cleaned up step-%d folder for single step execution", targetStep)
	return nil
}

// CleanupForResumeFromStep handles cleanup when resuming from a specific step
// This deletes step N and all subsequent steps, and updates progress
func (em *ExecutionManager) CleanupForResumeFromStep(ctx context.Context, resumeStep int, totalSteps int, runFolder string) error {
	orch := em.orchestrator

	if runFolder == "" {
		return fmt.Errorf("run folder not set - cannot cleanup for resume")
	}

	// Set run folder temporarily for cleanup
	previousRunFolder := orch.selectedRunFolder
	orch.selectedRunFolder = runFolder
	defer func() {
		orch.selectedRunFolder = previousRunFolder
	}()

	orch.GetLogger().Infof("🗑️ Cleaning up for resume from step %d (total: %d)", resumeStep, totalSteps)

	// Delete step folders from resumeStep to totalSteps
	cleanedCount := 0
	for stepNum := resumeStep; stepNum <= totalSteps; stepNum++ {
		if err := orch.deleteStepExecutionFolder(ctx, stepNum); err != nil {
			orch.GetLogger().Warnf("⚠️ Failed to delete step %d: %v (continuing)", stepNum, err)
		} else {
			cleanedCount++
		}
	}

	// Update progress: remove steps >= resumeStep-1 (0-based)
	progress, err := orch.loadStepProgress(ctx)
	if err == nil && progress != nil {
		newCompleted := []int{}
		startFromStep := resumeStep - 1 // Convert to 0-based
		for _, idx := range progress.CompletedStepIndices {
			if idx < startFromStep {
				newCompleted = append(newCompleted, idx)
			}
		}
		removedCount := len(progress.CompletedStepIndices) - len(newCompleted)
		progress.CompletedStepIndices = newCompleted
		if err := orch.saveStepProgress(ctx, progress); err != nil {
			orch.GetLogger().Warnf("⚠️ Failed to update progress: %v", err)
		} else {
			orch.GetLogger().Infof("📝 Updated progress: removed %d steps", removedCount)
		}
	}

	orch.GetLogger().Infof("✅ Cleaned %d step folders for resume from step %d", cleanedCount, resumeStep)
	return nil
}

// CleanupForFreshStart handles cleanup when starting from beginning
// This deletes all execution artifacts and initializes fresh progress
func (em *ExecutionManager) CleanupForFreshStart(ctx context.Context, totalSteps int, runFolder string) error {
	orch := em.orchestrator

	if runFolder == "" {
		return fmt.Errorf("run folder not set - cannot cleanup for fresh start")
	}

	// Set run folder temporarily for cleanup
	previousRunFolder := orch.selectedRunFolder
	orch.selectedRunFolder = runFolder
	defer func() {
		orch.selectedRunFolder = previousRunFolder
	}()

	orch.GetLogger().Infof("🗑️ Cleaning up for fresh start (total: %d steps)", totalSteps)

	// Delete progress file
	if err := orch.deleteStepProgress(ctx); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to delete progress: %v", err)
	}

	// Delete entire execution folder
	executionDir := fmt.Sprintf("%s/runs/%s/execution", orch.GetWorkspacePath(), runFolder)
	if err := orch.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to clean execution folder: %v", err)
	}

	// Initialize fresh progress
	if err := orch.initializeFreshProgress(ctx, totalSteps); err != nil {
		return fmt.Errorf("failed to initialize progress: %w", err)
	}

	orch.GetLogger().Infof("✅ Fresh start cleanup completed")
	return nil
}

// GetExecutionManager is a convenience method to get ExecutionManager from orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetExecutionManager() *ExecutionManager {
	return NewExecutionManager(hcpo)
}

// CleanupProgressOnly deletes the progress file only (no folder cleanup)
// Used for fast execute scenarios where we want to re-run but keep artifacts
func (em *ExecutionManager) CleanupProgressOnly(ctx context.Context) error {
	orch := em.orchestrator

	orch.GetLogger().Infof("🗑️ Cleaning up progress file only (fast execute mode)")

	if err := orch.deleteStepProgress(ctx); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to delete progress: %v", err)
		return err
	}

	orch.GetLogger().Infof("✅ Deleted progress file")
	return nil
}

// CleanupForPlanChange handles cleanup when plan structure changed
// This is used when user chooses to delete old progress and start fresh after plan change
// It handles backward compatibility with old folder structure
func (em *ExecutionManager) CleanupForPlanChange(ctx context.Context, totalSteps int, workspacePath, runMode string) error {
	orch := em.orchestrator

	orch.GetLogger().Infof("🧹 Cleaning up for plan change (total: %d steps)", totalSteps)

	// Delete progress file
	if err := orch.deleteStepProgress(ctx); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to delete progress: %v", err)
	}

	// Clean execution artifacts (handles both new and old structure for backward compat)
	if err := orch.cleanupExecutionArtifactsForFreshStart(ctx, workspacePath, runMode); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %v", err)
	}

	// Initialize fresh progress
	if err := orch.initializeFreshProgress(ctx, totalSteps); err != nil {
		return fmt.Errorf("failed to initialize progress: %w", err)
	}

	orch.GetLogger().Infof("✅ Plan change cleanup completed")
	return nil
}

// CleanupForStartFromBeginning handles cleanup when starting from beginning
// Similar to CleanupForPlanChange but used in different context
func (em *ExecutionManager) CleanupForStartFromBeginning(ctx context.Context, workspacePath, runMode string) error {
	orch := em.orchestrator

	orch.GetLogger().Infof("🧹 Cleaning up for start from beginning")

	// Delete progress file
	if err := orch.deleteStepProgress(ctx); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to delete progress: %v", err)
	}

	// Clean execution artifacts (handles both new and old structure)
	if err := orch.cleanupExecutionArtifactsForFreshStart(ctx, workspacePath, runMode); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %v", err)
	}

	orch.GetLogger().Infof("✅ Start from beginning cleanup completed")
	return nil
}

// CleanupExecutionFolder cleans only the execution folder (no progress changes)
// Used for fast execute range where we clean folders but keep progress structure
func (em *ExecutionManager) CleanupExecutionFolder(ctx context.Context, runFolder string) error {
	orch := em.orchestrator

	if runFolder == "" && orch.selectedRunFolder == "" {
		return fmt.Errorf("run folder not set - cannot cleanup execution folder")
	}

	targetFolder := runFolder
	if targetFolder == "" {
		targetFolder = orch.selectedRunFolder
	}

	var runWorkspacePath string
	if targetFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", orch.GetWorkspacePath(), targetFolder)
	} else {
		runWorkspacePath = orch.GetWorkspacePath()
	}

	executionDir := fmt.Sprintf("%s/execution", runWorkspacePath)

	orch.GetLogger().Infof("🗑️ Cleaning execution folder: %s", executionDir)

	if err := orch.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
		orch.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %v", err)
		return err
	}

	orch.GetLogger().Infof("✅ Cleaned execution folder")
	return nil
}
