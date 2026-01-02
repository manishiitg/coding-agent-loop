package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

// DetectionHistoryEntry represents a single learning detection result
type DetectionHistoryEntry struct {
	Iteration      int     `json:"iteration"`
	Timestamp      string  `json:"timestamp"`
	HasNewLearning bool    `json:"has_new_learning"`
	Reasoning      string  `json:"reasoning"`
	Confidence     float64 `json:"confidence"`
}

// ConsolidationHistoryEntry represents a single consolidation operation result
type ConsolidationHistoryEntry struct {
	Iteration          int    `json:"iteration"`
	Timestamp          string `json:"timestamp"`
	FilesConsolidated  int    `json:"files_consolidated,omitempty"`
	FilesDeleted       int    `json:"files_deleted,omitempty"`
	PatternsMerged     int    `json:"patterns_merged,omitempty"`
	PatternsUpdated    int    `json:"patterns_updated,omitempty"`
	OptimalPathsMarked int    `json:"optimal_paths_marked,omitempty"`
	UnreliableMarked   int    `json:"unreliable_marked,omitempty"`
	ConsolidatedFile   string `json:"consolidated_file,omitempty"`
	// Note: Output and NewLearningContent removed - learning content is stored in files, not metadata
}

// LearningMetadata represents the learning metadata stored per step
type LearningMetadata struct {
	StepID                   string                      `json:"step_id"`
	StepPath                 string                      `json:"step_path"`
	StepHash                 string                      `json:"step_hash,omitempty"` // SHA256 of step definition
	TotalIterations          int                         `json:"total_iterations"`
	ConsecutiveNoNewLearning int                         `json:"consecutive_no_new_learning"` // Legacy - keeping for backward compatibility
	SuccessfulRunsSimple     int                         `json:"successful_runs_simple"`      // Successful runs for TurnCount < 15
	SuccessfulRunsMedium     int                         `json:"successful_runs_medium"`      // Successful runs for TurnCount 15-30
	SuccessfulRunsComplex    int                         `json:"successful_runs_complex"`     // Successful runs for TurnCount > 30
	LastTurnCount            int                         `json:"last_turn_count"`             // Last recorded TurnCount
	LastLearningLLM          string                      `json:"last_learning_llm,omitempty"` // The LLM used for the last learning cycle
	LastLearningDetectedAt   string                      `json:"last_learning_detected_at,omitempty"`
	LastDetectionReasoning   string                      `json:"last_detection_reasoning,omitempty"`
	LastDetectionConfidence  float64                     `json:"last_detection_confidence,omitempty"`
	DetectionHistory         []DetectionHistoryEntry     `json:"detection_history,omitempty"`
	ConsolidationHistory     []ConsolidationHistoryEntry `json:"consolidation_history,omitempty"`
	LastConsolidationAt      string                      `json:"last_consolidation_at,omitempty"`
	// Auto-lock information
	AutoLockedAt      string `json:"auto_locked_at,omitempty"`      // Timestamp when auto-lock was triggered
	AutoLockReason    string `json:"auto_lock_reason,omitempty"`    // Reason: "threshold_reached", "maximum_learnings", etc.
	AutoLockIteration int    `json:"auto_lock_iteration,omitempty"` // Iteration number when auto-lock was triggered
	// Auto-unlock information
	AutoUnlockedAt      string `json:"auto_unlocked_at,omitempty"`      // Timestamp when auto-unlock was triggered
	AutoUnlockReason    string `json:"auto_unlock_reason,omitempty"`    // Reason: "validation_failed", "decision_step_false", "plan_changed"
	AutoUnlockIteration int    `json:"auto_unlock_iteration,omitempty"` // Iteration number when auto-unlock was triggered
	// Note: LastConsolidationOutput removed - learning content is stored in files, not metadata
}

// GetLearningMetadata reads and returns the learning metadata for a given step
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetLearningMetadata(
	ctx context.Context,
	learningPathIdentifier string,
) (*LearningMetadata, error) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	metadataPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier, ".learning_metadata.json")

	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err != nil {
		return nil, err // Return error if file doesn't exist or can't be read
	}

	var metadata LearningMetadata
	if err := json.Unmarshal([]byte(content), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse learning metadata: %w", err)
	}

	return &metadata, nil
}

