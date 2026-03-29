package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/PaesslerAG/jsonpath"
)

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

// MarshalJSON implements custom marshaling for PlanOrchestrationRoute
// This is needed to properly handle the SubAgentStep field which is a PlanStepInterface
func (r PlanOrchestrationRoute) MarshalJSON() ([]byte, error) {
	type routeJSON struct {
		RouteID       string          `json:"route_id"`
		RouteName     string          `json:"route_name"`
		Condition     string          `json:"condition"`
		SubAgentStep  json.RawMessage `json:"sub_agent_step"`
		ContextToPass string          `json:"context_to_pass,omitempty"`
	}

	result := routeJSON{
		RouteID:       r.RouteID,
		RouteName:     r.RouteName,
		Condition:     r.Condition,
		ContextToPass: r.ContextToPass,
	}

	// Marshal SubAgentStep if it exists
	if r.SubAgentStep != nil {
		subAgentJSON, err := json.Marshal(r.SubAgentStep)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal sub_agent_step: %w", err)
		}
		result.SubAgentStep = subAgentJSON
	} else {
		result.SubAgentStep = []byte("null")
	}

	return json.Marshal(result)
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
	Provider  string             `json:"provider,omitempty"`  // e.g., "openai", "bedrock", "openrouter", "vertex"
	ModelID   string             `json:"model_id,omitempty"`  // e.g., "gpt-4o", "claude-3-5-sonnet-20241022"
	Fallbacks []AgentLLMFallback `json:"fallbacks,omitempty"` // Optional fallback models for retry on failure
}

// AgentLLMFallback represents a fallback LLM model
type AgentLLMFallback struct {
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
}

