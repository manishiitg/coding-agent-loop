package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/shared"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// runSuccessLearningPhase analyzes successful executions to capture best practices and improve plan.json
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
// usedTempLLM: Which tempLLM was used during execution ("tempLLM1", "tempLLM2", or "" for original LLM)
func (hcpo *HumanControlledTodoPlannerOrchestrator) runSuccessLearningPhase(ctx context.Context, stepIndex int, stepPath string, learningPathIdentifier string, totalSteps int, step PlanStepInterface, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse, isCodeExecutionMode bool, usedTempLLM string) error {
	// Get agent configs once at the start
	agentConfigs := getAgentConfigs(step)

	// Use step-specific learning detail level, default to "exact" if not set
	learningDetailLevel := "exact" // default
	if agentConfigs != nil && agentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = agentConfigs.LearningDetailLevel
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Using step-specific learning detail level: '%s'", learningDetailLevel))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No step-specific learning detail level set, using default: 'exact'"))
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
			// Learnings are locked and learnings exist - skip learning
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping success learning analysis for %s/%d (using existing learnings)", learningPathIdentifier, totalSteps))
			return nil
		}
	}

	// Skip learning if "none" is selected or learning is disabled
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	// Use the provided step-specific code execution mode (already computed with step-level priority)
	shouldSkipLearning := (learningDetailLevel == "none" || (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning)) && !isCodeExecutionMode
	if shouldSkipLearning {
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping success learning analysis for %s/%d (learning disabled)", learningPathIdentifier, totalSteps))
		return nil
	}
	if isCodeExecutionMode && (learningDetailLevel == "none" || (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning)) {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing success learning for %s/%d (overriding step config)", learningPathIdentifier, totalSteps))
		// Override learning detail level to "exact" if it was "none"
		if learningDetailLevel == "none" {
			learningDetailLevel = "exact"
		}
	}

	// Success learning agent ALWAYS runs - it writes learnings (creates folder if needed)
	// Only the learning reading agent (which reads existing learnings) should check folder existence
	hcpo.GetLogger().Info(fmt.Sprintf("🧠 Starting success learning analysis for %s/%d: %s", learningPathIdentifier, totalSteps, step.GetTitle()))

	// Read previous learnings BEFORE learning phase runs (for comparison after learning phase completes)
	// This captures the state before the learning agent potentially modifies the files
	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepLearningsPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier)
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
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	successLearningAgentName := fmt.Sprintf("%s-success-learning-%s-%s", learningPathIdentifier, sanitizedTitle, learningMode)
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", learningPathIdentifier, successLearningAgentName, agentConfigs, isCodeExecutionMode)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to create success learning agent: %w", err), nil)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for success learning agent
	regularStep := getRegularPlanStep(step)
	successLearningTemplateVars := map[string]string{
		"StepTitle":           step.GetTitle(),
		"StepDescription":     regularStep.Description,
		"StepSuccessCriteria": regularStep.SuccessCriteria,
		"StepContextOutput":   step.GetContextOutput().String(),
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExecutionHistory":    shared.FormatConversationHistory(executionHistory),
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
	successLearningTemplateVars["StepExecutionPath"] = runWorkspacePath
	successLearningTemplateVars["StepNumber"] = learningPathIdentifier // Use learning path identifier instead of numeric step number

	// Add context dependencies as a comma-separated string
	if len(regularStep.ContextDependencies) > 0 {
		successLearningTemplateVars["StepContextDependencies"] = strings.Join(regularStep.ContextDependencies, ", ")
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

	// Execute extraction agent and capture output
	extractionOutput, _, err := successLearningAgent.Execute(ctx, successLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("success learning extraction failed: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Success learning extraction completed for %s (detail level: %s)", learningPathIdentifier, learningDetailLevel))

	// Extract the new learning file path from agent output
	// Output format: "Updated: {path}/_learning_new.md"
	newLearningFilePath := ""
	if extractionOutput != "" {
		// Try to extract path from output
		if strings.Contains(extractionOutput, "Updated:") {
			parts := strings.Split(extractionOutput, "Updated:")
			if len(parts) > 1 {
				newLearningFilePath = strings.TrimSpace(parts[1])
			}
		}
	}

	// If path extraction failed, construct it manually
	if newLearningFilePath == "" {
		newLearningFilePath = filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier, "_learning_new.md")
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Constructed new learning file path: %s", newLearningFilePath))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Extracted new learning file path from output: %s", newLearningFilePath))
	}

	// Run learning detection with consolidation (combined agent)
	// previousLearningsContent was captured BEFORE any learning agent ran
	// Determine if validation passed (success criteria met)
	validationPassed := validationResponse != nil && validationResponse.IsSuccessCriteriaMet
	hasNewLearning, reasoning, confidence, detectionErr := hcpo.detectNewLearningWithLLM(
		ctx,
		stepIndex,
		stepPath,
		learningPathIdentifier,
		agentConfigs,
		previousLearningsContent,
		step,
		usedTempLLM,
		validationPassed,
		newLearningFilePath, // Pass new learning file path - detection agent will perform consolidation
		isCodeExecutionMode, // Pass code execution mode for consolidation
	)
	if detectionErr == nil {
		// Update metadata and check if auto-lock should be triggered
		shouldAutoLock, metadataErr := hcpo.updateLearningMetadata(
			ctx,
			stepIndex,
			stepPath,
			learningPathIdentifier,
			hasNewLearning,
			reasoning,
			confidence,
		)
		if metadataErr == nil && shouldAutoLock {
			// Auto-lock learnings in step_config.json
			if lockErr := hcpo.autoLockStepLearningsInConfig(ctx, step.GetID(), reasoning); lockErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to auto-lock learnings for step %s: %v", step.GetID(), lockErr))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-locked learnings for step %s after 3 consecutive iterations with no new learning", step.GetID()))
			}
		} else if metadataErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata for %s: %v", learningPathIdentifier, metadataErr))
		}
	} else {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Learning detection failed for %s: %v (non-blocking)", learningPathIdentifier, detectionErr))
	}

	return nil
}

