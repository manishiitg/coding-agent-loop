package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/PaesslerAG/jsonpath"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/r3labs/diff/v3"
)

// Pre-parsed templates for planning agent - panics at startup if invalid
var planningUpdateSystemTemplate = MustRegisterTemplate("planningUpdateSystem", `## 🤖 ROLE: Planning Agent
**Task**: Design or refine structured execution plans ('plan.json').
**Context**: Workspace: {{.WorkspacePath}} | Date: {{.CurrentDate}} {{.CurrentTime}}

## ⚠️ MANDATORY PROTOCOL
1. **Approval First**: ALWAYS use 'human_feedback' BEFORE calling any plan modification tools.
2. **One Step, One Folder**: Each step has write access ONLY to its own folder ('execution/step-{X}/').
3. **Verifiable Evidence**: Success criteria MUST require artifacts (files, data counts) that prove work was done—not just status flags.
4. **Stable IDs**: Keep existing 'id' values stable. Only generate new IDs for truly new steps.
5. **Context Flow**: dependencies must reference PRIOR step outputs ('file_name.json', never paths).
6. **No Spawning**: Never replace {{"{{VARIABLE_NAME}}"}} placeholders with values.

---

## 🏗️ STEP DESIGN
- **Regular**: Standard task. 'context_output' is the result file.
- **Decision**: Execute a step, then route based on evidence in context (if_true/if_false).
- **Conditional**: Inspection-only branch (no execution).
- **Orchestration**: Iterative routing between sub-agents until success.
- **Loop**: Repeat until criteria met (polled progress).

{{if eq .UseKnowledgebase "true"}}### 📁 Persistent Storage (Knowledgebase)
- **knowledgebase/**: Persistent folder at workspace root. Never deleted across runs.
- **How to Use**: Use for global templates, reference data, or configurations shared across ALL runs. Design steps to read from here for persistent context. Use 'knowledgebase/file.ext' in descriptions.
{{end}}

### 📄 JSON FILE STRUCTURE BEST PRACTICES
**CRITICAL**: Keep JSON context output files SMALL (< 100KB). Large JSON files cause parsing failures and performance issues.

**DO**:
- Store structured data in JSON: counts, IDs, status, file references, brief summaries (< 1KB per field)
- For large text content (> 1KB), create a separate markdown file and reference it: {"details_file": "step_1_details.md"}
- Example good structure: {"status": "completed", "count": 5, "files": ["file1.md"], "summary": "Brief summary", "details_file": "step_1_details.md"}

**DON'T**:
- Put large text content directly in JSON fields (descriptions, logs, content > 1KB)
- Create JSON files > 100KB - they will fail to load during pre-validation

### 🔍 Validation Schemas
Every step MUST have a 'validation_schema' to enable fast code-based pre-validation.
- Target files/fields/types mentioned in success criteria.
- Use Go regex for 'pattern' checks (e.g., "^\\d{4}-\\d{2}-\\d{2}$").

---

{{if .VariableNames}}
## 🔑 VARIABLES
{{.VariableNames}}
{{end}}

## 📄 CURRENT PLAN
{{.ExistingPlanJSON}}

---

## 📤 OUTPUT RULES
- **Feedback**: 'human_feedback' -> Proposal -> User Approval -> Execute tools.
- **Questions**: Respond conversationally if clarification is needed.
- **Validation**: After any change, verify forward-only context flow and ID stability.

*No placeholders. No duplicate steps. No circular dependencies.*`)

// WorkflowPlanningTemplate holds template variables for human-controlled planning prompts
type WorkflowPlanningTemplate struct {
	Objective     string
	WorkspacePath string
}

