package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// StepBasedWorkflowOrchestrator manages simplified human-controlled todo planning process
// - Single execution (no iterations)
// - No validation phase
// - No critique phase
// - No cleanup phase
// - Simple direct planning approach
// - Always includes independent steps extraction for parallel execution
// - NEW: Includes learning phase after each step execution and validation
type StepBasedWorkflowOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
	// NEW: Store planning conversation for iterative refinement
	sessionID  string // For human feedback tracking
	workflowID string // For human feedback tracking

	// Variable management
	variablesManifest *VariablesManifest // Extracted variables
	variableValues    map[string]string  // Runtime variable values
	variableManager   *VariableManager   // Variable manager for variable extraction operations (independent from controller)

	// Fast execute mode tracking
	fastExecuteMode    bool // Whether we're in fast execute mode
	fastExecuteEndStep int  // Last step index to fast execute (0-based)

	// Single step execution mode
	runSingleStepOnly bool // Whether to run only a single step and stop
	singleStepTarget  int  // Target step index to run (0-based)

	// Skip human input mode tracking (runs learning but skips human feedback)
	skipHumanInput bool // Whether to skip human feedback requests (auto-approve steps)

	// Evaluation mode tracking (learnings go to evaluation/learnings/)
	isEvaluationMode bool // Whether we're running evaluation steps

	// Learning detail level preference (set once before execution, used for all learning phases)
	learningDetailLevel string // "exact" or "general"

	// Approved plan storage
	approvedPlan *PlanningResponse // Store approved plan

	// Run folder management
	selectedRunFolder string // Selected run folder name (e.g., "iteration-1", "iteration-2")
	selectedRunMode   string // Selected run mode (e.g., "use_same_run", "create_new_runs_always")

	// Batch execution context (tracked for step_progress_updated events)
	currentGroupId  string // Current group ID being executed
	currentGroupIdx int    // 0-based index of current group
	totalGroups     int    // Total number of groups in batch

	// Frontend-provided execution options (when provided, skips interactive prompts)
	executionOptions *ExecutionOptions

	// Preset-level agent defaults (used when step config doesn't specify)
	presetExecutionLLM       *AgentLLMConfig // Default for execution agents
	presetValidationLLM      *AgentLLMConfig // Default for validation agents
	presetLearningLLM        *AgentLLMConfig // Default for learning agents
	presetPhaseLLM           *AgentLLMConfig // Default for all phase agents (planning, anonymization, plan improvement, etc.)
	presetAnonymizationLLM   *AgentLLMConfig // Default for anonymization agent
	presetPlanImprovementLLM *AgentLLMConfig // Default for plan improvement agent

	// Temporary LLM overrides (highest priority, from ExecutionOptions)
	// Only applies to execution agents (not validation or learning agents) for all steps during this execution
	// Cascading fallback: tempLLM1 → tempLLM2 → step LLM (on validation failures)
	tempOverrideLLM  *AgentLLMConfig // First override LLM (used on first attempt)
	tempOverrideLLM2 *AgentLLMConfig // Second override LLM (used on second attempt if tempLLM1 fails)

	// Fallback to original LLM on validation failure (from ExecutionOptions)
	// If true, when validation fails, use original LLM instead of temp override for retry attempts
	fallbackToOriginalLLMOnFailure bool

	// Save validation responses to workspace (from ExecutionOptions)
	// If true, save validation responses to workspace validation folder
	saveValidationResponses bool

	// Preset-level feature toggles
	useKnowledgebase bool // Whether to create and reference knowledgebase folder (default: true)
}

// NewStepBasedWorkflowOrchestrator creates a new human-controlled todo planner orchestrator
func NewStepBasedWorkflowOrchestrator(
	ctx context.Context,
	provider string,
	model string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	selectedTools []string, // NEW parameter
	useCodeExecutionMode bool, // NEW parameter
	useToolSearchMode bool, // Enable tool search mode
	preDiscoveredTools []string, // Tools always available without searching
	mcpConfigPath string,
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	logger loggerv2.Logger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	toolCategories map[string]string, // NEW: tool category map
	presetExecutionLLM *AgentLLMConfig, // Optional preset default for execution agents
	presetValidationLLM *AgentLLMConfig, // Optional preset default for validation agents
	presetLearningLLM *AgentLLMConfig, // Optional preset default for learning agents
	presetPhaseLLM *AgentLLMConfig, // Optional preset default for all phase agents
	presetAnonymizationLLM *AgentLLMConfig, // Optional preset default for anonymization agent
	presetPlanImprovementLLM *AgentLLMConfig, // Optional preset default for plan improvement agent
	useKnowledgebase bool, // Whether to create and reference knowledgebase folder (default: true)
) (*StepBasedWorkflowOrchestrator, error) {

	// Create base workflow orchestrator
	// Note: provider and model parameters removed - not used (LLM comes from temp override/step config/preset)
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		logger,
		eventBridge,
		orchestrator.OrchestratorTypeWorkflow,
		mcpConfigPath,
		temperature,
		agentMode,
		selectedServers,
		selectedTools,        // Pass through actual selected tools
		useCodeExecutionMode, // NEW: Pass code execution mode
		useToolSearchMode,    // NEW: Pass tool search mode
		preDiscoveredTools,   // NEW: Pass pre-discovered tools
		llmConfig,
		maxTurns,
		customTools,
		customToolExecutors,
		toolCategories, // NEW: Pass category map
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	// Generate session ID for MCP connection sharing across all agents in this workflow
	// This MUST be set before creating any agents to ensure connection reuse
	// NOTE: We always run with groups, so include groupID in sessionID format
	// If groupID is not available yet (will be set later in batch execution), use "default-group" placeholder
	// The sessionID will be overridden in batch_execution.go when the actual groupID is known
	groupID := "default-group" // Placeholder - will be overridden in batch execution with actual groupID
	workflowSessionID := fmt.Sprintf("session-group-%s-%d", groupID, time.Now().UnixNano())
	baseOrchestrator.SetMCPSessionID(workflowSessionID)
	logger.Info(fmt.Sprintf("🔗 Set MCP session ID for workflow: %s (will be overridden with actual groupID in batch execution)", workflowSessionID))

	// NOTE: Default conditional agent is now created lazily when needed (in getConditionalAgentForStep)
	// This ensures it's created after batch execution setup, with correct session ID and runtime overrides
	// This matches the pattern used by execution and learning agents

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:         baseOrchestrator,
		sessionID:                workflowSessionID, // Use the same session ID set on BaseOrchestrator for MCP connection sharing
		workflowID:               fmt.Sprintf("workflow_%d", time.Now().UnixNano()),
		presetExecutionLLM:       presetExecutionLLM,
		presetValidationLLM:      presetValidationLLM,
		presetLearningLLM:        presetLearningLLM,
		presetPhaseLLM:           presetPhaseLLM,
		presetAnonymizationLLM:   presetAnonymizationLLM,
		presetPlanImprovementLLM: presetPlanImprovementLLM,
		saveValidationResponses:  true, // Default to true (save validation responses by default)
		useKnowledgebase:         useKnowledgebase,
	}

	// Create VariableManager for variable extraction operations (independent from controller)
	hcpo.variableManager = NewVariableManager(
		baseOrchestrator,
	)

	return hcpo, nil
}

