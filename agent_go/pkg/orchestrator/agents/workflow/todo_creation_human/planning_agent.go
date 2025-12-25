package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/r3labs/diff/v3"
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

// PlanOrchestrationRoute represents a possible route/sub-agent for planning
type PlanOrchestrationRoute struct {
	RouteID       string            `json:"route_id"`                  // Unique ID for this route
	RouteName     string            `json:"route_name"`                // Human-readable name
	Condition     string            `json:"condition"`                 // Condition description (e.g., "If error is authentication-related")
	SubAgentStep  PlanStepInterface `json:"sub_agent_step"`            // The sub-agent step to execute (private, not in main workflow) - must have "type" field
	ContextToPass string            `json:"context_to_pass,omitempty"` // Optional: specific context to pass to sub-agent
}

// UnmarshalJSON implements custom unmarshaling for PlanOrchestrationRoute
// This is needed to properly handle the SubAgentStep field which is a PlanStepInterface
func (r *PlanOrchestrationRoute) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a temporary struct to extract SubAgentStep as raw JSON
	var temp struct {
		RouteID       string          `json:"route_id"`
		RouteName     string          `json:"route_name"`
		Condition     string          `json:"condition"`
		SubAgentStep  json.RawMessage `json:"sub_agent_step"`
		ContextToPass string          `json:"context_to_pass,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal orchestration route: %w", err)
	}

	// Copy basic fields
	r.RouteID = temp.RouteID
	r.RouteName = temp.RouteName
	r.Condition = temp.Condition
	r.ContextToPass = temp.ContextToPass

	// Unmarshal nested SubAgentStep
	if len(temp.SubAgentStep) > 0 {
		// Check if it's null
		subAgentStepStr := string(temp.SubAgentStep)
		if subAgentStepStr != "null" {
			step, err := unmarshalStepFromJSON(temp.SubAgentStep)
			if err != nil {
				return fmt.Errorf("failed to unmarshal sub_agent_step: %w", err)
			}
			r.SubAgentStep = step
		} else {
			r.SubAgentStep = nil
		}
	} else {
		r.SubAgentStep = nil
	}

	return nil
}

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
	return fmt.Errorf(fmt.Sprintf("failed to unmarshal context_output as string or array"), nil)
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

// PrerequisiteRule represents a single prerequisite rule with one step dependency and one description
type PrerequisiteRule struct {
	DependsOnStep string `json:"depends_on_step"` // Step ID this rule depends on
	Description   string `json:"description"`     // User description of when to detect prerequisite failures for this specific step (e.g., "if login session is missing or expired, go back to step 0")
}

// ValidationSchema represents structured validation rules for step outputs
type ValidationSchema struct {
	Files []FileValidationRule `json:"files,omitempty"`
}

// FileValidationRule represents validation rules for a specific file
type FileValidationRule struct {
	FileName   string                `json:"file_name"`             // e.g., "results.json"
	MustExist  bool                  `json:"must_exist"`            // File must exist
	JSONChecks []JSONValidationCheck `json:"json_checks,omitempty"` // JSON structure checks
}

// JSONValidationCheck represents a validation check on JSON content
type JSONValidationCheck struct {
	Path             string           `json:"path"`                        // JSONPath, e.g., "$.status", "$.databases[0].name"
	MustExist        bool             `json:"must_exist"`                  // Key/path must exist
	ValueType        string           `json:"value_type,omitempty"`        // "string", "number", "boolean", "array", "object"
	MinLength        *int             `json:"min_length,omitempty"`        // For arrays/strings
	MaxLength        *int             `json:"max_length,omitempty"`        // For arrays/strings
	Pattern          string           `json:"pattern,omitempty"`           // Regex for format validation
	MinValue         *float64         `json:"min_value,omitempty"`         // For numbers
	MaxValue         *float64         `json:"max_value,omitempty"`         // For numbers
	ConsistencyCheck *ConsistencyRule `json:"consistency_check,omitempty"` // Compare with other fields
}

// ConsistencyRule represents a consistency check between fields
type ConsistencyRule struct {
	Type            string `json:"type"`              // "equals", "greater_than", "less_than", "array_length", "in_array"
	CompareWithPath string `json:"compare_with_path"` // JSONPath to compare with
}

// AgentConfigs represents per-agent configuration for a step
type AgentConfigs struct {
	ExecutionLLM                           *AgentLLMConfig    `json:"execution_llm,omitempty"`
	ValidationLLM                          *AgentLLMConfig    `json:"validation_llm,omitempty"`
	LearningLLM                            *AgentLLMConfig    `json:"learning_llm,omitempty"`
	ConditionalLLM                         *AgentLLMConfig    `json:"conditional_llm,omitempty"`                              // Step-specific conditional LLM for conditional step evaluation
	ExecutionMaxTurns                      *int               `json:"execution_max_turns,omitempty"`                          // default: 100
	ValidationMaxTurns                     *int               `json:"validation_max_turns,omitempty"`                         // default: 100
	LearningMaxTurns                       *int               `json:"learning_max_turns,omitempty"`                           // default: 100
	OrchestrationMaxIterations             *int               `json:"orchestration_max_iterations,omitempty"`                 // default: orchestrator max turns (typically 100)
	DisableValidation                      *bool              `json:"disable_validation,omitempty"`                           // skip validation entirely (nil = not set/enabled, true = disabled, false = explicitly enabled)
	SkipLLMValidationIfPreValidationPasses *bool              `json:"skip_llm_validation_if_pre_validation_passes,omitempty"` // if true, skip LLM validation when pre-validation passes (assume validation success)
	DisableLearning                        *bool              `json:"disable_learning,omitempty"`                             // disable learning for this step (nil = not set/enabled, true = disabled, false = explicitly enabled)
	LockLearnings                          *bool              `json:"lock_learnings,omitempty"`                               // lock learnings - prevents learning agent from running but still uses existing learnings (nil = not set/unlocked, true = locked, false = explicitly unlocked)
	LearningAfterLoopIteration             bool               `json:"learning_after_loop_iteration,omitempty"`                // run learning after each loop iteration
	LearningDetailLevel                    string             `json:"learning_detail_level,omitempty"`                        // "exact", "general", or "none" (default: "exact")
	SelectedServers                        []string           `json:"selected_servers,omitempty"`                             // step-level MCP server selection (subset of preset servers)
	SelectedTools                          []string           `json:"selected_tools,omitempty"`                               // step-level tool selection (format: "server:tool" or "server:*" for all tools)
	EnabledCustomToolCategories            []string           `json:"enabled_custom_tool_categories,omitempty"`               // e.g., ["workspace_tools", "human_tools"] - enables all tools in category
	EnabledCustomTools                     []string           `json:"enabled_custom_tools,omitempty"`                         // e.g., ["read_workspace_file", "human_feedback"] - enables specific tools (overrides categories if both specified)
	EnableLargeOutputVirtualTools          *bool              `json:"enable_large_output_virtual_tools,omitempty"`            // Enable/disable large output tools (default: true if nil)
	UseCodeExecutionMode                   *bool              `json:"use_code_execution_mode,omitempty"`                      // Step-level code execution mode override (nil = use preset default, true/false = override)
	EnablePrerequisiteDetection            *bool              `json:"enable_prerequisite_detection,omitempty"`                // Enable prerequisite failure detection for this step (default: false)
	PrerequisiteRules                      []PrerequisiteRule `json:"prerequisite_rules,omitempty"`                           // Array of prerequisite rules, each with one step dependency and one description
}

// ============================================================================
// TYPE-SAFE STEP SYSTEM (New Implementation)
// ============================================================================

// StepType represents the type of a plan step
type StepType string

const (
	StepTypeRegular       StepType = "regular"
	StepTypeConditional   StepType = "conditional"
	StepTypeDecision      StepType = "decision"
	StepTypeOrchestration StepType = "orchestration"
)

// CommonStepFields contains fields shared by all step types
type CommonStepFields struct {
	ID                          string                `json:"id"` // Stable step ID (generated from title) - required
	Title                       string                `json:"title"`
	Description                 string                `json:"description"`
	SuccessCriteria             string                `json:"success_criteria"`
	ContextDependencies         []string              `json:"context_dependencies"`
	ContextOutput               FlexibleContextOutput `json:"context_output"`                          // Use flexible type to handle string or array
	EnablePrerequisiteDetection *bool                 `json:"enable_prerequisite_detection,omitempty"` // Enable prerequisite failure detection for this step (default: false)
	PrerequisiteRules           []PrerequisiteRule    `json:"prerequisite_rules,omitempty"`            // Array of prerequisite rules, each with one step dependency and one description
	ValidationSchema            *ValidationSchema     `json:"validation_schema,omitempty"`             // Optional structured validation schema for step outputs
}

// PlanStepInterface is the interface that all step types must implement
// PlanStep is a type alias for PlanStepInterface for convenience
type PlanStepInterface interface {
	GetID() string
	GetTitle() string
	GetDescription() string
	GetSuccessCriteria() string
	GetContextDependencies() []string
	GetContextOutput() FlexibleContextOutput
	GetEnablePrerequisiteDetection() *bool
	GetPrerequisiteRules() []PrerequisiteRule
	GetValidationSchema() *ValidationSchema
	StepType() StepType
	// GetCommonFields returns a copy of common fields for convenience
	GetCommonFields() CommonStepFields
}

// RegularPlanStep represents a regular step (may have loops)
type RegularPlanStep struct {
	Type StepType `json:"type"` // Always "regular" - required for JSON marshaling/unmarshaling
	CommonStepFields
	HasLoop         bool          `json:"has_loop"`                   // true if step needs to loop
	LoopCondition   string        `json:"loop_condition"`             // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations   int           `json:"max_iterations,omitempty"`   // max iterations (default: 10)
	LoopDescription string        `json:"loop_description,omitempty"` // human-readable explanation
	AgentConfigs    *AgentConfigs `json:"-"`                          // runtime: per-agent configuration (LLM, max turns, toggles) - not stored in plan.json
}

// Implement PlanStepInterface for RegularPlanStep
func (r *RegularPlanStep) GetID() string                           { return r.ID }
func (r *RegularPlanStep) GetTitle() string                        { return r.Title }
func (r *RegularPlanStep) GetDescription() string                  { return r.Description }
func (r *RegularPlanStep) GetSuccessCriteria() string              { return r.SuccessCriteria }
func (r *RegularPlanStep) GetContextDependencies() []string        { return r.ContextDependencies }
func (r *RegularPlanStep) GetContextOutput() FlexibleContextOutput { return r.ContextOutput }
func (r *RegularPlanStep) GetEnablePrerequisiteDetection() *bool {
	return r.EnablePrerequisiteDetection
}
func (r *RegularPlanStep) GetPrerequisiteRules() []PrerequisiteRule { return r.PrerequisiteRules }
func (r *RegularPlanStep) GetValidationSchema() *ValidationSchema   { return r.ValidationSchema }
func (r *RegularPlanStep) StepType() StepType                       { return StepTypeRegular }
func (r *RegularPlanStep) GetCommonFields() CommonStepFields        { return r.CommonStepFields }

// MarshalJSON ensures the type field is always set when marshaling
func (r *RegularPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	r.Type = StepTypeRegular
	// Use type alias to avoid infinite recursion
	type Alias RegularPlanStep
	return json.Marshal((*Alias)(r))
}

// ConditionalPlanStep represents a conditional step with branches
// NOTE: Conditional steps are wrappers that branch based on conditions.
// Loops are NOT supported on conditional wrappers - if looping is needed, it should be on the branch steps (IfTrueSteps, IfFalseSteps).
// Prerequisite rules are NOT supported on conditional wrappers - if needed, they should be on the branch steps.
type ConditionalPlanStep struct {
	Type StepType `json:"type"` // Always "conditional" - required for JSON marshaling/unmarshaling
	CommonStepFields
	ConditionQuestion string              `json:"condition_question,omitempty"`    // question to ask ConditionalLLM
	ConditionContext  string              `json:"condition_context,omitempty"`     // context to provide to ConditionalLLM
	IfTrueSteps       []PlanStepInterface `json:"if_true_steps,omitempty"`         // nested steps for true branch
	IfFalseSteps      []PlanStepInterface `json:"if_false_steps,omitempty"`        // nested steps for false branch
	IfTrueNextStepID  string              `json:"if_true_next_step_id,omitempty"`  // ID of step to connect to after true branch completes (or "end" to end workflow). When if_true_steps is empty [], this is REQUIRED. When if_true_steps has steps, this is optional (defaults to next step in main plan if not specified).
	IfFalseNextStepID string              `json:"if_false_next_step_id,omitempty"` // ID of step to connect to after false branch completes (or "end" to end workflow). When if_false_steps is empty [], this is REQUIRED. When if_false_steps has steps, this is optional (defaults to next step in main plan if not specified).
	ConditionResult   *bool               `json:"-"`                               // runtime: stores decision result - not stored in plan.json
	ConditionReason   string              `json:"-"`                               // runtime: stores LLM reasoning - not stored in plan.json
	AgentConfigs      *AgentConfigs       `json:"-"`                               // runtime: per-agent configuration (LLM, max turns, toggles) - not stored in plan.json
}

// Implement PlanStepInterface for ConditionalPlanStep
func (c *ConditionalPlanStep) GetID() string                           { return c.ID }
func (c *ConditionalPlanStep) GetTitle() string                        { return c.Title }
func (c *ConditionalPlanStep) GetDescription() string                  { return c.Description }
func (c *ConditionalPlanStep) GetSuccessCriteria() string              { return c.SuccessCriteria }
func (c *ConditionalPlanStep) GetContextDependencies() []string        { return c.ContextDependencies }
func (c *ConditionalPlanStep) GetContextOutput() FlexibleContextOutput { return c.ContextOutput }
func (c *ConditionalPlanStep) GetEnablePrerequisiteDetection() *bool {
	return nil // Not supported on conditional wrappers
}
func (c *ConditionalPlanStep) GetPrerequisiteRules() []PrerequisiteRule { return nil } // Not supported on conditional wrappers
func (c *ConditionalPlanStep) GetValidationSchema() *ValidationSchema   { return c.ValidationSchema }
func (c *ConditionalPlanStep) StepType() StepType                       { return StepTypeConditional }
func (c *ConditionalPlanStep) GetCommonFields() CommonStepFields {
	// Return common fields but override prerequisite fields (not supported on conditional wrappers)
	return CommonStepFields{
		ID:                          c.ID,
		Title:                       c.Title,
		Description:                 c.Description,
		SuccessCriteria:             c.SuccessCriteria,
		ContextDependencies:         c.ContextDependencies,
		ContextOutput:               c.ContextOutput,
		EnablePrerequisiteDetection: nil, // Not supported on conditional wrappers
		PrerequisiteRules:           nil, // Not supported on conditional wrappers
		ValidationSchema:            c.ValidationSchema,
	}
}

// MarshalJSON ensures the type field is always set when marshaling
func (c *ConditionalPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	c.Type = StepTypeConditional
	// Use type alias to avoid infinite recursion
	type Alias ConditionalPlanStep
	return json.Marshal((*Alias)(c))
}

// UnmarshalJSON implements custom unmarshaling for ConditionalPlanStep
// This is needed to properly handle nested if_true_steps and if_false_steps arrays
func (c *ConditionalPlanStep) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a temporary struct to extract nested steps as raw JSON
	var temp struct {
		Type StepType `json:"type"`
		CommonStepFields
		ConditionQuestion string            `json:"condition_question,omitempty"`
		ConditionContext  string            `json:"condition_context,omitempty"`
		IfTrueSteps       []json.RawMessage `json:"if_true_steps,omitempty"`
		IfFalseSteps      []json.RawMessage `json:"if_false_steps,omitempty"`
		IfTrueNextStepID  string            `json:"if_true_next_step_id,omitempty"`
		IfFalseNextStepID string            `json:"if_false_next_step_id,omitempty"`
		// Runtime fields (ConditionResult, ConditionReason, AgentConfigs) are excluded - they use json:"-" and won't be in plan.json
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal conditional step: %w", err)
	}

	// Copy basic fields
	c.Type = temp.Type
	c.CommonStepFields = temp.CommonStepFields
	c.ConditionQuestion = temp.ConditionQuestion
	c.ConditionContext = temp.ConditionContext
	c.IfTrueNextStepID = temp.IfTrueNextStepID
	c.IfFalseNextStepID = temp.IfFalseNextStepID
	// Runtime fields (ConditionResult, ConditionReason, AgentConfigs) are not unmarshaled - they use json:"-"

	// Unmarshal nested steps
	if len(temp.IfTrueSteps) > 0 {
		steps, err := unmarshalStepsFromJSON(temp.IfTrueSteps)
		if err != nil {
			return fmt.Errorf("failed to unmarshal if_true_steps: %w", err)
		}
		c.IfTrueSteps = steps
	} else {
		c.IfTrueSteps = nil
	}

	if len(temp.IfFalseSteps) > 0 {
		steps, err := unmarshalStepsFromJSON(temp.IfFalseSteps)
		if err != nil {
			return fmt.Errorf("failed to unmarshal if_false_steps: %w", err)
		}
		c.IfFalseSteps = steps
	} else {
		c.IfFalseSteps = nil
	}

	return nil
}

// DecisionPlanStep represents a decision step (execute step, evaluate output, route based on result)
// NOTE: Decision steps are wrappers that route based on inner step execution.
// The wrapper only needs ID and Title for identification/display.
// Description, SuccessCriteria, ContextDependencies, and ContextOutput come from the inner DecisionStep.
// Loops are NOT supported on decision wrappers - if looping is needed, it should be on the inner DecisionStep.
type DecisionPlanStep struct {
	Type                       StepType          `json:"type"`                                   // Always "decision" - required for JSON marshaling/unmarshaling
	ID                         string            `json:"id"`                                     // Stable step ID - required for identification
	Title                      string            `json:"title"`                                  // Display title for the decision step wrapper
	DecisionStep               PlanStepInterface `json:"decision_step,omitempty"`                // The single step to execute (has its own Description, SuccessCriteria, etc.)
	DecisionEvaluationQuestion string            `json:"decision_evaluation_question,omitempty"` // Question to evaluate step output
	IfTrueNextStepID           string            `json:"if_true_next_step_id,omitempty"`         // ID of step to connect to if decision is true (or "end")
	IfFalseNextStepID          string            `json:"if_false_next_step_id,omitempty"`        // ID of step to connect to if decision is false (or "end")
	DecisionResult             *bool             `json:"-"`                                      // runtime: stores evaluation result (backward compatibility) - not stored in plan.json
	DecisionReason             string            `json:"-"`                                      // runtime: stores evaluation reasoning (backward compatibility) - not stored in plan.json
	DecisionResponse           *DecisionResponse `json:"-"`                                      // runtime: stores structured decision evaluation response - not stored in plan.json
	AgentConfigs               *AgentConfigs     `json:"-"`                                      // runtime: per-agent configuration (LLM, max turns, toggles) - not stored in plan.json
	// Prerequisite rules are NOT supported on decision wrappers - if needed, they should be on the inner DecisionStep.
}

// Implement PlanStepInterface for DecisionPlanStep
func (d *DecisionPlanStep) GetID() string                           { return d.ID }
func (d *DecisionPlanStep) GetTitle() string                        { return d.Title }
func (d *DecisionPlanStep) GetDescription() string                  { return "" }  // Not used - inner DecisionStep has description
func (d *DecisionPlanStep) GetSuccessCriteria() string              { return "" }  // Not used - inner DecisionStep has success criteria
func (d *DecisionPlanStep) GetContextDependencies() []string        { return nil } // Not used - inner DecisionStep has context dependencies
func (d *DecisionPlanStep) GetContextOutput() FlexibleContextOutput { return "" }  // Not used - inner DecisionStep produces context output
func (d *DecisionPlanStep) GetEnablePrerequisiteDetection() *bool {
	return nil // Not supported on decision wrappers
}
func (d *DecisionPlanStep) GetPrerequisiteRules() []PrerequisiteRule { return nil } // Not supported on decision wrappers
func (d *DecisionPlanStep) GetValidationSchema() *ValidationSchema {
	// Return validation schema from inner DecisionStep if it exists
	if d.DecisionStep != nil {
		return d.DecisionStep.GetValidationSchema()
	}
	return nil
}
func (d *DecisionPlanStep) StepType() StepType { return StepTypeDecision }
func (d *DecisionPlanStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:                          d.ID,
		Title:                       d.Title,
		Description:                 "",  // Not used for decision wrapper
		SuccessCriteria:             "",  // Not used for decision wrapper
		ContextDependencies:         nil, // Not used for decision wrapper
		ContextOutput:               "",  // Not used for decision wrapper
		EnablePrerequisiteDetection: nil, // Not supported on decision wrappers
		PrerequisiteRules:           nil, // Not supported on decision wrappers
		ValidationSchema:            d.GetValidationSchema(),
	}
}

// MarshalJSON ensures the type field is always set when marshaling
func (d *DecisionPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	d.Type = StepTypeDecision
	// Use type alias to avoid infinite recursion
	type Alias DecisionPlanStep
	return json.Marshal((*Alias)(d))
}

// UnmarshalJSON implements custom unmarshaling for DecisionPlanStep
// This is needed to properly handle nested decision_step
func (d *DecisionPlanStep) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a temporary struct to extract nested step as raw JSON
	var temp struct {
		Type                       StepType        `json:"type"`
		ID                         string          `json:"id"`
		Title                      string          `json:"title"`
		DecisionStep               json.RawMessage `json:"decision_step,omitempty"`
		DecisionEvaluationQuestion string          `json:"decision_evaluation_question,omitempty"`
		IfTrueNextStepID           string          `json:"if_true_next_step_id,omitempty"`
		IfFalseNextStepID          string          `json:"if_false_next_step_id,omitempty"`
		// Runtime fields (DecisionResult, DecisionReason, DecisionResponse, AgentConfigs) are excluded - they use json:"-" and won't be in plan.json
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal decision step: %w", err)
	}

	// Copy basic fields
	d.Type = temp.Type
	d.ID = temp.ID
	d.Title = temp.Title
	d.DecisionEvaluationQuestion = temp.DecisionEvaluationQuestion
	d.IfTrueNextStepID = temp.IfTrueNextStepID
	d.IfFalseNextStepID = temp.IfFalseNextStepID
	// Runtime fields (DecisionResult, DecisionReason, DecisionResponse, AgentConfigs) are not unmarshaled - they use json:"-"

	// Unmarshal nested decision_step
	if len(temp.DecisionStep) > 0 {
		step, err := unmarshalStepFromJSON(temp.DecisionStep)
		if err != nil {
			return fmt.Errorf("failed to unmarshal decision_step: %w", err)
		}
		d.DecisionStep = step
	} else {
		d.DecisionStep = nil
	}

	return nil
}

// OrchestrationPlanStep represents an orchestration step (orchestrator with multiple sub-agents)
// NOTE: Orchestration steps are wrappers that coordinate multiple sub-agents.
// The wrapper only needs ID and Title for identification/display.
// Description, SuccessCriteria, ContextDependencies, and ContextOutput come from the inner OrchestrationStep.
// Loops are NOT supported on orchestration wrappers - if looping is needed, it should be on the inner OrchestrationStep.
type OrchestrationPlanStep struct {
	Type                  StepType                 `json:"type"`                           // Always "orchestration" - required for JSON marshaling/unmarshaling
	ID                    string                   `json:"id"`                             // Stable step ID - required for identification
	Title                 string                   `json:"title"`                          // Display title for the orchestration step wrapper
	OrchestrationStep     PlanStepInterface        `json:"orchestration_step,omitempty"`   // The main orchestrator step to execute (has its own Description, SuccessCriteria, etc.)
	OrchestrationRoutes   []PlanOrchestrationRoute `json:"orchestration_routes,omitempty"` // Array of possible routes with conditions
	NextStepID            string                   `json:"next_step_id,omitempty"`         // ID of step after orchestration completes (or "end")
	OrchestrationResponse *OrchestrationResponse   `json:"-"`                              // runtime: stores selected route and success evaluation - not stored in plan.json
	AgentConfigs          *AgentConfigs            `json:"-"`                              // runtime: per-agent configuration (LLM, max turns, toggles) - not stored in plan.json
	// Prerequisite rules are NOT supported on orchestration wrappers - if needed, they should be on the inner OrchestrationStep.
}

// Implement PlanStepInterface for OrchestrationPlanStep
func (o *OrchestrationPlanStep) GetID() string                           { return o.ID }
func (o *OrchestrationPlanStep) GetTitle() string                        { return o.Title }
func (o *OrchestrationPlanStep) GetDescription() string                  { return "" }  // Not used - inner OrchestrationStep has description
func (o *OrchestrationPlanStep) GetSuccessCriteria() string              { return "" }  // Not used - inner OrchestrationStep has success criteria
func (o *OrchestrationPlanStep) GetContextDependencies() []string        { return nil } // Not used - inner OrchestrationStep has context dependencies
func (o *OrchestrationPlanStep) GetContextOutput() FlexibleContextOutput { return "" }  // Not used - inner OrchestrationStep produces context output
func (o *OrchestrationPlanStep) GetEnablePrerequisiteDetection() *bool {
	return nil // Not supported on orchestration wrappers
}
func (o *OrchestrationPlanStep) GetPrerequisiteRules() []PrerequisiteRule { return nil } // Not supported on orchestration wrappers
func (o *OrchestrationPlanStep) GetValidationSchema() *ValidationSchema {
	// Return validation schema from inner OrchestrationStep if it exists
	if o.OrchestrationStep != nil {
		return o.OrchestrationStep.GetValidationSchema()
	}
	return nil
}
func (o *OrchestrationPlanStep) StepType() StepType { return StepTypeOrchestration }
func (o *OrchestrationPlanStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:                          o.ID,
		Title:                       o.Title,
		Description:                 "",  // Not used for orchestration wrapper
		SuccessCriteria:             "",  // Not used for orchestration wrapper
		ContextDependencies:         nil, // Not used for orchestration wrapper
		ContextOutput:               "",  // Not used for orchestration wrapper
		EnablePrerequisiteDetection: nil, // Not supported on orchestration wrappers
		PrerequisiteRules:           nil, // Not supported on orchestration wrappers
		ValidationSchema:            o.GetValidationSchema(),
	}
}

// MarshalJSON ensures the type field is always set when marshaling
func (o *OrchestrationPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	o.Type = StepTypeOrchestration
	// Use type alias to avoid infinite recursion
	type Alias OrchestrationPlanStep
	return json.Marshal((*Alias)(o))
}

// UnmarshalJSON implements custom unmarshaling for OrchestrationPlanStep
// This is needed to properly handle nested orchestration_step and orchestration_routes[].sub_agent_step
func (o *OrchestrationPlanStep) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a temporary struct to extract nested steps as raw JSON
	var temp struct {
		Type                StepType        `json:"type"`
		ID                  string          `json:"id"`
		Title               string          `json:"title"`
		OrchestrationStep   json.RawMessage `json:"orchestration_step,omitempty"`
		OrchestrationRoutes []struct {
			RouteID       string          `json:"route_id"`
			RouteName     string          `json:"route_name"`
			Condition     string          `json:"condition"`
			SubAgentStep  json.RawMessage `json:"sub_agent_step"`
			ContextToPass string          `json:"context_to_pass,omitempty"`
		} `json:"orchestration_routes,omitempty"`
		NextStepID string `json:"next_step_id,omitempty"`
		// Runtime fields (OrchestrationResponse, AgentConfigs) are excluded - they use json:"-" and won't be in plan.json
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal orchestration step: %w", err)
	}

	// Copy basic fields
	o.Type = temp.Type
	o.ID = temp.ID
	o.Title = temp.Title
	o.NextStepID = temp.NextStepID
	// Runtime fields (OrchestrationResponse, AgentConfigs) are not unmarshaled - they use json:"-"

	// Unmarshal nested orchestration_step
	if len(temp.OrchestrationStep) > 0 {
		step, err := unmarshalStepFromJSON(temp.OrchestrationStep)
		if err != nil {
			return fmt.Errorf("failed to unmarshal orchestration_step: %w", err)
		}
		o.OrchestrationStep = step
	} else {
		o.OrchestrationStep = nil
	}

	// Unmarshal orchestration_routes with nested sub_agent_step
	if len(temp.OrchestrationRoutes) > 0 {
		o.OrchestrationRoutes = make([]PlanOrchestrationRoute, len(temp.OrchestrationRoutes))
		for i, route := range temp.OrchestrationRoutes {
			o.OrchestrationRoutes[i].RouteID = route.RouteID
			o.OrchestrationRoutes[i].RouteName = route.RouteName
			o.OrchestrationRoutes[i].Condition = route.Condition
			o.OrchestrationRoutes[i].ContextToPass = route.ContextToPass

			// Unmarshal nested sub_agent_step
			if len(route.SubAgentStep) > 0 {
				step, err := unmarshalStepFromJSON(route.SubAgentStep)
				if err != nil {
					return fmt.Errorf("failed to unmarshal sub_agent_step in route %d: %w", i, err)
				}
				o.OrchestrationRoutes[i].SubAgentStep = step
			} else {
				o.OrchestrationRoutes[i].SubAgentStep = nil
			}
		}
	} else {
		o.OrchestrationRoutes = nil
	}

	return nil
}

// PlanStep is now an alias for PlanStepInterface for convenience
// All code should use PlanStepInterface directly
type PlanStep = PlanStepInterface

// PlanningResponse represents the structured response from planning
// Uses type-safe PlanStepInterface - all plans must be in new format with "type" field
type PlanningResponse struct {
	Steps []PlanStepInterface `json:"-"`
}

// UnmarshalJSON implements custom unmarshaling for typed steps
func (pr *PlanningResponse) UnmarshalJSON(data []byte) error {
	var temp struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal plan: %w", err)
	}

	pr.Steps = make([]PlanStepInterface, len(temp.Steps))
	for i, stepData := range temp.Steps {
		// Check for type field
		var stepWithType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(stepData, &stepWithType); err != nil {
			return fmt.Errorf("failed to parse step %d type: %w", i, err)
		}

		if stepWithType.Type == "" {
			return fmt.Errorf("step %d is missing required 'type' field (must be: regular, conditional, decision, or orchestration)", i)
		}

		// Unmarshal based on type
		var typedStep PlanStepInterface
		switch stepWithType.Type {
		case "regular":
			var step RegularPlanStep
			if err := json.Unmarshal(stepData, &step); err != nil {
				return fmt.Errorf("failed to parse regular step %d: %w", i, err)
			}
			typedStep = &step
		case "conditional":
			var step ConditionalPlanStep
			if err := json.Unmarshal(stepData, &step); err != nil {
				return fmt.Errorf("failed to parse conditional step %d: %w", i, err)
			}
			typedStep = &step
		case "decision":
			var step DecisionPlanStep
			if err := json.Unmarshal(stepData, &step); err != nil {
				return fmt.Errorf("failed to parse decision step %d: %w", i, err)
			}
			typedStep = &step
		case "orchestration":
			var step OrchestrationPlanStep
			if err := json.Unmarshal(stepData, &step); err != nil {
				return fmt.Errorf("failed to parse orchestration step %d: %w", i, err)
			}
			typedStep = &step
		default:
			return fmt.Errorf("unknown step type %q in step %d (must be: regular, conditional, decision, or orchestration)", stepWithType.Type, i)
		}

		pr.Steps[i] = typedStep
	}

	return nil
}

// MarshalJSON implements custom marshaling for typed steps
func (pr PlanningResponse) MarshalJSON() ([]byte, error) {
	type stepWrapper struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"-"`
	}

	wrappedSteps := make([]json.RawMessage, len(pr.Steps))
	for i, step := range pr.Steps {
		// Marshal the step (which already has type field)
		stepJSON, err := json.Marshal(step)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal step %d: %w", i, err)
		}
		wrappedSteps[i] = stepJSON
	}

	return json.Marshal(map[string]interface{}{
		"steps": wrappedSteps,
	})
}