// updateLearningMetadata updates the learning metadata file with detection results
// Returns true if auto-lock should be triggered (consecutive_no_new_learning >= 2 OR total_iterations >= 10)
func (hcpo *HumanControlledTodoPlannerOrchestrator) updateLearningMetadata(
	ctx context.Context,
	stepIndex int,
	stepPath string,
	learningPathIdentifier string,
	hasNewLearning bool,
	reasoning string,
	confidence float64,
) (bool, error) {
	// Re-route to the new complexity-based logic with a default turn count of 0 (unknown)
	// and no plan step (will skip hash check). validationPassed is false (safe default).
	return hcpo.updateLearningMetadataWithTurnCount(ctx, stepIndex, stepPath, learningPathIdentifier, hasNewLearning, reasoning, confidence, 0, nil, false, "")
}

// updateLearningMetadataWithTurnCount updates the learning metadata with TurnCount-based complexity tracking.
// This is the implementation of the "TurnCount Tracker" described in learnings_architecture.md.
func (hcpo *HumanControlledTodoPlannerOrchestrator) updateLearningMetadataWithTurnCount(
	ctx context.Context,
	stepIndex int,
	stepPath string,
	learningPathIdentifier string,
	hasNewLearning bool,
	reasoning string,
	confidence float64,
	turnCount int,
	step PlanStepInterface,
	validationPassed bool,
	usedLLM string,
) (bool, error) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	metadataPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier, ".learning_metadata.json")

	// Read existing metadata or create new
	var metadata LearningMetadata
	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err != nil {
		// Metadata doesn't exist - create new
		metadata = LearningMetadata{
			StepID:                   learningPathIdentifier,
			StepPath:                 stepPath,
			TotalIterations:          0,
			ConsecutiveNoNewLearning: 0,
		}
	} else {
		// Parse existing metadata
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse learning metadata: %v (creating new)", err))
			metadata = LearningMetadata{
				StepID:                   learningPathIdentifier,
				StepPath:                 stepPath,
				TotalIterations:          0,
				ConsecutiveNoNewLearning: 0,
			}
		}
	}

	// Initialize slices if nil
	if metadata.DetectionHistory == nil {
		metadata.DetectionHistory = []DetectionHistoryEntry{}
	}
	if metadata.ConsolidationHistory == nil {
		metadata.ConsolidationHistory = []ConsolidationHistoryEntry{}
	}

	// Update common fields
	metadata.TotalIterations++
	metadata.LastTurnCount = turnCount
	metadata.LastLearningLLM = usedLLM
	if step != nil {
		metadata.StepHash = hcpo.CalculateStepHash(step)
	}

	if hasNewLearning {
		metadata.ConsecutiveNoNewLearning = 0
		metadata.LastLearningDetectedAt = time.Now().Format(time.RFC3339)
	} else {
		metadata.ConsecutiveNoNewLearning++
	}
	metadata.LastDetectionReasoning = reasoning
	metadata.LastDetectionConfidence = confidence

	// Increment complexity-based counters (ONLY on successful validation/new learning)
	// Note: In Mode A (Unlocked), we increment counters if validation passed.
	// Since this is called after success learning extraction, we increment here.
	if validationPassed && turnCount > 0 {
		if turnCount < 15 {
			metadata.SuccessfulRunsSimple++
		} else if turnCount <= 30 {
			metadata.SuccessfulRunsMedium++
		} else {
			metadata.SuccessfulRunsComplex++
		}
	}

	// Add detection result to history
	detectionEntry := DetectionHistoryEntry{
		Iteration:      metadata.TotalIterations,
		Timestamp:      time.Now().Format(time.RFC3339),
		HasNewLearning: hasNewLearning,
		Reasoning:      reasoning,
		Confidence:     confidence,
	}
	metadata.DetectionHistory = append(metadata.DetectionHistory, detectionEntry)

	// Limit history to last 50 entries
	if len(metadata.DetectionHistory) > 50 {
		metadata.DetectionHistory = metadata.DetectionHistory[len(metadata.DetectionHistory)-50:]
	}

	// Check if auto-lock should be triggered based on TurnCount complexity
	// Simple (< 15 turns): Lock after 3 successful runs.
	// Medium (15-30 turns): Lock after 5 successful runs.
	// Complex (> 30 turns): Lock after 10 successful runs.
	shouldAutoLock := false
	var autoLockReason string

	if metadata.SuccessfulRunsSimple >= 3 {
		shouldAutoLock = true
		autoLockReason = "threshold_reached_simple"
	} else if metadata.SuccessfulRunsMedium >= 5 {
		shouldAutoLock = true
		autoLockReason = "threshold_reached_medium"
	} else if metadata.SuccessfulRunsComplex >= 10 {
		shouldAutoLock = true
		autoLockReason = "threshold_reached_complex"
	}

	// Fallback to max iterations (safety)
	if !shouldAutoLock && metadata.TotalIterations >= 15 {
		shouldAutoLock = true
		autoLockReason = "maximum_learnings_reached"
	}

	if shouldAutoLock {
		metadata.AutoLockedAt = time.Now().Format(time.RFC3339)
		metadata.AutoLockReason = autoLockReason
		metadata.AutoLockIteration = metadata.TotalIterations
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-lock threshold reached for %s: %s", learningPathIdentifier, autoLockReason))
	}

	// Write updated metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return false, fmt.Errorf("failed to marshal learning metadata: %w", err)
	}

	if err := hcpo.BaseOrchestrator.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); err != nil {
		return false, fmt.Errorf("failed to write learning metadata: %w", err)
	}

	return shouldAutoLock, nil
}

