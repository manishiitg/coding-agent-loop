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

	"llm-providers/llmtypes"
	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
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
	ID                       string                `json:"id"` // Stable step ID (generated from title) - required
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
	// Conditional branching fields
	HasCondition      bool       `json:"has_condition"`                // true if step has conditional branches
	ConditionQuestion string     `json:"condition_question,omitempty"` // question to ask ConditionalLLM
	ConditionContext  string     `json:"condition_context,omitempty"`  // context to provide to ConditionalLLM
	IfTrueSteps       []PlanStep `json:"if_true_steps,omitempty"`      // nested steps for true branch
	IfFalseSteps      []PlanStep `json:"if_false_steps,omitempty"`     // nested steps for false branch
	ConditionResult   *bool      `json:"condition_result,omitempty"`   // runtime: stores decision result
	ConditionReason   string     `json:"condition_reason,omitempty"`   // runtime: stores LLM reasoning
	// AgentConfigs removed - now stored separately in step_config.json
}

// AddPlanStep represents a step to be added with insertion position
type AddPlanStep struct {
	PlanStep
	InsertAfterStepID string `json:"insert_after_step_id"` // REQUIRED: ID of step to insert after (use empty string "" to insert at beginning)
}

// PlanningResponse represents the structured response from planning
type PlanningResponse struct {
	Steps []PlanStep `json:"steps"`
}

// PartialPlanStep represents a partial update to a plan step (used only in tool schemas)
type PartialPlanStep struct {
	ExistingStepID      string                `json:"existing_step_id"`               // Required: ID of existing step to update
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
						"existing_step_id": {
							"type": "string",
							"description": "REQUIRED: The ID of the step in the existing plan that you want to update. Use the step's id field from the plan."
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
					"required": ["existing_step_id"]
				},
				"description": "Steps to update. For each step, provide existing_step_id (required) to identify which step to update, and only include the fields you want to change."
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
			"deleted_step_ids": {
				"type": "array",
				"items": { "type": "string" },
				"description": "IDs of steps to delete from the plan. Use the step's id field from the plan."
			}
		},
		"required": ["deleted_step_ids"]
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
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this new step. Generate a unique, URL-friendly ID based on the step title (e.g., 'deploy-application' from 'Deploy Application')."
						},
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
						"insert_after_step_id": {
							"type": "string",
							"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
						}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop", "insert_after_step_id"]
				},
				"description": "New steps to add to the plan. Provide complete step definitions with all required fields. Each step must specify insert_after_step_id to indicate where to insert it in the plan."
			}
		},
		"required": ["new_steps"]
	}`
}

// getConvertStepToConditionalSchema returns the JSON schema for convert_step_to_conditional tool
func getConvertStepToConditionalSchema() string {
	return `{
		"type": "object",
		"properties": {
			"step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to convert to conditional. Use the step's id field from the plan."
			},
			"condition_question": {
				"type": "string",
				"description": "REQUIRED: Question to ask the ConditionalLLM for decision making (e.g., 'Is the deployment healthy?')"
			},
			"condition_context": {
				"type": "string",
				"description": "OPTIONAL: Context to provide to ConditionalLLM (e.g., context files, status information). Can be empty string if not needed."
			},
			"if_true_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this branch step. Generate a unique, URL-friendly ID based on the step title (e.g., 'verify-deployment-health' from 'Verify Deployment Health')."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"},
						"loop_condition": {"type": "string"},
						"max_iterations": {"type": "integer"},
						"loop_description": {"type": "string"},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is true. Can be empty array [] if no true branch steps. Each step MUST include an 'id' field."
			},
			"if_false_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this branch step. Generate a unique, URL-friendly ID based on the step title (e.g., 'rollback-deployment' from 'Rollback Deployment')."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"},
						"loop_condition": {"type": "string"},
						"max_iterations": {"type": "integer"},
						"loop_description": {"type": "string"},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is false. Can be empty array [] if no false branch steps. Each step MUST include an 'id' field."
			}
		},
		"required": ["step_id", "condition_question", "if_true_steps", "if_false_steps"]
	}`
}

// getAddBranchStepsSchema returns the JSON schema for add_branch_steps tool
func getAddBranchStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step (parent step). Use the step's id field from the plan."
			},
			"branch_type": {
				"type": "string",
				"enum": ["if_true", "if_false"],
				"description": "REQUIRED: Which branch to add steps to - 'if_true' or 'if_false'"
			},
			"new_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this branch step. Generate a unique, URL-friendly ID based on the step title (e.g., 'verify-deployment-health' from 'Verify Deployment Health')."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"},
						"loop_condition": {"type": "string"},
						"max_iterations": {"type": "integer"},
						"loop_description": {"type": "string"},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				},
				"description": "REQUIRED: New steps to add to the specified branch. Provide complete step definitions with IDs."
			}
		},
		"required": ["parent_step_id", "branch_type", "new_steps"]
	}`
}

