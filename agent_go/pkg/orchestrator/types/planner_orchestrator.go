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

	"github.com/tmc/langchaingo/llms"
)

// OrchestratorState represents the complete state of the orchestrator
type OrchestratorState struct {
	// Execution state
	CurrentIteration int `json:"current_iteration"`
	CurrentStepIndex int `json:"current_step_index"`
	MaxIterations    int `json:"max_iterations"`

	// Results from each phase
	PlanningResults     []string `json:"planning_results"`
	ExecutionResults    []string `json:"execution_results"`
	ValidationResults   []string `json:"validation_results"`
	OrganizationResults []string `json:"organization_results"`
	ReportResults       []string `json:"report_results"`

	// Current phase context
	CurrentPhase       string `json:"current_phase"` // "planning", "execution", "validation", "organizer"
	ShouldContinue     bool   `json:"should_continue"`
	LastPlanningResult string `json:"last_planning_result"`

	// Configuration state
	Objective       string   `json:"objective"`
	SelectedServers []string `json:"selected_servers"`

	// Timing
	StartTime      time.Time `json:"start_time"`
	LastUpdateTime time.Time `json:"last_update_time"`

	// Conversation history
	ConversationHistory []llms.MessageContent `json:"conversation_history"`
}

// LLMConfig represents the LLM configuration from frontend
type LLMConfig struct {
	Provider              string                        `json:"provider"`
	ModelID               string                        `json:"model_id"`
	FallbackModels        []string                      `json:"fallback_models"`
	CrossProviderFallback *agents.CrossProviderFallback `json:"cross_provider_fallback,omitempty"`
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
}

// GetEventType returns the event type for IndependentStepsSelectedEvent
func (e *IndependentStepsSelectedEvent) GetEventType() events.EventType {
	return events.IndependentStepsSelected
}

// PlannerOrchestrator handles the flow from planning agent to execution agent
type PlannerOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Configuration
	provider      string
	model         string
	mcpConfigPath string
	temperature   float64
	agentMode     string

	// Execution mode configuration
	selectedOptions *PlannerSelectedOptions // Selected execution options

	// Selected servers for execution
	selectedServers []string

	// Detailed LLM configuration from frontend
	llmConfig *LLMConfig

	// Tracer for observability
	tracer observability.Tracer

	// Conversation history for context
	conversationHistory []llms.MessageContent

	// Orchestrator utils for agent management
	orchestratorUtils *OrchestratorUtils

	// Workspace path extracted from objective
	workspacePath string

	// Planner-specific state management
	currentIteration int
	currentStepIndex int
	maxIterations    int

	// Planner-specific results storage
	planningResults     []string
	executionResults    []string
	validationResults   []string
	organizationResults []string
	reportResults       []string

	// Parallel execution state
	parallelSteps   []ParallelStep
	parallelResults []ParallelResult
}

// NewPlannerOrchestrator creates a new planner orchestrator with default configurations
func NewPlannerOrchestrator(logger utils.ExtendedLogger, agentMode string, selectedOptions *PlannerSelectedOptions) *PlannerOrchestrator {
	return &PlannerOrchestrator{
		agentMode:       agentMode,
		selectedOptions: selectedOptions,
		// Initialize planner-specific state
		currentIteration:    0,
		currentStepIndex:    0,
		maxIterations:       10,
		planningResults:     make([]string, 0),
		executionResults:    make([]string, 0),
		validationResults:   make([]string, 0),
		organizationResults: make([]string, 0),
		reportResults:       make([]string, 0),
		// Initialize parallel execution state
		parallelSteps:   make([]ParallelStep, 0),
		parallelResults: make([]ParallelResult, 0),
	}
}

// InitializeAgents initializes the planner orchestrator with provided configurations
func (po *PlannerOrchestrator) InitializeAgents(ctx context.Context, provider, model, mcpConfigPath string, tracerID observability.TraceID, temperature float64, agentEventBridge EventBridge, selectedServers []string, cacheOnly bool, llmConfig *LLMConfig, tracer observability.Tracer, logger utils.ExtendedLogger, customTools []llms.Tool, customToolExecutors map[string]interface{}) error {

	// Store configuration values
	po.provider = provider
	po.model = model
	po.mcpConfigPath = mcpConfigPath
	po.temperature = temperature
	po.selectedServers = selectedServers
	po.llmConfig = llmConfig
	po.tracer = tracer

	// Create base orchestrator
	config := po.createAgentConfig("planner", "planner-orchestrator", 100, logger)
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		config,
		logger,
		tracer,
		agentEventBridge,
		agents.PlannerOrchestratorAgentType,
		orchestrator.OrchestratorTypePlanner,
	)
	if err != nil {
		return fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	po.BaseOrchestrator = baseOrchestrator

	// Store custom tools for use during agent setup
	po.SetWorkspaceTools(customTools, customToolExecutors)

	po.AgentTemplate.GetLogger().Infof("✅ PlannerOrchestrator initialized successfully - agents will be created on-demand")
	return nil
}

// setupAgent performs common agent setup tasks using shared utilities
func (po *PlannerOrchestrator) setupAgent(agent agents.OrchestratorAgent, agentType, agentName string, agentEventBridge EventBridge) error {
	// Create orchestrator config
	config := &OrchestratorConfig{
		Provider:        po.provider,
		Model:           po.model,
		MCPConfigPath:   po.mcpConfigPath,
		Temperature:     po.temperature,
		SelectedServers: po.selectedServers,
		AgentMode:       po.agentMode,
		Logger:          po.AgentTemplate.GetLogger(),
	}

	utils := newOrchestratorUtils(config)
	po.orchestratorUtils = utils // Store reference for later context updates

	// Use shared setup function with custom tools passed during initialization
	return utils.setupAgent(
		agent,
		agentType,
		agentName,
		po.WorkspaceTools, // Use custom tools passed during InitializeAgents
		po.WorkspaceToolExecutors,
		agentEventBridge,
		nil, // Context setting function - planner doesn't have specific context setting
	)
}

