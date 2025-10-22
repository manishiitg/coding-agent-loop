package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// TodoStepsExtractedEvent represents the event when todo steps are extracted from a plan
type TodoStepsExtractedEvent struct {
	events.BaseEventData
	TotalStepsExtracted int        `json:"total_steps_extracted"`
	ExtractedSteps      []TodoStep `json:"extracted_steps"`
	ExtractionMethod    string     `json:"extraction_method"`
}

// GetEventType returns the event type for TodoStepsExtractedEvent
func (e *TodoStepsExtractedEvent) GetEventType() events.EventType {
	return events.TodoStepsExtracted
}

// HumanControlledTodoPlannerOrchestrator manages simplified human-controlled todo planning process
// - Single execution (no iterations)
// - No validation phase
// - No critique phase
// - No cleanup phase
// - Simple direct planning approach
// - Always includes independent steps extraction for parallel execution
type HumanControlledTodoPlannerOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
	// NEW: Store planning conversation for iterative refinement
	sessionID  string // For human feedback tracking
	workflowID string // For human feedback tracking
}

// NewHumanControlledTodoPlannerOrchestrator creates a new human-controlled todo planner orchestrator
func NewHumanControlledTodoPlannerOrchestrator(
	provider string,
	model string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	mcpConfigPath string,
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
) (*HumanControlledTodoPlannerOrchestrator, error) {

	// Create base workflow orchestrator
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		logger,
		eventBridge,
		orchestrator.OrchestratorTypeWorkflow,
		provider,
		model,
		mcpConfigPath,
		temperature,
		agentMode,
		selectedServers,
		llmConfig,
		maxTurns,
		customTools,
		customToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	return &HumanControlledTodoPlannerOrchestrator{
		BaseOrchestrator: baseOrchestrator,
		sessionID:        fmt.Sprintf("session_%d", time.Now().UnixNano()),
		workflowID:       fmt.Sprintf("workflow_%d", time.Now().UnixNano()),
	}, nil
}

// CreateTodoList orchestrates the human-controlled todo planning process
// - Single execution (no iterations)
// - Skips validation phase
// - Skips critique phase
// - Skips cleanup phase
// - Simple direct planning approach
// - NEW: Includes human approval loop with iterative plan refinement
func (hcpo *HumanControlledTodoPlannerOrchestrator) CreateTodoList(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Infof("🚀 Starting human-controlled todo planning for objective: %s", objective)

	// Set objective and workspace path directly
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// Human-controlled mode: Single execution with simple approach
	hcpo.GetLogger().Infof("🔄 Single execution: Creating plan to execute objective")

	maxPlanRevisions := 20 // Allow up to 20 plan revisions
	var breakdownSteps []TodoStep
	var independentStepsResult string
	var err error

	// Initialize separate conversation histories for different agents
	// planningConversationHistory: Tracks conversation with planning agent (plan creation/revision)
	// breakdownConversationHistory: Tracks conversation with plan breakdown agent (step extraction)
	planningConversationHistory := []llms.MessageContent{}
	breakdownConversationHistory := []llms.MessageContent{}

	// Retry loop for plan approval and revision
	for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
		hcpo.GetLogger().Infof("🔄 Plan revision attempt %d/%d", revisionAttempt, maxPlanRevisions)

		// Phase 1: Create plan (with optional human feedback)
		_, planningConversationHistory, err = hcpo.runPlanningPhase(ctx, revisionAttempt, planningConversationHistory)
		if err != nil {
			return "", fmt.Errorf("planning phase failed: %w", err)
		}
		// Phase 1.5: Extract independent steps from plan (with separate conversation history)
		breakdownSteps, independentStepsResult, breakdownConversationHistory, err = hcpo.runIndependentStepsExtractionPhase(ctx, breakdownConversationHistory)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Independent steps extraction failed: %v", err)
			// Continue without independent steps if extraction fails
			independentStepsResult = "Independent steps extraction failed: " + err.Error()
			breakdownSteps = []TodoStep{} // Empty steps array
		}

		// Phase 1.75: Request human approval for plan
		approved, feedback, err := hcpo.requestPlanApproval(ctx, revisionAttempt)
		if err != nil {
			return "", fmt.Errorf("plan approval request failed: %w", err)
		}

		if approved {
			hcpo.GetLogger().Infof("✅ Plan approved by human, proceeding to execution")
			break // Exit retry loop and continue to execution
		}

		// Plan rejected with feedback for revision - add to both histories
		hcpo.GetLogger().Infof("🔄 Plan revision requested (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedback)
		hcpo.addUserFeedbackToHistory(feedback, &planningConversationHistory)
		hcpo.addUserFeedbackToHistory(feedback, &breakdownConversationHistory)

		if revisionAttempt >= maxPlanRevisions {
			return "", fmt.Errorf("max plan revision attempts (%d) reached", maxPlanRevisions)
		}
	}

	// Phase 2: Execute plan steps one by one (with validation after each step)
	_, err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1)
	if err != nil {
		return "", fmt.Errorf("execution phase failed: %w", err)
	}

	// Phase 3: Write/Update todo list
	err = hcpo.runWriterPhase(ctx, 1)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Writer phase failed: %v", err)
	}

	// Phase 5: Skip critique phase (human-controlled mode)
	// No critique step in human-controlled mode

	duration := time.Since(hcpo.GetStartTime())
	hcpo.GetLogger().Infof("✅ Human-controlled todo planning completed in %v", duration)

	return fmt.Sprintf(`# Todo Planning Complete

## Planning Summary
- **Objective**: %s
- **Duration**: %v
- **Workspace**: %s
- **Phases**: Direct Planning → JSON Step Extraction → Step-by-Step Execution with Validation → Writing

## Independent Steps Extracted (JSON Format)
%s

## Final Todo List
Todo list has been created and saved as `+"`todo_final.md`"+` in the workspace root by the writer agent.

## Validation Reports
Step-by-step validation reports have been created and saved as `+"`validation_report.md`"+` in the validation folder for each executed step.

## Next Steps
The todo list has been created and is ready for the execution phase. The independent steps are available in structured JSON format for programmatic access. Each step was validated after execution to ensure proper completion. All agents read from workspace files independently.`,
		hcpo.GetObjective(), duration, hcpo.GetWorkspacePath(),
		independentStepsResult), nil
}