// updateConsolidationMetadata updates the learning metadata file with consolidation results
func (hcpo *HumanControlledTodoPlannerOrchestrator) updateConsolidationMetadata(
	ctx context.Context,
	stepIndex int,
	stepPath string,
	learningPathIdentifier string,
	consolidationOutput string,
	newLearningContent string,
) error {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	metadataPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier, ".learning_metadata.json")

	// Read existing metadata or create new
	var metadata LearningMetadata
	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err != nil {
		// Metadata doesn't exist - create new
		metadata = LearningMetadata{
			StepID:               fmt.Sprintf("step-%d", stepIndex+1),
			StepPath:             stepPath,
			TotalIterations:      0,
			ConsolidationHistory: []ConsolidationHistoryEntry{},
		}
	} else {
		// Parse existing metadata
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse learning metadata: %v (creating new)", err))
			metadata = LearningMetadata{
				StepID:               fmt.Sprintf("step-%d", stepIndex+1),
				StepPath:             stepPath,
				TotalIterations:      0,
				ConsolidationHistory: []ConsolidationHistoryEntry{},
			}
		}
	}

	// Initialize DetectionHistory if nil (for backward compatibility with old metadata)
	if metadata.DetectionHistory == nil {
		metadata.DetectionHistory = []DetectionHistoryEntry{}
	}

	// Initialize ConsolidationHistory if nil (for backward compatibility with old metadata)
	if metadata.ConsolidationHistory == nil {
		metadata.ConsolidationHistory = []ConsolidationHistoryEntry{}
	}

	// Use TotalIterations for consolidation entry iteration number
	// If TotalIterations is 0, this is the first consolidation
	iteration := metadata.TotalIterations
	if iteration == 0 {
		iteration = 1
	}

	// Create consolidation entry (without learning content - stored in files, not metadata)
	consolidationEntry := ConsolidationHistoryEntry{
		Iteration:        iteration,
		Timestamp:        time.Now().Format(time.RFC3339),
		ConsolidatedFile: extractConsolidatedFilePath(consolidationOutput),
		// Note: Output and NewLearningContent removed - learning content is stored in files, not metadata
	}

	// Add consolidation result to history
	metadata.ConsolidationHistory = append(metadata.ConsolidationHistory, consolidationEntry)

	// Update last consolidation timestamp (output removed - learning content stored in files)
	metadata.LastConsolidationAt = time.Now().Format(time.RFC3339)

	// Limit history to last 50 entries to prevent unbounded growth
	const maxHistoryEntries = 50
	if len(metadata.ConsolidationHistory) > maxHistoryEntries {
		// Keep only the last maxHistoryEntries entries
		metadata.ConsolidationHistory = metadata.ConsolidationHistory[len(metadata.ConsolidationHistory)-maxHistoryEntries:]
	}

	// Write updated metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal learning metadata: %w", err)
	}

	if err := hcpo.BaseOrchestrator.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); err != nil {
		return fmt.Errorf("failed to write learning metadata: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📝 Updated consolidation metadata for %s", learningPathIdentifier))

	return nil
}

