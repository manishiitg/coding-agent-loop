package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LoadPlanForWorkshop reads planning/plan.json via the workspace API and caches it as approvedPlan.
// Called at the start of InteractiveWorkshopOnly and again inside ExecuteStepForWorkshop
// so that any plan edits made during the workshop session are always picked up.
// Uses ReadWorkspaceFile (workspace HTTP API) — NOT os.ReadFile — because the workspace
// path is a logical path resolved by the workspace service, not a local filesystem path.
func (hcpo *StepBasedWorkflowOrchestrator) LoadPlanForWorkshop(ctx context.Context) error {
	hcpo.GetLogger().Debug(fmt.Sprintf("[WORKSHOP_DEBUG] LoadPlanForWorkshop: workspacePath=%s, selectedRunFolder=%s, isEvaluationMode=%v",
		hcpo.GetWorkspacePath(), hcpo.selectedRunFolder, hcpo.isEvaluationMode))

	// In evaluation mode, load the evaluation plan instead
	if hcpo.isEvaluationMode {
		planContent, err := hcpo.ReadWorkspaceFile(ctx, "evaluation/evaluation_plan.json")
		if err != nil {
			return fmt.Errorf("cannot read evaluation_plan.json: %w", err)
		}
		var evalPlan EvaluationPlan
		if err := json.Unmarshal([]byte(planContent), &evalPlan); err != nil {
			return fmt.Errorf("cannot parse evaluation_plan.json: %w", err)
		}
		// Convert to PlanningResponse so workshop tools work uniformly
		hcpo.approvedPlan = &PlanningResponse{
			Steps: evalPlan.ToPlanSteps(),
		}
		hcpo.GetLogger().Debug(fmt.Sprintf("[WORKSHOP_DEBUG] LoadPlanForWorkshop: loaded evaluation plan with %d steps", len(evalPlan.Steps)))
		return nil
	}

	// ReadWorkspaceFile auto-prepends workspacePath to relative paths
	planContent, err := hcpo.ReadWorkspaceFile(ctx, "planning/plan.json")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP_DEBUG] LoadPlanForWorkshop: ReadWorkspaceFile failed: %v", err))
		return fmt.Errorf("cannot read plan.json: %w", err)
	}

	hcpo.GetLogger().Debug(fmt.Sprintf("[WORKSHOP_DEBUG] LoadPlanForWorkshop: read %d bytes from planning/plan.json", len(planContent)))

	var plan PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &plan); err != nil {
		return fmt.Errorf("cannot parse plan.json: %w", err)
	}
	hcpo.approvedPlan = &plan
	hcpo.GetLogger().Debug(fmt.Sprintf("[WORKSHOP_DEBUG] LoadPlanForWorkshop: loaded plan with %d steps", len(plan.Steps)))
	return nil
}

// WorkshopExecuteOptions holds per-call overrides for ExecuteStepForWorkshop.
// When GroupID is set, the controller resolves the run folder and variable values
// for that group, making each execute_step call self-contained.
type WorkshopExecuteOptions struct {
	GroupID      string // e.g., "group-1" — overrides session-level group
	Iteration    string // e.g., "iteration-3" — combined with group folder name to form RunFolder
	RunFolder    string // e.g., "iteration-3/xtech" — auto-calculated from Iteration + group, or set directly
	SkipLearning bool   // If true, skip the learning phase for this execution only (doesn't modify step_config)
	Instructions string // Optional orchestrator instructions for inner steps — appended to step description as "## Orchestrator Instructions"
	HumanInput   string // Optional human input for top-level steps — injected as critical feedback in PreviousStepsSummary
	Tier         int    // Optional LLM tier override (1=high, 2=medium, 3=low). 0 means no override.
}

