package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/agent/prompt"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerOrchestrationOrchestratorAgent executes the main orchestration step
// This agent focuses on orchestration and delegation, not direct execution
type HumanControlledTodoPlannerOrchestrationOrchestratorAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerOrchestrationOrchestratorAgent creates a new orchestration orchestrator agent
func NewHumanControlledTodoPlannerOrchestrationOrchestratorAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerOrchestrationOrchestratorAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.OrchestrationAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerOrchestrationOrchestratorAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// OrchestrationOrchestratorTemplate holds template variables for orchestration orchestrator agent prompts
type OrchestrationOrchestratorTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	IsCodeExecutionMode     string
	VariableNames           string
	VariableValues          string
	StepNumber              string
	StepExecutionPath       string
	PreviousStepsSummary    string
	OrchestrationRoutes     string // Description of available sub-agents
}

// Execute implements the OrchestratorAgent interface
// NOTE: This is a minimal implementation that delegates to ExecuteStructured.
// ExecuteStructured should be used directly for orchestration steps.
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Delegate to ExecuteStructured and convert the result to string format
	response, updatedHistory, err := hctpooa.ExecuteStructured(ctx, templateVars, conversationHistory)
	if err != nil {
		return "", nil, err
	}

	// Convert structured response to string format for backward compatibility
	result := fmt.Sprintf("Success Criteria Met: %t\nSelected Route: %s",
		response.SuccessCriteriaMet, response.SelectedRouteID)
	if response.SuccessCriteriaMet && response.SuccessReasoning != "" {
		result += fmt.Sprintf("\nSuccess Reasoning: %s", response.SuccessReasoning)
	}

	return result, updatedHistory, nil
}

// ExecuteStructured executes the orchestration orchestrator agent and returns structured OrchestrationResponse
// This includes routing decisions (which sub-agent to use) and success criteria evaluation
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*OrchestrationResponse, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := hctpooa.orchestrationOrchestratorSystemPromptProcessorStructured(templateVars)
	userMessage := hctpooa.orchestrationOrchestratorUserMessageProcessor(templateVars, conversationHistory)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Build schema for structured output
	schema := `{
		"type": "object",
		"properties": {
			"selected_route_id": {
				"type": "string",
				"description": "ID of the route (sub-agent) to execute from the available orchestration_routes. REQUIRED if success_criteria_met is false (you MUST always delegate to a sub-agent when success criteria is not met). Empty string only if success_criteria_met is true. If an \"end\" route exists in orchestration_routes, you can select it (route_id: \"end\") to immediately terminate the entire workflow when you determine the objective is complete."
			},
			"success_criteria_met": {
				"type": "boolean",
				"description": "Whether the orchestration step's success criteria is met"
			},
			"success_reasoning": {
				"type": "string",
				"description": "Detailed reasoning for success criteria evaluation. Required if success_criteria_met is true."
			},
			"instructions_to_sub_agent": {
				"type": "string",
				"description": "VERY DETAILED and PRECISE instructions to pass to the selected sub-agent. REQUIRED if selected_route_id is provided (not empty). Must be extremely specific - include exact actions, specific file names, precise steps, exact commands. Leave no ambiguity - the sub-agent must know EXACTLY what to do without any guessing. Include: specific actions to take, exact approach to follow, important context, expected behavior, exact file paths/names, precise requirements, any edge cases. Format: Use numbered steps, clear bullet points, explicit commands. Examples of good instructions: '1. Read file step-1/credentials.json. 2. Extract api_key field. 3. Create step-2/api_config.json with structure: {\"key\": \"<extracted_key>\"}. 4. Validate JSON before writing.' Examples of bad instructions: 'Process credentials' or 'Create config' (too vague). Make instructions comprehensive, actionable, unambiguous, and PRECISE."
			},
			"success_criteria_for_sub_agent": {
				"type": "string",
				"description": "Measurable and verifiable success criteria to pass to the selected sub-agent. REQUIRED if selected_route_id is provided (not empty). These criteria REPLACE the sub-agent's original success criteria and must be MEASURABLE and VERIFIABLE. Must be file-verifiable (reference specific file names, not paths), quantifiable (specific numbers, states, or conditions), and testable (can be objectively verified). Examples: 'File X contains exactly 5 entries', 'File Y exists with status field set to \"completed\"', 'Output file Z has validation errors count of 0'."
			},
			"context_dependencies_for_sub_agent": {
				"type": "string",
				"description": "Context dependencies to pass to the selected sub-agent. OPTIONAL if selected_route_id is provided. These dependencies REPLACE the sub-agent's original context dependencies and specify which files the sub-agent should read as input. Format: comma-separated list of relative file paths (e.g., \"step-1/output.json, step-2/credentials.json\")."
			},
			"context_output_for_sub_agent": {
				"type": "string",
				"description": "Context output file name to pass to the selected sub-agent. OPTIONAL if selected_route_id is provided. This REPLACES the sub-agent's original context output and specifies the output file name the sub-agent should create (e.g., \"step_3_output.json\"). The file will be created in the sub-agent's step folder."
			}
		},
		"required": ["success_criteria_met", "success_reasoning"]
	}`

	// Define tool name and description for structured output via tool calls
	// This single tool handles two scenarios:
	// 1. call_sub_agent: When calling a sub-agent (provide selected_route_id, instructions_to_sub_agent, success_criteria_for_sub_agent)
	// 2. completed_success_criteria: When success criteria is met (provide success_criteria_met: true, success_reasoning)
	toolName := "submit_orchestration_result"
	toolDescription := `Submit the orchestration result. This tool handles two scenarios:
1. **call_sub_agent**: When calling a sub-agent - provide selected_route_id (required), instructions_to_sub_agent (required), success_criteria_for_sub_agent (required), context_dependencies_for_sub_agent (optional), context_output_for_sub_agent (optional), success_criteria_met: false
2. **completed_success_criteria**: When success criteria is met - provide success_criteria_met: true, success_reasoning (required), selected_route_id: ""`

	// Use ExecuteStructuredWithInputProcessorViaTool
	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[OrchestrationResponse](
		hctpooa.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		schema,
		systemPrompt,
		true, // Overwrite system prompt
		toolName,
		toolDescription,
	)

	if err != nil {
		return nil, nil, fmt.Errorf("orchestration orchestrator structured execution failed: %w", err)
	}

	return &result, updatedHistory, nil
}

