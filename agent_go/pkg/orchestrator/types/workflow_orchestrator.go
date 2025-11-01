package types

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/database"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_execution"
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
	customTools []llmtypes.Tool,
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
	wo.GetLogger().Infof("👤 Starting Planning Phase")
	return wo.runHumanControlledPlanning(ctx, objective)
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
		wo.GetLogger().Warnf("⚠️ Failed to emit request human feedback event: %w", err)
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

	// Execution is complete - no refinement needed
	wo.GetLogger().Infof("✅ Execution phase completed successfully")

	// Emit orchestrator completion events
	wo.EmitOrchestratorEnd(ctx, objective, executionResult, "completed", "", "workflow_execution")
	wo.EmitUnifiedCompletionEvent(ctx, "workflow", "workflow", objective, executionResult, "completed", 1)

	return executionResult, nil
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
		wo.GetLogger().Errorf("🚀 WORKFLOW EXECUTION ERROR - executeFlow failed: %w", err)
		return "", err
	}

	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION SUCCESS - executeFlow completed successfully")
	return result, nil
}
