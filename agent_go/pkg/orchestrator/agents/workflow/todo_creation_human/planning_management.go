package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	"mcpagent/events"
	"mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// EnhancedPlanWithMetadata stores enhanced plan with caching metadata
type EnhancedPlanWithMetadata struct {
	Plan          *PlanningResponse  `json:"plan"`
	LastUpdated   time.Time          `json:"last_updated"`
	LearningFiles []LearningFileInfo `json:"learning_files"`
}

// LearningFileInfo stores information about a learning file for cache comparison
type LearningFileInfo struct {
	Filepath   string    `json:"filepath"`
	ModifiedAt time.Time `json:"modified_at"`
}

// validateDecisionStep validates that a decision step has all required fields
// Returns error if any required field is missing
func validateDecisionStep(step PlanStep, stepIndex int) error {
	if step.HasDecisionStep {
		if step.DecisionStep == nil {
			return fmt.Errorf(fmt.Sprintf("decision step at index %d (title: %q) is missing required decision_step field", stepIndex, step.Title), nil)
		}
		if step.DecisionStep.ID == "" {
			return fmt.Errorf(fmt.Sprintf("decision step at index %d (title: %q) has decision_step with missing required ID field", stepIndex, step.Title), nil)
		}
		if step.DecisionEvaluationQuestion == "" {
			return fmt.Errorf(fmt.Sprintf("decision step at index %d (title: %q) is missing required decision_evaluation_question field", stepIndex, step.Title), nil)
		}
		if step.IfTrueNextStepID == "" {
			return fmt.Errorf(fmt.Sprintf("decision step at index %d (title: %q) is missing required if_true_next_step_id field", stepIndex, step.Title), nil)
		}
		if step.IfFalseNextStepID == "" {
			return fmt.Errorf(fmt.Sprintf("decision step at index %d (title: %q) is missing required if_false_next_step_id field", stepIndex, step.Title), nil)
		}
		// Recursively validate nested decision step if it's also a decision step
		if step.DecisionStep.HasDecisionStep {
			if err := validateDecisionStep(*step.DecisionStep, stepIndex); err != nil {
				return err
			}
		}
		// Recursively validate nested branch steps in decision_step
		if len(step.DecisionStep.IfTrueSteps) > 0 {
			if err := validateBranchStepIDs(step.DecisionStep.IfTrueSteps, step.DecisionStep.Title, "true"); err != nil {
				return err
			}
		}
		if len(step.DecisionStep.IfFalseSteps) > 0 {
			if err := validateBranchStepIDs(step.DecisionStep.IfFalseSteps, step.DecisionStep.Title, "false"); err != nil {
				return err
			}
		}
	}
	return nil
}

