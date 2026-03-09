package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for planning management - panics at startup if invalid
var planningUpdateValidationErrorTemplate = MustRegisterTemplate("planningUpdateValidationError",
	`Review the existing plan, fix the following validation issues, and then update the plan based on the objective and my feedback: {{.ValidationErr}}. Always use the human_feedback tool first to confirm any changes with me.`)

var planningCreateUserMessageTemplate = MustRegisterTemplate("planningCreateUserMessage",
	`Objective: {{.Objective}}

Generate a comprehensive structured plan to achieve this objective.`)

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
// DecisionPlanStep is now flattened - fields are directly on the step
func validateDecisionStepTyped(step PlanStepInterface, stepIndex int) error {
	if decisionStep, ok := step.(*DecisionPlanStep); ok {
		if decisionStep.ID == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required ID field", stepIndex, step.GetTitle())
		}
		if decisionStep.Description == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required description field", stepIndex, step.GetTitle())
		}
		if decisionStep.SuccessCriteria == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required success_criteria field", stepIndex, step.GetTitle())
		}
		if decisionStep.DecisionEvaluationQuestion == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required decision_evaluation_question field", stepIndex, step.GetTitle())
		}
		if decisionStep.IfTrueNextStepID == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required if_true_next_step_id field", stepIndex, step.GetTitle())
		}
		if decisionStep.IfFalseNextStepID == "" {
			return fmt.Errorf("decision step at index %d (title: %q) is missing required if_false_next_step_id field", stepIndex, step.GetTitle())
		}
		// No nested step to validate - DecisionPlanStep is flattened
	}
	return nil
}

// validateRoutingStepTyped validates that a routing step has all required fields
func validateRoutingStepTyped(step PlanStepInterface, stepIndex int) error {
	if routingStep, ok := step.(*RoutingPlanStep); ok {
		if routingStep.ID == "" {
			return fmt.Errorf("routing step at index %d (title: %q) is missing required ID field", stepIndex, step.GetTitle())
		}
		if routingStep.RoutingQuestion == "" {
			return fmt.Errorf("routing step at index %d (title: %q) is missing required routing_question field", stepIndex, step.GetTitle())
		}
		if len(routingStep.Routes) < 2 {
			return fmt.Errorf("routing step at index %d (title: %q) must have at least 2 routes, got %d", stepIndex, step.GetTitle(), len(routingStep.Routes))
		}
		routeIDs := make(map[string]bool)
		for _, route := range routingStep.Routes {
			if route.RouteID == "" {
				return fmt.Errorf("routing step at index %d (title: %q) has a route with empty route_id", stepIndex, step.GetTitle())
			}
			if route.NextStepID == "" {
				return fmt.Errorf("routing step at index %d (title: %q) route %q is missing next_step_id", stepIndex, step.GetTitle(), route.RouteID)
			}
			if routeIDs[route.RouteID] {
				return fmt.Errorf("routing step at index %d (title: %q) has duplicate route_id %q", stepIndex, step.GetTitle(), route.RouteID)
			}
			routeIDs[route.RouteID] = true
		}
		if routingStep.DefaultRouteID != "" && !routeIDs[routingStep.DefaultRouteID] {
			return fmt.Errorf("routing step at index %d (title: %q) has default_route_id %q that doesn't match any route_id", stepIndex, step.GetTitle(), routingStep.DefaultRouteID)
		}
		// If description is set, success_criteria must also be set (execute-then-route mode)
		if routingStep.Description != "" && routingStep.SuccessCriteria == "" {
			return fmt.Errorf("routing step at index %d (title: %q) has description but is missing required success_criteria (execute-then-route mode requires both)", stepIndex, step.GetTitle())
		}
	}
	return nil
}

