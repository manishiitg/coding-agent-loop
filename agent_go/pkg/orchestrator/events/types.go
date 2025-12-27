package events

import (
	"mcpagent/events"
)

// Orchestrator Event Types
// These events are specific to the orchestrator application and are not part of the core mcpagent library
const (
	// Orchestrator events
	OrchestratorStart events.EventType = "orchestrator_start"
	OrchestratorEnd   events.EventType = "orchestrator_end"
	OrchestratorError events.EventType = "orchestrator_error"

	// Orchestrator Agent lifecycle events
	OrchestratorAgentStart events.EventType = "orchestrator_agent_start"
	OrchestratorAgentEnd   events.EventType = "orchestrator_agent_end"
	OrchestratorAgentError events.EventType = "orchestrator_agent_error"

	// Parallel execution events
	IndependentStepsSelected events.EventType = "independent_steps_selected"

	// Todo planning events
	TodoStepsExtracted  events.EventType = "todo_steps_extracted"
	VariablesExtracted  events.EventType = "variables_extracted"
	StepProgressUpdated events.EventType = "step_progress_updated"

	// Batch execution events (for variable groups)
	BatchExecutionStart    events.EventType = "batch_execution_start"
	BatchGroupStart        events.EventType = "batch_group_start"
	BatchGroupEnd          events.EventType = "batch_group_end"
	BatchExecutionEnd      events.EventType = "batch_execution_end"
	BatchExecutionCanceled events.EventType = "batch_execution_canceled"

	// Human Verification events
	HumanVerificationResponse events.EventType = "human_verification_response"
	RequestHumanFeedback      events.EventType = "request_human_feedback"
	BlockingHumanFeedback     events.EventType = "blocking_human_feedback"

	// Step token usage event
	StepTokenUsage events.EventType = "step_token_usage"

	// Learning events
	LearningSkipped events.EventType = "learning_skipped"
	TempLLMSkipped  events.EventType = "temp_llm_skipped"

	// Decision step evaluation events
	DecisionEvaluated events.EventType = "decision_evaluated"

	// Pre-validation events
	PreValidationCompleted events.EventType = "pre_validation_completed"

	// Step execution events
	StepExecutionStart  events.EventType = "step_execution_start"
	StepExecutionEnd    events.EventType = "step_execution_end"
	StepExecutionFailed events.EventType = "step_execution_failed"
)

// Helper function to get component from orchestrator event type
func GetComponentFromEventType(eventType events.EventType) string {
	switch eventType {
	case OrchestratorStart, OrchestratorEnd, OrchestratorError,
		OrchestratorAgentStart, OrchestratorAgentEnd, OrchestratorAgentError,
		IndependentStepsSelected, TodoStepsExtracted, VariablesExtracted,
		StepTokenUsage, StepProgressUpdated,
		BatchExecutionStart, BatchGroupStart, BatchGroupEnd, BatchExecutionEnd, BatchExecutionCanceled,
		HumanVerificationResponse, RequestHumanFeedback, BlockingHumanFeedback,
		LearningSkipped, TempLLMSkipped,
		DecisionEvaluated, PreValidationCompleted,
		StepExecutionStart, StepExecutionEnd, StepExecutionFailed:
		return "orchestrator"
	default:
		return "system"
	}
}

// Helper function to check if event is a start event
func IsStartEvent(eventType events.EventType) bool {
	return eventType == OrchestratorStart ||
		eventType == OrchestratorAgentStart
}

// Helper function to check if event is an end event
func IsEndEvent(eventType events.EventType) bool {
	return eventType == OrchestratorEnd ||
		eventType == OrchestratorAgentEnd
}