// createAgentConfig creates a generic agent configuration using shared utilities
func (po *PlannerOrchestrator) createAgentConfig(agentType, agentName string, maxTurns int, logger utils.ExtendedLogger) *agents.OrchestratorAgentConfig {
	config := &OrchestratorConfig{
		Provider:        po.provider,
		Model:           po.model,
		MCPConfigPath:   po.mcpConfigPath,
		Temperature:     po.temperature,
		SelectedServers: po.selectedServers,
		AgentMode:       po.agentMode,
		Logger:          logger,
	}

	utils := newOrchestratorUtils(config)

	setupConfig := &AgentSetupConfig{
		AgentType:    agentType,
		AgentName:    agentName,
		MaxTurns:     maxTurns,
		AgentMode:    po.agentMode,
		OutputFormat: agents.OutputFormatStructured,
	}

	// Convert LLMConfig to shared format
	var llmConfig *SharedLLMConfig
	if po.llmConfig != nil {
		llmConfig = &SharedLLMConfig{
			Provider:              po.llmConfig.Provider,
			ModelID:               po.llmConfig.ModelID,
			FallbackModels:        po.llmConfig.FallbackModels,
			CrossProviderFallback: po.llmConfig.CrossProviderFallback,
		}
	}

	return utils.createAgentConfigWithLLM(setupConfig, llmConfig)
}

// ExecuteFlow executes the complete orchestrator flow from planning to execution
func (po *PlannerOrchestrator) ExecuteFlow(ctx context.Context, objective string, conversationHistory []llms.MessageContent, agentEventBridge EventBridge) (string, error) {
	// Set objective and conversation history
	po.conversationHistory = conversationHistory

	if len(conversationHistory) > 0 {
		po.AgentTemplate.GetLogger().Infof("📚 Loaded %d messages from conversation history", len(conversationHistory))
	}

	// Extract workspace path from objective
	po.workspacePath = extractWorkspacePathFromObjective(objective)
	if po.workspacePath != "" {
		po.AgentTemplate.GetLogger().Infof("📁 Using workspace path: %s", po.workspacePath)
	} else {
		po.AgentTemplate.GetLogger().Warnf("⚠️ No workspace path found in objective")
	}

	// Orchestrator start event is now automatically emitted by BasePlannerOrchestrator.Execute()

	// Initialize variables for routing
	maxIterations := po.GetMaxIterations()
	startIteration := po.GetCurrentIteration()

	po.AgentTemplate.GetLogger().Infof("🔄 Starting iterative execution (max %d iterations) from iteration %d", maxIterations, startIteration)

	// Determine execution mode and route accordingly
	executionMode := po.GetExecutionMode()
	po.AgentTemplate.GetLogger().Infof("🎯 Execution mode: %s", executionMode.String())

	switch executionMode {
	case ParallelExecution:
		return po.executeParallel(ctx, objective, maxIterations, startIteration)
	case SequentialExecution:
		fallthrough
	default:
		return po.executeSequential(ctx, objective, maxIterations, startIteration)
	}
}