// PartialPlanStep represents a partial update to a plan step (used only in tool schemas)
// NOTE: This struct works with typed steps (PlanStepInterface).
// Nested steps (DecisionStep, OrchestrationStep, IfTrueSteps, IfFalseSteps) are PlanStepInterface.
// Once plan.json is migrated to use new type-safe types, this struct can be updated accordingly.
type PartialPlanStep struct {
	ExistingStepID              string                `json:"existing_step_id"`                        // Required: ID of existing step to update
	Title                       string                `json:"title,omitempty"`                         // Optional: New title (if renaming)
	Description                 string                `json:"description,omitempty"`                   // Optional: Updated description
	SuccessCriteria             string                `json:"success_criteria,omitempty"`              // Optional: Updated success criteria
	ContextDependencies         []string              `json:"context_dependencies,omitempty"`          // Optional: Updated context dependencies
	ContextOutput               FlexibleContextOutput `json:"context_output,omitempty"`                // Optional: Updated context output
	HasLoop                     *bool                 `json:"has_loop,omitempty"`                      // Optional: Updated has_loop (use pointer to distinguish unset from false)
	LoopCondition               string                `json:"loop_condition,omitempty"`                // Optional: Updated loop condition
	MaxIterations               *int                  `json:"max_iterations,omitempty"`                // Optional: Updated max iterations (use pointer to distinguish unset from 0)
	LoopDescription             string                `json:"loop_description,omitempty"`              // Optional: Updated loop description
	EnablePrerequisiteDetection *bool                 `json:"enable_prerequisite_detection,omitempty"` // Optional: Updated enable_prerequisite_detection (use pointer to distinguish unset from false)
	PrerequisiteRules           []PrerequisiteRule    `json:"prerequisite_rules,omitempty"`            // Optional: Updated prerequisite rules
	// Conditional step fields
	HasCondition      *bool                    `json:"has_condition,omitempty"`      // Optional: Updated has_condition (use pointer to distinguish unset from false)
	ConditionQuestion string                   `json:"condition_question,omitempty"` // Optional: Updated condition question
	ConditionContext  string                   `json:"condition_context,omitempty"`  // Optional: Updated condition context
	IfTrueSteps       []map[string]interface{} `json:"if_true_steps,omitempty"`      // Optional: Updated if_true_steps (nil = not provided, empty array = clear steps) - will be converted to PlanStepInterface
	IfFalseSteps      []map[string]interface{} `json:"if_false_steps,omitempty"`     // Optional: Updated if_false_steps (nil = not provided, empty array = clear steps) - will be converted to PlanStepInterface
	// Decision step fields
	DecisionStep               map[string]interface{} `json:"decision_step,omitempty"`                // Optional: Updated decision step - will be converted to PlanStepInterface
	DecisionEvaluationQuestion string                 `json:"decision_evaluation_question,omitempty"` // Optional: Updated decision evaluation question
	// Orchestration step fields
	OrchestrationStep   map[string]interface{}   `json:"orchestration_step,omitempty"`   // Optional: Updated orchestration step - will be converted to PlanStepInterface
	OrchestrationRoutes []PlanOrchestrationRoute `json:"orchestration_routes,omitempty"` // Optional: Updated orchestration routes
	// Routing fields (used by both conditional, decision, and routing steps)
	IfTrueNextStepID  string            `json:"if_true_next_step_id,omitempty"`  // Optional: Updated if_true_next_step_id
	IfFalseNextStepID string            `json:"if_false_next_step_id,omitempty"` // Optional: Updated if_false_next_step_id
	NextStepID        string            `json:"next_step_id,omitempty"`          // Optional: Updated next_step_id (for routing steps)
	ValidationSchema  *ValidationSchema `json:"validation_schema,omitempty"`     // Optional: Updated validation schema
}

// planFileMutex ensures thread-safe access to plan.json
var planFileMutex sync.Mutex

// changelogSessionMutex ensures thread-safe access to changelog session tracking
var changelogSessionMutex sync.Mutex

// changelogSessionFile tracks the current changelog file for the active session
// Format: changelog-YYYY-MM-DD-HH-MM-SS.json
var changelogSessionFile string

// changelogSessionStartTime tracks when the current session started
var changelogSessionStartTime time.Time

// PlanChangeLogEntry represents a single change entry in the changelog
type PlanChangeLogEntry struct {
	Timestamp   string          `json:"timestamp"`    // ISO 8601 timestamp
	ChangeType  string          `json:"change_type"`  // "update", "delete", "add", "convert_to_conditional", "convert_to_regular", "add_branch_steps", "update_branch_steps", "delete_branch_steps", "update_conditional_step"
	StepIDs     []string        `json:"step_ids"`     // Affected step IDs
	Description string          `json:"description"`  // Human-readable description of the change
	Details     string          `json:"details"`      // Additional details (JSON string of what changed)
	DiffChanges json.RawMessage `json:"diff_changes"` // Raw diff output from r3labs/diff library (stores diff.Changelog as JSON)
	// For backward compatibility and detailed field tracking (optional - can be derived from diff_changes)
	Changes []PlanFieldChange `json:"changes,omitempty"` // Old and new values for each changed field (deprecated, use diff_changes)
	// For revert support: store complete step snapshots
	AddedSteps        []json.RawMessage `json:"added_steps,omitempty"`          // Complete step data for "add" operations (to restore on revert) - stored as JSON
	DeletedSteps      []json.RawMessage `json:"deleted_steps,omitempty"`        // Complete step data for "delete" operations (to restore on revert) - stored as JSON
	InsertAfterStepID string            `json:"insert_after_step_id,omitempty"` // For "add" operations: where the step was inserted (needed for revert)
}

// PlanFieldChange represents a single field change with old and new values
type PlanFieldChange struct {
	StepID   string      `json:"step_id"`   // Step ID that was changed
	Field    string      `json:"field"`     // Field name (title, description, success_criteria, etc.)
	OldValue interface{} `json:"old_value"` // Old value (can be nil if field didn't exist)
	NewValue interface{} `json:"new_value"` // New value
}

// PlanChangeLog represents the changelog structure (used for reading multiple files)
type PlanChangeLog struct {
	Entries []PlanChangeLogEntry `json:"entries"`
}

// writeChangelogEntry writes a changelog entry to a session-based file in planning/changelog/
// All changes during a single planning agent execution session are written to the same file
// File format: changelog-YYYY-MM-DD-HH-MM-SS.json (session start timestamp)
func writeChangelogEntry(ctx context.Context, workspacePath string, entry PlanChangeLogEntry, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	changelogSessionMutex.Lock()
	defer changelogSessionMutex.Unlock()

	// Check if we need to start a new session (no active session or session is too old - more than 1 hour)
	now := time.Now()
	if changelogSessionFile == "" || now.Sub(changelogSessionStartTime) > time.Hour {
		// Start new session
		changelogSessionStartTime = now
		changelogSessionFile = fmt.Sprintf("changelog-%s.json", now.Format("2006-01-02-15-04-05"))
		logger.Info(fmt.Sprintf("📝 Starting new changelog session: %s", changelogSessionFile))
	}

	// Ensure entry timestamp is set
	if entry.Timestamp == "" {
		entry.Timestamp = now.Format(time.RFC3339)
	}

	changelogPath := filepath.Join(workspacePath, "planning", "changelog", changelogSessionFile)

	// Read existing changelog if it exists
	var changelog PlanChangeLog
	existingContent, err := readFile(ctx, changelogPath)
	if err == nil {
		// Changelog exists, unmarshal it
		if err := json.Unmarshal([]byte(existingContent), &changelog); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to parse existing changelog, creating new one: %v", err))
			changelog = PlanChangeLog{Entries: []PlanChangeLogEntry{}}
		}
	} else {
		// Changelog doesn't exist, create new one
		changelog = PlanChangeLog{Entries: []PlanChangeLogEntry{}}
	}

	// Add new entry
	changelog.Entries = append(changelog.Entries, entry)

	// Write updated changelog
	data, err := json.MarshalIndent(changelog, "", "  ")
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to marshal changelog: %w", err), nil)
	}

	if err := writeFile(ctx, changelogPath, string(data)); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write changelog file: %w", err), nil)
	}

	logger.Info(fmt.Sprintf("📝 Appended changelog entry to %s: %s - %s", changelogSessionFile, entry.ChangeType, entry.Description))
	return nil
}

// resetChangelogSession resets the changelog session (call this at the start of a new planning agent execution)
func resetChangelogSession() {
	changelogSessionMutex.Lock()
	defer changelogSessionMutex.Unlock()
	changelogSessionFile = ""
	changelogSessionStartTime = time.Time{}
}

// generateChangelogFromPlanDiff compares two plans and generates changelog entries for all differences
// This is called after the planning agent completes to capture all changes in one go
// Uses r3labs/diff library - stores raw diff output directly (no conversion needed)
func generateChangelogFromPlanDiff(ctx context.Context, workspacePath string, oldPlan *PlanningResponse, newPlan *PlanningResponse, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	if oldPlan == nil && newPlan == nil {
		return nil // No changes
	}

	// Use r3labs/diff to compare the entire plan structure
	// This gives us all changes in one go, including nested structures
	diffChangelog, err := diff.Diff(oldPlan, newPlan, diff.AllowTypeMismatch(true))
	if err != nil {
		// If diff fails, log warning but don't fail - we still have complete old/new plans for revert
		logger.Warn(fmt.Sprintf("⚠️ Failed to generate diff: %v. Storing complete plan snapshots for revert.", err))
		diffChangelog = diff.Changelog{}
	}

	// Extract step IDs that were affected from the diff
	affectedStepIDs := make(map[string]bool)
	var addedSteps []json.RawMessage
	var deletedSteps []json.RawMessage

	// Create maps for step lookup
	oldStepsByID := make(map[string]PlanStepInterface)
	if oldPlan != nil {
		for _, step := range oldPlan.Steps {
			oldStepsByID[step.GetID()] = step
		}
	}

	newStepsByID := make(map[string]PlanStepInterface)
	if newPlan != nil {
		for _, step := range newPlan.Steps {
			newStepsByID[step.GetID()] = step
		}
	}

	// Find added/deleted steps
	for stepID, oldStep := range oldStepsByID {
		if _, exists := newStepsByID[stepID]; !exists {
			affectedStepIDs[stepID] = true
			stepJSON, _ := json.Marshal(oldStep)
			deletedSteps = append(deletedSteps, stepJSON)
		}
	}

	for stepID, newStep := range newStepsByID {
		if _, exists := oldStepsByID[stepID]; !exists {
			affectedStepIDs[stepID] = true
			stepJSON, _ := json.Marshal(newStep)
			addedSteps = append(addedSteps, stepJSON)
		}
	}

	// Extract step IDs from diff paths (paths like ["Steps", "0", "Title"] or ["Steps", "0", "orchestration_step", "description"])
	// Map array indices to step IDs
	stepIndexToID := make(map[int]string)
	if newPlan != nil {
		for i, step := range newPlan.Steps {
			stepIndexToID[i] = step.GetID()
		}
	}

	// Check diff paths to find which steps were modified
	for _, change := range diffChangelog {
		if len(change.Path) >= 2 && change.Path[0] == "Steps" {
			// Path format: ["Steps", index, ...field path...]
			// change.Path[1] is already a string, parse it as index
			idxStr := change.Path[1]
			var idx int
			if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil {
				if stepID, exists := stepIndexToID[idx]; exists {
					affectedStepIDs[stepID] = true
				}
			}
		}
	}

	// Check if step order changed
	stepOrderChanges := false
	if oldPlan != nil && newPlan != nil {
		if len(oldPlan.Steps) == len(newPlan.Steps) {
			for i := range oldPlan.Steps {
				if oldPlan.Steps[i].GetID() != newPlan.Steps[i].GetID() {
					stepOrderChanges = true
					break
				}
			}
		} else {
			stepOrderChanges = true
		}
	}

	// If no changes detected, return early
	if len(diffChangelog) == 0 && len(affectedStepIDs) == 0 && !stepOrderChanges {
		logger.Info("📝 No plan changes detected - skipping changelog generation")
		return nil
	}

	// Marshal diff.Changelog to JSON for storage
	diffJSON, err := json.Marshal(diffChangelog)
	if err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to marshal diff changelog: %v", err))
		diffJSON = []byte("[]")
	}

	// Determine change type and description
	changeType := "update"
	description := "Plan updated"
	stepIDsList := make([]string, 0, len(affectedStepIDs))
	for stepID := range affectedStepIDs {
		stepIDsList = append(stepIDsList, stepID)
	}
	sort.Strings(stepIDsList)

	if len(deletedSteps) > 0 && len(addedSteps) > 0 {
		changeType = "update"
		description = fmt.Sprintf("Updated plan: %d change(s), %d step(s) added, %d deleted", len(diffChangelog), len(addedSteps), len(deletedSteps))
	} else if len(deletedSteps) > 0 {
		changeType = "delete"
		description = fmt.Sprintf("Deleted %d step(s) from plan", len(deletedSteps))
	} else if len(addedSteps) > 0 {
		changeType = "add"
		description = fmt.Sprintf("Added %d step(s) to plan", len(addedSteps))
	} else if len(diffChangelog) > 0 {
		changeType = "update"
		description = fmt.Sprintf("Updated plan: %d change(s) detected", len(diffChangelog))
	}

	if stepOrderChanges && len(stepIDsList) == 0 {
		changeType = "update"
		description = "Plan step order changed"
	}

	// Create changelog entry
	detailsJSON, _ := json.Marshal(map[string]interface{}{
		"affected_step_ids":  stepIDsList,
		"added_count":        len(addedSteps),
		"deleted_count":      len(deletedSteps),
		"updated_count":      len(stepIDsList) - len(addedSteps) - len(deletedSteps),
		"order_changed":      stepOrderChanges,
		"total_diff_changes": len(diffChangelog),
	})

	changelogEntry := PlanChangeLogEntry{
		Timestamp:    time.Now().Format(time.RFC3339),
		ChangeType:   changeType,
		StepIDs:      stepIDsList,
		Description:  description,
		Details:      string(detailsJSON),
		DiffChanges:  diffJSON, // Store raw diff output directly
		AddedSteps:   addedSteps,
		DeletedSteps: deletedSteps,
	}

	// Write changelog entry
	if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write changelog entry: %w", err), nil)
	}

	logger.Info(fmt.Sprintf("📝 Generated changelog entry: %s - %d diff changes across %d step(s)", changeType, len(diffChangelog), len(stepIDsList)))
	return nil
}

// readChangelog reads all changelog files from planning/changelog/ directory and combines them
// Returns all entries sorted by timestamp (oldest first)
func readChangelog(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error), listFiles func(context.Context, string) ([]string, error)) (*PlanChangeLog, error) {
	changelogDir := filepath.Join(workspacePath, "planning", "changelog")

	// List all files in changelog directory
	files, err := listFiles(ctx, changelogDir)
	if err != nil {
		// Directory doesn't exist or can't be read, return empty changelog
		return &PlanChangeLog{Entries: []PlanChangeLogEntry{}}, nil
	}

	// Filter to only changelog-*.json files
	changelogFiles := make([]string, 0)
	for _, file := range files {
		if strings.HasPrefix(file, "changelog-") && strings.HasSuffix(file, ".json") {
			changelogFiles = append(changelogFiles, file)
		}
	}

	if len(changelogFiles) == 0 {
		// No changelog files found
		return &PlanChangeLog{Entries: []PlanChangeLogEntry{}}, nil
	}

	// Read all changelog files and combine entries
	// Each file now contains a PlanChangeLog with multiple entries (session-based)
	allEntries := make([]PlanChangeLogEntry, 0)
	for _, filename := range changelogFiles {
		filePath := filepath.Join(changelogDir, filename)
		content, err := readFile(ctx, filePath)
		if err != nil {
			// Skip files that can't be read
			continue
		}

		// Try to unmarshal as PlanChangeLog (new format - session file with multiple entries)
		var changelog PlanChangeLog
		if err := json.Unmarshal([]byte(content), &changelog); err == nil {
			// Successfully parsed as PlanChangeLog - add all entries
			allEntries = append(allEntries, changelog.Entries...)
		} else {
			// Try old format (single entry per file) for backward compatibility
			var entry PlanChangeLogEntry
			if err := json.Unmarshal([]byte(content), &entry); err == nil {
				allEntries = append(allEntries, entry)
			}
			// If both fail, skip the file
		}
	}

	// Sort entries by timestamp (oldest first)
	sort.Slice(allEntries, func(i, j int) bool {
		timeI, errI := time.Parse(time.RFC3339, allEntries[i].Timestamp)
		timeJ, errJ := time.Parse(time.RFC3339, allEntries[j].Timestamp)
		if errI != nil || errJ != nil {
			// If parsing fails, keep original order
			return i < j
		}
		return timeI.Before(timeJ)
	})

	return &PlanChangeLog{Entries: allEntries}, nil
}

// getUpdateRegularStepSchema returns the JSON schema for update_regular_step tool
func getUpdateRegularStepSchema() string {
	return `{
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
						},
						"enable_prerequisite_detection": {
							"type": "boolean",
							"description": "OPTIONAL: Updated enable_prerequisite_detection flag. Only include if you want to change it. Set to true when this step depends on outputs from previous steps that might expire or become invalid (e.g., login sessions, API tokens, config files). If omitted, the existing value is preserved."
						},
						"prerequisite_rules": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"depends_on_step": {
										"type": "string",
										"description": "REQUIRED: The step ID this rule depends on. Must be a step that appears earlier in the plan."
									},
									"description": {
										"type": "string",
										"description": "REQUIRED: Natural language description of when to detect prerequisite failures for this specific step. Examples: 'If login session is missing or expired, go back to step 0', 'If config file is missing, go back to step 1'."
									}
								},
								"required": ["depends_on_step", "description"]
							},
							"description": "OPTIONAL: Updated prerequisite rules array. Only include if you want to change it. Each rule specifies one step dependency and one description of when to detect prerequisite failures. If omitted, the existing prerequisite rules are preserved."
						}
					},
					"required": ["existing_step_id"]
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

// getAddRegularStepSchema returns the JSON schema for add_regular_step tool
func getAddRegularStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this new step. Generate a unique, URL-friendly ID based on the step title (e.g., 'deploy-application' from 'Deploy Application')."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the new step"
			},
			"description": {
				"type": "string",
				"description": "REQUIRED: COMPREHENSIVE, DETAILED description of what this step accomplishes. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context."
			},
			"success_criteria": {
				"type": "string",
				"description": "REQUIRED: Detailed explanation of how to verify this step was completed successfully. Focus on EXECUTION-BASED validation - what work was actually done, not just file structure. Pre-validation handles file/field existence checks automatically. Your criteria should describe: (1) What evidence proves the execution agent actually performed the work (e.g., 'Agent read source files and processed data', 'Agent made API calls and received responses', 'Agent transformed data according to business rules'). (2) What outcomes demonstrate successful execution (e.g., 'Data was correctly transformed', 'All required operations completed', 'External system was updated'). (3) Evidence that can be verified against execution history (tool calls, file reads, data transformations). GOOD EXAMPLES: 'Execution history shows agent read source_data.json, processed all entries, and created transformed_data.json with correct structure', 'Agent successfully authenticated with API (tool calls show auth requests), retrieved data, and wrote results.json', 'Agent verified data integrity by reading source files, computing checksums, and comparing values'. BAD EXAMPLES (avoid): 'File contains status: success' (too vague, can be faked), 'File exists' (pre-validation handles this), 'All fields present' (pre-validation handles this)."
			},
			"context_dependencies": {
				"type": "array",
				"items": { "type": "string" },
				"description": "REQUIRED: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "REQUIRED: What context file this step will create for subsequent steps - e.g., 'step_1_results.json'. IMPORTANT: Execution agents work ONLY in the execution folder, so context output files will be written to the execution folder."
			},
			"has_loop": {
				"type": "boolean",
				"description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."
			},
			"loop_condition": {
				"type": "string",
				"description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."
			},
			"max_iterations": {
				"type": "integer",
				"description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."
			},
			"loop_description": {
				"type": "string",
				"description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."
			},
			"enable_prerequisite_detection": {
				"type": "boolean",
				"description": "OPTIONAL: Enable prerequisite failure detection for this step. Set to true when this step depends on outputs from previous steps that might expire or become invalid (e.g., login sessions, API tokens, config files). Default: false."
			},
			"prerequisite_rules": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"depends_on_step": {
							"type": "string",
							"description": "REQUIRED: The step ID this rule depends on. Must be a step that appears earlier in the plan."
						},
						"description": {
							"type": "string",
							"description": "REQUIRED: Natural language description of when to detect prerequisite failures for this specific step. Examples: 'If login session is missing or expired, go back to step 0', 'If config file is missing, go back to step 1'."
						}
					},
					"required": ["depends_on_step", "description"]
				},
				"description": "OPTIONAL: Array of prerequisite rules. Each rule specifies one step dependency and one description of when to detect prerequisite failures. Only include if enable_prerequisite_detection is true."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			},
			"validation_schema": {
				"type": "object",
				"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. You MUST generate this by parsing the success_criteria and extracting file names, field requirements, and validation rules. This enables pre-validation before LLM validation (improves speed by 50-70%). Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath like $.field_name), must_exist: boolean, value_type?: string (string/number/boolean/array/object), min_length?: number, max_length?: number, pattern?: string (regex), min_value?: number, max_value?: number, consistency_check?: {type: string (array_length/equals/greater_than/less_than), compare_with_path: string}}]}]}. Example: If success_criteria mentions 'File results.json contains status field and count field equals items array length', generate schema with file_name: 'results.json', json_checks for $.status and $.count with consistency_check.",
				"properties": {
					"files": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file_name": {"type": "string"},
								"must_exist": {"type": "boolean"},
								"json_checks": {
									"type": "array",
									"items": {
										"type": "object",
										"properties": {
											"path": {"type": "string"},
											"must_exist": {"type": "boolean"},
											"value_type": {"type": "string"},
											"min_length": {"type": "number"},
											"max_length": {"type": "number"},
											"pattern": {"type": "string"},
											"min_value": {"type": "number"},
											"max_value": {"type": "number"},
											"consistency_check": {
												"type": "object",
												"properties": {
													"type": {"type": "string"},
													"compare_with_path": {"type": "string"}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		},
		"required": ["id", "title", "description", "success_criteria", "context_dependencies", "context_output", "has_loop", "insert_after_step_id", "validation_schema"]
	}`
}

