package todo_creation_human

import (
	"context"
	"encoding/json"
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
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/shared"
)

// StepProgress tracks which steps have been completed
type StepProgress struct {
	CompletedStepIndices []int     `json:"completed_step_indices"` // 0-based indices
	TotalSteps           int       `json:"total_steps"`
	LastUpdated          time.Time `json:"last_updated"`
}

// TodoStep represents a todo step in the execution
type TodoStep struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	SuccessPatterns     []string `json:"success_patterns,omitempty"` // NEW - what worked (includes tools)
	FailurePatterns     []string `json:"failure_patterns,omitempty"` // NEW - what failed (includes tools to avoid)
}

// TodoStepsExtractedEvent represents the event when todo steps are extracted from a plan
type TodoStepsExtractedEvent struct {
	events.BaseEventData
	TotalStepsExtracted int        `json:"total_steps_extracted"`
	ExtractedSteps      []TodoStep `json:"extracted_steps"`
	ExtractionMethod    string     `json:"extraction_method"`
	PlanSource          string     `json:"plan_source"` // "existing_plan" or "new_plan"
}

// GetEventType returns the event type for TodoStepsExtractedEvent
func (e *TodoStepsExtractedEvent) GetEventType() events.EventType {
	return events.TodoStepsExtracted
}

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
	variablesManifest  *VariablesManifest // Extracted variables
	templatedObjective string             // Objective with {{VARS}}
	variableValues     map[string]string  // Runtime variable values

	// Fast execute mode tracking
	fastExecuteMode    bool // Whether we're in fast execute mode
	fastExecuteEndStep int  // Last step index to fast execute (0-based)

	// Learning detail level preference (set once before execution, used for all learning phases)
	learningDetailLevel string // "exact" or "general"
}

// NewHumanControlledTodoPlannerOrchestrator creates a new human-controlled todo planner orchestrator
func NewHumanControlledTodoPlannerOrchestrator(
	provider string,
	model string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	selectedTools []string, // NEW parameter
	mcpConfigPath string,
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
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
		selectedTools, // Pass through actual selected tools
		llmConfig,
		maxTurns,
		customTools,
		customToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	return &HumanControlledTodoPlannerOrchestrator{
		BaseOrchestrator: baseOrchestrator,
		sessionID:        fmt.Sprintf("session_%d", time.Now().UnixNano()),
		workflowID:       fmt.Sprintf("workflow_%d", time.Now().UnixNano()),
	}, nil
}

// getStepsProgressPath returns the path to steps_done.json file
func (hcpo *HumanControlledTodoPlannerOrchestrator) getStepsProgressPath() string {
	return fmt.Sprintf("%s/todo_creation_human/steps_done.json", hcpo.GetWorkspacePath())
}

// loadStepProgress loads progress from steps_done.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadStepProgress(ctx context.Context) (*StepProgress, error) {
	progressPath := hcpo.getStepsProgressPath()

	content, err := hcpo.ReadWorkspaceFile(ctx, progressPath)
	if err != nil {
		// File doesn't exist or error reading
		return nil, err
	}

	var progress StepProgress
	if err := json.Unmarshal([]byte(content), &progress); err != nil {
		return nil, fmt.Errorf("failed to parse steps_done.json: %w", err)
	}

	return &progress, nil
}

// saveStepProgress saves progress to steps_done.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) saveStepProgress(ctx context.Context, progress *StepProgress) error {
	progressPath := hcpo.getStepsProgressPath()

	progress.LastUpdated = time.Now()

	progressJSON, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal progress: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, progressPath, string(progressJSON)); err != nil {
		return fmt.Errorf("failed to write steps_done.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Saved step progress to %s", progressPath)
	return nil
}

// deleteStepProgress deletes steps_done.json file
func (hcpo *HumanControlledTodoPlannerOrchestrator) deleteStepProgress(ctx context.Context) error {
	progressPath := hcpo.getStepsProgressPath()

	if err := hcpo.DeleteWorkspaceFile(ctx, progressPath); err != nil {
		// Ignore error if file doesn't exist
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return fmt.Errorf("failed to delete steps_done.json: %w", err)
	}

	hcpo.GetLogger().Infof("🗑️ Deleted step progress file: %s", progressPath)
	return nil
}

