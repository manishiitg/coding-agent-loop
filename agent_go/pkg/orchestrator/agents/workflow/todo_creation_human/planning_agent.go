package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledTodoPlannerPlanningTemplate holds template variables for human-controlled planning prompts
type HumanControlledTodoPlannerPlanningTemplate struct {
	Objective     string
	WorkspacePath string
}

// HumanControlledTodoPlannerPlanningAgent creates a fast, simplified plan from the objective
type HumanControlledTodoPlannerPlanningAgent struct {
	*agents.BaseOrchestratorAgent
}

// FlexibleContextOutput handles both string and array types for context_output field
// This prevents JSON parsing errors when LLM returns arrays instead of strings
type FlexibleContextOutput string

// UnmarshalJSON implements custom unmarshaling for FlexibleContextOutput
// Handles both string and array formats to prevent parsing errors
func (f *FlexibleContextOutput) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexibleContextOutput(s)
		return nil
	}

	// Try to unmarshal as array
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		// Join array elements with comma and space
		*f = FlexibleContextOutput(strings.Join(arr, ", "))
		return nil
	}

	// If both fail, return the error from string unmarshal
	return fmt.Errorf("failed to unmarshal context_output as string or array")
}

// String returns the string value
func (f FlexibleContextOutput) String() string {
	return string(f)
}

// PlanStep represents a step in the planning output
type PlanStep struct {
	Title               string                `json:"title"`
	Description         string                `json:"description"`
	SuccessCriteria     string                `json:"success_criteria"`
	ContextDependencies []string              `json:"context_dependencies"`
	ContextOutput       FlexibleContextOutput `json:"context_output"`             // Use flexible type to handle string or array
	SuccessPatterns     []string              `json:"success_patterns,omitempty"` // what worked (includes tools)
	FailurePatterns     []string              `json:"failure_patterns,omitempty"` // what failed (includes tools to avoid)
	HasLoop             bool                  `json:"has_loop"`                   // true if step needs to loop
	LoopCondition       string                `json:"loop_condition"`             // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations       int                   `json:"max_iterations,omitempty"`   // max iterations (default: 10)
	LoopDescription     string                `json:"loop_description,omitempty"` // human-readable explanation
}

// PlanningResponse represents the structured response from planning
type PlanningResponse struct {
	Steps []PlanStep `json:"steps"`
}

// NewHumanControlledTodoPlannerPlanningAgent creates a new human-controlled todo planner planning agent
func NewHumanControlledTodoPlannerPlanningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerPlanningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanningAgentType, // Reuse the same type for now
		eventBridge,
	)

	return &HumanControlledTodoPlannerPlanningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the planning agent and returns structured JSON output
// userMessage: The user message to send (e.g., "Generate plan" for CREATE, or human feedback for UPDATE)
func (hctppa *HumanControlledTodoPlannerPlanningAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent, userMessage string) (*PlanningResponse, []llmtypes.MessageContent, error) {
	// Define the JSON schema for plan generation
	schema := `{
		"type": "object",
		"properties": {
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"title": {
							"type": "string",
							"description": "Short, clear title for the step"
						},
						"description": {
							"type": "string",
							"description": "COMPREHENSIVE, DETAILED description of what this step accomplishes. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context. Write a complete, detailed explanation that fully captures the step's requirements and scope."
						},
						"success_criteria": {
							"type": "string",
							"description": "Detailed explanation of how to verify this step was completed successfully - be specific and comprehensive"
						},
						"context_dependencies": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
						},
						"context_output": {
							"type": "string",
							"description": "What context file this step will create for subsequent steps - e.g., 'step_1_results.md'. Can be string or array (will be converted)."
						},
						"has_loop": {
							"type": "boolean",
							"description": "Whether this step needs to loop until condition is met. Set to true when step requires: (1) Polling/waiting for services or resources to become ready, (2) Retrying operations until they succeed, (3) Iterating until data appears or condition changes, (4) Checking status repeatedly until a goal is achieved, (5) Complex multi-operation tasks where the outcome is uncertain, (6) Tasks depending on external systems/APIs that might not be immediately available."
						},
						"loop_condition": {
							"type": "string",
							"description": "Condition that must be met to exit the loop (REQUIRED when has_loop is true). This should be the same as success_criteria - describe the condition that must be met."
						},
						"max_iterations": {
							"type": "integer",
							"description": "Maximum number of loop iterations allowed to prevent infinite loops (default: 10). Use higher values (20-50) for long-running operations, use lower values (3-5) for quick status checks. Only include when has_loop is true."
						},
						"loop_description": {
							"type": "string",
							"description": "Human-readable explanation of why the loop is needed and how it works. Only include when has_loop is true and a description is provided."
						}
					},
					"required": ["title", "description", "success_criteria", "has_loop"]
				}
			}
		},
		"required": ["steps"]
	}`

	// Generate system prompt using the processor
	systemPrompt := planningSystemPromptProcessor(templateVars)

	// Create a simple input processor that just returns the user message
	// In UPDATE mode: ExistingPlanJSON is in the system prompt, userMessage (human feedback) is the user message
	// In CREATE mode: userMessage is "Generate plan"
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use the new ExecuteStructuredWithInputProcessorViaTool method
	toolName := "submit_planning_response"
	toolDescription := "Submit the final structured planning response in JSON format. This tool should be called when you have completed the plan and are ready to provide the structured output."

	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[PlanningResponse](hctppa.BaseOrchestratorAgent, ctx, templateVars, inputProcessor, conversationHistory, schema, systemPrompt, false, toolName, toolDescription)
	if err != nil {
		return nil, nil, err
	}

	return &result, updatedHistory, nil
}

