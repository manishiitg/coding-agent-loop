package todo_creation_human

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"mcpagent/events"
)

// BatchExecutionResult contains the result of batch execution
type BatchExecutionResult struct {
	TotalGroups       int
	CompletedGroups   int
	FailedGroups      int
	CanceledGroups    int
	Duration          time.Duration
	Success           bool
	Error             string
	CompletedGroupIDs []string
	FailedGroupIDs    []string
}

// getEnabledGroupsForExecution returns the list of groups to execute
// Priority: ExecutionOptions.EnabledGroupIDs > manifest enabled groups
func (hcpo *HumanControlledTodoPlannerOrchestrator) getEnabledGroupsForExecution() []VariableGroup {
	if hcpo.variablesManifest == nil {
		return nil
	}

	// Check if ExecutionOptions specifies specific group IDs
	if hcpo.executionOptions != nil && len(hcpo.executionOptions.EnabledGroupIDs) > 0 {
		// Use specified group IDs from ExecutionOptions
		var groups []VariableGroup
		for _, groupID := range hcpo.executionOptions.EnabledGroupIDs {
			for _, g := range hcpo.variablesManifest.Groups {
				if g.GroupID == groupID {
					groups = append(groups, g)
					break
				}
			}
		}
		return groups
	}

	// Fall back to manifest's enabled groups
	return hcpo.variablesManifest.GetEnabledGroups()
}

// shouldUseBatchExecution determines if batch execution mode should be used
func (hcpo *HumanControlledTodoPlannerOrchestrator) shouldUseBatchExecution() bool {
	enabledGroups := hcpo.getEnabledGroupsForExecution()
	return len(enabledGroups) > 1
}

