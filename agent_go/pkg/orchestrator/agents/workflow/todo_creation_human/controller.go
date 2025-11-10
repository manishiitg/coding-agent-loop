package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	SuccessPatterns     []string `json:"success_patterns,omitempty"` // what worked (includes tools)
	FailurePatterns     []string `json:"failure_patterns,omitempty"` // what failed (includes tools to avoid)
	HasLoop             bool     `json:"has_loop"`                   // true if step needs to loop
	LoopCondition       string   `json:"loop_condition"`             // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations       int      `json:"max_iterations,omitempty"`   // max iterations (default: 10)
	LoopDescription     string   `json:"loop_description,omitempty"` // human-readable explanation
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
	return fmt.Sprintf("%s/steps_done.json", hcpo.GetWorkspacePath())
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

// initializeFreshProgress creates a new steps_done.json with the new total steps and empty completed indices
func (hcpo *HumanControlledTodoPlannerOrchestrator) initializeFreshProgress(ctx context.Context, newTotalSteps int) error {
	freshProgress := &StepProgress{
		CompletedStepIndices: []int{},
		TotalSteps:           newTotalSteps,
		LastUpdated:          time.Now(),
	}

	if err := hcpo.saveStepProgress(ctx, freshProgress); err != nil {
		return fmt.Errorf("failed to initialize fresh progress: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Initialized fresh progress with %d total steps", newTotalSteps)
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
	// WorkspacePath includes /todo_creation_human subdirectory
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(fmt.Sprintf("%s/todo_creation_human", workspacePath))

	// PHASE 0: Variable Extraction with Human Verification (NEW)
	// Check if variables.json already exists
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := hcpo.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing variables: %w", err)
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
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing variables: %w", err)
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
			hcpo.GetLogger().Warnf("⚠️ Failed to load variable values: %w", err)
		}

		// Switch to templated objective for all subsequent phases
		hcpo.SetObjective(templatedObjective)
		hcpo.GetLogger().Infof("✅ Using templated objective with {{VARIABLES}}: %s", templatedObjective)
	}

	// Request learning detail level preference ONCE after variables are approved
	// This preference will be used for learning integration phase and all learning phases (both success and failure)
	// We ask here (after variables) so it's available for both planning integration and execution learning
	// Note: We'll use the plan step count if available, otherwise we'll ask with a placeholder
	// The actual step count will be known after planning, but we ask early to avoid interrupting the flow later
	hcpo.GetLogger().Infof("🤔 Requesting learning detail level preference (will be used for all learning phases)")
	learningDetailLevel, err := hcpo.requestLearningDetailLevel(ctx, 0, 0, "All steps (count will be determined during planning)", false)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to get learning detail level preference: %v, defaulting to 'general'", err)
		hcpo.learningDetailLevel = "general"
	} else {
		hcpo.learningDetailLevel = learningDetailLevel
		hcpo.GetLogger().Infof("📝 Learning detail level set to '%s' for learning integration and all learning phases", learningDetailLevel)
	}

	// Check if plan.json already exists (workspacePath now includes /todo_creation_human)
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing plan: %w", err)
		// Continue with normal planning flow
		planExists = false
	}

	var breakdownSteps []TodoStep
	var initialPlanningFeedback string               // Store feedback for plan updates
	var approvedPlan *PlanningResponse               // Store approved plan for learning integration
	var existingPlanForFirstUpdate *PlanningResponse // Store existing plan for option3 (Update Existing Plan)

	if planExists {
		hcpo.GetLogger().Infof("📋 Found existing plan.json at %s", planPath)

		// Safety check: Ensure plan has steps
		if len(existingPlan.Steps) == 0 {
			hcpo.GetLogger().Errorf("❌ Existing plan has no steps")
			return "", fmt.Errorf("existing plan has no steps")
		}

		// Convert existing plan to TodoStep format and emit event BEFORE asking user choice
		// This allows user to see the plan before making a decision
		breakdownSteps = hcpo.convertPlanStepsToTodoSteps(existingPlan.Steps)
		hcpo.GetLogger().Infof("✅ Converted existing plan: %d steps extracted", len(breakdownSteps))
		hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")

		// Request human decision: use existing plan, create new plan, or update existing plan
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
			// Default to using existing plan
			planChoice = "option0"
		}

		switch planChoice {
		case "option0":
			// Use existing plan - directly use the parsed JSON (no approval needed since user explicitly chose to use it)
			// Note: breakdownSteps and event emission already done before the switch statement
			hcpo.GetLogger().Infof("✅ User chose to use existing plan, proceeding directly to learning integration")

			// Proceed directly to learning integration then execution (no approval needed)
			hcpo.GetLogger().Infof("✅ Existing plan ready for learning integration: %d steps", len(breakdownSteps))
			// Store approved plan for learning integration - this will skip the planning phase below
			approvedPlan = existingPlan
			// Keep planExists = true to prevent planning phase from running
			// The planning phase is skipped when approvedPlan is already set

		case "option1":
			// Create new plan - cleanup everything and create fresh plan
			hcpo.GetLogger().Infof("🔄 User chose to create new plan, cleaning up existing plan and related files")
			// Clean up existing plan and all related execution artifacts
			if err := hcpo.cleanupExistingPlanArtifacts(ctx, workspacePath); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to cleanup existing plan artifacts: %v (will continue anyway)", err)
			} else {
				hcpo.GetLogger().Infof("🗑️ Successfully cleaned up existing plan artifacts")
			}
			planExists = false

		case "option2":
			// Update existing plan - create new plan but keep artifacts (no cleanup)
			hcpo.GetLogger().Infof("🔄 User chose to update existing plan, creating new plan but keeping existing artifacts")

			// Delete plan_learnings.json cache since plan structure will change
			if err := hcpo.deleteEnhancedPlanCache(ctx); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to delete plan_learnings.json cache: %v (will continue anyway)", err)
			}

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
			// Store existing plan to pass to planning phase for updating
			existingPlanForFirstUpdate = existingPlan

		default:
			// Unknown choice - default to using existing plan
			hcpo.GetLogger().Warnf("⚠️ Unknown plan choice: %s, defaulting to use existing plan", planChoice)
			// planExists remains true - will use existing plan
			// Store approved plan to skip planning phase
			approvedPlan = existingPlan
		}
	}

	// Only run planning phase if:
	// 1. No existing plan exists (planExists = false), OR
	// 2. User wants to create/update plan (approvedPlan is nil - meaning option1 was not selected)
	if !planExists && approvedPlan == nil {
		hcpo.GetLogger().Infof("🔄 No existing plan found, creating new plan to execute objective")

		// NOTE: Don't delete existing progress here - only delete when actually starting new execution
		// This prevents losing progress if planning fails or if user chooses to use existing plan

		// Phase 1: Planning → Approval loop
		maxPlanRevisions := 20 // Allow up to 20 plan revisions
		// Initialize with initial planning feedback (e.g., from "Update Existing Plan" option)
		humanFeedback := initialPlanningFeedback
		var planningConversationHistory []llmtypes.MessageContent
		// Note: approvedPlan is already declared in outer scope (line 327)
		// Do NOT redeclare it here to avoid shadowing
		var err error

		for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Plan creation/approval attempt %d/%d", revisionAttempt, maxPlanRevisions)

			// Phase 1: Generate JSON plan directly (with optional human feedback)
			// Pass existing plan if this is a revision (to update it) OR if this is the first attempt with an existing plan (option3)
			var existingPlanForUpdate *PlanningResponse
			if revisionAttempt == 1 && existingPlanForFirstUpdate != nil {
				// First attempt with existing plan (option3: Update Existing Plan)
				existingPlanForUpdate = existingPlanForFirstUpdate
			} else if revisionAttempt > 1 && approvedPlan != nil {
				// Subsequent revision attempts (user rejected and provided feedback)
				existingPlanForUpdate = approvedPlan
			}
			approvedPlan, planningConversationHistory, err = hcpo.runPlanningPhase(ctx, revisionAttempt, humanFeedback, planningConversationHistory, existingPlanForUpdate)
			if err != nil {
				return "", fmt.Errorf("planning phase failed: %w", err)
			}

			// Safety check: Ensure plan has steps
			if len(approvedPlan.Steps) == 0 {
				return "", fmt.Errorf("new plan has no steps: planning agent returned empty steps array")
			}

			// Convert approved plan steps to TodoStep format for execution
			breakdownSteps = hcpo.convertPlanStepsToTodoSteps(approvedPlan.Steps)
			hcpo.GetLogger().Infof("✅ Converted new plan: %d steps extracted", len(breakdownSteps))

			// Emit todo steps extracted event
			hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "new_plan")

			// Request human approval for JSON plan (after event emission)
			approvedInternal, feedbackInternal, err := hcpo.requestPlanApproval(ctx, revisionAttempt)
			if err != nil {
				return "", fmt.Errorf("plan approval request failed: %w", err)
			}

			if approvedInternal {
				hcpo.GetLogger().Infof("✅ JSON plan approved by human, proceeding to learning integration with %d steps", len(breakdownSteps))
				break // Exit retry loop and continue to learning integration
			}

			// Plan rejected with feedback for revision
			hcpo.GetLogger().Infof("🔄 Plan revision requested (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedbackInternal)
			humanFeedback = feedbackInternal // Store feedback for next iteration

			if revisionAttempt >= maxPlanRevisions {
				return "", fmt.Errorf("max plan revision<|uniquepaddingtoken122|> attempts (%d) reached", maxPlanRevisions)
			}
		}

		// Plan approved, proceed to learning integration then execution
		// approvedPlan is already set in the loop above
	}

	// PHASE 1.5: Learning Integration - Enhance plan with success/failure patterns from learnings/
	// This runs after planning is approved but before execution
	if approvedPlan != nil && len(approvedPlan.Steps) > 0 {
		hcpo.GetLogger().Infof("🧠 Running learning integration phase to enhance plan with patterns")
		enhancedPlan, err := hcpo.runLearningIntegrationPhase(ctx, approvedPlan)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Learning integration phase failed: %v, proceeding with original plan", err)
			// Continue with original plan if integration fails
		} else {
			hcpo.GetLogger().Infof("✅ Learning integration completed: enhanced plan with patterns")
			// Update breakdownSteps from enhanced plan
			breakdownSteps = hcpo.convertPlanStepsToTodoSteps(enhancedPlan.Steps)
			approvedPlan = enhancedPlan // Update approved plan reference

			// Emit todo steps extracted event with enhanced patterns for frontend display
			hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "enhanced_plan_with_patterns")
		}
	}

	// EARLY PROGRESS CHECK: Check if all steps are already completed before proceeding
	// This prevents running execution unnecessarily if all steps are done
	hcpo.GetLogger().Infof("🔍 Early progress check: Checking if all steps are already completed")
	hcpo.GetLogger().Infof("🔍 DEBUG: breakdownSteps count before early progress check: %d", len(breakdownSteps))

	earlyProgress, err := hcpo.loadStepProgress(ctx)
	planChangeHandled := false // Track if we already handled plan change to avoid duplicate prompts
	if err == nil && earlyProgress != nil && len(earlyProgress.CompletedStepIndices) > 0 {
		hcpo.GetLogger().Infof("📊 Found early progress: %d/%d steps completed",
			len(earlyProgress.CompletedStepIndices), earlyProgress.TotalSteps)

		// Check if total steps match
		if earlyProgress.TotalSteps == len(breakdownSteps) {
			// Calculate if all steps are completed
			if len(earlyProgress.CompletedStepIndices) == earlyProgress.TotalSteps {
				hcpo.GetLogger().Infof("✅ ALL steps already completed - skipping to writer phase")

				// Phase 3: Write/Update todo list
				err = hcpo.runWriterPhaseWithHumanReview(ctx, 1)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Writer phase failed: %w", err)
				}

				// Return early with completion message
				return "Todo planning complete. All steps already executed. Final todo list saved as `todo_final.json`.", nil
			}
			hcpo.GetLogger().Infof("📊 Not all steps completed yet - will proceed with execution")
		} else {
			// Plan changed - ask user what to do (only once)
			hcpo.GetLogger().Warnf("⚠️ Total steps changed (previous: %d, current: %d), prompting user for decision",
				earlyProgress.TotalSteps, len(breakdownSteps))
			choice, err := hcpo.handlePlanChange(ctx, earlyProgress, len(breakdownSteps))
			planChangeHandled = true // Mark that we've already handled plan change
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for plan change: %w, defaulting to start from beginning", err)
				earlyProgress = nil // Default to start fresh
			} else {
				switch choice {
				case "option0": // Keep old progress (try to match)
					hcpo.GetLogger().Infof("✅ User chose to keep old progress (will try to match steps)")
					// Keep earlyProgress as-is, will be handled later
				case "option1": // Delete old progress and start fresh
					hcpo.GetLogger().Infof("🔄 User chose to delete old progress and start fresh")
					// Initialize fresh progress with new total steps
					if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to initialize fresh progress: %w", err)
					}
					// Clean up execution artifacts for fresh start
					executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
					if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory")
					}
					// Note: learnings/ folder is preserved - deleted manually only
					earlyProgress = nil
				default:
					hcpo.GetLogger().Warnf("⚠️ Unknown choice: %s, defaulting to delete old progress", choice)
					// Initialize fresh progress with new total steps
					if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to initialize fresh progress: %w", err)
					}
					// Clean up execution artifacts for fresh start
					executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
					if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory")
					}
					// Note: learnings/ folder is preserved - deleted manually only
					earlyProgress = nil
				}
			}
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
		// Check if there's existing progress (only if we haven't already handled plan change)
		if !planChangeHandled {
			existingProgress, err = hcpo.loadStepProgress(ctx)
			if err != nil {
				// File doesn't exist - this is normal for first run, log and continue
				hcpo.GetLogger().Infof("ℹ️ No existing progress file found (this is normal for first run), will start fresh execution")
				existingProgress = nil
				err = nil // Reset err to allow execution to proceed
			}
		} else {
			// Plan change was already handled, don't reload to avoid duplicate prompts
			hcpo.GetLogger().Infof("ℹ️ Plan change already handled, skipping reload to avoid duplicate prompts")
			existingProgress = nil
			err = nil
		}
	}

	// Process existing progress if available
	if err == nil && existingProgress != nil && len(existingProgress.CompletedStepIndices) > 0 {
		hcpo.GetLogger().Infof("📊 Found existing progress: %d/%d steps completed",
			len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps)

		// Check if total steps match (plan might have changed)
		// Only check if we haven't already handled plan change
		if !planChangeHandled && existingProgress.TotalSteps != len(breakdownSteps) {
			// Plan changed - ask user what to do
			hcpo.GetLogger().Warnf("⚠️ Plan has changed (previous: %d steps, current: %d steps), prompting user for decision",
				existingProgress.TotalSteps, len(breakdownSteps))
			choice, err := hcpo.handlePlanChange(ctx, existingProgress, len(breakdownSteps))
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for plan change: %w, defaulting to start from beginning", err)
				existingProgress = nil // Default to start fresh
			} else {
				switch choice {
				case "option0": // Keep old progress (try to match)
					hcpo.GetLogger().Infof("✅ User chose to keep old progress (will try to match steps)")
					// Keep existingProgress as-is, continue processing below
					// Note: Step matching logic may not work perfectly, but we'll try
				case "option1": // Delete old progress and start fresh
					hcpo.GetLogger().Infof("🔄 User chose to delete old progress and start fresh")
					// Initialize fresh progress with new total steps
					if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to initialize fresh progress: %w", err)
					}
					// Clean up execution artifacts for fresh start
					executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
					if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory")
					}
					// Note: learnings/ folder is preserved - deleted manually only
					existingProgress = nil
				default:
					hcpo.GetLogger().Warnf("⚠️ Unknown choice: %s, defaulting to delete old progress", choice)
					// Initialize fresh progress with new total steps
					if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to initialize fresh progress: %w", err)
					}
					// Clean up execution artifacts for fresh start
					executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
					if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory")
					}
					// Note: learnings/ folder is preserved - deleted manually only
					existingProgress = nil
				}
			}
		}

		// Process existing progress if still available after plan change handling
		if existingProgress != nil {
			// Check if all steps are completed first (using old step count for old progress)
			allStepsCompleted := len(existingProgress.CompletedStepIndices) == existingProgress.TotalSteps

			// Ask user if they want to resume
			nextIncompleteStep := 0
			if !allStepsCompleted {
				// Use the minimum of old and new step counts to avoid index issues
				maxStepsToCheck := existingProgress.TotalSteps
				if maxStepsToCheck > len(breakdownSteps) {
					maxStepsToCheck = len(breakdownSteps)
					hcpo.GetLogger().Infof("⚠️ Old progress has %d steps but new plan has %d steps - limiting check to %d steps",
						existingProgress.TotalSteps, len(breakdownSteps), maxStepsToCheck)
				}
				for i := 0; i < maxStepsToCheck; i++ {
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

				// Phase 3: Write/Update todo list
				err = hcpo.runWriterPhaseWithHumanReview(ctx, 1)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Writer phase failed: %w", err)
				}

				// Return early with completion message
				return "Todo planning complete. All steps already executed. Final todo list saved as `todo_final.json`.", nil
			} else if nextIncompleteStep > 0 {
				// Calculate the last completed step number (1-based) for display
				lastCompletedStepNumber := max(existingProgress.CompletedStepIndices) + 1 // Convert to 1-based

				requestID := fmt.Sprintf("resume_progress_%d", time.Now().UnixNano())
				resumeOptions := []string{
					fmt.Sprintf("Resume from Step %d", nextIncompleteStep),
					"Start from Beginning",
					fmt.Sprintf("Fast Execute (0 to Step %d)", lastCompletedStepNumber),
					"Fast Execute all steps",
					fmt.Sprintf("Fast Resume From Step %d", nextIncompleteStep),
				}
				choice, err := hcpo.RequestMultipleChoiceFeedback(
					ctx,
					requestID,
					fmt.Sprintf("Found existing progress: %d/%d steps completed. How would you like to proceed?",
						len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps),
					resumeOptions,
					fmt.Sprintf("Last updated: %s", existingProgress.LastUpdated.Format("2006-01-02 15:04:05")),
					hcpo.getSessionID(),
					hcpo.getWorkflowID(),
				)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for resuming: %w", err)
					choice = "option0" // Default to resume
				}

				// Track fast execute mode
				fastExecuteMode := false
				fastExecuteEndStep := -1

				switch choice {
				case "option0": // Resume from next incomplete step
					startFromStep = nextIncompleteStep - 1 // Convert back to 0-based
					hcpo.GetLogger().Infof("✅ User chose to resume from step %d", nextIncompleteStep)
				case "option1": // Start from beginning (normal execution)
					hcpo.GetLogger().Infof("🔄 User chose to start from beginning, will reset progress and cleanup execution artifacts")
					// Delete existing progress
					if err := hcpo.deleteStepProgress(ctx); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %w", err)
					}
					// Clean up execution artifacts for fresh start
					executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
					if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory")
					}
					// Clean up validation artifacts
					validationDir := fmt.Sprintf("%s/validation", hcpo.GetWorkspacePath())
					if err := hcpo.CleanupDirectory(ctx, validationDir, "validation"); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup validation directory: %w", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Cleaned up validation directory")
					}
					// Note: learnings/ folder is preserved - deleted manually only
					existingProgress = nil
					startFromStep = 0
				case "option2": // Fast execute completed steps (0 to lastCompletedStepNumber)
					hcpo.GetLogger().Infof("⚡ User chose fast execute mode for completed steps (0 to %d)", lastCompletedStepNumber)

					// Clean up execution artifacts for steps that will be re-executed
					executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
					hcpo.GetLogger().Infof("🔍 DEBUG: About to call CleanupDirectory for fast execute, path: %s", executionDir)
					if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory for fast re-execution")
					}
					hcpo.GetLogger().Infof("🔍 DEBUG: CleanupDirectory call completed for fast execute")

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
				case "option3": // Fast execute all steps
					hcpo.GetLogger().Infof("⚡ User chose fast execute mode for all steps")

					// Clean up execution artifacts for all steps
					executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
					if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory for fast re-execution")
					}

					fastExecuteMode = true
					fastExecuteEndStep = len(breakdownSteps) - 1 // Fast execute all steps
					startFromStep = 0
					// Clear all completed indices to re-execute everything
					existingProgress.CompletedStepIndices = []int{}
					hcpo.GetLogger().Infof("⚡ Will fast execute all steps (0 to %d)", fastExecuteEndStep)
				case "option4": // Fast resume from next incomplete step
					hcpo.GetLogger().Infof("⚡ User chose fast resume mode from step %d", nextIncompleteStep)

					// Note: No cleanup needed - we're just skipping learning/validation/human feedback for ALL steps
					// Fast execute ALL steps (0 to end) - this ensures any step that gets executed runs in fast mode

					fastExecuteMode = true
					// Fast execute ALL steps (0 to last step) - this covers all steps
					// Completed steps will be skipped, but if any step executes, it will be in fast mode
					fastExecuteEndStep = len(breakdownSteps) - 1 // Fast execute ALL steps (0 to end)
					startFromStep = nextIncompleteStep - 1       // Start from next incomplete step (0-based)

					// Keep all completed indices as-is - we're not re-executing completed steps
					// The execution loop will skip completed steps anyway, but fast execute mode will apply
					// to ALL steps (0 to end) if they get executed
					hcpo.GetLogger().Infof("⚡ Will fast execute ALL steps (0 to %d), starting execution from step %d (1-based: %d)", fastExecuteEndStep, startFromStep, nextIncompleteStep)
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
		hcpo.GetLogger().Warnf("⚠️ Writer phase with critique validation failed: %w", err)
	}

	duration := time.Since(hcpo.GetStartTime())
	hcpo.GetLogger().Infof("✅ Human-controlled todo planning completed in %v", duration)

	return "Todo planning complete. Final todo list saved as `todo_final.json`.", nil
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

	planResponse, updatedConversationHistory, err := planningAgentTyped.ExecuteStructured(ctx, planningTemplateVars, conversationHistory, userMessage)
	if err != nil {
		return nil, nil, fmt.Errorf("planning failed: %w", err)
	}

	// Save JSON plan to file manually
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planJSON, err := json.MarshalIndent(planResponse, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal plan to JSON: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, planPath, string(planJSON)); err != nil {
		return nil, nil, fmt.Errorf("failed to save plan.json: %w", err)
	}

	// Delete plan_learnings.json cache since plan has changed
	if err := hcpo.deleteEnhancedPlanCache(ctx); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to delete plan_learnings.json cache after saving new plan: %v (will continue anyway)", err)
	}

	hcpo.GetLogger().Infof("✅ JSON plan created successfully and saved to %s (%d steps, conversation has %d messages)", planPath, len(planResponse.Steps), len(updatedConversationHistory))

	return planResponse, updatedConversationHistory, nil
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
			ContextDependencies: step.ContextDependencies,
			ContextOutput:       step.ContextOutput.String(), // Convert FlexibleContextOutput to string
			SuccessPatterns:     step.SuccessPatterns,        // Include success patterns from learning integration
			FailurePatterns:     step.FailurePatterns,        // Include failure patterns from learning integration
			HasLoop:             step.HasLoop,
			LoopCondition:       step.LoopCondition,
			MaxIterations:       step.MaxIterations,
			LoopDescription:     step.LoopDescription,
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

	// Learning detail level preference should already be set before execution (requested before Phase 1.5)
	// If not set for some reason, default to 'general'
	if hcpo.learningDetailLevel == "" {
		hcpo.GetLogger().Warnf("⚠️ Learning detail level not set, defaulting to 'general'")
		hcpo.learningDetailLevel = "general"
	} else {
		hcpo.GetLogger().Infof("📝 Using learning detail level '%s' (set before execution phase)", hcpo.learningDetailLevel)
	}

	// Track human feedback across all steps for continuous improvement
	var humanFeedbackHistory []string

	// Execute each step one by one
	for i, step := range breakdownSteps {
		// Reset fast execute mode if we've passed the fast execute range
		// This ensures normal execution (with learning and human feedback) for steps after fastExecuteEndStep
		if hcpo.fastExecuteMode && i > hcpo.fastExecuteEndStep {
			hcpo.GetLogger().Infof("🔄 Fast execute mode completed (steps 0-%d), resetting to normal execution mode for step %d+", hcpo.fastExecuteEndStep, i+1)
			hcpo.SetFastExecuteMode(false, -1)
			// Ensure progress is saved when transitioning from fast to normal mode
			// This catches any steps that were completed in fast mode but not yet saved
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to save progress during fast→normal transition: %w", err)
			} else {
				hcpo.GetLogger().Infof("💾 Saved progress during fast→normal mode transition: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps)
			}
		}

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
		var executionConversationHistory []llmtypes.MessageContent // Only used for learning agents after execution
		stepCompleted := false

		// Outer loop: Handle re-execution with human feedback
		for !stepCompleted {

			// Prepare template variables for this specific step with individual fields
			// RESOLVE VARIABLES: Replace {{VARS}} with actual values for execution
			// Execution agent workspace path includes /execution/ subdirectory
			executionWorkspacePath := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
			templateVars := map[string]string{
				"StepNumber":          fmt.Sprintf("%d", i+1),
				"TotalSteps":          fmt.Sprintf("%d", len(breakdownSteps)),
				"StepTitle":           hcpo.resolveVariables(step.Title),
				"StepDescription":     hcpo.resolveVariables(step.Description),
				"StepSuccessCriteria": hcpo.resolveVariables(step.SuccessCriteria),
				"StepContextOutput":   hcpo.resolveVariables(step.ContextOutput),
				"WorkspacePath":       executionWorkspacePath,
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

			// Validate loop condition is provided when has_loop is true
			if step.HasLoop {
				if step.LoopCondition == "" {
					return nil, fmt.Errorf("step %d has has_loop=true but loop_condition is empty (required)", i+1)
				}
				// Set default max_iterations if not provided
				if step.MaxIterations == 0 {
					step.MaxIterations = 10
					hcpo.GetLogger().Infof("⚠️ Step %d has loop but no max_iterations specified, using default: 10", i+1)
				}
			}

			// Inner loop: Automatic retry logic
			var validationResponse *ValidationResponse

			// Loop handling: if step has loop, wrap execution in loop that checks loop condition
			var loopConditionMet bool
			var loopIterationCount int
			// Store previous iteration's execution and validation outputs for loop feedback
			var previousIterationExecutionOutput string
			var previousIterationValidationOutput string

			// Main execution loop (either single execution or loop iterations)
			// For non-loop steps, this executes once. For loop steps, it iterates until condition is met.
			// NOTE: No conversation history is passed to execution agent - all context via template variables
			for loopIteration := 0; ; loopIteration++ {
				// Initialize loop state on first iteration
				if loopIteration == 0 && step.HasLoop {
					loopConditionMet = false
					loopIterationCount = 0
					previousIterationExecutionOutput = ""
					previousIterationValidationOutput = ""
					hcpo.GetLogger().Infof("🔄 Step %d loop starting (max iterations: %d, condition: %s)", i+1, step.MaxIterations, step.LoopCondition)
				} else if loopIteration > 0 && step.HasLoop {
					// Previous iteration outputs are passed via template variables (PreviousIterationOutput)
					// Execution conversation history will be captured fresh from this iteration for learning agents
					hcpo.GetLogger().Infof("🔄 Step %d loop iteration %d/%d starting", i+1, loopIterationCount, step.MaxIterations)
				}

				// Check loop exit conditions (only for loop steps)
				if step.HasLoop {
					if loopConditionMet {
						hcpo.GetLogger().Infof("✅ Step %d loop condition met after %d iterations, exiting loop", i+1, loopIterationCount)
						// Skip validation, mark as completed
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: true,
							ExecutionStatus:      "COMPLETED",
							Reasoning:            fmt.Sprintf("Loop condition met after %d iterations. Validation skipped per loop exit.", loopIterationCount),
						}
						break // Exit main loop - proceed to mark as completed
					}
					if loopIterationCount >= step.MaxIterations {
						hcpo.GetLogger().Errorf("❌ Step %d reached max iterations (%d) without meeting loop condition, requesting human intervention", i+1, step.MaxIterations)
						// Request human intervention immediately, skip validation
						var err error
						var approved bool
						approved, _, err = hcpo.requestHumanFeedback(ctx, i+1, len(breakdownSteps),
							fmt.Sprintf("Loop reached max iterations (%d) without meeting condition: %s", step.MaxIterations, step.LoopCondition))
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %w", err)
							// Default to not approved so step doesn't complete
							approved = false
						}
						if approved {
							// User approved - treat as completed despite max iterations
							hcpo.GetLogger().Infof("✅ User approved step %d despite max iterations, marking as completed", i+1)
							validationResponse = &ValidationResponse{
								IsSuccessCriteriaMet: true,
								ExecutionStatus:      "COMPLETED",
								Reasoning:            "User approved completion despite max iterations reached",
							}
							loopConditionMet = true // Mark condition as met so loop exits
							break                   // Exit main loop
						} else {
							// User rejected - will re-execute step
							hcpo.GetLogger().Infof("🔄 User rejected approval, will re-execute step %d", i+1)
							break // Exit main loop; outer loop will re-execute since stepCompleted is still false
						}
					}
					loopIterationCount++
					hcpo.GetLogger().Infof("🔄 Step %d loop iteration %d/%d", i+1, loopIterationCount, step.MaxIterations)
				}

				// Add loop context to template variables if in loop mode
				if step.HasLoop {
					templateVars["HasLoop"] = "true"
					templateVars["LoopCondition"] = step.LoopCondition
					templateVars["LoopDescription"] = step.LoopDescription
					templateVars["CurrentIteration"] = fmt.Sprintf("%d", loopIterationCount)
					templateVars["MaxIterations"] = fmt.Sprintf("%d", step.MaxIterations)
					// Add previous iteration execution and validation outputs for loop steps (after iteration 1)
					if loopIterationCount > 1 && (previousIterationExecutionOutput != "" || previousIterationValidationOutput != "") {
						var combinedOutput strings.Builder
						if previousIterationExecutionOutput != "" {
							combinedOutput.WriteString("## Previous Loop Iteration Execution Output:\n")
							combinedOutput.WriteString(previousIterationExecutionOutput)
							combinedOutput.WriteString("\n\n")
						}
						if previousIterationValidationOutput != "" {
							combinedOutput.WriteString("## Previous Loop Iteration Validation Output:\n")
							combinedOutput.WriteString(previousIterationValidationOutput)
						}
						templateVars["PreviousIterationOutput"] = combinedOutput.String()
						hcpo.GetLogger().Infof("📝 Added previous iteration outputs to template variables for step %d (loop iteration %d)", i+1, loopIterationCount)
					} else {
						templateVars["PreviousIterationOutput"] = ""
					}
				} else {
					templateVars["HasLoop"] = "false"
					templateVars["LoopCondition"] = ""
					templateVars["LoopDescription"] = ""
					templateVars["CurrentIteration"] = ""
					templateVars["MaxIterations"] = ""
					templateVars["PreviousIterationOutput"] = ""
				}

				for retryAttempt := 1; retryAttempt <= maxRetryAttempts; retryAttempt++ {
					hcpo.GetLogger().Infof("🔄 Executing step %d/%d (attempt %d/%d): %s", i+1, len(breakdownSteps), retryAttempt, maxRetryAttempts, step.Title)

					// Add validation feedback to template variables if this is a retry or loop iteration
					if (retryAttempt > 1 || (step.HasLoop && loopIterationCount > 1)) && validationResponse != nil {
						var contextStr string
						if retryAttempt > 1 {
							contextStr = fmt.Sprintf("Validation Feedback (Retry Attempt %d)", retryAttempt)
						} else if step.HasLoop && loopIterationCount > 1 {
							contextStr = fmt.Sprintf("Validation Feedback (Loop Iteration %d)", loopIterationCount-1)
						} else {
							contextStr = "Validation Feedback"
						}
						templateVars["ValidationFeedback"] = hcpo.formatValidationResponseForTemplate(validationResponse, contextStr)
						hcpo.GetLogger().Infof("📝 Added validation feedback to template variables for step %d (retry: %d, loop iteration: %d)", i+1, retryAttempt, loopIterationCount)
					} else {
						templateVars["ValidationFeedback"] = "" // No validation feedback for first attempt/first iteration
					}

					// Create execution agent for this step
					// Resolve variables in step title before using in agent name
					resolvedTitle := hcpo.resolveVariables(step.Title)
					sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
					agentName := fmt.Sprintf("step-%d-%s", i+1, sanitizedTitle)
					// Add loop iteration to agent name if in loop mode
					if step.HasLoop && loopIterationCount > 0 {
						agentName = fmt.Sprintf("%s-loop-%d", agentName, loopIterationCount)
					}
					executionAgent, err := hcpo.createExecutionAgent(ctx, "execution", i+1, iteration, agentName)
					if err != nil {
						return nil, fmt.Errorf("failed to create execution agent for step %d: %w", i+1, err)
					}

					// Execute this specific step - no conversation history needed, all context in template variables
					// Capture execution result and conversation history (conversation history for learning agents)
					var executionResult string
					executionResult, executionConversationHistory, err = executionAgent.Execute(ctx, templateVars, []llmtypes.MessageContent{})
					if err != nil {
						hcpo.GetLogger().Warnf("⚠️ Step %d execution failed (attempt %d): %v", i+1, retryAttempt, err)
						if retryAttempt >= maxRetryAttempts {
							hcpo.GetLogger().Errorf("❌ Step %d execution failed after %d attempts, exiting retry loop", i+1, maxRetryAttempts)
							break // Exit retry loop - will proceed to human feedback
						}
						continue // Retry on next attempt
					}

					hcpo.GetLogger().Infof("✅ Step %d execution completed successfully (attempt %d)", i+1, retryAttempt)

					// Always validate step execution
					hcpo.GetLogger().Infof("🔍 Validating step %d execution (attempt %d)", i+1, retryAttempt)

					// Reuse sanitized title from execution agent (already computed above)
					validationAgentName := fmt.Sprintf("step-%d-%s", i+1, sanitizedTitle)
					// Add loop iteration to validation agent name if in loop mode
					if step.HasLoop && loopIterationCount > 0 {
						validationAgentName = fmt.Sprintf("%s-loop-%d", validationAgentName, loopIterationCount)
					}
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

					// If in loop mode, pass loop condition to validation agent
					if step.HasLoop {
						validationTemplateVars["LoopCondition"] = step.LoopCondition
						hcpo.GetLogger().Infof("🔍 Checking loop condition for step %d (iteration %d): %s", i+1, loopIterationCount, step.LoopCondition)
					} else {
						validationTemplateVars["LoopCondition"] = ""
					}

					// Validate this step's execution using structured output
					validationResponse, _, err = validationAgent.(*HumanControlledTodoPlannerValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llmtypes.MessageContent{})
					if err != nil {
						hcpo.GetLogger().Warnf("⚠️ Step %d validation failed (attempt %d): %v", i+1, retryAttempt, err)
						if retryAttempt >= maxRetryAttempts {
							break // Exit retry loop - will proceed to human feedback with nil validationResponse
						}
						continue // Retry on next attempt
					}

					hcpo.GetLogger().Infof("✅ Step %d validation completed successfully (attempt %d)", i+1, retryAttempt)
					hcpo.GetLogger().Infof("📊 Validation result: Success Criteria Met: %v, Status: %s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)

					// If in loop mode, check loop condition instead of full validation
					if step.HasLoop {
						// Check loop condition from validation response
						if validationResponse.LoopConditionMet {
							hcpo.GetLogger().Infof("✅ Step %d loop condition met (iteration %d)", i+1, loopIterationCount)
							loopConditionMet = true

							// Run success learning when loop completes successfully (before breaking)
							// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
							isFastExecuteStep := hcpo.IsFastExecuteStep(i)
							isLearningDisabled := hcpo.GetLearningDetailLevel() == "none"
							hcpo.GetLogger().Infof("🔍 DEBUG: Step %d (loop) - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v", i+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, isFastExecuteStep, isLearningDisabled)
							if !isFastExecuteStep && !isLearningDisabled {
								// Success Learning Agent - analyze what worked well and update plan.json
								// Loop condition met means step completed successfully
								hcpo.GetLogger().Infof("🧠 Running success learning analysis for step %d (loop completed)", i+1)
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
								if isFastExecuteStep {
									hcpo.GetLogger().Infof("⚡ Fast mode: Skipping learning agents for step %d", i+1)
								} else if isLearningDisabled {
									hcpo.GetLogger().Infof("⏭️ Learning disabled: Skipping learning agents for step %d", i+1)
								}
							}

							break // Exit retry loop, will exit main loop at top
						} else {
							hcpo.GetLogger().Infof("🔄 Step %d loop condition not met yet (iteration %d/%d), continuing loop", i+1, loopIterationCount, step.MaxIterations)
							// Capture execution result (final response) and validation outputs for next iteration
							previousIterationExecutionOutput = executionResult
							validationOutputParts := []string{}
							if validationResponse.Reasoning != "" {
								validationOutputParts = append(validationOutputParts, fmt.Sprintf("**Reasoning**: %s", validationResponse.Reasoning))
							}
							if validationResponse.LoopReasoning != "" {
								validationOutputParts = append(validationOutputParts, fmt.Sprintf("**Loop Reasoning**: %s", validationResponse.LoopReasoning))
							}
							if len(validationResponse.Feedback) > 0 {
								feedbackParts := []string{"**Feedback**: "}
								for _, fb := range validationResponse.Feedback {
									feedbackParts = append(feedbackParts, fmt.Sprintf("- [%s] %s: %s", fb.Severity, fb.Type, fb.Description))
								}
								validationOutputParts = append(validationOutputParts, strings.Join(feedbackParts, "\n"))
							}
							previousIterationValidationOutput = strings.Join(validationOutputParts, "\n\n")
							hcpo.GetLogger().Infof("📝 Captured execution and validation outputs for iteration %d (will be included in next iteration)", loopIterationCount)
							break // Exit retry loop, continue main loop for next iteration
						}
					}

					// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
					isFastExecuteStep := hcpo.IsFastExecuteStep(i)
					isLearningDisabled := hcpo.GetLearningDetailLevel() == "none"
					hcpo.GetLogger().Infof("🔍 DEBUG: Step %d - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v", i+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, isFastExecuteStep, isLearningDisabled)
					if isFastExecuteStep || isLearningDisabled {
						if isFastExecuteStep {
							hcpo.GetLogger().Infof("⚡ Fast mode: Skipping learning agents for step %d", i+1)
						} else if isLearningDisabled {
							hcpo.GetLogger().Infof("⏭️ Learning disabled: Skipping learning agents for step %d", i+1)
						}
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
							// SKIP failure learning for loop steps - loop steps only run success learning when condition is met
							if step.HasLoop {
								hcpo.GetLogger().Infof("🔄 Step %d is a loop step - skipping failure learning (loop steps only run success learning when condition is met)", i+1)
							} else {
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
					}

					// Check if success criteria was met (only for non-loop steps or when loop handling is done)
					if !step.HasLoop {
						if validationResponse.IsSuccessCriteriaMet {
							hcpo.GetLogger().Infof("✅ Step %d passed validation - success criteria met", i+1)
							break // Exit retry loop and continue to next step
						} else {
							hcpo.GetLogger().Warnf("⚠️ Step %d failed validation - success criteria not met (attempt %d/%d)", i+1, retryAttempt, maxRetryAttempts)

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
				} // End of retry loop

				// If in loop mode and condition not met, continue main loop
				if step.HasLoop && !loopConditionMet {
					continue // Continue main loop for next iteration
				}

				// Exit main loop if not in loop mode or loop condition met
				if !step.HasLoop {
					// Non-loop step: execute once and exit
					break // Exit main execution loop
				}
				if loopConditionMet {
					// Loop step with condition met: exit loop
					break // Exit main execution loop
				}
				// Loop step with condition not met: continue to next iteration
			} // End of main execution loop

			// BLOCKING HUMAN FEEDBACK - Ask user if they want to continue to next step
			// If user provides feedback (doesn't approve), stop workflow and ask user to manually update plan
			// FAST MODE: Skip human feedback and auto-approve
			// NORMAL MODE & LOOP MODE: Always request human feedback before moving to next step
			isFastExecuteStep := hcpo.IsFastExecuteStep(i)
			hcpo.GetLogger().Infof("🔍 DEBUG: Step %d human feedback check - fastExecuteMode=%v, fastExecuteEndStep=%d, stepIndex=%d, isFastExecuteStep=%v", i+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, i, isFastExecuteStep)
			var approved bool
			var feedback string

			// In fast execute mode, always auto-approve without human feedback
			if isFastExecuteStep {
				hcpo.GetLogger().Infof("⚡ Fast mode: Auto-approving step %d without human feedback (stepIndex=%d <= fastExecuteEndStep=%d)", i+1, i, hcpo.fastExecuteEndStep)
				approved = true
				feedback = "" // No feedback in fast mode
			} else {
				// Normal mode and loop mode: Request human feedback
				var validationSummary string
				if validationResponse != nil {
					validationSummary = fmt.Sprintf("Step %d validation completed. Success Criteria Met: %v, Status: %s", i+1, validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
				} else {
					validationSummary = fmt.Sprintf("Step %d execution failed - no validation response available", i+1)
				}
				var err error
				approved, feedback, err = hcpo.requestHumanFeedback(ctx, i+1, len(breakdownSteps), validationSummary)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %w", err)
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
				// Always save progress after marking a step as completed (both fast and normal mode)
				if err := hcpo.saveStepProgress(ctx, progress); err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to save step progress: %w", err)
				} else {
					modeStr := "fast mode"
					if !isFastExecuteStep {
						modeStr = "normal mode"
					}
					hcpo.GetLogger().Infof("✅ Step %d/%d marked as completed and saved (%s) - Total completed: %d/%d", i+1, len(breakdownSteps), modeStr, len(progress.CompletedStepIndices), progress.TotalSteps)
				}
				stepCompleted = true
			} else {
				// User provided feedback (didn't approve) - stop workflow and ask user to manually update plan
				hcpo.GetLogger().Infof("🛑 User provided feedback - stopping workflow. Feedback: %s", feedback)
				planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
				return nil, fmt.Errorf("workflow stopped: user feedback received. please manually update the plan at %s with the following feedback, then restart the workflow: %s", planPath, feedback)
			}
		} // End of outer loop for step execution
	}

	// Final save to ensure all completed steps are persisted
	// This is a safety measure to catch any steps that might have been missed
	if err := hcpo.saveStepProgress(ctx, progress); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to save final step progress: %w", err)
	} else {
		hcpo.GetLogger().Infof("💾 Final progress save completed: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps)
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

	// Execute variable extraction using structured output via tool
	extractionAgentTyped, ok := extractionAgent.(*VariableExtractionAgent)
	if !ok {
		return nil, "", fmt.Errorf("failed to cast variable extraction agent to correct type")
	}

	manifest, updatedHistory, err := extractionAgentTyped.ExecuteStructured(ctx, extractionTemplateVars, conversationHistory)
	if err != nil {
		return nil, "", fmt.Errorf("variable extraction failed: %w", err)
	}

	// Store manifest in orchestrator for future use
	hcpo.variablesManifest = manifest
	hcpo.templatedObjective = manifest.Objective

	// Save to file for persistence and debugging (optional but useful)
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to marshal variables manifest to JSON: %v (continuing anyway)", err)
	} else {
		if err := hcpo.WriteWorkspaceFile(ctx, variablesPath, string(variablesJSON)); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to save variables.json to file: %v (continuing anyway)", err)
		} else {
			hcpo.GetLogger().Infof("💾 Saved variables.json to %s for persistence", variablesPath)
		}
	}

	hcpo.GetLogger().Infof("✅ Extracted %d variables from objective (conversation has %d messages)", len(manifest.Variables), len(updatedHistory))
	return manifest, manifest.Objective, nil
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
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
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

// sanitizeTitleForAgentName sanitizes a step title for use in agent names
// - Removes step number prefixes (e.g., "Step 4:", "Step 5 -", "Step 3.")
// - Removes/replaces special characters (colons, slashes, etc.)
// - Normalizes whitespace and converts to lowercase
// - Removes multiple consecutive dashes
func (hcpo *HumanControlledTodoPlannerOrchestrator) sanitizeTitleForAgentName(title string) string {
	sanitized := strings.TrimSpace(title)

	// Remove step number prefixes (case-insensitive)
	// Matches: "Step N:", "Step N -", "Step N.", "Step N ", etc.
	stepNumberPattern := regexp.MustCompile(`(?i)^step\s+\d+\s*[:.\-]*\s*`)
	sanitized = stepNumberPattern.ReplaceAllString(sanitized, "")

	// Replace spaces with dashes
	sanitized = strings.ReplaceAll(sanitized, " ", "-")

	// Remove or replace special characters that aren't safe for agent names
	// Keep: letters, numbers, dashes, underscores
	// Remove: colons, slashes, backslashes, pipes, etc.
	specialCharPattern := regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
	sanitized = specialCharPattern.ReplaceAllString(sanitized, "-")

	// Normalize multiple consecutive dashes to single dash
	multiDashPattern := regexp.MustCompile(`-+`)
	sanitized = multiDashPattern.ReplaceAllString(sanitized, "-")

	// Remove leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")

	// Convert to lowercase for consistency
	sanitized = strings.ToLower(sanitized)

	// Ensure we have something left (fallback if everything was removed)
	if sanitized == "" {
		sanitized = "step"
	}

	return sanitized
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
	// Use stored learning detail level preference (set once before execution starts)
	learningDetailLevel := hcpo.GetLearningDetailLevel()
	if learningDetailLevel == "" {
		hcpo.GetLogger().Warnf("⚠️ Learning detail level not set, defaulting to 'general'")
		learningDetailLevel = "general"
	}

	// Skip learning if "none" is selected
	if learningDetailLevel == "none" {
		hcpo.GetLogger().Infof("⏭️ Skipping success learning analysis for step %d/%d (learning disabled)", stepNumber, totalSteps)
		return "", nil
	}

	hcpo.GetLogger().Infof("🧠 Starting success learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create success learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := hcpo.resolveVariables(step.Title)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	successLearningAgentName := fmt.Sprintf("step-%d-%s-%s", stepNumber, sanitizedTitle, learningMode)
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
	// Use stored learning detail level preference (set once before execution starts)
	learningDetailLevel := hcpo.GetLearningDetailLevel()
	if learningDetailLevel == "" {
		hcpo.GetLogger().Warnf("⚠️ Learning detail level not set, defaulting to 'general'")
		learningDetailLevel = "general"
	}

	// Skip learning if "none" is selected
	if learningDetailLevel == "none" {
		hcpo.GetLogger().Infof("⏭️ Skipping failure learning analysis for step %d/%d (learning disabled)", stepNumber, totalSteps)
		return "", "", nil
	}

	hcpo.GetLogger().Infof("🧠 Starting failure learning analysis for step %d/%d: %s", stepNumber, totalSteps, step.Title)

	// Create failure learning agent
	// Resolve variables in step title before using in agent name
	resolvedTitle := hcpo.resolveVariables(step.Title)
	sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
	// Include learning mode in agent name (exact or general)
	learningMode := "general"
	if learningDetailLevel == "exact" {
		learningMode = "exact"
	}
	failureLearningAgentName := fmt.Sprintf("step-%d-%s-%s", stepNumber, sanitizedTitle, learningMode)
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

// runWriterPhaseWithHumanReview creates todo list using learning integration agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) runWriterPhaseWithHumanReview(ctx context.Context, iteration int) error {
	hcpo.GetLogger().Infof("📝 Starting todo list generation using learning integration agent")

	// Read plan.json manually
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		return fmt.Errorf("failed to read plan.json: %w", err)
	}

	// Parse existing plan.json into PlanningResponse
	var existingPlan PlanningResponse
	if err := json.Unmarshal([]byte(planContent), &existingPlan); err != nil {
		return fmt.Errorf("failed to parse plan.json: %w", err)
	}

	// Create learning integration agent
	integrationAgent, err := hcpo.createLearningIntegrationAgent(ctx, "todo_generation", 0, 1)
	if err != nil {
		return fmt.Errorf("failed to create learning integration agent: %w", err)
	}

	// Prepare template variables
	integrationTemplateVars := map[string]string{
		"Objective":        hcpo.GetObjective(),
		"WorkspacePath":    fmt.Sprintf("%s/learnings", hcpo.GetWorkspacePath()), // Limit to learnings folder
		"ExistingPlanJSON": planContent,                                          // Pass plan.json contents
	}

	// Add variable names if available
	if variableNames := hcpo.formatVariableNames(); variableNames != "" {
		integrationTemplateVars["VariableNames"] = variableNames
	}

	// Add learning detail level if available
	if learningDetailLevel := hcpo.GetLearningDetailLevel(); learningDetailLevel != "" {
		integrationTemplateVars["LearningDetailLevel"] = learningDetailLevel
	}

	// Call ExecuteStructured - returns LearningPatternResponse (patterns only)
	integrationAgentTyped, ok := integrationAgent.(*HumanControlledLearningIntegrationAgent)
	if !ok {
		return fmt.Errorf("failed to cast learning integration agent to correct type")
	}

	patternResponse, _, err := integrationAgentTyped.ExecuteStructured(ctx, integrationTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return fmt.Errorf("learning integration failed: %w", err)
	}

	// Merge patterns into existing plan
	enhancedPlan := hcpo.mergePatternsIntoPlan(&existingPlan, patternResponse)
	if enhancedPlan == nil {
		return fmt.Errorf("failed to merge patterns into plan")
	}

	// Save enhanced plan back to plan.json (update original)
	planJSON, err := json.MarshalIndent(enhancedPlan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal enhanced plan: %w", err)
	}
	if err := hcpo.WriteWorkspaceFile(ctx, planPath, string(planJSON)); err != nil {
		return fmt.Errorf("failed to save enhanced plan.json: %w", err)
	}
	hcpo.GetLogger().Infof("✅ Enhanced plan saved to plan.json (%d steps with patterns)", len(enhancedPlan.Steps))

	// Also save as todo_final.json
	todoListPath := fmt.Sprintf("%s/../todo_final.json", hcpo.GetWorkspacePath())
	if err := hcpo.WriteWorkspaceFile(ctx, todoListPath, string(planJSON)); err != nil {
		return fmt.Errorf("failed to save todo_final.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Todo list saved to todo_final.json (%d steps)", len(enhancedPlan.Steps))

	return nil
}

// handlePlanChange prompts the user when the plan has changed (different number of steps)
// Returns: (choice string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) handlePlanChange(ctx context.Context, oldProgress *StepProgress, newTotalSteps int) (string, error) {
	hcpo.GetLogger().Infof("🤔 Requesting user decision for plan change: %d steps → %d steps", oldProgress.TotalSteps, newTotalSteps)

	// Generate unique request ID
	requestID := fmt.Sprintf("plan_change_decision_%d_%d_%d", oldProgress.TotalSteps, newTotalSteps, time.Now().UnixNano())

	// Build context message
	contextMsg := "**Plan Change Detected**\n\n"
	contextMsg += fmt.Sprintf("Previous plan had **%d steps** with **%d steps completed** (indices: %v)\n\n",
		oldProgress.TotalSteps, len(oldProgress.CompletedStepIndices), oldProgress.CompletedStepIndices)
	contextMsg += fmt.Sprintf("Current plan has **%d steps**\n\n", newTotalSteps)
	contextMsg += fmt.Sprintf("**Last updated**: %s\n\n", oldProgress.LastUpdated.Format("2006-01-02 15:04:05"))
	contextMsg += "**How would you like to proceed?**\n\n"
	contextMsg += "- **Option 1**: Keep old progress (try to match steps, may not work perfectly)\n"
	contextMsg += "- **Option 2**: Delete old progress and start completely fresh"

	// Use multiple-choice feedback with 2 options
	planChangeOptions := []string{
		"Keep Old Progress",                 // Option 0: Try to match steps
		"Delete Old Progress & Start Fresh", // Option 1: Delete and start fresh
	}
	choice, err := hcpo.RequestMultipleChoiceFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Plan changed from %d steps to %d steps. How would you like to proceed?", oldProgress.TotalSteps, newTotalSteps),
		planChangeOptions,
		contextMsg,
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)

	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Plan change decision request failed: %w", err)
		return "", err
	}

	hcpo.GetLogger().Infof("✅ User selected option for plan change: %s", choice)
	return choice, nil
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

// requestLearningDetailLevel asks user to choose the level of detail for learning analysis
// Returns: ("exact" for exact MCP tools with args, "general" for general patterns, "none" to skip learning, error)
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
		contextMsg += "\n- **No Learnings Required**: Skip all learning agents and learning integration"
		question = "How detailed should the learning analysis be for all steps?"
	} else {
		// Asking for specific step
		contextMsg = fmt.Sprintf("Step %d/%d: %s\n\nLearning Type: %s learning analysis", stepNumber, totalSteps, stepTitle, learningType)
		contextMsg += "\n\n**Choose the level of detail for learning analysis:**\n"
		contextMsg += "\n- **Exact MCP Tools**: Extract exact tool calls with complete argument JSON"
		contextMsg += "\n- **General Patterns**: Extract high-level approaches and paths to success"
		contextMsg += "\n- **No Learnings Required**: Skip all learning agents and learning integration"
		question = fmt.Sprintf("How detailed should the %s learning analysis be for step %d?", learningType, stepNumber)
	}

	// Use multiple-choice feedback with 3 options
	learningOptions := []string{
		"Exact MCP Tools",
		"General Patterns",
		"No Learnings Required",
	}
	choice, err := hcpo.RequestMultipleChoiceFeedback(
		ctx,
		requestID,
		question,
		learningOptions,
		contextMsg,
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)

	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Learning detail level request failed: %v, defaulting to 'general'", err)
		return "general", nil // Default to general if request fails
	}

	// Map response to our internal values
	if choice == "option0" {
		hcpo.GetLogger().Infof("✅ User selected: Exact MCP Tools")
		return "exact", nil
	} else if choice == "option1" {
		hcpo.GetLogger().Infof("✅ User selected: General Patterns")
		return "general", nil
	} else if choice == "option2" {
		hcpo.GetLogger().Infof("✅ User selected: No Learnings Required")
		return "none", nil
	}

	// Default to general if unclear
	hcpo.GetLogger().Warnf("⚠️ Unexpected choice: %s, defaulting to 'general'", choice)
	return "general", nil
}

