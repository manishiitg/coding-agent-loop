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
				Description: "Collaborate with the planning agent to create and iterate on a comprehensive todo list using MCP tools. You can refine and improve the todo list through conversation until you're satisfied with the final plan. Execution happens automatically after plan approval.",
				Options:     []WorkflowPhaseOption{}, // No options for planning phase
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

	// All workflow statuses now go through planning phase (which includes execution)
	// Execution is handled automatically within the planning phase after plan approval
	return wo.runPlanning(ctx, objective, selectedOptions)
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
	// Note: Execution now happens automatically after plan approval, so no separate phase needed
	if err := wo.emitRequestHumanFeedback(ctx, objective, todoListMarkdown,
		"planning_verification",
		database.WorkflowStatusPreVerification, // Stay in planning phase
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
