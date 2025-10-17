package types

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/database"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_execution"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_optimization"

	"github.com/tmc/langchaingo/llms"
)

// WorkflowPhaseOption represents an option for a workflow phase
type WorkflowPhaseOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Group       string `json:"group"` // Group this option belongs to (e.g., "run_management", "execution_strategy")
	Default     bool   `json:"default"`
}

// WorkflowPhase represents a workflow phase
type WorkflowPhase struct {
	ID          string                `json:"id"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Options     []WorkflowPhaseOption `json:"options,omitempty"`
}

// WorkflowStatus represents a workflow status
type WorkflowStatus struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// WorkflowConstants contains all workflow-related constants
type WorkflowConstants struct {
	Phases []WorkflowPhase `json:"phases"`
}

// GetWorkflowConstants returns the current workflow constants
func GetWorkflowConstants() WorkflowConstants {
	return WorkflowConstants{
		Phases: []WorkflowPhase{
			{
				ID:          database.WorkflowStatusPreVerification,
				Title:       "Planning & Todo Creation",
				Description: "Stage 1: Collaborate with the planning agent to create and iterate on a comprehensive todo list using MCP tools. You can refine and improve the todo list through conversation until you're satisfied with the final plan.",
				Options:     []WorkflowPhaseOption{}, // No options for planning phase
			},
			{
				ID:          database.WorkflowStatusPostVerification,
				Title:       "Execution & Review",
				Description: "Stage 2: Execute the approved todo list. The system will create multiple runs organized by date in the runs/ directory. You can review execution results and track progress over time.",
				Options: []WorkflowPhaseOption{
					// Run Management Options
					{
						ID:          "use_same_run",
						Label:       "Use Same Run",
						Description: "Continue using the existing run folder for the current date. This allows you to build upon previous execution results within the same day.",
						Group:       "run_management",
						Default:     false,
					},
					{
						ID:          "create_new_runs_always",
						Label:       "Create New Runs Always",
						Description: "Always create a new run folder for each execution, even on the same date. This provides a clean slate for each execution.",
						Group:       "run_management",
						Default:     true,
					},
					{
						ID:          "create_new_run_once_daily",
						Label:       "Create New Run Once Daily",
						Description: "Create a new run folder only once per day. Subsequent executions on the same date will use the existing run folder.",
						Group:       "run_management",
						Default:     false,
					},
					// Execution Strategy Options
					{
						ID:          SequentialExecution.String(),
						Label:       SequentialExecution.GetLabel(),
						Description: "Execute todos one by one in order, waiting for each to complete before starting the next",
						Group:       "execution_strategy",
						Default:     true,
					},
					{
						ID:          ParallelExecution.String(),
						Label:       ParallelExecution.GetLabel(),
						Description: "Execute multiple todos simultaneously when they don't have dependencies",
						Group:       "execution_strategy",
						Default:     false,
					},
				},
			},
			{
				ID:          database.WorkflowStatusPostVerificationTodoRefinement,
				Title:       "Todo Refinement",
				Description: "Stage 3: Based on execution results from runs/ output, refine and update the original todo list to improve future iterations and incorporate learnings from previous executions.",
				Options:     []WorkflowPhaseOption{}, // No options for refinement phase
			},
		},
	}
}

// GetWorkflowPhaseByID returns a workflow phase by its ID
func GetWorkflowPhaseByID(id string) *WorkflowPhase {
	constants := GetWorkflowConstants()
	for _, phase := range constants.Phases {
		if phase.ID == id {
			return &phase
		}
	}
	return nil
}

// HandleWorkflowConstants returns the current workflow constants via HTTP
func HandleWorkflowConstants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get workflow constants
	workflowConstants := GetWorkflowConstants()

	// Create response
	response := map[string]interface{}{
		"success":   true,
		"constants": workflowConstants,
		"message":   "Workflow constants retrieved successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// WorkflowOrchestrator handles todo-list-based workflow execution
type WorkflowOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Configuration (similar to PlannerOrchestrator)
	provider      string
	model         string
	mcpConfigPath string
	temperature   float64
	agentMode     string
	workspaceRoot string

	// Workspace path extracted from objective
	workspacePath string

	// State management
	state    *WorkflowState
	stateMux sync.RWMutex
	// Step tracking
	currentStep    int
	executionCycle int
	stepMux        sync.RWMutex

	// Agent event bridge for communication between agents
	agentEventBridge EventBridge

	// Context-aware event bridge for adding orchestrator context to events
	contextAwareBridge *ContextAwareEventBridge

	// Custom tools for ReAct agents
	customTools         []llms.Tool
	customToolExecutors map[string]interface{}

	// Logger
	logger utils.ExtendedLogger

	// Tracer for observability
	tracer observability.Tracer

	// Timing
	startTime time.Time

	// Selected servers for execution
	selectedServers []string
}

// WorkflowState represents the state of a workflow execution
type WorkflowState struct {
	// Task information
	Objective        string `json:"objective"`
	CurrentPhase     string `json:"current_phase"` // "planning", "human_verification", "execution", "validation", "workspace_update"
	CurrentTodoIndex int    `json:"current_todo_index"`

	// Step tracking
	CurrentStep    int `json:"current_step"`    // Current step in the execution cycle (0-based)
	ExecutionCycle int `json:"execution_cycle"` // Current execution cycle (1-based)
	TotalSteps     int `json:"total_steps"`     // Total steps in current cycle (4: execution, validation, workspace, conditional)

	// Human verification state
	HumanVerificationStatus       string `json:"human_verification_status"`        // "pending", "approved", "rejected", "revision_required"
	HumanVerificationEventEmitted bool   `json:"human_verification_event_emitted"` // Track if event was emitted
	HumanFeedback                 string `json:"human_feedback,omitempty"`

	// Execution state
	CompletedTodos []string `json:"completed_todos"`
	FailedTodos    []string `json:"failed_todos"`

	// Workspace state
	LastUpdateTime time.Time `json:"last_update_time"`
}

// Human verification types
type HumanVerificationRequest struct {
	Objective        string    `json:"objective"`
	TodoListMarkdown string    `json:"todo_list_markdown"`
	GeneratedAt      time.Time `json:"generated_at"`
	VerificationID   string    `json:"verification_id"`
	Status           string    `json:"status"` // "pending", "approved", "modified", "rejected"
}

type HumanVerificationResponse struct {
	VerificationID           string    `json:"verification_id"`
	Status                   string    `json:"status"` // "approved", "modified", "rejected"
	ModifiedTodoListMarkdown string    `json:"modified_todo_list_markdown,omitempty"`
	Comments                 string    `json:"comments,omitempty"`
	ApprovedAt               time.Time `json:"approved_at"`
}

// LLM verification check
type LLMVerificationCheck struct {
	VerificationFile string    `json:"verification_file"`
	CheckedAt        time.Time `json:"checked_at"`
	IsVerified       bool      `json:"is_verified"`
	Reasoning        string    `json:"reasoning"`
}

// Todo verification response
type TodoVerificationResponse struct {
	VerificationID           string    `json:"verification_id"`
	IsApproved               bool      `json:"is_approved"`
	Reasoning                string    `json:"reasoning"`
	VerifiedAt               time.Time `json:"verified_at"`
	SuggestedModifications   []string  `json:"suggested_modifications,omitempty"`
	ModifiedTodoListMarkdown string    `json:"modified_todo_list_markdown,omitempty"`
}

// NewWorkflowOrchestrator creates a new workflow orchestrator
func NewWorkflowOrchestrator(
	ctx context.Context,
	provider string,
	model string,
	mcpConfigPath string,
	temperature float64,
	agentMode string,
	workspaceRoot string,
	logger utils.ExtendedLogger,
	llm llms.Model,
	eventBridge EventBridge,
	tracer observability.Tracer,
	selectedServers []string,
) (*WorkflowOrchestrator, error) {

	// Create base orchestrator
	config := &agents.OrchestratorAgentConfig{
		Name:          "workflow-orchestrator",
		Type:          "workflow_orchestrator",
		ServerNames:   selectedServers,
		Model:         model,
		Provider:      provider,
		MaxTurns:      100,
		Temperature:   temperature,
		Mode:          agents.AgentMode(agentMode),
		OutputFormat:  agents.OutputFormatStructured,
		MCPConfigPath: mcpConfigPath,
		ToolChoice:    "auto",
		MaxRetries:    3,
		Timeout:       300,
		RateLimit:     60,
	}

	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		config,
		logger,
		tracer,
		eventBridge,
		agents.WorkflowOrchestratorAgentType,
		orchestrator.OrchestratorTypeWorkflow,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	// Create context-aware event bridge
	var contextAwareBridge *ContextAwareEventBridge
	if bridge, ok := eventBridge.(mcpagent.AgentEventListener); ok {
		contextAwareBridge = NewContextAwareEventBridge(bridge, logger)
	}

	// Create workflow orchestrator instance
	wo := &WorkflowOrchestrator{
		BaseOrchestrator:    baseOrchestrator,
		provider:            provider,
		model:               model,
		mcpConfigPath:       mcpConfigPath,
		temperature:         temperature,
		agentMode:           agentMode,
		workspaceRoot:       workspaceRoot,
		agentEventBridge:    eventBridge,
		contextAwareBridge:  contextAwareBridge,
		customTools:         nil, // Will be set via SetCustomTools
		customToolExecutors: make(map[string]interface{}),
		logger:              logger,
		tracer:              tracer,
		startTime:           time.Now(),
		selectedServers:     selectedServers,
		// Initialize step tracking
		currentStep:    0,
		executionCycle: 1,
	}

	return wo, nil
}

// InitializeAgents initializes the workflow orchestrator with provided configurations and custom tools
func (wo *WorkflowOrchestrator) InitializeAgents(ctx context.Context, customTools []llms.Tool, customToolExecutors map[string]interface{}) error {
	wo.logger.Infof("🚀 Initializing WorkflowOrchestrator with %d custom tools", len(customTools))

	// Store custom tools for use during agent creation
	wo.customTools = customTools
	wo.customToolExecutors = customToolExecutors

	wo.logger.Infof("✅ WorkflowOrchestrator initialized successfully with custom tools")
	return nil
}

// ExecuteWorkflow executes a workflow with the given parameters
func (wo *WorkflowOrchestrator) ExecuteWorkflow(
	ctx context.Context,
	workflowID string,
	objective string,
	workflowStatus string,
	selectedOptions *database.WorkflowSelectedOptions,
) (string, error) {
	wo.logger.Infof("🚀 Starting workflow execution for objective: %s (workflowID: %s, workflowStatus: %s)",
		objective, workflowID, workflowStatus)

	// Extract workspace path from objective
	wo.workspacePath = extractWorkspacePathFromObjective(objective)
	if wo.workspacePath == "" {
		return "", fmt.Errorf("workspace path not found in objective")
	}
	wo.logger.Infof("📁 Using workspace path: %s", wo.workspacePath)

	// Initialize workflow state
	wo.state = &WorkflowState{
		CurrentPhase: "planning",
	}

	// Check workflow status and execute appropriate flow
	switch workflowStatus {
	case database.WorkflowStatusPostVerificationTodoRefinement:
		wo.logger.Infof("🔄 Refinement requested - executing standalone refinement")

		// Execute refinement as standalone operation
		refinementResult, err := wo.runRefinement(ctx, objective)
		if err != nil {
			wo.logger.Errorf("❌ Refinement failed: %v", err)
			return "", fmt.Errorf("refinement failed: %w", err)
		}

		wo.logger.Infof("✅ Refinement completed successfully - %d characters", len(refinementResult))

		// Emit human verification request for refinement
		if err := wo.emitRefinementVerificationRequest(ctx, objective, refinementResult); err != nil {
			wo.logger.Warnf("⚠️ Failed to emit refinement verification request: %v", err)
		}

		wo.logger.Infof("✅ Refinement completed - waiting for human verification")
		return refinementResult, nil

	case database.WorkflowStatusPostVerification:
		wo.logger.Infof("✅ Human verification complete - proceeding to execution phase")

		// Proceed directly to execution phase
		return wo.runExecution(ctx, objective, selectedOptions)

	case database.WorkflowStatusPreVerification:
		// Run planning phase (human verification is now handled via events)
		wo.logger.Infof("📝 Running planning phase")

		// Workflow progress events removed as requested

		// Human feedback is now handled through normal chat interaction
		// No special feedback processing needed

		return wo.runPlanning(ctx, objective)

	default:
		wo.logger.Warnf("⚠️ Unknown workflow status: %s, defaulting to planning phase", workflowStatus)
		return wo.runPlanning(ctx, objective)
	}
}

func (wo *WorkflowOrchestrator) runPlanning(ctx context.Context, objective string) (string, error) {
	wo.logger.Infof("📝 Running planning phase for objective: %s", objective)

	// Create todo planner agent
	todoPlannerAgent, err := wo.createTodoPlannerAgent()
	if err != nil {
		return "", fmt.Errorf("failed to create todo planner agent: %w", err)
	}

	// Generate todo list
	todoListMarkdown, err := todoPlannerAgent.CreateTodoList(ctx, objective, wo.workspacePath)

	if err != nil {
		return "", fmt.Errorf("failed to create/update todo list: %w", err)
	}

	wo.logger.Infof("✅ Planning completed - todo list generated with %d characters", len(todoListMarkdown))

	// Note: Todo list is already saved by the todo planner agent using workspace tools
	// No need for additional save operation here

	// Emit request_human_feedback event
	if err := wo.emitRequestHumanFeedback(ctx, objective, todoListMarkdown,
		"planning_verification",
		database.WorkflowStatusPostVerification,
		"Todo List Planning Complete",
		"Approve Plan & Continue",
		"Please review the generated todo list and approve to proceed with execution."); err != nil {
		wo.logger.Warnf("⚠️ Failed to emit request human feedback event: %v", err)
	}

	// Increment execution cycle after planning phase completes
	wo.executionCycle++
	wo.logger.Infof("🔄 Planning phase completed - incremented execution cycle to %d", wo.executionCycle)

	return fmt.Sprintf("Planning completed. Todo list generated with %d characters. Ready for human verification.", len(todoListMarkdown)), nil
}

// runExecution runs the execution phase of the workflow
func (wo *WorkflowOrchestrator) runExecution(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.logger.Infof("🚀 Running execution phase for objective: %s", objective)

	// Create TodoExecutionOrchestrator
	todoExecutionOrchestrator, err := wo.createTodoExecutionOrchestrator()
	if err != nil {
		return "", fmt.Errorf("failed to create execution orchestrator: %w", err)
	}

	// Get run option
	runOption := wo.getRunOption(selectedOptions)
	wo.logger.Infof("🚀 Executing todos with run option: %s", runOption)

	// Delegate to TodoExecutionOrchestrator
	executionResult, err := todoExecutionOrchestrator.ExecuteTodos(ctx, objective, wo.workspacePath, runOption)
	if err != nil {
		return "", fmt.Errorf("execution orchestrator failed: %w", err)
	}

	wo.logger.Infof("✅ Execution orchestrator completed: %d characters of results", len(executionResult))

	// Emit request_human_feedback event for execution completion
	if err := wo.emitRequestHumanFeedback(ctx, objective, executionResult,
		"execution_verification",
		database.WorkflowStatusPostVerificationTodoRefinement,
		"Execution Phase Complete",
		"Review Results & Continue",
		"Please review the execution results and choose to refine the plan if needed."); err != nil {
		wo.logger.Warnf("⚠️ Failed to emit request human feedback event: %v", err)
	}

	return executionResult, nil
}

// runRefinement handles refinement requests for the workflow with iterative improvement loop
func (wo *WorkflowOrchestrator) runRefinement(ctx context.Context, objective string) (string, error) {
	wo.logger.Infof("🔄 Executing iterative refinement for objective: %s", objective)

	// Create TodoOptimizationOrchestrator
	todoOptimizationOrchestrator, err := wo.createTodoOptimizationOrchestrator()
	if err != nil {
		return "", fmt.Errorf("failed to create optimization orchestrator: %w", err)
	}

	// Delegate to TodoOptimizationOrchestrator
	refinementResult, err := todoOptimizationOrchestrator.ExecuteRefinement(ctx, objective, wo.workspacePath)
	if err != nil {
		return "", fmt.Errorf("optimization orchestrator failed: %w", err)
	}

	wo.logger.Infof("✅ Refinement completed successfully - %d characters", len(refinementResult))

	// Emit request_human_feedback event for refinement completion
	if err := wo.emitRequestHumanFeedback(ctx, objective, refinementResult,
		"refinement_verification",
		database.WorkflowStatusPostVerification,
		"Todo List Refinement Complete",
		"Approve Refined Plan & Continue",
		"Please review the refined todo list and approve to proceed with execution."); err != nil {
		wo.logger.Warnf("⚠️ Failed to emit request human feedback event: %v", err)
	}

	return refinementResult, nil
}

// Helper methods for workflow operations
// getSessionID returns the session ID for this workflow
func (wo *WorkflowOrchestrator) getSessionID() string {
	// This should be passed from the server or generated
	// For now, return a placeholder
	return "workflow-session-" + fmt.Sprintf("%d", time.Now().Unix())
}

// getWorkflowID returns the workflow ID for this workflow
func (wo *WorkflowOrchestrator) getWorkflowID() string {
	// This should be generated when the workflow starts
	// For now, return a placeholder
	return "workflow-" + fmt.Sprintf("%d", time.Now().Unix())
}

// setupAgent performs common agent setup tasks using shared utilities
func (wo *WorkflowOrchestrator) setupAgent(agent agents.OrchestratorAgent, agentType, agentName string) error {
	// Create orchestrator config
	config := &OrchestratorConfig{
		Provider:        wo.provider,
		Model:           wo.model,
		MCPConfigPath:   wo.mcpConfigPath,
		Temperature:     wo.temperature,
		SelectedServers: wo.selectedServers,
		AgentMode:       wo.agentMode,
		Logger:          wo.logger,
	}

	utils := newOrchestratorUtils(config)

	// Use shared setup function
	return utils.setupAgent(
		agent,
		agentType,
		agentName,
		wo.customTools, // ✅ FIXED: Pass workspace tools to sub-agents
		wo.customToolExecutors,
		wo.contextAwareBridge,
		wo.setWorkflowContext, // Context setting function
	)
}

// createAgentConfig creates a generic agent configuration using shared utilities
func (wo *WorkflowOrchestrator) createAgentConfig(agentType, agentName string, maxTurns int) *agents.OrchestratorAgentConfig {
	config := &OrchestratorConfig{
		Provider:        wo.provider,
		Model:           wo.model,
		MCPConfigPath:   wo.mcpConfigPath,
		Temperature:     wo.temperature,
		SelectedServers: wo.selectedServers,
		AgentMode:       wo.agentMode,
		Logger:          wo.logger,
	}

	utils := newOrchestratorUtils(config)

	setupConfig := &AgentSetupConfig{
		AgentType:    agentType,
		AgentName:    agentName,
		MaxTurns:     maxTurns,
		AgentMode:    wo.agentMode,
		OutputFormat: agents.OutputFormatStructured,
	}

	return utils.createAgentConfig(setupConfig)
}

// createTodoPlannerAgent creates a new todo planner orchestrator
func (wo *WorkflowOrchestrator) createTodoPlannerAgent() (*todo_creation.TodoPlannerOrchestrator, error) {
	config := wo.createAgentConfig("todo_planner", "workflow-todo-planner", 100)
	agent, err := todo_creation.NewTodoPlannerOrchestrator(config, wo.logger, wo.tracer, wo.agentEventBridge)
	if err != nil {
		return nil, fmt.Errorf("todo planner orchestrator creation failed: %w", err)
	}
	if err := wo.setupAgent(agent, "todo_planner", "todo planner orchestrator"); err != nil {
		return nil, err
	}

	// Set workspace tools for the TodoPlannerOrchestrator so it can pass them to sub-agents
	if wo.customTools != nil && wo.customToolExecutors != nil {
		agent.SetWorkspaceTools(wo.customTools, wo.customToolExecutors)
	}

	return agent, nil
}

// createTodoExecutionOrchestrator creates and configures the TodoExecutionOrchestrator
func (wo *WorkflowOrchestrator) createTodoExecutionOrchestrator() (*todo_execution.TodoExecutionOrchestrator, error) {
	config := wo.createAgentConfig("todo_execution", "workflow-todo-execution", 100)
	agent, err := todo_execution.NewTodoExecutionOrchestrator(config, wo.logger, wo.tracer, wo.agentEventBridge)
	if err != nil {
		return nil, fmt.Errorf("failed to create todo execution orchestrator: %w", err)
	}

	// Initialize the agent
	if err := agent.Initialize(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize todo execution orchestrator: %w", err)
	}

	// Set workspace tools if available
	if wo.customTools != nil && wo.customToolExecutors != nil {
		agent.SetWorkspaceTools(wo.customTools, wo.customToolExecutors)
	}

	return agent, nil
}

// getRunOption extracts the run option from selected options
func (wo *WorkflowOrchestrator) getRunOption(selectedOptions *database.WorkflowSelectedOptions) string {
	runOption := "create_new_runs_always" // default
	if selectedOptions != nil && selectedOptions.PhaseID == database.WorkflowStatusPostVerification {
		for _, selection := range selectedOptions.Selections {
			if selection.Group == "run_management" {
				runOption = selection.OptionID
				wo.logger.Infof("✅ Using selected run option: %s", runOption)
				break
			}
		}
	} else {
		wo.logger.Infof("⚠️ Using default run option: %s", runOption)
	}
	return runOption
}

// createTodoOptimizationOrchestrator creates and configures the TodoOptimizationOrchestrator
func (wo *WorkflowOrchestrator) createTodoOptimizationOrchestrator() (*todo_optimization.TodoOptimizationOrchestrator, error) {
	config := wo.createAgentConfig("todo_optimization", "workflow-todo-optimization", 100)
	agent, err := todo_optimization.NewTodoOptimizationOrchestrator(config, wo.logger, wo.tracer, wo.agentEventBridge)
	if err != nil {
		return nil, fmt.Errorf("failed to create todo optimization orchestrator: %w", err)
	}

	// Initialize the agent
	if err := agent.Initialize(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize todo optimization orchestrator: %w", err)
	}

	// Set workspace tools if available
	if wo.customTools != nil && wo.customToolExecutors != nil {
		agent.SetWorkspaceTools(wo.customTools, wo.customToolExecutors)
	}

	return agent, nil
}

// setWorkflowContext sets the orchestrator context for workflow agents
func (wo *WorkflowOrchestrator) setWorkflowContext(phase string, step int, agentName string) {
	if wo.contextAwareBridge == nil {
		wo.logger.Warnf("⚠️ Context-aware bridge is nil, cannot set context for %s", agentName)
		return
	}

	// Set orchestrator context with workflow-specific phase
	// Use execution cycle directly (1-based) - frontend will display iteration + 1
	wo.contextAwareBridge.SetOrchestratorContext(phase, step, wo.executionCycle, agentName)
	wo.logger.Infof("🎯 Set workflow context: %s (step %d, cycle %d) for %s", phase, step+1, wo.executionCycle, agentName)
}

// GetObjective returns the current workflow objective
func (wo *WorkflowOrchestrator) GetObjective() string {
	// For now, we'll need to read the objective from the human verification file
	// This is a simplified implementation - in a real scenario, we'd read from the file
	// Since the objective is passed to ExecuteWorkflow, we can store it in the orchestrator
	// For now, return empty string and let the caller handle it
	return ""
}

// ============================================================================

// emitWorkflowProgress function removed as requested

// emitRequestHumanFeedback emits a request human feedback event
func (wo *WorkflowOrchestrator) emitRequestHumanFeedback(ctx context.Context, objective string, todoListMarkdown string, verificationType string, nextPhase string, title string, actionLabel string, actionDescription string) error {
	wo.logger.Infof("📤 Emitting request human feedback event")

	// Generate unique request ID
	requestID := fmt.Sprintf("feedback_%d", time.Now().UnixNano())

	// Create request human feedback event data
	eventData := &events.RequestHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:         objective,
		TodoListMarkdown:  todoListMarkdown,
		SessionID:         wo.getSessionID(),
		WorkflowID:        wo.getWorkflowID(),
		RequestID:         requestID,
		VerificationType:  verificationType,
		NextPhase:         nextPhase,
		Title:             title,
		ActionLabel:       actionLabel,
		ActionDescription: actionDescription,
	}

	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      events.RequestHumanFeedback,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through event bridge if available
	if wo.agentEventBridge != nil {
		if bridge, ok := wo.agentEventBridge.(interface {
			HandleEvent(context.Context, *events.AgentEvent) error
		}); ok {
			return bridge.HandleEvent(ctx, agentEvent)
		}
	}

	return nil
}

// emitRefinementVerificationRequest emits a human verification request for refinement
func (wo *WorkflowOrchestrator) emitRefinementVerificationRequest(ctx context.Context, objective, refinementResult string) error {
	wo.logger.Infof("📤 Emitting refinement verification request")

	// Create request human feedback event data
	eventData := &events.RequestHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:         objective,
		TodoListMarkdown:  refinementResult,
		SessionID:         wo.getSessionID(),
		WorkflowID:        wo.getWorkflowID(),
		RequestID:         fmt.Sprintf("refinement_feedback_%d", time.Now().UnixNano()),
		VerificationType:  "refinement_verification",
		NextPhase:         "post-verification",
		Title:             "Refined Todo List Verification Required",
		ActionLabel:       "Accept Refined Plan & Continue",
		ActionDescription: "The todo list has been refined based on execution results. Please review and approve to proceed with execution.",
	}

	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      events.RequestHumanFeedback,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through event bridge if available
	if wo.agentEventBridge != nil {
		if bridge, ok := wo.agentEventBridge.(interface {
			HandleEvent(context.Context, *events.AgentEvent) error
		}); ok {
			return bridge.HandleEvent(ctx, agentEvent)
		}
	}

	return nil
}

// GetState returns the current state of the workflow orchestrator
func (wo *WorkflowOrchestrator) GetState() (*WorkflowState, error) {
	wo.stateMux.RLock()
	defer wo.stateMux.RUnlock()

	if wo.state == nil {
		return nil, fmt.Errorf("workflow state not initialized")
	}

	// Create a deep copy to avoid race conditions
	stateCopy := &WorkflowState{
		Objective:                     wo.state.Objective,
		CurrentPhase:                  wo.state.CurrentPhase,
		CurrentTodoIndex:              wo.state.CurrentTodoIndex,
		CurrentStep:                   wo.state.CurrentStep,
		ExecutionCycle:                wo.state.ExecutionCycle,
		TotalSteps:                    wo.state.TotalSteps,
		HumanVerificationStatus:       wo.state.HumanVerificationStatus,
		HumanVerificationEventEmitted: wo.state.HumanVerificationEventEmitted,
		HumanFeedback:                 wo.state.HumanFeedback,
		CompletedTodos:                make([]string, len(wo.state.CompletedTodos)),
		FailedTodos:                   make([]string, len(wo.state.FailedTodos)),
		LastUpdateTime:                wo.state.LastUpdateTime,
	}

	// Copy slices
	copy(stateCopy.CompletedTodos, wo.state.CompletedTodos)
	copy(stateCopy.FailedTodos, wo.state.FailedTodos)

	return stateCopy, nil
}

// RestoreState restores the workflow orchestrator to a previous state
func (wo *WorkflowOrchestrator) RestoreState(state *WorkflowState) error {
	if state == nil {
		return fmt.Errorf("cannot restore nil state")
	}

	wo.stateMux.Lock()
	defer wo.stateMux.Unlock()

	// Validate state
	if err := wo.validateWorkflowState(state); err != nil {
		return fmt.Errorf("invalid workflow state: %w", err)
	}

	// Restore state
	wo.state = state

	// Update step tracking to match restored state
	wo.stepMux.Lock()
	wo.currentStep = state.CurrentStep
	wo.executionCycle = state.ExecutionCycle
	wo.stepMux.Unlock()

	wo.logger.Infof("🔄 Restored workflow state: phase %s, todo index %d, step %d, cycle %d",
		state.CurrentPhase, state.CurrentTodoIndex, state.CurrentStep, state.ExecutionCycle)

	return nil
}

// validateWorkflowState validates the workflow state before restoration
func (wo *WorkflowOrchestrator) validateWorkflowState(state *WorkflowState) error {
	if state.CurrentPhase == "" {
		return fmt.Errorf("current phase cannot be empty")
	}
	if state.CurrentTodoIndex < 0 {
		return fmt.Errorf("current todo index cannot be negative")
	}
	if state.CurrentStep < 0 {
		return fmt.Errorf("current step cannot be negative")
	}
	if state.ExecutionCycle < 0 {
		return fmt.Errorf("execution cycle cannot be negative")
	}
	return nil
}

// extractWorkspacePathFromObjective extracts the workspace path from the objective string
func extractWorkspacePathFromObjective(objective string) string {
	// Look for pattern: "📁 Files in context: Workflow/[FolderName]"
	// This is the standard pattern used by workflow orchestrator
	prefix := "📁 Files in context: "
	if idx := strings.Index(objective, prefix); idx != -1 {
		// Find the start of the workspace path
		start := idx + len(prefix)
		// Find the end of the workspace path (typically before a newline or end of string)
		end := strings.Index(objective[start:], "\n")
		if end == -1 {
			// No newline found, use the rest of the string
			return objective[start:]
		}
		return objective[start : start+end]
	}
	return ""
}
