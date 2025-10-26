package types

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/llm"

	"github.com/tmc/langchaingo/llms"
)

// ExecutionMode represents the execution mode for orchestrator operations
type ExecutionMode string

const (
	// SequentialExecution runs tasks one after another
	SequentialExecution ExecutionMode = "sequential_execution"

	// ParallelExecution runs tasks concurrently
	ParallelExecution ExecutionMode = "parallel_execution"
)

// String returns the string representation of the execution mode
func (em ExecutionMode) String() string {
	return string(em)
}

// IsValid checks if the execution mode is valid
func (em ExecutionMode) IsValid() bool {
	switch em {
	case SequentialExecution, ParallelExecution:
		return true
	default:
		return false
	}
}

// GetLabel returns a human-readable label for the execution mode
func (em ExecutionMode) GetLabel() string {
	switch em {
	case SequentialExecution:
		return "Sequential Execution"
	case ParallelExecution:
		return "Parallel Execution"
	default:
		return "Parallel Execution" // Default fallback
	}
}

// ParseExecutionMode parses a string into an ExecutionMode, returning ParallelExecution as default
func ParseExecutionMode(mode string) ExecutionMode {
	switch mode {
	case string(SequentialExecution):
		return SequentialExecution
	case string(ParallelExecution):
		return ParallelExecution
	default:
		return ParallelExecution // Default fallback
	}
}

// AllExecutionModes returns all available execution modes
func AllExecutionModes() []ExecutionMode {
	return []ExecutionMode{
		SequentialExecution,
		ParallelExecution,
	}
}

// PlannerSelectedOption represents a selected option for planner execution
type PlannerSelectedOption struct {
	OptionID    string `json:"option_id"`
	OptionLabel string `json:"option_label"`
	OptionValue string `json:"option_value"`
	Group       string `json:"group"`
}

// PlannerSelectedOptions represents selected options for planner execution
type PlannerSelectedOptions struct {
	Selections []PlannerSelectedOption `json:"selections"`
}

// ParallelStep represents a step that can be executed in parallel
type ParallelStep struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Dependencies  []string `json:"dependencies"`
	IsIndependent bool     `json:"is_independent"`
}

// ParallelResult represents the result of a parallel step execution
type ParallelResult struct {
	StepID           string `json:"step_id"`
	ExecutionResult  string `json:"execution_result"`
	ValidationResult string `json:"validation_result"`
	Success          bool   `json:"success"`
	Error            string `json:"error,omitempty"`
}

// IndependentStepsSelectedEvent represents the event when independent steps are selected for parallel execution
type IndependentStepsSelectedEvent struct {
	events.BaseEventData
	TotalStepsAvailable int            `json:"total_steps_available"`
	SelectedSteps       []ParallelStep `json:"selected_steps"`
	SelectionCriteria   string         `json:"selection_criteria"`
	Reasoning           string         `json:"reasoning"`
	StepsCount          int            `json:"steps_count"`
	ExecutionMode       string         `json:"execution_mode"`
	PlanID              string         `json:"plan_id"`
}

// GetEventType returns the event type for IndependentStepsSelectedEvent
func (e *IndependentStepsSelectedEvent) GetEventType() events.EventType {
	return events.IndependentStepsSelected
}

// PlannerOrchestrator handles the flow from planning agent to execution agent
type PlannerOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Execution mode configuration
	selectedOptions *PlannerSelectedOptions // Selected execution options

	// Conversation history for context
	conversationHistory []llms.MessageContent
}

// NewPlannerOrchestrator creates a new planner orchestrator with full configuration
func NewPlannerOrchestrator(
	provider string,
	model string,
	mcpConfigPath string,
	temperature float64,
	agentMode string,
	workspaceRoot string,
	logger utils.ExtendedLogger,
	eventBridge mcpagent.AgentEventListener,
	tracer observability.Tracer,
	selectedServers []string,
	selectedOptions *PlannerSelectedOptions,
	selectedTools []string, // NEW parameter
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
) (*PlannerOrchestrator, error) {

	// Create base orchestrator
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		logger,
		eventBridge,
		orchestrator.OrchestratorTypePlanner,
		provider,
		model,
		mcpConfigPath,
		temperature,
		agentMode,
		selectedServers,
		selectedTools, // NEW: Pass through
		llmConfig,
		maxTurns,
		customTools,
		customToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	// Create planner orchestrator instance
	po := &PlannerOrchestrator{
		BaseOrchestrator: baseOrchestrator,

		// Execution mode configuration
		selectedOptions: selectedOptions,
	}

	return po, nil
}

