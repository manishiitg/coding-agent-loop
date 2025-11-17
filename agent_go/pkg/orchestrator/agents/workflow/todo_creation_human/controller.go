package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
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
	Title                    string        `json:"title"`
	Description              string        `json:"description"`
	SuccessCriteria          string        `json:"success_criteria"`
	ContextDependencies      []string      `json:"context_dependencies"`
	ContextOutput            string        `json:"context_output"`
	LearningFilesToReference []string      `json:"learning_files_to_reference,omitempty"` // learning files to read for context (execution agent reads full files)
	HasLoop                  bool          `json:"has_loop"`                              // true if step needs to loop
	LoopCondition            string        `json:"loop_condition"`                        // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations            int           `json:"max_iterations,omitempty"`              // max iterations (default: 10)
	LoopDescription          string        `json:"loop_description,omitempty"`            // human-readable explanation
	AgentConfigs             *AgentConfigs `json:"agent_configs,omitempty"`               // per-agent configuration (LLM, max turns, toggles)
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

// TodoStepsExtractedEvent represents the event when todo steps are extracted from a plan
type TodoStepsExtractedEvent struct {
	events.BaseEventData
	TotalStepsExtracted int        `json:"total_steps_extracted"`
	ExtractedSteps      []TodoStep `json:"extracted_steps"`
	ExtractionMethod    string     `json:"extraction_method"`
	PlanSource          string     `json:"plan_source"`          // "existing_plan" or "new_plan"
	WorkspacePath       string     `json:"workspace_path"`       // Workspace path for file operations (required)
	RunFolder           string     `json:"run_folder,omitempty"` // Run folder name for run-specific configs
}

// GetEventType returns the event type for TodoStepsExtractedEvent
func (e *TodoStepsExtractedEvent) GetEventType() events.EventType {
	return events.TodoStepsExtracted
}

// VariablesExtractedEvent represents the event when variables are extracted from objective
type VariablesExtractedEvent struct {
	events.BaseEventData
	Variables          []Variable `json:"variables"`
	TemplatedObjective string     `json:"templated_objective"`
	WorkspacePath      string     `json:"workspace_path"`       // Workspace path for file operations (required)
	RunFolder          string     `json:"run_folder,omitempty"` // Run folder name for run-specific configs
}

// GetEventType returns the event type for VariablesExtractedEvent
func (e *VariablesExtractedEvent) GetEventType() events.EventType {
	return events.VariablesExtracted
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

	// Skip human input mode tracking (runs learning but skips human feedback)
	skipHumanInput bool // Whether to skip human feedback requests (auto-approve steps)

	// Learning detail level preference (set once before execution, used for all learning phases)
	learningDetailLevel string // "exact" or "general"

	// Approved plan storage (for accessing run_mode during execution)
	approvedPlan *PlanningResponse // Store approved plan to access run_mode

	// Run folder management
	selectedRunFolder string // Selected run folder name (e.g., "initial", "2025-01-27-iteration-1")
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

// getStepsProgressPath returns the path to steps_done.json file in the run folder
func (hcpo *HumanControlledTodoPlannerOrchestrator) getStepsProgressPath() (string, error) {
	if hcpo.selectedRunFolder == "" {
		return "", fmt.Errorf("selectedRunFolder not set - run folder must be resolved before accessing steps_done.json")
	}
	return fmt.Sprintf("%s/runs/%s/steps_done.json", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder), nil
}

// loadStepProgress loads progress from steps_done.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadStepProgress(ctx context.Context) (*StepProgress, error) {
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return nil, err
	}

	content, err := hcpo.ReadWorkspaceFile(ctx, progressPath)
	if err != nil {
		// File doesn't exist or error reading
		return nil, fmt.Errorf("failed to load step progress: %w", err)
	}

	var progress StepProgress
	if err := json.Unmarshal([]byte(content), &progress); err != nil {
		return nil, fmt.Errorf("failed to parse steps_done.json: %w", err)
	}

	return &progress, nil
}