// CreateTodoList orchestrates the human-controlled todo planning process
// - Single execution (no iterations)
// - Includes validation phase (runs later in the workflow)
// - Includes critique phase during writer validation loop
// - Skips cleanup phase
// - Simple direct planning approach
// - NEW: Includes human approval loop with iterative plan refinement
func (hcpo *HumanControlledTodoPlannerOrchestrator) CreateTodoList(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Infof("🚀 Starting human-controlled todo planning for objective: %s", objective)

	// Set objective and workspace path directly
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// PHASE 0: Variable Extraction with Human Verification (NEW)
	// Check if variables.json already exists
	variablesPath := fmt.Sprintf("%s/todo_creation_human/variables/variables.json", workspacePath)
	variablesExist, existingVariablesManifest, err := hcpo.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing variables: %v", err)
		variablesExist = false
	}

	var variablesManifest *VariablesManifest
	var templatedObjective string

	// If variables exist, ask user if they want to use them or re-extract
	if variablesExist {
		requestID := fmt.Sprintf("existing_variables_decision_%d", time.Now().UnixNano())
		useExistingVariables, err := hcpo.RequestYesNoFeedback(
			ctx,
			requestID,
			"Found existing variables.json. Do you want to use the existing variables or extract new ones from the objective?",
			"Use Existing Variables", // Yes button label
			"Extract New Variables",  // No button label
			fmt.Sprintf("Variables file: %s\nFound %d variables", variablesPath, len(existingVariablesManifest.Variables)),
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing variables: %v", err)
			// Default to using existing variables
			useExistingVariables = true
		}

		if useExistingVariables {
			hcpo.GetLogger().Infof("✅ User chose to use existing variables")
			variablesManifest = existingVariablesManifest
			hcpo.variablesManifest = existingVariablesManifest // Store in orchestrator so formatVariableNames/Values can access it
			templatedObjective = existingVariablesManifest.Objective
		} else {
			hcpo.GetLogger().Infof("🔄 User chose to extract new variables, proceeding with extraction")
			// Delete existing variables file to ensure clean state before extraction
			if err := hcpo.DeleteWorkspaceFile(ctx, variablesPath); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to delete existing variables file: %v (will be overwritten during extraction)", err)
				// Continue anyway - extraction will overwrite the file
			} else {
				hcpo.GetLogger().Infof("🗑️ Deleted existing variables file: %s", variablesPath)
			}
			variablesExist = false // Trigger variable extraction
		}
	}

	// Extract variables if they don't exist or user wants to re-extract
	if !variablesExist {
		maxVariableRevisions := 10
		var variableFeedback string
		var variableConversationHistory []llmtypes.MessageContent

		for revisionAttempt := 1; revisionAttempt <= maxVariableRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

			// Run variable extraction phase (with optional human feedback)
			var err error
			variablesManifest, templatedObjective, err = hcpo.runVariableExtractionPhase(ctx, revisionAttempt, variableFeedback, variableConversationHistory)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Variable extraction failed: %v, continuing without variables", err)
				templatedObjective = objective // Use original objective if extraction fails
				break
			}

			// Accumulate conversation history for next iteration
			variableConversationHistory = append(variableConversationHistory, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeAI,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: fmt.Sprintf("Extracted %d variables from objective", len(variablesManifest.Variables))}},
			})

			hcpo.GetLogger().Infof("✅ Extracted %d variables, templated objective: %s",
				len(variablesManifest.Variables), templatedObjective)

			// Request human approval for extracted variables
			approved, feedback, err := hcpo.requestVariableApproval(ctx, variablesManifest, revisionAttempt)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Variable approval request failed: %v, will retry", err)
				// Don't auto-approve on error - treat as need for retry
				approved = false
				feedback = fmt.Sprintf("Error getting approval: %v", err)
			}

			if approved {
				hcpo.GetLogger().Infof("✅ Variables approved by human, proceeding to planning")
				break // Exit retry loop
			}

			// Variables rejected with feedback for revision
			hcpo.GetLogger().Infof("🔄 Variable revision requested (attempt %d/%d): %s", revisionAttempt, maxVariableRevisions, feedback)
			variableFeedback = feedback // Store feedback for next attempt

			if revisionAttempt >= maxVariableRevisions {
				hcpo.GetLogger().Warnf("⚠️ Max variable revision attempts (%d) reached, using extracted variables", maxVariableRevisions)
				break
			}
		}
	}

	// Load runtime variable values if provided and switch to templated objective
	if variablesManifest != nil {
		if err := hcpo.loadVariableValues(ctx); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to load variable values: %v", err)
		}

		// Switch to templated objective for all subsequent phases
		hcpo.SetObjective(templatedObjective)
		hcpo.GetLogger().Infof("✅ Using templated objective with {{VARIABLES}}: %s", templatedObjective)
	}

	// Check if plan.md already exists
	planPath := fmt.Sprintf("%s/todo_creation_human/planning/plan.md", workspacePath)
	planExists, planContent, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing plan: %v", err)
		// Continue with normal planning flow
		planExists = false
	}

	var breakdownSteps []TodoStep
	var initialPlanningFeedback string // Store feedback for plan updates

	if planExists {
		hcpo.GetLogger().Infof("📋 Found existing plan.md at %s", planPath)

		// Request human decision: use existing plan, create new plan, or update existing plan
		requestID := fmt.Sprintf("existing_plan_decision_%d", time.Now().UnixNano())
		planChoice, err := hcpo.RequestThreeChoiceFeedback(
			ctx,
			requestID,
			"Found existing plan.md. What would you like to do?",
			"Use Existing Plan",    // Option 1: Use existing plan as-is
			"Create New Plan",      // Option 2: Delete everything and create new plan
			"Update Existing Plan", // Option 3: Create new plan but keep existing artifacts
			fmt.Sprintf("Plan location: %s", planPath),
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing plan: %v", err)
			// Default to using existing plan
			planChoice = "option1"
		}

		switch planChoice {
		case "option1":
			// Use existing plan - keep planExists = true, proceed with plan reader
			hcpo.GetLogger().Infof("✅ User chose to use existing plan, converting to JSON and proceeding to execution")
			// planExists remains true, will continue with plan reader below

		case "option2":
			// Create new plan - cleanup everything and create fresh plan
			hcpo.GetLogger().Infof("🔄 User chose to create new plan, cleaning up existing plan and related files")
			// Clean up existing plan and all related execution artifacts
			if err := hcpo.cleanupExistingPlanArtifacts(ctx, workspacePath); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to cleanup existing plan artifacts: %v (will continue anyway)", err)
			} else {
				hcpo.GetLogger().Infof("🗑️ Successfully cleaned up existing plan artifacts")
			}
			planExists = false

		case "option3":
			// Update existing plan - create new plan but keep artifacts (no cleanup)
			hcpo.GetLogger().Infof("🔄 User chose to update existing plan, creating new plan but keeping existing artifacts")

			// Request human feedback about what they want to update in the plan
			updateFeedbackID := fmt.Sprintf("plan_update_feedback_%d", time.Now().UnixNano())
			approved, updateFeedback, err := hcpo.RequestHumanFeedback(
				ctx,
				updateFeedbackID,
				"What would you like to update in the existing plan? Please describe the changes or improvements you want.",
				fmt.Sprintf("Current plan location: %s\n\nYour feedback will be used to guide the creation of an updated plan while preserving existing validation, learning, and execution artifacts.", planPath),
				hcpo.getSessionID(),
				hcpo.getWorkflowID(),
			)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to get update feedback: %v, proceeding without specific update guidance", err)
				initialPlanningFeedback = "" // Proceed without feedback
			} else if approved {
				// User clicked "Approve" without providing feedback (approved=true means response was "Approve")
				hcpo.GetLogger().Infof("ℹ️ User approved without providing update feedback, will create updated plan without specific guidance")
				initialPlanningFeedback = ""
			} else if updateFeedback != "" {
				// User provided feedback (approved=false and feedback contains their input)
				hcpo.GetLogger().Infof("📝 Received update feedback: %s", updateFeedback)
				initialPlanningFeedback = updateFeedback // Store for use in planning phase
			} else {
				// Edge case: approved=false but empty feedback
				hcpo.GetLogger().Warnf("⚠️ Unexpected feedback state: approved=%v, feedback empty, proceeding without guidance", approved)
				initialPlanningFeedback = ""
			}

			// Don't cleanup - just set planExists to false so new plan will be created
			// Existing artifacts in validation/, learnings/, execution/ will be preserved
			planExists = false

		default:
			// Unknown choice - default to using existing plan
			hcpo.GetLogger().Warnf("⚠️ Unknown plan choice: %s, defaulting to use existing plan", planChoice)
			// planExists remains true
		}
	}

	if planExists {
		// Convert markdown plan to structured JSON using plan reader agent
		planReaderAgent, err := hcpo.createPlanReaderAgent(ctx, "plan_reading", 0, 1)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to create plan reader agent: %v", err)
			// Fall through to create new plan
			planExists = false
		} else {
			// Prepare template variables for plan reader agent
			readerTemplateVars := map[string]string{
				"Objective":     hcpo.GetObjective(),
				"WorkspacePath": hcpo.GetWorkspacePath(),
				"PlanMarkdown":  planContent, // Use the markdown content we found
			}
			// Add variable names if available
			if variableNames := hcpo.formatVariableNames(); variableNames != "" {
				readerTemplateVars["VariableNames"] = variableNames
			}

			// Execute plan reader agent to get structured output
			planReaderAgentTyped, ok := planReaderAgent.(*HumanControlledPlanReaderAgent)
			if !ok {
				hcpo.GetLogger().Warnf("⚠️ Failed to cast plan reader agent to correct type")
				planExists = false
			} else {
				existingPlan, err := planReaderAgentTyped.ExecuteStructured(ctx, readerTemplateVars, []llmtypes.MessageContent{})
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to convert markdown plan to JSON: %v", err)
					// Fall through to create new plan
					planExists = false
				} else {
					// Safety check: Ensure plan has steps
					if len(existingPlan.Steps) == 0 {
						hcpo.GetLogger().Errorf("❌ Existing plan has no steps - plan reader returned empty steps array")
						return "", fmt.Errorf("existing plan has no steps: plan reader returned empty steps array")
					}

					// Convert existing plan to TodoStep format
					breakdownSteps = hcpo.convertPlanStepsToTodoSteps(existingPlan.Steps)
					hcpo.GetLogger().Infof("✅ Converted existing plan: %d steps extracted", len(breakdownSteps))
					hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")

					// Request human approval for JSON plan (after event emission)
					maxPlanRevisions := 20 // Allow up to 20 plan revisions
					var humanFeedback string
					approved := false

					for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
						hcpo.GetLogger().Infof("🔄 Plan JSON approval attempt %d/%d", revisionAttempt, maxPlanRevisions)

						// Request human approval for JSON plan
						approvedInternal, feedbackInternal, err := hcpo.requestPlanApproval(ctx, revisionAttempt)
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Plan approval request failed: %v", err)
							// Default to approved if approval request fails
							approved = true
							break
						}

						if approvedInternal {
							hcpo.GetLogger().Infof("✅ JSON plan approved by human, proceeding to execution")
							approved = true
							break // Exit retry loop and continue to execution
						}

						// Plan rejected with feedback for revision
						hcpo.GetLogger().Infof("🔄 Plan revision requested (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedbackInternal)
						humanFeedback = feedbackInternal // Store feedback for next attempt

						if revisionAttempt >= maxPlanRevisions {
							hcpo.GetLogger().Warnf("⚠️ Max plan revision attempts (%d) reached, proceeding with current plan", maxPlanRevisions)
							approved = true // Proceed anyway
							break
						}

						// If user wants to revise, we need to go back and create a new plan
						// For existing plans, revision means creating a new plan
						if humanFeedback != "" {
							hcpo.GetLogger().Infof("🔄 User requested revision of existing plan, will create new plan")
							planExists = false
							break // Break out and fall through to create new plan
						}
					}

					if approved && planExists {
						// Plan approved, continue to execution
						hcpo.GetLogger().Infof("✅ Existing plan approved: %d steps ready for execution", len(breakdownSteps))
					}
				}
			}
		}
	}

	if !planExists {
		hcpo.GetLogger().Infof("🔄 No existing plan found, creating new plan to execute objective")

		// NOTE: Don't delete existing progress here - only delete when actually starting new execution
		// This prevents losing progress if plan reader fails or if user chooses to use existing plan

		// Phase 1.9: Planning → Plan Reader → Approval loop
		maxPlanRevisions := 20 // Allow up to 20 plan revisions
		// Initialize with initial planning feedback (e.g., from "Update Existing Plan" option)
		humanFeedback := initialPlanningFeedback
		var planReaderConversationHistory []llmtypes.MessageContent
		var approvedPlan *PlanningResponse
		var err error

		for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Plan creation/approval attempt %d/%d", revisionAttempt, maxPlanRevisions)

			// Phase 1: Create markdown plan (with optional human feedback)
			_, planReaderConversationHistory, err = hcpo.runPlanningPhase(ctx, revisionAttempt, humanFeedback, planReaderConversationHistory)
			if err != nil {
				return "", fmt.Errorf("planning phase failed: %w", err)
			}

			// Phase 1.75: Read markdown plan and convert to structured JSON
			approvedPlan, err = hcpo.runPlanReaderPhase(ctx)
			if err != nil {
				return "", fmt.Errorf("plan reader phase failed: %w", err)
			}

			// Safety check: Ensure plan has steps
			if len(approvedPlan.Steps) == 0 {
				return "", fmt.Errorf("new plan has no steps: plan reader returned empty steps array")
			}

			// Convert approved plan steps to TodoStep format for execution
			breakdownSteps = hcpo.convertPlanStepsToTodoSteps(approvedPlan.Steps)
			hcpo.GetLogger().Infof("✅ Converted new plan: %d steps extracted", len(breakdownSteps))

			// Emit todo steps extracted event after plan reader conversion
			hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "new_plan_converted")

			// Request human approval for JSON plan (after event emission)
			approvedInternal, feedbackInternal, err := hcpo.requestPlanApproval(ctx, revisionAttempt)
			if err != nil {
				return "", fmt.Errorf("plan approval request failed: %w", err)
			}

			if approvedInternal {
				hcpo.GetLogger().Infof("✅ JSON plan approved by human, proceeding to execution with %d steps", len(breakdownSteps))
				break // Exit retry loop and continue to execution
			}

			// Plan rejected with feedback for revision
			hcpo.GetLogger().Infof("🔄 Plan revision requested (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedbackInternal)
			humanFeedback = feedbackInternal // Store feedback for next iteration

			if revisionAttempt >= maxPlanRevisions {
				return "", fmt.Errorf("max plan revision<|uniquepaddingtoken122|> attempts (%d) reached", maxPlanRevisions)
			}
		}

		// Plan approved and converted, continue to execution
	}

	// EARLY PROGRESS CHECK: Check if all steps are already completed before proceeding
	// This prevents running plan reader unnecessarily if all steps are done
	hcpo.GetLogger().Infof("🔍 Early progress check: Checking if all steps are already completed")
	hcpo.GetLogger().Infof("🔍 DEBUG: breakdownSteps count before early progress check: %d", len(breakdownSteps))

	earlyProgress, err := hcpo.loadStepProgress(ctx)
	if err == nil && earlyProgress != nil && len(earlyProgress.CompletedStepIndices) > 0 {
		hcpo.GetLogger().Infof("📊 Found early progress: %d/%d steps completed",
			len(earlyProgress.CompletedStepIndices), earlyProgress.TotalSteps)

		// Check if total steps match
		if earlyProgress.TotalSteps == len(breakdownSteps) {
			// Calculate if all steps are completed
			if len(earlyProgress.CompletedStepIndices) == earlyProgress.TotalSteps {
				hcpo.GetLogger().Infof("✅ ALL steps already completed - skipping to writer phase")

				// Phase 3: Write/Update todo list with critique validation loop
				err = hcpo.runWriterPhaseWithHumanReview(ctx, 1)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Writer phase with critique validation failed: %v", err)
				}

				// Return early with completion message
				return "Todo planning complete. All steps already executed. Final todo list saved as `todo_final.md`.", nil
			}
			hcpo.GetLogger().Infof("📊 Not all steps completed yet - will proceed with execution")
		} else {
			hcpo.GetLogger().Warnf("⚠️ Total steps changed (previous: %d, current: %d), will create new progress",
				earlyProgress.TotalSteps, len(breakdownSteps))
			earlyProgress = nil // Don't use old progress if plan changed
		}
	}

	// Check for existing progress and ask user if they want to resume
	var startFromStep int = 0 // 0-based index, 0 means start from beginning
	var existingProgress *StepProgress

	// Use earlyProgress if available, otherwise load it
	if earlyProgress != nil {
		existingProgress = earlyProgress
		err = nil // Reset err since earlyProgress was successfully loaded earlier
		hcpo.GetLogger().Infof("✅ Using early progress (avoided reload)")
	} else {
		// Check if there's existing progress
		existingProgress, err = hcpo.loadStepProgress(ctx)
		if err != nil {
			// File doesn't exist - this is normal for first run, log and continue
			hcpo.GetLogger().Infof("ℹ️ No existing progress file found (this is normal for first run), will start fresh execution")
			existingProgress = nil
			err = nil // Reset err to allow execution to proceed
		}
	}

	// Process existing progress if available
	if err == nil && existingProgress != nil && len(existingProgress.CompletedStepIndices) > 0 {
		hcpo.GetLogger().Infof("📊 Found existing progress: %d/%d steps completed",
			len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps)

		// Check if total steps match (plan might have changed)
		if existingProgress.TotalSteps != len(breakdownSteps) {
			hcpo.GetLogger().Warnf("⚠️ Plan has changed (different number of steps), ignoring previous progress")
			existingProgress = nil
		} else {
			// Check if all steps are completed first
			allStepsCompleted := len(existingProgress.CompletedStepIndices) == existingProgress.TotalSteps

			// Ask user if they want to resume
			nextIncompleteStep := 0
			if !allStepsCompleted {
				for i := 0; i < existingProgress.TotalSteps; i++ {
					completed := false
					for _, completedIdx := range existingProgress.CompletedStepIndices {
						if completedIdx == i {
							completed = true
							break
						}
					}
					if !completed {
						nextIncompleteStep = i + 1 // 1-based for display
						break
					}
				}
			}

			if allStepsCompleted {
				// All steps are completed, skip directly to writer phase
				hcpo.GetLogger().Infof("✅ All steps already completed (%d/%d), skipping execution phase and going directly to writer phase",
					len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps)

				// Phase 3: Write/Update todo list with critique validation loop
				err = hcpo.runWriterPhaseWithHumanReview(ctx, 1)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Writer phase with critique validation failed: %v", err)
				}

				// Return early with completion message
				return "Todo planning complete. All steps already executed. Final todo list saved as `todo_final.md`.", nil
			} else if nextIncompleteStep > 0 {
				// Calculate the last completed step number (1-based) for display
				lastCompletedStepNumber := max(existingProgress.CompletedStepIndices) + 1 // Convert to 1-based

				requestID := fmt.Sprintf("resume_progress_%d", time.Now().UnixNano())
				choice, err := hcpo.RequestThreeChoiceFeedback(
					ctx,
					requestID,
					fmt.Sprintf("Found existing progress: %d/%d steps completed. How would you like to proceed?",
						len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps),
					fmt.Sprintf("Resume from Step %d", nextIncompleteStep),
					"Start from Beginning",
					fmt.Sprintf("Fast Execute (0 to Step %d)", lastCompletedStepNumber),
					fmt.Sprintf("Last updated: %s", existingProgress.LastUpdated.Format("2006-01-02 15:04:05")),
					hcpo.getSessionID(),
					hcpo.getWorkflowID(),
				)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for resuming: %v", err)
					choice = "option1" // Default to resume
				}

				// Track fast execute mode
				fastExecuteMode := false
				fastExecuteEndStep := -1

				switch choice {
				case "option1": // Resume from next incomplete step
					startFromStep = nextIncompleteStep - 1 // Convert back to 0-based
					hcpo.GetLogger().Infof("✅ User chose to resume from step %d", nextIncompleteStep)
				case "option2": // Start from beginning (normal execution)
					hcpo.GetLogger().Infof("🔄 User chose to start from beginning, will reset progress")
					// Delete existing progress and start fresh
					if err := hcpo.deleteStepProgress(ctx); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %v", err)
					}
					existingProgress = nil
					startFromStep = 0
				case "option3": // Fast execute completed steps
					hcpo.GetLogger().Infof("⚡ User chose fast execute mode for completed steps")
					fastExecuteMode = true
					fastExecuteEndStep = max(existingProgress.CompletedStepIndices)
					// Delete previous completed indices to re-execute them
					startFromStep = 0
					// Reset completed indices for steps to be re-executed
					var newCompletedIndices []int
					for _, idx := range existingProgress.CompletedStepIndices {
						if idx > fastExecuteEndStep {
							newCompletedIndices = append(newCompletedIndices, idx)
						}
					}
					existingProgress.CompletedStepIndices = newCompletedIndices
					hcpo.GetLogger().Infof("⚡ Will fast execute steps 0 to %d, then continue with normal execution from step %d", fastExecuteEndStep, nextIncompleteStep)
				}

				// Store fast execute mode for use in execution loop
				hcpo.SetFastExecuteMode(fastExecuteMode, fastExecuteEndStep)
			} else {
				// This should not happen if logic is correct, but handle edge case
				hcpo.GetLogger().Warnf("⚠️ Unexpected state: progress exists but couldn't determine next incomplete step. Starting from beginning.")
				existingProgress = nil
				startFromStep = 0
			}
		}
	}

	// Phase 2: Execute plan steps one by one (with validation after each step)

	// Safety check: Ensure breakdownSteps is not empty
	if len(breakdownSteps) == 0 {
		return "", fmt.Errorf("no steps to execute: breakdownSteps is empty (this should not happen - plan was approved but has no steps)")
	}

	hcpo.GetLogger().Infof("✅ Proceeding to execution phase with %d steps", len(breakdownSteps))

	// Initialize progress tracking if not already loaded
	if existingProgress == nil {
		existingProgress = &StepProgress{
			CompletedStepIndices: []int{},
			TotalSteps:           len(breakdownSteps),
		}
	}

	_, err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1, existingProgress, startFromStep)
	if err != nil {
		return "", fmt.Errorf("execution phase failed: %w", err)
	}

	// Phase 3: Write/Update todo list with critique validation loop
	err = hcpo.runWriterPhaseWithHumanReview(ctx, 1)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Writer phase with critique validation failed: %v", err)
	}

	duration := time.Since(hcpo.GetStartTime())
	hcpo.GetLogger().Infof("✅ Human-controlled todo planning completed in %v", duration)

	return "Todo planning complete. Final todo list saved as `todo_final.md`.", nil
}

