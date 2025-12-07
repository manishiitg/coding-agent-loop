package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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

// HumanControlledTodoPlannerPlanLearningsAlignmentTemplate holds template variables for alignment prompts
type HumanControlledTodoPlannerPlanLearningsAlignmentTemplate struct {
	WorkspacePath            string
	PlanJSON                 string
	AllowedPaths             string
	SelectedFolder           string
	IsCodeExecutionMode      string
	UseStepSpecificLearnings string
}

// HumanControlledTodoPlannerPlanLearningsAlignmentAgent checks alignment between plan and learnings
type HumanControlledTodoPlannerPlanLearningsAlignmentAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator // Reference to base orchestrator for RequestHumanFeedback
}

// NewHumanControlledTodoPlannerPlanLearningsAlignmentAgent creates a new plan learnings alignment agent
func NewHumanControlledTodoPlannerPlanLearningsAlignmentAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *HumanControlledTodoPlannerPlanLearningsAlignmentAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerAnonymizationAgentType, // Reuse anonymization agent type (or create new one if needed)
		eventBridge,
	)

	return &HumanControlledTodoPlannerPlanLearningsAlignmentAgent{
		BaseOrchestratorAgent: baseAgent,
		baseOrchestrator:      baseOrchestrator,
	}
}

// PlanLearningsAlignmentManager manages plan-learnings alignment agent creation independently from controller
type PlanLearningsAlignmentManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string

	// Preset LLM config for plan learnings alignment agent
	presetPlanLearningsAlignmentLLM *AgentLLMConfig
	// Learning LLM config (fallback for plan learnings alignment if presetPlanLearningsAlignmentLLM not set)
	presetLearningLLM *AgentLLMConfig
}

// NewPlanLearningsAlignmentManager creates a new PlanLearningsAlignmentManager
func NewPlanLearningsAlignmentManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetPlanLearningsAlignmentLLM *AgentLLMConfig,
	presetLearningLLM *AgentLLMConfig,
) *PlanLearningsAlignmentManager {
	return &PlanLearningsAlignmentManager{
		BaseOrchestrator:                baseOrchestrator,
		sessionID:                       sessionID,
		workflowID:                      workflowID,
		presetPlanLearningsAlignmentLLM: presetPlanLearningsAlignmentLLM,
		presetLearningLLM:               presetLearningLLM,
	}
}

