package events

import (
	"time"

	"github.com/manishiitg/mcpagent/events"
)

// Orchestrator Events
type OrchestratorStartEvent struct {
	events.BaseEventData
	Objective        string `json:"objective"`
	AgentsCount      int    `json:"agents_count"`
	ServersCount     int    `json:"servers_count"`
	Configuration    string `json:"configuration,omitempty"`
	OrchestratorType string `json:"orchestrator_type,omitempty"`
	ExecutionMode    string `json:"execution_mode,omitempty"`
}

func (e *OrchestratorStartEvent) GetEventType() events.EventType {
	return OrchestratorStart
}

type OrchestratorEndEvent struct {
	events.BaseEventData
	Objective        string        `json:"objective"`
	Result           string        `json:"result"`
	Duration         time.Duration `json:"duration"`
	Status           string        `json:"status"`
	Error            string        `json:"error,omitempty"`
	OrchestratorType string        `json:"orchestrator_type,omitempty"`
	ExecutionMode    string        `json:"execution_mode,omitempty"`
}

func (e *OrchestratorEndEvent) GetEventType() events.EventType {
	return OrchestratorEnd
}

type OrchestratorErrorEvent struct {
	events.BaseEventData
	Context          string        `json:"context"`
	Error            string        `json:"error"`
	Duration         time.Duration `json:"duration"`
	OrchestratorType string        `json:"orchestrator_type,omitempty"`
	ExecutionMode    string        `json:"execution_mode,omitempty"`
}

func (e *OrchestratorErrorEvent) GetEventType() events.EventType {
	return OrchestratorError
}

// Orchestrator Agent Events
type OrchestratorAgentStartEvent struct {
	events.BaseEventData
	AgentType    string            `json:"agent_type"`           // planning, execution, validation, organizer
	AgentName    string            `json:"agent_name"`           // specific agent name
	Objective    string            `json:"objective"`            // what the agent is trying to accomplish
	InputData    map[string]string `json:"input_data"`           // template variables passed to agent
	ModelID      string            `json:"model_id"`             // which LLM model
	Provider     string            `json:"provider"`             // which LLM provider
	ServersCount int               `json:"servers_count"`        // number of MCP servers available
	MaxTurns     int               `json:"max_turns"`            // maximum conversation turns
	PlanID       string            `json:"plan_id,omitempty"`    // associated plan ID
	StepIndex    int               `json:"step_index,omitempty"` // which step in the plan
	Iteration    int               `json:"iteration,omitempty"`  // which iteration of the loop
}

func (e *OrchestratorAgentStartEvent) GetEventType() events.EventType {
	return OrchestratorAgentStart
}

type OrchestratorAgentEndEvent struct {
	events.BaseEventData
	AgentType          string                 `json:"agent_type"`                    // planning, execution, validation, organizer
	AgentName          string                 `json:"agent_name"`                    // specific agent name
	Objective          string                 `json:"objective"`                     // what the agent was trying to accomplish
	InputData          map[string]string      `json:"input_data"`                    // template variables passed to agent
	Result             string                 `json:"result"`                        // agent's output/result (text summary)
	StructuredResponse map[string]interface{} `json:"structured_response,omitempty"` // structured response data (for ExecuteStructured calls)
	Success            bool                   `json:"success"`                       // whether agent completed successfully
	Error              string                 `json:"error,omitempty"`               // error message if failed
	Duration           time.Duration          `json:"duration"`                      // how long the agent took
	ModelID            string                 `json:"model_id"`                      // which LLM model was used
	Provider           string                 `json:"provider"`                      // which LLM provider
	ServersCount       int                    `json:"servers_count"`                 // number of MCP servers used
	MaxTurns           int                    `json:"max_turns"`                     // maximum conversation turns
	PlanID             string                 `json:"plan_id,omitempty"`             // associated plan ID
	StepIndex          int                    `json:"step_index,omitempty"`          // which step in the plan
	Iteration          int                    `json:"iteration,omitempty"`           // which iteration of the loop
	// Token usage fields
	PromptTokens          int `json:"prompt_tokens,omitempty"`
	CompletionTokens      int `json:"completion_tokens,omitempty"`
	TotalTokens           int `json:"total_tokens,omitempty"`
	CacheTokens           int `json:"cache_tokens,omitempty"`
	ReasoningTokens       int `json:"reasoning_tokens,omitempty"`
	LLMCallCount          int `json:"llm_call_count,omitempty"`
	CacheEnabledCallCount int `json:"cache_enabled_call_count,omitempty"`
}

