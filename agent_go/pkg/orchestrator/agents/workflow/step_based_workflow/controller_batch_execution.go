package step_based_workflow

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	baseevents "mcpagent/events"
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
func (hcpo *StepBasedWorkflowOrchestrator) getEnabledGroupsForExecution() []VariableGroup {
	if hcpo.variablesManifest == nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [GROUP SELECTION] variablesManifest is nil - cannot determine enabled groups"))
		return nil
	}

	// Log available groups in manifest for debugging
	availableGroupIDs := make([]string, len(hcpo.variablesManifest.Groups))
	for i, g := range hcpo.variablesManifest.Groups {
		availableGroupIDs[i] = g.GroupID
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [GROUP SELECTION] Available groups in manifest: %v", availableGroupIDs))

	// Check if ExecutionOptions specifies specific group IDs
	if hcpo.executionOptions != nil && len(hcpo.executionOptions.EnabledGroupIDs) > 0 {
		// Use specified group IDs from ExecutionOptions
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [GROUP SELECTION] Requested group IDs from execution options: %v", hcpo.executionOptions.EnabledGroupIDs))
		var groups []VariableGroup
		var foundGroupIDs []string
		var missingGroupIDs []string

		for _, groupID := range hcpo.executionOptions.EnabledGroupIDs {
			found := false
			for _, g := range hcpo.variablesManifest.Groups {
				if g.GroupID == groupID {
					groups = append(groups, g)
					foundGroupIDs = append(foundGroupIDs, groupID)
					hcpo.GetLogger().Info(fmt.Sprintf("✅ [GROUP SELECTION] Found group %s in manifest (enabled: %v)", groupID, g.Enabled))
					found = true
					break
				}
			}
			if !found {
				missingGroupIDs = append(missingGroupIDs, groupID)
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [GROUP SELECTION] Requested group %s not found in manifest", groupID))
			}
		}

		if len(missingGroupIDs) > 0 {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ [GROUP SELECTION] Some requested groups not found: %v (found: %v)", missingGroupIDs, foundGroupIDs), nil)
		}

		if len(groups) > 0 {
			returnedGroupIDs := make([]string, len(groups))
			for i, g := range groups {
				returnedGroupIDs[i] = g.GroupID
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ [GROUP SELECTION] Returning %d groups: %v", len(groups), returnedGroupIDs))
		} else {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [GROUP SELECTION] No groups found matching requested IDs, falling back to manifest enabled groups"))
		}

		// If we found at least some groups, return them (even if some were missing)
		if len(groups) > 0 {
			return groups
		}
	}

	// Fall back to manifest's enabled groups
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [GROUP SELECTION] No execution options or no matches found, using manifest's enabled groups"))
	enabledGroups := hcpo.variablesManifest.GetEnabledGroups()
	enabledGroupIDs := make([]string, len(enabledGroups))
	for i, g := range enabledGroups {
		enabledGroupIDs[i] = g.GroupID
	}
	hcpo.GetLogger().Info(fmt.Sprintf("✅ [GROUP SELECTION] Returning %d enabled groups from manifest: %v", len(enabledGroups), enabledGroupIDs))
	return enabledGroups
}

// shouldUseBatchExecution determines if batch execution mode should be used
func (hcpo *StepBasedWorkflowOrchestrator) shouldUseBatchExecution() bool {
	enabledGroups := hcpo.getEnabledGroupsForExecution()
	return len(enabledGroups) > 1
}

// runBatchExecution executes the workflow for multiple variable groups sequentially
// Uses ExecutionManager for centralized cleanup and progress management
func (hcpo *StepBasedWorkflowOrchestrator) runBatchExecution(
	ctx context.Context,
	breakdownSteps []PlanStepInterface,
	iteration int,
	execCtx *ExecutionContext,
) (*BatchExecutionResult, error) {
	enabledGroups := hcpo.getEnabledGroupsForExecution()
	totalGroups := len(enabledGroups)

	if totalGroups == 0 {
		return nil, fmt.Errorf(fmt.Sprintf("no enabled variable groups found for batch execution"), nil)
	}

	// Validate that returned groups match requested groups (if specified)
	if hcpo.executionOptions != nil && len(hcpo.executionOptions.EnabledGroupIDs) > 0 {
		returnedGroupIDs := make([]string, len(enabledGroups))
		for i, g := range enabledGroups {
			returnedGroupIDs[i] = g.GroupID
		}
		requestedGroupIDs := hcpo.executionOptions.EnabledGroupIDs

		// Check if all requested groups are present
		requestedSet := make(map[string]bool)
		for _, id := range requestedGroupIDs {
			requestedSet[id] = false
		}
		for _, id := range returnedGroupIDs {
			if _, exists := requestedSet[id]; exists {
				requestedSet[id] = true
			}
		}

		missing := make([]string, 0)
		for id, found := range requestedSet {
			if !found {
				missing = append(missing, id)
			}
		}

		if len(missing) > 0 {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ [BATCH EXECUTION] Requested groups not found in execution: %v (returned: %v)", missing, returnedGroupIDs), nil)
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ [BATCH EXECUTION] All requested groups found: %v", returnedGroupIDs))
		}

		// Check for unexpected groups
		returnedSet := make(map[string]bool)
		for _, id := range returnedGroupIDs {
			returnedSet[id] = true
		}
		unexpected := make([]string, 0)
		for _, id := range returnedGroupIDs {
			found := false
			for _, reqID := range requestedGroupIDs {
				if id == reqID {
					found = true
					break
				}
			}
			if !found {
				unexpected = append(unexpected, id)
			}
		}

		if len(unexpected) > 0 {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [BATCH EXECUTION] Unexpected groups in execution (not requested): %v", unexpected))
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔄 Starting batch execution for %d variable groups", totalGroups))

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
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Batch execution canceled during group %s", group.GroupID))
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

		hcpo.GetLogger().Info(fmt.Sprintf("📦 Batch execution: processing group %d/%d (%s)", groupIndex+1, totalGroups, group.GroupID))

		// Log group values being used for this execution
		if len(group.Values) > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [GROUP EXECUTION] Using variable values for group %s: %v", group.GroupID, group.Values))
		} else {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [GROUP EXECUTION] Group %s has no variable values!", group.GroupID))
		}

		// Determine run folder for this group
		// Use display_name if available (sanitized), otherwise fall back to group_id
		// Special case: if single group and selectedRunFolder already contains a group path, use it directly
		var runFolder string
		if totalGroups == 1 && hcpo.selectedRunFolder != "" && strings.Contains(hcpo.selectedRunFolder, "/") {
			// User explicitly selected a group folder (e.g., "iteration-13/siddharth")
			// Use it directly instead of recreating the path
			runFolder = hcpo.selectedRunFolder
		} else {
			// Multiple groups or no explicit group selection - create folder path
			runFolder = hcpo.createGroupRunFolder(baseIterationFolder, group.GroupID, group.DisplayName, totalGroups)
		}

		// Check if folder exists (to determine if we need cleanup)
		fullRunFolderPath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), runFolder)
		isNewFolder := true
		if exists := hcpo.workspaceFileExists(ctx, fullRunFolderPath); exists {
			isNewFolder = false
		}

		// Ensure run folder exists
		if err := hcpo.createRunFolderStructure(ctx, fullRunFolderPath); err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to create run folder for group %s: %v", group.GroupID, err), nil)
			result.FailedGroups++
			result.FailedGroupIDs = append(result.FailedGroupIDs, group.GroupID)
			continue
		}

		// Use ExecutionManager to prepare and apply cleanup for this group
		// Pass isFirstGroup=true only for the first group (groupIndex == 0)
		// This ensures resume step only applies to first group, subsequent groups start from beginning
		isFirstGroup := groupIndex == 0
		groupSetup, err := execManager.PrepareForBatchGroup(
			ctx,
			group.GroupID,
			runFolder,
			len(breakdownSteps),
			group.Values,
			isNewFolder,
			execCtx,      // Inherit base execution context settings
			isFirstGroup, // Only first group can use resume step
		)
		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to prepare execution for group %s: %v", group.GroupID, err), nil)
			result.FailedGroups++
			result.FailedGroupIDs = append(result.FailedGroupIDs, group.GroupID)
			continue
		}

		// Apply cleanup (deletes old artifacts, initializes fresh progress)
		if err := execManager.ApplyCleanup(ctx, groupSetup); err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to apply cleanup for group %s: %v", group.GroupID, err), nil)
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
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load progress for group %s, using in-memory: %v", group.GroupID, err))
			progress = &StepProgress{
				CompletedStepIndices:     make([]int, 0),
				TotalSteps:               len(breakdownSteps),
				LastUpdated:              time.Now(),
				DecisionEvaluationCounts: make(DecisionEvaluationCount),
			}
		}

		// Run execution phase for this group
		err = hcpo.runExecutionPhase(ctx, breakdownSteps, iteration, progress, groupSetup.StartFromStep, groupSetup.Context)

		groupDuration := time.Since(groupStartTime)
		remainingGroups := totalGroups - groupIndex - 1

		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Batch execution: group %s failed: %v", group.GroupID, err), nil)
			result.FailedGroups++
			result.FailedGroupIDs = append(result.FailedGroupIDs, group.GroupID)
			hcpo.emitBatchGroupEndEvent(ctx, group.GroupID, groupIndex, totalGroups, false, err.Error(), groupDuration, len(progress.CompletedStepIndices), len(breakdownSteps), runFolder, remainingGroups)

			// Check if we should stop on first failure
			// For now, continue with other groups
			continue
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Batch execution: group %s completed successfully", group.GroupID))
		result.CompletedGroups++
		result.CompletedGroupIDs = append(result.CompletedGroupIDs, group.GroupID)
		hcpo.emitBatchGroupEndEvent(ctx, group.GroupID, groupIndex, totalGroups, true, "", groupDuration, len(progress.CompletedStepIndices), len(breakdownSteps), runFolder, remainingGroups)

		// If single step mode was active, stop batch execution after this group
		// Single step mode should only run one group, not continue to additional groups
		if groupSetup.Context.RunSingleStepOnly {
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single step mode was active - stopping batch execution after group %s (skipping remaining %d group(s))", group.GroupID, remainingGroups))
			break
		}
	}

	result.Duration = time.Since(startTime)
	result.Success = result.FailedGroups == 0 && result.CanceledGroups == 0

	// Emit batch execution end event
	hcpo.emitBatchExecutionEndEvent(ctx, result, iteration)

	if result.Success {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Batch execution completed: %d/%d groups succeeded in %v", result.CompletedGroups, totalGroups, result.Duration))
	} else {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Batch execution completed with issues: %d completed, %d failed, %d canceled", result.CompletedGroups, result.FailedGroups, result.CanceledGroups))
	}

	return result, nil
}