// executeSequential executes the original sequential flow
func (po *PlannerOrchestrator) executeSequential(ctx context.Context, objective string, maxIterations, startIteration int) (string, error) {
	po.AgentTemplate.GetLogger().Infof("🔄 Starting sequential execution mode")

	// Helper function to emit orchestrator error event
	emitOrchestratorError := func(err error, context string) {
		if po.GetEventBridge() != nil {
			duration := time.Since(po.GetStartTime())
			orchestratorErrorEvent := &events.OrchestratorErrorEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Context:  context,
				Error:    err.Error(),
				Duration: duration,
			}

			// Create unified event wrapper
			unifiedEvent := &events.AgentEvent{
				Type:      events.OrchestratorError,
				Timestamp: time.Now(),
				Data:      orchestratorErrorEvent,
			}

			// Emit through the bridge
			if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
				bridge.HandleEvent(ctx, unifiedEvent)
				po.AgentTemplate.GetLogger().Infof("✅ Emitted orchestrator error event: %s", context)
			}
		}
	}

	// Initialize variables for the iterative loop
	currentStepIndex := po.GetCurrentStepIndex()
	executionResults := po.GetExecutionResults()
	validationResults := po.GetValidationResults()

	// Main iterative loop - continue from current state
	for iteration := startIteration; iteration < maxIterations; iteration++ {
		po.AgentTemplate.GetLogger().Infof("🔄 Iteration %d/%d", iteration+1, maxIterations)

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

			if len(po.GetReportResults()) > 0 {
				reportResultsStr = strings.Join(po.GetReportResults(), "\n\n")
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
			"WorkspacePath":     po.workspacePath,
		}

		// Get next step decision from planning agent

		// Create planning agent on-demand
		planningAgent, err := po.createPlanningAgent()
		if err != nil {
			return "", fmt.Errorf("failed to create planning agent: %w", err)
		}

		// Set orchestrator context for planning agent
		planningAgent.SetOrchestratorContext(currentStepIndex, iteration, objective, "planning-agent")
		// Also update the context-aware event bridge
		if po.orchestratorUtils != nil {
			po.orchestratorUtils.UpdateAgentContext("planning agent", "planning", currentStepIndex, iteration)
		}

		// Use Execute method to get structured response from planning agent with guidance
		planningTemplateVars["Objective"] = objective
		planningResult, err := planningAgent.Execute(ctx, planningTemplateVars, po.conversationHistory)

		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Planning failed: %v", err)
			emitOrchestratorError(err, "planning phase")
			return "", fmt.Errorf("planning failed: %w", err)
		}

		// Extract should_continue from the raw planning result using conditional LLM
		shouldContinue := po.extractShouldContinue(ctx, planningResult)

		// Store planning result for this iteration
		po.AddPlanningResult(planningResult)

		// Check if we should continue - BREAK if planning says no
		if !shouldContinue {
			po.AgentTemplate.GetLogger().Infof("✅ Workflow completion confirmed by planning agent")
			break
		}

		// Execute the current step
		po.AgentTemplate.GetLogger().Infof("🚀 Executing step %d", currentStepIndex+1)

		// Create execution agent on-demand
		executionAgent, err := po.createDedicatedExecutionAgent(currentStepIndex)
		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Failed to create execution agent: %v", err)
			emitOrchestratorError(err, "execution phase")
			return "", fmt.Errorf("failed to create execution agent: %w", err)
		}

		// Set orchestrator context for execution agent
		executionObjective := fmt.Sprintf("Execute step %d: %s", currentStepIndex+1, planningResult)
		executionAgent.SetOrchestratorContext(currentStepIndex, iteration, executionObjective, "execution-agent")
		// Also update the context-aware event bridge
		if po.orchestratorUtils != nil {
			po.orchestratorUtils.UpdateAgentContext("execution agent", "execution", currentStepIndex, iteration)
		}

		// Execute the current step using the raw planning response with guidance
		executionTemplateVars := map[string]string{
			"Objective":     planningResult, // Pass the planning result directly
			"WorkspacePath": po.workspacePath,
		}

		executionResult, err := executionAgent.Execute(ctx, executionTemplateVars, po.conversationHistory)

		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Execution failed for step %d: %v", currentStepIndex+1, err)
			emitOrchestratorError(err, fmt.Sprintf("execution phase - step %d", currentStepIndex+1))
			return "", fmt.Errorf("failed to execute step %d: %w", currentStepIndex+1, err)
		}

		po.AddExecutionResult(executionResult)

		// ✅ VALIDATION PHASE - Validate this step's execution result immediately

		// Create validation agent on-demand
		validationAgent, err := po.createDedicatedValidationAgent(currentStepIndex)
		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Failed to create validation agent: %v", err)
			emitOrchestratorError(err, "validation phase")
			return "", fmt.Errorf("failed to create validation agent: %w", err)
		}

		// Set orchestrator context for validation agent
		validationObjective := fmt.Sprintf("Validate step %d execution result against original plan", currentStepIndex+1)
		validationAgent.SetOrchestratorContext(currentStepIndex, iteration, validationObjective, "validation-agent")
		// Also update the context-aware event bridge
		if po.orchestratorUtils != nil {
			po.orchestratorUtils.UpdateAgentContext("validation agent", "validation", currentStepIndex, iteration)
		}

		// Prepare validation template variables with guidance
		validationTemplateVars := map[string]string{
			"Objective":        objective,
			"StepDescription":  planningResult, // Pass the original planning result directly
			"ExecutionResults": fmt.Sprintf("Step %d: %s", currentStepIndex+1, executionResult),
			"WorkspacePath":    po.workspacePath,
		}

		stepValidationResult, err := validationAgent.Execute(ctx, validationTemplateVars, po.conversationHistory)

		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Validation failed for step %d: %v", currentStepIndex+1, err)
			// Continue with execution result even if validation fails
			po.AgentTemplate.GetLogger().Warnf("⚠️ Continuing with execution result despite validation failure")
			// Set empty validation result when validation fails
			stepValidationResult = "Validation failed: " + err.Error()
		}

		// Store validation results for this step
		po.AddValidationResult(stepValidationResult)

		// ✅ ORGANIZATION PHASE - Organize this step's results immediately

		// Create organizer agent on-demand
		organizerAgent, err := po.createOrganizerAgent()
		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Failed to create organizer agent: %v", err)
			emitOrchestratorError(err, "organization phase")
			return "", fmt.Errorf("failed to create organizer agent: %w", err)
		}

		// Execute plan organization for this step with guidance
		organizationTemplateVars := map[string]string{
			"WorkflowContext":  fmt.Sprintf("Step %d of workflow for objective: %s", currentStepIndex+1, objective),
			"PlanningOutput":   planningResult, // Pass the original planning result directly
			"ExecutionOutput":  executionResult,
			"ValidationOutput": stepValidationResult,
			"WorkspacePath":    po.workspacePath,
		}

		organizerObjective := fmt.Sprintf("Organize and consolidate results from step %d for objective: %s", currentStepIndex+1, objective)

		// Set orchestrator context for organizer agent
		organizerAgent.SetOrchestratorContext(currentStepIndex, iteration, organizerObjective, "plan-organizer-agent")
		// Also update the context-aware event bridge
		if po.orchestratorUtils != nil {
			po.orchestratorUtils.UpdateAgentContext("organizer agent", "plan_organizer", currentStepIndex, iteration)
		}

		stepOrganizationResult, err := organizerAgent.Execute(ctx, organizationTemplateVars, po.conversationHistory)

		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Step %d organization failed: %v", currentStepIndex+1, err)
		} else {
			// Store the organized results for this step
			po.AddOrganizationResult(stepOrganizationResult)
		}

		// ✅ REPORT GENERATION PHASE - Generate report for this iteration

		// Create report agent on-demand
		reportAgent, err := po.createReportAgent()
		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Failed to create report agent: %v", err)
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
			"WorkspacePath":       po.workspacePath,
		}

		reportObjective := fmt.Sprintf("Generate report for step %d of objective: %s", currentStepIndex+1, objective)

		// Set orchestrator context for report agent
		reportAgent.SetOrchestratorContext(currentStepIndex, iteration, reportObjective, "report-agent")
		// Also update the context-aware event bridge
		if po.orchestratorUtils != nil {
			po.orchestratorUtils.UpdateAgentContext("report agent", "report_generation", currentStepIndex, iteration)
		}

		reportResult, err := reportAgent.Execute(ctx, reportTemplateVars, po.conversationHistory)

		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Step %d report generation failed: %v", currentStepIndex+1, err)
			// Continue even if report generation fails
			po.AgentTemplate.GetLogger().Warnf("⚠️ Continuing despite report generation failure")
		} else {
			// Store the report result for this step
			po.AddReportResult(reportResult)
		}

		// Move to next step
		currentStepIndex++
		po.SetCurrentStepIndex(currentStepIndex)
		po.SetCurrentIteration(iteration + 1)
	}

	// Prepare final result with iteration-by-iteration breakdown
	finalResult := fmt.Sprintf("Sequential orchestrator completed after %d iterations with %d steps executed.\n\n", startIteration+1, len(po.GetExecutionResults()))

	// Add iteration-by-iteration results
	finalResult += "ITERATION RESULTS:\n"
	finalResult += "=================\n"

	if len(po.GetExecutionResults()) > 0 {
		for i := 0; i < len(po.GetExecutionResults()); i++ {
			finalResult += fmt.Sprintf("ITERATION %d:\n", i+1)
			finalResult += "-----------\n"

			// Planning
			finalResult += "📋 PLANNING:\n"
			if i < len(po.GetPlanningResults()) && po.GetPlanningResults()[i] != "" {
				finalResult += fmt.Sprintf("Raw Response: %s\n", po.GetPlanningResults()[i])
			} else {
				finalResult += "No planning result available\n"
			}
			finalResult += "\n"

			// Execution
			finalResult += "🚀 EXECUTION:\n"
			if i < len(po.GetExecutionResults()) && po.GetExecutionResults()[i] != "" {
				finalResult += po.GetExecutionResults()[i] + "\n"
			} else {
				finalResult += "No execution result available\n"
			}
			finalResult += "\n"

			// Validation
			finalResult += "🔍 VALIDATION:\n"
			if i < len(po.GetValidationResults()) && po.GetValidationResults()[i] != "" {
				finalResult += po.GetValidationResults()[i] + "\n"
			} else {
				finalResult += "No validation result available\n"
			}
			finalResult += "\n"

			// Organization
			finalResult += "📊 ORGANIZATION:\n"
			if i < len(po.GetOrganizationResults()) && po.GetOrganizationResults()[i] != "" {
				finalResult += po.GetOrganizationResults()[i] + "\n"
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
	if len(po.GetPlanningResults()) > 0 && po.GetPlanningResults()[len(po.GetPlanningResults())-1] != "" {
		lastPlanningResult := po.GetPlanningResults()[len(po.GetPlanningResults())-1]
		finalResult += "FINAL PLANNING DECISION:\n"
		finalResult += "========================\n"
		finalResult += fmt.Sprintf("Raw Response: %s\n", lastPlanningResult)
	}

	po.AgentTemplate.GetLogger().Infof("🎉 Sequential Planner Orchestrator Flow completed successfully after %d iterations!", startIteration+1)

	return finalResult, nil
}

// executeParallel executes the parallel flow with dependency analysis and goroutines
func (po *PlannerOrchestrator) executeParallel(ctx context.Context, objective string, maxIterations, startIteration int) (string, error) {
	po.AgentTemplate.GetLogger().Infof("🚀 Starting parallel execution mode")

	// Helper function to emit orchestrator error event
	emitOrchestratorError := func(err error, context string) {
		if po.GetEventBridge() != nil {
			duration := time.Since(po.GetStartTime())
			orchestratorErrorEvent := &events.OrchestratorErrorEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Context:  context,
				Error:    err.Error(),
				Duration: duration,
			}

			// Create unified event wrapper
			unifiedEvent := &events.AgentEvent{
				Type:      events.OrchestratorError,
				Timestamp: time.Now(),
				Data:      orchestratorErrorEvent,
			}

			// Emit through the bridge
			if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
				bridge.HandleEvent(ctx, unifiedEvent)
				po.AgentTemplate.GetLogger().Infof("✅ Emitted orchestrator error event: %s", context)
			}
		}
	}

	// Step 1: Get initial plan from planning agent
	planningResult, err := po.getInitialPlan(ctx, objective)
	if err != nil {
		emitOrchestratorError(err, "initial planning phase")
		return "", fmt.Errorf("failed to get initial plan: %w", err)
	}

	// Step 2: Use plan breakdown agent to analyze dependencies and get independent steps
	independentSteps, err := po.analyzeDependencies(ctx, planningResult)
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

	po.AgentTemplate.GetLogger().Infof("🎉 Parallel execution completed successfully!")
	return finalReport, nil
}