// executeSequential executes the original sequential flow
func (po *PlannerOrchestrator) executeSequential(ctx context.Context, objective string) (string, error) {

	// Helper function to emit orchestrator error event
	emitOrchestratorError := func(err error, context string) {
		duration := time.Since(po.GetStartTime())
		orchestratorErrorEvent := &events.OrchestratorErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Context:          context,
			Error:            err.Error(),
			Duration:         duration,
			OrchestratorType: po.GetType(),
			ExecutionMode:    po.GetExecutionMode().String(),
		}

		// Create unified event wrapper
		unifiedEvent := &events.AgentEvent{
			Type:      events.OrchestratorError,
			Timestamp: time.Now(),
			Data:      orchestratorErrorEvent,
		}

		// Emit through the bridge
		// Emit through the bridge
		bridge := po.GetContextAwareBridge()
		bridge.HandleEvent(ctx, unifiedEvent)
		po.GetLogger().Infof("✅ Emitted orchestrator error event: %s", context)
	}

	// Initialize variables for the iterative loop
	currentStepIndex := 0
	executionResults := make([]string, 0)
	validationResults := make([]string, 0)
	planningResults := make([]string, 0)
	organizationResults := make([]string, 0)
	reportResults := make([]string, 0)

	// Main iterative loop - simplified stateless execution
	maxIterations := 10 // Fixed max iterations for stateless execution
	for iteration := 0; iteration < maxIterations; iteration++ {

		// ✅ PLANNING PHASE - Determine next step or workflow completion

		// Build context based on iteration
		var executionResultsStr, validationResultsStr, reportResultsStr string

		if iteration > 0 {
			// Step 2 onwards: Include actual content from previous iterations
			if len(executionResults) > 0 {
				executionResultsStr = strings.Join(executionResults, "\n\n")
			} else {
				executionResultsStr = "No previous execution results"
			}

			if len(validationResults) > 0 {
				validationResultsStr = strings.Join(validationResults, "\n\n")
			} else {
				validationResultsStr = "No previous validation results"
			}

			if len(reportResults) > 0 {
				reportResultsStr = strings.Join(reportResults, "\n\n")
			} else {
				reportResultsStr = "No previous report results"
			}

		} else {
			// First step: No previous execution results
			executionResultsStr = ""
			validationResultsStr = ""
			reportResultsStr = ""
		}

		// Prepare planning template variables with current context
		planningTemplateVars := map[string]string{
			"Objective":         objective,
			"ExecutionResults":  executionResultsStr,
			"ValidationResults": validationResultsStr,
			"ReportResults":     reportResultsStr,
			"WorkspacePath":     po.GetWorkspacePath(),
		}

		// Get next step decision from planning agent

		// Create planning agent on-demand
		planningAgent, err := po.createPlanningAgent(ctx, currentStepIndex, iteration)
		if err != nil {
			return "", fmt.Errorf("failed to create planning agent: %w", err)
		}

		// Set orchestrator context for planning agent
		// Context is now handled automatically during agent creation

		// Use Execute method to get structured response from planning agent with guidance
		planningTemplateVars["Objective"] = objective
		planningResult, _, err := planningAgent.Execute(ctx, planningTemplateVars, po.conversationHistory)

		if err != nil {
			po.GetLogger().Errorf("❌ Planning failed: %v", err)
			emitOrchestratorError(err, "planning phase")
			return "", fmt.Errorf("planning failed: %w", err)
		}

		// Extract should_continue from the raw planning result using conditional LLM
		shouldContinue := po.extractShouldContinue(ctx, planningResult)

		// Store planning result for this iteration
		planningResults = append(planningResults, planningResult)

		// Check if we should continue - BREAK if planning says no
		if !shouldContinue {
			po.GetLogger().Infof("✅ Workflow completion confirmed by planning agent")
			break
		}

		// Execute the current step

		// Create execution agent on-demand
		executionAgent, err := po.createDedicatedExecutionAgent(ctx, currentStepIndex, iteration)
		if err != nil {
			po.GetLogger().Errorf("❌ Failed to create execution agent: %v", err)
			emitOrchestratorError(err, "execution phase")
			return "", fmt.Errorf("failed to create execution agent: %w", err)
		}

		// Context is now handled automatically during agent creation

		// Execute the current step using the raw planning response with guidance
		executionTemplateVars := map[string]string{
			"Objective":     planningResult, // Pass the planning result directly
			"WorkspacePath": po.GetWorkspacePath(),
		}

		executionResult, _, err := executionAgent.Execute(ctx, executionTemplateVars, po.conversationHistory)

		if err != nil {
			po.GetLogger().Errorf("❌ Execution failed for step %d: %v", currentStepIndex+1, err)
			emitOrchestratorError(err, fmt.Sprintf("execution phase - step %d", currentStepIndex+1))
			return "", fmt.Errorf("failed to execute step %d: %w", currentStepIndex+1, err)
		}

		executionResults = append(executionResults, executionResult)

		// ✅ VALIDATION PHASE - Validate this step's execution result immediately

		// Create validation agent on-demand
		validationAgent, err := po.createDedicatedValidationAgent(ctx, currentStepIndex)
		if err != nil {
			po.GetLogger().Errorf("❌ Failed to create validation agent: %v", err)
			emitOrchestratorError(err, "validation phase")
			return "", fmt.Errorf("failed to create validation agent: %w", err)
		}
		// Context is now handled automatically during agent creation

		// Prepare validation template variables with guidance
		validationTemplateVars := map[string]string{
			"Objective":        objective,
			"StepDescription":  planningResult, // Pass the original planning result directly
			"ExecutionResults": fmt.Sprintf("Step %d: %s", currentStepIndex+1, executionResult),
			"WorkspacePath":    po.GetWorkspacePath(),
		}

		stepValidationResult, _, err := validationAgent.Execute(ctx, validationTemplateVars, po.conversationHistory)

		if err != nil {
			po.GetLogger().Errorf("❌ Validation failed for step %d: %v", currentStepIndex+1, err)
			// Continue with execution result even if validation fails
			po.GetLogger().Warnf("⚠️ Continuing with execution result despite validation failure")
			// Set empty validation result when validation fails
			stepValidationResult = "Validation failed: " + err.Error()
		}

		// Store validation results for this step
		validationResults = append(validationResults, stepValidationResult)

		// ✅ ORGANIZATION PHASE - Organize this step's results immediately

		// Create organizer agent on-demand
		organizerAgent, err := po.createOrganizerAgent(ctx, currentStepIndex, iteration)
		if err != nil {
			po.GetLogger().Errorf("❌ Failed to create organizer agent: %v", err)
			emitOrchestratorError(err, "organization phase")
			return "", fmt.Errorf("failed to create organizer agent: %w", err)
		}

		// Execute plan organization for this step with guidance
		organizationTemplateVars := map[string]string{
			"WorkflowContext":  fmt.Sprintf("Step %d of workflow for objective: %s", currentStepIndex+1, objective),
			"PlanningOutput":   planningResult, // Pass the original planning result directly
			"ExecutionOutput":  executionResult,
			"ValidationOutput": stepValidationResult,
			"WorkspacePath":    po.GetWorkspacePath(),
		}

		// Set orchestrator context for organizer agent
		// Context is now handled automatically during agent creation

		stepOrganizationResult, _, err := organizerAgent.Execute(ctx, organizationTemplateVars, po.conversationHistory)

		if err != nil {
			po.GetLogger().Errorf("❌ Step %d organization failed: %v", currentStepIndex+1, err)
		} else {
			// Store the organized results for this step
			organizationResults = append(organizationResults, stepOrganizationResult)
		}

		// ✅ REPORT GENERATION PHASE - Generate report for this iteration

		// Create report agent on-demand
		reportAgent, err := po.createReportAgent(ctx, currentStepIndex, iteration)
		if err != nil {
			po.GetLogger().Errorf("❌ Failed to create report agent: %v", err)
			emitOrchestratorError(err, "report generation phase")
			return "", fmt.Errorf("failed to create report agent: %w", err)
		}

		// Execute report generation for this step with guidance
		reportTemplateVars := map[string]string{
			"Objective":           objective,
			"PlanningResults":     planningResult, // Current step planning result
			"ExecutionResults":    executionResult,
			"ValidationResults":   stepValidationResult,
			"OrganizationResults": stepOrganizationResult,
			"WorkspacePath":       po.GetWorkspacePath(),
		}

		// Set orchestrator context for report agent
		// Context is now handled automatically during agent creation

		reportResult, _, err := reportAgent.Execute(ctx, reportTemplateVars, po.conversationHistory)

		if err != nil {
			po.GetLogger().Errorf("❌ Step %d report generation failed: %v", currentStepIndex+1, err)
			// Continue even if report generation fails
			po.GetLogger().Warnf("⚠️ Continuing despite report generation failure")
		} else {
			// Store the report result for this step
			reportResults = append(reportResults, reportResult)
		}

		// Move to next step
		currentStepIndex++
	}

	// Prepare final result with iteration-by-iteration breakdown
	finalResult := fmt.Sprintf("Sequential orchestrator completed after %d iterations with %d steps executed.\n\n", len(planningResults), len(executionResults))

	// Add iteration-by-iteration results
	finalResult += "ITERATION RESULTS:\n"
	finalResult += "=================\n"

	if len(executionResults) > 0 {
		for i := 0; i < len(executionResults); i++ {
			finalResult += fmt.Sprintf("ITERATION %d:\n", i+1)
			finalResult += "-----------\n"

			// Planning
			finalResult += "📋 PLANNING:\n"
			if i < len(planningResults) && planningResults[i] != "" {
				finalResult += fmt.Sprintf("Raw Response: %s\n", planningResults[i])
			} else {
				finalResult += "No planning result available\n"
			}
			finalResult += "\n"

			// Execution
			finalResult += "🚀 EXECUTION:\n"
			if i < len(executionResults) && executionResults[i] != "" {
				finalResult += executionResults[i] + "\n"
			} else {
				finalResult += "No execution result available\n"
			}
			finalResult += "\n"

			// Validation
			finalResult += "🔍 VALIDATION:\n"
			if i < len(validationResults) && validationResults[i] != "" {
				finalResult += validationResults[i] + "\n"
			} else {
				finalResult += "No validation result available\n"
			}
			finalResult += "\n"

			// Organization
			finalResult += "📊 ORGANIZATION:\n"
			if i < len(organizationResults) && organizationResults[i] != "" {
				finalResult += organizationResults[i] + "\n"
			} else {
				finalResult += "No organization result available\n"
			}
			finalResult += "\n"

			finalResult += "---\n\n"
		}
	} else {
		finalResult += "No iterations completed\n\n"
	}

	// Add planning decision results if available
	if len(planningResults) > 0 && planningResults[len(planningResults)-1] != "" {
		lastPlanningResult := planningResults[len(planningResults)-1]
		finalResult += "FINAL PLANNING DECISION:\n"
		finalResult += "========================\n"
		finalResult += fmt.Sprintf("Raw Response: %s\n", lastPlanningResult)
	}

	po.GetLogger().Infof("🎉 Sequential Planner Orchestrator Flow completed successfully after %d iterations!", len(planningResults))

	// Emit orchestrator completion events
	executionMode := po.GetExecutionMode().String()
	po.EmitOrchestratorEnd(ctx, objective, finalResult, "completed", "", executionMode)
	po.EmitUnifiedCompletionEvent(ctx, "planner", "planner", objective, finalResult, "completed", len(planningResults))

	return finalResult, nil
}

