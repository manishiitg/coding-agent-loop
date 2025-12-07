package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
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

// validatePlanStepIDs recursively validates that all steps have IDs
// Throws error if any step is missing an ID
func validatePlanStepIDs(steps []PlanStep) error {
	for i := range steps {
		if steps[i].ID == "" {
			return fmt.Errorf(fmt.Sprintf("step at index %d is missing required ID field. Step title: %q", i, steps[i].Title), nil)
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
		// Human feedback is passed directly as userMessage parameter to ExecuteStructured
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
	// - For CREATE mode: Use "Generate plan"
	// - For UPDATE mode: Use human feedback if provided, otherwise "Generate plan"
	var userMessage string
	if existingPlan != nil {
		// UPDATE mode: Use human feedback as user message
		if humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
			userMessage = humanFeedback
		} else {
			userMessage = "Generate plan" // Fallback if no human feedback
		}
	} else {
		// CREATE mode: Use static message for first-time plan generation
		userMessage = "Generate plan"
	}

	// Create fresh planning agent with proper context
	planningAgent, err := hcpo.createPlanningAgent(ctx, "planning", 0, iteration)
	if err != nil {
		return nil, nil, fmt.Errorf(fmt.Sprintf("failed to create planning agent: %w", err), nil)
	}

	// Execute planning agent using structured output
	planningAgentTyped, ok := planningAgent.(*HumanControlledTodoPlannerPlanningAgent)
	if !ok {
		return nil, nil, fmt.Errorf(fmt.Sprintf("failed to cast planning agent to correct type"), nil)
	}

	// Determine if we're in UPDATE mode
	isUpdateMode := existingPlan != nil

	var planResponse *PlanningResponse
	var updatedConversationHistory []llmtypes.MessageContent

	if isUpdateMode {
		// UPDATE mode: Use ExecuteStructuredUpdate (returns updated PlanningResponse directly)
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 UPDATE mode: Using ExecuteStructuredUpdate"))
		// Pass BaseOrchestrator's file operation methods and BaseOrchestrator to the planning agent
		updatedPlan, updatedHistory, updateErr := planningAgentTyped.ExecuteStructuredUpdate(ctx, planningTemplateVars, conversationHistory, userMessage, hcpo.ReadWorkspaceFile, hcpo.WriteWorkspaceFile, hcpo.BaseOrchestrator)
		if updateErr != nil {
			err = updateErr
			updatedConversationHistory = updatedHistory
		} else {
			// Plan is already updated in plan.json by the tools - just use it
			planResponse = updatedPlan
			updatedConversationHistory = updatedHistory
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Plan updated via tools (%d total steps)", len(updatedPlan.Steps)))
		}
	} else {
		// CREATE mode: Use ExecuteStructured
		hcpo.GetLogger().Info(fmt.Sprintf("📝 CREATE mode: Using ExecuteStructured"))
		planResponse, updatedConversationHistory, err = planningAgentTyped.ExecuteStructured(ctx, planningTemplateVars, conversationHistory, userMessage)
	}

	if err != nil {
		// Debug: Log the error type and message
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Planning agent returned error: %T, message: %s", err, err.Error()))
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] IsNonStructuredResponseError check: %v", agents.IsNonStructuredResponseError(err)))

		// Check if this is a non-structured response error (text response instead of structured output)
		if agents.IsNonStructuredResponseError(err) {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ [DEBUG] Detected NonStructuredResponseError in runPlanningPhase"))
			var nonStructuredErr *agents.NonStructuredResponseError
			if errors.As(err, &nonStructuredErr) {
				// Display the text response to the user and request feedback
				if isUpdateMode {
					hcpo.GetLogger().Info(fmt.Sprintf("📝 Planning agent returned conversational text instead of structured update. This is acceptable when user is just asking questions (no plan changes needed)."))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("📝 Planning agent returned conversational text instead of structured output. Displaying to user for feedback."))
				}

				// Generate unique request ID
				requestID := fmt.Sprintf("planning_text_response_%d_%d", iteration, time.Now().UnixNano())

				// Determine message based on mode
				var feedbackMessage string
				if isUpdateMode {
					feedbackMessage = "The planning agent provided the following conversational response. If this answers your question and no plan update is needed, click Approve. Otherwise, provide feedback to update the plan:"
				} else {
					feedbackMessage = "The planning agent provided the following response instead of a structured plan. Please provide feedback to help it generate a proper structured plan:"
				}

				// Display the text response and request feedback
				approved, feedback, feedbackErr := hcpo.RequestHumanFeedback(
					ctx,
					requestID,
					feedbackMessage,
					nonStructuredErr.TextResponse,
					hcpo.getSessionID(),
					hcpo.getWorkflowID(),
				)

				if feedbackErr != nil {
					return nil, nil, fmt.Errorf(fmt.Sprintf("failed to request human feedback for planning text response: %w", feedbackErr), nil)
				}

				// If user approved (clicked Approve button), treat as no plan update needed (acceptable for UPDATE mode)
				if approved {
					if isUpdateMode {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ User approved conversational response - no plan update needed. This is acceptable in UPDATE mode."))
						// Return error to indicate no plan update (the loop will handle this appropriately)
						return nil, nonStructuredErr.UpdatedHistory, fmt.Errorf(fmt.Sprintf("PLANNING_CONVERSATIONAL_APPROVED:no plan update needed"), nil)
					} else {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ User approved planning text response, but no structured plan was generated. This is unexpected - returning error."))
						return nil, nil, fmt.Errorf(fmt.Sprintf("planning agent returned text response but user approved without providing feedback to generate structured plan"), nil)
					}
				}

				// User provided feedback - return a special error that the loop can detect and handle
				// Use a specific error prefix that the loop will recognize
				// The updated history from the agent's response is included so conversation continues properly
				feedbackError := fmt.Errorf(fmt.Sprintf("PLANNING_TEXT_RESPONSE_FEEDBACK:%s", feedback), nil)
				hcpo.GetLogger().Info(fmt.Sprintf("🔄 [DEBUG] Returning feedback error from runPlanningPhase: %s", feedbackError.Error()))
				return nil, nonStructuredErr.UpdatedHistory, feedbackError
			}
		}
		// For other errors, return as-is
		return nil, nil, fmt.Errorf(fmt.Sprintf("planning failed: %w", err), nil)
	}

	// Only save plan for CREATE mode - UPDATE mode already saved it via tools
	if !isUpdateMode {
		// Validate that all steps have IDs (planning agent should always generate them)
		if err := validatePlanStepIDs(planResponse.Steps); err != nil {
			return nil, nil, fmt.Errorf(fmt.Sprintf("plan validation failed: %w", err), nil)
		}

		// Save JSON plan to file using shared helper (ensures mutex protection)
		planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
		if err := writePlanToFile(ctx, hcpo.GetWorkspacePath(), planResponse, nil, hcpo.WriteWorkspaceFile, hcpo.GetLogger()); err != nil {
			return nil, nil, fmt.Errorf(fmt.Sprintf("failed to save plan.json: %w", err), nil)
		}

		// Note: Learning integration cache removal no longer needed - execution agent auto-discovers files

		hcpo.GetLogger().Info(fmt.Sprintf("✅ JSON plan created successfully and saved to %s (%d steps, conversation has %d messages)", planPath, len(planResponse.Steps), len(updatedConversationHistory)))
	} else {
		// UPDATE mode: Plan already saved by tools, just log
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Plan already saved by tools (%d steps, conversation has %d messages)", len(planResponse.Steps), len(updatedConversationHistory)))
	}

	return planResponse, updatedConversationHistory, nil
}

