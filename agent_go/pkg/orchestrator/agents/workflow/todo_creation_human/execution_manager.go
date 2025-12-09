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
			orch.GetLogger().Info(fmt.Sprintf("📋 No options provided, defaulting to resume from step %d", setup.StartFromStep+1))
		} else {
			setup.Mode = ExecutionModeFresh
			setup.Cleanup = CleanupScope{
				InitFreshProgress: true,
				NewTotalSteps:     totalSteps,
			}
			orch.GetLogger().Info(fmt.Sprintf("📋 No options provided, defaulting to fresh start"))
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
		// Check if resuming from branch step
		if opts.ResumeFromBranchStep != nil {
			// Resuming from branch step - start from parent conditional step
			setup.Mode = ExecutionModeResumeFromStep
			setup.StartFromStep = opts.ResumeFromBranchStep.ParentStepIndex // Already 0-based
			setup.Cleanup = CleanupScope{
				UpdateProgress: true,
				CleanFromStep:  opts.ResumeFromBranchStep.ParentStepIndex + 1, // Convert to 1-based for cleanup
				NewTotalSteps:  totalSteps,
			}
			setup.Context.ResumeBranchStep = opts.ResumeFromBranchStep
			orch.GetLogger().Info(fmt.Sprintf("🔀 Resuming from branch step: parent=%d, branch=%s, step=%d",
				opts.ResumeFromBranchStep.ParentStepIndex+1, opts.ResumeFromBranchStep.BranchType, opts.ResumeFromBranchStep.BranchStepIndex+1))
		} else {
			// Regular step resume
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
		}

	case ExecutionStrategyResumeFromStepNoHuman:
		// Check if resuming from branch step
		if opts.ResumeFromBranchStep != nil {
			setup.Mode = ExecutionModeResumeFromStep
			setup.StartFromStep = opts.ResumeFromBranchStep.ParentStepIndex
			setup.Cleanup = CleanupScope{
				UpdateProgress: true,
				CleanFromStep:  opts.ResumeFromBranchStep.ParentStepIndex + 1,
				NewTotalSteps:  totalSteps,
			}
			setup.Context.SkipHumanInput = true
			setup.Context.ResumeBranchStep = opts.ResumeFromBranchStep
			orch.GetLogger().Info(fmt.Sprintf("🔀 Resuming from branch step (no human): parent=%d, branch=%s, step=%d",
				opts.ResumeFromBranchStep.ParentStepIndex+1, opts.ResumeFromBranchStep.BranchType, opts.ResumeFromBranchStep.BranchStepIndex+1))
		} else {
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
		}

	case ExecutionStrategyFastResumeFromStep:
		// Check if resuming from branch step
		if opts.ResumeFromBranchStep != nil {
			setup.Mode = ExecutionModeResumeFromStep
			setup.StartFromStep = opts.ResumeFromBranchStep.ParentStepIndex
			setup.Cleanup = CleanupScope{
				UpdateProgress: true,
				CleanFromStep:  opts.ResumeFromBranchStep.ParentStepIndex + 1,
				NewTotalSteps:  totalSteps,
			}
			setup.Context.FastExecuteMode = true
			setup.Context.FastExecuteEndStep = totalSteps - 1
			setup.Context.SkipHumanInput = true
			setup.Context.ResumeBranchStep = opts.ResumeFromBranchStep
			orch.GetLogger().Info(fmt.Sprintf("🔀 Fast resuming from branch step: parent=%d, branch=%s, step=%d",
				opts.ResumeFromBranchStep.ParentStepIndex+1, opts.ResumeFromBranchStep.BranchType, opts.ResumeFromBranchStep.BranchStepIndex+1))
		} else {
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
		}

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
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Unknown execution strategy '%s', defaulting to mode: %s", opts.ExecutionStrategy, setup.Mode))
	}

	orch.GetLogger().Info(fmt.Sprintf("📋 Prepared execution: mode=%s, startFrom=%d, cleanup=%s",
		setup.Mode, setup.StartFromStep+1, em.GetCleanupDescription(setup.Cleanup)))

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

	// Check execution strategy and resume step
	resumeStep := 0
	isStartFromBeginningStrategy := false
	if orch.executionOptions != nil {
		strategy := orch.executionOptions.ExecutionStrategy

		// Check if this is a "start from beginning" strategy
		isStartFromBeginningStrategy = strategy == ExecutionStrategyStartFromBeginning ||
			strategy == ExecutionStrategyStartFromBeginningNoHuman ||
			strategy == ExecutionStrategyFastExecuteAll ||
			strategy == ExecutionStrategyFastExecuteRange

		// Check if this is a resume strategy
		isResumeStrategy := strategy == ExecutionStrategyResumeFromStep ||
			strategy == ExecutionStrategyResumeFromStepNoHuman ||
			strategy == ExecutionStrategyFastResumeFromStep ||
			strategy == ExecutionStrategyRunSingleStep

		orch.GetLogger().Info(fmt.Sprintf("🔍 Batch group cleanup: strategy=%s, ResumeFromStep=%d, isResumeStrategy=%v, isStartFromBeginningStrategy=%v",
			strategy, orch.executionOptions.ResumeFromStep, isResumeStrategy, isStartFromBeginningStrategy))

		if isResumeStrategy && orch.executionOptions.ResumeFromStep > 0 {
			resumeStep = orch.executionOptions.ResumeFromStep
			orch.GetLogger().Info(fmt.Sprintf("🔍 Batch group cleanup: detected resume from step %d (strategy: %s)", resumeStep, strategy))
		} else if orch.executionOptions.ResumeFromStep > 0 {
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ Batch group cleanup: ResumeFromStep=%d but strategy=%s is not a resume strategy, ignoring ResumeFromStep",
				orch.executionOptions.ResumeFromStep, strategy))
		} else if isResumeStrategy {
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ Batch group cleanup: strategy=%s is a resume strategy but ResumeFromStep=%d (<=0), will not resume",
				strategy, orch.executionOptions.ResumeFromStep))
		}
	} else {
		orch.GetLogger().Info(fmt.Sprintf("🔍 Batch group cleanup: executionOptions is nil, resumeStep=0"))
	}

	// Determine cleanup scope
	cleanup := CleanupScope{
		DeleteProgress:    !isNewFolder, // Only delete if folder existed
		InitFreshProgress: true,         // Always initialize fresh progress
		NewTotalSteps:     totalSteps,
	}

	// CleanAllSteps should ONLY be set for "start from beginning" strategies
	// Never set it when resuming from a step
	if resumeStep > 0 {
		// Resuming from a specific step - clean from that step onwards
		cleanup.CleanFromStep = resumeStep // Delete step-N and all after
		cleanup.UpdateProgress = true      // Update progress to remove steps >= resumeStep
		orch.GetLogger().Info(fmt.Sprintf("🔧 Batch group cleanup: will clean from step %d onwards (preserving steps 1-%d)", resumeStep, resumeStep-1))
	} else if isStartFromBeginningStrategy && !isNewFolder {
		// Only set CleanAllSteps if it's explicitly a "start from beginning" strategy AND folder exists
		cleanup.CleanAllSteps = true
		orch.GetLogger().Info(fmt.Sprintf("🔧 Batch group cleanup: will clean ALL steps (start from beginning strategy)"))
	} else if !isNewFolder {
		// Folder exists but not a "start from beginning" strategy and not resuming
		// Don't clean anything - preserve existing step folders
		orch.GetLogger().Info(fmt.Sprintf("🔧 Batch group cleanup: folder exists but not starting from beginning and not resuming - preserving existing step folders"))
	}

	// Determine start step and mode: if resuming, use resume step; otherwise start from beginning
	startFromStep := 0
	executionMode := ExecutionModeFresh // Default: each group starts fresh
	if resumeStep > 0 {
		startFromStep = resumeStep - 1              // Convert to 0-based (step 3 -> index 2)
		executionMode = ExecutionModeResumeFromStep // Use resume mode when resuming
		orch.GetLogger().Info(fmt.Sprintf("🔧 Batch group execution: will start from step %d (0-based index: %d) in resume mode", resumeStep, startFromStep))
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

	orch.GetLogger().Info(fmt.Sprintf("📋 Prepared batch group '%s': folder=%s, isNew=%v, cleanup=%s",
		groupID, runFolder, isNewFolder, em.GetCleanupDescription(setup.Cleanup)))

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
		orch.GetLogger().Info(fmt.Sprintf("✅ No cleanup needed for mode: %s", setup.Mode))
		return nil
	}

	orch.GetLogger().Info(fmt.Sprintf("🧹 Applying cleanup: %s", em.GetCleanupDescription(scope)))

	// Ensure run folder is set (required for all cleanup operations)
	if setup.RunFolder == "" {
		return fmt.Errorf(fmt.Sprintf("run folder not set - cannot apply cleanup"), nil)
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
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete progress: %v (continuing)", err))
		} else {
			orch.GetLogger().Info(fmt.Sprintf("🗑️ Deleted steps_done.json (including all branch progress)"))
		}
	}

	// 2. Handle execution folder cleanup
	// Log cleanup scope for debugging
	orch.GetLogger().Info(fmt.Sprintf("🔍 ApplyCleanup: CleanAllSteps=%v, CleanFromStep=%d, CleanSpecificStep=%d, Mode=%s, StartFromStep=%d",
		scope.CleanAllSteps, scope.CleanFromStep, scope.CleanSpecificStep, setup.Mode, setup.StartFromStep))

	// Safety check: CleanAllSteps should never be true when resuming from a specific step
	if scope.CleanAllSteps {
		if setup.Mode == ExecutionModeResumeFromStep || (setup.StartFromStep > 0 && setup.Mode != ExecutionModeFresh) {
			orch.GetLogger().Warn(fmt.Sprintf("🚨 BUG: CleanAllSteps=true but mode=%s, startFromStep=%d! This should never happen when resuming. Falling back to CleanFromStep=%d",
				setup.Mode, setup.StartFromStep, setup.StartFromStep+1))
			// Fall back to cleaning only from the resume step
			scope.CleanAllSteps = false
			if setup.StartFromStep >= 0 {
				scope.CleanFromStep = setup.StartFromStep + 1 // Convert to 1-based
				scope.UpdateProgress = true
			}
		} else {
			// Delete entire execution/ folder (only for fresh starts)
			executionDir := fmt.Sprintf("%s/runs/%s/execution", orch.GetWorkspacePath(), setup.RunFolder)
			orch.GetLogger().Info(fmt.Sprintf("🗑️ CleanAllSteps=true: Deleting entire execution/ folder (mode=%s, startFromStep=%d)", setup.Mode, setup.StartFromStep))
			if err := orch.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
				orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to clean all steps: %v (continuing)", err))
			} else {
				orch.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned entire execution/ folder"))
			}
		}
	}

	if scope.CleanFromStep > 0 {
		// Delete step-N through step-Total
		cleanedCount := 0
		for stepNum := scope.CleanFromStep; stepNum <= scope.NewTotalSteps; stepNum++ {
			if err := orch.deleteStepExecutionFolder(ctx, stepNum); err != nil {
				orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete step %d: %v (continuing)", stepNum, err))
			} else {
				cleanedCount++
			}
		}
		orch.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned %d step folders (step-%d to step-%d)", cleanedCount, scope.CleanFromStep, scope.NewTotalSteps))
	} else if scope.CleanSpecificStep > 0 {
		// Delete only specific step
		if err := orch.deleteStepExecutionFolder(ctx, scope.CleanSpecificStep); err != nil {
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete step %d: %v (continuing)", scope.CleanSpecificStep, err))
		} else {
			orch.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned step-%d folder", scope.CleanSpecificStep))
		}
	}

	// 3. Update existing progress if needed (remove steps >= StartFromStep)
	// Note: StartFromStep can be 0 (resuming from step 1), so we check >= 0
	if scope.UpdateProgress && setup.StartFromStep >= 0 {
		progress, err := orch.loadStepProgress(ctx)
		if err != nil {
			// If progress file doesn't exist or can't be loaded, log warning but don't fail
			// This can happen if the file was deleted or corrupted
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load progress for update: %v (progress file may not exist yet)", err))
		} else if progress != nil {
			// Preserve TotalSteps from existing progress (don't overwrite with NewTotalSteps)
			// Only update CompletedStepIndices to remove steps >= StartFromStep
			orch.GetLogger().Info(fmt.Sprintf("🔍 Updating progress: StartFromStep=%d (0-based, step %d), existing completed steps: %v (total: %d)",
				setup.StartFromStep, setup.StartFromStep+1, progress.CompletedStepIndices, len(progress.CompletedStepIndices)))
			newCompleted := []int{}
			for _, idx := range progress.CompletedStepIndices {
				if idx < setup.StartFromStep {
					newCompleted = append(newCompleted, idx)
				}
			}
			removedCount := len(progress.CompletedStepIndices) - len(newCompleted)

			// Safety check: if we're about to clear all steps, log a warning
			if len(newCompleted) == 0 && len(progress.CompletedStepIndices) > 0 && setup.StartFromStep > 0 {
				orch.GetLogger().Warn(fmt.Sprintf("⚠️ WARNING: About to clear ALL completed steps! StartFromStep=%d, existing steps: %v. This might be a bug.",
					setup.StartFromStep, progress.CompletedStepIndices))
			}

			orch.GetLogger().Info(fmt.Sprintf("🔍 Progress update: keeping steps %v (removing %d steps >= step %d)",
				newCompleted, removedCount, setup.StartFromStep+1))
			progress.CompletedStepIndices = newCompleted

			// Handle branch progress cleanup
			branchStepsRemoved := 0
			if progress.BranchSteps != nil {
				orch.GetLogger().Info(fmt.Sprintf("🔍 Starting branch progress cleanup (found %d branch progress entries)", len(progress.BranchSteps)))
				// Special handling for resuming from branch step
				if setup.Context != nil && setup.Context.ResumeBranchStep != nil {
					orch.GetLogger().Info(fmt.Sprintf("🔀 Branch step resume mode: parent=%d, branch=%s, branch_step=%d",
						setup.Context.ResumeBranchStep.ParentStepIndex+1, setup.Context.ResumeBranchStep.BranchType, setup.Context.ResumeBranchStep.BranchStepIndex+1))
					// We're resuming from a branch step - keep the parent step's branch progress
					// but remove completed branch steps within that branch that are >= resume point
					parentStepIdx := setup.Context.ResumeBranchStep.ParentStepIndex
					branchType := setup.Context.ResumeBranchStep.BranchType
					resumeBranchStepIdx := setup.Context.ResumeBranchStep.BranchStepIndex

					if branchProgress, exists := progress.BranchSteps[parentStepIdx]; exists {
						orch.GetLogger().Info(fmt.Sprintf("📋 Found branch progress for step %d: branch_executed=%s, completed_steps=%d",
							parentStepIdx+1, branchProgress.BranchExecuted, len(branchProgress.CompletedSteps)))
						// Keep the branch progress but remove completed steps >= resume point
						branchExecutedStr := map[string]string{"if_true": "if-true", "if_false": "if-false"}[branchType]
						newCompletedSteps := []string{}

						for _, completedPath := range branchProgress.CompletedSteps {
							// Parse the path: "step-{N}-{if-true/if-false}-{idx}"
							// Only keep paths where the branch step index < resume point
							// Format: step-{parentStep+1}-{branchExecutedStr}-{branchStepIdx}
							expectedPrefix := fmt.Sprintf("step-%d-%s-", parentStepIdx+1, branchExecutedStr)
							if strings.HasPrefix(completedPath, expectedPrefix) {
								// Extract branch step index from path
								suffix := strings.TrimPrefix(completedPath, expectedPrefix)
								var branchStepIdx int
								if _, err := fmt.Sscanf(suffix, "%d", &branchStepIdx); err == nil {
									if branchStepIdx < resumeBranchStepIdx {
										// Keep this completed step (before resume point)
										newCompletedSteps = append(newCompletedSteps, completedPath)
									}
									// Otherwise, skip it (>= resume point, will be removed)
								} else {
									// Can't parse - keep it to be safe
									newCompletedSteps = append(newCompletedSteps, completedPath)
								}
							} else {
								// Not a branch step path for this branch - keep it
								newCompletedSteps = append(newCompletedSteps, completedPath)
							}
						}

						removedFromBranch := len(branchProgress.CompletedSteps) - len(newCompletedSteps)
						if removedFromBranch > 0 {
							orch.GetLogger().Info(fmt.Sprintf("🧹 Removing %d completed branch steps from step %d branch (keeping %d, resuming from branch step %d)",
								removedFromBranch, parentStepIdx+1, len(newCompletedSteps), resumeBranchStepIdx+1))
							branchProgress.CompletedSteps = newCompletedSteps
							progress.BranchSteps[parentStepIdx] = branchProgress
							branchStepsRemoved = removedFromBranch
						} else {
							orch.GetLogger().Info(fmt.Sprintf("ℹ️ No branch steps to remove from step %d branch (all steps are before resume point)", parentStepIdx+1))
						}

						// Remove branch progress for steps AFTER the parent step
						branchStepsToRemove := make([]int, 0)
						for stepIdx := range progress.BranchSteps {
							if stepIdx > parentStepIdx {
								branchStepsToRemove = append(branchStepsToRemove, stepIdx)
							}
						}
						if len(branchStepsToRemove) > 0 {
							orch.GetLogger().Info(fmt.Sprintf("🧹 Removing branch progress for %d step(s) after parent step %d: %v",
								len(branchStepsToRemove), parentStepIdx+1, branchStepsToRemove))
						}
						for _, stepIdx := range branchStepsToRemove {
							delete(progress.BranchSteps, stepIdx)
							branchStepsRemoved++
						}
					} else {
						// No existing branch progress for parent step - this shouldn't happen when resuming
						orch.GetLogger().Warn(fmt.Sprintf("⚠️ Resuming from branch step but no branch progress found for parent step %d", parentStepIdx+1))
					}
				} else {
					// Regular resume - remove branch progress for steps >= StartFromStep
					orch.GetLogger().Info(fmt.Sprintf("🔄 Regular resume mode: removing branch progress for steps >= %d (0-based: %d)", setup.StartFromStep+1, setup.StartFromStep))
					branchStepsToRemove := make([]int, 0)
					for stepIdx := range progress.BranchSteps {
						if stepIdx >= setup.StartFromStep {
							branchStepsToRemove = append(branchStepsToRemove, stepIdx)
						}
					}
					if len(branchStepsToRemove) > 0 {
						orch.GetLogger().Info(fmt.Sprintf("🧹 Removing branch progress for %d step(s): %v", len(branchStepsToRemove), branchStepsToRemove))
						for _, stepIdx := range branchStepsToRemove {
							if branchProgress, exists := progress.BranchSteps[stepIdx]; exists {
								orch.GetLogger().Info(fmt.Sprintf("  - Step %d: branch_executed=%s, completed_steps=%d",
									stepIdx+1, branchProgress.BranchExecuted, len(branchProgress.CompletedSteps)))
							}
							delete(progress.BranchSteps, stepIdx)
							branchStepsRemoved++
						}
						orch.GetLogger().Info(fmt.Sprintf("✅ Removed %d branch step progress entries from step %d onward", branchStepsRemoved, setup.StartFromStep+1))
					} else {
						orch.GetLogger().Info(fmt.Sprintf("ℹ️ No branch progress entries to remove (all branch steps are before step %d)", setup.StartFromStep+1))
					}
				}
			} else {
				orch.GetLogger().Info(fmt.Sprintf("ℹ️ No branch progress entries found in progress file"))
			}

			// Ensure TotalSteps is preserved (use existing value, or fallback to NewTotalSteps if 0)
			if progress.TotalSteps == 0 && scope.NewTotalSteps > 0 {
				progress.TotalSteps = scope.NewTotalSteps
				orch.GetLogger().Info(fmt.Sprintf("📝 Progress had TotalSteps=0, setting to %d", scope.NewTotalSteps))
			}

			if err := orch.saveStepProgress(ctx, progress); err != nil {
				orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update progress: %v", err))
			} else {
				orch.GetLogger().Info(fmt.Sprintf("📝 Updated progress: removed %d completed steps and %d branch progress entries >= step-%d, preserved TotalSteps=%d",
					removedCount, branchStepsRemoved, setup.StartFromStep+1, progress.TotalSteps))
			}
		} else {
			// Progress is nil (shouldn't happen if loadStepProgress succeeded, but handle it)
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ Progress is nil after successful load (unexpected)"))
		}
	}

	// 4. Initialize fresh progress if needed
	if scope.InitFreshProgress {
		if err := orch.initializeFreshProgress(ctx, scope.NewTotalSteps); err != nil {
			return fmt.Errorf(fmt.Sprintf("failed to initialize progress: %w", err), nil)
		}
		orch.GetLogger().Info(fmt.Sprintf("📝 Initialized fresh progress with %d steps", scope.NewTotalSteps))
	}

	// Restore previous run folder only if explicitly needed
	_ = previousRunFolder // Suppress unused warning

	orch.GetLogger().Info(fmt.Sprintf("✅ Cleanup completed for mode: %s", setup.Mode))
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

	orch.GetLogger().Info(fmt.Sprintf("🔧 Applied execution context: skipHuman=%v, fastMode=%v, singleStep=%v",
		setup.Context.SkipHumanInput, setup.Context.FastExecuteMode, setup.Context.RunSingleStepOnly))
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
		return fmt.Errorf(fmt.Sprintf("run folder not set - cannot cleanup for single step"), nil)
	}

	// Set run folder temporarily for cleanup
	previousRunFolder := orch.selectedRunFolder
	orch.selectedRunFolder = runFolder
	defer func() {
		orch.selectedRunFolder = previousRunFolder
	}()

	orch.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning up for single step execution: step-%d", targetStep))

	// Only delete the specific step folder
	if err := orch.deleteStepExecutionFolder(ctx, targetStep); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete step %d folder: %v", targetStep, err))
		return err
	}

	orch.GetLogger().Info(fmt.Sprintf("✅ Cleaned up step-%d folder for single step execution", targetStep))
	return nil
}

