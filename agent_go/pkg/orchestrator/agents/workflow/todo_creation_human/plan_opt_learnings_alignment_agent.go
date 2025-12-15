package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerPlanLearningsAlignmentTemplate holds template variables for alignment prompts
type HumanControlledTodoPlannerPlanLearningsAlignmentTemplate struct {
	WorkspacePath       string
	PlanJSON            string
	AllowedPaths        string
	SelectedFolder      string
	IsCodeExecutionMode string
}

// HumanControlledTodoPlannerPlanLearningsAlignmentAgent checks alignment between plan and learnings
type HumanControlledTodoPlannerPlanLearningsAlignmentAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator // Reference to base orchestrator for RequestHumanFeedback
}

// NewHumanControlledTodoPlannerPlanLearningsAlignmentAgent creates a new plan learnings alignment agent
func NewHumanControlledTodoPlannerPlanLearningsAlignmentAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *HumanControlledTodoPlannerPlanLearningsAlignmentAgent {
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

	// Learning LLM config (primary LLM for plan learnings alignment agent)
	presetLearningLLM *AgentLLMConfig
}

// NewPlanLearningsAlignmentManager creates a new PlanLearningsAlignmentManager
func NewPlanLearningsAlignmentManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetLearningLLM *AgentLLMConfig,
) *PlanLearningsAlignmentManager {
	return &PlanLearningsAlignmentManager{
		BaseOrchestrator:  baseOrchestrator,
		sessionID:         sessionID,
		workflowID:        workflowID,
		presetLearningLLM: presetLearningLLM,
	}
}

