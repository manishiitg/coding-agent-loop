package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
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

// addIDsToPlanSteps recursively adds IDs to all steps in a plan
// IDs are generated from step titles for consistency with step_config.json
// ID is now mandatory, so this function always generates IDs
func addIDsToPlanSteps(steps []PlanStep) {
	for i := range steps {
		// Always generate ID for top-level step (ID is mandatory)
		if steps[i].Title != "" {
			steps[i].ID = GenerateStepID(steps[i].Title)
		} else {
			// If no title, generate a fallback ID (should not happen in normal flow)
			steps[i].ID = GenerateStepID(fmt.Sprintf("step-%d", i))
		}

		// Recursively add IDs to branch steps
		if len(steps[i].IfTrueSteps) > 0 {
			addIDsToBranchSteps(steps[i].IfTrueSteps, steps[i].Title, "true")
		}
		if len(steps[i].IfFalseSteps) > 0 {
			addIDsToBranchSteps(steps[i].IfFalseSteps, steps[i].Title, "false")
		}
	}
}

// addIDsToBranchSteps recursively adds IDs to branch steps
// parentTitle: title of the parent step
// branchType: "true" or "false"
// ID is now mandatory, so this function always generates IDs
func addIDsToBranchSteps(steps []PlanStep, parentTitle, branchType string) {
	for i := range steps {
		// Always generate ID for branch step (ID is mandatory)
		// Format: parent-title + branch-type + nested-index + branch-title
		if steps[i].Title != "" && parentTitle != "" {
			idInput := fmt.Sprintf("%s-%s-%d-%s", parentTitle, branchType, i, steps[i].Title)
			steps[i].ID = GenerateStepID(idInput)
		} else {
			// If no title or parent title, generate a fallback ID (should not happen in normal flow)
			steps[i].ID = GenerateStepID(fmt.Sprintf("branch-%s-%d", branchType, i))
		}

		// Recursively add IDs to nested branch steps
		if len(steps[i].IfTrueSteps) > 0 {
			addIDsToBranchSteps(steps[i].IfTrueSteps, steps[i].Title, "true")
		}
		if len(steps[i].IfFalseSteps) > 0 {
			addIDsToBranchSteps(steps[i].IfFalseSteps, steps[i].Title, "false")
		}
	}
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
		hcpo.GetLogger().Infof("📄 Using provided existing plan with %d steps (UPDATE mode)", len(existingPlan.Steps))
	} else {
		planToUse = nil
		hcpo.GetLogger().Infof("📝 No existing plan provided - creating new plan (CREATE mode)")
	}

	// Serialize plan to JSON and pass in template (prevents agent from reading workspace)
	if planToUse != nil {
		existingPlanJSON, err := json.MarshalIndent(planToUse, "", "  ")
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to marshal existing plan to JSON: %v", err)
		} else {
			planningTemplateVars["ExistingPlanJSON"] = string(existingPlanJSON)
			hcpo.GetLogger().Infof("✅ Passing plan contents in template (prevents workspace file reads)")
		}
	}

	// Add variable names if available (planning agent should preserve variable placeholders)
	if variableNames := hcpo.formatVariableNames(); variableNames != "" {
		planningTemplateVars["VariableNames"] = variableNames
		hcpo.GetLogger().Infof("✅ Passing variable names to planning agent (for placeholder preservation)")
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
		return nil, nil, fmt.Errorf("failed to create planning agent: %w", err)
	}

	// Execute planning agent using structured output
	planningAgentTyped, ok := planningAgent.(*HumanControlledTodoPlannerPlanningAgent)
	if !ok {
		return nil, nil, fmt.Errorf("failed to cast planning agent to correct type")
	}

	// Determine if we're in UPDATE mode
	isUpdateMode := existingPlan != nil

	var planResponse *PlanningResponse
	var updatedConversationHistory []llmtypes.MessageContent

	if isUpdateMode {
		// UPDATE mode: Use ExecuteStructuredUpdate (returns updated PlanningResponse directly)
		hcpo.GetLogger().Infof("🔄 UPDATE mode: Using ExecuteStructuredUpdate")
		// Pass BaseOrchestrator's file operation methods to the planning agent
		updatedPlan, updatedHistory, updateErr := planningAgentTyped.ExecuteStructuredUpdate(ctx, planningTemplateVars, conversationHistory, userMessage, hcpo.ReadWorkspaceFile, hcpo.WriteWorkspaceFile)
		if updateErr != nil {
			err = updateErr
			updatedConversationHistory = updatedHistory
		} else {
			// Plan is already updated in plan.json by the tools - just use it
			planResponse = updatedPlan
			updatedConversationHistory = updatedHistory
			hcpo.GetLogger().Infof("✅ Plan updated via tools (%d total steps)", len(updatedPlan.Steps))
		}
	} else {
		// CREATE mode: Use ExecuteStructured
		hcpo.GetLogger().Infof("📝 CREATE mode: Using ExecuteStructured")
		planResponse, updatedConversationHistory, err = planningAgentTyped.ExecuteStructured(ctx, planningTemplateVars, conversationHistory, userMessage)
	}

	if err != nil {
		// Debug: Log the error type and message
		hcpo.GetLogger().Infof("🔍 [DEBUG] Planning agent returned error: %T, message: %s", err, err.Error())
		hcpo.GetLogger().Infof("🔍 [DEBUG] IsNonStructuredResponseError check: %v", agents.IsNonStructuredResponseError(err))

		// Check if this is a non-structured response error (text response instead of structured output)
		if agents.IsNonStructuredResponseError(err) {
			hcpo.GetLogger().Infof("✅ [DEBUG] Detected NonStructuredResponseError in runPlanningPhase")
			var nonStructuredErr *agents.NonStructuredResponseError
			if errors.As(err, &nonStructuredErr) {
				// Display the text response to the user and request feedback
				if isUpdateMode {
					hcpo.GetLogger().Infof("📝 Planning agent returned conversational text instead of structured update. This is acceptable when user is just asking questions (no plan changes needed).")
				} else {
					hcpo.GetLogger().Infof("📝 Planning agent returned conversational text instead of structured output. Displaying to user for feedback.")
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
					return nil, nil, fmt.Errorf("failed to request human feedback for planning text response: %w", feedbackErr)
				}

				// If user approved (clicked Approve button), treat as no plan update needed (acceptable for UPDATE mode)
				if approved {
					if isUpdateMode {
						hcpo.GetLogger().Infof("✅ User approved conversational response - no plan update needed. This is acceptable in UPDATE mode.")
						// Return error to indicate no plan update (the loop will handle this appropriately)
						return nil, nonStructuredErr.UpdatedHistory, fmt.Errorf("PLANNING_CONVERSATIONAL_APPROVED:no plan update needed")
					} else {
						hcpo.GetLogger().Infof("✅ User approved planning text response, but no structured plan was generated. This is unexpected - returning error.")
						return nil, nil, fmt.Errorf("planning agent returned text response but user approved without providing feedback to generate structured plan")
					}
				}

				// User provided feedback - return a special error that the loop can detect and handle
				// Use a specific error prefix that the loop will recognize
				// The updated history from the agent's response is included so conversation continues properly
				feedbackError := fmt.Errorf("PLANNING_TEXT_RESPONSE_FEEDBACK:%s", feedback)
				hcpo.GetLogger().Infof("🔄 [DEBUG] Returning feedback error from runPlanningPhase: %s", feedbackError.Error())
				return nil, nonStructuredErr.UpdatedHistory, feedbackError
			}
		}
		// For other errors, return as-is
		return nil, nil, fmt.Errorf("planning failed: %w", err)
	}

	// Only save plan for CREATE mode - UPDATE mode already saved it via tools
	if !isUpdateMode {
		// Add IDs to all steps before saving (for consistency with step_config.json)
		addIDsToPlanSteps(planResponse.Steps)

		// Save JSON plan to file manually
		planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())

		planJSON, err := json.MarshalIndent(planResponse, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal plan to JSON: %w", err)
		}

		if err := hcpo.WriteWorkspaceFile(ctx, planPath, string(planJSON)); err != nil {
			return nil, nil, fmt.Errorf("failed to save plan.json: %w", err)
		}
		// Note: IDs are added above before marshaling, so they're included in the saved file

		// Note: Learning integration cache removal no longer needed - execution agent auto-discovers files

		hcpo.GetLogger().Infof("✅ JSON plan created successfully and saved to %s (%d steps, conversation has %d messages)", planPath, len(planResponse.Steps), len(updatedConversationHistory))
	} else {
		// UPDATE mode: Plan already saved by tools, just log
		hcpo.GetLogger().Infof("✅ Plan already saved by tools (%d steps, conversation has %d messages)", len(planResponse.Steps), len(updatedConversationHistory))
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
	hcpo.GetLogger().Infof("🔒 Setting folder guard for planning agent - Read paths: %v, Write paths: %v (planning automatically readable via writePaths)", readPaths, writePaths)

	// Use CreateAndSetupStandardAgentWithCustomServers instead of CreateAndSetupStandardAgentWithCustomServersAndSystemPrompt
	// because system prompt is passed directly to ExecuteStructuredWithInputProcessor() in planning_agent.go
	// Planning agent doesn't need custom tools - it only uses structured output tool
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"human-controlled-planning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers needed - pure LLM planning agent
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerPlanningAgent(config, logger, tracer, eventBridge)
		},
		[]llmtypes.Tool{},        // Empty - planning agent doesn't need custom tools
		map[string]interface{}{}, // Empty - planning agent doesn't need custom tool executors
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create planning agent: %w", err)
	}

	return agent, nil
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the generic ReadWorkspaceFile function from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {
	hcpo.GetLogger().Infof("🔍 Checking for existing plan at %s", planPath)

	// Use the generic ReadWorkspaceFile function from base orchestrator
	planContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📋 No existing plan found: %v", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	// Parse JSON content to PlanningResponse
	var planResponse PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &planResponse); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to parse existing plan.json: %v", err)
		return false, nil, fmt.Errorf("failed to parse plan.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Found existing plan at %s with %d steps", planPath, len(planResponse.Steps))
	return true, &planResponse, nil
}