// runPlanningPhase creates or refines the step-wise plan
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanningPhase(ctx context.Context, iteration int, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	planningTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Create fresh planning agent with proper context
	planningAgent, err := hcpo.createPlanningAgent(ctx, "planning", 0, iteration)
	if err != nil {
		return "", conversationHistory, fmt.Errorf("failed to create planning agent: %w", err)
	}

	// Pass conversation history to planning agent
	result, conversationHistory, err := planningAgent.Execute(ctx, planningTemplateVars, conversationHistory)
	if err != nil {
		return "", conversationHistory, fmt.Errorf("planning failed: %w", err)
	}

	return result, conversationHistory, nil
}

// runExecutionPhase executes the plan steps one by one
func (hcpo *HumanControlledTodoPlannerOrchestrator) runExecutionPhase(ctx context.Context, breakdownSteps []TodoStep, iteration int) ([]llms.MessageContent, error) {
	hcpo.GetLogger().Infof("🔄 Starting step-by-step execution of %d steps", len(breakdownSteps))

	// Execute each step one by one
	for i, step := range breakdownSteps {
		hcpo.GetLogger().Infof("📋 Executing step %d/%d: %s", i+1, len(breakdownSteps), step.Title)

		// Create execution agent for this step
		executionAgent, err := hcpo.createExecutionAgent(ctx, "execution", i+1, iteration)
		if err != nil {
			return nil, fmt.Errorf("failed to create execution agent for step %d: %w", i+1, err)
		}

		// Create conversation history for execution agent
		executionConversationHistory := []llms.MessageContent{}

		// Prepare template variables for this specific step with individual fields
		templateVars := map[string]string{
			"StepNumber":          fmt.Sprintf("%d", i+1),
			"TotalSteps":          fmt.Sprintf("%d", len(breakdownSteps)),
			"StepTitle":           step.Title,
			"StepDescription":     step.Description,
			"StepSuccessCriteria": step.SuccessCriteria,
			"StepWhyThisStep":     step.WhyThisStep,
			"StepContextOutput":   step.ContextOutput,
			"WorkspacePath":       hcpo.GetWorkspacePath(),
		}

		// Add context dependencies as a comma-separated string
		if len(step.ContextDependencies) > 0 {
			templateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
		} else {
			templateVars["StepContextDependencies"] = ""
		}

		// Execute this specific step with execution conversation history
		_, executionConversationHistory, err = executionAgent.Execute(ctx, templateVars, executionConversationHistory)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Step %d execution failed: %v", i+1, err)
			continue
		}

		// Create conversation history for validation agent
		validationConversationHistory := []llms.MessageContent{}

		hcpo.GetLogger().Infof("✅ Step %d execution completed successfully", i+1)

		// Validate this step's execution
		hcpo.GetLogger().Infof("🔍 Validating step %d execution", i+1)

		validationAgent, err := hcpo.createValidationAgent(ctx, "validation", i+1, iteration)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to create validation agent for step %d: %v", i+1, err)
			continue
		}

		// Prepare validation template variables with individual fields
		validationTemplateVars := map[string]string{
			"StepNumber":          fmt.Sprintf("%d", i+1),
			"TotalSteps":          fmt.Sprintf("%d", len(breakdownSteps)),
			"StepTitle":           step.Title,
			"StepDescription":     step.Description,
			"StepSuccessCriteria": step.SuccessCriteria,
			"StepWhyThisStep":     step.WhyThisStep,
			"StepContextOutput":   step.ContextOutput,
			"WorkspacePath":       hcpo.GetWorkspacePath(),
			"ExecutionHistory":    hcpo.formatConversationHistory(executionConversationHistory),
		}

		// Add context dependencies as a comma-separated string
		if len(step.ContextDependencies) > 0 {
			validationTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
		} else {
			validationTemplateVars["StepContextDependencies"] = ""
		}

		// Validate this step's execution
		validationResult, _, err := validationAgent.Execute(ctx, validationTemplateVars, validationConversationHistory)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Step %d validation failed: %v", i+1, err)
			continue
		}

		hcpo.GetLogger().Infof("✅ Step %d validation completed successfully", i+1)

		// BLOCKING HUMAN FEEDBACK - Ask user if they want to continue to next step
		approved, feedback, err := hcpo.requestHumanFeedback(ctx, i+1, len(breakdownSteps), validationResult)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %v", err)
			// Default to continue if feedback fails
			approved = true
		}

		if !approved {
			hcpo.GetLogger().Infof("🛑 User requested to stop execution after step %d with feedback: %s", i+1, feedback)
			break
		}
	}

	hcpo.GetLogger().Infof("✅ All steps execution completed")
	return nil, nil
}