// createPlanLearningsAlignmentAgent creates and sets up a plan learnings alignment agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
// Always uses learnings/ folder (unified folder for all learning types)
func (plam *PlanLearningsAlignmentManager) createPlanLearningsAlignmentAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	selectedFolder := "learnings/"
	// Set folder guard paths: read-only access to planning/ (including changelog/), write access to selected learnings folder only
	planningPath := fmt.Sprintf("%s/planning", workspacePath)
	planningChangelogPath := fmt.Sprintf("%s/planning/changelog", workspacePath)
	selectedLearningsPath := fmt.Sprintf("%s/%s", workspacePath, selectedFolder)

	// Agent has read-only access to planning/ folder (for plan.json) and planning/changelog/ (for changelog files)
	// Write access only to selected learnings folder (for deleting orphaned files)
	readPaths := []string{planningPath, planningChangelogPath, selectedLearningsPath}
	writePaths := []string{selectedLearningsPath} // Write access only to selected folder for deleting orphaned files

	// Step-specific learnings: step-specific folders are at workspace root (not inside runs/)
	// Step-specific learnings are directly in learnings/step-*/
	plam.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings - agent can access step-specific folders in learnings/step-*/ (at workspace root)"))

	plam.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	plam.GetLogger().Info(fmt.Sprintf("🔍 Setting folder guard for plan learnings alignment agent - Read paths: %v, Write paths: %v (read-only access to planning/ and planning/changelog/, write access to %s folder only)", readPaths, writePaths, selectedFolder))

	// Use preset learning LLM if available, otherwise fall back to orchestrator default
	orchestratorLLMConfig := plam.GetLLMConfig()
	var llmConfigToUse *orchestrator.LLMConfig
	if plam.presetLearningLLM != nil && plam.presetLearningLLM.Provider != "" && plam.presetLearningLLM.ModelID != "" {
		// Use preset learning LLM
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              plam.presetLearningLLM.Provider,
			ModelID:               plam.presetLearningLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback,
			APIKeys:               orchestratorLLMConfig.APIKeys,
		}
		plam.GetLogger().Info(fmt.Sprintf("🔧 Using preset learning LLM for plan learnings alignment: %s/%s", plam.presetLearningLLM.Provider, plam.presetLearningLLM.ModelID))
	} else {
		// Fall back to orchestrator default
		llmConfigToUse = orchestratorLLMConfig
		plam.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default alignment LLM: %s/%s", plam.GetProvider(), plam.GetModel()))
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
	plam.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for plan learnings alignment agent (only execution agents use MCP tools)"))

	// Large output virtual tools are enabled for alignment (agent may generate large reports)

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
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
	plam.GetLogger().Info(fmt.Sprintf("🔍 Starting standalone plan-learnings alignment check for workspace: %s", workspacePath))

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
	plam.GetLogger().Info(fmt.Sprintf("✅ Found plan.json with %d steps for alignment check", len(existingPlan.Steps)))

	// Always use learnings/ folder (unified folder for all learning types)
	selectedFolder := "learnings/"
	plam.GetLogger().Info(fmt.Sprintf("✅ Using learnings/ folder (unified folder for all learning types)"))

	// No need to filter by execution mode - all learnings are in learnings/ folder
	filteredPlan := existingPlan
	plam.GetLogger().Info(fmt.Sprintf("📋 Using plan with %d steps for alignment check", len(filteredPlan.Steps)))

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
	// Agent has read access to planning/changelog/ and can discover/read changelog files on demand
	allowedPaths := "['planning/', 'planning/changelog/', 'learnings/']"
	alignmentTemplateVars := map[string]string{
		"WorkspacePath":       plam.GetWorkspacePath(),
		"PlanJSON":            string(planJSONBytes),
		"AllowedPaths":        allowedPaths,
		"SelectedFolder":      selectedFolder,
		"IsCodeExecutionMode": "false", // Not used anymore, but kept for template compatibility
		"SessionID":           plam.sessionID,
		"WorkflowID":          plam.workflowID,
	}

	// Execute alignment agent
	plam.GetLogger().Info(fmt.Sprintf("🔍 Executing plan learnings alignment agent for folder: %s", selectedFolder))
	result, conversationHistory, err := alignmentAgent.Execute(ctx, alignmentTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("plan learnings alignment agent execution failed: %w", err)
	}

	plam.GetLogger().Info(fmt.Sprintf("✅ Plan learnings alignment check completed successfully for folder: %s", selectedFolder))
	plam.GetLogger().Info(fmt.Sprintf("🔍 Alignment check result: %s", result))

	_ = conversationHistory // Conversation history not used for standalone alignment check

	return result, nil
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the shared readPlanFromFile helper which ensures thread-safe access via planFileMutex
func (plam *PlanLearningsAlignmentManager) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {
	plam.GetLogger().Info(fmt.Sprintf("🔍 Checking for existing plan at %s", planPath))

	// Extract workspace path from planPath (planPath is workspacePath/planning/plan.json)
	// readPlanFromFile expects workspacePath and constructs the path internally
	workspacePath := filepath.Dir(filepath.Dir(planPath))

	// Use the shared readPlanFromFile helper which acquires planFileMutex for thread-safe access
	plan, err := readPlanFromFile(ctx, workspacePath, plam.ReadWorkspaceFile)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			plam.GetLogger().Info(fmt.Sprintf("📋 No existing plan found: %v", err))
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	plam.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(plan.Steps)))
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
		allowedPaths = "['planning/', 'planning/changelog/', 'learnings/']"
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
		"WorkspacePath":       workspacePath,
		"PlanJSON":            planJSON,
		"AllowedPaths":        allowedPaths,
		"SelectedFolder":      selectedFolder,
		"IsCodeExecutionMode": isCodeExecutionMode,
	}

	// Create template data for alignment
	templateData := HumanControlledTodoPlannerPlanLearningsAlignmentTemplate{
		WorkspacePath:       workspacePath,
		PlanJSON:            planJSON,
		AllowedPaths:        allowedPaths,
		SelectedFolder:      selectedFolder,
		IsCodeExecutionMode: isCodeExecutionMode,
	}

	// Generate system prompt and user message separately
	systemPrompt := agent.alignmentSystemPromptProcessor(alignmentTemplateVars)
	userMessage := agent.alignmentUserMessageProcessor(alignmentTemplateVars)

	// Get logger from base agent's MCP agent
	baseAgent := agent.GetBaseAgent()
	var logger loggerv2.Logger
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
			logger.Info(fmt.Sprintf("🔍 Plan learnings alignment agent iteration %d/%d", iteration, maxIterations))
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
				logger.Info(fmt.Sprintf("🔍 Plan learnings alignment agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations))
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
					logger.Warn(fmt.Sprintf("⚠️ Failed to get user feedback: %v", err))
				}
				// Continue without blocking if feedback fails
				break
			}

			// If user clicked Approve button, we're done
			if approved {
				if logger != nil {
					logger.Info(fmt.Sprintf("✅ User approved - plan learnings alignment complete"))
				}
				break
			}

			// User provided feedback/question - always pass it to the agent and continue
			if feedback != "" && strings.TrimSpace(feedback) != "" {
				if logger != nil {
					logger.Info(fmt.Sprintf("📝 User provided feedback: %s", feedback))
				}
				// Use feedback directly as user message for next iteration
				// Note: BaseAgent.Execute() will automatically add it to conversation history
				userMessage = feedback
			} else {
				// No feedback provided but not approved - continue with same message
				if logger != nil {
					logger.Info(fmt.Sprintf("ℹ️ No feedback provided, continuing with same context"))
				}
			}
		} else {
			// Reached max iterations or no base orchestrator
			if logger != nil {
				logger.Info(fmt.Sprintf("🔍 Reached maximum iterations (%d) or no base orchestrator, ending conversation", maxIterations))
			}
			break
		}
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("🔍 Plan learnings alignment completed after %d iterations", iteration))
	}

	return currentResult, currentConversationHistory, nil
}