// saveStepProgress saves progress to steps_done.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) saveStepProgress(ctx context.Context, progress *StepProgress) error {
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return err
	}

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
	progressPath, err := hcpo.getStepsProgressPath()
	if err != nil {
		return err
	}

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
	// WorkspacePath is the base workspace path (no subdirectory)
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// PHASE 0: Check both variables and plan at start (before any prompts)
	// Check if variables.json already exists
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := hcpo.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing variables: %w", err)
		variablesExist = false
	}

	// Check if plan.json already exists (moved up to check both at start)
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing plan: %w", err)
		// Continue with normal planning flow
		planExists = false
	}

	var variablesManifest *VariablesManifest
	var templatedObjective string
	var existingVariablesForUpdate *VariablesManifest // Store existing variables for update mode
	var initialUpdateFeedback string                  // Store initial feedback for update mode
	var breakdownSteps []TodoStep
	var initialPlanningFeedback string               // Store feedback for plan updates
	var approvedPlan *PlanningResponse               // Store approved plan for learning integration
	var existingPlanForFirstUpdate *PlanningResponse // Store existing plan for option3 (Update Existing Plan)

	// If both exist, emit both events together before showing prompts
	if variablesExist && planExists {
		hcpo.GetLogger().Infof("📋 Found both existing variables.json and plan.json - emitting both events together")

		// Emit VariablesExtractedEvent
		hcpo.emitVariablesExtractedEvent(ctx, existingVariablesManifest.Variables, existingVariablesManifest.Objective)

		// Convert existing plan to TodoStep format and emit TodoStepsExtractedEvent
		breakdownSteps = hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
		hcpo.GetLogger().Infof("✅ Converted existing plan: %d steps extracted", len(breakdownSteps))
		hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")
	}

	// If variables exist, ask user if they want to use them, extract new ones, or update existing
	if variablesExist {
		requestID := fmt.Sprintf("existing_variables_decision_%d", time.Now().UnixNano())
		variableOptions := []string{
			"Use Existing Variables",    // Option 0: Use existing variables as-is
			"Extract New Variables",     // Option 1: Delete everything and extract new
			"Update Existing Variables", // Option 2: Update existing variables with feedback
		}
		variableChoice, err := hcpo.RequestMultipleChoiceFeedback(
			ctx,
			requestID,
			"Found existing variables.json. What would you like to do?",
			variableOptions,
			fmt.Sprintf("Variables file: %s\nFound %d variables", variablesPath, len(existingVariablesManifest.Variables)),
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing variables: %w", err)
			// Default to using existing variables
			variableChoice = "option0"
		}

		switch variableChoice {
		case "option0":
			// Use existing variables - directly use the parsed JSON
			hcpo.GetLogger().Infof("✅ User chose to use existing variables")
			variablesManifest = existingVariablesManifest
			hcpo.variablesManifest = existingVariablesManifest // Store in orchestrator so formatVariableNames/Values can access it
			templatedObjective = existingVariablesManifest.Objective
			// Emit VariablesExtractedEvent for existing variables (if not already emitted with plan)
			if !planExists {
				hcpo.emitVariablesExtractedEvent(ctx, existingVariablesManifest.Variables, existingVariablesManifest.Objective)
			}

		case "option1":
			// Extract new variables - cleanup everything and extract fresh
			hcpo.GetLogger().Infof("🔄 User chose to extract new variables, cleaning up existing variables file")
			// Delete existing variables file to ensure clean state before extraction
			if err := hcpo.DeleteWorkspaceFile(ctx, variablesPath); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to delete existing variables file: %v (will be overwritten during extraction)", err)
				// Continue anyway - extraction will overwrite the file
			} else {
				hcpo.GetLogger().Infof("🗑️ Deleted existing variables file: %s", variablesPath)
			}
			variablesExist = false // Trigger variable extraction

		case "option2":
			// Update existing variables - request feedback and update with existing context
			hcpo.GetLogger().Infof("🔄 User chose to update existing variables, requesting update feedback")

			// Format existing variables for display
			var variablesSummary strings.Builder
			variablesSummary.WriteString(fmt.Sprintf("Current variables (%d total):\n\n", len(existingVariablesManifest.Variables)))
			for _, variable := range existingVariablesManifest.Variables {
				variablesSummary.WriteString(fmt.Sprintf("- **{{%s}}**: %s\n", variable.Name, variable.Description))
				variablesSummary.WriteString(fmt.Sprintf("  - Value: %s\n", variable.Value))
				variablesSummary.WriteString("\n")
			}
			variablesSummary.WriteString(fmt.Sprintf("\n**Templated Objective**:\n%s", existingVariablesManifest.Objective))

			// Request human feedback about what they want to update
			updateFeedbackID := fmt.Sprintf("variable_update_feedback_%d", time.Now().UnixNano())
			approved, updateFeedback, err := hcpo.RequestHumanFeedback(
				ctx,
				updateFeedbackID,
				"What would you like to update in the existing variables? Please describe the changes or improvements you want.",
				fmt.Sprintf("Current variables location: %s\nFound %d variables\n\n%s\n\nYour feedback will be used to guide the update of variables while preserving unchanged ones.", variablesPath, len(existingVariablesManifest.Variables), variablesSummary.String()),
				hcpo.getSessionID(),
				hcpo.getWorkflowID(),
			)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to get update feedback: %v, proceeding without specific update guidance", err)
				updateFeedback = "" // Proceed without feedback
			} else if approved {
				// User clicked "Approve" without providing feedback (approved=true means response was "Approve")
				hcpo.GetLogger().Infof("ℹ️ User approved without providing update feedback, will update variables without specific guidance")
				updateFeedback = ""
			} else if updateFeedback != "" {
				// User provided feedback (approved=false and feedback contains their input)
				hcpo.GetLogger().Infof("📝 Received update feedback: %s", updateFeedback)
			} else {
				// Edge case: approved=false but empty feedback
				hcpo.GetLogger().Warnf("⚠️ Unexpected feedback state: approved=%v, feedback empty, proceeding without guidance", approved)
				updateFeedback = ""
			}

			// Don't delete variables file - keep it for update context
			// Set flag to trigger update mode extraction (pass existing variables to extraction phase)
			variablesExist = false // Trigger variable extraction, but with update mode
			// Store existing variables and feedback for use in extraction loop
			existingVariablesForUpdate = existingVariablesManifest
			initialUpdateFeedback = updateFeedback // Store initial feedback for first extraction attempt

		default:
			// Unknown choice - default to using existing variables
			hcpo.GetLogger().Warnf("⚠️ Unknown variable choice: %s, defaulting to use existing variables", variableChoice)
			variablesManifest = existingVariablesManifest
			hcpo.variablesManifest = existingVariablesManifest
			templatedObjective = existingVariablesManifest.Objective
		}
	}

	// Extract variables if they don't exist or user wants to re-extract
	if !variablesExist {
		maxVariableRevisions := 10
		var variableFeedback string
		var variableConversationHistory []llmtypes.MessageContent

		// Use initial update feedback for first attempt if in update mode
		if existingVariablesForUpdate != nil {
			variableFeedback = initialUpdateFeedback
			hcpo.GetLogger().Infof("📝 Using initial update feedback for first extraction attempt: %s", variableFeedback)
		}

		for revisionAttempt := 1; revisionAttempt <= maxVariableRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

			// Run variable extraction phase (with optional human feedback and existing variables for update mode)
			var err error
			variablesManifest, templatedObjective, variableConversationHistory, err = hcpo.runVariableExtractionPhase(ctx, revisionAttempt, variableFeedback, variableConversationHistory, existingVariablesForUpdate)
			if err != nil {
				// Check if this error contains user feedback for next attempt (from non-structured response)
				errMsg := err.Error()
				hcpo.GetLogger().Infof("🔍 [DEBUG] Variable extraction phase error detected: %s", errMsg)
				feedbackPrefix := "VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:"
				if strings.Contains(errMsg, feedbackPrefix) {
					hcpo.GetLogger().Infof("✅ [DEBUG] Detected VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK prefix in error")
					// Extract feedback from error message (handle both wrapped and unwrapped errors)
					parts := strings.Split(errMsg, feedbackPrefix)
					if len(parts) > 1 {
						extractedFeedback := strings.TrimSpace(parts[1])
						hcpo.GetLogger().Infof("📝 Extracted user feedback from variable extraction text response: %s", extractedFeedback)

						// Use extracted feedback for next iteration
						variableFeedback = extractedFeedback

						// Continue to next iteration (don't return error - this is expected behavior)
						if revisionAttempt >= maxVariableRevisions {
							hcpo.GetLogger().Warnf("⚠️ Max variable extraction revision attempts (%d) reached, continuing without variables", maxVariableRevisions)
							templatedObjective = objective // Use original objective if extraction fails
							break
						}
						hcpo.GetLogger().Infof("🔄 [DEBUG] Continuing to next variable extraction iteration with extracted feedback")
						continue
					} else {
						hcpo.GetLogger().Warnf("⚠️ [DEBUG] Found prefix but couldn't extract feedback from error: %s", errMsg)
					}
				} else {
					hcpo.GetLogger().Infof("❌ [DEBUG] Error does not contain VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK prefix, treating as real error")
				}
				// For other errors, log and continue without variables
				hcpo.GetLogger().Warnf("⚠️ Variable extraction failed: %v, continuing without variables", err)
				templatedObjective = objective // Use original objective if extraction fails
				break
			}

			// Use the updated conversation history from the agent (already includes the agent's response)
			// No need to manually append - the agent's updatedHistory already contains the full conversation
			hcpo.GetLogger().Infof("✅ Extracted %d variables, templated objective: %s (conversation has %d messages)",
				len(variablesManifest.Variables), templatedObjective, len(variableConversationHistory))

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
				// Emit VariablesExtractedEvent after approval
				hcpo.emitVariablesExtractedEvent(ctx, variablesManifest.Variables, templatedObjective)
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

	// Learning detail level is now configured per-step via AgentConfigs
	// Each step can specify its own learning detail level, defaults to "general" if not set
	hcpo.GetLogger().Infof("📝 Using per-step learning detail level configuration")

	// If only variables exist (not both), emit VariablesExtractedEvent
	if variablesExist && !planExists {
		hcpo.GetLogger().Infof("📋 Found existing variables.json (no plan) - emitting VariablesExtractedEvent")
		hcpo.emitVariablesExtractedEvent(ctx, existingVariablesManifest.Variables, existingVariablesManifest.Objective)
	}

	// If only plan exists (not both), we need to extract variables first, then emit both events
	// This case is handled below in the planning phase

	if planExists {
		hcpo.GetLogger().Infof("📋 Found existing plan.json at %s", planPath)

		// Safety check: Ensure plan has steps
		if len(existingPlan.Steps) == 0 {
			hcpo.GetLogger().Errorf("❌ Existing plan has no steps")
			return "", fmt.Errorf("existing plan has no steps")
		}

		// Convert existing plan to TodoStep format
		// Note: Event was already emitted if both variables and plan exist (above)
		// Only emit here if plan exists but variables don't
		if !variablesExist {
			// Plan exists but variables don't - emit plan event (variables will be extracted later)
			breakdownSteps = hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
			hcpo.GetLogger().Infof("✅ Converted existing plan: %d steps extracted", len(breakdownSteps))
			hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")
		} else {
			// Both events already emitted together, just convert steps for use
			breakdownSteps = hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
		}

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
			// Store approvedPlan in orchestrator for access during execution
			hcpo.approvedPlan = approvedPlan
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

			// Note: Learning integration cache removal no longer needed - execution agent auto-discovers files

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
			// Store approvedPlan in orchestrator for access during execution
			hcpo.approvedPlan = approvedPlan
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
				// Check if this error contains user feedback for next attempt (from non-structured response)
				errMsg := err.Error()
				hcpo.GetLogger().Infof("🔍 [DEBUG] Planning phase error detected: %s", errMsg)
				feedbackPrefix := "PLANNING_TEXT_RESPONSE_FEEDBACK:"
				if strings.Contains(errMsg, feedbackPrefix) {
					hcpo.GetLogger().Infof("✅ [DEBUG] Detected PLANNING_TEXT_RESPONSE_FEEDBACK prefix in error")
					// Extract feedback from error message (handle both wrapped and unwrapped errors)
					// Error might be: "PLANNING_TEXT_RESPONSE_FEEDBACK:feedback" or "planning phase failed: PLANNING_TEXT_RESPONSE_FEEDBACK:feedback"
					parts := strings.Split(errMsg, feedbackPrefix)
					if len(parts) > 1 {
						extractedFeedback := strings.TrimSpace(parts[1])
						hcpo.GetLogger().Infof("📝 Extracted user feedback from planning text response: %s", extractedFeedback)

						// Use extracted feedback for next iteration
						humanFeedback = extractedFeedback

						// Continue to next iteration (don't return error - this is expected behavior)
						if revisionAttempt >= maxPlanRevisions {
							return "", fmt.Errorf("max plan revision attempts (%d) reached", maxPlanRevisions)
						}
						hcpo.GetLogger().Infof("🔄 [DEBUG] Continuing to next planning iteration with extracted feedback")
						continue
					} else {
						hcpo.GetLogger().Warnf("⚠️ [DEBUG] Found prefix but couldn't extract feedback from error: %s", errMsg)
					}
				} else {
					hcpo.GetLogger().Infof("❌ [DEBUG] Error does not contain PLANNING_TEXT_RESPONSE_FEEDBACK prefix, treating as real error")
				}
				// For other errors, return as-is
				return "", fmt.Errorf("planning phase failed: %w", err)
			}

			// Safety check: Ensure plan has steps
			if len(approvedPlan.Steps) == 0 {
				return "", fmt.Errorf("new plan has no steps: planning agent returned empty steps array")
			}

			// Convert approved plan steps to TodoStep format for execution
			breakdownSteps = hcpo.convertPlanStepsToTodoSteps(ctx, approvedPlan.Steps)
			hcpo.GetLogger().Infof("✅ Converted new plan: %d steps extracted", len(breakdownSteps))

			// Emit todo steps extracted event
			hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "new_plan")

			// If variables were extracted (not from existing file), emit VariablesExtractedEvent if not already emitted
			// This handles the case when only plan existed and we extracted variables
			if variablesManifest != nil && !variablesExist {
				hcpo.GetLogger().Infof("📋 Emitting VariablesExtractedEvent for newly extracted variables")
				hcpo.emitVariablesExtractedEvent(ctx, variablesManifest.Variables, templatedObjective)
			}

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
		// Store approvedPlan in orchestrator for access during execution
		hcpo.approvedPlan = approvedPlan
	}

	// Resolve run folder early (after plan approval, before early progress check)
	// This ensures run folder is available for steps_done.json operations
	runMode := "use_same_run"
	if hcpo.approvedPlan != nil && hcpo.approvedPlan.RunMode != "" {
		runMode = hcpo.approvedPlan.RunMode
		hcpo.GetLogger().Infof("📁 Using run_mode from approved plan: %s", runMode)
	} else {
		hcpo.GetLogger().Infof("📁 No run_mode in approved plan, defaulting to 'use_same_run'")
	}

	selectedRunFolder, err := hcpo.resolveRunFolder(ctx, hcpo.GetWorkspacePath(), runMode)
	if err != nil {
		return "", fmt.Errorf("failed to resolve run folder: %w", err)
	}
	hcpo.selectedRunFolder = selectedRunFolder // Store for use in progress operations and execution
	hcpo.GetLogger().Infof("📁 Selected run folder: %s", selectedRunFolder)

	// Note: Learning integration phase removed - execution agent now auto-discovers learning files and scripts

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
				hcpo.GetLogger().Infof("✅ ALL steps already completed - asking user if they want to fast execute all steps again")

				// Ask user if they want to fast execute all steps again
				requestID := fmt.Sprintf("all_steps_done_decision_%d", time.Now().UnixNano())
				options := []string{
					"Fast Execute All Steps Again", // Option 0: Re-execute all steps
					"Skip Execution",               // Option 1: Skip to writer phase
				}
				progressPath, _ := hcpo.getStepsProgressPath()
				progressInfo := fmt.Sprintf("Last updated: %s", earlyProgress.LastUpdated.Format("2006-01-02 15:04:05"))
				if progressPath != "" {
					progressInfo = fmt.Sprintf("Progress file: %s\n%s", progressPath, progressInfo)
				}
				choice, err := hcpo.RequestMultipleChoiceFeedback(
					ctx,
					requestID,
					fmt.Sprintf("All steps are already completed (%d/%d). What would you like to do?", len(earlyProgress.CompletedStepIndices), earlyProgress.TotalSteps),
					options,
					progressInfo,
					hcpo.getSessionID(),
					hcpo.getWorkflowID(),
				)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to get user decision: %v, defaulting to skip execution", err)
					choice = "option1" // Default to skip
				}

				switch choice {
				case "option0":
					// Fast execute all steps again - delete progress and continue with execution
					hcpo.GetLogger().Infof("⚡ User chose to fast execute all steps again, clearing progress")
					if err := hcpo.deleteStepProgress(ctx); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to delete steps_done.json: %v (will continue anyway)", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Deleted steps_done.json to allow re-execution")
					}
					// Set fast execute mode for all steps
					hcpo.SetFastExecuteMode(true, len(breakdownSteps)-1)
					// Clear earlyProgress so execution continues normally
					earlyProgress = nil
					hcpo.GetLogger().Infof("⚡ Will fast execute all steps (0 to %d)", len(breakdownSteps)-1)

				case "option1":
					// Skip execution
					hcpo.GetLogger().Infof("⏭️ User chose to skip execution")

					// Return early with completion message
					return "Todo planning complete. All steps already executed.", nil

				default:
					// Unknown choice - default to skip
					hcpo.GetLogger().Warnf("⚠️ Unknown choice: %s, defaulting to skip execution", choice)
					return "Todo planning complete. All steps already executed.", nil
				}
			}
			hcpo.GetLogger().Infof("📊 Not all steps completed yet - will proceed with execution")
		} else {
			// Plan changed - ask user what to do (only once)
			hcpo.GetLogger().Warnf("⚠️ Total steps changed (previous: %d, current: %d), prompting user for decision",
				earlyProgress.TotalSteps, len(breakdownSteps))

			// Get run mode from approved plan (consistent with run folder resolution)
			runMode := "use_same_run"
			if hcpo.approvedPlan != nil && hcpo.approvedPlan.RunMode != "" {
				runMode = hcpo.approvedPlan.RunMode
				hcpo.GetLogger().Infof("📁 Using run_mode from approved plan: %s", runMode)
			} else {
				hcpo.GetLogger().Infof("📁 No run_mode in approved plan, defaulting to 'use_same_run'")
			}

			// Check if we should ask the question (only when reusing existing folder)
			shouldAsk := hcpo.shouldAskDeleteOldProgress(ctx, hcpo.GetWorkspacePath(), runMode)
			if !shouldAsk {
				hcpo.GetLogger().Infof("📁 Run mode '%s' will create new folder - skipping 'Delete old progress' question, old progress in old folder will be preserved", runMode)
				// Don't delete old progress file - it's in a different folder and won't interfere
				// Just clean up execution artifacts for the new folder (which will be created later)
				// Note: We don't call cleanupExecutionArtifactsForFreshStart here because it would try to clean
				// the folder that will be created, which doesn't exist yet. The cleanup will happen when needed.
				// Clear earlyProgress so we start fresh in the new folder
				earlyProgress = nil
				planChangeHandled = true
			} else {
				// Ask user what to do
				choice, err := hcpo.handlePlanChange(ctx, earlyProgress, len(breakdownSteps))
				planChangeHandled = true // Mark that we've already handled plan change
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for plan change: %w, defaulting to KEEP old progress (preserving user data)", err)
					// Keep earlyProgress as-is to preserve user data - don't delete progress file
					// User can manually delete if needed
				} else {
					switch choice {
					case "option0": // Keep old progress (try to match)
						hcpo.GetLogger().Infof("✅ User chose to keep old progress (will try to match steps)")
						// Keep earlyProgress as-is, will be handled later
					case "option1": // Delete old progress and start fresh
						hcpo.GetLogger().Infof("🔄 User chose to delete old progress and start fresh")
						// Delete old progress file first
						if err := hcpo.deleteStepProgress(ctx); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %w", err)
						}
						// Clean up execution artifacts for fresh start (handles both new and old structure)
						if err := hcpo.cleanupExecutionArtifactsForFreshStart(ctx, hcpo.GetWorkspacePath(), runMode); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %w", err)
						}
						// Initialize fresh progress with new total steps
						if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to initialize fresh progress: %w", err)
						}
						// Note: learnings/ folder is preserved - deleted manually only
						earlyProgress = nil
					default:
						hcpo.GetLogger().Warnf("⚠️ Unknown choice: %s, defaulting to KEEP old progress (preserving user data)", choice)
						// Keep earlyProgress as-is to preserve user data - don't delete progress file
						// User can manually delete if needed
					}
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

			// Get run mode from approved plan (consistent with run folder resolution)
			runMode := "use_same_run"
			if hcpo.approvedPlan != nil && hcpo.approvedPlan.RunMode != "" {
				runMode = hcpo.approvedPlan.RunMode
				hcpo.GetLogger().Infof("📁 Using run_mode from approved plan: %s", runMode)
			} else {
				hcpo.GetLogger().Infof("📁 No run_mode in approved plan, defaulting to 'use_same_run'")
			}

			// Check if we should ask the question (only when reusing existing folder)
			shouldAsk := hcpo.shouldAskDeleteOldProgress(ctx, hcpo.GetWorkspacePath(), runMode)
			if !shouldAsk {
				hcpo.GetLogger().Infof("📁 Run mode '%s' will create new folder - skipping 'Delete old progress' question, old progress in old folder will be preserved", runMode)
				// Don't delete old progress file - it's in a different folder and won't interfere
				// Just clean up execution artifacts for the new folder (which will be created later)
				// Note: We don't call cleanupExecutionArtifactsForFreshStart here because it would try to clean
				// the folder that will be created, which doesn't exist yet. The cleanup will happen when needed.
				// Clear existingProgress so we start fresh in the new folder
				existingProgress = nil
			} else {
				// Ask user what to do
				choice, err := hcpo.handlePlanChange(ctx, existingProgress, len(breakdownSteps))
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for plan change: %w, defaulting to KEEP old progress (preserving user data)", err)
					// Keep existingProgress as-is to preserve user data - don't delete progress file
					// User can manually delete if needed
				} else {
					switch choice {
					case "option0": // Keep old progress (try to match)
						hcpo.GetLogger().Infof("✅ User chose to keep old progress (will try to match steps)")
						// Keep existingProgress as-is, continue processing below
						// Note: Step matching logic may not work perfectly, but we'll try
					case "option1": // Delete old progress and start fresh
						hcpo.GetLogger().Infof("🔄 User chose to delete old progress and start fresh")
						// Delete old progress file first
						if err := hcpo.deleteStepProgress(ctx); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %w", err)
						}
						// Clean up execution artifacts for fresh start (handles both new and old structure)
						if err := hcpo.cleanupExecutionArtifactsForFreshStart(ctx, hcpo.GetWorkspacePath(), runMode); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %w", err)
						}
						// Initialize fresh progress with new total steps
						if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to initialize fresh progress: %w", err)
						}
						// Note: learnings/ folder is preserved - deleted manually only
						existingProgress = nil
					default:
						hcpo.GetLogger().Warnf("⚠️ Unknown choice: %s, defaulting to KEEP old progress (preserving user data)", choice)
						// Keep existingProgress as-is to preserve user data - don't delete progress file
						// User can manually delete if needed
					}
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
				// Check each step to find the first incomplete one
				for i := 0; i < maxStepsToCheck; i++ {
					completed := false
					for _, completedIdx := range existingProgress.CompletedStepIndices {
						if completedIdx == i {
							completed = true
							break
						}
					}
					if !completed {
						// i is 0-based index, convert to 1-based for display
						nextIncompleteStep = i + 1
						hcpo.GetLogger().Infof("🔍 Found next incomplete step: index %d (0-based) = step %d (1-based)", i, nextIncompleteStep)
						break
					}
				}
				// Safety check: if nextIncompleteStep is still 0 after the loop, it means all checked steps are completed
				// This can happen if totalSteps in progress doesn't match actual breakdownSteps count
				if nextIncompleteStep == 0 {
					hcpo.GetLogger().Warnf("⚠️ All checked steps are completed but allStepsCompleted is false - possible mismatch between totalSteps (%d) and actual steps (%d)",
						existingProgress.TotalSteps, len(breakdownSteps))
					// If we have more steps in breakdownSteps than in progress, start from the first unchecked step
					if len(breakdownSteps) > existingProgress.TotalSteps {
						nextIncompleteStep = existingProgress.TotalSteps + 1
						hcpo.GetLogger().Infof("🔍 Plan has more steps than progress - next incomplete step is step %d", nextIncompleteStep)
					}
				}
			}

			if allStepsCompleted {
				// All steps are completed, ask user if they want to fast execute all steps again
				hcpo.GetLogger().Infof("✅ All steps already completed (%d/%d), asking user if they want to fast execute all steps again",
					len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps)

				// Ask user if they want to fast execute all steps again
				requestID := fmt.Sprintf("all_steps_done_decision_%d", time.Now().UnixNano())
				options := []string{
					"Fast Execute All Steps Again", // Option 0: Re-execute all steps
					"Skip Execution",               // Option 1: Skip to writer phase
				}
				progressPath, _ := hcpo.getStepsProgressPath()
				progressInfo := fmt.Sprintf("Last updated: %s", existingProgress.LastUpdated.Format("2006-01-02 15:04:05"))
				if progressPath != "" {
					progressInfo = fmt.Sprintf("Progress file: %s\n%s", progressPath, progressInfo)
				}
				choice, err := hcpo.RequestMultipleChoiceFeedback(
					ctx,
					requestID,
					fmt.Sprintf("All steps are already completed (%d/%d). What would you like to do?", len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps),
					options,
					progressInfo,
					hcpo.getSessionID(),
					hcpo.getWorkflowID(),
				)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to get user decision: %v, defaulting to skip execution", err)
					choice = "option1" // Default to skip
				}

				switch choice {
				case "option0":
					// Fast execute all steps again - delete progress and continue with execution
					hcpo.GetLogger().Infof("⚡ User chose to fast execute all steps again, clearing progress")
					if err := hcpo.deleteStepProgress(ctx); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to delete steps_done.json: %v (will continue anyway)", err)
					} else {
						hcpo.GetLogger().Infof("🗑️ Deleted steps_done.json to allow re-execution")
					}
					// Set fast execute mode for all steps
					hcpo.SetFastExecuteMode(true, len(breakdownSteps)-1)
					// Clear existingProgress so execution continues normally
					existingProgress = nil
					startFromStep = 0
					hcpo.GetLogger().Infof("⚡ Will fast execute all steps (0 to %d)", len(breakdownSteps)-1)

				case "option1":
					// Skip execution
					hcpo.GetLogger().Infof("⏭️ User chose to skip execution")

					// Return early with completion message
					return "Todo planning complete. All steps already executed.", nil

				default:
					// Unknown choice - default to skip
					hcpo.GetLogger().Warnf("⚠️ Unknown choice: %s, defaulting to skip execution", choice)
					return "Todo planning complete. All steps already executed.", nil
				}
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
					fmt.Sprintf("Resume from Step %d without Human", nextIncompleteStep),
					"Start from Beginning without Human",
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
				skipHumanInput := false

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
					// Get run mode from approved plan if available, otherwise default
					runMode := "use_same_run"
					if hcpo.approvedPlan != nil && hcpo.approvedPlan.RunMode != "" {
						runMode = hcpo.approvedPlan.RunMode
						hcpo.GetLogger().Infof("📁 Using run_mode from approved plan: %s", runMode)
					} else {
						hcpo.GetLogger().Infof("📁 No run_mode in approved plan, defaulting to 'use_same_run'")
					}
					// Clean up execution artifacts for fresh start (handles both new and old structure)
					if err := hcpo.cleanupExecutionArtifactsForFreshStart(ctx, hcpo.GetWorkspacePath(), runMode); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %w", err)
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
				case "option5": // Resume from next incomplete step without human input
					startFromStep = nextIncompleteStep - 1 // Convert back to 0-based
					skipHumanInput = true
					hcpo.GetLogger().Infof("✅ User chose to resume from step %d without human input", nextIncompleteStep)
				case "option6": // Start from beginning without human input
					hcpo.GetLogger().Infof("🔄 User chose to start from beginning without human input, will reset progress and cleanup execution artifacts")
					// Delete existing progress
					if err := hcpo.deleteStepProgress(ctx); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %w", err)
					}
					// Get run mode from approved plan if available, otherwise default
					runMode := "use_same_run"
					if hcpo.approvedPlan != nil && hcpo.approvedPlan.RunMode != "" {
						runMode = hcpo.approvedPlan.RunMode
						hcpo.GetLogger().Infof("📁 Using run_mode from approved plan: %s", runMode)
					} else {
						hcpo.GetLogger().Infof("📁 No run_mode in approved plan, defaulting to 'use_same_run'")
					}
					// Clean up execution artifacts for fresh start (handles both new and old structure)
					if err := hcpo.cleanupExecutionArtifactsForFreshStart(ctx, hcpo.GetWorkspacePath(), runMode); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %w", err)
					}
					// Note: learnings/ folder is preserved - deleted manually only
					existingProgress = nil
					startFromStep = 0
					skipHumanInput = true
				}

				// Store fast execute mode and skip human input mode for use in execution loop
				hcpo.SetFastExecuteMode(fastExecuteMode, fastExecuteEndStep)
				hcpo.SetSkipHumanInput(skipHumanInput)
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
		// Initialize and save fresh progress file
		if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to initialize fresh progress: %w", err)
			// Continue anyway with in-memory progress
			existingProgress = &StepProgress{
				CompletedStepIndices: []int{},
				TotalSteps:           len(breakdownSteps),
			}
		} else {
			// Create in-memory progress object matching what was saved
			existingProgress = &StepProgress{
				CompletedStepIndices: []int{},
				TotalSteps:           len(breakdownSteps),
				LastUpdated:          time.Now(),
			}
		}
	}

	_, err = hcpo.runExecutionPhase(ctx, breakdownSteps, 1, existingProgress, startFromStep)
	if err != nil {
		return "", fmt.Errorf("execution phase failed: %w", err)
	}

	duration := time.Since(hcpo.GetStartTime())
	hcpo.GetLogger().Infof("✅ Human-controlled todo planning completed in %v", duration)

	return "Todo planning complete.", nil
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
		// Save JSON plan to file manually
		planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())

		// Create backup of existing plan.json before updating
		backupPath := fmt.Sprintf("%s/planning/plan_backup.json", hcpo.GetWorkspacePath())
		existingPlanContent, err := hcpo.ReadWorkspaceFile(ctx, planPath)
		if err == nil {
			// Existing plan.json found - create backup
			if err := hcpo.WriteWorkspaceFile(ctx, backupPath, existingPlanContent); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to create plan backup: %v (continuing anyway)", err)
			} else {
				hcpo.GetLogger().Infof("💾 Created backup of existing plan.json at %s", backupPath)
			}
		} else {
			// No existing plan.json (first time creation) - no backup needed
			hcpo.GetLogger().Infof("📝 No existing plan.json found - creating new plan (no backup needed)")
		}

		planJSON, err := json.MarshalIndent(planResponse, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal plan to JSON: %w", err)
		}

		if err := hcpo.WriteWorkspaceFile(ctx, planPath, string(planJSON)); err != nil {
			return nil, nil, fmt.Errorf("failed to save plan.json: %w", err)
		}

		// Note: Learning integration cache removal no longer needed - execution agent auto-discovers files

		hcpo.GetLogger().Infof("✅ JSON plan created successfully and saved to %s (%d steps, conversation has %d messages)", planPath, len(planResponse.Steps), len(updatedConversationHistory))
	} else {
		// UPDATE mode: Plan already saved by tools, just log
		hcpo.GetLogger().Infof("✅ Plan already saved by tools (%d steps, conversation has %d messages)", len(planResponse.Steps), len(updatedConversationHistory))
	}

	return planResponse, updatedConversationHistory, nil
}

