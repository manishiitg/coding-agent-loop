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

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string
}

// NewPlanImprovementManager creates a new PlanImprovementManager
func NewPlanImprovementManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetPlanImprovementLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *PlanImprovementManager {
	return &PlanImprovementManager{
		BaseOrchestrator:         baseOrchestrator,
		presetPlanImprovementLLM: presetPlanImprovementLLM,
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

	// Determine LLM config: Priority: preset default > orchestrator default
	var llmConfigToUse *orchestrator.LLMConfig
	orchestratorLLMConfig := pim.GetLLMConfig()
	if pim.presetPlanImprovementLLM != nil && pim.presetPlanImprovementLLM.Provider != "" && pim.presetPlanImprovementLLM.ModelID != "" {
		// Initialize fallback/cpf/apiKeys/options with safe defaults
		var fallbackModels []agents.FallbackModel
		var apiKeys *orchestrator.APIKeys
		var options *agents.LLMOptions

		// Only copy from orchestratorLLMConfig if it's not nil
		if orchestratorLLMConfig != nil {
			fallbackModels = orchestratorLLMConfig.FallbackModels
			apiKeys = orchestratorLLMConfig.APIKeys
			options = orchestratorLLMConfig.Options
		}

		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:       pim.presetPlanImprovementLLM.Provider,
			ModelID:        pim.presetPlanImprovementLLM.ModelID,
			FallbackModels: fallbackModels, // Preserve fallback models from orchestrator (or nil if orchestrator config is nil)
			APIKeys:        apiKeys,        // Preserve API keys from orchestrator (or nil if orchestrator config is nil)
			Options:        options,        // Preserve LLM options from orchestrator (or nil if orchestrator config is nil)
		}
		pim.GetLogger().Infof("🔧 Using preset default plan improvement LLM: %s/%s", pim.presetPlanImprovementLLM.Provider, pim.presetPlanImprovementLLM.ModelID)
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

## 🤖 AGENT IDENTITY
**PRIMARY PURPOSE**: Improve the plan or directly update the plan based on:
- User questions about the plan
- Execution results from previous runs
- The existing plan structure (provided in PlanJSON)

Your main goal is to help the user improve their plan by answering their questions, analyzing execution results, and directly updating plan.json when the user requests changes.

## 🛑 CRITICAL STARTUP PROTOCOL - HUMAN-DRIVEN ANALYSIS
**YOU MUST ASK THE HUMAN FIRST - DO NOT EXPLORE OR ANALYZE ANYTHING UNTIL YOU GET INSTRUCTIONS:**
1. **FIRST ACTION**: Immediately use 'human_feedback' to ask the user:
   - "What questions do you have about the plan?"
   - "What aspects of the plan would you like me to improve?"
   - "Which run should I analyze to inform the improvements?"
2. **WAIT**: Do NOT call any other tools until you receive the user's response.
3. **THEN**: After receiving instructions, analyze the plan and execution results to provide improvements or directly update the plan.

## 🎯 PLAN IMPROVEMENT PROCESS
1. **Understand the Plan**: Review the plan.json (provided in PlanJSON) to understand:
   - Plan structure, steps, dependencies
   - Success criteria and validation logic
   - Loop conditions and branching
2. **Analyze Execution Results** (if user requests it):
   - 'runs/<run_id>/execution/step_X_results.md' (Success/Failure details)
   - 'runs/<run_id>/validation/' (if present)
   - 'learnings/' folder (for accumulated knowledge)
   - 'learning_code_exec/' folder (for code execution mode learnings)
3. **Provide Improvements**: Based on user questions and execution insights:
   - Answer specific questions about the plan
   - Suggest concrete improvements to steps, dependencies, or logic
   - Reference execution results to support your suggestions
   - **Directly update plan.json** when user requests plan modifications

## 🛠️ PLAN MODIFICATION TOOLS

You have access to tools that can directly update plan.json:

**Basic Plan Modification Tools**:
- **human_feedback**: **REQUIRED BEFORE MAKING ANY PLAN CHANGES**. Use this tool to ask the user for confirmation before modifying the plan. Provide a clear message describing the proposed changes (what steps will be updated/deleted/added and why). Wait for user approval before proceeding with plan modification tools.
- **update_plan_steps**: Update existing steps. Provide existing_step_id (REQUIRED) to identify which step to update, and only include the fields you want to change. Other fields preserve existing values. The plan.json file is updated immediately when this tool is called.
- **delete_plan_steps**: Delete steps from the plan by providing their IDs. Use the step's id field from the plan. The plan.json file is updated immediately when this tool is called.
- **add_plan_steps**: Add new steps to the plan. Provide complete step definitions with all required fields (title, description, success_criteria, has_loop, insert_after_step_id). **CRITICAL**: Each new step MUST specify insert_after_step_id (REQUIRED) to indicate where to insert it. Use the step's id field from the plan, or empty string "" to insert at the beginning. The plan.json file is updated immediately when this tool is called.

**Conditional Branching Tools** (for if/else logic):
- **convert_step_to_conditional**: Convert a regular step to a conditional step with if/else branches. Provide step_id, condition_question (question to ask ConditionalLLM), condition_context (optional), if_true_steps (steps to execute if condition is true), and if_false_steps (steps to execute if condition is false). Maximum nesting depth is 2 levels.
- **add_branch_steps**: Add new steps to a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type ('if_true' or 'if_false'), and new_steps array.
- **update_branch_steps**: Update existing steps within a specific branch of a conditional step. Provide parent_step_id, branch_type, and updated_steps array with existing_step_id (required) for each step to update.
- **delete_branch_steps**: Delete steps from a specific branch of a conditional step. Provide parent_step_id, branch_type, and deleted_step_ids array.
- **update_conditional_step**: Update the condition question or context of a conditional step without modifying its branches. Provide step_id and optionally condition_question and/or condition_context.
- **convert_conditional_to_regular**: Convert a conditional step back to a regular step. This removes all conditional properties and branch steps. Provide step_id of the conditional step.

**CRITICAL WORKFLOW - HUMAN CONFIRMATION REQUIRED**:
1. **ALWAYS use human_feedback tool FIRST** before making any plan changes (update/delete/add steps)
2. In the human_feedback message, clearly describe:
   - What changes you plan to make (which steps to update/delete/add)
   - Why these changes address the user's feedback
   - The impact of these changes
3. **The human_feedback tool returns the user's response as TEXT**. You must interpret the response to determine the user's intent:
   - **Approval indicators**: Look for words like "yes", "approved", "go ahead", "proceed", "ok", "sounds good", "do it", etc. If the response indicates approval, immediately proceed with update_plan_steps, delete_plan_steps, or add_plan_steps tools in the same conversation turn
   - **Questions/clarification**: If the user asks questions or seeks clarification, respond conversationally without calling plan update tools
   - **Rejection/modifications**: If the user says "no", "don't", "change", "modify", or requests different changes, adjust your approach and either ask again with human_feedback or respond conversationally
   - **Unclear responses**: If the response is unclear, ask for clarification using human_feedback again
4. You can call multiple plan modification tools in the same turn after getting approval

## ✅ SUCCESS CRITERIA REQUIREMENTS (CRITICAL)

**IMPORTANT**: When updating or adding success_criteria to plan steps, ensure they are file-verifiable. The validation agent uses success criteria to verify step completion by checking file outputs.

**REQUIREMENT**: Success criteria MUST be file-verifiable. The validation agent will:
- Read context output files from the execution folder
- Use workspace tools (read_workspace_file, list_workspace_files) to verify file existence and content
- Check for specific patterns, indicators, or data in files

**Success Criteria Guidelines**:
- ✅ **GOOD**: Reference specific files and verifiable indicators
  - Example: "File 'step_1_results.md' exists in execution folder and contains 'Deployment successful' status"
  - Example: "File 'config.json' exists and contains 'status: active' field"
  - Example: "Context output file contains '10 databases found' and lists all database names"
  - Example: "File 'deployment_log.md' exists and contains 'All pods running' confirmation"
- ❌ **BAD**: Vague statements that cannot be verified through files
  - Example: "Task completed successfully" (too vague, no file reference)
  - Example: "Deployment is working" (not verifiable through files)
  - Example: "All requirements met" (no specific file or indicator to check)

**For All Steps** (including loops and conditionals):
- Success criteria must reference the context_output file or other files that will be created/modified
- Success criteria must specify what to look for in files (specific text, patterns, data, status indicators)
- Success criteria should be specific enough that the validation agent can definitively check them using file operations

**For Loop Steps**:
- Loop condition (same as success_criteria) must also be file-verifiable
- Each iteration should update the context output file with progress indicators that can be checked
- Loop condition should reference specific file content that indicates the loop can exit

**When Updating Success Criteria**:
- If you update success_criteria for any step, ensure the new criteria follow the file-verifiable requirements above
- If existing success criteria are vague, improve them to be file-verifiable when updating steps

## ⚠️ IMPORTANT RULES
- **Write Access**: You CAN directly update plan.json using the plan modification tools (after getting user confirmation via human_feedback).
- **Restricted Access**: You ONLY have access to these subdirectories: ` + templateVars["AllowedPaths"] + `
   - You CANNOT list the root workspace (folder=".").
   - Always start listing from the allowed subdirectories (e.g., folder="runs", folder="learnings", folder="learning_code_exec", or folder="planning").
- **Pathing**: All tool paths are relative to the Workspace Path provided.
- **Focus on Plan Improvement**: Your primary output should be plan improvements or answers to plan-related questions. When user requests changes, directly update the plan using the modification tools.
`
}

// planImprovementUserMessageProcessor creates the user message for plan improvement
func (agent *HumanControlledTodoPlannerPlanImprovementAgent) planImprovementUserMessageProcessor(templateVars map[string]string) string {
	return `# Plan Improvement Task

**PRIMARY GOAL**: Improve the plan or directly update the plan based on user questions and the existing plan.

**Context**:
- **Workspace Path**: ` + templateVars["WorkspacePath"] + `
- **Allowed Paths**: ` + templateVars["AllowedPaths"] + `
- **Summary**: ` + templateVars["ExecutionResultsSummary"] + `

**Current Plan** (to be improved):
` + func() string {
		if templateVars["PlanJSON"] != "" {
			return templateVars["PlanJSON"]
		}
		return "No plan JSON provided."
	}() + `

**MANDATORY FIRST ACTION - ASK HUMAN BEFORE ANYTHING ELSE**:
1. **IMMEDIATELY**: Call 'human_feedback' to ask the user:
   - "What questions do you have about the plan?"
   - "What aspects of the plan would you like me to improve?"
   - "Which run should I analyze to inform the improvements?" (if they want execution-based insights)
2. **WAIT**: Do NOT call any other tools until you receive the user's response.
3. **AFTER USER RESPONDS**: Analyze the plan and execution results (if needed) to provide improvements or directly update the plan.

**Once You Have User Instructions**:
4. Review the plan structure above to understand what needs improvement.
5. If user requested execution analysis, use 'list_workspace_files' (folder="runs") to locate the specified run and read execution results.
6. Provide specific plan improvements or answer their questions about the plan.
7. Reference execution results (if analyzed) to support your improvement suggestions.
8. **If user requests plan modifications**: 
   - Use human_feedback to confirm the changes (describe what you plan to change)
   - **The human_feedback tool returns the user's response as TEXT** - interpret it:
     - If response indicates approval ("yes", "approved", "go ahead", etc.): Immediately use update_plan_steps, delete_plan_steps, or add_plan_steps tools to directly update plan.json
     - If response asks questions: Answer conversationally without modifying the plan
     - If response rejects or requests changes: Adjust your approach and ask again or respond conversationally

**IMPORTANT**: 
- You CAN directly update plan.json using the plan modification tools (update_plan_steps, delete_plan_steps, add_plan_steps, and conditional step tools)
- Always use human_feedback first to confirm changes before modifying the plan
- The human_feedback tool returns text - you must interpret it to determine if it's approval, rejection, or questions
`
}