// runPlanningPhase creates markdown plan
// conversationHistory is updated in-place to accumulate across iterations
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanningPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	planningTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Add human feedback as a user message to conversation history BEFORE executing
	// This ensures it's part of the conversation and won't be duplicated by BaseAgent.Execute
	if humanFeedback != "" {
		feedbackMessage := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: humanFeedback}},
		}
		conversationHistory = append(conversationHistory, feedbackMessage)
		hcpo.GetLogger().Infof("📝 Added human feedback as user message to conversation history for iteration %d", iteration)
	}

	// Create fresh planning agent with proper context
	planningAgent, err := hcpo.createPlanningAgent(ctx, "planning", 0, iteration)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create planning agent: %w", err)
	}

	// Execute planning agent
	// If this is the first iteration (empty conversationHistory), default message will be used
	// If conversationHistory already contains human feedback, it will be used from history
	_, updatedConversationHistory, err := planningAgent.Execute(ctx, planningTemplateVars, conversationHistory)
	if err != nil {
		return "", nil, fmt.Errorf("planning failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Markdown plan created successfully (conversation has %d messages)", len(updatedConversationHistory))
	return "markdown_plan_created", updatedConversationHistory, nil
}

// runPlanReaderPhase reads markdown plan and converts to structured JSON
func (hcpo *HumanControlledTodoPlannerOrchestrator) runPlanReaderPhase(ctx context.Context) (*PlanningResponse, error) {
	hcpo.GetLogger().Infof("📖 Reading markdown plan and converting to structured JSON")

	// Create plan reader agent
	planReaderAgent, err := hcpo.createPlanReaderAgent(ctx, "plan_reading", 0, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan reader agent: %w", err)
	}

	// Read markdown plan content from workspace
	planPath := fmt.Sprintf("%s/todo_creation_human/planning/plan.md", hcpo.GetWorkspacePath())
	hcpo.GetLogger().Infof("📖 Reading plan markdown from: %s", planPath)

	planMarkdown, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if this is a file not found error (expected case for new plans)
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📝 Plan file not found, using empty content (new plan): %s", planPath)
			planMarkdown = ""
		} else {
			hcpo.GetLogger().Errorf("❌ Failed to read plan file %s: %v", planPath, err)
			return nil, fmt.Errorf("failed to read plan file %s: %w", planPath, err)
		}
	} else {
		hcpo.GetLogger().Infof("✅ Successfully read plan markdown (%d characters)", len(planMarkdown))
	}

	// Prepare template variables for plan reader agent
	readerTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
		"PlanMarkdown":  planMarkdown,
	}
	// Add variable names if available
	if variableNames := hcpo.formatVariableNames(); variableNames != "" {
		readerTemplateVars["VariableNames"] = variableNames
	}

	// Execute plan reader agent to get structured output
	planReaderAgentTyped, ok := planReaderAgent.(*HumanControlledPlanReaderAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast plan reader agent to correct type")
	}

	result, err := planReaderAgentTyped.ExecuteStructured(ctx, readerTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return nil, fmt.Errorf("plan reading failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Plan converted to structured JSON successfully")
	return result, nil
}