// convertPlanStepsToTodoSteps converts PlanStep to TodoStep format
// Merges agent configs from step_config.json by step index matching
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

		// Convert FlexibleContextOutput to string for TodoStep
		todoSteps[i] = TodoStep{
			Title:               step.Title,
			Description:         step.Description,
			SuccessCriteria:     step.SuccessCriteria,
			ContextDependencies: step.ContextDependencies,
			ContextOutput:       step.ContextOutput.String(), // Convert FlexibleContextOutput to string
			HasLoop:             step.HasLoop,
			LoopCondition:       step.LoopCondition,
			MaxIterations:       step.MaxIterations,
			LoopDescription:     step.LoopDescription,
			AgentConfigs:        agentConfigs, // Merged from step_config.json (validation enforced for loops)
		}
	}
	return todoSteps
}

// resolveRunFolder determines which run folder to use based on the run mode
// Returns the selected run folder name (e.g., "initial", "2025-01-27-iteration-1", "2025-01-27-initial")
func (hcpo *HumanControlledTodoPlannerOrchestrator) resolveRunFolder(ctx context.Context, workspacePath, runMode string) (string, error) {
	runsPath := fmt.Sprintf("%s/runs", workspacePath)

	// Get current date for dated folders
	today := time.Now().Format("2006-01-02")

	// Default to "use_same_run" if runMode is empty
	if runMode == "" {
		runMode = "use_same_run"
		hcpo.GetLogger().Infof("📁 No run_mode specified, defaulting to 'use_same_run'")
	}

	switch runMode {
	case "use_same_run":
		// Check if runs directory exists
		exists, _ := hcpo.workspaceFileExists(ctx, runsPath)
		if !exists {
			// Create initial run folder
			selectedFolder := "initial"
			if err := hcpo.createRunFolderStructure(ctx, fmt.Sprintf("%s/%s", runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// List existing run folders
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err != nil || len(existingFolders) == 0 {
			// Create initial folder if none exist
			selectedFolder := "initial"
			if err := hcpo.createRunFolderStructure(ctx, fmt.Sprintf("%s/%s", runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// Return the latest folder (alphabetically sorted, so latest date/name)
		sort.Strings(existingFolders)
		return existingFolders[len(existingFolders)-1], nil

	case "create_new_runs_always":
		// Always create a new dated folder with incremental number
		counter := 1
		for {
			selectedFolder := fmt.Sprintf("%s-iteration-%d", today, counter)
			fullPath := fmt.Sprintf("%s/%s", runsPath, selectedFolder)

			exists, _ := hcpo.workspaceFileExists(ctx, fullPath)
			if !exists {
				if err := hcpo.createRunFolderStructure(ctx, fullPath); err != nil {
					return "", err
				}
				return selectedFolder, nil
			}
			counter++
		}

	case "create_new_run_once_daily":
		// Check if today's folder exists
		prefix := today + "-"
		existingFolders, _ := hcpo.listRunFolders(ctx, runsPath)

		// Look for today's folder
		for _, folder := range existingFolders {
			if strings.HasPrefix(folder, prefix) {
				hcpo.GetLogger().Infof("📁 Using existing today's run folder: %s", folder)
				return folder, nil
			}
		}

		// Create new folder for today
		selectedFolder := fmt.Sprintf("%s-initial", today)
		fullPath := fmt.Sprintf("%s/%s", runsPath, selectedFolder)
		if err := hcpo.createRunFolderStructure(ctx, fullPath); err != nil {
			return "", err
		}
		return selectedFolder, nil

	default:
		return "", fmt.Errorf("unknown run mode: %s", runMode)
	}
}

// workspaceFileExists checks if a file or directory exists in the workspace
func (hcpo *HumanControlledTodoPlannerOrchestrator) workspaceFileExists(ctx context.Context, path string) (bool, error) {
	// Try to read a .keep file to check if directory exists
	_, err := hcpo.ReadWorkspaceFile(ctx, fmt.Sprintf("%s/.keep", path))
	if err == nil {
		return true, nil
	}

	// Try to read the path directly (for files)
	_, err = hcpo.ReadWorkspaceFile(ctx, path)
	if err == nil {
		return true, nil
	}

	// Check if it exists by listing parent directory (for both files and directories)
	parent := filepath.Dir(path)
	filename := filepath.Base(path)

	// List files and directories in parent using BaseOrchestrator method
	items, err := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, parent)
	if err == nil {
		for _, item := range items {
			if item == filename {
				return true, nil
			}
		}
	}

	return false, nil
}

// listRunFolders lists existing run folder names
func (hcpo *HumanControlledTodoPlannerOrchestrator) listRunFolders(ctx context.Context, runsPath string) ([]string, error) {
	// Use BaseOrchestrator's ListWorkspaceDirectories function
	return hcpo.BaseOrchestrator.ListWorkspaceDirectories(ctx, runsPath)
}

// createRunFolderStructure creates the basic structure for a run folder
func (hcpo *HumanControlledTodoPlannerOrchestrator) createRunFolderStructure(ctx context.Context, runPath string) error {
	// Create .keep file to ensure directory is created
	keepFile := fmt.Sprintf("%s/.keep", runPath)
	if err := hcpo.WriteWorkspaceFile(ctx, keepFile, "# This file ensures the run folder exists"); err != nil {
		return fmt.Errorf("failed to create run folder: %w", err)
	}

	// The actual folder creation will happen when files are written
	hcpo.GetLogger().Infof("✅ Created run folder structure: %s", runPath)
	return nil
}

// determineRunFolderForCleanup determines which run folder will be used (if any) without creating it
// Returns: (runFolderName, shouldCleanSpecificFolder, error)
// - runFolderName: The folder name that will be used (empty if new folder will be created)
// - shouldCleanSpecificFolder: Whether we should clean a specific folder (true if reusing existing folder)
func (hcpo *HumanControlledTodoPlannerOrchestrator) determineRunFolderForCleanup(ctx context.Context, workspacePath, runMode string) (string, bool, error) {
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	today := time.Now().Format("2006-01-02")

	// Default to "use_same_run" if runMode is empty
	if runMode == "" {
		runMode = "use_same_run"
	}

	switch runMode {
	case "use_same_run":
		// Check if runs directory exists
		exists, _ := hcpo.workspaceFileExists(ctx, runsPath)
		if !exists {
			// Will create "initial" - no existing folder to clean
			return "", false, nil
		}

		// List existing run folders
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err != nil || len(existingFolders) == 0 {
			// Will create "initial" - no existing folder to clean
			return "", false, nil
		}

		// Will reuse the latest folder - should clean it
		sort.Strings(existingFolders)
		return existingFolders[len(existingFolders)-1], true, nil

	case "create_new_runs_always":
		// Always creates new folder - no specific folder to clean
		return "", false, nil

	case "create_new_run_once_daily":
		// Check if today's folder exists
		prefix := today + "-"
		existingFolders, _ := hcpo.listRunFolders(ctx, runsPath)

		// Look for today's folder
		for _, folder := range existingFolders {
			if strings.HasPrefix(folder, prefix) {
				// Will reuse today's folder - should clean it
				return folder, true, nil
			}
		}

		// Will create new folder for today - no existing folder to clean
		return "", false, nil

	default:
		return "", false, fmt.Errorf("unknown run mode: %s", runMode)
	}
}

// shouldAskDeleteOldProgress determines if we should ask the "Delete old progress" question
// Returns true only when we're reusing an existing folder that might have old progress
func (hcpo *HumanControlledTodoPlannerOrchestrator) shouldAskDeleteOldProgress(ctx context.Context, workspacePath, runMode string) bool {
	_, shouldClean, err := hcpo.determineRunFolderForCleanup(ctx, workspacePath, runMode)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to determine run folder for cleanup check: %v, defaulting to ask question", err)
		return true // Default to asking if we can't determine
	}
	return shouldClean
}

// cleanupExecutionArtifactsForFreshStart cleans execution and validation artifacts based on run mode
// This handles both new runs folder structure and old structure for backward compatibility
func (hcpo *HumanControlledTodoPlannerOrchestrator) cleanupExecutionArtifactsForFreshStart(ctx context.Context, workspacePath, runMode string) error {
	hcpo.GetLogger().Infof("🧹 Starting cleanup of execution artifacts for fresh start (run_mode: %s)", runMode)

	// Determine which run folder will be used (if any)
	runFolderName, shouldCleanSpecificFolder, err := hcpo.determineRunFolderForCleanup(ctx, workspacePath, runMode)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to determine run folder for cleanup: %v, will only clean old structure", err)
		shouldCleanSpecificFolder = false
	}

	// Clean specific run folder if we're reusing it
	if shouldCleanSpecificFolder && runFolderName != "" {
		hcpo.GetLogger().Infof("📁 Cleaning specific run folder: %s", runFolderName)
		runFolderPath := fmt.Sprintf("%s/runs/%s", workspacePath, runFolderName)

		// Clean execution directory in run folder
		executionDir := fmt.Sprintf("%s/execution", runFolderPath)
		if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory in run folder: %w", err)
		} else {
			hcpo.GetLogger().Infof("🗑️ Cleaned up execution directory in run folder: %s", executionDir)
		}

		// Clean validation directory in run folder
		validationDir := fmt.Sprintf("%s/validation", runFolderPath)
		if err := hcpo.CleanupDirectory(ctx, validationDir, "validation"); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to cleanup validation directory in run folder: %w", err)
		} else {
			hcpo.GetLogger().Infof("🗑️ Cleaned up validation directory in run folder: %s", validationDir)
		}
	} else {
		hcpo.GetLogger().Infof("📁 No specific run folder to clean (will create new folder or use new structure)")
	}

	// Always clean old structure for backward compatibility
	hcpo.GetLogger().Infof("🧹 Cleaning old structure for backward compatibility")

	// Clean old execution directory
	oldExecutionDir := fmt.Sprintf("%s/execution", workspacePath)
	if err := hcpo.CleanupDirectory(ctx, oldExecutionDir, "execution"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup old execution directory: %w", err)
	} else {
		hcpo.GetLogger().Infof("🗑️ Cleaned up old execution directory: %s", oldExecutionDir)
	}

	// Clean old validation directory
	oldValidationDir := fmt.Sprintf("%s/validation", workspacePath)
	if err := hcpo.CleanupDirectory(ctx, oldValidationDir, "validation"); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to cleanup old validation directory: %w", err)
	} else {
		hcpo.GetLogger().Infof("🗑️ Cleaned up old validation directory: %s", oldValidationDir)
	}

	hcpo.GetLogger().Infof("✅ Completed cleanup of execution artifacts for fresh start")
	return nil
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

	// Learning detail level is now configured per-step via AgentConfigs
	// Each step can specify its own learning detail level, defaults to "general" if not set
	hcpo.GetLogger().Infof("📝 Using per-step learning detail level configuration")

	// Run folder should already be resolved early (after plan approval)
	if hcpo.selectedRunFolder == "" {
		return nil, fmt.Errorf("run folder not resolved - this should have been set after plan approval")
	}
	hcpo.GetLogger().Infof("📁 Using resolved run folder: %s", hcpo.selectedRunFolder)

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
		maxRetryAttempts := 5
		var executionConversationHistory []llmtypes.MessageContent // Only used for learning agents after execution
		stepCompleted := false

		// Outer loop: Handle re-execution with human feedback
		for !stepCompleted {

			// Prepare template variables for this specific step with individual fields
			// RESOLVE VARIABLES: Replace {{VARS}} with actual values for execution
			// Execution agent workspace path includes run folder: workspacePath/runs/{selectedRunFolder}/execution
			runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
			learningsPath := fmt.Sprintf("%s/learnings", hcpo.GetWorkspacePath())
			templateVars := map[string]string{
				"StepTitle":           hcpo.resolveVariables(step.Title),
				"StepDescription":     hcpo.resolveVariables(step.Description),
				"StepSuccessCriteria": hcpo.resolveVariables(step.SuccessCriteria),
				"StepContextOutput":   hcpo.resolveVariables(step.ContextOutput),
				"WorkspacePath":       executionWorkspacePath, // Execution subdirectory (folder guard validates against this)
				"LearningsPath":       learningsPath,          // Learnings folder path for reading learning files and Python scripts
				"LearningAgentOutput": "",                     // Will be populated with learning agent's output
			}

			// LearningAgentOutput is now empty - execution agent auto-discovers learning files and scripts
			templateVars["LearningAgentOutput"] = ""

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
					executionAgent, err := hcpo.createExecutionAgent(ctx, "execution", i+1, iteration, agentName, step.AgentConfigs)
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

					// Check if validation is disabled for this step
					disableValidation := step.AgentConfigs != nil && step.AgentConfigs.DisableValidation
					if disableValidation {
						hcpo.GetLogger().Infof("⏭️ Validation disabled for step %d - auto-approving", i+1)
						// Auto-approve: create a success validation response
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: true,
							ExecutionStatus:      "COMPLETED",
							Reasoning:            "Validation disabled - step auto-approved",
						}
						if step.HasLoop {
							// For loop steps, mark condition as met when validation is disabled
							validationResponse.LoopConditionMet = true
							loopConditionMet = true
						}
					} else {
						// Always validate step execution
						hcpo.GetLogger().Infof("🔍 Validating step %d execution (attempt %d)", i+1, retryAttempt)

						// Reuse sanitized title from execution agent (already computed above)
						validationAgentName := fmt.Sprintf("step-%d-%s", i+1, sanitizedTitle)
						// Add loop iteration to validation agent name if in loop mode
						if step.HasLoop && loopIterationCount > 0 {
							validationAgentName = fmt.Sprintf("%s-loop-%d", validationAgentName, loopIterationCount)
						}
						validationAgent, err := hcpo.createValidationAgent(ctx, "validation", i+1, iteration, validationAgentName, step.AgentConfigs)
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to create validation agent for step %d: %v", i+1, err)
							if retryAttempt >= maxRetryAttempts {
								break // Exit retry loop - will proceed to human feedback
							}
							continue // Retry on next attempt
						}

						// Prepare validation template variables with individual fields
						// Use run folder path if available
						var validationWorkspacePath string
						if hcpo.selectedRunFolder != "" {
							validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
						} else {
							validationWorkspacePath = hcpo.GetWorkspacePath()
						}
						validationTemplateVars := map[string]string{
							"StepTitle":           step.Title,
							"StepDescription":     step.Description,
							"StepSuccessCriteria": step.SuccessCriteria,
							"StepContextOutput":   step.ContextOutput,
							"WorkspacePath":       validationWorkspacePath,
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
					}

					// If in loop mode, check loop condition instead of full validation
					if step.HasLoop {
						// Check loop condition from validation response
						if validationResponse.LoopConditionMet {
							hcpo.GetLogger().Infof("✅ Step %d loop condition met (iteration %d)", i+1, loopIterationCount)
							loopConditionMet = true

							// Run success learning when loop completes successfully (before breaking)
							// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
							isFastExecuteStep := hcpo.IsFastExecuteStep(i)
							// Check step-specific learning detail level
							isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning
							isLearningDetailLevelNone := false
							if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
								isLearningDetailLevelNone = true
							}
							isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
							hcpo.GetLogger().Infof("🔍 DEBUG: Step %d (loop) - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v (detailLevelNone=%v, stepDisabled=%v)", i+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, isFastExecuteStep, isLearningDisabled, isLearningDetailLevelNone, isLearningDisabledStep)
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

							// Check if learning should run after each loop iteration
							learningAfterLoopIteration := step.AgentConfigs != nil && step.AgentConfigs.LearningAfterLoopIteration
							if learningAfterLoopIteration {
								// Run learning after this loop iteration
								isFastExecuteStep := hcpo.IsFastExecuteStep(i)
								// Check step-specific learning detail level
								isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning
								isLearningDetailLevelNone := false
								if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
									isLearningDetailLevelNone = true
								}
								isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone

								if !isFastExecuteStep && !isLearningDisabled {
									hcpo.GetLogger().Infof("🧠 Running learning analysis after loop iteration %d for step %d", loopIterationCount, i+1)
									// Run learning even though condition not met (for iteration analysis)
									successLearningOutput, err := hcpo.runSuccessLearningPhase(ctx, i+1, len(breakdownSteps), &step, executionConversationHistory, validationResponse)
									if err != nil {
										hcpo.GetLogger().Warnf("⚠️ Learning phase failed after loop iteration %d for step %d: %v", loopIterationCount, i+1, err)
									} else {
										hcpo.GetLogger().Infof("✅ Learning analysis completed after loop iteration %d for step %d", loopIterationCount, i+1)
										// Append learning analysis to template vars for next iteration
										if successLearningOutput != "" {
											existingOutput := templateVars["LearningAgentOutput"]
											if existingOutput != "" {
												templateVars["LearningAgentOutput"] = existingOutput + "\n\n" + successLearningOutput
											} else {
												templateVars["LearningAgentOutput"] = successLearningOutput
											}
										}
									}
								}
							}

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
					// Check step-specific learning detail level
					isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning
					isLearningDetailLevelNone := false
					if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
						isLearningDetailLevelNone = true
					}
					isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
					hcpo.GetLogger().Infof("🔍 DEBUG: Step %d - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v (detailLevelNone=%v, stepDisabled=%v)", i+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, isFastExecuteStep, isLearningDisabled, isLearningDetailLevelNone, isLearningDisabledStep)
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
			// SKIP HUMAN INPUT MODE: Skip human feedback but keep learning enabled
			// NORMAL MODE & LOOP MODE: Always request human feedback before moving to next step
			isFastExecuteStep := hcpo.IsFastExecuteStep(i)
			isSkipHumanInput := hcpo.IsSkipHumanInput()
			hcpo.GetLogger().Infof("🔍 DEBUG: Step %d human feedback check - fastExecuteMode=%v, fastExecuteEndStep=%d, stepIndex=%d, isFastExecuteStep=%v, skipHumanInput=%v", i+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, i, isFastExecuteStep, isSkipHumanInput)
			var approved bool
			var feedback string

			// In fast execute mode or skip human input mode, always auto-approve without human feedback
			if isFastExecuteStep || isSkipHumanInput {
				if isFastExecuteStep {
					hcpo.GetLogger().Infof("⚡ Fast mode: Auto-approving step %d without human feedback (stepIndex=%d <= fastExecuteEndStep=%d)", i+1, i, hcpo.fastExecuteEndStep)
				} else {
					hcpo.GetLogger().Infof("⚡ Skip human input mode: Auto-approving step %d without human feedback (learning will still run)", i+1)
				}
				approved = true
				feedback = "" // No feedback in fast mode or skip human input mode
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

// runVariableExtractionPhase extracts variables from objective (with optional human feedback and existing variables for update mode)
// Returns: (manifest, templatedObjective, updatedConversationHistory, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) runVariableExtractionPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llmtypes.MessageContent, existingVariables *VariablesManifest) (*VariablesManifest, string, []llmtypes.MessageContent, error) {
	if existingVariables != nil {
		hcpo.GetLogger().Infof("🔍 Starting variable extraction in UPDATE mode (attempt %d)", iteration)
	} else {
		hcpo.GetLogger().Infof("🔍 Starting variable extraction from objective (attempt %d)", iteration)
	}

	// Create variable extraction agent
	extractionAgent, err := hcpo.createVariableExtractionAgent(ctx)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create variable extraction agent: %w", err)
	}

	// Prepare template variables
	extractionTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Add existing variables JSON if in update mode (similar to planning agent's ExistingPlanJSON)
	if existingVariables != nil {
		existingVariablesJSON, err := json.MarshalIndent(existingVariables, "", "  ")
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to marshal existing variables to JSON: %v", err)
		} else {
			extractionTemplateVars["ExistingVariablesJSON"] = string(existingVariablesJSON)
			hcpo.GetLogger().Infof("✅ Passing existing variables contents in template (UPDATE mode)")
		}
	}

	// Determine user message based on whether this is first attempt or revision
	// - For first attempt: Use "Extract variables..." instruction
	// - For revisions: Use human feedback if provided, otherwise use instruction
	var userMessage string
	if humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
		// Revision attempt: Use human feedback as user message
		userMessage = humanFeedback
		hcpo.GetLogger().Infof("📝 Using human feedback as user message for variable extraction (attempt %d)", iteration)
	} else {
		// First attempt: Use static instruction
		userMessage = "Extract variables from the objective and call submit_variable_extraction_response tool with the structured output."
		hcpo.GetLogger().Infof("📝 Using default instruction for variable extraction (attempt %d)", iteration)
	}

	// Execute variable extraction using structured output via tool
	extractionAgentTyped, ok := extractionAgent.(*VariableExtractionAgent)
	if !ok {
		return nil, "", nil, fmt.Errorf("failed to cast variable extraction agent to correct type")
	}

	manifest, updatedHistory, err := extractionAgentTyped.ExecuteStructured(ctx, extractionTemplateVars, conversationHistory, userMessage)
	if err != nil {
		// Check if this is a non-structured response error (text response instead of structured output)
		if agents.IsNonStructuredResponseError(err) {
			var nonStructuredErr *agents.NonStructuredResponseError
			if errors.As(err, &nonStructuredErr) {
				// Display the text response to the user and request feedback
				hcpo.GetLogger().Infof("📝 Variable extraction agent returned conversational text instead of structured output. Displaying to user for feedback.")

				// Generate unique request ID
				requestID := fmt.Sprintf("variable_extraction_text_response_%d_%d", iteration, time.Now().UnixNano())

				// Display the text response and request feedback
				approved, feedback, feedbackErr := hcpo.RequestHumanFeedback(
					ctx,
					requestID,
					"The variable extraction agent provided the following response instead of a structured output. Please provide feedback to help it generate a proper structured response:",
					nonStructuredErr.TextResponse,
					hcpo.getSessionID(),
					hcpo.getWorkflowID(),
				)

				if feedbackErr != nil {
					return nil, "", nil, fmt.Errorf("failed to request human feedback for variable extraction text response: %w", feedbackErr)
				}

				// If user approved (clicked Approve button), treat as no feedback and continue
				// Otherwise, use the feedback for next attempt
				if approved {
					hcpo.GetLogger().Infof("✅ User approved variable extraction text response, but no structured output was generated. This is unexpected - returning error.")
					return nil, "", nil, fmt.Errorf("variable extraction agent returned text response but user approved without providing feedback to generate structured output")
				}

				// User provided feedback - return a special error that the loop can detect and handle
				// Use a specific error prefix that the loop will recognize
				// The updated history from the agent's response is included so conversation continues properly
				feedbackError := fmt.Errorf("VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:%s", feedback)
				hcpo.GetLogger().Infof("🔄 [DEBUG] Returning feedback error from runVariableExtractionPhase: %s", feedbackError.Error())
				return nil, "", nonStructuredErr.UpdatedHistory, feedbackError
			}
		}
		// For other errors, return as-is
		return nil, "", nil, fmt.Errorf("variable extraction failed: %w", err)
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
	return manifest, manifest.Objective, updatedHistory, nil
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
	// Set folder guard paths: allow reads from workspace (read-only), writes only to variables
	baseWorkspacePath := hcpo.GetWorkspacePath()
	variablesPath := fmt.Sprintf("%s/variables", baseWorkspacePath)

	// Read from base workspace (to understand objective), write only to variables folder
	// Note: Using base workspace as read path allows reading from root, but we restrict writes to variables/
	readPaths := []string{baseWorkspacePath}
	writePaths := []string{variablesPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for variable extraction agent - Read paths: %v, Write paths: %v (variables automatically readable via writePaths)", readPaths, writePaths)

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
		return nil, fmt.Errorf("failed to create variable extraction agent: %w", err)
	}

	return agent, nil
}