// getAddConditionalStepSchema returns the JSON schema for add_conditional_step tool
func getAddConditionalStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this conditional step. Generate a unique, URL-friendly ID based on the step title."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the conditional step"
			},
			"description": {
				"type": "string",
				"description": "OPTIONAL: Description of what this conditional step does. Can be empty string if not needed."
			},
			"success_criteria": {
				"type": "string",
				"description": "OPTIONAL: Success criteria for the conditional step wrapper. Can be empty string if not needed."
			},
			"context_dependencies": {
				"type": "array",
				"items": { "type": "string" },
				"description": "OPTIONAL: Context files from previous steps that this conditional step depends on. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "OPTIONAL: Context file this conditional step wrapper will create. Can be empty string if not needed."
			},
			"condition_question": {
				"type": "string",
				"description": "REQUIRED: Question to ask the ConditionalLLM for decision making (e.g., 'Is the deployment healthy?', 'Is user already logged in?')"
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
						"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular', 'conditional', 'decision', or 'orchestration'. Most branch steps are 'regular'."},
						"id": {"type": "string", "description": "REQUIRED: Stable step ID"},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"validation_schema": {
							"type": "object",
							"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. Generate by parsing success_criteria. Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath), must_exist: boolean, value_type?: string, min_length?: number, max_length?: number, pattern?: string, min_value?: number, max_value?: number, consistency_check?: {type: string, compare_with_path: string}}]}]}",
							"properties": {
								"files": {
									"type": "array",
									"items": {
										"type": "object",
										"properties": {
											"file_name": {"type": "string"},
											"must_exist": {"type": "boolean"},
											"json_checks": {
												"type": "array",
												"items": {
													"type": "object",
													"properties": {
														"path": {"type": "string"},
														"must_exist": {"type": "boolean"},
														"value_type": {"type": "string"},
														"min_length": {"type": "number"},
														"max_length": {"type": "number"},
														"pattern": {"type": "string"},
														"min_value": {"type": "number"},
														"max_value": {"type": "number"},
														"consistency_check": {
															"type": "object",
															"properties": {
																"type": {"type": "string"},
																"compare_with_path": {"type": "string"}
															}
														}
													}
												}
											}
										}
									}
								}
							}
						}
					},
					"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output", "validation_schema"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is true. Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Each step MUST include a 'type' field ('regular', 'conditional', 'decision', or 'orchestration') and an 'id' field."
			},
			"if_false_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular', 'conditional', 'decision', or 'orchestration'. Most branch steps are 'regular'."},
						"id": {"type": "string", "description": "REQUIRED: Stable step ID"},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"validation_schema": {
							"type": "object",
							"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. Generate by parsing success_criteria. Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath), must_exist: boolean, value_type?: string, min_length?: number, max_length?: number, pattern?: string, min_value?: number, max_value?: number, consistency_check?: {type: string, compare_with_path: string}}]}]}",
							"properties": {
								"files": {
									"type": "array",
									"items": {
										"type": "object",
										"properties": {
											"file_name": {"type": "string"},
											"must_exist": {"type": "boolean"},
											"json_checks": {
												"type": "array",
												"items": {
													"type": "object",
													"properties": {
														"path": {"type": "string"},
														"must_exist": {"type": "boolean"},
														"value_type": {"type": "string"},
														"min_length": {"type": "number"},
														"max_length": {"type": "number"},
														"pattern": {"type": "string"},
														"min_value": {"type": "number"},
														"max_value": {"type": "number"},
														"consistency_check": {
															"type": "object",
															"properties": {
																"type": {"type": "string"},
																"compare_with_path": {"type": "string"}
															}
														}
													}
												}
											}
										}
									}
								}
							}
						}
					},
					"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output", "validation_schema"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is false. Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Each step MUST include a 'type' field ('regular', 'conditional', 'decision', or 'orchestration') and an 'id' field."
			},
			"if_true_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: ID of step to connect to after true branch completes, or 'end' to end the workflow. REQUIRED when if_true_steps is empty []. Optional when if_true_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the plan, or 'end' to terminate."
			},
			"if_false_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: ID of step to connect to after false branch completes, or 'end' to end the workflow. REQUIRED when if_false_steps is empty []. Optional when if_false_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the plan, or 'end' to terminate."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "condition_question", "if_true_steps", "if_false_steps", "insert_after_step_id"]
	}`
}

// getAddDecisionStepSchema returns the JSON schema for add_decision_step tool
func getAddDecisionStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this decision step. Generate a unique, URL-friendly ID based on the step title."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the decision step"
			},
			"decision_step": {
				"type": "object",
				"description": "REQUIRED: The single step to execute. Must include all required fields. This is typically a 'regular' step type.",
				"properties": {
					"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular', 'conditional', 'decision', or 'orchestration'. For decision_step, typically use 'regular'."},
					"id": {"type": "string", "description": "REQUIRED: Stable step ID for the decision step"},
					"title": {"type": "string", "description": "REQUIRED: Title of the decision step"},
					"description": {"type": "string", "description": "REQUIRED: Description of what the decision step does"},
					"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the decision step completed successfully"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}, "description": "REQUIRED: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."},
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
					"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
					"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"validation_schema": {
						"type": "object",
						"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. Generate by parsing success_criteria. Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath), must_exist: boolean, value_type?: string, min_length?: number, max_length?: number, pattern?: string, min_value?: number, max_value?: number, consistency_check?: {type: string, compare_with_path: string}}]}]}",
						"properties": {
							"files": {
								"type": "array",
								"items": {
									"type": "object",
									"properties": {
										"file_name": {"type": "string"},
										"must_exist": {"type": "boolean"},
										"json_checks": {
											"type": "array",
											"items": {
												"type": "object",
												"properties": {
													"path": {"type": "string"},
													"must_exist": {"type": "boolean"},
													"value_type": {"type": "string"},
													"min_length": {"type": "number"},
													"max_length": {"type": "number"},
													"pattern": {"type": "string"},
													"min_value": {"type": "number"},
													"max_value": {"type": "number"},
													"consistency_check": {
														"type": "object",
														"properties": {
															"type": {"type": "string"},
															"compare_with_path": {"type": "string"}
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}
				},
				"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output", "validation_schema"]
			},
			"decision_evaluation_question": {
				"type": "string",
				"description": "REQUIRED: Question to evaluate the decision step's execution output (e.g., 'Is the deployment healthy and all services running?')"
			},
			"if_true_next_step_id": {
				"type": "string",
				"description": "REQUIRED: ID of step to route to after evaluation is true. Use step's id field from the plan, or 'end' to terminate workflow."
			},
			"if_false_next_step_id": {
				"type": "string",
				"description": "REQUIRED: ID of step to route to after evaluation is false. Use step's id field from the plan, or 'end' to terminate workflow."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "decision_step", "decision_evaluation_question", "if_true_next_step_id", "if_false_next_step_id", "insert_after_step_id"]
	}`
}

// getAddOrchestrationStepSchema returns the JSON schema for add_orchestration_step tool
func getAddOrchestrationStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this orchestration step. Generate a unique, URL-friendly ID based on the step title."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the orchestration step"
			},
			"orchestration_step": {
				"type": "object",
				"description": "REQUIRED: The main orchestrator step to execute. Must include all required fields. This is typically a 'regular' step type.",
				"properties": {
					"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular', 'conditional', 'decision', or 'orchestration'. For orchestration_step, typically use 'regular'."},
					"id": {"type": "string", "description": "REQUIRED: Stable step ID for the orchestration orchestrator step"},
					"title": {"type": "string", "description": "REQUIRED: Title of the orchestration orchestrator step"},
					"description": {"type": "string", "description": "REQUIRED: Description of what the orchestration orchestrator step does"},
					"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the orchestration orchestrator step completed successfully"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}, "description": "REQUIRED: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."},
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
					"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
					"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."}
				},
				"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output"]
			},
			"orchestration_routes": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"route_id": {
							"type": "string",
							"description": "REQUIRED: Unique ID for this route (e.g., 'auth-error-handler', 'network-error-handler')"
						},
						"route_name": {
							"type": "string",
							"description": "REQUIRED: Human-readable name for this route (e.g., 'Authentication Error Handler', 'Network Error Handler')"
						},
						"condition": {
							"type": "string",
							"description": "REQUIRED: Condition description for when this route should be selected (e.g., 'If error is authentication-related', 'If error is network-related')"
						},
						"sub_agent_step": {
							"type": "object",
							"description": "REQUIRED: The sub-agent step to execute for this route. Must include all required fields. This is typically a 'regular' step type.",
							"properties": {
								"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular', 'conditional', 'decision', or 'orchestration'. For sub_agent_step, typically use 'regular'."},
								"id": {"type": "string", "description": "REQUIRED: Stable step ID for the sub-agent step"},
								"title": {"type": "string", "description": "REQUIRED: Title of the sub-agent step"},
								"description": {"type": "string", "description": "REQUIRED: Description of what the sub-agent step does"},
								"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the sub-agent step completed successfully"},
								"context_dependencies": {"type": "array", "items": {"type": "string"}},
								"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
								"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
								"loop_condition": {"type": "string"},
								"max_iterations": {"type": "integer"},
								"loop_description": {"type": "string"},
								"validation_schema": {
									"type": "object",
									"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. Generate by parsing success_criteria. Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath), must_exist: boolean, value_type?: string, min_length?: number, max_length?: number, pattern?: string, min_value?: number, max_value?: number, consistency_check?: {type: string, compare_with_path: string}}]}]}",
									"properties": {
										"files": {
											"type": "array",
											"items": {
												"type": "object",
												"properties": {
													"file_name": {"type": "string"},
													"must_exist": {"type": "boolean"},
													"json_checks": {
														"type": "array",
														"items": {
															"type": "object",
															"properties": {
																"path": {"type": "string"},
																"must_exist": {"type": "boolean"},
																"value_type": {"type": "string"},
																"min_length": {"type": "number"},
																"max_length": {"type": "number"},
																"pattern": {"type": "string"},
																"min_value": {"type": "number"},
																"max_value": {"type": "number"},
																"consistency_check": {
																	"type": "object",
																	"properties": {
																		"type": {"type": "string"},
																		"compare_with_path": {"type": "string"}
																	}
																}
															}
														}
													}
												}
											}
										}
									}
								}
							},
							"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output", "validation_schema"]
						},
						"context_to_pass": {
							"type": "string",
							"description": "OPTIONAL: Specific context to pass to the sub-agent (e.g., 'Focus on authentication errors only')"
						}
					},
					"required": ["route_id", "route_name", "condition", "sub_agent_step"]
				},
				"description": "REQUIRED: Array of possible routes with their conditions and sub-agent steps. Must have at least one route. You can include a route with route_id: \"end\" to allow the orchestrator to terminate the workflow - this route should have route_name: \"End Workflow\" and a condition describing when to end (e.g., \"If objective is complete and no further work is needed\"). The sub_agent_step for \"end\" route can be minimal (title and description are sufficient)."
			},
			"next_step_id": {
				"type": "string",
				"description": "REQUIRED: ID of step to connect to after orchestration completes, or 'end' to end the workflow. Use step's id field from the plan, or 'end' to terminate."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "orchestration_step", "orchestration_routes", "next_step_id", "insert_after_step_id"]
	}`
}

// getAddLoopStepSchema returns the JSON schema for add_loop_step tool
func getAddLoopStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this loop step. Generate a unique, URL-friendly ID based on the step title."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the loop step"
			},
			"description": {
				"type": "string",
				"description": "REQUIRED: COMPREHENSIVE, DETAILED description of what this step accomplishes in each iteration."
			},
			"success_criteria": {
				"type": "string",
				"description": "REQUIRED: Detailed explanation of how to verify this step was completed successfully - must be file-verifiable."
			},
			"context_dependencies": {
				"type": "array",
				"items": { "type": "string" },
				"description": "REQUIRED: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "REQUIRED: What context file this step will create for subsequent steps - e.g., 'step_1_results.json'."
			},
			"loop_condition": {
				"type": "string",
				"description": "REQUIRED: Condition that must be met to exit the loop. This should be the same as success_criteria and must be file-verifiable."
			},
			"max_iterations": {
				"type": "integer",
				"description": "OPTIONAL: Maximum number of loop iterations allowed (default: 10). Recommended: 10 for normal operations, 20-50 for long-running operations, 3-5 for quick status checks."
			},
			"loop_description": {
				"type": "string",
				"description": "REQUIRED: Describe what happens in EACH ITERATION of the loop. Be specific about what the step does in each iteration."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "description", "success_criteria", "context_output", "loop_condition", "loop_description", "insert_after_step_id"]
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
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["id", "title", "description", "success_criteria", "context_dependencies", "has_loop"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is true. Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Each step MUST include an 'id' field. **IMPORTANT**: When if_true_steps is empty [], you MUST also provide if_true_next_step_id to specify which step comes next. Example: If checking 'Is user logged in?' and you want to skip to step 5 when true, use empty array [] and set if_true_next_step_id to 'step-5-id'."
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
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["id", "title", "description", "success_criteria", "context_dependencies", "has_loop"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is false. Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Each step MUST include an 'id' field. **IMPORTANT**: When if_false_steps is empty [], you MUST also provide if_false_next_step_id to specify which step comes next. Example: If checking 'Is user logged in?' and you want to execute login steps when false, provide [login_step1, login_step2]. If you want to skip when false, use empty array [] and set if_false_next_step_id to 'step-5-id'."
			},
			"if_true_next_step_id": {
				"type": "string",
				"description": "ID of step to connect to after true branch completes, or 'end' to end the workflow. REQUIRED when if_true_steps is empty []. Optional when if_true_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the main plan, or 'end' to terminate. Example: If conditional step is step 4 and you want to go to step 5 after true branch, set if_true_next_step_id to 'step-5-id'. If you want to end the workflow, set to 'end'."
			},
			"if_false_next_step_id": {
				"type": "string",
				"description": "ID of step to connect to after false branch completes, or 'end' to end the workflow. REQUIRED when if_false_steps is empty []. Optional when if_false_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the main plan, or 'end' to terminate. Example: If conditional step is step 4 and you want to go to step 5 after false branch, set if_false_next_step_id to 'step-5-id'. If you want to end the workflow, set to 'end'."
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
						"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular', 'conditional', 'decision', or 'orchestration'. Most branch steps are 'regular'."},
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this branch step. Generate a unique, URL-friendly ID based on the step title (e.g., 'verify-deployment-health' from 'Verify Deployment Health')."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop"]
				},
				"description": "REQUIRED: New steps to add to the specified branch. Provide complete step definitions with IDs. Each step MUST include a 'type' field ('regular', 'conditional', 'decision', or 'orchestration')."
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
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
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
			"existing_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step to update. Use the step's id field from the plan."
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
				"description": "OPTIONAL: Updated has_loop flag. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. Always set to false. If omitted, the existing has_loop value is preserved."
			},
			"loop_condition": {
				"type": "string",
				"description": "OPTIONAL: Updated loop condition. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing loop condition is preserved."
			},
			"max_iterations": {
				"type": "integer",
				"description": "OPTIONAL: Updated max iterations. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing max iterations is preserved."
			},
			"loop_description": {
				"type": "string",
				"description": "OPTIONAL: Updated loop description. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing loop description is preserved."
			},
			"enable_prerequisite_detection": {
				"type": "boolean",
				"description": "OPTIONAL: Updated enable_prerequisite_detection flag. Only include if you want to change it. Set to true when this step depends on outputs from previous steps that might expire or become invalid (e.g., login sessions, API tokens, config files). If omitted, the existing value is preserved."
			},
			"prerequisite_rules": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"depends_on_step": {
							"type": "string",
							"description": "REQUIRED: The step ID this rule depends on. Must be a step that appears earlier in the plan."
						},
						"description": {
							"type": "string",
							"description": "REQUIRED: Natural language description of when to detect prerequisite failures for this specific step. Examples: 'If login session is missing or expired, go back to step 0', 'If config file is missing, go back to step 1'."
						}
					},
					"required": ["depends_on_step", "description"]
				},
				"description": "OPTIONAL: Updated prerequisite rules array. Only include if you want to change it. Each rule specifies one step dependency and one description of when to detect prerequisite failures. If omitted, the existing prerequisite rules are preserved."
			},
			"condition_question": {
				"type": "string",
				"description": "OPTIONAL: Updated condition question. Only include if you want to change it. Question to ask the ConditionalLLM for decision making (e.g., 'Is the deployment healthy?', 'Is user already logged in?'). If omitted, the existing question is preserved."
			},
			"condition_context": {
				"type": "string",
				"description": "OPTIONAL: Updated condition context. Only include if you want to change it. Context to provide to ConditionalLLM (e.g., context files, status information). Can be empty string to clear existing context. If omitted, the existing context is preserved."
			},
			"if_true_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular', 'conditional', 'decision', or 'orchestration'. Most branch steps are 'regular'."},
						"id": {"type": "string", "description": "REQUIRED: Stable step ID"},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop"]
				},
				"description": "OPTIONAL: Updated if_true_steps array. Only include if you want to change it. Array of steps to execute if condition is true. Can be empty array [] to clear all steps. Each step MUST include a 'type' field ('regular', 'conditional', 'decision', or 'orchestration'). If omitted, the existing if_true_steps are preserved."
			},
			"if_false_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular', 'conditional', 'decision', or 'orchestration'. Most branch steps are 'regular'."},
						"id": {"type": "string", "description": "REQUIRED: Stable step ID"},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop"]
				},
				"description": "OPTIONAL: Updated if_false_steps array. Only include if you want to change it. Array of steps to execute if condition is false. Can be empty array [] to clear all steps. Each step MUST include a 'type' field ('regular', 'conditional', 'decision', or 'orchestration'). If omitted, the existing if_false_steps are preserved."
			},
			"if_true_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: Updated if_true_next_step_id. Only include if you want to change it. ID of step to connect to after true branch completes (REQUIRED when if_true_steps is empty []). Use step's id field from the plan, or 'end' to terminate. If omitted, the existing value is preserved."
			},
			"if_false_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: Updated if_false_next_step_id. Only include if you want to change it. ID of step to connect to after false branch completes (REQUIRED when if_false_steps is empty []). Use step's id field from the plan, or 'end' to terminate. If omitted, the existing value is preserved."
			}
		},
		"required": ["existing_step_id"]
	}`
}

// getUpdateDecisionStepSchema returns the JSON schema for update_decision_step tool
func getUpdateDecisionStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the decision step to update. Use the step's id field from the plan."
			},
			"title": {
				"type": "string",
				"description": "OPTIONAL: New title for the step. Only include if you want to rename the step. If omitted, the existing title is preserved."
			},
			"description": {
				"type": "string",
				"description": "OPTIONAL: Updated description. Only include if you want to change the description. If omitted, the existing description is preserved. NOTE: For decision steps (has_decision_step=true), this field is NOT used during execution - only decision_step.description is used. Do not update this field for decision steps."
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
				"description": "OPTIONAL: Updated has_loop flag. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. Always set to false. If omitted, the existing has_loop value is preserved."
			},
			"loop_condition": {
				"type": "string",
				"description": "OPTIONAL: Updated loop condition. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing loop condition is preserved."
			},
			"max_iterations": {
				"type": "integer",
				"description": "OPTIONAL: Updated max iterations. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing max iterations is preserved."
			},
			"loop_description": {
				"type": "string",
				"description": "OPTIONAL: Updated loop description. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing loop description is preserved."
			},
			"enable_prerequisite_detection": {
				"type": "boolean",
				"description": "OPTIONAL: Updated enable_prerequisite_detection flag. Only include if you want to change it. Set to true when this step depends on outputs from previous steps that might expire or become invalid (e.g., login sessions, API tokens, config files). If omitted, the existing value is preserved."
			},
			"prerequisite_rules": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"depends_on_step": {
							"type": "string",
							"description": "REQUIRED: The step ID this rule depends on. Must be a step that appears earlier in the plan."
						},
						"description": {
							"type": "string",
							"description": "REQUIRED: Natural language description of when to detect prerequisite failures for this specific step. Examples: 'If login session is missing or expired, go back to step 0', 'If config file is missing, go back to step 1'."
						}
					},
					"required": ["depends_on_step", "description"]
				},
				"description": "OPTIONAL: Updated prerequisite rules array. Only include if you want to change it. Each rule specifies one step dependency and one description of when to detect prerequisite failures. If omitted, the existing prerequisite rules are preserved."
			},
			"decision_step": {
				"type": "object",
				"description": "OPTIONAL: Updated decision step. Only include if you want to change it. The single step to execute. Must include all required fields: id, title, description, success_criteria, has_loop, context_output. If omitted, the existing decision step is preserved.",
				"properties": {
					"id": {"type": "string", "description": "REQUIRED: Stable step ID for the decision step"},
					"title": {"type": "string", "description": "REQUIRED: Title of the decision step"},
					"description": {"type": "string", "description": "REQUIRED: Description of what the decision step does"},
					"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the decision step completed successfully"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}},
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
					"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
					"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."}
				},
				"required": ["id", "title", "description", "success_criteria", "has_loop", "context_output"]
			},
			"decision_evaluation_question": {
				"type": "string",
				"description": "OPTIONAL: Updated decision evaluation question. Only include if you want to change it. Question to evaluate the decision step's execution output (e.g., 'Is the deployment healthy and all services running?'). If omitted, the existing question is preserved."
			},
			"if_true_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: Updated if_true_next_step_id. Only include if you want to change it. ID of step to route to after evaluation is true. Use step's id field from the plan, or 'end' to terminate. If omitted, the existing value is preserved."
			},
			"if_false_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: Updated if_false_next_step_id. Only include if you want to change it. ID of step to route to after evaluation is false. Use step's id field from the plan, or 'end' to terminate. If omitted, the existing value is preserved."
			}
		},
		"required": ["existing_step_id"]
	}`
}

// getUpdateOrchestrationStepSchema returns the JSON schema for update_orchestration_step tool
func getUpdateOrchestrationStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the orchestration step to update. Use the step's id field from the plan."
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
				"description": "OPTIONAL: Updated has_loop flag. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. Always set to false. If omitted, the existing has_loop value is preserved."
			},
			"loop_condition": {
				"type": "string",
				"description": "OPTIONAL: Updated loop condition. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing loop condition is preserved."
			},
			"max_iterations": {
				"type": "integer",
				"description": "OPTIONAL: Updated max iterations. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing max iterations is preserved."
			},
			"loop_description": {
				"type": "string",
				"description": "OPTIONAL: Updated loop description. Only include if you want to change it. NOTE: Loop support is currently not implemented in agents. This field is ignored. If omitted, the existing loop description is preserved."
			},
			"enable_prerequisite_detection": {
				"type": "boolean",
				"description": "OPTIONAL: Updated enable_prerequisite_detection flag. Only include if you want to change it. Set to true when this step depends on outputs from previous steps that might expire or become invalid (e.g., login sessions, API tokens, config files). If omitted, the existing value is preserved."
			},
			"prerequisite_rules": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"depends_on_step": {
							"type": "string",
							"description": "REQUIRED: The step ID this rule depends on. Must be a step that appears earlier in the plan."
						},
						"description": {
							"type": "string",
							"description": "REQUIRED: Natural language description of when to detect prerequisite failures for this specific step. Examples: 'If login session is missing or expired, go back to step 0', 'If config file is missing, go back to step 1'."
						}
					},
					"required": ["depends_on_step", "description"]
				},
				"description": "OPTIONAL: Updated prerequisite rules array. Only include if you want to change it. Each rule specifies one step dependency and one description of when to detect prerequisite failures. If omitted, the existing prerequisite rules are preserved."
			},
			"orchestration_step": {
				"type": "object",
				"description": "OPTIONAL: Updated orchestration step. Only include if you want to change it. The main orchestrator step to execute. Must include all required fields: id, title, description, success_criteria, has_loop, context_output. If omitted, the existing orchestration step is preserved.",
				"properties": {
					"id": {"type": "string", "description": "REQUIRED: Stable step ID for the orchestration orchestrator step"},
					"title": {"type": "string", "description": "REQUIRED: Title of the orchestration orchestrator step"},
					"description": {"type": "string", "description": "REQUIRED: Description of what the orchestration orchestrator step does"},
					"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the orchestration orchestrator step completed successfully"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}},
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
					"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
					"loop_condition": {"type": "string", "description": "OPTIONAL: Condition that must be met to exit the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"max_iterations": {"type": "integer", "description": "OPTIONAL: Maximum number of loop iterations allowed. NOTE: Loop support is currently not implemented in agents. This field is ignored."},
					"loop_description": {"type": "string", "description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. NOTE: Loop support is currently not implemented in agents. This field is ignored."}
				},
				"required": ["id", "title", "description", "success_criteria", "has_loop", "context_output"]
			},
			"orchestration_routes": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"route_id": {"type": "string", "description": "REQUIRED: Unique ID for this route"},
						"route_name": {"type": "string", "description": "REQUIRED: Human-readable name for this route"},
						"condition": {"type": "string", "description": "REQUIRED: Condition description for when this route should be selected"},
						"sub_agent_step": {
							"type": "object",
							"description": "REQUIRED: The sub-agent step to execute for this route",
							"properties": {
								"id": {"type": "string", "description": "REQUIRED: Stable step ID for the sub-agent step"},
								"title": {"type": "string", "description": "REQUIRED: Title of the sub-agent step"},
								"description": {"type": "string", "description": "REQUIRED: Description of what the sub-agent step does"},
								"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the sub-agent step completed successfully"},
								"context_dependencies": {"type": "array", "items": {"type": "string"}},
								"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
								"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
								"loop_condition": {"type": "string"},
								"max_iterations": {"type": "integer"},
								"loop_description": {"type": "string"}
							},
							"required": ["id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output"]
						},
						"context_to_pass": {"type": "string", "description": "OPTIONAL: Specific context to pass to the sub-agent"}
					},
					"required": ["route_id", "route_name", "condition", "sub_agent_step"]
				},
				"description": "OPTIONAL: Updated orchestration routes array. Only include if you want to change it. Array of possible routes with their conditions and sub-agent steps. Must have at least one route. If omitted, the existing routes are preserved."
			},
			"next_step_id": {
				"type": "string",
				"description": "OPTIONAL: Updated next_step_id. Only include if you want to change it. ID of step to route to after orchestration completes. Use step's id field from the plan, or 'end' to terminate. If omitted, the existing value is preserved."
			}
		},
		"required": ["existing_step_id"]
	}`
}