// getUpdateBranchStepsSchema returns the JSON schema for update_branch_steps tool
func getUpdateBranchStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step (parent step). Use the step's id field from the plan."
			},
			"branch_type": {
				"type": "string",
				"enum": ["if_true", "if_false"],
				"description": "REQUIRED: Which branch to update - 'if_true' or 'if_false'"
			},
			"updated_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"existing_step_id": {
							"type": "string",
							"description": "REQUIRED: The ID of the step within the branch to update. Use the step's id field from the plan."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"},
						"loop_condition": {"type": "string"},
						"max_iterations": {"type": "integer"},
						"loop_description": {"type": "string"},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"}
					},
					"required": ["existing_step_id"]
				},
				"description": "REQUIRED: Steps to update within the branch. For each step, provide existing_step_id (required) and only include fields you want to change."
			}
		},
		"required": ["parent_step_id", "branch_type", "updated_steps"]
	}`
}

// getDeleteBranchStepsSchema returns the JSON schema for delete_branch_steps tool
func getDeleteBranchStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step (parent step). Use the step's id field from the plan."
			},
			"branch_type": {
				"type": "string",
				"enum": ["if_true", "if_false"],
				"description": "REQUIRED: Which branch to delete steps from - 'if_true' or 'if_false'"
			},
			"deleted_step_ids": {
				"type": "array",
				"items": {"type": "string"},
				"description": "REQUIRED: IDs of steps to delete from the branch. Use the step's id field from the plan."
			}
		},
		"required": ["parent_step_id", "branch_type", "deleted_step_ids"]
	}`
}