// loadVariableValues loads runtime variable values from variables.json
// LoadVariableValues loads variable values from variables.json file
// Public method that accepts BaseOrchestrator, workspacePath, and runWorkspacePath as parameters
func LoadVariableValues(ctx context.Context, bo *orchestrator.BaseOrchestrator, workspacePath, runWorkspacePath string) (map[string]string, error) {
	// Try to load from run folder first (run-specific variables), then fallback to workspace default
	runVariablesPath := fmt.Sprintf("%s/variables/variables.json", runWorkspacePath)
	workspaceVariablesPath := fmt.Sprintf("%s/variables/variables.json", workspacePath)

	var variablesContent string
	var err error

	// Try run folder first
	variablesContent, err = bo.ReadWorkspaceFile(ctx, runVariablesPath)
	if err != nil {
		// Fallback to workspace folder
		variablesContent, err = bo.ReadWorkspaceFile(ctx, workspaceVariablesPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read variables.json from both locations: %w", err)
		}
		bo.GetLogger().Infof("📁 Loaded variables from workspace folder: %s", workspaceVariablesPath)
	} else {
		bo.GetLogger().Infof("📁 Loaded variables from runs folder: %s", runVariablesPath)
	}

	// Parse variables.json to get current values
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	// Load values into the variableValues map
	variableValues := make(map[string]string)
	for _, variable := range manifest.Variables {
		variableValues[variable.Name] = variable.Value
	}

	bo.GetLogger().Infof("✅ Loaded variable values from variables.json: %d variables", len(variableValues))
	return variableValues, nil
}