// WorkflowPlanningAgent creates a fast, simplified plan from the objective
type WorkflowPlanningAgent struct {
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
	ExecutionLLM                *AgentLLMConfig    `json:"execution_llm,omitempty"`
	ValidationLLM               *AgentLLMConfig    `json:"validation_llm,omitempty"`
	LearningLLM                 *AgentLLMConfig    `json:"learning_llm,omitempty"`
	ConditionalLLM              *AgentLLMConfig    `json:"conditional_llm,omitempty"`                // Step-specific conditional LLM for conditional step evaluation
	ExecutionMaxTurns           *int               `json:"execution_max_turns,omitempty"`            // default: 100
	ValidationMaxTurns          *int               `json:"validation_max_turns,omitempty"`           // default: 100
	LearningMaxTurns            *int               `json:"learning_max_turns,omitempty"`             // default: 100
	OrchestrationMaxIterations  *int               `json:"orchestration_max_iterations,omitempty"`   // default: orchestrator max turns (typically 100)
	DisableValidation           *bool              `json:"disable_validation,omitempty"`             // LLM validation: nil = disabled by default, false = explicitly enabled, true = disabled (pre-validation always runs if schema exists)
	LLMValidationMode           string             `json:"llm_validation_mode,omitempty"`            // "skip" (default), "auto", or "always". Controls LLM validation behavior when pre-validation passes.
	DisableLearning             *bool              `json:"disable_learning,omitempty"`               // disable learning for this step (nil = not set/enabled, true = disabled, false = explicitly enabled)
	LockLearnings               *bool              `json:"lock_learnings,omitempty"`                 // lock learnings - prevents learning agent from running but still uses existing learnings (nil = not set/unlocked, true = locked, false = explicitly unlocked)
	LearningAfterLoopIteration  bool               `json:"learning_after_loop_iteration,omitempty"`  // run learning after each loop iteration
	LearningDetailLevel         string             `json:"learning_detail_level,omitempty"`          // "exact", "general", or "none" (default: "exact")
	SelectedServers             []string           `json:"selected_servers,omitempty"`               // step-level MCP server selection (subset of preset servers)
	SelectedTools               []string           `json:"selected_tools,omitempty"`                 // step-level tool selection (format: "server:tool" or "server:*" for all tools)
	EnabledCustomToolCategories []string           `json:"enabled_custom_tool_categories,omitempty"` // e.g., ["workspace_tools", "human_tools"] - enables all tools in category
	EnabledCustomTools          []string           `json:"enabled_custom_tools,omitempty"`           // e.g., ["read_workspace_file", "human_feedback"] - enables specific tools (overrides categories if both specified)
	EnableContextOffloading     *bool              `json:"enable_context_offloading,omitempty"`      // Enable/disable context offloading (default: true if nil)
	UseCodeExecutionMode        *bool              `json:"use_code_execution_mode,omitempty"`        // Step-level code execution mode override (nil = use preset default, true/false = override)
	UseToolSearchMode           *bool              `json:"use_tool_search_mode,omitempty"`           // Step-level tool search mode override (nil = use preset default, true/false = override)
	PreDiscoveredTools          []string           `json:"pre_discovered_tools,omitempty"`           // Tools always available without searching (overrides preset if specified)
	EnabledSkills               []string           `json:"enabled_skills,omitempty"`                 // Step-level skill selection (skill folder names, overrides preset if specified)
	EnablePrerequisiteDetection *bool              `json:"enable_prerequisite_detection,omitempty"`  // Enable prerequisite failure detection for this step (default: false)
	PrerequisiteRules           []PrerequisiteRule `json:"prerequisite_rules,omitempty"`             // Array of prerequisite rules, each with one step dependency and one description
	KeepLearningFull            *bool              `json:"keep_learning_full,omitempty"`             // Feature flag: If true, include full learning content in system prompt; if false, only file paths in user message (default: false, can be overridden by KEEP_LEARNING_FULL env var)
	DisableTempLLM              *bool              `json:"disable_temp_llm,omitempty"`               // If true, skip tempLLM override and use step config base LLM (step config > preset > orchestrator default)
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
// Prerequisite rules are NOT supported on decision steps.
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
func (d *DecisionPlanStep) GetEnablePrerequisiteDetection() *bool {
	return nil // Not supported on decision steps
}
func (d *DecisionPlanStep) GetPrerequisiteRules() []PrerequisiteRule { return nil } // Not supported on decision steps
func (d *DecisionPlanStep) GetValidationSchema() *ValidationSchema   { return d.ValidationSchema }
func (d *DecisionPlanStep) StepType() StepType                       { return StepTypeDecision }
func (d *DecisionPlanStep) GetCommonFields() CommonStepFields {
	// Return common fields but override prerequisite fields (not supported on decision steps)
	return CommonStepFields{
		ID:                          d.ID,
		Title:                       d.Title,
		Description:                 d.Description,
		SuccessCriteria:             d.SuccessCriteria,
		ContextDependencies:         d.ContextDependencies,
		ContextOutput:               d.ContextOutput,
		EnablePrerequisiteDetection: nil, // Not supported on decision steps
		PrerequisiteRules:           nil, // Not supported on decision steps
		ValidationSchema:            d.ValidationSchema,
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
func (h *HumanInputPlanStep) GetEnablePrerequisiteDetection() *bool {
	return h.EnablePrerequisiteDetection
}
func (h *HumanInputPlanStep) GetPrerequisiteRules() []PrerequisiteRule { return h.PrerequisiteRules }
func (h *HumanInputPlanStep) GetValidationSchema() *ValidationSchema   { return h.ValidationSchema }
func (h *HumanInputPlanStep) StepType() StepType                       { return StepTypeHumanInput }
func (h *HumanInputPlanStep) GetCommonFields() CommonStepFields        { return h.CommonStepFields }

// MarshalJSON ensures the type field is always set when marshaling
func (h *HumanInputPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	h.Type = StepTypeHumanInput
	// Use type alias to avoid infinite recursion
	type Alias HumanInputPlanStep
	return json.Marshal((*Alias)(h))
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
			return fmt.Errorf("step %d is missing required 'type' field (must be: regular, conditional, decision, orchestration, or human_input)", i)
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
		case "human_input":
			var step HumanInputPlanStep
			if err := json.Unmarshal(stepData, &step); err != nil {
				return fmt.Errorf("failed to parse human_input step %d: %w", i, err)
			}
			typedStep = &step
		default:
			return fmt.Errorf("unknown step type %q in step %d (must be: regular, conditional, decision, orchestration, or human_input)", stepWithType.Type, i)
		}

		pr.Steps[i] = typedStep
	}

	return nil
}

// MarshalJSON implements custom marshaling for typed steps
func (pr PlanningResponse) MarshalJSON() ([]byte, error) {
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
	IfTrueNextStepID  string `json:"if_true_next_step_id,omitempty"`  // Optional: Updated if_true_next_step_id
	IfFalseNextStepID string `json:"if_false_next_step_id,omitempty"` // Optional: Updated if_false_next_step_id
	NextStepID        string `json:"next_step_id,omitempty"`          // Optional: Updated next_step_id (for routing steps)
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

	// Use relative path only - ReadWorkspaceFile/WriteWorkspaceFile auto-prepend workspacePath
	changelogPath := filepath.Join("planning", "changelog", changelogSessionFile)

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
		return fmt.Errorf("failed to marshal changelog: %w", err)
	}

	if err := writeFile(ctx, changelogPath, string(data)); err != nil {
		return fmt.Errorf("failed to write changelog file: %w", err)
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
		return fmt.Errorf("failed to write changelog entry: %w", err)
	}

	logger.Info(fmt.Sprintf("📝 Generated changelog entry: %s - %d diff changes across %d step(s)", changeType, len(diffChangelog), len(stepIDsList)))
	return nil
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
				"description": "OPTIONAL: Context file this conditional step wrapper will create. Can be empty string if not needed. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."
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
						"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."},
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"validation_schema": {
							"type": "object",
							"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. Generate by parsing success_criteria. Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath), must_exist: boolean, value_type?: string, min_length?: number, max_length?: number, pattern?: string (VALID Go regex only - must compile, balanced parentheses, properly escaped), min_value?: number, max_value?: number, consistency_check?: {type: string, compare_with_path: string}}]}]}. CRITICAL FOR array_length CONSISTENCY CHECKS - AVOID COMMON MISTAKE: path MUST point to COUNT/number field (e.g., '$.count', '$.total_expected_count'), compare_with_path MUST point to ARRAY field (e.g., '$.items', '$.downloaded_files'). CORRECT example: If 'count equals items array length', use path='$.count', consistency_check={type='array_length', compare_with_path='$.items'}. CORRECT example: If 'total_expected_count equals downloaded_files array length', use path='$.total_expected_count', consistency_check={type='array_length', compare_with_path='$.downloaded_files'}. INCORRECT (SWAPPED) - DO NOT DO THIS: path='$.items', compare_with_path='$.count' ❌ WRONG! Field name hints: Number fields contain 'count', 'total', 'length', 'size'. Array fields contain 'files', 'items', 'list', 'array', 'entries'. IMPORTANT: Only use pattern field if you can generate a valid Go regex. Invalid patterns will be skipped.",
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
						"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."},
						"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
						"validation_schema": {
							"type": "object",
							"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. Generate by parsing success_criteria. Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath), must_exist: boolean, value_type?: string, min_length?: number, max_length?: number, pattern?: string (VALID Go regex only - must compile, balanced parentheses, properly escaped), min_value?: number, max_value?: number, consistency_check?: {type: string, compare_with_path: string}}]}]}. CRITICAL FOR array_length CONSISTENCY CHECKS - AVOID COMMON MISTAKE: path MUST point to COUNT/number field (e.g., '$.count', '$.total_expected_count'), compare_with_path MUST point to ARRAY field (e.g., '$.items', '$.downloaded_files'). CORRECT example: If 'count equals items array length', use path='$.count', consistency_check={type='array_length', compare_with_path='$.items'}. CORRECT example: If 'total_expected_count equals downloaded_files array length', use path='$.total_expected_count', consistency_check={type='array_length', compare_with_path='$.downloaded_files'}. INCORRECT (SWAPPED) - DO NOT DO THIS: path='$.items', compare_with_path='$.count' ❌ WRONG! Field name hints: Number fields contain 'count', 'total', 'length', 'size'. Array fields contain 'files', 'items', 'list', 'array', 'entries'. IMPORTANT: Only use pattern field if you can generate a valid Go regex. Invalid patterns will be skipped.",
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
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."},
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
								"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."},
								"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop. NOTE: Loop support is currently not implemented in agents. Always set to false."},
								"loop_condition": {"type": "string"},
								"max_iterations": {"type": "integer"},
								"loop_description": {"type": "string"},
								"validation_schema": {
									"type": "object",
									"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. Generate by parsing success_criteria. Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath), must_exist: boolean, value_type?: string, min_length?: number, max_length?: number, pattern?: string (VALID Go regex only - must compile, balanced parentheses, properly escaped), min_value?: number, max_value?: number, consistency_check?: {type: string, compare_with_path: string}}]}]}. IMPORTANT: Only use pattern field if you can generate a valid Go regex. Invalid patterns will be skipped.",
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
				"description": "REQUIRED: What context file this step will create for subsequent steps - e.g., 'step_1_results.json'. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content (descriptions, logs, content > 1KB), store it in a separate markdown file (e.g., 'step_1_details.md') and reference it from JSON (e.g., {\"details_file\": \"step_1_details.md\"}). JSON should contain only structured data: counts, IDs, status, file references, brief summaries."
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
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."},
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
								"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."},
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
			},
			"enable_prerequisite_detection": {
				"type": "boolean",
				"description": "OPTIONAL: Updated enable_prerequisite_detection flag. Only include if you want to change it. Set to true when this step depends on outputs from previous steps that might expire or become invalid. If omitted, the existing value is preserved."
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
							"description": "REQUIRED: Natural language description of when to detect prerequisite failures for this specific step."
						}
					},
					"required": ["depends_on_step", "description"]
				},
				"description": "OPTIONAL: Updated prerequisite rules. Only include if you want to change them. If omitted, the existing prerequisite rules are preserved."
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
							"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."},
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
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create. CRITICAL: Keep JSON files SMALL (< 100KB). For large text content, store it in a separate markdown file and reference it from JSON."},
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