// getAddOrchestrationRouteSchema returns the JSON schema for add_orchestration_route tool
func getAddOrchestrationRouteSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the orchestration step (parent step). Use the step's id field from the plan."
			},
			"new_route": {
				"type": "object",
				"description": "REQUIRED: The new orchestration route to add. Must include all required fields.",
				"properties": {
					"route_id": {
						"type": "string",
						"description": "REQUIRED: Unique ID for this route (e.g., 'auth-error-handler', 'network-error-handler', 'end')"
					},
					"route_name": {
						"type": "string",
						"description": "REQUIRED: Human-readable name for this route (e.g., 'Authentication Error Handler', 'Network Error Handler', 'End Workflow')"
					},
					"condition": {
						"type": "string",
						"description": "REQUIRED: Condition description for when this route should be selected (e.g., 'If error is authentication-related', 'If objective is complete and no further work is needed')"
					},
					"sub_agent_step": {
						"type": "object",
						"description": "REQUIRED: The sub-agent step to execute for this route. Must include all required fields.",
						"properties": {
							"id": {"type": "string", "description": "REQUIRED: Stable step ID for the sub-agent step"},
							"title": {"type": "string", "description": "REQUIRED: Title of the sub-agent step"},
							"description": {"type": "string", "description": "REQUIRED: Description of what the sub-agent step does"},
							"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the sub-agent step completed successfully"},
							"context_dependencies": {"type": "array", "items": {"type": "string"}},
							"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
							"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
							"loop_condition": {"type": "string"},
							"max_iterations": {"type": "integer"},
							"loop_description": {"type": "string"}
						},
						"required": ["id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output"]
					},
					"context_to_pass": {
						"type": "string",
						"description": "OPTIONAL: Specific context to pass to the sub-agent (e.g., 'Focus on authentication errors only')"
					}
				},
				"required": ["route_id", "route_name", "condition", "sub_agent_step"]
			}
		},
		"required": ["parent_step_id", "new_route"]
	}`
}

// getUpdateOrchestrationRouteSchema returns the JSON schema for update_orchestration_route tool
func getUpdateOrchestrationRouteSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the orchestration step (parent step). Use the step's id field from the plan."
			},
			"existing_route_id": {
				"type": "string",
				"description": "REQUIRED: The route_id of the route to update. Use the route's route_id field from the plan."
			},
			"route_name": {
				"type": "string",
				"description": "OPTIONAL: Updated route name. Only include if you want to change it. If omitted, the existing route name is preserved."
			},
			"condition": {
				"type": "string",
				"description": "OPTIONAL: Updated condition description. Only include if you want to change it. If omitted, the existing condition is preserved."
			},
			"sub_agent_step": {
				"type": "object",
				"description": "OPTIONAL: Updated sub-agent step. Only include if you want to change it. Must include all required fields: id, title, description, success_criteria, context_dependencies, has_loop, context_output. If omitted, the existing sub-agent step is preserved.",
				"properties": {
					"id": {"type": "string", "description": "REQUIRED: Stable step ID for the sub-agent step"},
					"title": {"type": "string", "description": "REQUIRED: Title of the sub-agent step"},
					"description": {"type": "string", "description": "REQUIRED: Description of what the sub-agent step does"},
					"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the sub-agent step completed successfully"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}},
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
					"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
					"loop_condition": {"type": "string"},
					"max_iterations": {"type": "integer"},
					"loop_description": {"type": "string"}
				},
				"required": ["id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output"]
			},
			"context_to_pass": {
				"type": "string",
				"description": "OPTIONAL: Updated context to pass to the sub-agent. Only include if you want to change it. If omitted, the existing context_to_pass is preserved."
			}
		},
		"required": ["parent_step_id", "existing_route_id"]
	}`
}

// getDeleteOrchestrationRouteSchema returns the JSON schema for delete_orchestration_route tool
func getDeleteOrchestrationRouteSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the orchestration step (parent step). Use the step's id field from the plan."
			},
			"deleted_route_id": {
				"type": "string",
				"description": "REQUIRED: The route_id of the route to delete. Use the route's route_id field from the plan. NOTE: You must have at least one route remaining after deletion."
			}
		},
		"required": ["parent_step_id", "deleted_route_id"]
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

// getUpdateValidationSchemaSchema returns the JSON schema for update_validation_schema tool
func getUpdateValidationSchemaSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step in the existing plan that you want to update. Use the step's id field from the plan."
			},
			"validation_schema": {
				"type": "object",
				"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. You MUST generate this by parsing the success_criteria and extracting file names, field requirements, and validation rules. This enables pre-validation before LLM validation (improves speed by 50-70%). Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath like $.field_name), must_exist: boolean, value_type?: string (string/number/boolean/array/object), min_length?: number, max_length?: number, pattern?: string (regex), min_value?: number, max_value?: number, consistency_check?: {type: string (array_length/equals/greater_than/less_than), compare_with_path: string}}]}]}. Example: If success_criteria mentions 'File results.json contains status field and count field equals items array length', generate schema with file_name: 'results.json', json_checks for $.status and $.count with consistency_check.",
				"properties": {
					"files": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file_name": {"type": "string"},
								"must_exist": {"type": "boolean"},
								"json_checks": {
									"type": "array",
									"items": {
										"type": "object",
										"properties": {
											"path": {"type": "string"},
											"must_exist": {"type": "boolean"},
											"value_type": {"type": "string"},
											"min_length": {"type": "number"},
											"max_length": {"type": "number"},
											"pattern": {"type": "string"},
											"min_value": {"type": "number"},
											"max_value": {"type": "number"},
											"consistency_check": {
												"type": "object",
												"properties": {
													"type": {"type": "string"},
													"compare_with_path": {"type": "string"}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		},
		"required": ["existing_step_id", "validation_schema"]
	}`
}

// getUpdateSuccessCriteriaSchema returns the JSON schema for update_success_criteria tool
func getUpdateSuccessCriteriaSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step in the existing plan that you want to update. Use the step's id field from the plan."
			},
			"success_criteria": {
				"type": "string",
				"description": "REQUIRED: Detailed explanation of how to verify this step was completed successfully. Focus on EXECUTION-BASED validation - what work was actually done, not just file structure. Pre-validation handles file/field existence checks automatically. Your criteria should describe: (1) What evidence proves the execution agent actually performed the work (e.g., 'Agent read source files and processed data', 'Agent made API calls and received responses', 'Agent transformed data according to business rules'). (2) What outcomes demonstrate successful execution (e.g., 'Data was correctly transformed', 'All required operations completed', 'External system was updated'). (3) Evidence that can be verified against execution history (tool calls, file reads, data transformations). GOOD EXAMPLES: 'Execution history shows agent read source_data.json, processed all entries, and created transformed_data.json with correct structure', 'Agent successfully authenticated with API (tool calls show auth requests), retrieved data, and wrote results.json', 'Agent verified data integrity by reading source files, computing checksums, and comparing values'. BAD EXAMPLES (avoid): 'File contains status: success' (too vague, can be faked), 'File exists' (pre-validation handles this), 'All fields present' (pre-validation handles this)."
			}
		},
		"required": ["existing_step_id", "success_criteria"]
	}`
}

// readPlanFromFile reads plan.json from the workspace using BaseOrchestrator's ReadWorkspaceFile
func readPlanFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*PlanningResponse, error) {
	planPath := filepath.Join(workspacePath, "planning", "plan.json")

	planFileMutex.Lock()
	defer planFileMutex.Unlock()

	content, err := readFile(ctx, planPath)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to read plan.json: %w", err), nil)
	}

	var plan PlanningResponse
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse plan.json: %w", err), nil)
	}

	return &plan, nil
}

// writePlanToFile writes PlanningResponse to plan.json in the workspace using BaseOrchestrator's WriteWorkspaceFile
// Validates that all steps have IDs before saving (planning agent should always generate them)
func writePlanToFile(ctx context.Context, workspacePath string, plan *PlanningResponse, _ func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	planPath := filepath.Join(workspacePath, "planning", "plan.json")

	planFileMutex.Lock()
	defer planFileMutex.Unlock()

	// Validate that all steps have IDs (planning agent should always generate them)
	if err := validatePlanStepIDs(plan.Steps); err != nil {
		return fmt.Errorf(fmt.Sprintf("plan validation failed: %w", err), nil)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to marshal plan: %w", err), nil)
	}

	if err := writeFile(ctx, planPath, string(data)); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write plan.json: %w", err), nil)
	}

	return nil
}

// validateNestingDepth checks if the maximum nesting depth (2 levels) is exceeded
// Returns error if depth > 2, nil otherwise
func validateNestingDepth(step PlanStepInterface, currentDepth int) error {
	const maxDepth = 2
	if currentDepth > maxDepth {
		return fmt.Errorf(fmt.Sprintf("nesting depth exceeds maximum allowed depth of %d (current: %d)", maxDepth, currentDepth), nil)
	}

	// Check nested steps in branches (only ConditionalPlanStep has branches)
	if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
		for _, branchStep := range conditionalStep.IfTrueSteps {
			// Check if branch step is also conditional
			if _, isConditional := branchStep.(*ConditionalPlanStep); isConditional {
				if err := validateNestingDepth(branchStep, currentDepth+1); err != nil {
					return err
				}
			}
		}
		for _, branchStep := range conditionalStep.IfFalseSteps {
			// Check if branch step is also conditional
			if _, isConditional := branchStep.(*ConditionalPlanStep); isConditional {
				if err := validateNestingDepth(branchStep, currentDepth+1); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// convertMapToStep converts a map[string]interface{} to PlanStepInterface
// Uses the same logic as PlanningResponse.UnmarshalJSON
func convertMapToStep(stepMap map[string]interface{}) (PlanStepInterface, error) {
	// Check for type field
	stepType, ok := stepMap["type"].(string)
	if !ok || stepType == "" {
		return nil, fmt.Errorf("step is missing required 'type' field")
	}

	// Marshal to JSON and unmarshal into appropriate type
	stepJSON, err := json.Marshal(stepMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal step: %w", err)
	}

	var typedStep PlanStepInterface
	switch stepType {
	case "regular":
		var step RegularPlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse regular step: %w", err)
		}
		typedStep = &step
	case "conditional":
		var step ConditionalPlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse conditional step: %w", err)
		}
		typedStep = &step
	case "decision":
		var step DecisionPlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse decision step: %w", err)
		}
		typedStep = &step
	case "orchestration":
		var step OrchestrationPlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse orchestration step: %w", err)
		}
		typedStep = &step
	default:
		return nil, fmt.Errorf("unknown step type %q", stepType)
	}

	return typedStep, nil
}

// unmarshalStepFromJSON unmarshals a single step from JSON by checking its type field
// This is a helper function used by custom UnmarshalJSON methods for step types with nested steps
func unmarshalStepFromJSON(stepData json.RawMessage) (PlanStepInterface, error) {
	// Check for type field
	var stepWithType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(stepData, &stepWithType); err != nil {
		return nil, fmt.Errorf("failed to parse step type: %w", err)
	}

	if stepWithType.Type == "" {
		return nil, fmt.Errorf("step is missing required 'type' field (must be: regular, conditional, decision, or orchestration)")
	}

	// Unmarshal based on type
	var typedStep PlanStepInterface
	switch stepWithType.Type {
	case "regular":
		var step RegularPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse regular step: %w", err)
		}
		typedStep = &step
	case "conditional":
		var step ConditionalPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse conditional step: %w", err)
		}
		typedStep = &step
	case "decision":
		var step DecisionPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse decision step: %w", err)
		}
		typedStep = &step
	case "orchestration":
		var step OrchestrationPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse orchestration step: %w", err)
		}
		typedStep = &step
	default:
		return nil, fmt.Errorf("unknown step type %q (must be: regular, conditional, decision, or orchestration)", stepWithType.Type)
	}

	return typedStep, nil
}

// unmarshalStepsFromJSON unmarshals an array of steps from JSON
// This is a helper function used by custom UnmarshalJSON methods for step types with nested step arrays
func unmarshalStepsFromJSON(stepsData []json.RawMessage) ([]PlanStepInterface, error) {
	steps := make([]PlanStepInterface, len(stepsData))
	for i, stepData := range stepsData {
		step, err := unmarshalStepFromJSON(stepData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse step %d: %w", i, err)
		}
		steps[i] = step
	}
	return steps, nil
}

// updateValidationSchemaOnStep updates validation schema on any step type
func updateValidationSchemaOnStep(step PlanStepInterface, schema *ValidationSchema) {
	switch s := step.(type) {
	case *RegularPlanStep:
		s.ValidationSchema = schema
	case *ConditionalPlanStep:
		s.ValidationSchema = schema
	case *DecisionPlanStep:
		// For DecisionPlanStep, validation schema is on the inner DecisionStep
		if s.DecisionStep != nil {
			updateValidationSchemaOnStep(s.DecisionStep, schema)
		}
	case *OrchestrationPlanStep:
		// For OrchestrationPlanStep, validation schema is on the inner OrchestrationStep
		if s.OrchestrationStep != nil {
			updateValidationSchemaOnStep(s.OrchestrationStep, schema)
		}
	}
}

// compareNestedStepFields compares two PlanStepInterface objects and tracks all field changes
// prefix is used to create hierarchical field names (e.g., "orchestration_step.description")
func compareNestedStepFields(oldStep PlanStepInterface, newStep PlanStepInterface, stepID string, prefix string, fieldChanges *[]PlanFieldChange) {
	if oldStep == nil && newStep == nil {
		return
	}

	// If one is nil, track the entire step as changed
	if oldStep == nil {
		newStepJSON, _ := json.Marshal(newStep)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix,
			OldValue: nil,
			NewValue: string(newStepJSON),
		})
		return
	}
	if newStep == nil {
		oldStepJSON, _ := json.Marshal(oldStep)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix,
			OldValue: string(oldStepJSON),
			NewValue: nil,
		})
		return
	}

	// Compare common fields
	if oldStep.GetTitle() != newStep.GetTitle() {
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix + ".title",
			OldValue: oldStep.GetTitle(),
			NewValue: newStep.GetTitle(),
		})
	}
	if oldStep.GetDescription() != newStep.GetDescription() {
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix + ".description",
			OldValue: oldStep.GetDescription(),
			NewValue: newStep.GetDescription(),
		})
	}
	if oldStep.GetSuccessCriteria() != newStep.GetSuccessCriteria() {
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix + ".success_criteria",
			OldValue: oldStep.GetSuccessCriteria(),
			NewValue: newStep.GetSuccessCriteria(),
		})
	}

	// Compare context dependencies
	oldDeps := oldStep.GetContextDependencies()
	newDeps := newStep.GetContextDependencies()
	if !equalStringSlices(oldDeps, newDeps) {
		// Store as JSON for proper revert
		oldDepsJSON, _ := json.Marshal(oldDeps)
		newDepsJSON, _ := json.Marshal(newDeps)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix + ".context_dependencies",
			OldValue: string(oldDepsJSON),
			NewValue: string(newDepsJSON),
		})
	}

	// Compare context output
	oldOutput := oldStep.GetContextOutput().String()
	newOutput := newStep.GetContextOutput().String()
	if oldOutput != newOutput {
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix + ".context_output",
			OldValue: oldOutput,
			NewValue: newOutput,
		})
	}

	// Compare prerequisite detection
	oldPrereq := oldStep.GetEnablePrerequisiteDetection()
	newPrereq := newStep.GetEnablePrerequisiteDetection()
	if !equalBoolPtrs(oldPrereq, newPrereq) {
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix + ".enable_prerequisite_detection",
			OldValue: oldPrereq,
			NewValue: newPrereq,
		})
	}

	// Compare prerequisite rules
	oldRules := oldStep.GetPrerequisiteRules()
	newRules := newStep.GetPrerequisiteRules()
	if !equalPrerequisiteRules(oldRules, newRules) {
		// Store as JSON for proper revert
		oldRulesJSON, _ := json.Marshal(oldRules)
		newRulesJSON, _ := json.Marshal(newRules)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix + ".prerequisite_rules",
			OldValue: string(oldRulesJSON),
			NewValue: string(newRulesJSON),
		})
	}

	// Compare validation schema
	oldSchema := oldStep.GetValidationSchema()
	newSchema := newStep.GetValidationSchema()
	if !equalValidationSchemas(oldSchema, newSchema) {
		oldSchemaJSON := "nil"
		if oldSchema != nil {
			oldBytes, _ := json.Marshal(oldSchema)
			oldSchemaJSON = string(oldBytes)
		}
		newSchemaJSON := "nil"
		if newSchema != nil {
			newBytes, _ := json.Marshal(newSchema)
			newSchemaJSON = string(newBytes)
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    prefix + ".validation_schema",
			OldValue: oldSchemaJSON,
			NewValue: newSchemaJSON,
		})
	}

	// Compare type-specific fields
	switch oldS := oldStep.(type) {
	case *RegularPlanStep:
		if newS, ok := newStep.(*RegularPlanStep); ok {
			if oldS.HasLoop != newS.HasLoop {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".has_loop",
					OldValue: oldS.HasLoop,
					NewValue: newS.HasLoop,
				})
			}
			// Note: LoopCondition, MaxIterations, LoopDescription are already compared above in common fields
			// But we track them here for type-specific clarity
			if oldS.LoopCondition != newS.LoopCondition {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".loop_condition",
					OldValue: oldS.LoopCondition,
					NewValue: newS.LoopCondition,
				})
			}
			if oldS.MaxIterations != newS.MaxIterations {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".max_iterations",
					OldValue: oldS.MaxIterations,
					NewValue: newS.MaxIterations,
				})
			}
			if oldS.LoopDescription != newS.LoopDescription {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".loop_description",
					OldValue: oldS.LoopDescription,
					NewValue: newS.LoopDescription,
				})
			}
		}

	case *ConditionalPlanStep:
		if newS, ok := newStep.(*ConditionalPlanStep); ok {
			if oldS.ConditionQuestion != newS.ConditionQuestion {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".condition_question",
					OldValue: oldS.ConditionQuestion,
					NewValue: newS.ConditionQuestion,
				})
			}
			if oldS.ConditionContext != newS.ConditionContext {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".condition_context",
					OldValue: oldS.ConditionContext,
					NewValue: newS.ConditionContext,
				})
			}
			if oldS.IfTrueNextStepID != newS.IfTrueNextStepID {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".if_true_next_step_id",
					OldValue: oldS.IfTrueNextStepID,
					NewValue: newS.IfTrueNextStepID,
				})
			}
			if oldS.IfFalseNextStepID != newS.IfFalseNextStepID {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".if_false_next_step_id",
					OldValue: oldS.IfFalseNextStepID,
					NewValue: newS.IfFalseNextStepID,
				})
			}
			// Compare nested steps in branches (simplified - track count and IDs)
			if len(oldS.IfTrueSteps) != len(newS.IfTrueSteps) {
				oldIDs := make([]string, len(oldS.IfTrueSteps))
				for i, s := range oldS.IfTrueSteps {
					oldIDs[i] = s.GetID()
				}
				newIDs := make([]string, len(newS.IfTrueSteps))
				for i, s := range newS.IfTrueSteps {
					newIDs[i] = s.GetID()
				}
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".if_true_steps",
					OldValue: fmt.Sprintf("%d steps: %v", len(oldIDs), oldIDs),
					NewValue: fmt.Sprintf("%d steps: %v", len(newIDs), newIDs),
				})
			}
			if len(oldS.IfFalseSteps) != len(newS.IfFalseSteps) {
				oldIDs := make([]string, len(oldS.IfFalseSteps))
				for i, s := range oldS.IfFalseSteps {
					oldIDs[i] = s.GetID()
				}
				newIDs := make([]string, len(newS.IfFalseSteps))
				for i, s := range newS.IfFalseSteps {
					newIDs[i] = s.GetID()
				}
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".if_false_steps",
					OldValue: fmt.Sprintf("%d steps: %v", len(oldIDs), oldIDs),
					NewValue: fmt.Sprintf("%d steps: %v", len(newIDs), newIDs),
				})
			}
		}

	case *DecisionPlanStep:
		if newS, ok := newStep.(*DecisionPlanStep); ok {
			// Compare wrapper's title (already compared in common fields, but ensure it's here for clarity)
			if oldS.Title != newS.Title {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".title",
					OldValue: oldS.Title,
					NewValue: newS.Title,
				})
			}
			if oldS.DecisionEvaluationQuestion != newS.DecisionEvaluationQuestion {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".decision_evaluation_question",
					OldValue: oldS.DecisionEvaluationQuestion,
					NewValue: newS.DecisionEvaluationQuestion,
				})
			}
			if oldS.IfTrueNextStepID != newS.IfTrueNextStepID {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".if_true_next_step_id",
					OldValue: oldS.IfTrueNextStepID,
					NewValue: newS.IfTrueNextStepID,
				})
			}
			if oldS.IfFalseNextStepID != newS.IfFalseNextStepID {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".if_false_next_step_id",
					OldValue: oldS.IfFalseNextStepID,
					NewValue: newS.IfFalseNextStepID,
				})
			}
			// Compare nested decision step
			if oldS.DecisionStep != nil && newS.DecisionStep != nil {
				compareNestedStepFields(oldS.DecisionStep, newS.DecisionStep, stepID, prefix+".decision_step", fieldChanges)
			} else if oldS.DecisionStep != nil || newS.DecisionStep != nil {
				oldID := "nil"
				if oldS.DecisionStep != nil {
					oldID = oldS.DecisionStep.GetID()
				}
				newID := "nil"
				if newS.DecisionStep != nil {
					newID = newS.DecisionStep.GetID()
				}
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".decision_step",
					OldValue: oldID,
					NewValue: newID,
				})
			}
		}

	case *OrchestrationPlanStep:
		if newS, ok := newStep.(*OrchestrationPlanStep); ok {
			// Compare wrapper's title (already compared in common fields, but ensure it's here for clarity)
			// Title is already compared in common fields section above, but we include it here for wrapper-specific tracking
			if oldS.Title != newS.Title {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".title",
					OldValue: oldS.Title,
					NewValue: newS.Title,
				})
			}
			if oldS.NextStepID != newS.NextStepID {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".next_step_id",
					OldValue: oldS.NextStepID,
					NewValue: newS.NextStepID,
				})
			}
			// Compare nested orchestration step
			if oldS.OrchestrationStep != nil && newS.OrchestrationStep != nil {
				compareNestedStepFields(oldS.OrchestrationStep, newS.OrchestrationStep, stepID, prefix+".orchestration_step", fieldChanges)
			} else if oldS.OrchestrationStep != nil || newS.OrchestrationStep != nil {
				oldID := "nil"
				if oldS.OrchestrationStep != nil {
					oldID = oldS.OrchestrationStep.GetID()
				}
				newID := "nil"
				if newS.OrchestrationStep != nil {
					newID = newS.OrchestrationStep.GetID()
				}
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".orchestration_step",
					OldValue: oldID,
					NewValue: newID,
				})
			}
			// Compare orchestration routes
			if !equalOrchestrationRoutes(oldS.OrchestrationRoutes, newS.OrchestrationRoutes) {
				oldRoutesJSON, _ := json.Marshal(oldS.OrchestrationRoutes)
				newRoutesJSON, _ := json.Marshal(newS.OrchestrationRoutes)
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".orchestration_routes",
					OldValue: string(oldRoutesJSON),
					NewValue: string(newRoutesJSON),
				})
			}
		}
	}
}

// Helper functions for comparison
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalBoolPtrs(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func equalPrerequisiteRules(a, b []PrerequisiteRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].DependsOnStep != b[i].DependsOnStep || a[i].Description != b[i].Description {
			return false
		}
	}
	return true
}

func equalValidationSchemas(a, b *ValidationSchema) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

