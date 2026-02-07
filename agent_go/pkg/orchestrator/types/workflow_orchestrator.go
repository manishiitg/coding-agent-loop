package types

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/database"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

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
				ID:          "evaluation-designer",
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
				ID:          "evaluation-debugger",
				Title:       "Evaluation Debugger",
				Description: "Analyze evaluation results and plan to provide feedback and suggestions for improving the evaluation plan based on scores.",
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
			{
				ID:          "code-exec-debugging",
				Title:       "Code Debugger",
				Description: "Analyze execution logs and conversation history for code execution steps. Identifies common errors like hardcoded paths, incorrect CLI arguments, and workspace tool misuse, providing specific fixes to the plan.",
				Options:     []WorkflowPhaseOption{}, // No options for code debugger phase
			},
			{
				ID:          "execution-debugger",
				Title:       "Execution Debugger",
				Description: "Analyze execution results, logs, and validation reports to answer questions about what happened during workflow execution. Read-only analysis without plan modifications.",
				Options:     []WorkflowPhaseOption{},
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

	// Preset-level feature toggles
	useKnowledgebase bool // Whether to create and reference knowledgebase folder (default: true)

	// Tiered LLM allocation mode
	tieredConfig      *step_based_workflow.TieredLLMConfig
	llmAllocationMode string

	// Frontend-provided execution options (when provided, skips interactive prompts)
	executionOptions *step_based_workflow.ExecutionOptions

	// Synthetic plan for "Task Agent" mode
	virtualPlan *step_based_workflow.PlanningResponse

	// Session ID for MCP connection management
	// Generated once when workflow starts, used by all agents to share MCP connections
	sessionID string
}