// Agent creation methods - reuse from base orchestrator
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPlanningAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use CreateAndSetupStandardAgentWithCustomServers instead of CreateAndSetupStandardAgentWithCustomServersAndSystemPrompt
	// because system prompt is passed directly to ExecuteStructuredWithInputProcessor() in planning_agent.go
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"human-controlled-planning-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		hcpo.GetSelectedServers(), // Pass MCP servers so agent knows available tools/capabilities for planning
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

// checkForNewLearnings checks if there are new or modified learning files compared to cached metadata
// Returns: (hasNewLearnings bool, currentLearningFiles []LearningFileInfo, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkForNewLearnings(ctx context.Context, learningsDir string, cachedMetadata *EnhancedPlanWithMetadata) (bool, []LearningFileInfo, error) {
	hcpo.GetLogger().Infof("🔍 Checking for new learnings in %s", learningsDir)

	// Get list_workspace_files executor
	listExecutorInterface, exists := hcpo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		return false, nil, fmt.Errorf("list_workspace_files executor not found")
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return false, nil, fmt.Errorf("list_workspace_files executor has wrong type")
	}

	// Call list_workspace_files to get current learning files
	listArgs := map[string]interface{}{
		"folder":    learningsDir,
		"max_depth": 1, // Only list files directly in learnings directory
	}

	fileListJSON, err := listExecutor(ctx, listArgs)
	if err != nil {
		// Directory may not exist or be empty - this is okay
		hcpo.GetLogger().Infof("ℹ️ Failed to list files in learnings directory (may be empty): %v", err)
		return false, []LearningFileInfo{}, nil
	}

	// Parse the JSON response
	var filesList []map[string]interface{}
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		// Try alternative format - might be a single object with a "files" array
		var altFormat map[string]interface{}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil {
			if filesArray, ok := altFormat["files"].([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						filesList = append(filesList, fileMap)
					}
				}
			}
		}
		if len(filesList) == 0 {
			hcpo.GetLogger().Infof("ℹ️ No learning files found")
			return false, []LearningFileInfo{}, nil
		}
	}

	// Extract learning files (only .md files matching step_*_learning.md pattern)
	var currentLearningFiles []LearningFileInfo
	for _, fileInfo := range filesList {
		filepath, ok := fileInfo["filepath"].(string)
		if !ok || filepath == "" {
			continue
		}

		// Skip directories
		if isDirectory, ok := fileInfo["is_directory"].(bool); ok && isDirectory {
			continue
		}

		// Only process learning files (step_*_learning.md pattern)
		if !strings.Contains(filepath, "_learning.md") {
			continue
		}

		// Extract modification time
		var modifiedAt time.Time
		if modifiedAtStr, ok := fileInfo["modified_at"].(string); ok && modifiedAtStr != "" {
			// Try parsing as RFC3339
			if t, err := time.Parse(time.RFC3339, modifiedAtStr); err == nil {
				modifiedAt = t
			} else if t, err := time.Parse("2006-01-02T15:04:05Z07:00", modifiedAtStr); err == nil {
				modifiedAt = t
			}
		} else if modifiedAtFloat, ok := fileInfo["modified_at"].(float64); ok {
			// Unix timestamp
			modifiedAt = time.Unix(int64(modifiedAtFloat), 0)
		}

		currentLearningFiles = append(currentLearningFiles, LearningFileInfo{
			Filepath:   filepath,
			ModifiedAt: modifiedAt,
		})
	}

	// If no cached metadata, any learning files are considered new
	if cachedMetadata == nil {
		if len(currentLearningFiles) > 0 {
			hcpo.GetLogger().Infof("✅ Found %d learning files (no cache exists)", len(currentLearningFiles))
			return true, currentLearningFiles, nil
		}
		return false, currentLearningFiles, nil
	}

	// Compare current files with cached metadata
	// Check if any file is new or has been modified
	hasNewLearnings := false
	cachedFileMap := make(map[string]time.Time)
	for _, cachedFile := range cachedMetadata.LearningFiles {
		cachedFileMap[cachedFile.Filepath] = cachedFile.ModifiedAt
	}

	for _, currentFile := range currentLearningFiles {
		cachedTime, exists := cachedFileMap[currentFile.Filepath]
		if !exists {
			// New file
			hcpo.GetLogger().Infof("🆕 New learning file detected: %s", currentFile.Filepath)
			hasNewLearnings = true
		} else if currentFile.ModifiedAt.After(cachedTime) {
			// Modified file
			hcpo.GetLogger().Infof("📝 Modified learning file detected: %s (was: %s, now: %s)",
				currentFile.Filepath, cachedTime.Format(time.RFC3339), currentFile.ModifiedAt.Format(time.RFC3339))
			hasNewLearnings = true
		}
	}

	// Also check if any cached files were deleted (if we have fewer files now)
	if len(currentLearningFiles) < len(cachedMetadata.LearningFiles) {
		hcpo.GetLogger().Infof("🗑️ Some learning files were deleted (had %d, now %d)",
			len(cachedMetadata.LearningFiles), len(currentLearningFiles))
		hasNewLearnings = true
	}

	if !hasNewLearnings {
		hcpo.GetLogger().Infof("✅ No new learnings detected - cache is up to date")
	}

	return hasNewLearnings, currentLearningFiles, nil
}

