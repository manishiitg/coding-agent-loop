package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

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
	OrphanStepRef string            `json:"orphan_step_ref,omitempty"` // Optional: reference to a reusable orphan step definition in the same plan
	ContextToPass string            `json:"context_to_pass,omitempty"` // Optional: specific context to pass to sub-agent
}

// MarshalJSON implements custom marshaling for PlanOrchestrationRoute
// This is needed to properly handle the SubAgentStep field which is a PlanStepInterface
func (r PlanOrchestrationRoute) MarshalJSON() ([]byte, error) {
	type routeJSON struct {
		RouteID       string          `json:"route_id"`
		RouteName     string          `json:"route_name"`
		Condition     string          `json:"condition"`
		SubAgentStep  json.RawMessage `json:"sub_agent_step,omitempty"`
		OrphanStepRef string          `json:"orphan_step_ref,omitempty"`
		ContextToPass string          `json:"context_to_pass,omitempty"`
	}

	result := routeJSON{
		RouteID:       r.RouteID,
		RouteName:     r.RouteName,
		Condition:     r.Condition,
		OrphanStepRef: r.OrphanStepRef,
		ContextToPass: r.ContextToPass,
	}

	// For reusable orphan step references, persist only the reference in plan.json.
	if r.OrphanStepRef != "" {
		result.SubAgentStep = nil
	} else if r.SubAgentStep != nil {
		subAgentJSON, err := json.Marshal(r.SubAgentStep)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal sub_agent_step: %w", err)
		}
		result.SubAgentStep = subAgentJSON
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
		OrphanStepRef string          `json:"orphan_step_ref,omitempty"`
		ContextToPass string          `json:"context_to_pass,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal orchestration route: %w", err)
	}

	// Copy basic fields
	r.RouteID = temp.RouteID
	r.RouteName = temp.RouteName
	r.Condition = temp.Condition
	r.OrphanStepRef = temp.OrphanStepRef
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
	PublishedLLMID string                 `json:"published_llm_id,omitempty"` // Optional published LLM registry reference
	Provider       string                 `json:"provider,omitempty"`         // e.g., "openai", "bedrock", "openrouter", "vertex"
	ModelID        string                 `json:"model_id,omitempty"`         // e.g., "gpt-4o", "claude-3-5-sonnet-20241022"
	Options        map[string]interface{} `json:"options,omitempty"`          // Provider-specific runtime options
	Fallbacks      []AgentLLMFallback     `json:"fallbacks,omitempty"`        // Optional fallback models for retry on failure
}

// AgentLLMFallback represents a fallback LLM model
type AgentLLMFallback struct {
	PublishedLLMID string                 `json:"published_llm_id,omitempty"`
	Provider       string                 `json:"provider"`
	ModelID        string                 `json:"model_id"`
	Options        map[string]interface{} `json:"options,omitempty"`
}

// ValidationSchema represents structured validation rules for step outputs
type ValidationSchema struct {
	Files []FileValidationRule `json:"files,omitempty"`
	// DB gates the step on read-only SQL queries against db/db.sqlite — the source
	// of truth — instead of (or alongside) a hand-written output file. Lets a step
	// be validated on what it actually produced in the db, so context_output files
	// don't have to exist just to be validatable.
	DB []DBValidationRule `json:"db,omitempty"`
}

// DBValidationRule asserts on a read-only SQL query against db/db.sqlite.
type DBValidationRule struct {
	Name    string                `json:"name,omitempty"`     // label shown in failure feedback
	SQL     string                `json:"sql"`                // read-only SELECT against db/db.sqlite
	MinRows *int                  `json:"min_rows,omitempty"` // result must have >= N rows
	MaxRows *int                  `json:"max_rows,omitempty"` // result must have <= N rows
	Checks  []JSONValidationCheck `json:"checks,omitempty"`   // JSONPath asserts on the FIRST result row (row = object keyed by column)
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
	ExecutionLLM                 *AgentLLMConfig `json:"execution_llm,omitempty"`
	ExecutionTier                string          `json:"execution_tier,omitempty"` // Persistent execution tier override in tiered mode: "high" | "medium" | "low"
	LearningLLM                  *AgentLLMConfig `json:"learning_llm,omitempty"`
	ExecutionMaxTurns            *int            `json:"execution_max_turns,omitempty"`             // default: 500
	LearningObjective            string          `json:"learning_objective,omitempty"`              // What SKILL.md should capture from successful runs of this step — selectors, timings, auth flows, tool-call patterns, API quirks. This is the instruction the step agent uses during its post-completion turn to know what HOW-to-run knowledge to extract. Required when learnings_access includes write; must be specific (not "learn from this run").
	LearningsAccess              string          `json:"learnings_access,omitempty"`                // "read" | "read-write" | "none". Mirrors knowledgebase_access. "read" (default): step sees global SKILL.md in its prompt but doesn't write. "read-write": reads and writes — requires learning_objective to be non-empty. "none": no read, no write. Empty = legacy auto-migration (see resolveLearningsAccess). Writes happen via the step agent's own post-completion turn (shell + diff_patch_workspace_file); no separate analyzer runs.
	LearningsWriteMethod         string          `json:"learnings_write_method,omitempty"`          // Retained for plan.json backward compatibility; the only supported write path is the step agent's own post-completion turn. Setting this is a no-op — omit it from new plans.
	LockLearnings                *bool           `json:"lock_learnings,omitempty"`                  // lock learnings (SKILL.md) - prevents new writes but still uses existing SKILL.md (nil = not set/unlocked, true = locked, false = explicitly unlocked)
	LockCode                     *bool           `json:"lock_code,omitempty"`                       // lock code (main.py) - prevents LLM-rewritten main.py from being saved back to learnings, skips fix loop (nil = not set/unlocked, true = locked, false = explicitly unlocked)
	SelectedServers              []string        `json:"selected_servers,omitempty"`                // step-level MCP server selection (subset of preset servers)
	SelectedTools                []string        `json:"selected_tools,omitempty"`                  // step-level tool selection (format: "server:tool" or "server:*" for all tools)
	EnabledCustomTools           []string        `json:"enabled_custom_tools,omitempty"`            // e.g., ["read_workspace_file", "human_feedback"] - enables specific tools (overrides categories if both specified)
	EnableContextOffloading      *bool           `json:"enable_context_offloading,omitempty"`       // Enable/disable context offloading (default: true if nil)
	UseCodeExecutionMode         *bool           `json:"use_code_execution_mode,omitempty"`         // Step-level code execution mode override (nil = use preset default, true/false = override)
	EnabledSkills                []string        `json:"enabled_skills,omitempty"`                  // Step-level skill selection (skill folder names, overrides preset if specified)
	KnowledgebaseAccess          string          `json:"knowledgebase_access,omitempty"`            // "read" | "write" | "read-write" | "none". If empty, defaults to "none" — KB is opt-in per step. Preset-level UseKnowledgebase is a prerequisite (off → forced "none"). When granted "read", this also covers user-supplied runtime context at knowledgebase/context/context.md. knowledgebase/context/ is excluded from improve_kb passes — user-supplied content is never silently rewritten.
	KnowledgebaseWriteMethod     string          `json:"knowledgebase_write_method,omitempty"`      // "agent" | "direct". How KB writes happen when knowledgebase_access permits them. Runtime fallback is "agent": the post-step KB update agent reads the step's trail + knowledgebase_contribution and writes notes/. Builder preference is usually "direct" so the step agent writes notes/ itself via shell + diff_patch_workspace_file during execution, with a post-completion self-review turn. Use "agent" mainly when the user explicitly wants it or when the step's output is messy enough that a separate extractor is clearly better. Only meaningful when knowledgebase_access ∈ {"write", "read-write"}.
	KnowledgebaseContribution    string          `json:"knowledgebase_contribution,omitempty"`      // User-authored instruction for KB writes — what this step should contribute to notes/. In "agent" write-method, it's the extraction instruction handed to the post-step KB update agent. In "direct" write-method, it's injected into the step agent's prompt as its contribution contract. Required to trigger KB writes; if empty, no KB write happens regardless of access.
	DBAccess                     string          `json:"db_access,omitempty"`                       // "read" | "read-write". Empty defaults to "read-write" — every step gets read+write to db/ (back-compat). Set "read" for least-privilege read-only steps (pure readers, report-shaping, validation): db/ stays in read paths but is stripped from write paths. Mirrors the eval-step db_write opt-out.
	TodoTaskOrchestratorTier     *int            `json:"todo_task_orchestrator_tier,omitempty"`     // Tier for todo task orchestrator agent (1/2/3) in tiered mode
	DisableParallelToolExecution *bool           `json:"disable_parallel_tool_execution,omitempty"` // Disable parallel tool execution for this step (nil = enabled by default, true = disabled, false = explicitly enabled)
	CodingAgentTmuxLifecycle     string          `json:"coding_agent_tmux_lifecycle,omitempty"`     // Tmux lifecycle for CLI coding providers: "close_on_completion" (default for steps) or "keep_alive" (only when a step intentionally needs the native coding session after completion).
	// Transport selects the CLI transport for coding-agent providers
	// (claude-code, codex-cli, cursor-cli, gemini-cli, pi-cli)
	// for this step:
	//   ""           — inherit the agent/preset default (tmux interactive)
	//   "tmux"       — force tmux interactive transport
	//   "structured" — force structured JSON transport (--print/--exec/stream-json)
	// Gemini CLI workflow steps always use structured stream-json; "tmux" is
	// ignored there. Gemini CLI chat can still use the persistent tmux TUI.
	// Non-coding-agent providers (anthropic, openai, vertex, ...) ignore this.
	Transport                   string `json:"transport,omitempty"`
	DisableTierOptimization     *bool  `json:"disable_tier_optimization,omitempty"`      // If true, execution and conditional agents always use Tier 1 (high reasoning)
	SuccessfulRuns              *int   `json:"successful_runs,omitempty"`                // System-managed counter. Written by syncSuccessfulRunsToStepConfig after each successful validation; mirrors the authoritative count in learning metadata. Read by the readiness checklist to gauge optimization progress (3+ = ready). Agents must NOT set this directly.
	ScriptedMaxFixIter          *int   `json:"learn_code_max_fix_iterations,omitempty"`  // Max LLM fix iterations when main.py execution fails (default: 5)
	DeclaredExecutionMode       string `json:"declared_execution_mode,omitempty"`        // Required mode decision for the step: "scripted" or "agentic" (legacy values "learn_code" / "code_exec" are still accepted on read)
	DeclaredExecutionModeReason string `json:"declared_execution_mode_reason,omitempty"` // Audit trail: why the declared mode is the best fit. Not consumed by Go runtime, but preserved so future LLM reviewers (harden, replan) reading raw step_config.json see the original decision rationale.
	DescriptionReviewed         *bool  `json:"description_reviewed,omitempty"`           // True when the step description has been reviewed — clarity AND secrets/hardcoded values.
	ReviewNotes                 string `json:"review_notes,omitempty"`                   // Free-form rationale covering why config, locks, learning/KB choices, or description review state are justified.
	GlobalSkillObjective        string `json:"global_skill_objective,omitempty"`         // Objective for the global skill — what domain knowledge should it capture and why
}

// ============================================================================
// TYPE-SAFE STEP SYSTEM (New Implementation)
// ============================================================================

// StepType represents the type of a plan step
type StepType string

const (
	StepTypeRegular    StepType = "regular"
	StepTypeHumanInput StepType = "human_input"
	StepTypeTodoTask   StepType = "todo_task"
	StepTypeRouting    StepType = "routing"
	StepTypeMessageSeq StepType = "message_sequence"
)

// CommonStepFields contains fields shared by all step types
type CommonStepFields struct {
	ID                  string                `json:"id"` // Stable step ID (generated from title) - required
	Title               string                `json:"title"`
	Description         string                `json:"description"`
	ContextDependencies []string              `json:"context_dependencies"`
	ContextOutput       FlexibleContextOutput `json:"context_output"`              // Use flexible type to handle string or array
	ValidationSchema    *ValidationSchema     `json:"validation_schema,omitempty"` // Optional structured validation schema for step outputs
	SharedWith          *StepSharing          `json:"shared_with,omitempty"`       // Optional: visibility rules for reusable orphan steps
}

// StepSharing defines which orchestrators in the same plan may reuse an orphan step.
type StepSharing struct {
	OrchestratorIDs []string `json:"orchestrator_ids,omitempty"`
}

// PlanStepInterface is the interface that all step types must implement
// PlanStep is a type alias for PlanStepInterface for convenience
type PlanStepInterface interface {
	GetID() string
	GetTitle() string
	GetDescription() string
	GetContextDependencies() []string
	GetContextOutput() FlexibleContextOutput
	GetValidationSchema() *ValidationSchema
	StepType() StepType
	// GetCommonFields returns a copy of common fields for convenience
	GetCommonFields() CommonStepFields
}

// RegularPlanStep represents a regular step
type RegularPlanStep struct {
	Type StepType `json:"type"` // Always "regular" - required for JSON marshaling/unmarshaling
	CommonStepFields
	HasLoop         bool          `json:"has_loop"`                   // DEPRECATED: loop feature removed, kept for JSON backward compatibility
	LoopCondition   string        `json:"loop_condition,omitempty"`   // DEPRECATED: loop feature removed
	MaxIterations   int           `json:"max_iterations,omitempty"`   // DEPRECATED: loop feature removed
	LoopDescription string        `json:"loop_description,omitempty"` // DEPRECATED: loop feature removed
	AgentConfigs    *AgentConfigs `json:"-"`                          // runtime: per-agent configuration (LLM, max turns, toggles) - not stored in plan.json
}

// Implement PlanStepInterface for RegularPlanStep
func (r *RegularPlanStep) GetID() string                           { return r.ID }
func (r *RegularPlanStep) GetTitle() string                        { return r.Title }
func (r *RegularPlanStep) GetDescription() string                  { return r.Description }
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

// RoutingRoute represents a single route option in a routing step
type RoutingRoute struct {
	RouteID    string `json:"route_id"`
	RouteName  string `json:"route_name"`
	Condition  string `json:"condition"`
	NextStepID string `json:"next_step_id"`
}

// RoutingResponse represents the structured response from routing evaluation
type RoutingResponse struct {
	SelectedRouteID string `json:"selected_route_id"` // The selected route ID
	Reasoning       string `json:"reasoning"`         // Reasoning for the selection
}

// RoutingPlanStep represents a deterministic N-way switch.
// Routing steps never execute agents. If an agent/probe/judgment is needed, put
// it in a prior regular step that writes route_selection.json, then point this
// routing step at that file via route_source_file or context_dependencies.
type RoutingPlanStep struct {
	Type StepType `json:"type"` // Always "routing" - required for JSON marshaling/unmarshaling
	CommonStepFields
	RoutingQuestion string           `json:"routing_question"`            // Human-readable route decision prompt; retained for compatibility/readability
	Routes          []RoutingRoute   `json:"routes"`                      // Available routes (min 2, required)
	DefaultRouteID  string           `json:"default_route_id,omitempty"`  // Optional fallback route_id when no route file is available
	RouteSourceFile string           `json:"route_source_file,omitempty"` // Optional route_selection.json source produced by a prior step
	SelectedRouteID string           `json:"-"`                           // runtime: stores selected route ID
	RoutingResponse *RoutingResponse `json:"-"`                           // runtime: stores structured routing response
	AgentConfigs    *AgentConfigs    `json:"-"`                           // runtime: per-agent configuration
}

// Implement PlanStepInterface for RoutingPlanStep
func (r *RoutingPlanStep) GetID() string                           { return r.ID }
func (r *RoutingPlanStep) GetTitle() string                        { return r.Title }
func (r *RoutingPlanStep) GetDescription() string                  { return r.Description }
func (r *RoutingPlanStep) GetContextDependencies() []string        { return r.ContextDependencies }
func (r *RoutingPlanStep) GetContextOutput() FlexibleContextOutput { return r.ContextOutput }
func (r *RoutingPlanStep) GetValidationSchema() *ValidationSchema  { return r.ValidationSchema }
func (r *RoutingPlanStep) StepType() StepType                      { return StepTypeRouting }
func (r *RoutingPlanStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:                  r.ID,
		Title:               r.Title,
		Description:         r.Description,
		ContextDependencies: r.ContextDependencies,
		ContextOutput:       r.ContextOutput,
		ValidationSchema:    r.ValidationSchema,
		SharedWith:          r.SharedWith,
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

// MessageSequenceWriteAccess controls temporary write access for a single sequence item.
// Read access to KB, DB, and global learnings is always available for the whole sequence.
type MessageSequenceWriteAccess struct {
	Knowledgebase bool `json:"knowledgebase,omitempty"`
	DB            bool `json:"db,omitempty"`
	Learnings     bool `json:"learnings,omitempty"`
}

// UnmarshalJSON rejects per-file scoping attempts on write_access. The access is
// folder-level booleans (knowledgebase/db/learnings); authors frequently try
// {"db": true, "paths": ["db/foo.json"]} expecting a single file to be writable.
// Standard decoding silently drops the unknown "paths" key, granting the whole
// db/ folder while the author thinks it was scoped — a real footgun. Catch the
// path-like keys with an explicit, actionable error instead.
func (w *MessageSequenceWriteAccess) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, key := range []string{"paths", "path", "files", "file"} {
		if _, ok := raw[key]; ok {
			return fmt.Errorf("write_access does not support per-file scoping (%q): it is folder-level booleans only — use {\"db\": true}, {\"knowledgebase\": true}, and/or {\"learnings\": true}", key)
		}
	}
	type alias MessageSequenceWriteAccess
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*w = MessageSequenceWriteAccess(a)
	return nil
}

type MessageSequenceFailurePolicy struct {
	Action     string `json:"action,omitempty"` // "stop_step" | "repair_with_llm" | "repair_same_session"
	MaxRetries int    `json:"max_retries,omitempty"`
}

func (p *MessageSequenceFailurePolicy) UnmarshalJSON(data []byte) error {
	var action string
	if err := json.Unmarshal(data, &action); err == nil {
		p.Action = action
		return nil
	}
	type alias MessageSequenceFailurePolicy
	return json.Unmarshal(data, (*alias)(p))
}

type MessageSequenceItem struct {
	ID               string                       `json:"id"`
	Type             string                       `json:"type"` // "user_message" | "code" | "prevalidation"
	Kind             string                       `json:"kind,omitempty"`
	Title            string                       `json:"title,omitempty"`
	Message          string                       `json:"message,omitempty"`
	Runtime          string                       `json:"runtime,omitempty"`
	ScriptPath       string                       `json:"script_path,omitempty"`
	InputFiles       []string                     `json:"input_files,omitempty"`
	InputJSON        map[string]interface{}       `json:"input_json,omitempty"`
	OutputFiles      []string                     `json:"output_files,omitempty"`
	WriteAccess      MessageSequenceWriteAccess   `json:"write_access,omitempty"`
	OnFailure        MessageSequenceFailurePolicy `json:"on_failure,omitempty"`
	SaveRepaired     bool                         `json:"save_repaired_script,omitempty"`
	ValidationSchema *ValidationSchema            `json:"validation_schema,omitempty"`
	Prevalidation    *ValidationSchema            `json:"prevalidation,omitempty"`
	// foreach items: iterate db/db.sqlite table rows, one templated user_message
	// per row. source_sql is a read-only query; each result row binds to '.'.
	SourceSQL     string `json:"source_sql,omitempty"`     // read-only SQL against db/db.sqlite
	MaxIterations int    `json:"max_iterations,omitempty"` // optional cap on rows (0 = all)
	// prevalidation entries: corrective turns allowed on gate failure. Used by the
	// orchestrator's scripted sequence (todo_task); a standalone message_sequence
	// uses its own fixed repair cap. Default 1 when unset.
	MaxCorrections int `json:"max_corrections,omitempty"`
}

type MessageSequencePlanStep struct {
	Type StepType `json:"type"`
	CommonStepFields
	Items        []MessageSequenceItem `json:"items,omitempty"`
	NextStepID   string                `json:"next_step_id,omitempty"`
	AgentConfigs *AgentConfigs         `json:"-"`
}

func (m *MessageSequencePlanStep) GetID() string                           { return m.ID }
func (m *MessageSequencePlanStep) GetTitle() string                        { return m.Title }
func (m *MessageSequencePlanStep) GetDescription() string                  { return m.Description }
func (m *MessageSequencePlanStep) GetContextDependencies() []string        { return m.ContextDependencies }
func (m *MessageSequencePlanStep) GetContextOutput() FlexibleContextOutput { return m.ContextOutput }
func (m *MessageSequencePlanStep) GetValidationSchema() *ValidationSchema  { return m.ValidationSchema }
func (m *MessageSequencePlanStep) StepType() StepType                      { return StepTypeMessageSeq }
func (m *MessageSequencePlanStep) GetCommonFields() CommonStepFields       { return m.CommonStepFields }

func (m *MessageSequencePlanStep) MarshalJSON() ([]byte, error) {
	m.Type = StepTypeMessageSeq
	type Alias MessageSequencePlanStep
	return json.Marshal((*Alias)(m))
}

// TodoTaskPlanStep represents a todo task orchestrator step that manages a dynamic todo list
// It combines predefined sub-agents (with learning/prevalidation) and an optional generic execution agent
// The main orchestrator creates/assigns tasks, then delegates to appropriate agents
// NOTE: Todo task steps are orchestration-like wrappers that manage todo lists instead of success criteria.
// Loops are NOT supported on todo task wrappers - the step completes when all todos are done.
type TodoTaskPlanStep struct {
	Type             StepType                 `json:"type"` // Always "todo_task" - required for JSON marshaling/unmarshaling
	CommonStepFields                          // Embeds ID, Title, Description, SuccessCriteria, ContextDependencies, ContextOutput, ValidationSchema
	PredefinedRoutes []PlanOrchestrationRoute `json:"predefined_routes,omitempty"` // Predefined sub-agents (with learning/prevalidation)
	NextStepID       string                   `json:"next_step_id,omitempty"`      // ID of step after todo task completes (or "end")
	Messages         []MessageSequenceItem    `json:"messages,omitempty"`          // Optional scripted message sequence fed into the orchestrator's own conversation after its first turn
	TodoTaskResponse *TodoTaskResponse        `json:"-"`                           // runtime: stores orchestrator decisions - not stored in plan.json
	AgentConfigs     *AgentConfigs            `json:"-"`                           // runtime: per-agent configuration - not stored in plan.json
}

// A todo_task step's optional scripted message sequence reuses MessageSequenceItem
// (the unified sequence-item type). The orchestrator only runs the conversational
// kinds — user_message/message, prevalidation, foreach — and delegates real work to
// sub-agents; code/file/write_access items are rejected at plan validation. After the
// orchestrator's first turn each item is fed into the SAME orchestrator conversation
// in order; a prevalidation item is a hard gate (up to MaxCorrections corrective
// turns). The whole sequence runs within one execution — no persistence or re-entry.

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
func (t *TodoTaskPlanStep) GetID() string                           { return t.ID }
func (t *TodoTaskPlanStep) GetTitle() string                        { return t.Title }
func (t *TodoTaskPlanStep) GetDescription() string                  { return t.Description }
func (t *TodoTaskPlanStep) GetContextDependencies() []string        { return t.ContextDependencies }
func (t *TodoTaskPlanStep) GetContextOutput() FlexibleContextOutput { return t.ContextOutput }
func (t *TodoTaskPlanStep) GetValidationSchema() *ValidationSchema  { return t.ValidationSchema }
func (t *TodoTaskPlanStep) StepType() StepType                      { return StepTypeTodoTask }
func (t *TodoTaskPlanStep) GetCommonFields() CommonStepFields       { return t.CommonStepFields }

// MarshalJSON ensures the type field is always set when marshaling TodoTaskPlanStep
// Writes the flat format (no nested todo_task_step)
func (t *TodoTaskPlanStep) MarshalJSON() ([]byte, error) {
	// Ensure type is set
	t.Type = StepTypeTodoTask

	// Use type alias to avoid infinite recursion
	type Alias TodoTaskPlanStep
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements custom unmarshaling for TodoTaskPlanStep
// Supports both the new flat format and the legacy nested todo_task_step format for backwards compatibility.
func (t *TodoTaskPlanStep) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a temporary struct to extract nested steps as raw JSON
	var temp struct {
		Type  StepType `json:"type"`
		ID    string   `json:"id"`
		Title string   `json:"title"`
		// Flat format fields (new)
		Description         string                `json:"description"`
		ContextDependencies []string              `json:"context_dependencies"`
		ContextOutput       FlexibleContextOutput `json:"context_output"`
		ValidationSchema    *ValidationSchema     `json:"validation_schema,omitempty"`
		// Legacy nested field (backwards compatibility)
		TodoTaskStep     json.RawMessage `json:"todo_task_step,omitempty"`
		PredefinedRoutes []struct {
			RouteID       string          `json:"route_id"`
			RouteName     string          `json:"route_name"`
			Condition     string          `json:"condition"`
			SubAgentStep  json.RawMessage `json:"sub_agent_step"`
			OrphanStepRef string          `json:"orphan_step_ref,omitempty"`
			ContextToPass string          `json:"context_to_pass,omitempty"`
		} `json:"predefined_routes,omitempty"`
		NextStepID string                `json:"next_step_id,omitempty"`
		Messages   []MessageSequenceItem `json:"messages,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal todo_task step: %w", err)
	}

	// Copy basic fields
	t.Type = temp.Type
	t.ID = temp.ID
	t.Title = temp.Title
	t.NextStepID = temp.NextStepID
	t.Messages = temp.Messages

	// Copy flat format fields
	t.Description = temp.Description
	t.ContextDependencies = temp.ContextDependencies
	t.ContextOutput = temp.ContextOutput
	t.ValidationSchema = temp.ValidationSchema

	// BACKWARDS COMPATIBILITY: if legacy todo_task_step is present, migrate fields from it
	// Top-level fields take precedence if both are present
	if len(temp.TodoTaskStep) > 0 && string(temp.TodoTaskStep) != "null" {
		innerStep, err := unmarshalStepFromJSON(temp.TodoTaskStep)
		if err != nil {
			return fmt.Errorf("failed to unmarshal legacy todo_task_step: %w", err)
		}
		if t.Description == "" {
			t.Description = innerStep.GetDescription()
		}
		if t.ContextDependencies == nil {
			t.ContextDependencies = innerStep.GetContextDependencies()
		}
		if t.ContextOutput == "" {
			t.ContextOutput = innerStep.GetContextOutput()
		}
		if t.ValidationSchema == nil {
			t.ValidationSchema = innerStep.GetValidationSchema()
		}
	}

	// Unmarshal predefined_routes with nested sub_agent_step
	if len(temp.PredefinedRoutes) > 0 {
		t.PredefinedRoutes = make([]PlanOrchestrationRoute, len(temp.PredefinedRoutes))
		for i, route := range temp.PredefinedRoutes {
			t.PredefinedRoutes[i].RouteID = route.RouteID
			t.PredefinedRoutes[i].RouteName = route.RouteName
			t.PredefinedRoutes[i].Condition = route.Condition
			t.PredefinedRoutes[i].OrphanStepRef = route.OrphanStepRef
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

// PlanStep is now an alias for PlanStepInterface for convenience
// All code should use PlanStepInterface directly
type PlanStep = PlanStepInterface

// PlanningResponse represents the structured response from planning
// Uses type-safe PlanStepInterface - all plans must be in new format with "type" field
type PlanningResponse struct {
	Objective       string              `json:"objective,omitempty"`
	SuccessCriteria string              `json:"success_criteria,omitempty"`
	Steps           []PlanStepInterface `json:"-"`
	OrphanSteps     []PlanStepInterface `json:"-"`
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
		return nil, fmt.Errorf("%s %d is missing required 'type' field (must be: regular, human_input, todo_task, routing, or message_sequence)", label, index)
	}

	switch stepWithType.Type {
	case "regular":
		var step RegularPlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse regular %s %d: %w", label, index, err)
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
	case "message_sequence":
		var step MessageSequencePlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse message_sequence %s %d: %w", label, index, err)
		}
		return &step, nil
	default:
		return nil, fmt.Errorf("unknown step type %q in %s %d (must be: regular, human_input, todo_task, routing, or message_sequence)", stepWithType.Type, label, index)
	}
}

