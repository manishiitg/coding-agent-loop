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
	pim.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings enabled - agent can access step-specific folders in learnings/{step_id}/ (covered by learnings/ read/write path)"))

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
// runPath is optional - if provided (e.g., "runs/iteration-11" or "iteration-11"), it will be used directly
// If not provided or invalid, the function will ask the user via human feedback
func (pim *PlanImprovementManager) PlanImprovementOnly(ctx context.Context, originalWorkspacePath string, runPath string) (string, error) {
	pim.GetLogger().Info(fmt.Sprintf("📊 Starting standalone plan improvement for workspace: %s", originalWorkspacePath))

	// Store original workspace path
	var validatedRunPath string
	var err error

	// If runPath is provided, try to validate it first
	if runPath != "" {
		pim.GetLogger().Info(fmt.Sprintf("📊 Using provided run path: %s", runPath))
		validatedRunPath, err = pim.validateRunPath(ctx, originalWorkspacePath, runPath)
		if err != nil {
			pim.GetLogger().Warn(fmt.Sprintf("⚠️ Provided run path validation failed: %v, falling back to user input", err))
			// Fall back to asking user
			validatedRunPath, err = pim.requestAndValidateFullPath(ctx, originalWorkspacePath)
			if err != nil {
				return "", fmt.Errorf("failed to get validated path: %w", err)
			}
		} else {
			pim.GetLogger().Info(fmt.Sprintf("✅ Successfully validated provided run path: %s", validatedRunPath))
		}
	} else {
		// No run path provided - request and validate full path from user via blocking human feedback
		pim.GetLogger().Info(fmt.Sprintf("📊 Requesting full path from user for plan improvement analysis"))
		validatedRunPath, err = pim.requestAndValidateFullPath(ctx, originalWorkspacePath)
		if err != nil {
			return "", fmt.Errorf("failed to get validated path: %w", err)
		}
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
		"Workspace root: %s\nSelected run folder: %s\n\nRun folder contains:\n- %s/execution/ - step execution outputs\n- %s/logs/ - validation and execution logs\n\nUse list_workspace_files to explore:\n- Execution result files in %s/execution/\n- Detailed logs in %s/logs/step-X/ including:\n  * validation-{N}.json - validation responses for each validation attempt\n  * execution/execution-attempt-{N}-iteration-{M}.json - execution results with retry/loop information\n  * execution/execution-attempt-{N}-iteration-{M}-conversation.json - full conversation history for each execution attempt\n\nLearnings are stored at workspace root:\n- learnings/\n- learnings/{step_id}/ (regular steps, using step IDs from plan.json)\n- learnings/{step_id}/ (branch steps, using step IDs from plan.json where step_id is the branch step's own ID)\n- learnings/{step_id}/ (orchestration sub-agents, using step IDs from plan.json where step_id is the sub-agent's own ID)\n\nPlan is stored at:\n- planning/plan.json",
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

// validateRunPath validates a run path without asking the user
// runPath can be in format "runs/iteration-11", "iteration-11", or "iteration-11/group-7"
// Returns the full path relative to original workspace (e.g., "runs/iteration-11") if valid
func (pim *PlanImprovementManager) validateRunPath(ctx context.Context, originalWorkspacePath string, runPath string) (string, error) {
	// Clean up the path (remove leading/trailing spaces and slashes, remove runs/ prefix if included)
	cleanedPath := strings.TrimSpace(runPath)
	cleanedPath = strings.TrimPrefix(cleanedPath, "runs/")
	cleanedPath = strings.TrimPrefix(cleanedPath, "/")
	cleanedPath = strings.TrimSuffix(cleanedPath, "/")

	if cleanedPath == "" {
		return "", fmt.Errorf("empty run path provided")
	}

	// Construct full path: runs/{cleanedPath}
	// Examples: runs/iteration-11 or runs/iteration-11/group-7
	fullPath := fmt.Sprintf("%s/runs/%s", originalWorkspacePath, cleanedPath)
	executionPath := fmt.Sprintf("%s/execution", fullPath)

	pim.GetLogger().Info(fmt.Sprintf("🔍 Validating run path: %s (full path: %s, checking execution: %s)", cleanedPath, fullPath, executionPath))

	// First check if the base path exists
	_, err := pim.ListWorkspaceFiles(ctx, fullPath)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s: %w", fullPath, err)
	}

	// Check if execution/ folder exists in this path
	files, err := pim.ListWorkspaceFiles(ctx, executionPath)
	if err != nil {
		return "", fmt.Errorf("path does not contain an execution/ folder: %s: %w", fullPath, err)
	}

	// Path exists and has execution/ folder
	pim.GetLogger().Info(fmt.Sprintf("✅ Validated run path: runs/%s (found %d items in execution/ folder)", cleanedPath, len(files)))

	// Return the full path relative to original workspace (e.g., "runs/iteration-11")
	return fmt.Sprintf("runs/%s", cleanedPath), nil
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	planJSON := templateVars["PlanJSON"]
	executionResultsSummary := templateVars["ExecutionResultsSummary"]
	runPathRelative := templateVars["RunPathRelative"]
	validatedRunPath := templateVars["ValidatedRunPath"]
	runWorkspacePath := templateVars["RunWorkspacePath"]

	// runPathRelative is required for correct log/execution context; fail fast if missing
	if strings.TrimSpace(runPathRelative) == "" {
		return "", nil, fmt.Errorf("RunPathRelative is required for plan improvement but was empty")
	}

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
	// Pass unlock function to automatically unlock learnings when plan is modified
	// Use base orchestrator to create unlock function (plan improvement agent only has base orchestrator)
	// Note: For plan improvement agent, we use workspacePath (which is the original workspace root)
	// since learnings are stored in the original workspace, not in run folders
	var unlockLearningsFunc func(context.Context, string, int) error
	if agent.baseOrchestrator != nil {
		// Use workspacePath for unlock operations (learnings are in workspace root)
		unlockLearningsFunc = createUnlockLearningsFunctionFromBase(agent.baseOrchestrator, workspacePath)
	}
	if err := registerPlanModificationTools(mcpAgent, workspacePath, logger, readFile, writeFile, moveFile, "plan improvement agent", unlockLearningsFunc); err != nil {
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

	runPathNote := fmt.Sprintf(`
## VALIDATED RUN PATH
The user has specified the run path to analyze: **%s**
Execution results and logs are under: **%s/execution/** and **%s/logs/** (relative to workspace root)
Run workspace absolute path: **%s**
`, runPathRelative, runPathRelative, runPathRelative, runWorkspacePath)

	return `# Plan Improvement Agent

Analyze execution results and logs to identify and fix plan issues. Update plan.json directly after user confirmation.` + runPathNote + `

---

## ⚠️ CRITICAL RULES

| Rule | Description |
|------|-------------|
| **1. Confirm First** | ALWAYS use human_feedback BEFORE any plan changes |
| **2. Start with Question** | First action: Ask "What would you like to improve?" via human_feedback |
| **3. Gather Evidence** | Read files BEFORE proposing changes (plan, logs, execution results, learnings) |
| **4. File-Verifiable Criteria** | Success criteria must be file-verifiable AND evidence-based (counts, lists, samples), not just status flags or external state |
| **5. Evidence-Based Criteria** | Avoid status-only criteria like 'status: \"success\"' or 'all checks passed' as the sole requirement – they can be gamed by flipping a flag. Require concrete evidence that is hard to fake. |

---

## 📋 EVIDENCE CHECKLIST
Before proposing changes, read:
1. ✅ Current plan: planning/plan.json
2. ✅ Execution outputs: ` + runPathRelative + `/execution/
3. ✅ Validation logs: ` + runPathRelative + `/logs/step-X/validation-N.json
4. ✅ Learnings (shared + step-specific): learnings/ and learnings/{step_id}/

Include **Files inspected:** list in responses.

---

## 🧩 HOW TO ANALYZE AND IMPROVE PLANS

### 1. Root Cause Analysis
- **Don't treat symptoms**: If step 5 fails validation, check if steps 1-4 produced correct outputs
- **Trace backwards**: Failed verification → check what was verified → check what produced the data
- **Check assumptions**: Does step description match what actually happened in logs?

### 2. Common Issues to Check

| Issue | Where to Look | Fix |
|-------|---------------|-----|
| **Weak success criteria** | Compare criteria to validation logs | Make file-verifiable with specific content checks |
| **Missing verification** | Check if critical operations are verified | Add verification step + decision routing |
| **Wrong step sequence** | Review context_dependencies and execution order | Reorder steps, fix dependencies |
| **Incomplete descriptions** | Compare step description to conversation history | Add missing details execution agent needs |
| **Outdated learnings** | Check learnings vs current execution patterns | Update or remove obsolete guidance |

### 3. Decision Step Analysis
For decision steps that route incorrectly:
- Read logs/step-X/decision-evaluation.json - see what LLM decided and why
- Check decision_evaluation_question - does it force re-verification from evidence?
- Verify success_criteria encode **logical correctness**, not just "file exists"

### 4. Validation Failures
When validation fails repeatedly:
- Read logs/step-X/validation-N.json for each attempt
- Check if failure is due to:
  - Weak success criteria (too vague) → Use update_success_criteria tool to make it more specific and execution-based
  - Missing or incorrect validation schema → Use update_validation_schema tool to add/update structured validation rules
  - Wrong file reference (file name mismatch) → Update validation schema file_name or success criteria
  - Unrealistic criteria (requires external state validation agent can't check) → Make criteria file-verifiable only
- **Pre-validation failures**: If pre-validation fails (structural checks), the validation schema needs updating
  - Use update_validation_schema tool to fix file existence, JSON structure, or consistency checks
  - Pre-validation runs before LLM validation and blocks it if structural checks fail
- **LLM validation failures**: If pre-validation passes but LLM validation fails, focus on success criteria
  - Use update_success_criteria tool to emphasize execution history verification
  - Success criteria should focus on what work was actually done, not just file structure

### 5. Learnings Quality
Good learnings are:
- **Specific**: "Use jq '.items[] | select(.status == \"active\")'" not "filter the data"
- **Contextual**: When to use approach X vs Y
- **Current**: Reflect latest successful patterns, not outdated attempts

---

## 🔄 WORKFLOW

| Step | Action |
|------|--------|
| **1. Ask User** | Use human_feedback: "What would you like to improve?" |
| **2. Gather Evidence** | Read plan, logs, execution outputs, learnings (see checklist above) |
| **3. Analyze** | Apply root cause analysis, identify issues |
| **4. Propose** | Use human_feedback: describe what/why/how to change |
| **5. Interpret** | "yes"/"ok" = proceed; questions = answer; "no" = adjust |
| **6. Update** | After approval: use plan modification tools + write learnings |

---

## 🛠️ TOOLS

### Workspace Tools
| Tool | Purpose |
|------|---------|
| human_feedback | **REQUIRED FIRST** - Get approval before plan changes |
| read_workspace_file | Read logs, execution results, learnings |
| list_workspace_files | Explore folder structure |
| write_workspace_file | Update learnings files |

### Plan Modification Tools (After Approval Only)
| Tool | Use For |
|------|---------|
| update_regular_step, update_conditional_step, update_decision_step, update_routing_step | Update existing steps |
| update_validation_schema | Update validation schema for an existing step (fast code-based pre-validation rules) |
| update_success_criteria | Update success criteria for an existing step (execution-based validation focus) |
| add_regular_step, add_conditional_step, add_decision_step, add_routing_step, add_loop_step | Add new steps |
| delete_plan_steps | Remove steps |
| convert_step_to_conditional, add_branch_steps, update_branch_steps, delete_branch_steps | Manage conditionals |

### Response Interpretation
| User Response | Your Action |
|---------------|-------------|
| "yes", "ok", "proceed" | Execute plan changes immediately |
| Questions/clarifications | Answer conversationally, NO tool calls |
| "no", "change X instead" | Revise proposal, ask again |
| Unclear | Ask for clarification via human_feedback |

---

## 📊 LOGS STRUCTURE

**Location**: ` + runPathRelative + `/logs/step-X/

| File | Contains |
|------|----------|
| validation-N.json | Validation attempts and failures |
| execution/execution-attempt-N-iteration-M.json | Execution results with retry info |
| execution/execution-attempt-N-iteration-M-conversation.json | Full LLM conversation (tool calls, responses) |
| decision-evaluation.json | Decision routing logic (for decision steps) |
| routing-evaluation.json | Route selection reasoning (for routing steps) |

---

## ✅ SUCCESS CRITERIA RULES

**CRITICAL**: Validation agent has NO MCP tools - only reads/lists files.

### Two-Layer Validation System
The system uses a two-layer validation approach:
1. **Pre-Validation (Code)**: Fast structural checks - file existence, JSON structure, consistency
   - Handled by validation_schema field (mandatory for all steps)
   - Use update_validation_schema tool to update these rules
   - Blocks LLM validation if structural checks fail
2. **LLM Validation**: Deep authenticity checks - execution history verification, anti-hallucination
   - Handled by success_criteria field (execution-based validation focus)
   - Use update_success_criteria tool to update these rules
   - Focuses on proving work was actually done, not just file structure

### Anti-gaming principle
The execution agent creates both the evidence and any status fields in the same files. If success criteria only checks for a status like 'status: \"success\"' or 'all checks passed', the agent can satisfy it by flipping flags without doing the real work.

To avoid this, success criteria MUST:
- Be file-verifiable, and
- Require concrete evidence (counts, lists, data samples, consistency checks) that would be hard to fake.

### Rules
| Rule | Example |
|------|---------|
| **File-verifiable only** | ✅ File 'results.json' contains a 'databases' array and 'database_count' field equal to the array length <br> ❌ API returns 200 (validation agent can't call APIs) |
| **Check evidence, not just status** | ✅ File 'verification.json' lists tab names, row counts per tab, and sample dates that can be recomputed and checked <br> ❌ File 'verification.json' only has 'status: \"passed\"' |
| **For verification steps** | ✅ Encode that ALL checks pass when recomputed from the raw evidence (counts, lists, samples), not just that a 'passed' flag exists <br> ❌ "verification file exists" or "all checks show passed" with no underlying evidence |

---

## 📚 LEARNINGS

**Location**: learnings/ (shared) + learnings/{step_id}/ (step-specific, using step IDs from plan.json)

**Structure depends on execution mode**:
- **Simple Mode**: .md files + scripts/*.py
- **Code Execution Mode**: .md files + code/*.go

Check conversation history for write_code tool calls to determine mode.

**Quality criteria**:
- Specific (exact commands/code that worked)
- Contextual (when to use each approach)
- Current (reflect latest successful patterns)

---

## 📋 FINAL REMINDERS

- **Folder access**: Only ` + templateVars["AllowedPaths"] + `
- **All paths relative** to workspace root
- **Confirmation required**: human_feedback BEFORE plan changes
- **Tools update immediately**: plan.json and learnings/ written on tool call
`
}

// planImprovementUserMessageProcessor creates the user message for plan improvement
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) planImprovementUserMessageProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	runPathRelative := templateVars["RunPathRelative"]

	dataSourcesSection := ""
	// Run path is always provided (validated before execution).
	dataSourcesSection = fmt.Sprintf(`
## AVAILABLE DATA SOURCES (Selected Run Path: %s)

1. **Plan**: %s/planning/plan.json
2. **Learnings**: %s/learnings/ (structure depends on execution mode - see system prompt for details)
3. **Execution Results**: %s/%s/execution/ - Step execution outputs
4. **Validation Logs**: %s/%s/logs/step-X/validation.json (or validation-2.json, validation-3.json, etc.)
5. **Execution Logs**: %s/%s/logs/step-X/execution/execution-attempt-{N}-iteration-{M}.json - Execution results
6. **Conversation History**: %s/%s/logs/step-X/execution/execution-attempt-{N}-iteration-{M}-conversation.json - Full LLM conversation (original JSON structure)

**Decision Steps** (if present): %s/%s/logs/step-X/decision-evaluation.json, %s/%s/logs/step-X/execution/decision-inner-step.json, %s/%s/execution/step-X-decision/ (see system prompt for details)

**Routing Steps** (if present): %s/%s/logs/step-X/routing-evaluation.json, %s/%s/logs/step-X/execution/routing-main-step.json, %s/%s/execution/step-X-routing/ (see system prompt for details)
`, runPathRelative, workspacePath, workspacePath, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative, workspacePath, runPathRelative)

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