// createPlanLearningsAlignmentAgent creates and sets up a plan learnings alignment agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
// Always uses learnings/ folder (unified folder for all learning types)
func (plam *PlanLearningsAlignmentManager) createPlanLearningsAlignmentAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	selectedFolder := "learnings/"
	// Set folder guard paths: read-only access to planning/, write access to selected learnings folder only
	planningPath := fmt.Sprintf("%s/planning", workspacePath)
	selectedLearningsPath := fmt.Sprintf("%s/%s", workspacePath, selectedFolder)

	// Agent has read-only access to planning/ folder (for plan.json) and write access to selected learnings folder (for deleting orphaned files)
	readPaths := []string{planningPath, selectedLearningsPath}
	writePaths := []string{selectedLearningsPath} // Write access only to selected folder for deleting orphaned files

	// Step-specific learnings: step-specific folders are at workspace root (not inside runs/)
	// Step-specific learnings are directly in learnings/step-*/
	plam.GetLogger().Infof("📁 Step-specific learnings - agent can access step-specific folders in learnings/step-*/ (at workspace root)")

	plam.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	plam.GetLogger().Infof("🔍 Setting folder guard for plan learnings alignment agent - Read paths: %v, Write paths: %v (read-only access to planning/, write access to %s folder only)", readPaths, writePaths, selectedFolder)

	// Use preset LLM config if available, otherwise fall back to learning LLM, then orchestrator default
	orchestratorLLMConfig := plam.GetLLMConfig()
	var llmConfigToUse *orchestrator.LLMConfig
	if plam.presetPlanLearningsAlignmentLLM != nil && plam.presetPlanLearningsAlignmentLLM.Provider != "" && plam.presetPlanLearningsAlignmentLLM.ModelID != "" {
		// Use preset LLM config
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              plam.presetPlanLearningsAlignmentLLM.Provider,
			ModelID:               plam.presetPlanLearningsAlignmentLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback,
			APIKeys:               orchestratorLLMConfig.APIKeys,
		}
		plam.GetLogger().Infof("🔧 Using preset plan learnings alignment LLM: %s/%s", plam.presetPlanLearningsAlignmentLLM.Provider, plam.presetPlanLearningsAlignmentLLM.ModelID)
	} else if plam.presetLearningLLM != nil && plam.presetLearningLLM.Provider != "" && plam.presetLearningLLM.ModelID != "" {
		// Fallback to learning LLM if plan learnings alignment LLM not set
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              plam.presetLearningLLM.Provider,
			ModelID:               plam.presetLearningLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback,
			APIKeys:               orchestratorLLMConfig.APIKeys,
		}
		plam.GetLogger().Infof("🔧 Using preset learning LLM as fallback for plan learnings alignment: %s/%s", plam.presetLearningLLM.Provider, plam.presetLearningLLM.ModelID)
	} else {
		// Fall back to orchestrator default
		llmConfigToUse = orchestratorLLMConfig
		plam.GetLogger().Infof("🔧 Using orchestrator default alignment LLM: %s/%s", plam.GetProvider(), plam.GetModel())
	}

	// Use workspace tools directly - they already include human_feedback (created by createCustomTools in server.go)
	// No need to add human tools separately as they're already combined in WorkspaceTools
	allTools := plam.WorkspaceTools
	allExecutors := plam.WorkspaceToolExecutors

	// Create agent config with the selected LLM config
	config := plam.CreateStandardAgentConfigWithLLM("plan-learnings-alignment-agent", 100, agents.OutputFormatStructured, llmConfigToUse)

	// Alignment agent doesn't need MCP servers - uses workspace tools only
	config.ServerNames = []string{mcpclient.NoServers}

	// Code execution mode only applies to execution agents, not plan learnings alignment agents
	config.UseCodeExecutionMode = false
	plam.GetLogger().Infof("🔧 Disabling code execution mode for plan learnings alignment agent (only execution agents use MCP tools)")

	// Large output virtual tools are enabled for alignment (agent may generate large reports)

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewHumanControlledTodoPlannerPlanLearningsAlignmentAgent(cfg, logger, tracer, eventBridge, plam.BaseOrchestrator)
	}

	// Use base orchestrator's CreateAndSetupStandardAgentWithConfig to avoid code duplication
	// This handles initialization, event bridge connection, and tool registration
	// Set overwriteSystemPrompt to true for alignment agent (replaces default MCP prompt with agent-specific prompt)
	agent, err := plam.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"plan-learnings-alignment",
		0, 0, // step, iteration
		createAgentFunc,
		allTools,
		allExecutors,
		true, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup plan learnings alignment agent: %w", err)
	}

	return agent, nil
}

