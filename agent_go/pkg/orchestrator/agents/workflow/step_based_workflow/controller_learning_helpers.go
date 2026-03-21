package step_based_workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// CalculateStepHash calculates a SHA256 hash of the step's critical fields.
// This is used to detect if the step's definition has changed, requiring a learning reset.
func (hcpo *StepBasedWorkflowOrchestrator) CalculateStepHash(step PlanStepInterface) string {
	if step == nil {
		return ""
	}

	// Combine critical fields that define the task
	var sb strings.Builder
	sb.WriteString(step.GetTitle())
	sb.WriteString("|")
	sb.WriteString(step.GetDescription())
	sb.WriteString("|")
	sb.WriteString(step.GetSuccessCriteria())
	sb.WriteString("|")

	// Sort dependencies to ensure deterministic hash regardless of order
	deps := step.GetContextDependencies()
	if len(deps) > 0 {
		// Create a copy to avoid modifying the original step
		sortedDeps := make([]string, len(deps))
		copy(sortedDeps, deps)
		sort.Strings(sortedDeps)
		sb.WriteString(strings.Join(sortedDeps, ","))
	}

	// Add resolved servers and tools to hash - changing MCPs should reset learnings
	// since the agent now has different tools available which affects what it can learn.
	// Mirror the exact runtime resolution: step-level filtered by workflow cap, or workflow defaults.
	agentConfigs := getAgentConfigs(step)
	workflowServers := hcpo.GetSelectedServers()
	var effectiveServers []string
	var effectiveTools []string
	if agentConfigs != nil && len(agentConfigs.SelectedServers) > 0 {
		// Intersect step servers with workflow cap — matches what the agent actually gets at runtime
		effectiveServers = filterServersByWorkflow(agentConfigs.SelectedServers, workflowServers)
	} else {
		effectiveServers = workflowServers
	}
	if agentConfigs != nil && len(agentConfigs.SelectedTools) > 0 {
		effectiveTools = filterToolsByWorkflow(agentConfigs.SelectedTools, workflowServers)
	} else {
		effectiveTools = hcpo.GetSelectedTools()
	}
	if len(effectiveServers) > 0 {
		sortedServers := make([]string, len(effectiveServers))
		copy(sortedServers, effectiveServers)
		sort.Strings(sortedServers)
		sb.WriteString("|servers:")
		sb.WriteString(strings.Join(sortedServers, ","))
	}
	if len(effectiveTools) > 0 {
		sortedTools := make([]string, len(effectiveTools))
		copy(sortedTools, effectiveTools)
		sort.Strings(sortedTools)
		sb.WriteString("|tools:")
		sb.WriteString(strings.Join(sortedTools, ","))
	}

	// Calculate SHA256
	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:])
}

// getEffectiveToolsForStep returns the list of effective MCP server/tool names for a step.
// Uses the same resolution logic as CalculateStepHash: step-level filtered by workflow cap, or workflow defaults.
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