// runFailureLearningPhase analyzes failed executions to provide refined task descriptions for retry
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
// runFailureLearningPhase analyzes failed executions to provide refined task descriptions
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) runFailureLearningPhase(ctx context.Context, stepIndex int, stepPath string, learningPathIdentifier string, totalSteps int, step PlanStepInterface, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse, isCodeExecutionMode bool) (string, string, error) {
	// Get agent configs once at the start
	agentConfigs := getAgentConfigs(step)

	// Use step-specific learning detail level, default to "exact" if not set
	learningDetailLevel := "exact" // default
	if agentConfigs != nil && agentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = agentConfigs.LearningDetailLevel
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Using step-specific learning detail level: '%s'", learningDetailLevel))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No step-specific learning detail level set, using default: 'exact'"))
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
			// Learnings are locked and learnings exist - skip learning
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping failure learning analysis for %s/%d (using existing learnings)", learningPathIdentifier, totalSteps))
			return "", "", nil
		}
	}

	// Skip learning if "none" is selected or learning is disabled
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	// Use the provided step-specific code execution mode (already computed with step-level priority)
	shouldSkipLearning := (learningDetailLevel == "none" || (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning)) && !isCodeExecutionMode
	if shouldSkipLearning {
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping failure learning analysis for %s/%d (learning disabled)", learningPathIdentifier, totalSteps))
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

	// Read previous learnings BEFORE learning phase runs (for comparison after learning phase completes)
	// This captures the state before the learning agent potentially modifies the files
	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepLearningsPath := filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier)
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
	// Include learning mode in agent name (exact or general)
	learningMode := "exact"
	if learningDetailLevel == "general" {
		learningMode = "general"
	}
	failureLearningAgentName := fmt.Sprintf("%s-failure-learning-%s-%s", learningPathIdentifier, sanitizedTitle, learningMode)
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", learningPathIdentifier, failureLearningAgentName, agentConfigs, isCodeExecutionMode)
	if err != nil {
		return "", "", fmt.Errorf(fmt.Sprintf("failed to create failure learning agent: %w", err), nil)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for failure learning agent
	regularStep := getRegularPlanStep(step)
	failureLearningTemplateVars := map[string]string{
		"StepTitle":           step.GetTitle(),
		"StepDescription":     regularStep.Description,
		"StepSuccessCriteria": regularStep.SuccessCriteria,
		"StepContextOutput":   step.GetContextOutput().String(),
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExecutionHistory":    shared.FormatConversationHistory(executionHistory),
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

	// Add context dependencies as a comma-separated string
	if len(regularStep.ContextDependencies) > 0 {
		failureLearningTemplateVars["StepContextDependencies"] = strings.Join(regularStep.ContextDependencies, ", ")
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

	// Execute extraction agent and capture output
	extractionOutput, _, err := failureLearningAgent.Execute(ctx, failureLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return "", "", fmt.Errorf(fmt.Sprintf("failure learning extraction failed: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Failure learning extraction completed for %s (detail level: %s)", learningPathIdentifier, learningDetailLevel))

	// Extract the new learning file path from agent output
	// Output format: "Updated: {path}/_learning_new.md"
	newLearningFilePath := ""
	if extractionOutput != "" {
		// Try to extract path from output
		if strings.Contains(extractionOutput, "Updated:") {
			parts := strings.Split(extractionOutput, "Updated:")
			if len(parts) > 1 {
				newLearningFilePath = strings.TrimSpace(parts[1])
			}
		}
	}

	// If path extraction failed, construct it manually
	if newLearningFilePath == "" {
		newLearningFilePath = filepath.Join(baseWorkspacePath, "learnings", learningPathIdentifier, "_learning_new.md")
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Constructed new learning file path: %s", newLearningFilePath))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Extracted new learning file path from output: %s", newLearningFilePath))
	}

	// Run learning detection with consolidation (combined agent)
	// previousLearningsContent was captured BEFORE any learning agent ran
	// For failure learning phase, no tempLLM was used (execution failed) and validation did not pass
	hasNewLearning, reasoning, confidence, detectionErr := hcpo.detectNewLearningWithLLM(
		ctx,
		stepIndex,
		stepPath,
		learningPathIdentifier,
		agentConfigs,
		previousLearningsContent,
		step,
		"",                  // No tempLLM used for failed executions
		false,               // Validation did not pass (this is a failure learning phase)
		newLearningFilePath, // Pass new learning file path - detection agent will perform consolidation
		isCodeExecutionMode, // Pass code execution mode for consolidation
	)
	if detectionErr == nil {
		// Update metadata and check if auto-lock should be triggered
		shouldAutoLock, metadataErr := hcpo.updateLearningMetadata(
			ctx,
			stepIndex,
			stepPath,
			learningPathIdentifier,
			hasNewLearning,
			reasoning,
			confidence,
		)
		if metadataErr == nil && shouldAutoLock {
			// Auto-lock learnings in step_config.json
			if lockErr := hcpo.autoLockStepLearningsInConfig(ctx, step.GetID(), reasoning); lockErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to auto-lock learnings for step %s: %v", step.GetID(), lockErr))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("🔒 Auto-locked learnings for step %s after 3 consecutive iterations with no new learning", step.GetID()))
			}
		} else if metadataErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata for %s: %v", learningPathIdentifier, metadataErr))
		}
	} else {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Learning detection failed for %s: %v (non-blocking)", learningPathIdentifier, detectionErr))
	}

	// Return empty strings since detection agent handles the output
	// The function signature requires (string, string, error) for backward compatibility
	return "", "", nil
}