// ExecuteStepForWorkshop executes a single step by its ID for the interactive workshop phase.
// It reuses the standard execution pipeline (PrepareExecution → ApplyCleanup → runExecutionPhase)
// so that step execution behaves identically to the normal "run single step" UI action.
//
// If opts is non-nil and opts.GroupID is set, the controller resolves the run folder and
// variable values for that group before execution. This allows each execute_step call to
// target a different group without changing session state.
func (hcpo *StepBasedWorkflowOrchestrator) ExecuteStepForWorkshop(
	ctx context.Context,
	stepID string,
	opts *WorkshopExecuteOptions,
) (string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] ExecuteStepForWorkshop: stepID=%s, runFolder=%s, codeExec=%v",
		stepID, hcpo.selectedRunFolder, hcpo.GetUseCodeExecutionMode()))
	// 1. Apply per-call overrides (group_id, run_folder, human_input)
	if opts != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Per-call options: groupID=%s, runFolder=%s, humanInput=%d chars", opts.GroupID, opts.RunFolder, len(opts.HumanInput)))
		if err := hcpo.applyWorkshopExecuteOptions(ctx, opts); err != nil {
			return "", fmt.Errorf("failed to apply execute options: %w", err)
		}
		// Set workshop human input (cleared after execution)
		hcpo.interactiveWorkflowHumanInput = opts.HumanInput
	}
	if hcpo.selectedRunFolder == "" {
		return "", fmt.Errorf("no run folder selected; cannot execute step %q — pass group_id or run_folder in execute_step, or select a group first", stepID)
	}

	// 1b. Ensure variable values are loaded (same as normal workflow's Execute method).
	// If group_id was passed, applyWorkshopExecuteOptions already loaded group values.
	// Otherwise, fall back to LoadVariableValues (reads from variables.json).
	if hcpo.variableValues == nil {
		if hcpo.variablesManifest != nil && len(hcpo.variablesManifest.Groups) == 1 {
			// Single group — use its values automatically
			g := hcpo.variablesManifest.Groups[0]
			hcpo.variableValues = g.Values
			SyncVariablesToWorkspaceEnv(hcpo.BaseOrchestrator, g.Values)
			hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Auto-loaded variable values from single group %q (%d vars)", g.GroupID, len(g.Values)))
		} else {
			vals, loadErr := LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
			if loadErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Failed to load variable values: %v", loadErr))
			} else if vals != nil {
				hcpo.variableValues = vals
				SyncVariablesToWorkspaceEnv(hcpo.BaseOrchestrator, vals)
				hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Loaded %d variable values via fallback", len(vals)))
			}
		}
	}

	// 2. Re-read plan.json + populate runtime fields from step_config.json
	if err := hcpo.LoadPlanForWorkshop(ctx); err != nil {
		return "", fmt.Errorf("failed to load plan: %w", err)
	}
	stepConfigs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Failed to read step_config.json: %v (using defaults)", err))
		stepConfigs = []StepConfig{}
	}
	// Populate runtime fields for ALL steps (top-level + inner)
	allStepInfos := collectAllSteps(hcpo.approvedPlan.Steps)
	for _, info := range allStepInfos {
		if err := populateRuntimeFields(info.Step, stepConfigs); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Failed to populate runtime fields: %v", err))
		}
	}

	// 2b. If skip_learning requested, temporarily override DisableLearning on the target step's config.
	// This avoids modifying step_config.json — it's a runtime-only override for this execution.
	if opts != nil && opts.SkipLearning {
		for i := range stepConfigs {
			if stepConfigs[i].ID == stepID {
				if stepConfigs[i].AgentConfigs == nil {
					stepConfigs[i].AgentConfigs = &AgentConfigs{}
				}
				disableVal := true
				stepConfigs[i].AgentConfigs.DisableLearning = &disableVal
				hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] skip_learning=true: temporarily disabled learning for step %q (runtime only)", stepID))
				// Re-populate runtime fields for this step so the override takes effect
				for _, info := range allStepInfos {
					if info.Step.GetID() == stepID {
						_ = populateRuntimeFields(info.Step, stepConfigs)
						break
					}
				}
				break
			}
		}
	}

	// 3. Resolve step ID → find in top-level or inner steps
	stepInfo := findWorkshopStepByID(hcpo.approvedPlan.Steps, stepID)
	if stepInfo == nil {
		allIDs := make([]string, 0, len(allStepInfos))
		for _, info := range allStepInfos {
			allIDs = append(allIDs, info.Step.GetID())
		}
		hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Step %q not found. Valid IDs: %v", stepID, allIDs))
		return "", fmt.Errorf("step with ID %q not found in plan", stepID)
	}

	isInnerStep := stepInfo.TopIndex < 0
	if isInnerStep {
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Executing INNER step %q (parent=%s, branch=%s, runFolder=%s)",
			stepID, stepInfo.ParentID, stepInfo.BranchName, hcpo.selectedRunFolder))
	}

	// For inner steps, we execute them standalone as a single-step plan.
	// For top-level steps, we use the normal execution pipeline.
	var breakdownSteps []PlanStepInterface
	var targetIndex int
	if isInnerStep {
		step := stepInfo.Step
		// Append orchestrator instructions to inner step description (simulates what the
		// parent todo_task/orchestration agent would provide via call_sub_agent).
		if opts != nil && opts.Instructions != "" {
			step = appendInstructionsToStep(step, opts.Instructions)
			hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Appended orchestrator instructions to inner step %q (%d chars)", stepID, len(opts.Instructions)))
		}
		breakdownSteps = []PlanStepInterface{step}
		targetIndex = 0
	} else {
		breakdownSteps = hcpo.approvedPlan.Steps
		targetIndex = stepInfo.TopIndex - 1 // convert 1-based to 0-based
	}
	totalSteps := len(breakdownSteps)
	hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Executing step %q (index=%d, total=%d, inner=%v, runFolder=%s)",
		stepID, targetIndex, totalSteps, isInnerStep, hcpo.selectedRunFolder))

	// 4. Ensure run folder exists
	// In evaluation mode, redirect outputs to evaluation/runs/ instead of runs/
	if hcpo.isEvaluationMode && !strings.Contains(hcpo.selectedRunFolder, "evaluation") {
		hcpo.selectedRunFolder = fmt.Sprintf("../evaluation/runs/%s", hcpo.selectedRunFolder)
		hcpo.SetIterationFolder(hcpo.selectedRunFolder)
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Evaluation mode: redirected run folder to %s", hcpo.selectedRunFolder))
	}
	fullRunFolderPath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	if err := hcpo.createRunFolderStructure(ctx, fullRunFolderPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Failed to create run folder structure: %v (continuing)", err))
	}

	// 5. Standard execution pipeline — same as normal "run single step" UI action
	execOpts := &ExecutionOptions{
		ExecutionStrategy: ExecutionStrategyRunSingleStep,
		ResumeFromStep:    targetIndex + 1, // 1-based
	}

	execManager := NewExecutionManager(hcpo)

	// Load or initialize progress
	progress, err := hcpo.loadStepProgress(ctx)
	if err != nil {
		if initErr := hcpo.initializeFreshProgress(ctx, totalSteps); initErr != nil {
			return "", fmt.Errorf("failed to initialize progress: %w", initErr)
		}
		progress, err = hcpo.loadStepProgress(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to load progress: %w", err)
		}
	}

	setup, err := execManager.PrepareExecution(ctx, execOpts, progress, totalSteps, hcpo.selectedRunFolder)
	if err != nil {
		return "", fmt.Errorf("failed to prepare execution: %w", err)
	}

	// For inner steps: skip cleanup and set step path override so the inner step
	// writes to its own folder (e.g., "step-3-sub-login-expert") instead of "step-1"
	// which would collide with/delete the top-level step-1's output.
	if isInnerStep {
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP-INNER] Original cleanup scope for inner step %q: CleanAll=%v, CleanFrom=%d, CleanSpecific=%d",
			stepID, setup.Cleanup.CleanAllSteps, setup.Cleanup.CleanFromStep, setup.Cleanup.CleanSpecificStep))
		setup.Cleanup = CleanupScope{} // No cleanup — don't delete other steps' outputs
		innerStepPath := resolveInnerStepPath(hcpo.approvedPlan.Steps, stepInfo)
		setup.Context.StepPathOverride = innerStepPath
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP-INNER] Inner step %q: skipping cleanup, using step path %q (target=%d, singleStep=%v)",
			stepID, innerStepPath, setup.Context.SingleStepTarget, setup.Context.RunSingleStepOnly))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Top-level step %q: cleanup scope CleanAll=%v, CleanFrom=%d, CleanSpecific=%d",
			stepID, setup.Cleanup.CleanAllSteps, setup.Cleanup.CleanFromStep, setup.Cleanup.CleanSpecificStep))
	}

	if err := execManager.ApplyCleanup(ctx, setup); err != nil {
		return "", fmt.Errorf("failed to apply cleanup: %w", err)
	}

	execManager.ApplyExecutionContext(setup)

	// Reload progress after cleanup
	progress, err = hcpo.loadStepProgress(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Failed to reload progress after cleanup, using in-memory: %v", err))
		progress = &StepProgress{
			CompletedStepIndices: make([]int, 0),
			TotalSteps:           totalSteps,
		}
	}

	// 6. Run via standard execution pipeline (handles all step types: regular, conditional, routing, etc.)
	execErr := hcpo.runExecutionPhase(ctx, breakdownSteps, 1, progress, setup.StartFromStep, setup.Context)

	// 7. Read execution result from logs (runExecutionPhase writes results to log files)
	result := ""
	if execErr == nil {
		loaded := false
		if isInnerStep && setup.Context.StepPathOverride != "" {
			// For inner steps, read from the overridden step path
			if r, ok := hcpo.loadStepResultFromLogsByPath(ctx, setup.Context.StepPathOverride); ok {
				result = r
				loaded = true
			}
		}
		if !loaded {
			if r, ok := hcpo.loadSingleStepResultFromLogs(ctx, targetIndex+1); ok {
				result = r
			} else {
				result = fmt.Sprintf("Step %q completed successfully", stepID)
			}
		}
	}

	if execErr != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Step %q failed: %v", stepID, execErr))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Step %q completed (result len=%d)", stepID, len(result)))
	}

	// Reset single step mode and workshop human input so subsequent calls don't inherit them
	hcpo.SetRunSingleStepMode(false, -1)
	hcpo.interactiveWorkflowHumanInput = ""

	return result, execErr
}