// validatePlanStepIDs recursively validates that all steps have IDs
// Throws error if any step is missing an ID
func validatePlanStepIDs(steps []PlanStep) error {
	for i := range steps {
		if steps[i].ID == "" {
			return fmt.Errorf(fmt.Sprintf("step at index %d is missing required ID field. Step title: %q", i, steps[i].Title), nil)
		}

		// Validate decision step fields
		if err := validateDecisionStep(steps[i], i); err != nil {
			return err
		}

		// Recursively validate branch steps
		if len(steps[i].IfTrueSteps) > 0 {
			if err := validateBranchStepIDs(steps[i].IfTrueSteps, steps[i].Title, "true"); err != nil {
				return err
			}
		}
		if len(steps[i].IfFalseSteps) > 0 {
			if err := validateBranchStepIDs(steps[i].IfFalseSteps, steps[i].Title, "false"); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateBranchStepIDs recursively validates that all branch steps have IDs
func validateBranchStepIDs(steps []PlanStep, parentTitle, branchType string) error {
	for i := range steps {
		if steps[i].ID == "" {
			return fmt.Errorf(fmt.Sprintf("branch step at index %d in %s branch of parent %q is missing required ID field. Step title: %q", i, branchType, parentTitle, steps[i].Title), nil)
		}

		// Recursively validate nested branch steps
		if len(steps[i].IfTrueSteps) > 0 {
			if err := validateBranchStepIDs(steps[i].IfTrueSteps, steps[i].Title, "true"); err != nil {
				return err
			}
		}
		if len(steps[i].IfFalseSteps) > 0 {
			if err := validateBranchStepIDs(steps[i].IfFalseSteps, steps[i].Title, "false"); err != nil {
				return err
			}
		}
	}
	return nil
}

// runPlanningPhase generates JSON plan directly
// conversationHistory is updated in-place to accumulate across iterations
// Returns the generated PlanningResponse and updated conversation history
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanningPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llmtypes.MessageContent, existingPlan *PlanningResponse) (*PlanningResponse, []llmtypes.MessageContent, error) {
	planningTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
		// Human feedback is passed directly as userMessage parameter to the planning agent
		// It will be included in the update prompt template when in UPDATE mode
	}

	// Always pass plan.json contents in template - never let agent read from workspace
	// Use the provided existingPlan parameter if available (for UPDATE mode), otherwise nil (for CREATE mode)
	// Do NOT check disk as fallback - this prevents accidentally using old plans when creating new ones
	var planToUse *PlanningResponse
	if existingPlan != nil {
		planToUse = existingPlan
		hcpo.GetLogger().Info(fmt.Sprintf("📄 Using provided existing plan with %d steps (UPDATE mode)", len(existingPlan.Steps)))
	} else {
		planToUse = nil
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No existing plan provided - creating new plan (CREATE mode)"))
	}

	// Serialize plan to JSON and pass in template (prevents agent from reading workspace)
	if planToUse != nil {
		existingPlanJSON, err := json.MarshalIndent(planToUse, "", "  ")
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal existing plan to JSON: %v", err))
		} else {
			planningTemplateVars["ExistingPlanJSON"] = string(existingPlanJSON)
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Passing plan contents in template (prevents workspace file reads)"))
		}
	}

	// Add variable names if available (planning agent should preserve variable placeholders)
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		planningTemplateVars["VariableNames"] = variableNames
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Passing variable names to planning agent (for placeholder preservation)"))
	}

	// Determine user message based on mode
	// - For CREATE mode: Use clear, action-oriented instruction
	// - For UPDATE mode: Use human feedback if provided, otherwise clear instruction
	var userMessage string
	if existingPlan != nil {
		// UPDATE mode: Use human feedback as user message (user's natural language feedback)
		if humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
			userMessage = humanFeedback
		} else {
			// Check if plan has validation errors
			validationErr := validatePlanStepIDs(existingPlan.Steps)
			if validationErr != nil {
				// Fallback: Clear instruction for plan updates with validation error fix
				userMessage = fmt.Sprintf("Review the existing plan and fix any validation errors. The plan has validation issues: %v. Use the plan modification tools (update_plan_steps, delete_plan_steps, add_regular_step, add_conditional_step, add_decision_step, add_loop_step) to fix validation errors and make any other changes. Always use human_feedback tool first to confirm changes with the user.", validationErr)
			} else {
				// Fallback: Clear instruction for plan updates
				userMessage = "Review the existing plan and update it based on the objective. Use the plan modification tools (update_plan_steps, delete_plan_steps, add_regular_step, add_conditional_step, add_decision_step, add_loop_step) to make changes. Always use human_feedback tool first to confirm changes with the user."
			}
		}
	} else {
		// CREATE mode: Clear, action-oriented instruction for first-time plan generation
		// System prompt contains all detailed guidelines - user message should be concise and directive
		userMessage = "Generate a comprehensive structured plan to achieve the objective. Use type-specific tools (add_regular_step, add_conditional_step, add_decision_step, add_loop_step) to build the plan incrementally, starting with the first step."
	}

	// Create fresh planning agent with proper context
	planningAgent, err := hcpo.createPlanningAgent(ctx, "planning", 0, iteration)
	if err != nil {
		return nil, nil, fmt.Errorf(fmt.Sprintf("failed to create planning agent: %w", err), nil)
	}

	// Execute planning agent using plan modification tools (unified for both CREATE and UPDATE modes)
	planningAgentTyped, ok := planningAgent.(*HumanControlledTodoPlannerPlanningAgent)
	if !ok {
		return nil, nil, fmt.Errorf(fmt.Sprintf("failed to cast planning agent to correct type"), nil)
	}

	// Determine if we're in UPDATE mode
	isUpdateMode := existingPlan != nil

	// Reset changelog session at the start of a new planning agent execution
	resetChangelogSession()

	workspacePath := hcpo.GetWorkspacePath()

	// If CREATE mode and no plan exists, create empty plan.json
	if !isUpdateMode {
		emptyPlan := &PlanningResponse{Steps: []PlanStep{}}
		if err := writePlanToFile(ctx, workspacePath, emptyPlan, nil, hcpo.WriteWorkspaceFile, hcpo.GetLogger()); err != nil {
			return nil, nil, fmt.Errorf(fmt.Sprintf("failed to create empty plan.json: %w", err), nil)
		}
	}

	// Get the underlying base agent
	baseAgent := planningAgentTyped.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil {
		return nil, nil, fmt.Errorf(fmt.Sprintf("base agent is not initialized"), nil)
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, nil, fmt.Errorf(fmt.Sprintf("MCP agent is not initialized"), nil)
	}

	// Register WorkspaceTools (including human_feedback) before plan modification tools
	if hcpo.BaseOrchestrator != nil {
		toolsToRegister := hcpo.BaseOrchestrator.WorkspaceTools
		executorsToUse := hcpo.BaseOrchestrator.WorkspaceToolExecutors

		if toolsToRegister != nil && executorsToUse != nil {
			toolsToRegister, wrappedExecutors := hcpo.BaseOrchestrator.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Registering %d workspace tools (including human_feedback) for planning agent", len(toolsToRegister)))

			for _, tool := range toolsToRegister {
				if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
					var params map[string]interface{}
					if tool.Function.Parameters != nil {
						paramsBytes, err := json.Marshal(tool.Function.Parameters)
						if err == nil {
							json.Unmarshal(paramsBytes, &params)
						}
					}
					if params == nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to convert parameters for tool %s", tool.Function.Name))
						continue
					}

					if toolExecutor, ok := executor.(func(context.Context, map[string]interface{}) (string, error)); ok {
						if err := mcpAgent.RegisterCustomTool(
							tool.Function.Name,
							tool.Function.Description,
							params,
							toolExecutor,
							"workspace",
						); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to register workspace tool %s: %v", tool.Function.Name, err))
						} else {
							hcpo.GetLogger().Debug(fmt.Sprintf("✅ Registered workspace tool: %s", tool.Function.Name))
						}
					}
				}
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Registered workspace tools for planning agent"))
		}
	}

	// Register all plan modification tools using shared function
	if err := registerPlanModificationTools(mcpAgent, workspacePath, hcpo.GetLogger(), hcpo.ReadWorkspaceFile, hcpo.WriteWorkspaceFile, hcpo.MoveWorkspaceFile, "planning agent"); err != nil {
		return nil, nil, err
	}

	// Register variable extraction tools (extract_variables, update_variable)
	if err := registerVariableExtractionTools(mcpAgent, workspacePath, hcpo.GetLogger(), hcpo.ReadWorkspaceFile, hcpo.WriteWorkspaceFile, "planning agent"); err != nil {
		return nil, nil, err
	}

	// Generate system prompt based on mode
	var systemPrompt string
	if isUpdateMode {
		systemPrompt = planningSystemPromptProcessorForUpdate(planningTemplateVars)
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 UPDATE mode: Using update system prompt"))
	} else {
		systemPrompt = planningSystemPromptProcessorForCreate(planningTemplateVars)
		hcpo.GetLogger().Info(fmt.Sprintf("📝 CREATE mode: Using create system prompt"))
	}

	// Create input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute using ExecuteWithTemplateValidation (standard pattern used by other agents)
	// This includes automatic event emission (agent start/end events)
	_, updatedConversationHistory, err := planningAgentTyped.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, planningTemplateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
	if err != nil {
		return nil, updatedConversationHistory, fmt.Errorf(fmt.Sprintf("agent execution failed: %w", err), nil)
	}

	// Check if any plan modification tools were called
	toolCalls := extractToolCallsFromMessages(updatedConversationHistory)
	planUpdateToolCalled := false
	for _, toolName := range toolCalls {
		if toolName == "update_plan_steps" || toolName == "delete_plan_steps" || toolName == "add_regular_step" || toolName == "add_conditional_step" || toolName == "add_decision_step" || toolName == "add_loop_step" ||
			toolName == "convert_step_to_conditional" || toolName == "add_branch_steps" || toolName == "update_branch_steps" ||
			toolName == "delete_branch_steps" || toolName == "update_conditional_step" || toolName == "convert_conditional_to_regular" {
			planUpdateToolCalled = true
		}
	}

	// Read the current plan.json (whether tools were called or not)
	planResponse, err := readPlanFromFile(ctx, workspacePath, hcpo.ReadWorkspaceFile)
	if err != nil {
		return nil, updatedConversationHistory, fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
	}

	if !planUpdateToolCalled {
		// No tools called - conversational response
		if isUpdateMode {
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Planning agent in UPDATE mode: Conversational response (no plan changes). Returning current plan."))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Planning agent in CREATE mode: Conversational response (no plan changes). Returning current plan."))
		}
		return planResponse, updatedConversationHistory, nil
	}

	// Tools were called - plan.json was updated
	if isUpdateMode {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Plan updated via tools (%d steps)", len(planResponse.Steps)))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Plan created via tools (%d steps)", len(planResponse.Steps)))
	}

	// Emit event to notify frontend that plan was updated
	if hcpo.BaseOrchestrator != nil {
		CheckAndEmitPlanUpdateEvent(ctx, hcpo.BaseOrchestrator, updatedConversationHistory, workspacePath, hcpo.ReadWorkspaceFile)
	}

	// Validate that all steps have IDs (planning agent should always generate them)
	if err := validatePlanStepIDs(planResponse.Steps); err != nil {
		return nil, nil, fmt.Errorf(fmt.Sprintf("plan validation failed: %w", err), nil)
	}

	// Plan is already saved by tools (both CREATE and UPDATE modes)
	// Planning agent creates empty plan.json in CREATE mode, then tools add steps
	// In UPDATE mode, tools modify existing plan.json
	// Both modes: plan.json is already up-to-date, no need to save again
	if isUpdateMode {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Plan updated via tools (%d steps, conversation has %d messages)", len(planResponse.Steps), len(updatedConversationHistory)))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Plan created via tools (%d steps, conversation has %d messages)", len(planResponse.Steps), len(updatedConversationHistory)))
	}

	return planResponse, updatedConversationHistory, nil
}