// Execute implements the OrchestratorAgent interface (kept for compatibility, but ExecuteStructured should be used)
func (hctppa *HumanControlledTodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// This method is kept for interface compatibility but is not used in the current implementation.
	// The controller uses ExecuteStructured instead.
	// Return a simple user message that will work if this method is ever called
	userMessage := "Generate plan"
	if humanFeedback := templateVars["HumanFeedback"]; humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
		userMessage = humanFeedback
	}

	result, updatedHistory, err := hctppa.ExecuteStructured(ctx, templateVars, conversationHistory, userMessage)
	if err != nil {
		return "", conversationHistory, err
	}

	// Convert structured response to string for compatibility
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", conversationHistory, fmt.Errorf("failed to marshal structured response: %w", err)
	}

	return string(resultJSON), updatedHistory, nil
}

// planningSystemPromptProcessor routes to appropriate system prompt based on whether updating or creating
func planningSystemPromptProcessor(templateVars map[string]string) string {
	// Check if we're updating an existing plan
	if existingPlanJSON := templateVars["ExistingPlanJSON"]; existingPlanJSON != "" && strings.TrimSpace(existingPlanJSON) != "" {
		return planningSystemPromptProcessorForUpdate(templateVars)
	}
	return planningSystemPromptProcessorForCreate(templateVars)
}

