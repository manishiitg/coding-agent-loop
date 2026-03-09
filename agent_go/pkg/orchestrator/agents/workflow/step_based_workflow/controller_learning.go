package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/shared"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// runSuccessLearningPhase analyzes successful executions to capture best practices and improve plan.json
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
// usedTempLLM: Which tempLLM was used during execution ("tempLLM1", "tempLLM2", or "" for original LLM)
func (hcpo *StepBasedWorkflowOrchestrator) runSuccessLearningPhase(ctx context.Context, stepIndex int, stepPath string, learningPathIdentifier string, totalSteps int, step PlanStepInterface, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse, isCodeExecutionMode bool, usedTempLLM string, turnCount int, executionLLM string, triggerReason string) error {
	// Get agent configs once at the start
	agentConfigs := getAgentConfigs(step)

	// Use step-specific learning detail level, default to "exact" if not set
	learningDetailLevel := "exact" // default
	if agentConfigs != nil && agentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = agentConfigs.LearningDetailLevel
	}

	// AUTO-UNLOCK LEARNINGS: If validation failed, automatically unlock learnings so the step can learn from the failure
	// This ensures that when validation fails, learnings are unlocked even if they were previously locked
	validationFailed := validationResponse != nil && !validationResponse.IsSuccessCriteriaMet
	if validationFailed {
		isLearningsLocked := agentConfigs != nil && agentConfigs.LockLearnings != nil && *agentConfigs.LockLearnings
		if isLearningsLocked {
			hcpo.GetLogger().Info(fmt.Sprintf("🔓 Validation failed - auto-unlocking learnings for step %s so it can learn from the failure", step.GetID()))
			if unlockErr := hcpo.unlockStepLearningsInConfig(ctx, step.GetID()); unlockErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to auto-unlock learnings for step %s: %v", step.GetID(), unlockErr))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Auto-unlocked learnings for step %s (validation failed)", step.GetID()))
				// Update unlock metadata
				if metadataErr := hcpo.updateUnlockMetadata(ctx, step.GetID(), stepIndex, stepPath, learningPathIdentifier, "validation_failed"); metadataErr != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update unlock metadata for step %s: %v", step.GetID(), metadataErr))
				}
				// Update agentConfigs to reflect the unlock (for this function's execution)
				if agentConfigs != nil {
					lockValue := false
					agentConfigs.LockLearnings = &lockValue
				}
			}
		}
	}

	// Helper function to update metadata with turnCount when learning is skipped
	updateMetadataWhenSkipped := func(skipReason string) error {
		// Determine which LLM would have been used (for metadata tracking)
		learningLLMConfig := hcpo.selectLearningLLM(ctx, agentConfigs, step.GetID(), stepPath)
		if learningLLMConfig == nil {
			err := fmt.Errorf("no valid LLM configuration found for learning agent")
			hcpo.GetLogger().Error("❌ No valid LLM configuration found for learning agent, skipping metadata update", err)
			return err
		}
		learningLLM := fmt.Sprintf("%s/%s", learningLLMConfig.Primary.Provider, learningLLMConfig.Primary.ModelID)

		// Update metadata with turnCount but don't increment counters (learning was skipped)
		// We still want to record last_turn_count for complexity tracking
		// Note: validationPassed is set to false here NOT because validation failed, but because
		// we want to prevent counter increments when learning is skipped. The turnCount is still
		// recorded for complexity determination purposes.
		_, metadataErr := hcpo.updateLearningMetadataWithTurnCount(
			ctx,
			stepIndex,
			stepPath,
			learningPathIdentifier,
			false, // hasNewLearning = false (learning was skipped)
			fmt.Sprintf("Learning skipped: %s (turnCount recorded for complexity tracking)", skipReason),
			0.0, // confidence = 0 (not applicable when skipped)
			turnCount,
			step,
			false, // validationPassed = false (don't increment counters when learning is skipped, even though validation may have passed)
			executionLLM,
			learningLLM,
		)
		if metadataErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata (skipped) for %s: %v", learningPathIdentifier, metadataErr))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("📊 Recorded turnCount (%d) for %s (learning skipped: %s)", turnCount, learningPathIdentifier, skipReason))
		}
		return metadataErr
	}

	// LIMIT SUCCESS LEARNING: Check if we already have sufficient successful learnings (>= 3)
	// If so, skip success learning but keep unlocked to allow failure learning
	// We check cumulative successful runs across all complexities
	metadata, err := hcpo.GetLearningMetadata(ctx, learningPathIdentifier)
	if err == nil && metadata != nil {
		totalSuccessfulLearnings := metadata.SuccessfulRunsSimple + metadata.SuccessfulRunsMedium + metadata.SuccessfulRunsComplex
		if totalSuccessfulLearnings >= 3 {
			hcpo.GetLogger().Info(fmt.Sprintf("🧠 Sufficient success learnings captured (%d >= 3) for %s - skipping success learning agent", totalSuccessfulLearnings, learningPathIdentifier))
			// Skip learning but record turnCount (without incrementing counters)
			// This effectively "locks" success learning but keeps the step unlocked for failure learning
			_ = updateMetadataWhenSkipped(fmt.Sprintf("sufficient success learnings (%d >= 3)", totalSuccessfulLearnings))
			return nil
		}
	} else if err != nil {
		// Log warning but continue if metadata read fails (assume 0 learnings)
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read learning metadata for limit check: %v (continuing)", err))
	}

	// LOCK LEARNINGS: Check if learnings are locked (prevents learning agent from running but still uses existing learnings)
	// Note: Lock learnings takes precedence - even in code execution mode, if learnings are locked, skip learning agent
	// EXCEPTION: If learnings are locked but learnings don't exist, still run learning to create initial learnings
	isLearningsLocked := agentConfigs != nil && agentConfigs.LockLearnings != nil && *agentConfigs.LockLearnings
	if isLearningsLocked {
		// Check if learnings folder exists and has content
		learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, step.GetID(), stepIndex, stepPath)
		if err != nil {
			// If we can't check, assume empty and run learning
		} else if learningsEmpty {
			// Learnings are locked but folder is empty - run learning to create initial learnings
		} else {
			// Learnings are locked and learnings exist - skip learning but record turnCount
			_ = updateMetadataWhenSkipped("learnings locked")
			return nil
		}
	}

	// Skip learning if "none" is selected or learning is disabled
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	// Use the provided step-specific code execution mode (already computed with step-level priority)
	shouldSkipLearning := (learningDetailLevel == "none" || (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning)) && !isCodeExecutionMode
	if shouldSkipLearning {
		// Learning is disabled - skip learning but record turnCount
		_ = updateMetadataWhenSkipped("learning disabled")
		return nil
	}
	if isCodeExecutionMode && (learningDetailLevel == "none" || (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning)) {
		// Override learning detail level to "exact" if it was "none"
		if learningDetailLevel == "none" {
			learningDetailLevel = "exact"
		}
	}

	// Success learning agent ALWAYS runs - it writes learnings (creates folder if needed)
	// Only the learning reading agent (which reads existing learnings) should check folder existence
	hcpo.GetLogger().Info(fmt.Sprintf("🧠 Starting success learning analysis for %s/%d: %s", learningPathIdentifier, totalSteps, step.GetTitle()))

	// Log learning start
	_ = hcpo.logLearningExecution(ctx, stepPath, map[string]interface{}{
		"type":             "learning_start",
		"step_path":        stepPath,
		"learning_type":    "success",
		"learning_path_id": learningPathIdentifier,
		"timestamp":        shared.GetTimestamp(),
	})

	// Read previous learnings BEFORE learning phase runs (for comparison after learning phase completes)
	// This captures the state before the learning agent potentially modifies the files
	// Use RELATIVE path - workspace functions auto-prepend workspacePath
	// getLearningsBasePath returns "evaluation/learnings" or "learnings" based on isEvaluationMode
	learningsBase := hcpo.getLearningsBasePath()
	stepLearningsPath := filepath.Join(learningsBase, learningPathIdentifier)

	// Ensure the learning folder exists before reading/writing learnings
	if err := hcpo.ensureStepLearningsFolderExists(ctx, stepLearningsPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure learning folder exists: %v", err))
	}

	previousLearningFiles, _ := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
	previousLearningsContent := ""
	if len(previousLearningFiles) > 0 {
		previousLearningsContent, _ = hcpo.formatStepLearningFilesAsHistory(previousLearningFiles)
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Read %d previous learning file(s) for comparison (before learning phase)", len(previousLearningFiles)))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No previous learning files found (first iteration)"))
	}

	// Create success learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := ResolveVariables(step.GetTitle(), hcpo.variableValues)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name
	learningMode := "exact"
	successLearningAgentName := fmt.Sprintf("%s-success-learning-%s-%s", learningPathIdentifier, sanitizedTitle, learningMode)
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", learningPathIdentifier, successLearningAgentName, agentConfigs, isCodeExecutionMode, step.GetID(), stepPath, stepIndex)
	if err != nil {
		return fmt.Errorf("failed to create success learning agent: %w", err)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for success learning agent
	// Use interface methods instead of direct field access to support all step types (RegularPlanStep, EvaluationStep, etc.)
	stepContextOutput := step.GetContextOutput().String()

	// COST OPTIMIZATION: Use aggressive truncation to reduce learning agent input costs
	// Execution history can be 50K-200K+ tokens for complex steps with many tool calls.
	// FormatHistoryForLearningAggressive limits to last 15 messages (~15K tokens max),
	// reducing costs by 70-90% while preserving essential patterns (write operations, recent messages).
	formattedHistory := shared.FormatHistoryForLearningAggressive(executionHistory)

	successLearningTemplateVars := map[string]string{
		"StepTitle":           step.GetTitle(),
		"StepDescription":     step.GetDescription(),
		"StepSuccessCriteria": step.GetSuccessCriteria(),
		"StepContextOutput":   stepContextOutput,
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExecutionHistory":    formattedHistory,
		"ValidationResult":    string(validationResultJSON),
		"CurrentObjective":    hcpo.GetObjective(),
		"LearningDetailLevel": learningDetailLevel, // Pass learning detail preference
	}
	hcpo.GetLogger().Info(fmt.Sprintf("✅ [DEBUG] runSuccessLearningPhase: Template variables map created"))

	// Add step-specific paths (always enabled)
	// Calculate run workspace path - learnings are at the same level as execution/, not inside it
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	// StepExecutionPath should be runWorkspacePath (runs/{runFolder}), not execution path
	// Learnings are stored at workspace root using step IDs: learnings/{step_id}/
	// All steps (regular, branch, sub-agent) use learnings/{step_id}/ where step_id is the step's own unique ID
	successLearningTemplateVars["StepExecutionPath"] = runWorkspacePath
	successLearningTemplateVars["StepNumber"] = learningPathIdentifier // Use learning path identifier instead of numeric step number

	// Add execution logs folder path so learning agents can read execution logs if needed
	// Execution logs contain actual tool usage, conversation history, and execution results
	// Calculate validation workspace path (reused later in the function)
	var validationWorkspacePathForLogs string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePathForLogs = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePathForLogs = hcpo.GetWorkspacePath()
	}
	executionLogsPath := getExecutionFolderPathForLogs(validationWorkspacePathForLogs, stepPath)
	successLearningTemplateVars["ExecutionLogsPath"] = executionLogsPath

	// Add context dependencies as a comma-separated string
	contextDeps := step.GetContextDependencies()
	if len(contextDeps) > 0 {
		successLearningTemplateVars["StepContextDependencies"] = strings.Join(contextDeps, ", ")
	} else {
		successLearningTemplateVars["StepContextDependencies"] = ""
	}

	// Add variable names if available
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		successLearningTemplateVars["VariableNames"] = variableNames
	}

	// Pass existing learnings content directly (already read and formatted above)
	// This allows the learning agent to see existing patterns and build upon them
	successLearningTemplateVars["ExistingLearningsContent"] = previousLearningsContent
	if previousLearningsContent != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Passing existing learnings content to success learning agent (%d chars)", len(previousLearningsContent)))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No existing learnings content to pass (first iteration)"))
	}

	// Also pass existing learning file path for backward compatibility (if agent needs to read file)
	// Extract step number from learning path identifier for getExistingLearningFilePath (which expects numeric step number)
	// For branch steps, we'll use the parent step number
	var stepNumberForFileCheck int
	fmt.Sscanf(learningPathIdentifier, "step-%d", &stepNumberForFileCheck)
	existingLearningFilePath := hcpo.getExistingLearningFilePath(ctx, stepNumberForFileCheck, step.GetTitle())
	if existingLearningFilePath != "" {
		successLearningTemplateVars["ExistingLearningFilePath"] = existingLearningFilePath
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Found existing learning file path: %s", existingLearningFilePath))
	} else {
		successLearningTemplateVars["ExistingLearningFilePath"] = ""
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No existing learning file path found for %s", learningPathIdentifier))
	}

	// Execute extraction agent
	learningResult, learningConv, err := successLearningAgent.Execute(ctx, successLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		// Log learning failure
		_ = hcpo.logLearningExecution(ctx, stepPath, map[string]interface{}{
			"type":          "learning_failed",
			"step_path":     stepPath,
			"learning_type": "success",
			"error":         err.Error(),
			"timestamp":     time.Now().Format(time.RFC3339),
		})
		return fmt.Errorf("success learning extraction failed: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Success learning extraction completed for %s (detail level: %s)", learningPathIdentifier, learningDetailLevel))

	// Determine log file path for conversation
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}
	validationFolderPath := getValidationFolderPath(validationWorkspacePath, stepPath)
	convPath := fmt.Sprintf("%s/learning-conversation.json", validationFolderPath)

	// Save conversation
	convJSON, _ := json.MarshalIndent(learningConv, "", "  ")
	_ = hcpo.WriteWorkspaceFile(ctx, convPath, string(convJSON))

	// Log learning completion
	_ = hcpo.logLearningExecution(ctx, stepPath, map[string]interface{}{
		"type":              "learning_completed",
		"step_path":         stepPath,
		"learning_type":     "success",
		"detail_level":      learningDetailLevel,
		"result":            learningResult,
		"conversation_path": convPath,
		"trigger_reason":    triggerReason,
		"timestamp":         time.Now().Format(time.RFC3339),
	})

	// Extraction agent consolidates and writes directly to final file via LLM instructions
	// No temp file handling needed - detection agent will read the final consolidated file

	// SKIP learning detection - now using rule-based TurnCount locking
	// We no longer need to detect "new learning" with an LLM, as stability is determined by
	// successful execution counts per complexity level.
	hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping learning detection for %s - using TurnCount-based rule system", learningPathIdentifier))

	// Set default values for metadata update (legacy fields)
	hasNewLearning := true // Assume true to reset legacy consecutive-no-learning counter
	reasoning := "TurnCount-based locking active (detection skipped)"
	confidence := 1.0

	// Determine which LLM was used for learning (for metadata tracking)
	learningLLMConfig := hcpo.selectLearningLLM(ctx, agentConfigs, step.GetID(), stepPath)
	if learningLLMConfig == nil {
		err := fmt.Errorf("no valid LLM configuration found for learning agent")
		hcpo.GetLogger().Error("❌ No valid LLM configuration found for learning agent, skipping metadata update", err)
		return err
	}
	learningLLM := fmt.Sprintf("%s/%s", learningLLMConfig.Primary.Provider, learningLLMConfig.Primary.ModelID)

	// Update metadata and check if auto-lock should be triggered
	shouldAutoLock, metadataErr := hcpo.updateLearningMetadataWithTurnCount(
		ctx,
		stepIndex,
		stepPath,
		learningPathIdentifier,
		hasNewLearning,
		reasoning,
		confidence,
		turnCount,
		step,
		true, // Validation passed
		executionLLM,
		learningLLM,
	)
	if metadataErr == nil && shouldAutoLock {
		// Auto-lock learnings in step_config.json
		if lockErr := hcpo.autoLockStepLearningsInConfig(ctx, step.GetID(), reasoning); lockErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to auto-lock learnings for step %s: %v", step.GetID(), lockErr))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-locked learnings for step %s (threshold reached: %s)", step.GetID(), reasoning))
		}
	} else if metadataErr != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata for %s: %v", learningPathIdentifier, metadataErr))
	}
	return nil
}