// readPlanFromFile reads plan.json from the workspace using BaseOrchestrator's ReadWorkspaceFile
// NOTE: workspacePath parameter is kept for API compatibility but NOT used in path construction
// ReadWorkspaceFile auto-prepends the workspace path, so we only pass the relative path
func readPlanFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*PlanningResponse, error) {
	// Use relative path only - ReadWorkspaceFile auto-prepends workspacePath
	planPath := filepath.Join("planning", "plan.json")

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
// NOTE: workspacePath parameter is kept for API compatibility but NOT used in path construction
// WriteWorkspaceFile auto-prepends the workspace path, so we only pass the relative path
func writePlanToFile(ctx context.Context, workspacePath string, plan *PlanningResponse, _ func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	// Use relative path only - WriteWorkspaceFile auto-prepends workspacePath
	planPath := filepath.Join("planning", "plan.json")

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
func validateNestingDepth(step PlanStepInterface, currentDepth int) error {
	const maxDepth = 2
	if currentDepth > maxDepth {
		return fmt.Errorf("nesting depth exceeds maximum allowed depth of %d (current: %d)", maxDepth, currentDepth)
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
	case "human_input":
		var step HumanInputPlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse human_input step: %w", err)
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

			// Validate compare_with_path if it exists
			if check.ConsistencyCheck != nil {
				comparePath := strings.TrimSpace(check.ConsistencyCheck.CompareWithPath)
				if comparePath == "" {
					errors = append(errors, fmt.Sprintf("File '%s': Empty compare_with_path is not allowed in consistency check", fileRule.FileName))
				} else {
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
		if partialUpdate.EnablePrerequisiteDetection != nil {
			updated.EnablePrerequisiteDetection = partialUpdate.EnablePrerequisiteDetection
		}
		if partialUpdate.PrerequisiteRules != nil {
			updated.PrerequisiteRules = partialUpdate.PrerequisiteRules
		}
		if partialUpdate.ValidationSchema != nil {
			updated.ValidationSchema = partialUpdate.ValidationSchema
		}
		return &updated

	default:
		// Unknown type - return original
		return existingStep
	}
}

// NewWorkflowPlanningAgent creates a new human-controlled todo planner planning agent
func NewWorkflowPlanningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowPlanningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanningAgentType, // Reuse the same type for now
		eventBridge,
	)

	return &WorkflowPlanningAgent{
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
		return -1, nil, fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs)
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

// createUpdateConditionalStepExecutor creates an executor function for update_conditional_step tool
func createUpdateConditionalStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
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
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs)
		}

		conditionalStep, ok := existingStep.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", partialUpdate.ExistingStepID)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Handle condition_question and condition_context with explicit empty string support
		_, conditionQuestionProvided := args["condition_question"]
		_, conditionContextProvided := args["condition_context"]

		if conditionQuestionProvided {
			oldValue := conditionalStep.ConditionQuestion
			newValue, _ := args["condition_question"].(string)
			if newValue != "" || oldValue != newValue {
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
			_, _, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
			if err != nil {
				return "", err
			}
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

// createUpdateOrchestrationStepExecutor creates an executor function for update_orchestration_step tool
func createUpdateOrchestrationStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
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

		// Find the orchestration step
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

		// Validate it's an orchestration step before updating
		_, ok := existingStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not an orchestration step", partialUpdate.ExistingStepID)
		}

		// Track changes for changelog
		fieldChanges := make([]PlanFieldChange, 0)

		// Log what we're updating for debugging
		if partialUpdate.OrchestrationStep != nil {
			// Get old description for comparison
			if orchestrationStep, ok := existingStep.(*OrchestrationPlanStep); ok && orchestrationStep.OrchestrationStep != nil {
				_ = orchestrationStep.OrchestrationStep.GetDescription()
			}
		}

		// Update the step
		// Note: Changelog is now generated automatically after agent execution completes (see generateChangelogFromPlanDiff)
		var stepIndex int
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

		// Validate the updated step has all required fields for orchestration steps
		// This ensures the agent gets immediate feedback if validation fails
		if err := validateOrchestrationStepFieldsTyped(updatedOrchestrationStep); err != nil {
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
				return "", fmt.Errorf("invalid step ID in deleted_step_ids: %v", id)
			}
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
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				// Build list of available step IDs for better error message
				availableIDs := make([]string, 0, len(oldPlan.Steps))
				for _, step := range oldPlan.Steps {
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

		newPlan := &PlanningResponse{Steps: filteredSteps}

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

// createAddHumanInputStepExecutor creates an executor function for add_human_input_step tool
func createAddHumanInputStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "human_input", unlockLearningsFunc)
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
		}

		// Read current plan
		oldPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
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
				// Insert new step after this one
				newPlanSteps = append(newPlanSteps, typedStep)
			}
		}

		newPlan := &PlanningResponse{Steps: newPlanSteps}

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
		return fmt.Errorf("failed to parse update regular step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_regular_step",
		"Update a regular step in the plan. Provide existing_step_id (required) to identify which step to update, and only include the fields you want to change (title, description, success_criteria, context fields, loop fields, prerequisite fields). The plan.json file is updated immediately when this tool is called.",
		regularUpdateParams,
		createUpdateRegularStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_regular_step tool: %w", err)
	}

	conditionalUpdateSchema := getUpdateConditionalStepSchema()
	conditionalUpdateParams, err := parseSchemaForToolParameters(conditionalUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update conditional step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_conditional_step",
		"Update a conditional step in the plan. Provide existing_step_id (required) to identify which conditional step to update, and only include the fields you want to change (condition_question, condition_context, if_true_steps, if_false_steps, next_step_ids). The plan.json file is updated immediately when this tool is called.",
		conditionalUpdateParams,
		createUpdateConditionalStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_conditional_step tool: %w", err)
	}

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

	orchestrationUpdateSchema := getUpdateOrchestrationStepSchema()
	orchestrationUpdateParams, err := parseSchemaForToolParameters(orchestrationUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update orchestration step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_orchestration_step",
		"Update an orchestration step in the plan. Provide existing_step_id (required) to identify which orchestration step to update, and only include the fields you want to change (orchestration_step, orchestration_routes, next_step_id). The plan.json file is updated immediately when this tool is called.",
		orchestrationUpdateParams,
		createUpdateOrchestrationStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_orchestration_step tool: %w", err)
	}

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

	conditionalSchema := getAddConditionalStepSchema()
	conditionalParams, err := parseSchemaForToolParameters(conditionalSchema)
	if err != nil {
		return fmt.Errorf("failed to parse conditional step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_conditional_step",
		"Add a conditional step to the plan. Use this for if/else logic based on runtime conditions. Conditional steps evaluate a question and execute different branch steps based on the result. They do NOT execute the step itself - only evaluate the condition. Provide: id, title, condition_question, if_true_steps, if_false_steps, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		conditionalParams,
		createAddConditionalStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_conditional_step tool: %w", err)
	}

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

	orchestrationSchema := getAddOrchestrationStepSchema()
	orchestrationParams, err := parseSchemaForToolParameters(orchestrationSchema)
	if err != nil {
		return fmt.Errorf("failed to parse orchestration step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_orchestration_step",
		"Add an orchestration step to the plan. Use this when you need an orchestrator that can choose between multiple sub-agents based on conditions. Orchestration steps EXECUTE a main orchestrator step, analyze the situation, and select one of multiple sub-agents to call based on step description and success criteria. The main orchestrator loops until its success criteria are met. Sub-agents are private to the orchestration step and execute without validation. Provide: id, title, orchestration_step (the main orchestrator step), orchestration_routes (array of routes with conditions and sub-agent steps), next_step_id, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		orchestrationParams,
		createAddOrchestrationStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_orchestration_step tool: %w", err)
	}

	loopSchema := getAddLoopStepSchema()
	loopParams, err := parseSchemaForToolParameters(loopSchema)
	if err != nil {
		return fmt.Errorf("failed to parse loop step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_loop_step",
		"Add a loop step to the plan. Use this for steps that need to repeat until a condition is met (polling, retrying, waiting). Provide: id, title, description, success_criteria, context_output, loop_condition, loop_description, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		loopParams,
		createAddLoopStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_loop_step tool: %w", err)
	}

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

	// Register conditional step tools
	convertToConditionalSchema := getConvertStepToConditionalSchema()
	convertToConditionalParams, err := parseSchemaForToolParameters(convertToConditionalSchema)
	if err != nil {
		return fmt.Errorf("failed to parse convert_step_to_conditional schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"convert_step_to_conditional",
		"Convert a regular step to a conditional step with if/else branches. Provide step_id, condition_question, condition_context (optional), if_true_steps, and if_false_steps. The step will become a conditional decision point that executes one branch based on the condition evaluation.",
		convertToConditionalParams,
		createConvertStepToConditionalExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register convert_step_to_conditional tool: %w", err)
	}

	addBranchStepsSchema := getAddBranchStepsSchema()
	addBranchStepsParams, err := parseSchemaForToolParameters(addBranchStepsSchema)
	if err != nil {
		return fmt.Errorf("failed to parse add_branch_steps schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_branch_steps",
		"Add new steps to a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type ('if_true' or 'if_false'), and new_steps array. The steps will be appended to the specified branch.",
		addBranchStepsParams,
		createAddBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_branch_steps tool: %w", err)
	}

	updateBranchStepsSchema := getUpdateBranchStepsSchema()
	updateBranchStepsParams, err := parseSchemaForToolParameters(updateBranchStepsSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update_branch_steps schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_branch_steps",
		"Update existing steps within a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type, and updated_steps array. For each step, provide existing_step_id (required) and only include fields you want to change.",
		updateBranchStepsParams,
		createUpdateBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_branch_steps tool: %w", err)
	}

	deleteBranchStepsSchema := getDeleteBranchStepsSchema()
	deleteBranchStepsParams, err := parseSchemaForToolParameters(deleteBranchStepsSchema)
	if err != nil {
		return fmt.Errorf("failed to parse delete_branch_steps schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_branch_steps",
		"Delete steps from a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type, and deleted_step_ids array. Use the step's id field from the plan.",
		deleteBranchStepsParams,
		createDeleteBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register delete_branch_steps tool: %w", err)
	}

	convertToRegularSchema := getConvertConditionalToRegularSchema()
	convertToRegularParams, err := parseSchemaForToolParameters(convertToRegularSchema)
	if err != nil {
		return fmt.Errorf("failed to parse convert_conditional_to_regular schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"convert_conditional_to_regular",
		"Convert a conditional step back to a regular step. This removes all conditional properties and branch steps. Provide step_id of the conditional step to convert.",
		convertToRegularParams,
		createConvertConditionalToRegularExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register convert_conditional_to_regular tool: %w", err)
	}

	// Register orchestration route management tools
	addOrchestrationRouteSchema := getAddOrchestrationRouteSchema()
	addOrchestrationRouteParams, err := parseSchemaForToolParameters(addOrchestrationRouteSchema)
	if err != nil {
		return fmt.Errorf("failed to parse add_orchestration_route schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_orchestration_route",
		"Add a new route (sub-agent) to an orchestration step. Provide parent_step_id and new_route with all required fields (route_id, route_name, condition, sub_agent_step). The plan.json file is updated immediately when this tool is called.",
		addOrchestrationRouteParams,
		createAddOrchestrationRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_orchestration_route tool: %w", err)
	}

	updateOrchestrationRouteSchema := getUpdateOrchestrationRouteSchema()
	updateOrchestrationRouteParams, err := parseSchemaForToolParameters(updateOrchestrationRouteSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update_orchestration_route schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_orchestration_route",
		"Update an existing route (sub-agent) within an orchestration step. Provide parent_step_id, existing_route_id, and only include the fields you want to change (route_name, condition, sub_agent_step, context_to_pass). The plan.json file is updated immediately when this tool is called.",
		updateOrchestrationRouteParams,
		createUpdateOrchestrationRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_orchestration_route tool: %w", err)
	}

	deleteOrchestrationRouteSchema := getDeleteOrchestrationRouteSchema()
	deleteOrchestrationRouteParams, err := parseSchemaForToolParameters(deleteOrchestrationRouteSchema)
	if err != nil {
		return fmt.Errorf("failed to parse delete_orchestration_route schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_orchestration_route",
		"Delete a route (sub-agent) from an orchestration step. Provide parent_step_id and deleted_route_id. NOTE: The orchestration step must have at least one route remaining after deletion. The plan.json file is updated immediately when this tool is called.",
		deleteOrchestrationRouteParams,
		createDeleteOrchestrationRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register delete_orchestration_route tool: %w", err)
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
				return "", fmt.Errorf("failed to create variables.json: %w", err)
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
		return fmt.Errorf("failed to parse extract_variables schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"extract_variables",
		"Extract variables from text or objective. Provide the text to analyze, and the tool will guide you to extract hard-coded values (URLs, account IDs, ports, credentials, resource names, etc.) as variables. After extraction, use update_variable tool to add each variable.",
		extractParams,
		createExtractVariablesExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register extract_variables tool: %w", err)
	}

	// Register update_variable tool (reuse from variable_extraction_agent.go)
	updateVariableSchema := getUpdateVariableSchema()
	updateVariableParams, err := parseSchemaForToolParameters(updateVariableSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update_variable schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_variable",
		"Update, add, or delete variables in variables.json. Provide action (required: 'update', 'add', or 'delete'), existing_variable_name (required for update/delete), and fields to update. The variables.json file is updated immediately when this tool is called.",
		updateVariableParams,
		createUpdateVariableExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_variable tool: %w", err)
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
				return "", fmt.Errorf("failed to parse if_true step: %w", err)
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
				return "", fmt.Errorf("failed to parse if_false step: %w", err)
			}
			ifFalseSteps = append(ifFalseSteps, typedStep)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("failed to write plan: %w", err)
		}
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
				return "", fmt.Errorf("failed to parse new step: %w", err)
			}
			newSteps = append(newSteps, typedStep)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		conditionalStep, ok := parentStep.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", parentStepID)
		}

		// Validate that all new branch steps have IDs (required for config matching)
		for i, newStep := range newSteps {
			if newStep.GetID() == "" {
				return "", fmt.Errorf("branch step at index %d is missing required ID field. Step title: %q", i, newStep.GetTitle())
			}
		}

		// Validate nesting depth for new steps (starting from depth 1 since they're being added to a conditional)
		for _, newStep := range newSteps {
			if err := validateNestingDepth(newStep, 1); err != nil {
				return "", fmt.Errorf("new_steps validation failed: %w", err)
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
			return "", fmt.Errorf("failed to write plan: %w", err)
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

		conditionalStep, ok := parentStep.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", parentStepID)
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
				return "", fmt.Errorf("step ID '%s' not found in %s branch. Available step IDs: %v", partialUpdate.ExistingStepID, branchType, availableIDs)
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
				return "", fmt.Errorf("updated step validation failed: %w", err)
			}
		}

		// Update the conditional step in the plan
		plan.Steps[parentStepIndex] = conditionalStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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
				return "", fmt.Errorf("invalid step ID in deleted_step_ids: %v", id)
			}
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		conditionalStep, ok := parentStep.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", parentStepID)
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
				return "", fmt.Errorf("step ID '%s' not found in %s branch (cannot delete). Available step IDs: %v", id, branchType, availableIDs)
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
			return "", fmt.Errorf("failed to write plan: %w", err)
		}
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
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		orchestrationStep, ok := parentStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not an orchestration step", parentStepID)
		}

		// Validate that the new route has a route_id
		if newRoute.RouteID == "" {
			return "", fmt.Errorf(fmt.Sprintf("new route is missing required route_id field"), nil)
		}

		// Check if route_id already exists
		for _, existingRoute := range orchestrationStep.OrchestrationRoutes {
			if existingRoute.RouteID == newRoute.RouteID {
				return "", fmt.Errorf("route with route_id '%s' already exists in orchestration step '%s'", newRoute.RouteID, parentStepID)
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
			return "", fmt.Errorf("failed to write plan: %w", err)
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
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		orchestrationStep, ok := parentStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not an orchestration step", parentStepID)
		}

		// Find the route to update
		var routeToUpdate *PlanOrchestrationRoute
		for i := range orchestrationStep.OrchestrationRoutes {
			if orchestrationStep.OrchestrationRoutes[i].RouteID == existingRouteID {
				routeToUpdate = &orchestrationStep.OrchestrationRoutes[i]
				break
			}
		}
		if routeToUpdate == nil {
			availableRouteIDs := make([]string, 0, len(orchestrationStep.OrchestrationRoutes))
			for _, route := range orchestrationStep.OrchestrationRoutes {
				availableRouteIDs = append(availableRouteIDs, route.RouteID)
			}
			return "", fmt.Errorf("route with route_id '%s' not found in orchestration step '%s'. Available route IDs: %v", existingRouteID, parentStepID, availableRouteIDs)
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

		// Update the orchestration step in the plan
		plan.Steps[parentStepIndex] = orchestrationStep

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
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
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs)
		}

		orchestrationStep, ok := parentStep.(*OrchestrationPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not an orchestration step", parentStepID)
		}

		// Validate that we have at least one route remaining after deletion
		if len(orchestrationStep.OrchestrationRoutes) <= 1 {
			return "", fmt.Errorf("cannot delete route '%s' - orchestration step must have at least one route", deletedRouteID)
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
			return "", fmt.Errorf("route with route_id '%s' not found in orchestration step '%s'. Available route IDs: %v", deletedRouteID, parentStepID, availableRouteIDs)
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
			return "", fmt.Errorf("failed to write plan: %w", err)
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
			return "", fmt.Errorf("failed to read plan: %w", err)
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
			return "", fmt.Errorf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs)
		}

		conditionalStep, ok := stepToConvert.(*ConditionalPlanStep)
		if !ok {
			return "", fmt.Errorf("step with ID '%s' is not a conditional step", stepID)
		}

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
			return "", fmt.Errorf("failed to write plan: %w", err)
		}
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

// Execute implements the OrchestratorAgent interface
// NOTE: This method is not used - planning agent execution is handled in runPlanningPhase() in planning_management.go
// which uses BaseAgent().Execute() directly after registering tools
func (hctppa *WorkflowPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", conversationHistory, fmt.Errorf("Execute() is not used for planning agent - use runPlanningPhase() instead")
}

// planningSystemPromptProcessorForUpdate generates system prompt for updating an existing plan
func planningSystemPromptProcessorForUpdate(templateVars map[string]string) string {
	now := time.Now()
	templateData := map[string]interface{}{
		"Objective":              templateVars["Objective"],
		"WorkspacePath":          templateVars["WorkspacePath"],
		"ExecutionWorkspacePath": fmt.Sprintf("%s/execution", templateVars["WorkspacePath"]),
		"ExistingPlanJSON":       templateVars["ExistingPlanJSON"],
		"VariableNames":          templateVars["VariableNames"],
		"CurrentDate":            now.Format("2006-01-02"),
		"CurrentTime":            now.Format("15:04:05"),
	}

	var result strings.Builder
	if err := planningUpdateSystemTemplate.Execute(&result, templateData); err != nil {
		return "Error executing planning update system prompt template: " + err.Error()
	}
	return result.String()
}