// loadEnhancedPlanFromCache loads the cached enhanced plan from plan_learnings.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadEnhancedPlanFromCache(ctx context.Context, cachePath string) (*EnhancedPlanWithMetadata, error) {
	hcpo.GetLogger().Infof("📖 Loading enhanced plan from cache: %s", cachePath)

	content, err := hcpo.ReadWorkspaceFile(ctx, cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cachedMetadata EnhancedPlanWithMetadata
	if err := json.Unmarshal([]byte(content), &cachedMetadata); err != nil {
		return nil, fmt.Errorf("failed to parse cache file: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Loaded cached enhanced plan (last updated: %s, %d learning files)",
		cachedMetadata.LastUpdated.Format(time.RFC3339), len(cachedMetadata.LearningFiles))
	return &cachedMetadata, nil
}

// saveEnhancedPlanToCache saves the enhanced plan with metadata to plan_learnings.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) saveEnhancedPlanToCache(ctx context.Context, cachePath string, enhancedPlan *PlanningResponse, learningFiles []LearningFileInfo) error {
	hcpo.GetLogger().Infof("💾 Saving enhanced plan to cache: %s", cachePath)

	cachedMetadata := EnhancedPlanWithMetadata{
		Plan:          enhancedPlan,
		LastUpdated:   time.Now(),
		LearningFiles: learningFiles,
	}

	cacheJSON, err := json.MarshalIndent(cachedMetadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, cachePath, string(cacheJSON)); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Saved enhanced plan to cache (%d learning files)", len(learningFiles))
	return nil
}