// createPlanningAgent creates a planning agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from learnings and planning, writes to both planning and learnings (for folder syncing)
	baseWorkspacePath := hcpo.GetWorkspacePath()
	planningPath := fmt.Sprintf("%s/planning", baseWorkspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)

	// Read paths: learnings (for reading existing folders), planning is automatically readable since it's in writePaths
	readPaths := []string{learningsPath}
	// Write paths: planning (for plan.json) and learnings (for renaming folders when step numbering changes)
	writePaths := []string{planningPath, learningsPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for planning agent - Read paths: %v, Write paths: %v (write access to learnings/ for folder syncing)", readPaths, writePaths))

	// Determine LLM config: Priority: presetPlanningLLM > presetLearningLLM > orchestrator default
	var llmConfigToUse *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if hcpo.presetPlanningLLM != nil && hcpo.presetPlanningLLM.Provider != "" && hcpo.presetPlanningLLM.ModelID != "" {
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:       hcpo.presetPlanningLLM.Provider,
			ModelID:        hcpo.presetPlanningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default planning LLM: %s/%s", hcpo.presetPlanningLLM.Provider, hcpo.presetPlanningLLM.ModelID))
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		// Fallback to learning LLM if planning LLM not set
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset learning LLM as fallback for planning: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID))
	} else {
		llmConfigToUse = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default planning LLM: %s/%s", hcpo.GetProvider(), hcpo.GetModel()))
	}

	// Use CreateAndSetupStandardAgentWithCustomServers instead of CreateAndSetupStandardAgentWithCustomServersAndSystemPrompt
	// because system prompt is passed directly to the planning agent's Execute() method
	// Planning agent uses plan modification tools (registered in runPlanningPhase)
	// Create agent config with custom LLM
	agentConfig := hcpo.CreateStandardAgentConfigWithLLM("human-controlled-planning-agent", hcpo.GetMaxTurns(), agents.OutputFormatStructured, llmConfigToUse)
	agentConfig.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM planning agent

	// Code execution mode only applies to execution agents, not planning agents
	agentConfig.UseCodeExecutionMode = false
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for planning agent (only execution agents use MCP tools)"))

	// Disable large output virtual tools for planning agent
	disabled := false
	agentConfig.EnableLargeOutputVirtualTools = &disabled
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling large output virtual tools for planning agent"))

	// Create agent using provided factory function
	agent := NewHumanControlledTodoPlannerPlanningAgent(agentConfig, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to initialize planning agent: %w", err), nil)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware event bridge is nil for planning agent"), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Checking agent structure for planning agent"))
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("base agent is nil for planning agent"), nil)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("MCP agent is nil for planning agent"), nil)
	}

	// 🔗 Connect agent to orchestrator's main event bridge using existing bridge (reuse)
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(interface {
		SetOrchestratorContext(phase string, step int, agentName string)
	}); ok {
		cab.SetOrchestratorContext(phase, step, baseAgentName)
		mcpAgent.AddEventListener(eventBridge)
		hcpo.GetLogger().Info(fmt.Sprintf("🔗 Reused context-aware bridge connected to %s (step %d, agent %s)", phase, step+1, baseAgentName))
	} else {
		mcpAgent.AddEventListener(eventBridge)
		hcpo.GetLogger().Info(fmt.Sprintf("🔗 Connected event bridge to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName))
	}

	return agent, nil
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the generic ReadWorkspaceFile function from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Checking for existing plan at %s", planPath))

	// Use the generic ReadWorkspaceFile function from base orchestrator
	planContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Info(fmt.Sprintf("📋 No existing plan found: %v", err))
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf(fmt.Sprintf("failed to check existing plan: %w", err), nil)
	}

	// Parse JSON content to PlanningResponse
	var planResponse PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &planResponse); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing plan.json: %v", err))
		return false, nil, fmt.Errorf(fmt.Sprintf("failed to parse plan.json: %w", err), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(planResponse.Steps)))
	return true, &planResponse, nil
}

// requestPlanApproval requests human approval for the generated plan
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestPlanApproval(
	ctx context.Context,
	revisionAttempt int,
) (bool, string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("⏸️ Requesting human approval for plan (attempt %d)", revisionAttempt))

	// Generate unique request ID
	requestID := fmt.Sprintf("plan_approval_%d_%d", time.Now().UnixNano(), revisionAttempt)

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		"Please review the plan and provide approval or feedback",
		"", // No additional context for plan approval
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

