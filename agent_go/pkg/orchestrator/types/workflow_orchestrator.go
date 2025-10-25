package types

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/database"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human"
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
				Options: []WorkflowPhaseOption{
					{
						ID:          "human_controlled",
						Label:       "Human Controlled (single execution, fast)",
						Description: "Simplified approach with single execution, no validation, no critique, no cleanup, focused on fastest plan creation",
						Group:       "planning_strategy",
						Default:     true, // Make this default
					},
					{
						ID:          "auto_model",
						Label:       "Auto Model (10 iterations with validation)",
						Description: "Full automated model with 10 iterations, validation agent, and adaptive strategy system",
						Group:       "planning_strategy",
						Default:     false,
					},
				},
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
	provider string,
	model string,
	mcpConfigPath string,
	temperature float64,
	agentMode string,
	logger utils.ExtendedLogger,
	eventBridge mcpagent.AgentEventListener,
	tracer observability.Tracer,
	selectedServers []string,
	selectedTools []string, // NEW parameter
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
) (*WorkflowOrchestrator, error) {

	// Create base orchestrator
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
		selectedTools, // NEW: Pass through
		llmConfig,     // LLM configuration
		maxTurns,
		customTools,
		customToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	// Create workflow orchestrator instance
	wo := &WorkflowOrchestrator{
		BaseOrchestrator: baseOrchestrator,
	}

	return wo, nil
}

// executeFlow executes a workflow with the given parameters
func (wo *WorkflowOrchestrator) executeFlow(
	ctx context.Context,
	objective string,
	workspacePath string,
	workflowStatus string,
	selectedOptions *database.WorkflowSelectedOptions,
) (string, error) {
	// Set workspace path from parameter
	wo.SetWorkspacePath(workspacePath)
	if wo.GetWorkspacePath() == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Check workflow status and execute appropriate flow
	switch workflowStatus {
	case database.WorkflowStatusPostVerificationTodoRefinement:
		// Execute refinement as standalone operation
		refinementResult, err := wo.runRefinement(ctx, objective)
		if err != nil {
			wo.GetLogger().Errorf("❌ Refinement failed: %v", err)
			return "", fmt.Errorf("refinement failed: %w", err)
		}

		// Emit human verification request for refinement
		if err := wo.emitRefinementVerificationRequest(ctx, objective, refinementResult); err != nil {
			wo.GetLogger().Warnf("⚠️ Failed to emit refinement verification request: %v", err)
		}

		// Emit orchestrator completion events
		wo.EmitOrchestratorEnd(ctx, objective, refinementResult, "completed", "", "workflow_execution")
		wo.EmitUnifiedCompletionEvent(ctx, "workflow", "workflow", objective, refinementResult, "completed", 1)

		return refinementResult, nil

	case database.WorkflowStatusPostVerification:
		// Proceed directly to execution phase
		return wo.runExecution(ctx, objective, selectedOptions)

	case database.WorkflowStatusPreVerification:
		// Run planning phase
		return wo.runPlanning(ctx, objective, selectedOptions)

	default:
		wo.GetLogger().Warnf("⚠️ Unknown workflow status: %s, defaulting to planning phase", workflowStatus)
		return wo.runPlanning(ctx, objective, selectedOptions)
	}
}

func (wo *WorkflowOrchestrator) runPlanning(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	// Get planning strategy from selected options (if available)
	planningStrategy := wo.getPlanningStrategy(selectedOptions)
	wo.GetLogger().Infof("🎯 Planning strategy: %s", planningStrategy)

	// Call different orchestrators based on strategy
	if planningStrategy == "auto_model" {
		wo.GetLogger().Infof("🤖 Starting Auto Model Planning")
		return wo.runAutoModelPlanning(ctx, objective)
	} else {
		wo.GetLogger().Infof("👤 Starting Human Controlled Planning")
		return wo.runHumanControlledPlanning(ctx, objective)
	}
}