// extractConsolidatedFilePath extracts the consolidated file path from consolidation output
// The output format is typically: "Updated: /path/to/file.md"
func extractConsolidatedFilePath(output string) string {
	// Look for "Updated: " prefix
	if strings.HasPrefix(output, "Updated: ") {
		path := strings.TrimPrefix(output, "Updated: ")
		// Remove any trailing whitespace or newlines
		path = strings.TrimSpace(path)
		return path
	}
	// If no "Updated: " prefix, try to find a file path pattern
	// Look for common patterns like "learnings/{step_id}/..._learning.md"
	if strings.Contains(output, "_learning.md") {
		// Try to extract the path
		parts := strings.Fields(output)
		for _, part := range parts {
			if strings.Contains(part, "_learning.md") {
				return strings.TrimSpace(part)
			}
		}
	}
	return ""
}

// autoLockStepLearningsInConfig updates step_config.json to set LockLearnings = true
func (hcpo *HumanControlledTodoPlannerOrchestrator) autoLockStepLearningsInConfig(
	ctx context.Context,
	stepID string,
	reasoning string,
) error {
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-locking learnings for step %s in step_config.json", stepID))

	// Read current step configs
	configs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		return fmt.Errorf("failed to read step configs: %w", err)
	}

	// Find step config by ID
	var stepConfig *StepConfig
	for i := range configs {
		if configs[i].ID == stepID {
			stepConfig = &configs[i]
			break
		}
	}

	// If step config doesn't exist, create it
	if stepConfig == nil {
		stepConfig = &StepConfig{
			ID:           stepID,
			AgentConfigs: &AgentConfigs{},
		}
		configs = append(configs, *stepConfig)
	}

	// Ensure AgentConfigs exists
	if stepConfig.AgentConfigs == nil {
		stepConfig.AgentConfigs = &AgentConfigs{}
	}

	// Set LockLearnings = true
	lockValue := true
	stepConfig.AgentConfigs.LockLearnings = &lockValue

	// Update the config in the slice
	for i := range configs {
		if configs[i].ID == stepID {
			configs[i] = *stepConfig
			break
		}
	}

	// Write updated configs
	if err := hcpo.WriteStepConfigs(ctx, configs); err != nil {
		return fmt.Errorf("failed to write step configs: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Auto-locked learnings for step %s (LockLearnings = true)", stepID))

	// Emit event to notify frontend that step config was updated
	if err := hcpo.emitStepConfigUpdatedEvent(ctx, stepID); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit step config updated event: %v", err))
		// Don't fail the whole operation if event emission fails
	}

	return nil
}

// updateUnlockMetadata updates the learning metadata file with unlock information
func (hcpo *HumanControlledTodoPlannerOrchestrator) updateUnlockMetadata(
	ctx context.Context,
	stepID string,
	stepIndex int,
	stepPath string,
	learningPathIdentifier string,
	unlockReason string,
) error {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	metadataPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier, ".learning_metadata.json")

	// Read existing metadata or create new
	var metadata LearningMetadata
	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err != nil {
		// Metadata doesn't exist - create new
		metadata = LearningMetadata{
			StepID:                   fmt.Sprintf("step-%d", stepIndex+1),
			StepPath:                 stepPath,
			TotalIterations:          0,
			ConsecutiveNoNewLearning: 0,
			DetectionHistory:         []DetectionHistoryEntry{},
			ConsolidationHistory:     []ConsolidationHistoryEntry{},
		}
	} else {
		// Parse existing metadata
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse learning metadata: %v (creating new)", err))
			metadata = LearningMetadata{
				StepID:                   fmt.Sprintf("step-%d", stepIndex+1),
				StepPath:                 stepPath,
				TotalIterations:          0,
				ConsecutiveNoNewLearning: 0,
				DetectionHistory:         []DetectionHistoryEntry{},
				ConsolidationHistory:     []ConsolidationHistoryEntry{},
			}
		}
	}

	// Initialize DetectionHistory if nil (for backward compatibility)
	if metadata.DetectionHistory == nil {
		metadata.DetectionHistory = []DetectionHistoryEntry{}
	}

	// Initialize ConsolidationHistory if nil (for backward compatibility)
	if metadata.ConsolidationHistory == nil {
		metadata.ConsolidationHistory = []ConsolidationHistoryEntry{}
	}

	// Update unlock information
	metadata.AutoUnlockedAt = time.Now().Format(time.RFC3339)
	metadata.AutoUnlockReason = unlockReason
	metadata.AutoUnlockIteration = metadata.TotalIterations

	// Write updated metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal learning metadata: %w", err)
	}

	if err := hcpo.BaseOrchestrator.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); err != nil {
		return fmt.Errorf("failed to write learning metadata: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Updated unlock metadata for %s (reason: %s)", learningPathIdentifier, unlockReason))
	return nil
}

