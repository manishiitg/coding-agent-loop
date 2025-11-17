package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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

// AddPlanStep represents a step to be added with insertion position
type AddPlanStep struct {
	PlanStep
	InsertAfterStepTitle string `json:"insert_after_step_title"` // REQUIRED: Title of step to insert after (use empty string "" to insert at beginning)
}

// PlanningResponse represents the structured response from planning
type PlanningResponse struct {
	Steps   []PlanStep `json:"steps"`
	RunMode string     `json:"run_mode,omitempty"` // "use_same_run", "create_new_runs_always", "create_new_run_once_daily"
}

// PartialPlanStep represents a partial update to a plan step (used only in tool schemas)
type PartialPlanStep struct {
	ExistingStepTitle   string                `json:"existing_step_title"`            // Required: Title of existing step to update
	Title               string                `json:"title,omitempty"`                // Optional: New title (if renaming)
	Description         string                `json:"description,omitempty"`          // Optional: Updated description
	SuccessCriteria     string                `json:"success_criteria,omitempty"`     // Optional: Updated success criteria
	ContextDependencies []string              `json:"context_dependencies,omitempty"` // Optional: Updated context dependencies
	ContextOutput       FlexibleContextOutput `json:"context_output,omitempty"`       // Optional: Updated context output
	HasLoop             *bool                 `json:"has_loop,omitempty"`             // Optional: Updated has_loop (use pointer to distinguish unset from false)
	LoopCondition       string                `json:"loop_condition,omitempty"`       // Optional: Updated loop condition
	MaxIterations       *int                  `json:"max_iterations,omitempty"`       // Optional: Updated max iterations (use pointer to distinguish unset from 0)
	LoopDescription     string                `json:"loop_description,omitempty"`     // Optional: Updated loop description
}

// planFileMutex ensures thread-safe access to plan.json
var planFileMutex sync.Mutex

// getUpdatePlanStepsSchema returns the JSON schema for update_plan_steps tool
func getUpdatePlanStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"updated_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"existing_step_title": {
							"type": "string",
							"description": "REQUIRED: The exact title of the step in the existing plan that you want to update. Must match exactly (case-sensitive, preserve whitespace)."
						},
						"title": {
							"type": "string",
							"description": "OPTIONAL: New title for the step. Only include if you want to rename the step. If omitted, the existing title is preserved."
						},
						"description": {
							"type": "string",
							"description": "OPTIONAL: Updated description. Only include if you want to change the description. If omitted, the existing description is preserved."
						},
						"success_criteria": {
							"type": "string",
							"description": "OPTIONAL: Updated success criteria. Only include if you want to change it. If omitted, the existing success criteria is preserved."
						},
						"context_dependencies": {
							"type": "array",
							"items": { "type": "string" },
							"description": "OPTIONAL: Updated context dependencies. Only include if you want to change them. If omitted, the existing context dependencies are preserved."
						},
						"context_output": {
							"type": "string",
							"description": "OPTIONAL: Updated context output. Only include if you want to change it. If omitted, the existing context output is preserved."
						},
						"has_loop": {
							"type": "boolean",
							"description": "OPTIONAL: Updated has_loop flag. Only include if you want to change it. If omitted, the existing has_loop value is preserved."
						},
						"loop_condition": {
							"type": "string",
							"description": "OPTIONAL: Updated loop condition. Only include if you want to change it. If omitted, the existing loop condition is preserved."
						},
						"max_iterations": {
							"type": "integer",
							"description": "OPTIONAL: Updated max iterations. Only include if you want to change it. If omitted, the existing max iterations is preserved."
						},
						"loop_description": {
							"type": "string",
							"description": "OPTIONAL: Updated loop description. Only include if you want to change it. If omitted, the existing loop description is preserved."
						}
					},
					"required": ["existing_step_title"]
				},
				"description": "Steps to update. For each step, provide existing_step_title (required) to identify which step to update, and only include the fields you want to change."
			}
		},
		"required": ["updated_steps"]
	}`
}

// getDeletePlanStepsSchema returns the JSON schema for delete_plan_steps tool
func getDeletePlanStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"deleted_step_titles": {
				"type": "array",
				"items": { "type": "string" },
				"description": "Titles of steps to delete from the plan. Must match existing plan titles exactly (case-sensitive, preserve whitespace)."
			}
		},
		"required": ["deleted_step_titles"]
	}`
}