// CleanupForResumeFromStep handles cleanup when resuming from a specific step
// This deletes step N and all subsequent steps, and updates progress
func (em *ExecutionManager) CleanupForResumeFromStep(ctx context.Context, resumeStep int, totalSteps int, runFolder string) error {
	orch := em.orchestrator

	if runFolder == "" {
		return fmt.Errorf(fmt.Sprintf("run folder not set - cannot cleanup for resume"), nil)
	}

	// Set run folder temporarily for cleanup
	previousRunFolder := orch.selectedRunFolder
	orch.selectedRunFolder = runFolder
	defer func() {
		orch.selectedRunFolder = previousRunFolder
	}()

	orch.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning up for resume from step %d (total: %d)", resumeStep, totalSteps))

	// Delete step folders from resumeStep to totalSteps
	cleanedCount := 0
	for stepNum := resumeStep; stepNum <= totalSteps; stepNum++ {
		if err := orch.deleteStepExecutionFolder(ctx, stepNum); err != nil {
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete step %d: %v (continuing)", stepNum, err))
		} else {
			cleanedCount++
		}
	}

	// Update progress: remove steps >= resumeStep-1 (0-based)
	progress, err := orch.loadStepProgress(ctx)
	if err != nil {
		// If progress file doesn't exist or can't be loaded, log warning but don't fail
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load progress for resume cleanup: %v (progress file may not exist yet)", err))
	} else if progress != nil {
		newCompleted := []int{}
		startFromStep := resumeStep - 1 // Convert to 0-based
		for _, idx := range progress.CompletedStepIndices {
			if idx < startFromStep {
				newCompleted = append(newCompleted, idx)
			}
		}
		removedCount := len(progress.CompletedStepIndices) - len(newCompleted)
		progress.CompletedStepIndices = newCompleted

		// Remove branch progress for steps >= startFromStep
		branchStepsRemoved := 0
		if progress.BranchSteps != nil {
			branchStepsToRemove := make([]int, 0)
			for stepIdx := range progress.BranchSteps {
				if stepIdx >= startFromStep {
					branchStepsToRemove = append(branchStepsToRemove, stepIdx)
				}
			}
			for _, stepIdx := range branchStepsToRemove {
				delete(progress.BranchSteps, stepIdx)
				branchStepsRemoved++
			}
			if branchStepsRemoved > 0 {
				orch.GetLogger().Info(fmt.Sprintf("🧹 Removed %d branch step progress entries from step %d onward", branchStepsRemoved, resumeStep))
			}
		}

		// Preserve TotalSteps - use existing value, or fallback to provided totalSteps if 0
		if progress.TotalSteps == 0 && totalSteps > 0 {
			progress.TotalSteps = totalSteps
			orch.GetLogger().Info(fmt.Sprintf("📝 Progress had TotalSteps=0, setting to %d", totalSteps))
		}

		if err := orch.saveStepProgress(ctx, progress); err != nil {
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update progress: %v", err))
		} else {
			orch.GetLogger().Info(fmt.Sprintf("📝 Updated progress: removed %d completed steps and %d branch progress entries, preserved TotalSteps=%d", removedCount, branchStepsRemoved, progress.TotalSteps))
		}
	} else {
		// Progress is nil (shouldn't happen if loadStepProgress succeeded, but handle it)
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Progress is nil after successful load (unexpected)"))
	}

	orch.GetLogger().Info(fmt.Sprintf("✅ Cleaned %d step folders for resume from step %d", cleanedCount, resumeStep))
	return nil
}