// getUpdateConditionalStepSchema returns the JSON schema for update_conditional_step tool
func getUpdateConditionalStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step to update. Use the step's id field from the plan."
			},
			"condition_question": {
				"type": "string",
				"description": "OPTIONAL: Updated condition question. Only include if you want to change it. If omitted, the existing question is preserved."
			},
			"condition_context": {
				"type": "string",
				"description": "OPTIONAL: Updated condition context. Only include if you want to change it. If omitted, the existing context is preserved."
			}
		},
		"required": ["step_id"]
	}`
}

// getConvertConditionalToRegularSchema returns the JSON schema for convert_conditional_to_regular tool
func getConvertConditionalToRegularSchema() string {
	return `{
		"type": "object",
		"properties": {
			"step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step to convert back to regular. Use the step's id field from the plan. This will remove all conditional properties and branch steps."
			}
		},
		"required": ["step_id"]
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
// Validates that all steps have IDs before saving (planning agent should always generate them)
func writePlanToFile(ctx context.Context, workspacePath string, plan *PlanningResponse, _ func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger utils.ExtendedLogger) error {
	planPath := filepath.Join(workspacePath, "planning", "plan.json")

	planFileMutex.Lock()
	defer planFileMutex.Unlock()

	// Validate that all steps have IDs (planning agent should always generate them)
	if err := validatePlanStepIDs(plan.Steps); err != nil {
		return fmt.Errorf("plan validation failed: %w", err)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	if err := writeFile(ctx, planPath, string(data)); err != nil {
		return fmt.Errorf("failed to write plan.json: %w", err)
	}

	return nil
}

// validateNestingDepth checks if the maximum nesting depth (2 levels) is exceeded
// Returns error if depth > 2, nil otherwise
func validateNestingDepth(step PlanStep, currentDepth int) error {
	const maxDepth = 2
	if currentDepth > maxDepth {
		return fmt.Errorf("nesting depth exceeds maximum allowed depth of %d (current: %d)", maxDepth, currentDepth)
	}

	// Check nested steps in branches
	if step.HasCondition {
		for _, branchStep := range step.IfTrueSteps {
			if branchStep.HasCondition {
				if err := validateNestingDepth(branchStep, currentDepth+1); err != nil {
					return err
				}
			}
		}
		for _, branchStep := range step.IfFalseSteps {
			if branchStep.HasCondition {
				if err := validateNestingDepth(branchStep, currentDepth+1); err != nil {
					return err
				}
			}
		}
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

		// Create map of existing steps by ID
		existingStepsMap := make(map[string]*PlanStep)
		for i := range plan.Steps {
			existingStepsMap[plan.Steps[i].ID] = &plan.Steps[i]
		}

		// Apply updates
		for _, partialUpdate := range partialUpdates {
			existingStep, exists := existingStepsMap[partialUpdate.ExistingStepID]
			if !exists {
				// Build list of available step IDs for better error message
				availableIDs := make([]string, 0, len(plan.Steps))
				for _, step := range plan.Steps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs)
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
		// Extract deleted_step_ids from args
		deletedIDsRaw, ok := args["deleted_step_ids"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid deleted_step_ids argument")
		}

		// Convert to string array
		deletedIDs := make([]string, 0, len(deletedIDsRaw))
		for _, id := range deletedIDsRaw {
			if idStr, ok := id.(string); ok {
				deletedIDs = append(deletedIDs, idStr)
			} else {
				return "", fmt.Errorf("invalid step ID in deleted_step_ids: %v", id)
			}
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Create set of deleted step IDs
		deletedSet := make(map[string]bool)
		for _, id := range deletedIDs {
			deletedSet[id] = true
		}

		// Validate that all deleted steps exist
		existingStepsMap := make(map[string]bool)
		for _, step := range plan.Steps {
			existingStepsMap[step.ID] = true
		}
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				// Build list of available step IDs for better error message
				availableIDs := make([]string, 0, len(plan.Steps))
				for _, step := range plan.Steps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf("step ID '%s' not found in existing plan (cannot delete). Available step IDs: %v", id, availableIDs)
			}
		}

		// Filter out deleted steps
		filteredSteps := make([]PlanStep, 0, len(plan.Steps))
		for _, step := range plan.Steps {
			if !deletedSet[step.ID] {
				filteredSteps = append(filteredSteps, step)
			}
		}

		plan.Steps = filteredSteps

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Deleted %d steps from plan", len(deletedIDs))
		return fmt.Sprintf("Successfully deleted %d step(s) from the plan", len(deletedIDs)), nil
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

		// Create map of step IDs to indices for quick lookup
		idToIndex := make(map[string]int)
		for i, step := range plan.Steps {
			idToIndex[step.ID] = i
		}

		// Track which positions we need to insert at (grouped by insertion point)
		insertionPoints := make(map[int][]PlanStep) // original index -> steps to insert after this index

		for i, addStep := range addSteps {
			// Validate that step has ID (LLM should always provide it)
			if addStep.PlanStep.ID == "" {
				return "", fmt.Errorf("step at index %d in new_steps is missing required ID field. Step title: %q", i, addStep.PlanStep.Title)
			}

			var afterIndex int
			var found bool

			if addStep.InsertAfterStepID == "" {
				// Insert at beginning (before index 0, so afterIndex = -1)
				afterIndex = -1
				found = true
			} else {
				// Find the step to insert after
				afterIndex, found = idToIndex[addStep.InsertAfterStepID]
				if !found {
					// Build list of available step IDs for better error message
					availableIDs := make([]string, 0, len(plan.Steps))
					for _, step := range plan.Steps {
						availableIDs = append(availableIDs, step.ID)
					}
					return "", fmt.Errorf("step ID '%s' not found in existing plan (cannot insert after it). Available step IDs: %v", addStep.InsertAfterStepID, availableIDs)
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

	// Register human_feedback tool first (required before making plan changes)
	humanTools := virtualtools.CreateHumanTools()
	humanToolExecutors := virtualtools.CreateHumanToolExecutors()
	if len(humanTools) > 0 && len(humanToolExecutors) > 0 {
		humanTool := humanTools[0] // Get the first (and only) human tool
		if humanTool.Function != nil {
			// Convert Parameters to map[string]interface{}
			var params map[string]interface{}
			if humanTool.Function.Parameters != nil {
				paramsBytes, err := json.Marshal(humanTool.Function.Parameters)
				if err == nil {
					json.Unmarshal(paramsBytes, &params)
				}
			}
			if params != nil {
				if executor, exists := humanToolExecutors[humanTool.Function.Name]; exists {
					mcpAgent.RegisterCustomTool(
						humanTool.Function.Name,
						humanTool.Function.Description,
						params,
						executor,
					)
					logger.Infof("✅ Registered human_feedback tool for planning agent")
				}
			}
		}
	}

	mcpAgent.RegisterCustomTool(
		"update_plan_steps",
		"Update existing steps in the plan. Provide existing_step_id (required) to identify which step to update, and only include the fields you want to change. The plan.json file is updated immediately when this tool is called.",
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
		"Delete steps from the plan by providing their IDs. Use the step's id field from the plan. The plan.json file is updated immediately when this tool is called.",
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
		"Add new steps to the plan. Provide complete step definitions with all required fields (title, description, success_criteria, has_loop, insert_after_step_id). CRITICAL: Each step MUST specify insert_after_step_id (REQUIRED) to indicate where to insert it. Use the step's id field from the plan, or empty string \"\" to insert at the beginning. The plan.json file is updated immediately when this tool is called.",
		addParams,
		createAddPlanStepsExecutor(workspacePath, logger, readFile, writeFile),
	)

	// Register conditional step tools
	convertToConditionalSchema := getConvertStepToConditionalSchema()
	convertToConditionalParams, err := parseSchemaForToolParameters(convertToConditionalSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse convert_step_to_conditional schema: %w", err)
	}
	mcpAgent.RegisterCustomTool(
		"convert_step_to_conditional",
		"Convert a regular step to a conditional step with if/else branches. Provide step_id, condition_question, condition_context (optional), if_true_steps, and if_false_steps. The step will become a conditional decision point that executes one branch based on the condition evaluation.",
		convertToConditionalParams,
		createConvertStepToConditionalExecutor(workspacePath, logger, readFile, writeFile),
	)

	addBranchStepsSchema := getAddBranchStepsSchema()
	addBranchStepsParams, err := parseSchemaForToolParameters(addBranchStepsSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse add_branch_steps schema: %w", err)
	}
	mcpAgent.RegisterCustomTool(
		"add_branch_steps",
		"Add new steps to a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type ('if_true' or 'if_false'), and new_steps array. The steps will be appended to the specified branch.",
		addBranchStepsParams,
		createAddBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
	)

	updateBranchStepsSchema := getUpdateBranchStepsSchema()
	updateBranchStepsParams, err := parseSchemaForToolParameters(updateBranchStepsSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse update_branch_steps schema: %w", err)
	}
	mcpAgent.RegisterCustomTool(
		"update_branch_steps",
		"Update existing steps within a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type, and updated_steps array. For each step, provide existing_step_id (required) and only include fields you want to change.",
		updateBranchStepsParams,
		createUpdateBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
	)

	deleteBranchStepsSchema := getDeleteBranchStepsSchema()
	deleteBranchStepsParams, err := parseSchemaForToolParameters(deleteBranchStepsSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse delete_branch_steps schema: %w", err)
	}
	mcpAgent.RegisterCustomTool(
		"delete_branch_steps",
		"Delete steps from a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type, and deleted_step_ids array. Use the step's id field from the plan.",
		deleteBranchStepsParams,
		createDeleteBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
	)

	updateConditionalStepSchema := getUpdateConditionalStepSchema()
	updateConditionalStepParams, err := parseSchemaForToolParameters(updateConditionalStepSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse update_conditional_step schema: %w", err)
	}
	mcpAgent.RegisterCustomTool(
		"update_conditional_step",
		"Update the condition question or context of a conditional step without modifying its branches. Provide step_id and optionally condition_question and/or condition_context. Only provided fields will be updated.",
		updateConditionalStepParams,
		createUpdateConditionalStepExecutor(workspacePath, logger, readFile, writeFile),
	)

	convertToRegularSchema := getConvertConditionalToRegularSchema()
	convertToRegularParams, err := parseSchemaForToolParameters(convertToRegularSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse convert_conditional_to_regular schema: %w", err)
	}
	mcpAgent.RegisterCustomTool(
		"convert_conditional_to_regular",
		"Convert a conditional step back to a regular step. This removes all conditional properties and branch steps. Provide step_id of the conditional step to convert.",
		convertToRegularParams,
		createConvertConditionalToRegularExecutor(workspacePath, logger, readFile, writeFile),
	)

	// Generate system prompt for update mode
	systemPrompt := planningSystemPromptProcessorForUpdate(templateVars)

	// Execute the agent with normal Execute (not StructuredOutputViaTool)
	_, updatedHistory, err := baseAgent.Execute(ctx, userMessage, conversationHistory, systemPrompt, false)
	if err != nil {
		return nil, updatedHistory, fmt.Errorf("agent execution failed: %w", err)
	}

	// Check if any of our custom tools were called
	toolCalls := extractToolCallsFromMessages(updatedHistory)
	planUpdateToolCalled := false
	for _, toolName := range toolCalls {
		if toolName == "update_plan_steps" || toolName == "delete_plan_steps" || toolName == "add_plan_steps" ||
			toolName == "convert_step_to_conditional" || toolName == "add_branch_steps" || toolName == "update_branch_steps" ||
			toolName == "delete_branch_steps" || toolName == "update_conditional_step" || toolName == "convert_conditional_to_regular" {
			planUpdateToolCalled = true
		}
	}

	// Read the current plan.json (whether tools were called or not)
	// In UPDATE mode, conversational responses are normal - not an error
	// If tools were called, plan.json was updated. If not, we return the current plan unchanged.
	currentPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		return nil, updatedHistory, fmt.Errorf("failed to read plan: %w", err)
	}

	if !planUpdateToolCalled {
		// No tools called - this is a normal conversational response, not an error
		// Return the current plan (unchanged) so conversation can continue
		logger.Infof("📝 Planning agent in UPDATE mode: Conversational response (no plan changes). Returning current plan.")
		return currentPlan, updatedHistory, nil
	}

	// Tools were called - plan.json was updated
	logger.Infof("✅ Plan updated via tools (%d steps)", len(currentPlan.Steps))
	return currentPlan, updatedHistory, nil
}

// createConvertStepToConditionalExecutor creates an executor function for convert_step_to_conditional tool
func createConvertStepToConditionalExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, ok := args["step_id"].(string)
		if !ok || stepID == "" {
			return "", fmt.Errorf("invalid or missing step_id")
		}

		conditionQuestion, ok := args["condition_question"].(string)
		if !ok || conditionQuestion == "" {
			return "", fmt.Errorf("invalid or missing condition_question")
		}

		conditionContext, _ := args["condition_context"].(string) // Optional

		// Extract if_true_steps and if_false_steps
		ifTrueStepsRaw, ok := args["if_true_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid if_true_steps argument")
		}
		ifFalseStepsRaw, ok := args["if_false_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid if_false_steps argument")
		}

		// Convert to JSON and unmarshal to PlanStep arrays
		ifTrueStepsJSON, err := json.Marshal(ifTrueStepsRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal if_true_steps: %w", err)
		}
		var ifTrueSteps []PlanStep
		if err := json.Unmarshal(ifTrueStepsJSON, &ifTrueSteps); err != nil {
			return "", fmt.Errorf("failed to parse if_true_steps: %w", err)
		}

		ifFalseStepsJSON, err := json.Marshal(ifFalseStepsRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal if_false_steps: %w", err)
		}
		var ifFalseSteps []PlanStep
		if err := json.Unmarshal(ifFalseStepsJSON, &ifFalseSteps); err != nil {
			return "", fmt.Errorf("failed to parse if_false_steps: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the step to convert by ID
		var stepToConvert *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == stepID {
				stepToConvert = &plan.Steps[i]
				break
			}
		}
		if stepToConvert == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs)
		}

		// Validate nesting depth for branch steps (starting from depth 1 since this step becomes conditional)
		for _, branchStep := range ifTrueSteps {
			if err := validateNestingDepth(branchStep, 1); err != nil {
				return "", fmt.Errorf("if_true_steps validation failed: %w", err)
			}
		}
		for _, branchStep := range ifFalseSteps {
			if err := validateNestingDepth(branchStep, 1); err != nil {
				return "", fmt.Errorf("if_false_steps validation failed: %w", err)
			}
		}

		// Convert step to conditional
		stepToConvert.HasCondition = true
		stepToConvert.ConditionQuestion = conditionQuestion
		stepToConvert.ConditionContext = conditionContext
		stepToConvert.IfTrueSteps = ifTrueSteps
		stepToConvert.IfFalseSteps = ifFalseSteps

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Converted step '%s' to conditional with %d true branch steps and %d false branch steps", stepToConvert.Title, len(ifTrueSteps), len(ifFalseSteps))
		return fmt.Sprintf("Successfully converted step '%s' to conditional", stepToConvert.Title), nil
	}
}