// applyWorkshopExecuteOptions applies per-call group/run_folder overrides.
// If GroupID is set, it resolves the run folder from the group and loads
// the group's variable values. If only RunFolder is set, it uses that directly.
func (hcpo *StepBasedWorkflowOrchestrator) applyWorkshopExecuteOptions(ctx context.Context, opts *WorkshopExecuteOptions) error {
	if opts.GroupID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] applyWorkshopExecuteOptions: groupID=%s, manifestLoaded=%v", opts.GroupID, hcpo.variablesManifest != nil))

		// Reload manifest from disk if not loaded (can happen on cached sessions)
		if hcpo.variablesManifest == nil {
			variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
			_, manifest, err := hcpo.variableManager.checkExistingVariables(ctx, variablesPath)
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Failed to reload variables manifest: %v", err))
			} else if manifest != nil {
				hcpo.variablesManifest = manifest
				hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Reloaded variables manifest from disk (%d groups, %d vars)", len(manifest.Groups), len(manifest.Variables)))
			}
		}

		// Resolve variable values for this group.
		// Try direct group ID match first, then fall back to matching by sanitized display name
		// (agents often pass the folder name which is derived from the display name).
		if hcpo.variablesManifest != nil {
			groupValues := hcpo.variablesManifest.GetVariableValues(opts.GroupID)
			resolvedGroupID := opts.GroupID
			if groupValues == nil {
				// Try matching by sanitized display name
				for _, g := range hcpo.variablesManifest.Groups {
					if g.DisplayName != "" {
						sanitized := hcpo.sanitizeDisplayNameForFolder(g.DisplayName)
						if sanitized == opts.GroupID {
							groupValues = g.Values
							resolvedGroupID = g.GroupID
							hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Resolved group %q by display name to group ID %q", opts.GroupID, resolvedGroupID))
							break
						}
					}
				}
			}
			if groupValues != nil {
				hcpo.variableValues = groupValues
				SyncVariablesToWorkspaceEnv(hcpo.BaseOrchestrator, groupValues)
				hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Loaded %d variable values for group %s (resolved=%s): %v", len(groupValues), opts.GroupID, resolvedGroupID, groupValues))
			} else {
				// Group not found — return a clear error so the agent asks the user for the correct group_id.
				var groupDescs []string
				for _, g := range hcpo.variablesManifest.Groups {
					if g.DisplayName != "" {
						groupDescs = append(groupDescs, fmt.Sprintf("%s (%s)", g.GroupID, g.DisplayName))
					} else {
						groupDescs = append(groupDescs, g.GroupID)
					}
				}
				return fmt.Errorf("group_id %q not found. Available groups: %s — ask the user which group to use", opts.GroupID, strings.Join(groupDescs, ", "))
			}
		} else {
			hcpo.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Cannot resolve group %q — variables manifest is nil even after reload attempt", opts.GroupID))
		}

		// Resolve run folder if not explicitly provided
		if opts.RunFolder == "" {
			// Build run folder from group: use latest iteration + group folder name
			runFolder, err := hcpo.resolveGroupRunFolder(ctx, opts.GroupID)
			if err != nil {
				return fmt.Errorf("failed to resolve run folder for group %q: %w", opts.GroupID, err)
			}
			opts.RunFolder = runFolder
		}
	}

	if opts.RunFolder != "" {
		hcpo.SetSelectedRunFolder(opts.RunFolder)
		hcpo.GetLogger().Debug(fmt.Sprintf("[WORKSHOP_DEBUG] Set run folder to %s", opts.RunFolder))
	}

	return nil
}