// executeParallel executes the parallel flow with dependency analysis and goroutines
func (po *PlannerOrchestrator) executeParallel(ctx context.Context, objective string) (string, error) {

	// Helper function to emit orchestrator error event
	emitOrchestratorError := func(err error, context string) {
		duration := time.Since(po.GetStartTime())
		orchestratorErrorEvent := &events.OrchestratorErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Context:          context,
			Error:            err.Error(),
			Duration:         duration,
			OrchestratorType: po.GetType(),
			ExecutionMode:    po.GetExecutionMode().String(),
		}

		// Create unified event wrapper
		unifiedEvent := &events.AgentEvent{
			Type:      events.OrchestratorError,
			Timestamp: time.Now(),
			Data:      orchestratorErrorEvent,
		}

		// Emit through the bridge
		// Emit through the bridge
		bridge := po.GetContextAwareBridge()
		bridge.HandleEvent(ctx, unifiedEvent)
		po.GetLogger().Infof("✅ Emitted orchestrator error event: %s", context)
	}

	// Step 1: Get initial plan from planning agent
	planningResult, err := po.getInitialPlan(ctx, objective)
	if err != nil {
		emitOrchestratorError(err, "initial planning phase")
		return "", fmt.Errorf("failed to get initial plan: %w", err)
	}

	// Step 2: Use plan breakdown agent to analyze dependencies and get independent steps
	independentSteps, err := po.analyzeDependenciesWithStructuredOutput(ctx, planningResult)
	if err != nil {
		emitOrchestratorError(err, "dependency analysis phase")
		return "", fmt.Errorf("failed to analyze dependencies: %w", err)
	}

	// Step 3: Select up to 3 independent steps for parallel execution
	parallelSteps := po.selectParallelSteps(ctx, independentSteps)

	// Step 4: Execute steps in parallel with goroutines
	parallelResults, err := po.executeStepsInParallel(ctx, parallelSteps)
	if err != nil {
		emitOrchestratorError(err, "parallel execution phase")
		return "", fmt.Errorf("failed to execute steps in parallel: %w", err)
	}

	// Step 5: Organize results from parallel execution
	organizedResult, err := po.organizeParallelResults(ctx, parallelResults)
	if err != nil {
		emitOrchestratorError(err, "parallel organization phase")
		return "", fmt.Errorf("failed to organize parallel results: %w", err)
	}

	// Step 6: Generate final report using existing report agent
	finalReport, err := po.generateParallelReport(ctx, organizedResult, parallelResults)
	if err != nil {
		emitOrchestratorError(err, "parallel report generation")
		return "", fmt.Errorf("failed to generate parallel report: %w", err)
	}

	// Emit orchestrator completion events
	executionMode := po.GetExecutionMode().String()
	po.EmitOrchestratorEnd(ctx, objective, finalReport, "completed", "", executionMode)
	po.EmitUnifiedCompletionEvent(ctx, "planner", "planner", objective, finalReport, "completed", len(parallelResults))

	return finalReport, nil
}