// CheckAlignmentOnly runs only the plan-learnings alignment check (standalone, independent from other phases)
// This is a separate workflow phase that can be run independently
func (plam *PlanLearningsAlignmentManager) CheckAlignmentOnly(ctx context.Context, workspacePath string) (string, error) {
	plam.GetLogger().Infof("🔍 Starting standalone plan-learnings alignment check for workspace: %s", workspacePath)

	// Set workspace path
	plam.SetWorkspacePath(workspacePath)

	// Check if plan.json exists - REQUIRED for alignment check
	planPath := fmt.Sprintf("%s/planning/plan.json", plam.GetWorkspacePath())
	planExist, existingPlan, err := plam.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing plan: %w", err)
	}
	if !planExist {
		return "", fmt.Errorf("plan.json not found at %s - planning must be run first as a separate phase", planPath)
	}

	// Plan exists - use it for alignment check
	plam.GetLogger().Infof("✅ Found plan.json with %d steps for alignment check", len(existingPlan.Steps))

	// Always use learnings/ folder (unified folder for all learning types)
	selectedFolder := "learnings/"
	plam.GetLogger().Infof("✅ Using learnings/ folder (unified folder for all learning types)")

	// No need to filter by execution mode - all learnings are in learnings/ folder
	filteredPlan := existingPlan
	plam.GetLogger().Infof("📋 Using plan with %d steps for alignment check", len(filteredPlan.Steps))

	// Prepare filtered plan JSON for template
	planJSONBytes, err := json.MarshalIndent(filteredPlan, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal filtered plan to JSON: %w", err)
	}

	// Create alignment agent
	alignmentAgent, err := plam.createPlanLearningsAlignmentAgent(ctx, plam.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("failed to create plan learnings alignment agent: %w", err)
	}

	// Prepare template variables
	// Use actual workspace path so agent can navigate correctly
	// Explicitly list allowed paths for the agent (step-specific learnings always enabled)
	allowedPaths := "['planning/', 'runs/']"
	alignmentTemplateVars := map[string]string{
		"WorkspacePath":            plam.GetWorkspacePath(),
		"PlanJSON":                 string(planJSONBytes),
		"AllowedPaths":             allowedPaths,
		"SelectedFolder":           selectedFolder,
		"IsCodeExecutionMode":      "false", // Not used anymore, but kept for template compatibility
		"UseStepSpecificLearnings": "true",
		"SessionID":                plam.sessionID,
		"WorkflowID":               plam.workflowID,
	}

	// Execute alignment agent
	plam.GetLogger().Infof("🔍 Executing plan learnings alignment agent for folder: %s", selectedFolder)
	result, conversationHistory, err := alignmentAgent.Execute(ctx, alignmentTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("plan learnings alignment agent execution failed: %w", err)
	}

	plam.GetLogger().Infof("✅ Plan learnings alignment check completed successfully for folder: %s", selectedFolder)
	plam.GetLogger().Infof("🔍 Alignment check result: %s", result)

	_ = conversationHistory // Conversation history not used for standalone alignment check

	return result, nil
}

// requestFolderSelection is deprecated - always uses learnings/ folder now
// Kept for backward compatibility but no longer prompts user
func (plam *PlanLearningsAlignmentManager) requestFolderSelection(ctx context.Context) (string, error) {
	// Always return learnings/ folder (unified folder for all learning types)
	plam.GetLogger().Infof("✅ Using learnings/ folder for alignment check (unified folder)")
	return "option0", nil
}

