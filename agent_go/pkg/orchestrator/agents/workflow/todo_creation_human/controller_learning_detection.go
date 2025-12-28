package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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
	TotalIterations          int                         `json:"total_iterations"`
	ConsecutiveNoNewLearning int                         `json:"consecutive_no_new_learning"`
	LastLearningDetectedAt   string                      `json:"last_learning_detected_at,omitempty"`
	LastDetectionReasoning   string                      `json:"last_detection_reasoning,omitempty"`
	LastDetectionConfidence  float64                     `json:"last_detection_confidence,omitempty"`
	DetectionHistory         []DetectionHistoryEntry     `json:"detection_history,omitempty"`
	ConsolidationHistory     []ConsolidationHistoryEntry `json:"consolidation_history,omitempty"`
	LastConsolidationAt      string                      `json:"last_consolidation_at,omitempty"`
	// Auto-lock information
	AutoLockedAt      string `json:"auto_locked_at,omitempty"`      // Timestamp when auto-lock was triggered
	AutoLockReason    string `json:"auto_lock_reason,omitempty"`    // Reason: "consecutive_no_new_learning" or "maximum_learnings"
	AutoLockIteration int    `json:"auto_lock_iteration,omitempty"` // Iteration number when auto-lock was triggered
	// Auto-unlock information
	AutoUnlockedAt      string `json:"auto_unlocked_at,omitempty"`      // Timestamp when auto-unlock was triggered
	AutoUnlockReason    string `json:"auto_unlock_reason,omitempty"`    // Reason: "validation_failed" or "decision_step_false"
	AutoUnlockIteration int    `json:"auto_unlock_iteration,omitempty"` // Iteration number when auto-unlock was triggered
	// Note: LastConsolidationOutput removed - learning content is stored in files, not metadata
}