// validatePlanStepIDs recursively validates that all steps have IDs
// Throws error if any step is missing an ID
func validatePlanStepIDs(steps []PlanStepInterface) error {
	for i, step := range steps {
		if step.GetID() == "" {
			return fmt.Errorf("step at index %d is missing required ID field. Step title: %q", i, step.GetTitle())
		}

		// Validate decision step fields
		if err := validateDecisionStepTyped(step, i); err != nil {
			return err
		}

		// Validate routing step fields
		if err := validateRoutingStepTyped(step, i); err != nil {
			return err
		}

		// Recursively validate branch steps (for conditional steps)
		if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
			if len(conditionalStep.IfTrueSteps) > 0 {
				if err := validateBranchStepIDs(conditionalStep.IfTrueSteps, step.GetTitle(), "true"); err != nil {
					return err
				}
			}
			if len(conditionalStep.IfFalseSteps) > 0 {
				if err := validateBranchStepIDs(conditionalStep.IfFalseSteps, step.GetTitle(), "false"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateBranchStepIDs recursively validates that all branch steps have IDs
func validateBranchStepIDs(steps []PlanStepInterface, parentTitle, branchType string) error {
	for i, step := range steps {
		if step.GetID() == "" {
			return fmt.Errorf("branch step at index %d in %s branch of parent %q is missing required ID field. Step title: %q", i, branchType, parentTitle, step.GetTitle())
		}

		// Recursively validate nested branch steps
		if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
			if len(conditionalStep.IfTrueSteps) > 0 {
				if err := validateBranchStepIDs(conditionalStep.IfTrueSteps, step.GetTitle(), "true"); err != nil {
					return err
				}
			}
			if len(conditionalStep.IfFalseSteps) > 0 {
				if err := validateBranchStepIDs(conditionalStep.IfFalseSteps, step.GetTitle(), "false"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// runPlanningPhase generates JSON plan directly
// conversationHistory is updated in-place to accumulate across iterations
// Returns the generated PlanningResponse and updated conversation history
func (hcpo *StepBasedWorkflowOrchestrator) runPlanningPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llmtypes.MessageContent, existingPlan *PlanningResponse) (*PlanningResponse, []llmtypes.MessageContent, error) {
	planningTemplateVars := map[string]string{
		"Objective":           hcpo.GetObjective(),
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"IsCodeExecutionMode": fmt.Sprintf("%v", requiresCodeExecutionForProvider(hcpo.presetPhaseLLM)),
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

	// Add capabilities context so the planner knows which MCP servers, browser tools,
	// skills, and secrets are available — critical for generating accurate step descriptions.
	if capabilities := BuildPlanningCapabilitiesContext(hcpo); capabilities != "" {
		planningTemplateVars["AvailableCapabilities"] = capabilities
		hcpo.GetLogger().Info("✅ Passing capabilities context to planning agent (servers, browser, skills, secrets)")
	}

	// Determine user message based on mode
	// - For CREATE mode: concise, action-oriented instruction
	// - For UPDATE mode: use human feedback if provided, otherwise a short update/fix instruction
	var userMessage string
	if existingPlan != nil {
		// UPDATE mode: Use human feedback as user message (user's natural language feedback)
		if humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
			userMessage = humanFeedback
		} else {
			// Check if plan has validation errors
			validationErr := validatePlanStepIDs(existingPlan.Steps)
			if validationErr != nil {
				// Fallback: concise instruction for plan updates with validation error fix
				var result strings.Builder
				if err := planningUpdateValidationErrorTemplate.Execute(&result, map[string]interface{}{
					"ValidationErr": validationErr,
				}); err != nil {
					userMessage = "Review the existing plan and update it based on the objective and my feedback. Always use the human_feedback tool first to confirm any changes with me."
				} else {
					userMessage = result.String()
				}
			} else {
				// Fallback: concise instruction for plan updates
				userMessage = "Review the existing plan and update it based on the objective and my feedback. Always use the human_feedback tool first to confirm any changes with me."
			}
		}
	} else {
		// CREATE mode
		if humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
			// If we have feedback (e.g. from previous iteration where user provided objective), use it
			userMessage = humanFeedback
		} else if hcpo.GetObjective() == "" {
			// If objective is empty and no feedback, tell agent to ask user
			userMessage = "I haven't defined an objective yet. Please ask me what I want to achieve."
		} else {
			// Standard CREATE mode with objective
			// concise, action-oriented instruction for first-time plan generation.
			// Include the objective explicitly since it's no longer shown in the system prompt.
			var result strings.Builder
			if err := planningCreateUserMessageTemplate.Execute(&result, map[string]interface{}{
				"Objective": hcpo.GetObjective(),
			}); err != nil {
				userMessage = fmt.Sprintf("Objective: %s\n\nGenerate a comprehensive structured plan to achieve this objective.", hcpo.GetObjective())
			} else {
				userMessage = result.String()
			}
		}
	}

	// Create fresh planning agent with proper context
	planningAgent, err := hcpo.createPlanningAgent(ctx, "planning", 0, iteration)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create planning agent: %w", err)
	}

	// Execute planning agent using plan modification tools (unified for both CREATE and UPDATE modes)
	planningAgentTyped, ok := planningAgent.(*WorkflowPlanningAgent)
	if !ok {
		return nil, nil, fmt.Errorf(fmt.Sprintf("failed to cast planning agent to correct type"), nil)
	}

	// Determine if we're in UPDATE mode
	isUpdateMode := existingPlan != nil

	// Reset changelog session at the start of a new planning agent execution
	resetChangelogSession()

	workspacePath := hcpo.GetWorkspacePath()

	// Store initial plan state BEFORE agent execution for changelog comparison
	var initialPlan *PlanningResponse
	if isUpdateMode {
		// Deep copy the existing plan to avoid mutations
		planJSON, err := json.Marshal(existingPlan)
		if err == nil {
			var copiedPlan PlanningResponse
			if err := json.Unmarshal(planJSON, &copiedPlan); err == nil {
				initialPlan = &copiedPlan
			}
		}
	} else {
		// CREATE mode - initial plan is empty
		initialPlan = &PlanningResponse{Steps: []PlanStep{}}
	}

	// If CREATE mode and no plan exists, create empty plan.json
	if !isUpdateMode {
		emptyPlan := &PlanningResponse{Steps: []PlanStep{}}
		if err := writePlanToFile(ctx, workspacePath, emptyPlan, nil, hcpo.WriteWorkspaceFile, hcpo.GetLogger()); err != nil {
			return nil, nil, fmt.Errorf("failed to create empty plan.json: %w", err)
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

	// Register minimal workspace tools (shell_command + human) before plan modification tools
	// Phase agents only need shell_command for file operations and human_feedback for user interaction
	if hcpo.BaseOrchestrator != nil {
		phaseTools, phaseExecutors := hcpo.BaseOrchestrator.PreparePhaseAgentTools()

		if phaseTools != nil && phaseExecutors != nil {
			toolsToRegister, wrappedExecutors := hcpo.BaseOrchestrator.PrepareWorkspaceToolsWithFolderGuard(phaseTools, phaseExecutors)

			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Registering %d workspace tools (shell_command + human) for planning agent", len(toolsToRegister)))

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
						// Wrap human tools to inject SessionEventEmitter so blocking feedback events
						// reach the frontend (same fix as in registerCustomToolsForAgent)
						finalExec := toolExecutor
						isHumanTool := false
						if hcpo.ToolCategories != nil {
							if cat, catExists := hcpo.ToolCategories[tool.Function.Name]; catExists && cat == virtualtools.GetHumanToolCategory() {
								isHumanTool = true
							}
						}
						if isHumanTool && hcpo.GetContextAwareBridge() != nil {
							emitter := &orchestrator.BridgeSessionEventEmitter{Bridge: hcpo.GetContextAwareBridge()}
							origExec := toolExecutor
							finalExec = func(ctx context.Context, args map[string]interface{}) (string, error) {
								ctx = context.WithValue(ctx, virtualtools.SessionEventEmitterKey, emitter)
								return origExec(ctx, args)
							}
						}
						if err := mcpAgent.RegisterCustomTool(
							tool.Function.Name,
							tool.Function.Description,
							params,
							finalExec,
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
	// Pass unlock function to automatically unlock learnings when plan is modified
	unlockLearningsFunc := hcpo.createUnlockLearningsFunction()
	if err := registerPlanModificationTools(mcpAgent, workspacePath, hcpo.GetLogger(), hcpo.ReadWorkspaceFile, hcpo.WriteWorkspaceFile, hcpo.MoveWorkspaceFile, "planning agent", unlockLearningsFunc); err != nil {
		return nil, nil, err
	}

	// Register variable extraction tools (extract_variables, update_variable)
	if err := registerVariableExtractionTools(mcpAgent, workspacePath, hcpo.GetLogger(), hcpo.ReadWorkspaceFile, hcpo.WriteWorkspaceFile, "planning agent"); err != nil {
		return nil, nil, err
	}

	// Update code execution registry to include all late-registered tools (plan modification + variable extraction)
	// Without this, CLI providers (claude-code, gemini-cli) won't see these tools via the HTTP bridge
	if requiresCodeExecutionForProvider(hcpo.presetPhaseLLM) {
		if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update code execution registry with plan modification tools: %v", err))
		} else {
			hcpo.GetLogger().Info("✅ Code execution registry updated with plan modification and variable extraction tools for CLI provider")
		}
	}

	// Always use UPDATE mode prompt (handles both CREATE and UPDATE scenarios)
	systemPrompt := planningSystemPromptProcessorForUpdate(planningTemplateVars)
	if isUpdateMode {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 UPDATE mode: Using update system prompt"))
	} else {
	}

	// Create input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute using ExecuteWithTemplateValidation (standard pattern used by other agents)
	// This includes automatic event emission (agent start/end events)
	_, updatedConversationHistory, err := planningAgentTyped.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, planningTemplateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
	if err != nil {
		return nil, updatedConversationHistory, fmt.Errorf("agent execution failed: %w", err)
	}

	// Check if any plan modification tools were called
	toolCalls := extractToolCallsFromMessages(updatedConversationHistory)
	planUpdateToolCalled := false
	for _, toolName := range toolCalls {
		if toolName == "update_regular_step" || toolName == "update_conditional_step" || toolName == "update_decision_step" || toolName == "update_routing_step" || toolName == "delete_plan_steps" || toolName == "add_regular_step" || toolName == "add_conditional_step" || toolName == "add_decision_step" || toolName == "add_routing_step" || toolName == "add_loop_step" ||
			toolName == "convert_step_to_conditional" || toolName == "add_branch_steps" || toolName == "update_branch_steps" ||
			toolName == "delete_branch_steps" || toolName == "convert_conditional_to_regular" {
			planUpdateToolCalled = true
		}
	}

	// Read the current plan.json (whether tools were called or not)
	planResponse, err := readPlanFromFile(ctx, workspacePath, hcpo.ReadWorkspaceFile)
	if err != nil {
		return nil, updatedConversationHistory, fmt.Errorf("failed to read plan: %w", err)
	}

	// Generate changelog from plan diff (AFTER agent execution, BEFORE human feedback)
	// This captures ALL changes made during the agent execution in one comprehensive changelog entry
	if err := generateChangelogFromPlanDiff(ctx, workspacePath, initialPlan, planResponse, hcpo.ReadWorkspaceFile, hcpo.WriteWorkspaceFile, hcpo.GetLogger()); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to generate changelog from plan diff: %v", err))
		// Don't fail the entire operation if changelog generation fails
	}

	if !planUpdateToolCalled {
		// No tools called - conversational response
		if isUpdateMode {
		} else {
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
		return nil, nil, fmt.Errorf("plan validation failed: %w", err)
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
func (hcpo *StepBasedWorkflowOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from learnings, planning, and runs (for execution logs), writes to learnings only (for folder syncing)
	baseWorkspacePath := hcpo.GetWorkspacePath()
	planningPath := fmt.Sprintf("%s/planning", baseWorkspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)
	runsPath := fmt.Sprintf("%s/runs", baseWorkspacePath)

	// Read paths: planning (read plan.json), learnings (read existing folders), runs (execution logs)
	// planning/ is read-only here — all writes to plan.json go through dedicated plan modification tools
	// (update_regular_step, add_regular_step, delete_plan_steps, etc.) which call WriteWorkspaceFile
	// directly and bypass folder guard. This prevents shell/write_workspace_file from writing
	// malformed JSON or bypassing schema validation and learnings-unlock logic.
	readPaths := []string{planningPath, learningsPath, runsPath}
	// Write paths: learnings only (for renaming folders when step numbering changes after plan edits)
	writePaths := []string{learningsPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for planning agent - Read paths: %v, Write paths: %v (planning/ read-only via guard; plan writes go through dedicated tools only)", readPaths, writePaths))

	// Determine LLM config: Priority: presetPhaseLLM only
	var llmConfigToUse *orchestrator.LLMConfig
	if hcpo.presetPhaseLLM != nil && hcpo.presetPhaseLLM.Provider != "" && hcpo.presetPhaseLLM.ModelID != "" {
		// Use phase LLM for planning agent
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetPhaseLLM.Provider,
				ModelID:  hcpo.presetPhaseLLM.ModelID,
			},
			APIKeys: hcpo.GetAPIKeys(), // Safe: returns nil if orchestratorLLMConfig is nil
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM for planning: %s/%s", hcpo.presetPhaseLLM.Provider, hcpo.presetPhaseLLM.ModelID))
	} else {
		return nil, fmt.Errorf("no valid LLM configuration found for planning agent: presetPhaseLLM is empty or invalid")
	}

	// Create agent config with custom LLM
	agentConfig := hcpo.CreateStandardAgentConfigWithLLM("human-controlled-planning-agent", hcpo.GetMaxTurns(), agents.OutputFormatStructured, llmConfigToUse)
	agentConfig.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM planning agent

	// Code execution mode and tool search mode only apply to execution agents, not planning agents
	// Phase agents always use simple mode UNLESS the provider requires code execution (claude-code, gemini-cli)
	agentConfig.UseCodeExecutionMode = requiresCodeExecutionForProvider(hcpo.presetPhaseLLM)
	agentConfig.UseToolSearchMode = false
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Planning agent code execution mode: %v (provider requires it: %v)", agentConfig.UseCodeExecutionMode, requiresCodeExecutionForProvider(hcpo.presetPhaseLLM)))

	// Disable large output virtual tools for planning agent
	disabled := false
	agentConfig.EnableContextOffloading = &disabled
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling context offloading for planning agent"))

	// Planning agent uses plan modification tools (registered in runPlanningPhase, not here)
	// Pass empty tools/executors - tools will be registered separately
	toolsToRegister := []llmtypes.Tool{}
	executorsToUse := make(map[string]interface{})

	// Use base factory! (This handles all setup automatically)
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		agentConfig,
		phase,
		step,
		iteration,
		phase, // stepID (use phase name for phase-only agents)
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowPlanningAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister, // Empty - tools registered separately in runPlanningPhase
		executorsToUse,  // Empty - tools registered separately in runPlanningPhase
		false,           // Don't overwrite system prompt - planning agent manages its own prompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup planning agent: %w", err)
	}

	return agent, nil
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the generic ReadWorkspaceFile function from base orchestrator
func (hcpo *StepBasedWorkflowOrchestrator) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {

	// Use the generic ReadWorkspaceFile function from base orchestrator
	planContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	// Parse JSON content to PlanningResponse
	var planResponse PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &planResponse); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing plan.json: %v", err))
		return false, nil, fmt.Errorf("failed to parse plan.json: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(planResponse.Steps)))
	return true, &planResponse, nil
}

// requestPlanApproval requests human approval for the generated plan
// Returns: (approved bool, feedback string, error)
func (hcpo *StepBasedWorkflowOrchestrator) requestPlanApproval(
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

// populateRuntimeFields populates runtime fields (AgentConfigs, etc.) on plan steps in-place
// This maintains type safety by working directly with plan step types
func populateRuntimeFields(typedStep PlanStepInterface, stepConfigs []StepConfig) error {
	// Match config by step ID
	var agentConfigs *AgentConfigs
	stepID := typedStep.GetID()
	if stepID == "" {
		return fmt.Errorf("step is missing required ID field. Step title: %q", typedStep.GetTitle())
	} else if stepConfigs != nil {
		agentConfigs = MatchStepConfigByID(stepID, stepConfigs)
	}

	// Use type switch to handle different step types
	switch step := typedStep.(type) {
	case *RegularPlanStep:
		// Regular step (may have loops)
		// Merge prerequisite detection settings from PlanStep into AgentConfigs
		if step.EnablePrerequisiteDetection != nil || len(step.PrerequisiteRules) > 0 {
			if agentConfigs == nil {
				agentConfigs = &AgentConfigs{}
			}
			if agentConfigs.EnablePrerequisiteDetection == nil && step.EnablePrerequisiteDetection != nil {
				agentConfigs.EnablePrerequisiteDetection = step.EnablePrerequisiteDetection
			}
			if len(agentConfigs.PrerequisiteRules) == 0 && len(step.PrerequisiteRules) > 0 {
				agentConfigs.PrerequisiteRules = step.PrerequisiteRules
			}
		}

		// LLM validation is required for loop steps (to evaluate loop condition)
		// Since LLM validation is disabled by default (nil = disabled), always force it on for loop steps
		if step.HasLoop {
			val := false // false = validation enabled
			if agentConfigs == nil {
				agentConfigs = &AgentConfigs{
					DisableValidation: &val,
				}
			} else if agentConfigs.DisableValidation == nil || *agentConfigs.DisableValidation {
				enabledConfigs := *agentConfigs
				enabledConfigs.DisableValidation = &val
				agentConfigs = &enabledConfigs
			}
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		return nil

	case *ConditionalPlanStep:
		// Conditional step: populate branch steps recursively
		if len(step.IfTrueSteps) > 0 {
			for _, branchStep := range step.IfTrueSteps {
				if err := populateRuntimeFields(branchStep, stepConfigs); err != nil {
					return fmt.Errorf("failed to populate if_true branch step: %w", err)
				}
			}
		}

		if len(step.IfFalseSteps) > 0 {
			for _, branchStep := range step.IfFalseSteps {
				if err := populateRuntimeFields(branchStep, stepConfigs); err != nil {
					return fmt.Errorf("failed to populate if_false branch step: %w", err)
				}
			}
		}

		// Conditional steps should never have validation - they only evaluate conditions
		if agentConfigs == nil {
			val := true
			agentConfigs = &AgentConfigs{
				DisableValidation: &val,
			}
		} else if agentConfigs.DisableValidation == nil || !*agentConfigs.DisableValidation {
			val := true
			disabledConfigs := *agentConfigs
			disabledConfigs.DisableValidation = &val
			agentConfigs = &disabledConfigs
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		return nil

	case *DecisionPlanStep:
		// Decision step is now flattened - no nested step to populate
		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		return nil

	case *OrchestrationPlanStep:
		// Orchestration step: populate inner OrchestrationStep recursively
		if step.OrchestrationStep != nil {
			if err := populateRuntimeFields(step.OrchestrationStep, stepConfigs); err != nil {
				return fmt.Errorf("failed to populate orchestration inner step: %w", err)
			}
		}

		// Populate sub-agent steps in routes recursively
		for i := range step.OrchestrationRoutes {
			route := &step.OrchestrationRoutes[i]
			if route.SubAgentStep != nil {
				if err := populateRuntimeFields(route.SubAgentStep, stepConfigs); err != nil {
					return fmt.Errorf("failed to populate sub-agent step for route '%s': %w", route.RouteID, err)
				}
				// Sub-agents should have validation disabled
				// Get the populated config from the sub-agent step
				switch subStep := route.SubAgentStep.(type) {
				case *RegularPlanStep:
					if subStep.AgentConfigs == nil {
						val := true
						subStep.AgentConfigs = &AgentConfigs{
							DisableValidation: &val,
						}
					} else if subStep.AgentConfigs.DisableValidation == nil || !*subStep.AgentConfigs.DisableValidation {
						val := true
						disabledConfigs := *subStep.AgentConfigs
						disabledConfigs.DisableValidation = &val
						subStep.AgentConfigs = &disabledConfigs
					}
				}
			}
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		return nil

	case *HumanInputPlanStep:
		// Human input step: no execution, validation, or learning - just asks question and blocks
		// Merge prerequisite detection settings from PlanStep into AgentConfigs
		if step.EnablePrerequisiteDetection != nil || len(step.PrerequisiteRules) > 0 {
			if agentConfigs == nil {
				agentConfigs = &AgentConfigs{}
			}
			if agentConfigs.EnablePrerequisiteDetection == nil && step.EnablePrerequisiteDetection != nil {
				agentConfigs.EnablePrerequisiteDetection = step.EnablePrerequisiteDetection
			}
			if len(agentConfigs.PrerequisiteRules) == 0 && len(step.PrerequisiteRules) > 0 {
				agentConfigs.PrerequisiteRules = step.PrerequisiteRules
			}
		}

		// Human input steps should never have validation, learning, or execution agents
		if agentConfigs == nil {
			val := true
			agentConfigs = &AgentConfigs{
				DisableValidation: &val,
				DisableLearning:   &val,
			}
		} else {
			// Ensure validation and learning are disabled
			val := true
			if agentConfigs.DisableValidation == nil || !*agentConfigs.DisableValidation {
				disabledConfigs := *agentConfigs
				disabledConfigs.DisableValidation = &val
				agentConfigs = &disabledConfigs
			}
			if agentConfigs.DisableLearning == nil || !*agentConfigs.DisableLearning {
				disabledConfigs := *agentConfigs
				disabledConfigs.DisableLearning = &val
				agentConfigs = &disabledConfigs
			}
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		return nil

	case *EvaluationStep:
		// Evaluation step
		step.AgentConfigs = agentConfigs
		return nil

	case *RoutingPlanStep:
		// Routing step: similar to decision step - evaluates a question and routes to one of N next steps
		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		return nil

	case *TodoTaskPlanStep:
		// Todo task step: populate inner TodoTaskStep recursively
		if step.TodoTaskStep != nil {
			if err := populateRuntimeFields(step.TodoTaskStep, stepConfigs); err != nil {
				return fmt.Errorf("failed to populate todo task inner step: %w", err)
			}
		}

		// Populate sub-agent steps in predefined routes recursively
		for i := range step.PredefinedRoutes {
			route := &step.PredefinedRoutes[i]
			if route.SubAgentStep != nil {
				if err := populateRuntimeFields(route.SubAgentStep, stepConfigs); err != nil {
					return fmt.Errorf("failed to populate sub-agent step for route '%s': %w", route.RouteID, err)
				}
				// Sub-agents should have validation disabled
				switch subStep := route.SubAgentStep.(type) {
				case *RegularPlanStep:
					if subStep.AgentConfigs == nil {
						val := true
						subStep.AgentConfigs = &AgentConfigs{
							DisableValidation: &val,
						}
					} else if subStep.AgentConfigs.DisableValidation == nil || !*subStep.AgentConfigs.DisableValidation {
						val := true
						disabledConfigs := *subStep.AgentConfigs
						disabledConfigs.DisableValidation = &val
						subStep.AgentConfigs = &disabledConfigs
					}
				}
			}
		}

		// Populate runtime field directly on plan step
		step.AgentConfigs = agentConfigs
		return nil

	default:
		return fmt.Errorf("unknown step type: %T", typedStep)
	}
}

// populateStepRuntimeFields populates runtime fields on a PlanStepInterface and returns it
// This function populates AgentConfigs and other runtime fields from step_config.json
// For execution, use plan steps directly with populated runtime fields
func populateStepRuntimeFields(typedStep PlanStepInterface, stepConfigs []StepConfig) (PlanStepInterface, error) {
	// Populate runtime fields on the plan step
	if err := populateRuntimeFields(typedStep, stepConfigs); err != nil {
		return nil, err
	}

	// Recursively populate runtime fields for nested steps
	switch step := typedStep.(type) {
	case *ConditionalPlanStep:
		// Populate branch steps recursively
		if len(step.IfTrueSteps) > 0 {
			for _, branchStep := range step.IfTrueSteps {
				if err := populateRuntimeFields(branchStep, stepConfigs); err != nil {
					return nil, fmt.Errorf("failed to populate if_true branch step: %w", err)
				}
			}
		}
		if len(step.IfFalseSteps) > 0 {
			for _, branchStep := range step.IfFalseSteps {
				if err := populateRuntimeFields(branchStep, stepConfigs); err != nil {
					return nil, fmt.Errorf("failed to populate if_false branch step: %w", err)
				}
			}
		}

	case *DecisionPlanStep:
		// Decision step is now flattened - no nested step to populate

	case *OrchestrationPlanStep:
		// Populate inner OrchestrationStep
		if step.OrchestrationStep != nil {
			if err := populateRuntimeFields(step.OrchestrationStep, stepConfigs); err != nil {
				return nil, fmt.Errorf("failed to populate orchestration inner step: %w", err)
			}
		}
		// Populate sub-agent steps in routes
		for _, route := range step.OrchestrationRoutes {
			if route.SubAgentStep != nil {
				if err := populateRuntimeFields(route.SubAgentStep, stepConfigs); err != nil {
					return nil, fmt.Errorf("failed to populate sub-agent step: %w", err)
				}
			}
		}

	case *EvaluationStep:
		// No nested steps for evaluation steps currently

	case *RoutingPlanStep:
		// Routing step is flattened - no nested steps to populate
	}

	// Return the step with populated runtime fields
	return typedStep, nil
}

func (hcpo *StepBasedWorkflowOrchestrator) populateStepsRuntimeFields(ctx context.Context, planSteps []PlanStepInterface) ([]PlanStepInterface, error) {
	// Read step configs from step_config.json
	stepConfigs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults for all steps)", err))
		stepConfigs = []StepConfig{}
	}

	// Read global step overrides from step_override.json (applied after per-step configs)
	stepOverrides, err := hcpo.ReadStepOverrides(ctx)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_override.json: %v (skipping global overrides)", err))
		stepOverrides = nil
	}
	if stepOverrides != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Loaded global step overrides from step_override.json"))
	}

	// Log available config IDs for debugging
	if len(stepConfigs) > 0 {
		configIDs := make([]string, 0, len(stepConfigs))
		for _, config := range stepConfigs {
			if config.ID != "" {
				configIDs = append(configIDs, config.ID)
			}
		}
	} else {
	}

	// Match configs by step index (0-based)
	matchedConfigs, err := MatchStepConfigs(planSteps, stepConfigs)
	if err != nil {
		return nil, fmt.Errorf("failed to match step configs: %w", err)
	}

	todoSteps := make([]PlanStepInterface, len(planSteps))
	for i, step := range planSteps {
		// Step is already PlanStepInterface, no conversion needed

		// Get matched config for this step (may be nil if no match)
		var agentConfigs *AgentConfigs
		if config, found := matchedConfigs[i]; found {
			agentConfigs = config
			// Log code execution mode for debugging
			if agentConfigs.UseCodeExecutionMode != nil {
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("📋 Step '%s' (ID: %s) matched config - use_code_execution_mode: nil (will use preset default)", step.GetTitle(), step.GetID()))
			}
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Step '%s' (ID: %s) has NO config match in step_config.json - will use preset defaults", step.GetTitle(), step.GetID()))
		}

		// Populate runtime fields (this properly handles inner steps for decision/orchestration)
		todoStep, err := populateStepRuntimeFields(step, stepConfigs)
		if err != nil {
			return nil, fmt.Errorf("failed to populate runtime fields for step %d (title: %q, ID: %s): %w", i, step.GetTitle(), step.GetID(), err)
		}

		// Merge matched configs with existing configs (if any)
		// This preserves any configs set during conversion and merges in step_config.json configs
		if agentConfigs != nil {
			existingConfigs := getAgentConfigs(todoStep)
			if existingConfigs == nil {
				// Set AgentConfigs on the step if it doesn't exist
				switch s := todoStep.(type) {
				case *RegularPlanStep:
					s.AgentConfigs = agentConfigs
				case *ConditionalPlanStep:
					s.AgentConfigs = agentConfigs
				case *DecisionPlanStep:
					s.AgentConfigs = agentConfigs
				case *OrchestrationPlanStep:
					s.AgentConfigs = agentConfigs
				case *EvaluationStep:
					s.AgentConfigs = agentConfigs
				case *RoutingPlanStep:
					s.AgentConfigs = agentConfigs
				}
			} else {
				// Merge configs from step_config.json into existing configs
				MergeAgentConfigFields(existingConfigs, agentConfigs, step.GetID(), hcpo.GetLogger())
			}

			// Note: Inner orchestration steps should NOT inherit config from wrapper steps
			// Each step (wrapper and inner) loads its own config by its own ID only
			// The inner step will get its config when ApplyStepConfigFromFile is called for it during execution
		}

		// Apply global overrides from step_override.json (highest priority - wins over per-step config)
		if stepOverrides != nil {
			existingConfigs := getAgentConfigs(todoStep)
			if existingConfigs == nil {
				// Set AgentConfigs on the step with override values
				switch s := todoStep.(type) {
				case *RegularPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *ConditionalPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *DecisionPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *OrchestrationPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *EvaluationStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				case *RoutingPlanStep:
					overrideCopy := *stepOverrides
					s.AgentConfigs = &overrideCopy
				}
			} else {
				MergeAgentConfigFields(existingConfigs, stepOverrides, step.GetID(), hcpo.GetLogger())
			}
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Applied global overrides for step '%s' (ID: %s)", step.GetTitle(), step.GetID()))
		}

		// Log config matching results for nested steps
		switch s := todoStep.(type) {
		case *DecisionPlanStep:
			// Decision step is now flattened - configs are directly on the step
			innerConfigs := getAgentConfigs(s)
			if innerConfigs != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step '%s' (ID: %s) matched config from step_config.json", s.GetTitle(), s.GetID()))
			}
		case *OrchestrationPlanStep:
			if s.OrchestrationStep != nil {
				innerConfigs := getAgentConfigs(s.OrchestrationStep)
				if innerConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step '%s' (ID: %s) matched config from step_config.json", s.OrchestrationStep.GetTitle(), s.OrchestrationStep.GetID()))
				}
			}
		case *ConditionalPlanStep:
			for _, branchStep := range s.IfTrueSteps {
				branchConfigs := getAgentConfigs(branchStep)
				if branchConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Branch step '%s' (ID: %s) matched config from step_config.json", branchStep.GetTitle(), branchStep.GetID()))
				}
			}
			for _, branchStep := range s.IfFalseSteps {
				branchConfigs := getAgentConfigs(branchStep)
				if branchConfigs != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Branch step '%s' (ID: %s) matched config from step_config.json", branchStep.GetTitle(), branchStep.GetID()))
				}
			}
		}

		todoSteps[i] = todoStep
	}
	return todoSteps, nil
}

// EmitTodoStepsExtractedEvent emits an event when todo steps are extracted from a plan
// Public method that accepts BaseOrchestrator and other parameters
func EmitTodoStepsExtractedEvent(ctx context.Context, bo *orchestrator.BaseOrchestrator, extractedSteps []PlanStepInterface, planSource, extractionMethod, runFolder, workspacePath string) {
	EmitTodoStepsExtractedEventWithMetadata(ctx, bo, extractedSteps, planSource, extractionMethod, runFolder, workspacePath, nil)
}

// EmitTodoStepsExtractedEventWithMetadata emits an event when todo steps are extracted from a plan with optional metadata
// Metadata can include changed_step_ids and deleted_step_ids for granular event handling
func EmitTodoStepsExtractedEventWithMetadata(ctx context.Context, bo *orchestrator.BaseOrchestrator, extractedSteps []PlanStepInterface, planSource, extractionMethod, runFolder, workspacePath string, metadata map[string]interface{}) {
	if bo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data with metadata
	baseEventData := baseevents.BaseEventData{
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
	unifiedEvent := &baseevents.AgentEvent{
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
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ [EmitTodoStepsExtractedEventWithMetadata] Failed to emit todo steps extracted event: %v", err))
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

// IsPlanModificationTool checks if a tool name is a plan modification tool
func IsPlanModificationTool(name string) bool {
	return name == "update_regular_step" || name == "update_conditional_step" || name == "update_decision_step" || name == "update_routing_step" || name == "update_orchestration_step" || name == "update_human_input_step" || name == "update_todo_task_step" || name == "delete_plan_steps" || name == "add_regular_step" || name == "add_conditional_step" || name == "add_decision_step" || name == "add_routing_step" || name == "add_orchestration_step" || name == "add_loop_step" || name == "add_human_input_step" || name == "add_todo_task_step" ||
		name == "convert_step_to_conditional" || name == "add_branch_steps" || name == "update_branch_steps" ||
		name == "delete_branch_steps" || name == "convert_conditional_to_regular" || name == "update_validation_schema" || name == "update_success_criteria" ||
		name == "add_orchestration_route" || name == "update_orchestration_route" || name == "delete_orchestration_route" ||
		name == "add_todo_task_route" || name == "update_todo_task_route" || name == "delete_todo_task_route"
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
				case "update_regular_step", "update_conditional_step", "update_decision_step", "update_routing_step", "update_orchestration_step":
					// Extract existing_step_id from updated step
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
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

				case "add_regular_step", "add_conditional_step", "add_decision_step", "add_routing_step", "add_orchestration_step", "add_loop_step":
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

				case "convert_step_to_conditional":
					// Extract existing_step_id
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}

				case "convert_conditional_to_regular":
					// Extract existing_step_id
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}

				case "update_validation_schema", "update_success_criteria":
					// Extract existing_step_id from updated step
					if stepID, ok := argsMap["existing_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}

				case "add_orchestration_route":
					// Extract parent_step_id (the orchestration step that contains the route)
					if stepID, ok := argsMap["parent_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}
					// Extract sub_agent_step.id from new_route (sub-agents have their own step IDs)
					// When a new route is added, the sub-agent step is effectively "added" to the plan
					if newRouteRaw, ok := argsMap["new_route"].(map[string]interface{}); ok {
						if subAgentStepRaw, ok := newRouteRaw["sub_agent_step"].(map[string]interface{}); ok {
							if subAgentStepID, ok := subAgentStepRaw["id"].(string); ok && subAgentStepID != "" {
								changed.Added = append(changed.Added, subAgentStepID)
							}
						}
					}

				case "update_orchestration_route":
					// Extract parent_step_id (the orchestration step that contains the route)
					if stepID, ok := argsMap["parent_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}
					// Extract sub_agent_step.id from sub_agent_step parameter (if provided)
					// When a route's sub-agent is updated, the sub-agent step is effectively "updated"
					if subAgentStepRaw, ok := argsMap["sub_agent_step"].(map[string]interface{}); ok {
						if subAgentStepID, ok := subAgentStepRaw["id"].(string); ok && subAgentStepID != "" {
							changed.Updated = append(changed.Updated, subAgentStepID)
						}
					}

				case "delete_orchestration_route":
					// Extract parent_step_id (the orchestration step that contains the route)
					if stepID, ok := argsMap["parent_step_id"].(string); ok && stepID != "" {
						changed.Updated = append(changed.Updated, stepID)
					}
					// Note: We can't extract the deleted sub-agent step ID from the tool call arguments alone
					// because delete_orchestration_route only provides parent_step_id and deleted_route_id
					// The sub-agent step ID would need to be read from the plan.json file, which is complex
					// For now, we track the parent step as updated, which will refresh the frontend
					// The frontend will see the deleted route (and its sub-agent node) when it refreshes the plan
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

	// Extract tool calls from conversation history
	toolCalls := ExtractToolCallsFromMessages(conversationHistory)
	bo.GetLogger().Info(fmt.Sprintf("🔍 [CheckAndEmitPlanUpdateEvent] Extracted %d tool calls: %v", len(toolCalls), toolCalls))

	// Check if any plan or step_config modification tool was called
	needsEvent := false
	for _, name := range toolCalls {
		if IsPlanModificationTool(name) || IsStepConfigModificationTool(name) {
			needsEvent = true
			break
		}
	}

	if !needsEvent {
		return
	}

	// Extract changed step IDs from tool call arguments (granular event data)
	changedStepIDs := ExtractChangedStepIDsFromMessages(conversationHistory)
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

	// Use plan steps directly for the event (no conversion needed)
	// The frontend will merge step_config.json when it receives the event and refreshes
	// Convert orchestration routes to use PlanStepInterface directly
	planSteps := make([]PlanStepInterface, len(plan.Steps))
	for i, step := range plan.Steps {
		// For orchestration steps, we need to convert routes to use PlanStepInterface
		if orchestrationStep, ok := step.(*OrchestrationPlanStep); ok {
			// Create a copy with updated routes
			orchestrationRoutes := make([]PlanOrchestrationRoute, len(orchestrationStep.OrchestrationRoutes))
			copy(orchestrationRoutes, orchestrationStep.OrchestrationRoutes)
			// Routes already use PlanStepInterface, so no conversion needed
			planSteps[i] = step
		} else {
			planSteps[i] = step
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
	EmitTodoStepsExtractedEventWithMetadata(ctx, bo, planSteps, "updated_plan", "agent_tool_modification", "", workspacePath, metadata)
	bo.GetLogger().Info(fmt.Sprintf("✅ Emitted plan update event: %d steps (changed: %d added, %d updated, %d deleted)",
		len(planSteps), len(changedStepIDs.Added), len(changedStepIDs.Updated), len(changedStepIDs.Deleted)))
}

// CreatePlanOnly runs only the planning phase (standalone, independent from CreateTodoList)
// This is a separate workflow phase that can be run independently
// Similar to ExtractVariablesOnly in variable_management.go
func (hcpo *StepBasedWorkflowOrchestrator) CreatePlanOnly(ctx context.Context, objective, workspacePath string) (string, error) {

	// Set workspace path early (needed for checking plan)
	hcpo.SetWorkspacePath(workspacePath)

	// Check if plan.json exists - if so, skip asking what to build
	checkPlanPath := "planning/plan.json"
	initialPlanExists, _, _ := hcpo.checkExistingPlan(ctx, checkPlanPath)

	if !initialPlanExists {
		// Step 1: Always ask the user what they want to build
		requestID := fmt.Sprintf("plan_objective_inquiry_%d", time.Now().UnixNano())
		_, feedback, err := hcpo.RequestHumanFeedback(
			ctx,
			requestID,
			"What would you like to build?",
			"",
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to request objective from user: %v", err))
		} else if feedback != "" {
			objective = feedback
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Received objective from user: %s", objective))
		}

		// Step 2: Ask clarifying questions about requirements/preferences before planning
		if objective != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Asking clarifying questions before planning for: %s", objective))
			clarifyID := fmt.Sprintf("plan_clarify_%d", time.Now().UnixNano())
			_, clarifyFeedback, clarifyErr := hcpo.RequestHumanFeedback(
				ctx,
				clarifyID,
				fmt.Sprintf("Before I create a plan for: \"%s\"\n\nPlease share any additional details:\n- Expected outcome or deliverables?\n- Preferences on approach or tools?\n- Constraints, priorities, or scope boundaries?\n\n(Type your details, or just hit Approve to proceed as-is)", objective),
				"",
				hcpo.getSessionID(),
				hcpo.getWorkflowID(),
			)
			if clarifyErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to request clarifying questions: %v — proceeding with objective as-is", clarifyErr))
			} else if clarifyFeedback != "" {
				objective = objective + "\n\nAdditional context from user:\n" + clarifyFeedback
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Received clarifying feedback, enriched objective"))
			}
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Plan exists at %s - skipping objective inquiry", checkPlanPath))
	}

	// Set objective
	hcpo.SetObjective(objective)

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
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Loaded existing variables from variables.json"))
	} else {
		// No variables.json - create it with empty variables and the original objective

		// Create new VariablesManifest with original objective and empty variables
		newManifest := &VariablesManifest{
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
			}
		}
	}

	// Load runtime variable values if provided
	variableValues, err := LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load variable values: %v", err))
	} else {
		hcpo.variableValues = variableValues
	}

	// Check if plan.json already exists
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check for existing plan: %v", err))
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

		// Try to emit event immediately so UI can display the existing plan
		// If conversion fails (invalid plan), log warning but continue - agent will fix it
		// TODO: Commented out - not required for now
		// breakdownSteps, err := hcpo.populateStepsRuntimeFields(ctx, existingPlan.Steps)
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
		
		// Check if plan has validation errors by attempting validation
		if validationErr := validatePlanStepIDs(existingPlan.Steps); validationErr != nil {
			updatePrompt = fmt.Sprintf("The existing plan has validation errors that need to be fixed: %v\n\nThe planning agent will automatically fix these validation errors. You can also describe any additional changes or improvements you want.", validationErr)
		}

		updateFeedbackID := fmt.Sprintf("plan_update_feedback_%d", time.Now().UnixNano())
		approved, updateFeedback, err := hcpo.RequestHumanFeedback(
			ctx,
			updateFeedbackID,
			updatePrompt,
			"", // No additional context (matches plan_opt_improvement_agent behavior)
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
	if !planExists {
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
							return "", fmt.Errorf("max plan revision attempts (%d) reached", maxPlanRevisions)
						}
						continue
					}
				}
				return "", fmt.Errorf("planning phase failed: %w", err)
			}

			if len(approvedPlan.Steps) == 0 {
				// If no steps created, check if agent asked a question (conversational flow)
				// This handles the "empty objective" case where agent asks "What do you want to do?"
				lastMsg := planningConversationHistory[len(planningConversationHistory)-1]
				if lastMsg.Role == llmtypes.ChatMessageTypeAI {
					var agentText string
					for _, part := range lastMsg.Parts {
						if textPart, ok := part.(llmtypes.TextContent); ok {
							agentText += textPart.Text
						}
					}

					if agentText != "" {
						// Agent asked a question - request feedback from user
						hcpo.GetLogger().Info(fmt.Sprintf("💬 Agent asked question: %s", agentText))
						
						// Request human feedback with agent's question
						requestID := fmt.Sprintf("plan_clarification_%d_%d", time.Now().UnixNano(), revisionAttempt)
						approved, feedback, err := hcpo.RequestHumanFeedback(
							ctx,
							requestID,
							agentText,
							"", // No additional context
							hcpo.getSessionID(),
							hcpo.getWorkflowID(),
						)
						
						if err != nil {
							return "", fmt.Errorf("failed to request feedback for agent question: %w", err)
						}
						
						// Use user's feedback for next iteration
						humanFeedback = feedback
						if approved && feedback == "" {
							// User approved but didn't answer? Treat as "continue" or "yes"
							humanFeedback = "Yes, please proceed."
						}
						
						if revisionAttempt >= maxPlanRevisions {
							return "", fmt.Errorf("max plan revision attempts (%d) reached", maxPlanRevisions)
						}
						continue
					}
				}
				
				return "", fmt.Errorf(fmt.Sprintf("new plan has no steps: planning agent returned empty steps array"), nil)
			}

			// Populate runtime fields for approved plan steps
			breakdownSteps, err := hcpo.populateStepsRuntimeFields(ctx, approvedPlan.Steps)
			if err != nil {
				return "", fmt.Errorf("failed to convert approved plan steps: %w", err)
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
				return "", fmt.Errorf("plan approval request failed: %w", err)
			}

			if approvedInternal {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ JSON plan approved by human"))
				break
			}

			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Plan revision requested (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedbackInternal))
			humanFeedback = feedbackInternal

			if revisionAttempt >= maxPlanRevisions {
				return "", fmt.Errorf("max plan revision attempts (%d) reached", maxPlanRevisions)
			}
		}

		hcpo.approvedPlan = approvedPlan
	}

	// Ensure event is emitted at the end if we have an approved plan
	// This ensures the UI always sees the plan, even if event was emitted earlier
	// TODO: Commented out - not required for now
	// if approvedPlan != nil && !eventEmitted {
	// 	breakdownSteps, err := hcpo.populateStepsRuntimeFields(ctx, approvedPlan.Steps)
	// 	if err != nil {
	// 		return "", fmt.Errorf("failed to convert approved plan steps: %w", err)
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
			summary.WriteString(fmt.Sprintf("%d. %s\n", i+1, step.GetDescription()))
		}
		return summary.String(), nil
	}

	return "Planning completed (no plan created).", nil
}