// loadVariableValues is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadVariableValues(ctx context.Context) error {
	variableValues, err := LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
	if err != nil {
		return err
	}
	hcpo.variableValues = variableValues
	return nil
}

// ResolveVariables replaces {{VARIABLE}} placeholders with actual values
// Public method that accepts variableValues as parameter
func ResolveVariables(text string, variableValues map[string]string) string {
	if variableValues == nil {
		return text // No variables to resolve
	}

	resolved := text
	for varName, varValue := range variableValues {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		resolved = strings.ReplaceAll(resolved, placeholder, varValue)
	}
	return resolved
}

// resolveVariables is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) resolveVariables(text string) string {
	return ResolveVariables(text, hcpo.variableValues)
}

// ResolveVariablesArray resolves variables in an array of strings
// Public method that accepts variableValues as parameter
func ResolveVariablesArray(arr []string, variableValues map[string]string) []string {
	if variableValues == nil {
		return arr // No variables to resolve
	}

	resolved := make([]string, len(arr))
	for i, item := range arr {
		resolved[i] = ResolveVariables(item, variableValues)
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

// FormatVariableNames formats the variables manifest into a human-readable string for agent prompts
// Public method that accepts manifest as parameter
func FormatVariableNames(manifest *VariablesManifest) string {
	if manifest == nil || len(manifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range manifest.Variables {
		builder.WriteString(fmt.Sprintf("- {{%s}} - %s\n", variable.Name, variable.Description))
	}
	return builder.String()
}

// formatVariableNames is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatVariableNames() string {
	return FormatVariableNames(hcpo.variablesManifest)
}

// FormatVariableValues formats the variables manifest with their actual values for agent prompts
// Public method that accepts manifest and variableValues as parameters
func FormatVariableValues(manifest *VariablesManifest, variableValues map[string]string) string {
	if manifest == nil || len(manifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range manifest.Variables {
		// Get the actual resolved value from variableValues map if available
		actualValue := variable.Value
		if variableValues != nil {
			if resolvedValue, exists := variableValues[variable.Name]; exists {
				actualValue = resolvedValue
			}
		}
		builder.WriteString(fmt.Sprintf("- {{%s}} = %s - %s\n", variable.Name, actualValue, variable.Description))
	}
	return builder.String()
}

// formatVariableValues is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatVariableValues() string {
	return FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues)
}

// runSuccessLearningPhase analyzes successful executions to capture best practices and improve plan.json
func (hcpo *HumanControlledTodoPlannerOrchestrator) runSuccessLearningPhase(ctx context.Context, stepNumber, totalSteps int, step *TodoStep, executionHistory []llmtypes.MessageContent, validationResponse *ValidationResponse) (string, error) {
	// Use step-specific learning detail level, default to "general" if not set
	learningDetailLevel := "general" // default
	if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = step.AgentConfigs.LearningDetailLevel
		hcpo.GetLogger().Infof("📝 Using step-specific learning detail level: '%s'", learningDetailLevel)
	} else {
		hcpo.GetLogger().Infof("📝 No step-specific learning detail level set, using default: 'general'")
	}

	// Skip learning if "none" is selected or learning is disabled
	if learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning) {
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
	successLearningAgent, err := hcpo.createSuccessLearningAgent(ctx, "success_learning", stepNumber, 1, successLearningAgentName, step.AgentConfigs)
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
	// Use step-specific learning detail level, default to "general" if not set
	learningDetailLevel := "general" // default
	if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel != "" {
		learningDetailLevel = step.AgentConfigs.LearningDetailLevel
		hcpo.GetLogger().Infof("📝 Using step-specific learning detail level: '%s'", learningDetailLevel)
	} else {
		hcpo.GetLogger().Infof("📝 No step-specific learning detail level set, using default: 'general'")
	}

	// Skip learning if "none" is selected or learning is disabled
	if learningDetailLevel == "none" || (step.AgentConfigs != nil && step.AgentConfigs.DisableLearning) {
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
	failureLearningAgent, err := hcpo.createFailureLearningAgent(ctx, "failure_learning", stepNumber, 1, failureLearningAgentName, step.AgentConfigs)
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
		return "", fmt.Errorf("failed to request plan change decision: %w", err)
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

// Agent creation methods - reuse from base orchestrator
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

func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from learnings (read-only) and execution (via writePaths), writes only to execution
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)

	// Only specify learnings in readPaths - execution is automatically readable since it's in writePaths
	readPaths := []string{learningsPath}
	writePaths := []string{executionWorkspacePath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard - Read paths: %v, Write paths: %v (execution automatically readable via writePaths)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ExecutionMaxTurns != nil {
		maxTurns = *stepConfig.ExecutionMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific execution max turns: %d", maxTurns)
	}

	// Determine LLM config: use step-specific if provided, otherwise use orchestrator default
	var llmConfig *orchestrator.LLMConfig
	if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ExecutionLLM.Provider,
			ModelID:        stepConfig.ExecutionLLM.ModelID,
			FallbackModels: []string{}, // Use empty fallback for step-specific configs
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific execution LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID)
	} else {
		llmConfig = hcpo.GetLLMConfig()
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Use step-specific servers/tools if provided, otherwise use orchestrator defaults
	if stepConfig != nil && len(stepConfig.SelectedServers) > 0 {
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Infof("🔧 Using step-specific execution servers: %v", stepConfig.SelectedServers)
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Infof("🔧 Using step-specific execution tools: %v", stepConfig.SelectedTools)
	}

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
	}

	// Create agent using provided factory function
	agent := NewHumanControlledTodoPlannerExecutionAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize execution agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools - filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Filter tools based on enabled categories and/or specific tools
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		if len(stepConfig.EnabledCustomTools) > 0 {
			hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d specific tools enabled: %v", len(toolsToRegister), stepConfig.EnabledCustomTools)
		} else {
			hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools from categories %v", len(toolsToRegister), stepConfig.EnabledCustomToolCategories)
		}
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		wrappedExecutors := hcpo.WrapWorkspaceToolsWithFolderGuard(executorsToUse)
		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
					)
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())
	}

	return agent, nil
}

