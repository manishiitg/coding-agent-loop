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
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
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
				ID:          "plan-improvement",
				Title:       "Plan Debugger",
				Description: "Analyze execution results, plan.json, learnings folder, and validation reports to provide feedback and suggestions for improving the plan based on real execution outcomes.",
				Options:     []WorkflowPhaseOption{}, // No options for plan debugger phase
			},
			{
				ID:          "plan-learnings-alignment",
				Title:       "Plan-Learnings Alignment",
				Description: "Check alignment between plan.json and learnings folder. Identifies orphaned learning files (for deleted steps), missing learnings (for new steps), and provides options to manage mismatches.",
				Options:     []WorkflowPhaseOption{}, // No options for alignment phase
			},
			{
				ID:          "plan-tool-optimization",
				Title:       "Plan Tool Optimization",
				Description: "Analyze plan.json and learnings folder to optimize tool selections in step_config.json. Compares configured tools vs actually used tools and updates step_config.json to include only tools that were used.",
				Options:     []WorkflowPhaseOption{}, // No options for tool optimization phase
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
	presetExecutionLLM              *todo_creation_human.AgentLLMConfig // Default for execution agents
	presetValidationLLM             *todo_creation_human.AgentLLMConfig // Default for validation agents
	presetLearningLLM               *todo_creation_human.AgentLLMConfig // Default for learning agents
	presetLearningReadingLLM        *todo_creation_human.AgentLLMConfig // Default for learning reading agent
	presetPlanningLLM               *todo_creation_human.AgentLLMConfig // Default for planning agent
	presetVariableExtractionLLM     *todo_creation_human.AgentLLMConfig // Default for variable extraction agent
	presetPlanImprovementLLM        *todo_creation_human.AgentLLMConfig // Default for plan improvement agent
	presetPlanToolOptimizationLLM   *todo_creation_human.AgentLLMConfig // Default for plan tool optimization agent
	presetPlanLearningsAlignmentLLM *todo_creation_human.AgentLLMConfig // Default for plan learnings alignment agent

	// Frontend-provided execution options (when provided, skips interactive prompts)
	executionOptions *todo_creation_human.ExecutionOptions
}

// SetExecutionOptions sets the execution options from frontend
// When set, backend will use these options instead of asking interactively
func (wo *WorkflowOrchestrator) SetExecutionOptions(options *todo_creation_human.ExecutionOptions) {
	wo.executionOptions = options
	if options != nil {
		wo.GetLogger().Info(fmt.Sprintf("📋 WorkflowOrchestrator: Execution options set from frontend: run_mode=%s, strategy=%s, run_folder=%s",
			options.RunMode, options.ExecutionStrategy, options.SelectedRunFolder))
	}
}