// Helper methods for parallel execution

// getInitialPlan gets the initial plan from the planning agent
func (po *PlannerOrchestrator) getInitialPlan(ctx context.Context, objective string) (string, error) {
	po.AgentTemplate.GetLogger().Infof("📋 Getting initial plan from planning agent")

	// Set orchestrator context for planning agent
	planningAgent, err := po.createPlanningAgent()
	if err != nil {
		return "", fmt.Errorf("failed to create planning agent: %w", err)
	}

	planningAgent.SetOrchestratorContext(0, 0, objective, "planning-agent")
	if po.orchestratorUtils != nil {
		po.orchestratorUtils.UpdateAgentContext("planning agent", "planning", 0, 0)
	}

	// Prepare planning template variables
	planningTemplateVars := map[string]string{
		"Objective":     objective,
		"WorkspacePath": po.workspacePath,
	}

	// Execute planning agent
	planningResult, err := planningAgent.Execute(ctx, planningTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("planning agent failed: %w", err)
	}

	po.AgentTemplate.GetLogger().Infof("✅ Initial plan generated successfully")
	return planningResult, nil
}

// analyzeDependencies analyzes dependencies and creates independent steps using structured output
func (po *PlannerOrchestrator) analyzeDependencies(ctx context.Context, planningResult string) ([]ParallelStep, error) {
	po.AgentTemplate.GetLogger().Infof("🔍 Analyzing dependencies for parallel execution using structured output")

	// Create plan breakdown agent on-demand
	breakdownAgent, err := po.createPlanBreakdownAgent()
	if err != nil {
		return nil, fmt.Errorf("failed to create plan breakdown agent: %w", err)
	}

	// Set orchestrator context for breakdown agent
	breakdownAgent.SetOrchestratorContext(0, 0, "Analyze plan dependencies", "plan-breakdown-agent")

	// Use structured output directly from breakdown agent
	breakdownResponse, err := breakdownAgent.(*agents.PlanBreakdownAgent).AnalyzeDependencies(ctx, planningResult, po.GetObjective(), po.workspacePath)
	if err != nil {
		return nil, fmt.Errorf("plan breakdown failed: %w", err)
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

	po.AgentTemplate.GetLogger().Infof("✅ Found %d independent steps for parallel execution", len(parallelSteps))
	return parallelSteps, nil
}

// selectParallelSteps selects up to 3 independent steps for parallel execution
func (po *PlannerOrchestrator) selectParallelSteps(ctx context.Context, independentSteps []ParallelStep) []ParallelStep {
	po.AgentTemplate.GetLogger().Infof("🎯 Selecting up to 3 independent steps from %d available steps", len(independentSteps))

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
		po.AgentTemplate.GetLogger().Warnf("⚠️ No independent steps found, creating fallback step")
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

	po.AgentTemplate.GetLogger().Infof("✅ Selected %d steps for parallel execution", len(trulyIndependent))
	return trulyIndependent
}

// emitIndependentStepsSelectedEvent emits an event when independent steps are selected
func (po *PlannerOrchestrator) emitIndependentStepsSelectedEvent(ctx context.Context, availableSteps, selectedSteps []ParallelStep) {
	if po.GetEventBridge() == nil {
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
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.IndependentStepsSelected,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
		if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
			po.AgentTemplate.GetLogger().Warnf("⚠️ Failed to emit independent steps selected event: %v", err)
		} else {
			po.AgentTemplate.GetLogger().Infof("✅ Emitted independent steps selected event: %d steps selected", len(selectedSteps))
		}
	}
}