// unlockStepLearningsInConfig updates step_config.json to set LockLearnings = false (unlocks learnings)
func (hcpo *HumanControlledTodoPlannerOrchestrator) unlockStepLearningsInConfig(
	ctx context.Context,
	stepID string,
) error {
	hcpo.GetLogger().Info(fmt.Sprintf("🔓 Unlocking learnings for step %s in step_config.json", stepID))

	// Read current step configs
	configs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		return fmt.Errorf("failed to read step configs: %w", err)
	}

	// Find step config by ID
	var stepConfig *StepConfig
	configIndex := -1
	for i := range configs {
		if configs[i].ID == stepID {
			stepConfig = &configs[i]
			configIndex = i
			break
		}
	}

	// If step config doesn't exist, nothing to unlock
	if stepConfig == nil {
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Step config for %s doesn't exist - nothing to unlock", stepID))
		return nil
	}

	// Ensure AgentConfigs exists
	if stepConfig.AgentConfigs == nil {
		stepConfig.AgentConfigs = &AgentConfigs{}
	}

	// Set LockLearnings = false (explicitly unlock)
	lockValue := false
	stepConfig.AgentConfigs.LockLearnings = &lockValue

	// Update the config in the slice
	if configIndex >= 0 {
		configs[configIndex] = *stepConfig
	}

	// Write updated configs
	if err := hcpo.WriteStepConfigs(ctx, configs); err != nil {
		return fmt.Errorf("failed to write step configs: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Unlocked learnings for step %s (LockLearnings = false)", stepID))
	return nil
}

// unlockStepLearningsAndResetMetadata unlocks learnings and resets metadata counter
func (hcpo *HumanControlledTodoPlannerOrchestrator) unlockStepLearningsAndResetMetadata(
	ctx context.Context,
	stepID string,
	stepIndex int,
	stepPath string,
	learningPathIdentifier string,
) error {
	// Unlock in step config
	if err := hcpo.unlockStepLearningsInConfig(ctx, stepID); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to unlock learnings in config for step %s: %v", stepID, err))
		// Continue to reset metadata even if config unlock fails
	}

	// Reset validation failure count for UI (since step is being unlocked/reset)
	if err := hcpo.ResetValidationFailureCount(ctx, stepPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to reset validation failure count for %s: %v", stepPath, err))
	}

	// Reset metadata counter
	baseWorkspacePath := hcpo.GetWorkspacePath()
	metadataPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier, ".learning_metadata.json")

	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err == nil {
		// Metadata exists - reset counter
		var metadata LearningMetadata
		if err := json.Unmarshal([]byte(content), &metadata); err == nil {
			metadata.ConsecutiveNoNewLearning = 0
			metadataJSON, marshalErr := json.MarshalIndent(metadata, "", "  ")
			if marshalErr == nil {
				if writeErr := hcpo.BaseOrchestrator.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); writeErr == nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Reset learning metadata counter for %s", learningPathIdentifier))
				}
			}
		}
	}

	return nil
}

// createUnlockLearningsFunction creates a function that can unlock learnings for a step
// This function can be passed to planning agent executors
func (hcpo *HumanControlledTodoPlannerOrchestrator) createUnlockLearningsFunction() func(context.Context, string, int) error {
	return func(ctx context.Context, stepID string, stepIndex int) error {
		// Use step ID for learning path identifier (new format)
		learningPathIdentifier := stepID

		// Calculate step path (relative to workspace)
		stepPath := fmt.Sprintf("planning/step-%d", stepIndex+1)

		// Call the full unlock function
		return hcpo.unlockStepLearningsAndResetMetadata(ctx, stepID, stepIndex, stepPath, learningPathIdentifier)
	}
}

