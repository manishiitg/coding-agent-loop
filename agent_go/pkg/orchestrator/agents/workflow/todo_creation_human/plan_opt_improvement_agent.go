package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerPlanImprovementTemplate holds template variables for plan improvement prompts
type HumanControlledTodoPlannerPlanImprovementTemplate struct {
	WorkspacePath           string
	RunPathRelative         string
	RunWorkspacePath        string
	PlanJSON                string
	ExecutionResultsSummary string
	AllowedPaths            string
}

// HumanControlledTodoPlannerPlanImprovementAgent analyzes execution results and provides feedback for plan improvement
type HumanControlledTodoPlannerPlanImprovementAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator // Reference to base orchestrator for RequestHumanFeedback
}

// NewHumanControlledTodoPlannerPlanImprovementAgent creates a new plan improvement agent
func NewHumanControlledTodoPlannerPlanImprovementAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *HumanControlledTodoPlannerPlanImprovementAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanImprovementAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerPlanImprovementAgent{
		BaseOrchestratorAgent: baseAgent,
		baseOrchestrator:      baseOrchestrator,
	}
}

// PlanImprovementManager manages plan improvement agent creation independently from controller
type PlanImprovementManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Plan improvement LLM config (optional preset)
	presetPlanImprovementLLM *AgentLLMConfig
	// Learning LLM config (fallback for plan improvement if presetPlanImprovementLLM not set)
	presetLearningLLM *AgentLLMConfig

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string
}

// NewPlanImprovementManager creates a new PlanImprovementManager
func NewPlanImprovementManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetPlanImprovementLLM *AgentLLMConfig,
	presetLearningLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *PlanImprovementManager {
	return &PlanImprovementManager{
		BaseOrchestrator:         baseOrchestrator,
		presetPlanImprovementLLM: presetPlanImprovementLLM,
		presetLearningLLM:        presetLearningLLM,
		sessionID:                sessionID,
		workflowID:               workflowID,
	}
}

// createPlanImprovementAgent creates and sets up a plan improvement agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
// workspacePath: current workspace path (may be a subdirectory like workspace/runs/iteration-1)
// originalWorkspacePath: original workspace root (for accessing learnings/ and planning/ folders)
func (pim *PlanImprovementManager) createPlanImprovementAgent(ctx context.Context, workspacePath string, originalWorkspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read-only access to specific folders only
	// Plan improvement agent needs read access to:
	// - Current workspace: for execution results and logs (execution/, logs/)
	// - Original workspace learnings/: for learnings analysis (shared and step-specific learnings)
	// - Original workspace planning/: for reading plan.json
	currentWorkspacePath := workspacePath
	learningsPath := fmt.Sprintf("%s/learnings", originalWorkspacePath)
	planningPath := fmt.Sprintf("%s/planning", originalWorkspacePath)

	// Build read paths list - explicit read-only access
	// Use current workspace for execution/logs, original workspace for learnings/planning
	readPaths := []string{
		currentWorkspacePath, // Read execution results and logs from current workspace
		learningsPath,        // Read learnings from original workspace (shared and step-specific)
		planningPath,         // Read plan.json from original workspace
	}

	// Step-specific learnings are always enabled - folders are at workspace root
	// The learningsPath already covers these since they're under learnings/
	pim.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings enabled - agent can access step-specific folders in learnings/step-*/ (covered by learnings/ read/write path)"))

	// Write paths: learnings folder for updating learnings, plan modifications via custom tools
	// Plan modifications are done via custom tools (not workspace tools), and the tool executors handle file writing directly
	writePaths := []string{
		learningsPath, // Write access to learnings folder for updating learnings
	}

	// Set folder guard with read and write access
	pim.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	pim.GetLogger().Info(fmt.Sprintf("📊 Setting folder guard for plan improvement agent:"))
	pim.GetLogger().Info(fmt.Sprintf("   ✅ Read paths (%d): %v", len(readPaths), readPaths))
	pim.GetLogger().Info(fmt.Sprintf("   ✅ Write paths (%d): %v (learnings folder for updates, plan updates via custom tools)", len(writePaths), writePaths))
	pim.GetLogger().Info(fmt.Sprintf("   📝 Plan updates are done via custom tools, not workspace tools"))

	// Determine LLM config: Priority: presetPlanImprovementLLM > presetLearningLLM > orchestrator default
	var llmConfigToUse *orchestrator.LLMConfig
	orchestratorLLMConfig := pim.GetLLMConfig()
	if pim.presetPlanImprovementLLM != nil && pim.presetPlanImprovementLLM.Provider != "" && pim.presetPlanImprovementLLM.ModelID != "" {
		// Initialize fallback/cpf/apiKeys with safe defaults
		var fallbackModels []string
		var crossProviderFallback *agents.CrossProviderFallback
		var apiKeys *orchestrator.APIKeys

		// Only copy from orchestratorLLMConfig if it's not nil
		if orchestratorLLMConfig != nil {
			fallbackModels = orchestratorLLMConfig.FallbackModels
			crossProviderFallback = orchestratorLLMConfig.CrossProviderFallback
			apiKeys = orchestratorLLMConfig.APIKeys
		}

		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              pim.presetPlanImprovementLLM.Provider,
			ModelID:               pim.presetPlanImprovementLLM.ModelID,
			FallbackModels:        fallbackModels,        // Preserve fallback models from orchestrator (or nil if orchestrator config is nil)
			CrossProviderFallback: crossProviderFallback, // Preserve cross-provider fallback (or nil if orchestrator config is nil)
			APIKeys:               apiKeys,               // Preserve API keys from orchestrator (or nil if orchestrator config is nil)
		}
		pim.GetLogger().Info(fmt.Sprintf("🔧 Using preset default plan improvement LLM: %s/%s", pim.presetPlanImprovementLLM.Provider, pim.presetPlanImprovementLLM.ModelID))
	} else if pim.presetLearningLLM != nil && pim.presetLearningLLM.Provider != "" && pim.presetLearningLLM.ModelID != "" {
		// Fallback to learning LLM if plan improvement LLM not set
		var fallbackModels []string
		var crossProviderFallback *agents.CrossProviderFallback
		var apiKeys *orchestrator.APIKeys

		// Only copy from orchestratorLLMConfig if it's not nil
		if orchestratorLLMConfig != nil {
			fallbackModels = orchestratorLLMConfig.FallbackModels
			crossProviderFallback = orchestratorLLMConfig.CrossProviderFallback
			apiKeys = orchestratorLLMConfig.APIKeys
		}

		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              pim.presetLearningLLM.Provider,
			ModelID:               pim.presetLearningLLM.ModelID,
			FallbackModels:        fallbackModels,
			CrossProviderFallback: crossProviderFallback,
			APIKeys:               apiKeys,
		}
		pim.GetLogger().Info(fmt.Sprintf("🔧 Using preset learning LLM as fallback for plan improvement: %s/%s", pim.presetLearningLLM.Provider, pim.presetLearningLLM.ModelID))
	} else {
		llmConfigToUse = orchestratorLLMConfig
		pim.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default plan improvement LLM: %s/%s", pim.GetProvider(), pim.GetModel()))
	}

	// Use workspace tools directly - they already include human_feedback (created by createCustomTools in server.go)
	// No need to add human tools separately as they're already combined in WorkspaceTools
	allTools := pim.WorkspaceTools
	allExecutors := pim.WorkspaceToolExecutors

	// Create agent config with the selected LLM config
	config := pim.CreateStandardAgentConfigWithLLM("plan-improvement-agent", 100, agents.OutputFormatStructured, llmConfigToUse)

	// Plan improvement agent doesn't need MCP servers - uses workspace tools only
	config.ServerNames = []string{mcpclient.NoServers}

	// Code execution mode only applies to execution agents, not plan improvement agents
	config.UseCodeExecutionMode = false
	pim.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for plan improvement agent (only execution agents use MCP tools)"))

	// Large output virtual tools are enabled for plan improvement (agent may generate large feedback reports)

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewHumanControlledTodoPlannerPlanImprovementAgent(cfg, logger, tracer, eventBridge, pim.BaseOrchestrator)
	}

	// Use base orchestrator's CreateAndSetupStandardAgentWithConfig to avoid code duplication
	// This handles initialization, event bridge connection, and tool registration
	// Set overwriteSystemPrompt to true for plan improvement agent (replaces default MCP prompt with agent-specific prompt)
	agent, err := pim.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"plan-improvement",
		0, 0, // step, iteration
		createAgentFunc,
		allTools,
		allExecutors,
		true, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup plan improvement agent: %w", err)
	}

	return agent, nil
}