func equalOrchestrationRoutes(a, b []PlanOrchestrationRoute) bool {
	if len(a) != len(b) {
		return false
	}
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

// mergePartialStepUpdate merges a PartialPlanStep update into an existing PlanStepInterface
// Uses type switches to handle each step type appropriately
func mergePartialStepUpdate(existingStep PlanStepInterface, partialUpdate PartialPlanStep) PlanStepInterface {
	// Use type switch to handle each step type
	switch step := existingStep.(type) {
	case *RegularPlanStep:
		// Create updated copy
		updated := *step
		if partialUpdate.Title != "" {
			updated.Title = partialUpdate.Title
		}
		if partialUpdate.Description != "" {
			updated.Description = partialUpdate.Description
		}
		if partialUpdate.SuccessCriteria != "" {
			updated.SuccessCriteria = partialUpdate.SuccessCriteria
		}
		if partialUpdate.ContextDependencies != nil {
			updated.ContextDependencies = partialUpdate.ContextDependencies
		}
		if partialUpdate.ContextOutput != "" {
			updated.ContextOutput = FlexibleContextOutput(partialUpdate.ContextOutput)
		}
		if partialUpdate.HasLoop != nil {
			updated.HasLoop = *partialUpdate.HasLoop
		}
		if partialUpdate.LoopCondition != "" {
			updated.LoopCondition = partialUpdate.LoopCondition
		}
		if partialUpdate.MaxIterations != nil {
			updated.MaxIterations = *partialUpdate.MaxIterations
		}
		if partialUpdate.LoopDescription != "" {
			updated.LoopDescription = partialUpdate.LoopDescription
		}
		if partialUpdate.EnablePrerequisiteDetection != nil {
			updated.EnablePrerequisiteDetection = partialUpdate.EnablePrerequisiteDetection
		}
		if partialUpdate.PrerequisiteRules != nil {
			updated.PrerequisiteRules = partialUpdate.PrerequisiteRules
		}
		if partialUpdate.ValidationSchema != nil {
			updated.ValidationSchema = partialUpdate.ValidationSchema
		}
		// Validation schema is LLM-generated only - no code-based auto-generation
		return &updated

	case *ConditionalPlanStep:
		updated := *step
		if partialUpdate.Title != "" {
			updated.Title = partialUpdate.Title
		}
		if partialUpdate.ConditionQuestion != "" {
			updated.ConditionQuestion = partialUpdate.ConditionQuestion
		}
		if partialUpdate.ConditionContext != "" {
			updated.ConditionContext = partialUpdate.ConditionContext
		}
		if partialUpdate.IfTrueSteps != nil {
			// Convert map[string]interface{} to PlanStepInterface
			updated.IfTrueSteps = make([]PlanStepInterface, len(partialUpdate.IfTrueSteps))
			for i, stepMap := range partialUpdate.IfTrueSteps {
				converted, err := convertMapToStep(stepMap)
				if err != nil {
					// If conversion fails, we can't update - return original
					return existingStep
				}
				updated.IfTrueSteps[i] = converted
			}
		}
		if partialUpdate.IfFalseSteps != nil {
			updated.IfFalseSteps = make([]PlanStepInterface, len(partialUpdate.IfFalseSteps))
			for i, stepMap := range partialUpdate.IfFalseSteps {
				converted, err := convertMapToStep(stepMap)
				if err != nil {
					return existingStep
				}
				updated.IfFalseSteps[i] = converted
			}
		}
		if partialUpdate.IfTrueNextStepID != "" {
			updated.IfTrueNextStepID = partialUpdate.IfTrueNextStepID
		}
		if partialUpdate.IfFalseNextStepID != "" {
			updated.IfFalseNextStepID = partialUpdate.IfFalseNextStepID
		}
		if partialUpdate.ValidationSchema != nil {
			updated.ValidationSchema = partialUpdate.ValidationSchema
		}
		return &updated

	case *DecisionPlanStep:
		updated := *step
		if partialUpdate.Title != "" {
			updated.Title = partialUpdate.Title
		}
		if partialUpdate.DecisionStep != nil {
			// If we have an existing nested step, merge the partial update into it to preserve fields
			// that weren't in the update (like context_dependencies, validation_schema, etc.)
			if updated.DecisionStep != nil {
				// Create a PartialPlanStep from the map by extracting fields directly
				nestedPartial := PartialPlanStep{}
				// Extract common fields from the map
				if desc, ok := partialUpdate.DecisionStep["description"].(string); ok {
					nestedPartial.Description = desc
				}
				if title, ok := partialUpdate.DecisionStep["title"].(string); ok {
					nestedPartial.Title = title
				}
				if successCriteria, ok := partialUpdate.DecisionStep["success_criteria"].(string); ok {
					nestedPartial.SuccessCriteria = successCriteria
				}
				if contextDeps, ok := partialUpdate.DecisionStep["context_dependencies"].([]interface{}); ok {
					nestedPartial.ContextDependencies = make([]string, 0, len(contextDeps))
					for _, dep := range contextDeps {
						if depStr, ok := dep.(string); ok {
							nestedPartial.ContextDependencies = append(nestedPartial.ContextDependencies, depStr)
						}
					}
				}
				if contextOutput, ok := partialUpdate.DecisionStep["context_output"]; ok {
					if contextOutputMap, ok := contextOutput.(map[string]interface{}); ok {
						contextOutputJSON, _ := json.Marshal(contextOutputMap)
						json.Unmarshal(contextOutputJSON, &nestedPartial.ContextOutput)
					}
				}
				if validationSchema, ok := partialUpdate.DecisionStep["validation_schema"]; ok {
					if validationSchemaMap, ok := validationSchema.(map[string]interface{}); ok {
						validationSchemaJSON, _ := json.Marshal(validationSchemaMap)
						var vs ValidationSchema
						if json.Unmarshal(validationSchemaJSON, &vs) == nil {
							nestedPartial.ValidationSchema = &vs
						}
					}
				}
				if nextStepID, ok := partialUpdate.DecisionStep["next_step_id"].(string); ok {
					nestedPartial.NextStepID = nextStepID
				}
				// Merge the nested partial update into the existing nested step
				updated.DecisionStep = mergePartialStepUpdate(updated.DecisionStep, nestedPartial)
			} else {
				// No existing nested step - convert and assign directly
				converted, err := convertMapToStep(partialUpdate.DecisionStep)
				if err != nil {
					return existingStep
				}
				updated.DecisionStep = converted
			}
		}
		if partialUpdate.DecisionEvaluationQuestion != "" {
			updated.DecisionEvaluationQuestion = partialUpdate.DecisionEvaluationQuestion
		}
		if partialUpdate.IfTrueNextStepID != "" {
			updated.IfTrueNextStepID = partialUpdate.IfTrueNextStepID
		}
		if partialUpdate.IfFalseNextStepID != "" {
			updated.IfFalseNextStepID = partialUpdate.IfFalseNextStepID
		}
		if partialUpdate.ValidationSchema != nil && updated.DecisionStep != nil {
			// Update validation schema on the inner DecisionStep (can be any step type)
			updateValidationSchemaOnStep(updated.DecisionStep, partialUpdate.ValidationSchema)
		}
		return &updated

	case *OrchestrationPlanStep:
		updated := *step
		if partialUpdate.Title != "" {
			updated.Title = partialUpdate.Title
		}
		if partialUpdate.OrchestrationStep != nil {
			// If we have an existing nested step, merge the partial update into it to preserve fields
			// that weren't in the update (like context_dependencies, validation_schema, etc.)
			if updated.OrchestrationStep != nil {
				// Create a PartialPlanStep from the map by extracting fields directly
				nestedPartial := PartialPlanStep{}
				// Extract common fields from the map
				if desc, ok := partialUpdate.OrchestrationStep["description"].(string); ok {
					nestedPartial.Description = desc
				}
				if title, ok := partialUpdate.OrchestrationStep["title"].(string); ok {
					nestedPartial.Title = title
				}
				if successCriteria, ok := partialUpdate.OrchestrationStep["success_criteria"].(string); ok {
					nestedPartial.SuccessCriteria = successCriteria
				}
				if contextDeps, ok := partialUpdate.OrchestrationStep["context_dependencies"].([]interface{}); ok {
					nestedPartial.ContextDependencies = make([]string, 0, len(contextDeps))
					for _, dep := range contextDeps {
						if depStr, ok := dep.(string); ok {
							nestedPartial.ContextDependencies = append(nestedPartial.ContextDependencies, depStr)
						}
					}
				}
				if contextOutput, ok := partialUpdate.OrchestrationStep["context_output"]; ok {
					if contextOutputMap, ok := contextOutput.(map[string]interface{}); ok {
						contextOutputJSON, _ := json.Marshal(contextOutputMap)
						json.Unmarshal(contextOutputJSON, &nestedPartial.ContextOutput)
					}
				}
				if validationSchema, ok := partialUpdate.OrchestrationStep["validation_schema"]; ok {
					if validationSchemaMap, ok := validationSchema.(map[string]interface{}); ok {
						validationSchemaJSON, _ := json.Marshal(validationSchemaMap)
						var vs ValidationSchema
						if json.Unmarshal(validationSchemaJSON, &vs) == nil {
							nestedPartial.ValidationSchema = &vs
						}
					}
				}
				if nextStepID, ok := partialUpdate.OrchestrationStep["next_step_id"].(string); ok {
					nestedPartial.NextStepID = nextStepID
				}
				// Merge the nested partial update into the existing nested step
				updated.OrchestrationStep = mergePartialStepUpdate(updated.OrchestrationStep, nestedPartial)
			} else {
				// No existing nested step - convert and assign directly
				converted, err := convertMapToStep(partialUpdate.OrchestrationStep)
				if err != nil {
					return existingStep
				}
				updated.OrchestrationStep = converted
			}
		}
		if partialUpdate.OrchestrationRoutes != nil {
			// Convert routes - SubAgentStep needs conversion
			updated.OrchestrationRoutes = make([]PlanOrchestrationRoute, len(partialUpdate.OrchestrationRoutes))
			for i, route := range partialUpdate.OrchestrationRoutes {
				updated.OrchestrationRoutes[i] = route
				// Note: PlanOrchestrationRoute.SubAgentStep is PlanStepInterface, and PartialPlanStep uses map[string]interface{} for nested steps
			}
		}
		if partialUpdate.NextStepID != "" {
			updated.NextStepID = partialUpdate.NextStepID
		}
		if partialUpdate.ValidationSchema != nil && updated.OrchestrationStep != nil {
			// Update validation schema on the inner OrchestrationStep (can be any step type)
			updateValidationSchemaOnStep(updated.OrchestrationStep, partialUpdate.ValidationSchema)
		}
		return &updated

	default:
		// Unknown type - return original
		return existingStep
	}
}

// NewHumanControlledTodoPlannerPlanningAgent creates a new human-controlled todo planner planning agent
func NewHumanControlledTodoPlannerPlanningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerPlanningAgent {
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

// updateSingleStep is a helper function that updates a single step in the plan
// Returns the step index and field changes for changelog
func updateSingleStep(plan *PlanningResponse, partialUpdate PartialPlanStep, fieldChanges *[]PlanFieldChange) (int, []string, error) {
	// Find the step to update
	var existingStep PlanStepInterface
	stepIndex := -1
	for i, step := range plan.Steps {
		if step.GetID() == partialUpdate.ExistingStepID {
			existingStep = step
			stepIndex = i
			break
		}
	}
	if existingStep == nil {
		availableIDs := make([]string, 0, len(plan.Steps))
		for _, step := range plan.Steps {
			availableIDs = append(availableIDs, step.GetID())
		}
		return -1, nil, fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs), nil)
	}

	changedFields := []string{}

	// Track each field change with old and new values using interface methods
	if partialUpdate.Title != "" {
		changedFields = append(changedFields, "title")
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "title",
			OldValue: existingStep.GetTitle(),
			NewValue: partialUpdate.Title,
		})
	}
	if partialUpdate.Description != "" {
		changedFields = append(changedFields, "description")
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "description",
			OldValue: existingStep.GetDescription(),
			NewValue: partialUpdate.Description,
		})
	}
	if partialUpdate.SuccessCriteria != "" {
		changedFields = append(changedFields, "success_criteria")
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "success_criteria",
			OldValue: existingStep.GetSuccessCriteria(),
			NewValue: partialUpdate.SuccessCriteria,
		})
	}
	if partialUpdate.ContextDependencies != nil {
		changedFields = append(changedFields, "context_dependencies")
		oldDeps := existingStep.GetContextDependencies()
		// Store as JSON for proper revert
		oldDepsJSON, _ := json.Marshal(oldDeps)
		newDepsJSON, _ := json.Marshal(partialUpdate.ContextDependencies)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "context_dependencies",
			OldValue: string(oldDepsJSON),
			NewValue: string(newDepsJSON),
		})
	}
	if partialUpdate.ContextOutput != "" {
		changedFields = append(changedFields, "context_output")
		oldOutput := existingStep.GetContextOutput()
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "context_output",
			OldValue: oldOutput.String(),
			NewValue: partialUpdate.ContextOutput,
		})
	}
	// HasLoop is only for RegularPlanStep
	if partialUpdate.HasLoop != nil {
		changedFields = append(changedFields, "has_loop")
		oldHasLoop := false
		if regularStep, ok := existingStep.(*RegularPlanStep); ok {
			oldHasLoop = regularStep.HasLoop
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "has_loop",
			OldValue: oldHasLoop,
			NewValue: *partialUpdate.HasLoop,
		})
	}
	if partialUpdate.LoopCondition != "" {
		changedFields = append(changedFields, "loop_condition")
		oldLoopCondition := ""
		if regularStep, ok := existingStep.(*RegularPlanStep); ok {
			oldLoopCondition = regularStep.LoopCondition
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "loop_condition",
			OldValue: oldLoopCondition,
			NewValue: partialUpdate.LoopCondition,
		})
	}
	if partialUpdate.MaxIterations != nil {
		changedFields = append(changedFields, "max_iterations")
		oldMaxIterations := 0
		if regularStep, ok := existingStep.(*RegularPlanStep); ok {
			oldMaxIterations = regularStep.MaxIterations
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "max_iterations",
			OldValue: oldMaxIterations,
			NewValue: *partialUpdate.MaxIterations,
		})
	}
	if partialUpdate.LoopDescription != "" {
		changedFields = append(changedFields, "loop_description")
		oldLoopDescription := ""
		if regularStep, ok := existingStep.(*RegularPlanStep); ok {
			oldLoopDescription = regularStep.LoopDescription
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "loop_description",
			OldValue: oldLoopDescription,
			NewValue: partialUpdate.LoopDescription,
		})
	}
	if partialUpdate.EnablePrerequisiteDetection != nil {
		changedFields = append(changedFields, "enable_prerequisite_detection")
		var oldValue interface{} = nil
		if existingStep.GetEnablePrerequisiteDetection() != nil {
			oldValue = *existingStep.GetEnablePrerequisiteDetection()
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "enable_prerequisite_detection",
			OldValue: oldValue,
			NewValue: *partialUpdate.EnablePrerequisiteDetection,
		})
	}
	if partialUpdate.PrerequisiteRules != nil {
		changedFields = append(changedFields, "prerequisite_rules")
		oldRules := existingStep.GetPrerequisiteRules()
		// Store as JSON for proper revert
		oldRulesJSON, _ := json.Marshal(oldRules)
		newRulesJSON, _ := json.Marshal(partialUpdate.PrerequisiteRules)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "prerequisite_rules",
			OldValue: string(oldRulesJSON),
			NewValue: string(newRulesJSON),
		})
	}
	// Conditional step fields
	if partialUpdate.ConditionQuestion != "" {
		changedFields = append(changedFields, "condition_question")
		oldConditionQuestion := ""
		if conditionalStep, ok := existingStep.(*ConditionalPlanStep); ok {
			oldConditionQuestion = conditionalStep.ConditionQuestion
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "condition_question",
			OldValue: oldConditionQuestion,
			NewValue: partialUpdate.ConditionQuestion,
		})
	}
	if partialUpdate.ConditionContext != "" {
		changedFields = append(changedFields, "condition_context")
		oldConditionContext := ""
		if conditionalStep, ok := existingStep.(*ConditionalPlanStep); ok {
			oldConditionContext = conditionalStep.ConditionContext
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "condition_context",
			OldValue: oldConditionContext,
			NewValue: partialUpdate.ConditionContext,
		})
	}
	if partialUpdate.IfTrueSteps != nil {
		changedFields = append(changedFields, "if_true_steps")
		// Get old steps
		var oldSteps []PlanStepInterface
		if conditionalStep, ok := existingStep.(*ConditionalPlanStep); ok {
			oldSteps = conditionalStep.IfTrueSteps
		}
		// Convert new steps from maps to PlanStepInterface
		newSteps := make([]PlanStepInterface, 0, len(partialUpdate.IfTrueSteps))
		for _, stepMap := range partialUpdate.IfTrueSteps {
			converted, err := convertMapToStep(stepMap)
			if err == nil {
				newSteps = append(newSteps, converted)
			}
		}
		// Compare steps in detail
		maxLen := len(oldSteps)
		if len(newSteps) > maxLen {
			maxLen = len(newSteps)
		}
		for i := 0; i < maxLen; i++ {
			stepPrefix := fmt.Sprintf("if_true_steps[%d]", i)
			if i >= len(oldSteps) {
				// New step added
				if i < len(newSteps) {
					newStepJSON, _ := json.Marshal(newSteps[i])
					*fieldChanges = append(*fieldChanges, PlanFieldChange{
						StepID:   partialUpdate.ExistingStepID,
						Field:    stepPrefix,
						OldValue: nil,
						NewValue: string(newStepJSON),
					})
				}
			} else if i >= len(newSteps) {
				// Step removed
				oldStepJSON, _ := json.Marshal(oldSteps[i])
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    stepPrefix,
					OldValue: string(oldStepJSON),
					NewValue: nil,
				})
			} else {
				// Step modified - compare fields
				compareNestedStepFields(oldSteps[i], newSteps[i], partialUpdate.ExistingStepID, stepPrefix, fieldChanges)
			}
		}
	}
	if partialUpdate.IfFalseSteps != nil {
		changedFields = append(changedFields, "if_false_steps")
		// Get old steps
		var oldSteps []PlanStepInterface
		if conditionalStep, ok := existingStep.(*ConditionalPlanStep); ok {
			oldSteps = conditionalStep.IfFalseSteps
		}
		// Convert new steps from maps to PlanStepInterface
		newSteps := make([]PlanStepInterface, 0, len(partialUpdate.IfFalseSteps))
		for _, stepMap := range partialUpdate.IfFalseSteps {
			converted, err := convertMapToStep(stepMap)
			if err == nil {
				newSteps = append(newSteps, converted)
			}
		}
		// Compare steps in detail
		maxLen := len(oldSteps)
		if len(newSteps) > maxLen {
			maxLen = len(newSteps)
		}
		for i := 0; i < maxLen; i++ {
			stepPrefix := fmt.Sprintf("if_false_steps[%d]", i)
			if i >= len(oldSteps) {
				// New step added
				if i < len(newSteps) {
					newStepJSON, _ := json.Marshal(newSteps[i])
					*fieldChanges = append(*fieldChanges, PlanFieldChange{
						StepID:   partialUpdate.ExistingStepID,
						Field:    stepPrefix,
						OldValue: nil,
						NewValue: string(newStepJSON),
					})
				}
			} else if i >= len(newSteps) {
				// Step removed
				oldStepJSON, _ := json.Marshal(oldSteps[i])
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    stepPrefix,
					OldValue: string(oldStepJSON),
					NewValue: nil,
				})
			} else {
				// Step modified - compare fields
				compareNestedStepFields(oldSteps[i], newSteps[i], partialUpdate.ExistingStepID, stepPrefix, fieldChanges)
			}
		}
	}
	if partialUpdate.IfTrueNextStepID != "" {
		changedFields = append(changedFields, "if_true_next_step_id")
		oldIfTrueNextStepID := ""
		if conditionalStep, ok := existingStep.(*ConditionalPlanStep); ok {
			oldIfTrueNextStepID = conditionalStep.IfTrueNextStepID
		} else if decisionStep, ok := existingStep.(*DecisionPlanStep); ok {
			oldIfTrueNextStepID = decisionStep.IfTrueNextStepID
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "if_true_next_step_id",
			OldValue: oldIfTrueNextStepID,
			NewValue: partialUpdate.IfTrueNextStepID,
		})
	}
	if partialUpdate.IfFalseNextStepID != "" {
		changedFields = append(changedFields, "if_false_next_step_id")
		oldIfFalseNextStepID := ""
		if conditionalStep, ok := existingStep.(*ConditionalPlanStep); ok {
			oldIfFalseNextStepID = conditionalStep.IfFalseNextStepID
		} else if decisionStep, ok := existingStep.(*DecisionPlanStep); ok {
			oldIfFalseNextStepID = decisionStep.IfFalseNextStepID
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "if_false_next_step_id",
			OldValue: oldIfFalseNextStepID,
			NewValue: partialUpdate.IfFalseNextStepID,
		})
	}
	// Decision step fields
	if partialUpdate.DecisionStep != nil {
		changedFields = append(changedFields, "decision_step")
		// Convert new decision step from map to PlanStepInterface
		newDecisionStep, err := convertMapToStep(partialUpdate.DecisionStep)
		if err == nil {
			// Get old decision step
			var oldDecisionStep PlanStepInterface
			if decisionStep, ok := existingStep.(*DecisionPlanStep); ok {
				oldDecisionStep = decisionStep.DecisionStep
			}
			// Compare nested step fields in detail
			compareNestedStepFields(oldDecisionStep, newDecisionStep, partialUpdate.ExistingStepID, "decision_step", fieldChanges)
		} else {
			// Fallback to ID-only tracking if conversion fails
			oldDecisionStep := "nil"
			if decisionStep, ok := existingStep.(*DecisionPlanStep); ok && decisionStep.DecisionStep != nil {
				oldDecisionStep = decisionStep.DecisionStep.GetID()
			}
			newDecisionStepID := ""
			if id, ok := partialUpdate.DecisionStep["id"].(string); ok {
				newDecisionStepID = id
			}
			*fieldChanges = append(*fieldChanges, PlanFieldChange{
				StepID:   partialUpdate.ExistingStepID,
				Field:    "decision_step",
				OldValue: oldDecisionStep,
				NewValue: newDecisionStepID,
			})
		}
	}
	if partialUpdate.DecisionEvaluationQuestion != "" {
		changedFields = append(changedFields, "decision_evaluation_question")
		oldDecisionEvaluationQuestion := ""
		if decisionStep, ok := existingStep.(*DecisionPlanStep); ok {
			oldDecisionEvaluationQuestion = decisionStep.DecisionEvaluationQuestion
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "decision_evaluation_question",
			OldValue: oldDecisionEvaluationQuestion,
			NewValue: partialUpdate.DecisionEvaluationQuestion,
		})
	}
	// Orchestration step fields
	if partialUpdate.OrchestrationStep != nil {
		changedFields = append(changedFields, "orchestration_step")
		// Convert new orchestration step from map to PlanStepInterface
		newOrchestrationStep, err := convertMapToStep(partialUpdate.OrchestrationStep)
		if err == nil {
			// Get old orchestration step
			var oldOrchestrationStep PlanStepInterface
			if orchestrationStep, ok := existingStep.(*OrchestrationPlanStep); ok {
				oldOrchestrationStep = orchestrationStep.OrchestrationStep
			}
			// Compare nested step fields in detail
			compareNestedStepFields(oldOrchestrationStep, newOrchestrationStep, partialUpdate.ExistingStepID, "orchestration_step", fieldChanges)
		} else {
			// Fallback to ID-only tracking if conversion fails
			oldOrchestrationStep := "nil"
			if orchestrationStep, ok := existingStep.(*OrchestrationPlanStep); ok && orchestrationStep.OrchestrationStep != nil {
				oldOrchestrationStep = orchestrationStep.OrchestrationStep.GetID()
			}
			newOrchestrationStepID := ""
			if id, ok := partialUpdate.OrchestrationStep["id"].(string); ok {
				newOrchestrationStepID = id
			}
			*fieldChanges = append(*fieldChanges, PlanFieldChange{
				StepID:   partialUpdate.ExistingStepID,
				Field:    "orchestration_step",
				OldValue: oldOrchestrationStep,
				NewValue: newOrchestrationStepID,
			})
		}
	}
	if partialUpdate.OrchestrationRoutes != nil {
		changedFields = append(changedFields, "orchestration_routes")
		// Get old routes
		var oldRoutes []PlanOrchestrationRoute
		if orchestrationStep, ok := existingStep.(*OrchestrationPlanStep); ok {
			oldRoutes = orchestrationStep.OrchestrationRoutes
		}
		// Compare routes in detail
		if !equalOrchestrationRoutes(oldRoutes, partialUpdate.OrchestrationRoutes) {
			// Track detailed changes for each route
			maxLen := len(oldRoutes)
			if len(partialUpdate.OrchestrationRoutes) > maxLen {
				maxLen = len(partialUpdate.OrchestrationRoutes)
			}
			for i := 0; i < maxLen; i++ {
				routePrefix := fmt.Sprintf("orchestration_routes[%d]", i)
				if i >= len(oldRoutes) {
					// New route added
					newRouteJSON, _ := json.Marshal(partialUpdate.OrchestrationRoutes[i])
					*fieldChanges = append(*fieldChanges, PlanFieldChange{
						StepID:   partialUpdate.ExistingStepID,
						Field:    routePrefix,
						OldValue: nil,
						NewValue: string(newRouteJSON),
					})
				} else if i >= len(partialUpdate.OrchestrationRoutes) {
					// Route removed
					oldRouteJSON, _ := json.Marshal(oldRoutes[i])
					*fieldChanges = append(*fieldChanges, PlanFieldChange{
						StepID:   partialUpdate.ExistingStepID,
						Field:    routePrefix,
						OldValue: string(oldRouteJSON),
						NewValue: nil,
					})
				} else {
					// Route modified - compare fields
					oldRoute := oldRoutes[i]
					newRoute := partialUpdate.OrchestrationRoutes[i]
					if oldRoute.RouteID != newRoute.RouteID {
						*fieldChanges = append(*fieldChanges, PlanFieldChange{
							StepID:   partialUpdate.ExistingStepID,
							Field:    routePrefix + ".route_id",
							OldValue: oldRoute.RouteID,
							NewValue: newRoute.RouteID,
						})
					}
					if oldRoute.RouteName != newRoute.RouteName {
						*fieldChanges = append(*fieldChanges, PlanFieldChange{
							StepID:   partialUpdate.ExistingStepID,
							Field:    routePrefix + ".route_name",
							OldValue: oldRoute.RouteName,
							NewValue: newRoute.RouteName,
						})
					}
					if oldRoute.Condition != newRoute.Condition {
						*fieldChanges = append(*fieldChanges, PlanFieldChange{
							StepID:   partialUpdate.ExistingStepID,
							Field:    routePrefix + ".condition",
							OldValue: oldRoute.Condition,
							NewValue: newRoute.Condition,
						})
					}
					if oldRoute.ContextToPass != newRoute.ContextToPass {
						*fieldChanges = append(*fieldChanges, PlanFieldChange{
							StepID:   partialUpdate.ExistingStepID,
							Field:    routePrefix + ".context_to_pass",
							OldValue: oldRoute.ContextToPass,
							NewValue: newRoute.ContextToPass,
						})
					}
					// Compare nested sub-agent step
					if oldRoute.SubAgentStep != nil && newRoute.SubAgentStep != nil {
						compareNestedStepFields(oldRoute.SubAgentStep, newRoute.SubAgentStep, partialUpdate.ExistingStepID, routePrefix+".sub_agent_step", fieldChanges)
					} else if oldRoute.SubAgentStep != nil || newRoute.SubAgentStep != nil {
						oldID := "nil"
						if oldRoute.SubAgentStep != nil {
							oldID = oldRoute.SubAgentStep.GetID()
						}
						newID := "nil"
						if newRoute.SubAgentStep != nil {
							newID = newRoute.SubAgentStep.GetID()
						}
						*fieldChanges = append(*fieldChanges, PlanFieldChange{
							StepID:   partialUpdate.ExistingStepID,
							Field:    routePrefix + ".sub_agent_step",
							OldValue: oldID,
							NewValue: newID,
						})
					}
				}
			}
		}
	}
	if partialUpdate.NextStepID != "" {
		changedFields = append(changedFields, "next_step_id")
		oldNextStepID := ""
		if orchestrationStep, ok := existingStep.(*OrchestrationPlanStep); ok {
			oldNextStepID = orchestrationStep.NextStepID
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "next_step_id",
			OldValue: oldNextStepID,
			NewValue: partialUpdate.NextStepID,
		})
	}
	if partialUpdate.ValidationSchema != nil {
		changedFields = append(changedFields, "validation_schema")
		oldSchema := existingStep.GetValidationSchema()
		oldSchemaJSON := "nil"
		if oldSchema != nil {
			oldSchemaBytes, _ := json.Marshal(oldSchema)
			oldSchemaJSON = string(oldSchemaBytes)
		}
		newSchemaJSON := "nil"
		if partialUpdate.ValidationSchema != nil {
			newSchemaBytes, _ := json.Marshal(partialUpdate.ValidationSchema)
			newSchemaJSON = string(newSchemaBytes)
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "validation_schema",
			OldValue: oldSchemaJSON,
			NewValue: newSchemaJSON,
		})
	}

	// Merge partial update
	plan.Steps[stepIndex] = mergePartialStepUpdate(existingStep, partialUpdate)

	return stepIndex, changedFields, nil
}

// createUpdateRegularStepExecutor creates an executor function for update_regular_step tool
func createUpdateRegularStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PartialPlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal step: %w", err), nil)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse step: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		stepIndex, _, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed after update: %w", err), nil)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", partialUpdate.ExistingStepID))
			}
		}

		logger.Info(fmt.Sprintf("✅ Updated regular step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated regular step '%s' in the plan", partialUpdate.ExistingStepID), nil
	}
}

// createUpdateConditionalStepExecutor creates an executor function for update_conditional_step tool
func createUpdateConditionalStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PartialPlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal step: %w", err), nil)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse step: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the conditional step
		var existingStep PlanStepInterface
		stepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == partialUpdate.ExistingStepID {
				existingStep = step
				stepIndex = i
				break
			}
		}
		if existingStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs), nil)
		}

		conditionalStep, ok := existingStep.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", partialUpdate.ExistingStepID), nil)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)
		changedFields := []string{}

		// Handle condition_question and condition_context with explicit empty string support
		_, conditionQuestionProvided := args["condition_question"]
		_, conditionContextProvided := args["condition_context"]

		if conditionQuestionProvided {
			oldValue := conditionalStep.ConditionQuestion
			newValue, _ := args["condition_question"].(string)
			if newValue != "" || oldValue != newValue {
				changedFields = append(changedFields, "condition_question")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "condition_question",
					OldValue: oldValue,
					NewValue: newValue,
				})
				conditionalStep.ConditionQuestion = newValue
			}
		}

		if conditionContextProvided {
			oldValue := conditionalStep.ConditionContext
			newValue, _ := args["condition_context"].(string)
			changedFields = append(changedFields, "condition_context")
			fieldChanges = append(fieldChanges, PlanFieldChange{
				StepID:   partialUpdate.ExistingStepID,
				Field:    "condition_context",
				OldValue: oldValue,
				NewValue: newValue,
			})
			conditionalStep.ConditionContext = newValue
		}

		// Update other conditional fields using the helper
		if partialUpdate.IfTrueSteps != nil || partialUpdate.IfFalseSteps != nil || partialUpdate.IfTrueNextStepID != "" || partialUpdate.IfFalseNextStepID != "" {
			_, additionalChangedFields, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
			if err != nil {
				return "", err
			}
			changedFields = append(changedFields, additionalChangedFields...)
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed after update: %w", err), nil)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", partialUpdate.ExistingStepID))
			}
		}

		logger.Info(fmt.Sprintf("✅ Updated conditional step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated conditional step '%s' in the plan", partialUpdate.ExistingStepID), nil
	}
}

// createUpdateDecisionStepExecutor creates an executor function for update_decision_step tool
func createUpdateDecisionStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PartialPlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal step: %w", err), nil)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse step: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the decision step
		var existingStep PlanStepInterface
		stepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == partialUpdate.ExistingStepID {
				existingStep = step
				stepIndex = i
				break
			}
		}
		if existingStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs), nil)
		}

		// Validate it's a decision step before updating
		_, ok := existingStep.(*DecisionPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a decision step", partialUpdate.ExistingStepID), nil)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		stepIndex, _, err = updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Get the updated step from the plan (updateSingleStep already updated it)
		updatedStep := plan.Steps[stepIndex]
		updatedDecisionStep, ok := updatedStep.(*DecisionPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("updated step is not a decision step"), nil)
		}

		// Validate the updated step has all required fields for decision steps
		// This ensures the agent gets immediate feedback if validation fails
		if err := validateDecisionStepFieldsTyped(updatedDecisionStep); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("validation failed after update: %w", err), nil)
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed after update: %w", err), nil)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", partialUpdate.ExistingStepID))
			}
		}

		logger.Info(fmt.Sprintf("✅ Updated decision step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated decision step '%s' in the plan", partialUpdate.ExistingStepID), nil
	}
}