// determineBaseIterationFolder determines the base iteration folder based on run mode
func (hcpo *StepBasedWorkflowOrchestrator) determineBaseIterationFolder(ctx context.Context) string {
	var baseIterationFolder string
	var baseIterationNum int

	if hcpo.selectedRunFolder != "" {
		// User selected a specific folder - use it
		baseIterationFolder = hcpo.selectedRunFolder
		// Extract iteration number from folder name
		if strings.Contains(baseIterationFolder, "/") {
			// Nested folder: extract iteration-X from "iteration-X/group-Y" or "iteration-X/display-name"
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
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Using selected run folder: %s (iteration %d)", baseIterationFolder, baseIterationNum))
	} else if hcpo.selectedRunMode == "create_new_runs_always" {
		// Create new iteration folder
		baseIterationNum = hcpo.getNextIterationNumber(ctx)
		baseIterationFolder = fmt.Sprintf("iteration-%d", baseIterationNum)
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Creating new iteration folder: %s", baseIterationFolder))
	} else {
		// use_same_run mode - use latest existing iteration or create new one
		runsPath := fmt.Sprintf("%s/runs", hcpo.GetWorkspacePath())
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err == nil && len(existingFolders) > 0 {
			maxIteration := hcpo.findMaxIterationNumber(existingFolders)
			if maxIteration > 0 {
				baseIterationNum = maxIteration
				baseIterationFolder = fmt.Sprintf("iteration-%d", baseIterationNum)
				hcpo.GetLogger().Info(fmt.Sprintf("📁 Using existing iteration folder: %s", baseIterationFolder))
			} else {
				baseIterationNum = 1
				baseIterationFolder = fmt.Sprintf("iteration-%d", baseIterationNum)
				hcpo.GetLogger().Info(fmt.Sprintf("📁 No existing iteration folders found, creating: %s", baseIterationFolder))
			}
		} else {
			baseIterationNum = 1
			baseIterationFolder = fmt.Sprintf("iteration-%d", baseIterationNum)
			hcpo.GetLogger().Info(fmt.Sprintf("📁 No existing folders found, creating: %s", baseIterationFolder))
		}
	}

	return baseIterationFolder
}

// findMaxIterationNumber finds the highest iteration number from folder list
func (hcpo *StepBasedWorkflowOrchestrator) findMaxIterationNumber(folders []string) int {
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

// sanitizeDisplayNameForFolder sanitizes a display name for use in folder paths
// - Removes/replaces special characters that aren't safe for filesystem
// - Normalizes whitespace and converts to lowercase
// - Removes multiple consecutive dashes
// - Falls back to empty string if result is invalid
func (hcpo *StepBasedWorkflowOrchestrator) sanitizeDisplayNameForFolder(displayName string) string {
	if displayName == "" {
		return ""
	}

	sanitized := strings.TrimSpace(displayName)

	// Replace spaces with dashes
	sanitized = strings.ReplaceAll(sanitized, " ", "-")

	// Remove or replace special characters that aren't safe for folder names
	// Keep: letters, numbers, dashes, underscores
	// Remove: colons, slashes, backslashes, pipes, etc.
	specialCharPattern := regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
	sanitized = specialCharPattern.ReplaceAllString(sanitized, "-")

	// Normalize multiple consecutive dashes to single dash
	multiDashPattern := regexp.MustCompile(`-+`)
	sanitized = multiDashPattern.ReplaceAllString(sanitized, "-")

	// Remove leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")

	// Convert to lowercase for consistency
	sanitized = strings.ToLower(sanitized)

	// If result is empty or too short, return empty (will fall back to group_id)
	if sanitized == "" || len(sanitized) < 1 {
		return ""
	}

	return sanitized
}

// createGroupRunFolder creates the run folder path for a specific group
// Uses display_name if available (sanitized), otherwise falls back to group_id
func (hcpo *StepBasedWorkflowOrchestrator) createGroupRunFolder(baseIterationFolder, groupID, displayName string, totalGroups int) string {
	if totalGroups > 1 {
		// Multiple groups - use nested structure
		// Try to use sanitized display_name, fall back to group_id
		folderName := groupID
		if displayName != "" {
			sanitized := hcpo.sanitizeDisplayNameForFolder(displayName)
			if sanitized != "" {
				folderName = sanitized
			}
		}
		return fmt.Sprintf("%s/%s", baseIterationFolder, folderName)
	}
	// Single group - use base folder directly
	return baseIterationFolder
}

// getNextIterationNumber determines the next iteration number for batch execution
func (hcpo *StepBasedWorkflowOrchestrator) getNextIterationNumber(ctx context.Context) int {
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

func (hcpo *StepBasedWorkflowOrchestrator) emitBatchExecutionStartEvent(ctx context.Context, totalGroups int, enabledGroupIDs []string, iteration int) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchExecutionStartEvent(totalGroups, enabledGroupIDs, iteration, hcpo.GetWorkspacePath())
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      events.BatchExecutionStart,
		Timestamp: time.Now(),
		Data:      event,
	})
}

