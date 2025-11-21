package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
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
}

// NewPlanLearningsAlignmentManager creates a new PlanLearningsAlignmentManager
func NewPlanLearningsAlignmentManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
) *PlanLearningsAlignmentManager {
	return &PlanLearningsAlignmentManager{
		BaseOrchestrator: baseOrchestrator,
		sessionID:        sessionID,
		workflowID:       workflowID,
	}
}

// createPlanLearningsAlignmentAgent creates and sets up a plan learnings alignment agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
func (plam *PlanLearningsAlignmentManager) createPlanLearningsAlignmentAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read-only access to planning/ and learnings/ folders
	planningPath := fmt.Sprintf("%s/planning", workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)

	// Agent has read-only access to planning/ folder (for plan.json) and learnings/ folder (for learning files)
	readPaths := []string{planningPath, learningsPath}
	writePaths := []string{} // No write access - read-only agent
	plam.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	plam.GetLogger().Infof("🔍 Setting folder guard for plan learnings alignment agent - Read paths: %v, Write paths: %v (read-only access to planning/ and learnings/ folders)", readPaths, writePaths)

	// Use orchestrator default LLM config
	llmConfigToUse := plam.GetLLMConfig()
	plam.GetLogger().Infof("🔧 Using orchestrator default alignment LLM: %s/%s", plam.GetProvider(), plam.GetModel())

	// Use workspace tools directly - they already include human_feedback (created by createCustomTools in server.go)
	// No need to add human tools separately as they're already combined in WorkspaceTools
	allTools := plam.WorkspaceTools
	allExecutors := plam.WorkspaceToolExecutors

	// Create agent config with the selected LLM config
	config := plam.CreateStandardAgentConfigWithLLM("plan-learnings-alignment-agent", 100, agents.OutputFormatStructured, llmConfigToUse)

	// Alignment agent doesn't need MCP servers - uses workspace tools only
	config.ServerNames = []string{mcpclient.NoServers}

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
	allowedPaths := "['planning/', 'learnings/']"
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
**PRIMARY PURPOSE**: Check alignment between plan.json and learnings folder to identify:
- Orphaned learning files (files for steps that no longer exist in the plan)
- Missing learnings (steps in plan that don't have corresponding learning files)
- Mismatched learnings (learning files that might need updates due to step changes)

Your main goal is to analyze the plan and learnings folder, identify misalignments, and present findings to the user for review.

## 🎯 ALIGNMENT CHECK PROCESS
1. **Understand the Plan**: Review the plan.json (provided in PlanJSON) to extract:
   - All step IDs (including branch steps in if_true_steps and if_false_steps)
   - Step titles (used for matching learning file names)
   - Step structure and hierarchy

2. **Discover Learnings Folder**: Use 'list_workspace_files' to explore the learnings folder:
   - List files in 'learnings/' folder (look for *_learning.md files)
   - List files in 'learnings/scripts/' folder (look for *_script.py files)
   - Read learning files to understand their content and match them to plan steps

3. **Match Learnings to Steps**: For each learning file:
   - Extract the step title from filename (format: {StepTitle}_learning.md or {StepTitle}_script.py)
   - Match against step titles in the plan (use fuzzy matching - normalize titles by removing special chars, converting to lowercase)
   - Check if step ID exists in plan (for orphaned files, the step might have been deleted)

4. **Identify Misalignments**:
   - **Orphaned Learnings**: Learning files that don't match any step in the current plan
   - **Missing Learnings**: Steps in plan that don't have corresponding learning files
   - **Potentially Stale**: Learning files for steps that were renamed or modified

5. **Present Findings**: Use 'human_feedback' tool to present:
   - Summary of alignment status
   - List of orphaned learning files (if any)
   - List of steps without learnings (if any)
   - Recommendations for what to do with orphaned files

## ⚠️ IMPORTANT RULES
- **Read-Only**: You cannot modify or delete files. You can only analyze and report.
- **Restricted Access**: You ONLY have access to these subdirectories: ` + templateVars["AllowedPaths"] + `
   - You CANNOT list the root workspace (folder=".").
   - Always start listing from the allowed subdirectories (e.g., folder="learnings" or folder="planning").
- **Pathing**: All tool paths are relative to the Workspace Path provided.
- **Focus on Alignment**: Your primary output should be alignment analysis and recommendations. Present findings clearly and ask user what they want to do with orphaned files.

## 🔍 MATCHING STRATEGY
- Learning files typically follow pattern: {StepTitle}_learning.md or {StepTitle}_script.py
- Step titles in plan may have different formatting (spaces, special chars)
- Use fuzzy matching: normalize both filename and step title (lowercase, remove special chars, replace spaces with underscores)
- If exact match not found, try partial matches (filename contains step title or vice versa)
- Consider reading learning file content to find step ID references if filename matching fails
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
1. **Extract all step IDs from plan**: Review the plan.json above and extract all step IDs (including branch steps in if_true_steps and if_false_steps). Note step titles for matching.

2. **Discover learnings folder**: Use 'list_workspace_files' (folder="learnings") to list all learning files:
   - Look for *.md files in learnings/ folder
   - Look for *.py files in learnings/scripts/ folder

3. **Match learnings to steps**: For each learning file:
   - Extract step title from filename (remove _learning.md or _script.py suffix)
   - Normalize filename and step titles (lowercase, remove special chars, replace spaces with underscores)
   - Match against step titles in plan
   - Identify orphaned files (no matching step) and missing learnings (steps without files)

4. **Present findings**: Use 'human_feedback' tool to present:
   - Summary: Total steps, total learning files, alignment status
   - Orphaned learning files (if any) - files for deleted steps
   - Steps without learnings (if any) - new steps that don't have learning files yet
   - Ask user what they want to do with orphaned files (keep, delete, review individually)

**IMPORTANT**: 
- You cannot delete or modify files - only analyze and report
- Use fuzzy matching for step titles (normalize before comparing)
- Present findings clearly and ask user for approval or feedback
`
}
