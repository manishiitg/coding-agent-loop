package types

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"

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
				ID:          workflowtypes.WorkflowStatusWorkflowBuilder,
				Title:       "Workflow Builder",
				Description: "Execute steps, update the plan, tweak configs, generate learnings, debug, manage schedules, and run evaluations — all in one conversation.",
				Options:     []WorkflowPhaseOption{},
			},
			{
				ID:          workflowtypes.WorkflowStatusPreVerification,
				Title:       "Execution",
				Description: "Execute the approved plan using MCP tools. This phase runs after planning is complete.",
				Options:     []WorkflowPhaseOption{},
			},
			{
				ID:          workflowtypes.WorkflowStatusEvalExecution,
				Title:       "Evaluation Execution",
				Description: "Execute the evaluation plan against workflow execution results to generate scores and feedback.",
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

	// Preset-level agent defaults
	presetPhaseLLM       *step_based_workflow.AgentLLMConfig // Default for lightweight phase agents.
	presetMaintenanceLLM *step_based_workflow.AgentLLMConfig // Default for expensive background maintenance/advisor agents.

	// Preset-level feature toggles
	useKnowledgebase  bool   // Whether to create and reference knowledgebase folder (default: true)
	lockKnowledgebase bool   // When true, post-step KB update agent never fires; graph.json mutates only via explicit reorganize_knowledgebase calls
	kbShape           string // "graph+notes" (default) | "notes-only"; controls which KB artifacts exist.

	// Tiered LLM allocation mode
	tieredConfig *step_based_workflow.TieredLLMConfig

	// Frontend-provided execution options (when provided, skips interactive prompts)
	executionOptions *step_based_workflow.ExecutionOptions

	// Synthetic plan for "Task Agent" mode
	virtualPlan *step_based_workflow.PlanningResponse

	// Session ID for MCP connection management
	// Generated once when workflow starts, used by all agents to share MCP connections
	sessionID string

	// HTTP session ID (from the frontend/API layer) for scoped MCP cleanup
	httpSessionID string

	// CDP port for browser mode detection (0 = headless, >0 = CDP mode)
	cdpPort int

	// Browser mode for prompt instructions ("playwright", "headless", "cdp", or "none").
	browserMode string

	// toolCallQueryFunc provides live tool call query capability for workshop sessions.
	// Set by the server layer which has access to the EventStore.
	toolCallQueryFunc step_based_workflow.ToolCallQueryFunc

	// extraSubAgentNotifier is an additional notifier set by the server layer (e.g. for bgAgentRegistry).
	// It is reserved for execution paths that need extra sub-agent notifications.
	extraSubAgentNotifier step_based_workflow.SubAgentNotifier
}

// SetToolCallQueryFunc sets the function for querying live tool calls from the event store.
// Called by server.go after creating the orchestrator to inject EventStore access.
func (wo *WorkflowOrchestrator) SetToolCallQueryFunc(fn step_based_workflow.ToolCallQueryFunc) {
	wo.toolCallQueryFunc = fn
}

// SetExtraSubAgentNotifier sets an additional notifier chained with the workshop notifier.
// Used by server.go to hook bgAgentRegistry for synthetic turn notifications.
func (wo *WorkflowOrchestrator) SetExtraSubAgentNotifier(n step_based_workflow.SubAgentNotifier) {
	wo.extraSubAgentNotifier = n
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
		wo.ApplyWorkflowLogContext(wo.GetWorkspacePath(), orchestrator.SingleSelectedGroupName(options.EnabledGroupNames))
	}
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

// LockKnowledgebase returns whether the post-step KB update agent is frozen.
func (wo *WorkflowOrchestrator) LockKnowledgebase() bool {
	return wo.lockKnowledgebase
}

// SetLockKnowledgebase toggles the lock_knowledgebase flag.
func (wo *WorkflowOrchestrator) SetLockKnowledgebase(v bool) {
	wo.lockKnowledgebase = v
}