// Helper methods for parallel execution

// getInitialPlan gets the initial plan from the planning agent
func (po *PlannerOrchestrator) getInitialPlan(ctx context.Context, objective string) (string, error) {
	po.GetLogger().Infof("📋 Getting initial plan from planning agent")

	// Set orchestrator context for planning agent
	planningAgent, err := po.createPlanningAgent(ctx, 0, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create planning agent: %w", err)
	}

	// Context is now handled automatically during agent creation

	// Prepare planning template variables
	planningTemplateVars := map[string]string{
		"Objective":     objective,
		"WorkspacePath": po.GetWorkspacePath(),
	}

	// Execute planning agent
	planningResult, _, err := planningAgent.Execute(ctx, planningTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("planning agent failed: %w", err)
	}

	po.GetLogger().Infof("✅ Initial plan generated successfully")
	return planningResult, nil
}

// analyzeDependenciesWithStructuredOutput analyzes dependencies using structured output
func (po *PlannerOrchestrator) analyzeDependenciesWithStructuredOutput(ctx context.Context, planningResult string) ([]ParallelStep, error) {
	po.GetLogger().Infof("🔍 Analyzing dependencies for parallel execution using structured output")

	// Create plan breakdown agent on-demand
	breakdownAgent, err := po.createPlanBreakdownAgent(ctx, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan breakdown agent: %w", err)
	}

	// Context is now handled automatically during agent creation

	// Prepare template variables for the breakdown agent
	templateVars := map[string]string{
		"PlanningResult": planningResult,
		"Objective":      po.GetObjective(),
		"WorkspacePath":  po.GetWorkspacePath(),
	}

	// Cast to PlanBreakdownAgent to access the ExecuteStructured method
	breakdownAgentTyped, ok := breakdownAgent.(*agents.PlanBreakdownAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast breakdown agent to PlanBreakdownAgent type")
	}

	// Use the agent's ExecuteStructured method directly
	breakdownResponse, err := breakdownAgentTyped.ExecuteStructured(ctx, templateVars, po.conversationHistory)
	if err != nil {
		return nil, fmt.Errorf("plan breakdown structured execution failed: %w", err)
	}

	// Convert structured response to ParallelStep format
	var parallelSteps []ParallelStep
	for _, step := range breakdownResponse.Steps {
		parallelSteps = append(parallelSteps, ParallelStep{
			ID:            step.ID,
			Description:   step.Description,
			Dependencies:  step.Dependencies,
			IsIndependent: step.IsIndependent,
		})
	}

	po.GetLogger().Infof("✅ Found %d independent steps for parallel execution", len(parallelSteps))
	return parallelSteps, nil
}

