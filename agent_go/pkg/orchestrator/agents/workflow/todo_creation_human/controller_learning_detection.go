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
	"mcpagent/mcpclient"

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
	Output             string `json:"output"`
	NewLearningContent string `json:"new_learning_content,omitempty"` // Content from _learning_new.md before consolidation
	FilesConsolidated  int    `json:"files_consolidated,omitempty"`
	FilesDeleted       int    `json:"files_deleted,omitempty"`
	PatternsMerged     int    `json:"patterns_merged,omitempty"`
	PatternsUpdated    int    `json:"patterns_updated,omitempty"`
	OptimalPathsMarked int    `json:"optimal_paths_marked,omitempty"`
	UnreliableMarked   int    `json:"unreliable_marked,omitempty"`
	ConsolidatedFile   string `json:"consolidated_file,omitempty"`
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
	LastConsolidationOutput  string                      `json:"last_consolidation_output,omitempty"`
}

// detectNewLearningWithLLM runs the learning detection agent to compare old vs new learning files
// previousLearningsContent: Combined content of all learning files BEFORE the learning phase ran (read directly from files)
// step: Step information for task context (title, description, success criteria, etc.)
// Returns: (hasNewLearning, reasoning, confidence, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) detectNewLearningWithLLM(
	ctx context.Context,
	stepIndex int,
	stepPath string,
	learningPathIdentifier string,
	stepConfig *AgentConfigs,
	previousLearningsContent string,
	step *TodoStep,
) (bool, string, float64, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Running learning detection for %s", learningPathIdentifier))

	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepLearningsPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier)

	// Get current learnings (what exists now after learning agent has run)
	currentLearningFiles, err := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read current learning files: %v (treating as no new learning)", err))
		return false, "Failed to read current learning files", 0.5, nil
	}

	// Combine current learning files into single content
	currentLearningsContent, _ := hcpo.formatStepLearningFilesAsHistory(currentLearningFiles)

	// If no current learning files, the learning agent may not have written any files
	if len(currentLearningFiles) == 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No current learning files found for %s - learning may not have written files, treating as no new learning", learningPathIdentifier))
		return false, "No current learning files found (learning may not have written files)", 0.5, nil
	}

	// If no previous learnings, this is the first iteration - treat as new learning
	if previousLearningsContent == "" {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No previous learnings found for %s - treating as new learning (first iteration)", learningPathIdentifier))
		return true, "First iteration - no previous learnings", 1.0, nil
	}

	// Both previous and current learnings exist - use LLM to compare them
	// Create detection agent
	agentName := fmt.Sprintf("%s-learning-detection", learningPathIdentifier)
	detectionAgent, err := hcpo.createLearningDetectionAgent(ctx, agentName, stepConfig)
	if err != nil {
		return false, "", 0.0, fmt.Errorf("failed to create learning detection agent: %w", err)
	}

	// Prepare template variables with step context
	templateVars := map[string]string{
		"PreviousLearningsContent": previousLearningsContent,
		"CurrentLearningsContent":  currentLearningsContent,
		"StepTitle":                step.Title,
		"StepDescription":          step.Description,
		"StepSuccessCriteria":      step.SuccessCriteria,
		"StepContextOutput":        step.ContextOutput,
		"TaskObjective":            hcpo.GetObjective(),
	}

	// Add context dependencies as comma-separated string
	if len(step.ContextDependencies) > 0 {
		templateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
	} else {
		templateVars["StepContextDependencies"] = ""
	}

	// Execute detection agent
	detectionResponse, _, err := detectionAgent.ExecuteStructured(ctx, templateVars, []llmtypes.MessageContent{})
	if err != nil {
		return false, "", 0.0, fmt.Errorf("learning detection agent execution failed: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Learning detection result for %s: has_new_learning=%v, confidence=%.2f", learningPathIdentifier, detectionResponse.HasNewLearning, detectionResponse.Confidence))

	return detectionResponse.HasNewLearning, detectionResponse.Reasoning, detectionResponse.Confidence, nil
}