// createAddBranchStepsExecutor creates an executor function for add_branch_steps tool
func createAddBranchStepsExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf("invalid or missing parent_step_id")
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf("invalid branch_type: must be 'if_true' or 'if_false'")
		}

		newStepsRaw, ok := args["new_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid new_steps argument")
		}

		// Convert to JSON and unmarshal to PlanStep array
		newStepsJSON, err := json.Marshal(newStepsRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal new_steps: %w", err)
		}
		var newSteps []PlanStep
		if err := json.Unmarshal(newStepsJSON, &newSteps); err != nil {
			return "", fmt.Errorf("failed to parse new_steps: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the parent conditional step by ID
		var parentStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == parentStepID {
				parentStep = &plan.Steps[i]
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		if !parentStep.HasCondition {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", parentStepID)
		}

		// Validate that all new branch steps have IDs (required for config matching)
		for i, newStep := range newSteps {
			if newStep.ID == "" {
				return "", fmt.Errorf("branch step at index %d is missing required ID field. Step title: %q", i, newStep.Title)
			}
		}

		// Validate nesting depth for new steps (starting from depth 1 since they're being added to a conditional)
		for _, newStep := range newSteps {
			if err := validateNestingDepth(newStep, 1); err != nil {
				return "", fmt.Errorf("new_steps validation failed: %w", err)
			}
		}

		// Add steps to the appropriate branch
		if branchType == "if_true" {
			parentStep.IfTrueSteps = append(parentStep.IfTrueSteps, newSteps...)
		} else {
			parentStep.IfFalseSteps = append(parentStep.IfFalseSteps, newSteps...)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Added %d steps to %s branch of conditional step '%s'", len(newSteps), branchType, parentStep.Title)
		return fmt.Sprintf("Successfully added %d step(s) to %s branch of conditional step '%s'", len(newSteps), branchType, parentStep.Title), nil
	}
}

// createUpdateBranchStepsExecutor creates an executor function for update_branch_steps tool
func createUpdateBranchStepsExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf("invalid or missing parent_step_id")
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf("invalid branch_type: must be 'if_true' or 'if_false'")
		}

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

		// Find the parent conditional step by ID
		var parentStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == parentStepID {
				parentStep = &plan.Steps[i]
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		if !parentStep.HasCondition {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", parentStepID)
		}

		// Get the appropriate branch
		var branchSteps *[]PlanStep
		if branchType == "if_true" {
			branchSteps = &parentStep.IfTrueSteps
		} else {
			branchSteps = &parentStep.IfFalseSteps
		}

		// Create map of existing branch steps by ID
		existingStepsMap := make(map[string]*PlanStep)
		for i := range *branchSteps {
			existingStepsMap[(*branchSteps)[i].ID] = &(*branchSteps)[i]
		}

		// Apply updates
		for _, partialUpdate := range partialUpdates {
			existingStep, exists := existingStepsMap[partialUpdate.ExistingStepID]
			if !exists {
				availableIDs := make([]string, 0, len(*branchSteps))
				for _, step := range *branchSteps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf("step ID '%s' not found in %s branch. Available step IDs: %v", partialUpdate.ExistingStepID, branchType, availableIDs)
			}

			// Merge partial update
			*existingStep = mergePartialStepUpdate(*existingStep, partialUpdate)

			// Validate nesting depth after update
			if err := validateNestingDepth(*existingStep, 1); err != nil {
				return "", fmt.Errorf("updated step validation failed: %w", err)
			}
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Updated %d steps in %s branch of conditional step '%s'", len(partialUpdates), branchType, parentStep.Title)
		return fmt.Sprintf("Successfully updated %d step(s) in %s branch of conditional step '%s'", len(partialUpdates), branchType, parentStep.Title), nil
	}
}

// createDeleteBranchStepsExecutor creates an executor function for delete_branch_steps tool
func createDeleteBranchStepsExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf("invalid or missing parent_step_id")
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf("invalid branch_type: must be 'if_true' or 'if_false'")
		}

		deletedIDsRaw, ok := args["deleted_step_ids"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid deleted_step_ids argument")
		}

		// Convert to string array
		deletedIDs := make([]string, 0, len(deletedIDsRaw))
		for _, id := range deletedIDsRaw {
			if idStr, ok := id.(string); ok {
				deletedIDs = append(deletedIDs, idStr)
			} else {
				return "", fmt.Errorf("invalid step ID in deleted_step_ids: %v", id)
			}
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the parent conditional step by ID
		var parentStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == parentStepID {
				parentStep = &plan.Steps[i]
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		if !parentStep.HasCondition {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", parentStepID)
		}

		// Get the appropriate branch
		var branchSteps *[]PlanStep
		if branchType == "if_true" {
			branchSteps = &parentStep.IfTrueSteps
		} else {
			branchSteps = &parentStep.IfFalseSteps
		}

		// Create set of deleted step IDs
		deletedSet := make(map[string]bool)
		for _, id := range deletedIDs {
			deletedSet[id] = true
		}

		// Validate that all deleted steps exist
		existingStepsMap := make(map[string]bool)
		for _, step := range *branchSteps {
			existingStepsMap[step.ID] = true
		}
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				availableIDs := make([]string, 0, len(*branchSteps))
				for _, step := range *branchSteps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf("step ID '%s' not found in %s branch (cannot delete). Available step IDs: %v", id, branchType, availableIDs)
			}
		}

		// Filter out deleted steps
		filteredSteps := make([]PlanStep, 0, len(*branchSteps))
		for _, step := range *branchSteps {
			if !deletedSet[step.ID] {
				filteredSteps = append(filteredSteps, step)
			}
		}

		*branchSteps = filteredSteps

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Deleted %d steps from %s branch of conditional step '%s'", len(deletedIDs), branchType, parentStep.Title)
		return fmt.Sprintf("Successfully deleted %d step(s) from %s branch of conditional step '%s'", len(deletedIDs), branchType, parentStep.Title), nil
	}
}