// createPlanningAgent creates a planning agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from learnings (read-only) and planning (via writePaths), writes only to planning
	baseWorkspacePath := hcpo.GetWorkspacePath()
	planningPath := fmt.Sprintf("%s/planning", baseWorkspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)

	// Only specify learnings in readPaths - planning is automatically readable since it's in writePaths
	readPaths := []string{learningsPath}
	writePaths := []string{planningPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for planning agent - Read paths: %v, Write paths: %v (planning automatically readable via writePaths)", readPaths, writePaths))

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
	// because system prompt is passed directly to ExecuteStructuredWithInputProcessor() in planning_agent.go
	// Planning agent doesn't need custom tools - it only uses structured output tool
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
// stepConfigs: step configs file for matching branch step configs by ID
func convertBranchSteps(planSteps []PlanStep, stepConfigs *StepConfigFile) ([]TodoStep, error) {
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

		todoSteps[i] = TodoStep{
			ID:                  step.ID, // Copy ID from PlanStep for frontend matching
			Title:               step.Title,
			Description:         step.Description,
			SuccessCriteria:     step.SuccessCriteria,
			ContextDependencies: step.ContextDependencies,
			ContextOutput:       step.ContextOutput.String(),
			HasLoop:             step.HasLoop,
			LoopCondition:       step.LoopCondition,
			MaxIterations:       step.MaxIterations,
			LoopDescription:     step.LoopDescription,
			HasCondition:        step.HasCondition,
			ConditionQuestion:   step.ConditionQuestion,
			ConditionContext:    step.ConditionContext,
			IfTrueSteps:         ifTrueSteps,
			IfFalseSteps:        ifFalseSteps,
			AgentConfigs:        agentConfigs, // Matched from step_config.json by ID
		}
	}
	return todoSteps, nil
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) convertPlanStepsToTodoSteps(ctx context.Context, planSteps []PlanStep) ([]TodoStep, error) {
	// Read step configs from step_config.json
	stepConfigs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults for all steps)", err))
		stepConfigs = &StepConfigFile{Steps: []StepConfig{}}
	}

	// Log available config IDs for debugging
	if stepConfigs != nil && len(stepConfigs.Steps) > 0 {
		configIDs := make([]string, 0, len(stepConfigs.Steps))
		for _, config := range stepConfigs.Steps {
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

		// Convert FlexibleContextOutput to string for TodoStep
		todoSteps[i] = TodoStep{
			ID:                  step.ID, // Copy ID from PlanStep for frontend matching
			Title:               step.Title,
			Description:         step.Description,
			SuccessCriteria:     step.SuccessCriteria,
			ContextDependencies: step.ContextDependencies,
			ContextOutput:       step.ContextOutput.String(), // Convert FlexibleContextOutput to string
			HasLoop:             step.HasLoop,
			LoopCondition:       step.LoopCondition,
			MaxIterations:       step.MaxIterations,
			LoopDescription:     step.LoopDescription,
			HasCondition:        step.HasCondition,
			ConditionQuestion:   step.ConditionQuestion,
			ConditionContext:    step.ConditionContext,
			IfTrueSteps:         ifTrueSteps,
			IfFalseSteps:        ifFalseSteps,
			AgentConfigs:        agentConfigs, // Merged from step_config.json (validation enforced for loops)
		}
	}
	return todoSteps, nil
}