// runWriterPhase creates optimal todo list based on plan and execution experience
func (hcpo *HumanControlledTodoPlannerOrchestrator) runWriterPhase(ctx context.Context, iteration int) error {
	writerAgent, err := hcpo.createWriterAgent(ctx, "writing", 0, iteration)
	if err != nil {
		return fmt.Errorf("failed to create writer agent: %w", err)
	}

	// Prepare template variables for Execute method
	writerTemplateVars := map[string]string{
		"Objective":       hcpo.GetObjective(),
		"WorkspacePath":   hcpo.GetWorkspacePath(),
		"TotalIterations": fmt.Sprintf("%d", iteration),
	}

	_, _, err = writerAgent.Execute(ctx, writerTemplateVars, nil)
	if err != nil {
		return fmt.Errorf("todo list creation failed: %w", err)
	}

	return nil
}

// requestHumanFeedback requests human feedback after validation and blocks until user responds
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestHumanFeedback(ctx context.Context, currentStep, totalSteps int, validationResult string) (bool, string, error) {
	hcpo.GetLogger().Infof("🤔 Requesting human feedback for step %d/%d", currentStep, totalSteps)

	// Generate unique request ID
	requestID := fmt.Sprintf("step_feedback_%d_%d_%d", currentStep, totalSteps, time.Now().UnixNano())

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Step %d/%d validation completed. Should we continue with execution of the next step?", currentStep, totalSteps),
		validationResult, // Show validation results as context
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

// Agent creation methods - reuse from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"human-controlled-planning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerPlanningAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"execution-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerExecutionAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createValidationAgent creates a validation agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"validation-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerValidationAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) createWriterAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"writer-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerWriterAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// runIndependentStepsExtractionPhase extracts independent steps from the plan using plan breakdown agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) runIndependentStepsExtractionPhase(ctx context.Context, conversationHistory []llms.MessageContent) ([]TodoStep, string, []llms.MessageContent, error) {
	hcpo.GetLogger().Infof("🔍 Extracting independent steps from plan (with separate conversation history)")

	// Create plan breakdown agent
	breakdownAgent, err := hcpo.createPlanBreakdownAgent(ctx, "independent_extraction", 0, 1)
	if err != nil {
		return nil, "", conversationHistory, fmt.Errorf("failed to create plan breakdown agent: %w", err)
	}

	// Prepare template variables for breakdown agent
	breakdownTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Execute breakdown agent to extract independent steps using structured output
	breakdownAgentTyped, ok := breakdownAgent.(*HumanControlledPlanBreakdownAgent)
	if !ok {
		return nil, "", conversationHistory, fmt.Errorf("failed to cast breakdown agent to correct type")
	}

	// Pass conversation history to plan breakdown agent
	breakdownResponse, err := breakdownAgentTyped.ExecuteStructured(ctx, breakdownTemplateVars, conversationHistory)
	if err != nil {
		return nil, "", conversationHistory, fmt.Errorf("plan breakdown failed: %w", err)
	}

	// Add breakdown agent response to its conversation history
	// Note: This modifies the conversationHistory slice in place
	assistantMessage := llms.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{llms.TextContent{Text: fmt.Sprintf("Extracted %d steps: %v", len(breakdownResponse.Steps), breakdownResponse.Steps)}},
	}
	conversationHistory = append(conversationHistory, assistantMessage)

	// Emit todo steps extracted event
	hcpo.emitTodoStepsExtractedEvent(ctx, breakdownResponse.Steps)

	// Convert structured response to JSON format for response
	jsonData, err := json.MarshalIndent(breakdownResponse, "", "  ")
	if err != nil {
		return nil, "", conversationHistory, fmt.Errorf("failed to marshal breakdown response to JSON: %w", err)
	}

	// Create a readable summary for logging
	var result strings.Builder
	result.WriteString(fmt.Sprintf("## Todo Steps Breakdown (JSON Format)\n\n**Objective**: %s\n\n", hcpo.GetObjective()))
	result.WriteString(fmt.Sprintf("**Total Steps**: %d\n\n", len(breakdownResponse.Steps)))
	result.WriteString("**JSON Response**:\n```json\n")
	result.WriteString(string(jsonData))
	result.WriteString("\n```\n")

	hcpo.GetLogger().Infof("✅ Todo steps extracted successfully: %d steps", len(breakdownResponse.Steps))
	return breakdownResponse.Steps, result.String(), conversationHistory, nil
}

