package step_based_workflow

import (
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	baseevents "github.com/manishiitg/mcpagent/events"
)

// DecisionResponseEvent represents the decision response data for events
// This is a local type that wraps the workflow DecisionResponse to match the event schema
type DecisionResponseEvent struct {
	Result     bool          `json:"result"`               // The decision result (true or false)
	Reasoning  string        `json:"reasoning"`            // Detailed reasoning for the decision
	Confidence string        `json:"confidence,omitempty"` // Optional confidence level
	Feedback   []interface{} `json:"feedback,omitempty"`   // Optional feedback array
	Evidence   []string      `json:"evidence,omitempty"`   // Optional evidence array
}

// DecisionEvaluatedEvent represents the event when a decision step is evaluated
// This implements baseevents.EventData interface to work with the event bridge
type DecisionEvaluatedEvent struct {
	baseevents.BaseEventData
	StepID           string                `json:"step_id"`           // Step ID from plan
	StepIndex        int                   `json:"step_index"`        // 0-based step index
	StepTitle        string                `json:"step_title"`        // Step title
	StepPath         string                `json:"step_path"`         // Step path (e.g., "step-8-decision")
	DecisionQuestion string                `json:"decision_question"` // The question that was evaluated
	DecisionResponse DecisionResponseEvent `json:"decision_response"` // The decision response
	RunFolder        string                `json:"run_folder"`        // Run folder name (e.g., "iteration-1")
	WorkspacePath    string                `json:"workspace_path"`    // Workspace path for file operations
}

// GetEventType implements baseevents.EventData interface
func (e *DecisionEvaluatedEvent) GetEventType() baseevents.EventType {
	return events.DecisionEvaluated
}

// StepStartedEvent represents the event when a step execution starts
type StepStartedEvent struct {
	baseevents.BaseEventData
	StepID        string `json:"step_id"`        // Step ID from plan
	StepIndex     int    `json:"step_index"`     // 0-based step index
	StepTitle     string `json:"step_title"`     // Step title
	StepPath      string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep  bool   `json:"is_branch_step"` // Whether this is a branch step
	RunFolder     string `json:"run_folder"`     // Run folder name (e.g., "iteration-1")
	WorkspacePath string `json:"workspace_path"` // Workspace path for file operations
}

func (e *StepStartedEvent) GetEventType() baseevents.EventType {
	return baseevents.StepExecutionStart
}

type StepFinishedEvent struct {
	baseevents.BaseEventData
	StepID       string `json:"step_id"`        // Step ID from plan
	StepIndex    int    `json:"step_index"`     // 0-based step index
	StepTitle    string `json:"step_title"`     // Step title
	StepPath     string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep bool   `json:"is_branch_step"` // Whether this is a branch step
}

func (e *StepFinishedEvent) GetEventType() baseevents.EventType {
	return baseevents.StepExecutionEnd
}

// StepFailedEvent represents the event when a step execution fails
type StepFailedEvent struct {
	baseevents.BaseEventData
	StepID       string `json:"step_id"`        // Step ID from plan
	StepIndex    int    `json:"step_index"`     // 0-based step index
	StepTitle    string `json:"step_title"`     // Step title
	StepPath     string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep bool   `json:"is_branch_step"` // Whether this is a branch step
	Error        string `json:"error"`          // Error message
}

func (e *StepFailedEvent) GetEventType() baseevents.EventType {
	return baseevents.StepExecutionFailed
}

// StepTokenUsageEvent represents token usage summary for a workflow step
// Note: This is a local type, but uses orchestrator events.StepTokenUsageEvent from the orchestrator/events package
type StepTokenUsageEvent struct {
	baseevents.BaseEventData
	Phase                 string `json:"phase"`                // e.g., "execution"
	Step                  int    `json:"step"`                 // step index (0-based)
	StepTitle             string `json:"step_title,omitempty"` // optional step title for display
	PromptTokens          int    `json:"prompt_tokens"`
	CompletionTokens      int    `json:"completion_tokens"`
	TotalTokens           int    `json:"total_tokens"`
	CacheTokens           int    `json:"cache_tokens"`
	ReasoningTokens       int    `json:"reasoning_tokens"`
	LLMCallCount          int    `json:"llm_call_count"`
	CacheEnabledCallCount int    `json:"cache_enabled_call_count"`
	// Pricing fields (in USD)
	InputCost     float64 `json:"input_cost_usd,omitempty"`
	OutputCost    float64 `json:"output_cost_usd,omitempty"`
	ReasoningCost float64 `json:"reasoning_cost_usd,omitempty"`
	CacheCost     float64 `json:"cache_cost_usd,omitempty"`
	TotalCost     float64 `json:"total_cost_usd,omitempty"`
	// Context window tracking
	ContextUsagePercent float64 `json:"context_usage_percent,omitempty"`
}

func (e *StepTokenUsageEvent) GetEventType() baseevents.EventType {
	return events.StepTokenUsage
}

