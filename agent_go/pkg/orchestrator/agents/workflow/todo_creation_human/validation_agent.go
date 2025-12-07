package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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
	IsCodeExecutionMode     string // "true" or "false" - indicates if code execution mode was used
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
	// Check if LoopCondition is provided (non-empty)
	hasLoopCondition := templateVars["LoopCondition"] != "" && strings.TrimSpace(templateVars["LoopCondition"]) != ""
	// Check if code execution mode was used
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"

	// Build reasoning description
	reasoningDescription := "Detailed reasoning for the validation decision. MUST explain: (1) What evidence was found (or missing) for each part of success criteria, (2) Any errors or failures in execution history, (3) Why the decision was made (pass/fail). Be specific and reference actual execution history content."

	// Build base schema
	baseSchema := `{
		"type": "object",
		"properties": {
			"is_success_criteria_met": {
				"type": "boolean",
				"description": "Whether the success criteria was met based on CLEAR, CONCRETE evidence in execution history. Be STRICT - only return true if ALL parts of success criteria have clear evidence and there are NO errors or failures in execution."
			},
			"execution_status": {
				"type": "string",
				"enum": ["COMPLETED", "PARTIAL", "FAILED", "INCOMPLETE"],
				"description": "Overall status of step execution. Use FAILED if there are any errors, tool call failures, or if success criteria is not met. Use COMPLETED only if ALL success criteria are met with clear evidence and NO errors."
			},
			"reasoning": {
				"type": "string",
				"description": "` + reasoningDescription + `"
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
func (hctpva *HumanControlledTodoPlannerValidationAgent) validationSystemPromptProcessor(templateVars map[string]string, hasLoopCondition bool, isCodeExecutionMode bool) string {
	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Create template data with loop condition flag and code execution mode
	type SystemPromptTemplate struct {
		WorkspacePath       string
		HasLoopCondition    bool
		IsCodeExecutionMode bool
		CurrentDate         string
		CurrentTime         string
	}
	templateData := SystemPromptTemplate{
		WorkspacePath:       templateVars["WorkspacePath"],
		HasLoopCondition:    hasLoopCondition,
		IsCodeExecutionMode: isCodeExecutionMode,
		CurrentDate:         currentDate,
		CurrentTime:         currentTime,
	}

	// Define the system prompt template
	templateStr := `# Validation Agent

## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

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

## 🔧 WORKSPACE TOOLS AVAILABLE

**You have access to workspace tools to verify execution claims:**
- **read_workspace_file**: Read files to verify execution agent's claims (e.g., check if files were created, verify content matches claims)
- **list_workspace_files**: List files in directories to verify execution results (e.g., check if expected files exist)
- **Other workspace tools**: Use any available workspace tools as needed to verify execution results

**When to Use Workspace Tools:**
- When execution history mentions files being created/modified - verify they actually exist
- When execution agent claims specific content in files - read files to verify the claims
- When success criteria requires specific files or outputs - use tools to check if they exist
- When you need additional evidence beyond what's in the conversation history
{{if .IsCodeExecutionMode}}
- **MANDATORY FOR CODE EXECUTION MODE**: ALWAYS verify code claims with workspace tools (see CODE EXECUTION MODE VALIDATION section below for details)
{{end}}

**Important**: Use workspace tools proactively to verify execution claims. Don't just trust the conversation history - verify actual file contents and existence when needed.

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

{{if .IsCodeExecutionMode}}
## ⚡ CODE EXECUTION MODE VALIDATION (CRITICAL - ANTI-HALLUCINATION)

**⚠️ CRITICAL WARNING**: The execution agent used CODE EXECUTION MODE. LLMs can easily write Go code that prints fake success messages without actually doing the work. You MUST verify that the code actually accomplished the task, not just that it ran successfully.

**MANDATORY VALIDATION STEPS FOR CODE EXECUTION MODE:**