// filterPlanByExecutionMode filters plan steps to only include steps matching the specified execution mode
// It reads step_config.json to determine each step's execution mode
func (plam *PlanLearningsAlignmentManager) filterPlanByExecutionMode(plan *PlanningResponse, isCodeExecutionMode bool) *PlanningResponse {
	// Read step configs to determine execution mode for each step
	stepConfigs, err := readStepConfigFromFile(context.Background(), plam.GetWorkspacePath(), plam.ReadWorkspaceFile)
	if err != nil {
		plam.GetLogger().Warnf("⚠️ Failed to read step_config.json: %v (will filter based on preset default)", err)
		stepConfigs = &StepConfigFile{Steps: []StepConfig{}}
	}

	// Create a map of step ID to config for quick lookup
	idConfigMap := make(map[string]*AgentConfigs)
	for i := range stepConfigs.Steps {
		config := &stepConfigs.Steps[i]
		if config.ID != "" {
			idConfigMap[config.ID] = config.AgentConfigs
		}
	}

	// Get preset code execution mode as fallback
	presetCodeExecMode := plam.GetUseCodeExecutionMode()

	// Recursive function to filter steps (handles nested branch steps)
	var filterSteps func([]PlanStep) []PlanStep
	filterSteps = func(steps []PlanStep) []PlanStep {
		filtered := []PlanStep{}
		for _, step := range steps {
			// Determine step's execution mode: step config > preset default
			stepIsCodeExecutionMode := presetCodeExecMode
			if agentConfigs := idConfigMap[step.ID]; agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
				stepIsCodeExecutionMode = *agentConfigs.UseCodeExecutionMode
			}

			// Include step if execution mode matches
			if stepIsCodeExecutionMode == isCodeExecutionMode {
				// Recursively filter branch steps
				filteredStep := step
				if len(step.IfTrueSteps) > 0 {
					filteredStep.IfTrueSteps = filterSteps(step.IfTrueSteps)
				}
				if len(step.IfFalseSteps) > 0 {
					filteredStep.IfFalseSteps = filterSteps(step.IfFalseSteps)
				}
				filtered = append(filtered, filteredStep)
			}
		}
		return filtered
	}

	filteredSteps := filterSteps(plan.Steps)

	// Create filtered plan with same structure but filtered steps
	filteredPlan := &PlanningResponse{
		Steps: filteredSteps,
	}

	return filteredPlan
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the shared readPlanFromFile helper which ensures thread-safe access via planFileMutex
func (plam *PlanLearningsAlignmentManager) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {
	plam.GetLogger().Infof("🔍 Checking for existing plan at %s", planPath)

	// Extract workspace path from planPath (planPath is workspacePath/planning/plan.json)
	// readPlanFromFile expects workspacePath and constructs the path internally
	workspacePath := filepath.Dir(filepath.Dir(planPath))

	// Use the shared readPlanFromFile helper which acquires planFileMutex for thread-safe access
	plan, err := readPlanFromFile(ctx, workspacePath, plam.ReadWorkspaceFile)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			plam.GetLogger().Infof("📋 No existing plan found: %v", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	plam.GetLogger().Infof("✅ Found existing plan at %s with %d steps", planPath, len(plan.Steps))
	return true, plan, nil
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerPlanLearningsAlignmentAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	planJSON := templateVars["PlanJSON"]

	// Provide default allowed paths if not present
	allowedPaths := templateVars["AllowedPaths"]
	if allowedPaths == "" {
		allowedPaths = "['planning/', 'learnings/']"
	}

	// Provide default selected folder if not present
	selectedFolder := templateVars["SelectedFolder"]
	if selectedFolder == "" {
		selectedFolder = "learnings/" // Default fallback
	}
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"]
	if isCodeExecutionMode == "" {
		isCodeExecutionMode = "false" // Default to MCP Tool Mode
	}

	// Prepare template variables
	alignmentTemplateVars := map[string]string{
		"WorkspacePath":            workspacePath,
		"PlanJSON":                 planJSON,
		"AllowedPaths":             allowedPaths,
		"SelectedFolder":           selectedFolder,
		"IsCodeExecutionMode":      isCodeExecutionMode,
		"UseStepSpecificLearnings": templateVars["UseStepSpecificLearnings"],
	}

	// Create template data for alignment
	templateData := HumanControlledTodoPlannerPlanLearningsAlignmentTemplate{
		WorkspacePath:            workspacePath,
		PlanJSON:                 planJSON,
		AllowedPaths:             allowedPaths,
		SelectedFolder:           selectedFolder,
		IsCodeExecutionMode:      isCodeExecutionMode,
		UseStepSpecificLearnings: templateVars["UseStepSpecificLearnings"],
	}

	// Generate system prompt and user message separately
	systemPrompt := agent.alignmentSystemPromptProcessor(alignmentTemplateVars)
	userMessage := agent.alignmentUserMessageProcessor(alignmentTemplateVars)

	// Get logger from base agent's MCP agent
	baseAgent := agent.GetBaseAgent()
	var logger utils.ExtendedLogger
	if baseAgent != nil {
		mcpAgent := baseAgent.Agent()
		if mcpAgent != nil && mcpAgent.Logger != nil {
			logger = mcpAgent.Logger
		}
	}

	// Maximum iterations for alignment analysis
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
			logger.Infof("🔍 Plan learnings alignment agent iteration %d/%d", iteration, maxIterations)
		}

		// Create a simple input processor that returns the user message
		inputProcessor := func(map[string]string) string {
			return userMessage
		}

		// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
		result, updatedConversationHistory, err := agent.ExecuteWithTemplateValidation(ctx, alignmentTemplateVars, inputProcessor, currentConversationHistory, templateData, systemPrompt, true)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedConversationHistory

		// After execution, ask if user wants to continue (blocking feedback)
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			if logger != nil {
				logger.Infof("🔍 Plan learnings alignment agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations)
			}

			// Generate unique request ID
			requestID := fmt.Sprintf("plan_learnings_alignment_continue_%d_%d", iteration, time.Now().UnixNano())

			// Request human feedback (blocking call)
			approved, feedback, err := agent.baseOrchestrator.RequestHumanFeedback(
				ctx,
				requestID,
				fmt.Sprintf("Plan learnings alignment analysis is complete (iteration %d/%d). Would you like to ask more questions or request additional analysis?", iteration, maxIterations),
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
					logger.Infof("✅ User approved - plan learnings alignment complete")
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
				logger.Infof("🔍 Reached maximum iterations (%d) or no base orchestrator, ending conversation", maxIterations)
			}
			break
		}
	}

	if logger != nil {
		logger.Infof("🔍 Plan learnings alignment completed after %d iterations", iteration)
	}

	return currentResult, currentConversationHistory, nil
}

