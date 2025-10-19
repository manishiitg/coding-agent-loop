package events

// StructuredOutputEvent represents structured output operation events
// This is a shared event type used across different packages for structured output operations
type StructuredOutputEvent struct {
	BaseEventData
	Operation string `json:"operation"`
	EventType string `json:"event_type"`
	Error     string `json:"error,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

// GetEventType returns the event type for StructuredOutputEvent
func (e *StructuredOutputEvent) GetEventType() EventType {
	switch e.EventType {
	case string(StructuredOutputStart):
		return StructuredOutputStart
	case string(StructuredOutputEnd):
		return StructuredOutputEnd
	case string(StructuredOutputError):
		return StructuredOutputError
	default:
		return StructuredOutputStart // Default fallback
	}
}
