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
	baseWorkspacePath := hcpo.GetWorkspacePath()
	metadataPath := filepath.Join(baseWorkspacePath, "learnings", stepID, ".learning_metadata.json")

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

	// Add LearningDetailLevel to hash - changing it should reset learnings
	// This ensures that if user switches from "general" to "exact", we restart learning to capture the new detail level
	agentConfigs := getAgentConfigs(step)
	learningDetailLevel := "exact" // default
	if agentConfigs != nil && agentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = agentConfigs.LearningDetailLevel
	}
	sb.WriteString("|")
	sb.WriteString(learningDetailLevel)

	// Calculate SHA256
	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:])
}

// CheckAndResetStepHash checks if the step definition has changed and resets learnings if it has.
// This is the "Step Hash Guard" described in learnings_architecture.md.
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

	currentHash := hcpo.CalculateStepHash(step)

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

	// Reset counters and unlock learnings
	return hcpo.ResetLearningMetadata(ctx, stepID, stepIndex, stepPath, currentHash, "plan_changed")
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
	// 1. Reset counters in metadata
	baseWorkspacePath := hcpo.GetWorkspacePath()
	metadataPath := filepath.Join(baseWorkspacePath, "learnings", stepID, ".learning_metadata.json")

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
	metadata.SuccessfulRunsMedium = 0
	metadata.SuccessfulRunsComplex = 0
	metadata.ConsecutiveNoNewLearning = 0 // Also reset legacy counter
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

	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepLearningsPath := getLearningFolderPathByStepID(baseWorkspacePath, stepID, stepPath)

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
