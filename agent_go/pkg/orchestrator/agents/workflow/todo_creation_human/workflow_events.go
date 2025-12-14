package todo_creation_human

import (
	"time"

	"mcpagent/events"
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
// This implements events.EventData interface to work with the event bridge
type DecisionEvaluatedEvent struct {
	events.BaseEventData
	StepID           string                `json:"step_id"`           // Step ID from plan
	StepIndex        int                   `json:"step_index"`        // 0-based step index
	StepTitle        string                `json:"step_title"`        // Step title
	StepPath         string                `json:"step_path"`         // Step path (e.g., "step-8-decision")
	DecisionQuestion string                `json:"decision_question"` // The question that was evaluated
	DecisionResponse DecisionResponseEvent `json:"decision_response"` // The decision response
	RunFolder        string                `json:"run_folder"`        // Run folder name (e.g., "iteration-1")
	WorkspacePath    string                `json:"workspace_path"`    // Workspace path for file operations
}

// GetEventType implements events.EventData interface
func (e *DecisionEvaluatedEvent) GetEventType() events.EventType {
	return events.EventType("decision_evaluated")
}

// StepStartedEvent represents the event when a step execution starts
type StepStartedEvent struct {
	events.BaseEventData
	StepID        string `json:"step_id"`        // Step ID from plan
	StepIndex     int    `json:"step_index"`     // 0-based step index
	StepTitle     string `json:"step_title"`     // Step title
	StepPath      string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep  bool   `json:"is_branch_step"` // Whether this is a branch step
	RunFolder     string `json:"run_folder"`     // Run folder name (e.g., "iteration-1")
	WorkspacePath string `json:"workspace_path"` // Workspace path for file operations
}

func (e *StepStartedEvent) GetEventType() events.EventType {
	return events.StepExecutionStart
}

// StepFinishedEvent represents the event when a step execution completes successfully
type StepFinishedEvent struct {
	events.BaseEventData
	StepID       string `json:"step_id"`        // Step ID from plan
	StepIndex    int    `json:"step_index"`     // 0-based step index
	StepTitle    string `json:"step_title"`     // Step title
	StepPath     string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep bool   `json:"is_branch_step"` // Whether this is a branch step
}

func (e *StepFinishedEvent) GetEventType() events.EventType {
	return events.StepExecutionEnd
}

// StepFailedEvent represents the event when a step execution fails
type StepFailedEvent struct {
	events.BaseEventData
	StepID       string `json:"step_id"`        // Step ID from plan
	StepIndex    int    `json:"step_index"`     // 0-based step index
	StepTitle    string `json:"step_title"`     // Step title
	StepPath     string `json:"step_path"`      // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep bool   `json:"is_branch_step"` // Whether this is a branch step
	Error        string `json:"error"`          // Error message
}

func (e *StepFailedEvent) GetEventType() events.EventType {
	return events.StepExecutionFailed
}

// StepTokenUsageEvent represents token usage summary for a workflow step
type StepTokenUsageEvent struct {
	events.BaseEventData
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

func (e *StepTokenUsageEvent) GetEventType() events.EventType {
	return events.StepTokenUsage
}

// NewStepTokenUsageEvent creates a new StepTokenUsageEvent
func NewStepTokenUsageEvent(phase string, step int, stepTitle string, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int) *StepTokenUsageEvent {
	return &StepTokenUsageEvent{
		BaseEventData: events.BaseEventData{
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
	events.BaseEventData
	CompletedStepIndices []int                      `json:"completed_step_indices"` // 0-based indices of completed steps
	TotalSteps           int                        `json:"total_steps"`            // Total number of steps in the plan
	WorkspacePath        string                     `json:"workspace_path"`         // Workspace path for file operations
	RunFolder            string                     `json:"run_folder"`             // Run folder name (e.g., "iteration-1")
	LastCompletedStep    int                        `json:"last_completed_step"`    // Most recently completed step index (-1 if unknown)
	BranchSteps          map[int]BranchStepProgress `json:"branch_steps,omitempty"` // Branch step progress for conditional steps
}

func (e *StepProgressUpdatedEvent) GetEventType() events.EventType {
	return events.StepProgressUpdated
}

// IndependentStepsSelectedEvent represents the event when independent steps are selected for parallel execution
type IndependentStepsSelectedEvent struct {
	events.BaseEventData
	StepIndices    []int    `json:"step_indices"`    // Indices of steps selected for parallel execution
	StepTitles     []string `json:"step_titles"`     // Titles of selected steps
	TotalSteps     int      `json:"total_steps"`     // Total number of steps in plan
	ExecutionBatch int      `json:"execution_batch"` // Which batch of parallel execution this is
}

func (e *IndependentStepsSelectedEvent) GetEventType() events.EventType {
	return events.IndependentStepsSelected
}