// getGroupIDs returns the group IDs from the variables manifest (for logging).
func (hcpo *StepBasedWorkflowOrchestrator) getGroupIDs() []string {
	if hcpo.variablesManifest == nil {
		return nil
	}
	ids := make([]string, len(hcpo.variablesManifest.Groups))
	for i, g := range hcpo.variablesManifest.Groups {
		ids[i] = g.GroupID
	}
	return ids
}

// resolveGroupRunFolder finds the run folder for a given group ID.
// It looks for existing iteration folders that contain a subfolder matching the group.
// Falls back to creating a path using the latest iteration + group display name or ID.
func (hcpo *StepBasedWorkflowOrchestrator) resolveGroupRunFolder(ctx context.Context, groupID string) (string, error) {
	workspacePath := hcpo.GetWorkspacePath()
	runsPath := fmt.Sprintf("%s/runs", workspacePath)

	// Find the group's display name for folder resolution
	groupFolderName := groupID
	if hcpo.variablesManifest != nil {
		for _, g := range hcpo.variablesManifest.Groups {
			if g.GroupID == groupID {
				if g.DisplayName != "" {
					sanitized := hcpo.sanitizeDisplayNameForFolder(g.DisplayName)
					if sanitized != "" {
						groupFolderName = sanitized
					}
				}
				break
			}
		}
	}

	// List existing iteration folders and find the latest one that contains this group
	existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
	if err != nil || len(existingFolders) == 0 {
		// No runs exist — use iteration-1
		runFolder := fmt.Sprintf("iteration-1/%s", groupFolderName)
		hcpo.GetLogger().Debug(fmt.Sprintf("[WORKSHOP_DEBUG] resolveGroupRunFolder: no existing runs, using %s", runFolder))
		return runFolder, nil
	}

	// Check existing folders in reverse order (latest first) for a matching group subfolder
	for i := len(existingFolders) - 1; i >= 0; i-- {
		iterFolder := existingFolders[i]
		candidatePath := fmt.Sprintf("%s/%s/%s", runsPath, iterFolder, groupFolderName)
		if hcpo.workspaceFileExists(ctx, candidatePath) {
			runFolder := fmt.Sprintf("%s/%s", iterFolder, groupFolderName)
			hcpo.GetLogger().Debug(fmt.Sprintf("[WORKSHOP_DEBUG] resolveGroupRunFolder: found existing %s", runFolder))
			return runFolder, nil
		}
	}

	// No existing group folder found — use the latest iteration
	latestIter := existingFolders[len(existingFolders)-1]
	runFolder := fmt.Sprintf("%s/%s", latestIter, groupFolderName)
	hcpo.GetLogger().Debug(fmt.Sprintf("[WORKSHOP_DEBUG] resolveGroupRunFolder: using latest iteration %s", runFolder))
	return runFolder, nil
}

