package step_based_workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// hashStepDescription returns a short hex SHA256 of the step description,
// trimmed of surrounding whitespace. Used by the auto-lock gate to detect
// whether learnings are converging on a stable description or whether the
// description keeps drifting (which should reset the lock counter).
func hashStepDescription(desc string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(desc)))
	return hex.EncodeToString(sum[:])
}

// DetectionHistoryEntry represents a single learning detection result
type DetectionHistoryEntry struct {
	Iteration      int     `json:"iteration"`
	Timestamp      string  `json:"timestamp"`
	HasNewLearning bool    `json:"has_new_learning"`
	Reasoning      string  `json:"reasoning"`
	Confidence     float64 `json:"confidence"`
}

// GlobalLearningID is the identifier used for the workflow-level global learning folder
const GlobalLearningID = "_global"

// LearningMetadata represents the learning metadata stored per step
type LearningMetadata struct {
	StepID              string `json:"step_id"`
	StepPath            string `json:"step_path"`
	LearningContentHash string `json:"learning_content_hash,omitempty"` // SHA256 of SKILL.md contents — if changed, force exploration mode
	TotalIterations     int    `json:"total_iterations"`
	SuccessfulRuns      int    `json:"successful_runs"`                 // Total count of successful runs (all description versions, observability only)
	LastDescriptionHash string `json:"last_description_hash,omitempty"` // SHA256 of step.GetDescription(); resets DescriptionHashRuns when it changes
	DescriptionHashRuns int    `json:"description_hash_runs,omitempty"` // Successful runs accumulated under LastDescriptionHash. Auto-lock gate fires at 3.
	FailureLearningRuns int    `json:"failure_learning_runs"`           // Count of failure learning runs (persisted across iterations)
	LastTurnCount       int    `json:"last_turn_count"`                 // Last recorded TurnCount
	LastExecutionLLM    string `json:"last_execution_llm,omitempty"`
	LastLearningLLM     string `json:"last_learning_llm,omitempty"`
	// Detection tracking
	LastLearningDetectedAt  string                  `json:"last_learning_detected_at,omitempty"`
	LastDetectionReasoning  string                  `json:"last_detection_reasoning,omitempty"`
	LastDetectionConfidence float64                 `json:"last_detection_confidence,omitempty"`
	DetectionHistory        []DetectionHistoryEntry `json:"detection_history,omitempty"`
	// Auto-lock information
	AutoLockedAt      string `json:"auto_locked_at,omitempty"`
	AutoLockReason    string `json:"auto_lock_reason,omitempty"`
	AutoLockIteration int    `json:"auto_lock_iteration,omitempty"`
	// Auto-unlock information
	AutoUnlockedAt      string `json:"auto_unlocked_at,omitempty"`
	AutoUnlockReason    string `json:"auto_unlock_reason,omitempty"`
	AutoUnlockIteration int    `json:"auto_unlock_iteration,omitempty"`
	// Adaptive execution tiering (Tier 1 High vs Tier 2 Medium) for execution agents.
	// The step starts on High, may promote to Medium after stable successful runs,
	// and falls back to High immediately if a Medium attempt fails.
	PreferredExecutionTier              string `json:"preferred_execution_tier,omitempty"`                 // "high" | "medium"
	LastExecutionTier                   string `json:"last_execution_tier,omitempty"`                      // Last execution tier actually used ("high" | "medium")
	MediumSuccessStreak                 int    `json:"medium_success_streak,omitempty"`                    // Consecutive successful executions on Medium
	HighSuccessStreakSinceMediumFailure int    `json:"high_success_streak_since_medium_failure,omitempty"` // Recovery successes required before retrying Medium
	LastMediumFailureAt                 string `json:"last_medium_failure_at,omitempty"`                   // Timestamp of the last Medium-tier failure
	LastTierDecisionReason              string `json:"last_tier_decision_reason,omitempty"`                // Human-readable reason for current preference
	LastTierChangeAt                    string `json:"last_tier_change_at,omitempty"`                      // Timestamp when PreferredExecutionTier last changed
	// Global learning: per-step contribution tracking (only used when StepID == "_global")
	// Maps step ID -> number of times that step has contributed to the global skill
	StepContributions map[string]int `json:"step_contributions,omitempty"`
}