// executeStepsInParallel executes steps in parallel with goroutines
func (po *PlannerOrchestrator) executeStepsInParallel(ctx context.Context, steps []ParallelStep) ([]ParallelResult, error) {
	po.AgentTemplate.GetLogger().Infof("🚀 Executing %d steps in parallel", len(steps))

	results := make([]ParallelResult, len(steps))
	errors := make([]error, len(steps))

	var wg sync.WaitGroup

	// Execute each step in a separate goroutine
	for i, step := range steps {
		wg.Add(1)
		go func(index int, parallelStep ParallelStep) {
			defer wg.Done()

			po.AgentTemplate.GetLogger().Infof("🔄 Starting parallel execution of step %d: %s", index+1, parallelStep.Description)

			// Execute step
			executionResult, err := po.executeSingleStep(ctx, parallelStep, index)
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
				po.AgentTemplate.GetLogger().Warnf("⚠️ Validation failed for step %d: %v", index+1, err)
				validationResult = "Validation failed: " + err.Error()
			}

			results[index] = ParallelResult{
				StepID:           parallelStep.ID,
				ExecutionResult:  executionResult,
				ValidationResult: validationResult,
				Success:          true,
			}

			po.AgentTemplate.GetLogger().Infof("✅ Completed parallel execution of step %d", index+1)
		}(i, step)
	}

	wg.Wait()

	// Check for errors and log them
	var failedSteps []string
	var aggregatedErrors []error
	for i, err := range errors {
		if err != nil {
			po.AgentTemplate.GetLogger().Errorf("❌ Step %d failed: %v", i+1, err)
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

	po.AgentTemplate.GetLogger().Infof("✅ All parallel executions completed")
	return results, returnError
}

// executeSingleStep executes a single step
func (po *PlannerOrchestrator) executeSingleStep(ctx context.Context, step ParallelStep, stepIndex int) (string, error) {
	// Create dedicated execution agent for this step
	executionAgent, err := po.createDedicatedExecutionAgent(stepIndex)
	if err != nil {
		return "", fmt.Errorf("failed to create execution agent: %w", err)
	}

	// Set orchestrator context for execution agent
	executionObjective := fmt.Sprintf("Execute parallel step %d: %s", stepIndex+1, step.Description)
	executionAgent.SetOrchestratorContext(stepIndex, 0, executionObjective, "parallel-execution-agent")

	// Prepare execution template variables
	executionTemplateVars := map[string]string{
		"Objective":     step.Description,
		"StepID":        step.ID,
		"WorkspacePath": po.workspacePath,
	}

	// Execute the step
	executionResult, err := executionAgent.Execute(ctx, executionTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("execution failed: %w", err)
	}

	return executionResult, nil
}

// validateSingleStep validates a single step
func (po *PlannerOrchestrator) validateSingleStep(ctx context.Context, step ParallelStep, executionResult string, stepIndex int) (string, error) {
	// Create dedicated validation agent for this step
	validationAgent, err := po.createDedicatedValidationAgent(stepIndex)
	if err != nil {
		return "", fmt.Errorf("failed to create validation agent: %w", err)
	}

	// Set orchestrator context for validation agent
	validationObjective := fmt.Sprintf("Validate parallel step %d execution result", stepIndex+1)
	validationAgent.SetOrchestratorContext(stepIndex, 0, validationObjective, "parallel-validation-agent")

	// Prepare validation template variables
	validationTemplateVars := map[string]string{
		"Objective":        po.GetObjective(),
		"StepDescription":  step.Description,
		"ExecutionResults": executionResult,
		"WorkspacePath":    po.workspacePath,
	}

	// Validate the step
	validationResult, err := validationAgent.Execute(ctx, validationTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	return validationResult, nil
}

// createDedicatedExecutionAgent creates a dedicated execution agent for parallel step execution
func (po *PlannerOrchestrator) createDedicatedExecutionAgent(stepIndex int) (agents.OrchestratorAgent, error) {
	agentName := fmt.Sprintf("execution-agent-step-%d", stepIndex+1)
	executionConfig := po.createAgentConfig("execution", agentName, 100, po.AgentTemplate.GetLogger())

	// Cast event bridge to the correct type
	var eventBridge EventBridge
	if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
		eventBridge = bridge
	}

	executionAgent := agents.NewOrchestratorExecutionAgent(context.Background(), executionConfig, po.AgentTemplate.GetLogger(), po.tracer, eventBridge)

	if err := po.setupAgent(executionAgent, "execution", agentName, eventBridge); err != nil {
		return nil, fmt.Errorf("failed to setup execution agent: %w", err)
	}

	return executionAgent, nil
}

// createDedicatedValidationAgent creates a dedicated validation agent for parallel step validation
func (po *PlannerOrchestrator) createDedicatedValidationAgent(stepIndex int) (agents.OrchestratorAgent, error) {
	agentName := fmt.Sprintf("validation-agent-step-%d", stepIndex+1)
	validationConfig := po.createAgentConfig("validation", agentName, 100, po.AgentTemplate.GetLogger())

	// Cast event bridge to the correct type
	var eventBridge EventBridge
	if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
		eventBridge = bridge
	}

	validationAgent := agents.NewOrchestratorValidationAgent(validationConfig, po.AgentTemplate.GetLogger(), po.tracer, eventBridge)

	if err := po.setupAgent(validationAgent, "validation", agentName, eventBridge); err != nil {
		return nil, fmt.Errorf("failed to setup validation agent: %w", err)
	}

	return validationAgent, nil
}