// convertPlanStepsToTodoSteps converts PlanStep to TodoStep format
func (hcpo *HumanControlledTodoPlannerOrchestrator) convertPlanStepsToTodoSteps(planSteps []PlanStep) []TodoStep {
	todoSteps := make([]TodoStep, len(planSteps))
	for i, step := range planSteps {
		// Convert FlexibleContextOutput to string for TodoStep
		todoSteps[i] = TodoStep{
			Title:               step.Title,
			Description:         step.Description,
			SuccessCriteria:     step.SuccessCriteria,
			WhyThisStep:         step.WhyThisStep,
			ContextDependencies: step.ContextDependencies,
			ContextOutput:       step.ContextOutput.String(), // Convert FlexibleContextOutput to string
			SuccessPatterns:     step.SuccessPatterns,
			FailurePatterns:     step.FailurePatterns,
		}
	}
	return todoSteps
}

// runExecutionPhase executes the plan steps one by one
func (hcpo *HumanControlledTodoPlannerOrchestrator) runExecutionPhase(
	ctx context.Context,
	breakdownSteps []TodoStep,
	iteration int,
	progress *StepProgress,
	startFromStep int,
) ([]llmtypes.MessageContent, error) {
	hcpo.GetLogger().Infof("🔄 Starting step-by-step execution of %d steps (starting from step %d)",
		len(breakdownSteps), startFromStep+1)

	// Request learning detail level preference ONCE before execution starts
	// This preference will be used for all learning phases (both success and failure)
	// ASKED IN ALL MODES (including fast mode) - learning happens even in fast mode
	if len(breakdownSteps) > 0 {
		// Ask once for all steps (use generic question for all steps)
		learningDetailLevel, err := hcpo.requestLearningDetailLevel(ctx, 0, len(breakdownSteps), fmt.Sprintf("All %d steps", len(breakdownSteps)), false)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get learning detail level preference: %v, defaulting to 'general'", err)
			hcpo.learningDetailLevel = "general"
		} else {
			hcpo.learningDetailLevel = learningDetailLevel
			hcpo.GetLogger().Infof("📝 Learning detail level set to '%s' for all learning phases (all modes)", learningDetailLevel)
		}
	} else {
		hcpo.learningDetailLevel = "general"
	}

	// Track human feedback across all steps for continuous improvement
	var humanFeedbackHistory []string

	// Execute each step one by one
	for i, step := range breakdownSteps {
		// Skip if step is already completed
		if i < startFromStep {
			hcpo.GetLogger().Infof("⏭️ Skipping step %d/%d (already completed): %s",
				i+1, len(breakdownSteps), step.Title)
			continue
		}

		// Check if step is in completed list
		isCompleted := false
		for _, completedIdx := range progress.CompletedStepIndices {
			if completedIdx == i {
				isCompleted = true
				break
			}
		}
		if isCompleted {
			hcpo.GetLogger().Infof("⏭️ Skipping step %d/%d (marked as completed): %s",
				i+1, len(breakdownSteps), step.Title)
			continue
		}

		hcpo.GetLogger().Infof("📋 Executing step %d/%d: %s", i+1, len(breakdownSteps), step.Title)

		// Initialize variables for step execution
		maxRetryAttempts := 3
		var executionConversationHistory []llmtypes.MessageContent
		var humanFeedback string
		stepCompleted := false

		// Outer loop: Handle re-execution with human feedback
		for !stepCompleted {
			// Add human feedback to conversation history if provided
			if humanFeedback != "" {
				humanFeedbackMessage := llmtypes.MessageContent{
					Role: llmtypes.ChatMessageTypeHuman,
					Parts: []llmtypes.ContentPart{llmtypes.TextContent{
						Text: fmt.Sprintf("## Human Feedback for Step %d:\n%s", i+1, humanFeedback),
					}},
				}
				executionConversationHistory = append(executionConversationHistory, humanFeedbackMessage)
				hcpo.GetLogger().Infof("📝 Added human feedback to conversation history for step %d", i+1)
				humanFeedback = "" // Reset for next iteration
			}

			// Prepare template variables for this specific step with individual fields
			// RESOLVE VARIABLES: Replace {{VARS}} with actual values for execution
			templateVars := map[string]string{
				"StepNumber":          fmt.Sprintf("%d", i+1),
				"TotalSteps":          fmt.Sprintf("%d", len(breakdownSteps)),
				"StepTitle":           hcpo.resolveVariables(step.Title),
				"StepDescription":     hcpo.resolveVariables(step.Description),
				"StepSuccessCriteria": hcpo.resolveVariables(step.SuccessCriteria),
				"StepWhyThisStep":     hcpo.resolveVariables(step.WhyThisStep),
				"StepContextOutput":   hcpo.resolveVariables(step.ContextOutput),
				"WorkspacePath":       hcpo.GetWorkspacePath(),
				"LearningAgentOutput": "", // Will be populated with learning agent's output
			}

			// Combine success and failure patterns from plan breakdown into LearningAgentOutput
			var learningOutputParts []string
			if len(step.SuccessPatterns) > 0 {
				learningOutputParts = append(learningOutputParts, "## ✅ Success Patterns from Plan:")
				for _, pattern := range step.SuccessPatterns {
					learningOutputParts = append(learningOutputParts, fmt.Sprintf("- Success Pattern: %s", pattern))
				}
			}
			if len(step.FailurePatterns) > 0 {
				learningOutputParts = append(learningOutputParts, "## ❌ Failure Patterns from Plan:")
				for _, pattern := range step.FailurePatterns {
					learningOutputParts = append(learningOutputParts, fmt.Sprintf("- Failure Pattern: %s", pattern))
				}
			}

			if len(learningOutputParts) > 0 {
				templateVars["LearningAgentOutput"] = strings.Join(learningOutputParts, "\n")
			} else {
				templateVars["LearningAgentOutput"] = ""
			}

			// Add context dependencies as a comma-separated string (also resolve variables)
			if len(step.ContextDependencies) > 0 {
				resolvedDeps := make([]string, len(step.ContextDependencies))
				for idx, dep := range step.ContextDependencies {
					resolvedDeps[idx] = hcpo.resolveVariables(dep)
				}
				templateVars["StepContextDependencies"] = strings.Join(resolvedDeps, ", ")
			} else {
				templateVars["StepContextDependencies"] = ""
			}

			// Add variable names if available (same format as other agents)
			if variableNames := hcpo.formatVariableNames(); variableNames != "" {
				templateVars["VariableNames"] = variableNames
			}

			// Add variable values if available (name = value - description format)
			if variableValues := hcpo.formatVariableValues(); variableValues != "" {
				templateVars["VariableValues"] = variableValues
			}

			// Add human feedback from previous steps to conversation history (first iteration only)
			if len(humanFeedbackHistory) > 0 && len(executionConversationHistory) == 0 {
				previousFeedbackMessage := llmtypes.MessageContent{
					Role: llmtypes.ChatMessageTypeHuman,
					Parts: []llmtypes.ContentPart{llmtypes.TextContent{
						Text: fmt.Sprintf("## Previous Steps' Feedback for Context:\n%s", strings.Join(humanFeedbackHistory, "\n---\n")),
					}},
				}
				executionConversationHistory = append(executionConversationHistory, previousFeedbackMessage)
				hcpo.GetLogger().Infof("📝 Added human feedback from previous steps to conversation history for step %d", i+1)
			}

			// Inner loop: Automatic retry logic
			var validationFeedback []ValidationFeedback
			var validationResponse *ValidationResponse

			for retryAttempt := 1; retryAttempt <= maxRetryAttempts; retryAttempt++ {
				hcpo.GetLogger().Infof("🔄 Executing step %d/%d (attempt %d/%d): %s", i+1, len(breakdownSteps), retryAttempt, maxRetryAttempts, step.Title)

				// Add validation feedback to template variables if this is a retry
				if retryAttempt > 1 && validationFeedback != nil {
					feedbackJSON, _ := json.Marshal(validationFeedback)
					templateVars["ValidationFeedback"] = fmt.Sprintf("## Validation Feedback (Retry Attempt %d):\n%s", retryAttempt, string(feedbackJSON))
					hcpo.GetLogger().Infof("📝 Added validation feedback to template variables for step %d, retry %d", i+1, retryAttempt)
				} else {
					templateVars["ValidationFeedback"] = "" // No validation feedback for first attempt
				}

				// Create execution agent for this step
				// Resolve variables in step title before using in agent name
				resolvedTitle := hcpo.resolveVariables(step.Title)
				agentName := fmt.Sprintf("execution-agent-step-%d-%s", i+1, strings.ReplaceAll(resolvedTitle, " ", "-"))
				executionAgent, err := hcpo.createExecutionAgent(ctx, "execution", i+1, iteration, agentName)
				if err != nil {
					return nil, fmt.Errorf("failed to create execution agent for step %d: %w", i+1, err)
				}

				// Execute this specific step with execution conversation history
				_, executionConversationHistory, err = executionAgent.Execute(ctx, templateVars, executionConversationHistory)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Step %d execution failed (attempt %d): %v", i+1, retryAttempt, err)
					if retryAttempt >= maxRetryAttempts {
						hcpo.GetLogger().Errorf("❌ Step %d execution failed after %d attempts, exiting retry loop", i+1, maxRetryAttempts)
						break // Exit retry loop - will proceed to human feedback
					}
					continue // Retry on next attempt
				}

				hcpo.GetLogger().Infof("✅ Step %d execution completed successfully (attempt %d)", i+1, retryAttempt)

				// Validate this step's execution using structured output
				hcpo.GetLogger().Infof("🔍 Validating step %d execution (attempt %d)", i+1, retryAttempt)

				// Reuse resolved title from execution agent (already resolved above)
				validationAgentName := fmt.Sprintf("validation-agent-step-%d-%s", i+1, strings.ReplaceAll(resolvedTitle, " ", "-"))
				validationAgent, err := hcpo.createValidationAgent(ctx, "validation", i+1, iteration, validationAgentName)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to create validation agent for step %d: %v", i+1, err)
					if retryAttempt >= maxRetryAttempts {
						break // Exit retry loop - will proceed to human feedback
					}
					continue // Retry on next attempt
				}

				// Prepare validation template variables with individual fields
				validationTemplateVars := map[string]string{
					"StepNumber":          fmt.Sprintf("%d", i+1),
					"TotalSteps":          fmt.Sprintf("%d", len(breakdownSteps)),
					"StepTitle":           step.Title,
					"StepDescription":     step.Description,
					"StepSuccessCriteria": step.SuccessCriteria,
					"StepWhyThisStep":     step.WhyThisStep,
					"StepContextOutput":   step.ContextOutput,
					"WorkspacePath":       hcpo.GetWorkspacePath(),
					"ExecutionHistory":    shared.FormatConversationHistory(executionConversationHistory),
				}

				// Add context dependencies as a comma-separated string
				if len(step.ContextDependencies) > 0 {
					validationTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
				} else {
					validationTemplateVars["StepContextDependencies"] = ""
				}

				// Validate this step's execution using structured output
				validationResponse, err = validationAgent.(*HumanControlledTodoPlannerValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llmtypes.MessageContent{})
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Step %d validation failed (attempt %d): %v", i+1, retryAttempt, err)
					if retryAttempt >= maxRetryAttempts {
						break // Exit retry loop - will proceed to human feedback with nil validationResponse
					}
					continue // Retry on next attempt
				}

				hcpo.GetLogger().Infof("✅ Step %d validation completed successfully (attempt %d)", i+1, retryAttempt)
				hcpo.GetLogger().Infof("📊 Validation result: Success Criteria Met: %v, Status: %s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)

				// FAST MODE: Skip learning agents entirely
				isFastExecuteStep := hcpo.IsFastExecuteStep(i)
				if isFastExecuteStep {
					hcpo.GetLogger().Infof("⚡ Fast mode: Skipping learning agents for step %d", i+1)
				} else {
					// Run appropriate learning phase based on validation result
					if validationResponse.IsSuccessCriteriaMet {
						// Success Learning Agent - analyze what worked well and update plan.json
						hcpo.GetLogger().Infof("🧠 Running success learning analysis for step %d", i+1)
						successLearningOutput, err := hcpo.runSuccessLearningPhase(ctx, i+1, len(breakdownSteps), &step, executionConversationHistory, validationResponse)
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Success learning phase failed for step %d: %v", i+1, err)
						} else {
							hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", i+1)

							// Append success learning analysis to existing LearningAgentOutput
							if successLearningOutput != "" {
								existingOutput := templateVars["LearningAgentOutput"]
								if existingOutput != "" {
									templateVars["LearningAgentOutput"] = existingOutput + "\n\n" + successLearningOutput
								} else {
									templateVars["LearningAgentOutput"] = successLearningOutput
								}
							}
						}
					} else {
						// Failure Learning Agent - analyze what went wrong and provide refined task description
						hcpo.GetLogger().Infof("🧠 Running failure learning analysis for step %d", i+1)
						refinedTaskDescription, learningAnalysis, err := hcpo.runFailureLearningPhase(ctx, i+1, len(breakdownSteps), &step, executionConversationHistory, validationResponse)
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failure learning phase failed for step %d: %v", i+1, err)
						} else {
							hcpo.GetLogger().Infof("✅ Failure learning analysis completed for step %d", i+1)

							// Update step description for retry
							if refinedTaskDescription != "" {
								step.Description = refinedTaskDescription
								templateVars["StepDescription"] = refinedTaskDescription
								hcpo.GetLogger().Infof("🔄 Updated step %d description with refined task for retry", i+1)
							}

							// Update LearningAgentOutput with full learning analysis
							if learningAnalysis != "" {
								existingOutput := templateVars["LearningAgentOutput"]
								if existingOutput != "" {
									templateVars["LearningAgentOutput"] = existingOutput + "\n\n" + learningAnalysis
								} else {
									templateVars["LearningAgentOutput"] = learningAnalysis
								}
							}
						}
					}
				}

				// Check if success criteria was met
				if validationResponse.IsSuccessCriteriaMet {
					hcpo.GetLogger().Infof("✅ Step %d passed validation - success criteria met", i+1)
					break // Exit retry loop and continue to next step
				} else {
					hcpo.GetLogger().Warnf("⚠️ Step %d failed validation - success criteria not met (attempt %d/%d)", i+1, retryAttempt, maxRetryAttempts)

					// Store feedback for next retry attempt
					validationFeedback = validationResponse.Feedback

					if retryAttempt >= maxRetryAttempts {
						hcpo.GetLogger().Errorf("❌ Step %d failed validation after %d attempts", i+1, maxRetryAttempts)
						// Continue to next step even if validation failed
						break
					} else {
						hcpo.GetLogger().Infof("🔄 Retrying step %d execution with validation feedback", i+1)
						// Note: conversation history is preserved from previous attempts for context
					}
				}
			}

			// BLOCKING HUMAN FEEDBACK - Ask user if they want to continue to next step or re-execute current step
			// FAST MODE: Skip human feedback and auto-approve
			isFastExecuteStep := hcpo.IsFastExecuteStep(i)
			var approved bool
			var feedback string

			if isFastExecuteStep {
				hcpo.GetLogger().Infof("⚡ Fast mode: Auto-approving step %d without human feedback", i+1)
				approved = true
				feedback = "" // No feedback in fast mode
			} else {
				// Normal mode: Request human feedback
				var validationSummary string
				if validationResponse != nil {
					validationSummary = fmt.Sprintf("Step %d validation completed. Success Criteria Met: %v, Status: %s", i+1, validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
				} else {
					validationSummary = fmt.Sprintf("Step %d execution failed - no validation response available", i+1)
				}
				var err error
				approved, feedback, err = hcpo.requestHumanFeedback(ctx, i+1, len(breakdownSteps), validationSummary)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %v", err)
					// Default to continue if feedback fails
					approved = true
				}
			}

			// Store human feedback for future steps (even if approved, user might have provided guidance)
			if feedback != "" {
				feedbackEntry := fmt.Sprintf("Step %d/%d Feedback: %s", i+1, len(breakdownSteps), feedback)
				humanFeedbackHistory = append(humanFeedbackHistory, feedbackEntry)
				hcpo.GetLogger().Infof("📝 Stored human feedback for future steps: %s", feedbackEntry)
			}

			if approved {
				// User approved - mark step as completed and exit outer loop
				progress.CompletedStepIndices = append(progress.CompletedStepIndices, i)
				if err := hcpo.saveStepProgress(ctx, progress); err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to save step progress: %v", err)
				} else {
					hcpo.GetLogger().Infof("✅ Step %d/%d marked as completed and saved", i+1, len(breakdownSteps))
				}
				stepCompleted = true
			} else if !isFastExecuteStep {
				// User rejected - ask if they want to re-execute this step with feedback or move to next step
				// Skip this in fast mode (should not happen anyway since we auto-approve)
				shouldReexecute, err := hcpo.requestReexecuteDecision(ctx, i+1, len(breakdownSteps), feedback)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Re-execution decision request failed: %v", err)
					shouldReexecute = false // Default to stop if decision fails
				}

				if shouldReexecute {
					// User wants to re-execute - set feedback and continue outer loop
					hcpo.GetLogger().Infof("🔄 Will re-execute step %d with human feedback: %s", i+1, feedback)
					humanFeedback = feedback
					// Outer loop will continue, adding feedback to conversation history
				} else {
					// User wants to stop execution
					hcpo.GetLogger().Infof("🛑 User requested to stop execution after step %d with feedback: %s", i+1, feedback)
					break // Break out of outer loop (ends all steps)
				}
			}
		} // End of outer loop for step execution
	}

	hcpo.GetLogger().Infof("✅ All steps execution completed")
	return nil, nil
}