// orchestrationOrchestratorSystemPromptProcessorStructured generates the system prompt for structured orchestration orchestrator agent
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) orchestrationOrchestratorSystemPromptProcessorStructured(templateVars map[string]string) string {
	now := time.Now()
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"

	templateData := map[string]interface{}{
		"CurrentDate":               now.Format("2006-01-02"),
		"CurrentTime":               now.Format("15:04:05"),
		"OrchestrationRoutes":       templateVars["OrchestrationRoutes"],
		"VariableNames":             templateVars["VariableNames"],
		"VariableValues":            templateVars["VariableValues"],
		"IsCodeExecutionMode":       isCodeExecutionMode,
		"WorkspacePath":             templateVars["WorkspacePath"],
		"StepNumber":                templateVars["StepNumber"],
		"StepExecutionPath":         templateVars["StepExecutionPath"],
		"PreviousStepsSummary":      templateVars["PreviousStepsSummary"],
		"LearningHistory":           templateVars["LearningHistory"],
		"CodeExecutionInstructions": "",
	}

	if isCodeExecutionMode {
		templateData["CodeExecutionInstructions"] = prompt.GetCodeExecutionInstructions()
	}

	templateStr := `# Orchestration Orchestrator Agent
**Session**: {{.CurrentDate}} {{.CurrentTime}} | **Mode**: ORCHESTRATION

## 🤖 ROLE
Coordinate work between sub-agents. You evaluate the situation, verify success, and delegate the NEXT logical action.

## ⚠️ CRITICAL RULES
1. **Mandatory Delegation**: If 'Success Criteria' is NOT met, you MUST select a sub-agent route.
2. **Evidence-Based**: Never guess. Use tools to verify files, status, and state before deciding.
3. **Precise Instructions**: When delegating, provide step-by-step instructions that include exact file names and tool parameters.
4. **Learning**: Use 'learning' route after success OR after surfacing a new routing pattern.

---

## 🏗️ AVAILABLE ROUTES
{{.OrchestrationRoutes}}

**Special Routes**:
- **"learning"**: Capture routing/evaluation patterns (recommended after success).
- **"end"**: Only use if the entire workflow objective is definitively complete.

---

{{if .VariableNames}}
## 🔑 VARIABLES
{{.VariableNames}}
{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}

**Handling**: Values are already injected in step descriptions. For Go code/tools, use these values directly.
{{if .IsCodeExecutionMode}}
**Go execution Rules**:
- **Path**: 'basePath := os.Args[1]'. Use 'filepath.Join(basePath, "relative/path")'.
- **Args**: Pass '{{.WorkspacePath}}' as the first argument in 'args'.
- **NEVER** hardcode absolute paths.
{{end}}{{end}}

{{if .LearningHistory}}
## 📚 ORCHESTRATOR LEARNINGS
{{.LearningHistory}}
{{end}}

---

## 🔍 EVALUATION & ROUTING FRAMEWORK

### 1. Analysis
- **Goal**: Analyze Context Dependencies vs. Success Criteria.
- **Tools**: Use 'read_workspace_file', 'list_workspace_files', and MCP tools to gather factual evidence of the current state.

### 2. Success Verification (MANDATORY)
- **Cross-Reference**: Verify 'Success Criteria' by cross-referencing Workspace State (artifacts) vs. Execution History (tool calls).
- **Authenticity**: Detect "Fake" work. If artifacts exist but history shows NO relevant tool calls (APIs, DBs, Shell) were made to generate them, it is a hallucination. FAIL the evaluation.
- **Evidence**: Your reasoning MUST cite specific file content or tool outputs.

### 3. Decision
- **If Met**: Call 'submit_orchestration_result' with 'success_criteria_met: true'.
- **If NOT Met**: Call 'submit_orchestration_result' with 'success_criteria_met: false' and:
  - **selected_route_id**: Choose the best sub-agent for the current gap.
  - **instructions_to_sub_agent**: Provide specific, unambiguous steps.
  - **success_criteria_for_sub_agent**: Provide verifiable criteria for the sub-agent.

---

## 📁 FILE ADVISORY
 
 | Path Type | Location | Behavior | Best Use |
 | :--- | :--- | :--- | :--- |
 | **Volatile** | {{.StepExecutionPath}}/ | Deleted on re-execution | Iteration-specific logs |
 | **Persistent** | knowledgebase/ | Never deleted across runs | Templates, Shared Config, Reference Data |
 | **Global** | execution/ | Read-only access | Cross-step dependencies |
 
- **Knowledgebase**: Use 'knowledgebase/' to store assets that should persist between different execution attempts (e.g., a "gold standard" template, long-term lookup tables, or project-level settings).
 - **Recovery**: Update '{{.StepExecutionPath}}/progress.md' with reasoning after major decisions.

*Output ONLY via 'submit_orchestration_result'.*`

	tmpl, err := template.New("orchestrationSystem").Parse(templateStr)
	if err != nil {
		return "Error parsing orchestration system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return "Error executing orchestration system prompt template: " + err.Error()
	}
	return result.String()
}