// CleanupForFreshStart handles cleanup when starting from beginning
// This deletes all execution artifacts and initializes fresh progress
func (em *ExecutionManager) CleanupForFreshStart(ctx context.Context, totalSteps int, runFolder string) error {
	orch := em.orchestrator

	if runFolder == "" {
		return fmt.Errorf(fmt.Sprintf("run folder not set - cannot cleanup for fresh start"), nil)
	}

	// Set run folder temporarily for cleanup
	previousRunFolder := orch.selectedRunFolder
	orch.selectedRunFolder = runFolder
	defer func() {
		orch.selectedRunFolder = previousRunFolder
	}()

	orch.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning up for fresh start (total: %d steps)", totalSteps))

	// Delete progress file
	if err := orch.deleteStepProgress(ctx); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete progress: %v", err))
	}

	// Delete entire execution folder
	executionDir := fmt.Sprintf("%s/runs/%s/execution", orch.GetWorkspacePath(), runFolder)
	if err := orch.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to clean execution folder: %v", err))
	}

	// Initialize fresh progress
	if err := orch.initializeFreshProgress(ctx, totalSteps); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to initialize progress: %w", err), nil)
	}

	orch.GetLogger().Info(fmt.Sprintf("✅ Fresh start cleanup completed"))
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

	orch.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning up progress file only (fast execute mode)"))

	if err := orch.deleteStepProgress(ctx); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete progress: %v", err))
		return err
	}

	orch.GetLogger().Info(fmt.Sprintf("✅ Deleted progress file"))
	return nil
}

