package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	orchestratorllm "mcp-agent/agent_go/pkg/orchestrator/llm"
	mcpagent "mcpagent/agent"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

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

	// Single step execution mode
	runSingleStepOnly bool // Whether to run only a single step and stop
	singleStepTarget  int  // Target step index to run (0-based)

	// Skip human input mode tracking (runs learning but skips human feedback)
	skipHumanInput bool // Whether to skip human feedback requests (auto-approve steps)

	// Learning detail level preference (set once before execution, used for all learning phases)
	learningDetailLevel string // "exact" or "general"

	// Approved plan storage
	approvedPlan *PlanningResponse // Store approved plan

	// Run folder management
	selectedRunFolder string // Selected run folder name (e.g., "iteration-1", "iteration-2")
	selectedRunMode   string // Selected run mode (e.g., "use_same_run", "create_new_runs_always")

	// Frontend-provided execution options (when provided, skips interactive prompts)
	executionOptions *ExecutionOptions

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
	useCodeExecutionMode bool, // NEW parameter
	mcpConfigPath string,
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	toolCategories map[string]string, // NEW: tool category map
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
		selectedTools,        // Pass through actual selected tools
		useCodeExecutionMode, // NEW: Pass code execution mode
		llmConfig,
		maxTurns,
		customTools,
		customToolExecutors,
		toolCategories, // NEW: Pass category map
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
		presetLearningLLM, // Pass learning LLM for fallback
		hcpo.sessionID,
		hcpo.workflowID,
	)

	return hcpo, nil
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
	breakdownSteps, err := hcpo.convertPlanStepsToTodoSteps(ctx, existingPlan.Steps)
	if err != nil {
		return "", fmt.Errorf("failed to convert existing plan steps: %w", err)
	}
	hcpo.GetLogger().Infof("✅ Converted existing plan: %d steps extracted", len(breakdownSteps))
	hcpo.emitTodoStepsExtractedEvent(ctx, breakdownSteps, "existing_plan")

	// Store approved plan for access during execution
	hcpo.approvedPlan = existingPlan

	// Note: Learning integration phase removed - execution agent now auto-discovers learning files and scripts

	// Check if execution options are provided from chfrontend
	execOpts := hcpo.executionOptions
	var selectedRunMode string
	var selectedRunFolder string

	if execOpts != nil {
		// ===== FRONTEND-PROVIDED EXECUTION OPTIONS =====
		// Use options from frontend, skip interactive prompts
		hcpo.GetLogger().Infof("📋 Using frontend-provided execution options")

		// Use run mode from options
		selectedRunMode = execOpts.RunMode
		if selectedRunMode == "" {
			selectedRunMode = "use_same_run" // Default
		}
		hcpo.selectedRunMode = selectedRunMode
		hcpo.GetLogger().Infof("📁 Using run mode from frontend: %s", selectedRunMode)

		// Resolve run folder with provided options
		var err error
		selectedRunFolder, err = hcpo.resolveRunFolderWithOptions(ctx, hcpo.GetWorkspacePath(), selectedRunMode, execOpts.SelectedRunFolder)
		if err != nil {
			return "", fmt.Errorf("failed to resolve run folder with frontend options: %w", err)
		}
		hcpo.selectedRunFolder = selectedRunFolder
		hcpo.GetLogger().Infof("📁 Resolved run folder: %s", selectedRunFolder)
	} else {
		// ===== INTERACTIVE MODE (no frontend options) =====
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
		selectedRunFolder, err = hcpo.resolveRunFolder(ctx, hcpo.GetWorkspacePath(), selectedRunMode)
		if err != nil {
			return "", fmt.Errorf("failed to resolve run folder with selected run mode: %w", err)
		}
		hcpo.selectedRunFolder = selectedRunFolder
		hcpo.GetLogger().Infof("📁 Resolved run folder with selected run mode: %s", selectedRunFolder)
	}

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
				hcpo.GetLogger().Infof("✅ ALL steps already completed")

				// Check if frontend provided action for all steps completed
				if execOpts != nil && execOpts.AllStepsCompletedAction != "" {
					// Use frontend-provided action
					switch execOpts.AllStepsCompletedAction {
					case AllStepsCompletedActionFastExecuteAgain:
						hcpo.GetLogger().Infof("⚡ Frontend chose to fast execute all steps again, clearing progress")
						if err := hcpo.deleteStepProgress(ctx); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to delete steps_done.json: %v (will continue anyway)", err)
						}
						hcpo.SetFastExecuteMode(true, len(breakdownSteps)-1)
						earlyProgress = nil
						hcpo.GetLogger().Infof("⚡ Will fast execute all steps (0 to %d)", len(breakdownSteps)-1)
					case AllStepsCompletedActionSkipExecution:
						hcpo.GetLogger().Infof("⏭️ Frontend chose to skip execution")
						return "Todo planning complete. All steps already executed.", nil
					default:
						hcpo.GetLogger().Warnf("⚠️ Unknown all_steps_completed_action: %s, defaulting to skip", execOpts.AllStepsCompletedAction)
						return "Todo planning complete. All steps already executed.", nil
					}
				} else {
					// Interactive mode - ask user
					hcpo.GetLogger().Infof("🤔 Asking user if they want to fast execute all steps again")
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
			}
			hcpo.GetLogger().Infof("📊 Not all steps completed yet - will proceed with execution")
		} else {
			// Plan changed - handle based on frontend options or ask user
			hcpo.GetLogger().Warnf("⚠️ Total steps changed (previous: %d, current: %d)",
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
				hcpo.GetLogger().Infof("📁 Run mode '%s' will create new folder - skipping 'Delete old progress' question", runMode)
				earlyProgress = nil
				planChangeHandled = true
			} else if execOpts != nil && execOpts.PlanChangeAction != "" {
				// Use frontend-provided action
				planChangeHandled = true
				switch execOpts.PlanChangeAction {
				case PlanChangeActionKeepOldProgress:
					hcpo.GetLogger().Infof("✅ Frontend chose to keep old progress (will try to match steps)")
					// Keep earlyProgress as-is
				case PlanChangeActionDeleteOldProgress:
					hcpo.GetLogger().Infof("🔄 Frontend chose to delete old progress and start fresh")
					if err := hcpo.deleteStepProgress(ctx); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %w", err)
					}
					if err := hcpo.cleanupExecutionArtifactsForFreshStart(ctx, hcpo.GetWorkspacePath(), runMode); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %w", err)
					}
					if err := hcpo.initializeFreshProgress(ctx, len(breakdownSteps)); err != nil {
						hcpo.GetLogger().Warnf("⚠️ Failed to initialize fresh progress: %w", err)
					}
					earlyProgress = nil
				default:
					hcpo.GetLogger().Warnf("⚠️ Unknown plan_change_action: %s, keeping old progress", execOpts.PlanChangeAction)
				}
			} else {
				// Interactive mode - ask user what to do
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
		hcpo.GetLogger().Infof("🆕 Starting fresh execution")

		// Track fast execute mode and skip human input mode
		fastExecuteMode := false
		fastExecuteEndStep := -1
		skipHumanInput := false

		// Check if frontend provided execution strategy
		if execOpts != nil && execOpts.ExecutionStrategy != "" {
			// Use frontend-provided execution strategy
			hcpo.GetLogger().Infof("📋 Using frontend-provided execution strategy: %s", execOpts.ExecutionStrategy)
			switch execOpts.ExecutionStrategy {
			case ExecutionStrategyStartFromBeginning:
				hcpo.GetLogger().Infof("✅ Frontend chose normal execution from beginning")
				// Defaults are correct
			case ExecutionStrategyFastExecuteAll:
				hcpo.GetLogger().Infof("⚡ Frontend chose fast execute mode for all steps")
				fastExecuteMode = true
				fastExecuteEndStep = len(breakdownSteps) - 1
				hcpo.GetLogger().Infof("⚡ Will fast execute all steps (0 to %d)", fastExecuteEndStep)
			case ExecutionStrategyStartFromBeginningNoHuman:
				hcpo.GetLogger().Infof("⚡ Frontend chose to start from beginning without human input")
				skipHumanInput = true
			case ExecutionStrategyRunSingleStep:
				targetStep := execOpts.ResumeFromStep
				if targetStep <= 0 {
					targetStep = 1 // Default to first step
				}
				hcpo.GetLogger().Infof("🎯 Frontend chose to run single step %d only (from resume_from_step: %d)", targetStep, execOpts.ResumeFromStep)
				startFromStep = targetStep - 1 // Convert to 0-based
				hcpo.SetRunSingleStepMode(true, startFromStep)
				// Note: For run_single_step, we don't modify steps_done.json - it's a one-off execution that doesn't affect progress tracking
			default:
				hcpo.GetLogger().Warnf("⚠️ Unknown execution strategy: %s, defaulting to normal execution", execOpts.ExecutionStrategy)
			}
		} else {
			// Interactive mode - ask for execution mode
			hcpo.GetLogger().Infof("🤔 Asking for execution options")
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
				// All steps are completed
				hcpo.GetLogger().Infof("✅ All steps already completed (%d/%d)",
					len(existingProgress.CompletedStepIndices), existingProgress.TotalSteps)

				// Check if frontend provided action
				if execOpts != nil && execOpts.AllStepsCompletedAction != "" {
					switch execOpts.AllStepsCompletedAction {
					case AllStepsCompletedActionFastExecuteAgain:
						hcpo.GetLogger().Infof("⚡ Frontend chose to fast execute all steps again, clearing progress")
						if err := hcpo.deleteStepProgress(ctx); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to delete steps_done.json: %v", err)
						}
						hcpo.SetFastExecuteMode(true, len(breakdownSteps)-1)
						existingProgress = nil
						startFromStep = 0
						hcpo.GetLogger().Infof("⚡ Will fast execute all steps (0 to %d)", len(breakdownSteps)-1)
					case AllStepsCompletedActionSkipExecution:
						hcpo.GetLogger().Infof("⏭️ Frontend chose to skip execution")
						return "Todo planning complete. All steps already executed.", nil
					default:
						hcpo.GetLogger().Warnf("⚠️ Unknown action: %s, defaulting to skip", execOpts.AllStepsCompletedAction)
						return "Todo planning complete. All steps already executed.", nil
					}
				} else {
					// Interactive mode - ask user
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
						hcpo.GetLogger().Infof("⚡ User chose to fast execute all steps again, clearing progress")
						if err := hcpo.deleteStepProgress(ctx); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to delete steps_done.json: %v (will continue anyway)", err)
						} else {
							hcpo.GetLogger().Infof("🗑️ Deleted steps_done.json to allow re-execution")
						}
						hcpo.SetFastExecuteMode(true, len(breakdownSteps)-1)
						existingProgress = nil
						startFromStep = 0
						hcpo.GetLogger().Infof("⚡ Will fast execute all steps (0 to %d)", len(breakdownSteps)-1)
					case "option1":
						hcpo.GetLogger().Infof("⏭️ User chose to skip execution")
						return "Todo planning complete. All steps already executed.", nil
					default:
						hcpo.GetLogger().Warnf("⚠️ Unknown choice: %s, defaulting to skip execution", choice)
						return "Todo planning complete. All steps already executed.", nil
					}
				}
			} else if nextIncompleteStep > 0 {
				// Calculate the last completed step number (1-based) for display
				lastCompletedStepNumber := max(existingProgress.CompletedStepIndices) + 1 // Convert to 1-based

				// Track fast execute mode
				fastExecuteMode := false
				fastExecuteEndStep := -1
				skipHumanInput := false

				// Check if frontend provided execution strategy
				if execOpts != nil && execOpts.ExecutionStrategy != "" {
					hcpo.GetLogger().Infof("📋 Using frontend-provided execution strategy: %s", execOpts.ExecutionStrategy)
					switch execOpts.ExecutionStrategy {
					case ExecutionStrategyResumeFromStep:
						isExplicit := execOpts.ResumeFromStep > 0
						startFromStep = hcpo.handleResumeStrategy(ctx, execOpts.ResumeFromStep, nextIncompleteStep, existingProgress, isExplicit)
						resumeStep := startFromStep + 1 // Convert back to 1-based for logging
						hcpo.GetLogger().Infof("✅ Frontend chose to resume from step %d", resumeStep)
					case ExecutionStrategyStartFromBeginning:
						hcpo.GetLogger().Infof("🔄 Frontend chose to start from beginning")
						if err := hcpo.deleteStepProgress(ctx); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %w", err)
						}
						runMode := hcpo.selectedRunMode
						if runMode == "" {
							runMode = "use_same_run"
						}
						if err := hcpo.cleanupExecutionArtifactsForFreshStart(ctx, hcpo.GetWorkspacePath(), runMode); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %w", err)
						}
						existingProgress = nil
						startFromStep = 0
					case ExecutionStrategyFastExecuteRange:
						endStep := execOpts.FastExecuteEndStep
						if endStep <= 0 {
							endStep = max(existingProgress.CompletedStepIndices)
						}
						hcpo.GetLogger().Infof("⚡ Frontend chose fast execute mode (0 to %d)", endStep)
						executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
						if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
						}
						fastExecuteMode = true
						fastExecuteEndStep = endStep
						startFromStep = 0
						var newCompletedIndices []int
						for _, idx := range existingProgress.CompletedStepIndices {
							if idx > fastExecuteEndStep {
								newCompletedIndices = append(newCompletedIndices, idx)
							}
						}
						existingProgress.CompletedStepIndices = newCompletedIndices
					case ExecutionStrategyFastExecuteAll:
						hcpo.GetLogger().Infof("⚡ Frontend chose fast execute mode for all steps")
						executionDir := fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
						if err := hcpo.CleanupDirectory(ctx, executionDir, "execution"); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution directory: %w", err)
						}
						fastExecuteMode = true
						fastExecuteEndStep = len(breakdownSteps) - 1
						startFromStep = 0
						existingProgress.CompletedStepIndices = []int{}
					case ExecutionStrategyFastResumeFromStep:
						isExplicit := execOpts.ResumeFromStep > 0
						startFromStep = hcpo.handleResumeStrategy(ctx, execOpts.ResumeFromStep, nextIncompleteStep, existingProgress, isExplicit)
						resumeStep := startFromStep + 1 // Convert back to 1-based for logging
						hcpo.GetLogger().Infof("⚡ Frontend chose fast resume mode from step %d", resumeStep)
						fastExecuteMode = true
						fastExecuteEndStep = len(breakdownSteps) - 1
					case ExecutionStrategyResumeFromStepNoHuman:
						isExplicit := execOpts.ResumeFromStep > 0
						startFromStep = hcpo.handleResumeStrategy(ctx, execOpts.ResumeFromStep, nextIncompleteStep, existingProgress, isExplicit)
						resumeStep := startFromStep + 1 // Convert back to 1-based for logging
						skipHumanInput = true
						hcpo.GetLogger().Infof("✅ Frontend chose to resume from step %d without human input", resumeStep)
					case ExecutionStrategyStartFromBeginningNoHuman:
						hcpo.GetLogger().Infof("🔄 Frontend chose to start from beginning without human input")
						if err := hcpo.deleteStepProgress(ctx); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to delete step progress: %w", err)
						}
						runMode := hcpo.selectedRunMode
						if runMode == "" {
							runMode = "use_same_run"
						}
						if err := hcpo.cleanupExecutionArtifactsForFreshStart(ctx, hcpo.GetWorkspacePath(), runMode); err != nil {
							hcpo.GetLogger().Warnf("⚠️ Failed to cleanup execution artifacts: %w", err)
						}
						existingProgress = nil
						startFromStep = 0
						skipHumanInput = true
					case ExecutionStrategyRunSingleStep:
						targetStep := execOpts.ResumeFromStep
						// Always use the step number sent from frontend (don't default to nextIncompleteStep)
						if targetStep <= 0 {
							targetStep = nextIncompleteStep
							hcpo.GetLogger().Warnf("⚠️ resume_from_step was <= 0, defaulting to nextIncompleteStep: %d", targetStep)
						} else {
							hcpo.GetLogger().Infof("🎯 Using exact step number from frontend: %d", targetStep)
						}
						hcpo.GetLogger().Infof("🎯 Frontend chose to run single step %d only (from resume_from_step: %d)", targetStep, execOpts.ResumeFromStep)
						startFromStep = targetStep - 1 // Convert to 0-based
						hcpo.SetRunSingleStepMode(true, startFromStep)
						// Note: For run_single_step, we don't modify steps_done.json - it's a one-off execution that doesn't affect progress tracking
					default:
						hcpo.GetLogger().Warnf("⚠️ Unknown execution strategy: %s, defaulting to resume", execOpts.ExecutionStrategy)
						startFromStep = nextIncompleteStep - 1
					}
				} else {
					// Interactive mode - ask user
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

// executeConditionalStep executes a conditional step by evaluating the condition and executing the chosen branch
// depth: current nesting depth (0 = main plan, 1 = first level conditional, 2 = second level conditional)
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

// SetRunSingleStepMode sets the single step execution mode
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetRunSingleStepMode(enabled bool, stepIndex int) {
	hcpo.runSingleStepOnly = enabled
	hcpo.singleStepTarget = stepIndex
}

// handleResumeStrategy handles resume strategy logic consistently across all resume strategies.
// If the user explicitly selected a step (isExplicitSelection=true), updates completed list
// to only include steps before the resume step, ensuring all steps from resume step onwards
// will be re-executed. This matches the behavior of run_single_step.
// Returns the 0-based startFromStep index.
func (hcpo *HumanControlledTodoPlannerOrchestrator) handleResumeStrategy(
	ctx context.Context,
	resumeStep int,
	nextIncompleteStep int,
	existingProgress *StepProgress,
	isExplicitSelection bool,
) int {
	// Default to next incomplete step if not explicitly provided
	if resumeStep <= 0 {
		resumeStep = nextIncompleteStep
		isExplicitSelection = false
	}

	startFromStep := resumeStep - 1 // Convert to 0-based

	// If user explicitly selected a step, update completed list to only include steps before resume step
	// This ensures that step X and all subsequent steps will be executed
	if isExplicitSelection && existingProgress != nil {
		var newCompletedIndices []int
		// Only keep steps that are before the resume step (0-based index < startFromStep)
		// This means if resuming from step 3 (1-based), only steps 0 and 1 (steps 1 and 2 in 1-based) remain completed
		for _, idx := range existingProgress.CompletedStepIndices {
			if idx < startFromStep {
				newCompletedIndices = append(newCompletedIndices, idx)
			}
		}
		existingProgress.CompletedStepIndices = newCompletedIndices
		hcpo.GetLogger().Infof("🔄 Updated completed list: only steps before step %d remain completed (removed step %d and all subsequent steps)", resumeStep, resumeStep)

		// Save the updated progress to steps_done.json
		if err := hcpo.saveStepProgress(ctx, existingProgress); err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to save updated step progress: %w", err)
		} else {
			hcpo.GetLogger().Infof("✅ Saved updated step progress: %d steps now marked as completed", len(newCompletedIndices))
		}
	}

	return startFromStep
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

// SetExecutionOptions sets the execution options from frontend
// When set, backend will use these options instead of asking interactively
func (hcpo *HumanControlledTodoPlannerOrchestrator) SetExecutionOptions(options *ExecutionOptions) {
	hcpo.executionOptions = options
	if options != nil {
		hcpo.GetLogger().Infof("📋 Execution options set from frontend: run_mode=%s, strategy=%s, run_folder=%s",
			options.RunMode, options.ExecutionStrategy, options.SelectedRunFolder)
	}
}

// GetExecutionOptions returns the current execution options
func (hcpo *HumanControlledTodoPlannerOrchestrator) GetExecutionOptions() *ExecutionOptions {
	return hcpo.executionOptions
}

// HasExecutionOptions checks if execution options are set
func (hcpo *HumanControlledTodoPlannerOrchestrator) HasExecutionOptions() bool {
	return hcpo.executionOptions != nil
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