// max returns the maximum value in a slice of integers
func max(slice []int) int {
	if len(slice) == 0 {
		return -1
	}
	maxVal := slice[0]
	for _, val := range slice {
		if val > maxVal {
			maxVal = val
		}
	}
	return maxVal
}

// runVariableExtractionPhase extracts variables from objective (with optional human feedback)
func (hcpo *HumanControlledTodoPlannerOrchestrator) runVariableExtractionPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llmtypes.MessageContent) (*VariablesManifest, string, error) {
	hcpo.GetLogger().Infof("🔍 Starting variable extraction from objective (attempt %d)", iteration)

	// Create variable extraction agent
	extractionAgent, err := hcpo.createVariableExtractionAgent(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create variable extraction agent: %w", err)
	}

	// Prepare template variables
	extractionTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Add human feedback to conversation if provided
	if humanFeedback != "" {
		feedbackMessage := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: humanFeedback}},
		}
		conversationHistory = append(conversationHistory, feedbackMessage)
		hcpo.GetLogger().Infof("📝 Added human feedback to variable extraction conversation (attempt %d)", iteration)
	}

	// Execute variable extraction
	_, updatedHistory, err := extractionAgent.Execute(ctx, extractionTemplateVars, conversationHistory)
	if err != nil {
		return nil, "", fmt.Errorf("variable extraction failed: %w", err)
	}

	// Read the generated variables.json file
	variablesPath := fmt.Sprintf("%s/todo_creation_human/variables/variables.json", hcpo.GetWorkspacePath())
	variablesContent, err := hcpo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read variables.json: %w", err)
	}

	// Parse JSON to get manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		return nil, "", fmt.Errorf("failed to parse variables.json: %w", err)
	}

	// Store manifest in orchestrator for future use
	hcpo.variablesManifest = &manifest
	hcpo.templatedObjective = manifest.Objective

	hcpo.GetLogger().Infof("✅ Extracted %d variables from objective (conversation has %d messages)", len(manifest.Variables), len(updatedHistory))
	return &manifest, manifest.Objective, nil
}

