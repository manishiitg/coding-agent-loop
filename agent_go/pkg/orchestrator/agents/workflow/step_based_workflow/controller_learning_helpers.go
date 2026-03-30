package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
)

// readStepLearningMetadata reads and parses the learning metadata file for a step.
func (hcpo *StepBasedWorkflowOrchestrator) readStepLearningMetadata(ctx context.Context, stepID string, stepPath string) (LearningMetadata, error) {
	// Use relative path - ReadWorkspaceFile auto-prepends workspacePath
	learningsBase := hcpo.getLearningsBasePath()
	metadataPath := filepath.Join(learningsBase, stepID, ".learning_metadata.json")

	var metadata LearningMetadata
	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err != nil {
		return metadata, err
	}

	if err := json.Unmarshal([]byte(content), &metadata); err != nil {
		return metadata, fmt.Errorf("failed to parse learning metadata: %w", err)
	}

	return metadata, nil
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
		case *OrchestrationPlanStep:
			// Check the inner orchestration step
			if s.OrchestrationStep != nil {
				if s.OrchestrationStep.GetID() == targetID {
					return s.OrchestrationStep
				}
				// Recurse into inner step if it has children
				if found := hcpo.findStepInPlan([]PlanStepInterface{s.OrchestrationStep}, targetID); found != nil {
					return found
				}
			}
			// Check sub-agents in routes
			for _, route := range s.OrchestrationRoutes {
				if route.SubAgentStep != nil {
					if route.SubAgentStep.GetID() == targetID {
						return route.SubAgentStep
					}
					if found := hcpo.findStepInPlan([]PlanStepInterface{route.SubAgentStep}, targetID); found != nil {
						return found
					}
				}
			}
		case *TodoTaskPlanStep:
			// Check the inner todo task step
			if s.TodoTaskStep != nil {
				if s.TodoTaskStep.GetID() == targetID {
					return s.TodoTaskStep
				}
				// Recurse into inner step if it has children
				if found := hcpo.findStepInPlan([]PlanStepInterface{s.TodoTaskStep}, targetID); found != nil {
					return found
				}
			}
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

// LoadStepLearningHistory loads and formats learning history for a step.
// Returns empty string if no learnings found or on error.
// Uses stepID for learning folder identification (supports inner steps).
func (hcpo *StepBasedWorkflowOrchestrator) LoadStepLearningHistory(
	ctx context.Context,
	stepID string,
	stepIndex int,
	stepPath string,
	stepType string,
) (string, error) {
	if stepID == "" {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Cannot load learnings: %s step has no ID", stepType))
		return "", nil
	}

	// getLearningFolderPathByStepID now returns RELATIVE path - workspace functions auto-prepend workspacePath
	stepLearningsPath := getLearningFolderPathByStepID("", stepID, stepPath, hcpo.isEvaluationMode)

	// Check if learnings folder exists and has files
	learningsFolderEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, stepIndex, stepPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check if %s step learnings folder is empty for step %s: %v, proceeding without learnings", stepType, stepID, err))
		// Continue anyway, readStepLearningFiles will handle non-existent folder
	}

	if learningsFolderEmpty {
		hcpo.GetLogger().Info(fmt.Sprintf("📁 %s step %s learnings folder is empty or does not exist: %s (proceeding without learnings)", stepType, stepID, stepLearningsPath))
		return "", nil
	}

	// Read learning files from step folder
	learningFiles, err := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read learning files from %s for %s step: %v - will proceed without learnings", stepLearningsPath, stepType, err))
		return "", nil
	}

	if len(learningFiles) == 0 {
		return "", nil
	}

	// Format learnings for system prompt
	formattedLearnings, _ := hcpo.formatStepLearningFilesAsHistory(learningFiles)
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Loaded %d learning file(s) for %s step agent system prompt", len(learningFiles), stepType))

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
