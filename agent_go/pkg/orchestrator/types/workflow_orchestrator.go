package types

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/database"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
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
				ID:          "variable-extraction",
				Title:       "Variable Extraction",
				Description: "Extract variables from the objective and replace hard-coded values with templated placeholders. This phase runs before planning to identify dynamic values that should be parameterized.",
				Options:     []WorkflowPhaseOption{}, // No options for variable extraction phase
			},
			{
				ID:          "planning",
				Title:       "Planning",
				Description: "Create and iterate on a comprehensive plan using the planning agent. You can refine and improve the plan through conversation until you're satisfied. This phase runs after variable extraction and before execution.",
				Options:     []WorkflowPhaseOption{}, // No options for planning phase
			},
			{
				ID:          database.WorkflowStatusPreVerification,
				Title:       "Execution",
				Description: "Execute the approved plan using MCP tools. This phase runs after both variable extraction and planning are complete.",
				Options:     []WorkflowPhaseOption{}, // No options for execution phase
			},
			{
				ID:          "anonymize-learnings",
				Title:       "Anonymize Learnings",
				Description: "Scan the learnings folder (both .md files and Python scripts) to identify actual values that match known variables, request human confirmation, and replace them with variable placeholders for reusability across different environments.",
				Options:     []WorkflowPhaseOption{}, // No options for anonymization phase
			},
			{
				ID:          "plan-improvement",
				Title:       "Plan Improvement",
				Description: "Analyze execution results, plan.json, learnings folder, and validation reports to provide feedback and suggestions for improving the plan based on real execution outcomes.",
				Options:     []WorkflowPhaseOption{}, // No options for plan improvement phase
			},
			{
				ID:          "plan-learnings-alignment",
				Title:       "Plan-Learnings Alignment",
				Description: "Check alignment between plan.json and learnings folder. Identifies orphaned learning files (for deleted steps), missing learnings (for new steps), and provides options to manage mismatches.",
				Options:     []WorkflowPhaseOption{}, // No options for alignment phase
			},
			{
				ID:          "learning-consolidation",
				Title:       "Learning Consolidation",
				Description: "Analyze and consolidate learning files across both learnings/ and learning_code_exec/ folders. Identifies duplicate patterns, similar patterns, and outdated patterns. Consolidates redundant learnings to optimize learning structure for better future execution efficiency.",
				Options:     []WorkflowPhaseOption{}, // No options for consolidation phase
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
	presetAnonymizationLLM          *todo_creation_human.AgentLLMConfig // Default for anonymization agent
	presetPlanImprovementLLM        *todo_creation_human.AgentLLMConfig // Default for plan improvement agent
	presetPlanToolOptimizationLLM   *todo_creation_human.AgentLLMConfig // Default for plan tool optimization agent
	presetPlanLearningsAlignmentLLM *todo_creation_human.AgentLLMConfig // Default for plan learnings alignment agent
	presetLearningConsolidationLLM  *todo_creation_human.AgentLLMConfig // Default for learning consolidation agent

	// Frontend-provided execution options (when provided, skips interactive prompts)
	executionOptions *todo_creation_human.ExecutionOptions
}