// alignmentSystemPromptProcessor creates the system prompt for plan learnings alignment
func (agent *HumanControlledTodoPlannerPlanLearningsAlignmentAgent) alignmentSystemPromptProcessor(templateVars map[string]string) string {
	selectedFolder := templateVars["SelectedFolder"]

	// Step-specific learnings: always use learnings/step-{X}/ for regular steps, learnings/step-{X}-{true/false}-{Y}/ for branch steps
	targetFolderPath := "learnings/step-{X}/ or learnings/step-{X}-{true/false}-{Y}/"
	folderStructureSection := `**STEP-SPECIFIC LEARNINGS MODE**: Learning files are stored in step-specific folders within the workspace ` + "`learnings/`" + ` directory (NOT in ` + "`runs/`" + `).
  - First, scan the ` + "`learnings/`" + ` directory: Use 'list_workspace_files' with folder="learnings"
  - Discover all step-specific folders:
    * Regular steps use learnings/step-{X}/ folders (e.g., step-1/, step-2/)
    * Branch steps use learnings/step-{parentStep}-{true/false}-{branchIdx}/ folders (e.g., step-3-true-0/, step-3-false-1/)
    * Decision step inner steps use learnings/step-{X}/ folders (same as regular steps - e.g., step-2/ for decision step 2's inner step)
  - **CRITICAL**: Each step folder (step-1/, step-2/, step-3-true-0/, step-3-false-1/, etc.) must be evaluated independently
  - **CRITICAL STEP-SPECIFIC RULE**: Check alignment ONLY within the SAME step folder
  - **NEVER compare across step folders**: Do NOT check alignment between step-1/ and step-2/ files, or between step-3/ and step-3-true-0/ files
  - **Branch step folders**: Branch steps (if_true_steps, if_false_steps) have their own folders separate from the parent conditional step folder
  - **Decision step inner steps**: Learnings for decision step inner steps are stored in learnings/step-{X}/ (same folder as the decision step's step number)
  - **Conditional/Decision step parents**: Conditional steps and decision steps themselves do NOT have learnings (they only evaluate conditions/routes). Only their branch steps (for conditionals) or inner steps (for decisions) have learnings.
  - **Consolidation within step folders**: If multiple files exist within a single step folder, they should be consolidated (handled by the consolidation agent, not this alignment agent)

**Expected Folder Structure**:
- learnings/step-{X}/ - All learnings for regular step X OR decision step X's inner step (MCP patterns, scripts, and code) (at workspace root)
- learnings/step-{X}-{true/false}-{Y}/ - All learnings for branch step Y of conditional step X (at workspace root)
- learnings/step-{X}/scripts/ - Python scripts for step X (if any)
- learnings/step-{X}/code/ - Go code patterns for step X (if any)
- learnings/step-{X}-{true/false}-{Y}/scripts/ - Python scripts for branch step (if any)
- learnings/step-{X}-{true/false}-{Y}/code/ - Go code patterns for branch step (if any)

**IMPORTANT - Step Type Learnings**:
- **Regular steps**: Learnings in learnings/step-{X}/
- **Conditional steps**: NO learnings (parent step only evaluates conditions). Branch steps have learnings in learnings/step-{X}-{true/false}-{Y}/
- **Decision steps**: NO learnings (parent step only evaluates inner step output). Inner step (decision_step) has learnings in learnings/step-{X}/ (same folder as decision step number)
- **Branch steps**: Learnings in learnings/step-{parentStep}-{true/false}-{branchIdx}/`
	discoverSection := `
**STEP-SPECIFIC MODE**:
- Scan ` + "`learnings/`" + `: Use 'list_workspace_files' with folder="learnings" to discover all step folders
- For each step folder found:
  * Regular steps: learnings/step-{X}/
  * Branch steps: learnings/step-{X}-{true/false}-{Y}/
- Then list files within each folder (and its scripts/ and code/ subfolders if they exist)
- **CRITICAL**: Check alignment WITHIN each step folder separately (step-1/, step-2/, step-3-true-0/, etc.)
- **NEVER compare across step folders**: Each step folder is independent (including branch step folders)`
	mismatchSection := `
- Suggest moving to the correct step-specific folder within ` + "`learnings/`" + `
  * Regular steps: learnings/step-{X}/ contains all learning types (MCP patterns, scripts, and code)
  * Branch steps: learnings/step-{X}-{true/false}-{Y}/ contains all learning types for branch step Y of conditional step X
- **CRITICAL**: Branch step files must be in branch step folders (step-{X}-{true/false}-{Y}/), not in parent step folders
- Present list to user via human_feedback and get approval
- If approved, use move_workspace_file to relocate`
	example1Section := `
  File: learnings/step-1/setup_aws_credentials_learning.md
  Step: Setup AWS Credentials (step-1, use_code_execution_mode: false)
  → MATCHED ✅ - Filename matches, MCP mode step, in correct step-specific folder

**Example 1b - MATCHED (Branch Step)**:
  File: learnings/step-3-true-0/retrieve_otp_learning.md
  Step: Retrieve and Submit OTP (step-3-if-true-0, branch step of conditional step 3)
  → MATCHED ✅ - Filename matches branch step, in correct branch step folder

**Example 1c - MATCHED (Decision Step Inner Step)**:
  File: learnings/step-2/check_deployment_status_learning.md
  Step: Check Deployment Status (decision step 2's inner step - decision_step field)
  → MATCHED ✅ - Filename matches decision step inner step, in correct folder (learnings/step-2/ - same as decision step number)

**Example 2 - MISMATCH** (filename matches but wrong folder):
  File: learnings/step-2/deploy_to_kubernetes_learning.md  
  Step: Deploy to Kubernetes (step-2, use_code_execution_mode: true)
  → All learnings are in learnings/step-{X}/ folder (unified folder)

**Example 2b - MISMATCH (Branch Step in Wrong Folder)**:
  File: learnings/step-3/retrieve_otp_learning.md
  Step: Retrieve and Submit OTP (step-3-if-true-0, branch step of conditional step 3)
  → MISMATCH ⚠️ - Branch step file is in parent step folder, should be in learnings/step-3-true-0/

**Example 2c - MISMATCH (Decision Step Inner Step in Wrong Folder)**:
  File: learnings/step-5/check_deployment_status_learning.md
  Step: Check Deployment Status (decision step 2's inner step - decision_step field)
  → MISMATCH ⚠️ - Decision step inner step file is in wrong folder, should be in learnings/step-2/ (decision step's step number)`
	example4Section := `
  File: learnings/step-3/old_step_name_learning.md
  Content: Contains step_3, Deploy Application
  Step: Deploy Application (step-3, use_code_execution_mode: false)
  → CONTENT-MATCHED ✅ - Content references valid step, correct step-specific folder

**Example 4b - CONTENT-MATCHED (Branch Step)**:
  File: learnings/step-3-true-0/old_branch_name_learning.md
  Content: Contains references to step-3-if-true-0, Retrieve OTP
  Step: Retrieve and Submit OTP (step-3-if-true-0, branch step of conditional step 3)
  → CONTENT-MATCHED ✅ - Content references valid branch step, correct branch step folder

**Example 5 - ORPHANED** (no match anywhere):
  File: learnings/step-5/removed_feature_learning.md  
  Filename: Doesn't match any current step
  Content: No references to current step IDs/titles
  → ORPHANED ⚠️ - No relevance to current plan
  → Action: Recommend deletion (with user approval)

**Example 5b - ORPHANED (Branch Step)**:
  File: learnings/step-4-false-1/old_branch_learning.md
  Filename: Doesn't match any current branch step
  Content: No references to current step IDs/titles
  → ORPHANED ⚠️ - No relevance to current plan
  → Action: Recommend deletion (with user approval)`

	return `# Plan-Learnings Alignment Agent

## 🤖 AGENT IDENTITY
**PRIMARY PURPOSE**: Analyze alignment between plan.json and learnings folders to identify and categorize all learning files, then help the user maintain consistency.

**Your Role**: You are a specialized agent focused on ensuring learning files match the current plan and are stored in the correct folders based on execution mode.

## 🎯 FOCUSED SCOPE

**IMPORTANT**: You are checking alignment for learnings/ folder (unified folder for all learning types).

**Plan**: The plan.json provided to you contains all steps, including regular steps, conditional steps, decision steps, and branch steps (if_true_steps, if_false_steps). All learnings are stored in step-specific folders:
- Regular steps: learnings/step-{X}/ folders
- Branch steps: learnings/step-{parentStep}-{true/false}-{branchIdx}/ folders
- Decision step inner steps: learnings/step-{X}/ folders (same as regular steps - stored in the decision step's step number folder)
- Conditional steps: NO learnings (parent step only evaluates conditions, doesn't execute)
- Decision steps: NO learnings (parent step only evaluates inner step output, doesn't execute itself)

**Target Folder**: ` + targetFolderPath + `

## 📁 CORE CONCEPTS

### Execution Mode & Folder
The selected folder corresponds to a specific execution mode:

| Step Type | Learnings Folder | File Types |
|-----------|------------------|------------|
| **Regular Steps** | learnings/step-{X}/ | *_learning.md, scripts/*_script.py, code/*_code.go |
| **Branch Steps** | learnings/step-{X}-{true/false}-{Y}/ | *_learning.md, scripts/*_script.py, code/*_code.go |

**Critical Rule**: All learnings for each step are stored in step-specific folders:
- Regular steps: learnings/step-{X}/ folder (unified folder)
- Branch steps: learnings/step-{parentStep}-{true/false}-{branchIdx}/ folder (separate from parent step)

` + folderStructureSection + `

### File Categories
You will classify each learning file into one of these categories:

- **MATCHED** ✅: File matches a step AND is in correct step-specific folder
  - Regular steps: learnings/step-{X}/ folder
  - Branch steps: learnings/step-{parentStep}-{true/false}-{branchIdx}/ folder
- **MISMATCH** ⚠️: File matches a step BUT is not in correct step-specific folder
  - Regular step file in wrong folder (e.g., step-1 file in step-2/)
  - Branch step file in parent folder (e.g., step-3-true-0 file in step-3/)
  - Branch step file in wrong branch folder (e.g., step-3-true-0 file in step-3-false-1/)
- **CONSOLIDATED** ✅: Valid pattern file (e.g., consolidated_*, general_*) - preserve these
- **CONTENT-MATCHED** ✅: Filename doesn't match any step, but content references valid step(s) AND is in correct folder
- **ORPHANED** ⚠️: Filename doesn't match any step AND content doesn't reference any steps

### Functional Correctness (Anti-Hallucination / Anti-Mock)
In addition to alignment categories above, you MUST also assess whether each learning file is *functionally trustworthy*.

**CorrectnessStatus** (choose one per file):
- **VERIFIED_OK** ✅: Claims are supported by evidence you can verify using available workspace tools (e.g., referenced files exist and contents match claims) AND code patterns are not mock/stub.
- **SUSPICIOUS** ⚠️: Strong indicators of hallucination/mocking (e.g., “success” claims with no evidence, hardcoded outputs, simulated data, stubs presented as real).
- **UNVERIFIABLE** ℹ️: You cannot verify the claim with the available workspace access (e.g., claim depends on external systems or files outside allowed paths). This is not automatically wrong, but must be explicitly marked as unverifiable with reasoning.

**CRITICAL**: Never “assume true”. If you can’t verify, mark UNVERIFIABLE. If you see mock/stub patterns presented as real, mark SUSPICIOUS.

## 🔄 MATCHING DECISION FLOW

For each learning file, follow this decision process:

**Step A**: Does filename match a step title?
  - YES: Is file in correct step-specific folder?
    - Regular step: Is file in learnings/step-{X}/ folder?
    - Branch step: Is file in learnings/step-{parentStep}-{true/false}-{branchIdx}/ folder?
    - Decision step inner step: Is file in learnings/step-{X}/ folder? (same as regular steps - uses decision step's step number)
    - Conditional step: NO learnings expected (parent step doesn't execute)
    - Decision step: NO learnings expected (parent step doesn't execute, only inner step has learnings)
    - YES → **MATCHED** ✅
    - NO → **MISMATCH** ⚠️ (suggest moving to correct step-specific folder)
  - NO: Continue to Step B

**Step B**: Is this a consolidated file (consolidated_*, general_*, *_patterns_*)?
  - YES → **CONSOLIDATED** ✅ (valid, preserve)
  - NO: Continue to Step C

**Step C**: Read file content - Does it reference any step ID/title from plan?
  - YES: Is file in correct folder for referenced step's mode?
    - YES → **CONTENT-MATCHED** ✅ (valid, preserve)
    - NO → **MISMATCH** ⚠️ (suggest moving to correct folder)
  - NO → **ORPHANED** ⚠️ (candidate for deletion with user approval)

**Step D (Functional Correctness Gate)**: For every file that is NOT ORPHANED:
  - Determine CorrectnessStatus (VERIFIED_OK / SUSPICIOUS / UNVERIFIABLE)
  - Provide evidence:
    - If VERIFIED_OK: cite what you verified (which file(s) you checked and what matched)
    - If SUSPICIOUS: cite the red flag(s) found (quotes/snippets and why it indicates mock/hallucination)
    - If UNVERIFIABLE: explain exactly what cannot be verified with current tools/access

## 📋 STEP-BY-STEP PROCESS

### 0. Read Plan Changelog (CRITICAL FIRST STEP)
- **IMPORTANT**: Before analyzing alignment, read all changelog files from planning/changelog/ directory
- Changelog files are named with timestamps: changelog-YYYY-MM-DD-HH-MM-SS.json (e.g., changelog-2025-01-27-14-30-25.json)
- Each file contains all changes from a single planning agent execution session (multiple entries per file)
- Each entry has: timestamp, change type, affected step IDs, description, and details
- **Purpose**: Understanding recent changes helps identify which learnings might be out of sync
- **How to read**:
  1. Use list_workspace_files with folder="planning/changelog" to get all changelog files
  2. Filter files that match pattern changelog-*.json
  3. Read each file using read_workspace_file (each file contains an array of entries)
  4. Combine all entries from all files and sort by timestamp (oldest first)
- **What to look for**:
  - Recent deletions: Steps that were deleted (learnings for these steps may be orphaned)
  - Recent updates: Steps that were modified (learnings may reference old step titles/descriptions)
  - Recent additions: New steps added (may not have learnings yet)
  - Conditional conversions: Steps converted to/from conditional (branch step learnings may be in wrong folders)
  - Branch step changes: Steps added/updated/deleted in conditional branches
- **Use changelog to prioritize**: Focus alignment checks on steps that were recently changed
- **Note**: If changelog directory doesn't exist or is empty, proceed with normal alignment check

### 1. Extract Plan Information
- Parse plan.json to get all step IDs, titles, and execution modes
- For each step, note agent_configs.use_code_execution_mode value (true/false/missing)
- **CRITICAL**: Identify step types:
  - Regular steps: Top-level steps in plan.steps array
  - Conditional steps: Steps with has_condition=true (parent step - NO learnings, only branch steps have learnings)
  - Decision steps: Steps with has_decision_step=true (parent step - NO learnings, only inner step has learnings)
  - Branch steps: Steps nested in if_true_steps or if_false_steps arrays
  - Decision step inner steps: Steps in decision_step field (stored in learnings/step-{X}/ where X is the decision step's step number)
- For branch steps, note the parent step ID and branch type (true/false)
- For decision steps, note that the inner step (decision_step field) has learnings in learnings/step-{X}/ (same folder as decision step number)
- Build a reference map: step ID → {title, execution_mode, expected_folder, step_type, is_branch_step, is_decision_inner, parent_step_id, branch_type, branch_index}
  - Regular steps: expected_folder = learnings/step-{X}/
  - Branch steps: expected_folder = learnings/step-{parentStep}-{true/false}-{branchIdx}/
  - Decision step inner steps: expected_folder = learnings/step-{X}/ (where X is the decision step's step number)
  - Conditional steps: expected_folder = NONE (no learnings - parent step doesn't execute)
  - Decision steps: expected_folder = NONE (no learnings - parent step doesn't execute, only inner step has learnings)
- **Cross-reference with changelog**: Mark steps that appear in recent changelog entries as "recently changed"

### 2. Discover Learning Files in Selected Folder
**FOCUS**: Only check files in ` + selectedFolder + `

` + discoverSection + `

### 3. Classify Each File (Multi-Layer Strategy)

**Layer 1 - Filename Matching**:
- Normalize filename and step titles (lowercase, remove special chars, spaces→underscores)
- Check if filename (without suffix) matches any step title (regular or branch steps)
- If matched: Check if file is in correct step-specific folder:
  - Regular step: learnings/step-{X}/ folder
  - Branch step: learnings/step-{parentStep}-{true/false}-{branchIdx}/ folder
- Result: MATCHED ✅ or MISMATCH ⚠️

**Layer 2 - Consolidated File Recognition**:
- Check if filename starts with consolidated_, general_, or contains _patterns_
- If yes → CONSOLIDATED ✅ (skip to next file)

**Layer 3 - Content Reading** (ONLY for files not matched in Layer 1 or 2):
- Use read_workspace_file to read the file content
- Search for references to step IDs or step titles from the plan
- If found: Check if file is in correct folder → CONTENT-MATCHED ✅ or MISMATCH ⚠️

**Layer 4 - Final Classification**:
- If no matches in any layer → ORPHANED ⚠️

**Layer 5 - Functional Correctness Review (MANDATORY for non-orphan files)**:
- **Goal**: Detect hallucinations/mocks and ensure learnings are not “fake-success” patterns.
- **For Markdown (` + "`*_learning.md`" + `)**:
  - If the doc claims “X file exists / was created / contains Y”: verify by listing/reading the referenced file(s) within allowed paths.
  - If it references outputs/IDs/URLs: treat as UNVERIFIABLE unless the evidence exists in workspace.
  - If it uses language like “pretend/simulate/mock/fake” without clearly labeling as example: mark SUSPICIOUS.
- **For Go (` + "`code/*_code.go`" + `)**:
  - Static inspect for mock patterns:
    - hardcoded outputs (“Success!”, “created N files”) without actual tool calls
    - simulated data arrays/maps presented as real results
    - stubs that return constants where real tool calls should be
  - Prefer patterns that call generated tool functions (e.g., ` + "`workspace_tools.*`" + `, ` + "`aws_tools.*`" + `) and handle errors.
- **For Python (` + "`scripts/*_script.py`" + `)**:
  - Static inspect for the same mock patterns and for missing real calls where claims are made.

**Important**: You cannot execute code in this agent. You can only (a) inspect code and (b) verify workspace state via workspace tools.

**Performance Optimization**: Only read file content (Layer 3) for files that didn't match in Layer 1 or 2. This avoids unnecessary reads.

### 4. Identify Missing Learnings
For each step in the plan, check if there's a MATCHED or CONTENT-MATCHED file in the expected folder. If not, flag as "Missing Learnings".

### 5. Present Findings via human_feedback
Use the human_feedback tool to show:
- **Summary**: Total steps, total files, category counts
- **Correctness Summary**: Counts by CorrectnessStatus (VERIFIED_OK / SUSPICIOUS / UNVERIFIABLE)
- **Matched Files**: Files correctly aligned (brief count)
- **Folder Mismatches**: Files in wrong folder with suggested moves
- **Consolidated Files**: Valid pattern files found
- **Content-Matched Files**: Files with valid content but non-standard filenames
- **Orphaned Files**: Files with no relevance to current plan (candidates for deletion)
- **Suspicious/Mock Learnings**: Files flagged as SUSPICIOUS with short evidence bullets
- **Unverifiable Learnings**: Files flagged as UNVERIFIABLE with what’s missing to verify
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
6. **No Guessing**: If a claim cannot be verified with workspace tools and allowed paths, mark it UNVERIFIABLE (do not “assume”).
7. **Flag Mocks**: If a learning/code pattern appears to “fake” success (hardcoded outputs, simulated data presented as real), mark it SUSPICIOUS.

## ✅ SUCCESS CRITERIA

Your alignment check is complete when you have:
- ✅ Extracted execution mode for every step in the filtered plan
- ✅ Listed all files in ` + selectedFolder + ` folder only
- ✅ Classified every file into one of the 5 categories
- ✅ Assigned CorrectnessStatus to every non-orphan file with evidence/reasoning
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
		discoverInstructions = `   - **STEP-SPECIFIC MODE**: Scan ` + "`learnings/`" + ` to discover all step folders
   - Use ` + "`list_workspace_files`" + ` with folder="learnings"
   - **FOCUS**: Check both regular and branch step folders:
     * Regular steps: learnings/step-{X}/ (e.g., step-1/, step-2/)
     * Branch steps: learnings/step-{X}-{true/false}-{Y}/ (e.g., step-3-true-0/, step-3-false-1/)
   - For each step folder, list files in:
     * the folder itself
     * scripts/ (if present)
     * code/ (if present)
   - **CRITICAL**: Check alignment WITHIN each step folder separately
   - **NEVER compare across step folders**
   - You should find all .md, .py, and .go files within step-specific learnings folders`
	} else {
		discoverInstructions = `   - **STEP-SPECIFIC MODE**: Scan ` + "`learnings/`" + ` to discover all step folders
   - Use ` + "`list_workspace_files`" + ` with folder="learnings"
   - **FOCUS**: Check both regular and branch step folders:
     * Regular steps: learnings/step-{X}/ (e.g., step-1/, step-2/)
     * Branch steps: learnings/step-{X}-{true/false}-{Y}/ (e.g., step-3-true-0/, step-3-false-1/)
   - For each step folder, list files in:
     * the folder itself
     * scripts/ (if present)
     * code/ (if present)
   - **CRITICAL**: Check alignment WITHIN each step folder separately
   - **NEVER compare across step folders**
   - You should find all .md, .py, and .go files within step-specific learnings folders`
	}

	var expectedFolderNote string
	if isCodeExecutionMode {
		expectedFolderNote = `     * Regular steps have use_code_execution_mode: true
     * Branch steps inherit execution mode from parent conditional step
     * Regular steps: Expect learnings in learnings/step-{X}/ (at workspace root, not inside runs/)
     * Branch steps: Expect learnings in learnings/step-{parentStep}-{true/false}-{branchIdx}/ (at workspace root, not inside runs/)`
	} else {
		expectedFolderNote = `     * Regular steps have use_code_execution_mode: false or missing
     * Branch steps inherit execution mode from parent conditional step
     * Regular steps: Expect learnings in learnings/step-{X}/ (at workspace root, not inside runs/)
     * Branch steps: Expect learnings in learnings/step-{parentStep}-{true/false}-{branchIdx}/ (at workspace root, not inside runs/)`
	}

	planJSON := templateVars["PlanJSON"]
	if planJSON == "" {
		planJSON = "No plan JSON provided."
	}

	changelogSection := `
**Plan Changelog**: You have read access to planning/changelog/ directory. Use list_workspace_files with folder="planning/changelog" to discover all changelog-*.json files, then read them individually using read_workspace_file. Combine all entries from all files and sort by timestamp (oldest first) to understand recent plan changes.
`

	return `# Plan-Learnings Alignment Check Task

**PRIMARY GOAL**: Perform a comprehensive alignment check between plan.json and learnings folders, then present findings to the user for review and action.

**Context**:
- **Workspace Path**: ` + templateVars["WorkspacePath"] + `
- **Allowed Paths**: ` + templateVars["AllowedPaths"] + `
- **Step-Specific Learnings Mode**: true

**Current Plan** (to check alignment against):
` + planJSON + `
` + changelogSection + `

## YOUR TASK CHECKLIST

**Phase 0: Read Plan Changelog (CRITICAL FIRST STEP)**

0. **Read the Plan Changelog**
   - **IMPORTANT**: Changelog files are stored as session-based timestamped files in planning/changelog/
   - File naming pattern: changelog-YYYY-MM-DD-HH-MM-SS.json (e.g., changelog-2025-01-27-14-30-25.json)
   - Each file contains all changes from a single planning agent execution session (multiple entries per file)
   - **How to read**:
     1. Use list_workspace_files with folder="planning/changelog" to discover all changelog files
     2. Filter files matching pattern changelog-*.json
     3. Read each file using read_workspace_file
     4. Each file contains an array of change entries (each entry has: timestamp, change_type, step_ids, description, details)
     5. Combine all entries from all files and sort by timestamp (oldest first)
   - The changelog contains a history of all plan modifications
   - **Purpose**: Understanding recent changes helps identify which learnings are likely out of sync
   - **What to extract from changelog**:
     * Deleted steps: Step IDs that were removed (their learnings may be orphaned)
     * Updated steps: Step IDs that were modified (learnings may reference old titles/descriptions)
     * Added steps: New step IDs (may not have learnings yet)
     * Conditional conversions: Steps converted to/from conditional (branch learnings may be misplaced)
     * Branch step changes: Steps added/updated/deleted in conditional branches
   - **Prioritization**: Steps mentioned in recent changelog entries should be checked first
   - **Note**: If changelog directory doesn't exist or is empty, that's fine - proceed with normal alignment check

**Phase 1: Analysis**

1. **Extract Step Information from Plan**
   - Parse the plan.json above to extract all step IDs and titles
   - **CRITICAL**: Identify step types:
     * Regular steps: Top-level steps in plan.steps array
     * Conditional steps: Steps with has_condition=true (parent step - NO learnings, only branch steps have learnings)
     * Decision steps: Steps with has_decision_step=true (parent step - NO learnings, only inner step has learnings)
     * Branch steps: Steps nested in if_true_steps or if_false_steps arrays (note parent step ID and branch type)
     * Decision step inner steps: Steps in decision_step field (stored in learnings/step-{X}/ where X is the decision step's step number)
   - For each step, note the agent_configs.use_code_execution_mode value:
` + expectedFolderNote + `
   - Create a mental map of: step ID → {title, execution_mode, expected_folder, step_type, is_branch_step, is_decision_inner, parent_step_id, branch_type, branch_index}
     * Regular steps: expected_folder = learnings/step-{X}/
     * Branch steps: expected_folder = learnings/step-{parentStep}-{true/false}-{branchIdx}/
     * Decision step inner steps: expected_folder = learnings/step-{X}/ (where X is the decision step's step number)
     * Conditional steps: expected_folder = NONE (no learnings - parent step doesn't execute)
     * Decision steps: expected_folder = NONE (no learnings - parent step doesn't execute, only inner step has learnings)

2. **Discover All Learning Files**
` + discoverInstructions + `

3. **Classify Each File** (Follow the decision flow from system prompt)
   
   For each file:
   - **Step A**: Try filename matching against step titles (regular steps, branch steps, and decision step inner steps)
     * If matched, check if folder is correct for step type:
       - Regular step: Is file in learnings/step-{X}/ folder?
       - Branch step: Is file in learnings/step-{parentStep}-{true/false}-{branchIdx}/ folder?
       - Decision step inner step: Is file in learnings/step-{X}/ folder? (where X is the decision step's step number)
       - Conditional step: NO learnings expected (parent step doesn't execute)
       - Decision step: NO learnings expected (parent step doesn't execute, only inner step has learnings)
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

4. **Functional Correctness Review (MANDATORY for non-orphan files)**
   - For every file that is not ORPHANED, assign a **CorrectnessStatus**:
     * VERIFIED_OK ✅: You verified claims using workspace tools (within allowed paths)
     * SUSPICIOUS ⚠️: You found strong mock/hallucination indicators (cite evidence)
     * UNVERIFIABLE ℹ️: You cannot verify claims with available access/tools (explain what’s missing)
   - **Markdown**: Verify any claims about files/contents by listing/reading those files if possible.
   - **Go/Python**: Static inspect for fake-success patterns (hardcoded outputs, simulated data presented as real, missing tool calls).
   - **Never guess**: If you cannot verify, mark UNVERIFIABLE.

5. **Identify Missing Learnings**
   - For each step in the plan, check if there's a MATCHED or CONTENT-MATCHED file in the expected folder
   - If not, mark as missing learning

**Phase 2: Present Findings**

6. **Use human_feedback tool** to present a comprehensive report:
   
   Structure your report like this:
   - **Summary**: X total steps, Y total learning files found
   - **Category Breakdown**: Counts for each category (MATCHED, MISMATCH, CONSOLIDATED, CONTENT-MATCHED, ORPHANED)
   - **Correctness Breakdown**: Counts for each correctness status (VERIFIED_OK, SUSPICIOUS, UNVERIFIABLE)
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

7. **Handle User Response**:
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
