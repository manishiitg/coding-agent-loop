package events

import (
	"github.com/manishiitg/mcpagent/events"
)

// AgentSessionIDKey is a context key for injecting agent session ID into event context.
// When set, the ContextAwareEventBridge tags events with this correlation ID,
// enabling the frontend to group tool calls under their parent orchestrator_agent_start.
type agentSessionIDKeyType struct{}

var AgentSessionIDKey = agentSessionIDKeyType{}

// IsSubAgentContextKey marks a context as belonging to a sub-agent (not the root agent).
// Only sub-agent events should have their correlation_id overridden for grouping.
type isSubAgentContextKeyType struct{}

var IsSubAgentContextKey = isSubAgentContextKeyType{}

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
	BlockingHumanQuestions    events.EventType = "blocking_human_questions"
	PlanApproval              events.EventType = "plan_approval"

	// Step token usage event
	StepTokenUsage events.EventType = "step_token_usage"

	// Learning events
	LearningSkipped events.EventType = "learning_skipped"
	TempLLMSkipped  events.EventType = "temp_llm_skipped"

	// Decision step evaluation events
	DecisionEvaluated events.EventType = "decision_evaluated"

	// Routing step evaluation events
	RoutingEvaluated events.EventType = "routing_evaluated"

	// Pre-validation events
	PreValidationCompleted events.EventType = "pre_validation_completed"

	// Todo task orchestration events
	TodoTaskRouteSelected  events.EventType = "todo_task_route_selected"  // When orchestrator selects a route/sub-agent
	TodoTaskItemCreated    events.EventType = "todo_task_item_created"    // When a todo item is created
	TodoTaskItemUpdated    events.EventType = "todo_task_item_updated"    // When a todo item is updated
	TodoTaskItemCompleted  events.EventType = "todo_task_item_completed"  // When a todo item is completed
	TodoTaskStepCompleted  events.EventType = "todo_task_step_completed"  // When the entire todo task step is completed
	TodoTaskStatusUpdate   events.EventType = "todo_task_status_update"   // Current tasks.md content after a sub-agent completes

)

// Helper function to get component from orchestrator event type
func GetComponentFromEventType(eventType events.EventType) string {
	switch eventType {
	case OrchestratorStart, OrchestratorEnd, OrchestratorError,
		OrchestratorAgentStart, OrchestratorAgentEnd, OrchestratorAgentError,
		IndependentStepsSelected, TodoStepsExtracted, VariablesExtracted,
		StepTokenUsage, StepProgressUpdated,
		BatchExecutionStart, BatchGroupStart, BatchGroupEnd, BatchExecutionEnd, BatchExecutionCanceled,
		HumanVerificationResponse, RequestHumanFeedback, BlockingHumanFeedback, BlockingHumanQuestions, PlanApproval,
		LearningSkipped, TempLLMSkipped,
		DecisionEvaluated, RoutingEvaluated, PreValidationCompleted,
		TodoTaskRouteSelected, TodoTaskItemCreated, TodoTaskItemUpdated, TodoTaskItemCompleted, TodoTaskStepCompleted, TodoTaskStatusUpdate:
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
