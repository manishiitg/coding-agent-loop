package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/shared"
	orchestratorllm "mcp-agent/agent_go/pkg/orchestrator/llm"
)

// BranchStepProgress tracks branch execution progress for conditional steps
type BranchStepProgress struct {
	BranchExecuted string   `json:"branch_executed"` // "if_true" or "if_false"
	CompletedSteps []string `json:"completed_steps"` // e.g., ["step-3-if-true-0", "step-3-if-true-1"]
}

// StepProgress tracks which steps have been completed
type StepProgress struct {
	CompletedStepIndices []int                      `json:"completed_step_indices"` // 0-based indices
	TotalSteps           int                        `json:"total_steps"`
	LastUpdated          time.Time                  `json:"last_updated"`
	BranchSteps          map[int]BranchStepProgress `json:"branch_steps,omitempty"` // key is step index (0-based)
}

// TodoStep represents a todo step in the execution
type TodoStep struct {
	ID                       string   `json:"id"` // Stable step ID (from PlanStep) - required for frontend matching
	Title                    string   `json:"title"`
	Description              string   `json:"description"`
	SuccessCriteria          string   `json:"success_criteria"`
	ContextDependencies      []string `json:"context_dependencies"`
	ContextOutput            string   `json:"context_output"`
	LearningFilesToReference []string `json:"learning_files_to_reference,omitempty"` // learning files to read for context (execution agent reads full files)
	HasLoop                  bool     `json:"has_loop"`                              // true if step needs to loop
	LoopCondition            string   `json:"loop_condition"`                        // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations            int      `json:"max_iterations,omitempty"`              // max iterations (default: 10)
	LoopDescription          string   `json:"loop_description,omitempty"`            // human-readable explanation
	// Conditional branching fields
	HasCondition      bool          `json:"has_condition"`                // true if step has conditional branches
	ConditionQuestion string        `json:"condition_question,omitempty"` // question to ask ConditionalLLM
	ConditionContext  string        `json:"condition_context,omitempty"`  // context to provide to ConditionalLLM
	IfTrueSteps       []TodoStep    `json:"if_true_steps,omitempty"`      // nested steps for true branch
	IfFalseSteps      []TodoStep    `json:"if_false_steps,omitempty"`     // nested steps for false branch
	ConditionResult   *bool         `json:"condition_result,omitempty"`   // runtime: stores decision result
	ConditionReason   string        `json:"condition_reason,omitempty"`   // runtime: stores LLM reasoning
	AgentConfigs      *AgentConfigs `json:"agent_configs,omitempty"`      // per-agent configuration (LLM, max turns, toggles)
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
	variablesManifest *VariablesManifest // Extracted variables
	variableValues    map[string]string  // Runtime variable values
	variableManager   *VariableManager   // Variable manager for variable extraction operations (independent from controller)

	// Fast execute mode tracking
	fastExecuteMode    bool // Whether we're in fast execute mode
	fastExecuteEndStep int  // Last step index to fast execute (0-based)

	// Skip human input mode tracking (runs learning but skips human feedback)
	skipHumanInput bool // Whether to skip human feedback requests (auto-approve steps)

	// Learning detail level preference (set once before execution, used for all learning phases)
	learningDetailLevel string // "exact" or "general"

	// Approved plan storage
	approvedPlan *PlanningResponse // Store approved plan

	// Run folder management
	selectedRunFolder string // Selected run folder name (e.g., "iteration-same", "iteration-1", "iteration-2")
	selectedRunMode   string // Selected run mode (e.g., "use_same_run", "create_new_runs_always")

	// Conditional LLM for conditional step evaluation
	conditionalLLM *orchestratorllm.ConditionalLLM

	// Preset-level agent defaults (used when step config doesn't specify)
	presetExecutionLLM          *AgentLLMConfig // Default for execution agents
	presetValidationLLM         *AgentLLMConfig // Default for validation agents
	presetLearningLLM           *AgentLLMConfig // Default for learning agents
	presetPlanningLLM           *AgentLLMConfig // Default for planning agent
	presetVariableExtractionLLM *AgentLLMConfig // Default for variable extraction agent
	presetAnonymizationLLM      *AgentLLMConfig // Default for anonymization agent
	presetPlanImprovementLLM    *AgentLLMConfig // Default for plan improvement agent
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
	presetExecutionLLM *AgentLLMConfig, // Optional preset default for execution agents
	presetValidationLLM *AgentLLMConfig, // Optional preset default for validation agents
	presetLearningLLM *AgentLLMConfig, // Optional preset default for learning agents
	presetPlanningLLM *AgentLLMConfig, // Optional preset default for planning agent
	presetVariableExtractionLLM *AgentLLMConfig, // Optional preset default for variable extraction agent
	presetAnonymizationLLM *AgentLLMConfig, // Optional preset default for anonymization agent
	presetPlanImprovementLLM *AgentLLMConfig, // Optional preset default for plan improvement agent
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

	// Create ConditionalLLM for conditional step evaluation
	// Get LLM config from orchestrator to preserve API keys from frontend
	orchestratorLLMConfig := baseOrchestrator.GetLLMConfig()
	conditionalLLMConfig := &agents.OrchestratorAgentConfig{
		Provider:    provider,
		Model:       model,
		Temperature: temperature,
		MaxRetries:  3,
	}
	// Preserve API keys from orchestrator LLM config (sent from frontend)
	if orchestratorLLMConfig != nil && orchestratorLLMConfig.APIKeys != nil {
		conditionalLLMConfig.APIKeys = &agents.AgentAPIKeys{
			OpenRouter: orchestratorLLMConfig.APIKeys.OpenRouter,
			OpenAI:     orchestratorLLMConfig.APIKeys.OpenAI,
			Anthropic:  orchestratorLLMConfig.APIKeys.Anthropic,
			Vertex:     orchestratorLLMConfig.APIKeys.Vertex,
		}
		if orchestratorLLMConfig.APIKeys.Bedrock != nil {
			conditionalLLMConfig.APIKeys.Bedrock = &agents.BedrockAgentConfig{
				Region: orchestratorLLMConfig.APIKeys.Bedrock.Region,
			}
		}
		logger.Infof("🔑 Preserved API keys for conditional LLM from orchestrator config")
	}
	conditionalLLM, err := orchestratorllm.CreateConditionalLLMWithEventBridge(
		conditionalLLMConfig,
		eventBridge,
		logger,
		tracer,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional LLM: %w", err)
	}

	hcpo := &HumanControlledTodoPlannerOrchestrator{
		BaseOrchestrator:            baseOrchestrator,
		sessionID:                   fmt.Sprintf("session_%d", time.Now().UnixNano()),
		workflowID:                  fmt.Sprintf("workflow_%d", time.Now().UnixNano()),
		conditionalLLM:              conditionalLLM,
		presetExecutionLLM:          presetExecutionLLM,
		presetValidationLLM:         presetValidationLLM,
		presetLearningLLM:           presetLearningLLM,
		presetPlanningLLM:           presetPlanningLLM,
		presetVariableExtractionLLM: presetVariableExtractionLLM,
		presetAnonymizationLLM:      presetAnonymizationLLM,
		presetPlanImprovementLLM:    presetPlanImprovementLLM,
	}

	// Create VariableManager for variable extraction operations (independent from controller)
	hcpo.variableManager = NewVariableManager(
		baseOrchestrator,
		presetVariableExtractionLLM,
		hcpo.sessionID,
		hcpo.workflowID,
	)

	return hcpo, nil
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

	// Backward compatibility: initialize BranchSteps if nil (old files won't have this field)
	if progress.BranchSteps == nil {
		progress.BranchSteps = make(map[int]BranchStepProgress)
		hcpo.GetLogger().Infof("📝 Initialized BranchSteps for backward compatibility")
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
		BranchSteps:          make(map[int]BranchStepProgress),
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
	// Check if variables.json exists - REQUIRED for planning
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := hcpo.variableManager.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing variables: %w", err)
	}
	if !variablesExist {
		return "", fmt.Errorf("variables.json not found at %s - variable extraction must be run first as a separate phase", variablesPath)
	}

	// Variables exist - use them
	hcpo.variablesManifest = existingVariablesManifest // Store in orchestrator so formatVariableNames/Values can access it
	templatedObjective := existingVariablesManifest.Objective

	// Check if plan.json exists - REQUIRED for execution
	planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
	planExists, existingPlan, err := hcpo.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing plan: %w", err)
	}
	if !planExists {
		return "", fmt.Errorf("plan.json not found at %s - planning must be run first as a separate phase", planPath)
	}

	// Plan exists - use it
	hcpo.GetLogger().Infof("📋 Found existing plan.json at %s with %d steps", planPath, len(existingPlan.Steps))

	// Safety check: Ensure plan has steps
	if len(existingPlan.Steps) == 0 {
		hcpo.GetLogger().Errorf("❌ Existing plan has no steps")
		return "", fmt.Errorf("existing plan has no steps")
	}

	// Load runtime variable values if provided and switch to templated objective
	variableValues, err := LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to load variable values: %w", err)
	} else {
		hcpo.variableValues = variableValues
	}

	// Switch to templated objective for all subsequent phases
	hcpo.SetObjective(templatedObjective)
	hcpo.GetLogger().Infof("✅ Using templated objective with {{VARIABLES}}: %s", templatedObjective)

	// Emit both events together
	hcpo.GetLogger().Infof("📋 Found both existing variables.json and plan.json - emitting both events together")
	hcpo.variableManager.emitVariablesExtractedEvent(ctx, existingVariablesManifest.Variables, existingVariablesManifest.Objective)

	// Convert existing plan to TodoStep format and emit TodoStepsExtractedEvent
	breakdownSteps := hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
	hcpo.GetLogger().Infof("✅ Converted existing plan: %d steps extracted", len(breakdownSteps))
	hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")

	// Store approved plan for access during execution
	hcpo.approvedPlan = existingPlan

	// Note: Learning integration phase removed - execution agent now auto-discovers learning files and scripts

	// Ask for run mode FIRST (before checking progress)
	// This allows user to select which run folder to use before we check for existing progress
	hcpo.GetLogger().Infof("📁 Asking for run mode selection before checking progress")

	// First, ask for run mode
	runModeRequestID := fmt.Sprintf("run_mode_selection_%d", time.Now().UnixNano())
	runModeOptions := []string{
		"Use Same Run",   // Option 0: use_same_run
		"Create New Run", // Option 1: create_new_runs_always
	}

	runModeChoice, err := hcpo.RequestMultipleChoiceFeedback(
		ctx,
		runModeRequestID,
		"Which run mode would you like to use for this execution?",
		runModeOptions,
		"Run mode determines how execution folders are organized:\n- Use Same Run: Reuses an existing run folder (you'll be asked to select which one)\n- Create New Run: Creates a new folder for this execution",
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for run mode: %w, defaulting to 'use_same_run'", err)
		runModeChoice = "option0" // Default to use_same_run
	}

	// Map choice to run mode value
	var selectedRunMode string
	switch runModeChoice {
	case "option0": // Use Same Run
		selectedRunMode = "use_same_run"
		hcpo.GetLogger().Infof("✅ User chose run mode: use_same_run")
	case "option1": // Create New Run
		selectedRunMode = "create_new_runs_always"
		hcpo.GetLogger().Infof("✅ User chose run mode: create_new_runs_always")
	default:
		hcpo.GetLogger().Warnf("⚠️ Unknown run mode choice: %s, defaulting to 'use_same_run'", runModeChoice)
		selectedRunMode = "use_same_run"
	}

	// Store selected run mode and resolve run folder with it
	hcpo.selectedRunMode = selectedRunMode
	selectedRunFolder, err := hcpo.resolveRunFolder(ctx, hcpo.GetWorkspacePath(), selectedRunMode)
	if err != nil {
		return "", fmt.Errorf("failed to resolve run folder with selected run mode: %w", err)
	}
	hcpo.selectedRunFolder = selectedRunFolder
	hcpo.GetLogger().Infof("📁 Resolved run folder with selected run mode: %s", selectedRunFolder)

	// EARLY PROGRESS CHECK: Check if all steps are already completed before proceeding
	// This prevents running execution unnecessarily if all steps are done
	// Now we check progress from the selected run folder
	hcpo.GetLogger().Infof("🔍 Early progress check: Checking if all steps are already completed in folder: %s", selectedRunFolder)
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

			// Use selected run mode (or default if not set yet)
			runMode := hcpo.selectedRunMode
			if runMode == "" {
				runMode = "use_same_run"
				hcpo.selectedRunMode = runMode
			}
			hcpo.GetLogger().Infof("📁 Using selected run mode: %s", runMode)

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

	// Ask for execution options when starting fresh (no existing progress)
	// Run mode was already selected earlier, so we only need to ask for execution mode
	if existingProgress == nil && startFromStep == 0 {
		hcpo.GetLogger().Infof("🆕 Starting fresh execution - asking for execution options")

		// Ask for execution mode
		execRequestID := fmt.Sprintf("fresh_start_execution_mode_%d", time.Now().UnixNano())
		execOptions := []string{
			"Start from Beginning",               // Option 0: Normal execution
			"Fast Execute all steps",             // Option 1: Fast execute all steps
			"Start from Beginning without Human", // Option 2: Skip human feedback
		}

		execChoice, err := hcpo.RequestMultipleChoiceFeedback(
			ctx,
			execRequestID,
			fmt.Sprintf("How would you like to execute the %d steps?", len(breakdownSteps)),
			execOptions,
			"Execution mode determines how steps are executed:\n- Start from Beginning: Normal execution with learning and human feedback\n- Fast Execute all steps: Skips learning and human feedback for faster execution\n- Start from Beginning without Human: Runs learning but auto-approves steps",
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for execution mode: %w, defaulting to normal execution", err)
			execChoice = "option0" // Default to normal execution
		}

		// Track fast execute mode and skip human input mode
		fastExecuteMode := false
		fastExecuteEndStep := -1
		skipHumanInput := false

		switch execChoice {
		case "option0": // Start from beginning (normal execution)
			hcpo.GetLogger().Infof("✅ User chose normal execution from beginning")
			// No changes needed - defaults are correct
		case "option1": // Fast execute all steps
			hcpo.GetLogger().Infof("⚡ User chose fast execute mode for all steps")
			fastExecuteMode = true
			fastExecuteEndStep = len(breakdownSteps) - 1 // Fast execute all steps
			hcpo.GetLogger().Infof("⚡ Will fast execute all steps (0 to %d)", fastExecuteEndStep)
		case "option2": // Start from beginning without human input
			hcpo.GetLogger().Infof("⚡ User chose to start from beginning without human input")
			skipHumanInput = true
		default:
			hcpo.GetLogger().Warnf("⚠️ Unknown choice: %s, defaulting to normal execution", execChoice)
			// Defaults are already set
		}

		// Store fast execute mode and skip human input mode for use in execution loop
		hcpo.SetFastExecuteMode(fastExecuteMode, fastExecuteEndStep)
		hcpo.SetSkipHumanInput(skipHumanInput)
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

			// Use selected run mode (or default if not set yet)
			runMode := hcpo.selectedRunMode
			if runMode == "" {
				runMode = "use_same_run"
				hcpo.selectedRunMode = runMode
			}
			hcpo.GetLogger().Infof("📁 Using selected run mode: %s", runMode)

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
					// Check if step has partial branch progress (conditional step with incomplete branches)
					if !completed && existingProgress.BranchSteps != nil {
						if branchProgress, hasBranchProgress := existingProgress.BranchSteps[i]; hasBranchProgress {
							// Step has branch progress but not completed - check if all branch steps are done
							// For now, treat as incomplete if step is not in CompletedStepIndices
							// This allows resuming from conditional steps with partial branch completion
							hcpo.GetLogger().Infof("🔍 Step %d has branch progress (branch=%s, completed_steps=%d) but not marked as completed - will resume", i+1, branchProgress.BranchExecuted, len(branchProgress.CompletedSteps))
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
					// Use selected run mode (or default if not set yet)
					runMode := hcpo.selectedRunMode
					if runMode == "" {
						runMode = "use_same_run"
						hcpo.selectedRunMode = runMode
					}
					hcpo.GetLogger().Infof("📁 Using selected run mode: %s", runMode)
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
					// Use selected run mode (or default if not set yet)
					runMode := hcpo.selectedRunMode
					if runMode == "" {
						runMode = "use_same_run"
						hcpo.selectedRunMode = runMode
					}
					hcpo.GetLogger().Infof("📁 Using selected run mode: %s", runMode)
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
				BranchSteps:          make(map[int]BranchStepProgress),
			}
		} else {
			// Create in-memory progress object matching what was saved
			existingProgress = &StepProgress{
				CompletedStepIndices: []int{},
				TotalSteps:           len(breakdownSteps),
				LastUpdated:          time.Now(),
				BranchSteps:          make(map[int]BranchStepProgress),
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

// resolveRunFolder determines which run folder to use based on the run mode
// Returns the selected run folder name (e.g., "iteration-same", "iteration-1", "iteration-2")
func (hcpo *HumanControlledTodoPlannerOrchestrator) resolveRunFolder(ctx context.Context, workspacePath, runMode string) (string, error) {
	runsPath := fmt.Sprintf("%s/runs", workspacePath)

	// Default to "use_same_run" if runMode is empty
	if runMode == "" {
		runMode = "use_same_run"
		hcpo.GetLogger().Infof("📁 No run_mode specified, defaulting to 'use_same_run'")
	}

	switch runMode {
	case "use_same_run":
		// Check if runs directory exists
		hcpo.GetLogger().Infof("🔍 DEBUG: Checking runs directory at: %s", runsPath)
		exists, _ := hcpo.workspaceFileExists(ctx, runsPath)
		hcpo.GetLogger().Infof("🔍 DEBUG: Runs directory exists: %v", exists)
		if !exists {
			// Create iteration-same run folder
			selectedFolder := "iteration-same"
			if err := hcpo.createRunFolderStructure(ctx, fmt.Sprintf("%s/%s", runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// List existing run folders
		hcpo.GetLogger().Infof("🔍 DEBUG: About to list run folders in: %s", runsPath)
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		hcpo.GetLogger().Infof("🔍 DEBUG: listRunFolders returned: folders=%v, error=%v", existingFolders, err)
		if err != nil || len(existingFolders) == 0 {
			// Create iteration-same folder if none exist
			selectedFolder := "iteration-same"
			if err := hcpo.createRunFolderStructure(ctx, fmt.Sprintf("%s/%s", runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// Separate "iteration-same" from iteration number folders and sort by iteration number
		var iterationSameFolder string
		var iterationFolders []string

		hcpo.GetLogger().Infof("🔍 DEBUG: Found %d existing folders: %v", len(existingFolders), existingFolders)

		for _, folder := range existingFolders {
			if folder == "iteration-same" {
				iterationSameFolder = folder
			} else {
				iterationFolders = append(iterationFolders, folder)
			}
		}

		hcpo.GetLogger().Infof("🔍 DEBUG: iteration-same=%s, iterationFolders count=%d: %v", iterationSameFolder, len(iterationFolders), iterationFolders)

		// Sort iteration folders by iteration number
		// Supports formats: "iteration-N", "YYYY-MM-DD-iteration-N", or "YYYY-MM-DD-initial"
		if len(iterationFolders) > 0 {
			sort.Slice(iterationFolders, func(i, j int) bool {
				// Extract iteration number from folder name
				extractIteration := func(name string) int {
					// Try to match "iteration-N" pattern (works for both "iteration-N" and "YYYY-MM-DD-iteration-N")
					re := regexp.MustCompile(`iteration-(\d+)$`)
					matches := re.FindStringSubmatch(name)
					if len(matches) > 1 {
						var num int
						if _, err := fmt.Sscanf(matches[1], "%d", &num); err == nil {
							return num
						}
					}
					// If "initial" or no match, treat as 0 (lowest priority)
					if strings.HasSuffix(name, "-initial") {
						return 0
					}
					return -1 // Unknown format, put at end
				}

				iterI := extractIteration(iterationFolders[i])
				iterJ := extractIteration(iterationFolders[j])

				// Sort by iteration number (descending - highest iteration first)
				if iterI != iterJ {
					return iterI > iterJ
				}
				// If same iteration number, sort alphabetically
				return iterationFolders[i] > iterationFolders[j]
			})
		}

		// Build folder options: always include "iteration-same" first if it exists, then up to 10 iteration folders
		folderOptions := make([]string, 0)
		if iterationSameFolder != "" {
			folderOptions = append(folderOptions, iterationSameFolder)
		}

		// Add up to 10 iteration folders (or all if less than 10)
		if len(iterationFolders) > 0 {
			maxIterationFolders := 10
			if len(iterationFolders) < maxIterationFolders {
				maxIterationFolders = len(iterationFolders)
			}
			folderOptions = append(folderOptions, iterationFolders[:maxIterationFolders]...)
		}

		hcpo.GetLogger().Infof("🔍 DEBUG: folderOptions count=%d: %v", len(folderOptions), folderOptions)

		// If only one folder exists, use it directly
		if len(folderOptions) == 1 {
			hcpo.GetLogger().Infof("📁 Using the only existing run folder: %s", folderOptions[0])
			return folderOptions[0], nil
		}

		// Multiple folders exist - ask user to select which one to use
		hcpo.GetLogger().Infof("📁 Found %d existing run folders, presenting %d options to user", len(existingFolders), len(folderOptions))

		// Ask user to select which run folder to use
		requestID := fmt.Sprintf("select_run_folder_%d", time.Now().UnixNano())

		// Build appropriate question based on number of folders
		var questionText string
		if len(existingFolders) == 1 {
			questionText = "Which run folder would you like to use?"
		} else if len(folderOptions) == len(existingFolders) {
			questionText = fmt.Sprintf("Found %d run folders. Which one would you like to use?", len(existingFolders))
		} else {
			questionText = fmt.Sprintf("Found %d run folders (showing %d most recent). Which one would you like to use?", len(existingFolders), len(folderOptions))
		}

		contextMsg := fmt.Sprintf("Found %d existing run folder(s). Which one would you like to use?\n\n", len(existingFolders))
		if len(existingFolders) > len(folderOptions) {
			contextMsg += fmt.Sprintf("**Note:** Showing %d most recent folders (sorted by iteration number).\n\n", len(folderOptions))
		}
		contextMsg += "**Available folders:**\n"
		for _, folder := range folderOptions {
			contextMsg += fmt.Sprintf("- %s\n", folder)
		}

		choice, err := hcpo.RequestMultipleChoiceFeedback(
			ctx,
			requestID,
			questionText,
			folderOptions,
			contextMsg,
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user selection for run folder: %w, defaulting to first option", err)
			// Default to first option (iteration-same if exists, otherwise highest iteration)
			return folderOptions[0], nil
		}

		// Parse the choice (format: "option0", "option1", etc.)
		// Extract the index from the choice string
		var selectedIndex int
		if _, err := fmt.Sscanf(choice, "option%d", &selectedIndex); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to parse folder choice '%s': %w, defaulting to first option", choice, err)
			return folderOptions[0], nil
		}

		// Validate index
		if selectedIndex < 0 || selectedIndex >= len(folderOptions) {
			hcpo.GetLogger().Warnf("⚠️ Invalid folder index %d (max: %d), defaulting to first option", selectedIndex, len(folderOptions)-1)
			return folderOptions[0], nil
		}

		selectedFolder := folderOptions[selectedIndex]
		hcpo.GetLogger().Infof("✅ User selected run folder: %s", selectedFolder)
		return selectedFolder, nil

	case "create_new_runs_always":
		// Always create a new iteration folder with incremental number
		counter := 1
		for {
			selectedFolder := fmt.Sprintf("iteration-%d", counter)
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

	// Default to "use_same_run" if runMode is empty
	if runMode == "" {
		runMode = "use_same_run"
	}

	switch runMode {
	case "use_same_run":
		// Check if runs directory exists
		exists, _ := hcpo.workspaceFileExists(ctx, runsPath)
		if !exists {
			// Will create "iteration-same" - no existing folder to clean
			return "", false, nil
		}

		// List existing run folders
		existingFolders, err := hcpo.listRunFolders(ctx, runsPath)
		if err != nil || len(existingFolders) == 0 {
			// Will create "iteration-same" - no existing folder to clean
			return "", false, nil
		}

		// Will reuse the latest folder - should clean it
		sort.Strings(existingFolders)
		return existingFolders[len(existingFolders)-1], true, nil

	case "create_new_runs_always":
		// Always creates new folder - no specific folder to clean
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

// executeConditionalStep executes a conditional step by evaluating the condition and executing the chosen branch
// depth: current nesting depth (0 = main plan, 1 = first level conditional, 2 = second level conditional)
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeConditionalStep(
	ctx context.Context,
	step TodoStep,
	stepIndex int,
	depth int,
	progress *StepProgress,
	previousContextFiles []string, // Context files from previous steps
	iteration int, // Current iteration number
) error {
	const maxDepth = 2
	if depth > maxDepth {
		return fmt.Errorf("nesting depth %d exceeds maximum allowed depth of %d", depth, maxDepth)
	}

	hcpo.GetLogger().Infof("🔀 Executing conditional step %d (depth %d): %s", stepIndex+1, depth, step.Title)

	// Check for existing branch progress
	var existingBranchProgress *BranchStepProgress
	var conditionResult bool
	var conditionReason string
	var resumeFromBranchStep int = 0 // 0 means start from beginning
	var updatedContextFiles []string // Context files from conditional step execution (if executed)

	if progress.BranchSteps == nil {
		progress.BranchSteps = make(map[int]BranchStepProgress)
	}

	if branchProgress, exists := progress.BranchSteps[stepIndex]; exists {
		existingBranchProgress = &branchProgress
		hcpo.GetLogger().Infof("📋 Found existing branch progress for step %d: branch=%s, completed_steps=%d", stepIndex+1, branchProgress.BranchExecuted, len(branchProgress.CompletedSteps))
		// Use stored branch execution result
		conditionResult = (branchProgress.BranchExecuted == "if_true")
		conditionReason = fmt.Sprintf("Resuming from saved branch progress: %s", branchProgress.BranchExecuted)
		hcpo.GetLogger().Infof("✅ Using stored branch execution: %s (result=%t, reason: %s)", branchProgress.BranchExecuted, conditionResult, conditionReason)

		// Determine which branch steps to execute based on stored branch
		var branchStepsToCheck []TodoStep
		if conditionResult {
			branchStepsToCheck = step.IfTrueSteps
		} else {
			branchStepsToCheck = step.IfFalseSteps
		}

		// Find first incomplete branch step
		for branchIdx := range branchStepsToCheck {
			branchStepPath := fmt.Sprintf("step-%d-%s-%d", stepIndex+1, branchProgress.BranchExecuted, branchIdx)
			completed := false
			for _, completedPath := range branchProgress.CompletedSteps {
				if completedPath == branchStepPath {
					completed = true
					break
				}
			}
			if !completed {
				resumeFromBranchStep = branchIdx
				hcpo.GetLogger().Infof("🔍 Resuming from branch step %d (path: %s)", branchIdx, branchStepPath)
				break
			}
		}
	} else {
		// No existing branch progress - execute conditional step and evaluate condition
		// First, execute the conditional step itself to get execution result
		stepPath := fmt.Sprintf("step-%d-conditional", stepIndex+1)
		conditionalExecutionResult, updatedContextFiles, err := hcpo.executeSingleStep(
			ctx,
			step,
			stepIndex,
			stepPath,
			1, // totalSteps = 1 for conditional step itself
			iteration,
			previousContextFiles,
			progress,
			false, // isBranchStep = false (conditional step is a main step)
		)
		if err != nil {
			hcpo.GetLogger().Errorf("❌ Failed to execute conditional step %d: %v", stepIndex+1, err)
			return fmt.Errorf("failed to execute conditional step: %w", err)
		}

		hcpo.GetLogger().Infof("✅ Conditional step execution completed, evaluating condition based on execution result")

		// Build context for ConditionalLLM
		contextBuilder := strings.Builder{}

		// Add execution result from the conditional step
		contextBuilder.WriteString("Current Step Execution Result:\n")
		contextBuilder.WriteString(conditionalExecutionResult)
		contextBuilder.WriteString("\n\n")

		// Add condition context if provided
		if step.ConditionContext != "" {
			contextBuilder.WriteString("Condition Context:\n")
			contextBuilder.WriteString(step.ConditionContext)
			contextBuilder.WriteString("\n\n")
		}

		// Add context from previous step outputs (using updated context files from step execution)
		if len(updatedContextFiles) > 0 {
			contextBuilder.WriteString("Previous Step Context Files:\n")
			for _, contextFile := range updatedContextFiles {
				// Try to read the context file
				runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
				executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
				contextFilePath := filepath.Join(executionWorkspacePath, contextFile)

				content, err := hcpo.ReadWorkspaceFile(ctx, contextFilePath)
				if err == nil {
					contextBuilder.WriteString(fmt.Sprintf("- %s:\n%s\n\n", contextFile, content))
				} else {
					contextBuilder.WriteString(fmt.Sprintf("- %s: (file not found or error reading)\n", contextFile))
				}
			}
		}

		conditionContext := contextBuilder.String()

		// Evaluate condition using ConditionalLLM
		hcpo.GetLogger().Infof("🤔 Evaluating condition for step %d (depth %d): %s", stepIndex+1, depth, step.ConditionQuestion)
		hcpo.GetLogger().Infof("📋 Condition context length: %d characters", len(conditionContext))

		conditionalResponse, err := hcpo.conditionalLLM.Decide(ctx, conditionContext, step.ConditionQuestion, stepIndex, 0)
		if err != nil {
			hcpo.GetLogger().Errorf("❌ Failed to evaluate condition for step %d: %v", stepIndex+1, err)
			// Emit error event if event bridge is available
			eventBridge := hcpo.GetContextAwareBridge()
			if eventBridge != nil {
				errorEvent := &events.OrchestratorAgentErrorEvent{
					BaseEventData: events.BaseEventData{
						Timestamp: time.Now(),
					},
					AgentType: "conditional",
					AgentName: "conditional-step-evaluation",
					Objective: fmt.Sprintf("Evaluate condition: %s", step.ConditionQuestion),
					Error:     err.Error(),
					StepIndex: stepIndex,
					Iteration: 0,
				}
				eventBridge.HandleEvent(ctx, &events.AgentEvent{
					Type:      events.OrchestratorAgentError,
					Timestamp: time.Now(),
					Data:      errorEvent,
				})
			}
			return fmt.Errorf("failed to evaluate condition: %w", err)
		}

		// Store result
		conditionResult = conditionalResponse.Result
		conditionReason = conditionalResponse.Reason

		hcpo.GetLogger().Infof("✅ Condition evaluated for step %d: result=%t, reason=%s", stepIndex+1, conditionResult, conditionReason)

		// Initialize branch progress
		branchExecuted := "if_false"
		if conditionResult {
			branchExecuted = "if_true"
		}
		progress.BranchSteps[stepIndex] = BranchStepProgress{
			BranchExecuted: branchExecuted,
			CompletedSteps: []string{},
		}
		hcpo.GetLogger().Infof("📝 Initialized branch progress for step %d: branch=%s", stepIndex+1, branchExecuted)
	}

	// Log decision details
	hcpo.GetLogger().Infof("📊 Conditional decision details - Step: %s, Question: %s, Result: %t, Depth: %d",
		step.Title, step.ConditionQuestion, conditionResult, depth)

	// Determine which branch to execute
	var branchSteps []TodoStep
	if conditionResult {
		branchSteps = step.IfTrueSteps
		hcpo.GetLogger().Infof("📋 Executing TRUE branch with %d steps", len(branchSteps))
	} else {
		branchSteps = step.IfFalseSteps
		hcpo.GetLogger().Infof("📋 Executing FALSE branch with %d steps", len(branchSteps))
	}

	// Track context files for branch steps
	branchContextFiles := make([]string, 0)
	if existingBranchProgress == nil {
		// New execution - use updated context files from conditional step execution
		branchContextFiles = append(branchContextFiles, updatedContextFiles...)
	} else {
		// Resuming - use previous context files (from previousContextFiles parameter)
		branchContextFiles = append(branchContextFiles, previousContextFiles...)
	}

	// Add conditional step's context output to branch context files if it exists
	if step.ContextOutput != "" {
		branchContextFiles = append(branchContextFiles, step.ContextOutput)
	}

	// Get branch executed string for path generation
	branchExecutedStr := map[bool]string{true: "if-true", false: "if-false"}[conditionResult]

	// Execute each step in the chosen branch
	for branchIdx, branchStep := range branchSteps {
		// Skip if resuming and this branch step is already completed
		if branchIdx < resumeFromBranchStep {
			hcpo.GetLogger().Infof("⏭️ Skipping branch step %d/%d (already completed): %s", branchIdx+1, len(branchSteps), branchStep.Title)
			continue
		}

		// Check if branch step is already completed (for resume case)
		branchStepPath := fmt.Sprintf("step-%d-%s-%d", stepIndex+1, branchExecutedStr, branchIdx)
		if existingBranchProgress != nil {
			completed := false
			for _, completedPath := range existingBranchProgress.CompletedSteps {
				if completedPath == branchStepPath {
					completed = true
					break
				}
			}
			if completed {
				hcpo.GetLogger().Infof("⏭️ Skipping branch step %d/%d (marked as completed): %s", branchIdx+1, len(branchSteps), branchStep.Title)
				continue
			}
		}

		hcpo.GetLogger().Infof("📋 Executing branch step %d/%d (depth %d): %s", branchIdx+1, len(branchSteps), depth+1, branchStep.Title)

		// Check if branch step is conditional (nested conditional)
		if branchStep.HasCondition {
			// Recursively execute nested conditional step
			hcpo.GetLogger().Infof("🔀 Executing nested conditional step in branch: %s (depth %d)", branchStep.Title, depth+1)
			if err := hcpo.executeConditionalStep(ctx, branchStep, stepIndex, depth+1, progress, branchContextFiles, iteration); err != nil {
				hcpo.GetLogger().Errorf("❌ Failed to execute nested conditional step '%s' at depth %d: %v", branchStep.Title, depth+1, err)
				return fmt.Errorf("failed to execute nested conditional step '%s': %w", branchStep.Title, err)
			}
			hcpo.GetLogger().Infof("✅ Completed nested conditional step: %s", branchStep.Title)
		} else {
			// Execute regular branch step using extracted execution logic
			branchExecutionResult, updatedBranchContextFiles, err := hcpo.executeSingleStep(
				ctx,
				branchStep,
				stepIndex, // Use parent step index for now
				branchStepPath,
				len(branchSteps), // Total steps in branch
				iteration,
				branchContextFiles,
				progress,
				true, // isBranchStep = true
			)
			if err != nil {
				hcpo.GetLogger().Errorf("❌ Failed to execute branch step '%s': %v", branchStep.Title, err)
				return fmt.Errorf("failed to execute branch step '%s': %w", branchStep.Title, err)
			}

			// Track branch step completion
			branchProgress := progress.BranchSteps[stepIndex]
			branchProgress.CompletedSteps = append(branchProgress.CompletedSteps, branchStepPath)
			progress.BranchSteps[stepIndex] = branchProgress
			// Save progress after each branch step completion
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to save branch step progress: %w", err)
			} else {
				hcpo.GetLogger().Infof("💾 Saved branch step progress: %s completed", branchStepPath)
			}

			// Update context files with branch step's output
			branchContextFiles = updatedBranchContextFiles

			hcpo.GetLogger().Infof("✅ Completed branch step: %s (execution result length: %d chars)", branchStep.Title, len(branchExecutionResult))
		}
	}

	// Verify all branch steps are completed
	branchProgress := progress.BranchSteps[stepIndex]
	expectedBranchSteps := len(branchSteps)
	completedBranchSteps := len(branchProgress.CompletedSteps)
	if completedBranchSteps < expectedBranchSteps {
		hcpo.GetLogger().Warnf("⚠️ Conditional step %d: only %d/%d branch steps completed", stepIndex+1, completedBranchSteps, expectedBranchSteps)
		// Don't mark as completed - will resume from incomplete branch steps
	} else {
		hcpo.GetLogger().Infof("✅ All %d branch steps completed for conditional step %d", expectedBranchSteps, stepIndex+1)
	}

	hcpo.GetLogger().Infof("✅ Completed conditional step %d: executed %s branch", stepIndex+1, map[bool]string{true: "TRUE", false: "FALSE"}[conditionResult])
	return nil
}

// executeSingleStep executes a single step with full functionality (execution, validation, learning, human feedback)
// This is a reusable function extracted from runExecutionPhase to support both regular steps and branch steps
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeSingleStep(
	ctx context.Context,
	step TodoStep,
	stepIndex int,
	stepPath string, // e.g., "step-1" or "step-1-if-true-0" for branch steps
	totalSteps int,
	iteration int,
	previousContextFiles []string,
	progress *StepProgress,
	isBranchStep bool, // true if this is a branch step (affects progress tracking)
) (executionResult string, updatedContextFiles []string, err error) {
	// Initialize updated context files as copy of previous context files
	updatedContextFiles = make([]string, len(previousContextFiles))
	copy(updatedContextFiles, previousContextFiles)

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
			"StepTitle":           ResolveVariables(step.Title, hcpo.variableValues),
			"StepDescription":     ResolveVariables(step.Description, hcpo.variableValues),
			"StepSuccessCriteria": ResolveVariables(step.SuccessCriteria, hcpo.variableValues),
			"StepContextOutput":   ResolveVariables(step.ContextOutput, hcpo.variableValues),
			"WorkspacePath":       executionWorkspacePath, // Execution subdirectory (folder guard validates against this)
			"LearningsPath":       learningsPath,          // Learnings folder path for reading learning files and Python scripts
		}

		// Add context dependencies as a comma-separated string (also resolve variables)
		if len(step.ContextDependencies) > 0 {
			resolvedDeps := ResolveVariablesArray(step.ContextDependencies, hcpo.variableValues)
			templateVars["StepContextDependencies"] = strings.Join(resolvedDeps, ", ")
		} else {
			templateVars["StepContextDependencies"] = ""
		}

		// Add variable names if available (same format as other agents)
		if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
			templateVars["VariableNames"] = variableNames
		}

		// Add variable values if available (name = value - description format)
		if variableValues := FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues); variableValues != "" {
			templateVars["VariableValues"] = variableValues
		}

		// Validate loop condition is provided when has_loop is true
		if step.HasLoop {
			if step.LoopCondition == "" {
				return "", updatedContextFiles, fmt.Errorf("step %d has has_loop=true but loop_condition is empty (required)", stepIndex+1)
			}
			// Set default max_iterations if not provided
			if step.MaxIterations == 0 {
				step.MaxIterations = 10
				hcpo.GetLogger().Infof("⚠️ Step %d has loop but no max_iterations specified, using default: 10", stepIndex+1)
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
				hcpo.GetLogger().Infof("🔄 Step %d loop starting (max iterations: %d, condition: %s)", stepIndex+1, step.MaxIterations, step.LoopCondition)
			} else if loopIteration > 0 && step.HasLoop {
				// Previous iteration outputs are passed via template variables (PreviousIterationOutput)
				// Execution conversation history will be captured fresh from this iteration for learning agents
				hcpo.GetLogger().Infof("🔄 Step %d loop iteration %d/%d starting", stepIndex+1, loopIterationCount, step.MaxIterations)
			}

			// Check loop exit conditions (only for loop steps)
			if step.HasLoop {
				if loopConditionMet {
					hcpo.GetLogger().Infof("✅ Step %d loop condition met after %d iterations, exiting loop", stepIndex+1, loopIterationCount)
					// Skip validation, mark as completed
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            fmt.Sprintf("Loop condition met after %d iterations. Validation skipped per loop exit.", loopIterationCount),
					}
					break // Exit main loop - proceed to mark as completed
				}
				if loopIterationCount >= step.MaxIterations {
					hcpo.GetLogger().Errorf("❌ Step %d reached max iterations (%d) without meeting loop condition, requesting human intervention", stepIndex+1, step.MaxIterations)
					// Request human intervention immediately, skip validation
					var err error
					var approved bool
					approved, _, err = hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps,
						fmt.Sprintf("Loop reached max iterations (%d) without meeting condition: %s", step.MaxIterations, step.LoopCondition))
					if err != nil {
						hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %w", err)
						// Default to not approved so step doesn't complete
						approved = false
					}
					if approved {
						// User approved - treat as completed despite max iterations
						hcpo.GetLogger().Infof("✅ User approved step %d despite max iterations, marking as completed", stepIndex+1)
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: true,
							ExecutionStatus:      "COMPLETED",
							Reasoning:            "User approved completion despite max iterations reached",
						}
						loopConditionMet = true // Mark condition as met so loop exits
						break                   // Exit main loop
					} else {
						// User rejected - will re-execute step
						hcpo.GetLogger().Infof("🔄 User rejected approval, will re-execute step %d", stepIndex+1)
						break // Exit main loop; outer loop will re-execute since stepCompleted is still false
					}
				}
				loopIterationCount++
				hcpo.GetLogger().Infof("🔄 Step %d loop iteration %d/%d", stepIndex+1, loopIterationCount, step.MaxIterations)
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
					hcpo.GetLogger().Infof("📝 Added previous iteration outputs to template variables for step %d (loop iteration %d)", stepIndex+1, loopIterationCount)
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
				hcpo.GetLogger().Infof("🔄 Executing step %d/%d (attempt %d/%d): %s", stepIndex+1, totalSteps, retryAttempt, maxRetryAttempts, step.Title)

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
					hcpo.GetLogger().Infof("📝 Added validation feedback to template variables for step %d (retry: %d, loop iteration: %d)", stepIndex+1, retryAttempt, loopIterationCount)
				} else {
					templateVars["ValidationFeedback"] = "" // No validation feedback for first attempt/first iteration
				}

				// Create execution agent for this step
				// Resolve variables in step title before using in agent name
				resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
				sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)
				agentName := fmt.Sprintf("%s-%s", stepPath, sanitizedTitle)
				// Add loop iteration to agent name if in loop mode
				if step.HasLoop && loopIterationCount > 0 {
					agentName = fmt.Sprintf("%s-loop-%d", agentName, loopIterationCount)
				}
				executionAgent, err := hcpo.createExecutionAgent(ctx, "execution", stepIndex+1, iteration, agentName, step.AgentConfigs)
				if err != nil {
					return "", updatedContextFiles, fmt.Errorf("failed to create execution agent for step %d: %w", stepIndex+1, err)
				}

				// Execute this specific step - no conversation history needed, all context in template variables
				// Capture execution result and conversation history (conversation history for learning agents)
				executionResult, executionConversationHistory, err = executionAgent.Execute(ctx, templateVars, []llmtypes.MessageContent{})
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Step %d execution failed (attempt %d): %v", stepIndex+1, retryAttempt, err)
					if retryAttempt >= maxRetryAttempts {
						hcpo.GetLogger().Errorf("❌ Step %d execution failed after %d attempts, exiting retry loop", stepIndex+1, maxRetryAttempts)
						break // Exit retry loop - will proceed to human feedback
					}
					continue // Retry on next attempt
				}

				hcpo.GetLogger().Infof("✅ Step %d execution completed successfully (attempt %d)", stepIndex+1, retryAttempt)

				// Check if validation is disabled for this step
				disableValidation := step.AgentConfigs != nil && step.AgentConfigs.DisableValidation
				if disableValidation {
					hcpo.GetLogger().Infof("⏭️ Validation disabled for step %d - auto-approving", stepIndex+1)
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
					hcpo.GetLogger().Infof("🔍 Validating step %d execution (attempt %d)", stepIndex+1, retryAttempt)

					// Reuse sanitized title from execution agent (already computed above)
					validationAgentName := fmt.Sprintf("%s-%s", stepPath, sanitizedTitle)
					// Add loop iteration to validation agent name if in loop mode
					if step.HasLoop && loopIterationCount > 0 {
						validationAgentName = fmt.Sprintf("%s-loop-%d", validationAgentName, loopIterationCount)
					}
					validationAgent, err := hcpo.createValidationAgent(ctx, "validation", stepIndex+1, iteration, validationAgentName, step.AgentConfigs)
					if err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to create validation agent for step %d: %v", stepIndex+1, err)
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
						hcpo.GetLogger().Infof("🔍 Checking loop condition for step %d (iteration %d): %s", stepIndex+1, loopIterationCount, step.LoopCondition)
					} else {
						validationTemplateVars["LoopCondition"] = ""
					}

					// Validate this step's execution using structured output
					validationResponse, _, err = validationAgent.(*HumanControlledTodoPlannerValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llmtypes.MessageContent{})
					if err != nil {
						hcpo.GetLogger().Warnf("⚠️ Step %d validation failed (attempt %d): %v", stepIndex+1, retryAttempt, err)
						if retryAttempt >= maxRetryAttempts {
							break // Exit retry loop - will proceed to human feedback with nil validationResponse
						}
						continue // Retry on next attempt
					}

					hcpo.GetLogger().Infof("✅ Step %d validation completed successfully (attempt %d)", stepIndex+1, retryAttempt)
					hcpo.GetLogger().Infof("📊 Validation result: Success Criteria Met: %v, Status: %s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
				}

				// If in loop mode, check loop condition instead of full validation
				if step.HasLoop {
					// Check loop condition from validation response
					if validationResponse.LoopConditionMet {
						hcpo.GetLogger().Infof("✅ Step %d loop condition met (iteration %d)", stepIndex+1, loopIterationCount)
						loopConditionMet = true

						// Run success learning when loop completes successfully (before breaking)
						// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
						isFastExecuteStep := hcpo.IsFastExecuteStep(stepIndex)
						// Check step-specific learning detail level
						isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning
						isLearningDetailLevelNone := false
						if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
							isLearningDetailLevelNone = true
						}
						isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
						hcpo.GetLogger().Infof("🔍 DEBUG: Step %d (loop) - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v (detailLevelNone=%v, stepDisabled=%v)", stepIndex+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, isFastExecuteStep, isLearningDisabled, isLearningDetailLevelNone, isLearningDisabledStep)
						if !isFastExecuteStep && !isLearningDisabled {
							// Success Learning Agent - analyze what worked well and update plan.json
							// Loop condition met means step completed successfully
							hcpo.GetLogger().Infof("🧠 Running success learning analysis for step %d (loop completed)", stepIndex+1)
							_, err := hcpo.runSuccessLearningPhase(ctx, stepIndex+1, totalSteps, &step, executionConversationHistory, validationResponse)
							if err != nil {
								hcpo.GetLogger().Warnf("⚠️ Success learning phase failed for step %d: %v", stepIndex+1, err)
							} else {
								hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", stepIndex+1)
							}
						} else {
							if isFastExecuteStep {
								hcpo.GetLogger().Infof("⚡ Fast mode: Skipping learning agents for step %d", stepIndex+1)
							} else if isLearningDisabled {
								hcpo.GetLogger().Infof("⏭️ Learning disabled: Skipping learning agents for step %d", stepIndex+1)
							}
						}

						break // Exit retry loop, will exit main loop at top
					} else {
						hcpo.GetLogger().Infof("🔄 Step %d loop condition not met yet (iteration %d/%d), continuing loop", stepIndex+1, loopIterationCount, step.MaxIterations)

						// Check if learning should run after each loop iteration
						learningAfterLoopIteration := step.AgentConfigs != nil && step.AgentConfigs.LearningAfterLoopIteration
						if learningAfterLoopIteration {
							// Run learning after this loop iteration
							isFastExecuteStep := hcpo.IsFastExecuteStep(stepIndex)
							// Check step-specific learning detail level
							isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning
							isLearningDetailLevelNone := false
							if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
								isLearningDetailLevelNone = true
							}
							isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone

							if !isFastExecuteStep && !isLearningDisabled {
								hcpo.GetLogger().Infof("🧠 Running learning analysis after loop iteration %d for step %d", loopIterationCount, stepIndex+1)
								// Run learning even though condition not met (for iteration analysis)
								_, err := hcpo.runSuccessLearningPhase(ctx, stepIndex+1, totalSteps, &step, executionConversationHistory, validationResponse)
								if err != nil {
									hcpo.GetLogger().Warnf("⚠️ Learning phase failed after loop iteration %d for step %d: %v", loopIterationCount, stepIndex+1, err)
								} else {
									hcpo.GetLogger().Infof("✅ Learning analysis completed after loop iteration %d for step %d", loopIterationCount, stepIndex+1)
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
				isFastExecuteStep := hcpo.IsFastExecuteStep(stepIndex)
				// Check step-specific learning detail level
				isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning
				isLearningDetailLevelNone := false
				if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
					isLearningDetailLevelNone = true
				}
				isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
				hcpo.GetLogger().Infof("🔍 DEBUG: Step %d - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v (detailLevelNone=%v, stepDisabled=%v)", stepIndex+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, isFastExecuteStep, isLearningDisabled, isLearningDetailLevelNone, isLearningDisabledStep)
				if isFastExecuteStep || isLearningDisabled {
					if isFastExecuteStep {
						hcpo.GetLogger().Infof("⚡ Fast mode: Skipping learning agents for step %d", stepIndex+1)
					} else if isLearningDisabled {
						hcpo.GetLogger().Infof("⏭️ Learning disabled: Skipping learning agents for step %d", stepIndex+1)
					}
				} else {
					// Run appropriate learning phase based on validation result
					if validationResponse.IsSuccessCriteriaMet {
						// Success Learning Agent - analyze what worked well and update plan.json
						hcpo.GetLogger().Infof("🧠 Running success learning analysis for step %d", stepIndex+1)
						_, err := hcpo.runSuccessLearningPhase(ctx, stepIndex+1, totalSteps, &step, executionConversationHistory, validationResponse)
						if err != nil {
							hcpo.GetLogger().Warnf("⚠️ Success learning phase failed for step %d: %v", stepIndex+1, err)
						} else {
							hcpo.GetLogger().Infof("✅ Success learning analysis completed for step %d", stepIndex+1)
						}
					} else {
						// Failure Learning Agent - analyze what went wrong and provide refined task description
						// SKIP failure learning for loop steps - loop steps only run success learning when condition is met
						if step.HasLoop {
							hcpo.GetLogger().Infof("🔄 Step %d is a loop step - skipping failure learning (loop steps only run success learning when condition is met)", stepIndex+1)
						} else {
							hcpo.GetLogger().Infof("🧠 Running failure learning analysis for step %d", stepIndex+1)
							refinedTaskDescription, _, err := hcpo.runFailureLearningPhase(ctx, stepIndex+1, totalSteps, &step, executionConversationHistory, validationResponse)
							if err != nil {
								hcpo.GetLogger().Warnf("⚠️ Failure learning phase failed for step %d: %v", stepIndex+1, err)
							} else {
								hcpo.GetLogger().Infof("✅ Failure learning analysis completed for step %d", stepIndex+1)

								// Update step description for retry
								if refinedTaskDescription != "" {
									step.Description = refinedTaskDescription
									templateVars["StepDescription"] = refinedTaskDescription
									hcpo.GetLogger().Infof("🔄 Updated step %d description with refined task for retry", stepIndex+1)
								}
							}
						}
					}
				}

				// Check if success criteria was met (only for non-loop steps or when loop handling is done)
				if !step.HasLoop {
					if validationResponse.IsSuccessCriteriaMet {
						hcpo.GetLogger().Infof("✅ Step %d passed validation - success criteria met", stepIndex+1)
						break // Exit retry loop and continue to next step
					} else {
						hcpo.GetLogger().Warnf("⚠️ Step %d failed validation - success criteria not met (attempt %d/%d)", stepIndex+1, retryAttempt, maxRetryAttempts)

						if retryAttempt >= maxRetryAttempts {
							hcpo.GetLogger().Errorf("❌ Step %d failed validation after %d attempts", stepIndex+1, maxRetryAttempts)
							// Continue to next step even if validation failed
							break
						} else {
							hcpo.GetLogger().Infof("🔄 Retrying step %d execution with validation feedback", stepIndex+1)
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
		isFastExecuteStep := hcpo.IsFastExecuteStep(stepIndex)
		isSkipHumanInput := hcpo.IsSkipHumanInput()
		hcpo.GetLogger().Infof("🔍 DEBUG: Step %d human feedback check - fastExecuteMode=%v, fastExecuteEndStep=%d, stepIndex=%d, isFastExecuteStep=%v, skipHumanInput=%v", stepIndex+1, hcpo.fastExecuteMode, hcpo.fastExecuteEndStep, stepIndex, isFastExecuteStep, isSkipHumanInput)
		var approved bool
		var feedback string

		// In fast execute mode or skip human input mode, always auto-approve without human feedback
		if isFastExecuteStep || isSkipHumanInput {
			if isFastExecuteStep {
				hcpo.GetLogger().Infof("⚡ Fast mode: Auto-approving step %d without human feedback (stepIndex=%d <= fastExecuteEndStep=%d)", stepIndex+1, stepIndex, hcpo.fastExecuteEndStep)
			} else {
				hcpo.GetLogger().Infof("⚡ Skip human input mode: Auto-approving step %d without human feedback (learning will still run)", stepIndex+1)
			}
			approved = true
			feedback = "" // No feedback in fast mode or skip human input mode
		} else {
			// Normal mode and loop mode: Request human feedback
			var validationSummary string
			if validationResponse != nil {
				validationSummary = fmt.Sprintf("Step %d validation completed. Success Criteria Met: %v, Status: %s", stepIndex+1, validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
			} else {
				validationSummary = fmt.Sprintf("Step %d execution failed - no validation response available", stepIndex+1)
			}
			var err error
			approved, feedback, err = hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps, validationSummary)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Human feedback request failed: %w", err)
				// Default to continue if feedback fails
				approved = true
			}
		}

		// Store human feedback for future steps (even if approved, user might have provided guidance)
		if feedback != "" {
			feedbackEntry := fmt.Sprintf("Step %d/%d Feedback: %s", stepIndex+1, totalSteps, feedback)
			// Note: humanFeedbackHistory is not available in this function scope, so we skip storing it
			// It will be handled by the caller if needed
			hcpo.GetLogger().Infof("📝 Human feedback received for step %d: %s", stepIndex+1, feedbackEntry)
		}

		if approved {
			// User approved - mark step as completed and exit outer loop
			// Only update progress if this is not a branch step
			if !isBranchStep {
				progress.CompletedStepIndices = append(progress.CompletedStepIndices, stepIndex)
				// Always save progress after marking a step as completed (both fast and normal mode)
				if err := hcpo.saveStepProgress(ctx, progress); err != nil {
					hcpo.GetLogger().Warnf("⚠️ Failed to save step progress: %w", err)
				} else {
					modeStr := "fast mode"
					if !isFastExecuteStep {
						modeStr = "normal mode"
					}
					hcpo.GetLogger().Infof("✅ Step %d/%d marked as completed and saved (%s) - Total completed: %d/%d", stepIndex+1, totalSteps, modeStr, len(progress.CompletedStepIndices), progress.TotalSteps)
				}

				// Emit step token usage summary
				stepTitle := step.Title
				if stepTitle == "" {
					stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
				}
				hcpo.EmitStepTokenUsage(ctx, "execution", stepIndex, stepTitle, false) // Don't clear - keep for potential future queries
			} else {
				hcpo.GetLogger().Infof("✅ Branch step %d completed (not updating main progress)", stepIndex+1)
			}
			stepCompleted = true
		} else {
			// User provided feedback (didn't approve) - stop workflow and ask user to manually update plan
			hcpo.GetLogger().Infof("🛑 User provided feedback - stopping workflow. Feedback: %s", feedback)
			planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
			return executionResult, updatedContextFiles, fmt.Errorf("workflow stopped: user feedback received. please manually update the plan at %s with the following feedback, then restart the workflow: %s", planPath, feedback)
		}
	} // End of outer loop for step execution

	// Append step's context output to context files if it exists
	if step.ContextOutput != "" {
		updatedContextFiles = append(updatedContextFiles, step.ContextOutput)
		hcpo.GetLogger().Infof("📝 Added step context output to context files: %s", step.ContextOutput)
	}

	return executionResult, updatedContextFiles, nil
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

		// Build context files from previous steps
		previousContextFiles := make([]string, 0)
		for prevIdx := 0; prevIdx < i; prevIdx++ {
			if prevIdx < len(breakdownSteps) && breakdownSteps[prevIdx].ContextOutput != "" {
				previousContextFiles = append(previousContextFiles, breakdownSteps[prevIdx].ContextOutput)
			}
		}

		// Check if this is a conditional step
		if step.HasCondition {
			// Execute conditional step
			hcpo.GetLogger().Infof("🔀 Starting conditional step execution: %s", step.Title)
			if err := hcpo.executeConditionalStep(ctx, step, i, 0, progress, previousContextFiles, iteration); err != nil {
				hcpo.GetLogger().Errorf("❌ Conditional step %d execution failed: %v", i+1, err)
				// Emit error event
				eventBridge := hcpo.GetContextAwareBridge()
				if eventBridge != nil {
					errorEvent := &events.OrchestratorAgentErrorEvent{
						BaseEventData: events.BaseEventData{
							Timestamp: time.Now(),
						},
						AgentType: "workflow",
						AgentName: "conditional-step-execution",
						Objective: fmt.Sprintf("Execute conditional step: %s", step.Title),
						Error:     err.Error(),
						StepIndex: i,
						Iteration: iteration,
					}
					eventBridge.HandleEvent(ctx, &events.AgentEvent{
						Type:      events.OrchestratorAgentError,
						Timestamp: time.Now(),
						Data:      errorEvent,
					})
				}
				return nil, fmt.Errorf("conditional step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Infof("✅ Conditional step %d completed successfully: %s", i+1, step.Title)

			// Mark conditional step as completed (executeConditionalStep handles progress internally)
			progress.CompletedStepIndices = append(progress.CompletedStepIndices, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to save progress after conditional step: %w", err)
			} else {
				hcpo.GetLogger().Infof("💾 Saved progress: conditional step %d marked as completed", i+1)
			}

			// Continue to next step
			continue
		}

		// Execute regular step using executeSingleStep
		stepPath := fmt.Sprintf("step-%d", i+1)
		executionResult, _, err := hcpo.executeSingleStep(
			ctx,
			step,
			i,
			stepPath,
			len(breakdownSteps),
			iteration,
			previousContextFiles,
			progress,
			false, // isBranchStep = false
		)
		if err != nil {
			hcpo.GetLogger().Errorf("❌ Step %d execution failed: %v", i+1, err)
			return nil, fmt.Errorf("step %d execution failed: %w", i+1, err)
		}

		// Log execution result (for debugging)
		hcpo.GetLogger().Infof("✅ Step %d execution completed (result length: %d chars)", i+1, len(executionResult))

		// Note: Progress tracking is handled inside executeSingleStep
		// Continue to next step
		continue
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

// OLD CODE REMOVED - The following section was replaced by executeSingleStep() function above
// All that logic has been extracted into executeSingleStep() for reusability

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
	resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
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
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
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
	resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
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
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
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

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ExecutionLLM.Provider,
			ModelID:        stepConfig.ExecutionLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific execution LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID)
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetExecutionLLM.Provider,
			ModelID:        hcpo.presetExecutionLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default execution LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default execution LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Use step-specific servers/tools if provided, otherwise use orchestrator defaults
	if stepConfig != nil && len(stepConfig.SelectedServers) > 0 {
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Infof("🔧 Using step-specific execution servers: %v", stepConfig.SelectedServers)
	} else if stepConfig != nil {
		// Log when stepConfig exists but SelectedServers is empty (will use orchestrator defaults)
		hcpo.GetLogger().Infof("🔧 Step config found but no SelectedServers specified - using orchestrator defaults")
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Infof("🔧 Using step-specific execution tools: %v", stepConfig.SelectedTools)
	} else if stepConfig != nil {
		// Log when stepConfig exists but SelectedTools is empty (will use orchestrator defaults)
		hcpo.GetLogger().Infof("🔧 Step config found but no SelectedTools specified - using orchestrator defaults")
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
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
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
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	// Priority: step config > preset default > orchestrator default
	if stepConfig != nil && stepConfig.ValidationLLM != nil && stepConfig.ValidationLLM.Provider != "" && stepConfig.ValidationLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ValidationLLM.Provider,
			ModelID:        stepConfig.ValidationLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific validation LLM: %s/%s", stepConfig.ValidationLLM.Provider, stepConfig.ValidationLLM.ModelID)
	} else if hcpo.presetValidationLLM != nil && hcpo.presetValidationLLM.Provider != "" && hcpo.presetValidationLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetValidationLLM.Provider,
			ModelID:        hcpo.presetValidationLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default validation LLM: %s/%s", hcpo.presetValidationLLM.Provider, hcpo.presetValidationLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default validation LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
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
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
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

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID)
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default learning LLM: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default learning LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
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
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
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

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID)
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default learning LLM: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default learning LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
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
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
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