// runFailureLearningPhase analyzes failed executions to provide refined task descriptions for retry
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
func (hcpo *StepBasedWorkflowOrchestrator) runFailureLearningPhase(ctx context.Context, stepIndex int, stepPath string, learningPathIdentifier string, totalSteps int, step PlanStepInterface, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse, isCodeExecutionMode bool, usedTempLLM string, turnCount int, executionLLM string, triggerReason string) (string, string, error) {
	// Ensure the learning folder exists before reading/writing learnings
	// Use RELATIVE path - workspace functions auto-prepend workspacePath
	// getLearningsBasePath returns "evaluation/learnings" or "learnings" based on isEvaluationMode
	learningsBase := hcpo.getLearningsBasePath()
	stepLearningsPath := filepath.Join(learningsBase, learningPathIdentifier)
	if err := hcpo.ensureStepLearningsFolderExists(ctx, stepLearningsPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure learning folder exists: %v", err))
	}

	// Get agent configs once at the start
	agentConfigs := getAgentConfigs(step)

	// Use step-specific learning detail level, default to "exact" if not set
	learningDetailLevel := "exact" // default
	if agentConfigs != nil && agentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = agentConfigs.LearningDetailLevel
	}

	// AUTO-UNLOCK LEARNINGS: If validation failed, automatically unlock learnings so the step can learn from the failure
	// This ensures that when validation fails, learnings are unlocked even if they were previously locked
	validationFailed := validationResponse != nil && !validationResponse.IsSuccessCriteriaMet
	if validationFailed {
		isLearningsLocked := agentConfigs != nil && agentConfigs.LockLearnings != nil && *agentConfigs.LockLearnings
		if isLearningsLocked {
			hcpo.GetLogger().Info(fmt.Sprintf("🔓 Validation failed - auto-unlocking learnings for step %s so it can learn from the failure", step.GetID()))
			if unlockErr := hcpo.unlockStepLearningsInConfig(ctx, step.GetID()); unlockErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to auto-unlock learnings for step %s: %v", step.GetID(), unlockErr))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Auto-unlocked learnings for step %s (validation failed)", step.GetID()))
				// Update unlock metadata
				if metadataErr := hcpo.updateUnlockMetadata(ctx, step.GetID(), stepIndex, stepPath, learningPathIdentifier, "validation_failed"); metadataErr != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update unlock metadata for step %s: %v", step.GetID(), metadataErr))
				}
				// Update agentConfigs to reflect the unlock (for this function's execution)
				if agentConfigs != nil {
					lockValue := false
					agentConfigs.LockLearnings = &lockValue
				}
			}
		}
	}

	// TEMP LLM OVERRIDE: Skip failure learning if tempLLM was used (we should fallback to main LLM instead of learning)
	// This prevents wasting tokens on failure learning when a cheaper tempLLM failed - we just retry with the better LLM
	if usedTempLLM != "" {
		// Check if skip flags are enabled
		shouldSkipFailureLearningDueToTempOverride := false
		if hcpo.executionOptions != nil {
			if usedTempLLM == "tempLLM1" && hcpo.executionOptions.SkipLearningWhenTempLLM1 {
				shouldSkipFailureLearningDueToTempOverride = true
				hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM1 was used and SkipLearningWhenTempLLM1 flag is enabled - skipping failure learning for step %d", stepIndex+1))
			} else if usedTempLLM == "tempLLM2" && hcpo.executionOptions.SkipLearningWhenTempLLM2 {
				shouldSkipFailureLearningDueToTempOverride = true
				hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM2 was used and SkipLearningWhenTempLLM2 flag is enabled - skipping failure learning for step %d", stepIndex+1))
			}
		}

		if shouldSkipFailureLearningDueToTempOverride {
			// Skip failure learning and just return empty refined description
			// The system will fallback to the original/main LLM for the next retry
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping failure learning for %s/%d (%s failed validation, will retry with main LLM)", learningPathIdentifier, totalSteps, usedTempLLM))
			// Note: We don't call updateMetadataWhenSkippedFailure here because we want to let the main LLM retry handle metadata updates
			return "", "", nil
		}
	}

	// Helper function to update metadata with turnCount when learning is skipped
	updateMetadataWhenSkippedFailure := func(skipReason string) error {
		// Determine which LLM would have been used (for metadata tracking)
		learningLLMConfig := hcpo.selectLearningLLM(ctx, agentConfigs, step.GetID(), stepPath)
		if learningLLMConfig == nil {
			err := fmt.Errorf("no valid LLM configuration found for learning agent")
			hcpo.GetLogger().Error("❌ No valid LLM configuration found for learning agent, skipping metadata update", err)
			return err
		}
		learningLLM := fmt.Sprintf("%s/%s", learningLLMConfig.Primary.Provider, learningLLMConfig.Primary.ModelID)

		// Update metadata with turnCount but don't increment counters (learning was skipped)
		// We still want to record last_turn_count for complexity tracking
		// Note: validationPassed = false because this is failure learning (validation failed)
		_, metadataErr := hcpo.updateLearningMetadataWithTurnCount(
			ctx,
			stepIndex,
			stepPath,
			learningPathIdentifier,
			false, // hasNewLearning = false (learning was skipped)
			fmt.Sprintf("Failure learning skipped: %s (turnCount recorded for complexity tracking)", skipReason),
			0.0, // confidence = 0 (not applicable when skipped)
			turnCount,
			step,
			false, // validationPassed = false (validation failed, and learning was skipped)
			executionLLM,
			learningLLM,
		)
		if metadataErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata (skipped) for %s: %v", learningPathIdentifier, metadataErr))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("📊 Recorded turnCount (%d) for %s (failure learning skipped: %s)", turnCount, learningPathIdentifier, skipReason))
		}
		return metadataErr
	}

	// LOCK LEARNINGS: Check if learnings are locked (prevents learning agent from running but still uses existing learnings)
	// Note: Lock learnings takes precedence - even in code execution mode, if learnings are locked, skip learning agent
	// EXCEPTION: If learnings are locked but learnings don't exist, still run learning to create initial learnings
	isLearningsLocked := agentConfigs != nil && agentConfigs.LockLearnings != nil && *agentConfigs.LockLearnings
	if isLearningsLocked {
		// Check if learnings folder exists and has content
		learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, step.GetID(), stepIndex, stepPath)
		if err != nil {
			// If we can't check, assume empty and run learning
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked but cannot check if learnings exist - running learning to create initial learnings for %s/%d", learningPathIdentifier, totalSteps))
		} else if learningsEmpty {
			// Learnings are locked but folder is empty - run learning to create initial learnings
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked but folder is empty - running learning to create initial learnings for %s/%d", learningPathIdentifier, totalSteps))
		} else {
			// Learnings are locked and learnings exist - skip learning but record turnCount
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping failure learning analysis for %s/%d (using existing learnings)", learningPathIdentifier, totalSteps))
			_ = updateMetadataWhenSkippedFailure("learnings locked")
			return "", "", nil
		}
	}

	// Skip learning if "none" is selected or learning is disabled
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	// Use the provided step-specific code execution mode (already computed with step-level priority)
	shouldSkipLearning := (learningDetailLevel == "none" || (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning)) && !isCodeExecutionMode
	if shouldSkipLearning {
		// Learning is disabled - skip learning but record turnCount
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping failure learning analysis for %s/%d (learning disabled)", learningPathIdentifier, totalSteps))
		_ = updateMetadataWhenSkippedFailure("learning disabled")
		return "", "", nil
	}
	if isCodeExecutionMode && (learningDetailLevel == "none" || (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning)) {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing failure learning for %s/%d (overriding step config)", learningPathIdentifier, totalSteps))
		// Override learning detail level to "exact" if it was "none"
		if learningDetailLevel == "none" {
			learningDetailLevel = "exact"
		}
	}

	// Failure learning agent ALWAYS runs - it writes learnings (creates folder if needed)
	// Only the learning reading agent (which reads existing learnings) should check folder existence
	hcpo.GetLogger().Info(fmt.Sprintf("🧠 Starting failure learning analysis for %s/%d: %s", learningPathIdentifier, totalSteps, step.GetTitle()))

	// Log learning start
	_ = hcpo.logLearningExecution(ctx, stepPath, map[string]interface{}{
		"type":             "learning_start",
		"step_path":        stepPath,
		"learning_type":    "failure",
		"learning_path_id": learningPathIdentifier,
		"timestamp":        shared.GetTimestamp(),
	})

	// Read previous learnings BEFORE learning phase runs (for comparison after learning phase completes)
	// This captures the state before the learning agent potentially modifies the files
	// Note: stepLearningsPath was already set earlier with RELATIVE path
	previousLearningFiles, _ := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
	previousLearningsContent := ""
	if len(previousLearningFiles) > 0 {
		previousLearningsContent, _ = hcpo.formatStepLearningFilesAsHistory(previousLearningFiles)
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Read %d previous learning file(s) for comparison (before learning phase)", len(previousLearningFiles)))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No previous learning files found (first iteration)"))
	}

	// Create failure learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := ResolveVariables(step.GetTitle(), hcpo.variableValues)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name
	learningMode := "exact"
	failureLearningAgentName := fmt.Sprintf("%s-failure-learning-%s-%s", learningPathIdentifier, sanitizedTitle, learningMode)
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", learningPathIdentifier, failureLearningAgentName, agentConfigs, isCodeExecutionMode, step.GetID(), stepPath, stepIndex)
	if err != nil {
		return "", "", fmt.Errorf("failed to create failure learning agent: %w", err)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for failure learning agent
	// Use interface methods instead of direct field access to support all step types (RegularPlanStep, EvaluationStep, etc.)
	failureLearningTemplateVars := map[string]string{
		"StepTitle":           step.GetTitle(),
		"StepDescription":     step.GetDescription(),
		"StepSuccessCriteria": step.GetSuccessCriteria(),
		"StepContextOutput":   step.GetContextOutput().String(),
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		// COST OPTIMIZATION: Use aggressive truncation to reduce learning agent input costs
		// Execution history can be 50K-200K+ tokens for complex steps with many tool calls.
		// FormatHistoryForLearningAggressive limits to last 15 messages (~15K tokens max),
		// reducing costs by 70-90% while preserving essential patterns (write operations, recent messages).
		"ExecutionHistory":    shared.FormatHistoryForLearningAggressive(executionHistory),
		"ValidationResult":    string(validationResultJSON),
		"CurrentObjective":    hcpo.GetObjective(),
		"LearningDetailLevel": learningDetailLevel, // Pass learning detail preference
	}

	// Add step-specific paths (always enabled)
	// Calculate run workspace path - learnings are at the same level as execution/, not inside it
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	// StepExecutionPath should be runWorkspacePath (runs/{runFolder}), not execution path
	// Learnings are stored at workspace root using step IDs: learnings/{step_id}/
	// All steps (regular, branch, sub-agent) use learnings/{step_id}/ where step_id is the step's own unique ID
	failureLearningTemplateVars["StepExecutionPath"] = runWorkspacePath
	failureLearningTemplateVars["StepNumber"] = learningPathIdentifier // Use learning path identifier instead of numeric step number

	// Add execution logs folder path so learning agents can read execution logs if needed
	// Execution logs contain actual tool usage, conversation history, and execution results
	// Calculate validation workspace path (reused later in the function)
	var validationWorkspacePathForLogs string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePathForLogs = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePathForLogs = hcpo.GetWorkspacePath()
	}
	executionLogsPath := getExecutionFolderPathForLogs(validationWorkspacePathForLogs, stepPath)
	failureLearningTemplateVars["ExecutionLogsPath"] = executionLogsPath

	// Add context dependencies as a comma-separated string
	contextDeps := step.GetContextDependencies()
	if len(contextDeps) > 0 {
		failureLearningTemplateVars["StepContextDependencies"] = strings.Join(contextDeps, ", ")
	} else {
		failureLearningTemplateVars["StepContextDependencies"] = ""
	}

	// Add variable names if available
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		failureLearningTemplateVars["VariableNames"] = variableNames
	}

	// Pass existing learnings content directly (already read and formatted above)
	// This allows the learning agent to see existing patterns and build upon them
	failureLearningTemplateVars["ExistingLearningsContent"] = previousLearningsContent
	if previousLearningsContent != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Passing existing learnings content to failure learning agent (%d chars)", len(previousLearningsContent)))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No existing learnings content to pass (first iteration)"))
	}

	// Also pass existing learning file path for backward compatibility (if agent needs to read file)
	// Extract step number from learning path identifier for getExistingLearningFilePath (which expects numeric step number)
	// For branch steps, we'll use the parent step number
	var stepNumberForFileCheck int
	fmt.Sscanf(learningPathIdentifier, "step-%d", &stepNumberForFileCheck)
	existingLearningFilePath := hcpo.getExistingLearningFilePath(ctx, stepNumberForFileCheck, step.GetTitle())
	if existingLearningFilePath != "" {
		failureLearningTemplateVars["ExistingLearningFilePath"] = existingLearningFilePath
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Found existing learning file path: %s", existingLearningFilePath))
	} else {
		failureLearningTemplateVars["ExistingLearningFilePath"] = ""
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No existing learning file path found for %s", learningPathIdentifier))
	}

	// Execute extraction agent
	learningResult, learningConv, err := failureLearningAgent.Execute(ctx, failureLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		// Log learning failure
		_ = hcpo.logLearningExecution(ctx, stepPath, map[string]interface{}{
			"type":          "learning_failed",
			"step_path":     stepPath,
			"learning_type": "failure",
			"error":         err.Error(),
			"timestamp":     time.Now().Format(time.RFC3339),
		})
		return "", "", fmt.Errorf("failure learning extraction failed: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Failure learning extraction completed for %s (detail level: %s)", learningPathIdentifier, learningDetailLevel))

	// Determine log file path for conversation
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}
	validationFolderPath := getValidationFolderPath(validationWorkspacePath, stepPath)
	convPath := fmt.Sprintf("%s/learning-failure-conversation.json", validationFolderPath)

	// Save conversation
	convJSON, _ := json.MarshalIndent(learningConv, "", "  ")
	_ = hcpo.WriteWorkspaceFile(ctx, convPath, string(convJSON))

	// Log learning completion
	_ = hcpo.logLearningExecution(ctx, stepPath, map[string]interface{}{
		"type":              "learning_completed",
		"step_path":         stepPath,
		"learning_type":     "failure",
		"detail_level":      learningDetailLevel,
		"result":            learningResult,
		"conversation_path": convPath,
		"trigger_reason":    triggerReason,
		"timestamp":         time.Now().Format(time.RFC3339),
	})

	// Extraction agent consolidates and writes directly to final file via LLM instructions
	// No temp file handling needed - detection agent will read the final consolidated file

	// SKIP learning detection for failure learning phase (per user objective)
	// We want to avoid premature locking of learnings during failure loops, as further retries might yield success
	hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping learning detection for failure phase of %s - assuming new learning to keep loop active", learningPathIdentifier))
	hasNewLearning := true
	reasoning := "Failure learning phase - detection skipped to avoid premature locking"
	confidence := 1.0

	// Determine which LLM was used for learning (for metadata tracking)
	learningLLMConfig := hcpo.selectLearningLLM(ctx, agentConfigs, step.GetID(), stepPath)
	if learningLLMConfig == nil {
		err := fmt.Errorf("no valid LLM configuration found for learning agent")
		hcpo.GetLogger().Error("❌ No valid LLM configuration found for learning agent, skipping metadata update", err)
		return "", "", err
	}
	learningLLM := fmt.Sprintf("%s/%s", learningLLMConfig.Primary.Provider, learningLLMConfig.Primary.ModelID)

	// Update metadata and check if auto-lock should be triggered
	// Note: validationPassed is false because this is failure learning (validation failed)
	shouldAutoLock, metadataErr := hcpo.updateLearningMetadataWithTurnCount(
		ctx,
		stepIndex,
		stepPath,
		learningPathIdentifier,
		hasNewLearning,
		reasoning,
		confidence,
		turnCount,
		step,
		false, // validationPassed = false (validation failed)
		executionLLM,
		learningLLM,
	)
	if metadataErr == nil && shouldAutoLock {
		// Auto-lock learnings in step_config.json
		if lockErr := hcpo.autoLockStepLearningsInConfig(ctx, step.GetID(), reasoning); lockErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to auto-lock learnings for step %s: %v", step.GetID(), lockErr))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-locked learnings for step %s (threshold reached)", step.GetID()))
		}
	} else if metadataErr != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata for %s: %v", learningPathIdentifier, metadataErr))
	}

	// Return empty strings since detection agent handles the output
	// The function signature requires (string, string, error) for backward compatibility
	return "", "", nil
}