// ValidationSchema represents structured validation rules for step outputs
type ValidationSchema struct {
	Files []FileValidationRule `json:"files,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling to fix double-escaped regex patterns
// This fixes patterns at the source (during JSON unmarshaling) rather than during validation
func (vs *ValidationSchema) UnmarshalJSON(data []byte) error {
	// Use type alias to avoid infinite recursion
	type Alias ValidationSchema
	alias := (*Alias)(vs)

	// Unmarshal normally first
	if err := json.Unmarshal(data, alias); err != nil {
		return err
	}

	// Fix double-escaped patterns in all checks
	for i := range vs.Files {
		for j := range vs.Files[i].JSONChecks {
			if vs.Files[i].JSONChecks[j].Pattern != "" {
				fixed := fixDoubleEscapedPattern(vs.Files[i].JSONChecks[j].Pattern)
				vs.Files[i].JSONChecks[j].Pattern = fixed
			}
		}
	}

	return nil
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
	ExecutionLLM                        *AgentLLMConfig `json:"execution_llm,omitempty"`
	LearningLLM                         *AgentLLMConfig `json:"learning_llm,omitempty"`
	ConditionalLLM                      *AgentLLMConfig `json:"conditional_llm,omitempty"`                        // Step-specific conditional LLM for conditional step evaluation
	ExecutionMaxTurns                   *int            `json:"execution_max_turns,omitempty"`                    // default: 100
	LearningMaxTurns                    *int            `json:"learning_max_turns,omitempty"`                     // default: 100
	OrchestrationMaxIterations          *int            `json:"orchestration_max_iterations,omitempty"`           // default: orchestrator max turns (typically 100)
	DisableLearning                     *bool           `json:"disable_learning,omitempty"`                       // disable learning for this step (nil = not set/enabled, true = disabled, false = explicitly enabled)
	LockLearnings                       *bool           `json:"lock_learnings,omitempty"`                         // lock learnings - prevents learning agent from running but still uses existing learnings (nil = not set/unlocked, true = locked, false = explicitly unlocked)
	LearningAfterLoopIteration          bool            `json:"learning_after_loop_iteration,omitempty"`          // run learning after each loop iteration
	LearningDetailLevel                 string          `json:"learning_detail_level,omitempty"`                  // "exact" or "none" (default: "exact")
	LearningMode                        string          `json:"learning_mode,omitempty"`                          // "human_assisted" (default) or "auto". human_assisted = skip automatic learning, use generate_learnings manually. auto = learning runs automatically after execution.
	SelectedServers                     []string        `json:"selected_servers,omitempty"`                       // step-level MCP server selection (subset of preset servers)
	SelectedTools                       []string        `json:"selected_tools,omitempty"`                         // step-level tool selection (format: "server:tool" or "server:*" for all tools)
	EnabledCustomToolCategories         []string        `json:"enabled_custom_tool_categories,omitempty"`         // e.g., ["workspace_tools", "human_tools"] - enables all tools in category
	EnabledCustomTools                  []string        `json:"enabled_custom_tools,omitempty"`                   // e.g., ["read_workspace_file", "human_feedback"] - enables specific tools (overrides categories if both specified)
	EnableContextOffloading             *bool           `json:"enable_context_offloading,omitempty"`              // Enable/disable context offloading (default: true if nil)
	UseCodeExecutionMode                *bool           `json:"use_code_execution_mode,omitempty"`                // Step-level code execution mode override (nil = use preset default, true/false = override)
	UseToolSearchMode                   *bool           `json:"use_tool_search_mode,omitempty"`                   // Step-level tool search mode override (nil = use preset default, true/false = override)
	PreDiscoveredTools                  []string        `json:"pre_discovered_tools,omitempty"`                   // Tools always available without searching (overrides preset if specified)
	EnabledSkills                       []string        `json:"enabled_skills,omitempty"`                         // Step-level skill selection (skill folder names, overrides preset if specified)
	KeepLearningFull                    *bool           `json:"keep_learning_full,omitempty"`                     // Feature flag: If true, include full learning content in system prompt; if false, only file paths in user message (default: false, can be overridden by KEEP_LEARNING_FULL env var)
	DisableKnowledgebase                *bool           `json:"disable_knowledgebase,omitempty"`                  // If true, disable knowledgebase access for this step (nil = use preset default, true = disabled, false = explicitly enabled)
	DisableTempLLM                      *bool           `json:"disable_temp_llm,omitempty"`                       // If true, skip tempLLM override and use the normal workflow LLM path (step config > tiered)
	TodoTaskOrchestratorTier            *int            `json:"todo_task_orchestrator_tier,omitempty"`            // Tier for todo task orchestrator agent (1/2/3) in tiered mode
	EnableDynamicTierSelection          *bool           `json:"enable_dynamic_tier_selection,omitempty"`          // Allow todo task orchestrator to choose tier for sub-agents
	OrchestratorLLM                     *AgentLLMConfig `json:"orchestrator_llm,omitempty"`                       // Direct LLM override for orchestrator (works in both tiered and manual modes)
	SubAgentLLM                         *AgentLLMConfig `json:"sub_agent_llm,omitempty"`                          // Direct LLM override for ALL sub-agents spawned by this step (works in both tiered and manual modes)
	DisableParallelToolExecution        *bool           `json:"disable_parallel_tool_execution,omitempty"`        // Disable parallel tool execution for this step (nil = enabled by default, true = disabled, false = explicitly enabled)
	DisableTierOptimization             *bool           `json:"disable_tier_optimization,omitempty"`              // If true, always use Tier 1 (high reasoning) regardless of learning maturity — disables maturity-based tier downgrade
	Optimized                           *bool           `json:"optimized,omitempty"`                              // If true, step is considered optimized — triggers tier downgrade to lower-cost LLMs
	SuccessfulRuns                      *int            `json:"successful_runs,omitempty"`                        // Count of successful runs — tracks progress toward optimization readiness (3+ = ready to optimize)
	UseLearnCodeMode                    *bool           `json:"use_learn_code_mode,omitempty"`                    // Learn code mode: LLM writes main.py once, saved and reused without LLM on future runs (nil = disabled)
	LearnCodeMaxFixIter                 *int            `json:"learn_code_max_fix_iterations,omitempty"`          // Max LLM fix iterations when main.py execution fails (default: 5)
	DeclaredExecutionMode               string          `json:"declared_execution_mode,omitempty"`                // Required mode decision for the step: "learn_code", "code_exec", "tool_search", or "simple"
	DeclaredExecutionModeReason         string          `json:"declared_execution_mode_reason,omitempty"`         // Why this mode is the best fit for the step
	LearnCodeRejectionReason            string          `json:"learn_code_rejection_reason,omitempty"`            // Required when declared_execution_mode is not "learn_code"
	CodeExecRejectionReason             string          `json:"code_exec_rejection_reason,omitempty"`             // Required when declared_execution_mode is "tool_search" or "simple"
	ToolSearchRejectionReason           string          `json:"tool_search_rejection_reason,omitempty"`           // Required when declared_execution_mode is "simple"
	DescriptionHash                     string          `json:"description_hash,omitempty"`                       // SHA256 of the current step description. If it changes, optimization review is stale.
	DescriptionOptimized                *bool           `json:"description_optimized,omitempty"`                  // True when the description has been reviewed and judged optimized for execution.
	DescriptionOptimizationReason       string          `json:"description_optimization_reason,omitempty"`        // Why the current description is considered optimized.
	DescriptionLearningsAlignmentReason string          `json:"description_learnings_alignment_reason,omitempty"` // How the description reflects the learnings gathered so far.
	DescriptionNoSecrets                *bool           `json:"description_no_secrets,omitempty"`                 // True when the description has been reviewed for secrets/hardcoded values and cleared.
	DescriptionSecretsReviewReason      string          `json:"description_secrets_review_reason,omitempty"`      // Why the description is considered free of secrets/hardcoded values.
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
	StepTypeHumanInput    StepType = "human_input"
	StepTypeTodoTask      StepType = "todo_task"
	StepTypeRouting       StepType = "routing"
)

// CommonStepFields contains fields shared by all step types
type CommonStepFields struct {
	ID                  string                `json:"id"` // Stable step ID (generated from title) - required
	Title               string                `json:"title"`
	Description         string                `json:"description"`
	SuccessCriteria     string                `json:"success_criteria"`
	ContextDependencies []string              `json:"context_dependencies"`
	ContextOutput       FlexibleContextOutput `json:"context_output"`              // Use flexible type to handle string or array
	ValidationSchema    *ValidationSchema     `json:"validation_schema,omitempty"` // Optional structured validation schema for step outputs
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
func (r *RegularPlanStep) GetValidationSchema() *ValidationSchema  { return r.ValidationSchema }
func (r *RegularPlanStep) StepType() StepType                      { return StepTypeRegular }
func (r *RegularPlanStep) GetCommonFields() CommonStepFields       { return r.CommonStepFields }

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
func (c *ConditionalPlanStep) GetValidationSchema() *ValidationSchema  { return c.ValidationSchema }
func (c *ConditionalPlanStep) StepType() StepType                      { return StepTypeConditional }
func (c *ConditionalPlanStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:                  c.ID,
		Title:               c.Title,
		Description:         c.Description,
		SuccessCriteria:     c.SuccessCriteria,
		ContextDependencies: c.ContextDependencies,
		ContextOutput:       c.ContextOutput,
		ValidationSchema:    c.ValidationSchema,
	}
}

// MarshalJSON ensures the type field is always set when marshaling
func (c *ConditionalPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	c.Type = StepTypeConditional

	type conditionalJSON struct {
		Type StepType `json:"type"`
		CommonStepFields
		ConditionQuestion string            `json:"condition_question,omitempty"`
		ConditionContext  string            `json:"condition_context,omitempty"`
		IfTrueSteps       []json.RawMessage `json:"if_true_steps,omitempty"`
		IfFalseSteps      []json.RawMessage `json:"if_false_steps,omitempty"`
		IfTrueNextStepID  string            `json:"if_true_next_step_id,omitempty"`
		IfFalseNextStepID string            `json:"if_false_next_step_id,omitempty"`
	}

	result := conditionalJSON{
		Type:              c.Type,
		CommonStepFields:  c.CommonStepFields,
		ConditionQuestion: c.ConditionQuestion,
		ConditionContext:  c.ConditionContext,
		IfTrueNextStepID:  c.IfTrueNextStepID,
		IfFalseNextStepID: c.IfFalseNextStepID,
	}

	// Marshal IfTrueSteps
	if len(c.IfTrueSteps) > 0 {
		result.IfTrueSteps = make([]json.RawMessage, len(c.IfTrueSteps))
		for i, step := range c.IfTrueSteps {
			stepJSON, err := json.Marshal(step)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal if_true_steps[%d]: %w", i, err)
			}
			result.IfTrueSteps[i] = stepJSON
		}
	}

	// Marshal IfFalseSteps
	if len(c.IfFalseSteps) > 0 {
		result.IfFalseSteps = make([]json.RawMessage, len(c.IfFalseSteps))
		for i, step := range c.IfFalseSteps {
			stepJSON, err := json.Marshal(step)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal if_false_steps[%d]: %w", i, err)
			}
			result.IfFalseSteps[i] = stepJSON
		}
	}

	return json.Marshal(result)
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
// Decision steps execute their own description/success_criteria (execution phase),
// then evaluate the output against DecisionEvaluationQuestion (evaluation phase),
// and route to IfTrueNextStepID or IfFalseNextStepID based on the evaluation result.
type DecisionPlanStep struct {
	Type StepType `json:"type"` // Always "decision" - required for JSON marshaling/unmarshaling
	CommonStepFields
	DecisionEvaluationQuestion string            `json:"decision_evaluation_question,omitempty"` // Question to evaluate step output
	IfTrueNextStepID           string            `json:"if_true_next_step_id,omitempty"`         // ID of step to connect to if decision is true (or "end")
	IfFalseNextStepID          string            `json:"if_false_next_step_id,omitempty"`        // ID of step to connect to if decision is false (or "end")
	DecisionResult             *bool             `json:"-"`                                      // runtime: stores evaluation result (backward compatibility) - not stored in plan.json
	DecisionReason             string            `json:"-"`                                      // runtime: stores evaluation reasoning (backward compatibility) - not stored in plan.json
	DecisionResponse           *DecisionResponse `json:"-"`                                      // runtime: stores structured decision evaluation response - not stored in plan.json
	AgentConfigs               *AgentConfigs     `json:"-"`                                      // runtime: per-agent configuration (LLM, max turns, toggles) - not stored in plan.json
}

// Implement PlanStepInterface for DecisionPlanStep
func (d *DecisionPlanStep) GetID() string                           { return d.ID }
func (d *DecisionPlanStep) GetTitle() string                        { return d.Title }
func (d *DecisionPlanStep) GetDescription() string                  { return d.Description }
func (d *DecisionPlanStep) GetSuccessCriteria() string              { return d.SuccessCriteria }
func (d *DecisionPlanStep) GetContextDependencies() []string        { return d.ContextDependencies }
func (d *DecisionPlanStep) GetContextOutput() FlexibleContextOutput { return d.ContextOutput }
func (d *DecisionPlanStep) GetValidationSchema() *ValidationSchema  { return d.ValidationSchema }
func (d *DecisionPlanStep) StepType() StepType                      { return StepTypeDecision }
func (d *DecisionPlanStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:                  d.ID,
		Title:               d.Title,
		Description:         d.Description,
		SuccessCriteria:     d.SuccessCriteria,
		ContextDependencies: d.ContextDependencies,
		ContextOutput:       d.ContextOutput,
		ValidationSchema:    d.ValidationSchema,
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
// This handles both the new flattened format and the legacy nested decision_step format
func (d *DecisionPlanStep) UnmarshalJSON(data []byte) error {
	// First, check if this is the legacy format with nested decision_step
	var legacyCheck struct {
		DecisionStep json.RawMessage `json:"decision_step,omitempty"`
	}
	if err := json.Unmarshal(data, &legacyCheck); err == nil && len(legacyCheck.DecisionStep) > 0 && string(legacyCheck.DecisionStep) != "null" {
		// Legacy format detected - migrate from nested decision_step
		return d.unmarshalLegacyFormat(data)
	}

	// New flattened format - use type alias to avoid infinite recursion
	type Alias DecisionPlanStep
	var temp Alias
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal decision step: %w", err)
	}

	*d = DecisionPlanStep(temp)
	return nil
}

// unmarshalLegacyFormat handles migration from the old nested decision_step format
func (d *DecisionPlanStep) unmarshalLegacyFormat(data []byte) error {
	// Parse the legacy format
	var legacy struct {
		Type                       StepType              `json:"type"`
		ID                         string                `json:"id"`
		Title                      string                `json:"title"`
		DecisionStep               json.RawMessage       `json:"decision_step,omitempty"`
		DecisionEvaluationQuestion string                `json:"decision_evaluation_question,omitempty"`
		IfTrueNextStepID           string                `json:"if_true_next_step_id,omitempty"`
		IfFalseNextStepID          string                `json:"if_false_next_step_id,omitempty"`
		ContextDependencies        []string              `json:"context_dependencies,omitempty"`
		ContextOutput              FlexibleContextOutput `json:"context_output,omitempty"`
		ValidationSchema           *ValidationSchema     `json:"validation_schema,omitempty"`
	}

	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("failed to unmarshal legacy decision step: %w", err)
	}

	// Parse the nested decision_step to extract its fields
	var innerStep struct {
		ID                  string                `json:"id"`
		Title               string                `json:"title"`
		Description         string                `json:"description"`
		SuccessCriteria     string                `json:"success_criteria"`
		ContextDependencies []string              `json:"context_dependencies,omitempty"`
		ContextOutput       FlexibleContextOutput `json:"context_output,omitempty"`
		ValidationSchema    *ValidationSchema     `json:"validation_schema,omitempty"`
	}

	if err := json.Unmarshal(legacy.DecisionStep, &innerStep); err != nil {
		return fmt.Errorf("failed to unmarshal inner decision_step: %w", err)
	}

	// Migrate: Copy fields from inner step to the flattened structure
	// Use inner step's ID if wrapper doesn't have one, otherwise use wrapper's ID
	d.Type = legacy.Type
	if legacy.ID != "" {
		d.ID = legacy.ID
	} else {
		d.ID = innerStep.ID
	}
	if legacy.Title != "" {
		d.Title = legacy.Title
	} else {
		d.Title = innerStep.Title
	}
	d.Description = innerStep.Description
	d.SuccessCriteria = innerStep.SuccessCriteria

	// Context dependencies: prefer inner step's, fallback to wrapper's
	if len(innerStep.ContextDependencies) > 0 {
		d.ContextDependencies = innerStep.ContextDependencies
	} else {
		d.ContextDependencies = legacy.ContextDependencies
	}

	// Context output: prefer inner step's, fallback to wrapper's
	if innerStep.ContextOutput != "" {
		d.ContextOutput = innerStep.ContextOutput
	} else {
		d.ContextOutput = legacy.ContextOutput
	}

	// Validation schema: prefer inner step's, fallback to wrapper's
	if innerStep.ValidationSchema != nil {
		d.ValidationSchema = innerStep.ValidationSchema
	} else {
		d.ValidationSchema = legacy.ValidationSchema
	}

	d.DecisionEvaluationQuestion = legacy.DecisionEvaluationQuestion
	d.IfTrueNextStepID = legacy.IfTrueNextStepID
	d.IfFalseNextStepID = legacy.IfFalseNextStepID

	// Log the migration (note: we don't have access to logger here, so this is silent)
	// The migration will be visible when the plan is next saved (it will be in the new format)

	return nil
}

// RoutingRoute represents a single route option in a routing step
type RoutingRoute struct {
	RouteID    string `json:"route_id"`
	RouteName  string `json:"route_name"`
	Condition  string `json:"condition"`
	NextStepID string `json:"next_step_id"`
}

// RoutingResponse represents the structured response from routing evaluation
type RoutingResponse struct {
	SelectedRouteID string `json:"selected_route_id"` // The route selected by the LLM
	Reasoning       string `json:"reasoning"`         // Reasoning for the selection
}

// RoutingPlanStep represents a routing step that evaluates N-way routing
// Two modes:
// - Execute-then-route: Has Description/SuccessCriteria -> executes first, then LLM evaluates output to pick a route
// - Pure routing: No Description/SuccessCriteria -> LLM evaluates prior context to pick a route (multi-way conditional)
type RoutingPlanStep struct {
	Type StepType `json:"type"` // Always "routing" - required for JSON marshaling/unmarshaling
	CommonStepFields
	RoutingQuestion string           `json:"routing_question"`           // Question to evaluate for route selection (required)
	Routes          []RoutingRoute   `json:"routes"`                     // Available routes (min 2, required)
	DefaultRouteID  string           `json:"default_route_id,omitempty"` // Optional fallback route_id if LLM picks invalid route
	SelectedRouteID string           `json:"-"`                          // runtime: stores selected route ID
	RoutingResponse *RoutingResponse `json:"-"`                          // runtime: stores structured routing response
	AgentConfigs    *AgentConfigs    `json:"-"`                          // runtime: per-agent configuration
}

// Implement PlanStepInterface for RoutingPlanStep
func (r *RoutingPlanStep) GetID() string                           { return r.ID }
func (r *RoutingPlanStep) GetTitle() string                        { return r.Title }
func (r *RoutingPlanStep) GetDescription() string                  { return r.Description }
func (r *RoutingPlanStep) GetSuccessCriteria() string              { return r.SuccessCriteria }
func (r *RoutingPlanStep) GetContextDependencies() []string        { return r.ContextDependencies }
func (r *RoutingPlanStep) GetContextOutput() FlexibleContextOutput { return r.ContextOutput }
func (r *RoutingPlanStep) GetValidationSchema() *ValidationSchema  { return r.ValidationSchema }
func (r *RoutingPlanStep) StepType() StepType                      { return StepTypeRouting }
func (r *RoutingPlanStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:                  r.ID,
		Title:               r.Title,
		Description:         r.Description,
		SuccessCriteria:     r.SuccessCriteria,
		ContextDependencies: r.ContextDependencies,
		ContextOutput:       r.ContextOutput,
		ValidationSchema:    r.ValidationSchema,
	}
}

// MarshalJSON ensures the type field is always set when marshaling
func (r *RoutingPlanStep) MarshalJSON() ([]byte, error) {
	r.Type = StepTypeRouting
	type Alias RoutingPlanStep
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements custom unmarshaling for RoutingPlanStep
func (r *RoutingPlanStep) UnmarshalJSON(data []byte) error {
	type Alias RoutingPlanStep
	var temp Alias
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal routing step: %w", err)
	}
	*r = RoutingPlanStep(temp)
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
}

// Implement PlanStepInterface for OrchestrationPlanStep
func (o *OrchestrationPlanStep) GetID() string                           { return o.ID }
func (o *OrchestrationPlanStep) GetTitle() string                        { return o.Title }
func (o *OrchestrationPlanStep) GetDescription() string                  { return "" }  // Not used - inner OrchestrationStep has description
func (o *OrchestrationPlanStep) GetSuccessCriteria() string              { return "" }  // Not used - inner OrchestrationStep has success criteria
func (o *OrchestrationPlanStep) GetContextDependencies() []string        { return nil } // Not used - inner OrchestrationStep has context dependencies
func (o *OrchestrationPlanStep) GetContextOutput() FlexibleContextOutput { return "" }  // Not used - inner OrchestrationStep produces context output
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
		ID:                  o.ID,
		Title:               o.Title,
		Description:         "",  // Not used for orchestration wrapper
		SuccessCriteria:     "",  // Not used for orchestration wrapper
		ContextDependencies: nil, // Not used for orchestration wrapper
		ContextOutput:       "",  // Not used for orchestration wrapper
		ValidationSchema:    o.GetValidationSchema(),
	}
}

// MarshalJSON ensures the type field is always set when marshaling
func (o *OrchestrationPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	o.Type = StepTypeOrchestration

	type orchestrationJSON struct {
		Type                StepType                 `json:"type"`
		ID                  string                   `json:"id"`
		Title               string                   `json:"title"`
		OrchestrationStep   json.RawMessage          `json:"orchestration_step,omitempty"`
		OrchestrationRoutes []PlanOrchestrationRoute `json:"orchestration_routes,omitempty"`
		NextStepID          string                   `json:"next_step_id,omitempty"`
	}

	result := orchestrationJSON{
		Type:                o.Type,
		ID:                  o.ID,
		Title:               o.Title,
		OrchestrationRoutes: o.OrchestrationRoutes, // Go will call MarshalJSON on each PlanOrchestrationRoute
		NextStepID:          o.NextStepID,
	}

	// Marshal OrchestrationStep if it exists
	if o.OrchestrationStep != nil {
		stepJSON, err := json.Marshal(o.OrchestrationStep)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal orchestration_step: %w", err)
		}
		result.OrchestrationStep = stepJSON
	} else {
		result.OrchestrationStep = []byte("null")
	}

	return json.Marshal(result)
}

// HumanInputPlanStep represents a step that asks a question to a human and blocks for input
// This step type has no LLM, no execution, no validation, and no learning - just human input
type HumanInputPlanStep struct {
	Type StepType `json:"type"` // Always "human_input" - required for JSON marshaling/unmarshaling
	CommonStepFields
	Question           string            `json:"question"`                      // Required: question to ask human
	VariableName       string            `json:"variable_name,omitempty"`       // Optional: store response in variable
	ResponseType       string            `json:"response_type,omitempty"`       // "text" (default), "yesno", "multiple_choice"
	Options            []string          `json:"options,omitempty"`             // For multiple_choice type
	NextStepID         string            `json:"next_step_id"`                  // Default: where to go after response (or "end") - used if conditional routing not specified
	IfYesNextStepID    string            `json:"if_yes_next_step_id,omitempty"` // Optional: for yesno type when response is "yes"
	IfNoNextStepID     string            `json:"if_no_next_step_id,omitempty"`  // Optional: for yesno type when response is "no"
	OptionRoutes       map[string]string `json:"option_routes,omitempty"`       // Optional: for multiple_choice type - maps option index (as string "0", "1", etc.) or option value to next_step_id
	SelectedNextStepID string            `json:"-"`                             // runtime: stores computed next step ID based on response - not stored in plan.json
	AgentConfigs       *AgentConfigs     `json:"-"`                             // runtime: per-agent configuration (LLM, max turns, toggles) - not stored in plan.json
}

// Implement PlanStepInterface for HumanInputPlanStep
func (h *HumanInputPlanStep) GetID() string                           { return h.ID }
func (h *HumanInputPlanStep) GetTitle() string                        { return h.Title }
func (h *HumanInputPlanStep) GetDescription() string                  { return h.Description }
func (h *HumanInputPlanStep) GetSuccessCriteria() string              { return h.SuccessCriteria }
func (h *HumanInputPlanStep) GetContextDependencies() []string        { return h.ContextDependencies }
func (h *HumanInputPlanStep) GetContextOutput() FlexibleContextOutput { return h.ContextOutput }
func (h *HumanInputPlanStep) GetValidationSchema() *ValidationSchema  { return h.ValidationSchema }
func (h *HumanInputPlanStep) StepType() StepType                      { return StepTypeHumanInput }
func (h *HumanInputPlanStep) GetCommonFields() CommonStepFields       { return h.CommonStepFields }

// MarshalJSON ensures the type field is always set when marshaling
func (h *HumanInputPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	h.Type = StepTypeHumanInput
	// Use type alias to avoid infinite recursion
	type Alias HumanInputPlanStep
	return json.Marshal((*Alias)(h))
}

// TodoTaskPlanStep represents a todo task orchestrator step that manages a dynamic todo list
// It combines predefined sub-agents (with learning/prevalidation) and an optional generic execution agent
// The main orchestrator creates/assigns tasks, then delegates to appropriate agents
// NOTE: Todo task steps are orchestration-like wrappers that manage todo lists instead of success criteria.
// Loops are NOT supported on todo task wrappers - the step completes when all todos are done.
type TodoTaskPlanStep struct {
	Type               StepType                 `json:"type"`                           // Always "todo_task" - required for JSON marshaling/unmarshaling
	ID                 string                   `json:"id"`                             // Stable step ID - required for identification
	Title              string                   `json:"title"`                          // Display title for the todo task step wrapper
	TodoTaskStep       PlanStepInterface        `json:"todo_task_step,omitempty"`       // The main orchestrator step metadata (Description, SuccessCriteria, etc.)
	PredefinedRoutes   []PlanOrchestrationRoute `json:"predefined_routes,omitempty"`    // Predefined sub-agents (with learning/prevalidation)
	EnableGenericAgent bool                     `json:"enable_generic_agent,omitempty"` // Allow generic execution agent (no learning/prevalidation)
	NextStepID         string                   `json:"next_step_id,omitempty"`         // ID of step after todo task completes (or "end")
	TodoTaskResponse   *TodoTaskResponse        `json:"-"`                              // runtime: stores orchestrator decisions - not stored in plan.json
	AgentConfigs       *AgentConfigs            `json:"-"`                              // runtime: per-agent configuration - not stored in plan.json
}

// TodoTaskResponse represents the structured output from the TodoTask orchestrator agent
type TodoTaskResponse struct {
	// Task management (via tools, not structured output - these are for reference)
	TasksCreated   []string `json:"tasks_created,omitempty"`   // IDs of tasks created this turn
	TasksUpdated   []string `json:"tasks_updated,omitempty"`   // IDs of tasks updated this turn
	TasksCompleted []string `json:"tasks_completed,omitempty"` // IDs of tasks completed this turn

	// Agent assignment decision
	NextAction      string `json:"next_action"`                 // "delegate", "complete", "continue"
	SelectedRouteID string `json:"selected_route_id,omitempty"` // Route ID for predefined agent
	UseGenericAgent bool   `json:"use_generic_agent,omitempty"` // Use generic agent instead

	// Delegation instructions
	TodoIDToExecute            string `json:"todo_id_to_execute,omitempty"`             // Which todo to work on
	InstructionsToSubAgent     string `json:"instructions_to_sub_agent,omitempty"`      // Detailed instructions
	SuccessCriteriaForSubAgent string `json:"success_criteria_for_sub_agent,omitempty"` // Measurable criteria

	// Overall status
	AllTasksComplete bool   `json:"all_tasks_complete"`          // True when all todos are completed
	ProgressSummary  string `json:"progress_summary"`            // Human-readable progress
	CompletionReason string `json:"completion_reason,omitempty"` // Why the step is complete
}

// Implement PlanStepInterface for TodoTaskPlanStep
func (t *TodoTaskPlanStep) GetID() string    { return t.ID }
func (t *TodoTaskPlanStep) GetTitle() string { return t.Title }
func (t *TodoTaskPlanStep) GetDescription() string {
	if t.TodoTaskStep != nil {
		return t.TodoTaskStep.GetDescription()
	}
	return ""
}
func (t *TodoTaskPlanStep) GetSuccessCriteria() string {
	if t.TodoTaskStep != nil {
		return t.TodoTaskStep.GetSuccessCriteria()
	}
	return ""
}
func (t *TodoTaskPlanStep) GetContextDependencies() []string {
	if t.TodoTaskStep != nil {
		return t.TodoTaskStep.GetContextDependencies()
	}
	return nil
}
func (t *TodoTaskPlanStep) GetContextOutput() FlexibleContextOutput {
	if t.TodoTaskStep != nil {
		return t.TodoTaskStep.GetContextOutput()
	}
	return ""
}
func (t *TodoTaskPlanStep) GetValidationSchema() *ValidationSchema {
	if t.TodoTaskStep != nil {
		return t.TodoTaskStep.GetValidationSchema()
	}
	return nil
}
func (t *TodoTaskPlanStep) StepType() StepType { return StepTypeTodoTask }
func (t *TodoTaskPlanStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:                  t.ID,
		Title:               t.Title,
		Description:         t.GetDescription(),
		SuccessCriteria:     t.GetSuccessCriteria(),
		ContextDependencies: t.GetContextDependencies(),
		ContextOutput:       t.GetContextOutput(),
		ValidationSchema:    t.GetValidationSchema(),
	}
}

// MarshalJSON ensures the type field is always set when marshaling TodoTaskPlanStep
func (t *TodoTaskPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	t.Type = StepTypeTodoTask

	// Note: enable_generic_agent is not included in JSON output as it's always true for todo task steps
	type todoTaskJSON struct {
		Type             StepType                 `json:"type"`
		ID               string                   `json:"id"`
		Title            string                   `json:"title"`
		TodoTaskStep     json.RawMessage          `json:"todo_task_step,omitempty"`
		PredefinedRoutes []PlanOrchestrationRoute `json:"predefined_routes,omitempty"`
		NextStepID       string                   `json:"next_step_id,omitempty"`
	}

	result := todoTaskJSON{
		Type:             t.Type,
		ID:               t.ID,
		Title:            t.Title,
		PredefinedRoutes: t.PredefinedRoutes,
		NextStepID:       t.NextStepID,
	}

	// Marshal TodoTaskStep if it exists
	if t.TodoTaskStep != nil {
		stepJSON, err := json.Marshal(t.TodoTaskStep)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal todo_task_step: %w", err)
		}
		result.TodoTaskStep = stepJSON
	} else {
		result.TodoTaskStep = []byte("null")
	}

	return json.Marshal(result)
}

// UnmarshalJSON implements custom unmarshaling for TodoTaskPlanStep
// This is needed to properly handle nested todo_task_step and predefined_routes[].sub_agent_step
func (t *TodoTaskPlanStep) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a temporary struct to extract nested steps as raw JSON
	var temp struct {
		Type             StepType        `json:"type"`
		ID               string          `json:"id"`
		Title            string          `json:"title"`
		TodoTaskStep     json.RawMessage `json:"todo_task_step,omitempty"`
		PredefinedRoutes []struct {
			RouteID       string          `json:"route_id"`
			RouteName     string          `json:"route_name"`
			Condition     string          `json:"condition"`
			SubAgentStep  json.RawMessage `json:"sub_agent_step"`
			ContextToPass string          `json:"context_to_pass,omitempty"`
		} `json:"predefined_routes,omitempty"`
		EnableGenericAgent bool   `json:"enable_generic_agent,omitempty"`
		NextStepID         string `json:"next_step_id,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal todo_task step: %w", err)
	}

	// Copy basic fields
	t.Type = temp.Type
	t.ID = temp.ID
	t.Title = temp.Title
	t.EnableGenericAgent = true // Generic agent is always enabled for todo task steps
	t.NextStepID = temp.NextStepID

	// Unmarshal nested todo_task_step
	if len(temp.TodoTaskStep) > 0 && string(temp.TodoTaskStep) != "null" {
		step, err := unmarshalStepFromJSON(temp.TodoTaskStep)
		if err != nil {
			return fmt.Errorf("failed to unmarshal todo_task_step: %w", err)
		}
		t.TodoTaskStep = step
	} else {
		t.TodoTaskStep = nil
	}

	// Unmarshal predefined_routes with nested sub_agent_step
	if len(temp.PredefinedRoutes) > 0 {
		t.PredefinedRoutes = make([]PlanOrchestrationRoute, len(temp.PredefinedRoutes))
		for i, route := range temp.PredefinedRoutes {
			t.PredefinedRoutes[i].RouteID = route.RouteID
			t.PredefinedRoutes[i].RouteName = route.RouteName
			t.PredefinedRoutes[i].Condition = route.Condition
			t.PredefinedRoutes[i].ContextToPass = route.ContextToPass

			// Unmarshal nested sub_agent_step
			if len(route.SubAgentStep) > 0 && string(route.SubAgentStep) != "null" {
				step, err := unmarshalStepFromJSON(route.SubAgentStep)
				if err != nil {
					return fmt.Errorf("failed to unmarshal sub_agent_step in predefined route %d: %w", i, err)
				}
				t.PredefinedRoutes[i].SubAgentStep = step
			} else {
				t.PredefinedRoutes[i].SubAgentStep = nil
			}
		}
	} else {
		t.PredefinedRoutes = nil
	}

	return nil
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
	Objective         string              `json:"objective,omitempty"`
	SuccessCriteria   string              `json:"success_criteria,omitempty"`
	WorkflowOptimized *bool               `json:"workflow_optimized,omitempty"`
	Steps             []PlanStepInterface `json:"-"`
	OrphanSteps       []PlanStepInterface `json:"-"`
}