// getAddPlanStepsSchema returns the JSON schema for add_plan_steps tool
func getAddPlanStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"new_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"title": {
							"type": "string",
							"description": "Short, clear title for the new step"
						},
						"description": {
							"type": "string",
							"description": "COMPREHENSIVE, DETAILED description of what this step accomplishes. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context."
						},
						"success_criteria": {
							"type": "string",
							"description": "Detailed explanation of how to verify this step was completed successfully - be specific and comprehensive"
						},
						"context_dependencies": {
							"type": "array",
							"items": { "type": "string" },
							"description": "List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
						},
						"context_output": {
							"type": "string",
							"description": "What context file this step will create for subsequent steps - e.g., 'step_1_results.md'. IMPORTANT: Execution agents work ONLY in the execution folder, so context output files will be written to the execution folder."
						},
						"has_loop": {
							"type": "boolean",
							"description": "Whether this step needs to loop until condition is met. Set to true when step requires polling, retrying, or waiting for external systems."
						},
						"loop_condition": {
							"type": "string",
							"description": "Condition that must be met to exit the loop (REQUIRED when has_loop is true). This should be the same as success_criteria."
						},
						"max_iterations": {
							"type": "integer",
							"description": "Maximum number of loop iterations allowed (default: 10). Only include when has_loop is true."
						},
						"loop_description": {
							"type": "string",
							"description": "Describe what happens in EACH ITERATION of the loop. Only include when has_loop is true."
						},
						"insert_after_step_title": {
							"type": "string",
							"description": "REQUIRED: The exact title of the step to insert after. Must match exactly (case-sensitive, preserve whitespace). Use empty string \"\" to insert at the beginning of the plan (before the first step)."
						}
					},
					"required": ["title", "description", "success_criteria", "has_loop", "insert_after_step_title"]
				},
				"description": "New steps to add to the plan. Provide complete step definitions with all required fields. Each step must specify insert_after_step_title to indicate where to insert it in the plan."
			}
		},
		"required": ["new_steps"]
	}`
}

// readPlanFromFile reads plan.json from the workspace using BaseOrchestrator's ReadWorkspaceFile
func readPlanFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*PlanningResponse, error) {
	planPath := filepath.Join(workspacePath, "planning", "plan.json")

	planFileMutex.Lock()
	defer planFileMutex.Unlock()

	content, err := readFile(ctx, planPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan.json: %w", err)
	}

	var plan PlanningResponse
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan.json: %w", err)
	}

	return &plan, nil
}

// writePlanToFile writes PlanningResponse to plan.json in the workspace using BaseOrchestrator's WriteWorkspaceFile
// Creates a backup of the existing plan.json before writing (if it exists)
func writePlanToFile(ctx context.Context, workspacePath string, plan *PlanningResponse, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger utils.ExtendedLogger) error {
	planPath := filepath.Join(workspacePath, "planning", "plan.json")
	backupPath := filepath.Join(workspacePath, "planning", "plan_backup.json")

	planFileMutex.Lock()
	defer planFileMutex.Unlock()

	// Create backup of existing plan.json before updating (if it exists)
	existingPlanContent, err := readFile(ctx, planPath)
	if err == nil {
		// Existing plan.json found - create backup
		if err := writeFile(ctx, backupPath, existingPlanContent); err != nil {
			if logger != nil {
				logger.Warnf("⚠️ Failed to create plan backup: %v (continuing anyway)", err)
			}
		} else {
			if logger != nil {
				logger.Infof("💾 Created backup of existing plan.json at %s", backupPath)
			}
		}
	}
	// If readFile fails, it means plan.json doesn't exist yet (first time creation) - no backup needed

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	if err := writeFile(ctx, planPath, string(data)); err != nil {
		return fmt.Errorf("failed to write plan.json: %w", err)
	}

	return nil
}

// mergePartialStepUpdate merges a PartialPlanStep update into an existing PlanStep
func mergePartialStepUpdate(existingStep PlanStep, partialUpdate PartialPlanStep) PlanStep {
	merged := existingStep

	// Update fields only if they are provided (not zero values)
	if partialUpdate.Title != "" {
		merged.Title = partialUpdate.Title
	}
	if partialUpdate.Description != "" {
		merged.Description = partialUpdate.Description
	}
	if partialUpdate.SuccessCriteria != "" {
		merged.SuccessCriteria = partialUpdate.SuccessCriteria
	}
	if partialUpdate.ContextDependencies != nil {
		merged.ContextDependencies = partialUpdate.ContextDependencies
	}
	if partialUpdate.ContextOutput != "" {
		merged.ContextOutput = partialUpdate.ContextOutput
	}
	if partialUpdate.HasLoop != nil {
		merged.HasLoop = *partialUpdate.HasLoop
	}
	if partialUpdate.LoopCondition != "" {
		merged.LoopCondition = partialUpdate.LoopCondition
	}
	if partialUpdate.MaxIterations != nil {
		merged.MaxIterations = *partialUpdate.MaxIterations
	}
	if partialUpdate.LoopDescription != "" {
		merged.LoopDescription = partialUpdate.LoopDescription
	}

	return merged
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
// Returns PlanningResponse for CREATE mode
// Note: For UPDATE mode, use ExecuteStructuredUpdate instead
func (hctppa *HumanControlledTodoPlannerPlanningAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent, userMessage string) (*PlanningResponse, []llmtypes.MessageContent, error) {

	// CREATE mode: Use existing PlanningResponse schema
	// Define the JSON schema for plan generation (CREATE mode)
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
			},
			"run_mode": {
				"type": "string",
				"enum": ["use_same_run", "create_new_runs_always", "create_new_run_once_daily"],
				"description": "Run mode for execution: 'use_same_run' (reuse latest run folder), 'create_new_runs_always' (always create new), 'create_new_run_once_daily' (one per day). Default: 'use_same_run'"
			}
		},
		"required": ["steps"]
	}`

	// Generate system prompt using the processor
	systemPrompt := planningSystemPromptProcessor(templateVars)

	// Create a simple input processor that just returns the user message
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