// planningSystemPromptProcessorForCreate generates system prompt for creating a new plan
func planningSystemPromptProcessorForCreate(templateVars map[string]string) string {
	templateData := map[string]string{
		"Objective":     templateVars["Objective"],
		"WorkspacePath": templateVars["WorkspacePath"],
		"VariableNames": templateVars["VariableNames"],
	}

	templateStr := `## 🤖 AGENT IDENTITY
- **Role**: Planning Agent (Create Mode)
- **Responsibility**: Generate a comprehensive structured plan in JSON format to execute the objective
- **Output Format**: Structured JSON (not markdown, not files)

## 🎯 OBJECTIVE

**PRIMARY OBJECTIVE**: {{.Objective}}

**WORKSPACE**: {{.WorkspacePath}}

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES

**IMPORTANT**: The objective may contain variable placeholders like {{"{{"}}VARIABLE_NAME{{"}}"}}. These are environment-specific values that will be replaced at execution time.

Available variables:
{{.VariableNames}}

**CRITICAL VARIABLE HANDLING RULES**:
- **PRESERVE variable placeholders** in all plan steps (title, description, success_criteria, etc.)
- **NEVER replace placeholders** with actual values - keep them as {{"{{"}}VARIABLE_NAME{{"}}"}}
- **NEVER hard-code values** - always use variable placeholders when values change across environments
- **Use variables consistently** - if the objective mentions a variable, use it in relevant step descriptions
- **DO NOT CREATE NEW VARIABLES** - Only use variables that are already present in the objective or existing plan. Do not introduce new variable placeholders that weren't already defined.
- **Example**: If objective has {{"{{"}}AWS_ACCOUNT_ID{{"}}"}}, use {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} in step descriptions, not actual account IDs

**Why?** Plans must work across dev/staging/prod environments without modification. Execution agents will replace placeholders with actual values.

{{end}}
## 🔧 MCP TOOLS AND CAPABILITIES

**MCP Tools Available**: You have access to MCP servers and their tools. These are provided so you understand what capabilities will be available during execution.

**CRITICAL TOOL USAGE RULES**:
- **DO NOT execute tools unless absolutely required** for planning purposes
- **You may review available tools** to understand capabilities and inform your plan
- **Only execute tools if**:
  - You need to verify specific information that directly affects the plan structure
  - You need to check constraints, requirements, or system states that cannot be inferred
  - The information is critical for creating an accurate, executable plan
- **Default behavior**: Generate the plan based on the objective and your knowledge of available tools WITHOUT executing them
- **Remember**: Execution agents will have the same tools available - focus on planning strategy, not execution details

## 📋 PLANNING GUIDELINES
- **Comprehensive Scope**: Create complete plan to achieve objective
- **Actionable Steps**: Each step should be concrete and executable
- **DETAILED DESCRIPTIONS**: Write COMPREHENSIVE, DETAILED descriptions for each step. Descriptions should be thorough, complete, and provide sufficient context. Include specific details about what needs to be accomplished, what tools or approaches might be needed, what outcomes are expected, and any important considerations.
- **Clear Success Criteria**: Define how to verify each step worked - be specific and detailed
- **Logical Order**: Steps should follow logical sequence
- **Focus on Strategy**: Plan what needs to be done, not how to do it (execution details will be handled by execution agents)
- **Agent Execution Limits**: Each step should be completable by one agent using MCP tools before reaching context output limits
- **All Steps Are Validated**: Every step will be validated after execution - no need to specify validation requirements
- **Loop Support**: Set has_loop to true when the step requires: (1) Polling/waiting for a service or resource to become ready, (2) Retrying operations until they succeed, (3) Iterating until data appears or condition changes, (4) Checking status repeatedly until a goal is achieved, (5) Complex multi-operation tasks where the outcome is uncertain, (6) Tasks that depend on external systems/APIs that might not be immediately available. When has_loop is true, you MUST provide loop_condition (same as success_criteria) and max_iterations (default: 10, use 20-50 for long-running operations, use 3-5 for quick status checks).

## 🤖 MULTI-AGENT COORDINATION
- **Different Agents**: Each step is executed by a different agent
- **Data Sharing**: Steps may need to share context/data between each other
- **Context Dependencies**: Each step should specify what context files it needs from previous steps (use empty array [] if none)
- **Context Output**: Each step should specify what context file it will create for subsequent steps
- **Use relative paths only** - NEVER use absolute paths

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 OUTPUT REQUIREMENTS

**CRITICAL**: 
- When you have completed the plan, call the submit_planning_response tool with the structured JSON data
- The tool accepts the complete plan structure matching the schema
- Do NOT read or write any files
- Do NOT include success_patterns or failure_patterns (they will be added later by learning integration agent)
- Do NOT include markdown formatting or explanations - just call the tool with pure JSON data
- The tool will handle the structured output submission
`

	tmpl, err := template.New("human_controlled_planning_create").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled planning template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled planning template: %v", err)
	}

	return result.String()
}

