package step_based_workflow

import (
	"encoding/json"
	"fmt"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	baseevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// BranchStepProgress tracks branch execution progress for conditional steps
type BranchStepProgress struct {
	BranchExecuted string   `json:"branch_executed"` // "if_true" or "if_false"
	CompletedSteps []string `json:"completed_steps"` // e.g., ["step-3-if-true-0", "step-3-if-true-1"]
}

// DecisionEvaluationCount tracks how many times a specific decision has been made
// Key format: "{stepID}:{result}" where result is "true" or "false"
type DecisionEvaluationCount map[string]int

// OrchestrationRoute represents a possible route/sub-agent (private to orchestration step)
type OrchestrationRoute struct {
	RouteID       string            `json:"route_id"`                  // Unique ID for this route
	RouteName     string            `json:"route_name"`                // Human-readable name
	Condition     string            `json:"condition"`                 // Condition description (e.g., "If error is authentication-related")
	SubAgentStep  PlanStepInterface `json:"sub_agent_step"`            // The sub-agent step to execute (private, not in main workflow)
	ContextToPass string            `json:"context_to_pass,omitempty"` // Optional: specific context to pass to sub-agent
}

// OrchestrationResponse represents the structured output from orchestration evaluation
type OrchestrationResponse struct {
	SelectedRouteID                string `json:"selected_route_id"`                            // Which route was selected (can be "end" to terminate workflow, empty to continue working, or a route ID)
	SuccessCriteriaMet             bool   `json:"success_criteria_met"`                         // Whether main orchestrator's success criteria is met
	SuccessReasoning               string `json:"success_reasoning,omitempty"`                  // Reasoning for success criteria evaluation
	InstructionsToSubAgent         string `json:"instructions_to_sub_agent,omitempty"`          // Instructions to pass to the selected sub-agent (replaces step description, required if selected_route_id is provided)
	SuccessCriteriaForSubAgent     string `json:"success_criteria_for_sub_agent,omitempty"`     // Success criteria to pass to the selected sub-agent (replaces step success criteria, required if selected_route_id is provided)
	ContextDependenciesForSubAgent string `json:"context_dependencies_for_sub_agent,omitempty"` // Context dependencies to pass to the selected sub-agent (replaces step context dependencies, optional)
	ContextOutputForSubAgent       string `json:"context_output_for_sub_agent,omitempty"`       // Context output file name to pass to the selected sub-agent (replaces step context output, optional)
}

// StepProgress tracks which steps have been completed
type StepProgress struct {
	CompletedStepIndices     []int                      `json:"completed_step_indices"` // 0-based indices
	TotalSteps               int                        `json:"total_steps"`
	LastUpdated              time.Time                  `json:"last_updated"`
	BranchSteps              map[int]BranchStepProgress `json:"branch_steps,omitempty"`        // key is step index (0-based)
	ValidationFailures       map[string]int             `json:"validation_failures,omitempty"` // key is step path, value is failure count
	DecisionEvaluationCounts DecisionEvaluationCount    `json:"-"`                             // in-memory only: tracks decision step evaluations to prevent infinite loops (not persisted)
	ArchivalCounts           map[int]int                `json:"archival_counts,omitempty"`     // key is stepNumber (1-based), value is archive run count
}

// BranchStepResumeTarget represents a branch step to resume from
type BranchStepResumeTarget struct {
	ParentStepIndex int    `json:"parent_step_index"` // 0-based index of conditional step
	BranchType      string `json:"branch_type"`       // "if_true" or "if_false"
	BranchStepIndex int    `json:"branch_step_index"` // 0-based index within the branch
}

// ExecutionOptions represents user-selected execution options from frontend
// When provided, backend will use these options instead of asking interactively
type ExecutionOptions struct {
	RunMode                 string                  `json:"run_mode"`                             // "use_same_run" or "create_new_runs_always"
	SelectedRunFolder       string                  `json:"selected_run_folder,omitempty"`        // If use_same_run and user selected specific folder
	ExecutionStrategy       string                  `json:"execution_strategy"`                   // Execution strategy (see constants below)
	ResumeFromStep          int                     `json:"resume_from_step,omitempty"`           // 1-based step number to resume from (for top-level steps)
	ResumeFromBranchStep    *BranchStepResumeTarget `json:"resume_from_branch_step,omitempty"`    // For resuming from branch steps
	FastExecuteEndStep      int                     `json:"fast_execute_end_step,omitempty"`      // 0-based last step for fast execute range
	PlanChangeAction        string                  `json:"plan_change_action,omitempty"`         // "keep_old_progress" or "delete_old_progress"
	AllStepsCompletedAction string                  `json:"all_steps_completed_action,omitempty"` // "fast_execute_again" or "skip_execution"

	// Temporary LLM overrides (optional, overrides step-level configs for this execution only)
	// Only applies to execution agents (not validation or learning agents)
	// Takes highest priority over step configs and tiered execution selection
	// Cascading fallback: tempLLM1 → tempLLM2 → step LLM (on validation failures)
	TempOverrideLLM  *AgentLLMConfig `json:"temp_override_llm,omitempty"`  // First override LLM (used on first attempt)
	TempOverrideLLM2 *AgentLLMConfig `json:"temp_override_llm2,omitempty"` // Second override LLM (used on second attempt if tempLLM1 fails)

	// Fallback behavior when validation fails
	FallbackToOriginalLLMOnFailure bool `json:"fallback_to_original_llm_on_failure,omitempty"` // If true, use the normal workflow LLM path (step config > tiered) instead of temp override when validation fails

	// Learning behavior when tempLLM is active (per-model control)
	SkipLearningWhenTempLLM1 bool `json:"skip_learning_when_temp_llm1,omitempty"` // If true, skip learning phases when tempLLM1 is used (default: false, learning runs)
	SkipLearningWhenTempLLM2 bool `json:"skip_learning_when_temp_llm2,omitempty"` // If true, skip learning phases when tempLLM2 is used (default: false, learning runs)

	// Temporary LLM for learning agents (optional, used when learnings already exist for a step)
	// If learnings exist for a step_id, use TempLearningLLM if configured
	// If no learnings exist (new learning), always use default LLM (step config → preset)
	TempLearningLLM *AgentLLMConfig `json:"temp_learning_llm,omitempty"`

	// Variable group execution options (for batch execution with multiple groups)
	EnabledGroupIDs []string `json:"enabled_group_ids,omitempty"` // Group IDs to execute (if empty, uses groups' enabled flags)

	// Cleanup control
	SkipExecutionCleanup bool `json:"skip_execution_cleanup,omitempty"` // If true, skip deleting execution folders before running steps
}

// BatchExecutionProgress tracks execution progress across multiple variable groups
type BatchExecutionProgress struct {
	TotalGroups     int                      `json:"total_groups"`     // Total number of enabled groups
	EnabledGroups   []string                 `json:"enabled_groups"`   // Group IDs to execute
	CompletedGroups []string                 `json:"completed_groups"` // Group IDs that finished
	CurrentGroup    string                   `json:"current_group"`    // Currently executing group ID
	GroupProgress   map[string]*StepProgress `json:"group_progress"`   // Per-group step progress
	LastUpdated     time.Time                `json:"last_updated"`
	IterationNumber int                      `json:"iteration_number"` // Current iteration number (e.g., 1 for iteration-1)
}

// ExecutionContext represents immutable execution configuration
// Created once at execution start and passed through the call chain
type ExecutionContext struct {
	SkipHumanInput     bool                    // Whether to skip human feedback requests (auto-approve steps)
	FastExecuteMode    bool                    // Whether we're in fast execute mode
	FastExecuteEndStep int                     // Last step index to fast execute (0-based, -1 means not set)
	RunSingleStepOnly  bool                    // Whether to run only a single step and stop
	SingleStepTarget   int                     // Target step index to run (0-based)
	ResumeBranchStep   *BranchStepResumeTarget // For resuming from a specific branch step (nil if not resuming from branch)
	IsEvaluationMode   bool                    // Whether we're running evaluation steps (learnings go to evaluation/learnings/)
	StepPathOverride   string                  // If set, overrides the default "step-{N}" path for the target step (used for inner steps in workshop)

	// ConversationHistoryCapture is an optional pointer; when non-nil the execution engine
	// writes the agent's full conversation history into it after Execute() returns.
	// This is used by sub-agent callers (e.g., get_sub_agent_conversation tool) to
	// retrieve the internal conversation without modifying the execution path.
	ConversationHistoryCapture *[]llmtypes.MessageContent
}

// DecisionContext represents context from a decision step that routed to this step
// This context is passed to the next step after a decision step routes to it
type DecisionContext struct {
	DecisionStepIndex       int    // Index of the decision step that made the decision (0-based)
	DecisionStepTitle       string // Title of the decision step
	DecisionResult          bool   // The decision result (true/false)
	DecisionReasoning       string // The reasoning text from the decision evaluation
	DecisionExecutionResult string // The execution output from the decision step's inner step
}

// Execution strategy constants
// All strategies run with learning enabled and no human feedback.
const (
	// Fresh start
	ExecutionStrategyStartFromBeginningNoHuman = "start_from_beginning_no_human" // Default: fresh start with learning, no human feedback

	// Resume
	ExecutionStrategyResumeFromStepNoHuman = "resume_from_step_no_human" // Resume from specific step with learning, no human feedback

	// Single step execution
	ExecutionStrategyRunSingleStep = "run_single_step" // Run only the specified step and stop

	// Deprecated aliases — mapped to the kept strategies for backward compatibility
	ExecutionStrategyStartFromBeginning = ExecutionStrategyStartFromBeginningNoHuman // Deprecated: use StartFromBeginningNoHuman
	ExecutionStrategyFastExecuteAll     = ExecutionStrategyStartFromBeginningNoHuman // Deprecated: use StartFromBeginningNoHuman
	ExecutionStrategyResumeFromStep     = ExecutionStrategyResumeFromStepNoHuman     // Deprecated: use ResumeFromStepNoHuman
	ExecutionStrategyFastResumeFromStep = ExecutionStrategyResumeFromStepNoHuman     // Deprecated: use ResumeFromStepNoHuman
	ExecutionStrategyFastExecuteRange   = ExecutionStrategyStartFromBeginningNoHuman // Deprecated: use StartFromBeginningNoHuman

	// Plan change actions
	PlanChangeActionKeepOldProgress   = "keep_old_progress"
	PlanChangeActionDeleteOldProgress = "delete_old_progress"

	// All steps completed actions
	AllStepsCompletedActionSkipExecution = "skip_execution"
)

// TodoStep has been removed - use PlanStepInterface instead
// All execution code now uses PlanStepInterface directly for type safety

// TodoStepsExtractedEvent represents the event when todo steps are extracted from a plan
type TodoStepsExtractedEvent struct {
	baseevents.BaseEventData
	TotalStepsExtracted int                 `json:"total_steps_extracted"`
	ExtractedSteps      []PlanStepInterface `json:"extracted_steps"`
	ExtractionMethod    string              `json:"extraction_method"`
	PlanSource          string              `json:"plan_source"`          // "existing_plan" or "new_plan"
	WorkspacePath       string              `json:"workspace_path"`       // Workspace path for file operations (required)
	RunFolder           string              `json:"run_folder,omitempty"` // Run folder name for run-specific configs
}

// MarshalJSON implements custom JSON marshaling for TodoStepsExtractedEvent
// This is needed because PlanStepInterface is an interface and needs special handling
func (e *TodoStepsExtractedEvent) MarshalJSON() ([]byte, error) {
	type Alias TodoStepsExtractedEvent
	aux := &struct {
		ExtractedSteps []json.RawMessage `json:"extracted_steps"`
		*Alias
	}{
		Alias: (*Alias)(e),
	}

	// Marshal each step to JSON
	aux.ExtractedSteps = make([]json.RawMessage, len(e.ExtractedSteps))
	for i, step := range e.ExtractedSteps {
		stepJSON, err := json.Marshal(step)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal step %d: %w", i, err)
		}
		aux.ExtractedSteps[i] = stepJSON
	}

	return json.Marshal(aux)
}

// GetEventType returns the event type for TodoStepsExtractedEvent
func (e *TodoStepsExtractedEvent) GetEventType() baseevents.EventType {
	return events.TodoStepsExtracted
}
