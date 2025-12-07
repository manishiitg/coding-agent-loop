package todo_creation_human

import (
	"context"
	"encoding/json"
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

// PrerequisiteDetectionResponse represents the structured response from prerequisite detection analysis
type PrerequisiteDetectionResponse struct {
	FailureType         string `json:"failure_type"`                     // "prerequisite" | "execution" - type of failure detected
	ShouldRetryFromStep *int   `json:"should_retry_from_step,omitempty"` // 0-based index of step to retry from (for prerequisite failures)
	RetryReason         string `json:"retry_reason,omitempty"`           // Reason for retrying from specific step
	Reasoning           string `json:"reasoning"`                        // Detailed reasoning for the decision
}

// HumanControlledTodoPlannerPrerequisiteDetectionAgent detects if validation failure is due to missing prerequisites
type HumanControlledTodoPlannerPrerequisiteDetectionAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerPrerequisiteDetectionAgent creates a new prerequisite detection agent
func NewHumanControlledTodoPlannerPrerequisiteDetectionAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerPrerequisiteDetectionAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPrerequisiteDetectionAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerPrerequisiteDetectionAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hctppda *HumanControlledTodoPlannerPrerequisiteDetectionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for prerequisite detection agent - use ExecuteStructured() instead")
}

// ExecuteStructured executes the prerequisite detection agent and returns structured output
func (hctppda *HumanControlledTodoPlannerPrerequisiteDetectionAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*PrerequisiteDetectionResponse, []llmtypes.MessageContent, error) {
	// Build schema for structured output
	schema := `{
		"type": "object",
		"properties": {
			"failure_type": {
				"type": "string",
				"enum": ["prerequisite", "execution"],
				"description": "Type of failure detected. Use 'prerequisite' if the failure is due to missing prerequisite from previous step (as described in one of the prerequisite rules). Use 'execution' if the failure is due to execution issues."
			},
			"should_retry_from_step": {
				"type": "integer",
				"description": "0-based index of step to retry from. Only set this when failure_type is 'prerequisite' and the condition in one of the prerequisite rules is met. Use the dependency_step_info.step_index from the matching rule. Leave null/undefined for execution failures."
			},
			"retry_reason": {
				"type": "string",
				"description": "Reason for retrying from specific step. Required when should_retry_from_step is set. Should indicate which prerequisite rule matched."
			},
			"reasoning": {
				"type": "string",
				"description": "Detailed reasoning for the prerequisite detection decision. MUST explain: (1) Which prerequisite rules were evaluated, (2) Whether any rule's condition was met in the execution history, (3) Why it's a prerequisite failure vs execution failure, (4) Which specific rule matched (if any) and why."
			}
		},
		"required": ["failure_type", "reasoning"]
	}`

	// Generate system prompt and user message
	systemPrompt := hctppda.prerequisiteDetectionSystemPromptProcessor(templateVars)
	userMessage := hctppda.prerequisiteDetectionUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Define tool name and description for structured output via tool calls
	toolName := "submit_prerequisite_detection_result"
	toolDescription := "Submit the prerequisite detection analysis result. This tool should be called with the structured response containing failure type, retry step (if prerequisite failure), and reasoning."

	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[PrerequisiteDetectionResponse](
		hctppda.BaseOrchestratorAgent,
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

// prerequisiteDetectionSystemPromptProcessor generates the system prompt for prerequisite detection agent
func (hctppda *HumanControlledTodoPlannerPrerequisiteDetectionAgent) prerequisiteDetectionSystemPromptProcessor(templateVars map[string]string) string {
	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	prerequisiteInfoJSON := templateVars["PrerequisiteInfo"]
	validationResponseJSON := templateVars["ValidationResponse"]

	// Create template data
	type SystemPromptTemplate struct {
		WorkspacePath      string
		PrerequisiteInfo   string
		ValidationResponse string
		CurrentDate        string
		CurrentTime        string
	}
	templateData := SystemPromptTemplate{
		WorkspacePath:      templateVars["WorkspacePath"],
		PrerequisiteInfo:   prerequisiteInfoJSON,
		ValidationResponse: validationResponseJSON,
		CurrentDate:        currentDate,
		CurrentTime:        currentTime,
	}

	// Define the system prompt template
	templateStr := `# Prerequisite Detection Agent

## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Prerequisite Detection Agent
- **Responsibility**: Analyze validation failures to determine if they are due to missing prerequisites from previous steps
- **Mode**: Failure type classification (prerequisite vs execution)

## 📋 **CONTEXT**

You are called AFTER validation has failed. Your task is to determine if the validation failure is due to a missing prerequisite from a previous step, or if it's an execution failure.

**Validation Response:**
{{.ValidationResponse}}

**Prerequisite Information:**
{{.PrerequisiteInfo}}

## 🔍 **YOUR TASK**

Analyze the validation failure and determine if it's due to a missing prerequisite. You have access to:
1. The validation response (which indicates validation failed)
2. The execution history (from the validation context)
3. Prerequisite rules that define when to detect prerequisite failures

## 📊 **PREREQUISITE RULES STRUCTURE**

The prerequisite information contains prerequisite rules. Each rule specifies:
- **Condition**: The description of when to detect prerequisite failures (e.g., "if login session is missing or expired")
- **Target Step**: The step number and title to navigate back to if the condition is met
- **Dependency Status**: Whether the dependency step completed successfully
- **Context Output**: Whether the expected context output file exists

## 🔄 **PREREQUISITE FAILURE DETECTION PROCESS**

**Step 1: Parse Prerequisite Rules**
- Read the prerequisite rules from the formatted information
- Understand each rule's condition and target step

**Step 2: Evaluate Each Rule**
For each prerequisite rule:
1. **Analyze the Condition**: Understand when to detect prerequisite failures based on the rule's condition description
   - Extract the condition (e.g., "login session is missing", "config file is missing")
   - Identify the target step number from the rule (e.g., "Step 0", "Step 1")

2. **Analyze Execution History**: Check if the condition in the description is met in the execution history:
   - Look for error messages matching the condition (e.g., "session expired", "logged out", "authentication failed", "file not found")
   - Check if prerequisite state matches description (e.g., "login session missing", "not authenticated", "config file missing")
   - Verify if the failure is due to missing prerequisite vs execution error

3. **Check Dependency Step Info**: Review the dependency step information:
   - Whether the dependency step completed successfully
   - Whether context output files from the dependency exist
   - Step index for navigation

**Step 3: Make Decision**
- **If ANY rule's condition is met** → This is a **PREREQUISITE FAILURE**
  - Set failure_type to "prerequisite"
  - Set should_retry_from_step to the target step index (0-based). The step number is shown in the rule (e.g., "Step 0" = index 0, "Step 1" = index 1)
  - Set retry_reason to explain which rule matched and why (e.g., "Login session expired as described in prerequisite rule for step 0")
  - In reasoning field: Explicitly mention that prerequisite failure was detected and which rule matched

- **If NO rule's condition is met** → This is an **EXECUTION FAILURE**
  - Set failure_type to "execution"
  - **CRITICAL**: Do NOT set should_retry_from_step (leave it null/undefined) - execution failures only retry the current step, never navigate to other steps
  - In reasoning field: MUST explain which prerequisites were evaluated and why this is an execution failure (not a prerequisite failure). For example: "Prerequisites were checked: [list which prerequisites were evaluated]. None of the prerequisite conditions were met. This is an execution failure because [reason]."

## 📝 **EXAMPLES**

**Example 1: Prerequisite Failure**
- Rule 1: Condition: "If login session is missing or expired", Target Step: Step 0
- Execution history shows: "session expired", "authentication failed", "not logged in"
- Decision: PREREQUISITE FAILURE
- should_retry_from_step: 0 (Step 0 = index 0)
- retry_reason: "Login session expired as described in prerequisite rule for step 0"

**Example 2: Execution Failure**
- Rule 1: Condition: "If login session is missing or expired", Target Step: Step 0
- Execution history shows: "API timeout", "network error", "invalid request format"
- Decision: EXECUTION FAILURE
- should_retry_from_step: null/undefined
- retry_reason: "" (empty, not set)

**Example 3: Multiple Rules**
- Rule 1: Condition: "If login session is missing or expired", Target Step: Step 0
- Rule 2: Condition: "If config file is missing", Target Step: Step 1
- Execution history shows: "config file not found"
- Decision: PREREQUISITE FAILURE (Rule 2 matches)
- should_retry_from_step: 1 (Step 1 = index 1)
- retry_reason: "Config file is missing as described in prerequisite rule for step 1"

## ⚠️ **CRITICAL RULES**

1. **Only set should_retry_from_step if ONE of the rule's conditions is clearly met**
2. **Use the step number from the matching rule for should_retry_from_step** (Step N = index N-1, so Step 0 = index 0, Step 1 = index 1, etc.)
3. **If you're unsure or no rules match, default to EXECUTION FAILURE (no navigation)**
4. **Each rule is independent - evaluate all rules and use the first one that matches**
5. **Be specific in reasoning - list which prerequisites were checked and why they did/didn't match**

## 📤 **OUTPUT FORMAT**

**USE THE 'submit_prerequisite_detection_result' TOOL TO SUBMIT YOUR ANALYSIS**

You MUST call the 'submit_prerequisite_detection_result' tool with your analysis. Do NOT return JSON directly in your response - use the tool instead.

The tool accepts a structured object with:
- failure_type: string - **REQUIRED** - Type of failure ("prerequisite" or "execution")
- should_retry_from_step: integer - **OPTIONAL** - 0-based index of step to retry from. Only set when failure_type is "prerequisite" and condition in one of the prerequisite rules is met. Use the dependency_step_info.step_index from the matching rule. Leave null/undefined for execution failures.
- retry_reason: string - **OPTIONAL** - Reason for retrying from specific step. Include when should_retry_from_step is set. Should indicate which prerequisite rule matched.
- reasoning: string - **REQUIRED** - Detailed reasoning for the decision

**Example JSON structure:**
` + "```json" + `
{
  "failure_type": "prerequisite",
  "should_retry_from_step": 0,
  "retry_reason": "Login session expired as described in prerequisite rule for step 0",
  "reasoning": "Evaluated prerequisite rules: [Rule 1: depends on step-0, condition: 'login session missing or expired']. Execution history shows 'session expired' and 'authentication failed' messages, which match the condition in Rule 1. This is a prerequisite failure because the login session from step 0 is missing/expired, requiring navigation back to step 0 to re-establish the session."
}
` + "```" + `

**CRITICAL**: You MUST call the 'submit_prerequisite_detection_result' tool with your analysis. The tool will be available to you - use it to submit your structured response.`

	// Parse and execute the template
	tmpl, err := template.New("prerequisiteDetectionSystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing prerequisite detection system prompt template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing prerequisite detection system prompt template: %v", err)
	}

	return result.String()
}

// prerequisiteDetectionUserMessageProcessor generates the user message for prerequisite detection agent
func (hctppda *HumanControlledTodoPlannerPrerequisiteDetectionAgent) prerequisiteDetectionUserMessageProcessor(templateVars map[string]string) string {
	// Parse validation response to extract key information
	var validationResponse *ValidationResponse
	if validationResponseJSON := templateVars["ValidationResponse"]; validationResponseJSON != "" {
		if err := json.Unmarshal([]byte(validationResponseJSON), &validationResponse); err != nil {
			// If parsing fails, continue with empty validation response
			validationResponse = nil
		}
	}

	// Create template data
	type UserMessageTemplate struct {
		StepTitle           string
		StepDescription     string
		ValidationResponse  string
		PrerequisiteInfo    string
		ExecutionHistory    string
		WorkspacePath       string
		IsCodeExecutionMode string
		ValidationReasoning string
		ValidationStatus    string
	}

	validationReasoning := ""
	validationStatus := ""
	if validationResponse != nil {
		validationReasoning = validationResponse.Reasoning
		validationStatus = validationResponse.ExecutionStatus
	}

	templateData := UserMessageTemplate{
		StepTitle:           templateVars["StepTitle"],
		StepDescription:     templateVars["StepDescription"],
		ValidationResponse:  templateVars["ValidationResponse"],
		PrerequisiteInfo:    templateVars["PrerequisiteInfo"],
		ExecutionHistory:    templateVars["ExecutionHistory"],
		WorkspacePath:       templateVars["WorkspacePath"],
		IsCodeExecutionMode: templateVars["IsCodeExecutionMode"],
		ValidationReasoning: validationReasoning,
		ValidationStatus:    validationStatus,
	}

	// Define the user message template
	templateStr := `# Prerequisite Detection Task

## 📋 **STEP CONTEXT**
- **Title**: {{.StepTitle}}
- **Description**: {{.StepDescription}}
- **Workspace**: {{.WorkspacePath}}
- **Validation Status**: {{.ValidationStatus}}

## ❌ **VALIDATION FAILURE**

Validation has failed for this step. Your task is to determine if the failure is due to a missing prerequisite from a previous step.

**Validation Reasoning:**
{{.ValidationReasoning}}

**Full Validation Response:**
{{.ValidationResponse}}

## 📝 **EXECUTION CONVERSATION HISTORY**

The execution history below contains the ACTUAL execution flow that led to the validation failure. Analyze it carefully to determine if prerequisite conditions are met.

{{if eq .IsCodeExecutionMode "true"}}
**⚠️ CODE EXECUTION MODE DETECTED**: The execution used code execution mode. Check for prerequisite-related errors in code execution output.
{{end}}

{{.ExecutionHistory}}

## 🔄 **PREREQUISITE RULES**

The prerequisite information below defines when to detect prerequisite failures. Evaluate each rule against the execution history.

{{.PrerequisiteInfo}}

## 🧠 **YOUR TASK**

Analyze the validation failure and determine if it's due to a missing prerequisite:

1. **Parse Prerequisite Rules**: Extract all prerequisite rules from the prerequisite information
2. **Evaluate Each Rule**: Check if any rule's condition is met in the execution history
3. **Make Decision**:
   - If ANY rule's condition is met → PREREQUISITE FAILURE → Set failure_type to "prerequisite" and should_retry_from_step
   - If NO rule's condition is met → EXECUTION FAILURE → Set failure_type to "execution" (no should_retry_from_step)

**CRITICAL INSTRUCTIONS:**
- Be STRICT: Only mark as prerequisite failure if a rule's condition is clearly met
- Check execution history for error messages matching prerequisite conditions
- If unsure, default to EXECUTION FAILURE (no navigation)
- Use the dependency_step_info.step_index from the matching rule for should_retry_from_step
- Provide detailed reasoning explaining which prerequisites were checked and why they did/didn't match

Follow the prerequisite detection process in the system prompt. Call the 'submit_prerequisite_detection_result' tool with your analysis.`

	// Parse and execute the template
	tmpl, err := template.New("prerequisiteDetectionUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing prerequisite detection user message template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing prerequisite detection user message template: %v", err)
	}

	return result.String()
}