// parseStepFromJSON parses a single step from raw JSON using the type field
func parseStepFromJSON(stepData json.RawMessage, index int, label string) (PlanStepInterface, error) {
	var stepWithType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(stepData, &stepWithType); err != nil {
		return nil, fmt.Errorf("failed to parse %s %d type: %w", label, index, err)
	}

	if stepWithType.Type == "" {
		return nil, fmt.Errorf("%s %d is missing required 'type' field (must be: regular, conditional, decision, orchestration, human_input, todo_task, or routing)", label, index)
	}

	switch stepWithType.Type {
	case "regular":
		var step RegularPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse regular %s %d: %w", label, index, err)
		}
		return &step, nil
	case "conditional":
		var step ConditionalPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse conditional %s %d: %w", label, index, err)
		}
		return &step, nil
	case "decision":
		var step DecisionPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse decision %s %d: %w", label, index, err)
		}
		return &step, nil
	case "orchestration":
		var step OrchestrationPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse orchestration %s %d: %w", label, index, err)
		}
		return &step, nil
	case "human_input":
		var step HumanInputPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse human_input %s %d: %w", label, index, err)
		}
		return &step, nil
	case "todo_task":
		var step TodoTaskPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse todo_task %s %d: %w", label, index, err)
		}
		return &step, nil
	case "routing":
		var step RoutingPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse routing %s %d: %w", label, index, err)
		}
		return &step, nil
	default:
		return nil, fmt.Errorf("unknown step type %q in %s %d (must be: regular, conditional, decision, orchestration, human_input, todo_task, or routing)", stepWithType.Type, label, index)
	}
}

// UnmarshalJSON implements custom unmarshaling for typed steps
func (pr *PlanningResponse) UnmarshalJSON(data []byte) error {
	var temp struct {
		Objective         string            `json:"objective"`
		SuccessCriteria   string            `json:"success_criteria"`
		WorkflowOptimized *bool             `json:"workflow_optimized"`
		Steps             []json.RawMessage `json:"steps"`
		OrphanSteps       []json.RawMessage `json:"orphan_steps"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal plan: %w", err)
	}

	pr.Objective = temp.Objective
	pr.SuccessCriteria = temp.SuccessCriteria
	pr.WorkflowOptimized = temp.WorkflowOptimized
	pr.Steps = make([]PlanStepInterface, len(temp.Steps))
	for i, stepData := range temp.Steps {
		typedStep, err := parseStepFromJSON(stepData, i, "step")
		if err != nil {
			return err
		}
		pr.Steps[i] = typedStep
	}

	if len(temp.OrphanSteps) > 0 {
		pr.OrphanSteps = make([]PlanStepInterface, len(temp.OrphanSteps))
		for i, stepData := range temp.OrphanSteps {
			typedStep, err := parseStepFromJSON(stepData, i, "orphan step")
			if err != nil {
				return err
			}
			pr.OrphanSteps[i] = typedStep
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for typed steps
func (pr PlanningResponse) MarshalJSON() ([]byte, error) {
	wrappedSteps := make([]json.RawMessage, len(pr.Steps))
	for i, step := range pr.Steps {
		stepJSON, err := json.Marshal(step)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal step %d: %w", i, err)
		}
		wrappedSteps[i] = stepJSON
	}

	result := map[string]interface{}{
		"steps": wrappedSteps,
	}

	if pr.Objective != "" {
		result["objective"] = pr.Objective
	}
	if pr.SuccessCriteria != "" {
		result["success_criteria"] = pr.SuccessCriteria
	}
	if pr.WorkflowOptimized != nil {
		result["workflow_optimized"] = *pr.WorkflowOptimized
	}

	if len(pr.OrphanSteps) > 0 {
		wrappedOrphans := make([]json.RawMessage, len(pr.OrphanSteps))
		for i, step := range pr.OrphanSteps {
			stepJSON, err := json.Marshal(step)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal orphan step %d: %w", i, err)
			}
			wrappedOrphans[i] = stepJSON
		}
		result["orphan_steps"] = wrappedOrphans
	}

	return json.Marshal(result)
}

// PartialPlanStep represents a partial update to a plan step (used only in tool schemas)
// NOTE: This struct works with typed steps (PlanStepInterface).
// Nested steps (DecisionStep, OrchestrationStep, IfTrueSteps, IfFalseSteps) are PlanStepInterface.
// Once plan.json is migrated to use new type-safe types, this struct can be updated accordingly.
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
	// Todo task step fields
	TodoTaskStep     map[string]interface{}   `json:"todo_task_step,omitempty"`    // Optional: Updated todo task step - will be converted to PlanStepInterface
	PredefinedRoutes []PlanOrchestrationRoute `json:"predefined_routes,omitempty"` // Optional: Updated predefined routes for todo task steps
	// Routing fields (used by both conditional, decision, and routing steps)
	IfTrueNextStepID  string `json:"if_true_next_step_id,omitempty"`  // Optional: Updated if_true_next_step_id
	IfFalseNextStepID string `json:"if_false_next_step_id,omitempty"` // Optional: Updated if_false_next_step_id
	NextStepID        string `json:"next_step_id,omitempty"`          // Optional: Updated next_step_id (for routing steps)
	// Routing step fields
	RoutingQuestion string         `json:"routing_question,omitempty"` // Optional: Updated routing question
	Routes          []RoutingRoute `json:"routes,omitempty"`           // Optional: Updated routes
	DefaultRouteID  string         `json:"default_route_id,omitempty"` // Optional: Updated default route ID
	// Human input step fields
	Question         string            `json:"question,omitempty"`            // Optional: Updated question
	VariableName     string            `json:"variable_name,omitempty"`       // Optional: Updated variable name
	ResponseType     string            `json:"response_type,omitempty"`       // Optional: Updated response type
	Options          []string          `json:"options,omitempty"`             // Optional: Updated options (for multiple_choice)
	IfYesNextStepID  string            `json:"if_yes_next_step_id,omitempty"` // Optional: Updated if_yes_next_step_id (for yesno)
	IfNoNextStepID   string            `json:"if_no_next_step_id,omitempty"`  // Optional: Updated if_no_next_step_id (for yesno)
	OptionRoutes     map[string]string `json:"option_routes,omitempty"`       // Optional: Updated option routes (for multiple_choice)
	ValidationSchema *ValidationSchema `json:"validation_schema,omitempty"`   // Optional: Updated validation schema
}

// planFileMutex ensures thread-safe access to plan.json
var planFileMutex sync.Mutex

// PlanFieldChange represents a single field change with old and new values
type PlanFieldChange struct {
	StepID   string      `json:"step_id"`   // Step ID that was changed
	Field    string      `json:"field"`     // Field name (title, description, success_criteria, etc.)
	OldValue interface{} `json:"old_value"` // Old value (can be nil if field didn't exist)
	NewValue interface{} `json:"new_value"` // New value
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
				"description": "REQUIRED: What context file this step will create for subsequent steps - e.g., 'step_1_results.json'. IMPORTANT: Execution agents work ONLY in the execution folder, so context output files will be written to the execution folder. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content (descriptions, logs, content > 1KB), store it in a separate markdown file (e.g., 'step_1_details.md') and reference it from JSON (e.g., {\"details_file\": \"step_1_details.md\"}). JSON should contain only structured data: counts, IDs, status, file references, brief summaries. Large text content belongs in markdown files."
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
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			},
			"validation_schema": {
				"type": "object",
				"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. You MUST generate this by parsing the success_criteria and extracting file names, field requirements, and validation rules. This enables pre-validation before LLM validation (improves speed by 50-70%). Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath like $.field_name), must_exist: boolean, value_type?: string (string/number/boolean/array/object), min_length?: number, max_length?: number, pattern?: string (VALID Go regex - must compile with regexp.Compile, ensure balanced parentheses, escape special chars), min_value?: number, max_value?: number, consistency_check?: {type: string (array_length/equals/greater_than/less_than), compare_with_path: string}}]}]}. Example: If success_criteria mentions 'File results.json contains status field and count field equals items array length', generate schema with file_name: 'results.json', json_checks: [{path: '$.status', must_exist: true}, {path: '$.count', must_exist: true, consistency_check: {type: 'array_length', compare_with_path: '$.items'}}]. CRITICAL FOR array_length CONSISTENCY CHECKS - AVOID COMMON MISTAKE: For array_length checks, path MUST point to COUNT/NUMBER field (e.g., '$.count', '$.total_expected_count', '$.length'), and compare_with_path MUST point to ARRAY field (e.g., '$.items', '$.downloaded_files', '$.files'). CORRECT examples: {path: '$.count', consistency_check: {type: 'array_length', compare_with_path: '$.items'}}, {path: '$.total_expected_count', consistency_check: {type: 'array_length', compare_with_path: '$.downloaded_files'}}. INCORRECT (SWAPPED) - DO NOT DO THIS: {path: '$.items', consistency_check: {type: 'array_length', compare_with_path: '$.count'}} ❌ WRONG - paths are swapped! Field name hints: Number fields often contain 'count', 'total', 'length', 'size', 'num'. Array fields often contain 'files', 'items', 'list', 'array', 'entries', 'results'. IMPORTANT: For pattern field, only use if you can generate a VALID Go regex pattern. Invalid patterns will be skipped. Examples of valid patterns: '^success$', '^\\d+$', '^[A-Za-z0-9_]+$'. Do NOT use incomplete patterns.",
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
											"pattern": {"type": "string", "description": "OPTIONAL: Valid Go regex pattern for string format validation. MUST be valid Go regex syntax (use regexp.Compile). Common patterns: '^[A-Z]+$' (uppercase only), '^\\d{4}-\\d{2}-\\d{2}$' (date format), '^[a-z0-9-]+$' (lowercase alphanumeric with hyphens). CRITICAL: Ensure all parentheses are balanced, escape special characters (use \\\\ for backslash, \\\\( for literal parenthesis), and test your pattern. Invalid patterns will be skipped with a warning. Examples: '^success$', '^\\d+$', '^[A-Za-z0-9_]+$'. Avoid: incomplete patterns like 'VALUE\\\\(SUBSTITUTE\\\\(.*' (missing closing parentheses)."},
											"min_value": {"type": "number"},
											"max_value": {"type": "number"},
											"consistency_check": {
												"type": "object",
												"properties": {
													"type": {"type": "string", "description": "Type of consistency check: 'array_length' (CRITICAL - AVOID COMMON MISTAKE: path must point to COUNT/number field like '$.count' or '$.total_expected_count', compare_with_path must point to ARRAY field like '$.items' or '$.downloaded_files'. DO NOT SWAP THEM! Correct: path='$.count', compare_with_path='$.items'. Wrong: path='$.items', compare_with_path='$.count' ❌), 'equals', 'greater_than', 'less_than', or 'in_array'"},
													"compare_with_path": {"type": "string", "description": "REQUIRED: JSONPath to compare with. For 'array_length' type: MUST point to the ARRAY field (e.g., '$.items', '$.downloaded_files', '$.files', '$.databases', '$.entries'). DO NOT use number fields here! For other types: JSONPath to compare with (e.g., '$.count', '$.status'). Must be a valid, non-empty JSONPath string."}
												},
												"required": ["type", "compare_with_path"]
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

// getAddDecisionStepSchema returns the JSON schema for add_decision_step tool
// Decision steps use a flattened structure: description, success_criteria, context_output, etc. are directly on the step
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
			"description": {
				"type": "string",
				"description": "REQUIRED: Description of what the decision step does during execution phase"
			},
			"success_criteria": {
				"type": "string",
				"description": "REQUIRED: How to verify the decision step's execution phase completed successfully"
			},
			"context_dependencies": {
				"type": "array",
				"items": {"type": "string"},
				"description": "REQUIRED: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."
			},
			"validation_schema": {
				"type": "object",
				"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. Generate by parsing success_criteria. Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath), must_exist: boolean, value_type?: string, min_length?: number, max_length?: number, pattern?: string, min_value?: number, max_value?: number, consistency_check?: {type: string, compare_with_path: string}}]}]}.",
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
			},
			"decision_evaluation_question": {
				"type": "string",
				"description": "REQUIRED: Question to evaluate the decision step's execution output (e.g., 'Is the deployment healthy and all services running?'). This is asked AFTER the step executes."
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
		"required": ["id", "title", "description", "success_criteria", "context_dependencies", "context_output", "validation_schema", "decision_evaluation_question", "if_true_next_step_id", "if_false_next_step_id", "insert_after_step_id"]
	}`
}

// getAddRoutingStepSchema returns the JSON schema for add_routing_step tool
func getAddRoutingStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this routing step. Generate a unique, URL-friendly ID based on the step title."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the routing step"
			},
			"description": {
				"type": "string",
				"description": "OPTIONAL: Description of what the routing step does during execution phase. If provided, the step executes first (execute-then-route mode). If omitted, routing evaluates prior context only (pure routing mode)."
			},
			"success_criteria": {
				"type": "string",
				"description": "OPTIONAL: How to verify execution completed. REQUIRED if description is provided (execute-then-route mode)."
			},
			"context_dependencies": {
				"type": "array",
				"items": {"type": "string"},
				"description": "REQUIRED: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "OPTIONAL: Context file this step will create. Only needed for execute-then-route mode."
			},
			"routing_question": {
				"type": "string",
				"description": "REQUIRED: Question to evaluate for route selection (e.g., 'Based on the classification result, which processing pipeline should handle this input?'). This is asked AFTER execution (if execute-then-route) or evaluated against prior context (if pure routing)."
			},
			"routes": {
				"type": "array",
				"minItems": 2,
				"items": {
					"type": "object",
					"properties": {
						"route_id": {
							"type": "string",
							"description": "REQUIRED: Unique identifier for this route (e.g., 'route_positive', 'route_negative')"
						},
						"route_name": {
							"type": "string",
							"description": "REQUIRED: Human-readable name for this route (e.g., 'Positive Sentiment')"
						},
						"condition": {
							"type": "string",
							"description": "REQUIRED: Description of when this route should be selected (e.g., 'Select when sentiment analysis indicates positive sentiment')"
						},
						"next_step_id": {
							"type": "string",
							"description": "REQUIRED: ID of step to route to when this route is selected. Use step ID from the plan, or 'end' to terminate workflow."
						}
					},
					"required": ["route_id", "route_name", "condition", "next_step_id"]
				},
				"description": "REQUIRED: Array of possible routes (minimum 2). Each route has a route_id, route_name, condition description, and next_step_id."
			},
			"default_route_id": {
				"type": "string",
				"description": "OPTIONAL: Fallback route_id to use if LLM picks an invalid route. Must match one of the route_ids in routes."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string to insert at the beginning."
			}
		},
		"required": ["id", "title", "routing_question", "routes", "context_dependencies", "insert_after_step_id"]
	}`
}

// getUpdateRoutingStepSchema returns the JSON schema for update_routing_step tool
func getUpdateRoutingStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the routing step to update. Use the step's id field from the plan."
			},
			"title": {
				"type": "string",
				"description": "OPTIONAL: New title for the step."
			},
			"description": {
				"type": "string",
				"description": "OPTIONAL: Updated description. Provide to enable execute-then-route mode."
			},
			"success_criteria": {
				"type": "string",
				"description": "OPTIONAL: Updated success criteria. Required if description is provided."
			},
			"context_dependencies": {
				"type": "array",
				"items": { "type": "string" },
				"description": "OPTIONAL: Updated context dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "OPTIONAL: Updated context output."
			},
			"routing_question": {
				"type": "string",
				"description": "OPTIONAL: Updated routing question."
			},
			"routes": {
				"type": "array",
				"minItems": 2,
				"items": {
					"type": "object",
					"properties": {
						"route_id": {"type": "string"},
						"route_name": {"type": "string"},
						"condition": {"type": "string"},
						"next_step_id": {"type": "string"}
					},
					"required": ["route_id", "route_name", "condition", "next_step_id"]
				},
				"description": "OPTIONAL: Updated routes array (minimum 2)."
			},
			"default_route_id": {
				"type": "string",
				"description": "OPTIONAL: Updated default route ID."
			}
		},
		"required": ["existing_step_id"]
	}`
}