// createValidationAgent creates a validation agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution (read-only) and validation (via writePaths), writes only to validation
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionPath := fmt.Sprintf("%s/execution", runWorkspacePath)
	validationPath := fmt.Sprintf("%s/validation", runWorkspacePath)

	// Only specify execution in readPaths - validation is automatically readable since it's in writePaths
	readPaths := []string{executionPath}
	writePaths := []string{validationPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for validation agent - Read paths: %v, Write paths: %v (validation automatically readable via writePaths)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ValidationMaxTurns != nil {
		maxTurns = *stepConfig.ValidationMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific validation max turns: %d", maxTurns)
	}

	// Determine LLM config: use step-specific if provided, otherwise use orchestrator default
	var llmConfig *orchestrator.LLMConfig
	if stepConfig != nil && stepConfig.ValidationLLM != nil && stepConfig.ValidationLLM.Provider != "" && stepConfig.ValidationLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ValidationLLM.Provider,
			ModelID:        stepConfig.ValidationLLM.ModelID,
			FallbackModels: []string{}, // Use empty fallback for step-specific configs
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific validation LLM: %s/%s", stepConfig.ValidationLLM.Provider, stepConfig.ValidationLLM.ModelID)
	} else {
		llmConfig = hcpo.GetLLMConfig()
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Validation agents always use NoServers (pure LLM validation agent)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM validation agent

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
	}

	// Create agent using provided factory function
	agent := NewHumanControlledTodoPlannerValidationAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize validation agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools - filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Filter tools based on enabled categories and/or specific tools
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		if len(stepConfig.EnabledCustomTools) > 0 {
			hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d specific tools enabled: %v", len(toolsToRegister), stepConfig.EnabledCustomTools)
		} else {
			hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools from categories %v", len(toolsToRegister), stepConfig.EnabledCustomToolCategories)
		}
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		wrappedExecutors := hcpo.WrapWorkspaceToolsWithFolderGuard(executorsToUse)
		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
					)
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())
	}

	return agent, nil
}

