package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerPlanImprovementTemplate holds template variables for plan improvement prompts
type HumanControlledTodoPlannerPlanImprovementTemplate struct {
	WorkspacePath           string
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
func NewHumanControlledTodoPlannerPlanImprovementAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *HumanControlledTodoPlannerPlanImprovementAgent {
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
func (pim *PlanImprovementManager) createPlanImprovementAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read-only access to runs/ folder, learnings/ folders, and planning/ folder
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	learningCodeExecPath := fmt.Sprintf("%s/learning_code_exec", workspacePath)
	planningPath := fmt.Sprintf("%s/planning", workspacePath)

	// Agent has read-only access to runs/ folder for execution results, both learnings/ folders for learnings analysis,
	// and planning/ folder to read plan.json. Plan modifications are done via custom tools (not workspace tools),
	// so the agent doesn't need write access - the tool executors handle file writing directly.
	readPaths := []string{runsPath, learningsPath, learningCodeExecPath, planningPath}
	writePaths := []string{} // No write access - plan updates are done via custom tool executors, not workspace tools
	pim.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	pim.GetLogger().Infof("📊 Setting folder guard for plan improvement agent - Read paths: %v, Write paths: %v (read-only access to runs/, learnings/, learning_code_exec/, and planning/ folders. Plan updates via custom tools)", readPaths, writePaths)

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
		pim.GetLogger().Infof("🔧 Using preset default plan improvement LLM: %s/%s", pim.presetPlanImprovementLLM.Provider, pim.presetPlanImprovementLLM.ModelID)
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
		pim.GetLogger().Infof("🔧 Using preset learning LLM as fallback for plan improvement: %s/%s", pim.presetLearningLLM.Provider, pim.presetLearningLLM.ModelID)
	} else {
		llmConfigToUse = orchestratorLLMConfig
		pim.GetLogger().Infof("🔧 Using orchestrator default plan improvement LLM: %s/%s", pim.GetProvider(), pim.GetModel())
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
	pim.GetLogger().Infof("🔧 Disabling code execution mode for plan improvement agent (only execution agents use MCP tools)")

	// Large output virtual tools are enabled for plan improvement (agent may generate large feedback reports)

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
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
func (pim *PlanImprovementManager) PlanImprovementOnly(ctx context.Context, workspacePath string) (string, error) {
	pim.GetLogger().Infof("📊 Starting standalone plan improvement for workspace: %s", workspacePath)

	// Set workspace path
	pim.SetWorkspacePath(workspacePath)

	// Check if plan.json exists - REQUIRED for plan improvement
	planPath := fmt.Sprintf("%s/planning/plan.json", pim.GetWorkspacePath())
	planExist, existingPlan, err := pim.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing plan: %w", err)
	}
	if !planExist {
		return "", fmt.Errorf("plan.json not found at %s - planning must be run first as a separate phase", planPath)
	}

	// Plan exists - use it for plan improvement
	pim.GetLogger().Infof("✅ Found plan.json with %d steps for plan improvement", len(existingPlan.Steps))

	// Prepare plan JSON for template
	planJSONBytes, err := json.MarshalIndent(existingPlan, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan to JSON: %w", err)
	}

	// Don't pre-check execution results - let the agent explore runs/ folder itself using its tools
	executionResultsSummary := "Execution results are in the runs/ folder. Use list_workspace_files to explore and find execution result files."

	// Create plan improvement agent
	planImprovementAgent, err := pim.createPlanImprovementAgent(ctx, pim.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("failed to create plan improvement agent: %w", err)
	}

	// Prepare template variables
	// Use actual workspace path so agent can navigate correctly (runs/ is a subdirectory)
	// Explicitly list allowed paths for the agent (includes planning/ for reading plan.json, learning_code_exec/ for code execution mode learnings)
	allowedPaths := "['runs/', 'learnings/', 'learning_code_exec/', 'planning/']"
	planImprovementTemplateVars := map[string]string{
		"WorkspacePath":           pim.GetWorkspacePath(),
		"PlanJSON":                string(planJSONBytes),
		"ExecutionResultsSummary": executionResultsSummary,
		"AllowedPaths":            allowedPaths,
		"SessionID":               pim.sessionID,
		"WorkflowID":              pim.workflowID,
	}

	// Execute plan improvement agent
	pim.GetLogger().Infof("📊 Executing plan improvement agent...")
	result, conversationHistory, err := planImprovementAgent.Execute(ctx, planImprovementTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("plan improvement agent execution failed: %w", err)
	}

	pim.GetLogger().Infof("✅ Plan improvement completed successfully")
	pim.GetLogger().Infof("📊 Plan improvement result: %s", result)

	_ = conversationHistory // Conversation history not used for standalone plan improvement

	return result, nil
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the generic ReadWorkspaceFile function from base orchestrator
func (pim *PlanImprovementManager) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {
	pim.GetLogger().Infof("🔍 Checking for existing plan at %s", planPath)

	// Use the generic ReadWorkspaceFile function from base orchestrator
	planContent, err := pim.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			pim.GetLogger().Infof("📋 No existing plan found: %v", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	// Parse JSON content to PlanningResponse
	var planResponse PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &planResponse); err != nil {
		pim.GetLogger().Warnf("⚠️ Failed to parse existing plan.json: %v", err)
		return false, nil, fmt.Errorf("failed to parse plan.json: %w", err)
	}

	pim.GetLogger().Infof("✅ Found existing plan at %s with %d steps", planPath, len(planResponse.Steps))
	return true, &planResponse, nil
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	planJSON := templateVars["PlanJSON"]
	executionResultsSummary := templateVars["ExecutionResultsSummary"]

	// Provide default allowed paths if not present
	allowedPaths := templateVars["AllowedPaths"]
	if allowedPaths == "" {
		allowedPaths = "['runs/', 'learnings/', 'learning_code_exec/', 'planning/']"
	}

	// Prepare template variables
	planImprovementTemplateVars := map[string]string{
		"WorkspacePath":           workspacePath,
		"PlanJSON":                planJSON,
		"ExecutionResultsSummary": executionResultsSummary,
		"AllowedPaths":            allowedPaths,
	}

	// Create template data for plan improvement
	templateData := HumanControlledTodoPlannerPlanImprovementTemplate{
		WorkspacePath:           workspacePath,
		PlanJSON:                planJSON,
		ExecutionResultsSummary: executionResultsSummary,
		AllowedPaths:            allowedPaths,
	}

	// Get logger and MCP agent from base agent
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	var logger utils.ExtendedLogger
	var mcpAgent *mcpagent.Agent
	if baseAgent != nil {
		mcpAgent = baseAgent.Agent()
		if mcpAgent != nil && mcpAgent.Logger != nil {
			logger = mcpAgent.Logger
		}
	}

	if mcpAgent == nil {
		return "", nil, fmt.Errorf("MCP agent is not initialized")
	}

	// Get readFile and writeFile functions from base orchestrator
	// We need to access the base orchestrator to get these methods
	// Since agent has baseOrchestrator reference, we can use it
	var readFile func(context.Context, string) (string, error)
	var writeFile func(context.Context, string, string) error
	if agent.baseOrchestrator != nil {
		readFile = agent.baseOrchestrator.ReadWorkspaceFile
		writeFile = agent.baseOrchestrator.WriteWorkspaceFile
	} else {
		return "", nil, fmt.Errorf("base orchestrator is not initialized")
	}

	// Register all plan modification tools using shared function
	if err := registerPlanModificationTools(mcpAgent, workspacePath, logger, readFile, writeFile, "plan improvement agent"); err != nil {
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

	// Main execution loop with blocking human feedback
	for iteration < maxIterations {
		iteration++
		if logger != nil {
			logger.Infof("📊 Plan improvement agent iteration %d/%d", iteration, maxIterations)
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
					logger.Infof("🔍 [PlanImprovementAgent] Plan modification tool detected in iteration %d, emitting event immediately", iteration)
				}
				CheckAndEmitPlanUpdateEvent(ctx, agent.baseOrchestrator, updatedConversationHistory, workspacePath, readFile)
			}
		}

		// After execution, ask if user wants to continue (blocking feedback)
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			if logger != nil {
				logger.Infof("📊 Plan improvement agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations)
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
					logger.Warnf("⚠️ Failed to get user feedback: %v", err)
				}
				// Continue without blocking if feedback fails
				break
			}

			// If user clicked Approve button, we're done
			if approved {
				if logger != nil {
					logger.Infof("✅ User approved - plan improvement complete")
				}
				break
			}

			// User provided feedback/question - always pass it to the agent and continue
			if feedback != "" && strings.TrimSpace(feedback) != "" {
				if logger != nil {
					logger.Infof("📝 User provided feedback: %s", feedback)
				}
				// Use feedback directly as user message for next iteration
				// Note: BaseAgent.Execute() will automatically add it to conversation history
				userMessage = feedback
			} else {
				// No feedback provided but not approved - continue with same message
				if logger != nil {
					logger.Infof("ℹ️ No feedback provided, continuing with same context")
				}
			}
		} else {
			// Reached max iterations or no base orchestrator
			if logger != nil {
				logger.Infof("📊 Reached maximum iterations (%d) or no base orchestrator, ending conversation", maxIterations)
			}
			break
		}
	}

	if logger != nil {
		logger.Infof("📊 Plan improvement completed after %d iterations", iteration)
	}

	// Check if plan modification tools were called and emit event if needed
	// This ensures the frontend is notified of plan changes
	if logger != nil {
		logger.Infof("🔍 [PlanImprovementAgent] Calling CheckAndEmitPlanUpdateEvent (baseOrchestrator: %v, conversationHistory length: %d)", agent.baseOrchestrator != nil, len(currentConversationHistory))
	}
	CheckAndEmitPlanUpdateEvent(ctx, agent.baseOrchestrator, currentConversationHistory, workspacePath, readFile)
	if logger != nil {
		logger.Infof("🔍 [PlanImprovementAgent] CheckAndEmitPlanUpdateEvent call completed")
	}

	return currentResult, currentConversationHistory, nil
}

