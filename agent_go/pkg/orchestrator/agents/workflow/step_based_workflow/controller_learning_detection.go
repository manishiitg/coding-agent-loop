package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
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
	StepHash                 string                      `json:"step_hash,omitempty"`                // SHA256 of step definition
	LearningContentHash      string                      `json:"learning_content_hash,omitempty"`    // SHA256 of learning file contents — if changed, force exploration mode
	TotalIterations          int                         `json:"total_iterations"`
	ConsecutiveNoNewLearning int                         `json:"consecutive_no_new_learning"` // Legacy - keeping for backward compatibility
	SuccessfulRunsSimple     int                         `json:"successful_runs_simple"`      // Successful runs for TurnCount < 100 (TODO: Turn-based classification is unreliable - varies by model, doesn't reflect actual complexity)
	SuccessfulRunsMedium     int                         `json:"successful_runs_medium"`      // Successful runs for TurnCount 100-200 (TODO: Turn-based classification is unreliable - varies by model, doesn't reflect actual complexity)
	SuccessfulRunsComplex    int                         `json:"successful_runs_complex"`     // Successful runs for TurnCount > 200 (TODO: Turn-based classification is unreliable - varies by model, doesn't reflect actual complexity)
	LastTurnCount            int                         `json:"last_turn_count"`             // Last recorded TurnCount
	LastExecutionLLM         string                      `json:"last_execution_llm,omitempty"` // The LLM used for the last execution (associated with last_turn_count)
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

// getLearningsBasePath returns the correct learnings base path based on evaluation mode
// In evaluation mode: "evaluation/learnings"
// In regular mode: "learnings"
func (hcpo *StepBasedWorkflowOrchestrator) getLearningsBasePath() string {
	if hcpo.isEvaluationMode {
		return "evaluation/learnings"
	}
	return "learnings"
}