// getConditionalAgentForStep returns the conditional agent to use for a specific step
// Priority: step config conditional_llm > default conditionalAgent
// Uses the standard factory pattern for proper event bridge connection and context setup
// agentName: custom agent name for this specific use case (e.g., "conditional-step-evaluation", "decision-step-evaluation")
// phase: orchestrator phase for context (e.g., "conditional_evaluation", "decision_evaluation")
func (hcpo *StepBasedWorkflowOrchestrator) getConditionalAgentForStep(ctx context.Context, step PlanStepInterface, stepIndex int, agentName, phase string) *WorkflowConditionalAgent {
	stepID := step.GetID()
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", step.GetTitle())
	}

	// Check if step has step-specific config (conditional LLM or code execution mode)
	agentConfigs := getAgentConfigs(step)
	hasValidConditionalLLM := agentConfigs != nil && agentConfigs.ConditionalLLM != nil && agentConfigs.ConditionalLLM.Provider != "" && agentConfigs.ConditionalLLM.ModelID != ""
	hasStepSpecificConfig := agentConfigs != nil && (hasValidConditionalLLM || agentConfigs.UseCodeExecutionMode != nil)

	// Determine code execution mode
	var isCodeExecutionMode bool
	if agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *agentConfigs.UseCodeExecutionMode
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
	}

	// For conditional/decision agents, skip tempLLM and use step/preset LLM directly
	// Fallback order: ConditionalLLM → ExecutionLLM → preset ExecutionLLM → orchestrator default
	// Create LLM config: use conditional LLM if specified, otherwise use execution LLM
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()

	if hasStepSpecificConfig {
		// Step has specific config - use step-specific LLM
		if hasValidConditionalLLM {
			conditionalLLMConfig := agentConfigs.ConditionalLLM
			llmConfig = &orchestrator.LLMConfig{
				Primary: orchestrator.LLMModel{
					Provider: conditionalLLMConfig.Provider,
					ModelID:  conditionalLLMConfig.ModelID,
				},
				APIKeys: orchestratorLLMConfig.APIKeys,
			}
		} else if agentConfigs.ExecutionLLM != nil && agentConfigs.ExecutionLLM.Provider != "" && agentConfigs.ExecutionLLM.ModelID != "" {
			executionLLMConfig := agentConfigs.ExecutionLLM
			llmConfig = &orchestrator.LLMConfig{
				Primary: orchestrator.LLMModel{
					Provider: executionLLMConfig.Provider,
					ModelID:  executionLLMConfig.ModelID,
				},
				APIKeys: orchestratorLLMConfig.APIKeys,
			}
		} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
			llmConfig = &orchestrator.LLMConfig{
				Primary: orchestrator.LLMModel{
					Provider: hcpo.presetExecutionLLM.Provider,
					ModelID:  hcpo.presetExecutionLLM.ModelID,
				},
				APIKeys: orchestratorLLMConfig.APIKeys,
			}
		} else if orchestratorLLMConfig != nil && orchestratorLLMConfig.Primary.Provider != "" && orchestratorLLMConfig.Primary.ModelID != "" {
			llmConfig = orchestratorLLMConfig
		} else {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ No valid LLM configuration found for conditional agent step '%s'", step.GetTitle()))
			return nil
		}

		// Use provided agent name, or fallback to default format
		actualAgentName := agentName
		if actualAgentName == "" {
			actualAgentName = fmt.Sprintf("conditional-agent-%s", stepID)
		}

		// Use provided phase, or fallback to default
		actualPhase := phase
		if actualPhase == "" {
			actualPhase = "conditional_evaluation"
		}

		// Construct stepPath from stepIndex (e.g., "step-3" for stepIndex=2)
		stepPath := fmt.Sprintf("step-%d", stepIndex+1)

		// Create fresh step-specific conditional agent (no caching)
		agent, err := hcpo.createConditionalAgent(
			ctx,
			actualPhase,     // phase (from parameter)
			stepIndex,       // step index
			0,               // iteration
			actualAgentName, // agent name (from parameter)
			agentConfigs,    // step config (includes UseCodeExecutionMode)
			llmConfig,       // conditional LLM config
			stepPath,        // step path for execution folder write access
			stepID,          // step ID for step-specific learnings folder access
		)
		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to create step-specific conditional agent for step '%s': %v", step.GetTitle(), err), nil)
			return nil
		}

		// Type assert to conditional agent
		stepConditionalAgent, ok := agent.(*WorkflowConditionalAgent)
		if !ok {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Factory returned wrong agent type for step '%s'", step.GetTitle()), nil)
			return nil
		}

		if hasValidConditionalLLM {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Created step-specific conditional agent for step '%s' (ID: %s, code exec: %v): %s/%s", step.GetTitle(), stepID, isCodeExecutionMode, agentConfigs.ConditionalLLM.Provider, agentConfigs.ConditionalLLM.ModelID))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Created step-specific conditional agent for step '%s' (ID: %s, code exec: %v): using execution LLM", step.GetTitle(), stepID, isCodeExecutionMode))
		}
		return stepConditionalAgent
	}

	// No step-specific config - create default conditional agent fresh (no caching)
	// Fallback order: preset ExecutionLLM → orchestrator default
	if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetExecutionLLM.Provider,
				ModelID:  hcpo.presetExecutionLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys,
		}
	} else if orchestratorLLMConfig != nil && orchestratorLLMConfig.Primary.Provider != "" && orchestratorLLMConfig.Primary.ModelID != "" {
		llmConfig = orchestratorLLMConfig
	} else {
		hcpo.GetLogger().Error("❌ No valid LLM configuration found for default conditional agent", nil)
		return nil
	}

	// Use provided agent name, or fallback to default format
	actualAgentName := agentName
	if actualAgentName == "" {
		actualAgentName = "conditional-agent-default"
	}

	// Use provided phase, or fallback to default
	actualPhase := phase
	if actualPhase == "" {
		actualPhase = "conditional_evaluation"
	}

	// Construct stepPath from stepIndex (e.g., "step-3" for stepIndex=2)
	stepPath := fmt.Sprintf("step-%d", stepIndex+1)

	// Create fresh default conditional agent (no caching, matches execution/learning agent pattern)
	agent, err := hcpo.createConditionalAgent(
		ctx,
		actualPhase,     // phase
		stepIndex,       // step index
		0,               // iteration
		actualAgentName, // agent name
		nil,             // no step config (default agent)
		llmConfig,       // LLM config
		stepPath,        // step path for execution folder write access
		stepID,          // step ID
	)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to create default conditional agent: %v", err), nil)
		return nil
	}

	// Type assert to conditional agent
	defaultConditionalAgent, ok := agent.(*WorkflowConditionalAgent)
	if !ok {
		hcpo.GetLogger().Error("❌ Factory returned wrong agent type for default conditional agent", nil)
		return nil
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Created default conditional agent fresh (no caching, matches execution/learning agent pattern)"))
	return defaultConditionalAgent
}