// runAutoModelPlanning runs the auto model planning with full validation and critique
func (wo *WorkflowOrchestrator) runAutoModelPlanning(ctx context.Context, objective string) (string, error) {
	wo.GetLogger().Infof("🤖 Running Auto Model Planning for objective: %s", objective)

	// Create auto model planner orchestrator directly
	llmConfig := wo.GetLLMConfig()
	todoPlannerAgent, err := todo_creation.NewTodoPlannerOrchestrator(
		wo.GetProvider(),
		wo.GetModel(),
		wo.GetTemperature(),
		wo.GetAgentMode(),
		wo.GetSelectedServers(),
		wo.GetSelectedTools(), // NEW: Pass selected tools
		wo.GetMCPConfigPath(),
		llmConfig,
		wo.GetMaxTurns(),
		wo.GetLogger(),
		wo.GetTracer(),
		wo.GetContextAwareBridge(),
		wo.WorkspaceTools,
		wo.WorkspaceToolExecutors,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create auto model planner orchestrator: %w", err)
	}

	// Generate todo list using Execute method
	todoListMarkdown, err := todoPlannerAgent.Execute(ctx, objective, wo.GetWorkspacePath(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create/update todo list: %w", err)
	}

	// Emit request_human_feedback event
	if err := wo.emitRequestHumanFeedback(ctx, objective, todoListMarkdown,
		"planning_verification",
		database.WorkflowStatusPostVerification,
		"Auto Model Planning Complete",
		"Approve Plan & Continue",
		"Please review the generated todo list and approve to proceed with execution."); err != nil {
		wo.GetLogger().Warnf("⚠️ Failed to emit request human feedback event: %v", err)
	}

	planningResult := fmt.Sprintf("Auto model planning completed. Todo list generated with %d characters. Ready for human verification.", len(todoListMarkdown))

	// Emit orchestrator completion events
	wo.EmitOrchestratorEnd(ctx, objective, planningResult, "completed", "", "workflow_execution")
	wo.EmitUnifiedCompletionEvent(ctx, "workflow", "workflow", objective, planningResult, "completed", 1)

	return planningResult, nil
}

// runHumanControlledPlanning runs the human controlled planning with simplified approach
func (wo *WorkflowOrchestrator) runHumanControlledPlanning(ctx context.Context, objective string) (string, error) {
	wo.GetLogger().Infof("👤 Running Human Controlled Planning for objective: %s", objective)

	// Create human controlled planner orchestrator directly
	llmConfig := wo.GetLLMConfig()
	todoPlannerAgent, err := todo_creation_human.NewHumanControlledTodoPlannerOrchestrator(
		wo.GetProvider(),
		wo.GetModel(),
		wo.GetTemperature(),
		wo.GetAgentMode(),
		wo.GetSelectedServers(),
		wo.GetSelectedTools(), // NEW: Pass selected tools
		wo.GetMCPConfigPath(),
		llmConfig,
		wo.GetMaxTurns(),
		wo.GetLogger(),
		wo.GetTracer(),
		wo.GetContextAwareBridge(),
		wo.WorkspaceTools,
		wo.WorkspaceToolExecutors,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Generate todo list using Execute method
	todoListMarkdown, err := todoPlannerAgent.Execute(ctx, objective, wo.GetWorkspacePath(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create/update todo list: %w", err)
	}

	// Emit request_human_feedback event
	if err := wo.emitRequestHumanFeedback(ctx, objective, todoListMarkdown,
		"planning_verification",
		database.WorkflowStatusPostVerification,
		"Human Controlled Planning Complete",
		"Approve Plan & Continue",
		"Please review the generated todo list and approve to proceed with execution."); err != nil {
		wo.GetLogger().Warnf("⚠️ Failed to emit request human feedback event: %v", err)
	}

	planningResult := fmt.Sprintf("Human controlled planning completed. Todo list generated with %d characters. Ready for human verification.", len(todoListMarkdown))

	// Emit orchestrator completion events
	wo.EmitOrchestratorEnd(ctx, objective, planningResult, "completed", "", "workflow_execution")
	wo.EmitUnifiedCompletionEvent(ctx, "workflow", "workflow", objective, planningResult, "completed", 1)

	return planningResult, nil
}

// runExecution runs the execution phase of the workflow
func (wo *WorkflowOrchestrator) runExecution(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	// Create TodoExecutionOrchestrator
	todoExecutionOrchestrator, err := wo.createTodoExecutionOrchestrator()
	if err != nil {
		return "", fmt.Errorf("failed to create execution orchestrator: %w", err)
	}

	// Get run option
	runOption := wo.getRunOption(selectedOptions)

	// Delegate to TodoExecutionOrchestrator using Execute method
	executionOptions := map[string]interface{}{
		"runOption": runOption,
	}
	executionResult, err := todoExecutionOrchestrator.Execute(ctx, objective, wo.GetWorkspacePath(), executionOptions)
	if err != nil {
		return "", fmt.Errorf("execution orchestrator failed: %w", err)
	}

	// Emit request_human_feedback event for execution completion
	if err := wo.emitRequestHumanFeedback(ctx, objective, executionResult,
		"execution_verification",
		database.WorkflowStatusPostVerificationTodoRefinement,
		"Execution Phase Complete",
		"Review Results & Continue",
		"Please review the execution results and choose to refine the plan if needed."); err != nil {
		wo.GetLogger().Warnf("⚠️ Failed to emit request human feedback event: %v", err)
	}

	// Emit orchestrator completion events
	wo.EmitOrchestratorEnd(ctx, objective, executionResult, "completed", "", "workflow_execution")
	wo.EmitUnifiedCompletionEvent(ctx, "workflow", "workflow", objective, executionResult, "completed", 1)

	return executionResult, nil
}

// runRefinement handles refinement requests for the workflow with iterative improvement loop
func (wo *WorkflowOrchestrator) runRefinement(ctx context.Context, objective string) (string, error) {
	// Create TodoOptimizationOrchestrator
	todoOptimizationOrchestrator, err := wo.createTodoOptimizationOrchestrator()
	if err != nil {
		return "", fmt.Errorf("failed to create optimization orchestrator: %w", err)
	}

	// Delegate to TodoOptimizationOrchestrator using Execute method
	refinementResult, err := todoOptimizationOrchestrator.Execute(ctx, objective, wo.GetWorkspacePath(), nil)
	if err != nil {
		return "", fmt.Errorf("optimization orchestrator failed: %w", err)
	}

	// Emit request_human_feedback event for refinement completion
	if err := wo.emitRequestHumanFeedback(ctx, objective, refinementResult,
		"refinement_verification",
		database.WorkflowStatusPostVerification,
		"Todo List Refinement Complete",
		"Approve Refined Plan & Continue",
		"Please review the refined todo list and approve to proceed with execution."); err != nil {
		wo.GetLogger().Warnf("⚠️ Failed to emit request human feedback event: %v", err)
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

// createTodoExecutionOrchestrator creates and configures the TodoExecutionOrchestrator
func (wo *WorkflowOrchestrator) createTodoExecutionOrchestrator() (orchestrator.Orchestrator, error) {
	llmConfig := wo.GetLLMConfig()
	agent, err := todo_execution.NewTodoExecutionOrchestrator(wo.GetProvider(), wo.GetModel(), wo.GetTemperature(), wo.GetAgentMode(), wo.GetSelectedServers(), wo.GetSelectedTools(), wo.GetMCPConfigPath(), llmConfig, wo.GetMaxTurns(), wo.GetLogger(), wo.GetTracer(), wo.GetContextAwareBridge(), wo.WorkspaceTools, wo.WorkspaceToolExecutors)
	if err != nil {
		return nil, fmt.Errorf("failed to create todo execution orchestrator: %w", err)
	}

	// Set workspace tools if available
	// Note: WorkspaceTools and WorkspaceToolExecutors are already available from BaseOrchestrator

	return agent, nil
}

// getRunOption extracts the run option from selected options
func (wo *WorkflowOrchestrator) getRunOption(selectedOptions *database.WorkflowSelectedOptions) string {
	runOption := "create_new_runs_always" // default
	if selectedOptions != nil && selectedOptions.PhaseID == database.WorkflowStatusPostVerification {
		for _, selection := range selectedOptions.Selections {
			if selection.Group == "run_management" {
				runOption = selection.OptionID
				break
			}
		}
	}
	return runOption
}

// getPlanningStrategy returns the planning strategy to use based on selected options
func (wo *WorkflowOrchestrator) getPlanningStrategy(selectedOptions *database.WorkflowSelectedOptions) string {
	// Default to human_controlled
	strategy := "human_controlled"

	// Debug logging
	if selectedOptions == nil {
		wo.GetLogger().Infof("🔍 DEBUG: selectedOptions is nil, using default strategy: %s", strategy)
	} else {
		wo.GetLogger().Infof("🔍 DEBUG: selectedOptions.PhaseID: %s", selectedOptions.PhaseID)
		wo.GetLogger().Infof("🔍 DEBUG: selectedOptions.Selections count: %d", len(selectedOptions.Selections))

		for i, selection := range selectedOptions.Selections {
			wo.GetLogger().Infof("🔍 DEBUG: Selection[%d] - Group: %s, OptionID: %s", i, selection.Group, selection.OptionID)
		}
	}

	// Check if selected options are provided and contain planning strategy selection
	if selectedOptions != nil && selectedOptions.PhaseID == database.WorkflowStatusPreVerification {
		for _, selection := range selectedOptions.Selections {
			if selection.Group == "planning_strategy" {
				strategy = selection.OptionID
				wo.GetLogger().Infof("🔍 DEBUG: Found planning_strategy selection: %s", strategy)
				break
			}
		}
	}

	wo.GetLogger().Infof("🎯 Using planning strategy: %s", strategy)
	return strategy
}

// createTodoOptimizationOrchestrator creates and configures the TodoOptimizationOrchestrator
func (wo *WorkflowOrchestrator) createTodoOptimizationOrchestrator() (orchestrator.Orchestrator, error) {
	llmConfig := wo.GetLLMConfig()
	agent, err := todo_optimization.NewTodoOptimizationOrchestrator(wo.GetProvider(), wo.GetModel(), wo.GetTemperature(), wo.GetAgentMode(), wo.GetSelectedServers(), wo.GetSelectedTools(), wo.GetMCPConfigPath(), llmConfig, wo.GetMaxTurns(), wo.GetLogger(), wo.GetTracer(), wo.GetContextAwareBridge(), wo.WorkspaceTools, wo.WorkspaceToolExecutors)
	if err != nil {
		return nil, fmt.Errorf("failed to create todo optimization orchestrator: %w", err)
	}

	// Set workspace tools if available
	// Note: WorkspaceTools and WorkspaceToolExecutors are already available from BaseOrchestrator

	return agent, nil
}

// emitWorkflowProgress function removed as requested

// emitRequestHumanFeedback emits a request human feedback event
func (wo *WorkflowOrchestrator) emitRequestHumanFeedback(ctx context.Context, objective string, todoListMarkdown string, verificationType string, nextPhase string, title string, actionLabel string, actionDescription string) error {

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
	if wo.GetContextAwareBridge() != nil {
		if bridge, ok := wo.GetContextAwareBridge().(interface {
			HandleEvent(context.Context, *events.AgentEvent) error
		}); ok {
			return bridge.HandleEvent(ctx, agentEvent)
		}
	}

	return nil
}

// emitRefinementVerificationRequest emits a human verification request for refinement
func (wo *WorkflowOrchestrator) emitRefinementVerificationRequest(ctx context.Context, objective, refinementResult string) error {

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
	if wo.GetContextAwareBridge() != nil {
		if bridge, ok := wo.GetContextAwareBridge().(interface {
			HandleEvent(context.Context, *events.AgentEvent) error
		}); ok {
			return bridge.HandleEvent(ctx, agentEvent)
		}
	}

	return nil
}

// Execute implements the Orchestrator interface
func (wo *WorkflowOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION START - Execute method called")
	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - objective: %s", objective)
	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - workspacePath: %s", workspacePath)
	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - options: %+v", options)

	// Validate options if provided
	if options != nil {
		wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - options is not nil, validating...")

		// Validate workflowStatus if provided
		if workflowStatusVal, exists := options["workflowStatus"]; exists {
			wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - workflowStatus found: %+v (type: %T)", workflowStatusVal, workflowStatusVal)
			if workflowStatus, ok := workflowStatusVal.(string); !ok {
				return "", fmt.Errorf("invalid workflowStatus: expected string, got %T", workflowStatusVal)
			} else if workflowStatus == "" {
				return "", fmt.Errorf("invalid workflowStatus: cannot be empty string")
			} else {
				// Validate it's a known workflow status
				validStatuses := []string{
					database.WorkflowStatusPreVerification,
					database.WorkflowStatusPostVerification,
					database.WorkflowStatusPostVerificationTodoRefinement,
				}
				valid := false
				for _, status := range validStatuses {
					if workflowStatus == status {
						valid = true
						break
					}
				}
				if !valid {
					return "", fmt.Errorf("invalid workflowStatus: %s, valid statuses: %v", workflowStatus, validStatuses)
				}
			}
		} else {
			wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - workflowStatus not found in options")
		}

		// Validate selectedOptions if provided
		if selectedOptsVal, exists := options["selectedOptions"]; exists {
			wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions found: %+v (type: %T)", selectedOptsVal, selectedOptsVal)
			if selectedOptsVal != nil {
				if _, ok := selectedOptsVal.(*database.WorkflowSelectedOptions); !ok {
					return "", fmt.Errorf("invalid selectedOptions: expected *database.WorkflowSelectedOptions, got %T", selectedOptsVal)
				}
			}
		} else {
			wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions not found in options")
		}
	} else {
		wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - options is nil")
	}

	// Extract options from the map with defaults
	var workflowStatus string
	if ws, ok := options["workflowStatus"].(string); ok && ws != "" {
		workflowStatus = ws
		wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - extracted workflowStatus: %s", workflowStatus)
	} else {
		workflowStatus = database.WorkflowStatusPreVerification // Default to planning phase
		wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - using default workflowStatus: %s", workflowStatus)
	}

	var selectedOptions *database.WorkflowSelectedOptions
	if opts, ok := options["selectedOptions"]; ok && opts != nil {
		if so, ok := opts.(*database.WorkflowSelectedOptions); ok {
			selectedOptions = so
			wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - extracted selectedOptions: %+v", selectedOptions)
			if selectedOptions != nil {
				wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions.PhaseID: %s", selectedOptions.PhaseID)
				wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions.Selections count: %d", len(selectedOptions.Selections))
			}
		}
	} else {
		wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - no selectedOptions extracted")
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Validate objective
	if objective == "" {
		return "", fmt.Errorf("objective cannot be empty")
	}

	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - About to call executeFlow with workflowStatus: %s", workflowStatus)
	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions for executeFlow: %+v", selectedOptions)

	// Call the existing executeFlow method with the extracted parameters
	result, err := wo.executeFlow(ctx, objective, workspacePath, workflowStatus, selectedOptions)
	if err != nil {
		wo.GetLogger().Errorf("🚀 WORKFLOW EXECUTION ERROR - executeFlow failed: %v", err)
		return "", err
	}

	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION SUCCESS - executeFlow completed successfully")
	return result, nil
}
