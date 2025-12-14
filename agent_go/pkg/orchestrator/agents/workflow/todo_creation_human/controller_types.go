package todo_creation_human

import (
	"time"

	"mcpagent/events"
)

// BranchStepProgress tracks branch execution progress for conditional steps
type BranchStepProgress struct {
	BranchExecuted string   `json:"branch_executed"` // "if_true" or "if_false"
	CompletedSteps []string `json:"completed_steps"` // e.g., ["step-3-if-true-0", "step-3-if-true-1"]
}

// DecisionEvaluationCount tracks how many times a specific decision has been made
// Key format: "{stepID}:{result}" where result is "true" or "false"
type DecisionEvaluationCount map[string]int

// StepProgress tracks which steps have been completed
type StepProgress struct {
	CompletedStepIndices     []int                      `json:"completed_step_indices"` // 0-based indices
	TotalSteps               int                        `json:"total_steps"`
	LastUpdated              time.Time                  `json:"last_updated"`
	BranchSteps              map[int]BranchStepProgress `json:"branch_steps,omitempty"` // key is step index (0-based)
	DecisionEvaluationCounts DecisionEvaluationCount    `json:"-"`                      // in-memory only: tracks decision step evaluations to prevent infinite loops (not persisted)
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
	// Takes highest priority over step configs, preset defaults, and orchestrator defaults for execution agents
	// Cascading fallback: tempLLM1 → tempLLM2 → step LLM (on validation failures)
	TempOverrideLLM  *AgentLLMConfig `json:"temp_override_llm,omitempty"`  // First override LLM (used on first attempt)
	TempOverrideLLM2 *AgentLLMConfig `json:"temp_override_llm2,omitempty"` // Second override LLM (used on second attempt if tempLLM1 fails)

	// Fallback behavior when validation fails
	FallbackToOriginalLLMOnFailure bool `json:"fallback_to_original_llm_on_failure,omitempty"` // If true, use original LLM (step config > preset > orchestrator) instead of temp override when validation fails

	// Learning behavior when tempLLM is active (per-model control)
	SkipLearningWhenTempLLM1 bool `json:"skip_learning_when_temp_llm1,omitempty"` // If true, skip learning phases when tempLLM1 is used (default: false, learning runs)
	SkipLearningWhenTempLLM2 bool `json:"skip_learning_when_temp_llm2,omitempty"` // If true, skip learning phases when tempLLM2 is used (default: false, learning runs)

	// Validation response persistence
	SaveValidationResponses bool `json:"save_validation_responses,omitempty"` // If true, save validation responses to workspace validation folder (default: true)

	// Variable group execution options (for batch execution with multiple groups)
	EnabledGroupIDs []string `json:"enabled_group_ids,omitempty"` // Group IDs to execute (if empty, uses groups' enabled flags)
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
const (
	// Fresh start strategies
	ExecutionStrategyStartFromBeginning        = "start_from_beginning"          // Normal execution with learning and human feedback
	ExecutionStrategyFastExecuteAll            = "fast_execute_all"              // Fast execute all steps (skip learning and human feedback)
	ExecutionStrategyStartFromBeginningNoHuman = "start_from_beginning_no_human" // Without human feedback (learning enabled)

	// Resume strategies
	ExecutionStrategyResumeFromStep        = "resume_from_step"          // Resume from specific step (normal mode)
	ExecutionStrategyFastResumeFromStep    = "fast_resume_from_step"     // Fast resume from step
	ExecutionStrategyResumeFromStepNoHuman = "resume_from_step_no_human" // Resume without human
	ExecutionStrategyFastExecuteRange      = "fast_execute_range"        // Fast execute 0 to step X

	// Single step execution
	ExecutionStrategyRunSingleStep = "run_single_step" // Run only the specified step and stop

	// Plan change actions
	PlanChangeActionKeepOldProgress   = "keep_old_progress"
	PlanChangeActionDeleteOldProgress = "delete_old_progress"

	// All steps completed actions
	AllStepsCompletedActionFastExecuteAgain = "fast_execute_again"
	AllStepsCompletedActionSkipExecution    = "skip_execution"
)

// TodoStep represents a todo step in the execution
type TodoStep struct {
	ID                  string   `json:"id"` // Stable step ID (from PlanStep) - required for frontend matching
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	HasLoop             bool     `json:"has_loop"`                   // true if step needs to loop
	LoopCondition       string   `json:"loop_condition"`             // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations       int      `json:"max_iterations,omitempty"`   // max iterations (default: 10)
	LoopDescription     string   `json:"loop_description,omitempty"` // human-readable explanation
	// Conditional branching fields
	HasCondition      bool       `json:"has_condition"`                   // true if step has conditional branches
	ConditionQuestion string     `json:"condition_question,omitempty"`    // question to ask ConditionalLLM
	ConditionContext  string     `json:"condition_context,omitempty"`     // context to provide to ConditionalLLM
	IfTrueSteps       []TodoStep `json:"if_true_steps,omitempty"`         // nested steps for true branch
	IfFalseSteps      []TodoStep `json:"if_false_steps,omitempty"`        // nested steps for false branch
	IfTrueNextStepID  string     `json:"if_true_next_step_id,omitempty"`  // ID of step to connect to after true branch completes (or "end" to end workflow)
	IfFalseNextStepID string     `json:"if_false_next_step_id,omitempty"` // ID of step to connect to after false branch completes (or "end" to end workflow)
	ConditionResult   *bool      `json:"condition_result,omitempty"`      // runtime: stores decision result
	ConditionReason   string     `json:"condition_reason,omitempty"`      // runtime: stores LLM reasoning
	// Decision step fields (execute step, evaluate output, route based on result)
	HasDecisionStep            bool              `json:"has_decision_step,omitempty"`            // true if step executes a single step and routes based on result
	DecisionStep               *TodoStep         `json:"decision_step,omitempty"`                // The single step to execute
	DecisionEvaluationQuestion string            `json:"decision_evaluation_question,omitempty"` // Question to evaluate step output
	DecisionResult             *bool             `json:"decision_result,omitempty"`              // runtime: stores evaluation result (backward compatibility)
	DecisionReason             string            `json:"decision_reason,omitempty"`              // runtime: stores evaluation reasoning (backward compatibility)
	DecisionResponse           *DecisionResponse `json:"decision_response,omitempty"`            // runtime: stores structured decision evaluation response
	AgentConfigs               *AgentConfigs     `json:"agent_configs,omitempty"`                // per-agent configuration (LLM, max turns, toggles)
}

// TodoStepsExtractedEvent represents the event when todo steps are extracted from a plan
type TodoStepsExtractedEvent struct {
	events.BaseEventData
	TotalStepsExtracted int        `json:"total_steps_extracted"`
	ExtractedSteps      []TodoStep `json:"extracted_steps"`
	ExtractionMethod    string     `json:"extraction_method"`
	PlanSource          string     `json:"plan_source"`          // "existing_plan" or "new_plan"
	WorkspacePath       string     `json:"workspace_path"`       // Workspace path for file operations (required)
	RunFolder           string     `json:"run_folder,omitempty"` // Run folder name for run-specific configs
}

// GetEventType returns the event type for TodoStepsExtractedEvent
func (e *TodoStepsExtractedEvent) GetEventType() events.EventType {
	return events.TodoStepsExtracted
}