// selectParallelSteps selects up to 3 independent steps for parallel execution
func (po *PlannerOrchestrator) selectParallelSteps(ctx context.Context, independentSteps []ParallelStep) []ParallelStep {
	po.GetLogger().Infof("🎯 Selecting up to 3 independent steps from %d available steps", len(independentSteps))

	// Filter for truly independent steps (no dependencies)
	var trulyIndependent []ParallelStep
	for _, step := range independentSteps {
		if step.IsIndependent && len(step.Dependencies) == 0 {
			trulyIndependent = append(trulyIndependent, step)
		}
	}

	// If we have fewer than 3 truly independent steps, include some with minimal dependencies
	if len(trulyIndependent) < 3 {
		for _, step := range independentSteps {
			if step.IsIndependent && len(step.Dependencies) <= 1 && len(trulyIndependent) < 3 {
				// Check if this step is already included
				found := false
				for _, existing := range trulyIndependent {
					if existing.ID == step.ID {
						found = true
						break
					}
				}
				if !found {
					trulyIndependent = append(trulyIndependent, step)
				}
			}
		}
	}

	// Limit to 3 steps maximum
	if len(trulyIndependent) > 3 {
		trulyIndependent = trulyIndependent[:3]
	}

	// If still no steps, create a fallback
	if len(trulyIndependent) == 0 {
		po.GetLogger().Warnf("⚠️ No independent steps found, creating fallback step")
		trulyIndependent = []ParallelStep{
			{
				ID:            "fallback_step",
				Description:   "Fallback step from breakdown analysis",
				Dependencies:  []string{},
				IsIndependent: true,
			},
		}
	}

	// Emit independent steps selected event using context-aware bridge
	po.emitIndependentStepsSelectedEvent(ctx, independentSteps, trulyIndependent)

	po.GetLogger().Infof("✅ Selected %d steps for parallel execution", len(trulyIndependent))
	return trulyIndependent
}

// emitIndependentStepsSelectedEvent emits an event when independent steps are selected
func (po *PlannerOrchestrator) emitIndependentStepsSelectedEvent(ctx context.Context, availableSteps, selectedSteps []ParallelStep) {
	if po.GetContextAwareBridge() == nil {
		return
	}

	// Determine selection criteria and reasoning
	var criteria, reasoning string
	if len(selectedSteps) == len(availableSteps) {
		criteria = "all_available_steps"
		reasoning = "All available steps were selected for parallel execution"
	} else if len(selectedSteps) == 3 {
		criteria = "exactly_three_steps"
		reasoning = "Selected exactly 3 steps for optimal parallel execution"
	} else if len(selectedSteps) == 1 && selectedSteps[0].ID == "fallback_step" {
		criteria = "fallback_step"
		reasoning = "No independent steps found, created fallback step"
	} else {
		criteria = "filtered_selection"
		reasoning = fmt.Sprintf("Selected %d independent steps from %d available steps", len(selectedSteps), len(availableSteps))
	}

	// Create event data
	eventData := &IndependentStepsSelectedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalStepsAvailable: len(availableSteps),
		SelectedSteps:       selectedSteps,
		SelectionCriteria:   criteria,
		Reasoning:           reasoning,
		StepsCount:          len(selectedSteps),
		ExecutionMode:       po.GetExecutionMode().String(),
		PlanID:              fmt.Sprintf("plan_%d_%d", 0, time.Now().Unix()),
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.IndependentStepsSelected,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := po.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		po.GetLogger().Warnf("⚠️ Failed to emit independent steps selected event: %v", err)
	} else {
		po.GetLogger().Infof("✅ Emitted independent steps selected event: %d steps selected", len(selectedSteps))
	}
}

// executeStepsInParallel executes steps in parallel with goroutines
func (po *PlannerOrchestrator) executeStepsInParallel(ctx context.Context, steps []ParallelStep) ([]ParallelResult, error) {
	po.GetLogger().Infof("🚀 Executing %d steps in parallel", len(steps))

	results := make([]ParallelResult, len(steps))
	errors := make([]error, len(steps))

	var wg sync.WaitGroup

	// Execute each step in a separate goroutine
	for i, step := range steps {
		wg.Add(1)
		go func(index int, parallelStep ParallelStep) {
			defer wg.Done()

			po.GetLogger().Infof("🔄 Starting parallel execution of step %d: %s", index+1, parallelStep.Description)

			// Execute step
			executionResult, err := po.executeSingleStep(ctx, parallelStep, index, steps)
			if err != nil {
				errors[index] = err
				results[index] = ParallelResult{
					StepID:  parallelStep.ID,
					Success: false,
					Error:   err.Error(),
				}
				return
			}

			// Validate step
			validationResult, err := po.validateSingleStep(ctx, parallelStep, executionResult, index)
			if err != nil {
				po.GetLogger().Warnf("⚠️ Validation failed for step %d: %v", index+1, err)
				validationResult = "Validation failed: " + err.Error()
			}

			results[index] = ParallelResult{
				StepID:           parallelStep.ID,
				ExecutionResult:  executionResult,
				ValidationResult: validationResult,
				Success:          true,
			}

			po.GetLogger().Infof("✅ Completed parallel execution of step %d", index+1)
		}(i, step)
	}

	wg.Wait()

	// Check for errors and log them
	var failedSteps []string
	var aggregatedErrors []error
	for i, err := range errors {
		if err != nil {
			po.GetLogger().Errorf("❌ Step %d failed: %v", i+1, err)
			failedSteps = append(failedSteps, fmt.Sprintf("Step %d: %v", i+1, err))
			aggregatedErrors = append(aggregatedErrors, err)
		}
	}

	// Return aggregated error if any steps failed
	var returnError error
	if len(aggregatedErrors) > 0 {
		if len(aggregatedErrors) == 1 {
			// Single failure - return the original error
			returnError = aggregatedErrors[0]
		} else {
			// Multiple failures - create aggregated error
			returnError = fmt.Errorf("parallel execution failed: %d steps failed - %s",
				len(aggregatedErrors), strings.Join(failedSteps, "; "))
		}
	}

	po.GetLogger().Infof("✅ All parallel executions completed")
	return results, returnError
}