// UnmarshalJSON implements custom unmarshaling for typed steps
func (pr *PlanningResponse) UnmarshalJSON(data []byte) error {
	var temp struct {
		Objective       string            `json:"objective"`
		SuccessCriteria string            `json:"success_criteria"`
		Steps           []json.RawMessage `json:"steps"`
		OrphanSteps     []json.RawMessage `json:"orphan_steps"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal plan: %w", err)
	}

	pr.Objective = temp.Objective
	pr.SuccessCriteria = temp.SuccessCriteria
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
// Nested steps (OrchestrationStep, IfTrueSteps, IfFalseSteps) are PlanStepInterface.
// Once plan.json is migrated to use new type-safe types, this struct can be updated accordingly.
type PartialPlanStep struct {
	ExistingStepID      string                `json:"existing_step_id"`               // Required: ID of existing step to update
	Title               string                `json:"title,omitempty"`                // Optional: New title (if renaming)
	Description         string                `json:"description,omitempty"`          // Optional: Updated description
	ClearDescription    bool                  `json:"clear_description,omitempty"`    // Optional: Clear legacy descriptions that are no longer valid for routing steps
	ContextDependencies []string              `json:"context_dependencies,omitempty"` // Optional: Updated context dependencies
	ContextOutput       FlexibleContextOutput `json:"context_output,omitempty"`       // Optional: Updated context output
	HasLoop             *bool                 `json:"has_loop,omitempty"`             // DEPRECATED: loop feature removed, kept for JSON backward compatibility
	LoopCondition       string                `json:"loop_condition,omitempty"`       // DEPRECATED: loop feature removed
	MaxIterations       *int                  `json:"max_iterations,omitempty"`       // DEPRECATED: loop feature removed
	LoopDescription     string                `json:"loop_description,omitempty"`     // DEPRECATED: loop feature removed
	// Todo task step fields
	TodoTaskStep     map[string]interface{}   `json:"todo_task_step,omitempty"`    // Optional: Updated todo task step - will be converted to PlanStepInterface
	PredefinedRoutes []PlanOrchestrationRoute `json:"predefined_routes,omitempty"` // Optional: Updated predefined routes for todo task steps
	Messages         []MessageSequenceItem    `json:"messages,omitempty"`          // Optional: Updated scripted message sequence for todo task steps
	// Routing fields
	NextStepID string `json:"next_step_id,omitempty"` // Optional: Updated next_step_id (for routing steps)
	// Routing step fields
	RoutingQuestion string         `json:"routing_question,omitempty"`  // Optional: Updated routing question
	Routes          []RoutingRoute `json:"routes,omitempty"`            // Optional: Updated routes
	DefaultRouteID  string         `json:"default_route_id,omitempty"`  // Optional: Updated default route ID
	RouteSourceFile string         `json:"route_source_file,omitempty"` // Optional: Updated deterministic route source file
	// Human input step fields
	Question         string            `json:"question,omitempty"`            // Optional: Updated question
	VariableName     string            `json:"variable_name,omitempty"`       // Optional: Updated variable name
	ResponseType     string            `json:"response_type,omitempty"`       // Optional: Updated response type
	Options          []string          `json:"options,omitempty"`             // Optional: Updated options (for multiple_choice)
	IfYesNextStepID  string            `json:"if_yes_next_step_id,omitempty"` // Optional: Updated if_yes_next_step_id (for yesno)
	IfNoNextStepID   string            `json:"if_no_next_step_id,omitempty"`  // Optional: Updated if_no_next_step_id (for yesno)
	OptionRoutes     map[string]string `json:"option_routes,omitempty"`       // Optional: Updated option routes (for multiple_choice)
	ValidationSchema *ValidationSchema `json:"validation_schema,omitempty"`   // Optional: Updated validation schema
	// Message sequence fields
	Items []MessageSequenceItem `json:"items,omitempty"`
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

// Every plan-mod tool requires a `reason` field — a one-line rationale captured
// at tool-invocation time so the plan changelog can store the why alongside the
// diff. The schema fragment is hard-coded in each schema; keep them in sync.

// PlanChangelogEntry is one entry in the per-session plan changelog. Each
// successful plan-mod tool call appends one entry to the active session file
// under planning/changelog/.
type PlanChangelogEntry struct {
	Timestamp    string            `json:"timestamp"`               // ISO 8601 UTC
	Tool         string            `json:"tool"`                    // tool name (e.g. "update_regular_step")
	Reason       string            `json:"reason"`                  // mandatory rationale supplied by the agent
	StepIDs      []string          `json:"step_ids,omitempty"`      // affected step IDs
	Changes      []PlanFieldChange `json:"changes,omitempty"`       // per-field old/new values when known
	AddedSteps   []json.RawMessage `json:"added_steps,omitempty"`   // full JSON of steps added (for revert)
	DeletedSteps []json.RawMessage `json:"deleted_steps,omitempty"` // full JSON of steps deleted (for revert)
}

// PlanChangelog is the per-session changelog file under planning/changelog/.
type PlanChangelog struct {
	Entries []PlanChangelogEntry `json:"entries"`
}

// Session-file tracking — one file per workshop run, rotated after an hour
// of inactivity.
var (
	planChangelogSessionMutex sync.Mutex
	planChangelogSessionFile  string
	planChangelogSessionStart time.Time
)

// asString safely extracts a string from a map[string]interface{} value,
// returning "" for nil / missing / non-string values.
func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// requireReason extracts and validates the mandatory `reason` argument that
// every plan-mod tool requires. Returns the trimmed reason or an error if
// missing/empty.
func requireReason(args map[string]interface{}) (string, error) {
	reason := strings.TrimSpace(asString(args["reason"]))
	if reason == "" {
		return "", fmt.Errorf("reason is required: provide a one-sentence rationale for this plan change (it will be appended to the plan changelog)")
	}
	return reason, nil
}

// logPlanChange is the non-fatal wrapper every plan-mod executor calls after a
// successful writePlanToFile. A changelog write failure is logged at warn
// level but never blocks the actual plan mutation.
func logPlanChange(
	ctx context.Context,
	workspacePath string,
	entry PlanChangelogEntry,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	logger loggerv2.Logger,
) {
	if err := writePlanChangelogEntry(ctx, workspacePath, entry, readFile, writeFile, logger); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Plan changelog write failed (non-fatal): %v", err))
	}
}

// writePlanChangelogEntry appends entry to the active session changelog file
// under planning/changelog/. The file is created if missing; subsequent calls
// within the same hour append to the same file. Errors are logged at warn
// level by the caller — a changelog write must never block the actual plan
// mutation.
func writePlanChangelogEntry(
	ctx context.Context,
	workspacePath string,
	entry PlanChangelogEntry,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	logger loggerv2.Logger,
) error {
	planChangelogSessionMutex.Lock()
	defer planChangelogSessionMutex.Unlock()

	now := time.Now().UTC()
	if planChangelogSessionFile == "" || now.Sub(planChangelogSessionStart) > time.Hour {
		planChangelogSessionStart = now
		planChangelogSessionFile = fmt.Sprintf("changelog-%s.json", now.Format("2006-01-02-15-04-05"))
		logger.Info(fmt.Sprintf("📝 Plan changelog: starting new session file %s", planChangelogSessionFile))
	}

	if entry.Timestamp == "" {
		entry.Timestamp = now.Format(time.RFC3339)
	}

	relPath := filepath.Join("planning", "changelog", planChangelogSessionFile)
	changelogPath := normalizePathForWorkspaceAPI(relPath, workspacePath)

	var clog PlanChangelog
	if existing, err := readFile(ctx, changelogPath); err == nil && strings.TrimSpace(existing) != "" {
		if err := json.Unmarshal([]byte(existing), &clog); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Plan changelog: failed to parse existing file, starting fresh: %v", err))
			clog = PlanChangelog{}
		}
	}
	clog.Entries = append(clog.Entries, entry)

	data, err := json.MarshalIndent(clog, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan changelog: %w", err)
	}
	if err := writeFile(ctx, changelogPath, string(data)); err != nil {
		return fmt.Errorf("write plan changelog: %w", err)
	}
	logger.Info(fmt.Sprintf("📝 Plan changelog: %s — %s", entry.Tool, entry.Reason))
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
						"context_dependencies": {
							"type": "array",
							"items": { "type": "string" },
							"description": "OPTIONAL: Updated context dependencies. Only include if you want to change them. If omitted, the existing context dependencies are preserved."
						},
						"context_output": {
							"type": "string",
							"description": "OPTIONAL: Updated context output. Only include if you want to change it. If omitted, the existing context output is preserved."
						},
						"reason": {
							"type": "string",
							"description": "REQUIRED: One-sentence rationale for why this change is being made. Captured into the plan changelog. Be specific — 'tighten validation' is weak; 'add validation_schema for context_output to catch upstream JSON-shape regressions surfaced in iteration-3' is good."
						}
					},
					"required": ["existing_step_id", "reason"]
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
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why these steps are being removed. Captured into the plan changelog. Be specific — 'cleanup' is weak; 'remove step-3 (intent-classifier) — replaced by step-2 doing the classification inline' is good."
			}
		},
		"required": ["deleted_step_ids", "reason"]
	}`
}

// getCreatePlanSchema returns the JSON schema for the create_plan tool.
// The tool takes no arguments — it initializes an empty planning/plan.json
// so that subsequent add_*_step tools have a file to modify.
func getCreatePlanSchema() string {
	return `{
		"type": "object",
		"properties": {}
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
				"description": "REQUIRED: COMPREHENSIVE, DETAILED executable instruction for the step agent. This is the main prompt the regular step agent runs, not metadata. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context."
			},
			"context_dependencies": {
				"type": "array",
				"items": { "type": "string" },
				"description": "REQUIRED: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "OPTIONAL: The context file this step creates for subsequent steps - e.g., 'step_1_results.json'. OMIT IT when the step's real output is the db (db/db.sqlite) — then validate the step with validation_schema.db instead of a file, and downstream steps read the db; this avoids a hand-written receipt file that drifts from the db. Provide it when a downstream step needs the result injected via context_dependencies, or you want a small file artifact. Execution agents work ONLY in the execution folder, so the file is written there. Keep JSON files SMALL (< 100KB); put large text (logs, content > 1KB) in a separate markdown file and reference it from JSON. JSON should hold structured data: counts, IDs, status, file references, brief summaries."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			},
			"validation_schema": {
				"type": "object",
				"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. You MUST generate this by parsing the step description and extracting file names, field requirements, and validation rules. This enables pre-validation before LLM validation (improves speed by 50-70%). Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath like $.field_name), must_exist: boolean, value_type?: string (string/number/boolean/array/object), min_length?: number, max_length?: number, pattern?: string (VALID Go regex - must compile with regexp.Compile, ensure balanced parentheses, escape special chars), min_value?: number, max_value?: number, consistency_check?: {type: string (array_length/equals/greater_than/less_than), compare_with_path: string}}]}]}. Example: If the step description requires 'File results.json contains status field and count field equals items array length', generate schema with file_name: 'results.json', json_checks: [{path: '$.status', must_exist: true}, {path: '$.count', must_exist: true, consistency_check: {type: 'array_length', compare_with_path: '$.items'}}]. CRITICAL FOR array_length CONSISTENCY CHECKS - AVOID COMMON MISTAKE: For array_length checks, path MUST point to COUNT/NUMBER field (e.g., '$.count', '$.total_expected_count', '$.length'), and compare_with_path MUST point to ARRAY field (e.g., '$.items', '$.downloaded_files', '$.files'). CORRECT examples: {path: '$.count', consistency_check: {type: 'array_length', compare_with_path: '$.items'}}, {path: '$.total_expected_count', consistency_check: {type: 'array_length', compare_with_path: '$.downloaded_files'}}. INCORRECT (SWAPPED) - DO NOT DO THIS: {path: '$.items', consistency_check: {type: 'array_length', compare_with_path: '$.count'}} ❌ WRONG - paths are swapped! Field name hints: Number fields often contain 'count', 'total', 'length', 'size', 'num'. Array fields often contain 'files', 'items', 'list', 'array', 'entries', 'results'. IMPORTANT: For pattern field, only use if you can generate a VALID Go regex pattern. Invalid patterns will be skipped. Examples of valid patterns: '^success$', '^\\d+$', '^[A-Za-z0-9_]+$'. Do NOT use incomplete patterns. DB CHECKS — to gate on the db source of truth instead of a file, add a 'db' array alongside 'files': {db: [{name, sql (read-only SELECT against db/db.sqlite), min_rows?, max_rows?, checks? (same json_check shape, asserted on the FIRST result row, which is an object keyed by column)}]}. min_rows:1 requires the query returns results; max_rows:0 requires none (e.g. a query for FAIL rows must return 0); checks with path '$.column' + pattern assert a column value on the first row. PREFER db checks when the step writes its results to db/db.sqlite — they validate what was actually produced, so the step needs no hand-written output file that can drift from the db.",
				"properties": {
					"files": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file_name": {"type": "string", "description": "Path rules: a bare filename (e.g. 'results.json') is resolved under the step's execution folder. A path starting with 'db/' is resolved against the workflow-root 'db/' store; pre-validation accepts the file either there OR inside the step execution folder, since 'db/' and the step folder are the only two places a step may legally write. Paths starting with other workflow-root prefixes ('knowledgebase/', 'learnings/', 'planning/', etc.) are workflow-root-relative and must be present exactly at that location — those folders are written by dedicated agents, so a step-local copy is a bug and will fail validation."},
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
			,
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this step is being added. Captured into the plan changelog. Be specific — 'add new step' is weak; 'add step-4 (verify-payment) — eval iteration-3 showed 30% of records skip payment verification when step-3 fails silently' is good."
			}
		},
		"required": ["id", "title", "description", "context_dependencies","insert_after_step_id", "validation_schema", "reason"]
	}`
}

func getAddMessageSequenceStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "REQUIRED: Stable URL-friendly step ID."},
			"title": {"type": "string", "description": "REQUIRED: Short title for the message sequence step."},
			"description": {"type": "string", "description": "REQUIRED: The opening instruction AND objective. This IS EXECUTED as the first user turn (turn 0): it leads items[0] inside the same conversation — identical to how a todo_task's description is its first turn. Write it as an actionable opening instruction, not throwaway metadata (anything you put here runs). Then split the follow-up work into items[] (turns 1..N)."},
			"context_dependencies": {"type": "array", "items": {"type": "string"}, "description": "REQUIRED: Prior context files this sequence depends on. Use [] if none."},
			"context_output": {"type": "string", "description": "OPTIONAL: Summary/result file for later steps. Omit when the step writes its result to the db (validate via validation_schema.db)."},
			"items": {
				"type": "array",
				"description": "REQUIRED: Ordered queue of follow-up turns (turns 1..N; the step description is turn 0). Prefer multiple short user_message items over one large prompt. Use prevalidation items between turns as hard gates. Use kind=\"learning\" or write_access.learnings=true only for an intentional in-sequence learning write to learnings/_global/; the normal learning phase still runs only after the whole step based on step_config.",
				"items": {
					"type": "object",
					"properties": {
						"id": {"type": "string"},
						"type": {"type": "string", "description": "user_message, code, prevalidation, or foreach"},
						"kind": {"type": "string", "description": "Drives item-scoped write access. One of: learning, knowledgebase, db, or code (when code, db/kb/learnings write access is auto-inferred from output_files paths). Omit for a plain user_message item with no extra write access. Explicit write_access overrides kind."},
						"title": {"type": "string"},
						"message": {"type": "string", "description": "For user_message items: concise instruction for one turn, run in the same persistent conversation. Use plain work turns, or self-validation/interrogation turns (e.g. \"Did you actually call X? Quote its exact output. Did you actually produce Y?\") to make the step check its own work before a prevalidation gate. For foreach items: a Go text/template rendered once per row of source, with the row bound to '.' (e.g. 'Process {{.id}}: {{.task}}')."},
						"runtime": {"type": "string", "description": "For code items: python."},
						"script_path": {"type": "string", "description": "For code items: workspace-relative source script to execute."},
						"input_files": {"type": "array", "items": {"type": "string"}},
						"input_json": {"type": "object"},
						"output_files": {"type": "array", "items": {"type": "string"}},
						"write_access": {
							"type": "object",
							"description": "Item-scoped writes only. Reads for kb/db/learnings are always open. Folder-level booleans only — NO per-file path scoping (a \"paths\" list is rejected); db:true grants the whole db/ folder. Use learnings:true sparingly for deliberate in-sequence writes to learnings/_global/; prefer normal step-level learning for automatic post-step extraction.",
							"properties": {
								"knowledgebase": {"type": "boolean"},
								"db": {"type": "boolean"},
								"learnings": {"type": "boolean"}
							}
						},
						"on_failure": {
							"type": "object",
							"properties": {
								"action": {"type": "string", "description": "CODE ITEMS ONLY (ignored for user_message/prevalidation items): stop_step (default), repair_with_llm, or repair_same_session."},
								"max_retries": {"type": "number"}
							}
						},
						"save_repaired_script": {"type": "boolean"},
						"validation_schema": {"type": "object", "description": "For prevalidation items: backend validation schema (a hard gate). Interleave prevalidation items with DIFFERENT schemas between turns to gate distinct claims one at a time."},
						"prevalidation": {"type": "object", "description": "Alias for validation_schema on a prevalidation item."},
						"source_sql": {"type": "string", "description": "For foreach items: a read-only SQL query against db/db.sqlite (e.g. \"SELECT id, name FROM tasks WHERE status='pending'\"). The runtime runs it and sends one user_message turn per result row, with the row (an object keyed by column) bound to '.' in the message template. Use this to reliably process every row a prior step wrote to the db."},
						"max_iterations": {"type": "number", "description": "For foreach items: optional cap on rows processed (0 = all). Excess rows are skipped and logged, never silently dropped."}
					},
					"required": ["id", "type"]
				}
			},
			"next_step_id": {"type": "string", "description": "Optional next step ID or end."},
			"insert_after_step_id": {"type": "string", "description": "REQUIRED: Existing step ID to insert after, or empty string to insert first."},
			"validation_schema": {"type": "object"},
			"reason": {"type": "string", "description": "REQUIRED: One-sentence rationale for why this sequence step is being added."}
		},
		"required": ["id", "title", "description", "context_dependencies","items", "insert_after_step_id", "reason"]
	}`
}

func getUpdateMessageSequenceStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_step_id": {"type": "string", "description": "REQUIRED: ID of the message_sequence step to update."},
			"title": {"type": "string"},
			"description": {"type": "string"},
			"context_dependencies": {"type": "array", "items": {"type": "string"}},
			"context_output": {"type": "string"},
			"items": {"type": "array", "items": {"type": "object"}, "description": "Replace the ordered item queue."},
			"next_step_id": {"type": "string"},
			"validation_schema": {"type": "object"},
			"reason": {"type": "string", "description": "REQUIRED: One-sentence rationale for this update."}
		},
		"required": ["existing_step_id", "reason"]
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
				"description": "DO NOT SET for routing steps. Routing is deterministic-only and never executes an agent. If a probe/judgment is needed, add a prior regular step that writes route_selection.json, then route from that file."
			},
			"context_dependencies": {
				"type": "array",
				"items": {"type": "string"},
				"description": "REQUIRED: List of context files from previous steps that this step depends on. For prior-step routing decisions, include route_selection.json and ensure a prior step declares route_selection.json in context_output. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "Do not set for routing steps. Routing steps do not create outputs; a prior regular step should declare route_selection.json in its context_output when it produces the route decision."
			},
			"routing_question": {
				"type": "string",
				"description": "REQUIRED for compatibility/readability: Human-readable route decision prompt. It is not evaluated by an LLM at runtime; the selected route must come from caller route_selections, route_source_file, route_selection.json dependency, or default_route_id."
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
				"description": "OPTIONAL: Fallback route_id to use when no route_selection.json is available. Must match one of the route_ids in routes."
			},
			"route_source_file": {
				"type": "string",
				"description": "OPTIONAL: Explicit route_selection.json source file, usually a context output from a prior step. The file must contain {\"select_route\":\"<route_id>\"}."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string to insert at the beginning."
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this routing step is being added. Captured into the plan changelog."
			}
		},
		"required": ["id", "title", "routing_question", "routes", "context_dependencies", "insert_after_step_id", "reason"]
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
				"description": "DO NOT SET for routing steps. Routing is deterministic-only and never executes an agent. Use clear_description=true to remove a legacy routing description; put any probe/judgment in a prior regular step that writes route_selection.json."
			},
			"clear_description": {
				"type": "boolean",
				"description": "OPTIONAL: Set true to clear a legacy routing description during migration. New routing steps must keep description empty."
			},
			"context_dependencies": {
				"type": "array",
				"items": { "type": "string" },
				"description": "OPTIONAL: Updated context dependencies. Use route_selection.json when consuming a prior step's deterministic route decision."
			},
			"context_output": {
				"type": "string",
				"description": "Do not set for routing steps. Routing steps do not create outputs; route_selection.json should be produced by a prior regular step or preseeded by caller route_selections."
			},
			"routing_question": {
				"type": "string",
				"description": "OPTIONAL: Updated human-readable routing prompt. Runtime does not LLM-evaluate this field."
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
			},
			"route_source_file": {
				"type": "string",
				"description": "OPTIONAL: Updated explicit route_selection.json source file produced by a prior step."
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this routing step is being updated. Captured into the plan changelog."
			}
		},
		"required": ["existing_step_id", "reason"]
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
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this human-input step is being added. Captured into the plan changelog."
			}
		},
		"required": ["id", "title", "question", "next_step_id", "insert_after_step_id", "reason"]
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
			"description": {
				"type": "string",
				"description": "REQUIRED: The overall objective AND the orchestrator's opening instruction. This IS EXECUTED as the orchestrator's first user turn (turn 0) — write it as an actionable task framing, not throwaway metadata. The orchestrator breaks it into tasks and delegates via routes."
			},
			"context_dependencies": {
				"type": "array",
				"items": {"type": "string"},
				"description": "REQUIRED: List of context files from previous steps. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "REQUIRED: Context file this step will create with final summary."
			},
			"validation_schema": {
				"type": "object",
				"description": "OPTIONAL: Validation schema for the step output",
				"properties": {
					"files": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file_name": {"type": "string", "description": "Path rules: a bare filename (e.g. 'results.json') is resolved under the step's execution folder. A path starting with 'db/' is resolved against the workflow-root 'db/' store; pre-validation accepts the file either there OR inside the step execution folder, since 'db/' and the step folder are the only two places a step may legally write. Paths starting with other workflow-root prefixes ('knowledgebase/', 'learnings/', 'planning/', etc.) are workflow-root-relative and must be present exactly at that location — those folders are written by dedicated agents, so a step-local copy is a bug and will fail validation."},
								"must_exist": {"type": "boolean"},
								"json_checks": {"type": "array", "items": {"type": "object"}}
							}
						}
					}
				}
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
								"description": {"type": "string", "description": "REQUIRED: What this specialized agent does AND its standing brief. This IS EXECUTED as the agent's opening instruction (turn 0) on the first call — the orchestrator's per-call call_sub_agent instructions are added on top. Write it as an actionable brief, not throwaway metadata."},
								"context_dependencies": {"type": "array", "items": {"type": "string"}},
								"context_output": {"type": "string", "description": "OPTIONAL: Context file this step creates. Omit when the step writes to the db (validate via validation_schema.db)."},
								"predefined_routes": {"type": "array", "description": "When type='todo_task', nested predefined routes for the child todo task."},
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
													"file_name": {"type": "string", "description": "Path rules: a bare filename (e.g. 'results.json') is resolved under the step's execution folder. A path starting with 'db/' is resolved against the workflow-root 'db/' store; pre-validation accepts the file either there OR inside the step execution folder, since 'db/' and the step folder are the only two places a step may legally write. Paths starting with other workflow-root prefixes ('knowledgebase/', 'learnings/', 'planning/', etc.) are workflow-root-relative and must be present exactly at that location — those folders are written by dedicated agents, so a step-local copy is a bug and will fail validation."},
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
			"messages": {
				"type": "array",
				"description": "OPTIONAL: Scripted message sequence for long, multi-phase work on one orchestrator step. After the orchestrator's first turn, each entry is fed into the SAME orchestrator conversation in order, so it keeps going with full memory of prior turns and sub-agent results. Runs within one execution; NO persistence/re-entry. A foreach entry iterates a db array (e.g. one a prior step wrote) and feeds one orchestrator turn per row — reliable enumeration of every row. For a specialist that resumes across the orchestrator's own repeated calls, use a message_sequence route instead.",
				"items": {
					"type": "object",
					"properties": {
						"id": {"type": "string"},
						"type": {"type": "string", "description": "user_message (alias: message; default), prevalidation, or foreach. Same message_sequence item shape, but the orchestrator runs only these conversational kinds — code/file items must go to a sub-agent route."},
						"message": {"type": "string", "description": "message entries: the instruction for one orchestrator turn (e.g. a follow-up phase, or 'now verify X and fix any gaps'). foreach entries: a Go text/template rendered once per row of source, row bound to '.' (e.g. 'Handle task {{.id}}: {{.desc}}')."},
						"validation_schema": {"type": "object", "description": "prevalidation entries: a hard gate checked between turns. On failure the orchestrator receives the failures as a corrective turn and retries up to max_corrections."},
						"max_corrections": {"type": "number", "description": "prevalidation entries only: corrective orchestrator turns allowed on gate failure (default 1)."},
						"source_sql": {"type": "string", "description": "foreach entries: a read-only SQL query against db/db.sqlite (e.g. \"SELECT id, name FROM tasks WHERE status='pending'\"). The orchestrator gets one turn per result row, with the row bound to '.' — reliable processing of every row a prior step wrote to the db."},
						"max_iterations": {"type": "number", "description": "foreach entries: optional cap on rows processed (0 = all)."}
					},
					"required": ["type"]
				}
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string to insert at the beginning."
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this todo-task step is being added. Captured into the plan changelog."
			}
		},
		"required": ["id", "title", "description", "context_dependencies","next_step_id", "insert_after_step_id", "reason"]
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
			"description": {
				"type": "string",
				"description": "OPTIONAL: Updated description of the overall objective"
			},
			"context_dependencies": {
				"type": "array",
				"items": {"type": "string"},
				"description": "OPTIONAL: Updated list of context files from previous steps"
			},
			"context_output": {
				"type": "string",
				"description": "OPTIONAL: Updated context file this step will create"
			},
			"validation_schema": {
				"type": "object",
				"description": "OPTIONAL: Updated validation schema"
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
								"context_dependencies": {"type": "array", "items": {"type": "string"}},
								"context_output": {"type": "string"},
								"predefined_routes": {"type": "array", "description": "When type='todo_task', nested predefined routes for the child todo task."},
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
			},
			"messages": {
				"type": "array",
				"description": "OPTIONAL: Replaces the scripted message sequence. After the orchestrator's first turn, each entry is fed into the same orchestrator conversation in order. Entries use the same message_sequence item shape {id, type, message, validation_schema, max_corrections, source_sql, max_iterations}, but the orchestrator runs only the CONVERSATIONAL kinds — type: user_message (or message), prevalidation, foreach. It cannot run code/file items itself; delegate that to a sub-agent route.",
				"items": {"type": "object"}
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this todo-task step is being updated. Captured into the plan changelog."
			}
		},
		"required": ["existing_step_id", "reason"]
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
					"orphan_step_ref": {
						"type": "string",
						"description": "OPTIONAL: ID of a reusable orphan step from orphan_steps in this same plan. Use this instead of sub_agent_step when you want the route to reuse a shared orphan step definition that is shared_with this parent orchestrator."
					},
					"sub_agent_step": {
						"type": "object",
						"description": "OPTIONAL: The inline sub-agent step definition. This agent has learning and prevalidation. Use type='regular' for a focused execution agent. Use type='todo_task' ONLY when this route needs its own nested multi-phase orchestrator (1 level of nesting supported). IMPORTANT: A nested todo_task's routes must use type='regular' — do NOT nest a todo_task inside a todo_task inside a todo_task (2+ levels not supported). Omit this when using orphan_step_ref.",
						"properties": {
							"type": {"type": "string", "description": "REQUIRED: Step type. Use 'regular' for standard execution. Use 'todo_task' for a nested orchestrator that manages multiple phases via its own routes. Maximum 1 level of todo_task nesting — a nested todo_task's sub_agent_step routes must always be 'regular'."},
							"id": {"type": "string", "description": "REQUIRED: Stable step ID for the sub-agent step"},
							"title": {"type": "string", "description": "REQUIRED: Title of the sub-agent step"},
							"description": {"type": "string", "description": "REQUIRED: What this specialized agent does AND its standing brief. This IS EXECUTED as the agent's opening instruction (turn 0) on the first call — the orchestrator's per-call call_sub_agent instructions are added on top. Write it as an actionable brief, not throwaway metadata."},
							"context_dependencies": {"type": "array", "items": {"type": "string"}},
							"context_output": {"type": "string", "description": "OPTIONAL: Context file this step creates. Omit when the step writes to the db (validate via validation_schema.db)."},
							"todo_task_step": {"type": "object", "description": "When type='todo_task': the nested orchestrator's inner regular step metadata."},
							"predefined_routes": {"type": "array", "description": "When type='todo_task': predefined routes for the nested orchestrator. Each route's sub_agent_step must be type='regular'."},
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
				"required": ["route_id", "route_name", "condition"]
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this todo-task route is being added. Captured into the plan changelog."
			}
		},
		"required": ["parent_step_id", "new_route", "reason"]
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
			"orphan_step_ref": {
				"type": "string",
				"description": "OPTIONAL: Reference a reusable orphan step from orphan_steps in this same plan. When set, the route will resolve that orphan step for this orchestrator instead of using an inline sub_agent_step."
			},
			"sub_agent_step": {
				"type": "object",
				"description": "OPTIONAL: Updated inline sub-agent step. Use type='regular' for standard execution. Use type='todo_task' for a 1-level nested orchestrator. A nested todo_task's own routes must be type='regular' (2+ levels not supported). Omit this when using orphan_step_ref.",
				"properties": {
					"type": {"type": "string", "description": "Use 'regular' or 'todo_task' (1 level max). A nested todo_task's routes must use 'regular'."},
					"id": {"type": "string"},
					"title": {"type": "string"},
					"description": {"type": "string"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}},
					"context_output": {"type": "string"},
					"todo_task_step": {"type": "object", "description": "When type='todo_task': nested orchestrator inner step metadata."},
					"predefined_routes": {"type": "array", "description": "When type='todo_task': nested routes — each must be type='regular'."},
						"validation_schema": {"type": "object"}
				},
				"required": ["type", "id", "title"]
			},
			"context_to_pass": {
				"type": "string",
				"description": "OPTIONAL: Updated context to pass to the sub-agent."
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this todo-task route is being updated. Captured into the plan changelog."
			}
		},
		"required": ["parent_step_id", "existing_route_id", "reason"]
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
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this todo-task route is being deleted. Captured into the plan changelog."
			}
		},
		"required": ["parent_step_id", "deleted_route_id", "reason"]
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
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this human-input step is being updated. Captured into the plan changelog."
			}
		},
		"required": ["existing_step_id", "reason"]
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
				"description": "REQUIRED: Structured validation schema for fast code-based pre-validation. You MUST generate this by parsing the step description and extracting file names, field requirements, and validation rules. This enables pre-validation before LLM validation (improves speed by 50-70%). Structure: {files: [{file_name: string, must_exist: boolean, json_checks: [{path: string (JSONPath like $.field_name), must_exist: boolean, value_type?: string (string/number/boolean/array/object), min_length?: number, max_length?: number, pattern?: string (VALID Go regex - must compile with regexp.Compile, ensure balanced parentheses, escape special chars), min_value?: number, max_value?: number, consistency_check?: {type: string (array_length/equals/greater_than/less_than), compare_with_path: string}}]}]}. Example: If the step description requires 'File results.json contains status field and count field equals items array length', generate schema with file_name: 'results.json', json_checks: [{path: '$.status', must_exist: true}, {path: '$.count', must_exist: true, consistency_check: {type: 'array_length', compare_with_path: '$.items'}}]. CRITICAL FOR array_length CONSISTENCY CHECKS - AVOID COMMON MISTAKE: For array_length checks, path MUST point to COUNT/NUMBER field (e.g., '$.count', '$.total_expected_count', '$.length'), and compare_with_path MUST point to ARRAY field (e.g., '$.items', '$.downloaded_files', '$.files'). CORRECT examples: {path: '$.count', consistency_check: {type: 'array_length', compare_with_path: '$.items'}}, {path: '$.total_expected_count', consistency_check: {type: 'array_length', compare_with_path: '$.downloaded_files'}}. INCORRECT (SWAPPED) - DO NOT DO THIS: {path: '$.items', consistency_check: {type: 'array_length', compare_with_path: '$.count'}} ❌ WRONG - paths are swapped! Field name hints: Number fields often contain 'count', 'total', 'length', 'size', 'num'. Array fields often contain 'files', 'items', 'list', 'array', 'entries', 'results'. IMPORTANT: For pattern field, only use if you can generate a VALID Go regex pattern. Invalid patterns will be skipped. Examples of valid patterns: '^success$', '^\\d+$', '^[A-Za-z0-9_]+$'. Do NOT use incomplete patterns. DB CHECKS — to gate on the db source of truth instead of a file, add a 'db' array alongside 'files': {db: [{name, sql (read-only SELECT against db/db.sqlite), min_rows?, max_rows?, checks? (same json_check shape, asserted on the FIRST result row, which is an object keyed by column)}]}. min_rows:1 requires the query returns results; max_rows:0 requires none (e.g. a query for FAIL rows must return 0); checks with path '$.column' + pattern assert a column value on the first row. PREFER db checks when the step writes its results to db/db.sqlite — they validate what was actually produced, so the step needs no hand-written output file that can drift from the db.",
				"properties": {
					"files": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file_name": {"type": "string", "description": "Path rules: a bare filename (e.g. 'results.json') is resolved under the step's execution folder. A path starting with 'db/' is resolved against the workflow-root 'db/' store; pre-validation accepts the file either there OR inside the step execution folder, since 'db/' and the step folder are the only two places a step may legally write. Paths starting with other workflow-root prefixes ('knowledgebase/', 'learnings/', 'planning/', etc.) are workflow-root-relative and must be present exactly at that location — those folders are written by dedicated agents, so a step-local copy is a bug and will fail validation."},
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
			},
			"reason": {
				"type": "string",
				"description": "REQUIRED: One-sentence rationale for why this validation schema is being updated. Captured into the plan changelog."
			}
		},
		"required": ["existing_step_id", "validation_schema", "reason"]
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
	if err := resolvePlanOrphanStepRefs(&plan); err != nil {
		return nil, fmt.Errorf("failed to resolve orphan step references in plan.json: %w", err)
	}
	if err := validateLoadedPlanStructure(&plan); err != nil {
		return nil, fmt.Errorf("plan.json uses an invalid or legacy format: %w", err)
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

	validationJSON, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("failed to marshal plan for validation: %w", err)
	}
	var validationPlan PlanningResponse
	if err := json.Unmarshal(validationJSON, &validationPlan); err != nil {
		return fmt.Errorf("failed to unmarshal plan for validation: %w", err)
	}
	if err := resolvePlanOrphanStepRefs(&validationPlan); err != nil {
		return fmt.Errorf("plan validation failed: %w", err)
	}
	if err := validateLoadedPlanStructure(&validationPlan); err != nil {
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
	case "message_sequence":
		var step MessageSequencePlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return nil, fmt.Errorf("failed to parse message_sequence step: %w", err)
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
	case "message_sequence":
		var step MessageSequencePlanStep
		if err := json.Unmarshal(stepData, &step); err != nil {
			return nil, fmt.Errorf("failed to parse message_sequence step: %w", err)
		}
		step.Type = StepTypeMessageSeq
		typedStep = &step
	default:
		return nil, fmt.Errorf("unknown step type %q (must be: regular, human_input, todo_task, routing, or message_sequence)", stepType)
	}

	return typedStep, nil
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

var _ = updateValidationSchemaOnStep

// updateValidationSchemaOnStep updates validation schema on any step type
func updateValidationSchemaOnStep(step PlanStepInterface, schema *ValidationSchema) {
	switch s := step.(type) {
	case *RegularPlanStep:
		s.ValidationSchema = schema
	case *HumanInputPlanStep:
		s.ValidationSchema = schema
	case *TodoTaskPlanStep:
		s.ValidationSchema = schema
	case *RoutingPlanStep:
		s.ValidationSchema = schema
	case *MessageSequencePlanStep:
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
		// No type-specific fields to diff (loop fields removed)

	case *MessageSequencePlanStep:
		if newS, ok := newStep.(*MessageSequencePlanStep); ok {
			oldItemsJSON, _ := json.Marshal(oldS.Items)
			newItemsJSON, _ := json.Marshal(newS.Items)
			if string(oldItemsJSON) != string(newItemsJSON) {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   stepID,
					Field:    prefix + ".items",
					OldValue: string(oldItemsJSON),
					NewValue: string(newItemsJSON),
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
		if partialUpdate.ContextDependencies != nil {
			updated.ContextDependencies = partialUpdate.ContextDependencies
		}
		if partialUpdate.ContextOutput != "" {
			updated.ContextOutput = FlexibleContextOutput(partialUpdate.ContextOutput)
		}
		// Loop fields ignored (feature removed)
		if partialUpdate.ValidationSchema != nil {
			updated.ValidationSchema = partialUpdate.ValidationSchema
		}
		// Validation schema is LLM-generated only - no code-based auto-generation
		return &updated

	case *HumanInputPlanStep:
		updated := *step
		if partialUpdate.Title != "" {
			updated.Title = partialUpdate.Title
		}
		if partialUpdate.Description != "" {
			updated.Description = partialUpdate.Description
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

	case *MessageSequencePlanStep:
		updated := *step
		if partialUpdate.Title != "" {
			updated.Title = partialUpdate.Title
		}
		if partialUpdate.Description != "" {
			updated.Description = partialUpdate.Description
		}
		if partialUpdate.ContextDependencies != nil {
			updated.ContextDependencies = partialUpdate.ContextDependencies
		}
		if partialUpdate.ContextOutput != "" {
			updated.ContextOutput = FlexibleContextOutput(partialUpdate.ContextOutput)
		}
		if partialUpdate.Items != nil {
			updated.Items = partialUpdate.Items
		}
		if partialUpdate.NextStepID != "" {
			updated.NextStepID = partialUpdate.NextStepID
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
		if partialUpdate.Description != "" {
			updated.Description = partialUpdate.Description
		}
		if partialUpdate.ContextDependencies != nil {
			updated.ContextDependencies = partialUpdate.ContextDependencies
		}
		if partialUpdate.ContextOutput != "" {
			updated.ContextOutput = partialUpdate.ContextOutput
		}
		// BACKWARDS COMPATIBILITY: if legacy todo_task_step map is present, extract fields from it
		if partialUpdate.TodoTaskStep != nil {
			if desc, ok := partialUpdate.TodoTaskStep["description"].(string); ok && desc != "" {
				updated.Description = desc
			}
			if title, ok := partialUpdate.TodoTaskStep["title"].(string); ok && title != "" {
				updated.Title = title
			}
			if contextDeps, ok := partialUpdate.TodoTaskStep["context_dependencies"].([]interface{}); ok {
				updated.ContextDependencies = make([]string, 0, len(contextDeps))
				for _, dep := range contextDeps {
					if depStr, ok := dep.(string); ok {
						updated.ContextDependencies = append(updated.ContextDependencies, depStr)
					}
				}
			}
			if contextOutput, ok := partialUpdate.TodoTaskStep["context_output"]; ok {
				if contextOutputMap, ok := contextOutput.(map[string]interface{}); ok {
					contextOutputJSON, _ := json.Marshal(contextOutputMap)
					json.Unmarshal(contextOutputJSON, &updated.ContextOutput)
				} else if contextOutputStr, ok := contextOutput.(string); ok {
					updated.ContextOutput = FlexibleContextOutput(contextOutputStr)
				}
			}
			if validationSchema, ok := partialUpdate.TodoTaskStep["validation_schema"]; ok {
				if validationSchemaMap, ok := validationSchema.(map[string]interface{}); ok {
					validationSchemaJSON, _ := json.Marshal(validationSchemaMap)
					var vs ValidationSchema
					if json.Unmarshal(validationSchemaJSON, &vs) == nil {
						updated.ValidationSchema = &vs
					}
				}
			}
		}
		if partialUpdate.PredefinedRoutes != nil {
			updated.PredefinedRoutes = make([]PlanOrchestrationRoute, len(partialUpdate.PredefinedRoutes))
			for i, route := range partialUpdate.PredefinedRoutes {
				updated.PredefinedRoutes[i] = route
			}
		}
		if partialUpdate.Messages != nil {
			updated.Messages = partialUpdate.Messages
		}
		if partialUpdate.NextStepID != "" {
			updated.NextStepID = partialUpdate.NextStepID
		}
		if partialUpdate.ValidationSchema != nil {
			updated.ValidationSchema = partialUpdate.ValidationSchema
		}
		return &updated

	case *RoutingPlanStep:
		updated := *step
		if partialUpdate.Title != "" {
			updated.Title = partialUpdate.Title
		}
		if partialUpdate.ClearDescription {
			updated.Description = ""
		} else if partialUpdate.Description != "" {
			updated.Description = partialUpdate.Description
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
		if partialUpdate.RouteSourceFile != "" {
			updated.RouteSourceFile = partialUpdate.RouteSourceFile
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
		case *TodoTaskPlanStep:
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
		case *TodoTaskPlanStep:
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
	if partialUpdate.ClearDescription {
		changedFields = append(changedFields, "description")
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "description",
			OldValue: existingStep.GetDescription(),
			NewValue: "",
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
	if partialUpdate.Items != nil {
		changedFields = append(changedFields, "items")
		oldItemsJSON := "[]"
		if sequenceStep, ok := existingStep.(*MessageSequencePlanStep); ok {
			oldBytes, _ := json.Marshal(sequenceStep.Items)
			oldItemsJSON = string(oldBytes)
		}
		newBytes, _ := json.Marshal(partialUpdate.Items)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "items",
			OldValue: oldItemsJSON,
			NewValue: string(newBytes),
		})
	}
	// Loop fields ignored (feature removed)
	// Legacy todo_task_step field — extract fields and track them as top-level changes
	if partialUpdate.TodoTaskStep != nil {
		if desc, ok := partialUpdate.TodoTaskStep["description"].(string); ok && desc != "" {
			changedFields = append(changedFields, "description (via todo_task_step)")
			if todoTaskStep, ok := existingStep.(*TodoTaskPlanStep); ok {
				*fieldChanges = append(*fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "description",
					OldValue: todoTaskStep.Description,
					NewValue: desc,
				})
			}
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
		switch step := existingStep.(type) {
		case *TodoTaskPlanStep:
			oldNextStepID = step.NextStepID
		case *HumanInputPlanStep:
			oldNextStepID = step.NextStepID
		case *MessageSequencePlanStep:
			oldNextStepID = step.NextStepID
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "next_step_id",
			OldValue: oldNextStepID,
			NewValue: partialUpdate.NextStepID,
		})
	}
	if partialUpdate.Messages != nil {
		changedFields = append(changedFields, "messages")
		oldMessagesJSON := "[]"
		if todoTaskStep, ok := existingStep.(*TodoTaskPlanStep); ok {
			oldBytes, _ := json.Marshal(todoTaskStep.Messages)
			oldMessagesJSON = string(oldBytes)
		}
		newBytes, _ := json.Marshal(partialUpdate.Messages)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "messages",
			OldValue: oldMessagesJSON,
			NewValue: string(newBytes),
		})
	}
	if partialUpdate.Question != "" {
		changedFields = append(changedFields, "question")
		oldQuestion := ""
		if humanInputStep, ok := existingStep.(*HumanInputPlanStep); ok {
			oldQuestion = humanInputStep.Question
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "question",
			OldValue: oldQuestion,
			NewValue: partialUpdate.Question,
		})
	}
	if partialUpdate.VariableName != "" {
		changedFields = append(changedFields, "variable_name")
		oldVariableName := ""
		if humanInputStep, ok := existingStep.(*HumanInputPlanStep); ok {
			oldVariableName = humanInputStep.VariableName
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "variable_name",
			OldValue: oldVariableName,
			NewValue: partialUpdate.VariableName,
		})
	}
	if partialUpdate.ResponseType != "" {
		changedFields = append(changedFields, "response_type")
		oldResponseType := ""
		if humanInputStep, ok := existingStep.(*HumanInputPlanStep); ok {
			oldResponseType = humanInputStep.ResponseType
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "response_type",
			OldValue: oldResponseType,
			NewValue: partialUpdate.ResponseType,
		})
	}
	if partialUpdate.Options != nil {
		changedFields = append(changedFields, "options")
		oldOptionsJSON := "[]"
		if humanInputStep, ok := existingStep.(*HumanInputPlanStep); ok {
			oldBytes, _ := json.Marshal(humanInputStep.Options)
			oldOptionsJSON = string(oldBytes)
		}
		newBytes, _ := json.Marshal(partialUpdate.Options)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "options",
			OldValue: oldOptionsJSON,
			NewValue: string(newBytes),
		})
	}
	if partialUpdate.IfYesNextStepID != "" {
		changedFields = append(changedFields, "if_yes_next_step_id")
		oldNextStepID := ""
		if humanInputStep, ok := existingStep.(*HumanInputPlanStep); ok {
			oldNextStepID = humanInputStep.IfYesNextStepID
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "if_yes_next_step_id",
			OldValue: oldNextStepID,
			NewValue: partialUpdate.IfYesNextStepID,
		})
	}
	if partialUpdate.IfNoNextStepID != "" {
		changedFields = append(changedFields, "if_no_next_step_id")
		oldNextStepID := ""
		if humanInputStep, ok := existingStep.(*HumanInputPlanStep); ok {
			oldNextStepID = humanInputStep.IfNoNextStepID
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "if_no_next_step_id",
			OldValue: oldNextStepID,
			NewValue: partialUpdate.IfNoNextStepID,
		})
	}
	if partialUpdate.OptionRoutes != nil {
		changedFields = append(changedFields, "option_routes")
		oldRoutesJSON := "{}"
		if humanInputStep, ok := existingStep.(*HumanInputPlanStep); ok {
			oldBytes, _ := json.Marshal(humanInputStep.OptionRoutes)
			oldRoutesJSON = string(oldBytes)
		}
		newBytes, _ := json.Marshal(partialUpdate.OptionRoutes)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "option_routes",
			OldValue: oldRoutesJSON,
			NewValue: string(newBytes),
		})
	}
	if partialUpdate.RoutingQuestion != "" {
		changedFields = append(changedFields, "routing_question")
		oldQuestion := ""
		if routingStep, ok := existingStep.(*RoutingPlanStep); ok {
			oldQuestion = routingStep.RoutingQuestion
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "routing_question",
			OldValue: oldQuestion,
			NewValue: partialUpdate.RoutingQuestion,
		})
	}
	if partialUpdate.Routes != nil {
		changedFields = append(changedFields, "routes")
		oldRoutesJSON := "[]"
		if routingStep, ok := existingStep.(*RoutingPlanStep); ok {
			oldBytes, _ := json.Marshal(routingStep.Routes)
			oldRoutesJSON = string(oldBytes)
		}
		newBytes, _ := json.Marshal(partialUpdate.Routes)
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "routes",
			OldValue: oldRoutesJSON,
			NewValue: string(newBytes),
		})
	}
	if partialUpdate.DefaultRouteID != "" {
		changedFields = append(changedFields, "default_route_id")
		oldDefaultRouteID := ""
		if routingStep, ok := existingStep.(*RoutingPlanStep); ok {
			oldDefaultRouteID = routingStep.DefaultRouteID
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "default_route_id",
			OldValue: oldDefaultRouteID,
			NewValue: partialUpdate.DefaultRouteID,
		})
	}
	if partialUpdate.RouteSourceFile != "" {
		changedFields = append(changedFields, "route_source_file")
		oldRouteSourceFile := ""
		if routingStep, ok := existingStep.(*RoutingPlanStep); ok {
			oldRouteSourceFile = routingStep.RouteSourceFile
		}
		*fieldChanges = append(*fieldChanges, PlanFieldChange{
			StepID:   partialUpdate.ExistingStepID,
			Field:    "route_source_file",
			OldValue: oldRouteSourceFile,
			NewValue: partialUpdate.RouteSourceFile,
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

func planStepUpdateRequiresDependentArtifactReview(fieldChanges []PlanFieldChange) bool {
	return len(fieldChanges) > 0
}

func planStepUpdateInvalidatesDescriptionReview(fieldChanges []PlanFieldChange) bool {
	for _, change := range fieldChanges {
		field := change.Field
		if field == "description" ||
			field == "context_dependencies" ||
			field == "context_output" ||
			field == "items" ||
			field == "messages" ||
			field == "next_step_id" ||
			field == "validation_schema" ||
			field == "routing_question" ||
			field == "routes" ||
			field == "default_route_id" ||
			field == "route_source_file" ||
			field == "question" ||
			field == "variable_name" ||
			field == "response_type" ||
			field == "options" ||
			field == "if_yes_next_step_id" ||
			field == "if_no_next_step_id" ||
			field == "option_routes" ||
			strings.HasPrefix(field, "predefined_routes") ||
			strings.Contains(field, ".sub_agent_step") {
			return true
		}
	}
	return false
}

func clearDescriptionReviewedAfterPlanUpdate(ctx context.Context, workspacePath, stepID string, fieldChanges []PlanFieldChange, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) (bool, error) {
	if !planStepUpdateInvalidatesDescriptionReview(fieldChanges) {
		return false, nil
	}

	configs, err := readStepConfigViaFileCallback(ctx, workspacePath, readFile)
	if err != nil {
		return false, err
	}
	for i := range configs {
		if configs[i].ID != stepID {
			continue
		}
		if configs[i].AgentConfigs == nil || configs[i].AgentConfigs.DescriptionReviewed == nil {
			return false, nil
		}
		clearStepConfigField(&configs[i], "description_reviewed")
		if err := writeStepConfigViaFileCallback(ctx, workspacePath, configs, writeFile); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func buildPlanStepDependentArtifactReviewNotice(stepID string, fieldChanges []PlanFieldChange, descriptionReviewCleared bool) string {
	if !planStepUpdateRequiresDependentArtifactReview(fieldChanges) {
		return ""
	}

	changed := make([]string, 0, len(fieldChanges))
	seen := make(map[string]bool, len(fieldChanges))
	for _, change := range fieldChanges {
		if change.Field == "" || seen[change.Field] {
			continue
		}
		seen[change.Field] = true
		changed = append(changed, change.Field)
	}

	var b strings.Builder
	b.WriteString("\n\nDependent artifact review required")
	if len(changed) > 0 {
		b.WriteString(" for changed fields: ")
		b.WriteString(strings.Join(changed, ", "))
	}
	b.WriteString(".\n")
	if descriptionReviewCleared {
		b.WriteString("- Cleared step_config.agent_configs.description_reviewed because the reviewed step contract may now be stale.\n")
	}
	b.WriteString("- Pre-validation: confirm validation_schema still matches the new output files/fields; update plan validation_schema or step_config validation_schema if needed.\n")
	b.WriteString("- Learnings: review learning_objective, learnings_access, lock_learnings, and any learnings/_global or learnings/" + stepID + " content for stale execution know-how.\n")
	b.WriteString("- DB: if output/state shape changed, update db/README.md, the db/db.sqlite table schema/writers/upsert rules, and any report widgets (sql) that read those columns.\n")
	b.WriteString("- KB: if business context or notes consumed/produced changed, update knowledgebase_access, knowledgebase_contribution, and description references.\n")
	b.WriteString("- Scripted code: if this step uses scripted/code execution or learnings/" + stepID + "/main.py exists, patch or regenerate main.py, or delete stale script state when returning to agentic execution.\n")
	b.WriteString("- Downstream wiring: if semantics changed, review reports/report_plan.json, evaluation/evaluation_plan.json, routes, and downstream context_dependencies.\n")
	b.WriteString("- Before marking the plan done, run get_workflow_command_guidance(kind=\"review-artifact-drift\", focus=\"" + stepID + "\").")
	return b.String()
}

func handlePlanStepDependentArtifactReview(ctx context.Context, workspacePath, stepID string, fieldChanges []PlanFieldChange, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) string {
	if !planStepUpdateRequiresDependentArtifactReview(fieldChanges) {
		return ""
	}

	descriptionReviewCleared, err := clearDescriptionReviewedAfterPlanUpdate(ctx, workspacePath, stepID, fieldChanges, readFile, writeFile)
	if err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to clear description_reviewed after updating step %s: %v", stepID, err))
	}
	return buildPlanStepDependentArtifactReviewNotice(stepID, fieldChanges, descriptionReviewCleared)
}

func buildAddedStepArtifactSetupNotice(stepID, stepType string) string {
	var b strings.Builder
	b.WriteString("\n\nNew step artifact setup required.\n")
	b.WriteString("- Step config: decide execution_llm/tier, servers/tools, browser mode, skills, secrets, and description_reviewed in planning/step_config.json.\n")
	b.WriteString("- Pre-validation: add or confirm validation_schema for required inputs/outputs and generated files.\n")
	b.WriteString("- Learnings: decide learnings_access, learning_objective, lock_learnings, and whether this step should write reusable execution know-how.\n")
	b.WriteString("- DB: decide whether the step reads/writes db/ files; update db/README.md and schemas/merge rules if it does.\n")
	b.WriteString("- KB: decide knowledgebase_access and knowledgebase_contribution for business context and notes.\n")
	b.WriteString("- Scripted code: if " + stepType + " step " + stepID + " should run code, create or review learnings/" + stepID + "/main.py and set code execution config; otherwise make sure no stale script is implied.\n")
	b.WriteString("- Downstream wiring: connect routes/next_step_id/context_dependencies, and update reports/report_plan.json or evaluation/evaluation_plan.json if this step affects outputs.\n")
	b.WriteString("- Before marking the plan done, run get_workflow_command_guidance(kind=\"review-artifact-drift\", focus=\"" + stepID + "\").")
	return b.String()
}

func buildDeletedStepArtifactCleanupNotice(deletedIDs []string, prunedConfigIDs []string) string {
	var b strings.Builder
	b.WriteString("\n\nDeleted step artifact cleanup required")
	if len(deletedIDs) > 0 {
		b.WriteString(" for: ")
		b.WriteString(strings.Join(deletedIDs, ", "))
	}
	b.WriteString(".\n")
	if len(prunedConfigIDs) > 0 {
		b.WriteString("- Removed matching planning/step_config.json entries: " + strings.Join(prunedConfigIDs, ", ") + ".\n")
	} else {
		b.WriteString("- Step config: confirm no stale planning/step_config.json entries remain for the deleted IDs.\n")
	}
	b.WriteString("- Plan wiring: remove or reroute next_step_id, routes, predefined_routes, and downstream context_dependencies that referenced deleted steps.\n")
	b.WriteString("- Pre-validation: remove validation schemas that validate deleted-step outputs and update schemas that depended on them.\n")
	b.WriteString("- Learnings/code: remove or archive stale learnings/<step-id>/ content and main.py scripts, unless intentionally kept as reusable docs.\n")
	b.WriteString("- DB/KB: remove stale db writers/readers, db/README.md mentions, knowledgebase references, and contribution rules tied to deleted steps.\n")
	b.WriteString("- Reports/evals/docs: update reports/report_plan.json, evaluation/evaluation_plan.json, docs, and widgets that referenced deleted outputs.\n")
	b.WriteString("- Before marking the plan done, run get_workflow_command_guidance(kind=\"review-artifact-drift\", focus=\"deleted steps\").")
	return b.String()
}

func buildTodoTaskRouteArtifactReviewNotice(parentStepID, routeID, action string, descriptionReviewCleared bool) string {
	var b strings.Builder
	b.WriteString("\n\nTodo route artifact review required for ")
	b.WriteString(action)
	b.WriteString(" route ")
	b.WriteString(routeID)
	b.WriteString(" under ")
	b.WriteString(parentStepID)
	b.WriteString(".\n")
	if descriptionReviewCleared {
		b.WriteString("- Cleared the parent step description_reviewed flag because its orchestration contract changed.\n")
	}
	switch action {
	case "added":
		b.WriteString("- Route setup: configure step_config, tools/servers/skills, prevalidation, learnings, KB, DB, and optional learnings/" + routeID + "/main.py for the new nested agent.\n")
	case "deleted":
		b.WriteString("- Route cleanup: remove stale step_config, learnings/" + routeID + ", scripted main.py, DB/KB/report/eval references, and downstream context dependencies for the removed nested agent.\n")
	default:
		b.WriteString("- Route update: review route condition/context_to_pass/sub-agent description against prevalidation, learnings, DB, KB, scripts, reports/evals, and downstream dependencies.\n")
	}
	b.WriteString("- Before marking the plan done, run get_workflow_command_guidance(kind=\"review-artifact-drift\", focus=\"" + parentStepID + "\").")
	return b.String()
}

func handleTodoTaskRouteArtifactReview(ctx context.Context, workspacePath, parentStepID, routeID, action string, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) string {
	fieldChanges := []PlanFieldChange{{
		StepID: parentStepID,
		Field:  "predefined_routes",
	}}
	descriptionReviewCleared, err := clearDescriptionReviewedAfterPlanUpdate(ctx, workspacePath, parentStepID, fieldChanges, readFile, writeFile)
	if err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to clear description_reviewed after %s route %s on step %s: %v", action, routeID, parentStepID, err))
	}
	return buildTodoTaskRouteArtifactReviewNotice(parentStepID, routeID, action, descriptionReviewCleared)
}

// createUpdateRegularStepExecutor creates an executor function for update_regular_step tool
func createUpdateRegularStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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

		// Track per-field changes — passed to the plan changelog after a
		// successful write.
		fieldChanges := make([]PlanFieldChange, 0)

		stepIndex, _, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateStepIDUniqueness(plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateCrossPlanStepIDUniqueness(ctx, workspacePath, readFile, plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:    "update_regular_step",
			Reason:  reason,
			StepIDs: []string{partialUpdate.ExistingStepID},
			Changes: fieldChanges,
		}, readFile, writeFile, logger)

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", partialUpdate.ExistingStepID))
			}
		}

		dependentReviewNotice := handlePlanStepDependentArtifactReview(ctx, workspacePath, partialUpdate.ExistingStepID, fieldChanges, readFile, writeFile, logger)

		logger.Info(fmt.Sprintf("✅ Updated regular step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated regular step '%s' in the plan%s", partialUpdate.ExistingStepID, dependentReviewNotice), nil
	}
}

func createUpdateMessageSequenceStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}
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
		existingStep, _, _ := findStepByID(plan.Steps, partialUpdate.ExistingStepID)
		if existingStep == nil {
			existingStep, _, _ = findStepByID(plan.OrphanSteps, partialUpdate.ExistingStepID)
		}
		if _, ok := existingStep.(*MessageSequencePlanStep); existingStep != nil && !ok {
			return "", fmt.Errorf("step %q is %T, not message_sequence", partialUpdate.ExistingStepID, existingStep)
		}

		fieldChanges := make([]PlanFieldChange, 0)
		stepIndex, _, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}
		updatedStep, _, _ := findStepByID(plan.Steps, partialUpdate.ExistingStepID)
		if updatedStep == nil {
			updatedStep, _, _ = findStepByID(plan.OrphanSteps, partialUpdate.ExistingStepID)
		}
		if sequenceStep, ok := updatedStep.(*MessageSequencePlanStep); ok {
			if err := validateMessageSequenceStepFieldsTyped(sequenceStep); err != nil {
				return "", fmt.Errorf("validation failed: %w", err)
			}
		}
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateStepIDUniqueness(plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateCrossPlanStepIDUniqueness(ctx, workspacePath, readFile, plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}
		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:    "update_message_sequence_step",
			Reason:  reason,
			StepIDs: []string{partialUpdate.ExistingStepID},
			Changes: fieldChanges,
		}, readFile, writeFile, logger)
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated message_sequence step %s: %v", partialUpdate.ExistingStepID, err))
			}
		}
		dependentReviewNotice := handlePlanStepDependentArtifactReview(ctx, workspacePath, partialUpdate.ExistingStepID, fieldChanges, readFile, writeFile, logger)
		logger.Info(fmt.Sprintf("✅ Updated message_sequence step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated message_sequence step '%s' in the plan%s", partialUpdate.ExistingStepID, dependentReviewNotice), nil
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
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:         "delete_plan_steps",
			Reason:       reason,
			StepIDs:      deletedIDs,
			DeletedSteps: deletedSteps,
		}, readFile, writeFile, logger)

		// Cascade-delete the matching entries from planning/step_config.json so
		// the step's per-step config doesn't linger as an orphan after its plan
		// entry is gone. Best-effort: a missing file or write failure is logged
		// but doesn't fail the plan-deletion call (the plan was already written).
		prunedConfigIDs := []string{}
		if existingConfigs, cfgErr := readStepConfigViaFileCallback(ctx, workspacePath, readFile); cfgErr != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to read step_config.json for cascade-delete: %v", cfgErr))
		} else if newConfigs, removed := pruneStepConfigsByID(existingConfigs, deletedSet); len(removed) > 0 {
			if writeErr := writeStepConfigViaFileCallback(ctx, workspacePath, newConfigs, writeFile); writeErr != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to cascade-delete step_config entries %v: %v", removed, writeErr))
			} else {
				prunedConfigIDs = removed
				logger.Info(fmt.Sprintf("🧹 Cascade-removed %d step_config entries: %v", len(removed), removed))
			}
		}

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

		cleanupNotice := buildDeletedStepArtifactCleanupNotice(deletedIDs, prunedConfigIDs)

		logger.Info(fmt.Sprintf("✅ Deleted %d steps from plan", len(deletedIDs)))
		return fmt.Sprintf("Successfully deleted %d step(s) from the plan%s", len(deletedIDs), cleanupNotice), nil
	}
}

// createCleanupOrphanStepConfigsExecutor creates the executor for the
// cleanup_orphan_step_configs tool. Reads planning/step_config.json, computes
// the set of step IDs that no longer exist in plan.json (top-level Steps +
// OrphanSteps, recursing into nested sub_agent_step / conditional branches),
// and rewrites step_config.json without those orphan entries.
func createCleanupOrphanStepConfigsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, _ map[string]interface{}) (string, error) {
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read plan: %w", err)
		}

		// Build the set of step IDs that legitimately exist anywhere in the plan
		// (including nested via collectAllSteps). Anything in step_config.json
		// outside this set is orphan.
		liveIDs := make(map[string]bool)
		for _, info := range collectAllSteps(plan.Steps) {
			liveIDs[info.Step.GetID()] = true
		}
		for _, info := range collectAllSteps(plan.OrphanSteps) {
			liveIDs[info.Step.GetID()] = true
		}

		configs, err := readStepConfigViaFileCallback(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read step_config.json: %w", err)
		}
		if len(configs) == 0 {
			return "No step_config.json entries found — nothing to clean up.", nil
		}

		kept := make([]StepConfig, 0, len(configs))
		var removed []string
		for _, cfg := range configs {
			if liveIDs[cfg.ID] {
				kept = append(kept, cfg)
				continue
			}
			removed = append(removed, cfg.ID)
		}

		if len(removed) == 0 {
			return fmt.Sprintf("All %d step_config.json entries match a step in plan.json — nothing to remove.", len(configs)), nil
		}

		if err := writeStepConfigViaFileCallback(ctx, workspacePath, kept, writeFile); err != nil {
			return "", fmt.Errorf("failed to write step_config.json: %w", err)
		}

		logger.Info(fmt.Sprintf("🧹 cleanup_orphan_step_configs removed %d orphan entries: %v", len(removed), removed))
		return fmt.Sprintf("Removed %d orphan step_config.json entries: %v. Kept %d.", len(removed), removed, len(kept)), nil
	}
}

// createUpdateHumanInputStepExecutor creates an executor function for update_human_input_step tool
func createUpdateHumanInputStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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

		fieldChanges := make([]PlanFieldChange, 0)

		var stepIndex int
		stepIndex, _, err = updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateStepIDUniqueness(plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateCrossPlanStepIDUniqueness(ctx, workspacePath, readFile, plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:    "update_human_input_step",
			Reason:  reason,
			StepIDs: []string{partialUpdate.ExistingStepID},
			Changes: fieldChanges,
		}, readFile, writeFile, logger)

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", partialUpdate.ExistingStepID))
			}
		}

		dependentReviewNotice := handlePlanStepDependentArtifactReview(ctx, workspacePath, partialUpdate.ExistingStepID, fieldChanges, readFile, writeFile, logger)

		logger.Info(fmt.Sprintf("✅ Updated human input step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated human input step '%s' in the plan%s", partialUpdate.ExistingStepID, dependentReviewNotice), nil
	}
}

// createUpdateTodoTaskStepExecutor creates an executor function for update_todo_task_step tool
func createUpdateTodoTaskStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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
		if err := validateStepIDUniqueness(plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateCrossPlanStepIDUniqueness(ctx, workspacePath, readFile, plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:    "update_todo_task_step",
			Reason:  reason,
			StepIDs: []string{partialUpdate.ExistingStepID},
			Changes: fieldChanges,
		}, readFile, writeFile, logger)

		// Unlock learnings for updated step
		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			} else {
				logger.Info(fmt.Sprintf("🔓 Unlocked learnings for updated step %s (plan was modified)", partialUpdate.ExistingStepID))
			}
		}

		_ = todoTaskStep // Suppress unused variable warning
		dependentReviewNotice := handlePlanStepDependentArtifactReview(ctx, workspacePath, partialUpdate.ExistingStepID, fieldChanges, readFile, writeFile, logger)
		logger.Info(fmt.Sprintf("✅ Updated todo task step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated todo task step '%s' in the plan%s", partialUpdate.ExistingStepID, dependentReviewNotice), nil
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
	if strings.TrimSpace(step.Description) != "" {
		return fmt.Errorf("routing step (title: %q, ID: %s) must not set description; routing is deterministic-only. Put any probe or judgment in a prior regular step that writes %s", step.Title, step.ID, routeSelectionFileName)
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
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}
		if clearDescription, _ := args["clear_description"].(bool); clearDescription {
			if description, ok := args["description"].(string); ok && strings.TrimSpace(description) != "" {
				return "", fmt.Errorf("description must be empty when clear_description=true")
			}
		}

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
		if err := validateStepIDUniqueness(plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateCrossPlanStepIDUniqueness(ctx, workspacePath, readFile, plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:    "update_routing_step",
			Reason:  reason,
			StepIDs: []string{partialUpdate.ExistingStepID},
			Changes: fieldChanges,
		}, readFile, writeFile, logger)

		if unlockLearningsFunc != nil && stepIndex >= 0 {
			if err := unlockLearningsFunc(ctx, partialUpdate.ExistingStepID, stepIndex); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to unlock learnings for updated step %s: %v", partialUpdate.ExistingStepID, err))
			}
		}

		dependentReviewNotice := handlePlanStepDependentArtifactReview(ctx, workspacePath, partialUpdate.ExistingStepID, fieldChanges, readFile, writeFile, logger)

		logger.Info(fmt.Sprintf("✅ Updated routing step '%s' in plan", partialUpdate.ExistingStepID))
		return fmt.Sprintf("Successfully updated routing step '%s' in the plan%s", partialUpdate.ExistingStepID, dependentReviewNotice), nil
	}
}

// createAddRegularStepExecutor creates an executor function for add_regular_step tool
func createAddRegularStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "regular", unlockLearningsFunc)
}

func createAddMessageSequenceStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "message_sequence", unlockLearningsFunc)
}

// createAddHumanInputStepExecutor creates an executor function for add_human_input_step tool
func createAddHumanInputStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "human_input", unlockLearningsFunc)
}

// createAddTodoTaskStepExecutor creates an executor function for add_todo_task_step tool
func createAddTodoTaskStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "todo_task", unlockLearningsFunc)
}

// validateTodoTaskStepFieldsTyped validates that a TodoTaskPlanStep has all required fields
// Returns an error message suitable for returning as a tool response if validation fails
func validateTodoTaskStepFieldsTyped(step *TodoTaskPlanStep) error {
	if step.ID == "" {
		return fmt.Errorf("step (title: %q) has todo_task step type but is missing required id field", step.Title)
	}
	if step.Description == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has todo_task step type but is missing required description field. Please provide a description", step.Title, step.ID)
	}
	if step.NextStepID == "" {
		return fmt.Errorf("step (title: %q, ID: %s) has todo_task step type but is missing required next_step_id field. Please provide the ID of the step to connect to after all todos are complete, or 'end' to terminate the workflow", step.Title, step.ID)
	}
	// Predefined routes are optional (orchestrators can be generic-agent-only)
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
		if route.SubAgentStep.GetID() == "" {
			return fmt.Errorf("step (title: %q, ID: %s) has predefined_route[%d] (route_id: %s) with sub_agent_step missing required ID field", step.Title, step.ID, i, route.RouteID)
		}
		if route.SubAgentStep.GetID() != route.RouteID {
			return fmt.Errorf("step (title: %q, ID: %s) has predefined_route[%d] with mismatched IDs: route_id=%q but sub_agent_step.id=%q. For todo routes, use a single canonical ID only", step.Title, step.ID, i, route.RouteID, route.SubAgentStep.GetID())
		}
		if route.SubAgentStep.GetTitle() == "" {
			return fmt.Errorf("step (title: %q, ID: %s) has predefined_route[%d] (route_id: %s) with sub_agent_step missing required title field", step.Title, step.ID, i, route.RouteID)
		}
	}
	if err := validateTodoTaskNestingDepth(step, 0); err != nil {
		return err
	}
	for i, m := range step.Messages {
		mType := strings.TrimSpace(m.Type)
		switch mType {
		case "", "message", "user_message":
			if strings.TrimSpace(m.Message) == "" {
				return fmt.Errorf("step (title: %q, ID: %s) messages[%d] is a message entry but has an empty message", step.Title, step.ID, i)
			}
		case "prevalidation":
			if m.ValidationSchema == nil {
				return fmt.Errorf("step (title: %q, ID: %s) messages[%d] is a prevalidation entry but has no validation_schema", step.Title, step.ID, i)
			}
		case "foreach":
			if strings.TrimSpace(m.SourceSQL) == "" {
				return fmt.Errorf("step (title: %q, ID: %s) messages[%d] is a foreach entry but has no source_sql", step.Title, step.ID, i)
			}
			if strings.TrimSpace(m.Message) == "" {
				return fmt.Errorf("step (title: %q, ID: %s) messages[%d] is a foreach entry but has no message (the per-row template)", step.Title, step.ID, i)
			}
		default:
			return fmt.Errorf("step (title: %q, ID: %s) messages[%d] has unsupported type %q (use message, prevalidation, or foreach)", step.Title, step.ID, i, mType)
		}
	}
	return nil
}

func validateMessageSequenceStepFieldsTyped(step *MessageSequencePlanStep) error {
	if step == nil {
		return fmt.Errorf("message_sequence step is nil")
	}
	if strings.TrimSpace(step.ID) == "" {
		return fmt.Errorf("message_sequence step (title: %q) is missing required id field", step.Title)
	}
	if strings.TrimSpace(step.Title) == "" {
		return fmt.Errorf("message_sequence step (ID: %s) is missing required title field", step.ID)
	}
	if strings.TrimSpace(step.Description) == "" {
		return fmt.Errorf("message_sequence step (title: %q, ID: %s) is missing required description field", step.Title, step.ID)
	}
	if len(step.Items) == 0 {
		return fmt.Errorf("message_sequence step (title: %q, ID: %s) must include at least one item", step.Title, step.ID)
	}
	seen := make(map[string]bool, len(step.Items))
	for i, item := range step.Items {
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("message_sequence step %q item[%d] is missing required id", step.ID, i)
		}
		if seen[item.ID] {
			return fmt.Errorf("message_sequence step %q has duplicate item id %q", step.ID, item.ID)
		}
		seen[item.ID] = true
		itemType := strings.TrimSpace(item.Type)
		if itemType == "" {
			itemType = "user_message"
		}
		switch itemType {
		case "user_message":
			if strings.TrimSpace(item.Message) == "" {
				return fmt.Errorf("message_sequence step %q item %q is a user_message but message is empty", step.ID, item.ID)
			}
		case "code":
			if strings.TrimSpace(item.ScriptPath) == "" {
				return fmt.Errorf("message_sequence step %q item %q is code but script_path is empty", step.ID, item.ID)
			}
			if item.Runtime != "" && item.Runtime != "python" && item.Runtime != "python3" {
				return fmt.Errorf("message_sequence step %q item %q has unsupported runtime %q; only python is supported", step.ID, item.ID, item.Runtime)
			}
		case "prevalidation":
			if item.ValidationSchema == nil && item.Prevalidation == nil && step.ValidationSchema == nil {
				return fmt.Errorf("message_sequence step %q item %q is prevalidation but no validation_schema/prevalidation exists", step.ID, item.ID)
			}
		case "foreach":
			if strings.TrimSpace(item.SourceSQL) == "" {
				return fmt.Errorf("message_sequence step %q item %q is foreach but source_sql is empty", step.ID, item.ID)
			}
			if strings.TrimSpace(item.Message) == "" {
				return fmt.Errorf("message_sequence step %q item %q is foreach but message (the per-row template) is empty", step.ID, item.ID)
			}
		default:
			return fmt.Errorf("message_sequence step %q item %q has unsupported type %q", step.ID, item.ID, item.Type)
		}

		// NOTE: per-item write access (db/kb/learnings) is intentionally NOT
		// validated from the message prose here. Detecting "this item writes the
		// db" from natural language is unreliable (false negatives like "upsert
		// into trade_ideas", false positives on descriptive text), so we don't
		// gate plans on a guess. Enforcement lives at the runtime folder guard
		// (the source of truth — it blocks the write and emits an actionable
		// "grant write_access.db" failure), and structured signals (kind /
		// output_files) auto-grant via resolveMessageSequenceItemWriteAccess. The
		// authoring contract is carried by guidance, not a prose regex.
	}
	return nil
}

func setStepIdentity(step PlanStepInterface, id, title string) error {
	switch s := step.(type) {
	case *RegularPlanStep:
		s.ID = id
		if strings.TrimSpace(s.Title) == "" {
			s.Title = title
		}
	case *TodoTaskPlanStep:
		s.ID = id
		if strings.TrimSpace(s.Title) == "" {
			s.Title = title
		}
	case *HumanInputPlanStep:
		s.ID = id
		if strings.TrimSpace(s.Title) == "" {
			s.Title = title
		}
	case *EvaluationStep:
		s.ID = id
		if strings.TrimSpace(s.Title) == "" {
			s.Title = title
		}
	case *RoutingPlanStep:
		s.ID = id
		if strings.TrimSpace(s.Title) == "" {
			s.Title = title
		}
	case *MessageSequencePlanStep:
		s.ID = id
		if strings.TrimSpace(s.Title) == "" {
			s.Title = title
		}
	default:
		return fmt.Errorf("unsupported sub_agent_step type %T for identity normalization", step)
	}
	return nil
}

//nolint:unused // kept for the deferred todo-route ID migration rollout.
type todoRouteIDMigration struct {
	ParentStepID string
	RouteID      string
	OldStepID    string
	NewStepID    string
	OldTitle     string
	NewTitle     string
}

//nolint:unused // kept for the deferred todo-route ID migration rollout.
func migrateTodoRouteIDsInStep(step PlanStepInterface, parentStepFilter string, changes *[]todoRouteIDMigration) error {
	switch s := step.(type) {
	case *TodoTaskPlanStep:
		applyRoutes := parentStepFilter == "" || s.GetID() == parentStepFilter
		if applyRoutes {
			for i := range s.PredefinedRoutes {
				route := &s.PredefinedRoutes[i]
				if route.SubAgentStep == nil {
					continue
				}
				oldID := route.SubAgentStep.GetID()
				oldTitle := route.SubAgentStep.GetTitle()
				if err := setStepIdentity(route.SubAgentStep, route.RouteID, route.RouteName); err != nil {
					return err
				}
				if oldID != route.SubAgentStep.GetID() || oldTitle != route.SubAgentStep.GetTitle() {
					*changes = append(*changes, todoRouteIDMigration{
						ParentStepID: s.GetID(),
						RouteID:      route.RouteID,
						OldStepID:    oldID,
						NewStepID:    route.SubAgentStep.GetID(),
						OldTitle:     oldTitle,
						NewTitle:     route.SubAgentStep.GetTitle(),
					})
				}
			}
		}
		for _, route := range s.PredefinedRoutes {
			if route.SubAgentStep != nil {
				if err := migrateTodoRouteIDsInStep(route.SubAgentStep, parentStepFilter, changes); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

//nolint:unused // kept for the deferred todo-route ID migration rollout.
func migrateTodoRouteIDsInPlan(plan *PlanningResponse, parentStepFilter string) ([]todoRouteIDMigration, error) {
	var changes []todoRouteIDMigration
	for _, step := range plan.Steps {
		if err := migrateTodoRouteIDsInStep(step, parentStepFilter, &changes); err != nil {
			return nil, err
		}
	}
	for _, step := range plan.OrphanSteps {
		if err := migrateTodoRouteIDsInStep(step, parentStepFilter, &changes); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

//nolint:unused // kept for the deferred todo-route ID migration rollout.
func updateStepConfigIDsForTodoRouteMigration(configs []StepConfig, migrations []todoRouteIDMigration) error {
	idMap := make(map[string]string)
	for _, migration := range migrations {
		if migration.OldStepID != "" && migration.OldStepID != migration.NewStepID {
			idMap[migration.OldStepID] = migration.NewStepID
		}
	}
	if len(idMap) == 0 {
		return nil
	}

	existing := make(map[string]int)
	for i, cfg := range configs {
		existing[cfg.ID] = i
	}
	for oldID, newID := range idMap {
		if oldIdx, okOld := existing[oldID]; okOld {
			if newIdx, okNew := existing[newID]; okNew && newIdx != oldIdx {
				return fmt.Errorf("cannot migrate step_config id %q to %q because both IDs already exist in planning/step_config.json", oldID, newID)
			}
		}
	}
	for i := range configs {
		if newID, ok := idMap[configs[i].ID]; ok {
			configs[i].ID = newID
		}
	}
	return nil
}

//nolint:unused // kept for the deferred todo-route ID migration rollout.
func writeStepConfigsFile(ctx context.Context, writeFile func(context.Context, string, string) error, configs []StepConfig) error {
	configFile := StepConfigFile{Steps: configs}
	jsonData, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal step_config.json: %w", err)
	}
	if err := writeFile(ctx, filepath.Join("planning", "step_config.json"), string(jsonData)); err != nil {
		return fmt.Errorf("failed to write planning/step_config.json: %w", err)
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
		for i, route := range s.PredefinedRoutes {
			if route.SubAgentStep == nil {
				continue
			}
			if err := validateTodoTaskNestingDepth(route.SubAgentStep, todoRouteDepth+1); err != nil {
				return fmt.Errorf("predefined_route[%d] (route_id: %s): %w", i, route.RouteID, err)
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
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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
		case "message_sequence":
			if sequenceStep, ok := typedStep.(*MessageSequencePlanStep); ok {
				if err := validateMessageSequenceStepFieldsTyped(sequenceStep); err != nil {
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
		if err := validateStepIDUniqueness(newPlan); err != nil {
			return "", fmt.Errorf("plan validation failed before writing: %w", err)
		}
		if err := validateCrossPlanStepIDUniqueness(ctx, workspacePath, readFile, newPlan); err != nil {
			return "", fmt.Errorf("plan validation failed before writing: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, newPlan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		var addedStepJSON []json.RawMessage
		if rawAdded, marshalErr := json.Marshal(typedStep); marshalErr == nil {
			addedStepJSON = []json.RawMessage{rawAdded}
		}
		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:       fmt.Sprintf("add_%s_step", stepType),
			Reason:     reason,
			StepIDs:    []string{typedStep.GetID()},
			AddedSteps: addedStepJSON,
		}, readFile, writeFile, logger)

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

		setupNotice := buildAddedStepArtifactSetupNotice(typedStep.GetID(), stepType)

		logger.Info(fmt.Sprintf("✅ Added %s step '%s' (ID: %s) to plan", stepType, typedStep.GetTitle(), typedStep.GetID()))
		return fmt.Sprintf("Successfully added %s step '%s' (ID: %s) to the plan%s", stepType, typedStep.GetTitle(), typedStep.GetID(), setupNotice), nil
	}
}

// createCreatePlanExecutor returns an executor for the create_plan tool, which
// initializes an empty planning/plan.json for workflows that don't have one yet.
// Fails if plan.json already exists so we never silently clobber a live plan.
func createCreatePlanExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, _ map[string]interface{}) (string, error) {
		planPath := normalizePathForWorkspaceAPI(filepath.Join("planning", "plan.json"), workspacePath)

		planFileMutex.Lock()
		defer planFileMutex.Unlock()

		if existing, err := readFile(ctx, planPath); err == nil && strings.TrimSpace(existing) != "" {
			return "", fmt.Errorf("plan.json already exists at %s — use add_regular_step / update_* / delete_plan_steps to modify it instead of recreating it", planPath)
		}

		emptyPlan := &PlanningResponse{}
		data, err := json.MarshalIndent(emptyPlan, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal empty plan: %w", err)
		}

		if err := writeFile(ctx, planPath, string(data)); err != nil {
			return "", fmt.Errorf("failed to write plan.json: %w", err)
		}

		logger.Info(fmt.Sprintf("🆕 Created new empty plan.json at %s", planPath))
		return fmt.Sprintf("Created empty plan.json at %s. Add steps with add_regular_step / add_message_sequence_step / add_human_input_step / add_todo_task_step / add_routing_step. For the first step, pass insert_after_step_id=\"\" to insert at the beginning.", planPath), nil
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

	// Register create_plan first — it's the only way to initialize planning/plan.json for a new
	// workflow so that the add_* tools (which require an existing plan) can run.
	createPlanSchema := getCreatePlanSchema()
	createPlanParams, err := parseSchemaForToolParameters(createPlanSchema)
	if err != nil {
		return fmt.Errorf("failed to parse create plan schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"create_plan",
		"Initialize an empty planning/plan.json for a new workflow. Call this FIRST when the workflow has no plan.json yet, before using add_regular_step / add_message_sequence_step / add_human_input_step / add_todo_task_step / add_routing_step to populate it. Refuses to overwrite an existing plan.json. Takes no arguments. Note: workflow objective lives in soul/soul.md — edit that file separately; plan.json no longer stores it.",
		createPlanParams,
		createCreatePlanExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register create_plan tool: %w", err)
	}

	// Register workflow-specific plan update tools with "workflow" category
	// Individual update tools for each step type
	regularUpdateSchema := getUpdateRegularStepSchema()
	regularUpdateParams, err := parseSchemaForToolParameters(regularUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update regular step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_regular_step",
		"Update a regular step in the plan. Provide existing_step_id (required) to identify which step to update, and only include the fields you want to change (title, description, context fields, loop fields). The plan.json file is updated immediately when this tool is called. After a substantive change, review whether the step's saved artifacts (SKILL.md learnings, scripted main.py, validation schema) still match the new plan — they can drift out of sync; run get_workflow_command_guidance(kind=\"review-artifact-drift\").",
		regularUpdateParams,
		createUpdateRegularStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_regular_step tool: %w", err)
	}

	// NOTE: update_conditional_step tool removed (deprecated in favor of decision/routing).

	// NOTE: update_orchestration_step tool removed (deprecated in favor of todo_task).

	humanInputUpdateSchema := getUpdateHumanInputStepSchema()
	humanInputUpdateParams, err := parseSchemaForToolParameters(humanInputUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update human input step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_human_input_step",
		"Update a human input step in the plan. Provide existing_step_id (required) to identify which human input step to update, and only include the fields you want to change (question, response_type, options, variable_name, context_output, next_step_id, if_yes_next_step_id/if_no_next_step_id, option_routes). The plan.json file is updated immediately when this tool is called. After a substantive change, review whether the step's saved artifacts still match the new plan — they can drift out of sync; run get_workflow_command_guidance(kind=\"review-artifact-drift\").",
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
		"Delete steps from the plan by providing their IDs. Use the step's id field from the plan. The plan.json file is updated immediately when this tool is called. Any matching entries in planning/step_config.json are also removed in the same call so a deleted step doesn't leave behind an orphan config.",
		deleteParams,
		createDeletePlanStepsExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register delete_plan_steps tool: %w", err)
	}

	cleanupOrphanSchema := `{
		"type": "object",
		"properties": {}
	}`
	cleanupOrphanParams, err := parseSchemaForToolParameters(cleanupOrphanSchema)
	if err != nil {
		return fmt.Errorf("failed to parse cleanup_orphan_step_configs schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"cleanup_orphan_step_configs",
		"Sweep planning/step_config.json and remove entries whose step_id no longer exists in plan.json (or plan.OrphanSteps). Use this once when you notice the agent describing orphan step_config entries it can't reach via update_step_config. Idempotent: returns the list of IDs removed (empty if nothing was orphaned).",
		cleanupOrphanParams,
		createCleanupOrphanStepConfigsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register cleanup_orphan_step_configs tool: %w", err)
	}

	// Register type-specific step addition tools
	regularSchema := getAddRegularStepSchema()
	regularParams, err := parseSchemaForToolParameters(regularSchema)
	if err != nil {
		return fmt.Errorf("failed to parse regular step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_regular_step",
		"Add a regular execution step to the plan. Use this for standard steps that execute once and produce output. Provide all required fields: id, title, description, context_output, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		regularParams,
		createAddRegularStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_regular_step tool: %w", err)
	}

	messageSequenceSchema := getAddMessageSequenceStepSchema()
	messageSequenceParams, err := parseSchemaForToolParameters(messageSequenceSchema)
	if err != nil {
		return fmt.Errorf("failed to parse message sequence step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_message_sequence_step",
		"Add a message_sequence step to the plan. Use this only when the user asks for a persistent single-agent conversation with an ordered queue of short user messages, optional prevalidation items, and optional Python code items. As a todo_task predefined route, use message_sequence when the orchestrator should reuse the same specialist conversation for critique, test feedback, validation feedback, or follow-up work; restart is controlled at execution time with message_sequence_restart. Reads for KB/db/learnings are always open; writes are item-scoped through write_access. Prefer small focused user messages, and add reference-check, hallucination-check, critique, or self-validation messages where helpful.",
		messageSequenceParams,
		createAddMessageSequenceStepExecutor(workspacePath, logger, readFile, writeFile, moveFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register add_message_sequence_step tool: %w", err)
	}

	// NOTE: add_conditional_step tool removed (deprecated in favor of decision/routing).
	// Schema, executor, and execution code kept for backward compatibility with existing workflows.

	// NOTE: add_orchestration_step tool removed (deprecated in favor of todo_task).
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
		"Add a todo task orchestration step to the plan. Use this when you need to manage a dynamic todo list with trackable tasks. The main orchestrator creates/assigns tasks, then delegates to predefined sub-agents (with learning and prevalidation) or a generic agent (workspace tools only, no learning). Predefined routes have MCP tool access and accumulate learnings. A route sub_agent_step can be regular for stateless work, message_sequence for a reusable specialist conversation with re-entry/restart behavior, or todo_task for one nested orchestration layer. The generic agent is for simple, ad-hoc tasks. Provide: id, title, todo_task_step (main orchestrator metadata), predefined_routes (optional, specialized sub-agents), enable_generic_agent (optional, default true), next_step_id, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
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
		"Add a deterministic routing step to the plan. Use this when the workflow must choose exactly one of multiple existing downstream steps. Routing has one mode only: read caller route_selections, the routing step's preseeded route_selection.json, route_source_file, a context_dependencies entry named route_selection.json, or default_route_id, then switch to the selected route. Do not set description or context_output on routing steps; if an agent/probe/judgment is needed, add a prior regular step that writes route_selection.json and declare that file in the routing step's route_source_file or context_dependencies. Provide: id, title, routing_question (readability/compatibility only), routes (min 2 with route_id/route_name/condition/next_step_id), context_dependencies, optional default_route_id, optional route_source_file, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
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
		"Update a deterministic routing step in the plan. Provide existing_step_id (required) and only include fields you want to change: title, routing_question, routes, default_route_id, route_source_file, context_dependencies, or clear_description=true for legacy migration. Do not set description or context_output; routing never executes an agent and never LLM-evaluates routing_question. The selected route must come from caller route_selections, route_selection.json, route_source_file, context_dependencies, or default_route_id. The plan.json file is updated immediately when this tool is called. After a substantive change, review whether saved artifacts still match the new plan — they can drift out of sync; run get_workflow_command_guidance(kind=\"review-artifact-drift\").",
		routingUpdateParams,
		createUpdateRoutingStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_routing_step tool: %w", err)
	}

	messageSequenceUpdateSchema := getUpdateMessageSequenceStepSchema()
	messageSequenceUpdateParams, err := parseSchemaForToolParameters(messageSequenceUpdateSchema)
	if err != nil {
		return fmt.Errorf("failed to parse update_message_sequence_step schema: %w", err)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_message_sequence_step",
		"Update a message_sequence step in the plan. Provide existing_step_id and only the fields to change. Replacing items changes the configured queue; an existing runtime session will still resume unless explicitly restarted by execution controls. After a substantive change, review whether the step's saved artifacts still match the new plan — they can drift out of sync; run get_workflow_command_guidance(kind=\"review-artifact-drift\").",
		messageSequenceUpdateParams,
		createUpdateMessageSequenceStepExecutor(workspacePath, logger, readFile, writeFile, unlockLearningsFunc),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register update_message_sequence_step tool: %w", err)
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
		"Update an Orchestrator step (todo_task type) in the plan. Provide existing_step_id (required) to identify which step to update, and only include the fields you want to change (title, todo_task_step, predefined_routes, next_step_id). The plan.json file is updated immediately when this tool is called. After a substantive change, review whether the step's saved artifacts still match the new plan — they can drift out of sync; run get_workflow_command_guidance(kind=\"review-artifact-drift\").",
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
		"Add a new predefined route (sub-agent) to an Orchestrator step (todo_task type). Provide parent_step_id and new_route with route_id, route_name, and condition, plus either sub_agent_step for an inline route definition or orphan_step_ref to reuse a shared orphan step from the same plan. The plan.json file is updated immediately when this tool is called.",
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
		"Update an existing predefined route (sub-agent) within an Orchestrator step (todo_task type). Provide parent_step_id, existing_route_id, and only include the fields you want to change (route_name, condition, orphan_step_ref, sub_agent_step, context_to_pass). Use orphan_step_ref to point the route at a reusable orphan step from the same plan. The plan.json file is updated immediately when this tool is called.",
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
		"Delete a predefined route (sub-agent) from an Orchestrator step (todo_task type). Provide parent_step_id and deleted_route_id. Unlike routing steps, Orchestrator steps may have 0 predefined routes (generic-agent-only). The plan.json file is updated immediately when this tool is called.",
		deleteTodoTaskRouteParams,
		createDeleteTodoTaskRouteExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf("failed to register delete_todo_task_route tool: %w", err)
	}

	// Register validation schema update tool
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

	if logger != nil {
		logger.Info(fmt.Sprintf("✅ Registered all plan modification tools for %s", agentName))
	}

	return nil
}

// createAddTodoTaskRouteExecutor creates an executor function for add_todo_task_route tool
func createAddTodoTaskRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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

		// Find the parent todo task step by ID. Recurses into nested steps
		// (e.g. a todo_task sitting in another todo_task's predefined_routes.sub_agent_step).
		parentStep, _, _ := findStepByID(plan.Steps, parentStepID)
		if parentStep == nil {
			parentStep, _, _ = findStepByID(plan.OrphanSteps, parentStepID)
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available top-level step IDs: %v", parentStepID, availableIDs)
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

		// Validate inline sub_agent_step identity when provided.
		if newRoute.OrphanStepRef != "" && newRoute.SubAgentStep != nil {
			return "", fmt.Errorf("new route %q cannot define both orphan_step_ref and sub_agent_step", newRoute.RouteID)
		}
		if newRoute.SubAgentStep != nil {
			if newRoute.SubAgentStep.GetID() != "" && newRoute.SubAgentStep.GetID() != newRoute.RouteID {
				return "", fmt.Errorf("sub_agent_step.id %q must exactly match route_id %q for predefined todo routes", newRoute.SubAgentStep.GetID(), newRoute.RouteID)
			}
			if err := setStepIdentity(newRoute.SubAgentStep, newRoute.RouteID, newRoute.RouteName); err != nil {
				return "", err
			}
		}

		// Add new route
		todoTaskStep.PredefinedRoutes = append(todoTaskStep.PredefinedRoutes, newRoute)
		if err := resolvePlanOrphanStepRefs(plan); err != nil {
			return "", fmt.Errorf("failed to resolve orphan step references after adding route: %w", err)
		}
		if err := validateTodoTaskStepFieldsTyped(todoTaskStep); err != nil {
			return "", fmt.Errorf("validation failed after adding route: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:    "add_todo_task_route",
			Reason:  reason,
			StepIDs: []string{parentStepID, newRoute.RouteID},
		}, readFile, writeFile, logger)

		routeReviewNotice := handleTodoTaskRouteArtifactReview(ctx, workspacePath, parentStepID, newRoute.RouteID, "added", readFile, writeFile, logger)

		logger.Info(fmt.Sprintf("✅ Added route '%s' (ID: %s) to todo task step '%s'", newRoute.RouteName, newRoute.RouteID, todoTaskStep.Title))
		return fmt.Sprintf("Successfully added route '%s' (ID: %s) to todo task step '%s'%s", newRoute.RouteName, newRoute.RouteID, todoTaskStep.Title, routeReviewNotice), nil
	}
}

// createUpdateTodoTaskRouteExecutor creates an executor function for update_todo_task_route tool
func createUpdateTodoTaskRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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

		// Find the parent todo task step by ID.
		parentStep, _, _ := findStepByID(plan.Steps, parentStepID)
		if parentStep == nil {
			parentStep, _, _ = findStepByID(plan.OrphanSteps, parentStepID)
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available top-level step IDs: %v", parentStepID, availableIDs)
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

		if orphanStepRef, ok := args["orphan_step_ref"].(string); ok {
			routeToUpdate.OrphanStepRef = strings.TrimSpace(orphanStepRef)
			if routeToUpdate.OrphanStepRef != "" {
				routeToUpdate.SubAgentStep = nil
			}
		}

		// Handle sub_agent_step update
		if subAgentStepRaw, ok := args["sub_agent_step"].(map[string]interface{}); ok {
			if routeToUpdate.OrphanStepRef != "" {
				return "", fmt.Errorf("route %q uses orphan_step_ref %q, so sub_agent_step cannot be updated inline", routeToUpdate.RouteID, routeToUpdate.OrphanStepRef)
			}
			if providedID, ok := subAgentStepRaw["id"].(string); ok && providedID != "" && providedID != routeToUpdate.RouteID {
				return "", fmt.Errorf("sub_agent_step.id %q must exactly match route_id %q for predefined todo routes", providedID, routeToUpdate.RouteID)
			}
			mergedSubAgentStepRaw := map[string]interface{}{}
			if routeToUpdate.SubAgentStep != nil {
				existingJSON, err := json.Marshal(routeToUpdate.SubAgentStep)
				if err != nil {
					return "", fmt.Errorf("failed to serialize existing sub_agent_step: %w", err)
				}
				if err := json.Unmarshal(existingJSON, &mergedSubAgentStepRaw); err != nil {
					return "", fmt.Errorf("failed to parse existing sub_agent_step: %w", err)
				}
			}
			for k, v := range subAgentStepRaw {
				mergedSubAgentStepRaw[k] = v
			}
			// Ensure type field is set
			if _, hasType := mergedSubAgentStepRaw["type"]; !hasType {
				mergedSubAgentStepRaw["type"] = "regular" // Default to regular if not specified
			}
			mergedSubAgentStepRaw["id"] = routeToUpdate.RouteID
			if title, ok := mergedSubAgentStepRaw["title"].(string); !ok || strings.TrimSpace(title) == "" {
				mergedSubAgentStepRaw["title"] = routeToUpdate.RouteName
			}
			updatedSubAgentStep, err := convertMapToStep(mergedSubAgentStepRaw)
			if err != nil {
				return "", fmt.Errorf("failed to parse sub_agent_step: %w", err)
			}
			routeToUpdate.SubAgentStep = updatedSubAgentStep
		}

		if err := resolvePlanOrphanStepRefs(plan); err != nil {
			return "", fmt.Errorf("failed to resolve orphan step references after route update: %w", err)
		}
		if err := validateTodoTaskStepFieldsTyped(todoTaskStep); err != nil {
			return "", fmt.Errorf("validation failed after route update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:    "update_todo_task_route",
			Reason:  reason,
			StepIDs: []string{parentStepID, existingRouteID},
		}, readFile, writeFile, logger)

		routeReviewNotice := handleTodoTaskRouteArtifactReview(ctx, workspacePath, parentStepID, existingRouteID, "updated", readFile, writeFile, logger)

		logger.Info(fmt.Sprintf("✅ Updated route '%s' (ID: %s) in todo task step '%s'", routeToUpdate.RouteName, existingRouteID, todoTaskStep.Title))
		return fmt.Sprintf("Successfully updated route '%s' (ID: %s) in todo task step '%s'%s", routeToUpdate.RouteName, existingRouteID, todoTaskStep.Title, routeReviewNotice), nil
	}
}

// createDeleteTodoTaskRouteExecutor creates an executor function for delete_todo_task_route tool
func createDeleteTodoTaskRouteExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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

		// Find the parent todo task step by ID. Recurses into nested steps
		// (e.g. a todo_task sitting in another todo_task's predefined_routes.sub_agent_step).
		parentStep, _, _ := findStepByID(plan.Steps, parentStepID)
		if parentStep == nil {
			parentStep, _, _ = findStepByID(plan.OrphanSteps, parentStepID)
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.GetID())
			}
			return "", fmt.Errorf("parent step ID '%s' not found in existing plan. Available top-level step IDs: %v", parentStepID, availableIDs)
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

		// Note: Unlike orchestration steps, todo task steps may have 0 predefined routes (generic-agent-only)

		// Remove the route
		todoTaskStep.PredefinedRoutes = append(
			todoTaskStep.PredefinedRoutes[:routeIndex],
			todoTaskStep.PredefinedRoutes[routeIndex+1:]...,
		)

		// Capture deleted route JSON before writing
		var deletedRouteJSON []json.RawMessage
		if rawDeleted, marshalErr := json.Marshal(deletedRoute); marshalErr == nil {
			deletedRouteJSON = []json.RawMessage{rawDeleted}
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:         "delete_todo_task_route",
			Reason:       reason,
			StepIDs:      []string{parentStepID, deletedRouteID},
			DeletedSteps: deletedRouteJSON,
		}, readFile, writeFile, logger)

		deletedRouteSet := map[string]bool{deletedRouteID: true}
		if existingConfigs, cfgErr := readStepConfigViaFileCallback(ctx, workspacePath, readFile); cfgErr != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to read step_config.json for route cascade-delete: %v", cfgErr))
		} else if newConfigs, removed := pruneStepConfigsByID(existingConfigs, deletedRouteSet); len(removed) > 0 {
			if writeErr := writeStepConfigViaFileCallback(ctx, workspacePath, newConfigs, writeFile); writeErr != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to cascade-delete route step_config entries %v: %v", removed, writeErr))
			} else {
				logger.Info(fmt.Sprintf("🧹 Cascade-removed %d route step_config entries: %v", len(removed), removed))
			}
		}

		routeReviewNotice := handleTodoTaskRouteArtifactReview(ctx, workspacePath, parentStepID, deletedRouteID, "deleted", readFile, writeFile, logger)

		logger.Info(fmt.Sprintf("✅ Deleted route '%s' (ID: %s) from todo task step '%s'", deletedRoute.RouteName, deletedRouteID, todoTaskStep.Title))
		return fmt.Sprintf("Successfully deleted route '%s' (ID: %s) from todo task step '%s'%s", deletedRoute.RouteName, deletedRouteID, todoTaskStep.Title, routeReviewNotice), nil
	}
}

// createUpdateValidationSchemaExecutor creates an executor function for update_validation_schema tool
func createUpdateValidationSchemaExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, unlockLearningsFunc func(context.Context, string, int) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		reason, err := requireReason(args)
		if err != nil {
			return "", err
		}

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
		stepIndex, _, err := updateSingleStep(plan, partialUpdate, &fieldChanges)
		if err != nil {
			return "", err
		}

		// Validate all steps after update
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateStepIDUniqueness(plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}
		if err := validateCrossPlanStepIDUniqueness(ctx, workspacePath, readFile, plan); err != nil {
			return "", fmt.Errorf("plan validation failed after update: %w", err)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write plan: %w", err)
		}

		logPlanChange(ctx, workspacePath, PlanChangelogEntry{
			Tool:    "update_validation_schema",
			Reason:  reason,
			StepIDs: []string{updateData.ExistingStepID},
			Changes: fieldChanges,
		}, readFile, writeFile, logger)

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