// CleanupForPlanChange handles cleanup when plan structure changed
// This is used when user chooses to delete old progress and start fresh after plan change
// It handles backward compatibility with old folder structure
func (em *ExecutionManager) CleanupForPlanChange(ctx context.Context, totalSteps int, workspacePath, runMode string) error {
	orch := em.orchestrator

	orch.GetLogger().Info(fmt.Sprintf("🧹 Cleaning up for plan change (total: %d steps)", totalSteps))

	// Delete progress file
	if err := orch.deleteStepProgress(ctx); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete progress: %v", err))
	}

	// Clean execution artifacts (handles both new and old structure for backward compat)
	if err := orch.cleanupExecutionArtifactsForFreshStart(ctx, workspacePath, runMode); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup execution artifacts: %v", err))
	}

	// Initialize fresh progress
	if err := orch.initializeFreshProgress(ctx, totalSteps); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to initialize progress: %w", err), nil)
	}

	orch.GetLogger().Info(fmt.Sprintf("✅ Plan change cleanup completed"))
	return nil
}

// CleanupForStartFromBeginning handles cleanup when starting from beginning
// Similar to CleanupForPlanChange but used in different context
func (em *ExecutionManager) CleanupForStartFromBeginning(ctx context.Context, workspacePath, runMode string) error {
	orch := em.orchestrator

	orch.GetLogger().Info(fmt.Sprintf("🧹 Cleaning up for start from beginning"))

	// Delete progress file
	if err := orch.deleteStepProgress(ctx); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete progress: %v", err))
	}

	// Clean execution artifacts (handles both new and old structure)
	if err := orch.cleanupExecutionArtifactsForFreshStart(ctx, workspacePath, runMode); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup execution artifacts: %v", err))
	}

	orch.GetLogger().Info(fmt.Sprintf("✅ Start from beginning cleanup completed"))
	return nil
}