// executeSingleStep executes a single step
func (po *PlannerOrchestrator) executeSingleStep(ctx context.Context, step ParallelStep, stepIndex int, allSteps []ParallelStep) (string, error) {
	// Create dedicated execution agent for this step
	executionAgent, err := po.createDedicatedExecutionAgent(ctx, stepIndex, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create execution agent: %w", err)
	}

	// Generate list of other objectives running in parallel
	var otherObjectives []string
	for i, otherStep := range allSteps {
		if i != stepIndex {
			otherObjectives = append(otherObjectives, fmt.Sprintf("Step %d: %s", i+1, otherStep.Description))
		}
	}
	otherObjectivesStr := strings.Join(otherObjectives, "; ")

	// Prepare execution template variables
	executionTemplateVars := map[string]string{
		"Objective":       step.Description,
		"StepID":          step.ID,
		"WorkspacePath":   po.GetWorkspacePath(),
		"OtherObjectives": otherObjectivesStr,
	}

	// Execute the step
	executionResult, _, err := executionAgent.Execute(ctx, executionTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("execution failed: %w", err)
	}

	return executionResult, nil
}

// validateSingleStep validates a single step
func (po *PlannerOrchestrator) validateSingleStep(ctx context.Context, step ParallelStep, executionResult string, stepIndex int) (string, error) {
	// Create dedicated validation agent for this step
	validationAgent, err := po.createDedicatedValidationAgent(ctx, stepIndex)
	if err != nil {
		return "", fmt.Errorf("failed to create validation agent: %w", err)
	}

	// Prepare validation template variables
	validationTemplateVars := map[string]string{
		"Objective":        po.GetObjective(),
		"StepDescription":  step.Description,
		"ExecutionResults": executionResult,
		"WorkspacePath":    po.GetWorkspacePath(),
	}

	// Validate the step
	validationResult, _, err := validationAgent.Execute(ctx, validationTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	return validationResult, nil
}

// createDedicatedExecutionAgent creates a dedicated execution agent based on execution mode
func (po *PlannerOrchestrator) createDedicatedExecutionAgent(ctx context.Context, stepIndex, iteration int) (agents.OrchestratorAgent, error) {
	// Check execution mode to determine which agent to create
	if po.IsParallelMode() {
		// Use parallel execution agent for parallel mode
		agentName := fmt.Sprintf("parallel-execution-agent-step-%d", stepIndex+1)

		agent, err := po.CreateAndSetupStandardAgent(
			ctx,
			agentName,
			"parallel_execution", // phase
			stepIndex,            // step
			iteration,            // iteration
			po.GetMaxTurns(),     // maxTurns
			agents.OutputFormatStructured,
			func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
				return agents.NewOrchestratorParallelExecutionAgent(ctx, config, logger, tracer, eventBridge)
			},
			po.WorkspaceTools,
			po.WorkspaceToolExecutors,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create parallel execution agent: %w", err)
		}
		return agent, nil
	} else {
		// Use regular execution agent for sequential mode
		agentName := fmt.Sprintf("execution-agent-step-%d", stepIndex+1)

		agent, err := po.CreateAndSetupStandardAgent(
			ctx,
			agentName,
			"sequential_execution", // phase
			stepIndex,              // step
			iteration,              // iteration
			po.GetMaxTurns(),       // maxTurns
			agents.OutputFormatStructured,
			func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
				return agents.NewOrchestratorExecutionAgent(ctx, config, logger, tracer, eventBridge)
			},
			po.WorkspaceTools,
			po.WorkspaceToolExecutors,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create execution agent: %w", err)
		}
		return agent, nil
	}
}

// createDedicatedValidationAgent creates a dedicated validation agent for parallel step validation
func (po *PlannerOrchestrator) createDedicatedValidationAgent(ctx context.Context, stepIndex int) (agents.OrchestratorAgent, error) {
	agentName := fmt.Sprintf("validation-agent-step-%d", stepIndex+1)

	// Use standardized agent creation and setup
	agent, err := po.CreateAndSetupStandardAgent(
		ctx,
		agentName,
		"parallel_validation", // phase
		stepIndex,             // step
		0,                     // iteration
		po.GetMaxTurns(),      // maxTurns
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return agents.NewOrchestratorValidationAgent(config, logger, tracer, eventBridge)
		},
		po.WorkspaceTools,
		po.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create validation agent: %w", err)
	}

	return agent, nil
}

