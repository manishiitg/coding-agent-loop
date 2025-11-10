package todo_execution

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

// TodoValidationTemplate holds template variables for validation prompts
type TodoValidationTemplate struct {
	Objective           string
	WorkspacePath       string
	ExecutionOutput     string
	StepNumber          int
	TotalSteps          int
	StepTitle           string
	StepSuccessCriteria string
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

// TodoValidationAgent extends BaseOrchestratorAgent with validation functionality
type TodoValidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoValidationAgent creates a new validation agent
func NewTodoValidationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoValidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.ValidationAgentType,
		eventBridge,
	)

	return &TodoValidationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the validation agent and returns structured output
func (tva *TodoValidationAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*ValidationResponse, []llmtypes.MessageContent, error) {
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
	systemPrompt := tva.validationSystemPromptProcessor(templateVars)
	userMessage := tva.validationUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Define tool name and description for structured output via tool calls
	toolName := "submit_validation_result"
	toolDescription := "Submit the validation analysis result for the step execution. This tool should be called with the structured validation response containing whether the success criteria was met, execution status, reasoning, and feedback."

	// Use the base orchestrator agent's ExecuteStructuredWithInputProcessorViaTool method with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[ValidationResponse](
		tva.BaseOrchestratorAgent,
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

// Execute implements the OrchestratorAgent interface
func (tva *TodoValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// NOTE: This method is NOT USED - use ExecuteStructured() instead
	return "", nil, fmt.Errorf("Execute() is not used for validation agent - use ExecuteStructured() instead")
}

// validationSystemPromptProcessor generates the system prompt for validation agent
func (tva *TodoValidationAgent) validationSystemPromptProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoValidationTemplate{
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

**CRITICAL VALIDATION PRINCIPLE**: Be STRICT and CONSERVATIVE. Only mark as successful if there is CLEAR, CONCRETE EVIDENCE that ALL parts of the success criteria were met. If there is ANY doubt, uncertainty, or missing evidence, mark as FAILED.

**Detailed Validation Steps:**

1. **Analyze Conversation History Structure**:
   - Review ALL messages in the execution conversation history
   - Identify ALL tool calls made by the execution agent
   - Check ALL tool responses for success/failure indicators
   - Look for error messages, exceptions, or failure indicators in tool responses
   - Verify the conversation shows a complete execution flow (not just partial attempts)

2. **Verify Each Part of Success Criteria**:
   - Break down the success criteria into individual requirements
   - For EACH requirement, find SPECIFIC evidence in the conversation history:
     - Tool calls that accomplished the requirement
     - Tool responses that show the requirement was met
     - Specific outputs, results, or confirmations
   - If ANY requirement lacks clear evidence, mark as FAILED

3. **Check for Errors and Failures**:
   - Look for tool call failures (error responses, exceptions, timeouts)
   - Check for error messages in tool responses
   - Identify incomplete tool calls (calls without responses)
   - Look for execution agent statements indicating problems, failures, or inability to complete
   - Check for any "failed", "error", "exception", "timeout", "not found" indicators

4. **Analyze Tool Usage and Results**:
   - Verify tools were used appropriately for the task
   - Check tool responses contain expected data/results
   - Verify tool outputs match what success criteria requires
   - Look for tool responses that indicate partial completion or missing data

5. **Assess Evidence Quality**:
   - Evidence must be CONCRETE and SPECIFIC (not vague or inferred)
   - Evidence must come from ACTUAL tool responses (not just agent claims)
   - Evidence must directly support the success criteria requirements
   - If evidence is missing, ambiguous, or insufficient, mark as FAILED

**Decision Criteria:**
- ✅ **PASS**: ALL parts of success criteria met with CLEAR, CONCRETE evidence from conversation history - NO errors or failures detected
- ❌ **FAIL**: ANY of the following:
  - Success criteria not fully met
  - Missing evidence for any part of success criteria
  - Tool call failures or errors detected
  - Incomplete execution (missing tool responses, partial completion)
  - Execution agent reported failures or problems
  - Insufficient or ambiguous evidence

**RED FLAGS (Mark as FAILED if you see):**
- Any tool call failures, errors, or exceptions in tool responses
- Execution agent mentions failures, being stuck, or inability to complete
- Missing tool responses (tool calls without corresponding responses)
- Tool responses contain error messages, "not found", "failed", "timeout", etc.
- Success criteria requires specific outcomes that are not clearly demonstrated in tool responses
- Execution conversation shows incomplete or partial completion
- Agent claims success but tool responses don't support the claims

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

**USE THE 'submit_validation_result' TOOL TO SUBMIT YOUR VALIDATION ANALYSIS**

You MUST call the 'submit_validation_result' tool with your validation analysis. Do NOT return JSON directly in your response - use the tool instead.

The tool accepts a structured object with:
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

**CRITICAL**: You MUST call the 'submit_validation_result' tool with your validation analysis. The tool will be available to you - use it to submit your structured validation response. Do NOT return JSON directly in your text response. Focus on the step execution conversation analysis. Check if the execution conversation provides sufficient evidence that the success criteria was met. Analyze tool usage and execution results to verify completion.`

	// Parse and execute the template
	tmpl, err := template.New("validationSystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing validation system prompt template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing validation system prompt template: %v", err)
	}

	return result.String()
}

// validationUserMessageProcessor generates the user message for validation agent
func (tva *TodoValidationAgent) validationUserMessageProcessor(templateVars map[string]string) string {
	// Parse numeric fields from templateVars
	stepNumber := 0
	totalSteps := 0
	if stepNumStr, exists := templateVars["StepNumber"]; exists {
		fmt.Sscanf(stepNumStr, "%d", &stepNumber)
	}
	if totalStepsStr, exists := templateVars["TotalSteps"]; exists {
		fmt.Sscanf(totalStepsStr, "%d", &totalSteps)
	}

	// Get loop condition if provided
	loopCondition := templateVars["LoopCondition"]

	// Create template data with loop condition
	templateData := map[string]interface{}{
		"StepTitle":           templateVars["StepTitle"],
		"StepDescription":     templateVars["StepDescription"],
		"StepSuccessCriteria": templateVars["StepSuccessCriteria"],
		"WorkspacePath":       templateVars["WorkspacePath"],
		"ExecutionOutput":     templateVars["ExecutionOutput"],
		"StepNumber":          stepNumber,
		"TotalSteps":          totalSteps,
		"LoopCondition":       loopCondition,
		"HasLoopCondition":    loopCondition != "",
	}

	// Define the user message template
	templateStr := `# Validation Task

## 📋 **STEP CONTEXT**
- **Title**: {{.StepTitle}}
- **Description**: {{.StepDescription}}
- **Success Criteria**: {{.StepSuccessCriteria}}
- **Workspace**: {{.WorkspacePath}}
- **Step**: {{.StepNumber}}/{{.TotalSteps}}

## 🔍 **STEP CONTEXT ANALYSIS**
- **Success Criteria**: Use the success criteria above to verify completion
- **Context Output**: Verify if the context output file was created as specified

## 📝 **EXECUTION CONVERSATION HISTORY TO VALIDATE**

**IMPORTANT**: The conversation history below contains the ACTUAL execution flow. Analyze it carefully:

- **Tool Calls**: Look for all tool calls made by the execution agent
- **Tool Responses**: Check each tool response for success/failure indicators
- **Errors**: Identify any error messages, exceptions, or failures
- **Evidence**: Find specific evidence for each part of the success criteria
- **Completeness**: Verify the execution was complete (not partial or interrupted)

{{.ExecutionOutput}}

{{if .HasLoopCondition}}
## 🧠 **YOUR TASK**

This step is in **LOOP CONDITION CHECK MODE** - you are checking the LOOP CONDITION, not the full success criteria.

**Loop Condition**: {{.LoopCondition}}

**Your Task**: Evaluate if the LOOP CONDITION is met based on the execution conversation history.

**CRITICAL**: Analyze the ACTUAL conversation history above:
1. Review ALL tool calls and responses in the conversation
2. Check for specific evidence that the loop condition is met
3. Look for errors or failures that indicate the condition is NOT met
4. Verify tool responses show the condition is satisfied (not just agent claims)

Follow the validation process in the system prompt, focusing on loop condition evaluation. Call the 'submit_validation_result' tool with your validation results, including loop_condition_met and loop_reasoning fields.
{{else}}
## 🧠 **YOUR TASK**

Validate if the step "{{.StepTitle}}" was completed successfully by checking if the SUCCESS CRITERIA was met.

**Success Criteria**: {{.StepSuccessCriteria}}

**CRITICAL INSTRUCTIONS:**

1. **Analyze the Conversation History Above**:
   - Review EVERY message in the execution conversation history
   - Identify ALL tool calls and their corresponding responses
   - Check for errors, failures, or incomplete executions
   - Find SPECIFIC evidence for each part of the success criteria

2. **Verify Each Part of Success Criteria**:
   - Break down: "{{.StepSuccessCriteria}}" into individual requirements
   - For EACH requirement, find concrete evidence in tool responses
   - If ANY requirement lacks clear evidence, mark as FAILED

3. **Check for Failures**:
   - Look for tool call failures or errors in tool responses
   - Check for execution agent statements indicating problems
   - Identify incomplete tool calls (calls without responses)
   - Look for error messages, exceptions, or failure indicators

4. **Be STRICT**:
   - Only mark as successful if ALL parts of success criteria have CLEAR evidence
   - Evidence must come from ACTUAL tool responses, not just agent claims
   - If in doubt or evidence is missing, mark as FAILED

**Validation Checklist:**
- [ ] All tool calls have corresponding responses
- [ ] No tool call failures or errors detected
- [ ] Each part of success criteria has specific evidence in tool responses
- [ ] Execution agent did not report any failures or problems
- [ ] Tool responses demonstrate success criteria outcomes clearly
- [ ] No incomplete or partial execution detected

Follow the validation process in the system prompt. Analyze the execution conversation history CAREFULLY and call the 'submit_validation_result' tool with your validation results.
{{end}}`

	// Parse and execute the template
	tmpl, err := template.New("validationUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing validation user message template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing validation user message template: %v", err)
	}

	return result.String()
}