// PlanImprovementOnly runs only the plan improvement phase (standalone, independent from other phases)
// This is a separate workflow phase that can be run independently
func (pim *PlanImprovementManager) PlanImprovementOnly(ctx context.Context, originalWorkspacePath string) (string, error) {
	pim.GetLogger().Info(fmt.Sprintf("📊 Starting standalone plan improvement for workspace: %s", originalWorkspacePath))

	// Store original workspace path
	// Request and validate full path from user via blocking human feedback
	pim.GetLogger().Info(fmt.Sprintf("📊 Requesting full path from user for plan improvement analysis"))
	validatedRunPath, err := pim.requestAndValidateFullPath(ctx, originalWorkspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to get validated path: %w", err)
	}

	// Keep orchestrator workspace rooted at the workspace root.
	// Run artifacts live under workspace root: runs/<iteration>/...
	// This matches how execution/decision step logging writes files (uses workspacePath/runs/<selectedRunFolder>/...).
	pim.SetWorkspacePath(originalWorkspacePath)
	runWorkspacePath := fmt.Sprintf("%s/%s", originalWorkspacePath, validatedRunPath)
	pim.GetLogger().Info(fmt.Sprintf("✅ Using validated run path (relative to workspace root): %s", validatedRunPath))
	pim.GetLogger().Info(fmt.Sprintf("✅ Workspace root set to: %s", originalWorkspacePath))
	pim.GetLogger().Info(fmt.Sprintf("✅ Run workspace path set to: %s", runWorkspacePath))

	// Check if plan.json exists - REQUIRED for plan improvement
	// Plan.json should be in the original workspace, not in the user-provided path
	planPath := fmt.Sprintf("%s/planning/plan.json", originalWorkspacePath)
	planExist, existingPlan, err := pim.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing plan: %w", err)
	}
	if !planExist {
		return "", fmt.Errorf(fmt.Sprintf("plan.json not found at %s - planning must be run first as a separate phase", planPath), nil)
	}

	// Plan exists - use it for plan improvement
	pim.GetLogger().Info(fmt.Sprintf("✅ Found plan.json with %d steps for plan improvement", len(existingPlan.Steps)))

	// Prepare plan JSON for template
	planJSONBytes, err := json.MarshalIndent(existingPlan, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan to JSON: %w", err)
	}

	// Create execution results summary based on the selected run folder.
	// Execution/logs live under runs/<run>/..., while plan/learnings are at workspace root.
	executionResultsSummary := fmt.Sprintf(
		"Workspace root: %s\nSelected run folder: %s\n\nRun folder contains:\n- %s/execution/ - step execution outputs\n- %s/logs/ - validation and execution logs\n\nUse list_workspace_files to explore:\n- Execution result files in %s/execution/\n- Detailed logs in %s/logs/step-X/ including:\n  * validation-{N}.json - validation responses for each validation attempt\n  * execution/execution-attempt-{N}-iteration-{M}.json - execution results with retry/loop information\n  * execution/execution-attempt-{N}-iteration-{M}-conversation.json - full conversation history for each execution attempt\n\nLearnings are stored at workspace root:\n- learnings/\n- learnings/step-X/ (and branch folders like learnings/step-3-true-0/)\n\nPlan is stored at:\n- planning/plan.json",
		originalWorkspacePath,
		validatedRunPath,
		validatedRunPath,
		validatedRunPath,
		validatedRunPath,
		validatedRunPath,
	)

	// Create plan improvement agent with run workspace path (for reading run artifacts)
	// and original workspace path (for reading planning/ and learnings/).
	planImprovementAgent, err := pim.createPlanImprovementAgent(ctx, runWorkspacePath, originalWorkspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to create plan improvement agent: %w", err)
	}

	// Prepare template variables
	// Use workspace root for plan/learnings, and runs/<run> for execution/logs.
	allowedPaths := fmt.Sprintf("['planning/', 'learnings/', '%s/']", validatedRunPath)
	planImprovementTemplateVars := map[string]string{
		// Workspace root (plan/learnings live here)
		"WorkspacePath": originalWorkspacePath,
		// Run information (execution/logs live here)
		"RunPathRelative":         validatedRunPath, // e.g. "runs/iteration-11"
		"RunWorkspacePath":        runWorkspacePath, // absolute path
		"ValidatedRunPath":        validatedRunPath, // backward-compat for prompt/template usage
		"OriginalWorkspacePath":   originalWorkspacePath,
		"PlanJSON":                string(planJSONBytes),
		"ExecutionResultsSummary": executionResultsSummary,
		"AllowedPaths":            allowedPaths,
		"SessionID":               pim.sessionID,
		"WorkflowID":              pim.workflowID,
	}

	// Execute plan improvement agent
	pim.GetLogger().Info(fmt.Sprintf("📊 Executing plan improvement agent..."))
	result, conversationHistory, err := planImprovementAgent.Execute(ctx, planImprovementTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("plan improvement agent execution failed: %w", err)
	}

	pim.GetLogger().Info(fmt.Sprintf("✅ Plan improvement completed successfully"))
	pim.GetLogger().Info(fmt.Sprintf("📊 Plan improvement result: %s", result))

	_ = conversationHistory // Conversation history not used for standalone plan improvement

	return result, nil
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the shared readPlanFromFile helper which acquires planFileMutex for thread-safe access
func (pim *PlanImprovementManager) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {
	pim.GetLogger().Info(fmt.Sprintf("🔍 Checking for existing plan at %s", planPath))

	// Extract workspace path from planPath (planPath is workspacePath/planning/plan.json)
	// readPlanFromFile expects workspacePath and constructs the path internally
	workspacePath := filepath.Dir(filepath.Dir(planPath))

	// Use the shared readPlanFromFile helper which acquires planFileMutex for thread-safe access
	plan, err := readPlanFromFile(ctx, workspacePath, pim.ReadWorkspaceFile)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			pim.GetLogger().Info(fmt.Sprintf("📋 No existing plan found: %v", err))
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	pim.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(plan.Steps)))
	return true, plan, nil
}

