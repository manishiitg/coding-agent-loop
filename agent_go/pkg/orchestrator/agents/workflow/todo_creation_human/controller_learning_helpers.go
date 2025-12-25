package todo_creation_human

import (
	"context"
	"fmt"
)

// LoadStepLearningHistory loads and formats learning history for a step.
// Returns empty string if no learnings found or on error.
// Uses stepID for learning folder identification (supports inner steps).
func (hcpo *HumanControlledTodoPlannerOrchestrator) LoadStepLearningHistory(
	ctx context.Context,
	stepID string,
	stepIndex int,
	stepPath string,
	stepType string, // e.g., "decision", "conditional", "orchestration"
) (string, error) {
	if stepID == "" {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Cannot load learnings: %s step has no ID", stepType))
		return "", nil
	}

	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepLearningsPath := getLearningFolderPathByStepID(baseWorkspacePath, stepID, stepPath)

	// Check if learnings folder exists and has files
	learningsFolderEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, stepIndex, stepPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check if %s step learnings folder is empty for step %s: %v, proceeding without learnings", stepType, stepID, err))
		return "", nil
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) ShouldSkipLearningDueToLock(
	ctx context.Context,
	step PlanStepInterface,
	stepID string,
	stepIndex int,
	stepPath string,
) (bool, error) {
	// Check if learnings are locked in agent configs
	agentConfigs := getAgentConfigs(step)
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