// createUpdatePlanStepsExecutor creates an executor function for update_plan_steps tool
func createUpdatePlanStepsExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract updated_steps from args
		updatedStepsRaw, ok := args["updated_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid updated_steps argument")
		}

		// Convert to JSON and unmarshal to PartialPlanStep array
		updatedStepsJSON, err := json.Marshal(updatedStepsRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal updated_steps: %w", err)
		}

		var partialUpdates []PartialPlanStep
		if err := json.Unmarshal(updatedStepsJSON, &partialUpdates); err != nil {
			return "", fmt.Errorf("failed to parse updated_steps: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Create map of existing steps by title
		existingStepsMap := make(map[string]*PlanStep)
		for i := range plan.Steps {
			existingStepsMap[plan.Steps[i].Title] = &plan.Steps[i]
		}

		// Apply updates
		for _, partialUpdate := range partialUpdates {
			existingStep, exists := existingStepsMap[partialUpdate.ExistingStepTitle]
			if !exists {
				// Build list of available step titles for better error message
				availableTitles := make([]string, 0, len(plan.Steps))
				for _, step := range plan.Steps {
					availableTitles = append(availableTitles, step.Title)
				}
				return "", fmt.Errorf("step title '%s' not found in existing plan. Available step titles: %v", partialUpdate.ExistingStepTitle, availableTitles)
			}

			// Merge partial update
			*existingStep = mergePartialStepUpdate(*existingStep, partialUpdate)
		}

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Updated %d steps in plan", len(partialUpdates))
		return fmt.Sprintf("Successfully updated %d step(s) in the plan", len(partialUpdates)), nil
	}
}