// convertPlanStepsToTodoSteps converts PlanStep to TodoStep format
// Merges agent configs from step_config.json by step index matching
// convertBranchSteps converts a slice of PlanStep to TodoStep (helper for recursive conversion)
// stepConfigs: step configs array for matching branch step configs by ID
func convertBranchSteps(planSteps []PlanStep, stepConfigs []StepConfig) ([]TodoStep, error) {
	if len(planSteps) == 0 {
		return nil, nil
	}
	todoSteps := make([]TodoStep, len(planSteps))
	for i := range planSteps {
		step := planSteps[i]
		// Steps always have IDs from backend - match config by step ID
		var agentConfigs *AgentConfigs
		if step.ID == "" {
			// This should never happen - steps always have IDs from backend
			// Throw error to match frontend behavior and catch bugs early
			stepTitle := "unknown"
			if step.Title != "" {
				stepTitle = step.Title
			}
			return nil, fmt.Errorf(fmt.Sprintf("branch step at index %d is missing required ID field. Step title: %q", i, stepTitle), nil)
		} else if stepConfigs != nil {
			// Debug: Log what we're searching for
			// Note: Can't use logger here, but we can add debug info later if needed
			agentConfigs = MatchStepConfigByID(step.ID, stepConfigs)
			// Config will be nil if not found (expected for new steps without saved configs)
			// Config will be non-nil if found (branch step will use its own configs)
		} else {
			// stepConfigs is nil - branch step will use default configs
		}

		// Validation is required for loop steps to check loop conditions
		// Ensure validation is not disabled for loop steps
		if step.HasLoop && agentConfigs != nil && agentConfigs.DisableValidation != nil && *agentConfigs.DisableValidation {
			// Create a copy of configs with validation enabled
			enabledConfigs := *agentConfigs
			val := false
			enabledConfigs.DisableValidation = &val
			agentConfigs = &enabledConfigs
		}

		// Recursively convert nested branch steps
		var ifTrueSteps []TodoStep
		if len(step.IfTrueSteps) > 0 {
			var err error
			ifTrueSteps, err = convertBranchSteps(step.IfTrueSteps, stepConfigs)
			if err != nil {
				return nil, fmt.Errorf(fmt.Sprintf("failed to convert if_true branch steps: %w", err), nil)
			}
		}

		var ifFalseSteps []TodoStep
		if len(step.IfFalseSteps) > 0 {
			var err error
			ifFalseSteps, err = convertBranchSteps(step.IfFalseSteps, stepConfigs)
			if err != nil {
				return nil, fmt.Errorf(fmt.Sprintf("failed to convert if_false branch steps: %w", err), nil)
			}
		}

		// Convert decision step if present
		var decisionTodoStep *TodoStep
		if step.HasDecisionStep && step.DecisionStep != nil {
			decisionSteps, err := convertBranchSteps([]PlanStep{*step.DecisionStep}, stepConfigs)
			if err != nil {
				return nil, fmt.Errorf(fmt.Sprintf("failed to convert decision step: %w", err), nil)
			}
			if len(decisionSteps) > 0 {
				decisionTodoStep = &decisionSteps[0]
			}
		}

		todoSteps[i] = TodoStep{
			ID:                         step.ID, // Copy ID from PlanStep for frontend matching
			Title:                      step.Title,
			Description:                step.Description,
			SuccessCriteria:            step.SuccessCriteria,
			ContextDependencies:        step.ContextDependencies,
			ContextOutput:              step.ContextOutput.String(),
			HasLoop:                    step.HasLoop,
			LoopCondition:              step.LoopCondition,
			MaxIterations:              step.MaxIterations,
			LoopDescription:            step.LoopDescription,
			HasCondition:               step.HasCondition,
			ConditionQuestion:          step.ConditionQuestion,
			ConditionContext:           step.ConditionContext,
			IfTrueSteps:                ifTrueSteps,
			IfFalseSteps:               ifFalseSteps,
			IfTrueNextStepID:           step.IfTrueNextStepID,
			IfFalseNextStepID:          step.IfFalseNextStepID,
			HasDecisionStep:            step.HasDecisionStep,
			DecisionStep:               decisionTodoStep,
			DecisionEvaluationQuestion: step.DecisionEvaluationQuestion,
			AgentConfigs:               agentConfigs, // Matched from step_config.json by ID
		}
	}
	return todoSteps, nil
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) convertPlanStepsToTodoSteps(ctx context.Context, planSteps []PlanStep) ([]TodoStep, error) {
	// Read step configs from step_config.json
	stepConfigs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults for all steps)", err))
		stepConfigs = []StepConfig{}
	}

	// Log available config IDs for debugging
	if len(stepConfigs) > 0 {
		configIDs := make([]string, 0, len(stepConfigs))
		for _, config := range stepConfigs {
			if config.ID != "" {
				configIDs = append(configIDs, config.ID)
			}
		}
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Available config IDs in step_config.json: %v", configIDs))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📋 No step configs available (step_config.json is empty or not found)"))
	}

	// Match configs by step index (0-based)
	matchedConfigs, err := MatchStepConfigs(planSteps, stepConfigs)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to match step configs: %w", err), nil)
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Matched %d/%d step configs from step_config.json", len(matchedConfigs), len(planSteps)))

	todoSteps := make([]TodoStep, len(planSteps))
	for i, step := range planSteps {
		// Validate decision step fields before conversion
		if step.HasDecisionStep {
			if step.DecisionStep == nil {
				return nil, fmt.Errorf(fmt.Sprintf("step at index %d (title: %q, ID: %s) has has_decision_step=true but is missing required decision_step field", i, step.Title, step.ID), nil)
			}
			if step.DecisionStep.ID == "" {
				return nil, fmt.Errorf(fmt.Sprintf("step at index %d (title: %q, ID: %s) has decision_step with missing required ID field", i, step.Title, step.ID), nil)
			}
			if step.DecisionEvaluationQuestion == "" {
				return nil, fmt.Errorf(fmt.Sprintf("step at index %d (title: %q, ID: %s) has has_decision_step=true but is missing required decision_evaluation_question field", i, step.Title, step.ID), nil)
			}
			if step.IfTrueNextStepID == "" {
				return nil, fmt.Errorf(fmt.Sprintf("step at index %d (title: %q, ID: %s) has has_decision_step=true but is missing required if_true_next_step_id field", i, step.Title, step.ID), nil)
			}
			if step.IfFalseNextStepID == "" {
				return nil, fmt.Errorf(fmt.Sprintf("step at index %d (title: %q, ID: %s) has has_decision_step=true but is missing required if_false_next_step_id field", i, step.Title, step.ID), nil)
			}
		}

		// Get matched config for this step (may be nil if no match)
		var agentConfigs *AgentConfigs
		if config, found := matchedConfigs[i]; found {
			agentConfigs = config
			// Log code execution mode for debugging
			if agentConfigs.UseCodeExecutionMode != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("📋 Step '%s' (ID: %s) matched config - use_code_execution_mode: %v", step.Title, step.ID, *agentConfigs.UseCodeExecutionMode))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("📋 Step '%s' (ID: %s) matched config - use_code_execution_mode: nil (will use preset default)", step.Title, step.ID))
			}
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Step '%s' (ID: %s) has NO config match in step_config.json - will use preset defaults", step.Title, step.ID))
		}

		// Validation is required for loop steps to check loop conditions
		// Ensure validation is not disabled for loop steps
		if step.HasLoop && agentConfigs != nil && agentConfigs.DisableValidation != nil && *agentConfigs.DisableValidation {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step '%s' is a loop step but has validation disabled - enabling validation (required for loop condition checks)", step.Title))
			// Create a copy of configs with validation enabled
			enabledConfigs := *agentConfigs
			val := false
			enabledConfigs.DisableValidation = &val
			agentConfigs = &enabledConfigs
		}

		// Conditional steps should never have validation - they only evaluate conditions
		// Ensure validation is disabled for conditional steps
		if step.HasCondition {
			if agentConfigs == nil {
				// Create new configs with validation disabled
				val := true
				agentConfigs = &AgentConfigs{
					DisableValidation: &val,
				}
				hcpo.GetLogger().Info(fmt.Sprintf("🔧 Conditional step '%s' - created configs with validation disabled", step.Title))
			} else if agentConfigs.DisableValidation == nil || !*agentConfigs.DisableValidation {
				// Validation is not disabled - force disable it
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step '%s' is a conditional step but has validation enabled - disabling validation (conditional steps only evaluate conditions)", step.Title))
				// Create a copy of configs with validation disabled
				disabledConfigs := *agentConfigs
				val := true
				disabledConfigs.DisableValidation = &val
				agentConfigs = &disabledConfigs
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Conditional step '%s' already has validation disabled", step.Title))
			}
		}

		// Convert branch steps recursively
		var ifTrueSteps []TodoStep
		if len(step.IfTrueSteps) > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 Converting %d if_true branch steps for step '%s' (ID: %s)", len(step.IfTrueSteps), step.Title, step.ID))
			var err error
			ifTrueSteps, err = convertBranchSteps(step.IfTrueSteps, stepConfigs)
			if err != nil {
				return nil, fmt.Errorf(fmt.Sprintf("failed to convert if_true branch steps for step '%s': %w", step.Title, err), nil)
			}
			// Log config matching results for branch steps
			for _, branchStep := range ifTrueSteps {
				if branchStep.AgentConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Branch step '%s' (ID: %s) matched config from step_config.json", branchStep.Title, branchStep.ID))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Branch step '%s' (ID: %s) has no config match - will use defaults", branchStep.Title, branchStep.ID))
				}
			}
		}

		var ifFalseSteps []TodoStep
		if len(step.IfFalseSteps) > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 Converting %d if_false branch steps for step '%s' (ID: %s)", len(step.IfFalseSteps), step.Title, step.ID))
			var err error
			ifFalseSteps, err = convertBranchSteps(step.IfFalseSteps, stepConfigs)
			if err != nil {
				return nil, fmt.Errorf(fmt.Sprintf("failed to convert if_false branch steps for step '%s': %w", step.Title, err), nil)
			}
			// Log config matching results for branch steps
			for _, branchStep := range ifFalseSteps {
				if branchStep.AgentConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Branch step '%s' (ID: %s) matched config from step_config.json", branchStep.Title, branchStep.ID))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Branch step '%s' (ID: %s) has no config match - will use defaults", branchStep.Title, branchStep.ID))
				}
			}
		}

		// Convert decision step if present
		var decisionTodoStep *TodoStep
		if step.HasDecisionStep && step.DecisionStep != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 Converting decision step for step '%s' (ID: %s)", step.Title, step.ID))
			decisionSteps, err := convertBranchSteps([]PlanStep{*step.DecisionStep}, stepConfigs)
			if err != nil {
				return nil, fmt.Errorf(fmt.Sprintf("failed to convert decision step for step '%s': %w", step.Title, err), nil)
			}
			if len(decisionSteps) > 0 {
				decisionTodoStep = &decisionSteps[0]
				if decisionTodoStep.AgentConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step '%s' (ID: %s) matched config from step_config.json", decisionTodoStep.Title, decisionTodoStep.ID))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Decision step '%s' (ID: %s) has no config match - will use defaults", decisionTodoStep.Title, decisionTodoStep.ID))
				}
			}
		}

		// Convert FlexibleContextOutput to string for TodoStep
		todoSteps[i] = TodoStep{
			ID:                         step.ID, // Copy ID from PlanStep for frontend matching
			Title:                      step.Title,
			Description:                step.Description,
			SuccessCriteria:            step.SuccessCriteria,
			ContextDependencies:        step.ContextDependencies,
			ContextOutput:              step.ContextOutput.String(), // Convert FlexibleContextOutput to string
			HasLoop:                    step.HasLoop,
			LoopCondition:              step.LoopCondition,
			MaxIterations:              step.MaxIterations,
			LoopDescription:            step.LoopDescription,
			HasCondition:               step.HasCondition,
			ConditionQuestion:          step.ConditionQuestion,
			ConditionContext:           step.ConditionContext,
			IfTrueSteps:                ifTrueSteps,
			IfFalseSteps:               ifFalseSteps,
			IfTrueNextStepID:           step.IfTrueNextStepID,
			IfFalseNextStepID:          step.IfFalseNextStepID,
			HasDecisionStep:            step.HasDecisionStep,
			DecisionStep:               decisionTodoStep,
			DecisionEvaluationQuestion: step.DecisionEvaluationQuestion,
			AgentConfigs:               agentConfigs, // Merged from step_config.json (validation enforced for loops)
		}
	}
	return todoSteps, nil
}