// getAddHumanInputStepSchema returns the JSON schema for add_human_input_step tool
func getAddHumanInputStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this human input step. Generate a unique, URL-friendly ID based on the step title (e.g., 'ask-user-approval' from 'Ask User Approval')."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the human input step"
			},
			"question": {
				"type": "string",
				"description": "REQUIRED: The question to ask the human. This will be displayed to the user and execution will block until they respond."
			},
			"response_type": {
				"type": "string",
				"enum": ["text", "yesno", "multiple_choice"],
				"description": "OPTIONAL: Type of response expected. 'text' (default) for free-form text, 'yesno' for yes/no questions, 'multiple_choice' for selecting from options. Default: 'text'."
			},
			"options": {
				"type": "array",
				"items": { "type": "string" },
				"description": "OPTIONAL: Array of options for multiple_choice response type. Required if response_type is 'multiple_choice'."
			},
			"variable_name": {
				"type": "string",
				"description": "OPTIONAL: Variable name to store the human's response. If provided, the response will be stored in this variable and can be referenced in subsequent steps using {{variable_name}}."
			},
			"context_output": {
				"type": "string",
				"description": "OPTIONAL: Context file name to save the response (e.g., 'step-1.json'). Defaults to 'step-{index}.json' if not specified. The response will be saved as JSON with question, response, response_type, timestamp, and step details."
			},
			"next_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the next step to execute after receiving the response, or 'end' to terminate the workflow. This is used as the default routing for 'text' response type, or as fallback if conditional routing is not specified."
			},
			"if_yes_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: For 'yesno' response type, the step ID to route to when user responds 'yes', or 'end' to terminate. If not specified, next_step_id will be used."
			},
			"if_no_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: For 'yesno' response type, the step ID to route to when user responds 'no', or 'end' to terminate. If not specified, next_step_id will be used."
			},
			"option_routes": {
				"type": "object",
				"additionalProperties": { "type": "string" },
				"description": "OPTIONAL: For 'multiple_choice' response type, maps option index (as string '0', '1', etc.) or option value to next_step_id. Example: {'0': 'step-3', '1': 'step-4', 'Option C': 'step-5'}. If not specified, next_step_id will be used for all options."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "question", "next_step_id", "insert_after_step_id"]
	}`
}

// getAddTodoTaskStepSchema returns the JSON schema for add_todo_task_step tool
func getAddTodoTaskStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this todo task step. Generate a unique, URL-friendly ID based on the step title (e.g., 'process-data-tasks' from 'Process Data Tasks')."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the todo task step"
			},
			"todo_task_step": {
				"type": "object",
				"description": "REQUIRED: The main todo task orchestrator step metadata. This provides the overall context for the todo task management.",
				"properties": {
					"type": {"type": "string", "description": "REQUIRED: Step type - must be 'regular' for the inner orchestrator step."},
					"id": {"type": "string", "description": "REQUIRED: Stable step ID for the todo task orchestrator step"},
					"title": {"type": "string", "description": "REQUIRED: Title of the todo task orchestrator step"},
					"description": {"type": "string", "description": "REQUIRED: Description of the overall objective - the orchestrator will break this into tasks"},
					"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the overall objective is complete (all todos done)"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}, "description": "REQUIRED: List of context files from previous steps. Use empty array [] if no dependencies."},
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create with final summary."},
					"has_loop": {"type": "boolean", "description": "REQUIRED: Always set to false for todo task steps."}
				},
				"required": ["type", "id", "title", "description", "success_criteria", "context_dependencies", "has_loop", "context_output"]
			},
			"predefined_routes": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"route_id": {
							"type": "string",
							"description": "REQUIRED: Unique ID for this predefined route (e.g., 'api-fetcher', 'data-transformer')"
						},
						"route_name": {
							"type": "string",
							"description": "REQUIRED: Human-readable name for this route (e.g., 'API Data Fetcher', 'Data Transformer')"
						},
						"condition": {
							"type": "string",
							"description": "REQUIRED: Description of when to use this predefined agent (e.g., 'For tasks requiring API calls', 'For data transformation tasks')"
						},
						"sub_agent_step": {
							"type": "object",
							"description": "REQUIRED: The sub-agent step definition. This agent has learning and prevalidation. Use type='regular' for a focused execution agent, or type='todo_task' when this top-level route needs its own nested orchestrator. Only one nested todo_task layer is allowed.",
							"properties": {
								"type": {"type": "string", "description": "REQUIRED: Step type. Supported values for todo task routes: 'regular' or 'todo_task'. Nested todo_task routes may not contain another todo_task route."},
								"id": {"type": "string", "description": "REQUIRED: Stable step ID for the sub-agent step"},
								"title": {"type": "string", "description": "REQUIRED: Title of the sub-agent step"},
								"description": {"type": "string", "description": "REQUIRED: Description of what this specialized agent does"},
								"success_criteria": {"type": "string", "description": "REQUIRED: How to verify sub-agent completed successfully"},
								"context_dependencies": {"type": "array", "items": {"type": "string"}},
								"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create."},
								"has_loop": {"type": "boolean", "description": "REQUIRED: Always set to false."},
								"todo_task_step": {"type": "object", "description": "When type='todo_task', the child orchestrator's inner regular step."},
								"predefined_routes": {"type": "array", "description": "When type='todo_task', nested predefined routes for the child todo task."},
								"enable_generic_agent": {"type": "boolean", "description": "When type='todo_task', whether the child todo task can use a generic agent."},
								"next_step_id": {"type": "string", "description": "When type='todo_task', child next step ID. Ignored when used as a sub-agent."},
								"validation_schema": {
									"type": "object",
									"description": "REQUIRED: Validation schema for the sub-agent output",
									"properties": {
										"files": {
											"type": "array",
											"items": {
												"type": "object",
												"properties": {
													"file_name": {"type": "string"},
													"must_exist": {"type": "boolean"},
													"json_checks": {"type": "array", "items": {"type": "object"}}
												}
											}
										}
									}
								}
							},
							"required": ["type", "id", "title"]
						}
					},
					"required": ["route_id", "route_name", "condition", "sub_agent_step"]
				},
				"description": "OPTIONAL: Array of predefined routes for specialized sub-agents. These agents have learning and prevalidation. Use for tasks that benefit from specialized handling and accumulated learnings."
			},
			"next_step_id": {
				"type": "string",
				"description": "REQUIRED: ID of step to connect to after all todos are complete, or 'end' to end the workflow."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string to insert at the beginning."
			}
		},
		"required": ["id", "title", "todo_task_step", "next_step_id", "insert_after_step_id"]
	}`
}

// getUpdateTodoTaskStepSchema returns the JSON schema for update_todo_task_step tool
func getUpdateTodoTaskStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the todo task step to update. Use the step's id field from the plan."
			},
			"title": {
				"type": "string",
				"description": "OPTIONAL: New title for the todo task step"
			},
			"todo_task_step": {
				"type": "object",
				"description": "OPTIONAL: Updated main todo task orchestrator step metadata. Only include fields you want to change.",
				"properties": {
					"type": {"type": "string", "description": "Step type - must be 'regular' for the inner orchestrator step."},
					"id": {"type": "string", "description": "Stable step ID for the todo task orchestrator step"},
					"title": {"type": "string", "description": "Title of the todo task orchestrator step"},
					"description": {"type": "string", "description": "Description of the overall objective"},
					"success_criteria": {"type": "string", "description": "How to verify the overall objective is complete"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}, "description": "List of context files from previous steps"},
					"context_output": {"type": "string", "description": "Context file this step will create with final summary"},
					"has_loop": {"type": "boolean", "description": "Always set to false for todo task steps"}
				}
			},
			"predefined_routes": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"route_id": {
							"type": "string",
							"description": "Unique ID for this predefined route"
						},
						"route_name": {
							"type": "string",
							"description": "Human-readable name for this route"
						},
						"condition": {
							"type": "string",
							"description": "Description of when to use this predefined agent"
						},
						"sub_agent_step": {
							"type": "object",
							"description": "The sub-agent step definition. Can be a regular step or a nested todo_task step. Only one nested todo_task layer is allowed.",
							"properties": {
								"type": {"type": "string", "description": "Supported values: 'regular' or 'todo_task'."},
								"id": {"type": "string"},
								"title": {"type": "string"},
								"description": {"type": "string"},
								"success_criteria": {"type": "string"},
								"context_dependencies": {"type": "array", "items": {"type": "string"}},
								"context_output": {"type": "string"},
								"has_loop": {"type": "boolean"},
								"todo_task_step": {"type": "object", "description": "When type='todo_task', the child orchestrator's inner regular step."},
								"predefined_routes": {"type": "array", "description": "When type='todo_task', nested predefined routes for the child todo task."},
								"enable_generic_agent": {"type": "boolean", "description": "When type='todo_task', whether the child todo task can use a generic agent."},
								"next_step_id": {"type": "string", "description": "When type='todo_task', child next step ID. Ignored when used as a sub-agent."},
								"validation_schema": {"type": "object"}
							}
						}
					}
				},
				"description": "OPTIONAL: Updated array of predefined routes. This REPLACES the existing routes."
			},
			"next_step_id": {
				"type": "string",
				"description": "OPTIONAL: ID of step to connect to after all todos are complete, or 'end'"
			}
		},
		"required": ["existing_step_id"]
	}`
}

// getAddTodoTaskRouteSchema returns the JSON schema for add_todo_task_route tool
func getAddTodoTaskRouteSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the todo task step (parent step). Use the step's id field from the plan."
			},
			"new_route": {
				"type": "object",
				"description": "REQUIRED: The new predefined route to add. Must include all required fields.",
				"properties": {
					"route_id": {
						"type": "string",
						"description": "REQUIRED: Unique ID for this route (e.g., 'api-fetcher', 'data-transformer')"
					},
					"route_name": {
						"type": "string",
						"description": "REQUIRED: Human-readable name for this route (e.g., 'API Data Fetcher', 'Data Transformer')"
					},
					"condition": {
						"type": "string",
						"description": "REQUIRED: Description of when to use this predefined agent (e.g., 'For tasks requiring API calls')"
					},
					"sub_agent_step": {
						"type": "object",
						"description": "REQUIRED: The sub-agent step definition. This agent has learning and prevalidation. Use type='regular' for a focused execution agent. Use type='todo_task' ONLY when this route needs its own nested multi-phase orchestrator (1 level of nesting supported). IMPORTANT: A nested todo_task's routes must use type='regular' — do NOT nest a todo_task inside a todo_task inside a todo_task (2+ levels not supported).",
						"properties": {
							"type": {"type": "string", "description": "REQUIRED: Step type. Use 'regular' for standard execution. Use 'todo_task' for a nested orchestrator that manages multiple phases via its own routes. Maximum 1 level of todo_task nesting — a nested todo_task's sub_agent_step routes must always be 'regular'."},
							"id": {"type": "string", "description": "REQUIRED: Stable step ID for the sub-agent step"},
							"title": {"type": "string", "description": "REQUIRED: Title of the sub-agent step"},
							"description": {"type": "string", "description": "REQUIRED: Description of what this specialized agent does"},
							"success_criteria": {"type": "string", "description": "REQUIRED: How to verify sub-agent completed successfully"},
							"context_dependencies": {"type": "array", "items": {"type": "string"}},
							"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create."},
							"has_loop": {"type": "boolean", "description": "REQUIRED: Always set to false."},
							"todo_task_step": {"type": "object", "description": "When type='todo_task': the nested orchestrator's inner regular step metadata."},
							"predefined_routes": {"type": "array", "description": "When type='todo_task': predefined routes for the nested orchestrator. Each route's sub_agent_step must be type='regular'."},
							"enable_generic_agent": {"type": "boolean", "description": "When type='todo_task': whether the nested orchestrator can use a generic agent."},
							"validation_schema": {
								"type": "object",
								"description": "OPTIONAL: Validation schema for the sub-agent output"
							}
						},
						"required": ["type", "id", "title"]
					},
					"context_to_pass": {
						"type": "string",
						"description": "OPTIONAL: Specific context to pass to the sub-agent"
					}
				},
				"required": ["route_id", "route_name", "condition", "sub_agent_step"]
			}
		},
		"required": ["parent_step_id", "new_route"]
	}`
}

// getUpdateTodoTaskRouteSchema returns the JSON schema for update_todo_task_route tool
func getUpdateTodoTaskRouteSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the todo task step (parent step). Use the step's id field from the plan."
			},
			"existing_route_id": {
				"type": "string",
				"description": "REQUIRED: The route_id of the route to update. Use the route's route_id field from the plan."
			},
			"route_name": {
				"type": "string",
				"description": "OPTIONAL: Updated route name. Only include if you want to change it."
			},
			"condition": {
				"type": "string",
				"description": "OPTIONAL: Updated condition description. Only include if you want to change it."
			},
			"sub_agent_step": {
				"type": "object",
				"description": "OPTIONAL: Updated sub-agent step. Use type='regular' for standard execution. Use type='todo_task' for a 1-level nested orchestrator. A nested todo_task's own routes must be type='regular' (2+ levels not supported).",
				"properties": {
					"type": {"type": "string", "description": "Use 'regular' or 'todo_task' (1 level max). A nested todo_task's routes must use 'regular'."},
					"id": {"type": "string"},
					"title": {"type": "string"},
					"description": {"type": "string"},
					"success_criteria": {"type": "string"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}},
					"context_output": {"type": "string"},
					"has_loop": {"type": "boolean"},
					"todo_task_step": {"type": "object", "description": "When type='todo_task': nested orchestrator inner step metadata."},
					"predefined_routes": {"type": "array", "description": "When type='todo_task': nested routes — each must be type='regular'."},
					"enable_generic_agent": {"type": "boolean"},
					"validation_schema": {"type": "object"}
				},
				"required": ["type", "id", "title"]
			},
			"context_to_pass": {
				"type": "string",
				"description": "OPTIONAL: Updated context to pass to the sub-agent."
			}
		},
		"required": ["parent_step_id", "existing_route_id"]
	}`
}

// getDeleteTodoTaskRouteSchema returns the JSON schema for delete_todo_task_route tool
func getDeleteTodoTaskRouteSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the todo task step (parent step). Use the step's id field from the plan."
			},
			"deleted_route_id": {
				"type": "string",
				"description": "REQUIRED: The route_id of the route to delete. Use the route's route_id field from the plan."
			}
		},
		"required": ["parent_step_id", "deleted_route_id"]
	}`
}