// requestVariableApproval requests human approval for extracted variables
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestVariableApproval(ctx context.Context, manifest *VariablesManifest, revisionAttempt int) (bool, string, error) {
	hcpo.GetLogger().Infof("⏸️ Requesting human approval for extracted variables (attempt %d)", revisionAttempt)

	// Format variables for display
	var variablesSummary strings.Builder
	variablesSummary.WriteString(fmt.Sprintf("Extracted %d variables from objective:\n\n", len(manifest.Variables)))

	for _, variable := range manifest.Variables {
		variablesSummary.WriteString(fmt.Sprintf("- **{{%s}}**: %s\n", variable.Name, variable.Description))
		variablesSummary.WriteString(fmt.Sprintf("  - Value: %s\n", variable.Value))
		variablesSummary.WriteString("\n")
	}

	variablesSummary.WriteString(fmt.Sprintf("\n**Templated Objective**:\n%s", manifest.Objective))

	// Generate unique request ID
	requestID := fmt.Sprintf("variable_approval_%d_%d", revisionAttempt, time.Now().UnixNano())

	// Use common human feedback function
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Please review the extracted variables (attempt %d). Are these correct or do you want to provide feedback for refinement?", revisionAttempt),
		variablesSummary.String(),
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

// createVariableExtractionAgent creates the variable extraction agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) createVariableExtractionAgent(ctx context.Context) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"variable-extraction-agent",
		"variable_extraction",
		0, // No step number
		0, // No iteration
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers needed - pure LLM extraction
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewVariableExtractionAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// loadVariableValues loads runtime variable values from variables.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadVariableValues(ctx context.Context) error {
	if hcpo.variablesManifest == nil {
		return nil // No variables to load
	}

	// Load variable values from variables.json
	variablesPath := fmt.Sprintf("%s/todo_creation_human/variables/variables.json", hcpo.GetWorkspacePath())
	variablesContent, err := hcpo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		return fmt.Errorf("failed to read variables.json: %w", err)
	}

	// Parse variables.json to get current values
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		return fmt.Errorf("failed to parse variables.json: %w", err)
	}

	// Load values into the variableValues map
	hcpo.variableValues = make(map[string]string)
	for _, variable := range manifest.Variables {
		hcpo.variableValues[variable.Name] = variable.Value
	}

	hcpo.GetLogger().Infof("✅ Loaded variable values from variables.json: %d variables", len(hcpo.variableValues))
	return nil
}

// resolveVariables replaces {{VARIABLE}} placeholders with actual values
func (hcpo *HumanControlledTodoPlannerOrchestrator) resolveVariables(text string) string {
	if hcpo.variableValues == nil {
		return text // No variables to resolve
	}

	resolved := text
	for varName, varValue := range hcpo.variableValues {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		resolved = strings.ReplaceAll(resolved, placeholder, varValue)
	}
	return resolved
}