// createUpdateConditionalStepExecutor creates an executor function for update_conditional_step tool
func createUpdateConditionalStepExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, ok := args["step_id"].(string)
		if !ok || stepID == "" {
			return "", fmt.Errorf("invalid or missing step_id")
		}

		conditionQuestion, _ := args["condition_question"].(string) // Optional
		conditionContext, _ := args["condition_context"].(string)   // Optional

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the conditional step by ID
		var conditionalStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == stepID {
				conditionalStep = &plan.Steps[i]
				break
			}
		}
		if conditionalStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs)
		}

		if !conditionalStep.HasCondition {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", stepID)
		}

		// Update conditional properties (only if provided)
		if conditionQuestion != "" {
			conditionalStep.ConditionQuestion = conditionQuestion
		}
		if conditionContext != "" {
			conditionalStep.ConditionContext = conditionContext
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Updated conditional step '%s'", conditionalStep.Title)
		return fmt.Sprintf("Successfully updated conditional step '%s'", conditionalStep.Title), nil
	}
}

// createConvertConditionalToRegularExecutor creates an executor function for convert_conditional_to_regular tool
func createConvertConditionalToRegularExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, ok := args["step_id"].(string)
		if !ok || stepID == "" {
			return "", fmt.Errorf("invalid or missing step_id")
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the conditional step by ID
		var conditionalStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == stepID {
				conditionalStep = &plan.Steps[i]
				break
			}
		}
		if conditionalStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs)
		}

		if !conditionalStep.HasCondition {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", stepID)
		}

		// Convert back to regular step (remove conditional properties and branches)
		conditionalStep.HasCondition = false
		conditionalStep.ConditionQuestion = ""
		conditionalStep.ConditionContext = ""
		conditionalStep.IfTrueSteps = nil
		conditionalStep.IfFalseSteps = nil
		conditionalStep.ConditionResult = nil
		conditionalStep.ConditionReason = ""

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Infof("✅ Converted conditional step '%s' back to regular step", conditionalStep.Title)
		return fmt.Sprintf("Successfully converted conditional step '%s' back to regular step", conditionalStep.Title), nil
	}
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
- **Conditional Branching**: Use conditional steps (has_condition=true) when you need if/else logic. Set condition_question (question for ConditionalLLM to evaluate), condition_context (context to provide), if_true_steps (steps if condition is true), and if_false_steps (steps if condition is false). Maximum nesting depth is 2 levels (conditional step can contain conditional steps in branches, but no deeper).

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
- **Tools**: Use human_feedback tool to confirm changes, then use update_plan_steps, delete_plan_steps, and add_plan_steps tools to modify the plan. These tools update plan.json immediately when called.

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
- **human_feedback**: **REQUIRED BEFORE MAKING ANY PLAN CHANGES**. Use this tool to ask the user for confirmation before modifying the plan. Provide a clear message describing the proposed changes (what steps will be updated/deleted/added and why). Wait for user approval before proceeding with plan modification tools. Generate a unique UUID for the unique_id parameter.
- **update_plan_steps**: Update existing steps. Provide existing_step_id (REQUIRED) to identify which step to update, and only include the fields you want to change. Other fields preserve existing values. To rename, include both existing_step_id and new title. The plan.json file is updated immediately when this tool is called.
- **delete_plan_steps**: Delete steps from the plan by providing their IDs. Use the step's id field from the plan. The plan.json file is updated immediately when this tool is called.
- **add_plan_steps**: Add new steps to the plan. Provide complete step definitions with all required fields (title, description, success_criteria, has_loop, insert_after_step_id). **CRITICAL**: Each new step MUST specify insert_after_step_id (REQUIRED) to indicate where to insert it. Use the step's id field from the plan, or empty string "" to insert at the beginning of the plan. Multiple steps with the same insert_after_step_id will be inserted in the order they appear in the array. The plan.json file is updated immediately when this tool is called.