// getUpdateDecisionStepSchema returns the JSON schema for update_decision_step tool
// Decision steps use a flattened structure with description, success_criteria, etc. directly on the step
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
				"description": "OPTIONAL: Updated description. Only include if you want to change the description. This is used during the execution phase. If omitted, the existing description is preserved."
			},
			"success_criteria": {
				"type": "string",
				"description": "OPTIONAL: Updated success criteria. Only include if you want to change it. This is used during the execution phase. If omitted, the existing success criteria is preserved."
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
			"validation_schema": {
				"type": "object",
				"description": "OPTIONAL: Updated validation schema. Only include if you want to change it. If omitted, the existing validation schema is preserved.",
				"properties": {
					"files": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file_name": {"type": "string"},
								"must_exist": {"type": "boolean"},
								"json_checks": {"type": "array", "items": {"type": "object"}}
							}
						}
					}
				}
			},
			"decision_evaluation_question": {
				"type": "string",
				"description": "OPTIONAL: Updated decision evaluation question. Only include if you want to change it. Question to evaluate the decision step's execution output (e.g., 'Is the deployment healthy and all services running?'). This is asked AFTER the step executes. If omitted, the existing question is preserved."
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

// getUpdateHumanInputStepSchema returns the JSON schema for update_human_input_step tool
func getUpdateHumanInputStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the human input step to update. Use the step's id field from the plan."
			},
			"title": {
				"type": "string",
				"description": "OPTIONAL: New title for the step. Only include if you want to rename the step. If omitted, the existing title is preserved."
			},
			"question": {
				"type": "string",
				"description": "OPTIONAL: Updated question to ask the human. Only include if you want to change it. If omitted, the existing question is preserved."
			},
			"response_type": {
				"type": "string",
				"enum": ["text", "yesno", "multiple_choice"],
				"description": "OPTIONAL: Updated response type. Only include if you want to change it. 'text' (default) for free-form text, 'yesno' for yes/no questions, 'multiple_choice' for selecting from options. If omitted, the existing response type is preserved."
			},
			"options": {
				"type": "array",
				"items": { "type": "string" },
				"description": "OPTIONAL: Updated options for multiple_choice response type. Only include if you want to change them. Required if response_type is 'multiple_choice'. If omitted, the existing options are preserved."
			},
			"variable_name": {
				"type": "string",
				"description": "OPTIONAL: Updated variable name to store the response. Only include if you want to change it. If omitted, the existing variable name is preserved."
			},
			"context_output": {
				"type": "string",
				"description": "OPTIONAL: Updated context file name to save the response. Only include if you want to change it. Defaults to 'step-{index}.json' if not specified. If omitted, the existing context output is preserved. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."
			},
			"next_step_id": {
				"type": "string",
				"description": "OPTIONAL: Updated next step ID. Only include if you want to change it. The ID of the next step to execute after receiving the response, or 'end' to terminate the workflow. If omitted, the existing next_step_id is preserved."
			},
			"if_yes_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: Updated if_yes_next_step_id. Only include if you want to change it. For 'yesno' response type, the step ID to route to when user responds 'yes', or 'end' to terminate. If omitted, the existing value is preserved."
			},
			"if_no_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: Updated if_no_next_step_id. Only include if you want to change it. For 'yesno' response type, the step ID to route to when user responds 'no', or 'end' to terminate. If omitted, the existing value is preserved."
			},
			"option_routes": {
				"type": "object",
				"additionalProperties": { "type": "string" },
				"description": "OPTIONAL: Updated option routes. Only include if you want to change them. For 'multiple_choice' response type, maps option index (as string '0', '1', etc.) or option value to next_step_id. If omitted, the existing option routes are preserved."
			}
		},
		"required": ["existing_step_id"]
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
				"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. You MUST generate this by parsing the success_criteria and extracting file names, field requirements, and validation rules. This enables pre-validation before LLM validation (improves speed by 50-70%). Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath like $.field_name), must_exist: boolean, value_type?: string (string/number/boolean/array/object), min_length?: number, max_length?: number, pattern?: string (VALID Go regex - must compile with regexp.Compile, ensure balanced parentheses, escape special chars), min_value?: number, max_value?: number, consistency_check?: {type: string (array_length/equals/greater_than/less_than), compare_with_path: string}}]}]}. Example: If success_criteria mentions 'File results.json contains status field and count field equals items array length', generate schema with file_name: 'results.json', json_checks: [{path: '$.status', must_exist: true}, {path: '$.count', must_exist: true, consistency_check: {type: 'array_length', compare_with_path: '$.items'}}]. CRITICAL FOR array_length CONSISTENCY CHECKS - AVOID COMMON MISTAKE: For array_length checks, path MUST point to COUNT/NUMBER field (e.g., '$.count', '$.total_expected_count', '$.length'), and compare_with_path MUST point to ARRAY field (e.g., '$.items', '$.downloaded_files', '$.files'). CORRECT examples: {path: '$.count', consistency_check: {type: 'array_length', compare_with_path: '$.items'}}, {path: '$.total_expected_count', consistency_check: {type: 'array_length', compare_with_path: '$.downloaded_files'}}. INCORRECT (SWAPPED) - DO NOT DO THIS: {path: '$.items', consistency_check: {type: 'array_length', compare_with_path: '$.count'}} ❌ WRONG - paths are swapped! Field name hints: Number fields often contain 'count', 'total', 'length', 'size', 'num'. Array fields often contain 'files', 'items', 'list', 'array', 'entries', 'results'. IMPORTANT: For pattern field, only use if you can generate a VALID Go regex pattern. Invalid patterns will be skipped. Examples of valid patterns: '^success$', '^\\d+$', '^[A-Za-z0-9_]+$'. Do NOT use incomplete patterns.",
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
											"pattern": {"type": "string", "description": "OPTIONAL: Valid Go regex pattern for string format validation. MUST be valid Go regex syntax (use regexp.Compile). Common patterns: '^[A-Z]+$' (uppercase only), '^\\d{4}-\\d{2}-\\d{2}$' (date format), '^[a-z0-9-]+$' (lowercase alphanumeric with hyphens). CRITICAL: Ensure all parentheses are balanced, escape special characters (use \\\\ for backslash, \\\\( for literal parenthesis), and test your pattern. Invalid patterns will be skipped with a warning. Examples: '^success$', '^\\d+$', '^[A-Za-z0-9_]+$'. Avoid: incomplete patterns like 'VALUE\\\\(SUBSTITUTE\\\\(.*' (missing closing parentheses)."},
											"min_value": {"type": "number"},
											"max_value": {"type": "number"},
											"consistency_check": {
												"type": "object",
												"properties": {
													"type": {"type": "string", "description": "Type of consistency check: 'array_length' (CRITICAL - AVOID COMMON MISTAKE: path must point to COUNT/number field like '$.count' or '$.total_expected_count', compare_with_path must point to ARRAY field like '$.items' or '$.downloaded_files'. DO NOT SWAP THEM! Correct: path='$.count', compare_with_path='$.items'. Wrong: path='$.items', compare_with_path='$.count' ❌), 'equals', 'greater_than', 'less_than', or 'in_array'"},
													"compare_with_path": {"type": "string", "description": "REQUIRED: JSONPath to compare with. For 'array_length' type: MUST point to the ARRAY field (e.g., '$.items', '$.downloaded_files', '$.files', '$.databases', '$.entries'). DO NOT use number fields here! For other types: JSONPath to compare with (e.g., '$.count', '$.status'). Must be a valid, non-empty JSONPath string."}
												},
												"required": ["type", "compare_with_path"]
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

// readPlanFromFile reads plan.json from the workspace.
// Uses normalizePathForWorkspaceAPI to build the full path, so the readFile function
// does not need to auto-prepend the workspace path (works with both orchestrator and chat-mode readers).
func readPlanFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*PlanningResponse, error) {
	planPath := normalizePathForWorkspaceAPI(filepath.Join("planning", "plan.json"), workspacePath)

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

// writePlanToFile writes PlanningResponse to plan.json in the workspace.
// Validates that all steps have IDs before saving (planning agent should always generate them).
// Uses normalizePathForWorkspaceAPI to build the full path.
func writePlanToFile(ctx context.Context, workspacePath string, plan *PlanningResponse, _ func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	planPath := normalizePathForWorkspaceAPI(filepath.Join("planning", "plan.json"), workspacePath)

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
	case "human_input":
		var step HumanInputPlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse human_input step: %w", err)
		}
		typedStep = &step
	case "todo_task":
		var step TodoTaskPlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse todo_task step: %w", err)
		}
		typedStep = &step
	case "routing":
		var step RoutingPlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse routing step: %w", err)
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

	// Default to "regular" type if not specified (common for sub_agent_steps in routes)
	stepType := stepWithType.Type
	if stepType == "" {
		stepType = "regular"
	}

	// Unmarshal based on type
	var typedStep PlanStepInterface
	switch stepType {
	case "regular":
		var step RegularPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse regular step: %w", err)
		}
		// Ensure Type field is set (may be empty if not in JSON)
		step.Type = StepTypeRegular
		typedStep = &step
	case "conditional":
		var step ConditionalPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse conditional step: %w", err)
		}
		step.Type = StepTypeConditional
		typedStep = &step
	case "decision":
		var step DecisionPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse decision step: %w", err)
		}
		step.Type = StepTypeDecision
		typedStep = &step
	case "orchestration":
		var step OrchestrationPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse orchestration step: %w", err)
		}
		step.Type = StepTypeOrchestration
		typedStep = &step
	case "human_input":
		var step HumanInputPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse human_input step: %w", err)
		}
		step.Type = StepTypeHumanInput
		typedStep = &step
	case "todo_task":
		var step TodoTaskPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse todo_task step: %w", err)
		}
		step.Type = StepTypeTodoTask
		typedStep = &step
	case "routing":
		var step RoutingPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse routing step: %w", err)
		}
		step.Type = StepTypeRouting
		typedStep = &step
	default:
		return nil, fmt.Errorf("unknown step type %q (must be: regular, conditional, decision, orchestration, human_input, todo_task, or routing)", stepType)
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