// EmitTodoStepsExtractedEvent emits an event when todo steps are extracted from a plan
// Public method that accepts BaseOrchestrator and other parameters
func EmitTodoStepsExtractedEvent(ctx context.Context, bo *orchestrator.BaseOrchestrator, extractedSteps []TodoStep, planSource, extractionMethod, runFolder, workspacePath string) {
	EmitTodoStepsExtractedEventWithMetadata(ctx, bo, extractedSteps, planSource, extractionMethod, runFolder, workspacePath, nil)
}

// EmitTodoStepsExtractedEventWithMetadata emits an event when todo steps are extracted from a plan with optional metadata
// Metadata can include changed_step_ids and deleted_step_ids for granular event handling
func EmitTodoStepsExtractedEventWithMetadata(ctx context.Context, bo *orchestrator.BaseOrchestrator, extractedSteps []TodoStep, planSource, extractionMethod, runFolder, workspacePath string, metadata map[string]interface{}) {
	if bo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data with metadata
	baseEventData := events.BaseEventData{
		Timestamp: time.Now(),
	}
	if metadata != nil {
		baseEventData.Metadata = metadata
	}

	eventData := &TodoStepsExtractedEvent{
		BaseEventData:       baseEventData,
		TotalStepsExtracted: len(extractedSteps),
		ExtractedSteps:      extractedSteps,
		ExtractionMethod:    extractionMethod,
		PlanSource:          planSource,
		WorkspacePath:       workspacePath,
		RunFolder:           runFolder,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.TodoStepsExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := bo.GetContextAwareBridge()
	if bridge == nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ [EmitTodoStepsExtractedEventWithMetadata] ContextAwareBridge is nil, cannot emit event"))
		return
	}
	bo.GetLogger().Info(fmt.Sprintf("📤 [EmitTodoStepsExtractedEventWithMetadata] About to emit event through bridge (bridge type: %T, metadata keys: %v)", bridge, getMetadataKeys(metadata)))
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ [EmitTodoStepsExtractedEventWithMetadata] Failed to emit todo steps extracted event: %w", err))
	} else {
		bo.GetLogger().Info(fmt.Sprintf("✅ [EmitTodoStepsExtractedEventWithMetadata] Successfully emitted todo steps extracted event: %d steps extracted", len(extractedSteps)))
	}
}

// getMetadataKeys returns a slice of metadata keys for logging
func getMetadataKeys(metadata map[string]interface{}) []string {
	if metadata == nil {
		return []string{}
	}
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	return keys
}

// emitTodoStepsExtractedEvent is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitTodoStepsExtractedEvent(ctx context.Context, extractedSteps []TodoStep, planSource string) {
	// Use default extraction method and workspace path from orchestrator
	EmitTodoStepsExtractedEvent(ctx, hcpo.BaseOrchestrator, extractedSteps, planSource, "structured_breakdown_agent", "", hcpo.GetWorkspacePath())
}

// IsPlanModificationTool checks if a tool name is a plan modification tool
func IsPlanModificationTool(name string) bool {
	return name == "update_plan_steps" || name == "delete_plan_steps" || name == "add_regular_step" || name == "add_conditional_step" || name == "add_decision_step" || name == "add_loop_step" ||
		name == "convert_step_to_conditional" || name == "add_branch_steps" || name == "update_branch_steps" ||
		name == "delete_branch_steps" || name == "update_conditional_step" || name == "convert_conditional_to_regular"
}

// IsStepConfigModificationTool checks if a tool name is a step_config modification tool
func IsStepConfigModificationTool(name string) bool {
	return name == "update_step_config_tools"
}

