package types

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/database"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "mcpagent/agent"
	baseevents "mcpagent/events"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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
				ID:          "planning",
				Title:       "Planning",
				Description: "Create and iterate on a comprehensive plan using the planning agent. You can refine and improve the plan through conversation until you're satisfied. Variable extraction is handled by the planning agent tools during this phase.",
				Options:     []WorkflowPhaseOption{}, // No options for planning phase
			},
			{
				ID:          database.WorkflowStatusPreVerification,
				Title:       "Execution",
				Description: "Execute the approved plan using MCP tools. This phase runs after planning is complete.",
				Options:     []WorkflowPhaseOption{}, // No options for execution phase
			},
			{
				ID:          "evaluation-planning",
				Title:       "Evaluation Designer",
				Description: "Create evaluation guides to assess workflow execution results. Define what to check, how to pre-validate, and score-based success criteria (0-10).",
				Options:     []WorkflowPhaseOption{},
			},
			{
				ID:          "evaluation-execution",
				Title:       "Evaluation Execution",
				Description: "Execute the evaluation plan against workflow execution results to generate scores and feedback.",
				Options:     []WorkflowPhaseOption{},
			},
			{
				ID:          "plan-improvement",
				Title:       "Plan Debugger",
				Description: "Analyze execution results, plan.json, learnings folder, and validation reports to provide feedback and suggestions for improving the plan based on real execution outcomes.",
				Options:     []WorkflowPhaseOption{}, // No options for plan debugger phase
			},
			{
				ID:          "plan-tool-optimization",
				Title:       "Plan Tool Optimization",
				Description: "Analyze plan.json and learnings folder to optimize tool selections in step_config.json. Compares configured tools vs actually used tools and updates step_config.json to include only tools that were used.",
				Options:     []WorkflowPhaseOption{}, // No options for tool optimization phase
			},
			{
				ID:          "learning-anonymization",
				Title:       "Learning Anonymization",
				Description: "Scan learnings folder and replace actual values with variable placeholders. For .md files: replace with {{VARIABLE_NAME}} placeholders. For .py files: refactor to accept variables as parameters (argparse/env vars). Makes learnings reusable across different environments.",
				Options:     []WorkflowPhaseOption{}, // No options for anonymization phase
			},
			{
				ID:          "plan-learnings-alignment",
				Title:       "Plan-Learnings Alignment",
				Description: "Analyze alignment between plan.json and learnings folders to identify and categorize learning files. Checks if files match steps, are in correct folders, and identifies orphaned or mismatched files.",
				Options:     []WorkflowPhaseOption{}, // No options for alignment phase
			},
			{
				ID:          "learning-consolidation",
				Title:       "Learning Consolidation",
				Description: "Analyze and consolidate learning files to identify duplicate patterns, similar patterns, and outdated patterns. Merges redundant patterns and optimizes learning structure for better future execution efficiency.",
				Options:     []WorkflowPhaseOption{}, // No options for consolidation phase
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
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// WorkflowOrchestrator handles todo-list-based workflow execution
type WorkflowOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Preset-level agent defaults (used when step config doesn't specify)
	presetExecutionLLM            *step_based_workflow.AgentLLMConfig // Default for execution agents
	presetValidationLLM           *step_based_workflow.AgentLLMConfig // Default for validation agents
	presetLearningLLM             *step_based_workflow.AgentLLMConfig // Default for learning agents
	presetPhaseLLM                *step_based_workflow.AgentLLMConfig // Default for all phase agents (planning, anonymization, plan improvement, etc.)
	presetPlanImprovementLLM      *step_based_workflow.AgentLLMConfig // Default for plan improvement agent
	presetPlanToolOptimizationLLM *step_based_workflow.AgentLLMConfig // Default for plan tool optimization agent

	// Frontend-provided execution options (when provided, skips interactive prompts)
	executionOptions *step_based_workflow.ExecutionOptions
}

// SetExecutionOptions sets the execution options from frontend
// When set, backend will use these options instead of asking interactively
func (wo *WorkflowOrchestrator) SetExecutionOptions(options *step_based_workflow.ExecutionOptions) {
	wo.executionOptions = options
	if options != nil {
		wo.GetLogger().Info(fmt.Sprintf("📋 WorkflowOrchestrator: Execution options set from frontend: run_mode=%s, strategy=%s, run_folder=%s",
			options.RunMode, options.ExecutionStrategy, options.SelectedRunFolder))
	}
}

// GetExecutionOptions returns the current execution options
func (wo *WorkflowOrchestrator) GetExecutionOptions() *step_based_workflow.ExecutionOptions {
	return wo.executionOptions
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
	logger loggerv2.Logger,
	eventBridge mcpagent.AgentEventListener,
	tracer observability.Tracer,
	selectedServers []string,
	selectedTools []string, // NEW parameter
	useCodeExecutionMode bool, // NEW parameter
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	toolCategories map[string]string, // NEW: tool category map
	presetLLMConfig *database.PresetLLMConfig, // Optional preset LLM config for agent defaults
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
		selectedTools,        // NEW: Pass through
		useCodeExecutionMode, // NEW: Pass through
		llmConfig,            // LLM configuration
		maxTurns,
		customTools,
		customToolExecutors,
		toolCategories, // NEW: Pass category map
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	// Extract agent-specific defaults from preset LLM config
	var presetExecutionLLM, presetValidationLLM, presetLearningLLM, presetPhaseLLM, presetPlanImprovementLLM, presetPlanToolOptimizationLLM *step_based_workflow.AgentLLMConfig
	if presetLLMConfig != nil {
		// Use agent-specific defaults if available, otherwise fall back to legacy single default
		if presetLLMConfig.ExecutionLLM != nil && presetLLMConfig.ExecutionLLM.Provider != "" && presetLLMConfig.ExecutionLLM.ModelID != "" {
			presetExecutionLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.ExecutionLLM.Provider,
				ModelID:  presetLLMConfig.ExecutionLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for execution
			presetExecutionLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.ValidationLLM != nil && presetLLMConfig.ValidationLLM.Provider != "" && presetLLMConfig.ValidationLLM.ModelID != "" {
			presetValidationLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.ValidationLLM.Provider,
				ModelID:  presetLLMConfig.ValidationLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for validation
			presetValidationLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.LearningLLM != nil && presetLLMConfig.LearningLLM.Provider != "" && presetLLMConfig.LearningLLM.ModelID != "" {
			presetLearningLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.LearningLLM.Provider,
				ModelID:  presetLLMConfig.LearningLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for learning
			presetLearningLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		// Extract phase LLM (used by all phase agents: planning, anonymization, plan improvement, etc.)
		if presetLLMConfig.PhaseLLM != nil && presetLLMConfig.PhaseLLM.Provider != "" && presetLLMConfig.PhaseLLM.ModelID != "" {
			presetPhaseLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.PhaseLLM.Provider,
				ModelID:  presetLLMConfig.PhaseLLM.ModelID,
			}
		}
		// Initialize all learning-related agents from learning LLM (not individually configurable in UI)
		if presetLearningLLM != nil {
			presetPlanImprovementLLM = presetLearningLLM
			presetPlanToolOptimizationLLM = presetLearningLLM
			// Note: presetAnonymizationLLM and presetLearningConsolidationLLM are deprecated and removed
		}
	}

	// Create workflow orchestrator instance
	wo := &WorkflowOrchestrator{
		BaseOrchestrator:              baseOrchestrator,
		presetExecutionLLM:            presetExecutionLLM,
		presetValidationLLM:           presetValidationLLM,
		presetLearningLLM:             presetLearningLLM,
		presetPhaseLLM:                presetPhaseLLM,
		presetPlanImprovementLLM:      presetPlanImprovementLLM,
		presetPlanToolOptimizationLLM: presetPlanToolOptimizationLLM,
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
	stepID string, // Optional step ID for step-specific phase execution
) (string, error) {
	// Set workspace path from parameter
	wo.SetWorkspacePath(workspacePath)
	if wo.GetWorkspacePath() == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Route to appropriate phase based on workflow status
	// IMPORTANT: Each phase is isolated and should NOT trigger other phases
	// Note: Variable extraction is now handled by planning agent tools, no separate phase needed

	if workflowStatus == "planning" {
		return wo.runPlanningOnly(ctx, objective, selectedOptions)
	}

	if workflowStatus == "evaluation-planning" {
		return wo.runEvaluationPlanningOnly(ctx, objective, selectedOptions)
	}

	if workflowStatus == "evaluation-execution" {
		return wo.runEvaluationExecutionOnly(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-improvement" {
		return wo.runPlanImprovement(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-tool-optimization" {
		return wo.runPlanToolOptimization(ctx, objective, selectedOptions, stepID)
	}

	if workflowStatus == "learning-anonymization" {
		return wo.runLearningAnonymization(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-learnings-alignment" {
		return wo.runPlanLearningsAlignment(ctx, objective, selectedOptions)
	}

	if workflowStatus == "learning-consolidation" {
		return wo.runLearningConsolidation(ctx, objective, selectedOptions)
	}

	// All other workflow statuses (execution) go through execution phase
	// Execution requires both variables.json and plan.json to exist
	return wo.runPlanning(ctx, objective, selectedOptions)
}

// runPlanningOnly runs only the planning phase
func (wo *WorkflowOrchestrator) runPlanningOnly(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("📋 Starting Planning Phase"))

	// Create human controlled planner orchestrator (needed for planning)
	llmConfig := wo.GetLLMConfig()
	todoPlannerAgent, err := step_based_workflow.NewStepBasedWorkflowOrchestrator(
		ctx,
		wo.GetProvider(),
		wo.GetModel(),
		wo.GetTemperature(),
		wo.GetAgentMode(),
		wo.GetSelectedServers(),
		wo.GetSelectedTools(),
		wo.GetUseCodeExecutionMode(), // NEW: Pass code execution mode
		wo.GetMCPConfigPath(),
		llmConfig,
		wo.GetMaxTurns(),
		wo.GetLogger(),
		wo.GetTracer(),
		wo.GetContextAwareBridge(),
		wo.WorkspaceTools,
		wo.WorkspaceToolExecutors,
		wo.ToolCategories,     // NEW: Pass category map
		wo.presetExecutionLLM, // Pass preset defaults
		wo.presetValidationLLM,
		wo.presetLearningLLM,
		wo.presetPhaseLLM,
		nil, // presetAnonymizationLLM (deprecated, no longer used)
		wo.presetPlanImprovementLLM,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Run only planning
	result, err := todoPlannerAgent.CreatePlanOnly(ctx, objective, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("planning failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Planning completed successfully"))
	return result, nil
}

// runEvaluationPlanningOnly runs only the evaluation planning phase
func (wo *WorkflowOrchestrator) runEvaluationPlanningOnly(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("📋 Starting Evaluation Planning Phase"))

	// Create evaluation manager directly (independent from controller)
	evaluationManager := step_based_workflow.NewEvaluationManager(
		wo.BaseOrchestrator,
		wo.presetPhaseLLM,
		wo.getSessionID(),
		wo.getWorkflowID(),
	)

	// Run evaluation planning
	result, err := evaluationManager.CreateEvaluationPlanOnly(ctx, objective, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("evaluation planning failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Evaluation planning completed successfully"))
	return result, nil
}

// runEvaluationExecutionOnly runs only the evaluation execution phase
func (wo *WorkflowOrchestrator) runEvaluationExecutionOnly(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info("🚀 Starting Evaluation Execution Phase")

	// Check execution options state BEFORE creating orchestrator
	// Note: We'll fail fast later if execution options are missing, but log early for debugging
	if wo.executionOptions == nil {
		wo.GetLogger().Warn("⚠️ Execution options is NIL - evaluation execution will fail without execution options")
	}

	// Fast-fail: Check if evaluation plan exists before setting up orchestrator
	evalPlanPath := "planning/evaluation_plan.json"
	_, err := wo.ReadWorkspaceFile(ctx, evalPlanPath)
	if err != nil {
		wo.GetLogger().Error(fmt.Sprintf("❌ Evaluation plan not found: %v", err), nil)
		return "", fmt.Errorf("evaluation plan not found at %s. Please run Evaluation Designer first to create an evaluation plan", evalPlanPath)
	}

	// Create human controlled planner orchestrator
	llmConfig := wo.GetLLMConfig()
	todoPlannerAgent, err := step_based_workflow.NewStepBasedWorkflowOrchestrator(
		ctx,
		wo.GetProvider(),
		wo.GetModel(),
		wo.GetTemperature(),
		wo.GetAgentMode(),
		wo.GetSelectedServers(),
		wo.GetSelectedTools(),
		wo.GetUseCodeExecutionMode(),
		wo.GetMCPConfigPath(),
		llmConfig,
		wo.GetMaxTurns(),
		wo.GetLogger(),
		wo.GetTracer(),
		wo.GetContextAwareBridge(),
		wo.WorkspaceTools,
		wo.WorkspaceToolExecutors,
		wo.ToolCategories,
		wo.presetExecutionLLM,
		wo.presetValidationLLM,
		wo.presetLearningLLM,
		wo.presetPhaseLLM,
		nil,
		wo.presetPlanImprovementLLM,
	)
	if err != nil {
		wo.GetLogger().Error(fmt.Sprintf("❌ Failed to create orchestrator: %v", err), nil)
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Pass execution options if set
	// CRITICAL: Execution options are required for evaluation execution
	if wo.executionOptions == nil {
		wo.GetLogger().Error("❌ Execution options is NIL - evaluation execution requires execution options", nil)
		return "", fmt.Errorf("evaluation execution requires execution options to be set (including selected run folder)")
	}

	// Validate that todoPlannerAgent was created successfully
	if todoPlannerAgent == nil {
		wo.GetLogger().Error("❌ todoPlannerAgent is nil after creation", nil)
		return "", fmt.Errorf("failed to create orchestrator: orchestrator is nil")
	}

	// Set execution options on the orchestrator
	todoPlannerAgent.SetExecutionOptions(wo.executionOptions)

	// Extract target run folder from execution options
	targetRunFolder := wo.executionOptions.SelectedRunFolder
	if targetRunFolder == "" {
		wo.GetLogger().Error("❌ targetRunFolder is empty in execution options - cannot proceed", nil)
		return "", fmt.Errorf("evaluation execution requires a selected run folder (iteration or group) in execution options")
	}

	// Run evaluation execution
	result, err := todoPlannerAgent.ExecuteEvaluationOnly(ctx, objective, wo.GetWorkspacePath(), targetRunFolder)
	if err != nil {
		wo.GetLogger().Error(fmt.Sprintf("❌ Evaluation execution failed: %v", err), nil)
		return "", fmt.Errorf("evaluation execution failed: %w", err)
	}

	wo.GetLogger().Info("✅ Evaluation execution completed successfully")
	return result, nil
}

// runPlanImprovement runs only the plan improvement phase
func (wo *WorkflowOrchestrator) runPlanImprovement(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("📊 Starting Plan Improvement Phase"))

	// Create plan improvement manager directly (independent from controller)
	planImprovementManager := step_based_workflow.NewPlanImprovementManager(
		wo.BaseOrchestrator,
		wo.presetPlanImprovementLLM,
		wo.presetPhaseLLM, // Pass phase LLM for fallback
		wo.getSessionID(),
		wo.getWorkflowID(),
	)

	// Extract selected_run_folder from execution options if available
	var runPath string
	if wo.executionOptions != nil && wo.executionOptions.SelectedRunFolder != "" {
		runPath = wo.executionOptions.SelectedRunFolder
		wo.GetLogger().Info(fmt.Sprintf("📊 Using selected_run_folder from execution options: %s", runPath))
	} else {
		wo.GetLogger().Info(fmt.Sprintf("📊 No selected_run_folder in execution options, will ask user for path"))
	}

	// Run only plan improvement
	result, err := planImprovementManager.PlanImprovementOnly(ctx, wo.GetWorkspacePath(), runPath)
	if err != nil {
		return "", fmt.Errorf("plan improvement failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Plan improvement completed successfully"))
	return result, nil
}

// runPlanToolOptimization runs only the plan tool optimization phase
func (wo *WorkflowOrchestrator) runPlanToolOptimization(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions, stepID string) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔧 Starting Plan Tool Optimization Phase"))
	if stepID != "" {
		wo.GetLogger().Info(fmt.Sprintf("🔧 Step-specific execution for step: %s", stepID))
	}

	// Create plan tool optimization manager directly (independent from controller)
	toolOptimizationManager := step_based_workflow.NewPlanToolOptimizationManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetPhaseLLM, // Pass phase LLM (primary LLM for plan tool optimization)
	)

	// Run only tool optimization (with optional step ID for step-specific execution)
	result, err := toolOptimizationManager.PlanToolOptimizationOnly(ctx, wo.GetWorkspacePath(), stepID)
	if err != nil {
		return "", fmt.Errorf("plan tool optimization failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Plan tool optimization completed successfully"))
	return result, nil
}

// runLearningAnonymization runs only the learning anonymization phase
func (wo *WorkflowOrchestrator) runLearningAnonymization(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔒 Starting Learning Anonymization Phase"))

	// Create anonymization manager directly (independent from controller)
	anonymizationManager := step_based_workflow.NewAnonymizationManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetPhaseLLM, // Pass phase LLM (primary LLM for anonymization)
	)

	// Run only anonymization
	result, err := anonymizationManager.AnonymizeLearningsOnly(ctx, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("learning anonymization failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Learning anonymization completed successfully"))
	return result, nil
}

// runPlanLearningsAlignment runs only the plan-learnings alignment phase
func (wo *WorkflowOrchestrator) runPlanLearningsAlignment(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔍 Starting Plan-Learnings Alignment Phase"))

	// Create plan learnings alignment manager directly (independent from controller)
	alignmentManager := step_based_workflow.NewPlanLearningsAlignmentManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetPhaseLLM, // Pass phase LLM (primary LLM for alignment)
	)

	// Run only alignment check
	result, err := alignmentManager.CheckAlignmentOnly(ctx, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("plan-learnings alignment failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Plan-learnings alignment completed successfully"))
	return result, nil
}

// runLearningConsolidation runs only the learning consolidation phase
func (wo *WorkflowOrchestrator) runLearningConsolidation(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔍 Starting Learning Consolidation Phase"))

	// Create learning consolidation manager directly (independent from controller)
	consolidationManager := step_based_workflow.NewLearningConsolidationManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetPhaseLLM, // Pass phase LLM (primary LLM for consolidation)
	)

	// Run only consolidation
	result, err := consolidationManager.ConsolidateLearningsOnly(ctx, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("learning consolidation failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Learning consolidation completed successfully"))
	return result, nil
}

// runPlanning runs the execution phase (requires both variables.json and plan.json to exist)
// This is called for execution status and executes the approved plan
func (wo *WorkflowOrchestrator) runPlanning(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🚀 Starting Execution Phase"))

	return wo.runHumanControlledPlanning(ctx, objective)
}

// runHumanControlledPlanning runs the execution phase (CreateTodoList)
// This requires both variables.json and plan.json to exist
func (wo *WorkflowOrchestrator) runHumanControlledPlanning(ctx context.Context, objective string) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🚀 Running Execution for objective: %s", objective))

	// Create human controlled planner orchestrator directly
	llmConfig := wo.GetLLMConfig()
	todoPlannerAgent, err := step_based_workflow.NewStepBasedWorkflowOrchestrator(
		ctx,
		wo.GetProvider(),
		wo.GetModel(),
		wo.GetTemperature(),
		wo.GetAgentMode(),
		wo.GetSelectedServers(),
		wo.GetSelectedTools(),        // NEW: Pass selected tools
		wo.GetUseCodeExecutionMode(), // NEW: Pass code execution mode
		wo.GetMCPConfigPath(),
		llmConfig,
		wo.GetMaxTurns(),
		wo.GetLogger(),
		wo.GetTracer(),
		wo.GetContextAwareBridge(),
		wo.WorkspaceTools,
		wo.WorkspaceToolExecutors,
		wo.ToolCategories,     // NEW: Pass category map
		wo.presetExecutionLLM, // Pass preset defaults
		wo.presetValidationLLM,
		wo.presetLearningLLM,
		wo.presetPhaseLLM,
		nil, // presetAnonymizationLLM (deprecated, no longer used)
		wo.presetPlanImprovementLLM,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Pass execution options from WorkflowOrchestrator to the todo planner if set
	if wo.executionOptions != nil {
		todoPlannerAgent.SetExecutionOptions(wo.executionOptions)
		wo.GetLogger().Info(fmt.Sprintf("📋 Passed execution options to todo planner: run_mode=%s, strategy=%s",
			wo.executionOptions.RunMode, wo.executionOptions.ExecutionStrategy))
	}

	// Generate todo list using Execute method
	todoListMarkdown, err := todoPlannerAgent.Execute(ctx, objective, wo.GetWorkspacePath(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create/update todo list: %w", err)
	}

	// Emit blocking_human_feedback event (no Slack notifications)
	// Note: Execution now happens automatically after plan approval, so no separate phase needed
	if err := wo.emitBlockingHumanFeedback(ctx, objective, todoListMarkdown,
		"Human Controlled Planning Complete",
		"Approve Plan & Continue",
		"Please review the generated todo list and approve to proceed with execution."); err != nil {
		wo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit blocking human feedback event: %v", err))
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

// emitBlockingHumanFeedback emits a blocking human feedback event (no Slack notifications)
func (wo *WorkflowOrchestrator) emitBlockingHumanFeedback(ctx context.Context, objective string, todoListMarkdown string, title string, actionLabel string, actionDescription string) error {

	// Generate unique request ID
	requestID := fmt.Sprintf("feedback_%d", time.Now().UnixNano())

	// Build question text from event data
	questionText := title
	if actionDescription != "" {
		questionText = fmt.Sprintf("%s\n\n%s", title, actionDescription)
	}
	if objective != "" {
		questionText = fmt.Sprintf("%s\n\nObjective: %s", questionText, objective)
	}

	// Build context message from todo list
	contextMsg := todoListMarkdown

	// Set default labels if not provided
	yesLabel := actionLabel
	if yesLabel == "" {
		yesLabel = "Approve Plan & Continue"
	}

	// Create blocking human feedback event data
	eventData := &orchestrator_events.BlockingHumanFeedbackEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
		},
		Question:      questionText,
		AllowFeedback: true, // Allow text feedback in frontend
		Context:       contextMsg,
		SessionID:     wo.getSessionID(),
		WorkflowID:    wo.getWorkflowID(),
		RequestID:     requestID,
		YesNoOnly:     false, // false = frontend shows textarea + "Approve & Continue" button
		YesLabel:      yesLabel,
		NoLabel:       "Reject",
	}

	// Create agent event
	agentEvent := &baseevents.AgentEvent{
		Type:      orchestrator_events.BlockingHumanFeedback,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through event bridge if available
	if wo.GetContextAwareBridge() != nil {
		if bridge, ok := wo.GetContextAwareBridge().(interface {
			HandleEvent(context.Context, *baseevents.AgentEvent) error
		}); ok {
			if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
				return err
			}
		}
	}

	// Note: blocking_human_feedback events do NOT send Slack notifications
	// Only request_human_feedback events send Slack notifications
	feedbackStore := virtualtools.GetHumanFeedbackStore()

	// Create feedback request without notifications (only registers in store for WaitForResponse)
	if err := feedbackStore.CreateRequestWithoutNotification(requestID, questionText); err != nil {
		wo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create feedback request: %v", err))
		// Don't return error, as the event is already emitted to frontend
	}

	return nil
}

// Execute implements the Orchestrator interface
func (wo *WorkflowOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	logger := wo.GetLogger()
	if logger == nil {
		return "", fmt.Errorf("logger is nil in Execute method")
	}

	// Validate options if provided
	if options != nil {
		// Validate workflowStatus if provided
		if workflowStatusVal, exists := options["workflowStatus"]; exists {
			if workflowStatus, ok := workflowStatusVal.(string); !ok {
				return "", fmt.Errorf("invalid workflowStatus: expected string, got %T", workflowStatusVal)
			} else if workflowStatus == "" {
				return "", fmt.Errorf("invalid workflowStatus: cannot be empty string")
			} else {
				// Validate it's a known workflow status
				validStatuses := []string{
					"planning",                             // Planning phase
					database.WorkflowStatusPreVerification, // Execution phase
					"evaluation-planning",                  // Evaluation planning phase
					"evaluation-execution",                 // Evaluation execution phase
					"plan-improvement",                     // Plan improvement phase
					"plan-tool-optimization",               // Plan tool optimization phase
					"learning-anonymization",               // Learning anonymization phase
					"plan-learnings-alignment",             // Plan-learnings alignment phase
					"learning-consolidation",               // Learning consolidation phase
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
		}

		// Validate selectedOptions if provided
		if selectedOptsVal, exists := options["selectedOptions"]; exists {
			if selectedOptsVal != nil {
				if _, ok := selectedOptsVal.(*database.WorkflowSelectedOptions); !ok {
					return "", fmt.Errorf("invalid selectedOptions: expected *database.WorkflowSelectedOptions, got %T", selectedOptsVal)
				}
			}
		}
	}

	// Extract options from the map with defaults
	var workflowStatus string
	if ws, ok := options["workflowStatus"].(string); ok && ws != "" {
		workflowStatus = ws
	} else {
		workflowStatus = database.WorkflowStatusPreVerification // Default to planning phase
	}

	var selectedOptions *database.WorkflowSelectedOptions
	if opts, ok := options["selectedOptions"]; ok && opts != nil {
		if so, ok := opts.(*database.WorkflowSelectedOptions); ok {
			selectedOptions = so
		}
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Validate objective
	if objective == "" {
		return "", fmt.Errorf("objective cannot be empty")
	}

	// Extract stepId from options if provided
	var stepID string
	if stepIDVal, exists := options["stepId"]; exists {
		if stepIDStr, ok := stepIDVal.(string); ok && stepIDStr != "" {
			stepID = stepIDStr
		}
	}

	// Call the existing executeFlow method with the extracted parameters
	result, err := wo.executeFlow(ctx, objective, workspacePath, workflowStatus, selectedOptions, stepID)
	if err != nil {
		wo.GetLogger().Error(fmt.Sprintf("❌ Workflow execution failed: %v", err), err)
		return "", err
	}

	return result, nil
}