// validateRegexPatternsInSchema validates all regex patterns in a ValidationSchema
// Returns an error with details if any pattern is invalid
// Note: Patterns are already fixed during JSON unmarshaling (see ValidationSchema.UnmarshalJSON),
// but we keep a safety net here in case patterns come from other sources
func validateRegexPatternsInSchema(schema *ValidationSchema) error {
	if schema == nil {
		return nil
	}

	var errors []string
	for _, fileRule := range schema.Files {
		for i := range fileRule.JSONChecks {
			check := &fileRule.JSONChecks[i]
			if check.Pattern != "" {
				// Safety net: Fix double-escaped patterns if they weren't fixed during unmarshaling
				// (e.g., if schema was created programmatically rather than from JSON)
				fixedPattern := fixDoubleEscapedPattern(check.Pattern)
				if fixedPattern != check.Pattern {
					// Update the pattern in the schema to the fixed version
					check.Pattern = fixedPattern
				}

				// Validate the pattern
				_, err := regexp.Compile(check.Pattern)
				if err != nil {
					errors = append(errors, fmt.Sprintf("Invalid regex pattern in file '%s' at path '%s': %v (pattern: '%s')", fileRule.FileName, check.Path, err, check.Pattern))
				}
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation schema contains invalid regex patterns:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// validateJSONPathSyntax validates that all JSONPaths in the schema are syntactically valid
func validateJSONPathSyntax(schema *ValidationSchema) error {
	if schema == nil {
		return nil
	}

	var errors []string
	dummyData := map[string]interface{}{}

	for _, fileRule := range schema.Files {
		for _, check := range fileRule.JSONChecks {
			// Validate check.Path
			path := strings.TrimSpace(check.Path)
			if path == "" {
				errors = append(errors, fmt.Sprintf("File '%s': Empty path is not allowed", fileRule.FileName))
			} else {
				if !strings.HasPrefix(path, "$.") {
					errors = append(errors, fmt.Sprintf("File '%s': Path '%s' must start with '$.'", fileRule.FileName, path))
				} else {
					// Check syntax by attempting to evaluate against dummy data
					_, err := jsonpath.Get(path, dummyData)
					if err != nil && strings.Contains(err.Error(), "parsing error") {
						errors = append(errors, fmt.Sprintf("File '%s': Invalid JSONPath syntax in '%s': %v", fileRule.FileName, path, err))
					}
				}
			}

			// Validate consistency_check if it exists and is actually specified
			// Skip validation if both Type and CompareWithPath are empty (treat as not specified)
			if check.ConsistencyCheck != nil {
				comparePath := strings.TrimSpace(check.ConsistencyCheck.CompareWithPath)
				checkType := strings.TrimSpace(check.ConsistencyCheck.Type)

				// Only validate if at least one field is specified
				if comparePath == "" && checkType == "" {
					// Both empty - treat as if consistency_check wasn't specified, skip validation
				} else if comparePath == "" || checkType == "" {
					// One is specified but not the other - error
					if comparePath == "" {
						errors = append(errors, fmt.Sprintf("File '%s': compare_with_path is required when consistency check type is specified", fileRule.FileName))
					}
					if checkType == "" {
						errors = append(errors, fmt.Sprintf("File '%s': type is required when consistency check compare_with_path is specified", fileRule.FileName))
					}
				} else {
					// Both specified - validate the path syntax
					if !strings.HasPrefix(comparePath, "$.") {
						errors = append(errors, fmt.Sprintf("File '%s': compare_with_path '%s' must start with '$.'", fileRule.FileName, comparePath))
					} else {
						// Check syntax
						_, err := jsonpath.Get(comparePath, dummyData)
						if err != nil && strings.Contains(err.Error(), "parsing error") {
							errors = append(errors, fmt.Sprintf("File '%s': Invalid JSONPath syntax in compare_with_path '%s': %v", fileRule.FileName, comparePath, err))
						}
					}
				}
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation schema contains invalid JSONPaths:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// detectFieldTypeFromPath analyzes a JSONPath to determine if it likely points to a number/count field or an array field
// Returns: (isNumberField, isArrayField)
func detectFieldTypeFromPath(path string) (bool, bool) {
	pathLower := strings.ToLower(path)

	// Number/count field indicators
	numberIndicators := []string{"count", "total", "length", "size", "num", "number", "amount", "quantity", "sum"}
	for _, indicator := range numberIndicators {
		if strings.Contains(pathLower, indicator) {
			return true, false
		}
	}

	// Array field indicators
	arrayIndicators := []string{"files", "items", "list", "array", "entries", "results", "data", "records", "elements", "objects", "values"}
	for _, indicator := range arrayIndicators {
		if strings.Contains(pathLower, indicator) {
			return false, true
		}
	}

	return false, false
}

// validateArrayLengthConsistencyChecks validates array_length consistency checks in a ValidationSchema
// This performs basic structural validation and detects ambiguous paths based on field name patterns
// Runtime validation (pre_validation.go) supports bidirectional checks (array_length(count, array) OR array_length(array, count))
func validateArrayLengthConsistencyChecks(schema *ValidationSchema) error {
	if schema == nil {
		return nil
	}

	var errors []string
	for _, fileRule := range schema.Files {
		for _, check := range fileRule.JSONChecks {
			if check.ConsistencyCheck == nil || check.ConsistencyCheck.Type != "array_length" {
				continue
			}

			path := strings.TrimSpace(check.Path)
			comparePath := strings.TrimSpace(check.ConsistencyCheck.CompareWithPath)

			// Basic validation: paths must be non-empty and different
			if path == "" {
				errors = append(errors, fmt.Sprintf(
					"File '%s': array_length consistency check has empty path. Path must be a valid JSONPath.",
					fileRule.FileName))
				continue
			}

			if comparePath == "" {
				errors = append(errors, fmt.Sprintf(
					"File '%s': array_length consistency check has empty compare_with_path. compare_with_path must be a valid JSONPath.",
					fileRule.FileName))
				continue
			}

			if path == comparePath {
				errors = append(errors, fmt.Sprintf(
					"File '%s': array_length consistency check has path '%s' equal to compare_with_path '%s'. They must be different - one should point to a COUNT/number field, the other to an ARRAY field.",
					fileRule.FileName, path, comparePath))
				continue
			}

			// Detect field types to check for ambiguity
			pathIsNumber, pathIsArray := detectFieldTypeFromPath(path)
			compareIsNumber, compareIsArray := detectFieldTypeFromPath(comparePath)

			// Check for ambiguous configurations
			if pathIsArray && compareIsArray {
				errors = append(errors, fmt.Sprintf(
					"File '%s': AMBIGUOUS CHECK! Both path '%s' and compare_with_path '%s' contain array field indicators. One must be a NUMBER/COUNT field. Please check your paths.",
					fileRule.FileName, path, comparePath))
			} else if pathIsNumber && compareIsNumber {
				errors = append(errors, fmt.Sprintf(
					"File '%s': AMBIGUOUS CHECK! Both path '%s' and compare_with_path '%s' contain number field indicators. One must be an ARRAY field. Please check your paths.",
					fileRule.FileName, path, comparePath))
			}

			// Note: We no longer enforce strict order (Number, Array) vs (Array, Number)
			// because the runtime validator now handles both cases.
			// We only flag if both look like the SAME type.
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation schema contains misconfigured array_length consistency checks:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// updateValidationSchemaOnStep updates validation schema on any step type
func updateValidationSchemaOnStep(step PlanStepInterface, schema *ValidationSchema) {
	switch s := step.(type) {
	case *RegularPlanStep:
		s.ValidationSchema = schema
	case *ConditionalPlanStep:
		s.ValidationSchema = schema
	case *DecisionPlanStep:
		// DecisionPlanStep has validation schema directly via CommonStepFields
		s.ValidationSchema = schema
	case *OrchestrationPlanStep:
		// For OrchestrationPlanStep, validation schema is on the inner OrchestrationStep
		if s.OrchestrationStep != nil {
			updateValidationSchemaOnStep(s.OrchestrationStep, schema)
		}
	case *HumanInputPlanStep:
		s.ValidationSchema = schema
	case *TodoTaskPlanStep:
		// For TodoTaskPlanStep, validation schema is on the inner TodoTaskStep
		if s.TodoTaskStep != nil {
			updateValidationSchemaOnStep(s.TodoTaskStep, schema)
		}
	case *RoutingPlanStep:
		s.ValidationSchema = schema
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
			// DecisionPlanStep is now flattened - compare decision-specific fields
			// Common fields (Title, Description, SuccessCriteria, etc.) are already compared above
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
		// DecisionPlanStep is now flattened - all fields are directly on the step
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
		if partialUpdate.ValidationSchema != nil {
			updated.ValidationSchema = partialUpdate.ValidationSchema
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

	case *HumanInputPlanStep:
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
		if partialUpdate.Question != "" {
			updated.Question = partialUpdate.Question
		}
		if partialUpdate.VariableName != "" {
			updated.VariableName = partialUpdate.VariableName
		}
		if partialUpdate.ResponseType != "" {
			updated.ResponseType = partialUpdate.ResponseType
		}
		if partialUpdate.Options != nil {
			updated.Options = partialUpdate.Options
		}
		if partialUpdate.NextStepID != "" {
			updated.NextStepID = partialUpdate.NextStepID
		}
		if partialUpdate.IfYesNextStepID != "" {
			updated.IfYesNextStepID = partialUpdate.IfYesNextStepID
		}
		if partialUpdate.IfNoNextStepID != "" {
			updated.IfNoNextStepID = partialUpdate.IfNoNextStepID
		}
		if partialUpdate.OptionRoutes != nil {
			updated.OptionRoutes = partialUpdate.OptionRoutes
		}
		if partialUpdate.ValidationSchema != nil {
			updated.ValidationSchema = partialUpdate.ValidationSchema
		}
		return &updated

	case *TodoTaskPlanStep:
		updated := *step
		if partialUpdate.Title != "" {
			updated.Title = partialUpdate.Title
		}
		if partialUpdate.TodoTaskStep != nil {
			// If we have an existing nested step, merge the partial update into it to preserve fields
			// that weren't in the update (like context_dependencies, validation_schema, etc.)
			if updated.TodoTaskStep != nil {
				// Create a PartialPlanStep from the map by extracting fields directly
				nestedPartial := PartialPlanStep{}
				// Extract common fields from the map
				if desc, ok := partialUpdate.TodoTaskStep["description"].(string); ok {
					nestedPartial.Description = desc
				}
				if title, ok := partialUpdate.TodoTaskStep["title"].(string); ok {
					nestedPartial.Title = title
				}
				if successCriteria, ok := partialUpdate.TodoTaskStep["success_criteria"].(string); ok {
					nestedPartial.SuccessCriteria = successCriteria
				}
				if contextDeps, ok := partialUpdate.TodoTaskStep["context_dependencies"].([]interface{}); ok {
					nestedPartial.ContextDependencies = make([]string, 0, len(contextDeps))
					for _, dep := range contextDeps {
						if depStr, ok := dep.(string); ok {
							nestedPartial.ContextDependencies = append(nestedPartial.ContextDependencies, depStr)
						}
					}
				}
				if contextOutput, ok := partialUpdate.TodoTaskStep["context_output"]; ok {
					if contextOutputMap, ok := contextOutput.(map[string]interface{}); ok {
						contextOutputJSON, _ := json.Marshal(contextOutputMap)
						json.Unmarshal(contextOutputJSON, &nestedPartial.ContextOutput)
					} else if contextOutputStr, ok := contextOutput.(string); ok {
						nestedPartial.ContextOutput = FlexibleContextOutput(contextOutputStr)
					}
				}
				if validationSchema, ok := partialUpdate.TodoTaskStep["validation_schema"]; ok {
					if validationSchemaMap, ok := validationSchema.(map[string]interface{}); ok {
						validationSchemaJSON, _ := json.Marshal(validationSchemaMap)
						var vs ValidationSchema
						if json.Unmarshal(validationSchemaJSON, &vs) == nil {
							nestedPartial.ValidationSchema = &vs
						}
					}
				}
				// Merge the nested partial update into the existing nested step
				updated.TodoTaskStep = mergePartialStepUpdate(updated.TodoTaskStep, nestedPartial)
			} else {
				// No existing nested step - convert and assign directly
				converted, err := convertMapToStep(partialUpdate.TodoTaskStep)
				if err != nil {
					return existingStep
				}
				updated.TodoTaskStep = converted
			}
		}
		if partialUpdate.PredefinedRoutes != nil {
			// Convert routes - SubAgentStep needs conversion
			updated.PredefinedRoutes = make([]PlanOrchestrationRoute, len(partialUpdate.PredefinedRoutes))
			for i, route := range partialUpdate.PredefinedRoutes {
				updated.PredefinedRoutes[i] = route
			}
		}
		if partialUpdate.NextStepID != "" {
			updated.NextStepID = partialUpdate.NextStepID
		}
		if partialUpdate.ValidationSchema != nil && updated.TodoTaskStep != nil {
			// Update validation schema on the inner TodoTaskStep
			updateValidationSchemaOnStep(updated.TodoTaskStep, partialUpdate.ValidationSchema)
		}
		return &updated

	case *RoutingPlanStep:
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
		if partialUpdate.ValidationSchema != nil {
			updated.ValidationSchema = partialUpdate.ValidationSchema
		}
		if partialUpdate.RoutingQuestion != "" {
			updated.RoutingQuestion = partialUpdate.RoutingQuestion
		}
		if len(partialUpdate.Routes) > 0 {
			updated.Routes = partialUpdate.Routes
		}
		if partialUpdate.DefaultRouteID != "" {
			updated.DefaultRouteID = partialUpdate.DefaultRouteID
		}
		return &updated

	default:
		// Unknown type - return original
		return existingStep
	}
}

// updateSingleStep is a helper function that updates a single step in the plan
// Returns the step index and field changes for changelog
// findStepByID recursively searches for a step with the given ID in the plan steps.
// It returns the step interface, its index at the current level, and the slice it belongs to.
func findStepByID(steps []PlanStepInterface, id string) (PlanStepInterface, int, []PlanStepInterface) {
	for i, step := range steps {
		if step.GetID() == id {
			return step, i, steps
		}

		// Search in nested structures
		switch s := step.(type) {
		case *ConditionalPlanStep:
			if foundStep, idx, slice := findStepByID(s.IfTrueSteps, id); foundStep != nil {
				return foundStep, idx, slice
			}
			if foundStep, idx, slice := findStepByID(s.IfFalseSteps, id); foundStep != nil {
				return foundStep, idx, slice
			}
		case *OrchestrationPlanStep:
			if s.OrchestrationStep != nil && s.OrchestrationStep.GetID() == id {
				// Special case: the internal orchestration_step matches
				// We return it but we need to handle its update specifically since it's not in a slice
				return s.OrchestrationStep, -1, nil
			}
			for _, route := range s.OrchestrationRoutes {
				if route.SubAgentStep != nil {
					if foundStep, idx, slice := findStepByID([]PlanStepInterface{route.SubAgentStep}, id); foundStep != nil {
						return foundStep, idx, slice
					}
				}
			}
		case *TodoTaskPlanStep:
			if s.TodoTaskStep != nil && s.TodoTaskStep.GetID() == id {
				// Special case: the internal todo_task_step matches
				return s.TodoTaskStep, -1, nil
			}
			for _, route := range s.PredefinedRoutes {
				if route.SubAgentStep != nil {
					if foundStep, idx, slice := findStepByID([]PlanStepInterface{route.SubAgentStep}, id); foundStep != nil {
						return foundStep, idx, slice
					}
				}
			}
		}
	}
	return nil, -1, nil
}

// updateStepRecursively updates a step within the plan structure, even if nested.
func updateStepRecursively(steps []PlanStepInterface, partialUpdate PartialPlanStep, fieldChanges *[]PlanFieldChange) (bool, int) {
	for i, step := range steps {
		if step.GetID() == partialUpdate.ExistingStepID {
			// Found the step at this level
			steps[i] = mergePartialStepUpdate(step, partialUpdate)
			return true, i
		}

		// Recursively search and update in nested structures
		switch s := step.(type) {
		case *ConditionalPlanStep:
			if updated, _ := updateStepRecursively(s.IfTrueSteps, partialUpdate, fieldChanges); updated {
				return true, i // Return parent index for learning unlock purposes (simplified)
			}
			if updated, _ := updateStepRecursively(s.IfFalseSteps, partialUpdate, fieldChanges); updated {
				return true, i
			}
		case *OrchestrationPlanStep:
			if s.OrchestrationStep != nil && s.OrchestrationStep.GetID() == partialUpdate.ExistingStepID {
				s.OrchestrationStep = mergePartialStepUpdate(s.OrchestrationStep, partialUpdate)
				return true, i
			}
			for j := range s.OrchestrationRoutes {
				if s.OrchestrationRoutes[j].SubAgentStep != nil {
					// We need to wrap the sub_agent_step in a temporary slice for the recursive call
					tmpSlice := []PlanStepInterface{s.OrchestrationRoutes[j].SubAgentStep}
					if updated, _ := updateStepRecursively(tmpSlice, partialUpdate, fieldChanges); updated {
						s.OrchestrationRoutes[j].SubAgentStep = tmpSlice[0]
						return true, i
					}
				}
			}
		case *TodoTaskPlanStep:
			if s.TodoTaskStep != nil && s.TodoTaskStep.GetID() == partialUpdate.ExistingStepID {
				s.TodoTaskStep = mergePartialStepUpdate(s.TodoTaskStep, partialUpdate)
				return true, i
			}
			for j := range s.PredefinedRoutes {
				if s.PredefinedRoutes[j].SubAgentStep != nil {
					tmpSlice := []PlanStepInterface{s.PredefinedRoutes[j].SubAgentStep}
					if updated, _ := updateStepRecursively(tmpSlice, partialUpdate, fieldChanges); updated {
						s.PredefinedRoutes[j].SubAgentStep = tmpSlice[0]
						return true, i
					}
				}
			}
		}
	}
	return false, -1
}

func updateSingleStep(plan *PlanningResponse, partialUpdate PartialPlanStep, fieldChanges *[]PlanFieldChange) (int, []string, error) {
	// Find the step to update (for field tracking)
	existingStep, _, _ := findStepByID(plan.Steps, partialUpdate.ExistingStepID)

	if existingStep == nil {
		// Try orphan steps
		existingStep, _, _ = findStepByID(plan.OrphanSteps, partialUpdate.ExistingStepID)
	}

	if existingStep == nil {
		availableIDs := make([]string, 0, len(plan.Steps)+len(plan.OrphanSteps))
		for _, step := range plan.Steps {
			availableIDs = append(availableIDs, step.GetID())
		}
		for _, step := range plan.OrphanSteps {
			availableIDs = append(availableIDs, step.GetID())
		}
		return -1, nil, fmt.Errorf("step ID '%s' not found in existing plan. Available top-level step IDs: %v", partialUpdate.ExistingStepID, availableIDs)
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
	// Decision step fields - DecisionPlanStep is now flattened, so only track decision_evaluation_question
	// Common fields (description, success_criteria, etc.) are tracked via the common field tracking above
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
	// Todo task step fields
	if partialUpdate.TodoTaskStep != nil {
		changedFields = append(changedFields, "todo_task_step")
		// Convert new todo task step from map to PlanStepInterface
		newTodoTaskStep, err := convertMapToStep(partialUpdate.TodoTaskStep)
		if err == nil {
			// Get old todo task step
			var oldTodoTaskStep PlanStepInterface
			if todoTaskStep, ok := existingStep.(*TodoTaskPlanStep); ok {
				oldTodoTaskStep = todoTaskStep.TodoTaskStep
			}
			// Compare nested step fields in detail
			compareNestedStepFields(oldTodoTaskStep, newTodoTaskStep, partialUpdate.ExistingStepID, "todo_task_step", fieldChanges)
		} else {
			// Fallback to ID-only tracking if conversion fails
			oldTodoTaskStep := "nil"
			if todoTaskStep, ok := existingStep.(*TodoTaskPlanStep); ok && todoTaskStep.TodoTaskStep != nil {
				oldTodoTaskStep = todoTaskStep.TodoTaskStep.GetID()
			}
			newTodoTaskStepID := ""
			if id, ok := partialUpdate.TodoTaskStep["id"].(string); ok {
				newTodoTaskStepID = id
			}
			*fieldChanges = append(*fieldChanges, PlanFieldChange{
				StepID:   partialUpdate.ExistingStepID,
				Field:    "todo_task_step",
				OldValue: oldTodoTaskStep,
				NewValue: newTodoTaskStepID,
			})
		}
	}
	if partialUpdate.PredefinedRoutes != nil {
		changedFields = append(changedFields, "predefined_routes")
		// Get old routes
		var oldRoutes []PlanOrchestrationRoute
		if todoTaskStep, ok := existingStep.(*TodoTaskPlanStep); ok {
			oldRoutes = todoTaskStep.PredefinedRoutes
		}
		// Compare routes in detail
		if !equalOrchestrationRoutes(oldRoutes, partialUpdate.PredefinedRoutes) {
			// Track detailed changes for each route
			maxLen := len(oldRoutes)
			if len(partialUpdate.PredefinedRoutes) > maxLen {
				maxLen = len(partialUpdate.PredefinedRoutes)
			}
			for i := 0; i < maxLen; i++ {
				routePrefix := fmt.Sprintf("predefined_routes[%d]", i)
				if i >= len(oldRoutes) {
					// New route added
					newRouteJSON, _ := json.Marshal(partialUpdate.PredefinedRoutes[i])
					*fieldChanges = append(*fieldChanges, PlanFieldChange{
						StepID:   partialUpdate.ExistingStepID,
						Field:    routePrefix,
						OldValue: nil,
						NewValue: string(newRouteJSON),
					})
				} else if i >= len(partialUpdate.PredefinedRoutes) {
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
					newRoute := partialUpdate.PredefinedRoutes[i]
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
		} else if todoTaskStep, ok := existingStep.(*TodoTaskPlanStep); ok {
			oldNextStepID = todoTaskStep.NextStepID
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

	// Validate regex patterns in validation schema before merging
	if partialUpdate.ValidationSchema != nil {
		if err := validateRegexPatternsInSchema(partialUpdate.ValidationSchema); err != nil {
			return -1, nil, fmt.Errorf("invalid regex patterns in validation schema: %w", err)
		}

		// Validate JSONPath syntax
		if err := validateJSONPathSyntax(partialUpdate.ValidationSchema); err != nil {
			return -1, nil, fmt.Errorf("invalid JSONPath syntax in validation schema: %w", err)
		}

		// Validate array_length consistency checks
		if err := validateArrayLengthConsistencyChecks(partialUpdate.ValidationSchema); err != nil {
			return -1, nil, fmt.Errorf("invalid array_length consistency checks in validation schema: %w", err)
		}
	}

	// Apply updates recursively
	updated, topLevelIndex := updateStepRecursively(plan.Steps, partialUpdate, fieldChanges)
	if !updated {
		// This shouldn't happen because we already checked for existence using findStepByID
		return -1, nil, fmt.Errorf("failed to apply update to step '%s'", partialUpdate.ExistingStepID)
	}

	return topLevelIndex, changedFields, nil
}

// createUpdateRegularStepExecutor creates an executor function for update_regular_step tool
func createUpdateRegularStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PartialPlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("failed to marshal step: %w", err)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf("failed to parse step: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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

// createUpdateDecisionStepExecutor creates an executor function for update_decision_step tool
func createUpdateDecisionStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PartialPlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("failed to marshal step: %w", err)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf("failed to parse step: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the decision step
		var existingStep PlanStepInterface
		for _, step := range plan.Steps {
			if step.GetID() == partialUpdate.ExistingStepID {
				existingStep = step
				break
			}
		}
		if existingStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs)
		}

		// Validate it's a decision step before updating
		_, ok := existingStep.(*DecisionPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a decision step", partialUpdate.ExistingStepID)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		var stepIndex int
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
			return "", fmt.Errorf("validation failed after update: %w", err)
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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

// extractStringArray extracts a []string from args[key], handling cases where the LLM
// sends either a proper JSON array or a stringified array (JSON or Python-style).
func extractStringArray(args map[string]interface{}, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("missing required argument: %s", key)
	}

	// Case 1: already a []interface{} (normal JSON array)
	if arr, ok := raw.([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, v := range arr {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("invalid %s: array elements must be strings", key)
			}
			result = append(result, s)
		}
		return result, nil
	}

	// Case 2: string — try to parse as JSON array, then fall back to Python-style
	if s, ok := raw.(string); ok {
		s = strings.TrimSpace(s)
		// Try JSON parse first: ["a", "b"]
		var parsed []string
		if err := json.Unmarshal([]byte(s), &parsed); err == nil {
			return parsed, nil
		}
		// Python-style: ['a', 'b'] — replace single quotes with double quotes and retry
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			fixed := strings.ReplaceAll(s, "'", "\"")
			if err := json.Unmarshal([]byte(fixed), &parsed); err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("invalid %s: could not parse string as array: %s", key, s)
	}

	return nil, fmt.Errorf("invalid %s: expected array or string, got %T", key, raw)
}

// createDeletePlanStepsExecutor creates an executor function for delete_plan_steps tool
// unlockLearningsFunc is optional - if provided, it will be called after plan deletions to unlock learnings
// Note: For deleted steps, we unlock based on the old plan's step indices before deletion
func createDeletePlanStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract deleted_step_ids from args
		deletedIDs, err := extractStringArray(args, "deleted_step_ids")
		if err != nil {
			return "", err
		}

		// Read current plan
		oldPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
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
		for _, step := range oldPlan.Steps {
			existingStepsMap[step.GetID()] = true
		}
		for _, step := range oldPlan.OrphanSteps {
			existingStepsMap[step.GetID()] = true
		}
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				// Build list of available step IDs for better error message
				availableIDs := make([]string, 0, len(oldPlan.Steps)+len(oldPlan.OrphanSteps))
				for _, step := range oldPlan.Steps {
					availableIDs = append(availableIDs, step.GetID())
				}
				for _, step := range oldPlan.OrphanSteps {
					availableIDs = append(availableIDs, step.GetID())
				}
				return "", fmt.Errorf("step ID '%s' not found in existing plan (cannot delete). Available step IDs: %v", id, availableIDs)
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

		// Also capture and filter orphan steps
		for i, step := range oldPlan.OrphanSteps {
			stepID := step.GetID()
			if deletedSet[stepID] {
				stepJSON, err := json.Marshal(step)
				if err != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to marshal deleted orphan step %s for changelog: %v", stepID, err))
					continue
				}
				deletedSteps = append(deletedSteps, stepJSON)
				deletedStepIndices[stepID] = len(oldPlan.Steps) + i // offset by main steps count
			}
		}

		filteredOrphanSteps := make([]PlanStepInterface, 0, len(oldPlan.OrphanSteps))
		for _, step := range oldPlan.OrphanSteps {
			if !deletedSet[step.GetID()] {
				filteredOrphanSteps = append(filteredOrphanSteps, step)
			}
		}

		newPlan := &PlanningResponse{Steps: filteredSteps, OrphanSteps: filteredOrphanSteps}

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, newPlan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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

// createUpdateHumanInputStepExecutor creates an executor function for update_human_input_step tool
func createUpdateHumanInputStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PartialPlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("failed to marshal step: %w", err)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf("failed to parse step: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the human input step
		var existingStep PlanStepInterface
		for _, step := range plan.Steps {
			if step.GetID() == partialUpdate.ExistingStepID {
				existingStep = step
				break
			}
		}
		if existingStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs)
		}

		// Validate it's a human input step before updating
		_, ok := existingStep.(*HumanInputPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a human input step", partialUpdate.ExistingStepID)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		var stepIndex int
		stepIndex, _, err = updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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

		logger.Info(fmt.Sprintf("✅ Updated human input step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated human input step '%s' in the plan", partialUpdate.ExistingStepID), nil
	}
}

// createUpdateTodoTaskStepExecutor creates an executor function for update_todo_task_step tool
func createUpdateTodoTaskStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PartialPlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("failed to marshal step: %w", err)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf("failed to parse step: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the todo task step
		var existingStep PlanStepInterface
		for _, step := range plan.Steps {
			if step.GetID() == partialUpdate.ExistingStepID {
				existingStep = step
				break
			}
		}
		if existingStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs)
		}

		// Validate it's a todo task step before updating
		todoTaskStep, ok := existingStep.(*TodoTaskPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a todo task step", partialUpdate.ExistingStepID)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		var stepIndex int
		stepIndex, _, err = updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Get the updated step from the plan
		updatedStep := plan.Steps[stepIndex]
		updatedTodoTaskStep, ok := updatedStep.(*TodoTaskPlanStep)
		if !ok {
			return "", fmt.Errorf("updated step is not a todo task step")
		}

		// Validate the updated step has all required fields
		if err := validateTodoTaskStepFieldsTyped(updatedTodoTaskStep); err != nil {
			return "", fmt.Errorf("validation failed after update: %w", err)
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", partialUpdate.ExistingStepID))
			}
		}

		_ = todoTaskStep // Suppress unused variable warning
		logger.Info(fmt.Sprintf("✅ Updated todo task step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated todo task step '%s' in the plan", partialUpdate.ExistingStepID), nil
	}
}

// createAddRoutingStepExecutor creates an executor function for add_routing_step tool
func createAddRoutingStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "routing", unlockLearningsFunc)
}

// validateRoutingStepFieldsTyped validates that a RoutingPlanStep has all required fields
func validateRoutingStepFieldsTyped(step *RoutingPlanStep) error {
	if step.ID == "" {
		return fmt.Errorf("routing step (title: %q) is missing required ID field", step.Title)
	}
	if step.RoutingQuestion == "" {
		return fmt.Errorf("routing step (title: %q, ID: %s) is missing required routing_question field", step.Title, step.ID)
	}
	if len(step.Routes) < 2 {
		return fmt.Errorf("routing step (title: %q, ID: %s) must have at least 2 routes, got %d", step.Title, step.ID, len(step.Routes))
	}
	// Check for duplicate route IDs
	routeIDs := make(map[string]bool)
	for _, route := range step.Routes {
		if route.RouteID == "" {
			return fmt.Errorf("routing step (title: %q, ID: %s) has a route with empty route_id", step.Title, step.ID)
		}
		if route.NextStepID == "" {
			return fmt.Errorf("routing step (title: %q, ID: %s) route %q is missing required next_step_id", step.Title, step.ID, route.RouteID)
		}
		if routeIDs[route.RouteID] {
			return fmt.Errorf("routing step (title: %q, ID: %s) has duplicate route_id %q", step.Title, step.ID, route.RouteID)
		}
		routeIDs[route.RouteID] = true
	}
	// If description is set, success_criteria must also be set (execute-then-route mode)
	if step.Description != "" && step.SuccessCriteria == "" {
		return fmt.Errorf("routing step (title: %q, ID: %s) has description but is missing required success_criteria (execute-then-route mode requires both)", step.Title, step.ID)
	}
	// Validate default_route_id if set
	if step.DefaultRouteID != "" {
		if !routeIDs[step.DefaultRouteID] {
			return fmt.Errorf("routing step (title: %q, ID: %s) has default_route_id %q that doesn't match any route_id", step.Title, step.ID, step.DefaultRouteID)
		}
	}
	return nil
}

// createUpdateRoutingStepExecutor creates an executor function for update_routing_step tool
func createUpdateRoutingStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("failed to marshal step: %w", err)
		}

		var partialUpdate PartialPlanStep
		if err := json.Unmarshal(stepJSON, &partialUpdate); err != nil {
			return "", fmt.Errorf("failed to parse step: %w", err)
		}

		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		var existingStep PlanStepInterface
		for _, step := range plan.Steps {
			if step.GetID() == partialUpdate.ExistingStepID {
				existingStep = step
				break
			}
		}
		if existingStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs)
		}

		if _, ok := existingStep.(*RoutingPlanStep); !ok {
			return "", fmt.Errorf("step with ID '%s' is not a routing step", partialUpdate.ExistingStepID)
		}

		fieldChanges := make([]PlanFieldChange, 0)
		stepIndex, _, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Validate the updated step
		updatedStep := plan.Steps[stepIndex]
		updatedRoutingStep, ok := updatedStep.(*RoutingPlanStep)
		if !ok {
			return "", fmt.Errorf("updated step is not a routing step")
		}

		if err := validateRoutingStepFieldsTyped(updatedRoutingStep); err != nil {
			return "", fmt.Errorf("validation failed after update: %w", err)
		}

		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			}
		}

		logger.Info(fmt.Sprintf("✅ Updated routing step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated routing step '%s' in the plan", partialUpdate.ExistingStepID), nil
	}
}

// createAddRegularStepExecutor creates an executor function for add_regular_step tool
func createAddRegularStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "regular", unlockLearningsFunc)
}

// createAddDecisionStepExecutor creates an executor function for add_decision_step tool
func createAddDecisionStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "decision", unlockLearningsFunc)
}

// createAddHumanInputStepExecutor creates an executor function for add_human_input_step tool
func createAddHumanInputStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "human_input", unlockLearningsFunc)
}