// createDeletePlanStepsExecutor creates an executor function for delete_plan_steps tool
func createDeletePlanStepsExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract deleted_step_titles from args
		deletedTitlesRaw, ok := args["deleted_step_titles"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid deleted_step_titles argument")
		}

		// Convert to string array
		deletedTitles := make([]string, 0, len(deletedTitlesRaw))
		for _, title := range deletedTitlesRaw {
			if titleStr, ok := title.(string); ok {
				deletedTitles = append(deletedTitles, titleStr)
			} else {
				return "", fmt.Errorf("invalid step title in deleted_step_titles: %v", title)
			}
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Create set of deleted step titles
		deletedSet := make(map[string]bool)
		for _, title := range deletedTitles {
			deletedSet[title] = true
		}

		// Validate that all deleted steps exist
		existingStepsMap := make(map[string]bool)
		for _, step := range plan.Steps {
			existingStepsMap[step.Title] = true
		}
		for _, title := range deletedTitles {
			if !existingStepsMap[title] {
				// Build list of available step titles for better error message
				availableTitles := make([]string, 0, len(plan.Steps))
				for _, step := range plan.Steps {
					availableTitles = append(availableTitles, step.Title)
				}
				return "", fmt.Errorf("step title '%s' not found in existing plan (cannot delete). Available step titles: %v", title, availableTitles)
			}
		}

		// Filter out deleted steps
		filteredSteps := make([]PlanStep, 0, len(plan.Steps))
		for _, step := range plan.Steps {
			if !deletedSet[step.Title] {
				filteredSteps = append(filteredSteps, step)
			}
		}

		plan.Steps = filteredSteps

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Deleted %d steps from plan", len(deletedTitles))
		return fmt.Sprintf("Successfully deleted %d step(s) from the plan", len(deletedTitles)), nil
	}
}

// createAddPlanStepsExecutor creates an executor function for add_plan_steps tool
func createAddPlanStepsExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract new_steps from args
		newStepsRaw, ok := args["new_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid new_steps argument")
		}

		// Convert to JSON and unmarshal to AddPlanStep array
		newStepsJSON, err := json.Marshal(newStepsRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal new_steps: %w", err)
		}

		var addSteps []AddPlanStep
		if err := json.Unmarshal(newStepsJSON, &addSteps); err != nil {
			return "", fmt.Errorf("failed to parse new_steps: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Create map of step titles to indices for quick lookup
		titleToIndex := make(map[string]int)
		for i, step := range plan.Steps {
			titleToIndex[step.Title] = i
		}

		// Track which positions we need to insert at (grouped by insertion point)
		insertionPoints := make(map[int][]PlanStep) // original index -> steps to insert after this index

		for _, addStep := range addSteps {
			var afterIndex int
			var found bool

			if addStep.InsertAfterStepTitle == "" {
				// Insert at beginning (before index 0, so afterIndex = -1)
				afterIndex = -1
				found = true
			} else {
				// Find the step to insert after
				afterIndex, found = titleToIndex[addStep.InsertAfterStepTitle]
				if !found {
					// Build list of available step titles for better error message
					availableTitles := make([]string, 0, len(plan.Steps))
					for _, step := range plan.Steps {
						availableTitles = append(availableTitles, step.Title)
					}
					return "", fmt.Errorf("step title '%s' not found in existing plan (cannot insert after it). Available step titles: %v", addStep.InsertAfterStepTitle, availableTitles)
				}
			}

			// Add to insertion points map (key is the index to insert after)
			insertionPoints[afterIndex] = append(insertionPoints[afterIndex], addStep.PlanStep)
		}

		// Build new plan with insertions
		// Iterate through original steps and insert new steps at the right positions
		newPlanSteps := make([]PlanStep, 0, len(plan.Steps)+len(addSteps))

		// First, handle insertion at the beginning (afterIndex = -1)
		if stepsToInsert, hasInsertion := insertionPoints[-1]; hasInsertion {
			newPlanSteps = append(newPlanSteps, stepsToInsert...)
		}

		// Then iterate through original steps
		for i, originalStep := range plan.Steps {
			// Add the original step
			newPlanSteps = append(newPlanSteps, originalStep)

			// Insert any steps that should go after this step
			if stepsToInsert, hasInsertion := insertionPoints[i]; hasInsertion {
				newPlanSteps = append(newPlanSteps, stepsToInsert...)
			}
		}

		plan.Steps = newPlanSteps

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Added %d new steps to plan", len(addSteps))
		return fmt.Sprintf("Successfully added %d new step(s) to the plan", len(addSteps)), nil
	}
}

