package step_based_workflow

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	baseevents "github.com/manishiitg/mcpagent/events"
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

// getEnabledGroupsForExecution returns the list of groups to execute.
// Group selection is mandatory. Priority: ExecutionOptions.EnabledGroupIDs > manifest enabled groups.
func (hcpo *StepBasedWorkflowOrchestrator) getEnabledGroupsForExecution() []VariableGroup {
	if hcpo.variablesManifest == nil {
		hcpo.GetLogger().Error("❌ [GROUP SELECTION] variablesManifest is nil - group configuration is required", nil)
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
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [GROUP SELECTION] No groups found matching requested IDs"))
		}

		// If we found at least some groups, return them (even if some were missing)
		if len(groups) > 0 {
			return groups
		}
	}

	// Fall back to manifest's enabled groups only when the manifest defines real groups.
	if !hcpo.variablesManifest.HasGroups() {
		hcpo.GetLogger().Error("❌ [GROUP SELECTION] No groups defined in variables manifest - group configuration is required", nil)
		return nil
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [GROUP SELECTION] No execution options or no matches found, using manifest's enabled groups"))
	enabledGroups := hcpo.variablesManifest.GetEnabledGroups()
	enabledGroupIDs := make([]string, len(enabledGroups))
	for i, g := range enabledGroups {
		enabledGroupIDs[i] = g.GroupID
	}
	hcpo.GetLogger().Info(fmt.Sprintf("✅ [GROUP SELECTION] Returning %d enabled groups from manifest: %v", len(enabledGroups), enabledGroupIDs))
	return enabledGroups
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
		return nil, fmt.Errorf("no enabled variable groups found for batch execution")
	}

	// Validate that all groups have valid GroupIDs
	for i, group := range enabledGroups {
		if group.GroupID == "" {
			// PANIC for debugging: groupID is required for session ID and folder structure
			panic(fmt.Sprintf("CRITICAL: Group at index %d has empty GroupID in runBatchExecution() - all groups must have valid GroupIDs. Group values: %v", i, group.Values))
		}
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

		// CRITICAL FIX: Close entire previous session before starting new group
		// This ensures the new session ID gets fresh connections with the correct Downloads path
		// We must close the entire session (not just playwright) to ensure all connections are released
		if groupIndex > 0 {
			// Get previous session ID BEFORE we set the new one
			previousSessionID := hcpo.getSessionID()
			if previousSessionID != "" {
				hcpo.GetLogger().Info(fmt.Sprintf("🔗 Closing entire previous session before starting group %s (session: %s)", group.GroupID, previousSessionID))
				mcpagent.CloseSession(previousSessionID)
			}
		}

		// Apply execution context (sets orchestrator state, including selectedRunFolder)
		// This MUST be done before setting session ID so that Downloads path override uses correct run folder
		execManager.ApplyExecutionContext(groupSetup)
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] After ApplyExecutionContext - selectedRunFolder: '%s', runFolder: '%s'", hcpo.selectedRunFolder, runFolder))

		// CRITICAL FIX: Generate a unique session ID for each workflow group
		// This ensures each group gets its own MCP connections (e.g., Playwright browser)
		// with the correct Downloads path. Without this, all groups share the same session ID
		// and reuse connections created with the first group's Downloads path.
		//
		// IMPORTANT: Agents within the SAME group share connections (this is correct behavior).
		// The session ID change ensures each GROUP gets its own isolated connections.
		groupSessionID := fmt.Sprintf("session-group-%s-%d", group.GroupID, time.Now().UnixNano())
		hcpo.sessionID = groupSessionID
		hcpo.BaseOrchestrator.SetMCPSessionID(groupSessionID)
		hcpo.GetLogger().Info(fmt.Sprintf("🔗 Generated unique MCP session ID for group %s: %s (run folder: %s)", group.GroupID, groupSessionID, hcpo.selectedRunFolder))
		// Track group session under HTTP session so stop handler can close it immediately
		if hcpo.httpSessionID != "" {
			mcpagent.RegisterHTTPSession(hcpo.httpSessionID, groupSessionID)
		}

		// Close MCP session after this group completes to free resources (browser profiles, etc.)
		// Use defer to ensure cleanup even if execution fails
		defer func() {
			hcpo.GetLogger().Info(fmt.Sprintf("🔗 Closing MCP session for group %s: %s", group.GroupID, groupSessionID))
			mcpagent.CloseSession(groupSessionID)
		}()

		// Update batch context for step_progress_updated events
		hcpo.currentGroupId = group.GroupID
		hcpo.currentGroupIdx = groupIndex
		hcpo.totalGroups = totalGroups

		// Also set batch context on context-aware bridge so ALL events get batch info in metadata
		if bridge := hcpo.GetContextAwareBridge(); bridge != nil {
			if batchBridge, ok := bridge.(interface {
				SetBatchContext(groupID string, groupIndex int, totalGroups int)
			}); ok {
				batchBridge.SetBatchContext(group.GroupID, groupIndex, totalGroups)
			}
		}

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
			if result.Error == "" {
				result.Error = err.Error() // Capture first failure reason
			}
			hcpo.emitBatchGroupEndEvent(ctx, group.GroupID, groupIndex, totalGroups, false, err.Error(), groupDuration, len(progress.CompletedStepIndices), len(breakdownSteps), runFolder, remainingGroups)

			// Check if we should stop on first failure
			// For now, continue with other groups
			continue
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Batch execution: group %s completed successfully", group.GroupID))
		result.CompletedGroups++
		result.CompletedGroupIDs = append(result.CompletedGroupIDs, group.GroupID)
		hcpo.emitBatchGroupEndEvent(ctx, group.GroupID, groupIndex, totalGroups, true, "", groupDuration, len(progress.CompletedStepIndices), len(breakdownSteps), runFolder, remainingGroups)

		// Auto-evaluation: Run scoring for this group if evaluation_plan.json exists
		if !hcpo.isEvaluationMode {
			if evalErr := hcpo.MaybeRunAutoEvaluation(ctx); evalErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Auto-evaluation failed for group %s: %v", group.GroupID, evalErr))
				// Don't fail the group if auto-evaluation fails
			}
		}

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

	// Clear batch context on context-aware bridge (cleanup after batch ends)
	if bridge := hcpo.GetContextAwareBridge(); bridge != nil {
		if batchBridge, ok := bridge.(interface {
			ClearBatchContext()
		}); ok {
			batchBridge.ClearBatchContext()
		}
	}

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
// ALWAYS uses nested structure (iteration-X/group) regardless of number of groups
func (hcpo *StepBasedWorkflowOrchestrator) createGroupRunFolder(baseIterationFolder, groupID, displayName string, totalGroups int) string {
	// ALWAYS use nested structure - try to use sanitized display_name, fall back to group_id
	folderName := groupID
	if displayName != "" {
		sanitized := hcpo.sanitizeDisplayNameForFolder(displayName)
		if sanitized != "" {
			folderName = sanitized
		}
	}
	return fmt.Sprintf("%s/%s", baseIterationFolder, folderName)
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

	// Convert execution options to map for event
	var executionOptionsMap map[string]interface{}
	if hcpo.executionOptions != nil {
		executionOptionsMap = hcpo.executionOptionsToMap()
	}

	event := events.NewBatchExecutionStartEvent(totalGroups, enabledGroupIDs, iteration, hcpo.GetWorkspacePath(), executionOptionsMap)
	bridge.HandleEvent(ctx, &baseevents.AgentEvent{
		Type:      events.BatchExecutionStart,
		Timestamp: time.Now(),
		Data:      event,
	})
}