// alignmentSystemPromptProcessor creates the system prompt for plan learnings alignment
func (agent *HumanControlledTodoPlannerPlanLearningsAlignmentAgent) alignmentSystemPromptProcessor(templateVars map[string]string) string {
	selectedFolder := templateVars["SelectedFolder"]

	// Step-specific learnings: always use learnings/step-{X}/ (unified folder for all learning types)
	targetFolderPath := "learnings/step-{X}/"
	folderStructureSection := `**STEP-SPECIFIC LEARNINGS MODE**: Learning files are stored in step-specific folders within runs/ directory.
  - First, scan the runs/ directory: Use 'list_workspace_files' with folder="runs" to discover run folders
  - For each run folder found, look for step-specific learning folders (at same level as execution/, not inside it):
    * All steps use learnings/step-{X}/ folders (each step has its own folder, at workspace root)
  - **CRITICAL**: Each step folder (step-1/, step-2/, etc.) should be checked independently
  - **CRITICAL STEP-SPECIFIC RULE**: Check alignment ONLY within the SAME step folder (e.g., step-1/, step-2/, etc.)
  - **NEVER compare across step folders**: Do NOT check alignment between step-1/ and step-2/ files
  - **Consolidation within step folders**: If multiple files exist within a single step folder, they should be consolidated (but that's handled by the consolidation agent, not this alignment agent)

**Expected Folder Structure**:
- learnings/step-{X}/ - All learnings for step X (MCP patterns, scripts, and code) (at workspace root)
- learnings/step-{X}/scripts/ - Python scripts for step X (if any)
- learnings/step-{X}/code/ - Go code patterns for step X (if any)`
	discoverSection := `
**STEP-SPECIFIC MODE**:
- First, scan runs/ directory: Use 'list_workspace_files' with folder="runs" to discover run folders
- For each run folder found, look for step-specific learning folders (at same level as execution/, not inside it):
  * All steps: learnings/step-{X}/ folders (at workspace root)
- **CRITICAL**: Check alignment WITHIN each step folder separately (step-1/, step-2/, etc.)
- **NEVER compare across step folders**: Each step folder is independent`
	mismatchSection := `
- Suggest moving to correct step-specific folder within the same run folder
  * Example: learnings/step-{X}/ contains all learning types (MCP patterns, scripts, and code)
- **CRITICAL**: Only move files WITHIN the same run folder, never across different run folders
- Present list to user via human_feedback and get approval
- If approved, use move_workspace_file to relocate`
	example1Section := `
  File: learnings/step-1/setup_aws_credentials_learning.md
  Step: Setup AWS Credentials (step-1, use_code_execution_mode: false)
  → MATCHED ✅ - Filename matches, MCP mode step, in correct step-specific folder

**Example 2 - MISMATCH** (filename matches but wrong folder):
  File: learnings/step-2/deploy_to_kubernetes_learning.md  
  Step: Deploy to Kubernetes (step-2, use_code_execution_mode: true)
  → All learnings are in learnings/step-{X}/ folder (unified folder)`
	example4Section := `
  File: learnings/step-3/old_step_name_learning.md
  Content: Contains step_3, Deploy Application
  Step: Deploy Application (step-3, use_code_execution_mode: false)
  → CONTENT-MATCHED ✅ - Content references valid step, correct step-specific folder

**Example 5 - ORPHANED** (no match anywhere):
  File: learnings/step-5/removed_feature_learning.md  
  Filename: Doesn't match any current step
  Content: No references to current step IDs/titles
  → ORPHANED ⚠️ - No relevance to current plan
  → Action: Recommend deletion (with user approval)`

	return `# Plan-Learnings Alignment Agent

## 🤖 AGENT IDENTITY
**PRIMARY PURPOSE**: Analyze alignment between plan.json and learnings folders to identify and categorize all learning files, then help the user maintain consistency.

**Your Role**: You are a specialized agent focused on ensuring learning files match the current plan and are stored in the correct folders based on execution mode.

## 🎯 FOCUSED SCOPE

**IMPORTANT**: You are checking alignment for learnings/ folder (unified folder for all learning types).

**Plan**: The plan.json provided to you contains all steps. All learnings are stored in learnings/step-{X}/ folders.

**Target Folder**: ` + targetFolderPath + `

## 📁 CORE CONCEPTS

### Execution Mode & Folder
The selected folder corresponds to a specific execution mode:

| Execution Mode | Learnings Folder | File Types |
|----------------|------------------|------------|
| **learnings/step-{X}/** | *_learning.md, scripts/*_script.py, code/*_code.go |

**Critical Rule**: All learnings for each step are stored in learnings/step-{X}/ folder (unified folder).

` + folderStructureSection + `

### File Categories
You will classify each learning file into one of these categories:

- **MATCHED** ✅: File matches a step AND is in learnings/step-{X}/ folder
- **MISMATCH** ⚠️: File matches a step BUT is not in learnings/step-{X}/ folder
- **CONSOLIDATED** ✅: Valid pattern file (e.g., consolidated_*, general_*) - preserve these
- **CONTENT-MATCHED** ✅: Filename doesn't match any step, but content references valid step(s)
- **ORPHANED** ⚠️: Filename doesn't match any step AND content doesn't reference any steps

## 🔄 MATCHING DECISION FLOW

For each learning file, follow this decision process:

**Step A**: Does filename match a step title?
  - YES: Is file in learnings/step-{X}/ folder?
    - YES → **MATCHED** ✅
    - NO → **MISMATCH** ⚠️ (suggest moving to learnings/step-{X}/ folder)
  - NO: Continue to Step B

**Step B**: Is this a consolidated file (consolidated_*, general_*, *_patterns_*)?
  - YES → **CONSOLIDATED** ✅ (valid, preserve)
  - NO: Continue to Step C

**Step C**: Read file content - Does it reference any step ID/title from plan?
  - YES: Is file in correct folder for referenced step's mode?
    - YES → **CONTENT-MATCHED** ✅ (valid, preserve)
    - NO → **MISMATCH** ⚠️ (suggest moving to correct folder)
  - NO → **ORPHANED** ⚠️ (candidate for deletion with user approval)

## 📋 STEP-BY-STEP PROCESS

### 1. Extract Plan Information
- Parse plan.json to get all step IDs, titles, and execution modes
- For each step, note agent_configs.use_code_execution_mode value (true/false/missing)
- Build a reference map: step ID → {title, execution_mode, expected_folder}

### 2. Discover Learning Files in Selected Folder
**FOCUS**: Only check files in ` + selectedFolder + `

` + discoverSection + `

### 3. Classify Each File (Multi-Layer Strategy)

**Layer 1 - Filename Matching**:
- Normalize filename and step titles (lowercase, remove special chars, spaces→underscores)
- Check if filename (without suffix) matches any step title
- If matched: Check if file is in correct folder → MATCHED ✅ or MISMATCH ⚠️

**Layer 2 - Consolidated File Recognition**:
- Check if filename starts with consolidated_, general_, or contains _patterns_
- If yes → CONSOLIDATED ✅ (skip to next file)

**Layer 3 - Content Reading** (ONLY for files not matched in Layer 1 or 2):
- Use read_workspace_file to read the file content
- Search for references to step IDs or step titles from the plan
- If found: Check if file is in correct folder → CONTENT-MATCHED ✅ or MISMATCH ⚠️

**Layer 4 - Final Classification**:
- If no matches in any layer → ORPHANED ⚠️

**Performance Optimization**: Only read file content (Layer 3) for files that didn't match in Layer 1 or 2. This avoids unnecessary reads.

### 4. Identify Missing Learnings
For each step in the plan, check if there's a MATCHED or CONTENT-MATCHED file in the expected folder. If not, flag as "Missing Learnings".

### 5. Present Findings via human_feedback
Use the human_feedback tool to show:
- **Summary**: Total steps, total files, category counts
- **Matched Files**: Files correctly aligned (brief count)
- **Folder Mismatches**: Files in wrong folder with suggested moves
- **Consolidated Files**: Valid pattern files found
- **Content-Matched Files**: Files with valid content but non-standard filenames
- **Orphaned Files**: Files with no relevance to current plan (candidates for deletion)
- **Missing Learnings**: Steps without learning files
- **Recommendations**: Clear actions for each category

### 6. Resolution Actions

**For MISMATCH files** (wrong folder):` + mismatchSection + `

**For ORPHANED files** (no matches):
- Present list to user via human_feedback with deletion recommendation
- **CRITICAL**: Wait for explicit user approval before deletion
- If approved, use delete_workspace_file to remove

**For other categories** (MATCHED, CONSOLIDATED, CONTENT-MATCHED):
- No action needed - these are valid files

## 📊 EXAMPLE CLASSIFICATIONS

**Example 1 - MATCHED** (filename + correct folder):` + example1Section + `

**Example 3 - CONSOLIDATED** (valid pattern file):
  File: learnings/consolidated_aws_patterns_learning.md
  → CONSOLIDATED ✅ - Valid pattern file from multiple steps

**Example 4 - CONTENT-MATCHED** (renamed but valid):` + example4Section + `

## ⚠️ CRITICAL RULES

1. **MANDATORY HUMAN APPROVAL**: ALWAYS use human_feedback BEFORE any write/delete/move operations
2. **Never Auto-Delete**: Even obviously orphaned files require user confirmation
3. **Read-Only Planning**: You cannot modify planning/plan.json or files in planning/
4. **Access Restrictions**: Only access subdirectories in ` + templateVars["AllowedPaths"] + ` - never list root workspace
5. **Preserve Valid Files**: MATCHED, CONSOLIDATED, and CONTENT-MATCHED files should never be deleted

## ✅ SUCCESS CRITERIA

Your alignment check is complete when you have:
- ✅ Extracted execution mode for every step in the filtered plan
- ✅ Listed all files in ` + selectedFolder + ` folder only
- ✅ Classified every file into one of the 5 categories
- ✅ Identified all missing learnings for steps in the filtered plan
- ✅ Presented comprehensive findings via human_feedback
- ✅ Received user decision on mismatches/orphans
- ✅ (If approved) Executed cleanup/move operations
`
}

