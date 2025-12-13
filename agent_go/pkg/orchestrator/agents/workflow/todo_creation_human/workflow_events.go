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
