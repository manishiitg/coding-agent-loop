package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchestratorllm "mcp-agent-builder-go/agent_go/pkg/orchestrator/llm"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerOrchestrator manages simplified human-controlled todo planning process
// - Single execution (no iterations)
// - No validation phase
// - No critique phase
// - No cleanup phase
// - Simple direct planning approach
// - Always includes independent steps extraction for parallel execution
// - NEW: Includes learning phase after each step execution and validation
type HumanControlledTodoPlannerOrchestrator struct {
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

	// Learning detail level preference (set once before execution, used for all learning phases)
	learningDetailLevel string // "exact" or "general"

	// Approved plan storage
	approvedPlan *PlanningResponse // Store approved plan

	// Run folder management
	selectedRunFolder string // Selected run folder name (e.g., "iteration-1", "iteration-2")
	selectedRunMode   string // Selected run mode (e.g., "use_same_run", "create_new_runs_always")

	// Frontend-provided execution options (when provided, skips interactive prompts)
	executionOptions *ExecutionOptions

	// Conditional Agent for conditional step evaluation (default/orchestrator-level)
	conditionalAgent *HumanControlledTodoPlannerConditionalAgent

	// Cache for step-specific conditional agent instances (key: step ID)
	stepConditionalAgentCache map[string]*HumanControlledTodoPlannerConditionalAgent
	stepConditionalAgentMutex sync.RWMutex

	// Preset-level agent defaults (used when step config doesn't specify)
	presetExecutionLLM          *AgentLLMConfig // Default for execution agents
	presetValidationLLM         *AgentLLMConfig // Default for validation agents
	presetLearningLLM           *AgentLLMConfig // Default for learning agents
	presetLearningReadingLLM    *AgentLLMConfig // Default for learning reading agent
	presetPlanningLLM           *AgentLLMConfig // Default for planning agent
	presetVariableExtractionLLM *AgentLLMConfig // Default for variable extraction agent
	presetAnonymizationLLM      *AgentLLMConfig // Default for anonymization agent
	presetPlanImprovementLLM    *AgentLLMConfig // Default for plan improvement agent

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
}

