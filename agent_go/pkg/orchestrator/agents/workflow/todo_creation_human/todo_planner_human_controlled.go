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
	}, nil
}

// CreateTodoList orchestrates the human-controlled todo planning process
// - Single execution (no iterations)
// - Skips validation phase
// - Skips critique phase
// - Skips cleanup phase
// - Simple direct planning approach
func (hcpo *HumanControlledTodoPlannerOrchestrator) CreateTodoList(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Infof("🚀 Starting human-controlled todo planning for objective: %s", objective)

	// Set objective and workspace path directly
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// Human-controlled mode: Single execution with simple approach
	hcpo.GetLogger().Infof("🔄 Single execution: Creating plan to execute objective")

	// Phase 1: Create plan
	err := hcpo.runPlanningPhase(ctx, 1, 1)
	if err != nil {
		return "", fmt.Errorf("planning phase failed: %w", err)
	}

	// Phase 1.5: Extract independent steps from plan
	breakdownSteps, independentStepsResult, err := hcpo.runIndependentStepsExtractionPhase(ctx)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Independent steps extraction failed: %v", err)
		// Continue without independent steps if extraction fails
		independentStepsResult = "Independent steps extraction failed: " + err.Error()
		breakdownSteps = []TodoStep{} // Empty steps array
	}

	// Phase 2: Execute plan steps one by one (with validation after each step)
	err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1)
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanningPhase(ctx context.Context, iteration int, maxIterations int) error {
	planningTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Create fresh planning agent with proper context
	planningAgent, err := hcpo.createPlanningAgent(ctx, "planning", 0, iteration)
	if err != nil {
		return fmt.Errorf("failed to create planning agent: %w", err)
	}

	_, err = planningAgent.Execute(ctx, planningTemplateVars, nil)
	if err != nil {
		return fmt.Errorf("planning failed: %w", err)
	}
	return nil
}

// runExecutionPhase executes the plan steps one by one
func (hcpo *HumanControlledTodoPlannerOrchestrator) runExecutionPhase(ctx context.Context, breakdownSteps []TodoStep, iteration int) error {
	hcpo.GetLogger().Infof("🔄 Starting step-by-step execution of %d steps", len(breakdownSteps))

	// Execute each step one by one
	for i, step := range breakdownSteps {
		hcpo.GetLogger().Infof("📋 Executing step %d/%d: %s", i+1, len(breakdownSteps), step.Title)

		// Create execution agent for this step
		executionAgent, err := hcpo.createExecutionAgent(ctx, "execution", i+1, iteration)
		if err != nil {
			return fmt.Errorf("failed to create execution agent for step %d: %w", i+1, err)
		}

		// Prepare template variables for this specific step
		templateVars := map[string]string{
			"StepNumber":      fmt.Sprintf("%d", i+1),
			"TotalSteps":      fmt.Sprintf("%d", len(breakdownSteps)),
			"StepTitle":       step.Title,
			"StepDescription": step.Description,
			"WorkspacePath":   hcpo.GetWorkspacePath(),
		}

		// Execute this specific step
		executionResult, err := executionAgent.Execute(ctx, templateVars, nil)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Step %d execution failed: %v", i+1, err)
			continue
		}

		hcpo.GetLogger().Infof("✅ Step %d execution completed successfully", i+1)

		// Validate this step's execution
		hcpo.GetLogger().Infof("🔍 Validating step %d execution", i+1)

		validationAgent, err := hcpo.createValidationAgent(ctx, "validation", i+1, iteration)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to create validation agent for step %d: %v", i+1, err)
			continue
		}

		// Prepare validation template variables
		validationTemplateVars := map[string]string{
			"StepNumber":      fmt.Sprintf("%d", i+1),
			"TotalSteps":      fmt.Sprintf("%d", len(breakdownSteps)),
			"StepTitle":       step.Title,
			"StepDescription": step.Description,
			"WorkspacePath":   hcpo.GetWorkspacePath(),
			"ExecutionOutput": executionResult,
		}

		// Validate this step's execution
		_, err = validationAgent.Execute(ctx, validationTemplateVars, nil)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Step %d validation failed: %v", i+1, err)
			continue
		}

		hcpo.GetLogger().Infof("✅ Step %d validation completed successfully", i+1)
	}

	hcpo.GetLogger().Infof("✅ All steps execution completed")
	return nil
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

	_, err = writerAgent.Execute(ctx, writerTemplateVars, nil)
	if err != nil {
		return fmt.Errorf("todo list creation failed: %w", err)
	}

	return nil
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) runIndependentStepsExtractionPhase(ctx context.Context) ([]TodoStep, string, error) {
	hcpo.GetLogger().Infof("🔍 Extracting independent steps from plan")

	// Create plan breakdown agent
	breakdownAgent, err := hcpo.createPlanBreakdownAgent(ctx, "independent_extraction", 0, 1)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create plan breakdown agent: %w", err)
	}

	// Prepare template variables for breakdown agent
	breakdownTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Execute breakdown agent to extract independent steps using structured output
	breakdownAgentTyped, ok := breakdownAgent.(*HumanControlledPlanBreakdownAgent)
	if !ok {
		return nil, "", fmt.Errorf("failed to cast breakdown agent to correct type")
	}

	breakdownResponse, err := breakdownAgentTyped.ExecuteStructured(ctx, breakdownTemplateVars, []llms.MessageContent{})
	if err != nil {
		return nil, "", fmt.Errorf("plan breakdown failed: %w", err)
	}

	// Emit todo steps extracted event
	hcpo.emitTodoStepsExtractedEvent(ctx, breakdownResponse.Steps)

	// Convert structured response to JSON format for response
	jsonData, err := json.MarshalIndent(breakdownResponse, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal breakdown response to JSON: %w", err)
	}

	// Create a readable summary for logging
	var result strings.Builder
	result.WriteString(fmt.Sprintf("## Todo Steps Breakdown (JSON Format)\n\n**Objective**: %s\n\n", hcpo.GetObjective()))
	result.WriteString(fmt.Sprintf("**Total Steps**: %d\n\n", len(breakdownResponse.Steps)))
	result.WriteString("**JSON Response**:\n```json\n")
	result.WriteString(string(jsonData))
	result.WriteString("\n```\n")

	hcpo.GetLogger().Infof("✅ Todo steps extracted successfully: %d steps", len(breakdownResponse.Steps))
	return breakdownResponse.Steps, result.String(), nil
}

// createPlanBreakdownAgent creates a plan breakdown agent for independent steps extraction
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanBreakdownAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		"plan-breakdown-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledPlanBreakdownAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

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