// getLearningsBasePath returns the learnings base path. Both execution and
// evaluation steps share the "learnings/" namespace — step-ID uniqueness
// across plan.json and evaluation_plan.json is enforced by
// validateCrossPlanStepIDUniqueness, so there is no collision risk.
func (hcpo *StepBasedWorkflowOrchestrator) getLearningsBasePath() string {
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

// MetadataUpdateResult describes side effects the caller must apply to step_config.json.
// Kept intentionally small: the metadata update itself is already written before this is returned.
type MetadataUpdateResult struct {
	ShouldAutoLock   bool   // Hash-stable threshold reached OR max-iterations fallback
	AutoLockReason   string // Human-readable reason to write into review_notes
	ShouldAutoUnlock bool   // Step was auto-locked and description hash changed → unlock
	AutoUnlockReason string // Human-readable reason
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
) (MetadataUpdateResult, error) {
	// Set inside the success branch when a previously-auto-locked step sees its
	// description hash change. Consumed by the auto-unlock block below.
	var descriptionChangedWhileAutoLocked bool
	// Use relative path - ReadWorkspaceFile/WriteWorkspaceFile auto-prepend workspacePath
	learningsBase := hcpo.getLearningsBasePath()
	metadataPath := filepath.Join(learningsBase, learningPathIdentifier, ".learning_metadata.json")

	// Read existing metadata or create new
	var metadata LearningMetadata
	content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, metadataPath)
	if err != nil {
		// Metadata doesn't exist - create new
		metadata = LearningMetadata{
			StepID:          learningPathIdentifier,
			StepPath:        stepPath,
			TotalIterations: 0,
		}
	} else {
		// Parse existing metadata
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse learning metadata: %v (creating new)", err))
			metadata = LearningMetadata{
				StepID:          learningPathIdentifier,
				StepPath:        stepPath,
				TotalIterations: 0,
			}
		}
	}

	// Initialize slices if nil
	if metadata.DetectionHistory == nil {
		metadata.DetectionHistory = []DetectionHistoryEntry{}
	}

	// Update common fields
	metadata.TotalIterations++
	metadata.LastTurnCount = turnCount
	metadata.LastExecutionLLM = executionLLM
	metadata.LastLearningLLM = learningLLM
	if hasNewLearning {
		metadata.LastLearningDetectedAt = time.Now().Format(time.RFC3339)
	}
	metadata.LastDetectionReasoning = reasoning
	metadata.LastDetectionConfidence = confidence

	// Increment successful run counter on successful validation
	if validationPassed && turnCount > 0 {
		metadata.SuccessfulRuns++
		// Sync successful run count to step_config.json so it's visible alongside optimized flag
		if step != nil {
			hcpo.syncSuccessfulRunsToStepConfig(ctx, step.GetID(), metadata.SuccessfulRuns)
		}

		// Description-hash-scoped successful-run counter — drives auto-lock.
		// If the step description has changed since the last successful run,
		// the learnings accumulated against the prior description are stale,
		// so the counter resets. Only when the SAME description accumulates
		// 3 successful runs do we consider learnings converged.
		if step != nil {
			currentHash := hashStepDescription(step.GetDescription())
			if metadata.LastDescriptionHash != currentHash {
				if metadata.LastDescriptionHash != "" {
					hcpo.GetLogger().Info(fmt.Sprintf("🔁 Step description changed for %s (hash %s → %s) — resetting description_hash_runs", learningPathIdentifier, metadata.LastDescriptionHash[:8], currentHash[:8]))
					// If this step was previously auto-locked, the stored learnings were
					// frozen against a description that no longer matches. Flag for
					// auto-unlock so the post-update caller clears LockLearnings in
					// step_config.json and the countdown restarts. We only auto-unlock
					// what we auto-locked — manual locks (AutoLockedAt empty) stay put.
					if metadata.AutoLockedAt != "" {
						descriptionChangedWhileAutoLocked = true
					}
				}
				metadata.LastDescriptionHash = currentHash
				metadata.DescriptionHashRuns = 0
			}
			metadata.DescriptionHashRuns++
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

	// Check if auto-lock should be triggered.
	// Global learning: never auto-lock — human decides when to lock.
	// Per-step learning: lock after 3 successful runs against the SAME step
	// description hash. When the description changes, DescriptionHashRuns
	// resets and the lock countdown starts over — so edits to the description
	// invalidate the "converged" signal the lock is based on.
	shouldAutoLock := false
	var autoLockReason string

	if learningPathIdentifier != GlobalLearningID {
		threshold := 3

		if metadata.DescriptionHashRuns >= threshold && metadata.LastDescriptionHash != "" {
			shouldAutoLock = true
			autoLockReason = fmt.Sprintf("description_hash_stable (%d successful runs on hash %s)", metadata.DescriptionHashRuns, metadata.LastDescriptionHash[:8])
		}

		// Fallback to max iterations (safety)
		if !shouldAutoLock && metadata.TotalIterations >= 15 {
			shouldAutoLock = true
			autoLockReason = "maximum_learnings_reached"
		}
	}

	if shouldAutoLock {
		metadata.AutoLockedAt = time.Now().Format(time.RFC3339)
		metadata.AutoLockReason = autoLockReason
		metadata.AutoLockIteration = metadata.TotalIterations
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-lock threshold reached for %s: %s", learningPathIdentifier, autoLockReason))
	}

	// Auto-unlock: step was auto-locked earlier, but now the description hash
	// has changed — the stored learnings are against a stale description.
	// Clear the auto-lock markers in metadata so step_config.json can be
	// unlocked in parallel by the caller.
	result := MetadataUpdateResult{
		ShouldAutoLock: shouldAutoLock,
		AutoLockReason: autoLockReason,
	}
	if descriptionChangedWhileAutoLocked && !shouldAutoLock {
		unlockReason := fmt.Sprintf("description_changed (new hash %s) — prior auto-lock invalidated", metadata.LastDescriptionHash[:8])
		metadata.AutoUnlockedAt = time.Now().Format(time.RFC3339)
		metadata.AutoUnlockReason = unlockReason
		metadata.AutoUnlockIteration = metadata.TotalIterations
		// Clear the auto-lock record so future description changes only trigger
		// unlock when a newer auto-lock exists to invalidate.
		metadata.AutoLockedAt = ""
		metadata.AutoLockReason = ""
		metadata.AutoLockIteration = 0
		result.ShouldAutoUnlock = true
		result.AutoUnlockReason = unlockReason
		hcpo.GetLogger().Info(fmt.Sprintf("🔓 Auto-unlock triggered for %s: %s", learningPathIdentifier, unlockReason))
	}

	// Write updated metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return MetadataUpdateResult{}, fmt.Errorf("failed to marshal learning metadata: %w", err)
	}

	if err := hcpo.BaseOrchestrator.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); err != nil {
		return MetadataUpdateResult{}, fmt.Errorf("failed to write learning metadata: %w", err)
	}

	return result, nil
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

	// Validate prerequisites before auto-locking and marking optimized
	// 1. Check learnings exist
	learningsPath := getLearningFolderPathByStepID("", stepID, "", hcpo.isEvaluationMode)
	learningFiles, _ := hcpo.readStepLearningFiles(ctx, learningsPath)
	if len(learningFiles) == 0 {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Skipping auto-lock for step %s: no learning files found in %s", stepID, learningsPath))
		return nil
	}

	// 2. Check pre-validation schema exists in plan
	if hcpo.approvedPlan != nil {
		if foundStep := hcpo.findStepInPlan(hcpo.approvedPlan.Steps, stepID); foundStep != nil {
			schema := foundStep.GetValidationSchema()
			if schema == nil || len(schema.Files) == 0 {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Skipping auto-lock for step %s: no pre-validation schema defined in plan", stepID))
				return nil
			}
		}
	}

	// Set LockLearnings = true AND Optimized = true together.
	// When the skill is built (3 runs on the same description hash), the step is
	// flagged optimized. NOTE: Optimized is currently a UI/reporting flag only —
	// it does NOT change runtime LLM tier selection. Learning stays on Tier 2
	// (Medium) and execution stays on Tier 1 (High) regardless. If you later
	// want optimized steps to use cheaper LLMs, wire it into ResolveForLearning
	// / ResolveForExecution in tiered_llm.go.
	lockValue := true
	stepConfig.AgentConfigs.LockLearnings = &lockValue
	stepConfig.AgentConfigs.Optimized = &lockValue
	if strings.TrimSpace(stepConfig.AgentConfigs.ReviewNotes) == "" {
		stepConfig.AgentConfigs.ReviewNotes = "Auto-locked after 3 successful runs against the same step description hash (learnings converged). Edit the description to invalidate and regenerate."
	}

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