// SetExecutionOptions sets the execution options from frontend
// When set, backend will use these options instead of asking interactively
func (wo *WorkflowOrchestrator) SetExecutionOptions(options *todo_creation_human.ExecutionOptions) {
	wo.executionOptions = options
	if options != nil {
		wo.GetLogger().Infof("📋 WorkflowOrchestrator: Execution options set from frontend: run_mode=%s, strategy=%s, run_folder=%s",
			options.RunMode, options.ExecutionStrategy, options.SelectedRunFolder)
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
	logger utils.ExtendedLogger,
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
	var presetExecutionLLM, presetValidationLLM, presetLearningLLM, presetLearningReadingLLM, presetPlanningLLM, presetVariableExtractionLLM, presetAnonymizationLLM, presetPlanImprovementLLM, presetPlanToolOptimizationLLM, presetPlanLearningsAlignmentLLM, presetLearningConsolidationLLM *todo_creation_human.AgentLLMConfig
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
		if presetLLMConfig.AnonymizationLLM != nil && presetLLMConfig.AnonymizationLLM.Provider != "" && presetLLMConfig.AnonymizationLLM.ModelID != "" {
			presetAnonymizationLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.AnonymizationLLM.Provider,
				ModelID:  presetLLMConfig.AnonymizationLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for anonymization
			presetAnonymizationLLM = &todo_creation_human.AgentLLMConfig{
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
		if presetLLMConfig.LearningConsolidationLLM != nil && presetLLMConfig.LearningConsolidationLLM.Provider != "" && presetLLMConfig.LearningConsolidationLLM.ModelID != "" {
			presetLearningConsolidationLLM = &todo_creation_human.AgentLLMConfig{
				Provider: presetLLMConfig.LearningConsolidationLLM.Provider,
				ModelID:  presetLLMConfig.LearningConsolidationLLM.ModelID,
			}
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for learning consolidation
			presetLearningConsolidationLLM = &todo_creation_human.AgentLLMConfig{
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
		presetAnonymizationLLM:          presetAnonymizationLLM,
		presetPlanImprovementLLM:        presetPlanImprovementLLM,
		presetPlanToolOptimizationLLM:   presetPlanToolOptimizationLLM,
		presetPlanLearningsAlignmentLLM: presetPlanLearningsAlignmentLLM,
		presetLearningConsolidationLLM:  presetLearningConsolidationLLM,
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
	if workflowStatus == "variable-extraction" {
		wo.GetLogger().Infof("🔍 Routing to variable extraction phase (workflowStatus: %s)", workflowStatus)
		return wo.runVariableExtraction(ctx, objective, selectedOptions)
	}

	if workflowStatus == "planning" {
		wo.GetLogger().Infof("📋 Routing to planning phase (workflowStatus: %s)", workflowStatus)
		return wo.runPlanningOnly(ctx, objective, selectedOptions)
	}

	if workflowStatus == "anonymize-learnings" {
		wo.GetLogger().Infof("🔒 Routing to anonymize learnings phase (workflowStatus: %s)", workflowStatus)
		return wo.runAnonymization(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-improvement" {
		wo.GetLogger().Infof("📊 Routing to plan improvement phase (workflowStatus: %s)", workflowStatus)
		return wo.runPlanImprovement(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-learnings-alignment" {
		wo.GetLogger().Infof("🔍 Routing to plan-learnings alignment phase (workflowStatus: %s)", workflowStatus)
		return wo.runPlanLearningsAlignment(ctx, objective, selectedOptions)
	}

	if workflowStatus == "learning-consolidation" {
		wo.GetLogger().Infof("🔍 Routing to learning consolidation phase (workflowStatus: %s)", workflowStatus)
		return wo.runLearningConsolidation(ctx, objective, selectedOptions)
	}

	if workflowStatus == "plan-tool-optimization" {
		wo.GetLogger().Infof("🔧 Routing to plan tool optimization phase (workflowStatus: %s)", workflowStatus)
		if stepID != "" {
			wo.GetLogger().Infof("🔧 Step-specific execution for step: %s", stepID)
		}
		return wo.runPlanToolOptimization(ctx, objective, selectedOptions, stepID)
	}

	// All other workflow statuses (execution) go through execution phase
	// Execution requires both variables.json and plan.json to exist
	wo.GetLogger().Infof("🚀 Routing to execution phase (workflowStatus: %s)", workflowStatus)
	return wo.runPlanning(ctx, objective, selectedOptions)
}

// runVariableExtraction runs only the variable extraction phase
func (wo *WorkflowOrchestrator) runVariableExtraction(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Infof("🔍 Starting Variable Extraction Phase")

	// Create variable manager directly (independent from controller)
	variableManager := todo_creation_human.NewVariableManager(
		wo.BaseOrchestrator,
		wo.presetVariableExtractionLLM,
		wo.presetLearningLLM, // Pass learning LLM for fallback
		wo.getSessionID(),
		wo.getWorkflowID(),
	)

	// Run only variable extraction
	result, err := variableManager.ExtractVariablesOnly(ctx, objective, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("variable extraction failed: %w", err)
	}

	wo.GetLogger().Infof("✅ Variable extraction completed successfully")
	return result, nil
}

// runPlanningOnly runs only the planning phase
func (wo *WorkflowOrchestrator) runPlanningOnly(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Infof("📋 Starting Planning Phase")

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
		wo.presetAnonymizationLLM,
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

	wo.GetLogger().Infof("✅ Planning completed successfully")
	return result, nil
}

// runAnonymization runs only the anonymization phase
func (wo *WorkflowOrchestrator) runAnonymization(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Infof("🔒 Starting Anonymize Learnings Phase")

	// Create anonymization manager directly (independent from controller)
	anonymizationManager := todo_creation_human.NewAnonymizationManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetAnonymizationLLM,
		wo.presetLearningLLM, // Pass learning LLM for fallback
	)

	// Run only anonymization
	result, err := anonymizationManager.AnonymizeLearningsOnly(ctx, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("anonymization failed: %w", err)
	}

	wo.GetLogger().Infof("✅ Anonymization completed successfully")
	return result, nil
}

// runPlanImprovement runs only the plan improvement phase
func (wo *WorkflowOrchestrator) runPlanImprovement(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Infof("📊 Starting Plan Improvement Phase")

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

	wo.GetLogger().Infof("✅ Plan improvement completed successfully")
	return result, nil
}

// runPlanLearningsAlignment runs only the plan-learnings alignment check phase
func (wo *WorkflowOrchestrator) runPlanLearningsAlignment(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Infof("🔍 Starting Plan-Learnings Alignment Phase")

	// Create plan learnings alignment manager directly (independent from controller)
	alignmentManager := todo_creation_human.NewPlanLearningsAlignmentManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetPlanLearningsAlignmentLLM,
		wo.presetLearningLLM, // Pass learning LLM for fallback
	)

	// Run only alignment check
	result, err := alignmentManager.CheckAlignmentOnly(ctx, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("plan-learnings alignment check failed: %w", err)
	}

	wo.GetLogger().Infof("✅ Plan-learnings alignment check completed successfully")
	return result, nil
}

// runLearningConsolidation runs only the learning consolidation phase
func (wo *WorkflowOrchestrator) runLearningConsolidation(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Infof("🔍 Starting Learning Consolidation Phase")

	// Create learning consolidation manager directly (independent from controller)
	consolidationManager := todo_creation_human.NewLearningConsolidationManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetLearningConsolidationLLM,
		wo.presetLearningLLM, // Pass learning LLM for fallback
	)

	// Run only consolidation
	result, err := consolidationManager.ConsolidateLearningsOnly(ctx, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("learning consolidation failed: %w", err)
	}

	wo.GetLogger().Infof("✅ Learning consolidation completed successfully")
	return result, nil
}

// runPlanToolOptimization runs only the plan tool optimization phase
func (wo *WorkflowOrchestrator) runPlanToolOptimization(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions, stepID string) (string, error) {
	wo.GetLogger().Infof("🔧 Starting Plan Tool Optimization Phase")
	if stepID != "" {
		wo.GetLogger().Infof("🔧 Step-specific execution for step: %s", stepID)
	}

	// Create plan tool optimization manager directly (independent from controller)
	toolOptimizationManager := todo_creation_human.NewPlanToolOptimizationManager(
		wo.BaseOrchestrator,
		wo.getSessionID(),
		wo.getWorkflowID(),
		wo.presetPlanToolOptimizationLLM,
		wo.presetLearningLLM, // Pass learning LLM for fallback
	)

	// Run only tool optimization (with optional step ID for step-specific execution)
	result, err := toolOptimizationManager.PlanToolOptimizationOnly(ctx, wo.GetWorkspacePath(), stepID)
	if err != nil {
		return "", fmt.Errorf("plan tool optimization failed: %w", err)
	}

	wo.GetLogger().Infof("✅ Plan tool optimization completed successfully")
	return result, nil
}

// runPlanning runs the execution phase (requires both variables.json and plan.json to exist)
// This is called for execution status and executes the approved plan
func (wo *WorkflowOrchestrator) runPlanning(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Infof("🚀 Starting Execution Phase")

	return wo.runHumanControlledPlanning(ctx, objective)
}

// runHumanControlledPlanning runs the execution phase (CreateTodoList)
// This requires both variables.json and plan.json to exist
func (wo *WorkflowOrchestrator) runHumanControlledPlanning(ctx context.Context, objective string) (string, error) {
	wo.GetLogger().Infof("🚀 Running Execution for objective: %s", objective)

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
		wo.presetAnonymizationLLM,
		wo.presetPlanImprovementLLM,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Pass execution options from WorkflowOrchestrator to the todo planner if set
	if wo.executionOptions != nil {
		todoPlannerAgent.SetExecutionOptions(wo.executionOptions)
		wo.GetLogger().Infof("📋 Passed execution options to todo planner: run_mode=%s, strategy=%s",
			wo.executionOptions.RunMode, wo.executionOptions.ExecutionStrategy)
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
					"variable-extraction",                  // Variable extraction phase
					"planning",                             // Planning phase
					database.WorkflowStatusPreVerification, // Execution phase
					"anonymize-learnings",                  // Anonymize learnings phase
					"plan-improvement",                     // Plan improvement phase
					"plan-learnings-alignment",             // Plan-learnings alignment phase
					"learning-consolidation",               // Learning consolidation phase
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

	// Extract stepId from options if provided
	var stepID string
	if stepIDVal, exists := options["stepId"]; exists {
		if stepIDStr, ok := stepIDVal.(string); ok && stepIDStr != "" {
			stepID = stepIDStr
			wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - stepId found: %s", stepID)
		}
	}

	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - About to call executeFlow with workflowStatus: %s", workflowStatus)
	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - selectedOptions for executeFlow: %+v", selectedOptions)
	if stepID != "" {
		wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION DEBUG - Step-specific execution for step: %s", stepID)
	}

	// Call the existing executeFlow method with the extracted parameters
	result, err := wo.executeFlow(ctx, objective, workspacePath, workflowStatus, selectedOptions, stepID)
	if err != nil {
		wo.GetLogger().Errorf("🚀 WORKFLOW EXECUTION ERROR - executeFlow failed: %w", err)
		return "", err
	}

	wo.GetLogger().Infof("🚀 WORKFLOW EXECUTION SUCCESS - executeFlow completed successfully")
	return result, nil
}