// createPlanningAgent creates a planning agent on-demand
func (po *PlannerOrchestrator) createPlanningAgent(ctx context.Context, stepIndex, iteration int) (agents.OrchestratorAgent, error) {
	// Use standardized agent creation and setup
	agent, err := po.CreateAndSetupStandardAgent(
		ctx,
		"planning-agent",
		"planning",       // phase
		stepIndex,        // step
		iteration,        // iteration
		po.GetMaxTurns(), // maxTurns
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return agents.NewOrchestratorPlanningAgent(config, logger, tracer, eventBridge)
		},
		po.WorkspaceTools,
		po.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create planning agent: %w", err)
	}

	return agent, nil
}

// createPlanBreakdownAgent creates a plan breakdown agent on-demand
func (po *PlannerOrchestrator) createPlanBreakdownAgent(ctx context.Context, stepIndex, iteration int) (agents.OrchestratorAgent, error) {
	// Use standardized agent creation and setup
	agent, err := po.CreateAndSetupStandardAgent(
		ctx,
		"plan-breakdown-agent",
		"plan_breakdown", // phase
		stepIndex,        // step
		iteration,        // iteration
		po.GetMaxTurns(), // maxTurns
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return agents.NewPlanBreakdownAgent(config, logger, tracer, eventBridge)
		},
		po.WorkspaceTools,
		po.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan breakdown agent: %w", err)
	}

	return agent, nil
}

// createOrganizerAgent creates an organizer agent on-demand
func (po *PlannerOrchestrator) createOrganizerAgent(ctx context.Context, stepIndex, iteration int) (agents.OrchestratorAgent, error) {
	// Use standardized agent creation and setup
	agent, err := po.CreateAndSetupStandardAgent(
		ctx,
		"plan-organizer-agent",
		"plan_organizer", // phase
		stepIndex,        // step
		iteration,        // iteration
		po.GetMaxTurns(), // maxTurns
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return agents.NewPlanOrganizerAgent(config, logger, tracer, eventBridge)
		},
		po.WorkspaceTools,
		po.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create organizer agent: %w", err)
	}

	return agent, nil
}

// createReportAgent creates a report agent on-demand
func (po *PlannerOrchestrator) createReportAgent(ctx context.Context, stepIndex, iteration int) (agents.OrchestratorAgent, error) {
	// Use standardized agent creation and setup
	agent, err := po.CreateAndSetupStandardAgent(
		ctx,
		"report-agent",
		"report_generation", // phase
		stepIndex,           // step
		iteration,           // iteration
		po.GetMaxTurns(),    // maxTurns
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return agents.NewOrchestratorReportAgent(config, logger, tracer, eventBridge)
		},
		po.WorkspaceTools,
		po.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create report agent: %w", err)
	}

	return agent, nil
}

// organizeParallelResults organizes results from parallel execution using organizer agent
func (po *PlannerOrchestrator) organizeParallelResults(ctx context.Context, results []ParallelResult) (string, error) {
	po.GetLogger().Infof("📊 Organizing parallel execution results using organizer agent")

	// Create organizer agent on-demand
	organizerAgent, err := po.createOrganizerAgent(ctx, 0, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create organizer agent: %w", err)
	}

	// Set orchestrator context for organizer agent

	// Prepare organizer template variables
	organizerTemplateVars := map[string]string{
		"Objective":       po.GetObjective(),
		"ParallelResults": po.formatParallelResults(results),
		"WorkspacePath":   po.GetWorkspacePath(),
	}

	// Organize the results using organizer agent
	organizedResult, _, err := organizerAgent.Execute(ctx, organizerTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("parallel organization failed: %w", err)
	}

	po.GetLogger().Infof("✅ Parallel results organized successfully")
	return organizedResult, nil
}

// formatParallelResults formats parallel results for display
func (po *PlannerOrchestrator) formatParallelResults(results []ParallelResult) string {
	formatted := "Parallel Execution Results:\n\n"
	for i, result := range results {
		formatted += fmt.Sprintf("Step %d (%s):\n", i+1, result.StepID)
		formatted += fmt.Sprintf("- Success: %t\n", result.Success)
		if result.Success {
			formatted += fmt.Sprintf("- Execution: %s\n", result.ExecutionResult)
			formatted += fmt.Sprintf("- Validation: %s\n", result.ValidationResult)
		} else {
			formatted += fmt.Sprintf("- Error: %s\n", result.Error)
		}
		formatted += "\n"
	}
	return formatted
}

// generateParallelReport generates the final report from parallel execution using report agent
func (po *PlannerOrchestrator) generateParallelReport(ctx context.Context, organizedResult string, results []ParallelResult) (string, error) {
	po.GetLogger().Infof("📋 Generating parallel execution report using report agent")

	// Create report agent on-demand
	reportAgent, err := po.createReportAgent(ctx, 0, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create report agent: %w", err)
	}

	// Set orchestrator context for report agent

	// Prepare report template variables
	reportTemplateVars := map[string]string{
		"Objective":        po.GetObjective(),
		"OrganizedResults": organizedResult,
		"ParallelResults":  po.formatParallelResults(results),
		"WorkspacePath":    po.GetWorkspacePath(),
	}

	// Generate the report using report agent
	finalReport, _, err := reportAgent.Execute(ctx, reportTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("parallel report generation failed: %w", err)
	}

	po.GetLogger().Infof("✅ Parallel execution report generated successfully")
	return finalReport, nil
}