// planningSystemPromptProcessorForUpdate generates system prompt for updating an existing plan
func planningSystemPromptProcessorForUpdate(templateVars map[string]string) string {
	templateData := map[string]string{
		"Objective":        templateVars["Objective"],
		"WorkspacePath":    templateVars["WorkspacePath"],
		"ExistingPlanJSON": templateVars["ExistingPlanJSON"],
		"VariableNames":    templateVars["VariableNames"],
	}

	templateStr := `## 🤖 AGENT IDENTITY
- **Role**: Planning Agent (Update Mode)
- **Responsibility**: Update an existing structured plan in JSON format based on human feedback
- **Output Format**: Structured JSON (not markdown, not files)
- **Mode**: UPDATE EXISTING PLAN - Make minimal, surgical changes only

## 🎯 OBJECTIVE

**PRIMARY OBJECTIVE**: {{.Objective}}

**WORKSPACE**: {{.WorkspacePath}}

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES

**IMPORTANT**: The objective may contain variable placeholders like {{"{{"}}VARIABLE_NAME{{"}}"}}. These are environment-specific values that will be replaced at execution time.

Available variables:
{{.VariableNames}}

**CRITICAL VARIABLE HANDLING RULES**:
- **PRESERVE variable placeholders** in all plan steps (title, description, success_criteria, etc.)
- **NEVER replace placeholders** with actual values - keep them as {{"{{"}}VARIABLE_NAME{{"}}"}}
- **NEVER hard-code values** - always use variable placeholders when values change across environments
- **Use variables consistently** - if the objective mentions a variable, use it in relevant step descriptions
- **DO NOT CREATE NEW VARIABLES** - Only use variables that are already present in the objective or existing plan. Do not introduce new variable placeholders that weren't already defined.
- **Example**: If objective has {{"{{"}}AWS_ACCOUNT_ID{{"}}"}}, use {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} in step descriptions, not actual account IDs

**Why?** Plans must work across dev/staging/prod environments without modification. Execution agents will replace placeholders with actual values.

{{end}}
## 📄 EXISTING PLAN (MUST BE PRESERVED)

**CRITICAL**: The current plan contents are provided below. This plan already achieves the objective. Your task is to make MINIMAL, SURGICAL changes based on human feedback only.

**EXISTING PLAN**:
{{.ExistingPlanJSON}}

## 🎯 UPDATE MODE PRINCIPLES

**CRITICAL**: You are in UPDATE mode, not CREATE mode. The existing plan already achieves the objective and is working correctly.

**PRIMARY DRIVER**: Human feedback in the user message is the ONLY reason to make changes. Do NOT regenerate based on objective.

**PRESERVATION FIRST**: Your default behavior should be to preserve everything. Only change what is explicitly requested.

**WHEN IN DOUBT, ASK**: If you have any uncertainty about the feedback or how to update the plan, ALWAYS ask clarifying questions using the human_feedback tool BEFORE making changes. Do NOT guess or make assumptions.

## 📋 UPDATE GUIDELINES

- **PRESERVE STRUCTURE**: Maintain the exact same step count, step order, and overall plan structure
- **MINIMAL CHANGES**: Only modify steps that are explicitly mentioned in human feedback
- **SURGICAL PRECISION**: Make targeted, focused changes to specific fields rather than rewriting steps
- **PRESERVE QUALITY**: Keep the same level of detail and quality in all unchanged steps
- **NO REGENERATION**: Do NOT create a new plan - update the existing one
- **FEEDBACK-DRIVEN**: If feedback doesn't mention a step, preserve it exactly as-is
- **PRESERVE VARIABLES**: Keep all variable placeholders ({{"{{"}}VARIABLE_NAME{{"}}"}}) exactly as they appear
- **DO NOT CREATE NEW VARIABLES**: Only use variables that are already present in the existing plan. Do not introduce new variable placeholders that weren't already defined in the original plan.

## 🔧 MCP TOOLS AND CAPABILITIES

**MCP Tools Available**: You have access to MCP servers and their tools. These are provided for reference only.

**TOOL USAGE IN UPDATE MODE**:
- **DO NOT execute tools** - you are updating an existing plan, not creating from scratch
- **DO NOT verify information** - the existing plan is already correct
- **Focus on text updates** based on feedback only
- **CRITICAL - When in Doubt, Ask Questions**: If you have ANY doubts or uncertainties about the human feedback or how to update the plan, you MUST use the human_feedback tool to ask clarifying questions BEFORE making changes. Do NOT guess or make assumptions. Always ask for clarification when:
  - The human feedback is unclear or ambiguous
  - You are unsure what specific changes are requested
  - You need clarification about which parts of the plan should be modified
  - You want to confirm your understanding before making changes
  - You are uncertain about preserving vs. changing certain parts
  - The feedback could be interpreted in multiple ways

## 🤖 MULTI-AGENT COORDINATION

- **Context Preservation**: If feedback doesn't mention context dependencies or outputs, preserve them exactly
- **Step Relationships**: Maintain the same context dependency chain unless feedback explicitly changes it
- **Use relative paths only** - NEVER use absolute paths

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 OUTPUT REQUIREMENTS

**CRITICAL**: 
- **DO NOT read plan.json from workspace** - the plan content is already provided in the system prompt above (ExistingPlanJSON)
- When you have completed updating the plan, call the submit_planning_response tool with the updated structured JSON data
- The tool accepts the complete updated plan structure matching the schema
- Make MINIMAL changes - only modify what was requested in feedback
- Do NOT read or write any files
- Do NOT include success_patterns or failure_patterns (they will be added later by learning integration agent)
- Do NOT include markdown formatting or explanations - just call the tool with pure JSON data
- The tool will handle the structured output submission
`

	tmpl, err := template.New("human_controlled_planning_update").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled planning template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled planning template: %v", err)
	}

	return result.String()
}