// createPlanningAgent creates a planning agent on-demand
func (po *PlannerOrchestrator) createPlanningAgent() (agents.OrchestratorAgent, error) {
	planningConfig := po.createAgentConfig("planning", "planning-agent", 100, po.AgentTemplate.GetLogger())

	// Cast event bridge to the correct type
	var eventBridge EventBridge
	if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
		eventBridge = bridge
	}

	planningAgent := agents.NewOrchestratorPlanningAgent(planningConfig, po.AgentTemplate.GetLogger(), po.tracer, eventBridge)

	if err := po.setupAgent(planningAgent, "planning", "planning agent", eventBridge); err != nil {
		return nil, fmt.Errorf("failed to setup planning agent: %w", err)
	}

	return planningAgent, nil
}

// createPlanBreakdownAgent creates a plan breakdown agent on-demand
func (po *PlannerOrchestrator) createPlanBreakdownAgent() (agents.OrchestratorAgent, error) {
	breakdownConfig := po.createAgentConfig("plan_breakdown", "plan-breakdown-agent", 100, po.AgentTemplate.GetLogger())

	// Cast event bridge to the correct type
	var eventBridge EventBridge
	if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
		eventBridge = bridge
	}

	breakdownAgent := agents.NewPlanBreakdownAgent(breakdownConfig, po.AgentTemplate.GetLogger(), po.tracer, eventBridge)

	if err := po.setupAgent(breakdownAgent, "plan_breakdown", "plan breakdown agent", eventBridge); err != nil {
		return nil, fmt.Errorf("failed to setup plan breakdown agent: %w", err)
	}

	return breakdownAgent, nil
}

// createOrganizerAgent creates an organizer agent on-demand
func (po *PlannerOrchestrator) createOrganizerAgent() (agents.OrchestratorAgent, error) {
	organizerConfig := po.createAgentConfig("plan_organizer", "plan-organizer-agent", 100, po.AgentTemplate.GetLogger())

	// Cast event bridge to the correct type
	var eventBridge EventBridge
	if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
		eventBridge = bridge
	}

	organizerAgent := agents.NewPlanOrganizerAgent(organizerConfig, po.AgentTemplate.GetLogger(), po.tracer, eventBridge)

	if err := po.setupAgent(organizerAgent, "organizer", "organizer agent", eventBridge); err != nil {
		return nil, fmt.Errorf("failed to setup organizer agent: %w", err)
	}

	return organizerAgent, nil
}

// createReportAgent creates a report agent on-demand
func (po *PlannerOrchestrator) createReportAgent() (agents.OrchestratorAgent, error) {
	reportConfig := po.createAgentConfig("report_generation", "report-agent", 100, po.AgentTemplate.GetLogger())

	// Cast event bridge to the correct type
	var eventBridge EventBridge
	if bridge, ok := po.GetEventBridge().(mcpagent.AgentEventListener); ok {
		eventBridge = bridge
	}

	reportAgent := agents.NewOrchestratorReportAgent(reportConfig, po.AgentTemplate.GetLogger(), po.tracer, eventBridge)

	if err := po.setupAgent(reportAgent, "report_generation", "report agent", eventBridge); err != nil {
		return nil, fmt.Errorf("failed to setup report agent: %w", err)
	}

	return reportAgent, nil
}

