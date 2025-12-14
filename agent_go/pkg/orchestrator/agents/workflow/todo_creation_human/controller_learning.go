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
func (hcpo *HumanControlledTodoPlannerOrchestrator) runSuccessLearningPhase(ctx context.Context, learningPathIdentifier string, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse, isCodeExecutionMode bool) error {
	// Use step-specific learning detail level, default to "exact" if not set
	learningDetailLevel := "exact" // default
	if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = step.AgentConfigs.LearningDetailLevel
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Using step-specific learning detail level: '%s'", learningDetailLevel))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No step-specific learning detail level set, using default: 'exact'"))
	}

	// LOCK LEARNINGS: Check if learnings are locked (prevents learning agent from running but still uses existing learnings)
	// Note: Lock learnings takes precedence - even in code execution mode, if learnings are locked, skip learning agent
	isLearningsLocked := step.AgentConfigs != nil && step.AgentConfigs.LockLearnings != nil && *step.AgentConfigs.LockLearnings
	if isLearningsLocked {
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping success learning analysis for %s/%d (using existing learnings)", learningPathIdentifier, totalSteps))
		return nil
	}

	// Skip learning if "none" is selected or learning is disabled
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	// Use the provided step-specific code execution mode (already computed with step-level priority)
	shouldSkipLearning := (learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning)) && !isCodeExecutionMode
	if shouldSkipLearning {
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping success learning analysis for %s/%d (learning disabled)", learningPathIdentifier, totalSteps))
		return nil
	}
	if isCodeExecutionMode && (learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning)) {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing success learning for %s/%d (overriding step config)", learningPathIdentifier, totalSteps))
		// Override learning detail level to "exact" if it was "none"
		if learningDetailLevel == "none" {
			learningDetailLevel = "exact"
		}
	}

	// Success learning agent ALWAYS runs - it writes learnings (creates folder if needed)
	// Only the learning reading agent (which reads existing learnings) should check folder existence
	hcpo.GetLogger().Info(fmt.Sprintf("🧠 Starting success learning analysis for %s/%d: %s", learningPathIdentifier, totalSteps, step.Title))

	// Create success learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	successLearningAgentName := fmt.Sprintf("%s-success-learning-%s-%s", learningPathIdentifier, sanitizedTitle, learningMode)
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", learningPathIdentifier, successLearningAgentName, step.AgentConfigs, isCodeExecutionMode)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to create success learning agent: %w", err), nil)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for success learning agent
	successLearningTemplateVars := map[string]string{
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"StepContextOutput":   step.ContextOutput,
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
	// This allows learnings to be at learnings/step-{X}/ or learnings/step-{X}-{branch}/ (at workspace root, not inside runs/)
	successLearningTemplateVars["StepExecutionPath"] = runWorkspacePath
	successLearningTemplateVars["StepNumber"] = learningPathIdentifier // Use learning path identifier instead of numeric step number

	// Add context dependencies as a comma-separated string
	if len(step.ContextDependencies) > 0 {
		successLearningTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
	} else {
		successLearningTemplateVars["StepContextDependencies"] = ""
	}

	// Add variable names if available
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		successLearningTemplateVars["VariableNames"] = variableNames
	}

	// Check if existing learning file exists and pass its path
	// Extract step number from learning path identifier for getExistingLearningFilePath (which expects numeric step number)
	// For branch steps, we'll use the parent step number
	var stepNumberForFileCheck int
	fmt.Sscanf(learningPathIdentifier, "step-%d", &stepNumberForFileCheck)
	existingLearningFilePath := hcpo.getExistingLearningFilePath(ctx, stepNumberForFileCheck, step.Title)
	if existingLearningFilePath != "" {
		successLearningTemplateVars["ExistingLearningFilePath"] = existingLearningFilePath
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Found existing learning file: %s", existingLearningFilePath))
	} else {
		successLearningTemplateVars["ExistingLearningFilePath"] = ""
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No existing learning file found for %s", learningPathIdentifier))
	}

	// Execute success learning agent and capture output
	_, _, err = successLearningAgent.Execute(ctx, successLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("success learning analysis failed: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Success learning analysis completed for %s (detail level: %s)", learningPathIdentifier, learningDetailLevel))
	return nil
}