// appendInstructionsToStep creates a shallow copy of the step with orchestrator instructions
// appended to its description. This mirrors what executePredefinedSubAgent does when the
// todo_task orchestrator delegates to a sub-agent via call_sub_agent.
func appendInstructionsToStep(step PlanStepInterface, instructions string) PlanStepInterface {
	switch s := step.(type) {
	case *RegularPlanStep:
		copy := *s
		if copy.Description != "" {
			copy.Description = fmt.Sprintf("%s\n\n## Orchestrator Instructions\n\n%s", copy.Description, instructions)
		} else {
			copy.Description = instructions
		}
		return &copy
	case *ConditionalPlanStep:
		copy := *s
		if copy.Description != "" {
			copy.Description = fmt.Sprintf("%s\n\n## Orchestrator Instructions\n\n%s", copy.Description, instructions)
		} else {
			copy.Description = instructions
		}
		return &copy
	case *DecisionPlanStep:
		copy := *s
		if copy.Description != "" {
			copy.Description = fmt.Sprintf("%s\n\n## Orchestrator Instructions\n\n%s", copy.Description, instructions)
		} else {
			copy.Description = instructions
		}
		return &copy
	default:
		// For step types without a direct Description field, return as-is
		return step
	}
}

// loadStepResultFromLogsByPath reads the latest execution result from logs using a custom step path.
// This is used for inner steps that use a non-standard path (e.g., "step-3-sub-login-expert").
func (hcpo *StepBasedWorkflowOrchestrator) loadStepResultFromLogsByPath(ctx context.Context, stepPath string) (string, bool) {
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, stepPath)
	var latestResult string
	var latestAttempt, latestIteration int

	for attempt := 1; attempt <= 10; attempt++ {
		for iteration := 0; iteration <= 10; iteration++ {
			filePath := fmt.Sprintf("%s/execution-attempt-%d-iteration-%d.json", executionLogsFolderPath, attempt, iteration)
			content, err := hcpo.ReadWorkspaceFile(ctx, filePath)
			if err != nil {
				continue
			}
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(content), &data); err != nil {
				continue
			}
			if execResult, ok := data["execution_result"].(string); ok {
				if attempt > latestAttempt || (attempt == latestAttempt && iteration > latestIteration) {
					latestResult = execResult
					latestAttempt = attempt
					latestIteration = iteration
				}
			}
		}
	}

	if latestResult != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("Loaded execution result from logs for %s (attempt %d, iteration %d)", stepPath, latestAttempt, latestIteration))
		return latestResult, true
	}
	return "", false
}