// CreateTodoList orchestrates the human-controlled todo planning process
// - Single execution (no iterations)
// - Includes validation phase (runs later in the workflow)
// - Includes critique phase during writer validation loop
// - Skips cleanup phase
// - Simple direct planning approach
// - NEW: Includes human approval loop with iterative plan refinement
func (hcpo *StepBasedWorkflowOrchestrator) CreateTodoList(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🚀 Starting human-controlled todo planning for objective: %s", objective))
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] CreateTodoList: Starting - workspacePath=%s", workspacePath))

	// Set objective and workspace path directly
	// WorkspacePath is the base workspace path (no subdirectory)
	// The workspace API will handle internal resolution to ../workspace-docs/ when needed
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] CreateTodoList: Objective and workspace path set to %s", workspacePath))

	// PHASE 0: Check both variables and plan at start (before any prompts)
	// Check if variables.json exists - OPTIONAL (planning agent can create it)
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := hcpo.variableManager.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		// Log error but continue without variables (planning agent can create them)
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check for existing variables: %v - proceeding without variables", err))
		variablesExist = false
	}

	var templatedObjective string
	if variablesExist && existingVariablesManifest != nil {
		// Variables exist - use them
		hcpo.variablesManifest = existingVariablesManifest // Store in orchestrator so formatVariableNames/Values can access it
		templatedObjective = existingVariablesManifest.Objective
		hcpo.SetObjective(templatedObjective)
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Using existing variables.json with %d variables", len(existingVariablesManifest.Variables)))
	} else {
		// No variables.json - planning agent can extract variables if needed
		hcpo.variablesManifest = nil
		// Use original objective (no templating)
		templatedObjective = hcpo.GetObjective()
	}

	// Check if plan.json exists - REQUIRED for execution
	// Use relative path - ReadWorkspaceFile auto-prepends workspacePath
	planPath := "planning/plan.json"
	planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing plan: %w", err)
	}
	if !planExists {
		return "", fmt.Errorf("plan.json not found at %s - planning must be run first as a separate phase", planPath)
	}

	// Plan exists - use it

	// Safety check: Ensure plan has steps
	if len(existingPlan.Steps) == 0 {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Existing plan has no steps"), nil)
		return "", fmt.Errorf(fmt.Sprintf("existing plan has no steps"), nil)
	}

	// Load runtime variable values if provided and switch to templated objective
	// If a specific group is selected via execution options, use that group's values
	var variableValues map[string]string
	if hcpo.executionOptions != nil && len(hcpo.executionOptions.EnabledGroupIDs) > 0 && hcpo.variablesManifest != nil {
		// Specific group(s) selected - use the first group's values (for single group execution)
		requestedGroupID := hcpo.executionOptions.EnabledGroupIDs[0]

		// Log available groups for debugging
		availableGroupIDs := make([]string, len(hcpo.variablesManifest.Groups))
		for i, g := range hcpo.variablesManifest.Groups {
			availableGroupIDs[i] = g.GroupID
		}

		variableValues = hcpo.variablesManifest.GetVariableValues(requestedGroupID)
		if variableValues == nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [VARIABLE LOADING] Group %s not found in manifest, falling back to LoadVariableValues", requestedGroupID))
			var err error
			variableValues, err = LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [VARIABLE LOADING] Failed to load variable values: %v", err))
			} else {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [VARIABLE LOADING] Loaded from fallback LoadVariableValues (may not match requested group %s)", requestedGroupID))
			}
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ [VARIABLE LOADING] Loaded variable values for selected group: %s (values: %v)", requestedGroupID, variableValues))

			// Validate: Double-check that we got the right group's values
			// Find the group in manifest to verify
			for _, g := range hcpo.variablesManifest.Groups {
				if g.GroupID == requestedGroupID {
					// Compare values to ensure they match
					valuesMatch := true
					if len(variableValues) != len(g.Values) {
						valuesMatch = false
					} else {
						for k, v := range variableValues {
							if g.Values[k] != v {
								valuesMatch = false
								hcpo.GetLogger().Error(fmt.Sprintf("❌ [VARIABLE LOADING] Value mismatch for key %s: expected %s, got %s", k, g.Values[k], v), nil)
								break
							}
						}
					}
					if !valuesMatch {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [VARIABLE LOADING] Variable values don't match group %s! Expected: %v, Got: %v", requestedGroupID, g.Values, variableValues), nil)
					} else {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ [VARIABLE LOADING] Verified variable values match group %s", requestedGroupID))
					}
					break
				}
			}
		}
	} else {
		// No specific group selected - use default LoadVariableValues (backward compatibility)
		var err error
		variableValues, err = LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [VARIABLE LOADING] Failed to load variable values: %v", err))
		}
	}

	if variableValues != nil {
		hcpo.variableValues = variableValues
		hcpo.GetLogger().Info(fmt.Sprintf("✅ [VARIABLE LOADING] Set hcpo.variableValues with %d variables", len(variableValues)))
	} else {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [VARIABLE LOADING] variableValues is nil - no variables loaded"))
	}

	// Switch to templated objective for all subsequent phases
	hcpo.SetObjective(templatedObjective)
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Using templated objective with {{VARIABLES}}: %s", templatedObjective))

	// Populate runtime fields on plan steps for execution
	stepConfigs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults)", err))
		stepConfigs = []StepConfig{}
	}
	for _, step := range existingPlan.Steps {
		if err := populateRuntimeFields(step, stepConfigs); err != nil {
			return "", fmt.Errorf("failed to populate runtime fields: %w", err)
		}
	}
	breakdownSteps := existingPlan.Steps // Use PlanStepInterface directly
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Prepared existing plan: %d steps with runtime fields populated", len(breakdownSteps)))

	// Store approved plan for access during execution
	hcpo.approvedPlan = existingPlan

	// Note: Learning integration phase removed - execution agent now auto-discovers learning files and scripts

	// Check if execution options are provided from chfrontend
	execOpts := hcpo.executionOptions
	var selectedRunMode string
	var selectedRunFolder string

	if execOpts != nil {
		// ===== FRONTEND-PROVIDED EXECUTION OPTIONS =====
		// Use options from frontend, skip interactive prompts

		// Use run mode from options
		selectedRunMode = execOpts.RunMode
		if selectedRunMode == "" {
			selectedRunMode = "use_same_run" // Default
		}
		hcpo.selectedRunMode = selectedRunMode
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Using run mode from frontend: %s", selectedRunMode))

		// Resolve run folder with provided options
		var err error
		selectedRunFolder, err = hcpo.resolveRunFolderWithOptions(ctx, hcpo.GetWorkspacePath(), selectedRunMode, execOpts.SelectedRunFolder)
		if err != nil {
			return "", fmt.Errorf("failed to resolve run folder with frontend options: %w", err)
		}
		hcpo.selectedRunFolder = selectedRunFolder
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Resolved run folder: %s", selectedRunFolder))
		// Set iteration folder for real-time token persistence
		hcpo.SetIterationFolder(selectedRunFolder)
	} else {
		// ===== INTERACTIVE MODE (no frontend options) =====
		// Ask for run mode FIRST (before checking progress)
		// This allows user to select which run folder to use before we check for existing progress
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Asking for run mode selection before checking progress"))

		// First, ask for run mode
		runModeRequestID := fmt.Sprintf("run_mode_selection_%d", time.Now().UnixNano())
		runModeOptions := []string{
			"Use Same Run",   // Option 0: use_same_run
			"Create New Run", // Option 1: create_new_runs_always
		}

		runModeChoice, err := hcpo.RequestMultipleChoiceFeedback(
			ctx,
			runModeRequestID,
			"Which run mode would you like to use for this execution?",
			runModeOptions,
			"Run mode determines how execution folders are organized:\n- Use Same Run: Reuses an existing run folder (you'll be asked to select which one)\n- Create New Run: Creates a new folder for this execution",
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to get user decision for run mode: %v, defaulting to 'use_same_run'", err))
			runModeChoice = "option0" // Default to use_same_run
		}

		// Map choice to run mode value
		switch runModeChoice {
		case "option0": // Use Same Run
			selectedRunMode = "use_same_run"
			hcpo.GetLogger().Info(fmt.Sprintf("✅ User chose run mode: use_same_run"))
		case "option1": // Create New Run
			selectedRunMode = "create_new_runs_always"
			hcpo.GetLogger().Info(fmt.Sprintf("✅ User chose run mode: create_new_runs_always"))
		default:
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Unknown run mode choice: %s, defaulting to 'use_same_run'", runModeChoice))
			selectedRunMode = "use_same_run"
		}

		// Store selected run mode and resolve run folder with it
		hcpo.selectedRunMode = selectedRunMode
		selectedRunFolder, err = hcpo.resolveRunFolder(ctx, hcpo.GetWorkspacePath(), selectedRunMode)
		if err != nil {
			return "", fmt.Errorf("failed to resolve run folder with selected run mode: %w", err)
		}
		hcpo.selectedRunFolder = selectedRunFolder
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Resolved run folder with selected run mode: %s", selectedRunFolder))
		// Set iteration folder for real-time token persistence
		hcpo.SetIterationFolder(selectedRunFolder)
	}

	// EARLY PROGRESS CHECK: Load progress from the selected run folder
	// Note: We no longer check if all steps are completed - execution will proceed regardless

	earlyProgress, err := hcpo.loadStepProgress(ctx)
	planChangeHandled := false // Track if we already handled plan change to avoid duplicate prompts
	if err == nil && earlyProgress != nil && len(earlyProgress.CompletedStepIndices) > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("📊 Found early progress: %d/%d steps completed", len(earlyProgress.CompletedStepIndices), earlyProgress.TotalSteps))

		// Check if total steps match
		if earlyProgress.TotalSteps == len(breakdownSteps) {
			// Plan matches - proceed with execution (no longer checking if all steps are completed)
			hcpo.GetLogger().Info(fmt.Sprintf("📊 Plan matches existing progress - will proceed with execution"))
		} else {
			// Plan changed - handle based on frontend options or ask user
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Total steps changed (previous: %d, current: %d)", earlyProgress.TotalSteps, len(breakdownSteps)))

			// Use selected run mode (or default if not set yet)
			runMode := hcpo.selectedRunMode
			if runMode == "" {
				runMode = "use_same_run"
				hcpo.selectedRunMode = runMode
			}
			hcpo.GetLogger().Info(fmt.Sprintf("📁 Using selected run mode: %s", runMode))

			// Check if we should ask the question (only when reusing existing folder)
			shouldAsk := hcpo.shouldAskDeleteOldProgress(ctx, hcpo.GetWorkspacePath(), runMode)
			if !shouldAsk {
				hcpo.GetLogger().Info(fmt.Sprintf("📁 Run mode '%s' will create new folder - skipping 'Delete old progress' question", runMode))
				earlyProgress = nil
				planChangeHandled = true
			} else if execOpts != nil && execOpts.PlanChangeAction != "" {
				// Use frontend-provided action
				planChangeHandled = true
				switch execOpts.PlanChangeAction {
				case PlanChangeActionKeepOldProgress:
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Frontend chose to keep old progress (will try to match steps)"))
					// Keep earlyProgress as-is
				case PlanChangeActionDeleteOldProgress:
					hcpo.GetLogger().Info(fmt.Sprintf("🔄 Frontend chose to delete old progress and start fresh"))
					execManager := hcpo.GetExecutionManager()
					if err := execManager.CleanupForPlanChange(ctx, len(breakdownSteps), hcpo.GetWorkspacePath(), runMode); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Plan change cleanup failed: %v", err))
					}
					earlyProgress = nil
				default:
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Unknown plan_change_action: %s, keeping old progress", execOpts.PlanChangeAction))
				}
			} else {
				// No frontend action provided - default to keeping old progress
				// User can select "start from beginning" from frontend if they want to start fresh
				planChangeHandled = true
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No plan_change_action provided, defaulting to keep old progress (user can select 'start from beginning' from frontend if needed)"))
				// Keep earlyProgress as-is
			}
		}
	}

	// Process execution strategy early to set controller state
	// This ensures skipHumanInput and fastExecuteMode are set regardless of code path
	if execOpts != nil && execOpts.ExecutionStrategy != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Processing execution strategy early: %s", execOpts.ExecutionStrategy))
		switch execOpts.ExecutionStrategy {
		case ExecutionStrategyStartFromBeginningNoHuman:
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Setting skipHumanInput=true for START_FROM_BEGINNING_NO_HUMAN (early processing)"))
			hcpo.SetSkipHumanInput(true)
		case ExecutionStrategyResumeFromStepNoHuman:
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Setting skipHumanInput=true for RESUME_FROM_STEP_NO_HUMAN (early processing)"))
			hcpo.SetSkipHumanInput(true)
		case ExecutionStrategyFastExecuteAll:
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Setting fastExecuteMode=true for FAST_EXECUTE_ALL (early processing)"))
			hcpo.SetFastExecuteMode(true, len(breakdownSteps)-1)
		case ExecutionStrategyFastResumeFromStep:
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Setting fastExecuteMode=true for FAST_RESUME_FROM_STEP (early processing)"))
			hcpo.SetFastExecuteMode(true, len(breakdownSteps)-1)
		}
	}

	// Determine if user wants to resume or start from beginning
	// Frontend always sends either: start from beginning (selectedStartPoint = 0) or resume from step X
	var startFromStep int = 0 // 0-based index, 0 means start from beginning
	var existingProgress *StepProgress
	isResuming := false

	// Check if user explicitly wants to resume
	// CRITICAL: Also check execution strategy, because resume strategy with ResumeFromStep=0
	// should still be treated as resuming (so validation can catch it)
	if execOpts != nil {
		// Check if it's a resume strategy
		isResumeStrategy := execOpts.ExecutionStrategy == ExecutionStrategyResumeFromStep ||
			execOpts.ExecutionStrategy == ExecutionStrategyResumeFromStepNoHuman ||
			execOpts.ExecutionStrategy == ExecutionStrategyFastResumeFromStep ||
			execOpts.ExecutionStrategy == ExecutionStrategyRunSingleStep

		if execOpts.ResumeFromStep > 0 || execOpts.ResumeFromBranchStep != nil || isResumeStrategy {
			isResuming = true
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 User chose to resume from step (ResumeFromStep=%d, strategy=%s)", execOpts.ResumeFromStep, execOpts.ExecutionStrategy))
		}
	}

	// Use earlyProgress if available, otherwise load it
	if earlyProgress != nil {
		existingProgress = earlyProgress
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Using early progress (avoided reload)"))
	} else {
		// Check if there's existing progress (only if we haven't already handled plan change)
		if !planChangeHandled {
			existingProgress, err = hcpo.loadStepProgress(ctx)
			if err != nil {
				// File doesn't exist - this is normal for first run, log and continue
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No existing progress file found (this is normal for first run), will start fresh execution"))
				existingProgress = nil
			}
		} else {
			// Plan change was already handled, don't reload to avoid duplicate prompts
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Plan change already handled, skipping reload to avoid duplicate prompts"))
			existingProgress = nil
		}
	}

	// Handle two cases: Start from beginning OR Resume from step X
	if !isResuming {
		// Case 1: Start from beginning
		hcpo.GetLogger().Info(fmt.Sprintf("🆕 Starting from beginning"))

		// Clean up execution folder when starting from beginning
		execManager := hcpo.GetExecutionManager()
		runMode := hcpo.selectedRunMode
		if runMode == "" {
			runMode = "use_same_run"
		}
		if err := execManager.CleanupForStartFromBeginning(ctx, hcpo.GetWorkspacePath(), runMode); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Start from beginning cleanup failed: %v", err))
		}
		// Reset progress to nil to ensure fresh initialization (this will reset DecisionEvaluationCounts)
		existingProgress = nil
		earlyProgress = nil // Also clear earlyProgress to ensure old counts don't persist
		// startFromStep is already 0 from initialization
	} else {
		// Case 2: Resume from step X
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Resuming from step"))

		// Load existing progress if available
		if existingProgress == nil {
			existingProgress, err = hcpo.loadStepProgress(ctx)
			if err != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No existing progress file found, will start from step specified by frontend"))
				existingProgress = nil
			}
		}

		// Handle plan change if steps don't match
		if existingProgress != nil && !planChangeHandled && existingProgress.TotalSteps != len(breakdownSteps) {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Plan has changed (previous: %d steps, current: %d steps)", existingProgress.TotalSteps, len(breakdownSteps)))

			// Use frontend-provided plan change action if available
			if execOpts != nil && execOpts.PlanChangeAction != "" {
				switch execOpts.PlanChangeAction {
				case PlanChangeActionKeepOldProgress:
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Frontend chose to keep old progress"))
					// Keep existingProgress as-is
				case PlanChangeActionDeleteOldProgress:
					hcpo.GetLogger().Info(fmt.Sprintf("🔄 Frontend chose to delete old progress and start fresh"))
					execManager := hcpo.GetExecutionManager()
					runMode := hcpo.selectedRunMode
					if runMode == "" {
						runMode = "use_same_run"
					}
					if err := execManager.CleanupForPlanChange(ctx, len(breakdownSteps), hcpo.GetWorkspacePath(), runMode); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Plan change cleanup failed: %v", err))
					}
					existingProgress = nil
				}
			} else {
				// No frontend action provided - default to keeping old progress
				// User can select "start from beginning" from frontend if they want to start fresh
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No plan_change_action provided, defaulting to keep old progress (user can select 'start from beginning' from frontend if needed)"))
			}
		}

		// Process resume logic if we have existing progress
		if existingProgress != nil {

			hcpo.GetLogger().Info(fmt.Sprintf("📊 Found existing progress: %d/%d steps completed", len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps))

			// Find next incomplete step (used as fallback if resume_from_step not specified)
			nextIncompleteStep := 0
			maxStepsToCheck := existingProgress.TotalSteps
			if maxStepsToCheck > len(breakdownSteps) {
				maxStepsToCheck = len(breakdownSteps)
			}
			for i := 0; i < maxStepsToCheck; i++ {
				completed := false
				for _, completedIdx := range existingProgress.CompletedStepIndices {
					if completedIdx == i {
						completed = true
						break
					}
				}
				if !completed {
					nextIncompleteStep = i + 1
					break
				}
			}
			if nextIncompleteStep == 0 && len(breakdownSteps) > existingProgress.TotalSteps {
				nextIncompleteStep = existingProgress.TotalSteps + 1
			}

			// Use resume_from_step or resume_from_branch_step from frontend
			// Frontend always sends one of these when resuming
			if execOpts != nil {
				if execOpts.ResumeFromBranchStep != nil {
					// Resume from branch step - handled in execution_manager.go
					hcpo.GetLogger().Info(fmt.Sprintf("🎯 Resuming from branch step: parent=%d, branch=%s, step=%d",
						execOpts.ResumeFromBranchStep.ParentStepIndex,
						execOpts.ResumeFromBranchStep.BranchType,
						execOpts.ResumeFromBranchStep.BranchStepIndex))
					// startFromStep will be set in execution_manager.go
				} else if execOpts.ResumeFromStep > 0 {
					// Use explicit step from frontend
					startFromStep = execOpts.ResumeFromStep - 1 // Convert to 0-based
					hcpo.GetLogger().Info(fmt.Sprintf("🎯 Resuming from step %d (from frontend)", execOpts.ResumeFromStep))
				} else if nextIncompleteStep > 0 {
					// Fallback to next incomplete step
					startFromStep = nextIncompleteStep - 1
					hcpo.GetLogger().Info(fmt.Sprintf("🎯 Resuming from next incomplete step %d", nextIncompleteStep))
				} else {
					// No incomplete step found, start from beginning
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ No incomplete step found, starting from beginning"))
					startFromStep = 0
				}

				// Handle execution strategy (fast execute, skip human input, etc.)
				if execOpts.ExecutionStrategy != "" {
					switch execOpts.ExecutionStrategy {
					case ExecutionStrategyRunSingleStep:
						hcpo.SetRunSingleStepMode(true, startFromStep)
						// Cleanup for single step will be handled by PrepareExecution + ApplyCleanup below
					case ExecutionStrategyFastResumeFromStep:
						hcpo.SetFastExecuteMode(true, len(breakdownSteps)-1)
					case ExecutionStrategyResumeFromStepNoHuman:
						hcpo.SetSkipHumanInput(true)
					}
				}
			} else {
				// No execOpts - next incomplete step would be used as fallback
				// but startFromStep will be set by PrepareExecution if needed
			}
		} else {
			// No existing progress - startFromStep will be set by PrepareExecution if needed
			if execOpts != nil && execOpts.ResumeFromStep > 0 {
				hcpo.GetLogger().Info(fmt.Sprintf("🎯 Resuming from step %d (no existing progress)", execOpts.ResumeFromStep))
			}
		}
	}

	// Apply cleanup if explicitly resuming from a step or branch step
	// This ensures step N and all subsequent steps are cleaned up before execution
	// Handles both regular step resume (ResumeFromStep) and branch step resume (ResumeFromBranchStep)
	// CRITICAL: Also call PrepareExecution when resume strategy is selected even if ResumeFromStep=0
	// This allows validation to catch invalid resume_from_step=0 and request human feedback
	isResumeStrategy := false
	if execOpts != nil && execOpts.ExecutionStrategy != "" {
		isResumeStrategy = execOpts.ExecutionStrategy == ExecutionStrategyResumeFromStep ||
			execOpts.ExecutionStrategy == ExecutionStrategyResumeFromStepNoHuman ||
			execOpts.ExecutionStrategy == ExecutionStrategyFastResumeFromStep ||
			execOpts.ExecutionStrategy == ExecutionStrategyRunSingleStep
	}

	// Call PrepareExecution if:
	// 1. Resume strategy is selected (even if ResumeFromStep=0, so validation can catch it), OR
	// 2. ResumeFromStep > 0, OR
	// 3. ResumeFromBranchStep is set
	if execOpts != nil && (isResumeStrategy || execOpts.ResumeFromStep > 0 || execOpts.ResumeFromBranchStep != nil) {
		execManager := hcpo.GetExecutionManager()

		// Use ExecutionManager to prepare execution setup (includes cleanup scope)
		// This will validate resume_from_step and request human feedback if invalid
		setup, err := execManager.PrepareExecution(ctx, execOpts, existingProgress, len(breakdownSteps), hcpo.selectedRunFolder)
		if err != nil {
			// If PrepareExecution returns error (e.g., user rejected human feedback), stop execution
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to prepare execution setup: %v", err), err)
			return "", fmt.Errorf("execution preparation failed: %w", err)
		} else if setup != nil {
			// Apply cleanup: delete step folders and update progress
			if err := execManager.ApplyCleanup(ctx, setup); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to apply cleanup for resume from step %d: %v (continuing anyway)", execOpts.ResumeFromStep, err))
			} else {
				// Log appropriate message based on cleanup scope
				if setup.Cleanup.CleanSpecificStep > 0 {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Cleaned up step %d for single step execution", setup.Cleanup.CleanSpecificStep))
				} else if setup.Cleanup.CleanFromStep > 0 {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Cleaned up step %d and all subsequent steps for resume", setup.Cleanup.CleanFromStep))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Applied cleanup for resume (scope: %s)", execManager.GetCleanupDescription(setup.Cleanup)))
				}

				// Reload progress after cleanup to get updated state
				updatedProgress, err := hcpo.loadStepProgress(ctx)
				if err == nil && updatedProgress != nil {
					existingProgress = updatedProgress
					hcpo.GetLogger().Info(fmt.Sprintf("📊 Reloaded progress after cleanup: %d/%d steps completed", len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps))
				}

				// Update startFromStep from setup (in case it was adjusted)
				if setup.StartFromStep >= 0 {
					startFromStep = setup.StartFromStep
					hcpo.GetLogger().Info(fmt.Sprintf("🎯 Updated startFromStep to %d (0-based) from execution setup", startFromStep))
				}
			}
		}
	}

	// Phase 2: Execute plan steps one by one (with validation after each step)

	// Safety check: Ensure breakdownSteps is not empty
	if len(breakdownSteps) == 0 {
		return "", fmt.Errorf(fmt.Sprintf("no steps to execute: breakdownSteps is empty (this should not happen - plan was approved but has no steps)"), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Proceeding to execution phase with %d steps", len(breakdownSteps)))

	// Build execution context once from current controller state
	execCtx := hcpo.buildExecutionContext()

	// Always use batch execution mode (even for single group) to ensure:
	// - Proper session ID management with actual groupID (not "default-group")
	// - Consistent folder structure (runs/iteration-X/group-Y/)
	// - Better isolation and cleanup per group
	enabledGroups := hcpo.getEnabledGroupsForExecution()

	// NOTE: Progress initialization is skipped here because batch execution will handle it per group
	// Each group has its own run folder and progress file, initialized by ApplyCleanup in runBatchExecution
	// This prevents duplicate "Step Progress Updated" events before batch_execution_start

	// DEBUG: Panic if no groups found or if groups have empty GroupID
	if len(enabledGroups) == 0 {
		// PANIC for debugging: groups are required for execution
		panic(fmt.Sprintf("CRITICAL: No variable groups found in getEnabledGroupsForExecution() - cannot proceed without groups. variablesManifest is nil: %v", hcpo.variablesManifest == nil))
	}

	// Validate that all groups have valid GroupIDs
	for i, group := range enabledGroups {
		if group.GroupID == "" {
			// PANIC for debugging: groupID is required for session ID and folder structure
			panic(fmt.Sprintf("CRITICAL: Group at index %d has empty GroupID - all groups must have valid GroupIDs for batch execution. Group values: %v", i, group.Values))
		}
	}

	if len(enabledGroups) > 1 {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Multiple variable groups detected (%d groups), using batch execution mode", len(enabledGroups)))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Single variable group detected (%s), using batch execution mode for consistent session ID and folder structure", enabledGroups[0].GroupID))
	}

	batchResult, err := hcpo.runBatchExecution(ctx, breakdownSteps, 1, execCtx)
	if err != nil {
		return "", fmt.Errorf("batch execution failed: %w", err)
	}
	if !batchResult.Success {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Batch execution completed with %d failed groups", batchResult.FailedGroups))
	}

	duration := time.Since(hcpo.GetStartTime())
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Human-controlled todo planning completed in %v", duration))

	return "Todo planning complete.", nil
}

