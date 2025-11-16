package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

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

// AgentLLMConfig represents LLM configuration for an agent
type AgentLLMConfig struct {
	Provider string `json:"provider,omitempty"` // e.g., "openai", "bedrock", "openrouter", "vertex"
	ModelID  string `json:"model_id,omitempty"` // e.g., "gpt-4o", "claude-3-5-sonnet-20241022"
}

// AgentConfigs represents per-agent configuration for a step
type AgentConfigs struct {
	ExecutionLLM                  *AgentLLMConfig `json:"execution_llm,omitempty"`
	ValidationLLM                 *AgentLLMConfig `json:"validation_llm,omitempty"`
	LearningLLM                   *AgentLLMConfig `json:"learning_llm,omitempty"`
	ExecutionMaxTurns             *int            `json:"execution_max_turns,omitempty"`               // default: 25
	ValidationMaxTurns            *int            `json:"validation_max_turns,omitempty"`              // default: 25
	LearningMaxTurns              *int            `json:"learning_max_turns,omitempty"`                // default: 25
	DisableValidation             bool            `json:"disable_validation,omitempty"`                // skip validation entirely
	DisableLearning               bool            `json:"disable_learning,omitempty"`                  // disable learning for this step
	LearningAfterLoopIteration    bool            `json:"learning_after_loop_iteration,omitempty"`     // run learning after each loop iteration
	LearningDetailLevel           string          `json:"learning_detail_level,omitempty"`             // "exact", "general", or "none" (default: "general")
	SelectedServers               []string        `json:"selected_servers,omitempty"`                  // step-level MCP server selection (subset of preset servers)
	SelectedTools                 []string        `json:"selected_tools,omitempty"`                    // step-level tool selection (format: "server:tool" or "server:*" for all tools)
	EnabledCustomToolCategories   []string        `json:"enabled_custom_tool_categories,omitempty"`    // e.g., ["workspace_tools", "human_tools"] - enables all tools in category
	EnabledCustomTools            []string        `json:"enabled_custom_tools,omitempty"`              // e.g., ["read_workspace_file", "human_feedback"] - enables specific tools (overrides categories if both specified)
	EnableLargeOutputVirtualTools *bool           `json:"enable_large_output_virtual_tools,omitempty"` // Enable/disable large output tools (default: true if nil)
}