// cleanupExistingPlanArtifacts deletes existing plan.json, steps_done.json, and all files in learnings/, execution/, and validation/ directories
// This is called when user chooses to create a new plan instead of using existing one
func (hcpo *HumanControlledTodoPlannerOrchestrator) cleanupExistingPlanArtifacts(ctx context.Context, workspacePath string) error {
	hcpo.GetLogger().Info(fmt.Sprintf("🧹 Starting cleanup of existing plan artifacts"))

	basePath := workspacePath

	// 1. Delete plan.json file
	planJSONPath := fmt.Sprintf("%s/planning/plan.json", basePath)
	if err := hcpo.DeleteWorkspaceFile(ctx, planJSONPath); err != nil {
		// Ignore "file not found" errors, but log others
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete plan.json: %w", err))
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleted plan.json: %s", planJSONPath))
	}

	// 1.5. Delete plan_learnings.json cache (since plan structure will change)
	planLearningsPath := fmt.Sprintf("%s/planning/plan_learnings.json", basePath)
	if err := hcpo.DeleteWorkspaceFile(ctx, planLearningsPath); err != nil {
		// Ignore "file not found" errors, but log others
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete plan_learnings.json: %w", err))
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleted plan_learnings.json: %s", planLearningsPath))
	}

	// 2. Clean all run folders (nuclear option - clean everything when creating new plan)
	runsPath := fmt.Sprintf("%s/runs", basePath)
	exists, _ := hcpo.workspaceFileExists(ctx, runsPath)
	if exists {
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err == nil && len(existingFolders) > 0 {
			hcpo.GetLogger().Info(fmt.Sprintf("📁 Cleaning all run folders (%d found)", len(existingFolders)))
			for _, folder := range existingFolders {
				runFolderPath := fmt.Sprintf("%s/runs/%s", basePath, folder)
				// Clean execution directory in run folder
				executionDir := fmt.Sprintf("%s/execution", runFolderPath)
				if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup execution directory in run folder %s: %w", folder, err))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned up execution directory in run folder: %s", executionDir))
				}
				// Clean steps_done.json from run folder
				stepsDonePath := fmt.Sprintf("%s/steps_done.json", runFolderPath)
				if err := hcpo.DeleteWorkspaceFile(ctx, stepsDonePath); err != nil {
					// Ignore "file not found" errors, but log others
					if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete steps_done.json in run folder %s: %w", folder, err))
					}
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Deleted steps_done.json from run folder: %s", stepsDonePath))
				}
			}
		}
	}

	// 3. Note: learnings/ folder is preserved - deleted manually only

	// 5. Delete all files in old execution/ directory (backward compatibility)
	executionDir := fmt.Sprintf("%s/execution", basePath)
	if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup execution directory: %w", err))
	}

	// Note: steps_done.json is now cleaned from run folders above (step 2), no longer in workspace root

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Cleanup of existing plan artifacts completed"))
	return nil
}

