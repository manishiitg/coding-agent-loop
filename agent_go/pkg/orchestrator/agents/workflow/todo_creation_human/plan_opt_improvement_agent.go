package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "mcpagent/agent"
	baseevents "mcpagent/events"
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
	// Phase LLM config (fallback for plan improvement if presetPlanImprovementLLM not set)
	presetPhaseLLM *AgentLLMConfig

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string
}

// NewPlanImprovementManager creates a new PlanImprovementManager
func NewPlanImprovementManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetPlanImprovementLLM *AgentLLMConfig,
	presetPhaseLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *PlanImprovementManager {
	return &PlanImprovementManager{
		BaseOrchestrator:         baseOrchestrator,
		presetPlanImprovementLLM: presetPlanImprovementLLM,
		presetPhaseLLM:           presetPhaseLLM,
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
	// Knowledgebase folder: knowledgebase/ (persistent files across runs, at workspace root)
	knowledgebasePath := getKnowledgebasePath(originalWorkspacePath)
	readPaths := []string{
		currentWorkspacePath, // Read execution results and logs from current workspace
		knowledgebasePath,    // Read knowledgebase folder (persistent files across runs)
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
	} else if pim.presetPhaseLLM != nil && pim.presetPhaseLLM.Provider != "" && pim.presetPhaseLLM.ModelID != "" {
		// Fallback to phase LLM if plan improvement LLM not set
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
			Provider:              pim.presetPhaseLLM.Provider,
			ModelID:               pim.presetPhaseLLM.ModelID,
			FallbackModels:        fallbackModels,
			CrossProviderFallback: crossProviderFallback,
			APIKeys:               apiKeys,
		}
		pim.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM as fallback for plan improvement: %s/%s", pim.presetPhaseLLM.Provider, pim.presetPhaseLLM.ModelID))
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
		return "", fmt.Errorf("plan.json not found at %s - planning must be run first as a separate phase", planPath)
	}

	// Plan exists - use it for plan improvement
	pim.GetLogger().Info(fmt.Sprintf("✅ Found plan.json with %d steps for plan improvement", len(existingPlan.Steps)))

	// Count sub-agents in orchestration steps before filtering
	totalSubAgentsBefore := countSubAgents(existingPlan)
	if totalSubAgentsBefore > 0 {
		pim.GetLogger().Info(fmt.Sprintf("📊 Found %d sub-agent(s) in orchestration steps", totalSubAgentsBefore))
	}

	// Filter out human input steps before passing to agent (they don't need optimization)
	filteredPlan := filterHumanInputSteps(existingPlan)
	humanInputCount := len(existingPlan.Steps) - len(filteredPlan.Steps)
	if humanInputCount > 0 {
		pim.GetLogger().Info(fmt.Sprintf("🔍 Filtered out %d human input step(s) from plan (no optimization needed)", humanInputCount))
	}

	// Count sub-agents after filtering
	totalSubAgentsAfter := countSubAgents(filteredPlan)
	if totalSubAgentsAfter > 0 {
		pim.GetLogger().Info(fmt.Sprintf("📊 After filtering: %d sub-agent(s) remain in orchestration steps", totalSubAgentsAfter))
	}
	if totalSubAgentsBefore != totalSubAgentsAfter {
		pim.GetLogger().Info(fmt.Sprintf("⚠️ Sub-agent count changed: %d → %d (some may have been human input steps)", totalSubAgentsBefore, totalSubAgentsAfter))
	}

	// Prepare plan JSON for template (using filtered plan)
	planJSONBytes, err := json.MarshalIndent(filteredPlan, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan to JSON: %w", err)
	}

	// Create execution results summary based on the selected run folder.
	// Execution/logs live under runs/<run>/..., while plan/learnings are at workspace root.
	executionResultsSummary := fmt.Sprintf(
		"Workspace root: %s\nSelected run folder: %s\n\nRun folder contains:\n- %s/execution/ - step execution outputs\n- %s/logs/ - validation and execution logs\n\nKnowledgebase folder (shared across all runs):\n- %s/knowledgebase/ - persistent files across all runs (templates, reference data, configurations - NEVER deleted during cleanup)\n\nUse list_workspace_files to explore:\n- Execution result files in %s/execution/\n- Knowledgebase files in %s/knowledgebase/ (persistent across all runs, at workspace root)\n- Detailed logs in %s/logs/step-X/ including:\n  * validation-{N}.json - validation responses for each validation attempt\n  * execution/execution-attempt-{N}-iteration-{M}.json - execution results with retry/loop information\n  * execution/execution-attempt-{N}-iteration-{M}-conversation.json - full conversation history for each execution attempt\n\nLearnings are stored at workspace root:\n- learnings/\n- learnings/{step_id}/ (regular steps, using step IDs from plan.json)\n- learnings/{step_id}/ (branch steps, using step IDs from plan.json where step_id is the branch step's own ID)\n- learnings/{step_id}/ (orchestration sub-agents, using step IDs from plan.json where step_id is the sub-agent's own ID)\n\nPlan is stored at:\n- planning/plan.json",
		originalWorkspacePath,
		validatedRunPath,
		validatedRunPath,
		validatedRunPath,
		originalWorkspacePath,
		validatedRunPath,
		originalWorkspacePath,
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

// filterHumanInputSteps removes human input steps from the plan (including from branch steps)
// Human input steps don't need optimization - they only ask questions and block for user input
func filterHumanInputSteps(plan *PlanningResponse) *PlanningResponse {
	filteredPlan := &PlanningResponse{
		Steps: make([]PlanStepInterface, 0, len(plan.Steps)),
	}

	// Helper function to recursively filter steps (handles branch steps)
	var filterStep func(step PlanStepInterface) PlanStepInterface
	filterStep = func(step PlanStepInterface) PlanStepInterface {
		// Skip human input steps
		if isHumanInputStep(step) {
			return nil
		}

		// Handle conditional steps (they have branch steps that also need filtering)
		if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
			filteredConditional := &ConditionalPlanStep{
				Type:              conditionalStep.Type,
				CommonStepFields:  conditionalStep.CommonStepFields,
				ConditionQuestion: conditionalStep.ConditionQuestion,
				ConditionContext:  conditionalStep.ConditionContext,
				IfTrueNextStepID:  conditionalStep.IfTrueNextStepID,
				IfFalseNextStepID: conditionalStep.IfFalseNextStepID,
				IfTrueSteps:       make([]PlanStepInterface, 0),
				IfFalseSteps:      make([]PlanStepInterface, 0),
				AgentConfigs:      conditionalStep.AgentConfigs,
			}

			// Filter if_true_steps
			for _, branchStep := range conditionalStep.IfTrueSteps {
				if filteredBranchStep := filterStep(branchStep); filteredBranchStep != nil {
					filteredConditional.IfTrueSteps = append(filteredConditional.IfTrueSteps, filteredBranchStep)
				}
			}

			// Filter if_false_steps
			for _, branchStep := range conditionalStep.IfFalseSteps {
				if filteredBranchStep := filterStep(branchStep); filteredBranchStep != nil {
					filteredConditional.IfFalseSteps = append(filteredConditional.IfFalseSteps, filteredBranchStep)
				}
			}

			return filteredConditional
		}

		// Handle orchestration steps (they have sub-agent steps that also need filtering)
		if orchestrationStep, ok := step.(*OrchestrationPlanStep); ok {
			filteredOrchestration := &OrchestrationPlanStep{
				Type:                orchestrationStep.Type,
				ID:                  orchestrationStep.ID,
				Title:               orchestrationStep.Title,
				OrchestrationStep:   orchestrationStep.OrchestrationStep,
				NextStepID:          orchestrationStep.NextStepID,
				OrchestrationRoutes: make([]PlanOrchestrationRoute, 0, len(orchestrationStep.OrchestrationRoutes)),
				AgentConfigs:        orchestrationStep.AgentConfigs,
			}

			// Filter orchestration routes (sub-agents)
			// IMPORTANT: We keep ALL non-human-input sub-agents - they need optimization
			for _, route := range orchestrationStep.OrchestrationRoutes {
				// Skip only if sub-agent is a human input step
				if isHumanInputStep(route.SubAgentStep) {
					continue // Skip this route (human input sub-agent)
				}

				// Recursively filter sub-agent step (in case it has nested structures like conditional branches)
				filteredSubAgentStep := filterStep(route.SubAgentStep)
				if filteredSubAgentStep != nil {
					// Create a copy of the route with filtered sub-agent
					filteredRoute := PlanOrchestrationRoute{
						RouteID:       route.RouteID,
						RouteName:     route.RouteName,
						Condition:     route.Condition,
						SubAgentStep:  filteredSubAgentStep,
						ContextToPass: route.ContextToPass,
					}
					filteredOrchestration.OrchestrationRoutes = append(filteredOrchestration.OrchestrationRoutes, filteredRoute)
				}
			}

			return filteredOrchestration
		}

		// For all other step types, keep as-is (regular, decision, loop steps)
		return step
	}

	// Filter all top-level steps
	for _, step := range plan.Steps {
		if filteredStep := filterStep(step); filteredStep != nil {
			filteredPlan.Steps = append(filteredPlan.Steps, filteredStep)
		}
	}

	return filteredPlan
}

// countSubAgents counts all sub-agents in orchestration steps (recursively)
func countSubAgents(plan *PlanningResponse) int {
	count := 0
	for _, step := range plan.Steps {
		if orchestrationStep, ok := step.(*OrchestrationPlanStep); ok {
			count += len(orchestrationStep.OrchestrationRoutes)
			// Recursively count sub-agents in conditional steps (if any sub-agents are conditional)
			for _, route := range orchestrationStep.OrchestrationRoutes {
				if conditionalSubAgent, ok := route.SubAgentStep.(*ConditionalPlanStep); ok {
					// Count branch steps in conditional sub-agents
					count += len(conditionalSubAgent.IfTrueSteps)
					count += len(conditionalSubAgent.IfFalseSteps)
				}
			}
		}
		// Also check if conditional steps contain orchestration steps
		if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
			for _, branchStep := range conditionalStep.IfTrueSteps {
				if branchOrchestration, ok := branchStep.(*OrchestrationPlanStep); ok {
					count += len(branchOrchestration.OrchestrationRoutes)
				}
			}
			for _, branchStep := range conditionalStep.IfFalseSteps {
				if branchOrchestration, ok := branchStep.(*OrchestrationPlanStep); ok {
					count += len(branchOrchestration.OrchestrationRoutes)
				}
			}
		}
	}
	return count
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
			promptMessage = fmt.Sprintf("Please provide the folder path to analyze (relative to runs/ folder).\n\nExamples:\n- 'iteration-11' - to analyze a specific iteration\n- 'iteration-11/group-7' - to analyze a specific group (using group ID)\n- 'iteration-11/production' - to analyze a specific group (using display name)\n\nThe path must contain an execution/ folder.\n\nThe workspace root is: %s", originalWorkspacePath)
		} else {
			promptMessage = fmt.Sprintf("The path you provided doesn't exist or doesn't contain an execution/ folder. Please provide a valid path relative to runs/ folder (attempt %d/%d).\n\nExamples: 'iteration-11', 'iteration-11/group-7', or 'iteration-11/production'\n\nThe workspace root is: %s", attempt, maxAttempts, originalWorkspacePath)
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

	return "", fmt.Errorf("failed to get valid path after %d attempts", maxAttempts)
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

	// Store initial plan state BEFORE agent execution for changelog comparison
	// This captures the state before any modifications are made
	var initialPlan *PlanningResponse
	existingPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		// If plan doesn't exist or can't be read, use empty plan
		initialPlan = &PlanningResponse{Steps: []PlanStep{}}
		if logger != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to read initial plan for changelog comparison: %v, using empty plan", err))
		}
	} else {
		// Deep copy the existing plan to avoid mutations
		planJSONBytes, err := json.Marshal(existingPlan)
		if err == nil {
			var copiedPlan PlanningResponse
			if err := json.Unmarshal(planJSONBytes, &copiedPlan); err == nil {
				initialPlan = &copiedPlan
				if logger != nil {
					logger.Info(fmt.Sprintf("📝 Stored initial plan state (%d steps) for changelog comparison", len(initialPlan.Steps)))
				}
			} else {
				// Deep copy failed - use empty plan to ensure changelog generation still works
				initialPlan = &PlanningResponse{Steps: []PlanStep{}}
				if logger != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to deep copy initial plan: %v, using empty plan", err))
				}
			}
		} else {
			// Marshal failed - use empty plan to ensure changelog generation still works
			initialPlan = &PlanningResponse{Steps: []PlanStep{}}
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to marshal initial plan: %v, using empty plan", err))
			}
		}
	}

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
			startedEvent := &orchestrator_events.OrchestratorAgentStartEvent{
				BaseEventData: baseevents.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				AgentType: "plan-improvement",
				AgentName: "plan-improvement-agent",
				Objective: "Improve plan based on execution results and user feedback",
				InputData: planImprovementTemplateVars,
			}
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type:      orchestrator_events.OrchestratorAgentStart,
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

		// Generate changelog from plan diff (AFTER each iteration, BEFORE human feedback)
		// This captures ALL changes made during the agent execution session so far in one comprehensive changelog entry
		// Ensure initialPlan is never nil (safety check)
		if initialPlan == nil {
			initialPlan = &PlanningResponse{Steps: []PlanStep{}}
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ initialPlan was nil, using empty plan for changelog generation"))
			}
		}

		planResponse, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to read plan for changelog generation (iteration %d): %v", iteration, err))
			}
		} else {
			if logger != nil {
				logger.Info(fmt.Sprintf("📝 Generating changelog after iteration %d: initialPlan has %d steps, planResponse has %d steps", iteration, len(initialPlan.Steps), len(planResponse.Steps)))
			}
			if err := generateChangelogFromPlanDiff(ctx, workspacePath, initialPlan, planResponse, readFile, writeFile, logger); err != nil {
				if logger != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to generate changelog from plan diff (iteration %d): %v", iteration, err))
				}
				// Don't fail the entire operation if changelog generation fails
			} else {
				if logger != nil {
					logger.Info(fmt.Sprintf("✅ Changelog generation completed successfully after iteration %d", iteration))
				}
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

	// Final changelog generation (safety measure - ensures changelog is written even if user never responded to blocking feedback)
	// This captures the final state after all iterations complete
	// Ensure initialPlan is never nil (safety check)
	if initialPlan == nil {
		initialPlan = &PlanningResponse{Steps: []PlanStep{}}
		if logger != nil {
			logger.Warn(fmt.Sprintf("⚠️ initialPlan was nil, using empty plan for final changelog generation"))
		}
	}

	planResponse, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to read plan for final changelog generation: %v", err))
		}
	} else {
		if logger != nil {
			logger.Info(fmt.Sprintf("📝 Generating final changelog after all iterations: initialPlan has %d steps, planResponse has %d steps", len(initialPlan.Steps), len(planResponse.Steps)))
		}
		if err := generateChangelogFromPlanDiff(ctx, workspacePath, initialPlan, planResponse, readFile, writeFile, logger); err != nil {
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to generate final changelog from plan diff: %v", err))
			}
			// Don't fail the entire operation if changelog generation fails
		} else {
			if logger != nil {
				logger.Info(fmt.Sprintf("✅ Final changelog generation completed successfully"))
			}
		}
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
			completedEvent := &orchestrator_events.OrchestratorAgentEndEvent{
				BaseEventData: baseevents.BaseEventData{
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
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type:      orchestrator_events.OrchestratorAgentEnd,
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
	templateStr := `# Plan Improvement Agent
**Context**: Analyzing run path '{{.RunPathRelative}}'

## 🤖 ROLE
Post-execution analyst. Identify and fix plan issues by analyzing ACTUAL tool calls and failures.

## ⚠️ CRITICAL RULES
1. **Confirm First**: Use 'human_feedback' BEFORE any plan changes.
2. **Start with Questions**: Ask "What would you like to improve?" first.
3. **Gather Evidence**: Read files (plan, logs, learnings) before proposing fixes.
4. **Concrete Criteria**: Success criteria MUST be file-verifiable (counts, samples) and hard to fake.

## 📋 EVIDENCE SOURCES
- **Default (Fast)**: 'planning/plan.json', 'learnings/', '{{.RunPathRelative}}/execution/', 'knowledgebase/'.
- **Knowledgebase**: Read 'knowledgebase/' for persistent templates, reference data, or global configs shared across all runs.
- **Logs (Slow/Requested)**: Only read '{{.RunPathRelative}}/logs/' if user asks about specific failures or "why" a step failed.

---

## 🧩 ANALYSIS CHECKLISTS

### 1. Orchestration Steps (Main + Sub-Agents)
Orchestration involves a Main Orchestrator looping over Sub-Agents.
- **Main Orchestrator**: Read 'orchestration-evaluation.json'. Is it looping forever? Are route conditions clear?
- **Sub-Agents**: Read 'logs/step-X-sub-agent-{i}/'. Are sub-agent success criteria file-verifiable? Did they receive correct instructions?
- **Root Cause**: If a sub-agent fails, check if the main orchestrator provided the right context.

### 2. Validation Failures
- **Pre-Validation (Structural)**: If this fails, update the 'validation_schema' (file exists, JSON format).
- **LLM Validation (Authenticity)**: If this fails, update 'success_criteria' to focus on execution history (proving work was done).

### 3. Anti-Gaming Principle
Avoid status-only criteria like 'status: "success"'. Require concrete evidence:
- ✅ "File 'X' contains array 'Y' with length 'Z'".
- ❌ "Step shows as success in verification file".

### 4. JSON File Size Issues
If JSON context output files are too large (> 100KB), they will fail to load during pre-validation:
- **Problem**: Large JSON files cause parsing failures ("invalid character 'd' after object key:value pair", file loading failures)
- **Solution**: Update step's context_output structure to reference markdown files for large content
- **Recommendation**: Suggest splitting large JSON into structured data (JSON) + large text (markdown file reference)
- **Example Fix**: Change {"large_content": "very long text..."} to {"summary": "brief", "details_file": "step_X_details.md"}

---

## 🔄 WORKFLOW
1. **Ask**: Use 'human_feedback' to align with user needs.
2. **Read**: Inspect logs/outputs/learnings.
3. **Propose**: Describe what/why/how to the user.
4. **Update**: After approval ("yes", "ok"), use plan modification tools.

## 🛠️ PLAN MODIFICATION TOOLS
Use 'update_*', 'add_*', 'delete_plan_steps', 'convert_step_to_conditional', etc., ONLY after user approval.

{{if .AllowedPaths}}**Allowed Paths**: {{.AllowedPaths}}{{end}}`

	tmpl, err := template.New("improvementSystem").Parse(templateStr)
	if err != nil {
		return "Error parsing improvement system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
		return "Error executing improvement system prompt template: " + err.Error()
	}
	return result.String()
}

func (agent *HumanControlledTodoPlannerPlanImprovementAgent) planImprovementUserMessageProcessor(templateVars map[string]string) string {
	templateStr := `# Plan Improvement Task
## 📋 CONTEXT
- **Workspace**: {{.WorkspacePath}}
- **Selected Run**: {{.RunPathRelative}}

## 📊 DATA
**Execution Summary**:
{{.ExecutionResultsSummary}}

**Current Plan**:
{{if .PlanJSON}}{{.PlanJSON}}{{else}}No plan provided.{{end}}

## 🧠 TASK
1. Analyze the plan vs. execution results.
2. Ask the user what to improve via 'human_feedback'.
3. Propose specific fixes and get approval before updating the plan.`

	tmpl, err := template.New("improvementUser").Parse(templateStr)
	if err != nil {
		return "Error parsing improvement user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
		return "Error executing improvement user message template: " + err.Error()
	}
	return result.String()
}