// runBatchExecution executes the workflow for multiple variable groups sequentially
// Uses ExecutionManager for centralized cleanup and progress management
func (hcpo *HumanControlledTodoPlannerOrchestrator) runBatchExecution(
	ctx context.Context,
	breakdownSteps []TodoStep,
	iteration int,
	execCtx *ExecutionContext,
) (*BatchExecutionResult, error) {
	enabledGroups := hcpo.getEnabledGroupsForExecution()
	totalGroups := len(enabledGroups)

	if totalGroups == 0 {
		return nil, fmt.Errorf("no enabled variable groups found for batch execution")
	}

	hcpo.GetLogger().Infof("🔄 Starting batch execution for %d variable groups", totalGroups)

	// Create ExecutionManager for centralized cleanup management
	execManager := NewExecutionManager(hcpo)

	// Emit batch execution start event
	enabledGroupIDs := make([]string, len(enabledGroups))
	for i, g := range enabledGroups {
		enabledGroupIDs[i] = g.GroupID
	}
	hcpo.emitBatchExecutionStartEvent(ctx, totalGroups, enabledGroupIDs, iteration)

	result := &BatchExecutionResult{
		TotalGroups:       totalGroups,
		CompletedGroupIDs: make([]string, 0),
		FailedGroupIDs:    make([]string, 0),
	}
	startTime := time.Now()

	// Determine base iteration folder
	baseIterationFolder := hcpo.determineBaseIterationFolder(ctx)

	// Execute for each enabled group sequentially
	for groupIndex, group := range enabledGroups {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			hcpo.GetLogger().Warnf("⚠️ Batch execution canceled during group %s", group.GroupID)
			result.CanceledGroups = totalGroups - groupIndex
			remainingGroupIDs := make([]string, 0)
			for i := groupIndex + 1; i < totalGroups; i++ {
				remainingGroupIDs = append(remainingGroupIDs, enabledGroups[i].GroupID)
			}
			hcpo.emitBatchExecutionCanceledEvent(ctx, totalGroups, groupIndex, group.GroupID, remainingGroupIDs, "context canceled")
			result.Error = "batch execution canceled"
			result.Duration = time.Since(startTime)
			return result, ctx.Err()
		default:
		}

		hcpo.GetLogger().Infof("📦 Batch execution: processing group %d/%d (%s)", groupIndex+1, totalGroups, group.GroupID)

		// Determine run folder for this group
		runFolder := hcpo.createGroupRunFolder(baseIterationFolder, group.GroupID, totalGroups)

		// Check if folder exists (to determine if we need cleanup)
		fullRunFolderPath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), runFolder)
		isNewFolder := true
		if exists, _ := hcpo.workspaceFileExists(ctx, fullRunFolderPath); exists {
			isNewFolder = false
		}

		// Ensure run folder exists
		if err := hcpo.createRunFolderStructure(ctx, fullRunFolderPath); err != nil {
			hcpo.GetLogger().Errorf("❌ Failed to create run folder for group %s: %v", group.GroupID, err)
			result.FailedGroups++
			result.FailedGroupIDs = append(result.FailedGroupIDs, group.GroupID)
			continue
		}

		// Use ExecutionManager to prepare and apply cleanup for this group
		groupSetup, err := execManager.PrepareForBatchGroup(
			ctx,
			group.GroupID,
			runFolder,
			len(breakdownSteps),
			group.Values,
			isNewFolder,
			execCtx, // Inherit base execution context settings
		)
		if err != nil {
			hcpo.GetLogger().Errorf("❌ Failed to prepare execution for group %s: %v", group.GroupID, err)
			result.FailedGroups++
			result.FailedGroupIDs = append(result.FailedGroupIDs, group.GroupID)
			continue
		}

		// Apply cleanup (deletes old artifacts, initializes fresh progress)
		if err := execManager.ApplyCleanup(ctx, groupSetup); err != nil {
			hcpo.GetLogger().Errorf("❌ Failed to apply cleanup for group %s: %v", group.GroupID, err)
			result.FailedGroups++
			result.FailedGroupIDs = append(result.FailedGroupIDs, group.GroupID)
			continue
		}

		// Apply execution context (sets orchestrator state)
		execManager.ApplyExecutionContext(groupSetup)

		// Emit batch group start event
		hcpo.emitBatchGroupStartEvent(ctx, group.GroupID, groupIndex, totalGroups, group.Values, runFolder, iteration)

		groupStartTime := time.Now()

		// Load the freshly initialized progress (created by ApplyCleanup)
		progress, err := hcpo.loadStepProgress(ctx)
		if err != nil {
			// If loading fails, create in-memory progress
			hcpo.GetLogger().Warnf("⚠️ Failed to load progress for group %s, using in-memory: %v", group.GroupID, err)
			progress = &StepProgress{
				CompletedStepIndices: make([]int, 0),
				TotalSteps:           len(breakdownSteps),
				LastUpdated:          time.Now(),
			}
		}

		// Run execution phase for this group
		_, err = hcpo.runExecutionPhase(ctx, breakdownSteps, iteration, progress, groupSetup.StartFromStep, groupSetup.Context)

		groupDuration := time.Since(groupStartTime)
		remainingGroups := totalGroups - groupIndex - 1

		if err != nil {
			hcpo.GetLogger().Errorf("❌ Batch execution: group %s failed: %v", group.GroupID, err)
			result.FailedGroups++
			result.FailedGroupIDs = append(result.FailedGroupIDs, group.GroupID)
			hcpo.emitBatchGroupEndEvent(ctx, group.GroupID, groupIndex, totalGroups, false, err.Error(), groupDuration, len(progress.CompletedStepIndices), len(breakdownSteps), runFolder, remainingGroups)

			// Check if we should stop on first failure
			// For now, continue with other groups
			continue
		}

		hcpo.GetLogger().Infof("✅ Batch execution: group %s completed successfully", group.GroupID)
		result.CompletedGroups++
		result.CompletedGroupIDs = append(result.CompletedGroupIDs, group.GroupID)
		hcpo.emitBatchGroupEndEvent(ctx, group.GroupID, groupIndex, totalGroups, true, "", groupDuration, len(progress.CompletedStepIndices), len(breakdownSteps), runFolder, remainingGroups)
	}

	result.Duration = time.Since(startTime)
	result.Success = result.FailedGroups == 0 && result.CanceledGroups == 0

	// Emit batch execution end event
	hcpo.emitBatchExecutionEndEvent(ctx, result, iteration)

	if result.Success {
		hcpo.GetLogger().Infof("✅ Batch execution completed: %d/%d groups succeeded in %v", result.CompletedGroups, totalGroups, result.Duration)
	} else {
		hcpo.GetLogger().Warnf("⚠️ Batch execution completed with issues: %d completed, %d failed, %d canceled", result.CompletedGroups, result.FailedGroups, result.CanceledGroups)
	}

	return result, nil
}