// requestPlanApproval requests human approval for the generated plan
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestPlanApproval(
	ctx context.Context,
	revisionAttempt int,
) (bool, string, error) {
	hcpo.GetLogger().Infof("⏸️ Requesting human approval for plan (attempt %d)", revisionAttempt)

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
// parentTitle: title of the parent step (for generating unique IDs)
// branchType: "true" or "false" (for generating unique IDs)
// stepConfigs: step configs file for matching branch step configs by ID
func convertBranchSteps(planSteps []PlanStep, parentTitle, branchType string, stepConfigs *StepConfigFile) []TodoStep {
	if len(planSteps) == 0 {
		return nil
	}
	todoSteps := make([]TodoStep, len(planSteps))
	for i, step := range planSteps {
		// Match config by ID (parent-title + branch-type + nested-index + branch-title)
		var agentConfigs *AgentConfigs
		if stepConfigs != nil && step.Title != "" && parentTitle != "" {
			agentConfigs = MatchStepConfigByID(parentTitle, branchType, i, step.Title, stepConfigs)
			// Note: Configs will be nil if not found (which is expected for new steps without saved configs)
		}

		// Validation is required for loop steps to check loop conditions
		// Ensure validation is not disabled for loop steps
		if step.HasLoop && agentConfigs != nil && agentConfigs.DisableValidation {
			// Create a copy of configs with validation enabled
			enabledConfigs := *agentConfigs
			enabledConfigs.DisableValidation = false
			agentConfigs = &enabledConfigs
		}

		// Recursively convert nested branch steps
		var ifTrueSteps []TodoStep
		if len(step.IfTrueSteps) > 0 {
			// For nested branches, use current step title as parent
			ifTrueSteps = convertBranchSteps(step.IfTrueSteps, step.Title, "true", stepConfigs)
		}

		var ifFalseSteps []TodoStep
		if len(step.IfFalseSteps) > 0 {
			// For nested branches, use current step title as parent
			ifFalseSteps = convertBranchSteps(step.IfFalseSteps, step.Title, "false", stepConfigs)
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
	return todoSteps
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) convertPlanStepsToTodoSteps(ctx context.Context, planSteps []PlanStep) []TodoStep {
	// Read step configs from step_config.json
	stepConfigs, err := hcpo.ReadStepConfigs(ctx)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to read step_config.json: %v (using defaults for all steps)", err)
		stepConfigs = &StepConfigFile{Steps: []StepConfig{}}
	}

	// Match configs by step index (0-based)
	matchedConfigs := MatchStepConfigs(planSteps, stepConfigs)
	hcpo.GetLogger().Infof("📋 Matched %d/%d step configs from step_config.json", len(matchedConfigs), len(planSteps))

	todoSteps := make([]TodoStep, len(planSteps))
	for i, step := range planSteps {
		// Get matched config for this step (may be nil if no match)
		var agentConfigs *AgentConfigs
		if config, found := matchedConfigs[i]; found {
			agentConfigs = config
		}

		// Validation is required for loop steps to check loop conditions
		// Ensure validation is not disabled for loop steps
		if step.HasLoop && agentConfigs != nil && agentConfigs.DisableValidation {
			hcpo.GetLogger().Warnf("⚠️ Step '%s' is a loop step but has validation disabled - enabling validation (required for loop condition checks)", step.Title)
			// Create a copy of configs with validation enabled
			enabledConfigs := *agentConfigs
			enabledConfigs.DisableValidation = false
			agentConfigs = &enabledConfigs
		}

		// Convert branch steps recursively with parent title context
		var ifTrueSteps []TodoStep
		if len(step.IfTrueSteps) > 0 {
			// Use step title as parent for ID generation
			ifTrueSteps = convertBranchSteps(step.IfTrueSteps, step.Title, "true", stepConfigs)
		}

		var ifFalseSteps []TodoStep
		if len(step.IfFalseSteps) > 0 {
			// Use step title as parent for ID generation
			ifFalseSteps = convertBranchSteps(step.IfFalseSteps, step.Title, "false", stepConfigs)
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
	return todoSteps
}

// cleanupExistingPlanArtifacts deletes existing plan.json, steps_done.json, and all files in learnings/, execution/, and validation/ directories
// This is called when user chooses to create a new plan instead of using existing one
func (hcpo *HumanControlledTodoPlannerOrchestrator) cleanupExistingPlanArtifacts(ctx context.Context, workspacePath string) error {
	hcpo.GetLogger().Infof("🧹 Starting cleanup of existing plan artifacts")

	basePath := workspacePath

	// 1. Delete plan.json file
	planJSONPath := fmt.Sprintf("%s/planning/plan.json", basePath)
	if err := hcpo.DeleteWorkspaceFile(ctx, planJSONPath); err != nil {
		// Ignore "file not found" errors, but log others
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Warnf("⚠️ Failed to delete plan.json: %w", err)
		}
	} else {
		hcpo.GetLogger().Infof("🗑️ Deleted plan.json: %s", planJSONPath)
	}

	// 1.5. Delete plan_learnings.json cache (since plan structure will change)
	planLearningsPath := fmt.Sprintf("%s/planning/plan_learnings.json", basePath)
	if err := hcpo.DeleteWorkspaceFile(ctx, planLearningsPath); err != nil {
		// Ignore "file not found" errors, but log others
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Warnf("⚠️ Failed to delete plan_learnings.json: %w", err)
		}
	} else {
		hcpo.GetLogger().Infof("🗑️ Deleted plan_learnings.json: %s", planLearningsPath)
	}

	// 2. Clean all run folders (nuclear option - clean everything when creating new plan)
	runsPath := fmt.Sprintf("%s/runs", basePath)
	exists, _ := hcpo.workspaceFileExists(ctx, runsPath)
	if exists {
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err == nil && len(existingFolders) > 0 {
			hcpo.GetLogger().Infof("📁 Cleaning all run folders (%d found)", len(existingFolders))
			for _, folder := range existingFolders {
				runFolderPath := fmt.Sprintf("%s/runs/%s", basePath, folder)
				// Clean execution directory in run folder
				executionDir := fmt.Sprintf("%s/execution", runFolderPath)
				if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory in run folder %s: %w", folder, err)
				} else {
					hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory in run folder: %s", executionDir)
				}
				// Clean validation directory in run folder
				validationDir := fmt.Sprintf("%s/validation", runFolderPath)
				if err := hcpo.CleanupDirectory(ctx, validationDir, "validation"); err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to cleanup validation directory in run folder %s: %w", folder, err)
				} else {
					hcpo.GetLogger().Infof("🗑️ Cleaned up validation directory in run folder: %s", validationDir)
				}
				// Clean steps_done.json from run folder
				stepsDonePath := fmt.Sprintf("%s/steps_done.json", runFolderPath)
				if err := hcpo.DeleteWorkspaceFile(ctx, stepsDonePath); err != nil {
					// Ignore "file not found" errors, but log others
					if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
						hcpo.GetLogger().Warnf("⚠️ Failed to delete steps_done.json in run folder %s: %w", folder, err)
					}
				} else {
					hcpo.GetLogger().Infof("🗑️ Deleted steps_done.json from run folder: %s", stepsDonePath)
				}
			}
		}
	}

	// 3. Delete all files in old validation/ directory (backward compatibility)
	validationDir := fmt.Sprintf("%s/validation", basePath)
	if err := hcpo.CleanupDirectory(ctx, validationDir, "validation"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup validation directory: %w", err)
	}

	// 4. Note: learnings/ folder is preserved - deleted manually only

	// 5. Delete all files in old execution/ directory (backward compatibility)
	executionDir := fmt.Sprintf("%s/execution", basePath)
	if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
	}

	// Note: steps_done.json is now cleaned from run folders above (step 2), no longer in workspace root

	hcpo.GetLogger().Infof("✅ Cleanup of existing plan artifacts completed")
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
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit todo steps extracted event: %w", err)
	} else {
		bo.GetLogger().Infof("✅ Emitted todo steps extracted event: %d steps extracted", len(extractedSteps))
	}
}