// alignmentUserMessageProcessor creates the user message for plan learnings alignment
func (agent *HumanControlledTodoPlannerPlanLearningsAlignmentAgent) alignmentUserMessageProcessor(templateVars map[string]string) string {
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"

	// Step-specific learnings: always use step-specific paths
	var discoverInstructions string
	if isCodeExecutionMode {
		discoverInstructions = `   - **STEP-SPECIFIC MODE**: First, scan runs/ directory to discover run folders
   - For each run folder found, look for step-specific learning folders (at same level as execution/, not inside it)
   - **FOCUS**: Check learnings/step-{X}/ folders (at workspace root)
   - **CRITICAL**: Check alignment WITHIN each step folder separately (step-1/, step-2/, etc.)
   - **NEVER compare across step folders**: Each step folder is independent
   - You should find all .md, .py, and .go files within step-specific learnings folders`
	} else {
		discoverInstructions = `   - **STEP-SPECIFIC MODE**: First, scan runs/ directory to discover run folders
   - For each run folder found, look for step-specific learning folders (at same level as execution/, not inside it)
   - **FOCUS**: Only check learnings/step-{X}/ folders (at workspace root)
   - **CRITICAL**: Check alignment WITHIN each step folder separately (step-1/, step-2/, etc.)
   - **NEVER compare across step folders**: Each step folder is independent
   - You should find all .md and .py files within step-specific learnings folders`
	}

	var expectedFolderNote string
	if isCodeExecutionMode {
		expectedFolderNote = `     * All steps in this plan have use_code_execution_mode: true
     * Expect learnings in learnings/step-{X}/ (at workspace root, not inside runs/)`
	} else {
		expectedFolderNote = `     * All steps in this plan have use_code_execution_mode: false or missing
     * Expect learnings in learnings/step-{X}/ (at workspace root, not inside runs/)`
	}

	planJSON := templateVars["PlanJSON"]
	if planJSON == "" {
		planJSON = "No plan JSON provided."
	}

	return `# Plan-Learnings Alignment Check Task

**PRIMARY GOAL**: Perform a comprehensive alignment check between plan.json and learnings folders, then present findings to the user for review and action.

**Context**:
- **Workspace Path**: ` + templateVars["WorkspacePath"] + `
- **Allowed Paths**: ` + templateVars["AllowedPaths"] + `
- **Step-Specific Learnings Mode**: true

**Current Plan** (to check alignment against):
` + planJSON + `

## YOUR TASK CHECKLIST

**Phase 1: Analysis**

1. **Extract Step Information from Plan**
   - Parse the plan.json above to extract all step IDs and titles
   - For each step, note the agent_configs.use_code_execution_mode value:
` + expectedFolderNote + `
   - Create a mental map of: step ID → {title, execution_mode, expected_folder}

2. **Discover All Learning Files**
` + discoverInstructions + `

3. **Classify Each File** (Follow the decision flow from system prompt)
   
   For each file:
   - **Step A**: Try filename matching against step titles
     * If matched, check if folder is correct for step's execution mode
     * Result: MATCHED ✅ or MISMATCH ⚠️
   
   - **Step B**: If no filename match, check if it's a consolidated file
     * Files starting with consolidated_, general_, or containing _patterns_
     * Result: CONSOLIDATED ✅
   
   - **Step C**: If neither A nor B matched, read file content using read_workspace_file
     * Search content for references to any step IDs or titles from the plan
     * If found, check if folder is correct for referenced step's execution mode
     * Result: CONTENT-MATCHED ✅ or MISMATCH ⚠️
   
   - **Step D**: If all checks failed
     * Result: ORPHANED ⚠️

   **IMPORTANT**: Only read file content (Step C) for files that didn't match in Steps A or B. Don't read ALL files - that's inefficient.

4. **Identify Missing Learnings**
   - For each step in the plan, check if there's a MATCHED or CONTENT-MATCHED file in the expected folder
   - If not, mark as missing learning

**Phase 2: Present Findings**

5. **Use human_feedback tool** to present a comprehensive report:
   
   Structure your report like this:
   - **Summary**: X total steps, Y total learning files found
   - **Category Breakdown**: Counts for each category (MATCHED, MISMATCH, CONSOLIDATED, CONTENT-MATCHED, ORPHANED)
   - **Matched Files** (✅): Brief count (no details needed - these are correct)
   - **Folder Mismatches** (⚠️): List files in wrong folders with specific move recommendations
   - **Consolidated Files** (✅): List pattern files found (these are valid)
   - **Content-Matched Files** (✅): List files with non-standard names but valid content
   - **Orphaned Files** (⚠️): List files with no relevance to current plan
   - **Missing Learnings**: List steps without learning files
   - **Recommended Actions**: 
     * For MISMATCH: Suggest moving to correct folder (provide specific commands)
     * For ORPHANED: Recommend deletion (but require user approval)
     * For MISSING: Note that learnings will be created when steps are executed

   **Ask the user**: What would you like to do with the mismatches and orphaned files?

**Phase 3: Take Action** (ONLY after user approval)

6. **Handle User Response**:
   - If user approves moving MISMATCH files: Use move_workspace_file to relocate them
   - If user approves deleting ORPHANED files: Use delete_workspace_file to remove them
   - If user says no or wants to review individually: Respect their decision

## CRITICAL REMINDERS

- **NEVER auto-delete or auto-move**: ALWAYS use human_feedback BEFORE any write/delete/move operations
- **Efficient content reading**: Only read files that don't match by filename or consolidated patterns
- **Preserve valid files**: MATCHED, CONSOLIDATED, and CONTENT-MATCHED files should never be deleted
- **Read-only planning folder**: Never modify planning/plan.json
- **Clear presentation**: Use the human_feedback tool to show findings in a well-structured, scannable format

## SUCCESS CRITERIA

You're done when you have:
- ✅ Classified every learning file into one of the 5 categories
- ✅ Identified all steps missing learnings
- ✅ Presented comprehensive findings via human_feedback
- ✅ Received user decision and executed approved actions (if any)
`
}