// Note: Learning integration functions removed - execution agent now auto-discovers learning files and scripts

// createSuccessLearningAgent creates a success learning agent for analyzing successful executions
func (hcpo *HumanControlledTodoPlannerOrchestrator) createSuccessLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution and learnings (read-only), writes only to learnings
	baseWorkspacePath := hcpo.GetWorkspacePath()
	executionPath := fmt.Sprintf("%s/execution", baseWorkspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)

	// Only specify execution in readPaths - learnings is automatically readable since it's in writePaths
	readPaths := []string{executionPath}
	writePaths := []string{learningsPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for success learning agent - Read paths: %v, Write paths: %v (learnings automatically readable via writePaths)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.LearningMaxTurns != nil {
		maxTurns = *stepConfig.LearningMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific learning max turns: %d", maxTurns)
	}

	// Determine LLM config: use step-specific if provided, otherwise use orchestrator default
	var llmConfig *orchestrator.LLMConfig
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: []string{}, // Use empty fallback for step-specific configs
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID)
	} else {
		llmConfig = hcpo.GetLLMConfig()
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Learning agents always use NoServers (pure LLM analysis agent)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM analysis agent

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
	}

	// Create agent using provided factory function
	agent := NewHumanControlledTodoPlannerSuccessLearningAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgentWithCustomServers)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize success learning agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools - filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Filter tools based on enabled categories and/or specific tools
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		if len(stepConfig.EnabledCustomTools) > 0 {
			hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d specific tools enabled: %v", len(toolsToRegister), stepConfig.EnabledCustomTools)
		} else {
			hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools from categories %v", len(toolsToRegister), stepConfig.EnabledCustomToolCategories)
		}
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		wrappedExecutors := hcpo.WrapWorkspaceToolsWithFolderGuard(executorsToUse)
		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
					)
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())
	}

	return agent, nil
}

