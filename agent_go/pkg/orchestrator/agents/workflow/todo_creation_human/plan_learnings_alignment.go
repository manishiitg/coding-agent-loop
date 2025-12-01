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
	WorkspacePath string
	PlanJSON      string
	AllowedPaths  string
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
}

// NewPlanLearningsAlignmentManager creates a new PlanLearningsAlignmentManager
func NewPlanLearningsAlignmentManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetPlanLearningsAlignmentLLM *AgentLLMConfig,
) *PlanLearningsAlignmentManager {
	return &PlanLearningsAlignmentManager{
		BaseOrchestrator:                baseOrchestrator,
		sessionID:                       sessionID,
		workflowID:                      workflowID,
		presetPlanLearningsAlignmentLLM: presetPlanLearningsAlignmentLLM,
	}
}

// createPlanLearningsAlignmentAgent creates and sets up a plan learnings alignment agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
func (plam *PlanLearningsAlignmentManager) createPlanLearningsAlignmentAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read-only access to planning/, write access to learnings/ folders
	planningPath := fmt.Sprintf("%s/planning", workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	learningCodeExecPath := fmt.Sprintf("%s/learning_code_exec", workspacePath)

	// Agent has read-only access to planning/ folder (for plan.json) and write access to both learnings/ folders (for deleting orphaned files)
	readPaths := []string{planningPath, learningsPath, learningCodeExecPath}
	writePaths := []string{learningsPath, learningCodeExecPath} // Write access to learnings folders for deleting orphaned files
	plam.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	plam.GetLogger().Infof("🔍 Setting folder guard for plan learnings alignment agent - Read paths: %v, Write paths: %v (read-only access to planning/, write access to learnings/ folders)", readPaths, writePaths)

	// Use preset LLM config if available, otherwise fall back to orchestrator default
	orchestratorLLMConfig := plam.GetLLMConfig()
	var llmConfigToUse *orchestrator.LLMConfig
	if plam.presetPlanLearningsAlignmentLLM != nil && plam.presetPlanLearningsAlignmentLLM.Provider != "" && plam.presetPlanLearningsAlignmentLLM.ModelID != "" {
		// Use preset LLM config
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:       plam.presetPlanLearningsAlignmentLLM.Provider,
			ModelID:        plam.presetPlanLearningsAlignmentLLM.ModelID,
			FallbackModels: orchestratorLLMConfig.FallbackModels,
			APIKeys:        orchestratorLLMConfig.APIKeys,
			Options:        orchestratorLLMConfig.Options,
		}
		plam.GetLogger().Infof("🔧 Using preset plan learnings alignment LLM: %s/%s", plam.presetPlanLearningsAlignmentLLM.Provider, plam.presetPlanLearningsAlignmentLLM.ModelID)
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

	// Prepare plan JSON for template
	planJSONBytes, err := json.MarshalIndent(existingPlan, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan to JSON: %w", err)
	}

	// Create alignment agent
	alignmentAgent, err := plam.createPlanLearningsAlignmentAgent(ctx, plam.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("failed to create plan learnings alignment agent: %w", err)
	}

	// Prepare template variables
	// Use actual workspace path so agent can navigate correctly
	// Explicitly list allowed paths for the agent
	allowedPaths := "['planning/', 'learnings/', 'learning_code_exec/']"
	alignmentTemplateVars := map[string]string{
		"WorkspacePath": plam.GetWorkspacePath(),
		"PlanJSON":      string(planJSONBytes),
		"AllowedPaths":  allowedPaths,
		"SessionID":     plam.sessionID,
		"WorkflowID":    plam.workflowID,
	}

	// Execute alignment agent
	plam.GetLogger().Infof("🔍 Executing plan learnings alignment agent...")
	result, conversationHistory, err := alignmentAgent.Execute(ctx, alignmentTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("plan learnings alignment agent execution failed: %w", err)
	}

	plam.GetLogger().Infof("✅ Plan learnings alignment check completed successfully")
	plam.GetLogger().Infof("🔍 Alignment check result: %s", result)

	_ = conversationHistory // Conversation history not used for standalone alignment check

	return result, nil
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

	// Prepare template variables
	alignmentTemplateVars := map[string]string{
		"WorkspacePath": workspacePath,
		"PlanJSON":      planJSON,
		"AllowedPaths":  allowedPaths,
	}

	// Create template data for alignment
	templateData := HumanControlledTodoPlannerPlanLearningsAlignmentTemplate{
		WorkspacePath: workspacePath,
		PlanJSON:      planJSON,
		AllowedPaths:  allowedPaths,
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
	return `# Plan-Learnings Alignment Agent

## 🤖 AGENT IDENTITY
**PRIMARY PURPOSE**: Analyze alignment between plan.json and learnings folders to identify and categorize all learning files, then help the user maintain consistency.

**Your Role**: You are a specialized agent focused on ensuring learning files match the current plan and are stored in the correct folders based on execution mode.

## 📁 CORE CONCEPTS

### Two Execution Modes & Folders
Each step in the plan has an execution mode that determines where its learnings should be stored:

| Execution Mode | Determined By | Learnings Folder | File Types |
|----------------|---------------|------------------|------------|
| **MCP Tool Mode** | agent_configs.use_code_execution_mode: false or missing | learnings/ | *_learning.md, scripts/*_script.py |
| **Code Execution Mode** | agent_configs.use_code_execution_mode: true | learning_code_exec/ | *_learning.md, code/*_code.go |

**Critical Rule**: Each step's learnings should be in ONLY ONE folder matching its execution mode.

### File Categories
You will classify each learning file into one of these categories:

- **MATCHED** ✅: File matches a step AND is in the correct folder for that step's execution mode
- **MISMATCH** ⚠️: File matches a step BUT is in the wrong folder for that step's execution mode
- **CONSOLIDATED** ✅: Valid pattern file (e.g., consolidated_*, general_*) - preserve these
- **CONTENT-MATCHED** ✅: Filename doesn't match any step, but content references valid step(s)
- **ORPHANED** ⚠️: Filename doesn't match any step AND content doesn't reference any steps

## 🔄 MATCHING DECISION FLOW

For each learning file, follow this decision process:

**Step A**: Does filename match a step title?
  - YES: Is file in correct folder for step's execution mode?
    - YES → **MATCHED** ✅
    - NO → **MISMATCH** ⚠️ (suggest moving to correct folder)
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

### 2. Discover Learning Files
Use list_workspace_files to list files in:
- learnings/ and learnings/scripts/  (MCP Tool Mode learnings)
- learning_code_exec/ and learning_code_exec/code/ (Code Execution Mode learnings)

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

**For MISMATCH files** (wrong folder):
- Suggest moving to correct folder: learnings/ ↔ learning_code_exec/
- Present list to user via human_feedback and get approval
- If approved, use move_workspace_file to relocate

**For ORPHANED files** (no matches):
- Present list to user via human_feedback with deletion recommendation
- **CRITICAL**: Wait for explicit user approval before deletion
- If approved, use delete_workspace_file to remove

**For other categories** (MATCHED, CONSOLIDATED, CONTENT-MATCHED):
- No action needed - these are valid files

## 📊 EXAMPLE CLASSIFICATIONS

**Example 1 - MATCHED** (filename + correct folder):
  File: learnings/setup_aws_credentials_learning.md
  Step: Setup AWS Credentials (use_code_execution_mode: false)
  → MATCHED ✅ - Filename matches, MCP mode step, in learnings/

**Example 2 - MISMATCH** (filename matches but wrong folder):
  File: learnings/deploy_to_kubernetes_learning.md  
  Step: Deploy to Kubernetes (use_code_execution_mode: true)
  → MISMATCH ⚠️ - Code execution step with learnings in learnings/
  → Action: Suggest moving to learning_code_exec/

**Example 3 - CONSOLIDATED** (valid pattern file):
  File: learnings/consolidated_aws_patterns_learning.md
  → CONSOLIDATED ✅ - Valid pattern file from multiple steps

**Example 4 - CONTENT-MATCHED** (renamed but valid):
  File: learnings/old_step_name_learning.md
  Content: Contains step_3, Deploy Application
  Step: Deploy Application (step_3, use_code_execution_mode: false)
  → CONTENT-MATCHED ✅ - Content references valid step, correct folder

**Example 5 - ORPHANED** (no match anywhere):
  File: learnings/removed_feature_learning.md  
  Filename: Doesn't match any current  step
  Content: No references to current step IDs/titles
  → ORPHANED ⚠️ - No relevance to current plan
  → Action: Recommend deletion (with user approval)

## ⚠️ CRITICAL RULES

1. **MANDATORY HUMAN APPROVAL**: ALWAYS use human_feedback BEFORE any write/delete/move operations
2. **Never Auto-Delete**: Even obviously orphaned files require user confirmation
3. **Read-Only Planning**: You cannot modify planning/plan.json or files in planning/
4. **Access Restrictions**: Only access subdirectories in ` + templateVars["AllowedPaths"] + ` - never list root workspace
5. **Preserve Valid Files**: MATCHED, CONSOLIDATED, and CONTENT-MATCHED files should never be deleted

## ✅ SUCCESS CRITERIA

Your alignment check is complete when you have:
- ✅ Extracted execution mode for every step in the plan
- ✅ Listed all files in both learnings folders
- ✅ Classified every file into one of the 5 categories
- ✅ Identified all missing learnings
- ✅ Presented comprehensive findings via human_feedback
- ✅ Received user decision on mismatches/orphans
- ✅ (If approved) Executed cleanup/move operations
`
}

// alignmentUserMessageProcessor creates the user message for plan learnings alignment
func (agent *HumanControlledTodoPlannerPlanLearningsAlignmentAgent) alignmentUserMessageProcessor(templateVars map[string]string) string {
	return `# Plan-Learnings Alignment Check Task

**PRIMARY GOAL**: Perform a comprehensive alignment check between plan.json and learnings folders, then present findings to the user for review and action.

**Context**:
- **Workspace Path**: ` + templateVars["WorkspacePath"] + `
- **Allowed Paths**: ` + templateVars["AllowedPaths"] + `

**Current Plan** (to check alignment against):
` + func() string {
		if templateVars["PlanJSON"] != "" {
			return templateVars["PlanJSON"]
		}
		return "No plan JSON provided."
	}() + `

## YOUR TASK CHECKLIST

**Phase 1: Analysis**

1. **Extract Step Information from Plan**
   - Parse the plan.json above to extract all step IDs and titles
   - For each step, note the agent_configs.use_code_execution_mode value:
     * true → Code Execution Mode → expect learnings in learning_code_exec/
     * false or missing → MCP Tool Mode → expect learnings in learnings/
   - Create a mental map of: step ID → {title, execution_mode, expected_folder}

2. **Discover All Learning Files**
   - Use list_workspace_files to list files in learnings/ and learnings/scripts/
   - Use list_workspace_files to list files in learning_code_exec/ and learning_code_exec/code/
   - You should find all .md and .py (MCP mode) or .go (code exec mode) files

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