// createAddTodoTaskStepExecutor creates an executor function for add_todo_task_step tool
func createAddTodoTaskStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "todo_task", unlockLearningsFunc)
}

// validateDecisionStepFieldsTyped validates that a DecisionPlanStep has all required fields
// Returns an error message suitable for returning as a tool response if validation fails
// DecisionPlanStep is now flattened - fields are directly on the step
func validateDecisionStepFieldsTyped(step *DecisionPlanStep) error {
	if step.ID == "" {
		return fmt.Errorf("step (title: %q) is missing required ID field. Please provide an ID for the decision step", step.Title)
	}
	if step.Description == "" {
		return fmt.Errorf("step (title: %q, ID: %s) is missing required description field. Please provide a description for the decision step", step.Title, step.ID)
	}
	if step.SuccessCriteria == "" {
		return fmt.Errorf("step (title: %q, ID: %s) is missing required success_criteria field. Please provide success_criteria for the decision step", step.Title, step.ID)
	}
	if step.DecisionEvaluationQuestion == "" {
		return fmt.Errorf("step (title: %q, ID: %s) is missing required decision_evaluation_question field. Please provide a question to evaluate the decision step's execution output", step.Title, step.ID)
	}
	if step.IfTrueNextStepID == "" {
		return fmt.Errorf("step (title: %q, ID: %s) is missing required if_true_next_step_id field. Please provide the ID of the step to route to after evaluation is true", step.Title, step.ID)
	}
	if step.IfFalseNextStepID == "" {
		return fmt.Errorf("step (title: %q, ID: %s) is missing required if_false_next_step_id field. Please provide the ID of the step to route to after evaluation is false", step.Title, step.ID)
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

// validateTodoTaskStepFieldsTyped validates that a TodoTaskPlanStep has all required fields
// Returns an error message suitable for returning as a tool response if validation fails
func validateTodoTaskStepFieldsTyped(step *TodoTaskPlanStep) error {
	if step.TodoTaskStep == nil {
		return fmt.Errorf("step (title: %q, ID: %s) has todo_task step type but is missing required todo_task_step field. Please provide the todo_task_step object with all required fields (id, title, description, success_criteria, has_loop, context_output)", step.Title, step.ID)
	}
	if step.TodoTaskStep.GetID() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has todo_task_step with missing required ID field. Please provide an ID for the todo_task_step", step.Title, step.ID)
	}
	if step.TodoTaskStep.GetDescription() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has todo_task_step with missing required description field. Please provide a description for the todo_task_step", step.Title, step.ID)
	}
	if step.TodoTaskStep.GetSuccessCriteria() == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has todo_task_step with missing required success_criteria field. Please provide success_criteria for the todo_task_step", step.Title, step.ID)
	}
	if step.NextStepID == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has todo_task step type but is missing required next_step_id field. Please provide the ID of the step to connect to after all todos are complete, or 'end' to terminate the workflow", step.Title, step.ID)
	}
	// Predefined routes are optional (enable_generic_agent can be used alone)
	// If predefined routes exist, validate them
	for i, route := range step.PredefinedRoutes {
		if route.RouteID == "" {
			return fmt.Errorf("step (title: %q, ID: %s) has predefined_route[%d] with missing required route_id field", step.Title, step.ID, i)
		}
		if route.RouteName == "" {
			return fmt.Errorf("step (title: %q, ID: %s) has predefined_route[%d] (route_id: %s) with missing required route_name field", step.Title, step.ID, i, route.RouteID)
		}
		if route.SubAgentStep == nil {
			return fmt.Errorf("step (title: %q, ID: %s) has predefined_route[%d] (route_id: %s) with missing required sub_agent_step field", step.Title, step.ID, i, route.RouteID)
		}
	}
	if err := validateTodoTaskNestingDepth(step, 0); err != nil {
		return err
	}
	return nil
}