// NewHumanControlledTodoPlannerOrchestrator creates a new human-controlled todo planner orchestrator
func NewHumanControlledTodoPlannerOrchestrator(
	provider string,
	model string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	selectedTools []string, // NEW parameter
	useCodeExecutionMode bool, // NEW parameter
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
	presetLearningReadingLLM *AgentLLMConfig, // Optional preset default for learning reading agent
	presetPlanningLLM *AgentLLMConfig, // Optional preset default for planning agent
	presetVariableExtractionLLM *AgentLLMConfig, // Optional preset default for variable extraction agent
	presetAnonymizationLLM *AgentLLMConfig, // Optional preset default for anonymization agent
	presetPlanImprovementLLM *AgentLLMConfig, // Optional preset default for plan improvement agent
) (*HumanControlledTodoPlannerOrchestrator, error) {

	// Create base workflow orchestrator
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
		selectedTools,        // Pass through actual selected tools
		useCodeExecutionMode, // NEW: Pass code execution mode
		llmConfig,
		maxTurns,
		customTools,
		customToolExecutors,
		toolCategories, // NEW: Pass category map
	)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to create base orchestrator: %w", err), nil)
	}

	// Create ConditionalAgent for conditional step evaluation
	// Get LLM config from orchestrator to preserve API keys from frontend
	orchestratorLLMConfig := baseOrchestrator.GetLLMConfig()
	conditionalAgentConfig := &agents.OrchestratorAgentConfig{
		Provider:             provider,
		Model:                model,
		Temperature:          temperature,
		MaxRetries:           3,
		ServerNames:          selectedServers,      // Pass servers for agent
		SelectedTools:        selectedTools,        // Pass tools for agent
		UseCodeExecutionMode: useCodeExecutionMode, // Pass code execution mode
		Mode:                 agents.AgentMode(agentMode),
		MaxTurns:             maxTurns,
		MCPConfigPath:        mcpConfigPath,
	}
	// Preserve API keys from orchestrator LLM config (sent from frontend)
	if orchestratorLLMConfig != nil && orchestratorLLMConfig.APIKeys != nil {
		conditionalAgentConfig.APIKeys = &agents.AgentAPIKeys{
			OpenRouter: orchestratorLLMConfig.APIKeys.OpenRouter,
			OpenAI:     orchestratorLLMConfig.APIKeys.OpenAI,
			Anthropic:  orchestratorLLMConfig.APIKeys.Anthropic,
			Vertex:     orchestratorLLMConfig.APIKeys.Vertex,
		}
		if orchestratorLLMConfig.APIKeys.Bedrock != nil {
			conditionalAgentConfig.APIKeys.Bedrock = &agents.BedrockAgentConfig{
				Region: orchestratorLLMConfig.APIKeys.Bedrock.Region,
			}
		}
		logger.Info(fmt.Sprintf("🔑 Preserved API keys for conditional agent from orchestrator config"))
	}
	// Create default conditional agent using factory pattern (same as execution agent)
	// Use CreateAndSetupStandardAgentWithConfig to ensure proper initialization, event bridge connection, and tool registration
	// Pass all workspace tools for default agent (no step config filtering)
	var conditionalAgent *HumanControlledTodoPlannerConditionalAgent
	conditionalAgentInterface, err := baseOrchestrator.CreateAndSetupStandardAgentWithConfig(
		context.Background(),
		conditionalAgentConfig,
		"conditional_evaluation", // phase
		0,                        // step (default agent, will be updated per-step)
		0,                        // iteration
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerConditionalAgent(cfg, logger, tracer, eventBridge)
		},
		customTools,         // Pass all workspace tools (default agent, no step config filtering)
		customToolExecutors, // Pass all workspace tool executors
		false,               // Don't overwrite system prompt - conditional agent manages its own prompt
	)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to create default conditional agent: %w", err), nil)
	}

	// Type assert to conditional agent
	var ok bool
	conditionalAgent, ok = conditionalAgentInterface.(*HumanControlledTodoPlannerConditionalAgent)
	if !ok {
		return nil, fmt.Errorf("factory returned wrong agent type for default conditional agent")
	}

	hcpo := &HumanControlledTodoPlannerOrchestrator{
		BaseOrchestrator:            baseOrchestrator,
		sessionID:                   fmt.Sprintf("session_%d", time.Now().UnixNano()),
		workflowID:                  fmt.Sprintf("workflow_%d", time.Now().UnixNano()),
		conditionalAgent:            conditionalAgent,
		stepConditionalAgentCache:   make(map[string]*HumanControlledTodoPlannerConditionalAgent),
		presetExecutionLLM:          presetExecutionLLM,
		presetValidationLLM:         presetValidationLLM,
		presetLearningLLM:           presetLearningLLM,
		presetLearningReadingLLM:    presetLearningReadingLLM,
		presetPlanningLLM:           presetPlanningLLM,
		presetVariableExtractionLLM: presetVariableExtractionLLM,
		presetAnonymizationLLM:      presetAnonymizationLLM,
		presetPlanImprovementLLM:    presetPlanImprovementLLM,
		saveValidationResponses:     true, // Default to true (save validation responses by default)
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) getConditionalAgentForStep(ctx context.Context, step TodoStep, stepIndex int, agentName, phase string) *HumanControlledTodoPlannerConditionalAgent {
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", step.Title)
	}

	// Check if step has step-specific config (conditional LLM or code execution mode)
	hasStepSpecificConfig := step.AgentConfigs != nil && (step.AgentConfigs.ConditionalLLM != nil || step.AgentConfigs.UseCodeExecutionMode != nil)

	if hasStepSpecificConfig {
		// Determine code execution mode for cache key
		var isCodeExecutionMode bool
		if step.AgentConfigs != nil && step.AgentConfigs.UseCodeExecutionMode != nil {
			isCodeExecutionMode = *step.AgentConfigs.UseCodeExecutionMode
		} else {
			isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		}

		// Include code execution mode in cache key to avoid using wrong cached agent
		cacheKey := fmt.Sprintf("%s-codeexec-%v", stepID, isCodeExecutionMode)

		// Check cache first
		hcpo.stepConditionalAgentMutex.RLock()
		cachedAgent, exists := hcpo.stepConditionalAgentCache[cacheKey]
		hcpo.stepConditionalAgentMutex.RUnlock()

		if exists && cachedAgent != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using cached step-specific conditional agent for step '%s' (ID: %s, code exec: %v)", step.Title, stepID, isCodeExecutionMode))
			return cachedAgent
		}

		// Create LLM config: use conditional LLM if specified, otherwise use execution LLM
		var llmConfig *orchestrator.LLMConfig
		orchestratorLLMConfig := hcpo.GetLLMConfig()

		if step.AgentConfigs.ConditionalLLM != nil {
			conditionalLLMConfig := step.AgentConfigs.ConditionalLLM
			llmConfig = &orchestrator.LLMConfig{
				Provider:       conditionalLLMConfig.Provider,
				ModelID:        conditionalLLMConfig.ModelID,
				FallbackModels: []string{},
				APIKeys:        orchestratorLLMConfig.APIKeys,
			}
		} else if step.AgentConfigs.ExecutionLLM != nil && step.AgentConfigs.ExecutionLLM.Provider != "" && step.AgentConfigs.ExecutionLLM.ModelID != "" {
			executionLLMConfig := step.AgentConfigs.ExecutionLLM
			llmConfig = &orchestrator.LLMConfig{
				Provider:       executionLLMConfig.Provider,
				ModelID:        executionLLMConfig.ModelID,
				FallbackModels: []string{},
				APIKeys:        orchestratorLLMConfig.APIKeys,
			}
		} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
			llmConfig = &orchestrator.LLMConfig{
				Provider:       hcpo.presetExecutionLLM.Provider,
				ModelID:        hcpo.presetExecutionLLM.ModelID,
				FallbackModels: []string{},
				APIKeys:        orchestratorLLMConfig.APIKeys,
			}
		} else {
			llmConfig = orchestratorLLMConfig
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

		// Use factory method - this handles initialization, event bridge connection, and tool registration
		agent, err := hcpo.createConditionalAgent(
			ctx,
			actualPhase,       // phase (from parameter)
			stepIndex,         // step index
			0,                 // iteration
			actualAgentName,   // agent name (from parameter)
			step.AgentConfigs, // step config (includes UseCodeExecutionMode)
			llmConfig,         // conditional LLM config
		)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create step-specific conditional agent for step '%s': %v, falling back to default", step.Title, err))
			return hcpo.conditionalAgent // Fallback to default
		}

		// Type assert to conditional agent
		stepConditionalAgent, ok := agent.(*HumanControlledTodoPlannerConditionalAgent)
		if !ok {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Factory returned wrong agent type for step '%s', falling back to default", step.Title))
			return hcpo.conditionalAgent // Fallback to default
		}

		// Cache the conditional agent with code execution mode in key
		hcpo.stepConditionalAgentMutex.Lock()
		hcpo.stepConditionalAgentCache[cacheKey] = stepConditionalAgent
		hcpo.stepConditionalAgentMutex.Unlock()

		if step.AgentConfigs.ConditionalLLM != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Created step-specific conditional agent for step '%s' (ID: %s, code exec: %v): %s/%s", step.Title, stepID, isCodeExecutionMode, step.AgentConfigs.ConditionalLLM.Provider, step.AgentConfigs.ConditionalLLM.ModelID))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Created step-specific conditional agent for step '%s' (ID: %s, code exec: %v): using execution LLM", step.Title, stepID, isCodeExecutionMode))
		}
		return stepConditionalAgent
	}

	// Use default conditional agent
	return hcpo.conditionalAgent
}

