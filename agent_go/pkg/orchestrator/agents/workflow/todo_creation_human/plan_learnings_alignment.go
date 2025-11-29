package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/mcpclient"
	"mcpagent/observability"
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
			Provider:              plam.presetPlanLearningsAlignmentLLM.Provider,
			ModelID:               plam.presetPlanLearningsAlignmentLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback,
			APIKeys:               orchestratorLLMConfig.APIKeys,
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
**PRIMARY PURPOSE**: Check alignment between plan.json and learnings folders to identify:
- Orphaned learning files (files for steps that no longer exist in the plan)
- Missing learnings (steps in plan that don't have corresponding learning files)
- Mismatched learnings (learning files that might need updates due to step changes)

Your main goal is to analyze the plan and learnings folders, identify misalignments, and present findings to the user for review.

## 📁 UNDERSTANDING THE TWO LEARNINGS FOLDERS

**IMPORTANT**: There are TWO separate learnings folders, each serving different execution modes:

1. **learnings/ folder** - MCP Tool Execution Mode:
   - Contains learning files for steps executed using **direct MCP tool calls**
   - Files: {StepTitle}_learning.md (tool call patterns, success/failure patterns)
   - Scripts: learnings/scripts/{StepTitle}_script.py (Python scripts for tool automation)
   - Used when execution agent makes direct MCP tool calls (not code execution mode)

2. **learning_code_exec/ folder** - Code Execution Mode:
   - Contains learning files for steps executed using **Go code generation**
   - Files: learning_code_exec/{StepTitle}_learning.md (Go code patterns, success/failure patterns)
   - Code: learning_code_exec/code/{StepTitle}_code.go (actual Go code examples)
   - Used when execution agent writes and executes Go code (code execution mode)

**Key Points**:
- A step can have learnings in **either** folder (depending on which mode was used)
- A step can have learnings in **both** folders (if executed in both modes at different times)
- Consolidated files can exist in either folder (e.g., consolidated_patterns_learning.md in learnings/ or learning_code_exec/)
- When matching files to steps, check **both folders** - a step might have learnings in one or both

## 🎯 ALIGNMENT CHECK PROCESS
1. **Understand the Plan**: Review the plan.json (provided in PlanJSON) to extract:
   - All step IDs (including branch steps in if_true_steps and if_false_steps)
   - Step titles (used for matching learning file names)
   - Step structure and hierarchy
   - **CRITICAL**: Check each step's agent_configs.use_code_execution_mode field to determine which folder to check:
     - If step has agent_configs.use_code_execution_mode: true → step uses CODE EXECUTION MODE → learnings should be in learning_code_exec/ folder ONLY
     - If step has agent_configs.use_code_execution_mode: false or field is missing/nil → step uses MCP TOOL MODE → learnings should be in learnings/ folder ONLY
   - **IMPORTANT**: Each step should have learnings in ONLY ONE folder based on its execution mode (not both folders)

2. **Discover Learnings Folders**: Use 'list_workspace_files' to explore both learnings folders:
   
   **Folder 1 - learnings/ (MCP Tool Execution Mode)**:
   - List files in 'learnings/' folder (look for *_learning.md files - MCP tool patterns)
   - List files in 'learnings/scripts/' folder (look for *_script.py files - Python automation scripts)
   - These contain learnings for steps executed using direct MCP tool calls
   
   **Folder 2 - learning_code_exec/ (Code Execution Mode)**:
   - List files in 'learning_code_exec/' folder (look for *_learning.md files - Go code patterns)
   - List files in 'learning_code_exec/code/' folder (look for *_code.go files - actual Go code examples)
   - These contain learnings for steps executed using Go code generation
   
   **CRITICAL FOLDER RULES**:
   - **Code Execution Steps** (agent_configs.use_code_execution_mode: true): Learnings should be in learning_code_exec/ folder ONLY
   - **MCP Tool Steps** (agent_configs.use_code_execution_mode: false or missing): Learnings should be in learnings/ folder ONLY
   - **Mismatch Detection**: If a code execution step has learnings in learnings/ folder, or an MCP tool step has learnings in learning_code_exec/ folder, flag this as a MISMATCH
   - **CRITICAL**: You MUST read the content of ALL learning files to properly match them to steps (see matching strategy below)

3. **Match Learnings to Steps** (MULTI-STEP PROCESS - DO NOT SKIP CONTENT READING):
   
   **Step 3a - Determine Step Execution Mode (CRITICAL FIRST STEP)**:
   - For each step in the plan, check its agent_configs.use_code_execution_mode field:
     - If true → step uses CODE EXECUTION MODE → expect learnings in learning_code_exec/ folder ONLY
     - If false or missing/nil → step uses MCP TOOL MODE → expect learnings in learnings/ folder ONLY
   - **This determines which folder to check for each step's learnings**
   
   **Step 3b - Filename Matching (Second Pass)**:
   - Extract the step title from filename (format: {StepTitle}_learning.md or {StepTitle}_script.py)
   - Match against step titles in the plan (use fuzzy matching - normalize titles by removing special chars, converting to lowercase)
   - **CRITICAL**: When matching, check if the file is in the CORRECT folder based on step's execution mode:
     - Code execution step → file should be in learning_code_exec/ folder
     - MCP tool step → file should be in learnings/ folder
   - If filename matches a step AND is in the correct folder, mark file as matched to that step
   - If filename matches a step BUT is in the WRONG folder, mark as MISMATCH (wrong folder for execution mode)
   
   **Step 3c - Consolidated File Recognition (Third Pass)**:
   - Check if filename matches consolidated/general pattern files:
     - Files starting with "consolidated_" (e.g., consolidated_patterns_learning.md, consolidated_aws_patterns_learning.md)
     - Files starting with "general_" (e.g., general_patterns_learning.md)
     - Files containing "_patterns_" in the name
   - **These files are VALID and should NOT be marked as orphaned** - they contain consolidated patterns from multiple steps
   - Mark these as "consolidated files" (valid, not orphaned)
   
   **Step 3d - Content-Based Matching (Fourth Pass - MANDATORY for unmatched files)**:
   - **CRITICAL**: For files that didn't match in Step 3b or 3c, you MUST read the file content using 'read_workspace_file'
   - Search file content for:
     - Step IDs from the plan (look for step ID references in the content)
     - Step titles from the plan (look for step title mentions in the content)
     - Pattern descriptions that reference specific steps
   - If content contains references to any step IDs or step titles from the plan:
     - Check if the file is in the CORRECT folder based on that step's execution mode
     - If in correct folder → mark file as matched to those steps
     - If in WRONG folder → mark as MISMATCH (content matches step but folder is wrong for execution mode)
   - **This handles renamed files (move_file operations) and merged files** - they may have different filenames but still contain valid learnings for steps
   
   **Step 3e - Final Orphan Detection**:
   - Only mark a file as "orphaned" if ALL of the following are true:
     - Filename doesn't match any step title (Step 3b failed)
     - Filename doesn't match consolidated/general patterns (Step 3c failed)
     - File content doesn't reference any step IDs or step titles from the plan (Step 3d failed)
   - **Files that match in ANY of the above steps should NOT be marked as orphaned**
   - **Exception**: Files in wrong folder for execution mode should be flagged as MISMATCH, not orphaned

4. **Identify Misalignments**:
   - **Orphaned Learnings**: Learning files that don't match any step in the current plan (after checking filename AND content)
   - **Missing Learnings**: Steps in plan that don't have corresponding learning files in the correct folder
   - **Folder Mismatches**: Learning files in the WRONG folder for the step's execution mode:
     - Code execution step (use_code_execution_mode: true) with learnings in learnings/ folder → MISMATCH
     - MCP tool step (use_code_execution_mode: false/missing) with learnings in learning_code_exec/ folder → MISMATCH
   - **Potentially Stale**: Learning files for steps that were renamed or modified (but still contain valid learnings)

5. **Present Findings**: Use 'human_feedback' tool to present:
   - Summary of alignment status
   - **Folder Mismatches** (if any): Files in wrong folder for step's execution mode (code execution step with learnings in learnings/, or MCP tool step with learnings in learning_code_exec/)
   - List of orphaned learning files (if any)
   - List of steps without learnings in the correct folder (if any)
   - Recommendations for what to do with orphaned files and folder mismatches

6. **Get User Approval Before Any Write Operations**: 
   - **CRITICAL**: You MUST use 'human_feedback' tool to get explicit user approval BEFORE calling any write/delete tools
   - Present the list of files you want to delete and wait for user confirmation
   - Only proceed with deletion after receiving explicit approval via 'human_feedback' response
   - Never delete files without first getting user confirmation through 'human_feedback'

7. **Clean Up Orphaned Files** (only after user approval):
   - After receiving approval via 'human_feedback', you can delete orphaned learning files using 'delete_workspace_file' tool
   - Only delete files that the user explicitly approved for deletion
   - Be careful to only delete files in learnings/ folders, never modify planning/ folder

## ⚠️ IMPORTANT RULES
- **MANDATORY HUMAN CONFIRMATION**: You MUST use 'human_feedback' tool to get user approval BEFORE any write/delete/edit operations. Never call write tools (delete_workspace_file, write_workspace_file, update_workspace_file, etc.) without first getting explicit confirmation via 'human_feedback'. The 'human_feedback' tool is available in your tool list - use it to pause execution and get user input.
- **Write Access**: You have write access to learnings/ folders and can delete orphaned learning files, but ONLY after user approval via 'human_feedback'. The planning/ folder is read-only (you cannot modify plan.json).
- **Restricted Access**: You ONLY have access to these subdirectories: ` + templateVars["AllowedPaths"] + `
   - You CANNOT list the root workspace (folder=".").
   - Always start listing from the allowed subdirectories (e.g., folder="learnings", folder="learning_code_exec", or folder="planning").
- **Pathing**: All tool paths are relative to the Workspace Path provided.
- **Workflow**: Analyze → Present findings with 'human_feedback' → Wait for user approval → Then delete approved files

## 🔍 MATCHING STRATEGY (MULTI-LAYER APPROACH)

**Layer 1 - Filename Matching**:
- Learning files typically follow pattern: {StepTitle}_learning.md or {StepTitle}_script.py
- Step titles in plan may have different formatting (spaces, special chars)
- Use fuzzy matching: normalize both filename and step title (lowercase, remove special chars, replace spaces with underscores)
- If exact match not found, try partial matches (filename contains step title or vice versa)

**Layer 2 - Consolidated File Recognition**:
- **Recognize consolidated files** (these are VALID and should NOT be marked as orphaned):
  - Files starting with "consolidated_" prefix (e.g., consolidated_patterns_learning.md, consolidated_aws_patterns_learning.md)
  - Files starting with "general_" prefix (e.g., general_patterns_learning.md)
  - Files containing "_patterns_" in the name
- These files contain consolidated patterns from multiple steps and are intentionally not tied to a single step
- **DO NOT mark consolidated files as orphaned** - they are valid learning files

**Layer 3 - Content-Based Matching (MANDATORY for unmatched files)**:
- **CRITICAL**: If filename doesn't match (Layer 1) and isn't a consolidated file (Layer 2), you MUST read the file content
- Use 'read_workspace_file' to read the complete file content
- Search content for:
  - Step IDs from the plan (exact matches or partial matches)
  - Step titles from the plan (exact matches or partial matches)
  - References to step descriptions or step contexts
- **This handles**:
  - **Renamed files**: Files that were moved/renamed but still contain valid learnings (content has step references)
  - **Merged files**: Files that combine learnings from multiple steps (content references multiple step IDs/titles)
  - **Files with non-standard naming**: Files that don't follow naming convention but contain valid learnings
- If content references any step from the plan, mark file as matched to those steps

**Layer 4 - Orphan Detection (Only after all layers)**:
- A file is only "orphaned" if:
  - Filename doesn't match any step (Layer 1 failed)
  - Filename doesn't match consolidated patterns (Layer 2 failed)
  - Content doesn't reference any step IDs or titles (Layer 3 failed)
- **Files that match in ANY layer should NOT be marked as orphaned**
`
}

// alignmentUserMessageProcessor creates the user message for plan learnings alignment
func (agent *HumanControlledTodoPlannerPlanLearningsAlignmentAgent) alignmentUserMessageProcessor(templateVars map[string]string) string {
	return `# Plan-Learnings Alignment Check Task

**PRIMARY GOAL**: Check alignment between plan.json and learnings folder, identify misalignments, and present findings to the user.

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

**YOUR TASKS**:
1. **Extract all step IDs and execution modes from plan**: Review the plan.json above and extract:
   - All step IDs (including branch steps in if_true_steps and if_false_steps)
   - Step titles for matching
   - **CRITICAL**: For each step, check agent_configs.use_code_execution_mode field:
     - If true → step uses CODE EXECUTION MODE → learnings should be in learning_code_exec/ folder ONLY
     - If false or missing/nil → step uses MCP TOOL MODE → learnings should be in learnings/ folder ONLY
   - **This determines which folder to check for each step's learnings**

2. **Discover learnings folders**: Use 'list_workspace_files' to list all learning files from both folders:
   
   **Folder 1 - learnings/ (MCP Tool Execution Mode)**:
   - Look for *.md files in learnings/ folder (MCP tool patterns)
   - Look for *.py files in learnings/scripts/ folder (Python automation scripts)
   - These are for steps executed using direct MCP tool calls
   
   **Folder 2 - learning_code_exec/ (Code Execution Mode)**:
   - Look for *.md files in learning_code_exec/ folder (Go code patterns)
   - Look for *.go files in learning_code_exec/code/ folder (actual Go code examples)
   - These are for steps executed using Go code generation
   
   **Important**: A step can have learnings in either folder, both folders, or neither. Check both folders when matching.

3. **Match learnings to steps** (MULTI-LAYER MATCHING - DO NOT SKIP CONTENT READING):
   
   **Layer 0 - Determine Step Execution Mode (CRITICAL FIRST STEP)**:
   - For each step, check agent_configs.use_code_execution_mode:
     - true → CODE EXECUTION MODE → expect learnings in learning_code_exec/ ONLY
     - false or missing → MCP TOOL MODE → expect learnings in learnings/ ONLY
   
   **Layer 1 - Filename Matching**:
   - Extract step title from filename (remove _learning.md or _script.py suffix)
   - Normalize filename and step titles (lowercase, remove special chars, replace spaces with underscores)
   - Match against step titles in plan
   - **CRITICAL**: Check if file is in CORRECT folder based on step's execution mode:
     - Code execution step → file should be in learning_code_exec/
     - MCP tool step → file should be in learnings/
   - If filename matches AND folder is correct → mark as matched
   - If filename matches BUT folder is WRONG → mark as MISMATCH (wrong folder)
   
   **Layer 2 - Consolidated File Recognition**:
   - Check if filename matches consolidated/general patterns:
     - Files starting with "consolidated_" (e.g., consolidated_patterns_learning.md)
     - Files starting with "general_" (e.g., general_patterns_learning.md)
     - Files containing "_patterns_" in the name
   - **These files are VALID** - they contain consolidated patterns from multiple steps
   - Mark as "consolidated files" (valid, not orphaned)
   
   **Layer 3 - Content-Based Matching (MANDATORY for unmatched files)**:
   - **CRITICAL**: For files that didn't match in Layer 1 or 2, you MUST read the file content using 'read_workspace_file'
   - Search file content for:
     - Step IDs from the plan (look for step ID references)
     - Step titles from the plan (look for step title mentions)
     - Pattern descriptions that reference specific steps
   - If content references any step from the plan, mark file as matched to those steps
   - **This handles renamed files (move_file) and merged files** - they may have different filenames but still contain valid learnings
   
   **Layer 4 - Final Orphan Detection**:
   - Only mark as "orphaned" if ALL layers failed:
     - Filename doesn't match any step (Layer 1 failed)
     - Filename doesn't match consolidated patterns (Layer 2 failed)
     - Content doesn't reference any step IDs or titles (Layer 3 failed)
   - **Files that match in ANY layer should NOT be marked as orphaned**
   
   - Identify missing learnings (steps without files)

4. **Present findings**: Use 'human_feedback' tool to present:
   - Summary: Total steps, total learning files, alignment status
   - **Folder Mismatches** (if any): Files in wrong folder for step's execution mode:
     - Code execution steps with learnings in learnings/ folder
     - MCP tool steps with learnings in learning_code_exec/ folder
   - Consolidated files found (files like consolidated_* or general_* - these are valid and should be preserved)
   - Files matched by content (files that don't match by filename but contain valid step references)
   - Orphaned learning files (if any) - files that don't match by filename, aren't consolidated, AND don't reference any steps in content
   - Steps without learnings in the correct folder (if any) - new steps that don't have learning files yet
   - Ask user what they want to do with orphaned files and folder mismatches (keep, delete, move to correct folder, review individually)
   - **List specific files you propose to delete or move** and request explicit approval
   - **IMPORTANT**: Clearly distinguish between:
     - Consolidated files (valid, preserve)
     - Content-matched files (valid, preserve)
     - Truly orphaned files (candidates for deletion, but only with user approval)

5. **Get user approval BEFORE any deletions**:
   - **CRITICAL**: You MUST use 'human_feedback' tool to get explicit user approval BEFORE calling 'delete_workspace_file'
   - Present the exact list of files you want to delete
   - Wait for user confirmation in the 'human_feedback' response
   - Do NOT proceed with deletion until you receive explicit approval

6. **Clean up orphaned files** (ONLY after receiving approval via 'human_feedback'):
   - After receiving approval in 'human_feedback' response, you can delete orphaned learning files using 'delete_workspace_file' tool
   - Only delete files that the user explicitly approved for deletion
   - Be careful to only delete files in learnings/ folders, never modify planning/ folder
   - If user says "no" or "keep", do NOT delete any files

**CRITICAL WORKFLOW RULES**: 
- **MANDATORY**: Always use 'human_feedback' tool BEFORE any write/delete/edit operations
- **NEVER** call 'delete_workspace_file', 'write_workspace_file', or 'update_workspace_file' without first getting user approval via 'human_feedback'
- The planning/ folder is read-only - you cannot modify plan.json
- **MANDATORY CONTENT READING**: You MUST read the content of ALL learning files that don't match by filename - do NOT skip this step
- **PRESERVE CONSOLIDATED FILES**: Never mark consolidated_* or general_* files as orphaned - they are valid
- **PRESERVE CONTENT-MATCHED FILES**: Files that reference steps in their content are valid, even if filename doesn't match
- Use multi-layer matching: Filename → Consolidated patterns → Content matching → Orphan detection
- Workflow: Analyze (with content reading) → Present with 'human_feedback' → Wait for approval → Then delete (if approved)
`
}