// createUpdateOrchestrationStepExecutor creates an executor function for update_orchestration_step tool
func createUpdateOrchestrationStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PartialPlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal step: %w", err), nil)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse step: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the orchestration step
		var existingStep PlanStepInterface
		stepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == partialUpdate.ExistingStepID {
				existingStep = step
				stepIndex = i
				break
			}
		}
		if existingStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs), nil)
		}

		// Validate it's an orchestration step before updating
		_, ok := existingStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not an orchestration step", partialUpdate.ExistingStepID), nil)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Log what we're updating for debugging
		if partialUpdate.OrchestrationStep != nil {
			orchestrationStepMap := partialUpdate.OrchestrationStep
			if desc, ok := orchestrationStepMap["description"].(string); ok {
				logger.Info(fmt.Sprintf("🔍 [DEBUG] Updating orchestration_step.description to: %s", desc))
			}
			// Get old description for comparison
			if orchestrationStep, ok := existingStep.(*OrchestrationPlanStep); ok && orchestrationStep.OrchestrationStep != nil {
				oldDesc := orchestrationStep.OrchestrationStep.GetDescription()
				logger.Info(fmt.Sprintf("🔍 [DEBUG] Old orchestration_step.description: %s", oldDesc))
			}
		}

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		stepIndex, _, err = updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Get the updated step from the plan (updateSingleStep already updated it)
		updatedStep := plan.Steps[stepIndex]
		updatedOrchestrationStep, ok := updatedStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("updated step is not an orchestration step"), nil)
		}

		// Log the updated description for debugging
		if updatedOrchestrationStep.OrchestrationStep != nil {
			newDesc := updatedOrchestrationStep.OrchestrationStep.GetDescription()
			logger.Info(fmt.Sprintf("🔍 [DEBUG] New orchestration_step.description after merge: %s", newDesc))
		}

		// Validate the updated step has all required fields for orchestration steps
		// This ensures the agent gets immediate feedback if validation fails
		if err := validateOrchestrationStepFieldsTyped(updatedOrchestrationStep); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("validation failed after update: %w", err), nil)
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed after update: %w", err), nil)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", partialUpdate.ExistingStepID))
			}
		}

		logger.Info(fmt.Sprintf("✅ Updated orchestration step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated orchestration step '%s' in the plan", partialUpdate.ExistingStepID), nil
	}
}

// createDeletePlanStepsExecutor creates an executor function for delete_plan_steps tool
// unlockLearningsFunc is optional - if provided, it will be called after plan deletions to unlock learnings
// Note: For deleted steps, we unlock based on the old plan's step indices before deletion
func createDeletePlanStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract deleted_step_ids from args
		deletedIDsRaw, ok := args["deleted_step_ids"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid deleted_step_ids argument"), nil)
		}

		// Convert to string array
		deletedIDs := make([]string, 0, len(deletedIDsRaw))
		for _, id := range deletedIDsRaw {
			if idStr, ok := id.(string); ok {
				deletedIDs = append(deletedIDs, idStr)
			} else {
				return "", fmt.Errorf(fmt.Sprintf("invalid step ID in deleted_step_ids: %v", id), nil)
			}
		}

		// Read current plan
		oldPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Create set of deleted step IDs
		deletedSet := make(map[string]bool)
		for _, id := range deletedIDs {
			deletedSet[id] = true
		}

		// Validate that all deleted steps exist
		existingStepsMap := make(map[string]bool)
		for _, step := range oldPlan.Steps {
			existingStepsMap[step.GetID()] = true
		}
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				// Build list of available step IDs for better error message
				availableIDs := make([]string, 0, len(oldPlan.Steps))
				for _, step := range oldPlan.Steps {
					availableIDs = append(availableIDs, step.GetID())
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan (cannot delete). Available step IDs: %v", id, availableIDs), nil)
			}
		}

		// Capture deleted steps BEFORE filtering (for changelog revert support)
		// Also capture step indices for unlock operations
		// Convert to JSON for changelog storage
		deletedSteps := make([]json.RawMessage, 0, len(deletedIDs))
		deletedStepIndices := make(map[string]int) // stepID -> old step index
		for i, step := range oldPlan.Steps {
			stepID := step.GetID()
			if deletedSet[stepID] {
				// Marshal step to JSON for changelog
				stepJSON, err := json.Marshal(step)
				if err != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to marshal deleted step %s for changelog: %v", stepID, err))
					continue
				}
				deletedSteps = append(deletedSteps, stepJSON)
				deletedStepIndices[stepID] = i
			}
		}

		// Filter out deleted steps
		filteredSteps := make([]PlanStepInterface, 0, len(oldPlan.Steps))
		for _, step := range oldPlan.Steps {
			if !deletedSet[step.GetID()] {
				filteredSteps = append(filteredSteps, step)
			}
		}

		newPlan := &PlanningResponse{Steps: filteredSteps}

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, newPlan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		// Unlock learnings for all deleted steps (if unlock function provided)
		// Use old step indices from before deletion
		if unlockLearningsFunc != nil {
			for _, stepID := range deletedIDs {
				if oldStepIndex, exists := deletedStepIndices[stepID]; exists {
					if err := unlockLearningsFunc(ctx, stepID, oldStepIndex); err != nil {
						logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for deleted step %s: %v", stepID, err))
					} else {
						logger.Info(fmt.Sprintf("🔓 Unlocked learnings for deleted step %s (plan was modified)", stepID))
					}
				}
			}
		}

		logger.Info(fmt.Sprintf("✅ Deleted %d steps from plan", len(deletedIDs)))
		return fmt.Sprintf("Successfully deleted %d step(s) from the plan", len(deletedIDs)), nil
	}
}

// createAddRegularStepExecutor creates an executor function for add_regular_step tool
func createAddRegularStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "regular", unlockLearningsFunc)
}

// createAddConditionalStepExecutor creates an executor function for add_conditional_step tool
func createAddConditionalStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "conditional", unlockLearningsFunc)
}

// createAddDecisionStepExecutor creates an executor function for add_decision_step tool
func createAddDecisionStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "decision", unlockLearningsFunc)
}

// createAddOrchestrationStepExecutor creates an executor function for add_orchestration_step tool
func createAddOrchestrationStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "orchestration", unlockLearningsFunc)
}

// createAddLoopStepExecutor creates an executor function for add_loop_step tool
func createAddLoopStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "loop", unlockLearningsFunc)
}

// validateDecisionStepFieldsTyped validates that a DecisionPlanStep has all required fields
// Returns an error message suitable for returning as a tool response if validation fails
func validateDecisionStepFieldsTyped(step *DecisionPlanStep) error {
	if step.DecisionStep == nil {
		return fmt.Errorf("step (title: %q, ID: %s) has decision step type but is missing required decision_step field. Please provide the decision_step object with all required fields (id, title, description, success_criteria, has_loop, context_output)", step.Title, step.ID)
	}
	if step.DecisionStep.GetID() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has decision_step with missing required ID field. Please provide an ID for the decision_step", step.Title, step.ID)
	}
	if step.DecisionStep.GetDescription() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has decision_step with missing required description field. Please provide a description for the decision_step", step.Title, step.ID)
	}
	if step.DecisionStep.GetSuccessCriteria() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has decision_step with missing required success_criteria field. Please provide success_criteria for the decision_step", step.Title, step.ID)
	}
	if step.DecisionEvaluationQuestion == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has decision step type but is missing required decision_evaluation_question field. Please provide a question to evaluate the decision step's execution output", step.Title, step.ID)
	}
	if step.IfTrueNextStepID == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has decision step type but is missing required if_true_next_step_id field. Please provide the ID of the step to route to after evaluation is true", step.Title, step.ID)
	}
	if step.IfFalseNextStepID == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has decision step type but is missing required if_false_next_step_id field. Please provide the ID of the step to route to after evaluation is false", step.Title, step.ID)
	}
	return nil
}

// validateOrchestrationStepFieldsTyped validates that an OrchestrationPlanStep has all required fields
// Returns an error message suitable for returning as a tool response if validation fails
func validateOrchestrationStepFieldsTyped(step *OrchestrationPlanStep) error {
	if step.OrchestrationStep == nil {
		return fmt.Errorf("step (title: %q, ID: %s) has orchestration step type but is missing required orchestration_step field. Please provide the orchestration_step object with all required fields (id, title, description, success_criteria, has_loop, context_output)", step.Title, step.ID)
	}
	if step.OrchestrationStep.GetID() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has orchestration_step with missing required ID field. Please provide an ID for the orchestration_step", step.Title, step.ID)
	}
	if step.OrchestrationStep.GetDescription() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has orchestration_step with missing required description field. Please provide a description for the orchestration_step", step.Title, step.ID)
	}
	if step.OrchestrationStep.GetSuccessCriteria() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has orchestration_step with missing required success_criteria field. Please provide success_criteria for the orchestration_step. This field is REQUIRED and must specify how to verify the orchestration step completed successfully", step.Title, step.ID)
	}
	if len(step.OrchestrationRoutes) == 0 {
		return fmt.Errorf("step (title: %q, ID: %s) has orchestration step type but has no orchestration_routes defined. Please provide at least one orchestration route with conditions and sub-agent steps", step.Title, step.ID)
	}
	if step.NextStepID == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has orchestration step type but is missing required next_step_id field. Please provide the ID of the step to connect to after orchestration completes, or 'end' to terminate the workflow", step.Title, step.ID)
	}
	return nil
}

// createSingleStepAdder is a shared executor that handles adding a single step to the plan
// stepType is used for logging and validation purposes
// unlockLearningsFunc is optional - if provided, it will be called after step addition to unlock learnings
func createSingleStepAdder(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, stepType string, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Ensure step has type field based on stepType parameter
		args["type"] = stepType

		// Convert map to typed step
		typedStep, err := convertMapToStep(args)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse step: %w", err), nil)
		}

		// Validate step has ID
		if typedStep.GetID() == "" {
			return "", fmt.Errorf(fmt.Sprintf("step is missing required ID field. Step title: %q", typedStep.GetTitle()), nil)
		}

		// Validation schema is LLM-generated only - no code-based auto-generation

		// Validate step type-specific required fields BEFORE writing to plan
		// This allows the agent to correct errors immediately via tool response
		switch stepType {
		case "decision":
			if decisionStep, ok := typedStep.(*DecisionPlanStep); ok {
				if err := validateDecisionStepFieldsTyped(decisionStep); err != nil {
					return "", fmt.Errorf(fmt.Sprintf("validation failed: %w", err), nil)
				}
			}
		case "orchestration":
			if orchestrationStep, ok := typedStep.(*OrchestrationPlanStep); ok {
				if err := validateOrchestrationStepFieldsTyped(orchestrationStep); err != nil {
					return "", fmt.Errorf(fmt.Sprintf("validation failed: %w", err), nil)
				}
			}
		}

		// Read current plan
		oldPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find insertion point
		var insertAfterStepID string
		if insertID, ok := args["insert_after_step_id"].(string); ok {
			insertAfterStepID = insertID
		} else {
			return "", fmt.Errorf(fmt.Sprintf("missing required insert_after_step_id field"), nil)
		}

		// Create map of step IDs to indices
		idToIndex := make(map[string]int)
		for i, s := range oldPlan.Steps {
			idToIndex[s.GetID()] = i
		}

		var afterIndex int
		var found bool

		if insertAfterStepID == "" {
			// Insert at beginning
			afterIndex = -1
			found = true
		} else {
			// Find the step to insert after
			afterIndex, found = idToIndex[insertAfterStepID]
			if !found {
				availableIDs := make([]string, 0, len(oldPlan.Steps))
				for _, s := range oldPlan.Steps {
					availableIDs = append(availableIDs, s.GetID())
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan (cannot insert after it). Available step IDs: %v", insertAfterStepID, availableIDs), nil)
			}
		}

		// Build new plan with insertion
		newPlanSteps := make([]PlanStepInterface, 0, len(oldPlan.Steps)+1)

		// Insert at beginning if needed
		if afterIndex == -1 {
			newPlanSteps = append(newPlanSteps, typedStep)
		}

		// Add existing steps and insert new step at the right position
		for i, originalStep := range oldPlan.Steps {
			newPlanSteps = append(newPlanSteps, originalStep)
			if i == afterIndex {
				// Insert new step after this one
				newPlanSteps = append(newPlanSteps, typedStep)
			}
		}

		newPlan := &PlanningResponse{Steps: newPlanSteps}

		// Validate all steps including the new one
		if err := validatePlanStepIDs(newPlan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed before writing: %w", err), nil)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, newPlan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		// Unlock learnings for the newly added step (if unlock function provided)
		if unlockLearningsFunc != nil {
			// Find the step index in the new plan
			stepIndex := -1
			stepID := typedStep.GetID()
			for i, s := range newPlan.Steps {
				if s.GetID() == stepID {
					stepIndex = i
					break
				}
			}
			if stepIndex >= 0 {
				if err := unlockLearningsFunc(ctx, stepID, stepIndex); err != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for newly added step %s: %v", stepID, err))
				} else {
					logger.Info(fmt.Sprintf("🔓 Unlocked learnings for newly added step %s (plan was modified)", stepID))
				}
			}
		}

		logger.Info(fmt.Sprintf("✅ Added %s step '%s' (ID: %s) to plan", stepType, typedStep.GetTitle(), typedStep.GetID()))
		return fmt.Sprintf("Successfully added %s step '%s' (ID: %s) to the plan", stepType, typedStep.GetTitle(), typedStep.GetID()), nil
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

// registerPlanModificationTools registers all plan modification tools (plan update tools only)
// Note: human_feedback is NOT registered here because it's already included in WorkspaceTools
// This shared function is used by both planning agent and plan improvement agent
// unlockLearningsFunc is optional - if provided, it will be called after plan modifications to unlock learnings
func registerPlanModificationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	moveFile func(context.Context, string, string) error,
	agentName string, // e.g., "planning agent" or "plan improvement agent"
	unlockLearningsFunc func(context.Context, string, int) error, // Optional: function to unlock learnings after plan modifications
) error {
	// Note: human_feedback is already registered via WorkspaceTools (created by createCustomTools in server.go)
	// No need to register it again here to avoid duplicate registration errors

	// Register workflow-specific plan update tools with "workflow" category
	// Individual update tools for each step type
	regularUpdateSchema := getUpdateRegularStepSchema()
	regularUpdateParams, err := parseSchemaForToolParameters(regularUpdateSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update regular step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_regular_step",
		"Update a regular step in the plan. Provide existing_step_id (required) to identify which step to update, and only include the fields you want to change (title, description, success_criteria, context fields, loop fields, prerequisite fields). The plan.json file is updated immediately when this tool is called.",
		regularUpdateParams,
		createUpdateRegularStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_regular_step tool: %w", err), nil)
	}

	conditionalUpdateSchema := getUpdateConditionalStepSchema()
	conditionalUpdateParams, err := parseSchemaForToolParameters(conditionalUpdateSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update conditional step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_conditional_step",
		"Update a conditional step in the plan. Provide existing_step_id (required) to identify which conditional step to update, and only include the fields you want to change (condition_question, condition_context, if_true_steps, if_false_steps, next_step_ids). The plan.json file is updated immediately when this tool is called.",
		conditionalUpdateParams,
		createUpdateConditionalStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_conditional_step tool: %w", err), nil)
	}

	decisionUpdateSchema := getUpdateDecisionStepSchema()
	decisionUpdateParams, err := parseSchemaForToolParameters(decisionUpdateSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update decision step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_decision_step",
		"Update a decision step in the plan. Provide existing_step_id (required) to identify which decision step to update, and only include the fields you want to change (decision_step, decision_evaluation_question, if_true_next_step_id, if_false_next_step_id). The plan.json file is updated immediately when this tool is called.",
		decisionUpdateParams,
		createUpdateDecisionStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_decision_step tool: %w", err), nil)
	}

	orchestrationUpdateSchema := getUpdateOrchestrationStepSchema()
	orchestrationUpdateParams, err := parseSchemaForToolParameters(orchestrationUpdateSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update orchestration step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_orchestration_step",
		"Update an orchestration step in the plan. Provide existing_step_id (required) to identify which orchestration step to update, and only include the fields you want to change (orchestration_step, orchestration_routes, next_step_id). The plan.json file is updated immediately when this tool is called.",
		orchestrationUpdateParams,
		createUpdateOrchestrationStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_orchestration_step tool: %w", err), nil)
	}

	deleteSchema := getDeletePlanStepsSchema()
	deleteParams, err := parseSchemaForToolParameters(deleteSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse delete schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_plan_steps",
		"Delete steps from the plan by providing their IDs. Use the step's id field from the plan. The plan.json file is updated immediately when this tool is called.",
		deleteParams,
		createDeletePlanStepsExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register delete_plan_steps tool: %w", err), nil)
	}

	// Register type-specific step addition tools
	regularSchema := getAddRegularStepSchema()
	regularParams, err := parseSchemaForToolParameters(regularSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse regular step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_regular_step",
		"Add a regular execution step to the plan. Use this for standard steps that execute once and produce output. Provide all required fields: id, title, description, success_criteria, context_output, has_loop, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		regularParams,
		createAddRegularStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_regular_step tool: %w", err), nil)
	}

	conditionalSchema := getAddConditionalStepSchema()
	conditionalParams, err := parseSchemaForToolParameters(conditionalSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse conditional step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_conditional_step",
		"Add a conditional step to the plan. Use this for if/else logic based on runtime conditions. Conditional steps evaluate a question and execute different branch steps based on the result. They do NOT execute the step itself - only evaluate the condition. Provide: id, title, condition_question, if_true_steps, if_false_steps, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		conditionalParams,
		createAddConditionalStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_conditional_step tool: %w", err), nil)
	}

	decisionSchema := getAddDecisionStepSchema()
	decisionParams, err := parseSchemaForToolParameters(decisionSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse decision step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_decision_step",
		"Add a decision step to the plan. Use this when you need to execute a step first, evaluate its output, and route based on the result. Decision steps EXECUTE a single step, then evaluate the output to determine routing. Provide: id, title, decision_step (the step to execute), decision_evaluation_question, if_true_next_step_id, if_false_next_step_id, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		decisionParams,
		createAddDecisionStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_decision_step tool: %w", err), nil)
	}

	orchestrationSchema := getAddOrchestrationStepSchema()
	orchestrationParams, err := parseSchemaForToolParameters(orchestrationSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse orchestration step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_orchestration_step",
		"Add an orchestration step to the plan. Use this when you need an orchestrator that can choose between multiple sub-agents based on conditions. Orchestration steps EXECUTE a main orchestrator step, analyze the situation, and select one of multiple sub-agents to call based on step description and success criteria. The main orchestrator loops until its success criteria are met. Sub-agents are private to the orchestration step and execute without validation. Provide: id, title, orchestration_step (the main orchestrator step), orchestration_routes (array of routes with conditions and sub-agent steps), next_step_id, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		orchestrationParams,
		createAddOrchestrationStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_orchestration_step tool: %w", err), nil)
	}

	loopSchema := getAddLoopStepSchema()
	loopParams, err := parseSchemaForToolParameters(loopSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse loop step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_loop_step",
		"Add a loop step to the plan. Use this for steps that need to repeat until a condition is met (polling, retrying, waiting). Provide: id, title, description, success_criteria, context_output, loop_condition, loop_description, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		loopParams,
		createAddLoopStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_loop_step tool: %w", err), nil)
	}

	// Register conditional step tools
	convertToConditionalSchema := getConvertStepToConditionalSchema()
	convertToConditionalParams, err := parseSchemaForToolParameters(convertToConditionalSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse convert_step_to_conditional schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"convert_step_to_conditional",
		"Convert a regular step to a conditional step with if/else branches. Provide step_id, condition_question, condition_context (optional), if_true_steps, and if_false_steps. The step will become a conditional decision point that executes one branch based on the condition evaluation.",
		convertToConditionalParams,
		createConvertStepToConditionalExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register convert_step_to_conditional tool: %w", err), nil)
	}

	addBranchStepsSchema := getAddBranchStepsSchema()
	addBranchStepsParams, err := parseSchemaForToolParameters(addBranchStepsSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse add_branch_steps schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_branch_steps",
		"Add new steps to a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type ('if_true' or 'if_false'), and new_steps array. The steps will be appended to the specified branch.",
		addBranchStepsParams,
		createAddBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_branch_steps tool: %w", err), nil)
	}

	updateBranchStepsSchema := getUpdateBranchStepsSchema()
	updateBranchStepsParams, err := parseSchemaForToolParameters(updateBranchStepsSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update_branch_steps schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_branch_steps",
		"Update existing steps within a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type, and updated_steps array. For each step, provide existing_step_id (required) and only include fields you want to change.",
		updateBranchStepsParams,
		createUpdateBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_branch_steps tool: %w", err), nil)
	}

	deleteBranchStepsSchema := getDeleteBranchStepsSchema()
	deleteBranchStepsParams, err := parseSchemaForToolParameters(deleteBranchStepsSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse delete_branch_steps schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_branch_steps",
		"Delete steps from a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type, and deleted_step_ids array. Use the step's id field from the plan.",
		deleteBranchStepsParams,
		createDeleteBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register delete_branch_steps tool: %w", err), nil)
	}

	convertToRegularSchema := getConvertConditionalToRegularSchema()
	convertToRegularParams, err := parseSchemaForToolParameters(convertToRegularSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse convert_conditional_to_regular schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"convert_conditional_to_regular",
		"Convert a conditional step back to a regular step. This removes all conditional properties and branch steps. Provide step_id of the conditional step to convert.",
		convertToRegularParams,
		createConvertConditionalToRegularExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register convert_conditional_to_regular tool: %w", err), nil)
	}

	// Register orchestration route management tools
	addOrchestrationRouteSchema := getAddOrchestrationRouteSchema()
	addOrchestrationRouteParams, err := parseSchemaForToolParameters(addOrchestrationRouteSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse add_orchestration_route schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_orchestration_route",
		"Add a new route (sub-agent) to an orchestration step. Provide parent_step_id and new_route with all required fields (route_id, route_name, condition, sub_agent_step). The plan.json file is updated immediately when this tool is called.",
		addOrchestrationRouteParams,
		createAddOrchestrationRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_orchestration_route tool: %w", err), nil)
	}

	updateOrchestrationRouteSchema := getUpdateOrchestrationRouteSchema()
	updateOrchestrationRouteParams, err := parseSchemaForToolParameters(updateOrchestrationRouteSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update_orchestration_route schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_orchestration_route",
		"Update an existing route (sub-agent) within an orchestration step. Provide parent_step_id, existing_route_id, and only include the fields you want to change (route_name, condition, sub_agent_step, context_to_pass). The plan.json file is updated immediately when this tool is called.",
		updateOrchestrationRouteParams,
		createUpdateOrchestrationRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_orchestration_route tool: %w", err), nil)
	}

	deleteOrchestrationRouteSchema := getDeleteOrchestrationRouteSchema()
	deleteOrchestrationRouteParams, err := parseSchemaForToolParameters(deleteOrchestrationRouteSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse delete_orchestration_route schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_orchestration_route",
		"Delete a route (sub-agent) from an orchestration step. Provide parent_step_id and deleted_route_id. NOTE: The orchestration step must have at least one route remaining after deletion. The plan.json file is updated immediately when this tool is called.",
		deleteOrchestrationRouteParams,
		createDeleteOrchestrationRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register delete_orchestration_route tool: %w", err), nil)
	}

	// Register validation schema and success criteria update tools
	updateValidationSchemaSchema := getUpdateValidationSchemaSchema()
	updateValidationSchemaParams, err := parseSchemaForToolParameters(updateValidationSchemaSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update_validation_schema schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_validation_schema",
		"Update the validation schema for an existing step in the plan. Provide existing_step_id (required) and validation_schema (required). The validation schema enables fast code-based pre-validation before LLM validation. The plan.json file is updated immediately when this tool is called.",
		updateValidationSchemaParams,
		createUpdateValidationSchemaExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_validation_schema tool: %w", err), nil)
	}

	updateSuccessCriteriaSchema := getUpdateSuccessCriteriaSchema()
	updateSuccessCriteriaParams, err := parseSchemaForToolParameters(updateSuccessCriteriaSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update_success_criteria schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_success_criteria",
		"Update the success criteria for an existing step in the plan. Provide existing_step_id (required) and success_criteria (required). Success criteria should focus on EXECUTION-BASED validation - what work was actually done, not just file structure. The plan.json file is updated immediately when this tool is called.",
		updateSuccessCriteriaParams,
		createUpdateSuccessCriteriaExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_success_criteria tool: %w", err), nil)
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("✅ Registered all plan modification tools for %s", agentName))
	}

	return nil
}

// getExtractVariablesSchema returns the JSON schema for extract_variables tool
func getExtractVariablesSchema() string {
	return `{
		"type": "object",
		"properties": {
			"text": {
				"type": "string",
				"description": "Text or objective to extract variables from. Look for hard-coded values like URLs, account IDs, ports, credentials, resource names, environment values, hosts/endpoints, specific identifiers, paths, and configurations."
			}
		},
		"required": ["text"]
	}`
}

// createExtractVariablesExecutor creates an executor function for extract_variables tool
func createExtractVariablesExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract text to analyze
		textRaw, ok := args["text"].(string)
		if !ok || textRaw == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid text argument"), nil)
		}
		text := textRaw

		// Check if variables.json already exists, create if it doesn't
		manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
		if err != nil {
			// Variables file doesn't exist - create new manifest
			manifest = &VariablesManifest{
				Variables:      []Variable{},
				Groups:         []VariableGroup{},
				ExtractionDate: time.Now().Format(time.RFC3339),
				Objective:      "", // Not important anymore, but keep for backward compatibility
			}
			// Write the new manifest
			if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("failed to create variables.json: %w", err), nil)
			}
		}

		// Log the extraction request
		textPreview := text
		if len(text) > 100 {
			textPreview = text[:100] + "..."
		}
		logger.Info(fmt.Sprintf("📝 Extract variables tool called with text: %s", textPreview))

		// Write changelog entry for extraction initiation
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"text_preview":             textPreview,
			"existing_variables_count": len(manifest.Variables),
		})
		changelogEntry := VariableChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "extraction",
			Description: fmt.Sprintf("Variable extraction initiated from text (existing variables: %d)", len(manifest.Variables)),
			Details:     string(detailsJSON),
			Changes:     []VariableFieldChange{},
		}
		if err := writeVariableChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write variable changelog entry: %v", err))
		}

		// Return guidance for the LLM to extract variables
		// The LLM should analyze the text and use update_variable tool to add each variable
		return fmt.Sprintf("Variables file ready. Analyze the provided text and extract hard-coded values as variables. Use update_variable tool with action='add' to add each extracted variable. Look for: URLs, account IDs, ports, credentials, resource names, environment values, hosts/endpoints, specific identifiers, paths, and configurations. For each value found, create a variable with UPPER_SNAKE_CASE name, the original value, and a clear description. Current variables file has %d variables.", len(manifest.Variables)), nil
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// registerVariableExtractionTools registers variable extraction and management tools
// This allows the planning agent to extract and manage variables based on human input
func registerVariableExtractionTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	agentName string, // e.g., "planning agent"
) error {
	// Register extract_variables tool
	extractSchema := getExtractVariablesSchema()
	extractParams, err := parseSchemaForToolParameters(extractSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse extract_variables schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"extract_variables",
		"Extract variables from text or objective. Provide the text to analyze, and the tool will guide you to extract hard-coded values (URLs, account IDs, ports, credentials, resource names, etc.) as variables. After extraction, use update_variable tool to add each variable.",
		extractParams,
		createExtractVariablesExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register extract_variables tool: %w", err), nil)
	}

	// Register update_variable tool (reuse from variable_extraction_agent.go)
	updateVariableSchema := getUpdateVariableSchema()
	updateVariableParams, err := parseSchemaForToolParameters(updateVariableSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update_variable schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_variable",
		"Update, add, or delete variables in variables.json. Provide action (required: 'update', 'add', or 'delete'), existing_variable_name (required for update/delete), and fields to update. The variables.json file is updated immediately when this tool is called.",
		updateVariableParams,
		createUpdateVariableExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_variable tool: %w", err), nil)
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("✅ Registered variable extraction tools for %s", agentName))
	}

	return nil
}