// createConditionalLLM creates a conditional LLM on-demand with planner-specific configuration
func (po *PlannerOrchestrator) createConditionalLLM() (*llm.ConditionalLLM, error) {
	// Create config for conditional LLM using planner-specific settings
	conditionalConfig := &agents.OrchestratorAgentConfig{
		Provider:      po.GetProvider(),
		Model:         po.GetModel(),
		Temperature:   po.GetTemperature(),
		ServerNames:   po.GetSelectedServers(),
		MCPConfigPath: po.GetMCPConfigPath(),
	}

	// Create conditional LLM with planner-specific context
	conditionalLLM, err := llm.CreateConditionalLLMWithEventBridge(conditionalConfig, po.GetContextAwareBridge(), po.GetLogger(), po.GetTracer())
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional LLM: %w", err)
	}

	return conditionalLLM, nil
}

// extractShouldContinue uses the conditional LLM to determine if the plan is executable and will achieve the objective
func (po *PlannerOrchestrator) extractShouldContinue(ctx context.Context, rawResponse string) bool {
	// Create conditional LLM on-demand
	conditionalLLM, err := po.createConditionalLLM()
	if err != nil {
		po.GetLogger().Errorf("❌ Failed to create conditional LLM: %v", err)
		return true // Default to continue if conditional LLM creation fails
	}

	// Use conditional LLM to make the objective achievement decision
	result, err := conditionalLLM.Decide(ctx, rawResponse, "Are there any incomplete steps in the plan. Yes or no", 0, 0)
	if err != nil {
		po.GetLogger().Errorf("❌ Conditional LLM objective achievement check failed: %v", err)
		return true // Default to continue if conditional LLM fails
	}

	po.GetLogger().Infof("🤔 Conditional LLM objective achievement check: %t", result.GetResult())
	return result.GetResult()
}

// GetExecutionMode returns the current execution mode
func (po *PlannerOrchestrator) GetExecutionMode() ExecutionMode {
	if po.selectedOptions != nil {
		for _, selection := range po.selectedOptions.Selections {
			if selection.Group == "execution_strategy" {
				return ParseExecutionMode(selection.OptionID)
			}
		}
	}
	return ParallelExecution // default
}

// IsParallelMode returns true if the orchestrator is in parallel mode
func (po *PlannerOrchestrator) IsParallelMode() bool {
	return po.GetExecutionMode() == ParallelExecution
}

// Execute implements the Orchestrator interface
func (po *PlannerOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate objective
	if objective == "" {
		return "", fmt.Errorf("objective cannot be empty")
	}

	// Validate options if provided
	var selectedOptions *PlannerSelectedOptions
	if options != nil {
		// Validate selectedOptions if provided
		if selectedOptsVal, exists := options["selectedOptions"]; exists {
			if selectedOptsVal != nil {
				if so, ok := selectedOptsVal.(*PlannerSelectedOptions); !ok {
					return "", fmt.Errorf("invalid selectedOptions: expected *PlannerSelectedOptions, got %T", selectedOptsVal)
				} else {
					selectedOptions = so
					// Validate execution mode in selectedOptions
					validExecutionMode := false
					for _, selection := range selectedOptions.Selections {
						if selection.Group == "execution_strategy" {
							executionMode := ParseExecutionMode(selection.OptionID)
							if executionMode.IsValid() {
								validExecutionMode = true
							} else {
								return "", fmt.Errorf("invalid execution mode in selectedOptions: %s, valid modes: %v", selection.OptionID, []ExecutionMode{SequentialExecution, ParallelExecution})
							}
							break
						}
					}
					if !validExecutionMode {
						return "", fmt.Errorf("selectedOptions must contain a valid execution_strategy selection")
					}
				}
			}
		}

		// Check for any other unexpected options
		validOptionKeys := map[string]bool{"selectedOptions": true}
		for key := range options {
			if !validOptionKeys[key] {
				return "", fmt.Errorf("unexpected option: %s, planner orchestrator only accepts: selectedOptions", key)
			}
		}
	}

	// Set workspace path from parameter
	po.SetWorkspacePath(workspacePath)

	// If selectedOptions were provided in options, update the orchestrator's selectedOptions
	if selectedOptions != nil {
		po.selectedOptions = selectedOptions
	}

	// Determine execution mode and route accordingly
	executionMode := po.GetExecutionMode()
	po.GetLogger().Infof("🎯 Execution mode: %s", executionMode.String())

	// Call executeFlow with empty conversation history and nil event bridge
	return po.executeFlow(ctx, objective, []llms.MessageContent{}, nil)
}

// executeFlow executes the orchestrator flow with conversation history and event bridge
func (po *PlannerOrchestrator) executeFlow(ctx context.Context, objective string, conversationHistory []llms.MessageContent, eventBridge mcpagent.AgentEventListener) (string, error) {
	// Set conversation history
	po.conversationHistory = conversationHistory

	if len(conversationHistory) > 0 {
		po.GetLogger().Infof("📚 Loaded %d messages from conversation history", len(conversationHistory))
	}

	// Determine execution mode and route accordingly
	executionMode := po.GetExecutionMode()
	po.GetLogger().Infof("🎯 Execution mode: %s", executionMode.String())

	switch executionMode {
	case ParallelExecution:
		return po.executeParallel(ctx, objective)
	case SequentialExecution:
		fallthrough
	default:
		return po.executeSequential(ctx, objective)
	}
}
