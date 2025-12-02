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
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) runSuccessLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse, isCodeExecutionMode bool) (string, error) {
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
	// Use the provided step-specific code execution mode (already computed with step-level priority)
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

	// Success learning agent ALWAYS runs - it writes learnings (creates folder if needed)
	// Only the learning reading agent (which reads existing learnings) should check folder existence
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
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", stepNumber, 1, successLearningAgentName, step.AgentConfigs, isCodeExecutionMode)
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
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) runFailureLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse, isCodeExecutionMode bool) (string, string, error) {
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
	// Use the provided step-specific code execution mode (already computed with step-level priority)
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

	// Failure learning agent ALWAYS runs - it writes learnings (creates folder if needed)
	// Only the learning reading agent (which reads existing learnings) should check folder existence
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
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", stepNumber, 1, failureLearningAgentName, step.AgentConfigs, isCodeExecutionMode)
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
// Extracts the most relevant content: learning file contents and agent's final analysis
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatLearningHistoryForExecution(learningHistory []llmtypes.MessageContent) string {
	if len(learningHistory) == 0 {
		return "No learning history available."
	}

	var result strings.Builder
	var learningFileContents []string
	var agentSummary string

	// Extract relevant content from conversation history
	for _, message := range learningHistory {
		// Skip user messages (instructions)
		if message.Role == llmtypes.ChatMessageTypeHuman {
			continue
		}

		for _, part := range message.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				// Capture the last assistant text as the summary/analysis
				if message.Role == llmtypes.ChatMessageTypeAI && p.Text != "" {
					agentSummary = p.Text
				}

			case llmtypes.ToolCallResponse:
				// Only capture read_workspace_file responses for learning files
				if strings.Contains(p.Name, "read_workspace_file") || strings.Contains(p.Name, "read_file") {
					content := p.Content
					// Check if this is a learning file (contains workflow or patterns)
					if strings.Contains(content, "_learning.md") ||
						strings.Contains(content, "EXECUTION WORKFLOW") ||
						strings.Contains(content, "SUCCESS TOOL") ||
						strings.Contains(content, "SUCCESS CODE") ||
						strings.Contains(content, "[Runs:") {
						learningFileContents = append(learningFileContents, content)
					}
				}
			}
		}
	}

	// Build the formatted output
	result.WriteString("## 📚 Learning Context for Execution\n\n")

	// Include learning file contents (the actual patterns/workflows)
	if len(learningFileContents) > 0 {
		result.WriteString("### 📄 Learning File Contents\n\n")
		for i, content := range learningFileContents {
			if i > 0 {
				result.WriteString("\n---\n\n")
			}
			result.WriteString(content)
			result.WriteString("\n")
		}
		result.WriteString("\n")
	}

	// Include agent's analysis/summary
	if agentSummary != "" {
		result.WriteString("### 🔍 Learning Agent Analysis\n\n")
		result.WriteString(agentSummary)
		result.WriteString("\n\n")
	}

	// If no learning content found, indicate that
	if len(learningFileContents) == 0 && agentSummary == "" {
		result.WriteString("No learning patterns found for this step.\n")
		result.WriteString("Execute based on step description and success criteria.\n\n")
	}

	// Add usage guidance based on content type
	if len(learningFileContents) > 0 {
		// Check if we have workflow-style learnings
		hasWorkflow := false
		for _, content := range learningFileContents {
			if strings.Contains(content, "EXECUTION WORKFLOW") || strings.Contains(content, "Step 1:") {
				hasWorkflow = true
				break
			}
		}

		if hasWorkflow {
			result.WriteString("### ⚡ Execution Mode: WORKFLOW\n")
			result.WriteString("**Follow the EXECUTION WORKFLOW steps in order.**\n")
			result.WriteString("- Execute Step 1 → Step 2 → Step 3... exactly as documented\n")
			result.WriteString("- Check prerequisites before each step\n")
			result.WriteString("- Use exact tool calls and arguments (resolve variables)\n")
			result.WriteString("- Apply error recovery if steps fail\n\n")
		} else {
			result.WriteString("### ⚡ Execution Mode: PATTERN-GUIDED\n")
			result.WriteString("**Use the patterns above as guidance.**\n")
			result.WriteString("- Adapt successful patterns to current step requirements\n")
			result.WriteString("- Avoid documented failure patterns\n")
			result.WriteString("- Step description is the primary source of truth\n\n")
		}
	}

	return result.String()
}
