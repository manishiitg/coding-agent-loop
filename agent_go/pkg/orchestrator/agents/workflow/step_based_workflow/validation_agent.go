package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// WorkflowValidationTemplate holds template variables for validation prompts
type WorkflowValidationTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
	LoopCondition           string // For loop steps: condition to check
	IsCodeExecutionMode     string // "true" or "false" - indicates if code execution mode was used
	DecisionReasoning       string // Context from decision step that routed to this step (empty if not routed from decision)
}

// ValidationFeedback represents combined issues and recommendations from validation
type ValidationFeedback struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // HIGH/MEDIUM/LOW
}

// ValidationResponse represents the structured response from validation analysis
type ValidationResponse struct {
	IsSuccessCriteriaMet bool                 `json:"is_success_criteria_met"`
	ExecutionStatus      string               `json:"execution_status"` // COMPLETED/PARTIAL/FAILED/INCOMPLETE
	Reasoning            string               `json:"reasoning"`
	Feedback             []ValidationFeedback `json:"feedback"`
	LoopConditionMet     bool                 `json:"loop_condition_met,omitempty"` // For loop steps: whether loop condition is met (only when LoopCondition is provided)
	LoopReasoning        string               `json:"loop_reasoning,omitempty"`     // For loop steps: reasoning for loop condition check (only when LoopCondition is provided)
}

// WorkflowValidationAgent validates if tasks were completed properly
type WorkflowValidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowValidationAgent creates a new human-controlled todo planner validation agent
func NewWorkflowValidationAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowValidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerValidationAgentType,
		eventBridge,
	)

	return &WorkflowValidationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hctpva *WorkflowValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf(fmt.Sprintf("Execute() is not used for validation agent - use ExecuteStructured() instead"), nil)
}

// ExecuteStructured executes the validation agent and returns structured output
func (hctpva *WorkflowValidationAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*ValidationResponse, []llmtypes.MessageContent, error) {
	// Check if LoopCondition is provided (non-empty)
	hasLoopCondition := templateVars["LoopCondition"] != "" && strings.TrimSpace(templateVars["LoopCondition"]) != ""
	// Check if code execution mode was used
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"

	// Build reasoning description
	reasoningDescription := "Detailed reasoning for the validation decision. MUST explain: (1) What evidence was found (or missing) for each part of success criteria, (2) Execution history evidence related to each requirement (quote tool calls, reference tool responses, cite execution steps), (3) Any errors or failures in execution history, (4) Why the decision was made (pass/fail). Be specific and reference actual execution history content. For each requirement, explicitly cite the execution history evidence that demonstrates it was met or not met."

	// Build base schema
	baseSchema := `{
		"type": "object",
		"properties": {
			"is_success_criteria_met": {
				"type": "boolean",
				"description": "Whether the success criteria was met in the final state. Return true if ALL parts of success criteria are satisfied. Return false if ANY part is not satisfied. Ignore retries, failures, or execution path - focus only on whether the end result meets requirements."
			},
			"execution_status": {
				"type": "string",
				"enum": ["COMPLETED", "PARTIAL", "FAILED", "INCOMPLETE"],
				"description": "Overall status: COMPLETED if ALL success criteria met in final state. FAILED if success criteria NOT met. PARTIAL if some (not all) criteria met. INCOMPLETE if cannot validate. Ignore how many attempts it took - only assess final outcome."
			},
			"reasoning": {
				"type": "string",
				"description": "` + reasoningDescription + ` **CRITICAL**: For each success criteria requirement, explicitly cite execution history evidence (quote tool calls, reference tool responses, cite execution steps) that demonstrates the requirement was met or not met. Share specific evidence from execution history related to each requirement."
			},
			"feedback": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"type": {
							"type": "string",
							"description": "Type of feedback (issue, recommendation, observation)"
						},
						"description": {
							"type": "string",
							"description": "Brief observation about execution quality or suggestions for improvement. Keep minimal - validation focuses on outcome, not process."
						},
						"severity": {
							"type": "string",
							"enum": ["HIGH", "MEDIUM", "LOW"],
							"description": "Severity level. Use HIGH only for critical issues that caused failure. Use LOW for minor observations."
						}
					},
					"required": ["type", "description", "severity"]
				}
			}`

	// Conditionally add loop fields and prerequisite fields to schema
	var additionalFields string
	var requiredFields []string = []string{"is_success_criteria_met", "execution_status", "reasoning"}

	if hasLoopCondition {
		additionalFields += `,
			"loop_condition_met": {
				"type": "boolean",
				"description": "Whether the loop condition is met. REQUIRED when LoopCondition is provided."
			},
			"loop_reasoning": {
				"type": "string",
				"description": "Reasoning for loop condition evaluation. REQUIRED when LoopCondition is provided."
			}`
		requiredFields = append(requiredFields, "loop_condition_met", "loop_reasoning")
	}

	// Build required fields JSON array
	requiredFieldsJSON := `["` + strings.Join(requiredFields, `", "`) + `"]`

	schema := baseSchema + additionalFields + `
		},
		"required": ` + requiredFieldsJSON + `
	}`

	// Generate system prompt and user message separately
	systemPrompt := hctpva.validationSystemPromptProcessor(templateVars, hasLoopCondition, isCodeExecutionMode)
	userMessage := hctpva.validationUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use the base orchestrator agent's ExecuteStructuredViaTool method with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	// Define tool name and description for structured output via tool calls
	toolName := "submit_validation_result"
	toolDescription := "Submit the validation analysis result for the step execution. This tool should be called with the structured validation response containing whether the success criteria was met, execution status, reasoning, and feedback."

	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[ValidationResponse](
		hctpva.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		schema,
		systemPrompt,
		true,
		toolName,
		toolDescription,
	)
	if err != nil {
		return nil, nil, err
	}

	return &result, updatedHistory, nil
}