// createFailureLearningAgent creates a failure learning agent for analyzing failed executions
// Note: This now uses the unified learning agent which handles both success and failure cases
func (hcpo *HumanControlledTodoPlannerOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution and learnings (read-only), writes only to learnings
	baseWorkspacePath := hcpo.GetWorkspacePath()
	executionPath := fmt.Sprintf("%s/execution", baseWorkspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)

	// Only specify execution in readPaths - learnings is automatically readable since it's in writePaths
	readPaths := []string{executionPath}
	writePaths := []string{learningsPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for failure learning agent - Read paths: %v, Write paths: %v (learnings automatically readable via writePaths)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.LearningMaxTurns != nil {
		maxTurns = *stepConfig.LearningMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific learning max turns: %d", maxTurns)
	}

	// Determine LLM config: use step-specific if provided, otherwise use orchestrator default
	var llmConfig *orchestrator.LLMConfig
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: []string{}, // Use empty fallback for step-specific configs
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID)
	} else {
		llmConfig = hcpo.GetLLMConfig()
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Learning agents always use NoServers (pure LLM analysis agent)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM analysis agent

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
	}

	// Create agent using provided factory function
	agent := NewHumanControlledTodoPlannerLearningAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgentWithCustomServers)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize failure learning agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools - filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Filter tools based on enabled categories and/or specific tools
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		if len(stepConfig.EnabledCustomTools) > 0 {
			hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d specific tools enabled: %v", len(toolsToRegister), stepConfig.EnabledCustomTools)
		} else {
			hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools from categories %v", len(toolsToRegister), stepConfig.EnabledCustomToolCategories)
		}
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		wrappedExecutors := hcpo.WrapWorkspaceToolsWithFolderGuard(executorsToUse)
		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
					)
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())
	}

	return agent, nil
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

// EmitVariablesExtractedEvent emits an event when variables are extracted from objective
// Public method that accepts BaseOrchestrator and other parameters
func EmitVariablesExtractedEvent(ctx context.Context, bo *orchestrator.BaseOrchestrator, variables []Variable, templatedObjective, runFolder, workspacePath string) {
	if bo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &VariablesExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Variables:          variables,
		TemplatedObjective: templatedObjective,
		WorkspacePath:      workspacePath,
		RunFolder:          runFolder,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.VariablesExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := bo.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit variables extracted event: %w", err)
	} else {
		bo.GetLogger().Infof("✅ Emitted variables extracted event: %d variables", len(variables))
	}
}

// emitVariablesExtractedEvent is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitVariablesExtractedEvent(ctx context.Context, variables []Variable, templatedObjective string) {
	// Use default workspace path from orchestrator
	EmitVariablesExtractedEvent(ctx, hcpo.BaseOrchestrator, variables, templatedObjective, "", hcpo.GetWorkspacePath())
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

// SetSkipHumanInput sets the skip human input mode (runs learning but skips human feedback)
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetSkipHumanInput(enabled bool) {
	hcpo.skipHumanInput = enabled
}

// IsSkipHumanInput checks if human feedback should be skipped
func (hcpo *HumanControlledTodoPlannerOrchestrator) IsSkipHumanInput() bool {
	return hcpo.skipHumanInput
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
		return false, nil, fmt.Errorf("failed to check existing variables: %w", err)
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