// GetExecutionOptions returns the current execution options
func (wo *WorkflowOrchestrator) GetExecutionOptions() *todo_creation_human.ExecutionOptions {
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
	var presetExecutionLLM, presetValidationLLM, presetLearningLLM, presetLearningReadingLLM, presetPlanningLLM, presetVariableExtractionLLM, presetPlanImprovementLLM, presetPlanToolOptimizationLLM, presetPlanLearningsAlignmentLLM *todo_creation_human.AgentLLMConfig
	if presetLLMConfig != nil {
		// Use agent-specific defaults if available, otherwise fall back to legacy single default
		if presetLLMConfig.ExecutionLLM != nil && presetLLMConfig.ExecutionLLM.Provider != "" && presetLLMConfig.ExecutionLLM.ModelID != "" {
			presetExecutionLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.ExecutionLLM.Provider,
				ModelID:  presetLLMConfig.ExecutionLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for execution
			presetExecutionLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.ValidationLLM != nil && presetLLMConfig.ValidationLLM.Provider != "" && presetLLMConfig.ValidationLLM.ModelID != "" {
			presetValidationLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.ValidationLLM.Provider,
				ModelID:  presetLLMConfig.ValidationLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for validation
			presetValidationLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.LearningLLM != nil && presetLLMConfig.LearningLLM.Provider != "" && presetLLMConfig.LearningLLM.ModelID != "" {
			presetLearningLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.LearningLLM.Provider,
				ModelID:  presetLLMConfig.LearningLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for learning
			presetLearningLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.LearningReadingLLM != nil && presetLLMConfig.LearningReadingLLM.Provider != "" && presetLLMConfig.LearningReadingLLM.ModelID != "" {
			presetLearningReadingLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.LearningReadingLLM.Provider,
				ModelID:  presetLLMConfig.LearningReadingLLM.ModelID,
			}
		} else if presetLLMConfig.ExecutionLLM != nil && presetLLMConfig.ExecutionLLM.Provider != "" && presetLLMConfig.ExecutionLLM.ModelID != "" {
			// Fall back to execution LLM if learning reading LLM not set
			presetLearningReadingLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.ExecutionLLM.Provider,
				ModelID:  presetLLMConfig.ExecutionLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for learning reading
			presetLearningReadingLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.PlanningLLM != nil && presetLLMConfig.PlanningLLM.Provider != "" && presetLLMConfig.PlanningLLM.ModelID != "" {
			presetPlanningLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.PlanningLLM.Provider,
				ModelID:  presetLLMConfig.PlanningLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for planning
			presetPlanningLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.VariableExtractionLLM != nil && presetLLMConfig.VariableExtractionLLM.Provider != "" && presetLLMConfig.VariableExtractionLLM.ModelID != "" {
			presetVariableExtractionLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.VariableExtractionLLM.Provider,
				ModelID:  presetLLMConfig.VariableExtractionLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for variable extraction
			presetVariableExtractionLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.PlanImprovementLLM != nil && presetLLMConfig.PlanImprovementLLM.Provider != "" && presetLLMConfig.PlanImprovementLLM.ModelID != "" {
			presetPlanImprovementLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.PlanImprovementLLM.Provider,
				ModelID:  presetLLMConfig.PlanImprovementLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for plan improvement
			presetPlanImprovementLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.PlanToolOptimizationLLM != nil && presetLLMConfig.PlanToolOptimizationLLM.Provider != "" && presetLLMConfig.PlanToolOptimizationLLM.ModelID != "" {
			presetPlanToolOptimizationLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.PlanToolOptimizationLLM.Provider,
				ModelID:  presetLLMConfig.PlanToolOptimizationLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for plan tool optimization
			presetPlanToolOptimizationLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		if presetLLMConfig.PlanLearningsAlignmentLLM != nil && presetLLMConfig.PlanLearningsAlignmentLLM.Provider != "" && presetLLMConfig.PlanLearningsAlignmentLLM.ModelID != "" {
			presetPlanLearningsAlignmentLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.PlanLearningsAlignmentLLM.Provider,
				ModelID:  presetLLMConfig.PlanLearningsAlignmentLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for plan learnings alignment
			presetPlanLearningsAlignmentLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
	}

	// Create workflow orchestrator instance
	wo := &WorkflowOrchestrator{
		BaseOrchestrator:                baseOrchestrator,
		presetExecutionLLM:              presetExecutionLLM,
		presetValidationLLM:             presetValidationLLM,
		presetLearningLLM:               presetLearningLLM,
		presetLearningReadingLLM:        presetLearningReadingLLM,
		presetPlanningLLM:               presetPlanningLLM,
		presetVariableExtractionLLM:     presetVariableExtractionLLM,
		presetPlanImprovementLLM:        presetPlanImprovementLLM,
		presetPlanToolOptimizationLLM:   presetPlanToolOptimizationLLM,
		presetPlanLearningsAlignmentLLM: presetPlanLearningsAlignmentLLM,
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
		wo.GetLogger().Info(fmt.Sprintf("📋 Routing to planning phase (workflowStatus: %s)", workflowStatus))
		return wo.runPlanningOnly(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-improvement" {
		wo.GetLogger().Info(fmt.Sprintf("📊 Routing to plan improvement phase (workflowStatus: %s)", workflowStatus))
		return wo.runPlanImprovement(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-learnings-alignment" {
		wo.GetLogger().Info(fmt.Sprintf("🔍 Routing to plan-learnings alignment phase (workflowStatus: %s)", workflowStatus))
		return wo.runPlanLearningsAlignment(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-tool-optimization" {
		wo.GetLogger().Info(fmt.Sprintf("🔧 Routing to plan tool optimization phase (workflowStatus: %s)", workflowStatus))
		if stepID != "" {
			wo.GetLogger().Info(fmt.Sprintf("🔧 Step-specific execution for step: %s", stepID))
		}
		return wo.runPlanToolOptimization(ctx, objective, selectedOptions, stepID)
	}

	// All other workflow statuses (execution) go through execution phase
	// Execution requires both variables.json and plan.json to exist
	wo.GetLogger().Info(fmt.Sprintf("🚀 Routing to execution phase (workflowStatus: %s)", workflowStatus))
	return wo.runPlanning(ctx, objective, selectedOptions)
}

// runPlanningOnly runs only the planning phase
func (wo *WorkflowOrchestrator) runPlanningOnly(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("📋 Starting Planning Phase"))

	// Create human controlled planner orchestrator (needed for planning)
	llmConfig := wo.GetLLMConfig()
	todoPlannerAgent, err := todo_creation_human.NewHumanControlledTodoPlannerOrchestrator(
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
		wo.presetLearningReadingLLM,
		wo.presetPlanningLLM,
		wo.presetVariableExtractionLLM,
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

// runPlanImprovement runs only the plan improvement phase
func (wo *WorkflowOrchestrator) runPlanImprovement(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("📊 Starting Plan Improvement Phase"))

	// Create plan improvement manager directly (independent from controller)
	planImprovementManager := todo_creation_human.NewPlanImprovementManager(
		wo.BaseOrchestrator,
		wo.presetPlanImprovementLLM,
		wo.presetLearningLLM, // Pass learning LLM for fallback
		wo.getSessionID(),
		wo.getWorkflowID(),
	)

	// Run only plan improvement
	result, err := planImprovementManager.PlanImprovementOnly(ctx, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("plan improvement failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Plan improvement completed successfully"))
	return result, nil
}

// runPlanLearningsAlignment runs only the plan-learnings alignment check phase
func (wo *WorkflowOrchestrator) runPlanLearningsAlignment(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔍 Starting Plan-Learnings Alignment Phase"))

	// Create plan learnings alignment manager directly (independent from controller)
	alignmentManager := todo_creation_human.NewPlanLearningsAlignmentManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetLearningLLM, // Pass learning LLM (primary LLM for plan learnings alignment)
	)

	// Run only alignment check
	result, err := alignmentManager.CheckAlignmentOnly(ctx, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("plan-learnings alignment check failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Plan-learnings alignment check completed successfully"))
	return result, nil
}

// runPlanToolOptimization runs only the plan tool optimization phase
func (wo *WorkflowOrchestrator) runPlanToolOptimization(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions, stepID string) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔧 Starting Plan Tool Optimization Phase"))
	if stepID != "" {
		wo.GetLogger().Info(fmt.Sprintf("🔧 Step-specific execution for step: %s", stepID))
	}

	// Create plan tool optimization manager directly (independent from controller)
	toolOptimizationManager := todo_creation_human.NewPlanToolOptimizationManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetLearningLLM, // Pass learning LLM (primary LLM for plan tool optimization)
	)

	// Run only tool optimization (with optional step ID for step-specific execution)
	result, err := toolOptimizationManager.PlanToolOptimizationOnly(ctx, wo.GetWorkspacePath(), stepID)
	if err != nil {
		return "", fmt.Errorf("plan tool optimization failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Plan tool optimization completed successfully"))
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
	todoPlannerAgent, err := todo_creation_human.NewHumanControlledTodoPlannerOrchestrator(
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
		wo.presetLearningReadingLLM,
		wo.presetPlanningLLM,
		wo.presetVariableExtractionLLM,
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
	eventData := &events.BlockingHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
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
	agentEvent := &events.AgentEvent{
		Type:      events.BlockingHumanFeedback,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through event bridge if available
	if wo.GetContextAwareBridge() != nil {
		if bridge, ok := wo.GetContextAwareBridge().(interface {
			HandleEvent(context.Context, *events.AgentEvent) error
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
	wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION START - Execute method called"))
	wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - objective: %s", objective))
	wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - workspacePath: %s", workspacePath))
	wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - options: %+v", options))

	// Validate options if provided
	if options != nil {
		wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - options is not nil, validating..."))

		// Validate workflowStatus if provided
		if workflowStatusVal, exists := options["workflowStatus"]; exists {
			wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - workflowStatus found: %+v (type: %T)", workflowStatusVal, workflowStatusVal))
			if workflowStatus, ok := workflowStatusVal.(string); !ok {
				return "", fmt.Errorf("invalid workflowStatus: expected string, got %T", workflowStatusVal)
			} else if workflowStatus == "" {
				return "", fmt.Errorf("invalid workflowStatus: cannot be empty string")
			} else {
				// Validate it's a known workflow status
				validStatuses := []string{
					"planning",                             // Planning phase
					database.WorkflowStatusPreVerification, // Execution phase
					"plan-improvement",                     // Plan improvement phase
					"plan-learnings-alignment",             // Plan-learnings alignment phase
					"plan-tool-optimization",               // Plan tool optimization phase
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
			wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - workflowStatus not found in options"))
		}

		// Validate selectedOptions if provided
		if selectedOptsVal, exists := options["selectedOptions"]; exists {
			wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions found: %+v (type: %T)", selectedOptsVal, selectedOptsVal))
			if selectedOptsVal != nil {
				if _, ok := selectedOptsVal.(*database.WorkflowSelectedOptions); !ok {
					return "", fmt.Errorf("invalid selectedOptions: expected *database.WorkflowSelectedOptions, got %T", selectedOptsVal)
				}
			}
		} else {
			wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions not found in options"))
		}
	} else {
		wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - options is nil"))
	}

	// Extract options from the map with defaults
	var workflowStatus string
	if ws, ok := options["workflowStatus"].(string); ok && ws != "" {
		workflowStatus = ws
		wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - extracted workflowStatus: %s", workflowStatus))
	} else {
		workflowStatus = database.WorkflowStatusPreVerification // Default to planning phase
		wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - using default workflowStatus: %s", workflowStatus))
	}

	var selectedOptions *database.WorkflowSelectedOptions
	if opts, ok := options["selectedOptions"]; ok && opts != nil {
		if so, ok := opts.(*database.WorkflowSelectedOptions); ok {
			selectedOptions = so
			wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - extracted selectedOptions: %+v", selectedOptions))
			if selectedOptions != nil {
				wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions.PhaseID: %s", selectedOptions.PhaseID))
				wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions.Selections count: %d", len(selectedOptions.Selections)))
			}
		}
	} else {
		wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - no selectedOptions extracted"))
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
			wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - stepId found: %s", stepID))
		}
	}

	wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - About to call executeFlow with workflowStatus: %s", workflowStatus))
	wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions for executeFlow: %+v", selectedOptions))
	if stepID != "" {
		wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION DEBUG - Step-specific execution for step: %s", stepID))
	}

	// Call the existing executeFlow method with the extracted parameters
	result, err := wo.executeFlow(ctx, objective, workspacePath, workflowStatus, selectedOptions, stepID)
	if err != nil {
		wo.GetLogger().Error(fmt.Sprintf("🚀 WORKFLOW EXECUTION ERROR - executeFlow failed: %w", err), err)
		return "", err
	}

	wo.GetLogger().Info(fmt.Sprintf("🚀 WORKFLOW EXECUTION SUCCESS - executeFlow completed successfully"))
	return result, nil
}