// getConditionalLLMForStep returns the ConditionalLLM to use for a specific step
// Priority: step config conditional_llm > default LLM config
// Uses simpler ConditionalLLM instead of full agent (no MCP tools needed for decision evaluation)
func (hcpo *HumanControlledTodoPlannerOrchestrator) getConditionalLLMForStep(step TodoStep, stepIndex int) (*orchestratorllm.ConditionalLLM, error) {
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("event bridge is required for conditional LLM")
	}

	logger := hcpo.GetLogger()
	tracer := hcpo.GetTracer()

	// Determine LLM config: Priority: step execution_llm > preset execution_llm > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()

	if step.AgentConfigs != nil && step.AgentConfigs.ExecutionLLM != nil && step.AgentConfigs.ExecutionLLM.Provider != "" && step.AgentConfigs.ExecutionLLM.ModelID != "" {
		// Use step-specific execution LLM config
		executionLLMConfig := step.AgentConfigs.ExecutionLLM
		llmConfig = &orchestrator.LLMConfig{
			Provider:       executionLLMConfig.Provider,
			ModelID:        executionLLMConfig.ModelID,
			FallbackModels: []string{},
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution LLM for conditional LLM: %s/%s", executionLLMConfig.Provider, executionLLMConfig.ModelID))
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		// Use preset execution LLM as fallback
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetExecutionLLM.Provider,
			ModelID:        hcpo.presetExecutionLLM.ModelID,
			FallbackModels: []string{},
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default execution LLM for conditional LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
	} else {
		// Use orchestrator default LLM config
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default conditional LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
	}

	// Convert to OrchestratorAgentConfig
	agentConfig := &agents.OrchestratorAgentConfig{
		Provider:    llmConfig.Provider,
		Model:       llmConfig.ModelID,
		Temperature: 0.0, // Use deterministic temperature for conditional decisions
		MaxRetries:  3,
	}
	// Convert APIKeys from orchestrator.APIKeys to agents.AgentAPIKeys
	if llmConfig.APIKeys != nil {
		agentConfig.APIKeys = &agents.AgentAPIKeys{
			OpenRouter: llmConfig.APIKeys.OpenRouter,
			OpenAI:     llmConfig.APIKeys.OpenAI,
			Anthropic:  llmConfig.APIKeys.Anthropic,
			Vertex:     llmConfig.APIKeys.Vertex,
		}
		if llmConfig.APIKeys.Bedrock != nil {
			agentConfig.APIKeys.Bedrock = &agents.BedrockAgentConfig{
				Region: llmConfig.APIKeys.Bedrock.Region,
			}
		}
	}

	// Create ConditionalLLM using helper function
	conditionalLLM, err := orchestratorllm.CreateConditionalLLMWithEventBridge(
		agentConfig,
		eventBridge,
		logger,
		tracer,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional LLM: %w", err)
	}

	return conditionalLLM, nil
}