// resolveInnerStepPath computes the correct execution folder path for an inner step
// based on its parent step's position and branch info. This matches the naming convention
// used by the normal execution pipeline:
//   - Branch steps: "step-{N}-if-true-{idx}" / "step-{N}-if-false-{idx}"
//   - Sub-agent routes: "step-{N}-sub-{route_id}"
//   - Todo task step: "step-{N}-todo-task"
func resolveInnerStepPath(topLevelSteps []PlanStepInterface, info *WorkshopStepInfo) string {
	// Find parent step's 1-based position in the top-level plan
	parentNum := 0
	for i, s := range topLevelSteps {
		if s.GetID() == info.ParentID {
			parentNum = i + 1
			break
		}
	}
	if parentNum == 0 {
		// Fallback: use step ID as suffix to avoid collisions
		return fmt.Sprintf("step-inner-%s", info.Step.GetID())
	}

	branch := info.BranchName
	switch {
	case branch == "if_true":
		return fmt.Sprintf("step-%d-if-true-0", parentNum)
	case branch == "if_false":
		return fmt.Sprintf("step-%d-if-false-0", parentNum)
	case branch == "todo_task_step":
		return fmt.Sprintf("step-%d-todo-task", parentNum)
	case branch == "orchestration_step":
		return fmt.Sprintf("step-%d-orchestration", parentNum)
	case strings.HasPrefix(branch, "route:"):
		routeID := strings.TrimPrefix(branch, "route:")
		return fmt.Sprintf("step-%d-sub-%s", parentNum, routeID)
	default:
		return fmt.Sprintf("step-%d-inner-%s", parentNum, info.Step.GetID())
	}
}