// detectNewLearningWithLLM runs the learning detection agent to compare old vs new learning files
// previousLearningsContent: Combined content of all learning files BEFORE the learning phase ran (read directly from files)
// step: Step information for task context (title, description, success criteria, etc.)
// usedTempLLM: Which tempLLM was used during execution ("tempLLM1", "tempLLM2", or "" for original LLM)
// validationPassed: Whether validation passed (success criteria met)
// Returns: (hasNewLearning, reasoning, confidence, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) detectNewLearningWithLLM(
	ctx context.Context,
	stepIndex int,
	stepPath string,
	learningPathIdentifier string,
	stepConfig *AgentConfigs,
	previousLearningsContent string,
	step PlanStepInterface,
	usedTempLLM string,
	validationPassed bool,
	isCodeExecutionMode bool, // Code execution mode (kept for future use, not used in detection)
) (bool, string, float64, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Running learning detection for %s", learningPathIdentifier))

	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepLearningsPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier)

	// Get current learnings (what exists now after extraction agent has consolidated)
	currentLearningFiles, err := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read current learning files: %v (treating as no new learning)", err))
		return false, "Failed to read current learning files", 0.5, nil
	}

	// Combine current learning files into single content
	currentLearningsContent, _ := hcpo.formatStepLearningFilesAsHistory(currentLearningFiles)

	// If no current learning files, the extraction agent may not have written any files
	if len(currentLearningFiles) == 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No current learning files found for %s - extraction agent may not have written files, treating as no new learning", learningPathIdentifier))
		return false, "No current learning files found (extraction agent may not have written files)", 0.5, nil
	}

	// Validate that currentLearningsContent is not empty after formatting
	currentLearningsContentTrimmed := strings.TrimSpace(currentLearningsContent)
	if currentLearningsContentTrimmed == "" {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Current learning files exist but formatted content is empty for %s - treating as no new learning", learningPathIdentifier))
		return false, "Current learning files exist but formatted content is empty", 0.5, nil
	}

	// Validate previous learnings content
	previousLearningsContentTrimmed := strings.TrimSpace(previousLearningsContent)

	// If no previous learnings, this is the first iteration - treat as new learning
	if previousLearningsContentTrimmed == "" {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No previous learnings found for %s - treating as new learning (first iteration)", learningPathIdentifier))
		return true, "First iteration - no previous learnings", 1.0, nil
	}

	// Log content sizes for debugging
	hcpo.GetLogger().Info(fmt.Sprintf("📊 Learning detection content sizes for %s - Previous: %d chars, Current: %d chars",
		learningPathIdentifier, len(previousLearningsContentTrimmed), len(currentLearningsContentTrimmed)))

	// Validate that both contents are non-empty before calling LLM
	if previousLearningsContentTrimmed == "" && currentLearningsContentTrimmed == "" {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Both previous and current learning contents are empty for %s - cannot compare, treating as no new learning", learningPathIdentifier))
		return false, "Both previous and current learning contents are empty - cannot compare", 0.5, nil
	}

	// Both previous and current learnings exist - use LLM to compare them
	// Create detection agent
	agentName := fmt.Sprintf("%s-learning-detection", learningPathIdentifier)
	detectionAgent, err := hcpo.createLearningDetectionAgent(ctx, agentName, stepConfig)
	if err != nil {
		return false, "", 0.0, fmt.Errorf("failed to create learning detection agent: %w", err)
	}

	// Prepare template variables with step context
	// Use trimmed versions to ensure we're not passing whitespace-only content
	templateVars := map[string]string{
		"PreviousLearningsContent": previousLearningsContentTrimmed,
		"CurrentLearningsContent":  currentLearningsContentTrimmed,
		"StepTitle":                step.GetTitle(),
		"StepDescription":          step.GetDescription(),
		"StepSuccessCriteria":      step.GetSuccessCriteria(),
		"StepContextOutput":        step.GetContextOutput().String(),
		"UsedTempLLM":              usedTempLLM,
		"ValidationPassed":         fmt.Sprintf("%v", validationPassed),
	}

	// Consolidation is now handled by extraction agents - no consolidation parameters needed

	// Add context dependencies as comma-separated string
	if len(step.GetContextDependencies()) > 0 {
		templateVars["StepContextDependencies"] = strings.Join(step.GetContextDependencies(), ", ")
	} else {
		templateVars["StepContextDependencies"] = ""
	}

	// Validate step context fields are not empty (critical for detection agent)
	stepTitle := strings.TrimSpace(step.GetTitle())
	stepDescription := strings.TrimSpace(step.GetDescription())
	stepSuccessCriteria := strings.TrimSpace(step.GetSuccessCriteria())

	if stepTitle == "" {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step title is empty for %s - detection may be inaccurate", learningPathIdentifier))
	}
	if stepDescription == "" {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step description is empty for %s - detection may be inaccurate", learningPathIdentifier))
	}
	if stepSuccessCriteria == "" {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step success criteria is empty for %s - detection may be inaccurate", learningPathIdentifier))
	}

	// Log template variable sizes for debugging (helps identify empty content issues)
	hcpo.GetLogger().Info(fmt.Sprintf("📊 Learning detection template variable sizes for %s - PreviousLearnings: %d chars, CurrentLearnings: %d chars, Title: %d chars, Description: %d chars, SuccessCriteria: %d chars",
		learningPathIdentifier,
		len(templateVars["PreviousLearningsContent"]),
		len(templateVars["CurrentLearningsContent"]),
		len(templateVars["StepTitle"]),
		len(templateVars["StepDescription"]),
		len(templateVars["StepSuccessCriteria"])))

	// Execute detection agent
	detectionResponse, _, err := detectionAgent.ExecuteStructured(ctx, templateVars, []llmtypes.MessageContent{})
	if err != nil {
		return false, "", 0.0, fmt.Errorf("learning detection agent execution failed: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Learning detection result for %s: has_new_learning=%v, confidence=%.2f", learningPathIdentifier, detectionResponse.HasNewLearning, detectionResponse.Confidence))

	return detectionResponse.HasNewLearning, detectionResponse.Reasoning, detectionResponse.Confidence, nil
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

	// Initialize DetectionHistory if nil (for backward compatibility with old metadata)
	if metadata.DetectionHistory == nil {
		metadata.DetectionHistory = []DetectionHistoryEntry{}
	}

	// Initialize ConsolidationHistory if nil (for backward compatibility with old metadata)
	if metadata.ConsolidationHistory == nil {
		metadata.ConsolidationHistory = []ConsolidationHistoryEntry{}
	}

	// Update metadata
	metadata.TotalIterations++
	if hasNewLearning {
		// Reset counter when new learning is detected
		metadata.ConsecutiveNoNewLearning = 0
		metadata.LastLearningDetectedAt = time.Now().Format(time.RFC3339)
		metadata.LastDetectionReasoning = reasoning
		metadata.LastDetectionConfidence = confidence
	} else {
		// Increment counter when no new learning
		metadata.ConsecutiveNoNewLearning++
		metadata.LastDetectionReasoning = reasoning
		metadata.LastDetectionConfidence = confidence
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

	// Limit history to last 50 entries to prevent unbounded growth
	const maxHistoryEntries = 50
	if len(metadata.DetectionHistory) > maxHistoryEntries {
		// Keep only the last maxHistoryEntries entries
		metadata.DetectionHistory = metadata.DetectionHistory[len(metadata.DetectionHistory)-maxHistoryEntries:]
	}

	// Check if auto-lock should be triggered
	// Condition 1: 3 consecutive iterations with no new learning (ConsecutiveNoNewLearning >= 2 means 2, 3, 4...)
	// Condition 2: 10 maximum learnings (TotalIterations >= 10 means after 10 learning attempts, always lock)
	shouldAutoLock := metadata.ConsecutiveNoNewLearning >= 2 || metadata.TotalIterations >= 10

	if shouldAutoLock {
		// Determine which condition triggered the auto-lock
		var autoLockReason string
		if metadata.TotalIterations >= 10 {
			autoLockReason = "maximum_learnings"
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-lock threshold reached for %s: %d total iterations (maximum learnings reached)", learningPathIdentifier, metadata.TotalIterations))
		} else {
			autoLockReason = "consecutive_no_new_learning"
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-lock threshold reached for %s: %d consecutive iterations with no new learning", learningPathIdentifier, metadata.ConsecutiveNoNewLearning))
		}

		// Save auto-lock information to metadata
		metadata.AutoLockedAt = time.Now().Format(time.RFC3339)
		metadata.AutoLockReason = autoLockReason
		metadata.AutoLockIteration = metadata.TotalIterations
	}

	// Write updated metadata (with auto-lock info if triggered)
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

// createLearningDetectionAgent creates a learning detection agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) createLearningDetectionAgent(
	ctx context.Context,
	agentName string,
	stepConfig *AgentConfigs,
) (*HumanControlledTodoPlannerLearningDetectionAgent, error) {
	// Fixed to 25 (not configurable)
	maxTurns := 25

	// Use learning LLM config (same as learning agents) - Priority: step config > preset default > orchestrator default
	// Since detection agent now also performs consolidation (which was previously done by learning consolidation agent),
	// it should use the same LLM as other learning agents
	llmConfig := hcpo.selectLearningLLM(stepConfig)

	// Create agent config with custom LLM
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Detection agent uses NoServers (pure LLM analysis)
	config.ServerNames = []string{mcpclient.NoServers}

	// Disable code execution mode for detection agent
	config.UseCodeExecutionMode = false
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Learning detection agent: Code execution mode explicitly disabled (config.UseCodeExecutionMode = false)"))

	// Disable large output virtual tools (context offloading) for learning detection agent
	// Detection agent should not offload its outputs to prevent issues with learning content comparison
	disabled := false
	config.EnableContextOffloading = &disabled
	hcpo.GetLogger().Info("🔧 Disabling large output virtual tools (context offloading) for learning detection agent")

	// Detection agent doesn't need tools (pure LLM analysis)
	toolsToRegister := []llmtypes.Tool{}
	executorsToUse := make(map[string]interface{})

	// Use base factory! (This handles all setup automatically)
	agentInterface, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"learning_detection",
		0, // step
		0, // iteration
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerLearningDetectionAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister, // Empty - detection agent doesn't use tools
		executorsToUse,  // Empty - detection agent doesn't use tools
		false,           // Don't overwrite system prompt - detection agent manages its own prompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup learning detection agent: %w", err)
	}

	// Type assert to specific agent type
	agent, ok := agentInterface.(*HumanControlledTodoPlannerLearningDetectionAgent)
	if !ok {
		return nil, fmt.Errorf("failed to type assert learning detection agent")
	}

	return agent, nil
}