1. **Code Inspection (Verify Intent)**:
   - **Extract Go code** from ` + "`write_code`" + ` tool calls in the execution history
   - **Analyze code structure** to verify it actually calls generated tool functions (e.g., ` + "`aws_tools.GetDocument()`" + `, ` + "`workspace_tools.ReadWorkspaceFile()`" + `)
   - **Detect suspicious patterns**:
     - ❌ Code that only prints without calling tools (e.g., ` + "`fmt.Println(\"Success! Created 10 files\")`" + ` without actual file operations)
     - ❌ Hardcoded return values (e.g., ` + "`return \"10 databases found\"`" + ` without actual API calls)
     - ❌ Simulated data generation (e.g., ` + "`results := []string{\"db1\", \"db2\"}`" + ` without actual queries)
     - ❌ Missing actual tool function calls (code compiles but doesn't do real work)
   - **Verify tool imports**: Check that code imports generated tool packages (e.g., ` + "`aws_tools`" + `, ` + "`workspace_tools`" + `) and actually uses them
   - **If code doesn't call tools**: Mark as FAILED - code must actually invoke tool functions, not just print success

2. **Reality Verification (Verify Actual Results)** - **MANDATORY**:
   - **Cross-check code claims with workspace state**:
     - If code claims to create files → **MUST verify files exist** using ` + "`read_workspace_file`" + ` or ` + "`list_workspace_files`" + `
     - If code claims to read data → **MUST verify data matches** by reading the actual files
     - If code claims to modify state → **MUST verify changes occurred** by checking before/after state
     - If code claims API results → **MUST verify** by checking if results match expected format and are realistic
   - **Use workspace tools proactively**: Don't trust code output alone - verify actual workspace state
   - **If claims don't match reality**: Mark as FAILED - code output is hallucinated

3. **Tool Call Verification (Verify Execution)**:
   - **Verify tool functions were invoked**: Check that generated tool functions (e.g., ` + "`aws_tools.GetDocument()`" + `, ` + "`workspace_tools.WriteWorkspaceFile()`" + `) are actually called in the code
   - **Check tool call patterns**: Verify tool calls match what success criteria requires
   - **Verify tool responses are used**: Check that code processes tool responses, not just ignores them
   - **If no tool calls found**: Mark as FAILED - code must actually call tools, not simulate results

4. **Output Analysis (Detect Hallucinations)**:
   - **Detect suspicious output patterns**:
     - Hardcoded success messages (e.g., "Task completed successfully" without evidence)
     - Simulated data that doesn't match expected format (e.g., fake IDs, fake URLs)
     - Outputs that don't match code logic (e.g., code prints results but doesn't call tools)
     - Claims without corresponding tool calls in code
   - **Verify output matches code logic**: If code only prints, output should reflect that - not claim actual work was done
   - **If output is suspicious**: Use workspace tools to verify claims

5. **Code Execution Results Analysis**:
   - **Check execution output** (stdout/stderr from ` + "`write_code`" + ` tool):
     - Look for Go compilation errors (` + "`compile error`" + `, ` + "`syntax error`" + `, ` + "`undefined`" + `)
     - Look for runtime errors (` + "`panic:`" + `, ` + "`runtime error`" + `, ` + "`nil pointer`" + `)
     - Look for timeouts (` + "`timeout`" + `, ` + "`killed`" + `, ` + "`context deadline exceeded`" + `)
     - Check exit codes (non-zero = failure)
   - **Verify execution succeeded**: Code must compile and run without errors
   - **If execution failed**: Mark as FAILED regardless of output

6. **Workspace Tool Verification (MANDATORY CHECKS)**:
   - **ALWAYS verify file operations**: If code claims to create/modify files, use workspace tools to verify:
     - ` + "`list_workspace_files`" + ` to check if files exist
     - ` + "`read_workspace_file`" + ` to verify file contents match claims
   - **ALWAYS verify data claims**: If code claims specific data/results, verify by reading actual files
   - **ALWAYS cross-reference**: Don't trust code output alone - verify with workspace tools
   - **If verification fails**: Mark as FAILED - code output is hallucinated

**CODE EXECUTION MODE RED FLAGS (Mark as FAILED):**
- Code only prints success messages without calling tools
- Code has hardcoded return values without actual API calls
- Code execution output claims success but workspace state doesn't match
- Code doesn't import or call generated tool functions
- File claims don't match actual workspace state (files don't exist or content differs)
- Code output contains simulated/fake data
- Tool function calls are missing from code
- Code compiles but doesn't actually do the work

**CODE EXECUTION MODE VALIDATION CHECKLIST:**
- [ ] Code actually calls generated tool functions (not just prints)
- [ ] Code execution succeeded (no compilation/runtime errors)
- [ ] Code output matches actual workspace state (verified with workspace tools)
- [ ] Files claimed to be created actually exist (verified)
- [ ] File contents match code claims (verified)
- [ ] Data/results are realistic and match expected format
- [ ] No hardcoded/fake outputs detected
- [ ] Tool function calls match success criteria requirements
{{end}}

**Detailed Validation Steps:**

1. **Analyze Conversation History Structure**:
   - Review ALL messages in the execution conversation history
   {{if .IsCodeExecutionMode}}
   - **For CODE EXECUTION MODE**: Follow the CODE EXECUTION MODE VALIDATION section above (extract Go code, analyze structure, verify tool calls)
   {{else}}
   - Identify ALL tool calls made by the execution agent
   {{end}}
   - Check ALL tool responses for success/failure indicators
   - Look for error messages, exceptions, or failure indicators in tool responses
   - Verify the conversation shows a complete execution flow (not just partial attempts)

2. **Verify Each Part of Success Criteria**:
   - Break down the success criteria into individual requirements
   - For EACH requirement, find SPECIFIC evidence in the conversation history:
     {{if .IsCodeExecutionMode}}
     - **For CODE EXECUTION MODE**: Follow CODE EXECUTION MODE VALIDATION section above (code execution results + workspace state verification)
     {{else}}
     - Tool calls that accomplished the requirement
     - Tool responses that show the requirement was met
     {{end}}
     - Specific outputs, results, or confirmations
   - If ANY requirement lacks clear evidence, mark as FAILED

3. **Check for Errors and Failures**:
   {{if .IsCodeExecutionMode}}
   - **For CODE EXECUTION MODE**: Follow CODE EXECUTION MODE VALIDATION section above (compilation errors, runtime errors, timeouts, exit codes)
   {{else}}
   - Look for tool call failures (error responses, exceptions, timeouts)
   - Check for error messages in tool responses
   {{end}}
   - Identify incomplete tool calls (calls without responses)
   - Look for execution agent statements indicating problems, failures, or inability to complete
   - Check for any "failed", "error", "exception", "timeout", "not found" indicators

4. **Analyze Tool Usage and Results**:
   {{if .IsCodeExecutionMode}}
   - **For CODE EXECUTION MODE**: Follow CODE EXECUTION MODE VALIDATION section above (verify tool calls, verify workspace state)
   {{else}}
   - Verify tools were used appropriately for the task
   - Check tool responses contain expected data/results
   - Verify tool outputs match what success criteria requires
   {{end}}
   - Look for tool responses that indicate partial completion or missing data

5. **Assess Evidence Quality**:
   - Evidence must be CONCRETE and SPECIFIC (not vague or inferred)
   {{if .IsCodeExecutionMode}}
   - **For CODE EXECUTION MODE**: Follow CODE EXECUTION MODE VALIDATION section above (code execution results + workspace state verification - MANDATORY)
   {{else}}
   - Evidence must come from ACTUAL tool responses (not just agent claims)
   {{end}}
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
{{if .IsCodeExecutionMode}}
- **CODE EXECUTION MODE SPECIFIC**:
  - Code only prints success without calling tools
  - Code has hardcoded/fake return values
  - Code execution output doesn't match workspace reality (verified with workspace tools)
  - Code doesn't import or call generated tool functions
  - File claims don't match actual workspace state
  - Go compilation or runtime errors
  - Code output is suspicious (simulated data, fake results)
{{else}}
- Any tool call failures, errors, or exceptions in tool responses
{{end}}
- Execution agent mentions failures, being stuck, or inability to complete
- Missing tool responses (tool calls without corresponding responses)
- Tool responses contain error messages, "not found", "failed", "timeout", etc.
- Success criteria requires specific outcomes that are not clearly demonstrated{{if .IsCodeExecutionMode}} in code execution results AND workspace state verification{{else}} in tool responses{{end}}
- Execution conversation shows incomplete or partial completion
- Agent claims success but{{if .IsCodeExecutionMode}} code output/workspace state doesn't support the claims{{else}} tool responses don't support the claims{{end}}

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
{{if .HasLoopCondition}}
- loop_condition_met: boolean - **REQUIRED** - Whether the loop condition is met
- loop_reasoning: string - **REQUIRED** - Detailed reasoning for loop condition evaluation
{{else}}
**CRITICAL**: Do NOT include loop_condition_met or loop_reasoning fields in your JSON response. These fields are ONLY used when LoopCondition is provided in the user message. Since LoopCondition is NOT provided, these fields must NOT appear in your response at all.
{{end}}

**Example JSON structure:**
{{if .HasLoopCondition}}
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
  ],
  "loop_condition_met": true,
  "loop_reasoning": "The loop condition was met based on the execution results."
}
` + "```" + `
{{else}}
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
{{end}}

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
		IsCodeExecutionMode:     templateVars["IsCodeExecutionMode"],
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

## 📝 **EXECUTION CONVERSATION HISTORY TO VALIDATE**

**IMPORTANT**: The conversation history below contains the ACTUAL execution flow. Analyze it carefully using the validation process in the system prompt.

{{if eq .IsCodeExecutionMode "true"}}
**⚠️ CODE EXECUTION MODE DETECTED**: Follow the CODE EXECUTION MODE VALIDATION section in the system prompt for detailed anti-hallucination validation steps.
{{end}}

{{.ExecutionHistory}}

{{if .LoopCondition}}
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
1. **Be STRICT**: Only mark as successful if there is CLEAR, CONCRETE evidence that ALL parts of the success criteria were met
2. **Check for Errors**: Look for ANY{{if eq .IsCodeExecutionMode "true"}} code execution errors{{else}} tool call failures, errors, or incomplete actions{{end}} in the execution history
3. **Verify Each Part**: Verify that EACH part of the success criteria has corresponding evidence{{if eq .IsCodeExecutionMode "true"}} (follow CODE EXECUTION MODE VALIDATION section in system prompt){{end}}
4. **If in Doubt, FAIL**: If there is ANY uncertainty or missing evidence, mark as FAILED
5. **Look for Failure Indicators**: Check if the execution agent mentioned failures, being stuck, or inability to complete the task
{{if eq .IsCodeExecutionMode "true"}}
6. **CODE EXECUTION MODE**: Follow the CODE EXECUTION MODE VALIDATION section in the system prompt for mandatory anti-hallucination validation steps
{{end}}

**Validation Checklist:**
- [ ] All parts of success criteria have clear evidence{{if eq .IsCodeExecutionMode "true"}} (code execution + workspace state verification){{end}}
- [ ] No{{if eq .IsCodeExecutionMode "true"}} code execution errors{{else}} tool call failures or errors{{end}} in execution
- [ ] Execution agent did not report any failures or problems
- [ ] Success criteria outcomes are clearly demonstrated
{{if eq .IsCodeExecutionMode "true"}}
- [ ] CODE EXECUTION MODE checklist completed (see system prompt CODE EXECUTION MODE VALIDATION CHECKLIST)
{{end}}

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