// readStepLearningFiles reads all learning files from a step-specific folder
// Reads .md files from the step folder, .go files from code/ subfolder (Code Execution Mode),
// and .py/.sh files from scripts/ subfolder (Simple Mode)
// Deletes _learning_new.md if it exists (leftover temp file from previous runs)
// Excludes metadata files (.learning_metadata.json)
// Returns a map of filename -> content
func (hcpo *StepBasedWorkflowOrchestrator) readStepLearningFiles(ctx context.Context, stepLearningsPath string) (map[string]string, error) {
	learningFiles := make(map[string]string)

	// List all files in the step folder
	files, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, stepLearningsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list files in %s: %w", stepLearningsPath, err)
	}

	// Delete _learning_new.md if it exists (leftover temp file from previous runs)
	tempFilePath := filepath.Join(stepLearningsPath, "_learning_new.md")
	exists, _ := hcpo.BaseOrchestrator.CheckWorkspaceFileExists(ctx, tempFilePath)
	if exists {
		if err := hcpo.BaseOrchestrator.DeleteWorkspaceFile(ctx, tempFilePath); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete temp file %s: %v", tempFilePath, err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleted leftover temp file: %s", tempFilePath))
		}
	}

	// Read all .md files from the step folder
	// Exclude metadata files (.learning_metadata.json) and temporary files (_learning_new.md) - these are for internal tracking only
	for _, file := range files {
		// Skip metadata files - these should not be passed to execution agents
		if file == ".learning_metadata.json" || strings.HasSuffix(file, ".learning_metadata.json") {
			continue
		}
		// Skip temporary learning files - _learning_new.md should have been deleted above, but skip it if still present
		if file == "_learning_new.md" {
			continue
		}
		if strings.HasSuffix(file, ".md") {
			filePath := filepath.Join(stepLearningsPath, file)
			content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, filePath)
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read learning file %s: %v", filePath, err))
				continue
			}
			learningFiles[file] = content
		}
	}

	// Check if code/ subfolder exists (for code execution mode)
	// This subfolder contains Python code examples/patterns
	codeSubfolderPath := filepath.Join(stepLearningsPath, "code")
	codeFiles, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, codeSubfolderPath)
	if err == nil && len(codeFiles) > 0 {
		// Read all .py and .go files from code/ subfolder (Python preferred, Go for legacy)
		codeFileCount := 0
		for _, file := range codeFiles {
			if strings.HasSuffix(file, ".py") || strings.HasSuffix(file, ".go") {
				filePath := filepath.Join(codeSubfolderPath, file)
				content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, filePath)
				if err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read code learning file %s: %v", filePath, err))
					continue
				}
				// Prefix with "code/" to indicate it's from the code subfolder
				learningFiles[filepath.Join("code", file)] = content
				codeFileCount++
			}
		}
		if codeFileCount > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("📁 Read %d code file(s) from code/ subfolder", codeFileCount))
		}
	}
	// Note: If code/ subfolder doesn't exist or is empty, that's fine - it's optional

	// Check if scripts/ subfolder exists (for simple mode)
	// This subfolder contains .py Python scripts and .sh shell scripts
	scriptsSubfolderPath := filepath.Join(stepLearningsPath, "scripts")
	scriptFiles, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, scriptsSubfolderPath)
	if err == nil && len(scriptFiles) > 0 {
		// Read all .py and .sh files from scripts/ subfolder
		scriptFileCount := 0
		for _, file := range scriptFiles {
			if strings.HasSuffix(file, ".py") || strings.HasSuffix(file, ".sh") {
				filePath := filepath.Join(scriptsSubfolderPath, file)
				content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, filePath)
				if err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read script learning file %s: %v", filePath, err))
					continue
				}
				// Prefix with "scripts/" to indicate it's from the scripts subfolder
				learningFiles[filepath.Join("scripts", file)] = content
				scriptFileCount++
			}
		}
		if scriptFileCount > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("📁 Read %d script file(s) (.py/.sh) from scripts/ subfolder", scriptFileCount))
		}
	}
	// Note: If scripts/ subfolder doesn't exist or is empty, that's fine - it's optional

	return learningFiles, nil
}