// requestAndValidateFullPath asks the user for the path via blocking human feedback and validates it has execution/ folder
// User provides paths like "iteration-11" or "iteration-11/group-7" (relative to runs/)
// Returns the full path relative to original workspace (e.g., "runs/iteration-11" or "runs/iteration-11/group-7")
func (pim *PlanImprovementManager) requestAndValidateFullPath(ctx context.Context, originalWorkspacePath string) (string, error) {
	maxAttempts := 5
	attempt := 0

	for attempt < maxAttempts {
		attempt++

		// Generate unique request ID
		requestID := fmt.Sprintf("plan_improvement_full_path_%d_%d", attempt, time.Now().UnixNano())

		// Ask user for the path
		var promptMessage string
		if attempt == 1 {
			promptMessage = fmt.Sprintf("Please provide the folder path to analyze (relative to runs/ folder).\n\nExamples:\n- 'iteration-11' - to analyze a specific iteration\n- 'iteration-11/group-7' - to analyze a specific group within an iteration\n\nThe path must contain an execution/ folder.\n\nThe workspace root is: %s", originalWorkspacePath)
		} else {
			promptMessage = fmt.Sprintf("The path you provided doesn't exist or doesn't contain an execution/ folder. Please provide a valid path relative to runs/ folder (attempt %d/%d).\n\nExamples: 'iteration-11' or 'iteration-11/group-7'\n\nThe workspace root is: %s", attempt, maxAttempts, originalWorkspacePath)
		}

		// Request human feedback (blocking call)
		approved, userPath, err := pim.RequestHumanFeedback(
			ctx,
			requestID,
			promptMessage,
			"",
			pim.sessionID,
			pim.workflowID,
		)
		if err != nil {
			return "", fmt.Errorf("failed to get path from user: %w", err)
		}

		// If user clicked Approve without providing a path, treat as cancellation
		if approved && strings.TrimSpace(userPath) == "" {
			return "", fmt.Errorf(fmt.Sprintf("user approved without providing a path"), nil)
		}

		// Clean up the path (remove leading/trailing spaces and slashes, remove runs/ prefix if included)
		userPath = strings.TrimSpace(userPath)
		userPath = strings.TrimPrefix(userPath, "runs/")
		userPath = strings.TrimPrefix(userPath, "/")
		userPath = strings.TrimSuffix(userPath, "/")

		if userPath == "" {
			pim.GetLogger().Warn(fmt.Sprintf("⚠️ Empty path provided, asking again"))
			continue
		}

		// Construct full path: runs/{userPath}
		// Examples: runs/iteration-11 or runs/iteration-11/group-7
		fullPath := fmt.Sprintf("%s/runs/%s", originalWorkspacePath, userPath)
		executionPath := fmt.Sprintf("%s/execution", fullPath)

		pim.GetLogger().Info(fmt.Sprintf("🔍 Validating path: %s (full path: %s, checking execution: %s)", userPath, fullPath, executionPath))

		// First check if the base path exists
		_, err = pim.ListWorkspaceFiles(ctx, fullPath)
		if err != nil {
			pim.GetLogger().Warn(fmt.Sprintf("⚠️ Path validation failed: %s does not exist: %v", fullPath, err))
			continue
		}

		// Check if execution/ folder exists in this path
		files, err := pim.ListWorkspaceFiles(ctx, executionPath)
		if err != nil {
			// execution/ folder doesn't exist
			pim.GetLogger().Warn(fmt.Sprintf("⚠️ Path validation failed: %s does not contain an execution/ folder: %v", fullPath, err))
			continue
		}

		// Path exists and has execution/ folder
		pim.GetLogger().Info(fmt.Sprintf("✅ Validated path: runs/%s (found %d items in execution/ folder)", userPath, len(files)))

		// Return the full path relative to original workspace (e.g., "runs/iteration-11")
		return fmt.Sprintf("runs/%s", userPath), nil
	}

	return "", fmt.Errorf(fmt.Sprintf("failed to get valid path after %d attempts", maxAttempts), nil)
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	planJSON := templateVars["PlanJSON"]
	executionResultsSummary := templateVars["ExecutionResultsSummary"]
	validatedRunPath := templateVars["ValidatedRunPath"]
	runPathRelative := templateVars["RunPathRelative"]
	runWorkspacePath := templateVars["RunWorkspacePath"]

	// Provide default allowed paths if not present
	allowedPaths := templateVars["AllowedPaths"]
	if allowedPaths == "" {
		if runPathRelative != "" {
			allowedPaths = fmt.Sprintf("['planning/', 'learnings/', '%s/']", runPathRelative)
		} else {
			allowedPaths = "['planning/', 'learnings/', 'runs/']"
		}
	}

	// Prepare template variables
	planImprovementTemplateVars := map[string]string{
		"WorkspacePath":           workspacePath,
		"RunPathRelative":         runPathRelative,
		"RunWorkspacePath":        runWorkspacePath,
		"PlanJSON":                planJSON,
		"ExecutionResultsSummary": executionResultsSummary,
		"AllowedPaths":            allowedPaths,
		"ValidatedRunPath":        validatedRunPath,
		"SessionID":               templateVars["SessionID"],
		"WorkflowID":              templateVars["WorkflowID"],
	}

	// Create template data for plan improvement
	templateData := HumanControlledTodoPlannerPlanImprovementTemplate{
		WorkspacePath:           workspacePath,
		RunPathRelative:         runPathRelative,
		RunWorkspacePath:        runWorkspacePath,
		PlanJSON:                planJSON,
		ExecutionResultsSummary: executionResultsSummary,
		AllowedPaths:            allowedPaths,
	}

	// Get logger and MCP agent from base agent
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	var logger loggerv2.Logger
	var mcpAgent *mcpagent.Agent
	if baseAgent != nil {
		mcpAgent = baseAgent.Agent()
		if mcpAgent != nil && mcpAgent.Logger != nil {
			logger = mcpAgent.Logger
		}
	}

	if mcpAgent == nil {
		return "", nil, fmt.Errorf(fmt.Sprintf("MCP agent is not initialized"), nil)
	}

	// Get readFile, writeFile, and moveFile functions from base orchestrator
	// We need to access the base orchestrator to get these methods
	// Since agent has baseOrchestrator reference, we can use it
	var readFile func(context.Context, string) (string, error)
	var writeFile func(context.Context, string, string) error
	var moveFile func(context.Context, string, string) error
	if agent.baseOrchestrator != nil {
		readFile = agent.baseOrchestrator.ReadWorkspaceFile
		writeFile = agent.baseOrchestrator.WriteWorkspaceFile
		moveFile = agent.baseOrchestrator.MoveWorkspaceFile
	} else {
		return "", nil, fmt.Errorf(fmt.Sprintf("base orchestrator is not initialized"), nil)
	}

	// Reset changelog session at the start of plan improvement agent execution
	// This ensures all changes during this execution are written to the same changelog file
	resetChangelogSession()

	// Register all plan modification tools using shared function
	if err := registerPlanModificationTools(mcpAgent, workspacePath, logger, readFile, writeFile, moveFile, "plan improvement agent"); err != nil {
		return "", nil, err
	}

	// Generate system prompt and user message separately
	systemPrompt := agent.planImprovementSystemPromptProcessor(planImprovementTemplateVars)
	userMessage := agent.planImprovementUserMessageProcessor(planImprovementTemplateVars)

	// Maximum iterations for plan improvement analysis
	maxIterations := 20
	iteration := 0
	currentResult := ""
	currentConversationHistory := conversationHistory

	// Extract sessionID and workflowID from template vars
	sessionID := templateVars["SessionID"]
	workflowID := templateVars["WorkflowID"]

	// Emit plan improvement started event
	if agent.baseOrchestrator != nil {
		eventBridge := agent.baseOrchestrator.GetContextAwareBridge()
		if eventBridge != nil {
			startedEvent := &events.OrchestratorAgentStartEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				AgentType: "plan-improvement",
				AgentName: "plan-improvement-agent",
				Objective: "Improve plan based on execution results and user feedback",
				InputData: planImprovementTemplateVars,
			}
			eventBridge.HandleEvent(ctx, &events.AgentEvent{
				Type:      events.OrchestratorAgentStart,
				Timestamp: time.Now(),
				Data:      startedEvent,
			})
			if logger != nil {
				logger.Info(fmt.Sprintf("📤 Emitted plan improvement started event"))
			}
		}
	}

	// Main execution loop with blocking human feedback
	for iteration < maxIterations {
		iteration++
		if logger != nil {
			logger.Info(fmt.Sprintf("📊 Plan improvement agent iteration %d/%d", iteration, maxIterations))
		}

		// Create a simple input processor that returns the user message
		inputProcessor := func(map[string]string) string {
			return userMessage
		}

		// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
		result, updatedConversationHistory, err := agent.ExecuteWithTemplateValidation(ctx, planImprovementTemplateVars, inputProcessor, currentConversationHistory, templateData, systemPrompt, true)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedConversationHistory

		// Check if plan modification tools were called in this iteration and emit event immediately
		// This ensures the frontend is notified of plan changes right away, not waiting for agent completion
		if agent.baseOrchestrator != nil {
			// Extract tool calls from this iteration's conversation history
			toolCalls := ExtractToolCallsFromMessages(updatedConversationHistory)
			planUpdateToolCalled := false
			for _, toolName := range toolCalls {
				if IsPlanModificationTool(toolName) || IsStepConfigModificationTool(toolName) {
					planUpdateToolCalled = true
					break
				}
			}

			if planUpdateToolCalled {
				if logger != nil {
					logger.Info(fmt.Sprintf("🔍 [PlanImprovementAgent] Plan modification tool detected in iteration %d, emitting event immediately", iteration))
				}
				CheckAndEmitPlanUpdateEvent(ctx, agent.baseOrchestrator, updatedConversationHistory, workspacePath, readFile)
			}
		}

		// After execution, ask if user wants to continue (blocking feedback)
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			if logger != nil {
				logger.Info(fmt.Sprintf("📊 Plan improvement agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations))
			}

			// Generate unique request ID
			requestID := fmt.Sprintf("plan_improvement_continue_%d_%d", iteration, time.Now().UnixNano())

			// Request human feedback (blocking call)
			approved, feedback, err := agent.baseOrchestrator.RequestHumanFeedback(
				ctx,
				requestID,
				fmt.Sprintf("Plan improvement analysis is complete (iteration %d/%d). Would you like to ask more questions about the plan or request additional improvements?", iteration, maxIterations),
				currentResult,
				sessionID,
				workflowID,
			)
			if err != nil {
				if logger != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to get user feedback: %v", err))
				}
				// Continue without blocking if feedback fails
				break
			}

			// If user clicked Approve button, we're done
			if approved {
				if logger != nil {
					logger.Info(fmt.Sprintf("✅ User approved - plan improvement complete"))
				}
				break
			}

			// User provided feedback/question - always pass it to the agent and continue
			if feedback != "" && strings.TrimSpace(feedback) != "" {
				if logger != nil {
					logger.Info(fmt.Sprintf("📝 User provided feedback: %s", feedback))
				}
				// Use feedback directly as user message for next iteration
				// Note: BaseAgent.Execute() will automatically add it to conversation history
				userMessage = feedback
			} else {
				// No feedback provided but not approved - continue with same message
				if logger != nil {
					logger.Info(fmt.Sprintf("ℹ️ No feedback provided, continuing with same context"))
				}
			}
		} else {
			// Reached max iterations or no base orchestrator
			if logger != nil {
				logger.Info(fmt.Sprintf("📊 Reached maximum iterations (%d) or no base orchestrator, ending conversation", maxIterations))
			}
			break
		}
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("📊 Plan improvement completed after %d iterations", iteration))
	}

	// Check if plan modification tools were called and emit event if needed
	// This ensures the frontend is notified of plan changes
	if logger != nil {
		logger.Info(fmt.Sprintf("🔍 [PlanImprovementAgent] Calling CheckAndEmitPlanUpdateEvent (baseOrchestrator: %v, conversationHistory length: %d)", agent.baseOrchestrator != nil, len(currentConversationHistory)))
	}
	CheckAndEmitPlanUpdateEvent(ctx, agent.baseOrchestrator, currentConversationHistory, workspacePath, readFile)
	if logger != nil {
		logger.Info(fmt.Sprintf("🔍 [PlanImprovementAgent] CheckAndEmitPlanUpdateEvent call completed"))
	}

	// Emit plan improvement completed event
	if agent.baseOrchestrator != nil {
		eventBridge := agent.baseOrchestrator.GetContextAwareBridge()
		if eventBridge != nil {
			completedEvent := &events.OrchestratorAgentEndEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				AgentType: "plan-improvement",
				AgentName: "plan-improvement-agent",
				Objective: "Improve plan based on execution results and user feedback",
				Result:    currentResult,
				Success:   true,
				InputData: planImprovementTemplateVars,
			}
			eventBridge.HandleEvent(ctx, &events.AgentEvent{
				Type:      events.OrchestratorAgentEnd,
				Timestamp: time.Now(),
				Data:      completedEvent,
			})
			if logger != nil {
				logger.Info(fmt.Sprintf("📤 Emitted plan improvement completed event"))
			}
		}
	}

	return currentResult, currentConversationHistory, nil
}

