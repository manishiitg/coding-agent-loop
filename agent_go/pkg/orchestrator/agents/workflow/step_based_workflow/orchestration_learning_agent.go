package step_based_workflow

import (
	"context"
	"fmt"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

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
- **Tier Selection**: Which LLM tier (1=High, 2=Medium, 3=Low) was used per route? Did lower tiers fail where higher tiers succeeded? Which routes are simple enough for Tier 2/3?
- **Task-Specific Failures**: Document routing/evaluation errors (ignore general code issues).

## 📁 FILE MANAGEMENT ALGORITHM (MANDATORY)
**Available tools**: execute_shell_command (for listing, reading, and deleting files) and diff_patch_workspace_file (for writing/updating files).
1. **Discover**: Use execute_shell_command with 'ls' to list 'SKILL.md' or any '*orchestrator_learning.md' files (legacy format) in '{{.WritePath}}'.
2. **Retrieve**: Use execute_shell_command with 'cat' to read ALL variations found.
3. **Legacy Migration**: If you find '*orchestrator_learning.md' files (legacy format) but no 'SKILL.md':
   - Read the legacy content and incorporate it into the new SKILL.md format with proper YAML frontmatter.
   - Derive the 'description' field from the legacy content (summarize the key routing/orchestration patterns).
   - Delete the legacy files after writing SKILL.md.
4. **Consolidate**: Merge current findings with history into ONE final file.
   - Prioritize LATEST successful patterns.
   - Anonymize variables ({{ "{{" }}VARS{{ "}}" }}) and normalize paths.
4. **Persist**: Use diff_patch_workspace_file to write ONE consolidated file to '{{.WritePath}}/SKILL.md'.
   The file MUST use YAML frontmatter in the following format:
   ` + "`" + `` + "`" + `` + "`" + `
   ---
   name: orchestrator-learning
   description: "Auto-generated orchestration learning"
   disable-model-invocation: true
   user-invocable: false
   ---

   (learning content here)
   ` + "`" + `` + "`" + `` + "`" + `
5. **Clean**: Use execute_shell_command with 'rm' to delete ALL '*orchestrator_learning.md' files and old learning files in that folder. Only 'SKILL.md' should remain.

## 📤 OUTPUT FORMAT
- **⭐ OPTIMAL ROUTING PATTERN** [Runs: X | Success: Y%]
- **🎯 ROUTE SELECTION PATTERNS**
- **✅ SUCCESS CRITERIA EVALUATION PATTERNS**
- **🏷️ TIER RECOMMENDATIONS** — Per-route tier assignment based on observed complexity and success/failure patterns:
  ` + "`" + `` + "`" + `` + "`" + `
  ## TIER RECOMMENDATIONS
  - route: {route_id} | tier: {1/2/3} | reason: {brief justification based on task complexity and observed results}
  - route: generic | tier: {1/2/3} | reason: {brief justification}
  ` + "`" + `` + "`" + `` + "`" + `
  The orchestrator reads this section at runtime to pick preferred_tier for each sub-agent call.
- **❌ FAILURES TO AVOID**

*Final Action: Output ONLY the updated file path. No summaries.*`)

var orchestrationLearningUserTemplate = MustRegisterTemplate("orchestrationLearningUser", `# Orchestration Learning Task

## 📋 Context
- **Step**: {{.StepTitle}} ({{.StepNumber}})
- **Goal**: {{.StepDescription}}
- **Routes**: [See below]

## 🧠 Instructions
1. **CONSOLIDATE**:
   - Use execute_shell_command with 'ls' to list files in '{{.WritePath}}'.
   - Use execute_shell_command with 'cat' to read 'SKILL.md' or any '*orchestrator_learning.md' files (legacy format).
   - Merge current history with existing patterns.
2. **EXTRACT**:
   - Map routing decisions and success evaluations.
   - Replace variables with placeholders: {{.Variables}}
3. **PERSIST & CLEAN**:
   - Use diff_patch_workspace_file to write ONE file: '{{.WritePath}}/SKILL.md' (with YAML frontmatter as described in system prompt).
   - Use execute_shell_command with 'rm' to delete all other '*orchestrator_learning.md' and old learning files.

---
## 🎯 AVAILABLE ROUTES
{{.OrchestrationRoutes}}

---
## 📊 ORCHESTRATION HISTORY
{{.OrchestrationHistory}}

---
## ✅ VALIDATION RESULTS
{{.ValidationResult}}

**Final Action**: Output ONLY the file path 'Updated: {{.WritePath}}/SKILL.md'.`)

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
		panic(fmt.Sprintf("orchestration learning system prompt template execution failed (missing variable?): %v", err))
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
		panic(fmt.Sprintf("orchestration learning user message template execution failed (missing variable?): %v", err))
	}

	return result.String()
}