// planImprovementSystemPromptProcessor creates the system prompt for plan improvement
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) planImprovementSystemPromptProcessor(templateVars map[string]string) string {
	return `# Plan Improvement Agent

## PURPOSE
Improve plan.json based on user questions and execution results. Can directly update the plan after user confirmation.

## FIRST ACTION (MANDATORY)
Use 'human_feedback' to ask: "What would you like to improve? Which run should I analyze?"
**WAIT** for response before doing anything else.

## WORKFLOW
1. **Ask User** → Use human_feedback first
2. **Analyze** → Review plan structure, execution results (if requested)
3. **Propose Changes** → Use human_feedback to describe proposed modifications
4. **Interpret Response** → Approval ("yes", "go ahead") = proceed; Questions = answer; Rejection = adjust
5. **Update** → After approval, use plan modification tools

## PLAN MODIFICATION TOOLS

| Tool | Purpose |
|------|---------|
| human_feedback | **REQUIRED** before any changes - get user confirmation |
| update_plan_steps | Update existing steps (existing_step_id required) |
| delete_plan_steps | Delete steps by ID |
| add_plan_steps | Add new steps (insert_after_step_id required, "" for beginning) |

**Conditional Tools**:
| Tool | Purpose |
|------|---------|
| convert_step_to_conditional | Add if/else branches (max 2 levels deep) |
| add_branch_steps | Add steps to if_true or if_false branch |
| update_branch_steps | Update steps in a branch |
| delete_branch_steps | Delete steps from a branch |
| update_conditional_step | Update condition question/context |
| convert_conditional_to_regular | Remove conditional, make regular step |

## SUCCESS CRITERIA REQUIREMENTS

Success criteria MUST be **file-verifiable** (validation agent checks files):

✅ **GOOD**: "File 'results.md' contains 'Deployment successful'"
✅ **GOOD**: "Context output contains '10 databases found'"
❌ **BAD**: "Task completed successfully" (no file reference)
❌ **BAD**: "Deployment is working" (not verifiable)

**For loops**: Loop condition must also be file-verifiable with progress indicators.

## RULES
- **Access**: Only ` + templateVars["AllowedPaths"] + ` (cannot list root ".")
- **Confirmation**: Always use human_feedback before modifying plan
- **Paths**: Relative to workspace path
`
}

// planImprovementUserMessageProcessor creates the user message for plan improvement
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) planImprovementUserMessageProcessor(templateVars map[string]string) string {
	return `# Plan Improvement Task

## DATA

**Workspace**: ` + templateVars["WorkspacePath"] + `
**Execution Results**: ` + templateVars["ExecutionResultsSummary"] + `

**Current Plan**:
` + func() string {
		if templateVars["PlanJSON"] != "" {
			return templateVars["PlanJSON"]
		}
		return "No plan provided."
	}() + `

Follow the workflow in the system prompt. Use human_feedback FIRST to ask what to improve.
`
}