// KBShape returns the preset-declared KB shape (raw; empty = default "graph+notes").
func (wo *WorkflowOrchestrator) KBShape() string {
	return wo.kbShape
}

// SetKBShape overrides the KB shape; accepts empty to fall back to the default.
func (wo *WorkflowOrchestrator) SetKBShape(v string) {
	wo.kbShape = v
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

// convertDBAgentLLMConfig converts a workflowtypes.AgentLLMConfig to step_based_workflow.AgentLLMConfig,
// including fallback models.
func convertDBAgentLLMConfig(dbConfig *workflowtypes.AgentLLMConfig) *step_based_workflow.AgentLLMConfig {
	if dbConfig == nil {
		return nil
	}
	cfg := &step_based_workflow.AgentLLMConfig{
		PublishedLLMID: dbConfig.PublishedLLMID,
		Provider:       dbConfig.Provider,
		ModelID:        dbConfig.ModelID,
		Options:        dbConfig.Options,
	}
	if len(dbConfig.Fallbacks) > 0 {
		cfg.Fallbacks = make([]step_based_workflow.AgentLLMFallback, len(dbConfig.Fallbacks))
		for i, fb := range dbConfig.Fallbacks {
			cfg.Fallbacks[i] = step_based_workflow.AgentLLMFallback{
				PublishedLLMID: fb.PublishedLLMID,
				Provider:       fb.Provider,
				ModelID:        fb.ModelID,
				Options:        fb.Options,
			}
		}
	}
	return cfg
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
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	toolCategories map[string]string, // NEW: tool category map
	presetLLMConfig *workflowtypes.PresetLLMConfig, // Optional preset LLM config for agent defaults
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
		llmConfig,            // LLM configuration
		maxTurns,
		customTools,
		customToolExecutors,
		toolCategories, // NEW: Pass category map
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	// Extract phase LLM from preset config
	var presetPhaseLLM *step_based_workflow.AgentLLMConfig
	var presetMaintenanceLLM *step_based_workflow.AgentLLMConfig
	var resolvedCodingAgentTiered *workflowtypes.TieredLLMConfig
	if presetLLMConfig != nil {
		if phase, tiered, ok := workflowtypes.ResolveCodingAgentConfig(presetLLMConfig); ok {
			presetPhaseLLM = convertDBAgentLLMConfig(phase)
			resolvedCodingAgentTiered = tiered
			if maintenance, ok := workflowtypes.ResolveCodingAgentAutoImproveConfig(presetLLMConfig); ok {
				presetMaintenanceLLM = convertDBAgentLLMConfig(maintenance)
			}
			log.Printf("[CODING_AGENT_LLM] Resolved coding agent %s/%s dynamically", presetLLMConfig.Provider, presetLLMConfig.ModelID)
		} else {
			if presetLLMConfig.AutoImproveLLM != nil && presetLLMConfig.AutoImproveLLM.Provider != "" && presetLLMConfig.AutoImproveLLM.ModelID != "" {
				presetMaintenanceLLM = convertDBAgentLLMConfig(presetLLMConfig.AutoImproveLLM)
			}
			if presetLLMConfig.PhaseLLM != nil && presetLLMConfig.PhaseLLM.Provider != "" && presetLLMConfig.PhaseLLM.ModelID != "" {
				presetPhaseLLM = convertDBAgentLLMConfig(presetLLMConfig.PhaseLLM)
			} else if presetLLMConfig.Provider != "" && presetLLMConfig.ModelID != "" {
				// Fall back to legacy single default for phase agents
				presetPhaseLLM = &step_based_workflow.AgentLLMConfig{
					Provider: presetLLMConfig.Provider,
					ModelID:  presetLLMConfig.ModelID,
				}
			}
		}
	}
	if presetMaintenanceLLM == nil {
		presetMaintenanceLLM = presetPhaseLLM
	}

	// Extract tiered LLM allocation config
	var tieredConfig *step_based_workflow.TieredLLMConfig
	if resolvedCodingAgentTiered != nil || (presetLLMConfig != nil && presetLLMConfig.TieredConfig != nil) {
		sourceTiered := resolvedCodingAgentTiered
		if sourceTiered == nil {
			sourceTiered = presetLLMConfig.TieredConfig
		}
		tier1 := convertDBAgentLLMConfig(sourceTiered.Tier1)
		tier2 := convertDBAgentLLMConfig(sourceTiered.Tier2)
		tier3 := convertDBAgentLLMConfig(sourceTiered.Tier3)
		tieredConfig = &step_based_workflow.TieredLLMConfig{
			Tier1: tier1,
			Tier2: tier2,
			Tier3: tier3,
		}
		// Phase LLM is independent of tiered mode - it's always configured separately
		// The frontend saves phase_llm with Tier1 as default when user hasn't explicitly set one
		if presetPhaseLLM != nil {
			log.Printf("[TIERED_LLM] Phase LLM (independent): %s/%s", presetPhaseLLM.Provider, presetPhaseLLM.ModelID)
		} else {
			log.Printf("[TIERED_LLM] WARNING: No Phase LLM configured - phase agents will fail")
		}
		log.Printf("[TIERED_LLM] Tiered mode enabled - Tier1: %s/%s (fallbacks: %d), Tier2: %s/%s (fallbacks: %d), Tier3: %s/%s (fallbacks: %d)",
			tieredConfig.Tier1.Provider, tieredConfig.Tier1.ModelID, len(tieredConfig.Tier1.Fallbacks),
			tieredConfig.Tier2.Provider, tieredConfig.Tier2.ModelID, len(tieredConfig.Tier2.Fallbacks),
			tieredConfig.Tier3.Provider, tieredConfig.Tier3.ModelID, len(tieredConfig.Tier3.Fallbacks))
	}

	// Extract feature toggles from preset config
	useKnowledgebase := true // Default to enabled
	if presetLLMConfig != nil && presetLLMConfig.UseKnowledgebase != nil {
		useKnowledgebase = *presetLLMConfig.UseKnowledgebase
	}
	lockKnowledgebase := false
	if presetLLMConfig != nil && presetLLMConfig.LockKnowledgebase != nil {
		lockKnowledgebase = *presetLLMConfig.LockKnowledgebase
	}
	kbShape := ""
	if presetLLMConfig != nil {
		kbShape = presetLLMConfig.KBShape
	}

	// Override context editing from preset config (base orchestrator defaults to env var)
	if presetLLMConfig != nil && presetLLMConfig.EnableContextEditing != nil {
		baseOrchestrator.SetEnableContextEditing(*presetLLMConfig.EnableContextEditing)
		log.Printf("[CONTEXT_EDITING] Preset override: enable_context_editing=%v", *presetLLMConfig.EnableContextEditing)
	}

	// Create workflow orchestrator instance
	wo := &WorkflowOrchestrator{
		BaseOrchestrator:     baseOrchestrator,
		presetPhaseLLM:       presetPhaseLLM,
		presetMaintenanceLLM: presetMaintenanceLLM,
		useKnowledgebase:     useKnowledgebase,
		lockKnowledgebase:    lockKnowledgebase,
		kbShape:              kbShape,
		tieredConfig:         tieredConfig,
	}

	return wo, nil
}

// executeFlow executes a workflow with the given parameters
func (wo *WorkflowOrchestrator) executeFlow(
	ctx context.Context,
	objective string,
	workspacePath string,
	workflowStatus string,
	selectedOptions *workflowtypes.WorkflowSelectedOptions,
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
	wo.ApplyWorkflowLogContext(workspacePath, orchestrator.SingleSelectedGroupName(func() []string {
		if wo.executionOptions == nil {
			return nil
		}
		return wo.executionOptions.EnabledGroupNames
	}()))
	if wo.GetWorkspacePath() == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Route to appropriate phase based on workflow status
	// IMPORTANT: Each phase is isolated and should NOT trigger other phases
	// Note: Variable extraction is now handled by planning agent tools, no separate phase needed

	if workflowStatus == workflowtypes.WorkflowStatusEvalExecution {
		return wo.runEvaluationExecutionOnly(ctx, objective, selectedOptions)
	}

	if workflowStatus == workflowtypes.WorkflowStatusWorkflowBuilder {
		return "", fmt.Errorf("%s is a chat-only phase — use phase chat mode instead of orchestrator execution", workflowStatus)
	}

	// All other workflow statuses (execution) go through execution phase
	// Execution requires both variables.json and plan.json to exist
	return wo.runPlanning(ctx, objective, selectedOptions)
}

// runEvaluationExecutionOnly runs only the evaluation execution phase
func (wo *WorkflowOrchestrator) runEvaluationExecutionOnly(ctx context.Context, objective string, selectedOptions *workflowtypes.WorkflowSelectedOptions) (string, error) {
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
		wo.GetMCPConfigPath(),
		llmConfig,
		wo.GetMaxTurns(),
		wo.GetLogger(),
		wo.GetTracer(),
		wo.GetContextAwareBridge(),
		wo.WorkspaceTools,
		wo.WorkspaceToolExecutors,
		wo.ToolCategories,
		wo.presetPhaseLLM,
		wo.presetMaintenanceLLM,
		wo.useKnowledgebase, // Feature toggle for knowledgebase
		wo.tieredConfig,     // Tiered LLM config
	)
	if err != nil {
		wo.GetLogger().Error(fmt.Sprintf("❌ Failed to create orchestrator: %v", err), nil)
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Propagate workspace env ref BEFORE session ID so SetMCPSessionID can update it
	if envRef := wo.GetWorkspaceEnvRef(); envRef != nil {
		todoPlannerAgent.SetWorkspaceEnvRef(envRef)
	}

	// Propagate MCP session ID to child orchestrator for connection sharing
	todoPlannerAgent.SetMCPSessionID(wo.getSessionID())
	// Propagate HTTP session ID for MCP cleanup scoping
	if wo.httpSessionID != "" {
		todoPlannerAgent.SetHTTPSessionID(wo.httpSessionID)
	}

	// Propagate selected skills to child orchestrator
	if skills := wo.GetSelectedSkills(); len(skills) > 0 {
		todoPlannerAgent.SetSelectedSkills(skills)
	}

	// Propagate secrets to child orchestrator
	if secrets := wo.GetSecrets(); len(secrets) > 0 {
		todoPlannerAgent.SetSecrets(secrets)
	}

	// Propagate CDP port for browser mode detection
	if wo.cdpPort > 0 {
		todoPlannerAgent.SetCdpPort(wo.cdpPort)
	}
	if wo.browserMode != "" {
		todoPlannerAgent.SetBrowserMode(wo.browserMode)
	}

	// Propagate knowledgebase lock flag + declared KB shape.
	todoPlannerAgent.SetLockKnowledgebase(wo.lockKnowledgebase)
	todoPlannerAgent.SetKBShape(wo.kbShape)

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

// runPlanning runs the execution phase (requires both variables.json and plan.json to exist)
// This is called for execution status and executes the approved plan
func (wo *WorkflowOrchestrator) runPlanning(ctx context.Context, objective string, selectedOptions *workflowtypes.WorkflowSelectedOptions) (string, error) {
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
		"", // provider (not used - LLM comes from temp override/step config/preset)
		"", // model (not used - LLM comes from temp override/step config/preset)
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
		wo.ToolCategories,
		wo.presetPhaseLLM,
		wo.presetMaintenanceLLM,
		wo.useKnowledgebase,
		wo.tieredConfig,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create human controlled planner orchestrator: %w", err)
	}

	// Propagate workspace env ref BEFORE session ID so SetMCPSessionID can update it
	if envRef := wo.GetWorkspaceEnvRef(); envRef != nil {
		todoPlannerAgent.SetWorkspaceEnvRef(envRef)
	}

	// Propagate MCP session ID to child orchestrator for connection sharing
	todoPlannerAgent.SetMCPSessionID(wo.getSessionID())
	// Propagate HTTP session ID for MCP cleanup scoping
	if wo.httpSessionID != "" {
		todoPlannerAgent.SetHTTPSessionID(wo.httpSessionID)
	}

	// Propagate selected skills to child orchestrator
	if skills := wo.GetSelectedSkills(); len(skills) > 0 {
		todoPlannerAgent.SetSelectedSkills(skills)
	}

	// Propagate secrets to child orchestrator
	if secrets := wo.GetSecrets(); len(secrets) > 0 {
		todoPlannerAgent.SetSecrets(secrets)
	}

	// Propagate CDP port for browser mode detection
	if wo.cdpPort > 0 {
		todoPlannerAgent.SetCdpPort(wo.cdpPort)
	}
	if wo.browserMode != "" {
		todoPlannerAgent.SetBrowserMode(wo.browserMode)
	}

	// Propagate knowledgebase lock flag + declared KB shape.
	todoPlannerAgent.SetLockKnowledgebase(wo.lockKnowledgebase)
	todoPlannerAgent.SetKBShape(wo.kbShape)

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

	// Emit the workflow completion event. Do not also emit unified_completion here:
	// the workflow step/agent end events already carry the result text, and rendering
	// both events causes duplicate final output in the UI.
	wo.EmitOrchestratorEnd(ctx, objective, planningResult, "completed", "", "workflow_execution")

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
		// Register with HTTP session tracker so CloseHTTPSession cleans it up on stop
		if wo.httpSessionID != "" {
			mcpagent.RegisterHTTPSession(wo.httpSessionID, wo.sessionID)
		}
	}
	return wo.sessionID
}

