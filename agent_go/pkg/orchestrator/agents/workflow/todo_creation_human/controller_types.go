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

// StepProgress tracks which steps have been completed
type StepProgress struct {
	CompletedStepIndices []int                      `json:"completed_step_indices"` // 0-based indices
	TotalSteps           int                        `json:"total_steps"`
	LastUpdated          time.Time                  `json:"last_updated"`
	BranchSteps          map[int]BranchStepProgress `json:"branch_steps,omitempty"` // key is step index (0-based)
}

// ExecutionOptions represents user-selected execution options from frontend
// When provided, backend will use these options instead of asking interactively
type ExecutionOptions struct {
	RunMode                 string `json:"run_mode"`                             // "use_same_run" or "create_new_runs_always"
	SelectedRunFolder       string `json:"selected_run_folder,omitempty"`        // If use_same_run and user selected specific folder
	ExecutionStrategy       string `json:"execution_strategy"`                   // Execution strategy (see constants below)
	ResumeFromStep          int    `json:"resume_from_step,omitempty"`           // 1-based step number to resume from
	FastExecuteEndStep      int    `json:"fast_execute_end_step,omitempty"`      // 0-based last step for fast execute range
	PlanChangeAction        string `json:"plan_change_action,omitempty"`         // "keep_old_progress" or "delete_old_progress"
	AllStepsCompletedAction string `json:"all_steps_completed_action,omitempty"` // "fast_execute_again" or "skip_execution"
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
	ID                       string   `json:"id"` // Stable step ID (from PlanStep) - required for frontend matching
	Title                    string   `json:"title"`
	Description              string   `json:"description"`
	SuccessCriteria          string   `json:"success_criteria"`
	ContextDependencies      []string `json:"context_dependencies"`
	ContextOutput            string   `json:"context_output"`
	LearningFilesToReference []string `json:"learning_files_to_reference,omitempty"` // learning files to read for context (execution agent reads full files)
	HasLoop                  bool     `json:"has_loop"`                              // true if step needs to loop
	LoopCondition            string   `json:"loop_condition"`                        // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations            int      `json:"max_iterations,omitempty"`              // max iterations (default: 10)
	LoopDescription          string   `json:"loop_description,omitempty"`            // human-readable explanation
	// Conditional branching fields
	HasCondition      bool          `json:"has_condition"`                // true if step has conditional branches
	ConditionQuestion string        `json:"condition_question,omitempty"` // question to ask ConditionalLLM
	ConditionContext  string        `json:"condition_context,omitempty"`  // context to provide to ConditionalLLM
	IfTrueSteps       []TodoStep    `json:"if_true_steps,omitempty"`      // nested steps for true branch
	IfFalseSteps      []TodoStep    `json:"if_false_steps,omitempty"`     // nested steps for false branch
	ConditionResult   *bool         `json:"condition_result,omitempty"`   // runtime: stores decision result
	ConditionReason   string        `json:"condition_reason,omitempty"`   // runtime: stores LLM reasoning
	AgentConfigs      *AgentConfigs `json:"agent_configs,omitempty"`      // per-agent configuration (LLM, max turns, toggles)
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