func (e *OrchestratorAgentEndEvent) GetEventType() events.EventType {
	return OrchestratorAgentEnd
}

type OrchestratorAgentErrorEvent struct {
	events.BaseEventData
	AgentType    string        `json:"agent_type"`           // planning, execution, validation, organizer
	AgentName    string        `json:"agent_name"`           // specific agent name
	Objective    string        `json:"objective"`            // what the agent was trying to accomplish
	Error        string        `json:"error"`                // error message
	Duration     time.Duration `json:"duration"`             // how long before error occurred
	ModelID      string        `json:"model_id"`             // which LLM model was used
	Provider     string        `json:"provider"`             // which LLM provider
	ServersCount int           `json:"servers_count"`        // number of MCP servers available
	MaxTurns     int           `json:"max_turns"`            // maximum conversation turns
	PlanID       string        `json:"plan_id,omitempty"`    // associated plan ID
	StepIndex    int           `json:"step_index,omitempty"` // which step in the plan
	Iteration    int           `json:"iteration,omitempty"`  // which iteration of the loop
}

func (e *OrchestratorAgentErrorEvent) GetEventType() events.EventType {
	return OrchestratorAgentError
}

type HumanVerificationResponseEvent struct {
	events.BaseEventData
	SessionID        string `json:"session_id"`
	WorkflowID       string `json:"workflow_id"`
	Response         string `json:"response"`          // "approved", "rejected", or revision feedback
	Feedback         string `json:"feedback"`          // Human feedback text
	RequiresRevision bool   `json:"requires_revision"` // Whether todo list needs revision
}

func (e *HumanVerificationResponseEvent) GetEventType() events.EventType {
	return HumanVerificationResponse
}

type RequestHumanFeedbackEvent struct {
	events.BaseEventData
	Objective        string `json:"objective"`
	TodoListMarkdown string `json:"todo_list_markdown"`
	SessionID        string `json:"session_id"`
	WorkflowID       string `json:"workflow_id"`
	RequestID        string `json:"request_id"` // Unique ID for this feedback request
	// NEW: Dynamic verification fields
	VerificationType  string `json:"verification_type,omitempty"`  // "planning_verification", "refinement_verification", "report_verification"
	NextPhase         string `json:"next_phase,omitempty"`         // The phase to transition to after approval
	Title             string `json:"title,omitempty"`              // Custom title text
	ActionLabel       string `json:"action_label,omitempty"`       // Custom button text
	ActionDescription string `json:"action_description,omitempty"` // Custom description text
}

func (e *RequestHumanFeedbackEvent) GetEventType() events.EventType {
	return RequestHumanFeedback
}

type BlockingHumanFeedbackEvent struct {
	events.BaseEventData
	Question      string   `json:"question"`       // Question to ask user
	AllowFeedback bool     `json:"allow_feedback"` // Whether to allow text feedback (defaults to true)
	Context       string   `json:"context"`        // Additional context (e.g., validation results)
	SessionID     string   `json:"session_id"`
	WorkflowID    string   `json:"workflow_id"`
	RequestID     string   `json:"request_id"`          // Unique ID for this feedback request
	YesNoOnly     bool     `json:"yes_no_only"`         // If true, show only Approve/Reject buttons (no textarea)
	YesLabel      string   `json:"yes_label,omitempty"` // Custom label for Approve button (default: "Approve")
	NoLabel       string   `json:"no_label,omitempty"`  // Custom label for Reject button (default: "Reject")
	Options       []string `json:"options,omitempty"`   // Array of option labels for multiple choice (renders as buttons)
}

func (e *BlockingHumanFeedbackEvent) GetEventType() events.EventType {
	return BlockingHumanFeedback
}

// BlockingHumanQuestionsQuestion represents a single question in the human_questions tool
type BlockingHumanQuestionsQuestion struct {
	ID       string `json:"id"`
	Question string `json:"question"`
}