// CreateTodoList orchestrates the human-controlled todo planning process
// - Single execution (no iterations)
// - Includes validation phase (runs later in the workflow)
// - Includes critique phase during writer validation loop
// - Skips cleanup phase
// - Simple direct planning approach
// - NEW: Includes human approval loop with iterative plan refinement
func (hcpo *HumanControlledTodoPlannerOrchestrator) CreateTodoList(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🚀 Starting human-controlled todo planning for objective: %s", objective))

	// Set objective and workspace path directly
	// WorkspacePath is the base workspace path (no subdirectory)
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

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
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No variables.json found - planning agent can extract variables if needed"))
		// Use original objective (no templating)
		templatedObjective = hcpo.GetObjective()
	}

	// Check if plan.json exists - REQUIRED for execution
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to check for existing plan: %w", err), nil)
	}
	if !planExists {
		return "", fmt.Errorf(fmt.Sprintf("plan.json not found at %s - planning must be run first as a separate phase", planPath), nil)
	}

	// Plan exists - use it
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Found existing plan.json at %s with %d steps", planPath, len(existingPlan.Steps)))

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
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [VARIABLE LOADING] Requested group ID: %s", requestedGroupID))

		// Log available groups for debugging
		availableGroupIDs := make([]string, len(hcpo.variablesManifest.Groups))
		for i, g := range hcpo.variablesManifest.Groups {
			availableGroupIDs[i] = g.GroupID
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [VARIABLE LOADING] Available groups in manifest: %v", availableGroupIDs))

		variableValues = hcpo.variablesManifest.GetVariableValues(requestedGroupID)
		if variableValues == nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [VARIABLE LOADING] Group %s not found in manifest, falling back to LoadVariableValues", requestedGroupID))
			var err error
			variableValues, err = LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [VARIABLE LOADING] Failed to load variable values: %w", err))
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
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [VARIABLE LOADING] No specific group selected, using default LoadVariableValues"))
		var err error
		variableValues, err = LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [VARIABLE LOADING] Failed to load variable values: %w", err))
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

	// Convert existing plan to TodoStep format for execution
	breakdownSteps, err := hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to convert existing plan steps: %w", err), nil)
	}
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Converted existing plan: %d steps extracted", len(breakdownSteps)))

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
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Using frontend-provided execution options"))

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
			return "", fmt.Errorf(fmt.Sprintf("failed to resolve run folder with frontend options: %w", err), nil)
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
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to get user decision for run mode: %w, defaulting to 'use_same_run'", err))
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
			return "", fmt.Errorf(fmt.Sprintf("failed to resolve run folder with selected run mode: %w", err), nil)
		}
		hcpo.selectedRunFolder = selectedRunFolder
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Resolved run folder with selected run mode: %s", selectedRunFolder))
		// Set iteration folder for real-time token persistence
		hcpo.SetIterationFolder(selectedRunFolder)
	}

	// EARLY PROGRESS CHECK: Load progress from the selected run folder
	// Note: We no longer check if all steps are completed - execution will proceed regardless
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Early progress check: Loading progress from folder: %s", selectedRunFolder))
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 DEBUG: breakdownSteps count before early progress check: %d", len(breakdownSteps)))

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
		err = nil // Reset err since earlyProgress was successfully loaded earlier
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Using early progress (avoided reload)"))
	} else {
		// Check if there's existing progress (only if we haven't already handled plan change)
		if !planChangeHandled {
			existingProgress, err = hcpo.loadStepProgress(ctx)
			if err != nil {
				// File doesn't exist - this is normal for first run, log and continue
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No existing progress file found (this is normal for first run), will start fresh execution"))
				existingProgress = nil
				err = nil // Reset err to allow execution to proceed
			}
		} else {
			// Plan change was already handled, don't reload to avoid duplicate prompts
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Plan change already handled, skipping reload to avoid duplicate prompts"))
			existingProgress = nil
			err = nil
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
		startFromStep = 0
	} else {
		// Case 2: Resume from step X
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Resuming from step"))

		// Load existing progress if available
		if existingProgress == nil {
			existingProgress, err = hcpo.loadStepProgress(ctx)
			if err != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No existing progress file found, will start from step specified by frontend"))
				existingProgress = nil
				err = nil
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
					hcpo.GetLogger().Info(fmt.Sprintf("📋 Using execution strategy: %s", execOpts.ExecutionStrategy))
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
				// No execOpts - use next incomplete step as fallback
				if nextIncompleteStep > 0 {
					startFromStep = nextIncompleteStep - 1
				}
			}
		} else {
			// No existing progress - use resume_from_step from frontend if available
			if execOpts != nil && execOpts.ResumeFromStep > 0 {
				startFromStep = execOpts.ResumeFromStep - 1
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

	// Initialize progress tracking if not already loaded
	// Only initialize fresh progress if we're NOT trying to preserve existing progress
	// (i.e., if planChangeHandled = false, this is a first run and we should initialize)
	if existingProgress == nil && !planChangeHandled {
		// Initialize and save fresh progress file (first run scenario)
		if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to initialize fresh progress: %w", err))
			// Continue anyway with in-memory progress
			existingProgress = &StepProgress{
				CompletedStepIndices:     []int{},
				TotalSteps:               len(breakdownSteps),
				BranchSteps:              make(map[int]BranchStepProgress),
				DecisionEvaluationCounts: make(DecisionEvaluationCount),
			}
		} else {
			// Create in-memory progress object matching what was saved
			existingProgress = &StepProgress{
				CompletedStepIndices:     []int{},
				TotalSteps:               len(breakdownSteps),
				LastUpdated:              time.Now(),
				BranchSteps:              make(map[int]BranchStepProgress),
				DecisionEvaluationCounts: make(DecisionEvaluationCount),
			}
		}
	} else if existingProgress != nil && !isResuming {
		// Safety check: if we're starting fresh but existingProgress exists (shouldn't happen, but handle it)
		// Reset DecisionEvaluationCounts to ensure old counts don't persist
		if existingProgress.DecisionEvaluationCounts != nil && len(existingProgress.DecisionEvaluationCounts) > 0 {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Starting fresh but found existing DecisionEvaluationCounts with %d entries - resetting to prevent infinite loop errors", len(existingProgress.DecisionEvaluationCounts)))
			existingProgress.DecisionEvaluationCounts = make(DecisionEvaluationCount)
			// Save the reset progress
			if err := hcpo.saveStepProgress(ctx, existingProgress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save reset progress: %w", err))
			}
		}
	} else if existingProgress == nil && planChangeHandled {
		// Plan change detected but progress file doesn't exist - preserve mode
		// Don't initialize fresh progress, just continue with nil and let execution handle it
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Preserving progress mode: progress file doesn't exist, continuing without initializing fresh progress"))
		// Create minimal in-memory progress for execution to work with
		existingProgress = &StepProgress{
			CompletedStepIndices:     []int{},
			TotalSteps:               len(breakdownSteps),
			BranchSteps:              make(map[int]BranchStepProgress),
			DecisionEvaluationCounts: make(DecisionEvaluationCount),
		}
	}

	// Build execution context once from current controller state
	execCtx := hcpo.buildExecutionContext()

	// Check if batch execution should be used (multiple variable groups enabled)
	if hcpo.shouldUseBatchExecution() {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Multiple variable groups detected, using batch execution mode"))
		batchResult, err := hcpo.runBatchExecution(ctx, breakdownSteps, 1, execCtx)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("batch execution failed: %w", err), nil)
		}
		if !batchResult.Success {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Batch execution completed with %d failed groups", batchResult.FailedGroups))
		}
	} else {
		// Single group or no groups - use standard execution
		err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1, existingProgress, startFromStep, execCtx)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("execution phase failed: %w", err), nil)
		}
	}

	duration := time.Since(hcpo.GetStartTime())
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Human-controlled todo planning completed in %v", duration))

	return "Todo planning complete.", nil
}