// extractToolCallsFromMessages scans messages for tool calls and returns the tool names that were called
func extractToolCallsFromMessages(messages []llmtypes.MessageContent) []string {
	toolNames := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != llmtypes.ChatMessageTypeAI {
			continue
		}
		for _, part := range msg.Parts {
			if toolCall, ok := part.(llmtypes.ToolCall); ok {
				if toolCall.FunctionCall != nil {
					toolNames[toolCall.FunctionCall.Name] = true
				}
			}
		}
	}
	result := make([]string, 0, len(toolNames))
	for name := range toolNames {
		result = append(result, name)
	}
	return result
}

// ExecuteStructuredUpdate executes the planning agent in UPDATE mode using 3 custom tools that directly update plan.json
// readFile and writeFile are BaseOrchestrator's ReadWorkspaceFile and WriteWorkspaceFile methods
func (hctppa *HumanControlledTodoPlannerPlanningAgent) ExecuteStructuredUpdate(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent, userMessage string, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) (*PlanningResponse, []llmtypes.MessageContent, error) {
	// Get workspace path from template vars
	workspacePath := templateVars["WorkspacePath"]
	if workspacePath == "" {
		return nil, nil, fmt.Errorf("WorkspacePath not found in template vars")
	}

	// Get the underlying MCP agent
	baseAgent := hctppa.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil {
		return nil, nil, fmt.Errorf("base agent is not initialized")
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, nil, fmt.Errorf("MCP agent is not initialized")
	}

	// Parse schemas and register the 3 custom tools
	updateSchema := getUpdatePlanStepsSchema()
	updateParams, err := parseSchemaForToolParameters(updateSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse update schema: %w", err)
	}
	// Get logger from MCP agent (it has a Logger field)
	logger := mcpAgent.Logger

	mcpAgent.RegisterCustomTool(
		"update_plan_steps",
		"Update existing steps in the plan. Provide existing_step_title (required) to identify which step to update, and only include the fields you want to change. The plan.json file is updated immediately when this tool is called.",
		updateParams,
		createUpdatePlanStepsExecutor(workspacePath, logger, readFile, writeFile),
	)

	deleteSchema := getDeletePlanStepsSchema()
	deleteParams, err := parseSchemaForToolParameters(deleteSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse delete schema: %w", err)
	}
	mcpAgent.RegisterCustomTool(
		"delete_plan_steps",
		"Delete steps from the plan by providing their exact titles. The plan.json file is updated immediately when this tool is called.",
		deleteParams,
		createDeletePlanStepsExecutor(workspacePath, logger, readFile, writeFile),
	)

	addSchema := getAddPlanStepsSchema()
	addParams, err := parseSchemaForToolParameters(addSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse add schema: %w", err)
	}
	mcpAgent.RegisterCustomTool(
		"add_plan_steps",
		"Add new steps to the plan. Provide complete step definitions with all required fields (title, description, success_criteria, has_loop, insert_after_step_title). CRITICAL: Each step MUST specify insert_after_step_title (REQUIRED) to indicate where to insert it. Use the exact title of the step to insert after, or empty string \"\" to insert at the beginning. The plan.json file is updated immediately when this tool is called.",
		addParams,
		createAddPlanStepsExecutor(workspacePath, logger, readFile, writeFile),
	)

	// Generate system prompt for update mode
	systemPrompt := planningSystemPromptProcessorForUpdate(templateVars)

	// Execute the agent with normal Execute (not StructuredOutputViaTool)
	textResponse, updatedHistory, err := baseAgent.Execute(ctx, userMessage, conversationHistory, systemPrompt, false)
	if err != nil {
		return nil, updatedHistory, fmt.Errorf("agent execution failed: %w", err)
	}

	// Check if any of our custom tools were called
	toolCalls := extractToolCallsFromMessages(updatedHistory)
	toolCalled := false
	for _, toolName := range toolCalls {
		if toolName == "update_plan_steps" || toolName == "delete_plan_steps" || toolName == "add_plan_steps" {
			toolCalled = true
			break
		}
	}

	// If no tools were called, this is a conversational response
	if !toolCalled {
		// Return NonStructuredResponseError so controller can handle it
		return nil, updatedHistory, &agents.NonStructuredResponseError{
			TextResponse:   textResponse,
			UpdatedHistory: updatedHistory,
			OriginalError:  fmt.Errorf("conversational response detected - no plan update tools were called"),
		}
	}

	// Tools were called - read the updated plan.json and return it
	updatedPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		return nil, updatedHistory, fmt.Errorf("failed to read updated plan: %w", err)
	}

	return updatedPlan, updatedHistory, nil
}