// emitTodoStepsExtractedEvent is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitTodoStepsExtractedEvent(ctx context.Context, extractedSteps []TodoStep, planSource string) {
	// Use default extraction method and workspace path from orchestrator
	EmitTodoStepsExtractedEvent(ctx, hcpo.BaseOrchestrator, extractedSteps, planSource, "structured_breakdown_agent", "", hcpo.GetWorkspacePath())
}

// CreatePlanOnly runs only the planning phase (standalone, independent from CreateTodoList)
// This is a separate workflow phase that can be run independently
// Similar to ExtractVariablesOnly in variable_management.go
func (hcpo *HumanControlledTodoPlannerOrchestrator) CreatePlanOnly(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Infof("📋 Starting standalone planning for objective: %s", objective)

	// Set objective and workspace path
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// Check if variables.json exists - REQUIRED for planning
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := hcpo.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing variables: %w", err)
	}
	if !variablesExist {
		return "", fmt.Errorf("variables.json not found at %s - variable extraction must be run first as a separate phase", variablesPath)
	}

	// Variables exist - use them
	hcpo.variablesManifest = existingVariablesManifest
	templatedObjective := existingVariablesManifest.Objective
	hcpo.SetObjective(templatedObjective)
	hcpo.GetLogger().Infof("✅ Using templated objective with {{VARIABLES}}: %s", templatedObjective)

	// Load runtime variable values if provided
	if err := hcpo.loadVariableValues(ctx); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to load variable values: %w", err)
	}

	// Check if plan.json already exists
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing plan: %w", err)
		planExists = false
	}

	var approvedPlan *PlanningResponse
	var initialPlanningFeedback string
	var existingPlanForFirstUpdate *PlanningResponse

	// If plan exists, emit event immediately so UI can display it while user decides what to do
	if planExists {
		breakdownSteps := hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
		hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")
		hcpo.GetLogger().Infof("📋 Emitted plan event for UI display (%d steps)", len(breakdownSteps))
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
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing plan: %w", err)
			planChoice = "option0"
		}

		switch planChoice {
		case "option0":
			// Use existing plan
			hcpo.GetLogger().Infof("✅ User chose to use existing plan")
			approvedPlan = existingPlan
			hcpo.approvedPlan = approvedPlan
			// Event already emitted above when plan was found

		case "option1":
			// Create new plan - cleanup everything and create fresh plan
			hcpo.GetLogger().Infof("🔄 User chose to create new plan, cleaning up existing plan and related files")
			if err := hcpo.cleanupExistingPlanArtifacts(ctx, workspacePath); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to cleanup existing plan artifacts: %v (will continue anyway)", err)
			} else {
				hcpo.GetLogger().Infof("🗑️ Successfully cleaned up existing plan artifacts")
			}
			planExists = false

		case "option2":
			// Update existing plan - create new plan but keep artifacts (no cleanup)
			hcpo.GetLogger().Infof("🔄 User chose to update existing plan, creating new plan but keeping existing artifacts")

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
				hcpo.GetLogger().Warnf("⚠️ Failed to get update feedback: %v, proceeding without specific update guidance", err)
				initialPlanningFeedback = ""
			} else if approved {
				hcpo.GetLogger().Infof("ℹ️ User approved without providing update feedback, will create updated plan without specific guidance")
				initialPlanningFeedback = ""
			} else if updateFeedback != "" {
				hcpo.GetLogger().Infof("📝 Received update feedback: %s", updateFeedback)
				initialPlanningFeedback = updateFeedback
			} else {
				hcpo.GetLogger().Warnf("⚠️ Unexpected feedback state: approved=%v, feedback empty, proceeding without guidance", approved)
				initialPlanningFeedback = ""
			}

			planExists = false
			existingPlanForFirstUpdate = existingPlan

		default:
			hcpo.GetLogger().Warnf("⚠️ Unknown plan choice: %s, defaulting to use existing plan", planChoice)
			approvedPlan = existingPlan
			hcpo.approvedPlan = approvedPlan
		}
	}

	// Run planning phase if plan doesn't exist or user wants to create/update
	if !planExists && approvedPlan == nil {
		hcpo.GetLogger().Infof("🔄 Creating new plan to execute objective")

		maxPlanRevisions := 20
		humanFeedback := initialPlanningFeedback
		var planningConversationHistory []llmtypes.MessageContent

		for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Plan creation/approval attempt %d/%d", revisionAttempt, maxPlanRevisions)

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
				return "", fmt.Errorf("new plan has no steps: planning agent returned empty steps array")
			}

			// Convert approved plan steps to TodoStep format
			breakdownSteps := hcpo.convertPlanStepsToTodoSteps(ctx, approvedPlan.Steps)
			hcpo.GetLogger().Infof("✅ Converted new plan: %d steps extracted", len(breakdownSteps))

			// Emit todo steps extracted event
			hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "new_plan")

			// Request human approval for JSON plan
			approvedInternal, feedbackInternal, err := hcpo.requestPlanApproval(ctx, revisionAttempt)
			if err != nil {
				return "", fmt.Errorf("plan approval request failed: %w", err)
			}

			if approvedInternal {
				hcpo.GetLogger().Infof("✅ JSON plan approved by human")
				break
			}

			hcpo.GetLogger().Infof("🔄 Plan revision requested (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedbackInternal)
			humanFeedback = feedbackInternal

			if revisionAttempt >= maxPlanRevisions {
				return "", fmt.Errorf("max plan revision attempts (%d) reached", maxPlanRevisions)
			}
		}

		hcpo.approvedPlan = approvedPlan
	}

	// Ensure event is emitted at the end if we have an approved plan
	// This ensures the UI always sees the plan, even if event was emitted earlier
	if approvedPlan != nil {
		breakdownSteps := hcpo.convertPlanStepsToTodoSteps(ctx, approvedPlan.Steps)
		// Only emit if we haven't already emitted (to avoid duplicates)
		// We emit here to ensure the UI always gets the event
		hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")
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