// updateLearningMetadata updates the learning metadata file with detection results
// Returns true if auto-lock should be triggered (consecutive_no_new_learning >= 2)
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

	// Write updated metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return false, fmt.Errorf("failed to marshal learning metadata: %w", err)
	}

	if err := hcpo.BaseOrchestrator.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); err != nil {
		return false, fmt.Errorf("failed to write learning metadata: %w", err)
	}

	// Check if auto-lock should be triggered
	shouldAutoLock := metadata.ConsecutiveNoNewLearning >= 2

	if shouldAutoLock {
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-lock threshold reached for %s: %d consecutive iterations with no new learning", learningPathIdentifier, metadata.ConsecutiveNoNewLearning))
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

	// Create consolidation entry
	consolidationEntry := ConsolidationHistoryEntry{
		Iteration:          iteration,
		Timestamp:          time.Now().Format(time.RFC3339),
		Output:             consolidationOutput,
		NewLearningContent: newLearningContent,
		ConsolidatedFile:   extractConsolidatedFilePath(consolidationOutput),
	}

	// Add consolidation result to history
	metadata.ConsolidationHistory = append(metadata.ConsolidationHistory, consolidationEntry)

	// Update last consolidation fields
	metadata.LastConsolidationAt = time.Now().Format(time.RFC3339)
	metadata.LastConsolidationOutput = consolidationOutput

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
	// Look for common patterns like "learnings/step-X/..._learning.md"
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
		// Calculate learning path identifier (1-based)
		learningPathIdentifier := fmt.Sprintf("step-%d", stepIndex+1)

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
		// Calculate learning path identifier (1-based)
		learningPathIdentifier := fmt.Sprintf("step-%d", stepIndex+1)

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

	// Read plan.json to get the step
	planPath := filepath.Join(hcpo.GetWorkspacePath(), "plan.json")
	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Reading plan.json from: %s", planPath))
	planContent, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [emitStepConfigUpdatedEvent] Failed to read plan.json: %v", err))
		return fmt.Errorf("failed to read plan.json: %w", err)
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Successfully read plan.json (size: %d bytes)", len(planContent)))

	var plan struct {
		Steps []struct {
			ID                  string   `json:"id"`
			Title               string   `json:"title"`
			Description         string   `json:"description"`
			SuccessCriteria     string   `json:"success_criteria"`
			ContextDependencies []string `json:"context_dependencies"`
			ContextOutput       string   `json:"context_output"`
			HasLoop             bool     `json:"has_loop"`
			LoopCondition       string   `json:"loop_condition"`
			MaxIterations       int      `json:"max_iterations"`
			LoopDescription     string   `json:"loop_description"`
			HasCondition        bool     `json:"has_condition"`
			ConditionQuestion   string   `json:"condition_question"`
			ConditionContext    string   `json:"condition_context"`
		} `json:"steps"`
	}

	if err := json.Unmarshal([]byte(planContent), &plan); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [emitStepConfigUpdatedEvent] Failed to parse plan.json: %v", err))
		return fmt.Errorf("failed to parse plan.json: %w", err)
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Successfully parsed plan.json (found %d steps)", len(plan.Steps)))

	// Find the step by ID
	var foundStep *struct {
		ID                  string   `json:"id"`
		Title               string   `json:"title"`
		Description         string   `json:"description"`
		SuccessCriteria     string   `json:"success_criteria"`
		ContextDependencies []string `json:"context_dependencies"`
		ContextOutput       string   `json:"context_output"`
		HasLoop             bool     `json:"has_loop"`
		LoopCondition       string   `json:"loop_condition"`
		MaxIterations       int      `json:"max_iterations"`
		LoopDescription     string   `json:"loop_description"`
		HasCondition        bool     `json:"has_condition"`
		ConditionQuestion   string   `json:"condition_question"`
		ConditionContext    string   `json:"condition_context"`
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Searching for step ID: %s (available step IDs: %v)", stepID, func() []string {
		ids := make([]string, 0, len(plan.Steps))
		for _, s := range plan.Steps {
			ids = append(ids, s.ID)
		}
		return ids
	}()))

	for i := range plan.Steps {
		if plan.Steps[i].ID == stepID {
			foundStep = &plan.Steps[i]
			hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Found step: %s (title: %s)", stepID, foundStep.Title))
			break
		}
	}

	if foundStep == nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [emitStepConfigUpdatedEvent] Step %s not found in plan.json", stepID))
		return fmt.Errorf("step %s not found in plan.json", stepID)
	}

	// Convert to TodoStep format
	todoStep := TodoStep{
		ID:                  foundStep.ID,
		Title:               foundStep.Title,
		Description:         foundStep.Description,
		SuccessCriteria:     foundStep.SuccessCriteria,
		ContextDependencies: foundStep.ContextDependencies,
		ContextOutput:       foundStep.ContextOutput,
		HasLoop:             foundStep.HasLoop,
		LoopCondition:       foundStep.LoopCondition,
		MaxIterations:       foundStep.MaxIterations,
		LoopDescription:     foundStep.LoopDescription,
		HasCondition:        foundStep.HasCondition,
		ConditionQuestion:   foundStep.ConditionQuestion,
		ConditionContext:    foundStep.ConditionContext,
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📤 [emitStepConfigUpdatedEvent] Created TodoStep for step: %s", stepID))

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
		[]TodoStep{todoStep},
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
	// Determine max turns (default: 10 for detection - faster than learning)
	maxTurns := 10
	if stepConfig != nil && stepConfig.LearningMaxTurns != nil {
		maxTurns = *stepConfig.LearningMaxTurns
		// Cap at 20 for detection (should be fast)
		if maxTurns > 20 {
			maxTurns = 20
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific learning max turns for detection: %d", maxTurns))
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: []string{},
			APIKeys:        orchestratorLLMConfig.APIKeys,
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific learning LLM for detection: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID))
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: []string{},
			APIKeys:        orchestratorLLMConfig.APIKeys,
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default learning LLM for detection: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID))
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default learning LLM for detection: %s/%s", llmConfig.Provider, llmConfig.ModelID))
	}

	// Create agent config with custom LLM
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Detection agent uses NoServers (pure LLM analysis)
	config.ServerNames = []string{mcpclient.NoServers}

	// Disable code execution mode for detection agent
	config.UseCodeExecutionMode = false

	// Disable large output virtual tools (context offloading) for learning detection agent
	// Detection agent should not offload its outputs to prevent issues with learning content comparison
	disabled := false
	config.EnableLargeOutputVirtualTools = &disabled
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling large output virtual tools (context offloading) for learning detection agent"))

	// Create agent
	agent := NewHumanControlledTodoPlannerLearningDetectionAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize agent
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize learning detection agent: %w", err)
	}

	// Connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext("learning_detection", 0, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Info(fmt.Sprintf("🔗 Context-aware bridge connected to learning detection agent (%s)", baseAgentName))
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	return agent, nil
}