// EmitTodoStepsExtractedEvent emits an event when todo steps are extracted from a plan
// Public method that accepts BaseOrchestrator and other parameters
func EmitTodoStepsExtractedEvent(ctx context.Context, bo *orchestrator.BaseOrchestrator, extractedSteps []TodoStep, planSource, extractionMethod, runFolder, workspacePath string) {
	if bo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &TodoStepsExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
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
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ [EmitTodoStepsExtractedEvent] ContextAwareBridge is nil, cannot emit event"))
		return
	}
	bo.GetLogger().Info(fmt.Sprintf("📤 [EmitTodoStepsExtractedEvent] About to emit event through bridge (bridge type: %T)", bridge))
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ [EmitTodoStepsExtractedEvent] Failed to emit todo steps extracted event: %w", err))
	} else {
		bo.GetLogger().Info(fmt.Sprintf("✅ [EmitTodoStepsExtractedEvent] Successfully emitted todo steps extracted event: %d steps extracted", len(extractedSteps)))
	}
}

// emitTodoStepsExtractedEvent is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitTodoStepsExtractedEvent(ctx context.Context, extractedSteps []TodoStep, planSource string) {
	// Use default extraction method and workspace path from orchestrator
	EmitTodoStepsExtractedEvent(ctx, hcpo.BaseOrchestrator, extractedSteps, planSource, "structured_breakdown_agent", "", hcpo.GetWorkspacePath())
}

