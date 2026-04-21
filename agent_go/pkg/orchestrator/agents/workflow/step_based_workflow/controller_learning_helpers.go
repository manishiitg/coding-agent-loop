package step_based_workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Learnings access levels (mirror of knowledgebase_access).
const (
	LearningsAccessRead      = "read"
	LearningsAccessReadWrite = "read-write"
	LearningsAccessNone      = "none"
)

// resolveLearningsAccess returns the effective learnings_access for a step.
// Explicit value wins; empty falls back to auto-migration:
//   - learning_objective non-empty → "read-write" (preserves legacy behavior)
//   - learning_objective empty     → "read"       (new default — all steps see _global/)
//
// An explicit bad value is normalized to "read" with a warning at validation time.
func resolveLearningsAccess(agentConfigs *AgentConfigs) string {
	if agentConfigs == nil {
		return LearningsAccessRead
	}
	v := strings.TrimSpace(agentConfigs.LearningsAccess)
	switch v {
	case LearningsAccessRead, LearningsAccessReadWrite, LearningsAccessNone:
		return v
	case "":
		if strings.TrimSpace(agentConfigs.LearningObjective) != "" {
			return LearningsAccessReadWrite
		}
		return LearningsAccessRead
	default:
		return LearningsAccessRead
	}
}

// canReadLearnings reports whether this step's execution prompt should include
// the global SKILL.md content. Read is the default unless explicitly set to
// "none"; routing steps and evaluation-mode runs always skip to keep their
// prompts lean.
func canReadLearnings(agentConfigs *AgentConfigs, step PlanStepInterface, isEvalMode bool) bool {
	if isEvalMode || (step != nil && isRoutingStep(step)) {
		return false
	}
	return resolveLearningsAccess(agentConfigs) != LearningsAccessNone
}

// canWriteLearnings reports whether the post-step learning agent should run
// for this step. Requires learnings_access == "read-write" AND a non-empty
// learning_objective (the extraction target for the writer). Routing and eval
// mode always skip.
func canWriteLearnings(agentConfigs *AgentConfigs, step PlanStepInterface, isEvalMode bool) bool {
	if isEvalMode || (step != nil && isRoutingStep(step)) {
		return false
	}
	if agentConfigs == nil {
		return false
	}
	if resolveLearningsAccess(agentConfigs) != LearningsAccessReadWrite {
		return false
	}
	return strings.TrimSpace(agentConfigs.LearningObjective) != ""
}

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