// formatStepLearningFilesAsHistory formats a map of learning files (filename -> content) into a formatted history string
// Returns the combined content and list of file paths
func (hcpo *StepBasedWorkflowOrchestrator) formatStepLearningFilesAsHistory(learningFiles map[string]string) (string, []string) {
	if len(learningFiles) == 0 {
		return "", []string{}
	}

	var result strings.Builder
	result.WriteString("## Learning Context (Pre-loaded - DO NOT re-read these files)\n\n")
	result.WriteString("**Note**: The following learning content has been pre-loaded from the learnings folder. ")
	result.WriteString("You do NOT need to read these files again - the full content is included below.\n\n")
	filePaths := make([]string, 0, len(learningFiles))

	// Sort filenames for consistent output
	filenames := make([]string, 0, len(learningFiles))
	for filename := range learningFiles {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)

	// Format each file with clear source attribution
	for i, filename := range filenames {
		content := learningFiles[filename]
		if i > 0 {
			result.WriteString("\n---\n\n")
		}
		// Make it very clear this is the file content, already loaded
		result.WriteString(fmt.Sprintf("### 📄 File: `%s` (content already loaded below)\n\n", filename))
		result.WriteString(content)
		result.WriteString("\n")
		filePaths = append(filePaths, filename)
	}

	return result.String(), filePaths
}