// executionOptionsToMap converts ExecutionOptions to a map for event serialization
func (hcpo *StepBasedWorkflowOrchestrator) executionOptionsToMap() map[string]interface{} {
	if hcpo.executionOptions == nil {
		return nil
	}

	opts := hcpo.executionOptions
	result := make(map[string]interface{})

	if opts.RunMode != "" {
		result["run_mode"] = opts.RunMode
	}
	if opts.SelectedRunFolder != "" {
		result["selected_run_folder"] = opts.SelectedRunFolder
	}
	if opts.ExecutionStrategy != "" {
		result["execution_strategy"] = opts.ExecutionStrategy
	}
	if opts.ResumeFromStep > 0 {
		result["resume_from_step"] = opts.ResumeFromStep
	}
	if opts.ResumeFromBranchStep != nil {
		result["resume_from_branch_step"] = map[string]interface{}{
			"parent_step_index": opts.ResumeFromBranchStep.ParentStepIndex,
			"branch_type":       opts.ResumeFromBranchStep.BranchType,
			"branch_step_index": opts.ResumeFromBranchStep.BranchStepIndex,
		}
	}
	if opts.FastExecuteEndStep > 0 {
		result["fast_execute_end_step"] = opts.FastExecuteEndStep
	}
	if opts.PlanChangeAction != "" {
		result["plan_change_action"] = opts.PlanChangeAction
	}
	if opts.AllStepsCompletedAction != "" {
		result["all_steps_completed_action"] = opts.AllStepsCompletedAction
	}
	if opts.TempOverrideLLM != nil {
		result["temp_override_llm"] = map[string]interface{}{
			"provider": opts.TempOverrideLLM.Provider,
			"model_id": opts.TempOverrideLLM.ModelID,
		}
	}
	if opts.TempOverrideLLM2 != nil {
		result["temp_override_llm2"] = map[string]interface{}{
			"provider": opts.TempOverrideLLM2.Provider,
			"model_id": opts.TempOverrideLLM2.ModelID,
		}
	}
	if opts.FallbackToOriginalLLMOnFailure {
		result["fallback_to_original_llm_on_failure"] = opts.FallbackToOriginalLLMOnFailure
	}

	return result
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