func validateTodoTaskNestingDepth(step PlanStepInterface, todoRouteDepth int) error {
	switch s := step.(type) {
	case *TodoTaskPlanStep:
		if todoRouteDepth > 1 {
			return fmt.Errorf(
				"todo_task step %q (ID: %s) exceeds the supported nesting depth. Only one nested todo_task layer is allowed under a todo task route",
				s.GetTitle(),
				s.GetID(),
			)
		}
		if s.TodoTaskStep != nil {
			if err := validateTodoTaskNestingDepth(s.TodoTaskStep, todoRouteDepth); err != nil {
				return err
			}
		}
		for i, route := range s.PredefinedRoutes {
			if route.SubAgentStep == nil {
				continue
			}
			if err := validateTodoTaskNestingDepth(route.SubAgentStep, todoRouteDepth+1); err != nil {
				return fmt.Errorf("predefined_route[%d] (route_id: %s): %w", i, route.RouteID, err)
			}
		}
	case *ConditionalPlanStep:
		for i, nested := range s.IfTrueSteps {
			if err := validateTodoTaskNestingDepth(nested, todoRouteDepth); err != nil {
				return fmt.Errorf("conditional if_true_steps[%d]: %w", i, err)
			}
		}
		for i, nested := range s.IfFalseSteps {
			if err := validateTodoTaskNestingDepth(nested, todoRouteDepth); err != nil {
				return fmt.Errorf("conditional if_false_steps[%d]: %w", i, err)
			}
		}
	case *OrchestrationPlanStep:
		if s.OrchestrationStep != nil {
			if err := validateTodoTaskNestingDepth(s.OrchestrationStep, todoRouteDepth); err != nil {
				return err
			}
		}
		for i, route := range s.OrchestrationRoutes {
			if route.SubAgentStep == nil {
				continue
			}
			if err := validateTodoTaskNestingDepth(route.SubAgentStep, todoRouteDepth); err != nil {
				return fmt.Errorf("orchestration_route[%d] (route_id: %s): %w", i, route.RouteID, err)
			}
		}
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
			return "", fmt.Errorf("failed to parse step: %w", err)
		}

		// Validate step has ID
		if typedStep.GetID() == "" {
			return "", fmt.Errorf("step is missing required ID field. Step title: %q", typedStep.GetTitle())
		}

		// Validation schema is LLM-generated only - no code-based auto-generation

		// Validate regex patterns in validation schema before proceeding
		if validationSchema := typedStep.GetValidationSchema(); validationSchema != nil {
			if err := validateRegexPatternsInSchema(validationSchema); err != nil {
				return "", fmt.Errorf("invalid regex patterns in validation schema: %w", err)
			}

			// Validate JSONPath syntax
			if err := validateJSONPathSyntax(validationSchema); err != nil {
				return "", fmt.Errorf("invalid JSONPath syntax in validation schema: %w", err)
			}

			// Validate array_length consistency checks
			if err := validateArrayLengthConsistencyChecks(validationSchema); err != nil {
				return "", fmt.Errorf("invalid array_length consistency checks in validation schema: %w", err)
			}
		}

		// Validate step type-specific required fields BEFORE writing to plan
		// This allows the agent to correct errors immediately via tool response
		switch stepType {
		case "decision":
			if decisionStep, ok := typedStep.(*DecisionPlanStep); ok {
				if err := validateDecisionStepFieldsTyped(decisionStep); err != nil {
					return "", fmt.Errorf("validation failed: %w", err)
				}
			}
		case "orchestration":
			if orchestrationStep, ok := typedStep.(*OrchestrationPlanStep); ok {
				if err := validateOrchestrationStepFieldsTyped(orchestrationStep); err != nil {
					return "", fmt.Errorf("validation failed: %w", err)
				}
			}
		case "todo_task":
			if todoTaskStep, ok := typedStep.(*TodoTaskPlanStep); ok {
				if err := validateTodoTaskStepFieldsTyped(todoTaskStep); err != nil {
					return "", fmt.Errorf("validation failed: %w", err)
				}
			}
		case "routing":
			if routingStep, ok := typedStep.(*RoutingPlanStep); ok {
				if err := validateRoutingStepFieldsTyped(routingStep); err != nil {
					return "", fmt.Errorf("validation failed: %w", err)
				}
			}
		}

		// Read current plan
		oldPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Check if this is an orphan step
		isOrphan := false
		if orphanVal, ok := args["is_orphan"].(bool); ok {
			isOrphan = orphanVal
		}

		var newPlan *PlanningResponse
		if isOrphan {
			// Orphan step: append to OrphanSteps array (no insertion point needed)
			newOrphanSteps := make([]PlanStepInterface, len(oldPlan.OrphanSteps)+1)
			copy(newOrphanSteps, oldPlan.OrphanSteps)
			newOrphanSteps[len(oldPlan.OrphanSteps)] = typedStep
			newPlan = &PlanningResponse{Steps: oldPlan.Steps, OrphanSteps: newOrphanSteps}
		} else {
			// Normal step: find insertion point and insert into Steps array
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
			} else {
				// Find the step to insert after
				afterIndex, found = idToIndex[insertAfterStepID]
				if !found {
					availableIDs := make([]string, 0, len(oldPlan.Steps))
					for _, s := range oldPlan.Steps {
						availableIDs = append(availableIDs, s.GetID())
					}
					return "", fmt.Errorf("step ID '%s' not found in existing plan (cannot insert after it). Available step IDs: %v", insertAfterStepID, availableIDs)
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
					newPlanSteps = append(newPlanSteps, typedStep)
				}
			}

			newPlan = &PlanningResponse{Steps: newPlanSteps, OrphanSteps: oldPlan.OrphanSteps}
		}

		// Validate all steps including the new one
		if err := validatePlanStepIDs(newPlan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed before writing: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, newPlan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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

// registerPlanModificationTools registers all plan modification tools (plan update tools only)
// Note: human_feedback is NOT registered here because it's already included in WorkspaceTools
// This shared function is used by planning agent, code exec debugging agent, etc.
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
		return fmt.Errorf("failed to parse update regular step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_regular_step",
		"Update a regular step in the plan. Provide existing_step_id (required) to identify which step to update, and only include the fields you want to change (title, description, success_criteria, context fields, loop fields). The plan.json file is updated immediately when this tool is called.",
		regularUpdateParams,
		createUpdateRegularStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_regular_step tool: %w", err)
	}

	// NOTE: update_conditional_step tool removed (deprecated in favor of decision/routing).

	decisionUpdateSchema := getUpdateDecisionStepSchema()
	decisionUpdateParams, err := parseSchemaForToolParameters(decisionUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update decision step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_decision_step",
		"Update a decision step in the plan. Provide existing_step_id (required) to identify which decision step to update, and only include the fields you want to change (decision_step, decision_evaluation_question, if_true_next_step_id, if_false_next_step_id). The plan.json file is updated immediately when this tool is called.",
		decisionUpdateParams,
		createUpdateDecisionStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_decision_step tool: %w", err)
	}

	// NOTE: update_orchestration_step tool removed (deprecated in favor of todo_task).

	humanInputUpdateSchema := getUpdateHumanInputStepSchema()
	humanInputUpdateParams, err := parseSchemaForToolParameters(humanInputUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update human input step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_human_input_step",
		"Update a human input step in the plan. Provide existing_step_id (required) to identify which human input step to update, and only include the fields you want to change (question, response_type, options, variable_name, context_output, next_step_id, if_yes_next_step_id/if_no_next_step_id, option_routes). The plan.json file is updated immediately when this tool is called.",
		humanInputUpdateParams,
		createUpdateHumanInputStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_human_input_step tool: %w", err)
	}

	deleteSchema := getDeletePlanStepsSchema()
	deleteParams, err := parseSchemaForToolParameters(deleteSchema)
	if err != nil {
		return fmt.Errorf("failed to parse delete schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_plan_steps",
		"Delete steps from the plan by providing their IDs. Use the step's id field from the plan. The plan.json file is updated immediately when this tool is called.",
		deleteParams,
		createDeletePlanStepsExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register delete_plan_steps tool: %w", err)
	}

	// Register type-specific step addition tools
	regularSchema := getAddRegularStepSchema()
	regularParams, err := parseSchemaForToolParameters(regularSchema)
	if err != nil {
		return fmt.Errorf("failed to parse regular step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_regular_step",
		"Add a regular execution step to the plan. Use this for standard steps that execute once and produce output. Provide all required fields: id, title, description, success_criteria, context_output, has_loop, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		regularParams,
		createAddRegularStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_regular_step tool: %w", err)
	}

	// NOTE: add_conditional_step tool removed (deprecated in favor of decision/routing).
	// Schema, executor, and execution code kept for backward compatibility with existing workflows.

	decisionSchema := getAddDecisionStepSchema()
	decisionParams, err := parseSchemaForToolParameters(decisionSchema)
	if err != nil {
		return fmt.Errorf("failed to parse decision step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_decision_step",
		"Add a decision step to the plan. Use this when you need to execute a step first, evaluate its output, and route based on the result. Decision steps EXECUTE a single step, then evaluate the output to determine routing. Provide: id, title, decision_step (the step to execute), decision_evaluation_question, if_true_next_step_id, if_false_next_step_id, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		decisionParams,
		createAddDecisionStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_decision_step tool: %w", err)
	}

	// NOTE: add_orchestration_step tool removed (deprecated in favor of todo_task).
	// Schema, executor, and execution code kept for backward compatibility with existing workflows.

	// NOTE: add_loop_step tool removed (deprecated — regular steps with has_loop cover this).
	// Schema, executor, and execution code kept for backward compatibility with existing workflows.

	humanInputSchema := getAddHumanInputStepSchema()
	humanInputParams, err := parseSchemaForToolParameters(humanInputSchema)
	if err != nil {
		return fmt.Errorf("failed to parse human input step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_human_input_step",
		"Add a human input step to the plan. Use this when you need to ask a question to a human and block execution until they respond. This step has no LLM, no execution, no validation, and no learning - it simply asks a question and waits for human input. The response is saved to a JSON file and passed to the next step. Provide: id, title, question (required), response_type (text/yesno/multiple_choice), options (for multiple_choice), variable_name (optional), context_output (optional, defaults to step-{index}.json), next_step_id (required), if_yes_next_step_id/if_no_next_step_id (for yesno), option_routes (for multiple_choice), insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		humanInputParams,
		createAddHumanInputStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_human_input_step tool: %w", err)
	}

	todoTaskSchema := getAddTodoTaskStepSchema()
	todoTaskParams, err := parseSchemaForToolParameters(todoTaskSchema)
	if err != nil {
		return fmt.Errorf("failed to parse todo task step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_todo_task_step",
		"Add a todo task orchestration step to the plan. Use this when you need to manage a dynamic todo list with trackable tasks. The main orchestrator creates/assigns tasks, then delegates to predefined sub-agents (with learning and prevalidation) or a generic agent (workspace tools only, no learning). Predefined routes have MCP tool access and accumulate learnings. The generic agent is for simple, ad-hoc tasks. Provide: id, title, todo_task_step (main orchestrator metadata), predefined_routes (optional, specialized sub-agents), enable_generic_agent (optional, default true), next_step_id, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		todoTaskParams,
		createAddTodoTaskStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_todo_task_step tool: %w", err)
	}

	routingSchema := getAddRoutingStepSchema()
	routingParams, err := parseSchemaForToolParameters(routingSchema)
	if err != nil {
		return fmt.Errorf("failed to parse routing step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_routing_step",
		"Add a routing step to the plan. Use this for N-way LLM-based routing where you need to evaluate a question and route to one of multiple possible next steps. Two modes: (1) Execute-then-route: provide description/success_criteria to execute a step first, then route based on output; (2) Pure routing: omit description to evaluate prior context only. Provide: id, title, routing_question, routes (min 2 with route_id/route_name/condition/next_step_id), context_dependencies, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		routingParams,
		createAddRoutingStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_routing_step tool: %w", err)
	}

	routingUpdateSchema := getUpdateRoutingStepSchema()
	routingUpdateParams, err := parseSchemaForToolParameters(routingUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update routing step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_routing_step",
		"Update a routing step in the plan. Provide existing_step_id (required) to identify which routing step to update, and only include the fields you want to change (title, description, success_criteria, routing_question, routes, default_route_id, context_dependencies, context_output). The plan.json file is updated immediately when this tool is called.",
		routingUpdateParams,
		createUpdateRoutingStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_routing_step tool: %w", err)
	}

	// NOTE: conditional branch tools removed (deprecated in favor of decision/routing).
	// Tools removed: convert_step_to_conditional, add_branch_steps, update_branch_steps,
	// delete_branch_steps, convert_conditional_to_regular.
	// Schema, executor, and execution code kept for backward compatibility with existing workflows.

	// NOTE: add/update/delete_orchestration_route tools removed (deprecated in favor of todo_task).

	// Register todo task step update tool
	todoTaskUpdateSchema := getUpdateTodoTaskStepSchema()
	todoTaskUpdateParams, err := parseSchemaForToolParameters(todoTaskUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update_todo_task_step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_todo_task_step",
		"Update an Orchestrator step (todo_task type) in the plan. Provide existing_step_id (required) to identify which step to update, and only include the fields you want to change (title, todo_task_step, predefined_routes, enable_generic_agent, next_step_id). The plan.json file is updated immediately when this tool is called.",
		todoTaskUpdateParams,
		createUpdateTodoTaskStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_todo_task_step tool: %w", err)
	}

	// Register todo task route management tools
	addTodoTaskRouteSchema := getAddTodoTaskRouteSchema()
	addTodoTaskRouteParams, err := parseSchemaForToolParameters(addTodoTaskRouteSchema)
	if err != nil {
		return fmt.Errorf("failed to parse add_todo_task_route schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_todo_task_route",
		"Add a new predefined route (sub-agent) to an Orchestrator step (todo_task type). Provide parent_step_id and new_route with all required fields (route_id, route_name, condition, sub_agent_step). The plan.json file is updated immediately when this tool is called.",
		addTodoTaskRouteParams,
		createAddTodoTaskRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_todo_task_route tool: %w", err)
	}

	updateTodoTaskRouteSchema := getUpdateTodoTaskRouteSchema()
	updateTodoTaskRouteParams, err := parseSchemaForToolParameters(updateTodoTaskRouteSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update_todo_task_route schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_todo_task_route",
		"Update an existing predefined route (sub-agent) within an Orchestrator step (todo_task type). Provide parent_step_id, existing_route_id, and only include the fields you want to change (route_name, condition, sub_agent_step, context_to_pass). The plan.json file is updated immediately when this tool is called.",
		updateTodoTaskRouteParams,
		createUpdateTodoTaskRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_todo_task_route tool: %w", err)
	}

	deleteTodoTaskRouteSchema := getDeleteTodoTaskRouteSchema()
	deleteTodoTaskRouteParams, err := parseSchemaForToolParameters(deleteTodoTaskRouteSchema)
	if err != nil {
		return fmt.Errorf("failed to parse delete_todo_task_route schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_todo_task_route",
		"Delete a predefined route (sub-agent) from an Orchestrator step (todo_task type). Provide parent_step_id and deleted_route_id. Unlike routing steps, Orchestrator steps can have 0 predefined routes if enable_generic_agent is true. The plan.json file is updated immediately when this tool is called.",
		deleteTodoTaskRouteParams,
		createDeleteTodoTaskRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register delete_todo_task_route tool: %w", err)
	}

	// Register validation schema and success criteria update tools
	updateValidationSchemaSchema := getUpdateValidationSchemaSchema()
	updateValidationSchemaParams, err := parseSchemaForToolParameters(updateValidationSchemaSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update_validation_schema schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_validation_schema",
		"Update the validation schema for an existing step in the plan. Provide existing_step_id (required) and validation_schema (required). The validation schema enables fast code-based pre-validation before LLM validation. The plan.json file is updated immediately when this tool is called.",
		updateValidationSchemaParams,
		createUpdateValidationSchemaExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_validation_schema tool: %w", err)
	}

	updateSuccessCriteriaSchema := getUpdateSuccessCriteriaSchema()
	updateSuccessCriteriaParams, err := parseSchemaForToolParameters(updateSuccessCriteriaSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update_success_criteria schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_success_criteria",
		"Update the success criteria for an existing step in the plan. Provide existing_step_id (required) and success_criteria (required). Success criteria should focus on EXECUTION-BASED validation - what work was actually done, not just file structure. The plan.json file is updated immediately when this tool is called.",
		updateSuccessCriteriaParams,
		createUpdateSuccessCriteriaExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_success_criteria tool: %w", err)
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("✅ Registered all plan modification tools for %s", agentName))
	}

	return nil
}

// createAddTodoTaskRouteExecutor creates an executor function for add_todo_task_route tool
func createAddTodoTaskRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Accept parent_step_id or legacy alias step_id
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			parentStepID, ok = args["step_id"].(string)
		}
		if !ok || parentStepID == "" {
			return "", fmt.Errorf("invalid or missing parent_step_id")
		}

		// Accept new_route or legacy alias predefined_route; handle JSON-string form
		var newRouteRaw map[string]interface{}
		if v, ok2 := args["new_route"]; ok2 {
			switch val := v.(type) {
			case map[string]interface{}:
				newRouteRaw = val
			case string:
				if err := json.Unmarshal([]byte(val), &newRouteRaw); err != nil {
					return "", fmt.Errorf("failed to parse new_route JSON string: %w", err)
				}
			}
		}
		if newRouteRaw == nil {
			if v, ok2 := args["predefined_route"]; ok2 {
				switch val := v.(type) {
				case map[string]interface{}:
					newRouteRaw = val
				case string:
					if err := json.Unmarshal([]byte(val), &newRouteRaw); err != nil {
						return "", fmt.Errorf("failed to parse predefined_route JSON string: %w", err)
					}
				}
			}
		}
		if newRouteRaw == nil {
			return "", fmt.Errorf("invalid new_route argument")
		}

		// Convert to JSON and unmarshal to PlanOrchestrationRoute
		newRouteJSON, err := json.Marshal(newRouteRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal new_route: %w", err)
		}
		var newRoute PlanOrchestrationRoute
		if err := json.Unmarshal(newRouteJSON, &newRoute); err != nil {
			return "", fmt.Errorf("failed to parse new_route: %w", err)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the parent todo task step by ID
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
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		todoTaskStep, ok := parentStep.(*TodoTaskPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a todo task step", parentStepID)
		}

		// Validate that the new route has a route_id
		if newRoute.RouteID == "" {
			return "", fmt.Errorf("new route is missing required route_id field")
		}

		// Check if route_id already exists
		for _, existingRoute := range todoTaskStep.PredefinedRoutes {
			if existingRoute.RouteID == newRoute.RouteID {
				return "", fmt.Errorf("route with route_id '%s' already exists in todo task step '%s'", newRoute.RouteID, parentStepID)
			}
		}

		// Validate that sub_agent_step has required fields
		if newRoute.SubAgentStep != nil && newRoute.SubAgentStep.GetID() == "" {
			return "", fmt.Errorf("sub_agent_step is missing required ID field")
		}

		// Add new route
		todoTaskStep.PredefinedRoutes = append(todoTaskStep.PredefinedRoutes, newRoute)
		if err := validateTodoTaskStepFieldsTyped(todoTaskStep); err != nil {
			return "", fmt.Errorf("validation failed after adding route: %w", err)
		}
		plan.Steps[parentStepIndex] = todoTaskStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Info(fmt.Sprintf("✅ Added route '%s' (ID: %s) to todo task step '%s'", newRoute.RouteName, newRoute.RouteID, todoTaskStep.Title))
		return fmt.Sprintf("Successfully added route '%s' (ID: %s) to todo task step '%s'", newRoute.RouteName, newRoute.RouteID, todoTaskStep.Title), nil
	}
}

// createUpdateTodoTaskRouteExecutor creates an executor function for update_todo_task_route tool
func createUpdateTodoTaskRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Accept parent_step_id or legacy alias step_id
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			parentStepID, ok = args["step_id"].(string)
		}
		if !ok || parentStepID == "" {
			return "", fmt.Errorf("invalid or missing parent_step_id")
		}

		existingRouteID, ok := args["existing_route_id"].(string)
		if !ok || existingRouteID == "" {
			return "", fmt.Errorf("invalid or missing existing_route_id")
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the parent todo task step by ID
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
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		todoTaskStep, ok := parentStep.(*TodoTaskPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a todo task step", parentStepID)
		}

		// Find the route to update
		var routeToUpdate *PlanOrchestrationRoute
		for i := range todoTaskStep.PredefinedRoutes {
			if todoTaskStep.PredefinedRoutes[i].RouteID == existingRouteID {
				routeToUpdate = &todoTaskStep.PredefinedRoutes[i]
				break
			}
		}
		if routeToUpdate == nil {
			availableRouteIDs := make([]string, 0, len(todoTaskStep.PredefinedRoutes))
			for _, route := range todoTaskStep.PredefinedRoutes {
				availableRouteIDs = append(availableRouteIDs, route.RouteID)
			}
			return "", fmt.Errorf("route with route_id '%s' not found in todo task step '%s'. Available route IDs: %v", existingRouteID, parentStepID, availableRouteIDs)
		}

		// Update fields if provided
		if routeName, ok := args["route_name"].(string); ok && routeName != "" {
			routeToUpdate.RouteName = routeName
		}

		if condition, ok := args["condition"].(string); ok && condition != "" {
			routeToUpdate.Condition = condition
		}

		if contextToPass, ok := args["context_to_pass"].(string); ok {
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
				return "", fmt.Errorf("failed to parse sub_agent_step: %w", err)
			}
			routeToUpdate.SubAgentStep = updatedSubAgentStep
		}

		if err := validateTodoTaskStepFieldsTyped(todoTaskStep); err != nil {
			return "", fmt.Errorf("validation failed after route update: %w", err)
		}

		// Update the todo task step in the plan
		plan.Steps[parentStepIndex] = todoTaskStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Info(fmt.Sprintf("✅ Updated route '%s' (ID: %s) in todo task step '%s'", routeToUpdate.RouteName, existingRouteID, todoTaskStep.Title))
		return fmt.Sprintf("Successfully updated route '%s' (ID: %s) in todo task step '%s'", routeToUpdate.RouteName, existingRouteID, todoTaskStep.Title), nil
	}
}

// createDeleteTodoTaskRouteExecutor creates an executor function for delete_todo_task_route tool
func createDeleteTodoTaskRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Accept parent_step_id or legacy alias step_id
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			parentStepID, ok = args["step_id"].(string)
		}
		if !ok || parentStepID == "" {
			return "", fmt.Errorf("invalid or missing parent_step_id")
		}

		deletedRouteID, ok := args["deleted_route_id"].(string)
		if !ok || deletedRouteID == "" {
			return "", fmt.Errorf("invalid or missing deleted_route_id")
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Find the parent todo task step by ID
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
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		todoTaskStep, ok := parentStep.(*TodoTaskPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a todo task step", parentStepID)
		}

		// Find the route to delete
		var deletedRoute *PlanOrchestrationRoute
		routeIndex := -1
		for i, route := range todoTaskStep.PredefinedRoutes {
			if route.RouteID == deletedRouteID {
				deletedRoute = &todoTaskStep.PredefinedRoutes[i]
				routeIndex = i
				break
			}
		}
		if deletedRoute == nil {
			availableRouteIDs := make([]string, 0, len(todoTaskStep.PredefinedRoutes))
			for _, route := range todoTaskStep.PredefinedRoutes {
				availableRouteIDs = append(availableRouteIDs, route.RouteID)
			}
			return "", fmt.Errorf("route with route_id '%s' not found in todo task step '%s'. Available route IDs: %v", deletedRouteID, parentStepID, availableRouteIDs)
		}

		// Note: Unlike orchestration steps, todo task steps can have 0 predefined routes if enable_generic_agent is true

		// Remove the route
		todoTaskStep.PredefinedRoutes = append(
			todoTaskStep.PredefinedRoutes[:routeIndex],
			todoTaskStep.PredefinedRoutes[routeIndex+1:]...,
		)
		plan.Steps[parentStepIndex] = todoTaskStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logger.Info(fmt.Sprintf("✅ Deleted route '%s' (ID: %s) from todo task step '%s'", deletedRoute.RouteName, deletedRouteID, todoTaskStep.Title))
		return fmt.Sprintf("Successfully deleted route '%s' (ID: %s) from todo task step '%s'", deletedRoute.RouteName, deletedRouteID, todoTaskStep.Title), nil
	}
}

// createUpdateValidationSchemaExecutor creates an executor function for update_validation_schema tool
func createUpdateValidationSchemaExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to extract validation schema
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("failed to marshal step: %w", err)
		}

		var updateData struct {
			ExistingStepID   string            `json:"existing_step_id"`
			ValidationSchema *ValidationSchema `json:"validation_schema"`
		}
		if err := json.Unmarshal(stepJSON, &updateData); err != nil {
			return "", fmt.Errorf("failed to parse update data: %w", err)
		}

		if updateData.ExistingStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("existing_step_id is required"), nil)
		}
		if updateData.ValidationSchema == nil {
			return "", fmt.Errorf(fmt.Sprintf("validation_schema is required"), nil)
		}

		// Validate regex patterns before updating the plan
		if err := validateRegexPatternsInSchema(updateData.ValidationSchema); err != nil {
			return "", fmt.Errorf("invalid regex patterns in validation schema: %w", err)
		}

		// Validate JSONPath syntax
		if err := validateJSONPathSyntax(updateData.ValidationSchema); err != nil {
			return "", fmt.Errorf("invalid JSONPath syntax in validation schema: %w", err)
		}

		// Validate array_length consistency checks
		if err := validateArrayLengthConsistencyChecks(updateData.ValidationSchema); err != nil {
			return "", fmt.Errorf("invalid array_length consistency checks in validation schema: %w", err)
		}

		// Create PartialPlanStep with only validation schema
		partialUpdate := PartialPlanStep{
			ExistingStepID:   updateData.ExistingStepID,
			ValidationSchema: updateData.ValidationSchema,
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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
			return "", fmt.Errorf("failed to marshal step: %w", err)
		}

		var updateData struct {
			ExistingStepID  string `json:"existing_step_id"`
			SuccessCriteria string `json:"success_criteria"`
		}
		if err := json.Unmarshal(stepJSON, &updateData); err != nil {
			return "", fmt.Errorf("failed to parse update data: %w", err)
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
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
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