// SetVirtualPlan sets a synthetic plan for the workflow (used by Task Agent mode)
func (wo *WorkflowOrchestrator) SetVirtualPlan(plan *step_based_workflow.PlanningResponse) {
	wo.virtualPlan = plan
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

// UseKnowledgebase returns whether the knowledgebase feature is enabled
func (wo *WorkflowOrchestrator) UseKnowledgebase() bool {
	return wo.useKnowledgebase
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
// Note: provider and model parameters removed - LLM selection uses temp override → step config → preset LLM priority
func NewWorkflowOrchestrator(
	mcpConfigPath string,
	temperature float64,
	agentMode string,
	logger loggerv2.Logger,
	eventBridge mcpagent.AgentEventListener,
	tracer observability.Tracer,
	selectedServers []string,
	selectedTools []string, // NEW parameter
	useCodeExecutionMode bool, // NEW parameter
	useToolSearchMode bool, // Enable tool search mode
	preDiscoveredTools []string, // Tools always available without searching
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	toolCategories map[string]string, // NEW: tool category map
	presetLLMConfig *database.PresetLLMConfig, // Optional preset LLM config for agent defaults
) (*WorkflowOrchestrator, error) {

	// Create base orchestrator
	// Note: provider and model parameters removed - not used for workflow orchestrator (LLM comes from temp override/step config/preset)
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		logger,
		eventBridge,
		orchestrator.OrchestratorTypeWorkflow,
		mcpConfigPath,
		temperature,
		agentMode,
		selectedServers,
		selectedTools,        // NEW: Pass through
		useCodeExecutionMode, // NEW: Pass through
		useToolSearchMode,    // NEW: Pass through
		preDiscoveredTools,   // NEW: Pass through
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
			log.Printf("[PRESET_EXECUTION_LLM_DEBUG] Extracted presetExecutionLLM from ExecutionLLM: %s/%s", presetExecutionLLM.Provider, presetExecutionLLM.ModelID)
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for execution
			presetExecutionLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
			log.Printf("[PRESET_EXECUTION_LLM_DEBUG] Extracted presetExecutionLLM from legacy Provider/ModelID: %s/%s", presetExecutionLLM.Provider, presetExecutionLLM.ModelID)
		} else {
			log.Printf("[PRESET_EXECUTION_LLM_DEBUG] No presetExecutionLLM found - presetLLMConfig.ExecutionLLM is nil and legacy Provider/ModelID are empty")
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
		} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
			// Fall back to legacy single default for phase agents
			presetPhaseLLM = &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.Provider,
				ModelID:  presetLLMConfig.ModelID,
			}
		}
		// Initialize all learning-related agents from learning LLM (not individually configurable in UI)
		if presetLearningLLM != nil {
			presetPlanImprovementLLM = presetLearningLLM
			presetPlanToolOptimizationLLM = presetLearningLLM
			// Note: presetAnonymizationLLM and presetLearningConsolidationLLM are deprecated and removed
		}
	} else {
		log.Printf("[PRESET_EXECUTION_LLM_DEBUG] presetLLMConfig is nil - no preset LLM config provided")
	}

	// Extract tiered LLM allocation config
	var llmAllocationMode string
	var tieredConfig *step_based_workflow.TieredLLMConfig
	if presetLLMConfig != nil && presetLLMConfig.LLMAllocationMode == "tiered" && presetLLMConfig.TieredConfig != nil {
		llmAllocationMode = "tiered"
		tieredConfig = &step_based_workflow.TieredLLMConfig{
			Tier1: &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.TieredConfig.Tier1.Provider,
				ModelID:  presetLLMConfig.TieredConfig.Tier1.ModelID,
			},
			Tier2: &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.TieredConfig.Tier2.Provider,
				ModelID:  presetLLMConfig.TieredConfig.Tier2.ModelID,
			},
			Tier3: &step_based_workflow.AgentLLMConfig{
				Provider: presetLLMConfig.TieredConfig.Tier3.Provider,
				ModelID:  presetLLMConfig.TieredConfig.Tier3.ModelID,
			},
		}
		// In tiered mode, only use Tier1 as fallback if no explicit Phase LLM is configured
		// This allows users to configure a separate Phase LLM even in tiered mode
		if presetPhaseLLM == nil {
			presetPhaseLLM = tieredConfig.Tier1
			log.Printf("[TIERED_LLM] Using Tier1 as Phase LLM fallback: %s/%s", tieredConfig.Tier1.Provider, tieredConfig.Tier1.ModelID)
		} else {
			log.Printf("[TIERED_LLM] Using explicitly configured Phase LLM: %s/%s", presetPhaseLLM.Provider, presetPhaseLLM.ModelID)
		}
		log.Printf("[TIERED_LLM] Tiered mode enabled - Tier1: %s/%s, Tier2: %s/%s, Tier3: %s/%s",
			tieredConfig.Tier1.Provider, tieredConfig.Tier1.ModelID,
			tieredConfig.Tier2.Provider, tieredConfig.Tier2.ModelID,
			tieredConfig.Tier3.Provider, tieredConfig.Tier3.ModelID)
	}

	// Extract feature toggles from preset config
	useKnowledgebase := true // Default to enabled
	if presetLLMConfig != nil && presetLLMConfig.UseKnowledgebase != nil {
		useKnowledgebase = *presetLLMConfig.UseKnowledgebase
	}

	// Override context editing from preset config (base orchestrator defaults to env var)
	if presetLLMConfig != nil && presetLLMConfig.EnableContextEditing != nil {
		baseOrchestrator.SetEnableContextEditing(*presetLLMConfig.EnableContextEditing)
		log.Printf("[CONTEXT_EDITING] Preset override: enable_context_editing=%v", *presetLLMConfig.EnableContextEditing)
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
		useKnowledgebase:              useKnowledgebase,
		tieredConfig:                  tieredConfig,
		llmAllocationMode:             llmAllocationMode,
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
	// Initialize MCP session ID early so all agents share connections
	// This generates the session ID and propagates it to BaseOrchestrator
	sessionID := wo.getSessionID()
	wo.GetLogger().Info(fmt.Sprintf("🔗 Workflow using MCP session: %s", sessionID))

	// Close all session connections when workflow ends
	// This releases browser profiles and other resources held by MCP servers
	defer func() {
		wo.GetLogger().Info(fmt.Sprintf("🔗 Closing MCP session: %s", sessionID))
		mcpagent.CloseSession(sessionID)
	}()

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

	if workflowStatus == "evaluation-designer" {
		return wo.runEvaluationDesignerOnly(ctx, objective, selectedOptions)
	}

	if workflowStatus == "evaluation-execution" {
		return wo.runEvaluationExecutionOnly(ctx, objective, selectedOptions)
	}

	if workflowStatus == "evaluation-debugger" {
		return wo.runEvaluationDebugger(ctx, objective, selectedOptions)
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

	if workflowStatus == "code-exec-debugging" {
		return wo.runCodeExecDebugging(ctx, objective, selectedOptions)
	}

	if workflowStatus == "execution-debugger" {
		return wo.runExecutionDebugger(ctx, objective, selectedOptions)
	}

	// All other workflow statuses (execution) go through execution phase
	// Execution requires both variables.json and plan.json to exist
	return wo.runPlanning(ctx, objective, selectedOptions)
}

// runCodeExecDebugging runs only the code execution debugging phase
func (wo *WorkflowOrchestrator) runCodeExecDebugging(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔍 Starting Code Execution Debugging Phase"))

	// Create code exec debugging manager directly (independent from controller)
	debuggingManager := step_based_workflow.NewCodeExecDebuggingManager(
		wo.BaseOrchestrator,
		wo.presetPhaseLLM, // Use phase LLM for debugging as requested
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

	// Run only code exec debugging
	result, err := debuggingManager.CodeExecDebuggingOnly(ctx, wo.GetWorkspacePath(), runPath)
	if err != nil {
		return "", fmt.Errorf("code execution debugging failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Code execution debugging completed successfully"))
	return result, nil
}

// runExecutionDebugger runs only the execution debugger phase (read-only)
func (wo *WorkflowOrchestrator) runExecutionDebugger(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔍 Starting Execution Debugger Phase"))

	// Create execution debugger manager directly (independent from controller)
	debuggerManager := step_based_workflow.NewExecutionDebuggerManager(
		wo.BaseOrchestrator,
		wo.presetPhaseLLM, // Use phase LLM for debugging
		wo.getSessionID(),
		wo.getWorkflowID(),
	)

	// Extract selected_run_folder from execution options if available
	var runPath string
	if wo.executionOptions != nil && wo.executionOptions.SelectedRunFolder != "" {
		runPath = wo.executionOptions.SelectedRunFolder
		wo.GetLogger().Info(fmt.Sprintf("📊 Using selected_run_folder from execution options: %s", runPath))
	} else {
		wo.GetLogger().Info("📊 No selected_run_folder in execution options, will ask user for path")
	}

	// Run only execution debugger
	result, err := debuggerManager.ExecutionDebuggerOnly(ctx, wo.GetWorkspacePath(), runPath)
	if err != nil {
		return "", fmt.Errorf("execution debugger failed: %w", err)
	}

	wo.GetLogger().Info("✅ Execution debugger completed successfully")
	return result, nil
}

// runPlanningOnly runs only the planning phase
func (wo *WorkflowOrchestrator) runPlanningOnly(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("📋 Starting Planning Phase"))

	// Create human controlled planner orchestrator (needed for planning)
	llmConfig := wo.GetLLMConfig()
	if wo.presetExecutionLLM != nil {
		wo.GetLogger().Info(fmt.Sprintf("[PRESET_EXECUTION_LLM_DEBUG] [runPlanningOnly] presetExecutionLLM: %s/%s", wo.presetExecutionLLM.Provider, wo.presetExecutionLLM.ModelID))
	} else {
		wo.GetLogger().Info("[PRESET_EXECUTION_LLM_DEBUG] [runPlanningOnly] presetExecutionLLM is nil")
	}
	todoPlannerAgent, err := step_based_workflow.NewStepBasedWorkflowOrchestrator(
		ctx,
		"", // provider (not used - LLM comes from temp override/step config/preset)
		"", // model (not used - LLM comes from temp override/step config/preset)
		wo.GetTemperature(),
		wo.GetAgentMode(),
		wo.GetSelectedServers(),
		wo.GetSelectedTools(),
		wo.GetUseCodeExecutionMode(), // NEW: Pass code execution mode
		wo.GetUseToolSearchMode(),    // NEW: Pass tool search mode
		wo.GetPreDiscoveredTools(),   // NEW: Pass pre-discovered tools
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
		wo.useKnowledgebase, // Feature toggle for knowledgebase
		wo.llmAllocationMode, // Tiered LLM allocation mode
		wo.tieredConfig,      // Tiered LLM config
	)
	if err != nil {
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Propagate MCP session ID to child orchestrator for connection sharing
	todoPlannerAgent.SetMCPSessionID(wo.getSessionID())

	// Run only planning
	result, err := todoPlannerAgent.CreatePlanOnly(ctx, objective, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("planning failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Planning completed successfully"))
	return result, nil
}

// runEvaluationDesignerOnly runs only the evaluation designer phase
func (wo *WorkflowOrchestrator) runEvaluationDesignerOnly(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("📋 Starting Evaluation Designer Phase"))

	// Create evaluation manager directly (independent from controller)
	evaluationManager := step_based_workflow.NewEvaluationManager(
		wo.BaseOrchestrator,
		wo.presetPhaseLLM,
		wo.getSessionID(),
		wo.getWorkflowID(),
	)

	// Run evaluation designer
	result, err := evaluationManager.CreateEvaluationPlanOnly(ctx, objective, wo.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("evaluation designer failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Evaluation Designer completed successfully"))
	return result, nil
}

// runEvaluationDebugger runs only the evaluation debugger phase
func (wo *WorkflowOrchestrator) runEvaluationDebugger(ctx context.Context, objective string, selectedOptions *database.WorkflowSelectedOptions) (string, error) {
	wo.GetLogger().Info(fmt.Sprintf("🔍 Starting Evaluation Debugger Phase"))

	// Create evaluation debugger manager directly
	debuggerManager := step_based_workflow.NewEvaluationDebuggerManager(
		wo.BaseOrchestrator,
		wo.presetPhaseLLM, // Use phase LLM for debugging
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

	// Run only evaluation debugger
	result, err := debuggerManager.EvaluationDebuggerOnly(ctx, wo.GetWorkspacePath(), runPath)
	if err != nil {
		return "", fmt.Errorf("evaluation debugger failed: %w", err)
	}

	wo.GetLogger().Info(fmt.Sprintf("✅ Evaluation debugger completed successfully"))
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
	// Note: evaluation_plan.json is stored in evaluation/ directory (not planning/) per documentation
	// ReadWorkspaceFile will automatically prepend workspace path for relative paths
	evalPlanPath := "evaluation/evaluation_plan.json"
	_, err := wo.ReadWorkspaceFile(ctx, evalPlanPath)
	if err != nil {
		// Check if it's actually a "file not found" error vs other errors (parsing, network, etc.)
		errMsg := err.Error()
		errMsgLower := strings.ToLower(errMsg)
		if strings.Contains(errMsgLower, "not found") || strings.Contains(errMsgLower, "no such file") || strings.Contains(errMsgLower, "document not found") || strings.Contains(errMsgLower, "file does not exist") || strings.Contains(errMsgLower, "file not found") {
			return "", fmt.Errorf("evaluation plan not found at %s. Please run Evaluation Designer first to create an evaluation plan", evalPlanPath)
		}
		// Other errors (parsing, network, etc.) should be returned as-is
		return "", fmt.Errorf("failed to read evaluation plan at %s: %w", evalPlanPath, err)
	}

	// Create human controlled planner orchestrator
	llmConfig := wo.GetLLMConfig()
	todoPlannerAgent, err := step_based_workflow.NewStepBasedWorkflowOrchestrator(
		ctx,
		"", // provider (not used - LLM comes from temp override/step config/preset)
		"", // model (not used - LLM comes from temp override/step config/preset)
		wo.GetTemperature(),
		wo.GetAgentMode(),
		wo.GetSelectedServers(),
		wo.GetSelectedTools(),
		wo.GetUseCodeExecutionMode(),
		wo.GetUseToolSearchMode(),    // NEW: Pass tool search mode
		wo.GetPreDiscoveredTools(),   // NEW: Pass pre-discovered tools
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
		wo.useKnowledgebase, // Feature toggle for knowledgebase
		wo.llmAllocationMode, // Tiered LLM allocation mode
		wo.tieredConfig,      // Tiered LLM config
	)
	if err != nil {
		wo.GetLogger().Error(fmt.Sprintf("❌ Failed to create orchestrator: %v", err), nil)
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Propagate MCP session ID to child orchestrator for connection sharing
	todoPlannerAgent.SetMCPSessionID(wo.getSessionID())

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
		wo.useKnowledgebase,
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
		wo.useKnowledgebase,
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
	if wo.presetExecutionLLM != nil {
		wo.GetLogger().Info(fmt.Sprintf("[PRESET_EXECUTION_LLM_DEBUG] [runHumanControlledPlanning] presetExecutionLLM: %s/%s", wo.presetExecutionLLM.Provider, wo.presetExecutionLLM.ModelID))
	} else {
		wo.GetLogger().Info("[PRESET_EXECUTION_LLM_DEBUG] [runHumanControlledPlanning] presetExecutionLLM is nil")
	}
	todoPlannerAgent, err := step_based_workflow.NewStepBasedWorkflowOrchestrator(
		ctx,
		"", // provider (not used - LLM comes from temp override/step config/preset)
		"", // model (not used - LLM comes from temp override/step config/preset)
		wo.GetTemperature(),
		wo.GetAgentMode(),
		wo.GetSelectedServers(),
		wo.GetSelectedTools(),        // NEW: Pass selected tools
		wo.GetUseCodeExecutionMode(), // NEW: Pass code execution mode
		wo.GetUseToolSearchMode(),    // NEW: Pass tool search mode
		wo.GetPreDiscoveredTools(),   // NEW: Pass pre-discovered tools
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
		wo.useKnowledgebase, // Feature toggle for knowledgebase
		wo.llmAllocationMode, // Tiered LLM allocation mode
		wo.tieredConfig,      // Tiered LLM config
	)
	if err != nil {
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Propagate MCP session ID to child orchestrator for connection sharing
	todoPlannerAgent.SetMCPSessionID(wo.getSessionID())

	// Pass execution options from WorkflowOrchestrator to the todo planner if set
	if wo.executionOptions != nil {
		todoPlannerAgent.SetExecutionOptions(wo.executionOptions)
		wo.GetLogger().Info(fmt.Sprintf("📋 Passed execution options to todo planner: run_mode=%s, strategy=%s",
			wo.executionOptions.RunMode, wo.executionOptions.ExecutionStrategy))
	}

	// Generate todo list using Execute method
	planningResult, err := todoPlannerAgent.Execute(ctx, objective, wo.GetWorkspacePath(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create/update todo list: %w", err)
	}

	// Emit orchestrator completion events
	wo.EmitOrchestratorEnd(ctx, objective, planningResult, "completed", "", "workflow_execution")
	wo.EmitUnifiedCompletionEvent(ctx, "workflow", "workflow", objective, planningResult, "completed", 1)

	return planningResult, nil
}

// Helper methods for workflow operations
// getSessionID returns the session ID for this workflow
// The session ID is generated once and reused for all agents in the workflow
// This allows MCP connections to be shared across agents in the same workflow
func (wo *WorkflowOrchestrator) getSessionID() string {
	if wo.sessionID == "" {
		// Generate session ID once when first requested
		wo.sessionID = fmt.Sprintf("workflow-session-%d", time.Now().UnixNano())
		wo.GetLogger().Info(fmt.Sprintf("🔗 Generated MCP session ID: %s", wo.sessionID))
		// Propagate to BaseOrchestrator so all agents inherit the session ID via config
		wo.SetMCPSessionID(wo.sessionID)
	}
	return wo.sessionID
}

// GetMCPSessionID returns the MCP session ID for external use (e.g., agent config)
func (wo *WorkflowOrchestrator) GetMCPSessionID() string {
	return wo.getSessionID()
}

// getWorkflowID returns the workflow ID for this workflow
func (wo *WorkflowOrchestrator) getWorkflowID() string {
	// This should be generated when the workflow starts
	// For now, return a placeholder
	return "workflow-" + fmt.Sprintf("%d", time.Now().Unix())
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
					"evaluation-designer",                  // Evaluation Designer phase
					"evaluation-execution",                 // Evaluation execution phase
					"evaluation-debugger",                  // Evaluation debugger phase
					"plan-improvement",                     // Plan improvement phase
					"plan-tool-optimization",               // Plan tool optimization phase
					"learning-anonymization",               // Learning anonymization phase
					"plan-learnings-alignment",             // Plan-learnings alignment phase
					"learning-consolidation",               // Learning consolidation phase
					"code-exec-debugging",                  // Code execution debugging phase
					"execution-debugger",                   // Execution debugger phase (read-only)
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