// validationSystemPromptProcessor generates the system prompt for validation agent
func (hctpva *WorkflowValidationAgent) validationSystemPromptProcessor(templateVars map[string]string, hasLoopCondition bool, isCodeExecutionMode bool) string {
	now := time.Now()
	templateData := map[string]interface{}{
		"WorkspacePath":                templateVars["WorkspacePath"],
		"HasLoopCondition":             hasLoopCondition,
		"IsCodeExecutionMode":          isCodeExecutionMode,
		"CurrentDate":                  now.Format("2006-01-02"),
		"CurrentTime":                  now.Format("15:04:05"),
		"WorkspaceVerificationResults": templateVars["WorkspaceVerificationResults"],
	}
	if templateData["WorkspaceVerificationResults"] == "" {
		templateData["WorkspaceVerificationResults"] = "No pre-validation schema provided."
	}

	templateStr := `# Validation Agent
**Date**: {{.CurrentDate}} {{.CurrentTime}} | **Mode**: {{if .IsCodeExecutionMode}}CODE EXECUTION{{else}}TOOL EXECUTION{{end}}

## 🤖 ROLE
Verify if the final workspace state matches the 'Success Criteria'. **Destination determines success, not the effort taken.**

## ⚠️ DUAL VERIFICATION (MANDATORY)
You MUST cross-reference two sources for EVERY requirement:
1. **Workspace State**: Use 'read_workspace_file' / 'list_workspace_files'.
   - Verify files exist.
   - Verify contents match success criteria requirements.
2. **Execution History**: Verify the agent actually performed the work.
   - Look for real tool calls (API requests, DB queries).
   - Detect "Fake" work: If a file exists but history shows NO tools were called to generate its content, it's a hallucination.

## ⚡ MODE: {{if .IsCodeExecutionMode}}CODE EXECUTION{{else}}TOOL EXECUTION{{end}}
{{if .IsCodeExecutionMode}}
### 🛡️ ANTI-HALLUCINATION CHECKLIST (CODE MODE)
LLMs often write Go code that just prints "Success!". You MUST detect this:
- **Analyze 'write_code'**: Does it call generated tool functions (e.g., 'aws_tools.GetDoc') or just 'fmt.Println'?
- **Red Flags (FAIL if found)**:
  - ❌ Code prints success messages without calling any workspace/provider tools.
  - ❌ Hardcoded return values (e.g., 'return "10 users found"').
  - ❌ Simulated data arrays created in the code instead of queried from tools.
  - ❌ Workspace state differs from what the code execution reported.
{{else}}
### 🛡️ VERIFICATION CHECKLIST (TOOL MODE)
- **Trace Evidence**: Match every claim in the execution history to a specific tool response.
- **Red Flags (FAIL if found)**:
  - ❌ Evidence created with hardcoded data without calling relevant APIs.
  - ❌ File contents don't align with tool responses in the history.
{{end}}

## 🔍 PROCESS
1. **Analyze**: Break success criteria into verifiable units.
2. **Verify**: Perform dual-source checks for each unit.
3. **Loop Check**: {{if .HasLoopCondition}}Evaluate 'Loop Condition' using current evidence.{{else}}N/A{{end}}
4. **Final Decision**:
   - **COMPLETED**: ALL criteria verified with concrete evidence.
   - **FAILED**: Any requirement unsatisfied or evidence appears fabricated.

## 📤 OUTPUT
Submit results via 'submit_validation_result'. Reason must cite specific tool calls and file segments.`

	tmpl, err := template.New("validationSystemPrompt").Parse(templateStr)
	if err != nil {
		return "Error parsing validation system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return "Error executing validation system prompt template: " + err.Error()
	}
	return result.String()
}

// validationUserMessageProcessor generates the user message for validation agent
func (hctpva *WorkflowValidationAgent) validationUserMessageProcessor(templateVars map[string]string) string {
	templateStr := `# Validation Task
## 📋 CONTEXT
- **Title**: {{.StepTitle}}
- **Criteria**: {{.StepSuccessCriteria}}
- **Workspace**: {{.WorkspacePath}}

## 📝 EXECUTION HISTORY
{{.ExecutionHistory}}

{{if .DecisionReasoning}}
## 🎯 DECISION CONTEXT
{{.DecisionReasoning}}
{{end}}

{{if .LoopCondition}}
## 🔄 LOOP CHECK
**Condition**: {{.LoopCondition}}
{{else}}
## 🧠 TASK
Verify if success criteria are met using Dual Verification (Files + Tool History).
{{end}}

**Submit results via 'submit_validation_result'.**`

	tmpl, err := template.New("validationUserMessage").Parse(templateStr)
	if err != nil {
		return "Error parsing validation user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
		return "Error executing validation user message template: " + err.Error()
	}
	return result.String()
}