// deleteEnhancedPlanCache deletes the cached enhanced plan (plan_learnings.json)
func (hcpo *HumanControlledTodoPlannerOrchestrator) deleteEnhancedPlanCache(ctx context.Context) error {
	cachePath := fmt.Sprintf("%s/planning/plan_learnings.json", hcpo.GetWorkspacePath())

	if err := hcpo.DeleteWorkspaceFile(ctx, cachePath); err != nil {
		// Ignore "file not found" errors, but log others
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Warnf("⚠️ Failed to delete plan_learnings.json: %w", err)
			return err
		}
		// File doesn't exist - that's okay
		hcpo.GetLogger().Infof("ℹ️ plan_learnings.json doesn't exist (nothing to delete)")
		return nil
	}

	hcpo.GetLogger().Infof("🗑️ Deleted plan_learnings.json cache")
	return nil
}

// runLearningIntegrationPhase enhances existing plan.json with success/failure patterns from learnings files
// Uses caching to avoid re-running integration when no new learnings are detected
func (hcpo *HumanControlledTodoPlannerOrchestrator) runLearningIntegrationPhase(ctx context.Context, existingPlan *PlanningResponse) (*PlanningResponse, error) {
	if existingPlan == nil {
		return nil, fmt.Errorf("existing plan is nil - cannot enhance without plan")
	}

	// Skip learning integration if "none" is selected
	if hcpo.GetLearningDetailLevel() == "none" {
		hcpo.GetLogger().Infof("⏭️ Skipping learning integration phase (learning disabled)")
		return existingPlan, nil
	}

	hcpo.GetLogger().Infof("🧠 Starting learning integration phase")

	// Check cache first
	cachePath := fmt.Sprintf("%s/planning/plan_learnings.json", hcpo.GetWorkspacePath())
	learningsDir := fmt.Sprintf("%s/learnings", hcpo.GetWorkspacePath())

	var cachedMetadata *EnhancedPlanWithMetadata
	cachedMetadata, err := hcpo.loadEnhancedPlanFromCache(ctx, cachePath)
	if err != nil {
		// Cache doesn't exist or is invalid - this is okay, we'll create it
		hcpo.GetLogger().Infof("ℹ️ No cache found or cache invalid: %v (will create new cache)", err)
		cachedMetadata = nil
	}

	// Check for new learnings
	hasNewLearnings, currentLearningFiles, err := hcpo.checkForNewLearnings(ctx, learningsDir, cachedMetadata)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for new learnings: %v (will run integration anyway)", err)
		hasNewLearnings = true // Default to running integration on error
	}

	// If cache exists and no new learnings, return cached plan
	if cachedMetadata != nil && !hasNewLearnings {
		hcpo.GetLogger().Infof("✅ Using cached enhanced plan (no new learnings detected)")
		return cachedMetadata.Plan, nil
	}

	// Need to run integration (either no cache or new learnings detected)
	if hasNewLearnings {
		hcpo.GetLogger().Infof("🔄 New learnings detected - running learning integration")
	} else {
		hcpo.GetLogger().Infof("🔄 No cache exists - running learning integration")
	}

	// Create learning integration agent
	integrationAgent, err := hcpo.createLearningIntegrationAgent(ctx, "learning_integration", 0, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create learning integration agent: %w", err)
	}

	// Serialize existing plan to JSON for agent input
	existingPlanJSON, err := json.MarshalIndent(existingPlan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal existing plan to JSON: %w", err)
	}

	// Prepare template variables for learning integration agent
	integrationTemplateVars := map[string]string{
		"Objective":           hcpo.GetObjective(),
		"WorkspacePath":       hcpo.GetWorkspacePath(),
		"ExistingPlanJSON":    string(existingPlanJSON),
		"LearningDetailLevel": hcpo.GetLearningDetailLevel(), // Pass learning detail level preference
	}

	// Add variable names if available
	if variableNames := hcpo.formatVariableNames(); variableNames != "" {
		integrationTemplateVars["VariableNames"] = variableNames
	}

	// Execute learning integration agent to get enhanced plan
	integrationAgentTyped, ok := integrationAgent.(*HumanControlledLearningIntegrationAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast learning integration agent to correct type")
	}

	patternResponse, _, err := integrationAgentTyped.ExecuteStructured(ctx, integrationTemplateVars, []llmtypes.MessageContent{})
	if err != nil {
		return nil, fmt.Errorf("learning integration failed: %w", err)
	}

	// Merge patterns into existing plan by matching step titles
	enhancedPlan := hcpo.mergePatternsIntoPlan(existingPlan, patternResponse)

	// Save enhanced plan to cache with current learning files metadata
	if err := hcpo.saveEnhancedPlanToCache(ctx, cachePath, enhancedPlan, currentLearningFiles); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to save enhanced plan to cache: %v (continuing anyway)", err)
		// Don't fail the whole operation if cache save fails
	}

	hcpo.GetLogger().Infof("✅ Learning integration completed: enhanced plan with patterns (saved to cache)")
	return enhancedPlan, nil
}

