package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledTodoPlannerValidationTemplate holds template variables for validation prompts
type HumanControlledTodoPlannerValidationTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
	LoopCondition           string // For loop steps: condition to check
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
	LoopConditionMet     bool                 `json:"loop_condition_met"` // For loop steps: whether loop condition is met
	LoopReasoning        string               `json:"loop_reasoning"`     // For loop steps: reasoning for loop condition check
}

// HumanControlledTodoPlannerValidationAgent validates if tasks were completed properly
type HumanControlledTodoPlannerValidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerValidationAgent creates a new human-controlled todo planner validation agent
func NewHumanControlledTodoPlannerValidationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerValidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerValidationAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerValidationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hctpva *HumanControlledTodoPlannerValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for validation agent - use ExecuteStructured() instead")
}

// ExecuteStructured executes the validation agent and returns structured output
func (hctpva *HumanControlledTodoPlannerValidationAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*ValidationResponse, []llmtypes.MessageContent, error) {
	// Define the JSON schema for validation analysis
	schema := `{
		"type": "object",
		"properties": {
			"is_success_criteria_met": {
				"type": "boolean",
				"description": "Whether the success criteria was met based on execution evidence"
			},
			"execution_status": {
				"type": "string",
				"enum": ["COMPLETED", "PARTIAL", "FAILED", "INCOMPLETE"],
				"description": "Overall status of step execution"
			},
			"reasoning": {
				"type": "string",
				"description": "Detailed reasoning for the validation decision"
			},
			"feedback": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"type": {
							"type": "string",
							"description": "Type of feedback (issue, recommendation, etc.)"
						},
						"description": {
							"type": "string",
							"description": "Description of the feedback"
						},
						"severity": {
							"type": "string",
							"enum": ["HIGH", "MEDIUM", "LOW"],
							"description": "Severity of the feedback"
						}
					},
					"required": ["type", "description", "severity"]
				}
			},
			"loop_condition_met": {
				"type": "boolean",
				"description": "Whether the loop condition is met (only used when LoopCondition is provided in template vars). Required when checking loop condition."
			},
			"loop_reasoning": {
				"type": "string",
				"description": "Reasoning for loop condition evaluation (only used when LoopCondition is provided). Required when checking loop condition."
			}
		},
		"required": ["is_success_criteria_met", "execution_status", "reasoning"]
	}`

	// Generate system prompt and user message separately
	systemPrompt := hctpva.validationSystemPromptProcessor(templateVars)
	userMessage := hctpva.validationUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use the base orchestrator agent's ExecuteStructured method with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessor[ValidationResponse](hctpva.BaseOrchestratorAgent, ctx, templateVars, inputProcessor, conversationHistory, schema, systemPrompt, true)
	if err != nil {
		return nil, nil, err
	}

	return &result, updatedHistory, nil
}