// createUnlockLearningsFunctionFromBase creates an unlock function using base orchestrator
// This is used when only BaseOrchestrator is available (e.g., in PlanImprovementManager)
func createUnlockLearningsFunctionFromBase(bo *orchestrator.BaseOrchestrator, workspacePath string) func(context.Context, string, int) error {
	return func(ctx context.Context, stepID string, stepIndex int) error {
		// Use step ID for learning path identifier (new format)
		learningPathIdentifier := stepID

		// Unlock in step config
		configs, err := ReadStepConfigs(ctx, bo, workspacePath, workspacePath)
		if err != nil {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step configs for unlock: %v", err))
			// Continue to reset metadata even if config read fails
		} else {
			// Find step config by ID
			var stepConfig *StepConfig
			configIndex := -1
			for i := range configs {
				if configs[i].ID == stepID {
					stepConfig = &configs[i]
					configIndex = i
					break
				}
			}

			// If step config exists, unlock it
			if stepConfig != nil {
				// Ensure AgentConfigs exists
				if stepConfig.AgentConfigs == nil {
					stepConfig.AgentConfigs = &AgentConfigs{}
				}

				// Set LockLearnings = false (explicitly unlock)
				lockValue := false
				stepConfig.AgentConfigs.LockLearnings = &lockValue

				// Update the config in the slice
				if configIndex >= 0 {
					configs[configIndex] = *stepConfig
				}

				// Write updated configs (using standalone WriteStepConfigs logic)
				configPath := filepath.Join(workspacePath, "planning", "step_config.json")
				configFile := StepConfigFile{Steps: configs}
				jsonData, err := json.MarshalIndent(configFile, "", "  ")
				if err == nil {
					if err := bo.WriteWorkspaceFile(ctx, configPath, string(jsonData)); err == nil {
						bo.GetLogger().Info(fmt.Sprintf("✅ Unlocked learnings for step %s (LockLearnings = false)", stepID))
					}
				}
			}
		}

		// Reset metadata counter
		metadataPath := filepath.Join(workspacePath, "learnings", learningPathIdentifier, ".learning_metadata.json")
		content, err := bo.ReadWorkspaceFile(ctx, metadataPath)
		if err == nil {
			// Metadata exists - reset counter
			var metadata LearningMetadata
			if err := json.Unmarshal([]byte(content), &metadata); err == nil {
				metadata.ConsecutiveNoNewLearning = 0
				metadataJSON, marshalErr := json.MarshalIndent(metadata, "", "  ")
				if marshalErr == nil {
					if writeErr := bo.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); writeErr == nil {
						bo.GetLogger().Info(fmt.Sprintf("✅ Reset learning metadata counter for %s", learningPathIdentifier))
					}
				}
			}
		}

		return nil
	}
}

// Note: savePreviousLearningsToMetadata has been removed
// Previous learnings are now read directly from files before the learning phase runs
// This avoids storing large content in metadata files

// emitStepConfigUpdatedEvent emits a TodoStepsExtracted event with the updated step config
// This notifies the frontend to update the React Flow node dynamically
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitStepConfigUpdatedEvent(
	ctx context.Context,
	stepID string,
) error {
	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Starting event emission for step %s", stepID))

	// Read plan.json from the correct location (planning/plan.json)
	planPath := filepath.Join(hcpo.GetWorkspacePath(), "planning", "plan.json")
	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Reading plan.json from: %s", planPath))
	planContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [emitStepConfigUpdatedEvent] Failed to read plan.json: %v, skipping event", err))
		return nil // Don't fail, just skip the event
	}

	var planResponse PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &planResponse); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [emitStepConfigUpdatedEvent] Failed to parse plan.json: %v, skipping event", err))
		return nil // Don't fail, just skip the event
	}

	// Find the step in the plan
	var foundStepPlan PlanStepInterface
	for _, step := range planResponse.Steps {
		if step.GetID() == stepID {
			foundStepPlan = step
			break
		}
	}

	if foundStepPlan == nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [emitStepConfigUpdatedEvent] Step %s not found in plan, skipping event emission", stepID))
		return nil // Don't fail, just skip the event
	}

	// Prepare metadata indicating this is a step config update (not a full plan update)
	metadata := map[string]interface{}{
		"changed_step_ids":   []string{stepID},
		"config_update_only": true, // Flag to indicate this is just a config update
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Prepared metadata: changed_step_ids=%v, config_update_only=true", metadata["changed_step_ids"]))

	// Emit the event with metadata
	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Calling EmitTodoStepsExtractedEventWithMetadata for step %s", stepID))
	EmitTodoStepsExtractedEventWithMetadata(
		ctx,
		hcpo.BaseOrchestrator,
		[]PlanStepInterface{foundStepPlan},
		"step_config_updated",
		"auto_lock_learnings",
		"",
		hcpo.GetWorkspacePath(),
		metadata,
	)

	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Successfully completed event emission for step %s (auto-lock learnings)", stepID))
	return nil
}