// determineBaseIterationFolder determines the base iteration folder based on run mode
func (hcpo *HumanControlledTodoPlannerOrchestrator) determineBaseIterationFolder(ctx context.Context) string {
	var baseIterationFolder string
	var baseIterationNum int

	if hcpo.selectedRunFolder != "" {
		// User selected a specific folder - use it
		baseIterationFolder = hcpo.selectedRunFolder
		// Extract iteration number from folder name
		if strings.Contains(baseIterationFolder, "/") {
			// Nested folder: extract iteration-X from "iteration-X/group-Y"
			if _, err := fmt.Sscanf(baseIterationFolder, "iteration-%d/", &baseIterationNum); err != nil {
				re := regexp.MustCompile(`iteration-(\d+)`)
				matches := re.FindStringSubmatch(baseIterationFolder)
				if len(matches) > 1 {
					fmt.Sscanf(matches[1], "%d", &baseIterationNum)
				} else {
					baseIterationNum = hcpo.getNextIterationNumber(ctx)
				}
			}
			// Use parent folder (iteration-X) as base
			parts := strings.Split(baseIterationFolder, "/")
			baseIterationFolder = parts[0]
		} else {
			// Top-level folder: extract iteration number
			if _, err := fmt.Sscanf(baseIterationFolder, "iteration-%d", &baseIterationNum); err != nil {
				baseIterationNum = hcpo.getNextIterationNumber(ctx)
			}
		}
		hcpo.GetLogger().Infof("📁 Using selected run folder: %s (iteration %d)", baseIterationFolder, baseIterationNum)
	} else if hcpo.selectedRunMode == "create_new_runs_always" {
		// Create new iteration folder
		baseIterationNum = hcpo.getNextIterationNumber(ctx)
		baseIterationFolder = fmt.Sprintf("iteration-%d", baseIterationNum)
		hcpo.GetLogger().Infof("📁 Creating new iteration folder: %s", baseIterationFolder)
	} else {
		// use_same_run mode - use latest existing iteration or create new one
		runsPath := fmt.Sprintf("%s/runs", hcpo.GetWorkspacePath())
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err == nil && len(existingFolders) > 0 {
			maxIteration := hcpo.findMaxIterationNumber(existingFolders)
			if maxIteration > 0 {
				baseIterationNum = maxIteration
				baseIterationFolder = fmt.Sprintf("iteration-%d", baseIterationNum)
				hcpo.GetLogger().Infof("📁 Using existing iteration folder: %s", baseIterationFolder)
			} else {
				baseIterationNum = 1
				baseIterationFolder = fmt.Sprintf("iteration-%d", baseIterationNum)
				hcpo.GetLogger().Infof("📁 No existing iteration folders found, creating: %s", baseIterationFolder)
			}
		} else {
			baseIterationNum = 1
			baseIterationFolder = fmt.Sprintf("iteration-%d", baseIterationNum)
			hcpo.GetLogger().Infof("📁 No existing folders found, creating: %s", baseIterationFolder)
		}
	}

	return baseIterationFolder
}

// findMaxIterationNumber finds the highest iteration number from folder list
func (hcpo *HumanControlledTodoPlannerOrchestrator) findMaxIterationNumber(folders []string) int {
	maxIteration := 0
	for _, folder := range folders {
		var iterNum int
		if _, err := fmt.Sscanf(folder, "iteration-%d", &iterNum); err == nil {
			if iterNum > maxIteration {
				maxIteration = iterNum
			}
		} else {
			// Try nested format: iteration-X/group-Y
			re := regexp.MustCompile(`iteration-(\d+)/`)
			matches := re.FindStringSubmatch(folder)
			if len(matches) > 1 {
				if _, err := fmt.Sscanf(matches[1], "%d", &iterNum); err == nil {
					if iterNum > maxIteration {
						maxIteration = iterNum
					}
				}
			}
		}
	}
	return maxIteration
}

// createGroupRunFolder creates the run folder path for a specific group
func (hcpo *HumanControlledTodoPlannerOrchestrator) createGroupRunFolder(baseIterationFolder, groupID string, totalGroups int) string {
	if totalGroups > 1 {
		// Multiple groups - use nested structure
		return fmt.Sprintf("%s/%s", baseIterationFolder, groupID)
	}
	// Single group - use base folder directly
	return baseIterationFolder
}