// executeConditionalStep executes a conditional step by evaluating the condition and executing the chosen branch
// depth: current nesting depth (0 = main plan, 1 = first level conditional, 2 = second level conditional)

// requestHumanFeedback requests human feedback after validation and blocks until user responds
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestHumanFeedback(ctx context.Context, currentStep, totalSteps int, validationResult string) (bool, string, error) {
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

func (hcpo *HumanControlledTodoPlannerOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate that no options are provided since this orchestrator doesn't use them
	if len(options) > 0 {
		return "", fmt.Errorf(fmt.Sprintf("human-controlled todo planner orchestrator does not accept options"), nil)
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf(fmt.Sprintf("workspace path is required"), nil)
	}

	// Call the existing CreateTodoList method
	return hcpo.CreateTodoList(ctx, objective, workspacePath)
}

// GetType returns the orchestrator type
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetType() string {
	return "human_controlled_todo_planner"
}

// Helper methods for human feedback tracking

// getSessionID returns the session ID for this orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) getSessionID() string {
	return hcpo.sessionID
}

// getWorkflowID returns the workflow ID for this orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) getWorkflowID() string {
	return hcpo.workflowID
}

// SetFastExecuteMode sets the fast execute mode and end step
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetFastExecuteMode(enabled bool, endStep int) {
	hcpo.fastExecuteMode = enabled
	hcpo.fastExecuteEndStep = endStep
}