// formatVariableNames formats the variables manifest into a human-readable string for agent prompts
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatVariableNames() string {
	if hcpo.variablesManifest == nil || len(hcpo.variablesManifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range hcpo.variablesManifest.Variables {
		builder.WriteString(fmt.Sprintf("- {{%s}} - %s\n", variable.Name, variable.Description))
	}
	return builder.String()
}

// formatVariableValues formats the variables manifest with their actual values for agent prompts
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatVariableValues() string {
	if hcpo.variablesManifest == nil || len(hcpo.variablesManifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range hcpo.variablesManifest.Variables {
		// Get the actual resolved value from variableValues map if available
		actualValue := variable.Value
		if hcpo.variableValues != nil {
			if resolvedValue, exists := hcpo.variableValues[variable.Name]; exists {
				actualValue = resolvedValue
			}
		}
		builder.WriteString(fmt.Sprintf("- {{%s}} = %s - %s\n", variable.Name, actualValue, variable.Description))
	}
	return builder.String()
}

// runSuccessLearningPhase analyzes successful executions to capture best practices and improve plan.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) runSuccessLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse) (string, error) {
	hcpo.GetLogger().Infof("🧠 Starting success learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Use stored learning detail level preference (set once before execution starts)
	learningDetailLevel := hcpo.GetLearningDetailLevel()
	if learningDetailLevel == "" {
		hcpo.GetLogger().Warnf("⚠️ Learning detail level not set, defaulting to 'general'")
		learningDetailLevel = "general"
	}

	// Create success learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := hcpo.resolveVariables(step.Title)
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	successLearningAgentName := fmt.Sprintf("success-learning-agent-step-%d-%s-%s", stepNumber, strings.ReplaceAll(resolvedTitle, " ", "-"), learningMode)
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", stepNumber, 1, successLearningAgentName)
	if err != nil {
		return "", fmt.Errorf("failed to create success learning agent: %w", err)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for success learning agent
	successLearningTemplateVars := map[string]string{
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"StepWhyThisStep":     step.WhyThisStep,
		"StepContextOutput":   step.ContextOutput,
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExecutionHistory":    shared.FormatConversationHistory(executionHistory),
		"ValidationResult":    string(validationResultJSON),
		"CurrentObjective":    hcpo.GetObjective(),
		"LearningDetailLevel": learningDetailLevel, // Pass learning detail preference
	}

	// Add context dependencies as a comma-separated string
	if len(step.ContextDependencies) > 0 {
		successLearningTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
	} else {
		successLearningTemplateVars["StepContextDependencies"] = ""
	}

	// Add variable names if available
	if variableNames := hcpo.formatVariableNames(); variableNames != "" {
		successLearningTemplateVars["VariableNames"] = variableNames
	}

	// Execute success learning agent and capture output
	successLearningOutput, _, err := successLearningAgent.Execute(ctx, successLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return "", fmt.Errorf("success learning analysis failed: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d (detail level: %s)", stepNumber, learningDetailLevel)
	return successLearningOutput, nil
}

// runFailureLearningPhase analyzes failed executions to provide refined task descriptions for retry
func (hcpo *HumanControlledTodoPlannerOrchestrator) runFailureLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse) (string, string, error) {
	hcpo.GetLogger().Infof("🧠 Starting failure learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Use stored learning detail level preference (set once before execution starts)
	learningDetailLevel := hcpo.GetLearningDetailLevel()
	if learningDetailLevel == "" {
		hcpo.GetLogger().Warnf("⚠️ Learning detail level not set, defaulting to 'general'")
		learningDetailLevel = "general"
	}

	// Create failure learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := hcpo.resolveVariables(step.Title)
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	failureLearningAgentName := fmt.Sprintf("failure-learning-agent-step-%d-%s-%s", stepNumber, strings.ReplaceAll(resolvedTitle, " ", "-"), learningMode)
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", stepNumber, 1, failureLearningAgentName)
	if err != nil {
		return "", "", fmt.Errorf("failed to create failure learning agent: %w", err)
	}

	// Format validation result for template
	validationResultJSON, err := json.MarshalIndent(validationResponse, "", "  ")
	if err != nil {
		validationResultJSON = []byte(fmt.Sprintf("Validation failed to marshal: %v", err))
	}

	// Prepare template variables for failure learning agent
	failureLearningTemplateVars := map[string]string{
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"StepWhyThisStep":     step.WhyThisStep,
		"StepContextOutput":   step.ContextOutput,
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExecutionHistory":    shared.FormatConversationHistory(executionHistory),
		"ValidationResult":    string(validationResultJSON),
		"CurrentObjective":    hcpo.GetObjective(),
		"LearningDetailLevel": learningDetailLevel, // Pass learning detail preference
	}

	// Add context dependencies as a comma-separated string
	if len(step.ContextDependencies) > 0 {
		failureLearningTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
	} else {
		failureLearningTemplateVars["StepContextDependencies"] = ""
	}

	// Add variable names if available
	if variableNames := hcpo.formatVariableNames(); variableNames != "" {
		failureLearningTemplateVars["VariableNames"] = variableNames
	}

	// Execute failure learning agent and capture output
	failureLearningOutput, _, err := failureLearningAgent.Execute(ctx, failureLearningTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return "", "", fmt.Errorf("failure learning analysis failed: %w", err)
	}

	// Extract refined task description from the output
	refinedTaskDescription := hcpo.extractRefinedTaskDescription(failureLearningOutput)
	learningAnalysis := failureLearningOutput // Use the full output as learning analysis

	hcpo.GetLogger().Infof("✅ Failure learning analysis completed for step %d (detail level: %s)", stepNumber, learningDetailLevel)
	return refinedTaskDescription, learningAnalysis, nil
}

// extractRefinedTaskDescription extracts the refined task description from learning agent output
func (hcpo *HumanControlledTodoPlannerOrchestrator) extractRefinedTaskDescription(learningOutput string) string {
	// Look for "### Refined Task:" section in the output
	lines := strings.Split(learningOutput, "\n")
	inRefinedTaskSection := false
	var refinedTaskLines []string

	for _, line := range lines {
		if strings.Contains(line, "### Refined Task:") {
			inRefinedTaskSection = true
			continue
		}
		if inRefinedTaskSection {
			// Stop when we hit the next section (starts with ###)
			if strings.HasPrefix(strings.TrimSpace(line), "###") && !strings.Contains(line, "Refined Task") {
				break
			}
			// Skip empty lines at the start
			if len(refinedTaskLines) == 0 && strings.TrimSpace(line) == "" {
				continue
			}
			refinedTaskLines = append(refinedTaskLines, line)
		}
	}

	refinedTask := strings.TrimSpace(strings.Join(refinedTaskLines, "\n"))
	if refinedTask == "" {
		// Fallback: return the original step description if no refined task found
		return ""
	}

	return refinedTask
}

// runWriterPhaseWithHumanReview creates todo list with human review and feedback loop
func (hcpo *HumanControlledTodoPlannerOrchestrator) runWriterPhaseWithHumanReview(ctx context.Context, iteration int) error {
	maxRevisions := 3 // Allow up to 3 revisions based on critique feedback
	var writerConversationHistory []llmtypes.MessageContent

	for revisionAttempt := 1; revisionAttempt <= maxRevisions; revisionAttempt++ {
		hcpo.GetLogger().Infof("📝 Writer revision attempt %d/%d", revisionAttempt, maxRevisions)

		// Create writer agent for this revision
		writerAgentName := fmt.Sprintf("writer-agent-revision-%d", revisionAttempt)
		writerAgent, err := hcpo.createWriterAgent(ctx, "writing", 0, iteration, writerAgentName)
		if err != nil {
			return fmt.Errorf("failed to create writer agent for revision %d: %w", revisionAttempt, err)
		}

		// Prepare template variables for Execute method
		writerTemplateVars := map[string]string{
			"Objective":       hcpo.GetObjective(),
			"WorkspacePath":   hcpo.GetWorkspacePath(),
			"TotalIterations": fmt.Sprintf("%d", iteration),
		}

		// Add variable names if available
		if variableNames := hcpo.formatVariableNames(); variableNames != "" {
			writerTemplateVars["VariableNames"] = variableNames
		}

		// Execute writer agent with conversation history
		_, writerConversationHistory, err = writerAgent.Execute(ctx, writerTemplateVars, writerConversationHistory)
		if err != nil {
			return fmt.Errorf("todo list creation failed for revision %d: %w", revisionAttempt, err)
		}

		hcpo.GetLogger().Infof("✅ Writer agent completed revision %d", revisionAttempt)

		// Run critique phase to validate quality
		critiqueAgentName := fmt.Sprintf("critique-agent-revision-%d", revisionAttempt)
		critiqueAgent, err := hcpo.createCritiqueAgent(ctx, "critique", 0, iteration, critiqueAgentName)
		if err != nil {
			return fmt.Errorf("failed to create critique agent for revision %d: %w", revisionAttempt, err)
		}

		// Prepare template variables for critique
		critiqueTemplateVars := map[string]string{
			"WorkspacePath": hcpo.GetWorkspacePath(),
		}

		// Add variable names if available
		if variableNames := hcpo.formatVariableNames(); variableNames != "" {
			critiqueTemplateVars["VariableNames"] = variableNames
		}

		// Execute critique agent with structured output
		critiqueAgentTyped, ok := critiqueAgent.(*HumanControlledTodoPlannerCritiqueAgent)
		if !ok {
			return fmt.Errorf("failed to cast critique agent to structured agent")
		}

		critiqueResponse, err := critiqueAgentTyped.ExecuteStructured(ctx, critiqueTemplateVars, nil)
		if err != nil {
			return fmt.Errorf("structured critique execution failed for revision %d: %w", revisionAttempt, err)
		}

		hcpo.GetLogger().Infof("✅ Critique completed for revision %d", revisionAttempt)
		hcpo.GetLogger().Infof("📊 Quality Acceptable: %v, Issues Found: %d", critiqueResponse.IsQualityAcceptable, len(critiqueResponse.Feedback))

		// Check if quality is acceptable
		if critiqueResponse.IsQualityAcceptable {
			hcpo.GetLogger().Infof("✅ Todo list quality is acceptable after revision %d", revisionAttempt)
			break // Exit revision loop
		}

		// Quality not acceptable - prepare feedback for next revision
		if len(critiqueResponse.Feedback) > 0 {
			hcpo.GetLogger().Warnf("⚠️ Quality issues found, preparing feedback for revision %d", revisionAttempt+1)
			// Format feedback as conversation history item
			feedbackText := "## Critique Feedback - Please Address These Issues:\n\n"
			for i, issue := range critiqueResponse.Feedback {
				feedbackText += fmt.Sprintf("%d. **%s**: %s\n", i+1, issue.Type, issue.Description)
			}
			hcpo.addUserFeedbackToHistory(feedbackText, &writerConversationHistory)
		}

		if revisionAttempt >= maxRevisions {
			hcpo.GetLogger().Warnf("⚠️ Max todo list revision attempts (%d) reached", maxRevisions)
			break
		}
	}

	return nil
}

// requestHumanFeedback requests human feedback after validation and blocks until user responds
// Returns: (approved bool, feedback string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestHumanFeedback(ctx context.Context, currentStep, totalSteps int, validationResult string) (bool, string, error) {
	hcpo.GetLogger().Infof("🤔 Requesting human feedback for step %d/%d", currentStep, totalSteps)

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

// requestReexecuteDecision asks user if they want to re-execute the current step with feedback or skip to next step
// Returns: (shouldReexecute bool, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestReexecuteDecision(ctx context.Context, currentStep, totalSteps int, feedback string) (bool, error) {
	hcpo.GetLogger().Infof("🔄 Requesting re-execution decision for step %d/%d", currentStep, totalSteps)

	// Generate unique request ID
	requestID := fmt.Sprintf("reexecute_decision_%d_%d_%d", currentStep, totalSteps, time.Now().UnixNano())

	// Use common human feedback function with yes/no semantics
	return hcpo.RequestYesNoFeedback(
		ctx,
		requestID,
		fmt.Sprintf("You provided feedback for step %d/%d. Would you like to re-execute this step with your feedback, or skip to the next step?", currentStep, totalSteps),
		"Re-execute Step with Feedback",
		"Skip to Next Step",
		fmt.Sprintf("Your feedback: %s", feedback),
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

// requestLearningDetailLevel asks user to choose the level of detail for learning analysis
// Returns: ("exact" for exact MCP tools with args, "general" for general patterns, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestLearningDetailLevel(ctx context.Context, stepNumber, totalSteps int, stepTitle string, isSuccess bool) (string, error) {
	learningType := "failure"
	if isSuccess {
		learningType = "success"
	}

	if stepNumber == 0 {
		hcpo.GetLogger().Infof("🤔 Requesting learning detail level preference for all %d steps", totalSteps)
	} else {
		hcpo.GetLogger().Infof("🤔 Requesting learning detail level preference for %s learning (step %d/%d)", learningType, stepNumber, totalSteps)
	}

	// Generate unique request ID
	requestID := fmt.Sprintf("learning_detail_level_%s_%d_%d_%d", learningType, stepNumber, totalSteps, time.Now().UnixNano())

	// Create context message
	var contextMsg string
	var question string
	if stepNumber == 0 {
		// Asking for all steps
		contextMsg = fmt.Sprintf("%s\n\n**Choose the level of detail for learning analysis (applies to all %d steps):**\n", stepTitle, totalSteps)
		contextMsg += "\n- **Exact MCP Tools**: Extract exact tool calls with complete argument JSON"
		contextMsg += "\n- **General Patterns**: Extract high-level approaches and paths to success"
		question = "How detailed should the learning analysis be for all steps?"
	} else {
		// Asking for specific step
		contextMsg = fmt.Sprintf("Step %d/%d: %s\n\nLearning Type: %s learning analysis", stepNumber, totalSteps, stepTitle, learningType)
		contextMsg += "\n\n**Choose the level of detail for learning analysis:**\n"
		contextMsg += "\n- **Exact MCP Tools**: Extract exact tool calls with complete argument JSON"
		contextMsg += "\n- **General Patterns**: Extract high-level approaches and paths to success"
		question = fmt.Sprintf("How detailed should the %s learning analysis be for step %d?", learningType, stepNumber)
	}

	// Use three-choice feedback with only two options (option3 will be empty but that's ok)
	choice, err := hcpo.RequestThreeChoiceFeedback(
		ctx,
		requestID,
		question,
		"Exact MCP Tools",
		"General Patterns",
		"", // Empty third option
		contextMsg,
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)

	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Learning detail level request failed: %v, defaulting to 'general'", err)
		return "general", nil // Default to general if request fails
	}

	// Map response to our internal values
	if choice == "option1" {
		hcpo.GetLogger().Infof("✅ User selected: Exact MCP Tools")
		return "exact", nil
	} else if choice == "option2" {
		hcpo.GetLogger().Infof("✅ User selected: General Patterns")
		return "general", nil
	}

	// Default to general if unclear
	hcpo.GetLogger().Warnf("⚠️ Unexpected choice: %s, defaulting to 'general'", choice)
	return "general", nil
}

// Agent creation methods - reuse from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServersAndSystemPrompt(
		ctx,
		"human-controlled-planning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // Planning agent only works with plan.md file, no MCP servers needed
		planningSystemPromptProcessor, // System prompt with detailed instructions
		func(templateVars map[string]string) string {
			// User message processor - use human feedback if provided, otherwise generic message
			if humanFeedback, ok := templateVars["HumanFeedback"]; ok && humanFeedback != "" {
				// Pass human feedback directly as user message (e.g., from "Update Existing Plan")
				return humanFeedback
			}
			// Default message for initial plan creation
			return "Create or update plan.md with a structured plan to execute the objective."
		},
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerPlanningAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerExecutionAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createValidationAgent creates a validation agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerValidationAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (hcpo *HumanControlledTodoPlannerOrchestrator) createWriterAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerWriterAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createCritiqueAgent creates a critique agent for validating todo list quality
func (hcpo *HumanControlledTodoPlannerOrchestrator) createCritiqueAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerCritiqueAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createPlanReaderAgent creates a plan reader agent for converting markdown to JSON
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanReaderAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"plan-reader-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // Special MCP identifier for no servers - plan reader only converts markdown to JSON
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledPlanReaderAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createSuccessLearningAgent creates a success learning agent for analyzing successful executions
func (hcpo *HumanControlledTodoPlannerOrchestrator) createSuccessLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerSuccessLearningAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createFailureLearningAgent creates a failure learning agent for analyzing failed executions
func (hcpo *HumanControlledTodoPlannerOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgent(
		ctx,
		agentName,
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerFailureLearningAgent(config, logger, tracer, eventBridge)
		},
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// emitTodoStepsExtractedEvent emits an event when todo steps are extracted from a plan
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitTodoStepsExtractedEvent(ctx context.Context, extractedSteps []TodoStep, planSource string) {
	if hcpo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &TodoStepsExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalStepsExtracted: len(extractedSteps),
		ExtractedSteps:      extractedSteps,
		ExtractionMethod:    "structured_breakdown_agent",
		PlanSource:          planSource,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.TodoStepsExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Debug: Log the event data before emission
	hcpo.GetLogger().Infof("🔍 DEBUG: Event data before emission: %+v", eventData)
	hcpo.GetLogger().Infof("🔍 DEBUG: Unified event before emission: %+v", unifiedEvent)

	// Emit through the context-aware bridge
	bridge := hcpo.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to emit todo steps extracted event: %v", err)
	} else {
		hcpo.GetLogger().Infof("✅ Emitted todo steps extracted event: %d steps extracted", len(extractedSteps))
	}
}

// Execute implements the Orchestrator interface
func (hcpo *HumanControlledTodoPlannerOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate that no options are provided since this orchestrator doesn't use them
	if len(options) > 0 {
		return "", fmt.Errorf("human-controlled todo planner orchestrator does not accept options")
	}

	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
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

// GetLearningDetailLevel returns the stored learning detail level preference
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetLearningDetailLevel() string {
	if hcpo.learningDetailLevel == "" {
		return "general" // Default
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

// checkExistingPlan checks if a plan file already exists in the workspace and returns the plan content if found
// Uses the generic ReadWorkspaceFile function from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkExistingPlan(ctx context.Context, planPath string) (bool, string, error) {
	hcpo.GetLogger().Infof("🔍 Checking for existing plan at %s", planPath)

	// Use the generic ReadWorkspaceFile function from base orchestrator
	planContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📋 No existing plan found: %v", err)
			return false, "", nil
		}
		// Other errors should be returned
		return false, "", err
	}

	hcpo.GetLogger().Infof("✅ Found existing plan at %s", planPath)
	return true, planContent, nil
}

// checkExistingVariables checks if variables.json already exists and loads it
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkExistingVariables(ctx context.Context, variablesPath string) (bool, *VariablesManifest, error) {
	hcpo.GetLogger().Infof("🔍 Checking for existing variables at %s", variablesPath)

	// Try to read variables.json
	variablesContent, err := hcpo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		// Check if it's a "file not found" error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📋 No existing variables found: %v", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, err
	}

	// Parse the existing variables manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to parse existing variables.json: %v", err)
		return false, nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Found existing variables.json with %d variables", len(manifest.Variables))
	return true, &manifest, nil
}

// cleanupExistingPlanArtifacts deletes existing plan.md, steps_done.json, and all files in learnings/, execution/, and validation/ directories
// This is called when user chooses to create a new plan instead of using existing one
func (hcpo *HumanControlledTodoPlannerOrchestrator) cleanupExistingPlanArtifacts(ctx context.Context, workspacePath string) error {
	hcpo.GetLogger().Infof("🧹 Starting cleanup of existing plan artifacts")

	basePath := fmt.Sprintf("%s/todo_creation_human", workspacePath)

	// 1. Delete plan.md file
	planPath := fmt.Sprintf("%s/planning/plan.md", basePath)
	if err := hcpo.DeleteWorkspaceFile(ctx, planPath); err != nil {
		// Ignore "file not found" errors, but log others
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Warnf("⚠️ Failed to delete plan.md: %v", err)
		}
	} else {
		hcpo.GetLogger().Infof("🗑️ Deleted plan.md: %s", planPath)
	}

	// 2. Delete all files in validation/ directory
	validationDir := fmt.Sprintf("%s/validation", basePath)
	if err := hcpo.CleanupDirectory(ctx, validationDir, "validation"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup validation directory: %v", err)
	}

	// 3. Delete all files in learnings/ directory
	learningsDir := fmt.Sprintf("%s/learnings", basePath)
	if err := hcpo.CleanupDirectory(ctx, learningsDir, "learnings"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup learnings directory: %v", err)
	}

	// 4. Delete all files in execution/ directory
	executionDir := fmt.Sprintf("%s/execution", basePath)
	if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %v", err)
	}

	// 5. Delete steps_done.json progress file
	if err := hcpo.deleteStepProgress(ctx); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to delete steps_done.json: %v", err)
	}

	hcpo.GetLogger().Infof("✅ Cleanup of existing plan artifacts completed")
	return nil
}

// addUserFeedbackToHistory adds human feedback to conversation history
func (hcpo *HumanControlledTodoPlannerOrchestrator) addUserFeedbackToHistory(feedback string, conversationHistory *[]llmtypes.MessageContent) {
	feedbackMessage := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: feedback}},
	}
	*conversationHistory = append(*conversationHistory, feedbackMessage)
}

// conversation history formatting moved to shared.FormatConversationHistory

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