// ExtractToolCallsFromMessages scans messages for tool calls and returns the tool names that were called
// This is a public version of extractToolCallsFromMessages for use by other agents
func ExtractToolCallsFromMessages(messages []llmtypes.MessageContent) []string {
	toolNames := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != llmtypes.ChatMessageTypeAI {
			continue
		}
		for _, part := range msg.Parts {
			if toolCall, ok := part.(llmtypes.ToolCall); ok {
				if toolCall.FunctionCall != nil {
					toolNames[toolCall.FunctionCall.Name] = true
				}
			}
		}
	}
	result := make([]string, 0, len(toolNames))
	for name := range toolNames {
		result = append(result, name)
	}
	return result
}

// ChangedStepIDs contains step IDs that were added, updated, or deleted
type ChangedStepIDs struct {
	Added   []string
	Updated []string
	Deleted []string
}

// ExtractChangedStepIDsFromMessages extracts which specific step IDs were changed from plan modification tool calls
// Returns changed step IDs grouped by operation type (added, updated, deleted)
func ExtractChangedStepIDsFromMessages(messages []llmtypes.MessageContent) ChangedStepIDs {
	changed := ChangedStepIDs{
		Added:   []string{},
		Updated: []string{},
		Deleted: []string{},
	}

	for _, msg := range messages {
		if msg.Role != llmtypes.ChatMessageTypeAI {
			continue
		}
		for _, part := range msg.Parts {
			if toolCall, ok := part.(llmtypes.ToolCall); ok {
				if toolCall.FunctionCall == nil {
					continue
				}

				toolName := toolCall.FunctionCall.Name
				args := toolCall.FunctionCall.Arguments

				// Parse arguments JSON
				var argsMap map[string]interface{}
				if err := json.Unmarshal([]byte(args), &argsMap); err != nil {
					continue
				}

				switch toolName {
				case "update_plan_steps":
					// Extract existing_step_id from each updated step
					if updatedStepsRaw, ok := argsMap["updated_steps"].([]interface{}); ok {
						for _, stepRaw := range updatedStepsRaw {
							if stepMap, ok := stepRaw.(map[string]interface{}); ok {
								if stepID, ok := stepMap["existing_step_id"].(string); ok && stepID != "" {
									changed.Updated = append(changed.Updated, stepID)
								}
							}
						}
					}

				case "delete_plan_steps":
					// Extract deleted_step_ids array
					if deletedIDsRaw, ok := argsMap["deleted_step_ids"].([]interface{}); ok {
						for _, idRaw := range deletedIDsRaw {
							if stepID, ok := idRaw.(string); ok && stepID != "" {
								changed.Deleted = append(changed.Deleted, stepID)
							}
						}
					}

				case "add_regular_step", "add_conditional_step", "add_decision_step", "add_loop_step":
					// Extract id from new step
					if stepID, ok := argsMap["id"].(string); ok && stepID != "" {
						changed.Added = append(changed.Added, stepID)
					}

				case "add_branch_steps":
					// Extract step IDs from branch steps
					if branchType, ok := argsMap["branch_type"].(string); ok {
						var stepsKey string
						if branchType == "true" {
							stepsKey = "if_true_steps"
						} else if branchType == "false" {
							stepsKey = "if_false_steps"
						}
						if stepsKey != "" {
							if stepsRaw, ok := argsMap[stepsKey].([]interface{}); ok {
								for _, stepRaw := range stepsRaw {
									if stepMap, ok := stepRaw.(map[string]interface{}); ok {
										if stepID, ok := stepMap["id"].(string); ok && stepID != "" {
											changed.Added = append(changed.Added, stepID)
										}
									}
								}
							}
						}
					}

				case "update_branch_steps":
					// Extract step IDs from updated branch steps
					if branchType, ok := argsMap["branch_type"].(string); ok {
						var stepsKey string
						if branchType == "true" {
							stepsKey = "if_true_steps"
						} else if branchType == "false" {
							stepsKey = "if_false_steps"
						}
						if stepsKey != "" {
							if stepsRaw, ok := argsMap[stepsKey].([]interface{}); ok {
								for _, stepRaw := range stepsRaw {
									if stepMap, ok := stepRaw.(map[string]interface{}); ok {
										if stepID, ok := stepMap["id"].(string); ok && stepID != "" {
											changed.Updated = append(changed.Updated, stepID)
										}
									}
								}
							}
						}
					}

				case "delete_branch_steps":
					// Extract step IDs from deleted branch steps
					if deletedIDsRaw, ok := argsMap["deleted_step_ids"].([]interface{}); ok {
						for _, idRaw := range deletedIDsRaw {
							if stepID, ok := idRaw.(string); ok && stepID != "" {
								changed.Deleted = append(changed.Deleted, stepID)
							}
						}
					}

				case "convert_step_to_conditional", "update_conditional_step":
					// Extract existing_step_id
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}

				case "convert_conditional_to_regular":
					// Extract existing_step_id
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}
				}
			}
		}
	}

	// Remove duplicates
	changed.Added = removeDuplicates(changed.Added)
	changed.Updated = removeDuplicates(changed.Updated)
	changed.Deleted = removeDuplicates(changed.Deleted)

	return changed
}