// createConvertStepToConditionalExecutor creates an executor function for convert_step_to_conditional tool
func createConvertStepToConditionalExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, ok := args["step_id"].(string)
		if !ok || stepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing step_id"), nil)
		}

		conditionQuestion, ok := args["condition_question"].(string)
		if !ok || conditionQuestion == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing condition_question"), nil)
		}

		conditionContext, _ := args["condition_context"].(string) // Optional

		// Extract if_true_steps and if_false_steps
		ifTrueStepsRaw, ok := args["if_true_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid if_true_steps argument"), nil)
		}
		ifFalseStepsRaw, ok := args["if_false_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid if_false_steps argument"), nil)
		}

		// Convert to typed steps
		ifTrueSteps := make([]PlanStepInterface, 0, len(ifTrueStepsRaw))
		for _, stepRaw := range ifTrueStepsRaw {
			stepMap, ok := stepRaw.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf(fmt.Sprintf("invalid step in if_true_steps"), nil)
			}
			// Ensure type field is set
			if _, hasType := stepMap["type"]; !hasType {
				stepMap["type"] = "regular" // Default to regular if not specified
			}
			typedStep, err := convertMapToStep(stepMap)
			if err != nil {
				return "", fmt.Errorf(fmt.Sprintf("failed to parse if_true step: %w", err), nil)
			}
			ifTrueSteps = append(ifTrueSteps, typedStep)
		}

		ifFalseSteps := make([]PlanStepInterface, 0, len(ifFalseStepsRaw))
		for _, stepRaw := range ifFalseStepsRaw {
			stepMap, ok := stepRaw.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf(fmt.Sprintf("invalid step in if_false_steps"), nil)
			}
			// Ensure type field is set
			if _, hasType := stepMap["type"]; !hasType {
				stepMap["type"] = "regular" // Default to regular if not specified
			}
			typedStep, err := convertMapToStep(stepMap)
			if err != nil {
				return "", fmt.Errorf(fmt.Sprintf("failed to parse if_false step: %w", err), nil)
			}
			ifFalseSteps = append(ifFalseSteps, typedStep)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the step to convert by ID
		var stepToConvert PlanStepInterface
		stepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == stepID {
				stepToConvert = step
				stepIndex = i
				break
			}
		}
		if stepToConvert == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs), nil)
		}

		// Validate nesting depth for branch steps (starting from depth 1 since this step becomes conditional)
		for _, branchStep := range ifTrueSteps {
			if err := validateNestingDepth(branchStep, 1); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("if_true_steps validation failed: %w", err), nil)
			}
		}
		for _, branchStep := range ifFalseSteps {
			if err := validateNestingDepth(branchStep, 1); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("if_false_steps validation failed: %w", err), nil)
			}
		}

		// Capture old values BEFORE converting (for changelog)
		oldConditionQuestion := ""
		oldConditionContext := ""
		oldIfTrueSteps := []PlanStepInterface{}
		oldIfFalseSteps := []PlanStepInterface{}
		if conditionalStep, ok := stepToConvert.(*ConditionalPlanStep); ok {
			oldConditionQuestion = conditionalStep.ConditionQuestion
			oldConditionContext = conditionalStep.ConditionContext
			oldIfTrueSteps = conditionalStep.IfTrueSteps
			oldIfFalseSteps = conditionalStep.IfFalseSteps
		}

		// Convert step to conditional (create new ConditionalPlanStep)
		conditionalStep := &ConditionalPlanStep{
			Type: StepTypeConditional,
			CommonStepFields: CommonStepFields{
				ID:    stepToConvert.GetID(),
				Title: stepToConvert.GetTitle(),
			},
			ConditionQuestion: conditionQuestion,
			ConditionContext:  conditionContext,
			IfTrueSteps:       ifTrueSteps,
			IfFalseSteps:      ifFalseSteps,
		}
		plan.Steps[stepIndex] = conditionalStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		branchStepIDs := make([]string, 0)
		for _, step := range ifTrueSteps {
			branchStepIDs = append(branchStepIDs, step.GetID())
		}
		for _, step := range ifFalseSteps {
			branchStepIDs = append(branchStepIDs, step.GetID())
		}
		fieldChanges := make([]PlanFieldChange, 0)
		// Check if step was already conditional
		wasConditional := false
		if _, ok := stepToConvert.(*ConditionalPlanStep); ok {
			wasConditional = true
		}
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "has_condition",
			OldValue: wasConditional,
			NewValue: true,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "condition_question",
			OldValue: oldConditionQuestion,
			NewValue: conditionQuestion,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "condition_context",
			OldValue: oldConditionContext,
			NewValue: conditionContext,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "if_true_steps",
			OldValue: oldIfTrueSteps,
			NewValue: ifTrueSteps,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "if_false_steps",
			OldValue: oldIfFalseSteps,
			NewValue: ifFalseSteps,
		})
		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		logger.Info(fmt.Sprintf("✅ Converted step '%s' to conditional with %d true branch steps and %d false branch steps", stepToConvert.GetTitle(), len(ifTrueSteps), len(ifFalseSteps)))
		return fmt.Sprintf("Successfully converted step '%s' to conditional", stepToConvert.GetTitle()), nil
	}
}

// createAddBranchStepsExecutor creates an executor function for add_branch_steps tool
func createAddBranchStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf(fmt.Sprintf("invalid branch_type: must be 'if_true' or 'if_false'"), nil)
		}

		newStepsRaw, ok := args["new_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid new_steps argument"), nil)
		}

		// Convert to typed steps
		newSteps := make([]PlanStepInterface, 0, len(newStepsRaw))
		for _, stepRaw := range newStepsRaw {
			stepMap, ok := stepRaw.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf(fmt.Sprintf("invalid step in new_steps"), nil)
			}
			// Ensure type field is set
			if _, hasType := stepMap["type"]; !hasType {
				stepMap["type"] = "regular" // Default to regular if not specified
			}
			typedStep, err := convertMapToStep(stepMap)
			if err != nil {
				return "", fmt.Errorf(fmt.Sprintf("failed to parse new step: %w", err), nil)
			}
			newSteps = append(newSteps, typedStep)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent conditional step by ID
		var parentStep PlanStepInterface
		parentStepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == parentStepID {
				parentStep = step
				parentStepIndex = i
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		conditionalStep, ok := parentStep.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", parentStepID), nil)
		}

		// Validate that all new branch steps have IDs (required for config matching)
		for i, newStep := range newSteps {
			if newStep.GetID() == "" {
				return "", fmt.Errorf(fmt.Sprintf("branch step at index %d is missing required ID field. Step title: %q", i, newStep.GetTitle()), nil)
			}
		}

		// Validate nesting depth for new steps (starting from depth 1 since they're being added to a conditional)
		for _, newStep := range newSteps {
			if err := validateNestingDepth(newStep, 1); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("new_steps validation failed: %w", err), nil)
			}
		}

		// Capture old branch steps BEFORE adding (for changelog)
		var oldBranchSteps []PlanStepInterface
		if branchType == "if_true" {
			oldBranchSteps = make([]PlanStepInterface, len(conditionalStep.IfTrueSteps))
			copy(oldBranchSteps, conditionalStep.IfTrueSteps)
			conditionalStep.IfTrueSteps = append(conditionalStep.IfTrueSteps, newSteps...)
		} else {
			oldBranchSteps = make([]PlanStepInterface, len(conditionalStep.IfFalseSteps))
			copy(oldBranchSteps, conditionalStep.IfFalseSteps)
			conditionalStep.IfFalseSteps = append(conditionalStep.IfFalseSteps, newSteps...)
		}
		plan.Steps[parentStepIndex] = conditionalStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		logger.Info(fmt.Sprintf("✅ Added %d steps to %s branch of conditional step '%s'", len(newSteps), branchType, conditionalStep.Title))
		return fmt.Sprintf("Successfully added %d step(s) to %s branch of conditional step '%s'", len(newSteps), branchType, conditionalStep.Title), nil
	}
}

// createUpdateBranchStepsExecutor creates an executor function for update_branch_steps tool
func createUpdateBranchStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf(fmt.Sprintf("invalid branch_type: must be 'if_true' or 'if_false'"), nil)
		}

		updatedStepsRaw, ok := args["updated_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid updated_steps argument"), nil)
		}

		// Convert to JSON and unmarshal to PartialPlanStep array
		updatedStepsJSON, err := json.Marshal(updatedStepsRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal updated_steps: %w", err), nil)
		}
		var partialUpdates []PartialPlanStep
		if err := json.Unmarshal(updatedStepsJSON, &partialUpdates); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse updated_steps: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent conditional step by ID
		var parentStep PlanStepInterface
		parentStepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == parentStepID {
				parentStep = step
				parentStepIndex = i
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		conditionalStep, ok := parentStep.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", parentStepID), nil)
		}

		// Get the appropriate branch
		var branchSteps *[]PlanStepInterface
		if branchType == "if_true" {
			branchSteps = &conditionalStep.IfTrueSteps
		} else {
			branchSteps = &conditionalStep.IfFalseSteps
		}

		// Create map of existing branch steps by ID
		existingStepsMap := make(map[string]PlanStepInterface)
		for i, step := range *branchSteps {
			existingStepsMap[step.GetID()] = (*branchSteps)[i]
		}

		// Track changes for changelog
		updatedBranchStepIDs := make([]string, 0, len(partialUpdates))
		fieldChanges := make([]PlanFieldChange, 0)

		// Apply updates
		for _, partialUpdate := range partialUpdates {
			existingStep, exists := existingStepsMap[partialUpdate.ExistingStepID]
			if !exists {
				availableIDs := make([]string, 0, len(*branchSteps))
				for _, step := range *branchSteps {
					availableIDs = append(availableIDs, step.GetID())
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in %s branch. Available step IDs: %v", partialUpdate.ExistingStepID, branchType, availableIDs), nil)
			}

			// Track old values before updating (using interface methods)
			updatedBranchStepIDs = append(updatedBranchStepIDs, partialUpdate.ExistingStepID)

			// Track each field change with old and new values
			if partialUpdate.Title != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "title",
					OldValue: existingStep.GetTitle(),
					NewValue: partialUpdate.Title,
				})
			}
			if partialUpdate.Description != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "description",
					OldValue: existingStep.GetDescription(),
					NewValue: partialUpdate.Description,
				})
			}
			if partialUpdate.SuccessCriteria != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "success_criteria",
					OldValue: existingStep.GetSuccessCriteria(),
					NewValue: partialUpdate.SuccessCriteria,
				})
			}
			if partialUpdate.ContextDependencies != nil {
				oldDeps := existingStep.GetContextDependencies()
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "context_dependencies",
					OldValue: fmt.Sprintf("%v", oldDeps),
					NewValue: fmt.Sprintf("%v", partialUpdate.ContextDependencies),
				})
			}
			if partialUpdate.ContextOutput != "" {
				oldOutput := existingStep.GetContextOutput()
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "context_output",
					OldValue: oldOutput.String(),
					NewValue: partialUpdate.ContextOutput,
				})
			}
			if partialUpdate.HasLoop != nil {
				oldHasLoop := false
				if regularStep, ok := existingStep.(*RegularPlanStep); ok {
					oldHasLoop = regularStep.HasLoop
				}
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "has_loop",
					OldValue: fmt.Sprintf("%v", oldHasLoop),
					NewValue: fmt.Sprintf("%v", *partialUpdate.HasLoop),
				})
			}
			if partialUpdate.LoopCondition != "" {
				oldLoopCondition := ""
				if regularStep, ok := existingStep.(*RegularPlanStep); ok {
					oldLoopCondition = regularStep.LoopCondition
				}
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "loop_condition",
					OldValue: oldLoopCondition,
					NewValue: partialUpdate.LoopCondition,
				})
			}
			if partialUpdate.MaxIterations != nil {
				oldMaxIterations := 0
				if regularStep, ok := existingStep.(*RegularPlanStep); ok {
					oldMaxIterations = regularStep.MaxIterations
				}
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "max_iterations",
					OldValue: fmt.Sprintf("%d", oldMaxIterations),
					NewValue: fmt.Sprintf("%d", *partialUpdate.MaxIterations),
				})
			}
			if partialUpdate.LoopDescription != "" {
				oldLoopDescription := ""
				if regularStep, ok := existingStep.(*RegularPlanStep); ok {
					oldLoopDescription = regularStep.LoopDescription
				}
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "loop_description",
					OldValue: oldLoopDescription,
					NewValue: partialUpdate.LoopDescription,
				})
			}

			// Merge partial update and update the branch step
			updatedStep := mergePartialStepUpdate(existingStep, partialUpdate)
			// Find the index in the branch steps and update it
			for i, step := range *branchSteps {
				if step.GetID() == partialUpdate.ExistingStepID {
					(*branchSteps)[i] = updatedStep
					break
				}
			}

			// Validate nesting depth after update
			if err := validateNestingDepth(updatedStep, 1); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("updated step validation failed: %w", err), nil)
			}
		}

		// Update the conditional step in the plan
		plan.Steps[parentStepIndex] = conditionalStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry
		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		logger.Info(fmt.Sprintf("✅ Updated %d steps in %s branch of conditional step '%s'", len(partialUpdates), branchType, conditionalStep.Title))
		return fmt.Sprintf("Successfully updated %d step(s) in %s branch of conditional step '%s'", len(partialUpdates), branchType, conditionalStep.Title), nil
	}
}

// createDeleteBranchStepsExecutor creates an executor function for delete_branch_steps tool
func createDeleteBranchStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf(fmt.Sprintf("invalid branch_type: must be 'if_true' or 'if_false'"), nil)
		}

		deletedIDsRaw, ok := args["deleted_step_ids"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid deleted_step_ids argument"), nil)
		}

		// Convert to string array
		deletedIDs := make([]string, 0, len(deletedIDsRaw))
		for _, id := range deletedIDsRaw {
			if idStr, ok := id.(string); ok {
				deletedIDs = append(deletedIDs, idStr)
			} else {
				return "", fmt.Errorf(fmt.Sprintf("invalid step ID in deleted_step_ids: %v", id), nil)
			}
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent conditional step by ID
		var parentStep PlanStepInterface
		parentStepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == parentStepID {
				parentStep = step
				parentStepIndex = i
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		conditionalStep, ok := parentStep.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", parentStepID), nil)
		}

		// Get the appropriate branch
		var branchSteps *[]PlanStepInterface
		if branchType == "if_true" {
			branchSteps = &conditionalStep.IfTrueSteps
		} else {
			branchSteps = &conditionalStep.IfFalseSteps
		}

		// Capture old branch steps BEFORE deleting (for changelog) - marshal to JSON
		oldBranchStepsJSON := make([]json.RawMessage, 0, len(*branchSteps))
		for _, step := range *branchSteps {
			stepJSON, err := json.Marshal(step)
			if err == nil {
				oldBranchStepsJSON = append(oldBranchStepsJSON, stepJSON)
			}
		}

		// Create set of deleted step IDs
		deletedSet := make(map[string]bool)
		for _, id := range deletedIDs {
			deletedSet[id] = true
		}

		// Validate that all deleted steps exist
		existingStepsMap := make(map[string]bool)
		for _, step := range *branchSteps {
			existingStepsMap[step.GetID()] = true
		}
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				availableIDs := make([]string, 0, len(*branchSteps))
				for _, step := range *branchSteps {
					availableIDs = append(availableIDs, step.GetID())
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in %s branch (cannot delete). Available step IDs: %v", id, branchType, availableIDs), nil)
			}
		}

		// Filter out deleted steps
		filteredSteps := make([]PlanStepInterface, 0, len(*branchSteps))
		for _, step := range *branchSteps {
			if !deletedSet[step.GetID()] {
				filteredSteps = append(filteredSteps, step)
			}
		}

		// Update the branch
		if branchType == "if_true" {
			conditionalStep.IfTrueSteps = filteredSteps
		} else {
			conditionalStep.IfFalseSteps = filteredSteps
		}
		plan.Steps[parentStepIndex] = conditionalStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		fieldChanges := make([]PlanFieldChange, 0)
		fieldName := fmt.Sprintf("%s_steps", branchType) // "if_true_steps" or "if_false_steps"
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   parentStepID,
			Field:    fieldName,
			OldValue: fmt.Sprintf("%d steps", len(oldBranchStepsJSON)),
			NewValue: fmt.Sprintf("%d steps", len(filteredSteps)),
		})
		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		logger.Info(fmt.Sprintf("✅ Deleted %d steps from %s branch of conditional step '%s'", len(deletedIDs), branchType, conditionalStep.Title))
		return fmt.Sprintf("Successfully deleted %d step(s) from %s branch of conditional step '%s'", len(deletedIDs), branchType, conditionalStep.Title), nil
	}
}

// createAddOrchestrationRouteExecutor creates an executor function for add_orchestration_route tool
func createAddOrchestrationRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		newRouteRaw, ok := args["new_route"].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid new_route argument"), nil)
		}

		// Convert to JSON and unmarshal to PlanOrchestrationRoute
		newRouteJSON, err := json.Marshal(newRouteRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal new_route: %w", err), nil)
		}
		var newRoute PlanOrchestrationRoute
		if err := json.Unmarshal(newRouteJSON, &newRoute); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse new_route: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent orchestration step by ID
		var parentStep PlanStepInterface
		parentStepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == parentStepID {
				parentStep = step
				parentStepIndex = i
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		orchestrationStep, ok := parentStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not an orchestration step", parentStepID), nil)
		}

		// Validate that the new route has a route_id
		if newRoute.RouteID == "" {
			return "", fmt.Errorf(fmt.Sprintf("new route is missing required route_id field"), nil)
		}

		// Check if route_id already exists
		for _, existingRoute := range orchestrationStep.OrchestrationRoutes {
			if existingRoute.RouteID == newRoute.RouteID {
				return "", fmt.Errorf(fmt.Sprintf("route with route_id '%s' already exists in orchestration step '%s'", newRoute.RouteID, parentStepID), nil)
			}
		}

		// Validate that sub_agent_step has required fields
		if newRoute.SubAgentStep != nil && newRoute.SubAgentStep.GetID() == "" {
			return "", fmt.Errorf(fmt.Sprintf("sub_agent_step is missing required ID field"), nil)
		}

		// Capture old routes BEFORE adding (for changelog)
		oldRoutes := make([]PlanOrchestrationRoute, len(orchestrationStep.OrchestrationRoutes))
		copy(oldRoutes, orchestrationStep.OrchestrationRoutes)

		// Add new route
		orchestrationStep.OrchestrationRoutes = append(orchestrationStep.OrchestrationRoutes, newRoute)
		plan.Steps[parentStepIndex] = orchestrationStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		logger.Info(fmt.Sprintf("✅ Added route '%s' (ID: %s) to orchestration step '%s'", newRoute.RouteName, newRoute.RouteID, orchestrationStep.Title))
		return fmt.Sprintf("Successfully added route '%s' (ID: %s) to orchestration step '%s'", newRoute.RouteName, newRoute.RouteID, orchestrationStep.Title), nil
	}
}

// createUpdateOrchestrationRouteExecutor creates an executor function for update_orchestration_route tool
func createUpdateOrchestrationRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		existingRouteID, ok := args["existing_route_id"].(string)
		if !ok || existingRouteID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing existing_route_id"), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent orchestration step by ID
		var parentStep PlanStepInterface
		parentStepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == parentStepID {
				parentStep = step
				parentStepIndex = i
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		orchestrationStep, ok := parentStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not an orchestration step", parentStepID), nil)
		}

		// Find the route to update
		var routeToUpdate *PlanOrchestrationRoute
		routeIndex := -1
		for i := range orchestrationStep.OrchestrationRoutes {
			if orchestrationStep.OrchestrationRoutes[i].RouteID == existingRouteID {
				routeToUpdate = &orchestrationStep.OrchestrationRoutes[i]
				routeIndex = i
				break
			}
		}
		if routeToUpdate == nil {
			availableRouteIDs := make([]string, 0, len(orchestrationStep.OrchestrationRoutes))
			for _, route := range orchestrationStep.OrchestrationRoutes {
				availableRouteIDs = append(availableRouteIDs, route.RouteID)
			}
			return "", fmt.Errorf(fmt.Sprintf("route with route_id '%s' not found in orchestration step '%s'. Available route IDs: %v", existingRouteID, parentStepID, availableRouteIDs), nil)
		}

		// Track field changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Update fields if provided
		if routeName, ok := args["route_name"].(string); ok && routeName != "" {
			fieldChanges = append(fieldChanges, PlanFieldChange{
				StepID:   parentStepID,
				Field:    fmt.Sprintf("orchestration_routes[%d].route_name", routeIndex),
				OldValue: routeToUpdate.RouteName,
				NewValue: routeName,
			})
			routeToUpdate.RouteName = routeName
		}

		if condition, ok := args["condition"].(string); ok && condition != "" {
			fieldChanges = append(fieldChanges, PlanFieldChange{
				StepID:   parentStepID,
				Field:    fmt.Sprintf("orchestration_routes[%d].condition", routeIndex),
				OldValue: routeToUpdate.Condition,
				NewValue: condition,
			})
			routeToUpdate.Condition = condition
		}

		if contextToPass, ok := args["context_to_pass"].(string); ok {
			fieldChanges = append(fieldChanges, PlanFieldChange{
				StepID:   parentStepID,
				Field:    fmt.Sprintf("orchestration_routes[%d].context_to_pass", routeIndex),
				OldValue: routeToUpdate.ContextToPass,
				NewValue: contextToPass,
			})
			routeToUpdate.ContextToPass = contextToPass
		}

		// Handle sub_agent_step update
		if subAgentStepRaw, ok := args["sub_agent_step"].(map[string]interface{}); ok {
			// Ensure type field is set
			if _, hasType := subAgentStepRaw["type"]; !hasType {
				subAgentStepRaw["type"] = "regular" // Default to regular if not specified
			}
			updatedSubAgentStep, err := convertMapToStep(subAgentStepRaw)
			if err != nil {
				return "", fmt.Errorf(fmt.Sprintf("failed to parse sub_agent_step: %w", err), nil)
			}

			oldSubAgentStepID := ""
			if routeToUpdate.SubAgentStep != nil {
				oldSubAgentStepID = routeToUpdate.SubAgentStep.GetID()
			}
			fieldChanges = append(fieldChanges, PlanFieldChange{
				StepID:   parentStepID,
				Field:    fmt.Sprintf("orchestration_routes[%d].sub_agent_step", routeIndex),
				OldValue: oldSubAgentStepID,
				NewValue: updatedSubAgentStep.GetID(),
			})
			routeToUpdate.SubAgentStep = updatedSubAgentStep
		}

		// Update the orchestration step in the plan
		plan.Steps[parentStepIndex] = orchestrationStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		logger.Info(fmt.Sprintf("✅ Updated route '%s' (ID: %s) in orchestration step '%s'", routeToUpdate.RouteName, existingRouteID, orchestrationStep.Title))
		return fmt.Sprintf("Successfully updated route '%s' (ID: %s) in orchestration step '%s'", routeToUpdate.RouteName, existingRouteID, orchestrationStep.Title), nil
	}
}

// createDeleteOrchestrationRouteExecutor creates an executor function for delete_orchestration_route tool
func createDeleteOrchestrationRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		deletedRouteID, ok := args["deleted_route_id"].(string)
		if !ok || deletedRouteID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing deleted_route_id"), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent orchestration step by ID
		var parentStep PlanStepInterface
		parentStepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == parentStepID {
				parentStep = step
				parentStepIndex = i
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		orchestrationStep, ok := parentStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not an orchestration step", parentStepID), nil)
		}

		// Validate that we have at least one route remaining after deletion
		if len(orchestrationStep.OrchestrationRoutes) <= 1 {
			return "", fmt.Errorf(fmt.Sprintf("cannot delete route '%s' - orchestration step must have at least one route", deletedRouteID), nil)
		}

		// Find the route to delete
		var deletedRoute *PlanOrchestrationRoute
		routeIndex := -1
		for i, route := range orchestrationStep.OrchestrationRoutes {
			if route.RouteID == deletedRouteID {
				deletedRoute = &orchestrationStep.OrchestrationRoutes[i]
				routeIndex = i
				break
			}
		}
		if deletedRoute == nil {
			availableRouteIDs := make([]string, 0, len(orchestrationStep.OrchestrationRoutes))
			for _, route := range orchestrationStep.OrchestrationRoutes {
				availableRouteIDs = append(availableRouteIDs, route.RouteID)
			}
			return "", fmt.Errorf(fmt.Sprintf("route with route_id '%s' not found in orchestration step '%s'. Available route IDs: %v", deletedRouteID, parentStepID, availableRouteIDs), nil)
		}

		// Capture old routes BEFORE deleting (for changelog)
		oldRoutes := make([]PlanOrchestrationRoute, len(orchestrationStep.OrchestrationRoutes))
		copy(oldRoutes, orchestrationStep.OrchestrationRoutes)

		// Remove the route
		orchestrationStep.OrchestrationRoutes = append(
			orchestrationStep.OrchestrationRoutes[:routeIndex],
			orchestrationStep.OrchestrationRoutes[routeIndex+1:]...,
		)
		plan.Steps[parentStepIndex] = orchestrationStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		logger.Info(fmt.Sprintf("✅ Deleted route '%s' (ID: %s) from orchestration step '%s'", deletedRoute.RouteName, deletedRouteID, orchestrationStep.Title))
		return fmt.Sprintf("Successfully deleted route '%s' (ID: %s) from orchestration step '%s'", deletedRoute.RouteName, deletedRouteID, orchestrationStep.Title), nil
	}
}