// GetLearningMetadata reads and returns the learning metadata for a given step
func (hcpo *StepBasedWorkflowOrchestrator) GetLearningMetadata(
	ctx context.Context,
	learningPathIdentifier string,
) (*LearningMetadata, error) {
	// Use relative path - ReadWorkspaceFile auto-prepends workspacePath
	learningsBase := hcpo.getLearningsBasePath()
	metadataPath := filepath.Join(learningsBase, learningPathIdentifier, ".learning_metadata.json")

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

// updateLearningMetadataWithTurnCount updates the learning metadata with TurnCount-based complexity tracking.
// This is the implementation of the "TurnCount Tracker" described in learnings_architecture.md.
func (hcpo *StepBasedWorkflowOrchestrator) updateLearningMetadataWithTurnCount(
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
	executionLLM string,
	learningLLM string,
) (bool, error) {
	// Use relative path - ReadWorkspaceFile/WriteWorkspaceFile auto-prepend workspacePath
	learningsBase := hcpo.getLearningsBasePath()
	metadataPath := filepath.Join(learningsBase, learningPathIdentifier, ".learning_metadata.json")

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
	metadata.LastExecutionLLM = executionLLM
	metadata.LastLearningLLM = learningLLM
	if step != nil {
		// Fix: Use original step from plan to ensure hash stability
		// Otherwise, dynamic orchestrator overrides cause hash drift and infinite auto-unlock loops
		var stepToHash PlanStepInterface = step
		if hcpo.approvedPlan != nil {
			if originalStep := hcpo.findStepInPlan(hcpo.approvedPlan.Steps, step.GetID()); originalStep != nil {
				stepToHash = originalStep
			}
		}
		metadata.StepHash = hcpo.CalculateStepHash(stepToHash)
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
	//
	// TODO: Turn-based classification is not reliable - turn count varies significantly based on
	// the LLM model used (e.g., Claude vs GPT vs cheaper models) and doesn't reflect actual
	// step complexity. We need to develop a better complexity metric that considers:
	// - Actual step requirements (dependencies, validation criteria, data transformations)
	// - Historical success rates and consistency
	// - Resource usage patterns
	// - Step interdependencies and workflow context
	if validationPassed && turnCount > 0 {
		if turnCount < 100 {
			metadata.SuccessfulRunsSimple++
		} else if turnCount <= 200 {
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
	// Use cumulative runs across all complexity levels, but check against threshold for current complexity
	// This allows steps that improve over time (complex → medium → simple) to lock faster
	// Simple (< 100 turns): Lock after 3 successful runs.
	// Medium (100-200 turns): Lock after 5 successful runs.
	// Complex (> 200 turns): Lock after 10 successful runs.
	//
	// TODO: Turn-based classification is not reliable - turn count varies significantly based on
	// the LLM model used and doesn't reflect actual step complexity. We need a better complexity metric.
	shouldAutoLock := false
	var autoLockReason string

	// Calculate cumulative successful runs across all complexity levels
	totalSuccessfulRuns := metadata.SuccessfulRunsSimple + metadata.SuccessfulRunsMedium + metadata.SuccessfulRunsComplex

	// Determine current complexity from last_turn_count
	var currentComplexity string
	var threshold int
	if turnCount > 0 {
		if turnCount < 100 {
			currentComplexity = "simple"
			threshold = 3
		} else if turnCount <= 200 {
			currentComplexity = "medium"
			threshold = 5
		} else {
			currentComplexity = "complex"
			threshold = 10
		}
	} else {
		// Fallback: use last_turn_count from metadata if turnCount is 0
		if metadata.LastTurnCount > 0 {
			if metadata.LastTurnCount < 100 {
				currentComplexity = "simple"
				threshold = 3
			} else if metadata.LastTurnCount <= 200 {
				currentComplexity = "medium"
				threshold = 5
			} else {
				currentComplexity = "complex"
				threshold = 10
			}
		} else {
			// No turn count available - use simple threshold as default
			currentComplexity = "simple"
			threshold = 3
		}
	}

	// Check if cumulative runs meet the threshold for current complexity
	if totalSuccessfulRuns >= threshold {
		shouldAutoLock = true
		autoLockReason = fmt.Sprintf("threshold_reached_%s (cumulative: %d runs, threshold: %d)", currentComplexity, totalSuccessfulRuns, threshold)
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

// autoLockStepLearningsInConfig updates step_config.json to set LockLearnings = true
func (hcpo *StepBasedWorkflowOrchestrator) autoLockStepLearningsInConfig(
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
func (hcpo *StepBasedWorkflowOrchestrator) updateUnlockMetadata(
	ctx context.Context,
	stepID string,
	stepIndex int,
	stepPath string,
	learningPathIdentifier string,
	unlockReason string,
) error {
	// Use relative path - ReadWorkspaceFile/WriteWorkspaceFile auto-prepend workspacePath
	learningsBase := hcpo.getLearningsBasePath()
	metadataPath := filepath.Join(learningsBase, learningPathIdentifier, ".learning_metadata.json")

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

	// Clear auto-lock info to prevent UI from showing "Locked (Auto)" state
	// The UI checks if AutoLockedAt is set to determine if it's auto-locked
	metadata.AutoLockedAt = ""
	metadata.AutoLockReason = ""
	metadata.AutoLockIteration = 0

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
func (hcpo *StepBasedWorkflowOrchestrator) unlockStepLearningsInConfig(
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
func (hcpo *StepBasedWorkflowOrchestrator) unlockStepLearningsAndResetMetadata(
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

	// Reset metadata counter - use relative path (ReadWorkspaceFile/WriteWorkspaceFile auto-prepend workspacePath)
	learningsBase := hcpo.getLearningsBasePath()
	metadataPath := filepath.Join(learningsBase, learningPathIdentifier, ".learning_metadata.json")

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
func (hcpo *StepBasedWorkflowOrchestrator) createUnlockLearningsFunction() func(context.Context, string, int) error {
	return func(ctx context.Context, stepID string, stepIndex int) error {
		// Use step ID for learning path identifier (new format)
		learningPathIdentifier := stepID

		// Calculate step path (relative to workspace)
		stepPath := fmt.Sprintf("planning/step-%d", stepIndex+1)

		// Call the full unlock function
		return hcpo.unlockStepLearningsAndResetMetadata(ctx, stepID, stepIndex, stepPath, learningPathIdentifier)
	}
}

// Note: savePreviousLearningsToMetadata has been removed
// Previous learnings are now read directly from files before the learning phase runs
// This avoids storing large content in metadata files

// emitStepConfigUpdatedEvent emits a TodoStepsExtracted event with the updated step config
// This notifies the frontend to update the React Flow node dynamically
func (hcpo *StepBasedWorkflowOrchestrator) emitStepConfigUpdatedEvent(
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