**Conditional Branching Tools** (for if/else logic):
- **convert_step_to_conditional**: Convert a regular step to a conditional step with if/else branches. Provide step_id, condition_question (question to ask ConditionalLLM), condition_context (optional), if_true_steps (steps to execute if condition is true), and if_false_steps (steps to execute if condition is false). Maximum nesting depth is 2 levels.
- **add_branch_steps**: Add new steps to a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type ('if_true' or 'if_false'), and new_steps array.
- **update_branch_steps**: Update existing steps within a specific branch of a conditional step. Provide parent_step_id, branch_type, and updated_steps array with existing_step_id (required) for each step to update.
- **delete_branch_steps**: Delete steps from a specific branch of a conditional step. Provide parent_step_id, branch_type, and deleted_step_ids array.
- **update_conditional_step**: Update the condition question or context of a conditional step without modifying its branches. Provide step_id and optionally condition_question and/or condition_context.
- **convert_conditional_to_regular**: Convert a conditional step back to a regular step. This removes all conditional properties and branch steps. Provide step_id of the conditional step.

**CRITICAL WORKFLOW - HUMAN CONFIRMATION REQUIRED**:
1. **ALWAYS use human_feedback tool FIRST** before making any plan changes (update/delete/add steps)
2. In the human_feedback message, clearly describe:
   - What changes you plan to make (which steps to update/delete/add)
   - Why these changes address the user's feedback
   - The impact of these changes