// planImprovementSystemPromptProcessor creates the system prompt for plan improvement
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) planImprovementSystemPromptProcessor(templateVars map[string]string) string {
	runPathRelative := templateVars["RunPathRelative"]
	runWorkspacePath := templateVars["RunWorkspacePath"]

	learningsLocationNote := `
## LEARNING FILES LOCATION

When step-specific learnings are enabled, learning files are stored in step-specific folders:
- Shared learnings: {WorkspacePath}/learnings/
- Regular step learnings: {WorkspacePath}/learnings/step-{X}/
- Branch step learnings: {WorkspacePath}/learnings/step-{parentStep}-{true/false}-{branchIdx}/ (e.g., step-3-true-0/, step-3-false-1/)

Check BOTH shared and step-specific folders (including branch step folders) when analyzing learnings. Use list_workspace_files to discover all step-specific folders recursively.

## WHAT ARE LEARNINGS?

**Learnings are hints and best practices** extracted from previous step executions:
- **Purpose**: Learnings provide guidance to the execution agent (LLM) on how to execute a step
- **Content**: They contain patterns, approaches, code snippets, and lessons learned from past executions
- **Usage**: The execution agent reads learnings before executing a step to understand:
  - What worked well in previous attempts
  - Common patterns or approaches that succeeded
  - Code snippets or commands that were effective
  - Best practices discovered through execution
- **Not executable code**: Learnings are reference material, not scripts that run automatically
- **When analyzing learnings**: Consider whether the learnings align with the current plan steps and if they provide useful guidance for execution

## LEARNINGS FOLDER STRUCTURE (EXECUTION MODE DEPENDENT)

**Learnings folder structure depends on execution mode** (Simple Mode vs Code Execution Mode):

### Simple Mode (MCP tools only):
- **Markdown files**: learnings/step-{X}/*.md - Tool patterns and workflows
- **Python scripts**: learnings/step-{X}/scripts/*.py - Python scripts that worked
- **Shared learnings**: learnings/*.md and learnings/scripts/*.py

### Code Execution Mode (MCP tools + Go code):
- **Markdown files**: learnings/step-{X}/*.md - Tool patterns and workflows
- **Go code**: learnings/step-{X}/code/*.go - Go code patterns that worked
- **Shared learnings**: learnings/*.md and learnings/code/*.go

**How to determine execution mode**:
- Check conversation history in logs/step-X/execution/execution-attempt-{N}-iteration-{M}-conversation.json
- If write_code tool was called → Code Execution Mode (look in code/ subfolder)
- If no write_code tool calls → Simple Mode (look in scripts/ subfolder for Python scripts)

**When analyzing learnings**:
- Check both .md files (always present) AND the appropriate code folder (scripts/ or code/) based on execution mode
- If unsure which mode was used, check conversation history first to determine which subfolder to examine
`

	validatedRunPath := templateVars["ValidatedRunPath"]
	runPathNote := ""
	if runPathRelative != "" || runWorkspacePath != "" || validatedRunPath != "" {
		// Prefer the explicit runPathRelative if provided.
		runPath := runPathRelative
		if runPath == "" {
			runPath = validatedRunPath
		}
		runPathNote = fmt.Sprintf(`
## VALIDATED RUN PATH
The user has specified the run path to analyze: **%s**
Execution results and logs are under: **%s/execution/** and **%s/logs/** (relative to workspace root)
Run workspace absolute path: **%s**
`, runPath, runPath, runPath, runWorkspacePath)
	}

	return `# Plan Improvement Agent

## PURPOSE
Analyze plan.json, execution results, and logs to improve the plan. You can update plan.json directly using plan modification tools, and update learnings files using write_workspace_file, but **ALWAYS get user confirmation via human_feedback FIRST** before making any changes.` + runPathNote + `

## CRITICAL RULE: ALWAYS CONFIRM BEFORE UPDATING
**⚠️ YOU MUST USE human_feedback FIRST before making any plan changes**
- ALWAYS use human_feedback tool FIRST to describe proposed changes and get user approval
- Only after user approval, use plan modification tools to update plan.json
- Never call plan modification tools without first getting confirmation via human_feedback

## FIRST ACTION (MANDATORY)
Use 'human_feedback' to ask: "What would you like to improve in the plan?"
**WAIT** for response before doing anything else.

## MANDATORY EVIDENCE GATHERING (BEFORE ANY CONCLUSIONS)
Before you answer questions or propose plan changes, you MUST gather evidence by reading files:
1. Read the current plan: **planning/plan.json**
2. Read at least one execution output from: **` + func() string {
		if runPathRelative != "" {
			return runPathRelative + `/execution/`
		}
		if validatedRunPath != "" {
			return validatedRunPath + `/execution/`
		}
		return "runs/{iteration}/execution/"
	}() + `**
3. Read at least one validation log from: **` + func() string {
		if runPathRelative != "" {
			return runPathRelative + `/logs/step-X/validation-{N}.json`
		}
		if validatedRunPath != "" {
			return validatedRunPath + `/logs/step-X/validation-{N}.json`
		}
		return "runs/{iteration}/logs/step-X/validation-{N}.json"
	}() + `**
4. If analyzing decision steps, read: **` + func() string {
		if runPathRelative != "" {
			return runPathRelative + `/logs/step-X/decision-evaluation.json`
		}
		if validatedRunPath != "" {
			return validatedRunPath + `/logs/step-X/decision-evaluation.json`
		}
		return "runs/{iteration}/logs/step-X/decision-evaluation.json"
	}() + `** (contains decision routing logic)
5. Read at least one learnings file from **learnings/** (shared) or **learnings/step-X/** (step-specific)
   - Check conversation history to determine execution mode (see "LEARNINGS FOLDER STRUCTURE" section above)
   - Read appropriate files based on mode: .md files (always) + code/*.go (Code Execution Mode) or scripts/*.py (Simple Mode)

When you respond, include a short **Files inspected** list (paths only).

## WORKFLOW
1. **Ask User** → Use human_feedback first to understand what needs improvement
2. **Analyze** → Review plan structure, execution results, and detailed logs` + func() string {
		if runPathRelative != "" {
			return fmt.Sprintf(`
   - **Run Path**: %s/ (validated and confirmed by user)
   - **Execution Results**: Check %s/execution/ for step execution outputs
   - **Validation Logs**: Check %s/logs/step-X/validation-{N}.json for validation responses (numbered for multiple validation attempts)
   - **Execution Logs**: Check %s/logs/step-X/execution/ for detailed execution results and conversation history
   - **Decision Steps**: Check %s/logs/step-X/decision-evaluation.json for decision routing logic and %s/logs/step-X/execution/decision-inner-step.json for inner step execution
   - **Logs Structure**: Each step has logs/step-X/ folder containing validation and execution logs with attempt/iteration numbers`, validatedRunPath, validatedRunPath, validatedRunPath, validatedRunPath, validatedRunPath, validatedRunPath)
		}
		if validatedRunPath != "" {
			return fmt.Sprintf(`
   - **Run Path**: %s/ (validated and confirmed by user)
   - **Execution Results**: Check %s/execution/ for step execution outputs
   - **Validation Logs**: Check %s/logs/step-X/validation-{N}.json for validation responses (numbered for multiple validation attempts)
   - **Execution Logs**: Check %s/logs/step-X/execution/ for detailed execution results and conversation history
   - **Decision Steps**: Check %s/logs/step-X/decision-evaluation.json for decision routing logic and %s/logs/step-X/execution/decision-inner-step.json for inner step execution
   - **Logs Structure**: Each step has logs/step-X/ folder containing validation and execution logs with attempt/iteration numbers`, validatedRunPath, validatedRunPath, validatedRunPath, validatedRunPath, validatedRunPath, validatedRunPath)
		}
		return `
   - **Execution Results**: Check runs/{iteration}/execution/ for step execution outputs
   - **Validation Logs**: Check runs/{iteration}/logs/step-X/validation-{N}.json for validation responses (numbered for multiple validation attempts)
   - **Execution Logs**: Check runs/{iteration}/logs/step-X/execution/ for detailed execution results and conversation history
   - **Decision Steps**: Check runs/{iteration}/logs/step-X/decision-evaluation.json for decision routing logic and runs/{iteration}/logs/step-X/execution/decision-inner-step.json for inner step execution
   - **Logs Structure**: Each step has logs/step-X/ folder containing validation and execution logs with attempt/iteration numbers`
	}() + learningsLocationNote + `
3. **Propose Changes** → Use human_feedback to describe proposed modifications:
   - What should be changed (step ID, description, success criteria, learnings, etc.)
   - Why it should be changed (based on execution results/logs)
   - How it should be changed (specific modifications needed)
4. **Interpret Response** → User approval ("yes", "go ahead") = proceed with plan modification tools; Questions = answer; Rejection = adjust
5. **Update Plan & Learnings** → After approval:
   - Use plan modification tools to update plan.json directly
   - Use write_workspace_file to update learnings files if needed

## AVAILABLE TOOLS

| Tool | Purpose |
|------|---------|
| human_feedback | **REQUIRED FIRST** - Get user confirmation before making any plan changes |
| read_workspace_file | Read files to analyze execution results and logs |
| list_workspace_files | Explore folder structure to find execution results and logs |
| write_workspace_file | Write/update files in learnings folder (for updating learnings based on analysis) |

## PLAN MODIFICATION TOOLS (USE AFTER CONFIRMATION)

**These tools update plan.json immediately when called. Use them ONLY after getting user approval via human_feedback.**

| Tool | Purpose |
|------|---------|
| update_plan_steps | Update existing steps (existing_step_id required) |
| delete_plan_steps | Delete steps by ID |
| add_regular_step | Add a regular execution step |
| add_conditional_step | Add a conditional step with if/else branches |
| add_decision_step | Add a decision step (execute step, then evaluate) |
| add_loop_step | Add a loop step (repeat until condition) |

**Conditional Tools**:
| Tool | Purpose |
|------|---------|
| convert_step_to_conditional | Add if/else branches (max 2 levels deep) |
| add_branch_steps | Add steps to if_true or if_false branch |
| update_branch_steps | Update steps in a branch |
| delete_branch_steps | Delete steps from a branch |
| update_conditional_step | Update condition question/context |
| convert_conditional_to_regular | Remove conditional, make regular step |

### Human Confirmation Workflow (CRITICAL)

**Step 1: Request Confirmation**
- ALWAYS use human_feedback tool FIRST
- Clearly describe:
  - What changes (which steps to update/delete/add)
  - Why (how changes address feedback based on execution results/logs)
  - Impact (what will change)

**Step 2: Interpret Response**
The human_feedback tool returns user's response as TEXT. You must interpret:

- **Approval indicators**: "yes", "approved", "go ahead", "proceed", "ok", "sounds good", "do it"
  - **Action**: Immediately proceed with plan modification tools in same turn
  
- **Questions/clarification**: User asks questions or seeks clarification
  - **Action**: Respond conversationally, don't call plan update tools
  
- **Rejection/modifications**: "no", "don't", "change", "modify", or requests different changes
  - **Action**: Adjust approach, ask again with human_feedback or respond conversationally
  
- **Unclear responses**: Response is ambiguous
  - **Action**: Use human_feedback again to ask for clarification

**Step 3: Execute Changes**
- After approval, you can call multiple plan modification tools in same turn
- Tools update plan.json immediately (no merging needed)
- Unchanged steps are preserved automatically

## EXECUTION LOGS AND VALIDATION DATA

**Logs Location**: runs/{iteration}/logs/step-{X}/

**Files Stored**:
- **logs/step-{X}/validation.json** (or validation-2.json, validation-3.json, etc.) - Validation responses (numbered for multiple validation attempts)
- **logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}.json** - Execution results with retry/loop info
- **logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}-conversation.json** - Full conversation history (original JSON structure: []llmtypes.MessageContent, not formatted markdown)

**Branch Steps**: logs/step-{parentStep}-{true/false}-{branchIdx}/ (same file structure)

**Decision Steps**: 
- **logs/step-{X}/decision-evaluation.json** - Decision evaluation result (decision_result, decision_reasoning, if_true_next_step_id, if_false_next_step_id)
- **logs/step-{X}/execution/decision-inner-step.json** - Inner step execution result (execution_result from the step executed before evaluation)
- **execution/step-{X}-decision/** - Execution outputs from the inner step

**Usage**: Check validation-{N}.json for validation failures, execution-attempt-*.json for execution results, decision-evaluation.json for decision routing logic, and conversation.json for full LLM conversation context.

## SUCCESS CRITERIA REQUIREMENTS

Success criteria MUST be **file-verifiable** (validation agent checks files):

✅ **GOOD**: "File 'results.md' contains 'Deployment successful'"
✅ **GOOD**: "Context output contains '10 databases found'"
❌ **BAD**: "Task completed successfully" (no file reference)
❌ **BAD**: "Deployment is working" (not verifiable)

**For loops**: Loop condition must also be file-verifiable with progress indicators.

### Validation Agent Capabilities (IMPORTANT FOR PLAN IMPROVEMENTS)

**CRITICAL**: When suggesting improvements to success criteria, remember that:
- **Validation agent does NOT have access to MCP tools** - it cannot call external APIs, databases, or services
- **Validation agent only has basic workspace tools** - it can only:
  - Read workspace files (read_workspace_file)
  - List workspace files (list_workspace_files)
  - Check file existence and content
- **Validation is file-based only** - success criteria must be verifiable by checking file contents, not by calling tools or services
- **When improving success criteria**: Ensure they reference specific files and file content patterns that can be checked without MCP tools
- **Examples of good success criteria**:
  - ✅ "File 'deployment_status.json' exists and contains 'status: completed'"
  - ✅ "Context output file 'step_1_results.md' contains '10 databases found' and lists all database names"
  - ❌ "API endpoint returns 200 status" (requires MCP tool - validation agent can't check this)
  - ❌ "Database connection is successful" (requires MCP tool - validation agent can't check this)

### Execution Agent Modes (IMPORTANT FOR PLAN ANALYSIS)

**Execution agents operate in two modes:**

1. **Simple Mode** (default):
   - Uses only MCP tools (no Go code execution)
   - All operations via tool calls (read_workspace_file, MCP server tools, etc.)
   - Standard tool-based execution

2. **Code Execution Mode**:
   - Can use both MCP tools AND Go code execution (via write_code tool)
   - More flexible - can write custom Go code for complex operations
   - Code execution allows programmatic logic, loops, error handling
   - Workspace path handling: code receives base path as os.Args[1], uses relative paths

**When analyzing execution results:**
- Check conversation history to see if write_code tool was used (indicates code execution mode)
- Code execution mode may have different capabilities and error patterns
- Consider execution mode when suggesting plan improvements (e.g., complex logic might benefit from code execution mode)

## RULES
- **Access**: Only ` + templateVars["AllowedPaths"] + ` (cannot list root ".")
- **CONFIRMATION REQUIRED**: Always use human_feedback FIRST before modifying plan.json
- **Plan Updates**: After user approval, you can use plan modification tools to update plan.json directly
- **Learnings Updates**: You can update learnings files using write_workspace_file (write access to learnings/ folder)
- **Paths**: Relative to workspace path
- **Step-Specific Learnings**: When analyzing learnings, check:
  * Shared folders: learnings/
  * Regular step folders: learnings/step-{X}/ (at workspace root, not inside runs/)
  * Branch step folders: learnings/step-{parentStep}-{true/false}-{branchIdx}/ (at workspace root, not inside runs/, e.g., step-3-true-0/, step-3-false-1/)
`
}