// BlockingHumanQuestionsEvent is emitted when the human_questions tool needs structured answers
type BlockingHumanQuestionsEvent struct {
	events.BaseEventData
	RequestID string                           `json:"request_id"`
	Questions []BlockingHumanQuestionsQuestion `json:"questions"`
	SessionID string                           `json:"session_id"`
}

func (e *BlockingHumanQuestionsEvent) GetEventType() events.EventType {
	return BlockingHumanQuestions
}

// TodoStep represents a todo step in the execution
type TodoStep struct {
	ID                  string   `json:"id,omitempty"` // Stable step ID (from PlanStep) - required for frontend matching
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	SuccessPatterns     []string `json:"success_patterns,omitempty"` // what worked (includes tools)
	FailurePatterns     []string `json:"failure_patterns,omitempty"` // what failed (includes tools to avoid)
}

// BranchStepProgress tracks branch execution progress for conditional steps
type BranchStepProgress struct {
	BranchExecuted string   `json:"branch_executed"` // "if_true" or "if_false"
	CompletedSteps []string `json:"completed_steps"` // e.g., ["step-3-if-true-0", "step-3-if-true-1"]
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
	return StepTokenUsage
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

// NewStepTokenUsageEventWithPricing creates a new StepTokenUsageEvent with pricing and context usage
func NewStepTokenUsageEventWithPricing(phase string, step int, stepTitle string, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int, inputCost, outputCost, reasoningCost, cacheCost, totalCost float64, contextUsagePercent float64) *StepTokenUsageEvent {
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
		InputCost:             inputCost,
		OutputCost:            outputCost,
		ReasoningCost:         reasoningCost,
		CacheCost:             cacheCost,
		TotalCost:             totalCost,
		ContextUsagePercent:   contextUsagePercent,
	}
}

// LearningSkippedEvent represents the event when learning is skipped due to temp LLM override
type LearningSkippedEvent struct {
	events.BaseEventData
	StepID          string `json:"step_id"`                     // Step ID from plan
	StepIndex       int    `json:"step_index"`                  // 0-based step index
	StepTitle       string `json:"step_title"`                  // Step title
	StepPath        string `json:"step_path"`                   // Step path (e.g., "step-1" or "step-1-if-true-0")
	IsBranchStep    bool   `json:"is_branch_step"`              // Whether this is a branch step
	Reason          string `json:"reason"`                      // Reason for skipping (e.g., "temp_llm_override")
	TempLLMProvider string `json:"temp_llm_provider,omitempty"` // Temp override LLM provider
	TempLLMModel    string `json:"temp_llm_model,omitempty"`    // Temp override LLM model
	RunFolder       string `json:"run_folder"`                  // Run folder name (e.g., "iteration-1")
	WorkspacePath   string `json:"workspace_path"`              // Workspace path
}

func (e *LearningSkippedEvent) GetEventType() events.EventType {
	return LearningSkipped
}

// DecisionFeedback represents feedback from decision evaluation
type DecisionFeedback struct {
	Type        string `json:"type"`        // "observation", "recommendation", "issue"
	Description string `json:"description"` // Brief description
	Severity    string `json:"severity"`    // HIGH/MEDIUM/LOW
}

// DecisionResponse represents the structured response from decision evaluation
type DecisionResponse struct {
	Result     bool               `json:"result"`     // The decision result (true or false)
	Reasoning  string             `json:"reasoning"`  // Detailed reasoning for the decision
	Confidence string             `json:"confidence"` // HIGH/MEDIUM/LOW - confidence level in the decision
	Feedback   []DecisionFeedback `json:"feedback"`   // Optional feedback and observations
	Evidence   []string           `json:"evidence"`   // List of evidence points that support the decision
}

// DecisionEvaluatedEvent represents the event when a decision step evaluation completes
type DecisionEvaluatedEvent struct {
	events.BaseEventData
	StepID           string           `json:"step_id"`           // Step ID from plan
	StepIndex        int              `json:"step_index"`        // 0-based step index
	StepTitle        string           `json:"step_title"`        // Step title
	StepPath         string           `json:"step_path"`         // Step path (e.g., "step-2")
	DecisionQuestion string           `json:"decision_question"` // The evaluation question
	DecisionResponse DecisionResponse `json:"decision_response"` // Structured decision response
	RunFolder        string           `json:"run_folder"`        // Run folder name (e.g., "iteration-1")
	WorkspacePath    string           `json:"workspace_path"`    // Workspace path
}

