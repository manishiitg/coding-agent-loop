package todo_creation_human

import (
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

// StepStartedEvent represents the event when a workflow step execution starts
// This implements events.EventData interface to work with the event bridge
type StepStartedEvent struct {
	events.BaseEventData
	StepID        string `json:"step_id"`        // Step ID from plan
	StepIndex     int    `json:"step_index"`     // 0-based step index
	StepTitle     string `json:"step_title"`     // Step title
	StepPath      string `json:"step_path"`      // Step path (e.g., "step-3")
	IsBranchStep  bool   `json:"is_branch_step"` // Whether this is a branch step
	RunFolder     string `json:"run_folder"`     // Run folder name (e.g., "iteration-1")
	WorkspacePath string `json:"workspace_path"` // Workspace path for file operations
}

// GetEventType implements events.EventData interface
func (e *StepStartedEvent) GetEventType() events.EventType {
	return events.StepExecutionStart
}

// StepFinishedEvent represents the event when a workflow step execution finishes
// This implements events.EventData interface to work with the event bridge
type StepFinishedEvent struct {
	events.BaseEventData
	StepID       string `json:"step_id"`        // Step ID from plan
	StepIndex    int    `json:"step_index"`     // 0-based step index
	StepTitle    string `json:"step_title"`     // Step title
	StepPath     string `json:"step_path"`      // Step path (e.g., "step-3")
	IsBranchStep bool   `json:"is_branch_step"` // Whether this is a branch step
}

// GetEventType implements events.EventData interface
func (e *StepFinishedEvent) GetEventType() events.EventType {
	return events.StepExecutionEnd
}

// StepProgressUpdatedEvent represents the event when step progress is updated
// This implements events.EventData interface to work with the event bridge
type StepProgressUpdatedEvent struct {
	events.BaseEventData
	CompletedStepIndices []int                             `json:"completed_step_indices"` // 0-based indices of completed steps
	TotalSteps           int                               `json:"total_steps"`            // Total number of steps
	WorkspacePath        string                            `json:"workspace_path"`         // Workspace path for file operations
	RunFolder            string                            `json:"run_folder"`             // Run folder name (e.g., "iteration-1")
	LastCompletedStep    int                               `json:"last_completed_step"`    // Highest completed step index (-1 if none)
	BranchSteps          map[int]events.BranchStepProgress `json:"branch_steps,omitempty"` // Branch step progress by step index
}

// GetEventType implements events.EventData interface
func (e *StepProgressUpdatedEvent) GetEventType() events.EventType {
	return events.StepProgressUpdated
}

// IndependentStepsSelectedEvent represents the event when independent steps are selected for parallel execution
// This implements events.EventData interface to work with the event bridge
type IndependentStepsSelectedEvent struct {
	events.BaseEventData
	StepIndices    []int    `json:"step_indices,omitempty"`    // 0-based indices of selected steps
	StepTitles     []string `json:"step_titles,omitempty"`     // Titles of selected steps
	TotalSteps     int      `json:"total_steps,omitempty"`     // Total number of steps
	ExecutionBatch int      `json:"execution_batch,omitempty"` // Batch number for execution
}

// GetEventType implements events.EventData interface
func (e *IndependentStepsSelectedEvent) GetEventType() events.EventType {
	return events.IndependentStepsSelected
}
