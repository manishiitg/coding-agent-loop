package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/shared"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// runSuccessLearningPhase analyzes successful executions to capture best practices and improve plan.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) runSuccessLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse) (string, error) {
	// Use step-specific learning detail level, default to "general" if not set
	learningDetailLevel := "general" // default
	if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = step.AgentConfigs.LearningDetailLevel
		hcpo.GetLogger().Infof("📝 Using step-specific learning detail level: '%s'", learningDetailLevel)
	} else {
		hcpo.GetLogger().Infof("📝 No step-specific learning detail level set, using default: 'general'")
	}

	// Skip learning if "none" is selected or learning is disabled
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	isCodeExecutionMode := hcpo.GetUseCodeExecutionMode()
	shouldSkipLearning := (learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning)) && !isCodeExecutionMode
	if shouldSkipLearning {
		hcpo.GetLogger().Infof("⏭️ Skipping success learning analysis for step %d/%d (learning disabled)", stepNumber, totalSteps)
		return "", nil
	}
	if isCodeExecutionMode && (learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning)) {
		hcpo.GetLogger().Infof("🔧 Code execution mode enabled - forcing success learning for step %d/%d (overriding step config)", stepNumber, totalSteps)
		// Override learning detail level to "general" if it was "none"
		if learningDetailLevel == "none" {
			learningDetailLevel = "general"
		}
	}

	hcpo.GetLogger().Infof("🧠 Starting success learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create success learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	successLearningAgentName := fmt.Sprintf("step-%d-%s-%s", stepNumber, sanitizedTitle, learningMode)
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", stepNumber, 1, successLearningAgentName, step.AgentConfigs)
	if err != nil {
		return "", fmt.Errorf("failed to create success learning agent: %w", err)
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

	// Execute success learning agent and capture output
	successLearningOutput, _, err := successLearningAgent.Execute(ctx, successLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return "", fmt.Errorf("success learning analysis failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d (detail level: %s)", stepNumber, learningDetailLevel)
	return successLearningOutput, nil
}

// runFailureLearningPhase analyzes failed executions to provide refined task descriptions for retry
func (hcpo *HumanControlledTodoPlannerOrchestrator) runFailureLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse) (string, string, error) {
	// Use step-specific learning detail level, default to "general" if not set
	learningDetailLevel := "general" // default
	if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = step.AgentConfigs.LearningDetailLevel
		hcpo.GetLogger().Infof("📝 Using step-specific learning detail level: '%s'", learningDetailLevel)
	} else {
		hcpo.GetLogger().Infof("📝 No step-specific learning detail level set, using default: 'general'")
	}

	// Skip learning if "none" is selected or learning is disabled
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	isCodeExecutionMode := hcpo.GetUseCodeExecutionMode()
	shouldSkipLearning := (learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning)) && !isCodeExecutionMode
	if shouldSkipLearning {
		hcpo.GetLogger().Infof("⏭️ Skipping failure learning analysis for step %d/%d (learning disabled)", stepNumber, totalSteps)
		return "", "", nil
	}
	if isCodeExecutionMode && (learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning)) {
		hcpo.GetLogger().Infof("🔧 Code execution mode enabled - forcing failure learning for step %d/%d (overriding step config)", stepNumber, totalSteps)
		// Override learning detail level to "general" if it was "none"
		if learningDetailLevel == "none" {
			learningDetailLevel = "general"
		}
	}

	hcpo.GetLogger().Infof("🧠 Starting failure learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create failure learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	failureLearningAgentName := fmt.Sprintf("step-%d-%s-%s", stepNumber, sanitizedTitle, learningMode)
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", stepNumber, 1, failureLearningAgentName, step.AgentConfigs)
	if err != nil {
		return "", "", fmt.Errorf("failed to create failure learning agent: %w", err)
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

	// Execute failure learning agent and capture output
	failureLearningOutput, _, err := failureLearningAgent.Execute(ctx, failureLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return "", "", fmt.Errorf("failure learning analysis failed: %w", err)
	}

	// Extract refined task description from the output
	refinedTaskDescription := hcpo.extractRefinedTaskDescription(failureLearningOutput)
	learningAnalysis := failureLearningOutput // Use the full output as learning analysis

	hcpo.GetLogger().Infof("✅ Failure learning analysis completed for step %d (detail level: %s)", stepNumber, learningDetailLevel)
	return refinedTaskDescription, learningAnalysis, nil
}

// extractRefinedTaskDescription extracts the refined task description from learning agent output
func (hcpo *HumanControlledTodoPlannerOrchestrator) extractRefinedTaskDescription(learningOutput string) string {
	// Look for "### Refined Task:" section in the output
	lines := strings.Split(learningOutput, "\n")
	inRefinedTaskSection := false
	var refinedTaskLines []string

	for _, line := range lines {
		if strings.Contains(line, "### Refined Task:") {
			inRefinedTaskSection = true
			continue
		}
		if inRefinedTaskSection {
			// Stop when we hit the next section (starts with ###)
			if strings.HasPrefix(strings.TrimSpace(line), "###") && !strings.Contains(line, "Refined Task") {
				break
			}
			// Skip empty lines at the start
			if len(refinedTaskLines) == 0 && strings.TrimSpace(line) == "" {
				continue
			}
			refinedTaskLines = append(refinedTaskLines, line)
		}
	}

	refinedTask := strings.TrimSpace(strings.Join(refinedTaskLines, "\n"))
	if refinedTask == "" {
		// Fallback: return the original step description if no refined task found
		return ""
	}

	return refinedTask
}

// formatLearningHistoryForExecution formats learning conversation history for inclusion in execution-only agent system prompt
// Removes the first user message (learning discovery instruction) since execution-only agent doesn't need it
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatLearningHistoryForExecution(learningHistory []llmtypes.MessageContent) string {
	if len(learningHistory) == 0 {
		return "No learning history available."
	}

	// Filter out the first user message (learning discovery instruction)
	// The execution-only agent only needs the actual discoveries (tool calls and responses)
	filteredHistory := make([]llmtypes.MessageContent, 0, len(learningHistory))
	firstUserMessageSkipped := false

	for i, message := range learningHistory {
		// Skip the first user message (typically the instruction to discover learnings)
		if i == 0 && message.Role == llmtypes.ChatMessageTypeHuman && !firstUserMessageSkipped {
			firstUserMessageSkipped = true
			hcpo.GetLogger().Infof("🔍 Filtering out first user message from learning history (learning discovery instruction)")
			continue
		}
		filteredHistory = append(filteredHistory, message)
	}

	if len(filteredHistory) == 0 {
		return "No learning discoveries found (only instruction message was present)."
	}

	// Use shared formatter to format the filtered conversation history
	formatted := shared.FormatConversationHistory(filteredHistory)

	// Add a header to make it clear this is learning context
	return fmt.Sprintf("## Learning Discovery Results\n\n%s\n\n**Note**: Use the insights above to inform your execution approach. Adapt patterns to match the current step requirements.", formatted)
}