// IsPlanModificationTool checks if a tool name is a plan modification tool
func IsPlanModificationTool(name string) bool {
	return name == "update_plan_steps" || name == "delete_plan_steps" || name == "add_plan_steps" ||
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

	// Emit the event
	EmitTodoStepsExtractedEvent(ctx, bo, todoSteps, "updated_plan", "agent_tool_modification", "", workspacePath)
	bo.GetLogger().Info(fmt.Sprintf("✅ Emitted plan update event: %d steps", len(todoSteps)))
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
	var eventEmitted bool = false
	var planSource string = ""

	// If plan exists, emit event immediately so UI can display it while user decides what to do
	if planExists {
		breakdownSteps, err := hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to convert existing plan steps: %w", err), nil)
		}
		hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")
		eventEmitted = true
		planSource = "existing_plan"
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Emitted plan event for UI display (%d steps)", len(breakdownSteps)))
	}

	// If plan exists, ask user if they want to use it, create new, or update existing
	if planExists {
		requestID := fmt.Sprintf("existing_plan_decision_%d", time.Now().UnixNano())
		planOptions := []string{
			"Use Existing Plan",    // Option 0: Use existing plan as-is
			"Create New Plan",      // Option 1: Delete everything and create new plan
			"Update Existing Plan", // Option 2: Create new plan but keep existing artifacts
		}
		planChoice, err := hcpo.RequestMultipleChoiceFeedback(
			ctx,
			requestID,
			"Found existing plan.json. What would you like to do?",
			planOptions,
			fmt.Sprintf("Plan location: %s\nFound %d steps", planPath, len(existingPlan.Steps)),
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to get user decision for existing plan: %w", err))
			planChoice = "option0"
		}

		switch planChoice {
		case "option0":
			// Use existing plan
			hcpo.GetLogger().Info(fmt.Sprintf("✅ User chose to use existing plan"))
			approvedPlan = existingPlan
			hcpo.approvedPlan = approvedPlan
			// Event already emitted above when plan was found

		case "option1":
			// Create new plan - cleanup everything and create fresh plan
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 User chose to create new plan, cleaning up existing plan and related files"))
			if err := hcpo.cleanupExistingPlanArtifacts(ctx, workspacePath); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup existing plan artifacts: %v (will continue anyway)", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Successfully cleaned up existing plan artifacts"))
			}
			planExists = false

		case "option2":
			// Update existing plan - create new plan but keep artifacts (no cleanup)
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 User chose to update existing plan, creating new plan but keeping existing artifacts"))

			// Request human feedback about what they want to update in the plan
			updateFeedbackID := fmt.Sprintf("plan_update_feedback_%d", time.Now().UnixNano())
			approved, updateFeedback, err := hcpo.RequestHumanFeedback(
				ctx,
				updateFeedbackID,
				"What would you like to update in the existing plan? Please describe the changes or improvements you want.",
				fmt.Sprintf("Current plan location: %s\nFound %d steps\n\nYour feedback will be used to guide the creation of an updated plan while preserving existing validation, learning, and execution artifacts.", planPath, len(existingPlan.Steps)),
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

			planExists = false
			existingPlanForFirstUpdate = existingPlan

		default:
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Unknown plan choice: %s, defaulting to use existing plan", planChoice))
			approvedPlan = existingPlan
			hcpo.approvedPlan = approvedPlan
		}
	}

	// Run planning phase if plan doesn't exist or user wants to create/update
	if !planExists && approvedPlan == nil {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Creating new plan to execute objective"))

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
			hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "new_plan")
			eventEmitted = true
			planSource = "new_plan"

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
	if approvedPlan != nil && !eventEmitted {
		breakdownSteps, err := hcpo.convertPlanStepsToTodoSteps(ctx, approvedPlan.Steps)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to convert approved plan steps: %w", err), nil)
		}
		// Determine correct source if not already set
		if planSource == "" {
			// If we haven't emitted yet, determine source based on context
			// If we're using the existing plan (from option0), it's "existing_plan"
			// Otherwise, it's a "new_plan"
			if existingPlan != nil && approvedPlan == existingPlan {
				planSource = "existing_plan"
			} else {
				planSource = "new_plan"
			}
		}
		hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, planSource)
	}

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