3. The human_feedback tool will automatically return the user's response. **After receiving the response**:
   - If user approved: Immediately proceed with update_plan_steps, delete_plan_steps, or add_plan_steps tools in the same conversation turn
   - If user asked questions or needs clarification: Respond conversationally without calling plan update tools
   - If user rejected or requested changes: Adjust your approach and either ask again with human_feedback or respond conversationally
4. You can call multiple plan modification tools in the same turn after getting approval

**Guidelines**:
- You can call multiple plan modification tools in one turn after getting approval
- Tools update plan.json immediately - no merging needed
- Unchanged steps are preserved automatically
- A step cannot be both updated and deleted

## 🤖 MULTI-AGENT COORDINATION

- Execution agents work in {{.ExecutionWorkspacePath}} folder only
- Update context dependencies/outputs when restructuring steps
- Use relative paths only (no absolute paths)

## 📤 OUTPUT REQUIREMENTS

**Workflow for plan changes**:
1. **First**: Use human_feedback tool to describe proposed changes and get user confirmation
2. **After human_feedback returns**: The tool automatically provides the user's response. Based on that response:
   - **If approved**: Immediately call update_plan_steps, delete_plan_steps, or add_plan_steps tools in the same conversation turn
   - **If questions/clarification needed**: Respond conversationally without calling plan update tools
   - **If rejected**: Adjust your approach and either ask again with human_feedback or respond conversationally
3. You can call multiple plan modification tools in the same turn after getting approval

**Respond conversationally when**: User asks questions, seeks clarification, or provides feedback that doesn't require plan changes. In this case, don't call any tools - just respond with text.

**IMPORTANT**: Never call update_plan_steps, delete_plan_steps, or add_plan_steps without first getting user confirmation via human_feedback tool. After human_feedback returns, you will automatically continue in the same turn and can make the plan changes.
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