// createPlanBreakdownAgent creates a plan breakdown agent for independent steps extraction
// Uses NO_SERVERS constant for pure LLM reasoning without tool distractions
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanBreakdownAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use NO_SERVERS constant for pure LLM reasoning
	noServers := []string{mcpclient.NoServers}

	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"plan-breakdown-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		noServers, // NO_SERVERS constant for pure LLM reasoning
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledPlanBreakdownAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	hcpo.GetLogger().Infof("✅ Plan breakdown agent created with NO_SERVERS for pure LLM reasoning")
	return agent, nil
}

// emitTodoStepsExtractedEvent emits an event when todo steps are extracted from a plan
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitTodoStepsExtractedEvent(ctx context.Context, extractedSteps []TodoStep) {
	if hcpo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &TodoStepsExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalStepsExtracted: len(extractedSteps),
		ExtractedSteps:      extractedSteps,
		ExtractionMethod:    "structured_breakdown_agent",
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.TodoStepsExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := hcpo.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to emit todo steps extracted event: %v", err)
	} else {
		hcpo.GetLogger().Infof("✅ Emitted todo steps extracted event: %d steps extracted", len(extractedSteps))
	}
}

// Execute implements the Orchestrator interface
func (hcpo *HumanControlledTodoPlannerOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate that no options are provided since this orchestrator doesn't use them
	if len(options) > 0 {
		return "", fmt.Errorf("human-controlled todo planner orchestrator does not accept options")
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Call the existing CreateTodoList method
	return hcpo.CreateTodoList(ctx, objective, workspacePath)
}

// GetType returns the orchestrator type
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetType() string {
	return "human_controlled_todo_planner"
}

// Helper methods for human feedback tracking

// getSessionID returns the session ID for this orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) getSessionID() string {
	return hcpo.sessionID
}

// getWorkflowID returns the workflow ID for this orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) getWorkflowID() string {
	return hcpo.workflowID
}

// addUserFeedbackToHistory adds human feedback to conversation history
func (hcpo *HumanControlledTodoPlannerOrchestrator) addUserFeedbackToHistory(feedback string, conversationHistory *[]llms.MessageContent) {
	feedbackMessage := llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: feedback}},
	}
	*conversationHistory = append(*conversationHistory, feedbackMessage)
}

// formatConversationHistory formats conversation history for template usage
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatConversationHistory(conversationHistory []llms.MessageContent) string {
	var result strings.Builder

	for _, message := range conversationHistory {
		// Skip system messages
		if message.Role == llms.ChatMessageTypeSystem {
			continue
		}

		switch message.Role {
		case llms.ChatMessageTypeHuman:
			result.WriteString("## Human Message\n")
		case llms.ChatMessageTypeAI:
			result.WriteString("## Assistant Response\n")
		default:
			result.WriteString("## Message\n")
		}

		for _, part := range message.Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				result.WriteString(textPart.Text)
				result.WriteString("\n\n")
			}
		}
		result.WriteString("---\n\n")
	}

	return result.String()
}

// requestPlanApproval requests human approval for the generated plan
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestPlanApproval(
	ctx context.Context,
	revisionAttempt int,
) (bool, string, error) {
	hcpo.GetLogger().Infof("⏸️ Requesting human approval for plan (attempt %d)", revisionAttempt)

	// Generate unique request ID
	requestID := fmt.Sprintf("plan_approval_%d_%d", time.Now().UnixNano(), revisionAttempt)

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		"Please review the plan and provide approval or feedback",
		"", // No additional context for plan approval
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}