func (e *DecisionEvaluatedEvent) GetEventType() events.EventType {
	return DecisionEvaluated
}

// TempLLMSkippedEvent represents the event when temp LLM override is skipped due to learnings folder having files
type TempLLMSkippedEvent struct {
	events.BaseEventData
	StepID          string `json:"step_id"`
	StepIndex       int    `json:"step_index"`
	StepTitle       string `json:"step_title"`
	StepPath        string `json:"step_path"`
	IsBranchStep    bool   `json:"is_branch_step"`
	Reason          string `json:"reason"`
	TempLLMProvider string `json:"temp_llm_provider,omitempty"`
	TempLLMModel    string `json:"temp_llm_model,omitempty"`
	LearningsPath   string `json:"learnings_path,omitempty"`
	RunFolder       string `json:"run_folder"`
	WorkspacePath   string `json:"workspace_path"`
}

func (e *TempLLMSkippedEvent) GetEventType() events.EventType {
	return TempLLMSkipped
}

// =============================================================================
// BATCH EXECUTION EVENTS (for variable groups)
// =============================================================================

// BatchExecutionStartEvent represents the start of batch execution across multiple variable groups
type BatchExecutionStartEvent struct {
	events.BaseEventData
	TotalGroups      int                    `json:"total_groups"`      // Total number of enabled groups
	EnabledGroupIDs  []string               `json:"enabled_group_ids"` // List of group IDs to execute
	IterationNumber  int                    `json:"iteration_number"`  // Current iteration number
	WorkspacePath    string                 `json:"workspace_path"`
	ExecutionOptions map[string]interface{} `json:"execution_options,omitempty"` // Execution options (run_mode, execution_strategy, etc.)
}

func (e *BatchExecutionStartEvent) GetEventType() events.EventType {
	return BatchExecutionStart
}

// NewBatchExecutionStartEvent creates a new BatchExecutionStartEvent
func NewBatchExecutionStartEvent(totalGroups int, enabledGroupIDs []string, iterationNumber int, workspacePath string, executionOptions map[string]interface{}) *BatchExecutionStartEvent {
	return &BatchExecutionStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalGroups:      totalGroups,
		EnabledGroupIDs:  enabledGroupIDs,
		IterationNumber:  iterationNumber,
		WorkspacePath:    workspacePath,
		ExecutionOptions: executionOptions,
	}
}

// BatchGroupStartEvent represents the start of execution for a specific variable group
type BatchGroupStartEvent struct {
	events.BaseEventData
	GroupID         string            `json:"group_id"`         // Current group ID
	GroupIndex      int               `json:"group_index"`      // 0-based index in enabled groups
	TotalGroups     int               `json:"total_groups"`     // Total number of enabled groups
	VariableValues  map[string]string `json:"variable_values"`  // Values for this group
	RunFolder       string            `json:"run_folder"`       // e.g., "iteration-1-group-1"
	IterationNumber int               `json:"iteration_number"` // Current iteration number
	WorkspacePath   string            `json:"workspace_path"`
}

func (e *BatchGroupStartEvent) GetEventType() events.EventType {
	return BatchGroupStart
}

// NewBatchGroupStartEvent creates a new BatchGroupStartEvent
func NewBatchGroupStartEvent(groupID string, groupIndex, totalGroups int, variableValues map[string]string, runFolder string, iterationNumber int, workspacePath string) *BatchGroupStartEvent {
	return &BatchGroupStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		GroupID:         groupID,
		GroupIndex:      groupIndex,
		TotalGroups:     totalGroups,
		VariableValues:  variableValues,
		RunFolder:       runFolder,
		IterationNumber: iterationNumber,
		WorkspacePath:   workspacePath,
	}
}

// BatchGroupEndEvent represents the completion of execution for a specific variable group
type BatchGroupEndEvent struct {
	events.BaseEventData
	GroupID         string        `json:"group_id"`         // Current group ID
	GroupIndex      int           `json:"group_index"`      // 0-based index in enabled groups
	TotalGroups     int           `json:"total_groups"`     // Total number of enabled groups
	Success         bool          `json:"success"`          // Whether this group completed successfully
	Error           string        `json:"error,omitempty"`  // Error message if failed
	Duration        time.Duration `json:"duration"`         // How long this group took
	CompletedSteps  int           `json:"completed_steps"`  // Number of steps completed
	TotalSteps      int           `json:"total_steps"`      // Total number of steps
	RunFolder       string        `json:"run_folder"`       // e.g., "iteration-1-group-1"
	RemainingGroups int           `json:"remaining_groups"` // How many groups are left
}