// mergePatternsIntoPlan merges patterns from LearningPatternResponse into existing PlanningResponse by matching step titles
func (hcpo *HumanControlledTodoPlannerOrchestrator) mergePatternsIntoPlan(existingPlan *PlanningResponse, patternResponse *LearningPatternResponse) *PlanningResponse {
	if existingPlan == nil || patternResponse == nil {
		return existingPlan
	}

	// Create a map of step titles to patterns for quick lookup
	patternMap := make(map[string]*LearningPatternStep)
	for i := range patternResponse.Steps {
		patternMap[patternResponse.Steps[i].Title] = &patternResponse.Steps[i]
	}

	// Create enhanced plan by copying existing plan and adding patterns
	enhancedPlan := &PlanningResponse{
		Steps: make([]PlanStep, len(existingPlan.Steps)),
	}

	for i, step := range existingPlan.Steps {
		enhancedPlan.Steps[i] = step // Copy all existing fields

		// Match by title and add patterns if found
		if patternStep, found := patternMap[step.Title]; found {
			enhancedPlan.Steps[i].SuccessPatterns = patternStep.SuccessPatterns
			enhancedPlan.Steps[i].FailurePatterns = patternStep.FailurePatterns
		} else {
			// No patterns found for this step - use empty arrays
			enhancedPlan.Steps[i].SuccessPatterns = []string{}
			enhancedPlan.Steps[i].FailurePatterns = []string{}
		}
	}

	return enhancedPlan
}