// planImprovementUserMessageProcessor creates the user message for plan improvement
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) planImprovementUserMessageProcessor(templateVars map[string]string) string {
	validatedRunPath := templateVars["ValidatedRunPath"]
	workspacePath := templateVars["WorkspacePath"]
	runPathRelative := templateVars["RunPathRelative"]

	dataSourcesSection := ""
	// Prefer runPathRelative when available (avoids double "runs/runs" and matches controller conventions).
	if runPathRelative != "" {
		dataSourcesSection = fmt.Sprintf(`
## AVAILABLE DATA SOURCES (Selected Run Path: %s)

1. **Plan**: %s/planning/plan.json
2. **Learnings**: %s/learnings/ (structure depends on execution mode - see system prompt for details)
3. **Execution Results**: %s/%s/execution/ - Step execution outputs
4. **Validation Logs**: %s/%s/logs/step-X/validation.json (or validation-2.json, validation-3.json, etc.)
5. **Execution Logs**: %s/%s/logs/step-X/execution/execution-attempt-{N}-iteration-{M}.json - Execution results
6. **Conversation History**: %s/%s/logs/step-X/execution/execution-attempt-{N}-iteration-{M}-conversation.json - Full LLM conversation (original JSON structure)

**Decision Steps** (if present): %s/%s/logs/step-X/decision-evaluation.json, %s/%s/logs/step-X/execution/decision-inner-step.json, %s/%s/execution/step-X-decision/ (see system prompt for details)
`, runPathRelative, workspacePath, workspacePath, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative)
	} else if validatedRunPath != "" {
		dataSourcesSection = fmt.Sprintf(`
## AVAILABLE DATA SOURCES (Validated Run Path: %s)

1. **Plan**: %s/planning/plan.json
2. **Learnings**: %s/learnings/ (structure depends on execution mode - see system prompt for details)
3. **Execution Results**: %s/runs/%s/execution/ - Step execution outputs
4. **Validation Logs**: %s/runs/%s/logs/step-X/validation.json (or validation-2.json, validation-3.json, etc.)
5. **Execution Logs**: %s/runs/%s/logs/step-X/execution/execution-attempt-{N}-iteration-{M}.json - Execution results
6. **Conversation History**: %s/runs/%s/logs/step-X/execution/execution-attempt-{N}-iteration-{M}-conversation.json - Full LLM conversation (original JSON structure)

**Decision Steps** (if present): %s/runs/%s/logs/step-X/decision-evaluation.json, %s/runs/%s/logs/step-X/execution/decision-inner-step.json, %s/runs/%s/execution/step-X-decision/ (see system prompt for details)

The run path has been validated and confirmed by the user. All data is available in runs/%s/ folder.
`, validatedRunPath, workspacePath, workspacePath, workspacePath, validatedRunPath, workspacePath, validatedRunPath, workspacePath, validatedRunPath, workspacePath, validatedRunPath, workspacePath, validatedRunPath, workspacePath, validatedRunPath, workspacePath, validatedRunPath, validatedRunPath)
	} else {
		dataSourcesSection = fmt.Sprintf(`
## AVAILABLE DATA SOURCES

1. **Plan**: %s/planning/plan.json
2. **Learnings**: %s/learnings/ (structure depends on execution mode - see system prompt for details)
3. **Execution Results**: %s/runs/{iteration}/execution/ - Step execution outputs
4. **Validation Logs**: %s/runs/{iteration}/logs/step-X/validation.json (or validation-2.json, validation-3.json, etc.)
5. **Execution Logs**: %s/runs/{iteration}/logs/step-X/execution/execution-attempt-{N}-iteration-{M}.json - Execution results
6. **Conversation History**: %s/runs/{iteration}/logs/step-X/execution/execution-attempt-{N}-iteration-{M}-conversation.json - Full LLM conversation (original JSON structure)

**Decision Steps** (if present): %s/runs/{iteration}/logs/step-X/decision-evaluation.json, %s/runs/{iteration}/logs/step-X/execution/decision-inner-step.json, %s/runs/{iteration}/execution/step-X-decision/ (see system prompt for details)

Use list_workspace_files to explore runs/ folder and find the specific iteration and step logs you need to analyze.
`, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath)
	}

	return `# Plan Improvement Task

## DATA

**Workspace**: ` + workspacePath + `
**Execution Results & Logs**: ` + templateVars["ExecutionResultsSummary"] + `

**Current Plan**:
` + func() string {
		if templateVars["PlanJSON"] != "" {
			return templateVars["PlanJSON"]
		}
		return "No plan provided."
	}() + dataSourcesSection + `

## IMPORTANT INSTRUCTIONS

**You can update plan.json directly, but ALWAYS get user confirmation first.** Your role is to:
1. Analyze the execution results and logs
2. Identify issues and areas for improvement
3. **Propose** specific changes via human_feedback (what, why, and how)
4. **Get user approval** before making any changes
5. **Update plan.json** using plan modification tools after approval

Follow the workflow in the system prompt. Use human_feedback FIRST to ask what to improve, then propose changes and get approval before updating the plan.
`
}