// executeConditionalStep executes a conditional step by evaluating the condition and executing the chosen branch
// depth: current nesting depth (0 = main plan, 1 = first level conditional, 2 = second level conditional)

// requestHumanFeedback requests human feedback after validation and blocks until user responds
// Returns: (approved bool, feedback string, error)
func (hcpo *StepBasedWorkflowOrchestrator) requestHumanFeedback(ctx context.Context, currentStep, totalSteps int, validationResult string) (bool, string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🤔 Requesting human feedback for step %d/%d", currentStep, totalSteps))

	// Generate unique request ID
	requestID := fmt.Sprintf("step_feedback_%d_%d_%d", currentStep, totalSteps, time.Now().UnixNano())

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Step %d/%d validation completed. Should we continue with execution of the next step?", currentStep, totalSteps),
		validationResult, // Show validation results as context
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

func (hcpo *StepBasedWorkflowOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate that no options are provided since this orchestrator doesn't use them
	if len(options) > 0 {
		return "", fmt.Errorf(fmt.Sprintf("human-controlled todo planner orchestrator does not accept options"), nil)
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf(fmt.Sprintf("workspace path is required"), nil)
	}

	// Call the existing CreateTodoList method
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Execute: About to call CreateTodoList"))
	result, err := hcpo.CreateTodoList(ctx, objective, workspacePath)
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Execute: CreateTodoList returned - error=%v", err))
	return result, err
}

// GetType returns the orchestrator type
func (hcpo *StepBasedWorkflowOrchestrator) GetType() string {
	return "human_controlled_todo_planner"
}

// UseKnowledgebase returns whether the knowledgebase feature is enabled
func (hcpo *StepBasedWorkflowOrchestrator) UseKnowledgebase() bool {
	return hcpo.useKnowledgebase
}

// Helper methods for human feedback tracking

// getSessionID returns the session ID for this orchestrator
// DEBUG: Panic if sessionID is empty to catch cases where it wasn't set properly
func (hcpo *StepBasedWorkflowOrchestrator) getSessionID() string {
	if hcpo.sessionID == "" {
		// PANIC for debugging: sessionID should always be set (either at controller creation or in batch execution)
		// This helps catch cases where sessionID is not properly initialized
		panic(fmt.Sprintf("CRITICAL: sessionID is empty in StepBasedWorkflowOrchestrator.getSessionID() - this should never happen. SessionID must be set before use."))
	}
	return hcpo.sessionID
}

// getWorkflowID returns the workflow ID for this orchestrator
func (hcpo *StepBasedWorkflowOrchestrator) getWorkflowID() string {
	return hcpo.workflowID
}

// SetFastExecuteMode sets the fast execute mode and end step
func (hcpo *StepBasedWorkflowOrchestrator) SetFastExecuteMode(enabled bool, endStep int) {
	hcpo.fastExecuteMode = enabled
	hcpo.fastExecuteEndStep = endStep
}

// SetRunSingleStepMode sets the single step execution mode
func (hcpo *StepBasedWorkflowOrchestrator) SetRunSingleStepMode(enabled bool, stepIndex int) {
	hcpo.runSingleStepOnly = enabled
	hcpo.singleStepTarget = stepIndex
}

// GetLearningDetailLevel returns the stored learning detail level preference
func (hcpo *StepBasedWorkflowOrchestrator) GetLearningDetailLevel() string {
	if hcpo.learningDetailLevel == "" {
		return "exact" // Default
	}
	return hcpo.learningDetailLevel
}

// SetLearningDetailLevel sets the learning detail level preference
func (hcpo *StepBasedWorkflowOrchestrator) SetLearningDetailLevel(level string) {
	hcpo.learningDetailLevel = level
}

// IsFastExecuteStep checks if a step should be executed in fast mode
func (hcpo *StepBasedWorkflowOrchestrator) IsFastExecuteStep(stepIndex int) bool {
	return hcpo.fastExecuteMode && stepIndex <= hcpo.fastExecuteEndStep
}

// SetSkipHumanInput sets the skip human input mode (runs learning but skips human feedback)
func (hcpo *StepBasedWorkflowOrchestrator) SetSkipHumanInput(enabled bool) {
	hcpo.skipHumanInput = enabled
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 SetSkipHumanInput called with value: %v", enabled))
}

// IsSkipHumanInput checks if human feedback should be skipped
func (hcpo *StepBasedWorkflowOrchestrator) IsSkipHumanInput() bool {
	return hcpo.skipHumanInput
}

// SetExecutionOptions sets the execution options from frontend
// When set, backend will use these options instead of asking interactively
func (hcpo *StepBasedWorkflowOrchestrator) SetExecutionOptions(options *ExecutionOptions) {
	hcpo.executionOptions = options
	if options != nil {

		// Apply temporary LLM overrides (highest priority for execution agents only)
		// Cascading fallback: tempLLM1 → tempLLM2 → step LLM
		if options.TempOverrideLLM != nil && options.TempOverrideLLM.Provider != "" && options.TempOverrideLLM.ModelID != "" {
			hcpo.tempOverrideLLM = options.TempOverrideLLM
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temporary execution agent LLM override 1 set: %s/%s (applies to execution agents only, not validation/learning)", options.TempOverrideLLM.Provider, options.TempOverrideLLM.ModelID))
		} else {
			// Clear any previous temporary override
			hcpo.tempOverrideLLM = nil
		}
		if options.TempOverrideLLM2 != nil && options.TempOverrideLLM2.Provider != "" && options.TempOverrideLLM2.ModelID != "" {
			hcpo.tempOverrideLLM2 = options.TempOverrideLLM2
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temporary execution agent LLM override 2 set: %s/%s (will be used on second attempt if tempLLM1 fails)", options.TempOverrideLLM2.Provider, options.TempOverrideLLM2.ModelID))
		} else {
			// Clear any previous temporary override
			hcpo.tempOverrideLLM2 = nil
		}

		// Store fallback to original LLM on failure flag
		hcpo.fallbackToOriginalLLMOnFailure = options.FallbackToOriginalLLMOnFailure
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Fallback to original LLM on validation failure flag: %v (from ExecutionOptions: %v)", hcpo.fallbackToOriginalLLMOnFailure, options.FallbackToOriginalLLMOnFailure))
		if hcpo.fallbackToOriginalLLMOnFailure {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Fallback to original LLM on validation failure enabled - will use original LLM instead of temp override when validation fails"))
		}

		// Store save validation responses flag (always enabled)
		hcpo.saveValidationResponses = true
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Save validation responses enabled - validation responses will be saved to workspace"))

	} else {
		// Clear temporary overrides when options are cleared
		hcpo.tempOverrideLLM = nil
		hcpo.tempOverrideLLM2 = nil
		hcpo.fallbackToOriginalLLMOnFailure = false
		hcpo.saveValidationResponses = true // Default to true when no options provided
	}
}

// GetExecutionOptions returns the current execution options
func (hcpo *StepBasedWorkflowOrchestrator) GetExecutionOptions() *ExecutionOptions {
	return hcpo.executionOptions
}

// buildExecutionContext creates an ExecutionContext from current controller state
// This should be called once at execution start to create an immutable context
func (hcpo *StepBasedWorkflowOrchestrator) buildExecutionContext() *ExecutionContext {
	execCtx := &ExecutionContext{
		SkipHumanInput:     hcpo.skipHumanInput,
		FastExecuteMode:    hcpo.fastExecuteMode,
		FastExecuteEndStep: hcpo.fastExecuteEndStep,
		RunSingleStepOnly:  hcpo.runSingleStepOnly,
		SingleStepTarget:   hcpo.singleStepTarget,
		IsEvaluationMode:   hcpo.isEvaluationMode,
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Built ExecutionContext: skipHumanInput=%v, fastExecuteMode=%v, fastExecuteEndStep=%d, runSingleStepOnly=%v, singleStepTarget=%d, isEvaluationMode=%v", execCtx.SkipHumanInput, execCtx.FastExecuteMode, execCtx.FastExecuteEndStep, execCtx.RunSingleStepOnly, execCtx.SingleStepTarget, execCtx.IsEvaluationMode))

	return execCtx
}

// HasExecutionOptions checks if execution options are set
func (hcpo *StepBasedWorkflowOrchestrator) HasExecutionOptions() bool {
	return hcpo.executionOptions != nil
}

// formatValidationResponseForTemplate formats validation response data for inclusion in template variables
// This makes validation output available to the execution agent via ValidationFeedback template variable
func (hcpo *StepBasedWorkflowOrchestrator) formatValidationResponseForTemplate(validationResponse *ValidationResponse, context string) string {
	if validationResponse == nil {
		return ""
	}

	var parts []string

	// Add context header
	if context != "" {
		parts = append(parts, fmt.Sprintf("## %s", context))
	}

	// Add reasoning
	if validationResponse.Reasoning != "" {
		parts = append(parts, fmt.Sprintf("**Reasoning**: %s", validationResponse.Reasoning))
	}

	// Add loop-specific information if present
	if validationResponse.LoopReasoning != "" {
		parts = append(parts, fmt.Sprintf("**Loop Condition Status**: %v", validationResponse.LoopConditionMet))
		parts = append(parts, fmt.Sprintf("**Loop Reasoning**: %s", validationResponse.LoopReasoning))
	}

	// Add execution status
	if validationResponse.ExecutionStatus != "" {
		parts = append(parts, fmt.Sprintf("**Execution Status**: %s", validationResponse.ExecutionStatus))
	}

	// Add success criteria status
	parts = append(parts, fmt.Sprintf("**Success Criteria Met**: %v", validationResponse.IsSuccessCriteriaMet))

	// Add feedback items
	if len(validationResponse.Feedback) > 0 {
		feedbackParts := []string{"**Feedback**: "}
		for _, fb := range validationResponse.Feedback {
			feedbackParts = append(feedbackParts, fmt.Sprintf("- [%s] %s: %s", fb.Severity, fb.Type, fb.Description))
		}
		parts = append(parts, strings.Join(feedbackParts, "\n"))
	}

	return strings.Join(parts, "\n\n")
}

// conversation history formatting moved to shared.FormatConversationHistory