// getExistingLearningFilePath checks if an existing learning file exists for the given step
// Returns the RELATIVE file path if it exists, empty string otherwise
func (hcpo *StepBasedWorkflowOrchestrator) getExistingLearningFilePath(ctx context.Context, stepNumber int, stepTitle string) string {
	// Resolve variables in step title
	resolvedTitle := ResolveVariables(stepTitle, hcpo.variableValues)

	// Use RELATIVE path - workspace functions auto-prepend workspacePath
	// getLearningsBasePath returns "evaluation/learnings" or "learnings" based on isEvaluationMode
	learningsBase := hcpo.getLearningsBasePath()
	learningsBasePath := fmt.Sprintf("%s/step-%d", learningsBase, stepNumber)

	// Construct the expected file path
	learningFileName := fmt.Sprintf("%s_learning.md", resolvedTitle)
	expectedFilePath := filepath.Join(learningsBasePath, learningFileName)

	// Try to read the file to check if it exists
	_, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, expectedFilePath)
	if err == nil {
		// File exists, return the RELATIVE path
		return expectedFilePath
	}

	// File doesn't exist, return empty string
	return ""
}

// logLearningExecution appends a learning execution entry to the learning log file (JSONL format)
func (hcpo *StepBasedWorkflowOrchestrator) logLearningExecution(ctx context.Context, stepPath string, entry map[string]interface{}) error {
	// Determine log file path
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	// Get validation folder path using stepPath
	// For regular steps: "logs/step-{X}/"
	// For branch steps: "logs/step-{parentStep}-{true/false}-{branchIdx}/"
	validationFolderPath := getValidationFolderPath(validationWorkspacePath, stepPath)

	// Create logs folder if it doesn't exist (using BaseOrchestrator.WriteWorkspaceFile which handles dirs, or manual check)
	// We'll rely on appendOrchestrationLogEntry logic which handles file writing

	learningLogFilePath := fmt.Sprintf("%s/learning-execution.json", validationFolderPath)

	// Use existing appendOrchestrationLogEntry helper which handles JSONL appending
	if err := hcpo.appendOrchestrationLogEntry(ctx, learningLogFilePath, entry); err != nil {
		return fmt.Errorf("failed to append learning log entry: %w", err)
	}

	return nil
}