// createLearningIntegrationAgent creates a learning integration agent for enhancing plans with patterns
func (hcpo *HumanControlledTodoPlannerOrchestrator) createLearningIntegrationAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"learning-integration-agent",
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers needed - pure LLM analysis agent
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledLearningIntegrationAgent(config, logger, tracer, eventBridge)
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
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		agentName,
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers needed - pure LLM analysis agent
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
// Note: This now uses the unified learning agent which handles both success and failure cases
func (hcpo *HumanControlledTodoPlannerOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string) (agents.OrchestratorAgent, error) {
	agent, err := hcpo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		agentName,
		phase,
		step,
		iteration,
		hcpo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers needed - pure LLM analysis agent
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerLearningAgent(config, logger, tracer, eventBridge)
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
		hcpo.GetLogger().Warnf("⚠️ Failed to emit todo steps extracted event: %w", err)
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
		return false, nil, err
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

// checkExistingVariables checks if variables.json already exists and loads it
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkExistingVariables(ctx context.Context, variablesPath string) (bool, *VariablesManifest, error) {
	hcpo.GetLogger().Infof("🔍 Checking for existing variables at %s", variablesPath)

	// Try to read variables.json
	variablesContent, err := hcpo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		// Check if it's a "file not found" error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📋 No existing variables found: %w", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, err
	}

	// Parse the existing variables manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to parse existing variables.json: %w", err)
		return false, nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Found existing variables.json with %d variables", len(manifest.Variables))
	return true, &manifest, nil
}

