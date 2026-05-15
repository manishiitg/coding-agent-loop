package step_based_workflow

import (
	"encoding/json"
	"fmt"
	"time"

	baseevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
)

// BranchStepProgress tracks branch execution progress for conditional steps
type BranchStepProgress struct {
	BranchExecuted string   `json:"branch_executed"` // "if_true" or "if_false"
	CompletedSteps []string `json:"completed_steps"` // e.g., ["step-3-if-true-0", "step-3-if-true-1"]
}

// RoutingEvaluationCount tracks how many times a specific route has been selected
// Key format: "{stepID}:{routeID}"
type RoutingEvaluationCount map[string]int

// OrchestrationRoute represents a possible route/sub-agent (private to orchestration step)
type OrchestrationRoute struct {
	RouteID       string            `json:"route_id"`                  // Unique ID for this route
	RouteName     string            `json:"route_name"`                // Human-readable name
	Condition     string            `json:"condition"`                 // Condition description (e.g., "If error is authentication-related")
	SubAgentStep  PlanStepInterface `json:"sub_agent_step"`            // The sub-agent step to execute (private, not in main workflow)
	ContextToPass string            `json:"context_to_pass,omitempty"` // Optional: specific context to pass to sub-agent
}

// StepProgress tracks which steps have been completed
type StepProgress struct {
	CompletedStepIndices    []int                      `json:"completed_step_indices"` // 0-based indices
	TotalSteps              int                        `json:"total_steps"`
	LastUpdated             time.Time                  `json:"last_updated"`
	BranchSteps             map[int]BranchStepProgress `json:"branch_steps,omitempty"`        // key is step index (0-based)
	ValidationFailures      map[string]int             `json:"validation_failures,omitempty"` // key is step path, value is failure count
	RoutingEvaluationCounts RoutingEvaluationCount     `json:"-"`                             // in-memory only: tracks routing step evaluations to prevent infinite loops (not persisted)
	ArchivalCounts          map[int]int                `json:"archival_counts,omitempty"`     // key is stepNumber (1-based), value is archive run count
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
	RunMode              string                  `json:"run_mode"`                          // "use_same_run" or "create_new_runs_always"
	SelectedRunFolder    string                  `json:"selected_run_folder,omitempty"`     // If use_same_run and user selected specific folder
	ExecutionStrategy    string                  `json:"execution_strategy"`                // Execution strategy (see constants below)
	ResumeFromStep       int                     `json:"resume_from_step,omitempty"`        // 1-based step number to resume from (for top-level steps)
	ResumeFromBranchStep *BranchStepResumeTarget `json:"resume_from_branch_step,omitempty"` // For resuming from branch steps
	PlanChangeAction     string                  `json:"plan_change_action,omitempty"`      // "keep_old_progress" or "delete_old_progress"

	// Variable group execution options (for batch execution with multiple groups)
	EnabledGroupNames []string `json:"enabled_group_names,omitempty"` // Group names to execute (if empty, uses groups' enabled flags)

	// Human input overrides: per-step responses for human_input steps (keyed by step ID).
	// When SkipHumanInput is true, these take priority over variableValues fallback.
	HumanInputs map[string]string `json:"human_inputs,omitempty"`

	// DisableEval skips the automatic evaluation pass after a successful full workflow run.
	DisableEval bool `json:"disable_eval,omitempty"`
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
	SkipHumanInput    bool                    // Whether to skip human feedback requests (auto-approve steps)
	RunSingleStepOnly bool                    // Whether to run only a single step and stop
	SingleStepTarget  int                     // Target step index to run (0-based)
	SavedScriptOnly   bool                    // Whether to run only saved learnings/{step-id}/main.py with no LLM fallback
	ResumeBranchStep  *BranchStepResumeTarget // For resuming from a specific branch step (nil if not resuming from branch)
	IsEvaluationMode  bool                    // Whether we're running evaluation steps
	StepPathOverride  string                  // If set, overrides the default "step-{N}" path for the target step (used for inner steps in workshop)

	// Human input overrides: per-step responses for human_input steps (keyed by step ID).
	// Propagated from ExecutionOptions.HumanInputs to controller.humanInputOverrides.
	HumanInputs map[string]string

	// WorkshopHumanInput is the single-step response the builder agent supplies via
	// execute_step(human_input="..."). Human input steps consume it as the response
	// substitute; executable steps receive it as high-priority prompt context.
	WorkshopHumanInput string

	// MessageSequenceRestart forces a message_sequence step to archive any existing
	// session and run its configured item queue from scratch.
	MessageSequenceRestart bool

	// ConversationHistoryCapture is an optional pointer; when non-nil the execution engine
	// writes the agent's full conversation history into it after Execute() returns.
	// This is used by sub-agent callers (e.g., get_sub_agent_conversation tool) to
	// retrieve the internal conversation without modifying the execution path.
	ConversationHistoryCapture *[]llmtypes.MessageContent
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

	// Plan change actions
	PlanChangeActionKeepOldProgress   = "keep_old_progress"
	PlanChangeActionDeleteOldProgress = "delete_old_progress"
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