// GetMCPSessionID returns the MCP session ID for external use (e.g., agent config)
func (wo *WorkflowOrchestrator) GetMCPSessionID() string {
	return wo.getSessionID()
}

// SetHTTPSessionID sets the HTTP session ID so that MCP sessions can be tracked
// and closed via mcpagent.CloseHTTPSession when the workflow stops.
func (wo *WorkflowOrchestrator) SetHTTPSessionID(httpSessionID string) {
	wo.httpSessionID = httpSessionID
}

// SetCdpPort sets the CDP port for browser mode detection.
// 0 = headless mode, >0 = CDP mode (connected to user's Chrome).
func (wo *WorkflowOrchestrator) SetCdpPort(port int) {
	wo.cdpPort = port
}

// SetBrowserMode sets the browser mode for prompt instructions.
func (wo *WorkflowOrchestrator) SetBrowserMode(mode string) {
	wo.browserMode = mode
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
					workflowtypes.WorkflowStatusPreVerification,
					workflowtypes.WorkflowStatusEvalExecution,
					workflowtypes.WorkflowStatusWorkflowBuilder,
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
				if _, ok := selectedOptsVal.(*workflowtypes.WorkflowSelectedOptions); !ok {
					return "", fmt.Errorf("invalid selectedOptions: expected *workflowtypes.WorkflowSelectedOptions, got %T", selectedOptsVal)
				}
			}
		}
	}

	// Extract options from the map with defaults
	var workflowStatus string
	if ws, ok := options["workflowStatus"].(string); ok && ws != "" {
		workflowStatus = ws
	} else {
		workflowStatus = workflowtypes.WorkflowStatusPreVerification // Default to planning phase
	}

	var selectedOptions *workflowtypes.WorkflowSelectedOptions
	if opts, ok := options["selectedOptions"]; ok && opts != nil {
		if so, ok := opts.(*workflowtypes.WorkflowSelectedOptions); ok {
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