// PlanStep represents a step in the planning output
type PlanStep struct {
	Title                    string                `json:"title"`
	Description              string                `json:"description"`
	SuccessCriteria          string                `json:"success_criteria"`
	WhyThisStep              string                `json:"why_this_step,omitempty"` // Optional explanation of why this step is needed
	ContextDependencies      []string              `json:"context_dependencies"`
	ContextOutput            FlexibleContextOutput `json:"context_output"`                        // Use flexible type to handle string or array
	LearningFilesToReference []string              `json:"learning_files_to_reference,omitempty"` // learning files to read for context (execution agent reads full files)
	HasLoop                  bool                  `json:"has_loop"`                              // true if step needs to loop
	LoopCondition            string                `json:"loop_condition"`                        // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations            int                   `json:"max_iterations,omitempty"`              // max iterations (default: 10)
	LoopDescription          string                `json:"loop_description,omitempty"`            // human-readable explanation
	// AgentConfigs removed - now stored separately in step_config.json
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
						"description": "COMPREHENSIVE, DETAILED description of what this step accomplishes. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context. FOR LOOPING STEPS (has_loop=true): Emphasize that progress MUST be saved after EACH iteration in the context output file. Each iteration should update/append to the context file so progress is preserved and visible to the next iteration. Describe how progress accumulates across iterations. Write a complete, detailed explanation that fully captures the step's requirements and scope."
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
							"description": "What context file this step will create for subsequent steps - e.g., 'step_1_results.md'. IMPORTANT: Execution agents work ONLY in the execution folder, so context output files will be written to the execution folder. Can be string or array (will be converted)."
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
						"description": "CRITICAL for looping steps: Describe what happens in EACH ITERATION of the loop. Be specific about: (1) What to check/verify in each iteration, (2) What actions to take in each iteration, (3) What progress indicators to look for, (4) How to save/update progress after each iteration. Example: 'Each iteration: Check deployment status via health endpoint, verify pod readiness count, save current status to context file, wait 30 seconds before next check.' This guides the execution agent on per-iteration behavior. Only include when has_loop is true."
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
		// Check if this is a non-structured response error (text response instead of structured output)
		// IMPORTANT: Return the error directly without wrapping, so runPlanningPhase can detect it
		if agents.IsNonStructuredResponseError(err) {
			// Return the original NonStructuredResponseError with UpdatedHistory so runPlanningPhase can handle it
			// Don't wrap it - wrapping breaks the error type check
			var nonStructuredErr *agents.NonStructuredResponseError
			if errors.As(err, &nonStructuredErr) {
				return nil, nonStructuredErr.UpdatedHistory, err
			}
			return nil, updatedHistory, err
		}
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
	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Calculate execution workspace path (execution folder only)
	executionWorkspacePath := fmt.Sprintf("%s/execution", templateVars["WorkspacePath"])

	templateData := map[string]string{
		"Objective":              templateVars["Objective"],
		"WorkspacePath":          templateVars["WorkspacePath"],
		"ExecutionWorkspacePath": executionWorkspacePath,
		"VariableNames":          templateVars["VariableNames"],
		"CurrentDate":            currentDate,
		"CurrentTime":            currentTime,
	}

	templateStr := `## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Planning Agent (Create Mode)
- **Responsibility**: Generate a comprehensive structured plan in JSON format to execute the objective
- **Output Format**: Structured JSON (not markdown, not files)

## 📁 FILE PERMISSIONS (Planning Agent)

**READ (Only if explicitly required for context):**
- **BY DEFAULT**: Do NOT read any files - plan based on the objective alone
- You may read files from the main workspace ONLY if explicitly instructed or necessary for planning
- **NEVER read from {{.ExecutionWorkspacePath}} folder** - that folder is exclusively for execution agents
- Reading files is optional and should be done sparingly - only when essential for understanding context

**NO WRITE PERMISSIONS:**
- **ABSOLUTELY NO file writing** - This agent does NOT write any files
- Output is ONLY via the 'submit_planning_response' tool with structured JSON
- Do NOT create, modify, or write any files to the workspace
- Do NOT write plan.json or any other files - the system handles file persistence

**CRITICAL**: Your only output method is calling the 'submit_planning_response' tool with structured JSON data. Never write files.

## 🎯 OBJECTIVE

**PRIMARY OBJECTIVE**: {{.Objective}}

**WORKSPACE**: {{.WorkspacePath}}

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES

Available variables:
{{.VariableNames}}

**CRITICAL RULES**:
- **PRESERVE variable placeholders** ({{"{{"}}VARIABLE_NAME{{"}}"}}) in all plan steps - never replace with actual values
- **Use existing variables only** - don't create new variable placeholders
- **Why?** Plans must work across dev/staging/prod environments without modification

{{end}}



## 📋 PLANNING GUIDELINES
- **Comprehensive Scope**: Create complete plan to achieve objective
- **Actionable Steps**: Each step should be concrete and executable with detailed descriptions
- **Clear Success Criteria**: Define how to verify each step worked - be specific and detailed
- **Logical Order**: Steps should follow logical sequence
- **Focus on Strategy**: Plan what needs to be done, not how to do it (execution details will be handled by execution agents)
- **Loop Support**: Set has_loop to true when step requires polling, retrying, or waiting for external systems. When has_loop is true, provide loop_condition (same as success_criteria) and max_iterations (default: 10, use 20-50 for long-running operations, use 3-5 for quick status checks). Each iteration should save progress to context_output file (append/update, don't overwrite).

## 🤖 MULTI-AGENT COORDINATION
- **Each step executed by different agent**: Steps share context via files
- **Execution Folder Limitation**: Execution agents work ONLY in {{.ExecutionWorkspacePath}} folder - all context output files must be written there
- **Context Dependencies**: Specify context files needed from previous steps (use empty array [] if none). Note: Execution agents will read these files from the execution folder.
- **Context Output**: Specify context file to create for subsequent steps (e.g., 'step_1_results.md'). Execution agents will write these files to {{.ExecutionWorkspacePath}} folder only.
- **Use relative paths only** - NEVER use absolute paths
- **Important**: You are the planning agent - do NOT read from {{.ExecutionWorkspacePath}}. Your job is to plan, not to read execution artifacts.

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 OUTPUT REQUIREMENTS

**CRITICAL**: 
- Call submit_planning_response tool with structured JSON data when plan is complete
- **NEVER write files** - output is ONLY via the submit_planning_response tool
- Do NOT include success_patterns/failure_patterns, or add markdown formatting - just pure JSON
- You may read files if needed for context, but writing files is strictly prohibited
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
	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Calculate execution workspace path (execution folder only)
	executionWorkspacePath := fmt.Sprintf("%s/execution", templateVars["WorkspacePath"])

	templateData := map[string]string{
		"Objective":              templateVars["Objective"],
		"WorkspacePath":          templateVars["WorkspacePath"],
		"ExecutionWorkspacePath": executionWorkspacePath,
		"ExistingPlanJSON":       templateVars["ExistingPlanJSON"],
		"VariableNames":          templateVars["VariableNames"],
		"CurrentDate":            currentDate,
		"CurrentTime":            currentTime,
	}

	templateStr := `## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Planning Agent (Update Mode)
- **Responsibility**: Update an existing structured plan in JSON format based on human feedback
- **Output Format**: Structured JSON (not markdown, not files)
- **Mode**: UPDATE EXISTING PLAN - Make intelligent updates based on human feedback

## 📁 FILE PERMISSIONS (Planning Agent)

**READ (Only if explicitly required for context):**
- **BY DEFAULT**: Do NOT read any files - plan based on the objective and existing plan provided above
- You may read files from the main workspace ONLY if explicitly instructed or necessary for planning
- **NEVER read from {{.ExecutionWorkspacePath}} folder** - that folder is exclusively for execution agents
- Reading files is optional and should be done sparingly - only when essential for understanding context
- **Note**: The existing plan content is already provided in the prompt above (ExistingPlanJSON) - you do NOT need to read plan.json

**NO WRITE PERMISSIONS:**
- **ABSOLUTELY NO file writing** - This agent does NOT write any files
- Output is ONLY via the 'submit_planning_response' tool with structured JSON
- Do NOT create, modify, or write any files to the workspace
- Do NOT write plan.json or any other files - the system handles file persistence

**CRITICAL**: Your only output method is calling the 'submit_planning_response' tool with structured JSON data. Never write files.

## 🎯 OBJECTIVE

**PRIMARY OBJECTIVE**: {{.Objective}}

**WORKSPACE**: {{.WorkspacePath}}

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES

Available variables:
{{.VariableNames}}

**CRITICAL RULES**:
- **PRESERVE variable placeholders** ({{"{{"}}VARIABLE_NAME{{"}}"}}) in all plan steps - never replace with actual values
- **Use existing variables only** - don't create new variable placeholders
- **Why?** Plans must work across dev/staging/prod environments without modification

{{end}}
## 📄 EXISTING PLAN

**CRITICAL**: The current plan contents are provided below. This plan already achieves the objective. Your task is to update the plan appropriately based on human feedback. Use your judgment to determine what changes are needed to address the feedback effectively.

**EXISTING PLAN**:
{{.ExistingPlanJSON}}

## 🎯 UPDATE GUIDELINES

**Mode**: UPDATE existing plan (not CREATE). The plan already achieves the objective. Make intelligent updates based on human feedback.

**Key Principles**:
- **Feedback-Driven**: Interpret feedback and make changes that logically address concerns. Adjust scope based on feedback (minor = targeted, substantial = comprehensive)
- **Logical Coherence**: If you change one part, update related parts to maintain consistency
- **Preserve Quality**: Keep same level of detail in all steps (changed and unchanged)
- **Preserve Variables**: Keep all variable placeholders ({{"{{"}}VARIABLE_NAME{{"}}"}}) exactly as they appear - critical for multi-environment support
- **Use Judgment**: Make changes that make sense. Don't hesitate to make substantial changes if feedback suggests fundamental issues, or targeted adjustments for minor feedback

## 🤖 MULTI-AGENT COORDINATION

- **Execution Folder Limitation**: Execution agents work ONLY in {{.ExecutionWorkspacePath}} folder - all context output files must be written there
- **Context Dependencies**: Update context dependencies/outputs as needed based on feedback. Maintain logical consistency - if you restructure steps, update dependency chain accordingly. Note: Execution agents will read these files from the execution folder.
- **Context Output**: Execution agents will write these files to {{.ExecutionWorkspacePath}} folder only.
- **Use relative paths only** - NEVER use absolute paths
- **Important**: You are the planning agent - do NOT read from {{.ExecutionWorkspacePath}}. Your job is to plan, not to read execution artifacts.

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 OUTPUT REQUIREMENTS

**CRITICAL**: 
- Call submit_planning_response tool with updated structured JSON data when done
- **NEVER write files** - output is ONLY via the submit_planning_response tool
- Do NOT read plan.json from workspace (plan content is in ExistingPlanJSON above)
- You may read other files if needed for context, but writing files is strictly prohibited
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
