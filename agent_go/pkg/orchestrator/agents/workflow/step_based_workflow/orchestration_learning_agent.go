package step_based_workflow

import (
	"context"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for orchestration learning - panics at startup if invalid
var orchestrationLearningSystemTemplate = MustRegisterTemplate("orchestrationLearningSystem", `# Orchestration Learning Agent

## 🤖 Role
Expert agent extracting routing decisions, success criteria evaluations, and delegation strategies.

## 🚨 CRITICAL PRINCIPLE
Only capture learnings specific to **orchestrator decision-making**.

## 🔍 EXTRACTION Checklist
- **Routing**: Which routes were selected and why (condition matching)?
- **Evaluation**: What indicated success criteria were met?
- **Delegation**: When was timing optimal? What made sub-agent instructions effective?
- **Task-Specific Failures**: Document routing/evaluation errors (ignore general code issues).

## 📁 FILE MANAGEMENT ALGORITHM (MANDATORY)
1. **Discover**: List ALL '*orchestrator_learning.md' files in '{{.WritePath}}'.
2. **Retrieve**: Read ALL variations found.
3. **Consolidate**: Merge current findings with history into ONE final file.
   - Prioritize LATEST successful patterns.
   - Anonymize variables ({{ "{{" }}VARS{{ "}}" }}) and normalize paths.
4. **Persist**: Write ONE consolidated file to '{{.WritePath}}/orchestrator_learning.md'.
5. **Clean**: Delete ALL other '*orchestrator_learning.md' files in that folder.

## 📤 OUTPUT FORMAT
- **⭐ OPTIMAL ROUTING PATTERN** [Runs: X | Success: Y%]
- **🎯 ROUTE SELECTION PATTERNS**
- **✅ SUCCESS CRITERIA EVALUATION PATTERNS**
- **❌ FAILURES TO AVOID**

*Final Action: Output ONLY the updated file path. No summaries.*`)

var orchestrationLearningUserTemplate = MustRegisterTemplate("orchestrationLearningUser", `# Orchestration Learning Task

## 📋 Context
- **Step**: {{.StepTitle}} ({{.StepNumber}})
- **Goal**: {{.StepDescription}}
- **Routes**: [See below]

## 🧠 Instructions
1. **CONSOLIDATE**:
   - List files in '{{.WritePath}}'.
   - Read ALL '*orchestrator_learning.md' files.
   - Merge current history with existing patterns.
2. **EXTRACT**:
   - Map routing decisions and success evaluations.
   - Replace variables with placeholders: {{.Variables}}
3. **PERSIST & CLEAN**:
   - Write ONE file: '{{.WritePath}}/orchestrator_learning.md'.
   - Delete all other '*orchestrator_learning.md' files.

---
## 🎯 AVAILABLE ROUTES
{{.OrchestrationRoutes}}

---
## 📊 ORCHESTRATION HISTORY
{{.OrchestrationHistory}}

---
## ✅ VALIDATION RESULTS
{{.ValidationResult}}

**Final Action**: Output ONLY the file path 'Updated: {{.WritePath}}/orchestrator_learning.md'.`)

// OrchestrationLearningTemplate holds template variables for orchestrator learning prompts
type OrchestrationLearningTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	OrchestrationHistory    string // Orchestrator conversation history and routing decisions
	ValidationResult        string
	OrchestrationRoutes     string // Available routes and their conditions
}

// WorkflowOrchestrationLearningAgent analyzes orchestrator decisions to capture learnings
// Focuses on routing decisions, success criteria evaluation, and delegation patterns
type WorkflowOrchestrationLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowOrchestrationLearningAgent creates a new orchestrator learning agent
func NewWorkflowOrchestrationLearningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowOrchestrationLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType, // Reuse same agent type
		eventBridge,
	)

	return &WorkflowOrchestrationLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (agent *WorkflowOrchestrationLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	stepContextOutput := templateVars["StepContextOutput"]
	workspacePath := templateVars["WorkspacePath"]
	orchestrationHistory := templateVars["OrchestrationHistory"]
	validationResult := templateVars["ValidationResult"]
	orchestrationRoutes := templateVars["OrchestrationRoutes"]
	variableNames := templateVars["VariableNames"]
	existingLearningsContent := templateVars["ExistingLearningsContent"]

	// Prepare template variables
	learningTemplateVars := map[string]string{
		"StepTitle":                stepTitle,
		"StepDescription":          stepDescription,
		"StepSuccessCriteria":      stepSuccessCriteria,
		"StepContextDependencies":  stepContextDependencies,
		"StepContextOutput":        stepContextOutput,
		"WorkspacePath":            workspacePath,
		"OrchestrationHistory":     orchestrationHistory,
		"ValidationResult":         validationResult,
		"OrchestrationRoutes":      orchestrationRoutes,
		"VariableNames":            variableNames,
		"ExistingLearningsContent": existingLearningsContent,
	}

	// Add step-specific paths if available
	if stepExecutionPath, ok := templateVars["StepExecutionPath"]; ok {
		learningTemplateVars["StepExecutionPath"] = stepExecutionPath
	}
	if stepNumber, ok := templateVars["StepNumber"]; ok {
		learningTemplateVars["StepNumber"] = stepNumber
	}

	// Create template data for learning
	templateData := OrchestrationLearningTemplate{
		StepTitle:               stepTitle,
		StepDescription:         stepDescription,
		StepSuccessCriteria:     stepSuccessCriteria,
		StepContextDependencies: stepContextDependencies,
		StepContextOutput:       stepContextOutput,
		WorkspacePath:           workspacePath,
		OrchestrationHistory:    orchestrationHistory,
		ValidationResult:        validationResult,
		OrchestrationRoutes:     orchestrationRoutes,
	}

	// Generate system prompt and user message
	systemPrompt := agent.orchestrationLearningSystemPromptProcessor(learningTemplateVars)
	userMessage := agent.orchestrationLearningUserMessageProcessor(learningTemplateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute with system prompt and user message
	return agent.ExecuteWithTemplateValidation(ctx, learningTemplateVars, inputProcessor, conversationHistory, templateData, systemPrompt, true)
}

// orchestrationLearningSystemPromptProcessor creates the system prompt for orchestrator learning
func (agent *WorkflowOrchestrationLearningAgent) orchestrationLearningSystemPromptProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := orchestrationLearningSystemTemplate.Execute(&result, map[string]interface{}{
		"WritePath": templateVars["WorkspacePath"] + "/learnings/" + templateVars["StepNumber"],
	}); err != nil {
		return "Error executing orchestration learning system prompt template: " + err.Error()
	}
	return result.String()
}

// orchestrationLearningUserMessageProcessor creates the user message for orchestrator learning
func (agent *WorkflowOrchestrationLearningAgent) orchestrationLearningUserMessageProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	writePath := workspacePath + "/learnings/" + stepNumber

	var result strings.Builder
	if err := orchestrationLearningUserTemplate.Execute(&result, map[string]interface{}{
		"StepTitle":            templateVars["StepTitle"],
		"StepNumber":           stepNumber,
		"StepDescription":      templateVars["StepDescription"],
		"WritePath":            writePath,
		"Variables":            templateVars["VariableNames"],
		"OrchestrationRoutes":  templateVars["OrchestrationRoutes"],
		"OrchestrationHistory": templateVars["OrchestrationHistory"],
		"ValidationResult":     templateVars["ValidationResult"],
	}); err != nil {
		return "Error executing orchestration learning user message template: " + err.Error()
	}

	return result.String()
}