// parseSchemaForToolParameters parses a JSON schema string and extracts properties for tool parameters
// This is a local copy of the function from mcpagent to avoid circular dependencies
func parseSchemaForToolParameters(schemaString string) (map[string]interface{}, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(schemaString), &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Extract properties - this becomes the tool parameters
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("schema missing 'properties' field or it's not an object")
	}

	// Build tool parameter schema with type "object"
	toolParams := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}

	// Add required fields if present
	if required, ok := schema["required"].([]interface{}); ok {
		toolParams["required"] = required
	}

	return toolParams, nil
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
- **CRITICAL**: Your only output method is calling the 'submit_planning_response' tool with structured JSON data

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

## 📤 OUTPUT REQUIREMENTS

**CRITICAL**: 
- Call submit_planning_response tool with structured JSON data when plan is complete
- Output is ONLY via the submit_planning_response tool
- Do NOT include success_patterns/failure_patterns, or add markdown formatting - just pure JSON
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
- **Task**: Update existing plan based on human feedback
- **Tools**: Use update_plan_steps, delete_plan_steps, and add_plan_steps tools to modify the plan. These tools update plan.json immediately when called.

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

Update this plan based on human feedback. Use judgment to determine what changes address the feedback.

{{.ExistingPlanJSON}}

## 🎯 UPDATE GUIDELINES

**Principles**:
- Interpret feedback and make logical changes (minor = targeted, substantial = comprehensive)
- Update related parts to maintain consistency
- Preserve variable placeholders ({{"{{"}}VARIABLE_NAME{{"}}"}}) exactly as-is
- Keep same detail level in all steps

**Available Tools**:
- **update_plan_steps**: Update existing steps. Provide existing_step_title (REQUIRED) to identify which step to update, and only include the fields you want to change. Other fields preserve existing values. To rename, include both existing_step_title and new title. The plan.json file is updated immediately when this tool is called.
- **delete_plan_steps**: Delete steps from the plan by providing their exact titles. Must match existing titles exactly (case-sensitive, preserve whitespace). The plan.json file is updated immediately when this tool is called.
- **add_plan_steps**: Add new steps to the plan. Provide complete step definitions with all required fields (title, description, success_criteria, has_loop, insert_after_step_title). **CRITICAL**: Each new step MUST specify insert_after_step_title (REQUIRED) to indicate where to insert it. Use the exact title of the step to insert after (case-sensitive, preserve whitespace). Use empty string "" to insert at the beginning of the plan. Multiple steps with the same insert_after_step_title will be inserted in the order they appear in the array. The plan.json file is updated immediately when this tool is called.

**Guidelines**:
- You can call multiple tools in one turn if needed
- Tools update plan.json immediately - no merging needed
- Unchanged steps are preserved automatically
- A step cannot be both updated and deleted

## 🤖 MULTI-AGENT COORDINATION

- Execution agents work in {{.ExecutionWorkspacePath}} folder only
- Update context dependencies/outputs when restructuring steps
- Use relative paths only (no absolute paths)

## 📤 OUTPUT REQUIREMENTS

**Call tools when**: User wants plan changes (modify/delete/add steps). Use update_plan_steps, delete_plan_steps, or add_plan_steps as appropriate. You can call multiple tools in one turn.

**Respond conversationally when**: User asks questions, seeks clarification, or provides feedback that doesn't require plan changes. In this case, don't call any tools - just respond with text.
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