// createConvertConditionalToRegularExecutor creates an executor function for convert_conditional_to_regular tool
func createConvertConditionalToRegularExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, ok := args["step_id"].(string)
		if !ok || stepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing step_id"), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the conditional step by ID
		var stepToConvert PlanStepInterface
		stepIndex := -1
		for i, step := range plan.Steps {
			if step.GetID() == stepID {
				stepToConvert = step
				stepIndex = i
				break
			}
		}
		if stepToConvert == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs), nil)
		}

		conditionalStep, ok := stepToConvert.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", stepID), nil)
		}

		// Capture old values BEFORE converting (for changelog)
		oldConditionQuestion := conditionalStep.ConditionQuestion
		oldConditionContext := conditionalStep.ConditionContext
		oldIfTrueStepsCount := len(conditionalStep.IfTrueSteps)
		oldIfFalseStepsCount := len(conditionalStep.IfFalseSteps)

		// Convert to regular step (create new RegularPlanStep)
		regularStep := &RegularPlanStep{
			Type: StepTypeRegular,
			CommonStepFields: CommonStepFields{
				ID:    conditionalStep.ID,
				Title: conditionalStep.Title,
			},
			// Note: RegularPlanStep doesn't have Description, SuccessCriteria, etc. by default
			// These would need to be provided or extracted from somewhere
		}

		// Write updated plan
		plan.Steps[stepIndex] = regularStep
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		fieldChanges := make([]PlanFieldChange, 0)
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "type",
			OldValue: "conditional",
			NewValue: "regular",
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "condition_question",
			OldValue: oldConditionQuestion,
			NewValue: "",
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "condition_context",
			OldValue: oldConditionContext,
			NewValue: "",
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "if_true_steps",
			OldValue: fmt.Sprintf("%d steps", oldIfTrueStepsCount),
			NewValue: "0 steps",
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "if_false_steps",
			OldValue: fmt.Sprintf("%d steps", oldIfFalseStepsCount),
			NewValue: "0 steps",
		})
		// Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)

		logger.Info(fmt.Sprintf("✅ Converted conditional step '%s' back to regular step", regularStep.Title))
		return fmt.Sprintf("Successfully converted conditional step '%s' back to regular step", regularStep.Title), nil
	}
}

// createUpdateValidationSchemaExecutor creates an executor function for update_validation_schema tool
func createUpdateValidationSchemaExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to extract validation schema
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal step: %w", err), nil)
		}

		var updateData struct {
			ExistingStepID   string            `json:"existing_step_id"`
			ValidationSchema *ValidationSchema `json:"validation_schema"`
		}
		if err := json.Unmarshal(stepJSON, &updateData); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse update data: %w", err), nil)
		}

		if updateData.ExistingStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("existing_step_id is required"), nil)
		}
		if updateData.ValidationSchema == nil {
			return "", fmt.Errorf(fmt.Sprintf("validation_schema is required"), nil)
		}

		// Create PartialPlanStep with only validation schema
		partialUpdate := PartialPlanStep{
			ExistingStepID:   updateData.ExistingStepID,
			ValidationSchema: updateData.ValidationSchema,
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		stepIndex, _, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed after update: %w", err), nil)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, updateData.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", updateData.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", updateData.ExistingStepID))
			}
		}

		logger.Info(fmt.Sprintf("✅ Updated validation schema for step '%s' in plan", updateData.ExistingStepID))
		return fmt.Sprintf("Successfully updated validation schema for step '%s' in the plan", updateData.ExistingStepID), nil
	}
}

// createUpdateSuccessCriteriaExecutor creates an executor function for update_success_criteria tool
func createUpdateSuccessCriteriaExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to extract success criteria
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal step: %w", err), nil)
		}

		var updateData struct {
			ExistingStepID  string `json:"existing_step_id"`
			SuccessCriteria string `json:"success_criteria"`
		}
		if err := json.Unmarshal(stepJSON, &updateData); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse update data: %w", err), nil)
		}

		if updateData.ExistingStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("existing_step_id is required"), nil)
		}
		if updateData.SuccessCriteria == "" {
			return "", fmt.Errorf(fmt.Sprintf("success_criteria is required"), nil)
		}

		// Create PartialPlanStep with only success criteria
		partialUpdate := PartialPlanStep{
			ExistingStepID:  updateData.ExistingStepID,
			SuccessCriteria: updateData.SuccessCriteria,
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		stepIndex, _, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed after update: %w", err), nil)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, updateData.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", updateData.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", updateData.ExistingStepID))
			}
		}

		logger.Info(fmt.Sprintf("✅ Updated success criteria for step '%s' in plan", updateData.ExistingStepID))
		return fmt.Sprintf("Successfully updated success criteria for step '%s' in the plan", updateData.ExistingStepID), nil
	}
}

// NOTE: Learning folders are named using step IDs (e.g., learnings/{step_id}/),
// so folders don't need to be renamed when steps are reordered - the step ID stays the same.

// parseSchemaForToolParameters parses a JSON schema string and extracts properties for tool parameters
// This is a local copy of the function from mcpagent to avoid circular dependencies
func parseSchemaForToolParameters(schemaString string) (map[string]interface{}, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(schemaString), &schema); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse schema JSON: %w", err), nil)
	}

	// Extract properties - this becomes the tool parameters
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf(fmt.Sprintf("schema missing 'properties' field or it's not an object"), nil)
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

// Execute implements the OrchestratorAgent interface
// NOTE: This method is not used - planning agent execution is handled in runPlanningPhase() in planning_management.go
// which uses BaseAgent().Execute() directly after registering tools
func (hctppa *HumanControlledTodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", conversationHistory, fmt.Errorf("Execute() is not used for planning agent - use runPlanningPhase() instead")
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

	templateStr := `## 📅 SESSION INFO
**Date**: {{.CurrentDate}} | **Time**: {{.CurrentTime}} | **Workspace**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
**Role**: Planning Agent (Update Mode)
**Task**: Update existing plan based on human feedback
**Tools**: human_feedback → type-specific update/add/delete tools → plan.json updates immediately

---

## ⚠️ CRITICAL RULES (Memorize These)

| Rule | Description |
|------|-------------|
| **1. Confirm First** | ALWAYS use human_feedback tool BEFORE any plan changes |
| **2. File Names Only** | Success criteria & context reference file names (e.g., 'step_1_results.json'), NEVER paths |
| **3. Preserve Variables** | Keep {{"{{"}}VARIABLE_NAME{{"}}"}} placeholders exactly as-is—never substitute values |
| **4. Valid Context Flow** | Each step's context_dependencies may include outputs from one or more earlier steps, but MUST NOT reference outputs from steps that execute after it |
| **5. Preserve Step IDs** | When updating plans, keep existing step id values stable whenever possible; only assign new IDs for truly new steps—do NOT delete and re-add steps just to change IDs |
| **6. Evidence-Based Criteria** | Success criteria must require VERIFIABLE EVIDENCE (counts, lists, data samples), not just status flags. Criteria like 'status: passed' can be gamed by simply editing the flag. |
| **7. Step Folder Isolation** | Each step or sub-agent has write access ONLY to its own step folder. It CANNOT write to other folders. This is a critical security and isolation rule—remember this when creating plans and designing step outputs. |

---

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES
{{.VariableNames}}
Use existing variables only—don't create new placeholders. Plans must work across environments without modification.
{{end}}

## 📄 EXISTING PLAN
{{.ExistingPlanJSON}}

---

## 🧩 HOW TO DESIGN A GREAT PLAN

### 1. Planning Mindset
- **Start from the end**: What files/states must exist when done? (final report, verified sheet, notification sent)
- **Work backwards**: What intermediate artifacts are needed? (raw data → transformed data → written to target → verified)
- **Think in file-flow**: Every transformation produces a named file (context_output) that later steps depend on

### 2. Step Decomposition

| Principle | Description |
|-----------|-------------|
| **One responsibility per step** | Fetch data, transform, write to target, verify, notify—separate concerns |
| **Avoid giant steps** | "Do everything" steps are impossible to validate/debug |
| **Avoid nano-steps** | Dozens of trivial steps create noise; group logically related work |
| **Rule of thumb** | If a sub-task has its own inputs, outputs, and failure modes → it deserves its own step |

### 3. Choosing Step Types

| Type | When to Use |
|------|-------------|
| **Regular** | Standard "do X, produce Y file" (most steps) |
| **Decision** | Execute a step, then route based on pass/fail evaluation (e.g., verify → if pass go to finish, if fail go to retry) |
| **Conditional** | No execution needed—just inspect current context and branch |
| **Loop** | Repeat until a file condition is satisfied (polling, retries, incremental progress) |
| **Orchestration** | Multiple specialized sub-agents needed; system iteratively chooses routes until success |

**⚠️ CRITICAL: Type Field Requirement**
- **ALL steps MUST include a 'type' field** set to one of: "regular", "conditional", "decision", or "orchestration"
- This includes **nested steps** in:
  - if_true_steps and if_false_steps arrays (conditional steps)
  - decision_step object (decision steps)
  - orchestration_step object (orchestration steps)
  - sub_agent_step objects in orchestration_routes (orchestration routes)
- Most nested steps are "regular" type unless they need special behavior

### JSON Format Examples for Each Step Type

**1. Regular Step Format:**
    {
      "type": "regular",
      "id": "deploy-application",
      "title": "Deploy Application",
      "description": "Deploy the application to production environment",
      "success_criteria": "File deployment_results.json contains status field set to 'deployed' AND deployment_id field is present",
      "context_dependencies": ["config.json", "credentials.json"],
      "context_output": "deployment_results.json",
      "has_loop": false,
      "enable_prerequisite_detection": false,
      "prerequisite_rules": []
    }

**2. Conditional Step Format (with nested steps):**
    {
      "type": "conditional",
      "id": "check-deployment-health",
      "title": "Check Deployment Health",
      "description": "Verify if deployment is healthy",
      "success_criteria": "Condition evaluated and branch executed",
      "context_dependencies": ["deployment_results.json"],
      "context_output": "health_check_result.json",
      "condition_question": "Is the deployment healthy and all services running?",
      "condition_context": "Check deployment_results.json for status and service health",
      "if_true_steps": [
        {
          "type": "regular",
          "id": "proceed-to-next",
          "title": "Proceed to Next Step",
          "description": "Deployment is healthy, proceed",
          "success_criteria": "File proceed.json created with status 'ready'",
          "context_dependencies": ["health_check_result.json"],
          "context_output": "proceed.json",
          "has_loop": false
        }
      ],
      "if_false_steps": [
        {
          "type": "regular",
          "id": "handle-failure",
          "title": "Handle Deployment Failure",
          "description": "Deployment failed, handle error",
          "success_criteria": "File error_handled.json created with error details",
          "context_dependencies": ["health_check_result.json"],
          "context_output": "error_handled.json",
          "has_loop": false
        }
      ],
      "if_true_next_step_id": "final-step",
      "if_false_next_step_id": "retry-deployment"
    }

**3. Decision Step Format (with nested decision_step):**
    {
      "type": "decision",
      "id": "evaluate-verification",
      "title": "Evaluate Verification Results",
      "decision_step": {
        "type": "regular",
        "id": "verify-data",
        "title": "Verify Data Integrity",
        "description": "Verify that all data transformations completed correctly",
        "success_criteria": "File verification.json contains detailed verification results with counts and samples",
        "context_dependencies": ["transformed_data.json"],
        "context_output": "verification.json",
        "has_loop": false
      },
      "decision_evaluation_question": "Based on verification.json in context, independently verify: (1) All required fields present, (2) Data counts match expected values, (3) Sample data validates correctly. Do NOT rely on status fields alone.",
      "if_true_next_step_id": "final-step",
      "if_false_next_step_id": "fix-errors"
    }

**4. Orchestration Step Format (with nested orchestration_step and routes):**
    {
      "type": "orchestration",
      "id": "handle-complex-task",
      "title": "Handle Complex Task",
      "orchestration_step": {
        "type": "regular",
        "id": "orchestrator-main",
        "title": "Main Orchestrator",
        "description": "Coordinate complex task execution",
        "success_criteria": "File orchestration_status.json contains completed status",
        "context_dependencies": ["initial_data.json"],
        "context_output": "orchestration_status.json",
        "has_loop": false
      },
      "orchestration_routes": [
        {
          "route_id": "auth-error-handler",
          "route_name": "Authentication Error Handler",
          "condition": "If error is authentication-related",
          "sub_agent_step": {
            "type": "regular",
            "id": "handle-auth-error",
            "title": "Handle Auth Error",
            "description": "Specialized handler for authentication errors",
            "success_criteria": "File auth_fixed.json created with new credentials",
            "context_dependencies": ["orchestration_status.json"],
            "context_output": "auth_fixed.json",
            "has_loop": false
          },
          "context_to_pass": "Focus on authentication errors only"
        },
        {
          "route_id": "data-error-handler",
          "route_name": "Data Error Handler",
          "condition": "If error is data-related",
          "sub_agent_step": {
            "type": "regular",
            "id": "handle-data-error",
            "title": "Handle Data Error",
            "description": "Specialized handler for data errors",
            "success_criteria": "File data_fixed.json created with corrected data",
            "context_dependencies": ["orchestration_status.json"],
            "context_output": "data_fixed.json",
            "has_loop": false
          }
        }
      ],
      "next_step_id": "final-step"
    }

**Key Points:**
- **ALL steps** (including nested ones) MUST have "type" field
- **ALL nested steps** MUST include: type, id, title, description, success_criteria, context_dependencies, has_loop, context_output
- context_dependencies is always an array (use [] if empty)
- context_output is always a string (file name, not path)
- has_loop is always a boolean (typically false)

### 4. Designing Context Flow
- **Forward-only**: Each step writes one context_output; later steps declare those as context_dependencies
- **Name for meaning**: Prefer 'login_session.json', 'sheet_update_results.json' over 'file1.json'
- **Minimal but sufficient**: Only depend on what you actually need—don't add all previous files "just in case"

### 5. Embedding Verification
For **critical operations** (external systems, data changes, money):
1. Plan an explicit **verification step** immediately after the action
2. **Verification step success criteria MUST focus on execution evidence** - what verification work was actually done (e.g., "Agent read source data and recomputed values", "Agent compared results against expected patterns") - NOT just status flags or file structure
3. Add a **decision step** with strict decision_evaluation_question that says: "recompute from the raw evidence, ignore any status fields"
4. Route to fix/retry on failure, proceed on success

**Why execution-based criteria matter**: The execution agent creates both evidence AND status. If criteria only check status or file structure, the agent can "pass" by writing status flags or creating empty files. Focus on execution history - did the agent actually do the verification work?

### 6. Iterating from Feedback/Logs
When feedback says "this failed" or logs show issues:
- Identify **which step's assumptions were wrong** (weak success criteria, missing dependency, wrong branching)
- Prefer **targeted edits** (update success_criteria, split a step, add verification) over rewriting entire plan

---

## 🔄 WORKFLOW

### Step 1: Request Confirmation
Use human_feedback tool with unique UUID. Describe:
- **What**: Which steps to update/delete/add (specific IDs)
- **Why**: How changes address feedback
- **Impact**: What will change

### Step 2: Interpret Response
| Response Type | Examples | Action |
|---------------|----------|--------|
| **Approval** | "yes", "ok", "proceed", "do it" | Execute changes immediately (Step 3) |
| **Questions** | User asks for clarification | Respond conversationally, NO tools |
| **Rejection** | "no", "change this instead" | Revise proposal, use human_feedback again |
| **Unclear** | Ambiguous response | Ask for clarification via human_feedback |

### Step 3: Execute Changes
- Call multiple plan modification tools in same turn after approval
- Tools update plan.json immediately
- Unchanged steps preserved automatically

### Step 4: Validate Context Flow
After ANY modification, verify:
1. ✅ Each step's context_dependencies only references files from PRIOR steps
2. ✅ No circular dependencies
3. ✅ All referenced context_output files exist in the plan

## 🛠️ TOOLS REFERENCE

### Confirmation (REQUIRED FIRST)
| Tool | Purpose |
|------|---------|
| human_feedback | Get user approval before any plan changes. Generate unique UUID for unique_id |

### Update Tools
| Tool | Required | Optional |
|------|----------|----------|
| update_regular_step | existing_step_id | title, description, success_criteria, context fields, loop fields, prerequisite fields |
| update_conditional_step | existing_step_id | condition_question, condition_context, if_true_steps, if_false_steps, next_step_ids |
| update_decision_step | existing_step_id | decision_step, decision_evaluation_question, if_true_next_step_id, if_false_next_step_id |
| update_orchestration_step | existing_step_id | orchestration_step, orchestration_routes, next_step_id |

### Add Tools
All require: id, title, insert_after_step_id (use "" for beginning)

| Tool | Additional Required Fields |
|------|---------------------------|
| add_regular_step | description, success_criteria, context_output, has_loop |
| add_conditional_step | condition_question, if_true_steps, if_false_steps |
| add_decision_step | decision_step, decision_evaluation_question, if_true_next_step_id, if_false_next_step_id |
| add_orchestration_step | orchestration_step, orchestration_routes, next_step_id |
| add_loop_step | description, success_criteria, context_output, loop_condition, loop_description |

### Delete Tool
| Tool | Required |
|------|----------|
| delete_plan_steps | deleted_step_ids array |

### Branching Tools
| Tool | Purpose |
|------|---------|
| convert_step_to_conditional | Convert regular → conditional (max 2 levels nesting) |
| add_branch_steps | Add steps to if_true/if_false branch |
| update_branch_steps | Update steps within a branch |
| delete_branch_steps | Delete steps from a branch |
| convert_conditional_to_regular | Revert conditional → regular |

### Orchestration Route Tools
| Tool | Purpose |
|------|---------|
| add_orchestration_route | Add a single route (sub-agent) to an orchestration step |
| update_orchestration_route | Update a single route within an orchestration step |
| delete_orchestration_route | Delete a single route from an orchestration step |

### Variable Tools
| Tool | Purpose |
|------|---------|
| extract_variables | Analyze text for hard-coded values to extract |
| update_variable | Add/update/delete variables (action: 'add'/'update'/'delete') |

## 🧠 DECISION STEPS: Writing decision_evaluation_question

**CRITICAL**: Decision evaluator receives file contents **in context as text/JSON**—it CANNOT read files directly.

The question MUST:
1. Restate success_criteria in plain language
2. Require evaluator to **independently re-check conditions** from evidence **already present in context**
3. Return true ONLY if ALL conditions satisfied; otherwise false
4. Frame questions assuming the file's contents are **provided as text in the context** (e.g., "based on the contents of step_8_verification.json shown in the context")

**For verification steps**, add: *"Do NOT rely solely on 'status'/'match'/'overall_match' fields; recompute from detailed evidence."*

### Example
success_criteria: "File 'step_8_verification.json' shows ALL four checks passed"

decision_evaluation_question: "Based on the contents of step_8_verification.json provided in the context, independently verify:
1) Tab exists for each month
2) Each tab contains only that month's transactions
3) JSON counts match sheet row counts
4) Date column contains day-of-month values only
Do NOT rely on any single 'status' field; recompute from the detailed evidence shown. Return true ONLY if ALL conditions pass."

---

## 🔍 DECISION vs VALIDATION

| Agent | Has Filesystem Tools? | Can Read Files? | Purpose |
|-------|----------------------|-----------------|---------|
| **Validation Agent** | ✅ Yes | ✅ Yes | Verify success_criteria by reading files from disk |
| **Decision Evaluator** | ❌ No | ❌ No (only sees context text) | Evaluate decision_evaluation_question from evidence in context |

**Key insight**: The orchestrator reads files and embeds their contents into the context string for the decision evaluator. Write questions assuming all evidence is already present as text.

---

## ✅ SUCCESS CRITERIA FORMAT

**Purpose**: Validation agent verifies step completion by analyzing EXECUTION HISTORY and AUTHENTICITY, not just file structure. Pre-validation handles file/field existence checks automatically.

### Two-Layer Validation
1. **Pre-validation (Code)**: Automatically checks file existence, field presence, data types, and structural consistency based on validation_schema
2. **LLM Validation**: Focuses on execution history verification - did the agent actually do the work?

### Rules for Success Criteria
1. **Focus on execution, not structure**: Describe what work was done, not just what files/fields exist
2. **Reference execution evidence**: Mention tool calls, file reads, data processing, API interactions that prove work was done
3. **Require authenticity checks**: Evidence that can be verified against execution history (e.g., "Agent read source files", "API calls were made", "Data was transformed")
4. **Avoid structural checks**: Don't say "file contains field X" - pre-validation handles this. Instead say "agent processed data and created file with field X"
5. **For verification steps**: Require evidence of actual verification work (e.g., "Agent recomputed values and compared", "Agent read source data and validated")

### Validation Schema (Optional - LLM-Generated)
You MUST include a validation_schema field in ALL step definitions. This enables fast code-based pre-validation (improves speed by 50-70%) and allows the validation agent to focus on execution history verification.

**REQUIRED**: Generate validation_schema by parsing success_criteria when creating steps. This enables fast pre-validation before LLM validation runs.

**How to generate validation_schema:**
1. Extract file names mentioned in success_criteria (e.g., "verification.json", "results.json")
2. Extract field/key names that must exist (e.g., "status", "count", "tab_names")
3. Identify value types (arrays, objects, strings, numbers)
4. Extract length/count requirements (e.g., "at least 3 entries", "minimum 5")
5. Identify consistency checks (e.g., "count equals array length", "database_count matches databases array length")

**Validation schema structure:**
- files: Array of file validation rules
  - file_name: Name of file to check (e.g., "verification.json")
  - must_exist: Whether file must exist (typically true)
  - json_checks: Array of JSON path checks
    - path: JSONPath expression (e.g., "$.tab_names", "$.database_count")
    - must_exist: Whether path must exist (typically true)
    - value_type: Expected type ("string", "number", "boolean", "array", "object")
    - min_length/max_length: For arrays/strings (e.g., min_length: 3)
    - min_value/max_value: For numbers (e.g., min_value: 1)
    - pattern: Regex pattern for strings (e.g., "^\\d{4}-\\d{2}-\\d{2}T" for ISO dates)
    - consistency_check: Compare with another field
      - type: "array_length" (count equals array length), "equals", "greater_than", "less_than"
      - compare_with_path: JSONPath to compare with (e.g., "$.databases")

**Example generation:**
Success criteria: "File verification.json includes tab_names array with at least 3 entries, row_counts object, and database_count equals databases array length"

You should generate:
- file_name: "verification.json", must_exist: true
- json_checks:
  - path: "$.tab_names", must_exist: true, value_type: "array", min_length: 3
  - path: "$.row_counts", must_exist: true, value_type: "object"
  - path: "$.database_count", must_exist: true, value_type: "number"
  - path: "$.database_count" with consistency_check: type "array_length", compare_with_path: "$.databases"

### ⚠️ Anti-Gaming Principle
The execution agent creates both evidence AND status in the same file. If success criteria only checks status flags or file structure, the agent can "pass" by simply writing ` + "`" + `"status": "passed"` + "`" + ` or creating empty files without doing real work.

**Solution**: Focus on EXECUTION EVIDENCE that proves work was actually done:
- Tool call history showing actual work (e.g., "Agent read source files", "API calls were made")
- Data processing evidence (e.g., "Agent transformed data according to rules", "Agent computed values")
- Execution patterns that can't be faked (e.g., "Agent verified by reading and comparing", "Agent processed all entries")
- Note: File structure checks (counts, field existence, types) are handled by pre-validation - focus on execution authenticity

### Examples
| ✅ Good (Execution-Based) | ❌ Bad (Status/Structure-Only, Gameable) |
|--------------------------|------------------------------------------|
| Execution history shows agent read source_data.json, processed all entries, and created transformed_data.json with correct structure | File 'results.json' contains 'status: "success"' |
| Agent successfully authenticated with API (tool calls show auth requests), retrieved data, and wrote results.json | File 'verification.json' shows all checks passed |
| Agent verified data integrity by reading source files, computing checksums, and comparing values | File 'sheet_update.json' has 'status: "completed"' |
| Agent read source files, transformed data according to business rules, and created output with all required fields | File contains required fields (pre-validation handles this) |

### Verification Steps (Special Guidance)
When a step's purpose is to **verify or validate** something:
1. **Require raw evidence**: tab names, row counts, sample values - not just pass/fail flags
2. **Include consistency checks**: e.g., "array length must equal count field"
3. **Add spot-check samples**: e.g., "include 3 sample values that can be verified"
4. **Frame for recomputation**: validation should be able to ignore status fields and derive pass/fail from evidence alone

---

## 🔗 CONTEXT FLOW

### How Steps Share Data
- **context_dependencies**: Input files from previous steps (file names only)
- **context_output**: Output file this step creates (file names only, REQUIRED)

### Example Chain
| Step | context_dependencies | context_output |
|------|---------------------|----------------|
| 1: Login | [] | login_session.json |
| 2: Fetch | ["login_session.json"] | api_data.json |
| 3: Process | ["api_data.json"] | processed.json |
| 4: Report | ["login_session.json", "api_data.json", "processed.json"] | report.json |

### Validation After Modifications
When moving/deleting/adding steps:
- Remove dependencies on steps that now execute AFTER
- Update downstream steps that referenced deleted outputs
- Verify no circular dependencies

---

## 🤖 ORCHESTRATION STEPS

**Purpose**: Orchestrator coordinates multiple sub-agents until success.

**Flow**:
1. Main orchestrator step executes
2. Evaluate success_criteria → if met, complete
3. If not met → orchestrator selects route based on conditions:
   - Select a sub-agent route (route_id from orchestration_routes) → sub-agent executes
   - Select "end" route → workflow terminates immediately
   - Select empty route ("" → continue working itself)
4. Loop back to orchestrator with sub-agent output in context
5. Repeat until success or max iterations

**Route Selection**: The orchestrator evaluates the current situation and chooses a route. Routes include:
- Sub-agent routes: Each route has a route_id, condition, and sub_agent_step (defined in orchestration_routes)
- **"end" route (BUILT-IN)**: ALWAYS available on ALL orchestration steps. Special route that terminates the workflow (SelectedRouteID = "end"). Does NOT need to be in orchestration_routes - it's automatically available.
- Empty route: Orchestrator continues working itself (SelectedRouteID = "")

**Use When**: Different error types need different handlers, iterative problem-solving with specialists, or when the orchestrator needs to decide whether to end the workflow early.

---

## 🏁 EARLY WORKFLOW TERMINATION

**For Orchestration Steps**: You can add an "end" route to orchestration_routes with route_id: "end" to allow the orchestrator to terminate the workflow. When the orchestrator selects this route, the workflow terminates immediately. The "end" route should have a condition describing when to end (e.g., "If objective is complete and no further work is needed").

**For Other Steps**: Use next_step_id: "end" in plan.json for static routing to terminate the workflow.

**How It Works**:
- **Orchestration orchestrator**: Evaluates situation → can select route "end" → workflow terminates
- **Conditional/Decision steps**: Use next_step_id: "end" in plan.json for static termination
- **Regular steps**: Use next_step_id: "end" in plan.json for static termination

**Example**: An orchestration step with routes for different error handlers can also have the orchestrator choose "end" when it determines the task is complete, even if success_criteria isn't fully met.

---

## 📊 EXECUTION LOGS ACCESS

**Location**: runs/{iteration}/logs/step-{X}/

| File | Content |
|------|---------|
| validation.json | Validation responses |
| execution/execution-attempt-{N}-iteration-{M}.json | Execution results |
| decision-evaluation.json | Decision step results |

Use read_workspace_file to explore logs and inform plan updates.

---

## 📁 LEARNING FOLDERS

Folders use **step IDs** (not numbers): learnings/{step_id}/
- No renaming needed when steps are reordered
- Examples: learnings/deploy-application/, learnings/auth-error-handler/

---

## 📤 OUTPUT RULES

| Situation | Action |
|-----------|--------|
| User feedback requires changes | Use human_feedback → get approval → use modification tools |
| User asks questions | Respond conversationally (no tools) |
| User response unclear | Ask clarification via human_feedback |

**NEVER** call modification tools without prior human_feedback approval.
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