// SetRunSingleStepMode sets the single step execution mode
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetRunSingleStepMode(enabled bool, stepIndex int) {
	hcpo.runSingleStepOnly = enabled
	hcpo.singleStepTarget = stepIndex
}

// GetLearningDetailLevel returns the stored learning detail level preference
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetLearningDetailLevel() string {
	if hcpo.learningDetailLevel == "" {
		return "exact" // Default
	}
	return hcpo.learningDetailLevel
}

// SetLearningDetailLevel sets the learning detail level preference
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetLearningDetailLevel(level string) {
	hcpo.learningDetailLevel = level
}

// IsFastExecuteStep checks if a step should be executed in fast mode
func (hcpo *HumanControlledTodoPlannerOrchestrator) IsFastExecuteStep(stepIndex int) bool {
	return hcpo.fastExecuteMode && stepIndex <= hcpo.fastExecuteEndStep
}

// SetSkipHumanInput sets the skip human input mode (runs learning but skips human feedback)
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetSkipHumanInput(enabled bool) {
	hcpo.skipHumanInput = enabled
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 SetSkipHumanInput called with value: %v", enabled))
}

// IsSkipHumanInput checks if human feedback should be skipped
func (hcpo *HumanControlledTodoPlannerOrchestrator) IsSkipHumanInput() bool {
	return hcpo.skipHumanInput
}

