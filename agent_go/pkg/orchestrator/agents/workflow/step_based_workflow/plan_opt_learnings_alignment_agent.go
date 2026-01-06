package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// WorkflowPlanLearningsAlignmentTemplate holds template variables for alignment prompts
type WorkflowPlanLearningsAlignmentTemplate struct {
	WorkspacePath       string
	PlanJSON            string
	AllowedPaths        string
	SelectedFolder      string
	IsCodeExecutionMode string
}

// WorkflowPlanLearningsAlignmentAgent checks alignment between plan and learnings
type WorkflowPlanLearningsAlignmentAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator // Reference to base orchestrator for RequestHumanFeedback
}

// NewWorkflowPlanLearningsAlignmentAgent creates a new plan learnings alignment agent
func NewWorkflowPlanLearningsAlignmentAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *WorkflowPlanLearningsAlignmentAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerAnonymizationAgentType, // Reuse anonymization agent type (or create new one if needed)
		eventBridge,
	)

	return &WorkflowPlanLearningsAlignmentAgent{
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

	// Phase LLM config (primary LLM for plan learnings alignment agent)
	presetPhaseLLM *AgentLLMConfig
}

// NewPlanLearningsAlignmentManager creates a new PlanLearningsAlignmentManager
func NewPlanLearningsAlignmentManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetPhaseLLM *AgentLLMConfig,
) *PlanLearningsAlignmentManager {
	return &PlanLearningsAlignmentManager{
		BaseOrchestrator: baseOrchestrator,
		sessionID:        sessionID,
		workflowID:       workflowID,
		presetPhaseLLM:   presetPhaseLLM,
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

	// Use preset phase LLM if available, otherwise fall back to orchestrator default
	orchestratorLLMConfig := plam.GetLLMConfig()
	var llmConfigToUse *orchestrator.LLMConfig
	if plam.presetPhaseLLM != nil && plam.presetPhaseLLM.Provider != "" && plam.presetPhaseLLM.ModelID != "" {
		// Use preset phase LLM
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: plam.presetPhaseLLM.Provider,
				ModelID:  plam.presetPhaseLLM.ModelID,
			},
			Fallbacks: plam.GetFallbacks(), // Safe: returns nil if orchestratorLLMConfig is nil
			APIKeys:   plam.GetAPIKeys(),   // Safe: returns nil if orchestratorLLMConfig is nil
		}
		plam.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM for plan learnings alignment: %s/%s", plam.presetPhaseLLM.Provider, plam.presetPhaseLLM.ModelID))
	} else if orchestratorLLMConfig != nil && orchestratorLLMConfig.Primary.Provider != "" && orchestratorLLMConfig.Primary.ModelID != "" {
		// Fall back to orchestrator default
		llmConfigToUse = orchestratorLLMConfig
		plam.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default alignment LLM: %s/%s", orchestratorLLMConfig.Primary.Provider, orchestratorLLMConfig.Primary.ModelID))
	} else {
		return nil, fmt.Errorf("no valid LLM configuration found for plan learnings alignment agent: presetPhaseLLM and orchestrator default LLM are both empty or invalid")
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
		return NewWorkflowPlanLearningsAlignmentAgent(cfg, logger, tracer, eventBridge, plam.BaseOrchestrator)
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
func (agent *WorkflowPlanLearningsAlignmentAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
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
	templateData := WorkflowPlanLearningsAlignmentTemplate{
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
func (agent *WorkflowPlanLearningsAlignmentAgent) alignmentSystemPromptProcessor(templateVars map[string]string) string {
	templateStr := `# Plan-Learnings Alignment Agent

## 🤖 Role
Expert auditor ensuring consistency between 'plan.json' and the 'learnings/' folder structure.

## 📁 FOLDER ARCHITECTURE (MANDATORY)
Navigate the workspace to discover all step-specific folders:
- **Regular/Decision/Routing**: ` + "`learnings/step-{X}/`" + `
- **Branch Steps**: ` + "`learnings/step-{X}-{true/false}-{Y}/`" + `
- **Orchestration Sub-Agents**: ` + "`learnings/step-{X}-sub-agent-{index}/`" + `

## 🔍 ALIGNMENT ALGORITHM
1. **Discover**: List 'learnings/' to find all step folders. List files within each.
2. **Classify**:
   - **MATCHED ✅**: Filename + Folder are correct for a plan step.
   - **MISMATCH ⚠️**: Valid filename/content but in WRONG folder (suggest move).
   - **CONSOLIDATED ✅**: Valid pattern files (prefix 'consolidated_', 'general_').
   - **CORRECTNESS 🛡️**: Check for "Fake Success" (mocks/hallucinations).
     - **VERIFIED_OK**: Claims match workspace reality.
     - **SUSPICIOUS**: Hardcoded "success", simulated results, or stubs.
     - **UNVERIFIABLE**: Cannot verify with available tools.
   - **ORPHANED 🗑️**: No relevance to current plan (suggest delete).

## 📋 OUTPUT WORKFLOW
1. **Scan Changelog**: Read 'planning/changelog/changelog-*.json' to identify recently changed steps.
2. **Audit**: Categorize every file and check for missing learnings per step.
3. **Report**: Use 'human_feedback' to present findings and recommended moves/deletions.
4. **Action**: ONLY perform 'move_workspace_file' or 'delete_workspace_file' AFTER user approval.

*Be precise. No auto-deletion. No guessing.*`

	tmpl, err := template.New("alignmentSystemPrompt").Parse(templateStr)
	if err != nil {
		return "Error parsing alignment system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
		return "Error executing alignment system prompt template: " + err.Error()
	}
	return result.String()
}

// alignmentUserMessageProcessor creates the user message for plan learnings alignment
func (agent *WorkflowPlanLearningsAlignmentAgent) alignmentUserMessageProcessor(templateVars map[string]string) string {
	templateStr := `# Plan-Learnings Alignment Check

## 📊 TASK
Verify all learning files against the current plan. Categorize files and detect inconsistencies or mocks.

## 📋 CONTEXT
- **Workspace**: {{.WorkspacePath}}
- **Allowed Access**: {{.AllowedPaths}}

## 🧠 ALIGNMENT CHECKLIST
1. **Phase 0**: Scan 'planning/changelog/' for recent step changes.
2. **Phase 1**: Analyze 'plan.json' (below) to map step IDs to their expected 'learnings/' subfolders.
3. **Phase 2**: Discover files in ALL step folders. Categorize (MATCHED, MISMATCH, CONSOLIDATED, ORPHANED).
4. **Phase 3**: Audit for **CorrectnessStatus** (VERIFIED_OK, SUSPICIOUS, UNVERIFIABLE).
5. **Phase 4**: Report findings via 'human_feedback'.

## 📄 CURRENT PLAN
{{.PlanJSON}}

**Final Goal**: Present a clear report and await user approval for any file relocations or deletions.`

	tmpl, err := template.New("alignmentUserMessage").Parse(templateStr)
	if err != nil {
		return "Error parsing alignment user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
		return "Error executing alignment user message template: " + err.Error()
	}
	return result.String()
}