// readStepLearningFiles reads all learning files from a step-specific folder
// Reads .md files from the step folder, .go files from code/ subfolder (Code Execution Mode),
// and .py/.sh files from scripts/ subfolder (Simple Mode)
// Excludes metadata files (.learning_metadata.json)
// Returns a map of filename -> content
func (hcpo *HumanControlledTodoPlannerOrchestrator) readStepLearningFiles(ctx context.Context, stepLearningsPath string) (map[string]string, error) {
	learningFiles := make(map[string]string)

	// List all files in the step folder
	files, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, stepLearningsPath)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to list files in %s: %w", stepLearningsPath, err), nil)
	}

	// Read all .md files from the step folder
	// Exclude metadata files (.learning_metadata.json) - these are for internal tracking only
	for _, file := range files {
		// Skip metadata files - these should not be passed to execution agents
		if file == ".learning_metadata.json" || strings.HasSuffix(file, ".learning_metadata.json") {
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
	// This subfolder contains .go code examples/patterns
	codeSubfolderPath := filepath.Join(stepLearningsPath, "code")
	codeFiles, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, codeSubfolderPath)
	if err == nil && len(codeFiles) > 0 {
		// Read all .go files from code/ subfolder
		goFileCount := 0
		for _, file := range codeFiles {
			if strings.HasSuffix(file, ".go") {
				filePath := filepath.Join(codeSubfolderPath, file)
				content, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, filePath)
				if err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read code learning file %s: %v", filePath, err))
					continue
				}
				// Prefix with "code/" to indicate it's from the code subfolder
				learningFiles[filepath.Join("code", file)] = content
				goFileCount++
			}
		}
		if goFileCount > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("📁 Read %d .go file(s) from code/ subfolder", goFileCount))
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatStepLearningFilesAsHistory(learningFiles map[string]string) (string, []string) {
	if len(learningFiles) == 0 {
		return "", []string{}
	}

	var result strings.Builder
	result.WriteString("## Learning Context\n\n")
	filePaths := make([]string, 0, len(learningFiles))

	// Sort filenames for consistent output
	filenames := make([]string, 0, len(learningFiles))
	for filename := range learningFiles {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)

	// Format each file
	for i, filename := range filenames {
		content := learningFiles[filename]
		if i > 0 {
			result.WriteString("\n---\n\n")
		}
		result.WriteString(fmt.Sprintf("### %s\n\n", filename))
		result.WriteString(content)
		result.WriteString("\n")
		filePaths = append(filePaths, filename)
	}

	return result.String(), filePaths
}

// getExistingLearningFilePath checks if an existing learning file exists for the given step
// Returns the full file path if it exists, empty string otherwise
func (hcpo *HumanControlledTodoPlannerOrchestrator) getExistingLearningFilePath(ctx context.Context, stepNumber int, stepTitle string) string {
	baseWorkspacePath := hcpo.GetWorkspacePath()

	// Resolve variables in step title
	resolvedTitle := ResolveVariables(stepTitle, hcpo.variableValues)

	// Always use learnings folder (unified folder for all learning types)
	learningsBasePath := fmt.Sprintf("%s/learnings/step-%d", baseWorkspacePath, stepNumber)

	// Construct the expected file path
	learningFileName := fmt.Sprintf("%s_learning.md", resolvedTitle)
	expectedFilePath := filepath.Join(learningsBasePath, learningFileName)

	// Try to read the file to check if it exists
	_, err := hcpo.BaseOrchestrator.ReadWorkspaceFile(ctx, expectedFilePath)
	if err == nil {
		// File exists, return the path
		return expectedFilePath
	}

	// File doesn't exist, return empty string
	return ""
}