// SetExecutionOptions sets the execution options from frontend
// When set, backend will use these options instead of asking interactively
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetExecutionOptions(options *ExecutionOptions) {
	hcpo.executionOptions = options
	if options != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Execution options set from frontend: run_mode=%s, strategy=%s, run_folder=%s", options.RunMode, options.ExecutionStrategy, options.SelectedRunFolder))

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

		// Store save validation responses flag (frontend always sends this value)
		hcpo.saveValidationResponses = options.SaveValidationResponses
		if !hcpo.saveValidationResponses {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Save validation responses disabled - validation responses will not be saved to workspace"))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Save validation responses enabled - validation responses will be saved to workspace"))
		}
	} else {
		// Clear temporary overrides when options are cleared
		hcpo.tempOverrideLLM = nil
		hcpo.tempOverrideLLM2 = nil
		hcpo.fallbackToOriginalLLMOnFailure = false
		hcpo.saveValidationResponses = true // Default to true when no options provided
	}
}

// GetExecutionOptions returns the current execution options
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetExecutionOptions() *ExecutionOptions {
	return hcpo.executionOptions
}

// buildExecutionContext creates an ExecutionContext from current controller state
// This should be called once at execution start to create an immutable context
func (hcpo *HumanControlledTodoPlannerOrchestrator) buildExecutionContext() *ExecutionContext {
	execCtx := &ExecutionContext{
		SkipHumanInput:     hcpo.skipHumanInput,
		FastExecuteMode:    hcpo.fastExecuteMode,
		FastExecuteEndStep: hcpo.fastExecuteEndStep,
		RunSingleStepOnly:  hcpo.runSingleStepOnly,
		SingleStepTarget:   hcpo.singleStepTarget,
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Built ExecutionContext: skipHumanInput=%v, fastExecuteMode=%v, fastExecuteEndStep=%d, runSingleStepOnly=%v, singleStepTarget=%d", execCtx.SkipHumanInput, execCtx.FastExecuteMode, execCtx.FastExecuteEndStep, execCtx.RunSingleStepOnly, execCtx.SingleStepTarget))

	return execCtx
}

// HasExecutionOptions checks if execution options are set
func (hcpo *HumanControlledTodoPlannerOrchestrator) HasExecutionOptions() bool {
	return hcpo.executionOptions != nil
}

// formatValidationResponseForTemplate formats validation response data for inclusion in template variables
// This makes validation output available to the execution agent via ValidationFeedback template variable
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatValidationResponseForTemplate(validationResponse *ValidationResponse, context string) string {
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