func (e *BatchGroupEndEvent) GetEventType() events.EventType {
	return BatchGroupEnd
}

// NewBatchGroupEndEvent creates a new BatchGroupEndEvent
func NewBatchGroupEndEvent(groupID string, groupIndex, totalGroups int, success bool, errorMsg string, duration time.Duration, completedSteps, totalSteps int, runFolder string, remainingGroups int) *BatchGroupEndEvent {
	return &BatchGroupEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		GroupID:         groupID,
		GroupIndex:      groupIndex,
		TotalGroups:     totalGroups,
		Success:         success,
		Error:           errorMsg,
		Duration:        duration,
		CompletedSteps:  completedSteps,
		TotalSteps:      totalSteps,
		RunFolder:       runFolder,
		RemainingGroups: remainingGroups,
	}
}

// BatchExecutionEndEvent represents the completion of all batch execution
type BatchExecutionEndEvent struct {
	events.BaseEventData
	TotalGroups       int           `json:"total_groups"`        // Total number of enabled groups
	CompletedGroups   int           `json:"completed_groups"`    // Number of groups that completed
	FailedGroups      int           `json:"failed_groups"`       // Number of groups that failed
	CanceledGroups    int           `json:"canceled_groups"`     // Number of groups that were canceled
	Duration          time.Duration `json:"duration"`            // Total batch execution time
	Success           bool          `json:"success"`             // Whether all groups succeeded
	Error             string        `json:"error,omitempty"`     // Error message if batch failed
	IterationNumber   int           `json:"iteration_number"`    // Current iteration number
	CompletedGroupIDs []string      `json:"completed_group_ids"` // IDs of completed groups
	FailedGroupIDs    []string      `json:"failed_group_ids"`    // IDs of failed groups
}

func (e *BatchExecutionEndEvent) GetEventType() events.EventType {
	return BatchExecutionEnd
}

// NewBatchExecutionEndEvent creates a new BatchExecutionEndEvent
func NewBatchExecutionEndEvent(totalGroups, completedGroups, failedGroups, canceledGroups int, duration time.Duration, success bool, errorMsg string, iterationNumber int, completedGroupIDs, failedGroupIDs []string) *BatchExecutionEndEvent {
	return &BatchExecutionEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalGroups:       totalGroups,
		CompletedGroups:   completedGroups,
		FailedGroups:      failedGroups,
		CanceledGroups:    canceledGroups,
		Duration:          duration,
		Success:           success,
		Error:             errorMsg,
		IterationNumber:   iterationNumber,
		CompletedGroupIDs: completedGroupIDs,
		FailedGroupIDs:    failedGroupIDs,
	}
}

// BatchExecutionCanceledEvent represents when batch execution is canceled by user
type BatchExecutionCanceledEvent struct {
	events.BaseEventData
	TotalGroups       int      `json:"total_groups"`        // Total number of enabled groups
	CompletedGroups   int      `json:"completed_groups"`    // Number of groups that completed before cancel
	CanceledGroupID   string   `json:"canceled_group_id"`   // ID of group that was running when canceled
	RemainingGroupIDs []string `json:"remaining_group_ids"` // IDs of groups that were not executed
	Reason            string   `json:"reason"`              // Reason for cancellation
}

func (e *BatchExecutionCanceledEvent) GetEventType() events.EventType {
	return BatchExecutionCanceled
}

// NewBatchExecutionCanceledEvent creates a new BatchExecutionCanceledEvent
func NewBatchExecutionCanceledEvent(totalGroups, completedGroups int, canceledGroupID string, remainingGroupIDs []string, reason string) *BatchExecutionCanceledEvent {
	return &BatchExecutionCanceledEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalGroups:       totalGroups,
		CompletedGroups:   completedGroups,
		CanceledGroupID:   canceledGroupID,
		RemainingGroupIDs: remainingGroupIDs,
		Reason:            reason,
	}
}