// validationSystemPromptProcessor generates the system prompt for validation agent
func (hctpva *HumanControlledTodoPlannerValidationAgent) validationSystemPromptProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerValidationTemplate{
		WorkspacePath: templateVars["WorkspacePath"],
	}

	// Define the system prompt template
	templateStr := `# Validation Agent

## 🤖 AGENT IDENTITY
- **Role**: Validation Agent
- **Responsibility**: Verify if the step success criteria was met and execution was completed properly
- **Mode**: Success criteria verification with execution output analysis

## 📁 FILE PERMISSIONS (Validation Agent)

**READ:**
- Context output files created by execution agent (located in {{.WorkspacePath}}/execution/ folder)
  - Note: {{.WorkspacePath}} is the base workspace path, so execution files are at {{.WorkspacePath}}/execution/step_X_results.md
- Any workspace files needed to verify execution claims

**NO WRITE PERMISSIONS:**
- This agent does NOT write any files - only returns structured JSON validation results
- Focus on verifying execution claims using evidence

## ⚠️ EDGE CASE HANDLING

**If execution history is empty or incomplete:**
- Return INCOMPLETE status
- Reasoning: "Execution history is missing or incomplete, cannot validate"
- Feedback: Request complete execution output

**If success criteria is ambiguous:**
- Validate based on available evidence
- Note ambiguity in feedback: "Success criteria unclear, validated based on observable results"

**If tool output is incomplete:**
- Mark as PARTIAL
- Feedback: List specific missing information needed for full validation

## 🔍 VALIDATION PROCESS

**General Validation Steps:**
1. **Review Execution History**: Analyze conversation for evidence of completion
2. **Check Success Criteria**: Verify if the success criteria was met
3. **Analyze Tool Usage**: Check which tools were used and their results
4. **Assess Evidence**: Identify what worked and what didn't

**Decision Criteria:**
- ✅ **PASS**: Success criteria met with sufficient evidence
- ❌ **FAIL**: Success criteria not met or insufficient evidence

## 🔄 LOOP CONDITION CHECK MODE (When Applicable)

**When LoopCondition is provided in the user message:**
- Focus on evaluating the LOOP CONDITION, not the full success criteria
- Return loop_condition_met: true if condition is met, false otherwise
- Return loop_reasoning: Detailed explanation of why the loop condition is or is not met
- Still return is_success_criteria_met, execution_status, and reasoning for consistency (but focus on loop condition evaluation)

**Loop Condition Check Steps:**
1. **Review Execution History**: Analyze conversation for evidence related to the loop condition
2. **Check Loop Condition**: Verify if the loop condition is met
3. **Analyze Tool Usage**: Check which tools were used and their results
4. **Assess Evidence**: Determine if the loop condition is satisfied

**Decision**:
- ✅ **LOOP CONDITION MET**: Loop condition is satisfied - step can exit loop
- ❌ **LOOP CONDITION NOT MET**: Loop condition is not satisfied - step must continue looping

## 📤 OUTPUT FORMAT

**RETURN STRUCTURED JSON RESPONSE ONLY**

The response should be a JSON object with:
- is_success_criteria_met: boolean - Whether the success criteria was met based on execution evidence
- execution_status: string - Overall status (COMPLETED/PARTIAL/FAILED/INCOMPLETE)
- reasoning: string - Detailed reasoning for the validation decision
- feedback: array of objects with type, description, and severity (HIGH/MEDIUM/LOW)
- loop_condition_met: boolean - **REQUIRED when LoopCondition is provided** - Whether the loop condition is met
- loop_reasoning: string - **REQUIRED when LoopCondition is provided** - Detailed reasoning for loop condition evaluation

**Example JSON structure:**
` + "```json" + `
{
  "is_success_criteria_met": true,
  "execution_status": "COMPLETED",
  "reasoning": "The execution conversation shows clear evidence that the success criteria was met. The agent successfully used MCP tools to accomplish the step objective and provided detailed results.",
  "feedback": [
    {
      "type": "Issue",
      "description": "Could have provided more detailed tool output",
      "severity": "LOW"
    },
    {
      "type": "Recommendation",
      "description": "Include more detailed tool output in future executions",
      "severity": "LOW"
    }
  ]
}
` + "```" + `

**Note**: Focus on the step execution conversation analysis. Check if the execution conversation provides sufficient evidence that the success criteria was met. Analyze tool usage and execution results to verify completion. Return structured JSON response only.`

	// Parse and execute the template
	tmpl, err := template.New("validationSystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing validation system prompt template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing validation system prompt template: %w", err)
	}

	return result.String()
}

// validationUserMessageProcessor generates the user message for validation agent
func (hctpva *HumanControlledTodoPlannerValidationAgent) validationUserMessageProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerValidationTemplate{
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		ExecutionHistory:        templateVars["ExecutionHistory"],
		LoopCondition:           templateVars["LoopCondition"],
	}

	// Define the user message template
	templateStr := `# Validation Task

## 📋 **STEP CONTEXT**
- **Title**: {{.StepTitle}}
- **Description**: {{.StepDescription}}
- **Success Criteria**: {{.StepSuccessCriteria}}
- **Context Dependencies**: {{.StepContextDependencies}}
- **Context Output**: {{.StepContextOutput}}
- **Workspace**: {{.WorkspacePath}}

## 🔍 **STEP CONTEXT ANALYSIS**
- **Success Criteria**: Use the success criteria above to verify completion
- **Context Dependencies**: Check if context dependencies files were properly read
- **Context Output**: Verify if the context output file was created as specified

## 📝 **EXECUTION CONVERSATION TO VALIDATE**
{{.ExecutionHistory}}

{{if .LoopCondition}}
## 🧠 **YOUR TASK**

This step is in **LOOP CONDITION CHECK MODE** - you are checking the LOOP CONDITION, not the full success criteria.

**Loop Condition**: {{.LoopCondition}}

**Your Task**: Evaluate if the LOOP CONDITION is met based on the execution results.

Follow the validation process in the system prompt, focusing on loop condition evaluation. Return loop_condition_met and loop_reasoning in your structured JSON response.
{{else}}
## 🧠 **YOUR TASK**

Validate if the step "{{.StepTitle}}" was completed successfully by checking if the SUCCESS CRITERIA was met.

**Success Criteria**: {{.StepSuccessCriteria}}

Follow the validation process in the system prompt. Analyze the execution conversation history and return a structured JSON response with validation results.
{{end}}`

	// Parse and execute the template
	tmpl, err := template.New("validationUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing validation user message template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing validation user message template: %w", err)
	}

	return result.String()
}