// removeDuplicates removes duplicate strings from a slice
func removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// CheckAndEmitPlanUpdateEvent checks if plan/step_config modification tools were called
// and emits todo_steps_extracted event if so. This helper can be used by any agent
// that modifies plan.json or step_config.json to ensure the frontend is notified.
//
// Parameters:
//   - ctx: context for the operation
//   - bo: BaseOrchestrator for event emission and logging
//   - conversationHistory: messages from the agent execution to check for tool calls
//   - workspacePath: workspace path for reading plan.json
//   - readFile: function to read files from workspace
func CheckAndEmitPlanUpdateEvent(
	ctx context.Context,
	bo *orchestrator.BaseOrchestrator,
	conversationHistory []llmtypes.MessageContent,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) {
	if bo == nil {
		// Log at info level so we can see if this is the issue
		return
	}

	bo.GetLogger().Info(fmt.Sprintf("🔍 [CheckAndEmitPlanUpdateEvent] Checking conversation history for plan modification tools (history length: %d)", len(conversationHistory)))

	// Extract tool calls from conversation history
	toolCalls := ExtractToolCallsFromMessages(conversationHistory)
	bo.GetLogger().Info(fmt.Sprintf("🔍 [CheckAndEmitPlanUpdateEvent] Extracted %d tool calls: %v", len(toolCalls), toolCalls))

	// Check if any plan or step_config modification tool was called
	needsEvent := false
	for _, name := range toolCalls {
		if IsPlanModificationTool(name) || IsStepConfigModificationTool(name) {
			needsEvent = true
			bo.GetLogger().Info(fmt.Sprintf("🔍 [CheckAndEmitPlanUpdateEvent] Found plan modification tool: %s", name))
			break
		}
	}

	if !needsEvent {
		bo.GetLogger().Info(fmt.Sprintf("📋 [CheckAndEmitPlanUpdateEvent] No plan/step_config modification tools called, skipping event emission"))
		return
	}

	bo.GetLogger().Info(fmt.Sprintf("📋 Plan/step_config modification detected, emitting update event..."))

	// Extract changed step IDs from tool call arguments (granular event data)
	changedStepIDs := ExtractChangedStepIDsFromMessages(conversationHistory)
	bo.GetLogger().Info(fmt.Sprintf("🔍 [CheckAndEmitPlanUpdateEvent] Extracted changed step IDs: added=%d, updated=%d, deleted=%d",
		len(changedStepIDs.Added), len(changedStepIDs.Updated), len(changedStepIDs.Deleted)))
	if len(changedStepIDs.Added) > 0 {
		bo.GetLogger().Info(fmt.Sprintf("   Added: %v", changedStepIDs.Added))
	}
	if len(changedStepIDs.Updated) > 0 {
		bo.GetLogger().Info(fmt.Sprintf("   Updated: %v", changedStepIDs.Updated))
	}
	if len(changedStepIDs.Deleted) > 0 {
		bo.GetLogger().Info(fmt.Sprintf("   Deleted: %v", changedStepIDs.Deleted))
	}

	// Read current plan
	plan, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read plan for event emission: %v", err))
		return
	}

	if plan == nil || len(plan.Steps) == 0 {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Plan is empty, skipping event emission"))
		return
	}

	// Convert plan steps to TodoStep format for the event
	// Note: We use a simplified conversion here since we don't have access to step_config.json
	// The frontend will merge step_config.json when it receives the event and refreshes
	todoSteps := make([]TodoStep, len(plan.Steps))
	for i, step := range plan.Steps {
		todoSteps[i] = TodoStep{
			ID:                  step.ID,
			Title:               step.Title,
			Description:         step.Description,
			SuccessCriteria:     step.SuccessCriteria,
			ContextDependencies: step.ContextDependencies,
			ContextOutput:       string(step.ContextOutput), // Cast FlexibleContextOutput to string
			HasLoop:             step.HasLoop,
			LoopCondition:       step.LoopCondition,
			MaxIterations:       step.MaxIterations,
			LoopDescription:     step.LoopDescription,
			HasCondition:        step.HasCondition,
			ConditionQuestion:   step.ConditionQuestion,
			ConditionContext:    step.ConditionContext,
		}
	}

	// Prepare metadata with changed step IDs for granular event handling
	// Combine added and updated into a single "changed_step_ids" array (frontend treats both as "changed")
	metadata := make(map[string]interface{})
	changedStepIDsCombined := make([]string, 0, len(changedStepIDs.Added)+len(changedStepIDs.Updated))
	changedStepIDsCombined = append(changedStepIDsCombined, changedStepIDs.Added...)
	changedStepIDsCombined = append(changedStepIDsCombined, changedStepIDs.Updated...)
	if len(changedStepIDsCombined) > 0 {
		metadata["changed_step_ids"] = changedStepIDsCombined
	}
	if len(changedStepIDs.Deleted) > 0 {
		metadata["deleted_step_ids"] = changedStepIDs.Deleted
	}

	// Emit the event with metadata
	EmitTodoStepsExtractedEventWithMetadata(ctx, bo, todoSteps, "updated_plan", "agent_tool_modification", "", workspacePath, metadata)
	bo.GetLogger().Info(fmt.Sprintf("✅ Emitted plan update event: %d steps (changed: %d added, %d updated, %d deleted)",
		len(todoSteps), len(changedStepIDs.Added), len(changedStepIDs.Updated), len(changedStepIDs.Deleted)))
}