// cleanupExistingPlanArtifacts deletes existing plan.json, steps_done.json, and all files in learnings/, execution/, and validation/ directories
// This is called when user chooses to create a new plan instead of using existing one
func (hcpo *HumanControlledTodoPlannerOrchestrator) cleanupExistingPlanArtifacts(ctx context.Context, workspacePath string) error {
	hcpo.GetLogger().Infof("🧹 Starting cleanup of existing plan artifacts")

	basePath := fmt.Sprintf("%s/todo_creation_human", workspacePath)

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

	// 2. Delete all files in validation/ directory
	validationDir := fmt.Sprintf("%s/validation", basePath)
	if err := hcpo.CleanupDirectory(ctx, validationDir, "validation"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup validation directory: %w", err)
	}

	// 3. Note: learnings/ folder is preserved - deleted manually only

	// 4. Delete all files in execution/ directory
	executionDir := fmt.Sprintf("%s/execution", basePath)
	if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
	}

	// 5. Delete steps_done.json progress file
	if err := hcpo.deleteStepProgress(ctx); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to delete steps_done.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Cleanup of existing plan artifacts completed")
	return nil
}

// formatValidationResponseForTemplate formats validation response data for inclusion in template variables
// This makes validation output available to the execution agent via ValidationFeedback template variable
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatValidationResponseForTemplate(validationResponse *ValidationResponse, context string) string {
	if validationResponse == nil {
		return ""
	}

	var parts []string

	// Add context header
	if context != "" {
		parts = append(parts, fmt.Sprintf("## %s", context))
	}

	// Add reasoning
	if validationResponse.Reasoning != "" {
		parts = append(parts, fmt.Sprintf("**Reasoning**: %s", validationResponse.Reasoning))
	}

	// Add loop-specific information if present
	if validationResponse.LoopReasoning != "" {
		parts = append(parts, fmt.Sprintf("**Loop Condition Status**: %v", validationResponse.LoopConditionMet))
		parts = append(parts, fmt.Sprintf("**Loop Reasoning**: %s", validationResponse.LoopReasoning))
	}

	// Add execution status
	if validationResponse.ExecutionStatus != "" {
		parts = append(parts, fmt.Sprintf("**Execution Status**: %s", validationResponse.ExecutionStatus))
	}

	// Add success criteria status
	parts = append(parts, fmt.Sprintf("**Success Criteria Met**: %v", validationResponse.IsSuccessCriteriaMet))

	// Add feedback items
	if len(validationResponse.Feedback) > 0 {
		feedbackParts := []string{"**Feedback**: "}
		for _, fb := range validationResponse.Feedback {
			feedbackParts = append(feedbackParts, fmt.Sprintf("- [%s] %s: %s", fb.Severity, fb.Type, fb.Description))
		}
		parts = append(parts, strings.Join(feedbackParts, "\n"))
	}

	return strings.Join(parts, "\n\n")
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