func (hcpo *StepBasedWorkflowOrchestrator) emitBatchGroupStartEvent(ctx context.Context, groupID string, groupIndex, totalGroups int, variableValues map[string]string, runFolder string, iteration int) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchGroupStartEvent(groupID, groupIndex, totalGroups, variableValues, runFolder, iteration, hcpo.GetWorkspacePath())
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      events.BatchGroupStart,
		Timestamp: time.Now(),
		Data:      event,
	})
}

func (hcpo *StepBasedWorkflowOrchestrator) emitBatchGroupEndEvent(ctx context.Context, groupID string, groupIndex, totalGroups int, success bool, errorMsg string, duration time.Duration, completedSteps, totalSteps int, runFolder string, remainingGroups int) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchGroupEndEvent(groupID, groupIndex, totalGroups, success, errorMsg, duration, completedSteps, totalSteps, runFolder, remainingGroups)
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      events.BatchGroupEnd,
		Timestamp: time.Now(),
		Data:      event,
	})
}

func (hcpo *StepBasedWorkflowOrchestrator) emitBatchExecutionEndEvent(ctx context.Context, result *BatchExecutionResult, iteration int) {
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
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      events.BatchExecutionEnd,
		Timestamp: time.Now(),
		Data:      event,
	})
}

func (hcpo *StepBasedWorkflowOrchestrator) emitBatchExecutionCanceledEvent(ctx context.Context, totalGroups, completedGroups int, canceledGroupID string, remainingGroupIDs []string, reason string) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	event := events.NewBatchExecutionCanceledEvent(totalGroups, completedGroups, canceledGroupID, remainingGroupIDs, reason)
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      events.BatchExecutionCanceled,
		Timestamp: time.Now(),
		Data:      event,
	})
}