// CreatePlanOnly runs only the planning phase (standalone, independent from CreateTodoList)
// This is a separate workflow phase that can be run independently
// Similar to ExtractVariablesOnly in variable_management.go
func (hcpo *HumanControlledTodoPlannerOrchestrator) CreatePlanOnly(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Starting standalone planning for objective: %s", objective))

	// Set objective and workspace path
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// Check if variables.json exists - OPTIONAL for planning (can proceed without it)
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := hcpo.variableManager.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		// Log error but continue without variables (for new workflows)
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check for existing variables: %v - proceeding without variables", err))
		variablesExist = false
	}

	if variablesExist && existingVariablesManifest != nil {
		// Variables exist - use them
		hcpo.variablesManifest = existingVariablesManifest
		templatedObjective := existingVariablesManifest.Objective
		hcpo.SetObjective(templatedObjective)
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Using templated objective with {{VARIABLES}}: %s", templatedObjective))
	} else {
		// No variables.json - create it with empty variables and the original objective
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No variables.json found - creating new variables.json with original objective"))

		// Create new VariablesManifest with original objective and empty variables
		newManifest := &VariablesManifest{
			Objective:      objective,         // Use the original objective from preset
			Variables:      []Variable{},      // Empty variables array
			Groups:         []VariableGroup{}, // Empty groups
			ExtractionDate: time.Now().Format(time.RFC3339),
		}

		// Write variables.json to workspace
		variablesJSON, err := json.MarshalIndent(newManifest, "", "  ")
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal variables manifest: %v - proceeding without variables", err))
			hcpo.variablesManifest = nil
		} else {
			if err := hcpo.WriteWorkspaceFile(ctx, variablesPath, string(variablesJSON)); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create variables.json: %v - proceeding without variables", err))
				hcpo.variablesManifest = nil
			} else {
				hcpo.variablesManifest = newManifest
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Created variables.json with original objective and empty variables"))
			}
		}
	}

	// Load runtime variable values if provided
	variableValues, err := LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load variable values: %w", err))
	} else {
		hcpo.variableValues = variableValues
	}

	// Check if plan.json already exists
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check for existing plan: %w", err))
		planExists = false
	}

	var approvedPlan *PlanningResponse
	var initialPlanningFeedback string
	var existingPlanForFirstUpdate *PlanningResponse
	// TODO: Commented out - not required for now
	// var eventEmitted bool = false
	// var planSource string = ""

	// If plan exists, always update it (no user choice needed)
	if planExists {
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Found existing plan.json with %d steps - proceeding to UPDATE mode", len(existingPlan.Steps)))

		// Try to emit event immediately so UI can display the existing plan
		// If conversion fails (invalid plan), log warning but continue - agent will fix it
		// TODO: Commented out - not required for now
		// breakdownSteps, err := hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
		// if err != nil {
		// 	hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to convert existing plan steps for UI display: %v. Plan has validation errors - planning agent will fix them.", err))
		// 	// Don't fail here - let the planning agent fix the invalid plan
		// 	// We'll skip emitting the event, but continue with the planning phase
		// } else {
		// 	// TODO: Commented out - not required for now
		// 	// hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")
		// 	eventEmitted = true
		// 	planSource = "existing_plan"
		// 	// hcpo.GetLogger().Info(fmt.Sprintf("📋 Emitted plan event for UI display (%d steps)", len(breakdownSteps)))
		// }
		// TODO: Commented out - not required for now
		// eventEmitted = true
		// planSource = "existing_plan"

		// Request human feedback about what they want to update in the plan
		// If plan has validation errors, inform the user that the agent will fix them
		updatePrompt := "What would you like to update in the existing plan? Please describe the changes or improvements you want."
		updateContext := fmt.Sprintf("Current plan location: %s\nFound %d steps\n\nYour feedback will be used to guide the creation of an updated plan while preserving existing validation, learning, and execution artifacts.", planPath, len(existingPlan.Steps))

		// Check if plan has validation errors by attempting validation
		if validationErr := validatePlanStepIDs(existingPlan.Steps); validationErr != nil {
			updatePrompt = "The existing plan has validation errors that need to be fixed. What would you like to update in the plan?"
			updateContext = fmt.Sprintf("Current plan location: %s\nFound %d steps\n\n⚠️ Plan validation errors detected: %v\n\nThe planning agent will automatically fix these validation errors. You can also describe any additional changes or improvements you want.\n\nYour feedback will be used to guide the creation of an updated plan while preserving existing validation, learning, and execution artifacts.", planPath, len(existingPlan.Steps), validationErr)
		}

		updateFeedbackID := fmt.Sprintf("plan_update_feedback_%d", time.Now().UnixNano())
		approved, updateFeedback, err := hcpo.RequestHumanFeedback(
			ctx,
			updateFeedbackID,
			updatePrompt,
			updateContext,
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to get update feedback: %v, proceeding without specific update guidance", err))
			initialPlanningFeedback = ""
		} else if approved {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ User approved without providing update feedback, will create updated plan without specific guidance"))
			initialPlanningFeedback = ""
		} else if updateFeedback != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Received update feedback: %s", updateFeedback))
			initialPlanningFeedback = updateFeedback
		} else {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Unexpected feedback state: approved=%v, feedback empty, proceeding without guidance", approved))
			initialPlanningFeedback = ""
		}

		// Set up for UPDATE mode - will go through planning phase to update the plan
		existingPlanForFirstUpdate = existingPlan
		planExists = false // Set to false so it goes into the planning loop
	}

	// Run planning phase if plan doesn't exist (CREATE mode) or if existing plan needs update (UPDATE mode)
	if !planExists && approvedPlan == nil {
		if existingPlanForFirstUpdate != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Updating existing plan (UPDATE mode)"))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Creating new plan to execute objective (CREATE mode)"))
		}

		maxPlanRevisions := 20
		humanFeedback := initialPlanningFeedback
		var planningConversationHistory []llmtypes.MessageContent

		for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Plan creation/approval attempt %d/%d", revisionAttempt, maxPlanRevisions))

			var existingPlanForUpdate *PlanningResponse
			if revisionAttempt == 1 && existingPlanForFirstUpdate != nil {
				existingPlanForUpdate = existingPlanForFirstUpdate
			} else if revisionAttempt > 1 && approvedPlan != nil {
				existingPlanForUpdate = approvedPlan
			}

			var err error
			approvedPlan, planningConversationHistory, err = hcpo.runPlanningPhase(ctx, revisionAttempt, humanFeedback, planningConversationHistory, existingPlanForUpdate)
			if err != nil {
				errMsg := err.Error()

				// Check for conversational approval sentinel (UPDATE mode - no plan update needed)
				if strings.HasPrefix(errMsg, "PLANNING_CONVERSATIONAL_APPROVED:") {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ User approved conversational response - no plan update needed. Using existing plan."))
					// Use existing plan since no update is needed
					if existingPlanForUpdate != nil {
						approvedPlan = existingPlanForUpdate
						// planningConversationHistory already updated from runPlanningPhase
						break
					} else {
						// This shouldn't happen in UPDATE mode, but handle gracefully
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Conversational approval received but no existing plan available"))
						return "", fmt.Errorf(fmt.Sprintf("conversational approval received but no existing plan to use"), nil)
					}
				}

				feedbackPrefix := "PLANNING_TEXT_RESPONSE_FEEDBACK:"
				if strings.Contains(errMsg, feedbackPrefix) {
					parts := strings.Split(errMsg, feedbackPrefix)
					if len(parts) > 1 {
						extractedFeedback := strings.TrimSpace(parts[1])
						humanFeedback = extractedFeedback
						if revisionAttempt >= maxPlanRevisions {
							return "", fmt.Errorf(fmt.Sprintf("max plan revision attempts (%d) reached", maxPlanRevisions), nil)
						}
						continue
					}
				}
				return "", fmt.Errorf(fmt.Sprintf("planning phase failed: %w", err), nil)
			}

			if len(approvedPlan.Steps) == 0 {
				return "", fmt.Errorf(fmt.Sprintf("new plan has no steps: planning agent returned empty steps array"), nil)
			}

			// Convert approved plan steps to TodoStep format
			breakdownSteps, err := hcpo.convertPlanStepsToTodoSteps(ctx, approvedPlan.Steps)
			if err != nil {
				return "", fmt.Errorf(fmt.Sprintf("failed to convert approved plan steps: %w", err), nil)
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Converted new plan: %d steps extracted", len(breakdownSteps)))

			// Emit todo steps extracted event
			// TODO: Commented out - not required for now
			// hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "new_plan")
			// eventEmitted = true
			// planSource = "new_plan"

			// Request human approval for JSON plan
			approvedInternal, feedbackInternal, err := hcpo.requestPlanApproval(ctx, revisionAttempt)
			if err != nil {
				return "", fmt.Errorf(fmt.Sprintf("plan approval request failed: %w", err), nil)
			}

			if approvedInternal {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ JSON plan approved by human"))
				break
			}

			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Plan revision requested (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedbackInternal))
			humanFeedback = feedbackInternal

			if revisionAttempt >= maxPlanRevisions {
				return "", fmt.Errorf(fmt.Sprintf("max plan revision attempts (%d) reached", maxPlanRevisions), nil)
			}
		}

		hcpo.approvedPlan = approvedPlan
	}

	// Ensure event is emitted at the end if we have an approved plan
	// This ensures the UI always sees the plan, even if event was emitted earlier
	// TODO: Commented out - not required for now
	// if approvedPlan != nil && !eventEmitted {
	// 	breakdownSteps, err := hcpo.convertPlanStepsToTodoSteps(ctx, approvedPlan.Steps)
	// 	if err != nil {
	// 		return "", fmt.Errorf(fmt.Sprintf("failed to convert approved plan steps: %w", err), nil)
	// 	}
	// 	// Determine correct source if not already set
	// 	if planSource == "" {
	// 		// If we haven't emitted yet, determine source based on context
	// 		// If we're using the existing plan without modification, it's "existing_plan"
	// 		// Otherwise, it's a "new_plan" (created or updated)
	// 		if existingPlan != nil && approvedPlan == existingPlan {
	// 			planSource = "existing_plan"
	// 		} else {
	// 			planSource = "new_plan"
	// 		}
	// 	}
	// 	// TODO: Commented out - not required for now
	// 	// hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, planSource)
	// }

	// Build result summary
	if approvedPlan != nil {
		var summary strings.Builder
		summary.WriteString("Planning completed successfully.\n\n")
		summary.WriteString(fmt.Sprintf("Created plan with %d steps:\n", len(approvedPlan.Steps)))
		for i, step := range approvedPlan.Steps {
			summary.WriteString(fmt.Sprintf("%d. %s\n", i+1, step.Description))
		}
		return summary.String(), nil
	}

	return "Planning completed (no plan created).", nil
}
