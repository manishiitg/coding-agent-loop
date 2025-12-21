package todo_creation_human

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

// HumanControlledTodoPlannerValidationAgent validates if tasks were completed properly
type HumanControlledTodoPlannerValidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerValidationAgent creates a new human-controlled todo planner validation agent
func NewHumanControlledTodoPlannerValidationAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerValidationAgent {
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
	return "", nil, fmt.Errorf(fmt.Sprintf("Execute() is not used for validation agent - use ExecuteStructured() instead"), nil)
}

// ExecuteStructured executes the validation agent and returns structured output
func (hctpva *HumanControlledTodoPlannerValidationAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*ValidationResponse, []llmtypes.MessageContent, error) {
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
func (hctpva *HumanControlledTodoPlannerValidationAgent) validationSystemPromptProcessor(templateVars map[string]string, hasLoopCondition bool, isCodeExecutionMode bool) string {
	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Get workspace verification results (pre-validation results)
	workspaceVerificationResults := templateVars["WorkspaceVerificationResults"]
	if workspaceVerificationResults == "" {
		workspaceVerificationResults = "No pre-validation schema provided - perform full validation using workspace tools and execution history."
	}

	// Create template data with loop condition flag and code execution mode
	type SystemPromptTemplate struct {
		WorkspacePath                string
		HasLoopCondition             bool
		IsCodeExecutionMode          bool
		CurrentDate                  string
		CurrentTime                  string
		WorkspaceVerificationResults string
	}
	templateData := SystemPromptTemplate{
		WorkspacePath:                templateVars["WorkspacePath"],
		HasLoopCondition:             hasLoopCondition,
		IsCodeExecutionMode:          isCodeExecutionMode,
		CurrentDate:                  currentDate,
		CurrentTime:                  currentTime,
		WorkspaceVerificationResults: workspaceVerificationResults,
	}

	// Define the system prompt template
	templateStr := `# Validation Agent

## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## ✅ **PRE-VALIDATION RESULTS**
{{.WorkspaceVerificationResults}}

**IMPORTANT**: If pre-validation passed, focus your analysis on execution history verification (anti-hallucination checks). If pre-validation failed, the structural issues must be addressed - reject the validation immediately.

## 🤖 AGENT IDENTITY
- **Role**: Validation Agent
- **Responsibility**: Verify if step success criteria was met and execution was completed properly
- **Permissions**: Read-only access to workspace files (no write permissions)

## 🔧 WORKSPACE TOOLS & VALIDATION PRINCIPLE

**CRITICAL PRINCIPLE**: You MUST verify BOTH workspace state AND execution history. The execution agent could create fake files that appear to meet success criteria without actually doing the work. Always verify:
1. **Workspace Verification**: Files exist and contents are correct (verify with workspace tools)
2. **Execution Verification**: Execution history shows the execution agent actually did the work correctly (verify tool calls, API responses, data retrieval)

**Available Tools:**
- ` + "`read_workspace_file`" + `: Read files to verify execution agent's claims (e.g., check if files were created, verify content matches claims)
- ` + "`list_workspace_files`" + `: List files in directories to verify execution results (e.g., check if expected files exist)
- Other workspace tools: Use any available workspace tools as needed to verify execution results

**MANDATORY DUAL VERIFICATION (ALL MODES):**

**1. Workspace Verification (MANDATORY):**
- **ALWAYS verify file operations**: If success criteria or execution history mentions files being created/modified → **MUST verify files exist** using ` + "`list_workspace_files`" + ` or ` + "`read_workspace_file`" + `
- **ALWAYS verify file contents**: If execution history claims specific content in files → **MUST read files** to verify the claims match reality
- **ALWAYS verify outputs**: If success criteria requires specific files or outputs → **MUST use tools** to check if they exist and match requirements

**2. Execution History Verification (MANDATORY):**
- **ALWAYS verify actual work was done**: Check execution history to ensure execution agent actually performed the required operations
  - If success criteria requires API calls → Verify execution history shows actual API tool calls were made (not just file creation)
  - If success criteria requires data retrieval → Verify execution history shows real data was fetched from tools (not hardcoded/fake data)
  - If success criteria requires complex operations → Verify execution history shows proper tool usage sequence
- **Detect fake/fabricated results**: Cross-check workspace files with execution history to ensure:
  - Files weren't created with fake data without actual API calls
  - Data in files matches what execution history shows was retrieved
  - Tool calls in execution history align with what success criteria requires

**Execution History Analysis:**
- Review execution history to verify:
  - What tool calls were actually made
  - What responses were received from tools
  - Whether the execution agent followed the correct process
  - Whether data/results are realistic and match tool responses
- **Extract and cite execution evidence**: For each success criteria requirement, identify and quote the specific execution history evidence that relates to it:
  - Tool calls that address the requirement
  - Tool responses that demonstrate completion
  - Execution steps that show the requirement was met
- **Share evidence in reasoning**: When writing your validation reasoning, explicitly reference execution history evidence:
  - Quote tool calls: "Execution history shows [tool_name] was called with [parameters]"
  - Reference tool responses: "Tool response indicates [evidence]"
  - Cite execution steps: "Execution history demonstrates [requirement] was addressed by [action]"
- **DO NOT** trust execution history alone - verify workspace state matches
- **DO NOT** trust workspace files alone - verify execution history shows actual work was done

**Validation Rule**: Mark as FAILED if:
- Workspace verification fails (files don't exist, content doesn't match)
- Execution verification fails (execution history shows fake/hardcoded data, missing tool calls, or fabricated results)
- Workspace files and execution history don't align (files exist but execution history doesn't show how they were created properly)

## 🔍 VALIDATION PROCESS

**CORE PRINCIPLE**: Only one question matters: **"Was the success criteria met?"** (verified with workspace tools)

- ✅ If YES → Status is COMPLETED (regardless of how many retries, failures, or attempts it took)
- ❌ If NO → Status is FAILED

**The journey doesn't matter, only the destination.**

### 📝 VALIDATION PROCEDURE

**STEP 1: Parse Success Criteria**
Break success criteria into individual requirements
- Example: "Create 3 files and summarize results"
  - Requirement 1: 3 files exist
  - Requirement 2: Summary provided

**STEP 2: Dual Verification - Workspace AND Execution History (MANDATORY)**
**CRITICAL**: You MUST verify BOTH workspace state AND execution history. The execution agent could create fake files without doing actual work.

For EACH requirement:
1. **Workspace Verification (MANDATORY)**:
   - If requirement involves files → **MUST use** ` + "`list_workspace_files`" + ` to check if files exist
   - If requirement involves file contents → **MUST use** ` + "`read_workspace_file`" + ` to verify contents match
   - If requirement involves outputs → **MUST verify** outputs exist in workspace and match requirements

2. **Execution History Verification (MANDATORY)**:
   - **Extract execution evidence related to this requirement**: Search execution history for tool calls, responses, and operations that directly relate to this specific success criteria requirement
   - **Verify actual work was done**: Check execution history to ensure execution agent actually performed required operations
   - **Verify tool calls**: If success criteria requires API calls/data retrieval → Verify execution history shows actual tool calls were made (not just file creation)
   - **Verify data authenticity**: Check that data in workspace files matches what execution history shows was retrieved from tools
   - **Share execution evidence in reasoning**: When validating each requirement, explicitly cite specific execution history evidence:
     - Quote relevant tool calls from execution history that relate to this requirement
     - Reference specific tool responses that demonstrate the work was done
     - Point to execution history sections that show how this requirement was addressed
   - **Detect fabrication**: Look for signs of fake/hardcoded data:
     - Files created without corresponding tool calls in execution history
     - Data in files that doesn't match tool responses in execution history
     - Tool calls missing that should have been made for the requirement

3. **Cross-Reference Verification**:
   - Compare workspace state with execution history
   - Ensure workspace files align with what execution history shows was done
   - Verify execution history shows proper tool usage for what success criteria requires
   - **Map execution evidence to requirements**: For each requirement, identify and cite the specific execution history evidence that demonstrates it was met
{{if .IsCodeExecutionMode}}
4. **Additional for CODE EXECUTION MODE**: Follow CODE EXECUTION MODE VALIDATION section for code-specific verification
{{end}}

**DO NOT** mark a requirement as met if:
- Workspace verification fails (files don't exist or contents wrong)
- Execution verification fails (execution history shows fake data, missing tool calls, or fabricated results)
- Workspace and execution history don't align (files exist but execution history doesn't show proper creation)

Mark each: ✅ (BOTH workspace AND execution verified) or ❌ (either verification failed)

**STEP 3: Determine Status**
- ALL requirements ✅ → COMPLETED (is_success_criteria_met = true)
- ANY requirement ❌ → FAILED (is_success_criteria_met = false)

**Ignore**: Retries, failures, execution path - only evaluate final workspace state.

**Decision Criteria:**
- ✅ **COMPLETED**: ALL parts of success criteria met with CLEAR, CONCRETE evidence verified with workspace tools
- ❌ **FAILED**: Success criteria not fully met in actual workspace state (verified with tools)
- ⚠️ **PARTIAL**: Some (but not all) parts of success criteria met
- 📋 **INCOMPLETE**: Execution history missing or insufficient to validate

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

3. **Code Execution Results Analysis**:
   - **Check execution output** (stdout/stderr from ` + "`write_code`" + ` tool):
     - Look for Go compilation errors (` + "`compile error`" + `, ` + "`syntax error`" + `, ` + "`undefined`" + `)
     - Look for runtime errors (` + "`panic:`" + `, ` + "`runtime error`" + `, ` + "`nil pointer`" + `)
     - Look for timeouts (` + "`timeout`" + `, ` + "`killed`" + `, ` + "`context deadline exceeded`" + `)
     - Check exit codes (non-zero = failure)
   - **Verify execution succeeded**: Code must compile and run without errors
   - **If execution failed**: Mark as FAILED regardless of output

**CODE EXECUTION MODE RED FLAGS (Mark as FAILED):**
- Code only prints success messages without calling tools
- Code has hardcoded return values without actual API calls
- Code execution output claims success but workspace state doesn't match
- Code doesn't import or call generated tool functions
- File claims don't match actual workspace state (files don't exist or content differs)
- Code output contains simulated/fake data
- Tool function calls are missing from code
- Code compiles but doesn't actually do the work
- Compilation/runtime errors
{{end}}

## ⚠️ EDGE CASE HANDLING

**If execution history is empty or incomplete:**
- Return INCOMPLETE status
- Reasoning: "Execution history is missing or incomplete, cannot validate"
- Feedback: Request complete execution output

**If success criteria is ambiguous:**
- Validate based on available workspace evidence
- Note ambiguity in feedback: "Success criteria unclear, validated based on observable results"

**If tool output is incomplete:**
- Mark as PARTIAL
- Feedback: List specific missing information needed for full validation

**Mark as FAILED only if:**
- **Workspace verification fails**: Success criteria not met in actual workspace state (verified with workspace tools)
  - Required files don't exist (verified with ` + "`list_workspace_files`" + `)
  - File contents don't match requirements (verified with ` + "`read_workspace_file`" + `)
  - Required outputs don't exist in workspace
  - Workspace state doesn't match what success criteria requires

- **Execution verification fails**: Execution history shows execution agent didn't actually do the work
  - Required tool calls are missing from execution history (e.g., API calls not made, data not retrieved)
  - Execution history shows fake/hardcoded data instead of real tool responses
  - Files exist in workspace but execution history doesn't show how they were created properly
  - Data in workspace files doesn't match what execution history shows was retrieved from tools
  - Execution agent created files without making required API calls or data retrieval operations

- **Dual verification mismatch**: Workspace and execution history don't align
  - Files exist but execution history shows no corresponding tool calls
  - File contents don't match tool responses in execution history
  - Execution history shows different data than what's in workspace files

{{if .IsCodeExecutionMode}}
- **CODE EXECUTION MODE ADDITIONAL**: Code claims don't match workspace reality OR execution history
  - Code claims files created but workspace verification shows they don't exist
  - Code claims data retrieved but workspace state shows no data
  - Code output is hallucinated (see CODE EXECUTION MODE VALIDATION section)
  - Code created files but execution history doesn't show proper tool calls
{{end}}
- **Missing evidence**: Cannot verify success criteria requirements using workspace tools OR execution history

**Do NOT mark as FAILED for:**
- Retries or multiple attempts (if final state meets criteria)
- Tool call failures that were eventually resolved
- Errors that didn't prevent final success
- "Messy" execution paths (if end result is correct)

{{if .HasLoopCondition}}
## 🔄 LOOP CONDITION CHECK MODE (When Applicable)

**When LoopCondition is provided in the user message:**
- Focus on evaluating the LOOP CONDITION, not the full success criteria
- Return loop_condition_met: true if condition is met, false otherwise
- Return loop_reasoning: Detailed explanation of why the loop condition is or is not met
- Still return is_success_criteria_met, execution_status, and reasoning for consistency (but focus on loop condition evaluation)

**Loop Condition Check Steps:**
1. **Review Execution History**: Analyze conversation for evidence related to the loop condition (as reference)
2. **Verify with Workspace Tools**: Use workspace tools to verify if loop condition is actually met
3. **Assess Evidence**: Determine if the loop condition is satisfied based on workspace verification

**Decision**:
- ✅ **LOOP CONDITION MET**: Loop condition is satisfied - step can exit loop
- ❌ **LOOP CONDITION NOT MET**: Loop condition is not satisfied - step must continue looping
{{end}}

## 📤 OUTPUT FORMAT

**USE THE 'submit_validation_result' TOOL TO SUBMIT YOUR VALIDATION ANALYSIS**

You MUST call the 'submit_validation_result' tool with your validation analysis. Do NOT return JSON directly in your response - use the tool instead.

The tool accepts a structured object with:
- is_success_criteria_met: boolean - Whether the success criteria was met based on workspace verification
- execution_status: string - Overall status (COMPLETED/PARTIAL/FAILED/INCOMPLETE)
- reasoning: string - Detailed reasoning for the validation decision. MUST explain: (1) What evidence was found (or missing) for each part of success criteria, (2) Execution history evidence related to each requirement (quote tool calls, reference tool responses, cite execution steps), (3) Any errors or failures in execution history, (4) Why the decision was made (pass/fail). Be specific and reference actual workspace verification results AND execution history evidence. For each requirement, explicitly cite the execution history evidence that demonstrates it was met or not met.
- feedback: array of objects with type, description, and severity (HIGH/MEDIUM/LOW)
{{if .HasLoopCondition}}
- loop_condition_met: boolean - **REQUIRED** - Whether the loop condition is met
- loop_reasoning: string - **REQUIRED** - Detailed reasoning for loop condition evaluation
{{else}}
**CRITICAL**: Do NOT include loop_condition_met or loop_reasoning fields in your JSON response. These fields are ONLY used when LoopCondition is provided in the user message. Since LoopCondition is NOT provided, these fields must NOT appear in your response at all.
{{end}}

**CRITICAL**: You MUST call the 'submit_validation_result' tool with your validation analysis. The tool will be available to you - use it to submit your structured validation response. Do NOT return JSON directly in your text response. Focus on workspace verification results and actual evidence found.`

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
		DecisionReasoning:       templateVars["DecisionReasoning"],
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

## 📝 **EXECUTION CONVERSATION HISTORY**

**IMPORTANT**: Follow the dual verification process in the system prompt. You MUST verify BOTH workspace state AND execution history to prevent fake files.

{{if eq .IsCodeExecutionMode "true"}}
**⚠️ CODE EXECUTION MODE DETECTED**: Follow the CODE EXECUTION MODE VALIDATION section in the system prompt.
{{end}}

{{.ExecutionHistory}}

{{if .DecisionReasoning}}
## 🎯 **IMPORTANT: Decision Context - READ CAREFULLY**

{{.DecisionReasoning}}

**🚨 CRITICAL: This decision context is IMPORTANT and MUST be considered when validating this step.**

**How to use this context:**
- **READ AND UNDERSTAND** why this step is being executed (what condition was evaluated)
- **USE the decision reasoning** to inform your validation approach and decision-making
- **CONSIDER the decision result and reasoning** when determining if the step was executed correctly
- The reasoning explains what was evaluated in the previous decision step and why routing led here
- **The execution output from the decision step** provides context about what was done before the decision was made
- **This context directly impacts** how you should validate this step's execution
{{end}}

{{if .LoopCondition}}
## 🧠 **YOUR TASK: Loop Condition Check**

**Loop Condition**: {{.LoopCondition}}

**Task**: Evaluate if the loop condition is met. Follow the dual verification process in the system prompt (workspace tools + execution history).

**Return**: loop_condition_met (boolean) and loop_reasoning (string) in your validation result.
{{else}}
## 🧠 **YOUR TASK: Validate Success Criteria**

**Success Criteria**: {{.StepSuccessCriteria}}

**Task**: Validate if the step "{{.StepTitle}}" was completed successfully.

**Process**: Follow the validation procedure in the system prompt:
1. Parse success criteria into requirements
2. For EACH requirement: Perform dual verification (workspace tools + execution history)
3. Determine status: ALL verified? → COMPLETED | ANY failed? → FAILED

**Call 'submit_validation_result' tool with your analysis.**
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