// CheckAndResetStepHash checks if the step definition has changed and resets learnings if it has.
// This is the "Step Hash Guard" described in learnings_architecture.md.
// Steps with learning_mode "human_assisted" skip this check — human-curated learnings
// are intentional and should not be invalidated by plan changes.
func (hcpo *StepBasedWorkflowOrchestrator) CheckAndResetStepHash(
	ctx context.Context,
	step PlanStepInterface,
	stepID string,
	stepIndex int,
	stepPath string,
) error {
	if stepID == "" {
		return nil
	}

	// Skip hash guard for human-assisted learning steps — their learnings are
	// manually curated and should not be reset on plan changes.
	if configs, err := hcpo.ReadStepConfigs(ctx); err == nil {
		for _, sc := range configs {
			if sc.ID == stepID && sc.AgentConfigs != nil && sc.AgentConfigs.LearningMode == "human_assisted" {
				hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping step hash guard for %s — learning_mode is human_assisted (learnings are manually curated)", stepID))
				return nil
			}
		}
	}

	// Try to find the original step in the approved plan to use for hashing.
	// This avoids hash mismatches due to dynamic orchestrator instructions being injected into the runtime step object.
	var originalStep PlanStepInterface
	if hcpo.approvedPlan != nil {
		originalStep = hcpo.findStepInPlan(hcpo.approvedPlan.Steps, stepID)
	}

	// Use original step if found, otherwise fallback to the current step (which might have runtime overrides)
	stepToHash := step
	if originalStep != nil {
		stepToHash = originalStep
	}

	currentHash := hcpo.CalculateStepHash(stepToHash)

	// Read existing metadata
	metadata, err := hcpo.readStepLearningMetadata(ctx, stepID, stepPath)
	if err != nil {
		// Metadata doesn't exist - this is a new step or first run
		// We'll create it later in the learning phase, but for now we're done
		return nil
	}

	// Check if hash matches
	if metadata.StepHash == currentHash {
		return nil // Hash matches, no reset needed
	}

	// Hash mismatch! Step definition changed.
	hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step definition changed for %s (Hash mismatch) - Triggering learning reset", stepID))

	// LOG DEBUG DETAILS FOR HASH MISMATCH
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [HASH DEBUG] Stored Hash: %s", metadata.StepHash))
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [HASH DEBUG] New Hash:    %s", currentHash))
	if originalStep != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [HASH DEBUG] Using original plan definition for hash to ignore dynamic orchestrator overrides"))
	}

	// Log components used for hash calculation
	deps := stepToHash.GetContextDependencies()
	sortedDeps := make([]string, len(deps))
	copy(sortedDeps, deps)
	sort.Strings(sortedDeps)

	desc := stepToHash.GetDescription()
	if len(desc) > 50 {
		desc = desc[:50] + "..."
	}

	crit := stepToHash.GetSuccessCriteria()
	if len(crit) > 50 {
		crit = crit[:50] + "..."
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [HASH DEBUG] Title: '%s'", stepToHash.GetTitle()))
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [HASH DEBUG] Description: '%s' (Length: %d)", desc, len(stepToHash.GetDescription())))
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [HASH DEBUG] Success Criteria: '%s' (Length: %d)", crit, len(stepToHash.GetSuccessCriteria())))
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [HASH DEBUG] Dependencies: %v", sortedDeps))

	// Reset counters and unlock learnings
	return hcpo.ResetLearningMetadata(ctx, stepID, stepIndex, stepPath, currentHash, "plan_changed")
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

// ResetLearningMetadata resets all stable run counters and unlocks learnings for a step.
func (hcpo *StepBasedWorkflowOrchestrator) ResetLearningMetadata(
	ctx context.Context,
	stepID string,
	stepIndex int,
	stepPath string,
	newHash string,
	unlockReason string,
) error {
	// 1. Reset counters in metadata - use relative path (ReadWorkspaceFile/WriteWorkspaceFile auto-prepend workspacePath)
	learningsBase := hcpo.getLearningsBasePath()
	metadataPath := filepath.Join(learningsBase, stepID, ".learning_metadata.json")

	var metadata LearningMetadata
	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err == nil {
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse learning metadata for reset: %v", err))
		}
	}

	// Reset all counters
	metadata.StepID = stepID
	metadata.StepPath = stepPath
	metadata.StepHash = newHash
	metadata.SuccessfulRunsSimple = 0

	// Clear auto-lock info to prevent UI from showing "Locked (Auto)" state
	// The UI checks if AutoLockedAt is set to determine if it's auto-locked
	metadata.AutoLockedAt = ""
	metadata.AutoLockReason = ""
	metadata.AutoLockIteration = 0

	metadata.AutoUnlockedAt = time.Now().Format(time.RFC3339)
	metadata.AutoUnlockReason = unlockReason
	metadata.AutoUnlockIteration = metadata.TotalIterations

	// Write updated metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err == nil {
		if err := hcpo.BaseOrchestrator.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write reset learning metadata: %v", err))
		}
	}

	// 2. Unlock learnings in step_config.json
	if err := hcpo.unlockStepLearningsInConfig(ctx, stepID); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to unlock learnings in config for step %s: %v", stepID, err))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔄 Resetting learning metadata and UNLOCKING learnings for %s (ID: %s) due to plan modification", stepPath, stepID))
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