// organizeParallelResults organizes results from parallel execution using organizer agent
func (po *PlannerOrchestrator) organizeParallelResults(ctx context.Context, results []ParallelResult) (string, error) {
	po.AgentTemplate.GetLogger().Infof("📊 Organizing parallel execution results using organizer agent")

	// Create organizer agent on-demand
	organizerAgent, err := po.createOrganizerAgent()
	if err != nil {
		return "", fmt.Errorf("failed to create organizer agent: %w", err)
	}

	// Set orchestrator context for organizer agent
	organizerObjective := fmt.Sprintf("Organize and consolidate results from parallel execution for objective: %s", po.GetObjective())
	organizerAgent.SetOrchestratorContext(0, 0, organizerObjective, "organizer-agent")

	// Prepare organizer template variables
	organizerTemplateVars := map[string]string{
		"Objective":       po.GetObjective(),
		"ParallelResults": po.formatParallelResults(results),
		"WorkspacePath":   po.workspacePath,
	}

	// Organize the results using organizer agent
	organizedResult, err := organizerAgent.Execute(ctx, organizerTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("parallel organization failed: %w", err)
	}

	po.AgentTemplate.GetLogger().Infof("✅ Parallel results organized successfully")
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
	po.AgentTemplate.GetLogger().Infof("📋 Generating parallel execution report using report agent")

	// Create report agent on-demand
	reportAgent, err := po.createReportAgent()
	if err != nil {
		return "", fmt.Errorf("failed to create report agent: %w", err)
	}

	// Set orchestrator context for report agent
	reportObjective := fmt.Sprintf("Generate comprehensive report from parallel execution of objective: %s", po.GetObjective())
	reportAgent.SetOrchestratorContext(0, 0, reportObjective, "report-agent")

	// Prepare report template variables
	reportTemplateVars := map[string]string{
		"Objective":        po.GetObjective(),
		"OrganizedResults": organizedResult,
		"ParallelResults":  po.formatParallelResults(results),
		"WorkspacePath":    po.workspacePath,
	}

	// Generate the report using report agent
	finalReport, err := reportAgent.Execute(ctx, reportTemplateVars, po.conversationHistory)
	if err != nil {
		return "", fmt.Errorf("parallel report generation failed: %w", err)
	}

	po.AgentTemplate.GetLogger().Infof("✅ Parallel execution report generated successfully")
	return finalReport, nil
}

// GetState returns the current orchestrator state
func (po *PlannerOrchestrator) GetState(objective string) (*OrchestratorState, error) {
	state := &OrchestratorState{
		CurrentIteration:    po.GetCurrentIteration(),
		CurrentStepIndex:    po.GetCurrentStepIndex(),
		MaxIterations:       po.GetMaxIterations(),
		PlanningResults:     po.GetPlanningResults(),
		ExecutionResults:    po.GetExecutionResults(),
		ValidationResults:   po.GetValidationResults(),
		OrganizationResults: po.GetOrganizationResults(),
		ReportResults:       po.GetReportResults(),
		CurrentPhase:        "planning", // Default phase
		ShouldContinue:      true,       // Default to continue
		LastPlanningResult:  "",         // Will be set during execution
		Objective:           objective,
		SelectedServers:     po.selectedServers,
		StartTime:           po.GetStartTime(),
		LastUpdateTime:      time.Now(),
		ConversationHistory: po.conversationHistory,
	}
	return state, nil
}

// RestoreState restores the orchestrator state
func (po *PlannerOrchestrator) RestoreState(state *OrchestratorState) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	po.SetCurrentIteration(state.CurrentIteration)
	po.SetCurrentStepIndex(state.CurrentStepIndex)
	po.SetMaxIterations(state.MaxIterations)
	po.selectedServers = state.SelectedServers
	po.conversationHistory = state.ConversationHistory

	// Restore results arrays using local methods
	po.SetPlanningResults(state.PlanningResults)
	po.SetExecutionResults(state.ExecutionResults)
	po.SetValidationResults(state.ValidationResults)
	po.SetOrganizationResults(state.OrganizationResults)
	po.SetReportResults(state.ReportResults)

	po.AgentTemplate.GetLogger().Infof("✅ Orchestrator state restored - iteration %d, step %d", state.CurrentIteration, state.CurrentStepIndex)
	return nil
}

// Close cleans up the orchestrator resources
func (po *PlannerOrchestrator) Close() {
	// No agents to close since they are created on-demand
	po.AgentTemplate.GetLogger().Infof("✅ PlannerOrchestrator closed - no persistent agents to cleanup")
}

// extractShouldContinue uses the conditional LLM to determine if the plan is executable and will achieve the objective
func (po *PlannerOrchestrator) extractShouldContinue(ctx context.Context, rawResponse string) bool {
	return po.extractShouldContinueWithContext(ctx, rawResponse, 0, 0)
}

// extractShouldContinueWithContext uses the conditional LLM to determine if the plan is executable and will achieve the objective
// with step index and iteration context for better event tracking
func (po *PlannerOrchestrator) extractShouldContinueWithContext(ctx context.Context, rawResponse string, stepIndex, iteration int) bool {
	if po.GetConditionalLLM() == nil {
		po.AgentTemplate.GetLogger().Errorf("❌ Conditional LLM not initialized")
		panic("Conditional LLM not initialized - cannot assess objective achievement")
	}

	// Use conditional LLM to make the objective achievement decision
	result, err := po.GetConditionalLLM().Decide(ctx, rawResponse, "Are there any incomplete steps in the plan. Yes or no", stepIndex, iteration)
	if err != nil {
		po.AgentTemplate.GetLogger().Errorf("❌ Conditional LLM objective achievement check failed: %v", err)
		return true // Default to continue if conditional LLM fails
	}

	po.AgentTemplate.GetLogger().Infof("🤔 Conditional LLM objective achievement check: %t", result.GetResult())
	return result.GetResult()
}

// GetCurrentIteration returns the current iteration
func (po *PlannerOrchestrator) GetCurrentIteration() int {
	return po.currentIteration
}

// SetCurrentIteration sets the current iteration
func (po *PlannerOrchestrator) SetCurrentIteration(iteration int) {
	po.currentIteration = iteration
}

// GetCurrentStepIndex returns the current step index
func (po *PlannerOrchestrator) GetCurrentStepIndex() int {
	return po.currentStepIndex
}