// NewStepTokenUsageEvent creates a new StepTokenUsageEvent
func NewStepTokenUsageEvent(phase string, step int, stepTitle string, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int) *StepTokenUsageEvent {
	return &StepTokenUsageEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
		},
		Phase:                 phase,
		Step:                  step,
		StepTitle:             stepTitle,
		PromptTokens:          promptTokens,
		CompletionTokens:      completionTokens,
		TotalTokens:           totalTokens,
		CacheTokens:           cacheTokens,
		ReasoningTokens:       reasoningTokens,
		LLMCallCount:          llmCallCount,
		CacheEnabledCallCount: cacheEnabledCallCount,
	}
}

// StepProgressUpdatedEvent represents the event when step progress is updated (steps_done.json changes)
type StepProgressUpdatedEvent struct {
	baseevents.BaseEventData
	WorkspacePath string `json:"workspace_path"`            // Workspace path for file operations
	RunFolder     string `json:"run_folder"`                // Run folder name (e.g., "iteration-1")
	CurrentStepId string `json:"current_step_id,omitempty"` // Step ID of the current step (starting, running, or completed)
	Status        string `json:"status,omitempty"`          // Step status: "start", "end", or empty (for progress updates)
	// Batch execution info (always present since backend always runs in batch context)
	GroupId     string `json:"group_id,omitempty"`     // Current group ID being executed
	GroupIndex  int    `json:"group_index"`            // 0-based index of current group
	TotalGroups int    `json:"total_groups"`           // Total number of groups in batch
}

func (e *StepProgressUpdatedEvent) GetEventType() baseevents.EventType {
	return events.StepProgressUpdated
}

// IndependentStepsSelectedEvent represents the event when independent steps are selected for parallel execution
type IndependentStepsSelectedEvent struct {
	baseevents.BaseEventData
	StepIndices    []int    `json:"step_indices"`    // Indices of steps selected for parallel execution
	StepTitles     []string `json:"step_titles"`     // Titles of selected steps
	TotalSteps     int      `json:"total_steps"`     // Total number of steps in plan
	ExecutionBatch int      `json:"execution_batch"` // Which batch of parallel execution this is
}

func (e *IndependentStepsSelectedEvent) GetEventType() baseevents.EventType {
	return events.IndependentStepsSelected
}

// PreValidationCompletedEvent represents the event when pre-validation completes
type PreValidationCompletedEvent struct {
	baseevents.BaseEventData
	StepID        string                    `json:"step_id"`          // Step ID from plan
	StepIndex     int                       `json:"step_index"`       // 0-based step index
	StepTitle     string                    `json:"step_title"`       // Step title
	StepPath      string                    `json:"step_path"`        // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep  bool                      `json:"is_branch_step"`   // Whether this is a branch step
	OverallPass   bool                      `json:"overall_pass"`     // Whether pre-validation passed
	TotalChecks   int                       `json:"total_checks"`     // Total number of checks performed
	PassedChecks  int                       `json:"passed_checks"`    // Number of checks that passed
	FailedChecks  int                       `json:"failed_checks"`    // Number of checks that failed
	FilesChecked  []FileCheckResultForEvent `json:"files_checked"`    // Results for each file checked
	Errors        []ValidationErrorForEvent `json:"errors,omitempty"` // Validation errors if any
	RunFolder     string                    `json:"run_folder"`       // Run folder name (e.g., "iteration-1")
	WorkspacePath string                    `json:"workspace_path"`   // Workspace path for file operations
}

// FileCheckResultForEvent is a simplified version of FileCheckResult for events
type FileCheckResultForEvent struct {
	FileName   string                    `json:"file_name"`   // Name of the file checked
	Exists     bool                      `json:"exists"`      // Whether file exists
	IsJSON     bool                      `json:"is_json"`     // Whether file is valid JSON
	JSONChecks []JSONCheckResultForEvent `json:"json_checks"` // Results of JSON validation checks
}

// JSONCheckResultForEvent is a simplified version of JSONCheckResult for events
type JSONCheckResultForEvent struct {
	Path      string `json:"path"`                // JSONPath expression
	Passed    bool   `json:"passed"`              // Whether check passed
	CheckType string `json:"check_type"`          // Type of check (must_exist, value_type, etc.)
	ErrorMsg  string `json:"error_msg,omitempty"` // Error message if check failed
}

// ValidationErrorForEvent is a simplified version of ValidationError for events
type ValidationErrorForEvent struct {
	File      string `json:"file"`       // File where error occurred
	Path      string `json:"path"`       // JSONPath where error occurred
	CheckType string `json:"check_type"` // Type of check that failed
	Expected  string `json:"expected"`   // Expected value
	Actual    string `json:"actual"`     // Actual value
	Message   string `json:"message"`    // Error message
}

func (e *PreValidationCompletedEvent) GetEventType() baseevents.EventType {
	return events.PreValidationCompleted
}