// CleanupExecutionFolder cleans only the execution folder (no progress changes)
// Used for fast execute range where we clean folders but keep progress structure
func (em *ExecutionManager) CleanupExecutionFolder(ctx context.Context, runFolder string) error {
	orch := em.orchestrator

	if runFolder == "" && orch.selectedRunFolder == "" {
		return fmt.Errorf(fmt.Sprintf("run folder not set - cannot cleanup execution folder"), nil)
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

	orch.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning execution folder: %s", executionDir))

	if err := orch.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup execution directory: %v", err))
		return err
	}

	orch.GetLogger().Info(fmt.Sprintf("✅ Cleaned execution folder"))
	return nil
}

// CleanupDownloadsFolder cleans the Downloads folder before step execution
// This ensures a clean state for each step by removing any files downloaded in previous steps
func (em *ExecutionManager) CleanupDownloadsFolder(ctx context.Context) error {
	orch := em.orchestrator

	// Use "Downloads" as relative path - workspace API will handle conversion to /app/workspace/Downloads
	// Note: CleanupDirectory will skip the Downloads folder itself and only delete files inside it
	downloadsDir := "Downloads"

	orch.GetLogger().Info(fmt.Sprintf("🗑️ [DOWNLOADS CLEANUP] Starting cleanup of Downloads folder: %s", downloadsDir))
	orch.GetLogger().Info(fmt.Sprintf("🔍 [DOWNLOADS CLEANUP] Workspace path: %s", orch.GetWorkspacePath()))

	// Check if Downloads folder exists before attempting cleanup
	// Use ListWorkspaceFiles to check if folder exists and has contents
	files, listErr := orch.BaseOrchestrator.ListWorkspaceFiles(ctx, downloadsDir)
	if listErr != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ [DOWNLOADS CLEANUP] Failed to list files in Downloads folder: %v (folder may not exist)", listErr))
		orch.GetLogger().Info(fmt.Sprintf("ℹ️ [DOWNLOADS CLEANUP] Downloads folder does not exist or is empty - nothing to clean"))
		return nil // Return nil to allow execution to continue
	}

	if len(files) == 0 {
		orch.GetLogger().Info(fmt.Sprintf("ℹ️ [DOWNLOADS CLEANUP] Downloads folder exists but is empty - nothing to clean"))
		return nil
	}

	orch.GetLogger().Info(fmt.Sprintf("📊 [DOWNLOADS CLEANUP] Found %d files/directories in Downloads folder before cleanup: %v", len(files), files))

	// Attempt cleanup
	if err := orch.CleanupDirectory(ctx, downloadsDir, "Downloads"); err != nil {
		// Non-blocking: log warning but don't fail - Downloads folder may not exist
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ [DOWNLOADS CLEANUP] Failed to cleanup Downloads folder: %v (continuing)", err))
		return nil // Return nil to allow execution to continue
	}

	// Verify cleanup by listing again
	filesAfter, listErrAfter := orch.BaseOrchestrator.ListWorkspaceFiles(ctx, downloadsDir)
	if listErrAfter != nil {
		orch.GetLogger().Warn(fmt.Sprintf("⚠️ [DOWNLOADS CLEANUP] Failed to verify cleanup (cannot list files after cleanup): %v", listErrAfter))
	} else {
		if len(filesAfter) == 0 {
			orch.GetLogger().Info(fmt.Sprintf("✅ [DOWNLOADS CLEANUP] Successfully cleaned Downloads folder (removed %d items, folder is now empty)", len(files)))
		} else {
			orch.GetLogger().Warn(fmt.Sprintf("⚠️ [DOWNLOADS CLEANUP] Cleanup incomplete - %d items remain in Downloads folder: %v (expected: 0)", len(filesAfter), filesAfter))
		}
	}

	return nil
}