// SetCurrentStepIndex sets the current step index
func (po *PlannerOrchestrator) SetCurrentStepIndex(stepIndex int) {
	po.currentStepIndex = stepIndex
}

// GetMaxIterations returns the max iterations
func (po *PlannerOrchestrator) GetMaxIterations() int {
	return po.maxIterations
}

// SetMaxIterations sets the max iterations
func (po *PlannerOrchestrator) SetMaxIterations(maxIterations int) {
	po.maxIterations = maxIterations
}

// Planner-specific results management methods

// GetPlanningResults returns the planning results
func (po *PlannerOrchestrator) GetPlanningResults() []string {
	return po.planningResults
}

// AddPlanningResult adds a planning result
func (po *PlannerOrchestrator) AddPlanningResult(result string) {
	po.planningResults = append(po.planningResults, result)
}

// GetExecutionResults returns the execution results
func (po *PlannerOrchestrator) GetExecutionResults() []string {
	return po.executionResults
}

// AddExecutionResult adds an execution result
func (po *PlannerOrchestrator) AddExecutionResult(result string) {
	po.executionResults = append(po.executionResults, result)
}

// GetValidationResults returns the validation results
func (po *PlannerOrchestrator) GetValidationResults() []string {
	return po.validationResults
}

// AddValidationResult adds a validation result
func (po *PlannerOrchestrator) AddValidationResult(result string) {
	po.validationResults = append(po.validationResults, result)
}

// GetOrganizationResults returns the organization results
func (po *PlannerOrchestrator) GetOrganizationResults() []string {
	return po.organizationResults
}

// AddOrganizationResult adds an organization result
func (po *PlannerOrchestrator) AddOrganizationResult(result string) {
	po.organizationResults = append(po.organizationResults, result)
}

// GetReportResults returns the report results
func (po *PlannerOrchestrator) GetReportResults() []string {
	return po.reportResults
}

// AddReportResult adds a report result
func (po *PlannerOrchestrator) AddReportResult(result string) {
	po.reportResults = append(po.reportResults, result)
}

// ClearPlanningResults clears all planning results
func (po *PlannerOrchestrator) ClearPlanningResults() {
	po.planningResults = make([]string, 0)
}

// ClearExecutionResults clears all execution results
func (po *PlannerOrchestrator) ClearExecutionResults() {
	po.executionResults = make([]string, 0)
}

// ClearValidationResults clears all validation results
func (po *PlannerOrchestrator) ClearValidationResults() {
	po.validationResults = make([]string, 0)
}

// ClearOrganizationResults clears all organization results
func (po *PlannerOrchestrator) ClearOrganizationResults() {
	po.organizationResults = make([]string, 0)
}

// ClearReportResults clears all report results
func (po *PlannerOrchestrator) ClearReportResults() {
	po.reportResults = make([]string, 0)
}

// SetPlanningResults sets all planning results
func (po *PlannerOrchestrator) SetPlanningResults(results []string) {
	po.planningResults = make([]string, len(results))
	copy(po.planningResults, results)
}

// SetExecutionResults sets all execution results
func (po *PlannerOrchestrator) SetExecutionResults(results []string) {
	po.executionResults = make([]string, len(results))
	copy(po.executionResults, results)
}

// SetValidationResults sets all validation results
func (po *PlannerOrchestrator) SetValidationResults(results []string) {
	po.validationResults = make([]string, len(results))
	copy(po.validationResults, results)
}

// SetOrganizationResults sets all organization results
func (po *PlannerOrchestrator) SetOrganizationResults(results []string) {
	po.organizationResults = make([]string, len(results))
	copy(po.organizationResults, results)
}

// SetReportResults sets all report results
func (po *PlannerOrchestrator) SetReportResults(results []string) {
	po.reportResults = make([]string, len(results))
	copy(po.reportResults, results)
}

// Execution mode management methods

// GetExecutionMode returns the current execution mode
func (po *PlannerOrchestrator) GetExecutionMode() ExecutionMode {
	if po.selectedOptions != nil {
		for _, selection := range po.selectedOptions.Selections {
			if selection.Group == "execution_strategy" {
				return ParseExecutionMode(selection.OptionID)
			}
		}
	}
	return SequentialExecution // default
}

// IsParallelMode returns true if the orchestrator is in parallel mode
func (po *PlannerOrchestrator) IsParallelMode() bool {
	return po.GetExecutionMode() == ParallelExecution
}

// Parallel execution state management methods

// GetParallelSteps returns the current parallel steps
func (po *PlannerOrchestrator) GetParallelSteps() []ParallelStep {
	return po.parallelSteps
}

// SetParallelSteps sets the parallel steps
func (po *PlannerOrchestrator) SetParallelSteps(steps []ParallelStep) {
	po.parallelSteps = make([]ParallelStep, len(steps))
	copy(po.parallelSteps, steps)
}

// GetParallelResults returns the current parallel results
func (po *PlannerOrchestrator) GetParallelResults() []ParallelResult {
	return po.parallelResults
}

// SetParallelResults sets the parallel results
func (po *PlannerOrchestrator) SetParallelResults(results []ParallelResult) {
	po.parallelResults = make([]ParallelResult, len(results))
	copy(po.parallelResults, results)
}

// AddParallelResult adds a parallel result
func (po *PlannerOrchestrator) AddParallelResult(result ParallelResult) {
	po.parallelResults = append(po.parallelResults, result)
}

// ClearParallelSteps clears all parallel steps
func (po *PlannerOrchestrator) ClearParallelSteps() {
	po.parallelSteps = make([]ParallelStep, 0)
}

// ClearParallelResults clears all parallel results
func (po *PlannerOrchestrator) ClearParallelResults() {
	po.parallelResults = make([]ParallelResult, 0)
}