// runFailureLearningPhase analyzes failed executions to provide refined task descriptions for retry
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
// runFailureLearningPhase analyzes failed executions to provide refined task descriptions
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) runFailureLearningPhase(ctx context.Context, learningPathIdentifier string, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse, isCodeExecutionMode bool) (string, string, error) {
	// Use step-specific learning detail level, default to "exact" if not set
	learningDetailLevel := "exact" // default
	if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = step.AgentConfigs.LearningDetailLevel
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Using step-specific learning detail level: '%s'", learningDetailLevel))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No step-specific learning detail level set, using default: 'exact'"))
	}

	// LOCK LEARNINGS: Check if learnings are locked (prevents learning agent from running but still uses existing learnings)
	// Note: Lock learnings takes precedence - even in code execution mode, if learnings are locked, skip learning agent
	isLearningsLocked := step.AgentConfigs != nil && step.AgentConfigs.LockLearnings != nil && *step.AgentConfigs.LockLearnings
	if isLearningsLocked {
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping failure learning analysis for %s/%d (using existing learnings)", learningPathIdentifier, totalSteps))
		return "", "", nil
	}

	// Skip learning if "none" is selected or learning is disabled
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	// Use the provided step-specific code execution mode (already computed with step-level priority)
	shouldSkipLearning := (learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning)) && !isCodeExecutionMode
	if shouldSkipLearning {
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping failure learning analysis for %s/%d (learning disabled)", learningPathIdentifier, totalSteps))
		return "", "", nil
	}
	if isCodeExecutionMode && (learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning)) {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing failure learning for %s/%d (overriding step config)", learningPathIdentifier, totalSteps))
		// Override learning detail level to "exact" if it was "none"
		if learningDetailLevel == "none" {
			learningDetailLevel = "exact"
		}
	}

	// Failure learning agent ALWAYS runs - it writes learnings (creates folder if needed)
	// Only the learning reading agent (which reads existing learnings) should check folder existence
	hcpo.GetLogger().Info(fmt.Sprintf("🧠 Starting failure learning analysis for %s/%d: %s", learningPathIdentifier, totalSteps, step.Title))

	// Create failure learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name (exact or general)
	learningMode := "exact"
	if learningDetailLevel == "general" {
		learningMode = "general"
	}
	failureLearningAgentName := fmt.Sprintf("%s-failure-learning-%s-%s", learningPathIdentifier, sanitizedTitle, learningMode)
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", learningPathIdentifier, failureLearningAgentName, step.AgentConfigs, isCodeExecutionMode)
	if err != nil {
		return "", "", fmt.Errorf(fmt.Sprintf("failed to create failure learning agent: %w", err), nil)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for failure learning agent
	failureLearningTemplateVars := map[string]string{
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"StepContextOutput":   step.ContextOutput,
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
	// This allows learnings to be at learnings/step-{X}/ or learnings/step-{X}-{branch}/ (at workspace root, not inside runs/)
	failureLearningTemplateVars["StepExecutionPath"] = runWorkspacePath
	failureLearningTemplateVars["StepNumber"] = learningPathIdentifier // Use learning path identifier instead of numeric step number

	// Add context dependencies as a comma-separated string
	if len(step.ContextDependencies) > 0 {
		failureLearningTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
	} else {
		failureLearningTemplateVars["StepContextDependencies"] = ""
	}

	// Add variable names if available
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		failureLearningTemplateVars["VariableNames"] = variableNames
	}

	// Check if existing learning file exists and pass its path
	// Extract step number from learning path identifier for getExistingLearningFilePath (which expects numeric step number)
	// For branch steps, we'll use the parent step number
	var stepNumberForFileCheck int
	fmt.Sscanf(learningPathIdentifier, "step-%d", &stepNumberForFileCheck)
	existingLearningFilePath := hcpo.getExistingLearningFilePath(ctx, stepNumberForFileCheck, step.Title)
	if existingLearningFilePath != "" {
		failureLearningTemplateVars["ExistingLearningFilePath"] = existingLearningFilePath
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Found existing learning file: %s", existingLearningFilePath))
	} else {
		failureLearningTemplateVars["ExistingLearningFilePath"] = ""
		hcpo.GetLogger().Info(fmt.Sprintf("📄 No existing learning file found for %s", learningPathIdentifier))
	}

	// Execute failure learning agent and capture output
	failureLearningOutput, _, err := failureLearningAgent.Execute(ctx, failureLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return "", "", fmt.Errorf(fmt.Sprintf("failure learning analysis failed: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Failure learning analysis completed for %s (detail level: %s)", learningPathIdentifier, learningDetailLevel))
	return failureLearningOutput, failureLearningOutput, nil
}

// readStepLearningFiles reads all .md learning files from a step-specific folder
// In code execution mode, also reads .go files from the code/ subfolder
// Returns a map of filename -> content
func (hcpo *HumanControlledTodoPlannerOrchestrator) readStepLearningFiles(ctx context.Context, stepLearningsPath string) (map[string]string, error) {
	learningFiles := make(map[string]string)

	// List all files in the step folder
	files, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, stepLearningsPath)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to list files in %s: %w", stepLearningsPath, err), nil)
	}

	// Read all .md files from the step folder
	for _, file := range files {
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

	return learningFiles, nil
}

// formatStepLearningFilesAsHistory formats a map of learning files (filename -> content) into a formatted history string
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatStepLearningFilesAsHistory(learningFiles map[string]string) string {
	if len(learningFiles) == 0 {
		return "No learning history available."
	}

	var result strings.Builder
	result.WriteString("## 📚 Learning Context\n\n")

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
		result.WriteString(fmt.Sprintf("### 📄 %s\n\n", filename))
		result.WriteString(content)
		result.WriteString("\n")
	}

	return result.String()
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
