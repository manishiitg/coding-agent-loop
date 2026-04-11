package step_based_workflow

import (
	"context"
	"fmt"
	"sort"
)

// getEffectiveToolsForStep returns the list of effective MCP server/tool names for a step.
// Uses step-level filtering against the workflow cap, or workflow defaults.
func (hcpo *StepBasedWorkflowOrchestrator) getEffectiveToolsForStep(step PlanStepInterface) []string {
	agentConfigs := getAgentConfigs(step)
	workflowServers := hcpo.GetSelectedServers()

	var result []string
	if agentConfigs != nil && len(agentConfigs.SelectedTools) > 0 {
		result = filterToolsByWorkflow(agentConfigs.SelectedTools, workflowServers)
	} else if agentConfigs != nil && len(agentConfigs.SelectedServers) > 0 {
		result = filterServersByWorkflow(agentConfigs.SelectedServers, workflowServers)
	} else {
		result = workflowServers
	}

	sort.Strings(result)
	return result
}

// findStepInPlan recursively finds a step by ID in the plan structure
func (hcpo *StepBasedWorkflowOrchestrator) findStepInPlan(steps []PlanStepInterface, targetID string) PlanStepInterface {
	for _, step := range steps {
		if step.GetID() == targetID {
			return step
		}

		// Handle nested steps
		switch s := step.(type) {
		case *ConditionalPlanStep:
			if found := hcpo.findStepInPlan(s.IfTrueSteps, targetID); found != nil {
				return found
			}
			if found := hcpo.findStepInPlan(s.IfFalseSteps, targetID); found != nil {
				return found
			}
		case *TodoTaskPlanStep:
			// Check sub-agents in routes
			for _, route := range s.PredefinedRoutes {
				if route.SubAgentStep != nil {
					if route.SubAgentStep.GetID() == targetID {
						return route.SubAgentStep
					}
					if found := hcpo.findStepInPlan([]PlanStepInterface{route.SubAgentStep}, targetID); found != nil {
						return found
					}
				}
			}
		}
	}
	return nil
}

// LoadGlobalLearningHistory loads and formats the global workflow-level learning history.
// Returns empty string if no global learnings found or on error.
func (hcpo *StepBasedWorkflowOrchestrator) LoadGlobalLearningHistory(
	ctx context.Context,
) (string, error) {
	globalLearningsPath := hcpo.getLearningsBasePath() + "/" + GlobalLearningID

	// Read learning files from global folder
	learningFiles, err := hcpo.readStepLearningFiles(ctx, globalLearningsPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read global learning files from %s: %v - proceeding without global learnings", globalLearningsPath, err))
		return "", nil
	}

	if len(learningFiles) == 0 {
		return "", nil
	}

	// Format learnings for system prompt
	formattedLearnings, _ := hcpo.formatStepLearningFilesAsHistory(learningFiles)
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Loaded %d global learning file(s) for execution agent system prompt", len(learningFiles)))

	return formattedLearnings, nil
}

// ShouldSkipLearningDueToLock checks if learning should be skipped due to locked learnings.
// Returns true if learnings are locked AND learnings folder has content (existing learnings to use).
// Returns false if learnings are locked BUT folder is empty (need to create initial learnings).
func (hcpo *StepBasedWorkflowOrchestrator) ShouldSkipLearningDueToLock(
	ctx context.Context,
	agentConfigs *AgentConfigs,
	stepID string,
	stepIndex int,
	stepPath string,
) (bool, error) {
	// Check if learnings are locked in agent configs
	isLearningsLocked := agentConfigs != nil &&
		agentConfigs.LockLearnings != nil &&
		*agentConfigs.LockLearnings

	if !isLearningsLocked {
		return false, nil // Not locked, proceed with learning
	}

	// Learnings are locked - check if folder has content
	if stepID == "" {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d has locked learnings but no ID - cannot check learnings folder, will run learning", stepIndex+1))
		return false, nil
	}

	learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, stepIndex, stepPath)
	if err != nil {
		// If we can't check (folder doesn't exist or error), assume empty and run learning
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked but cannot check if learnings exist for step ID '%s' (step %d) - will run learning to create initial learnings: %v", stepID, stepIndex+1, err))
		return false, nil
	}

	if learningsEmpty {
		// Learnings are locked but folder is empty - run learning to create initial learnings
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked but folder is empty for step ID '%s' (step %d) - will run learning to create initial learnings", stepID, stepIndex+1))
		return false, nil
	}

	// Learnings are locked and learnings exist - skip learning
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked and learnings exist for step ID '%s' (step %d) - skipping learning", stepID, stepIndex+1))
	return true, nil
}