// orchestrationOrchestratorUserMessageProcessor generates the user message for orchestration orchestrator agent
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) orchestrationOrchestratorUserMessageProcessor(templateVars map[string]string, conversationHistory []llmtypes.MessageContent) string {
	templateStr := `# Orchestration Task: {{.StepTitle}}
**Description**: {{.StepDescription}}
**Success Criteria**: {{.StepSuccessCriteria}}

## 📋 CONTEXT
- **Workspace**: {{.WorkspacePath}}
- **Step ID**: {{.StepNumber}}
- **Output Folder**: {{.StepExecutionPath}}/ (VOLATILE)
- **Dependencies**: {{.StepContextDependencies}}

{{if .PreviousStepsSummary}}
## 📊 History Summary
{{.PreviousStepsSummary}}
{{end}}

{{if .ValidationMessages}}
## 🧠 VALIDATION FEEDBACK
{{range .ValidationMessages}}- {{.}}
{{end}}{{end}}

## 🚀 ACTION
1. Evaluate current workspace state.
2. Decide: Is the goal complete or do we need a sub-agent?
3. Call 'submit_orchestration_result'.`

	// Extract validation messages from history
	var validationMessages []string
	for _, msg := range conversationHistory {
		content := ""
		for _, part := range msg.Parts {
			if textPart, ok := part.(llmtypes.TextContent); ok {
				content += textPart.Text
			}
		}
		lower := strings.ToLower(content)
		if strings.Contains(lower, "validation agent completed") || strings.Contains(lower, "validation failed") {
			validationMessages = append(validationMessages, strings.TrimSpace(content))
		}
	}

	templateData := map[string]interface{}{
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"WorkspacePath":           templateVars["WorkspacePath"],
		"StepNumber":              templateVars["StepNumber"],
		"StepExecutionPath":       templateVars["StepExecutionPath"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"PreviousStepsSummary":    templateVars["PreviousStepsSummary"],
		"ValidationMessages":      validationMessages,
	}

	tmpl, err := template.New("orchestrationUser").Parse(templateStr)
	if err != nil {
		return "Error parsing orchestration user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return "Error executing orchestration user message template: " + err.Error()
	}
	return result.String()
}