// autoUnlockStepLearningsInConfig clears LockLearnings and Optimized in step_config.json
// when a previously auto-locked step's description has changed (its stored learnings no
// longer match the current description). Only invoked from the description-hash change
// path; manual locks are left alone (the caller already checked metadata.AutoLockedAt).
func (hcpo *StepBasedWorkflowOrchestrator) autoUnlockStepLearningsInConfig(
	ctx context.Context,
	stepID string,
	reasoning string,
) error {
	hcpo.GetLogger().Info(fmt.Sprintf("🔓 Auto-unlocking learnings for step %s in step_config.json — %s", stepID, reasoning))

	configs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		return fmt.Errorf("failed to read step configs: %w", err)
	}

	found := false
	for i := range configs {
		if configs[i].ID != stepID {
			continue
		}
		found = true
		if configs[i].AgentConfigs == nil {
			// Nothing to unlock.
			return nil
		}
		// Clear the auto-lock + optimized markers. Optimized is cleared because
		// it moves in lock-step with LockLearnings in autoLockStepLearningsInConfig;
		// keeping it set after unlock would leave the step flagged "optimized" with
		// stale learnings — worst of both worlds.
		configs[i].AgentConfigs.LockLearnings = nil
		configs[i].AgentConfigs.Optimized = nil
		// Only overwrite review_notes if it still carries the auto-lock wording;
		// if a human edited it we preserve their content.
		if strings.HasPrefix(strings.TrimSpace(configs[i].AgentConfigs.ReviewNotes), "Auto-locked after") {
			configs[i].AgentConfigs.ReviewNotes = fmt.Sprintf("Auto-unlocked: %s. Learnings will regenerate on the next successful runs.", reasoning)
		}
		break
	}
	if !found {
		// Step config entry didn't exist — nothing to unlock. Safe no-op.
		return nil
	}

	if err := hcpo.WriteStepConfigs(ctx, configs); err != nil {
		return fmt.Errorf("failed to write step configs: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Auto-unlocked learnings for step %s (LockLearnings cleared, Optimized cleared)", stepID))

	if err := hcpo.emitStepConfigUpdatedEvent(ctx, stepID); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit step config updated event: %v", err))
	}
	return nil
}

// syncSuccessfulRunsToStepConfig updates the successful_runs count in step_config.json
// so it's visible alongside the optimized flag for the workflow builder.
func (hcpo *StepBasedWorkflowOrchestrator) syncSuccessfulRunsToStepConfig(ctx context.Context, stepID string, count int) {
	configs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step configs for syncing successful_runs: %v", err))
		return
	}

	found := false
	for i := range configs {
		if configs[i].ID == stepID {
			if configs[i].AgentConfigs == nil {
				configs[i].AgentConfigs = &AgentConfigs{}
			}
			configs[i].AgentConfigs.SuccessfulRuns = &count
			found = true
			break
		}
	}

	if !found {
		newConfig := StepConfig{
			ID:           stepID,
			AgentConfigs: &AgentConfigs{SuccessfulRuns: &count},
		}
		configs = append(configs, newConfig)
	}

	if err := hcpo.WriteStepConfigs(ctx, configs); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to sync successful_runs to step_config.json: %v", err))
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