// createBatchRunFolderName creates the run folder name for batch execution
// Format: iteration-X/group-Y (nested folder structure for multiple groups)
// Format: iteration-X (when single group - use top-level folder)
func (hcpo *HumanControlledTodoPlannerOrchestrator) createBatchRunFolderName(iterationNum int, groupID string) string {
	// Check if this is actually batch execution (multiple groups)
	enabledGroups := hcpo.getEnabledGroupsForExecution()
	if len(enabledGroups) <= 1 {
		// Single group or no groups - use top-level iteration folder (not nested)
		return fmt.Sprintf("iteration-%d", iterationNum)
	}
	// Multiple groups - use nested folder structure
	return fmt.Sprintf("iteration-%d/%s", iterationNum, groupID)
}

// getNextIterationNumber determines the next iteration number for batch execution
func (hcpo *HumanControlledTodoPlannerOrchestrator) getNextIterationNumber(ctx context.Context) int {
	runsPath := fmt.Sprintf("%s/runs", hcpo.GetWorkspacePath())

	// List existing run folders
	existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
	if err != nil || len(existingFolders) == 0 {
		return 1
	}

	// Find the highest iteration number
	// Support both old format (iteration-X-group-Y) and new format (iteration-X/group-Y)
	maxIteration := 0
	for _, folder := range existingFolders {
		var iterNum int
		// Try to parse iteration-X (top-level folder)
		if _, err := fmt.Sscanf(folder, "iteration-%d", &iterNum); err == nil {
			if iterNum > maxIteration {
				maxIteration = iterNum
			}
		} else {
			// Try old format: iteration-X-group-Y (backward compatibility)
			if _, err := fmt.Sscanf(folder, "iteration-%d-", &iterNum); err == nil {
				if iterNum > maxIteration {
					maxIteration = iterNum
				}
			}
		}
	}

	return maxIteration + 1
}

// Event emission helpers for batch execution

func (hcpo *HumanControlledTodoPlannerOrchestrator) emitBatchExecutionStartEvent(ctx context.Context, totalGroups int, enabledGroupIDs []string, iteration int) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchExecutionStartEvent(totalGroups, enabledGroupIDs, iteration, hcpo.GetWorkspacePath())
	bridge.HandleEvent(ctx, &events.AgentEvent{
		Type:      events.BatchExecutionStart,
		Timestamp: time.Now(),
		Data:      event,
	})
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) emitBatchGroupStartEvent(ctx context.Context, groupID string, groupIndex, totalGroups int, variableValues map[string]string, runFolder string, iteration int) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchGroupStartEvent(groupID, groupIndex, totalGroups, variableValues, runFolder, iteration, hcpo.GetWorkspacePath())
	bridge.HandleEvent(ctx, &events.AgentEvent{
		Type:      events.BatchGroupStart,
		Timestamp: time.Now(),
		Data:      event,
	})
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) emitBatchGroupEndEvent(ctx context.Context, groupID string, groupIndex, totalGroups int, success bool, errorMsg string, duration time.Duration, completedSteps, totalSteps int, runFolder string, remainingGroups int) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchGroupEndEvent(groupID, groupIndex, totalGroups, success, errorMsg, duration, completedSteps, totalSteps, runFolder, remainingGroups)
	bridge.HandleEvent(ctx, &events.AgentEvent{
		Type:      events.BatchGroupEnd,
		Timestamp: time.Now(),
		Data:      event,
	})
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) emitBatchExecutionEndEvent(ctx context.Context, result *BatchExecutionResult, iteration int) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchExecutionEndEvent(
		result.TotalGroups,
		result.CompletedGroups,
		result.FailedGroups,
		result.CanceledGroups,
		result.Duration,
		result.Success,
		result.Error,
		iteration,
		result.CompletedGroupIDs,
		result.FailedGroupIDs,
	)
	bridge.HandleEvent(ctx, &events.AgentEvent{
		Type:      events.BatchExecutionEnd,
		Timestamp: time.Now(),
		Data:      event,
	})
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) emitBatchExecutionCanceledEvent(ctx context.Context, totalGroups, completedGroups int, canceledGroupID string, remainingGroupIDs []string, reason string) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchExecutionCanceledEvent(totalGroups, completedGroups, canceledGroupID, remainingGroupIDs, reason)
	bridge.HandleEvent(ctx, &events.AgentEvent{
		Type:      events.BatchExecutionCanceled,
		Timestamp: time.Now(),
		Data:      event,
	})
}
