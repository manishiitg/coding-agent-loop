package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for learning consolidation - panics at startup if invalid
var consolidationSystemTemplate = MustRegisterTemplate("consolidationSystem", `# Learning Consolidation Agent

## 🤖 Role
Audit and merge patterns within 'learnings/step-{X}/' folders.

## 🚨 CRITICAL RULE: Isolation
- Consolidate ONLY within the **SAME** step folder (e.g., 'learnings/step-1/').
- **NEVER** merge across step folders (e.g., do NOT merge step-1/ with step-2/).
- **NEVER** merge branch folders with parent folders.
- **NEVER** create 'general_patterns_learning.md' or 'consolidated_patterns_learning.md'. Merge into **EXISTING** step-specific files.

- **Regular/Decision/Routing**: `learnings/step-{X}/`
- **Branch Steps**: `learnings/step-{X}-{true/false}-{Y}/`
- **Orchestration Sub-Agents**: `learnings/step-{X}-sub-agent-{index}/`
- **Todo Task Sub-Agents**: `learnings/step-{X}-sub-{routeID}/`

## 🔍 CONSOLIDATION CRITERIA
1. **Duplicates**: Same tool calls/logic WITHIN the folder.
2. **Similarities**: Overlapping patterns (merge into the best/most generalized version).
3. **Outdated**: Success rate <50% or superseded by a better pattern.
4. **Score Merging**: Combined Runs = Sum(Runs); Success % = (Total Success / Total Runs) * 100.

## 📋 WORKFLOW
1. **Analysis**: Discover relative step folders in 'learnings/'. Read all variations of '*_learning.md', '.py', '.go'.
2. **Proposal**: Use 'human_feedback' to list proposed merges/deletions/updates and impact estimate.
3. **Action**: ONLY call 'update_workspace_file' or 'delete_workspace_file' AFTER explicit user approval.

*No summaries. No auto-modifications. No guessing.*`)

var consolidationUserTemplate = MustRegisterTemplate("consolidationUser", `# Learning Consolidation Task

## 📊 Objective
Analyze, merge, and clean up redundant patterns within 'learnings/' step folders.

## 📋 Context
- **Workspace**: {{.WorkspacePath}}
- **Allowed Access**: {{.AllowedPaths}}

## 🧠 Instructions
1. **Discover**: List step folders in 'learnings/' (e.g., 'step-1/', 'step-3-true-0/').
2. **Retrieve**: Read all '*.md', '.py', '.go' variations in those folders.
3. **Analyze**: Identify duplicates, similar patterns, and outdated/low-success patterns.
4. **Report**: Use 'human_feedback' to present a list of proposed merges/updates.
5. **Action**: ONLY CALL 'update_workspace_file' or 'delete_workspace_file' AFTER user approval.

**Final Goal**: Consolidate patterns into existing step files and remove redundant variations.`)

// WorkflowLearningConsolidationTemplate holds template variables for consolidation prompts
type WorkflowLearningConsolidationTemplate struct {
	WorkspacePath string
	AllowedPaths  string
}

// WorkflowLearningConsolidationAgent consolidates learnings across all learning files
type WorkflowLearningConsolidationAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator // Reference to base orchestrator for RequestHumanFeedback
}

// NewWorkflowLearningConsolidationAgent creates a new learning consolidation agent
func NewWorkflowLearningConsolidationAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *WorkflowLearningConsolidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerAnonymizationAgentType, // Reuse anonymization agent type
		eventBridge,
	)

	return &WorkflowLearningConsolidationAgent{
		BaseOrchestratorAgent: baseAgent,
		baseOrchestrator:      baseOrchestrator,
	}
}

// LearningConsolidationManager manages learning consolidation agent creation independently from controller
type LearningConsolidationManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string

	// Phase LLM config (primary LLM for learning consolidation agent)
	presetPhaseLLM *AgentLLMConfig
}

// NewLearningConsolidationManager creates a new LearningConsolidationManager
func NewLearningConsolidationManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetPhaseLLM *AgentLLMConfig,
) *LearningConsolidationManager {
	return &LearningConsolidationManager{
		BaseOrchestrator: baseOrchestrator,
		sessionID:        sessionID,
		workflowID:       workflowID,
		presetPhaseLLM:   presetPhaseLLM,
	}
}

// createLearningConsolidationAgent creates and sets up a learning consolidation agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
// Always consolidates learnings/ folder (unified folder for all learning types)
func (lcm *LearningConsolidationManager) createLearningConsolidationAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read and write access to learnings folder only
	targetPath := fmt.Sprintf("%s/learnings", workspacePath)

	// Step-specific learnings: always add access to runs/ directory for scanning step-specific folders
	// Agent has read and write access to the learnings folder
	readPaths := []string{targetPath}
	writePaths := []string{targetPath} // Write access to learnings folder for consolidation

	// Step-specific learnings are at workspace root, not inside runs/
	// Step-specific folders are directly in learnings/step-*/
	lcm.GetLogger().Info(fmt.Sprintf("🔍 Step-specific learnings are at workspace root (learnings/step-*/)"))

	lcm.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	lcm.GetLogger().Info(fmt.Sprintf("🔍 Setting folder guard for learning consolidation agent - Read paths: %v, Write paths: %v (read/write access to learnings/ folder only)", readPaths, writePaths))

	// Use preset phase LLM only
	var llmConfigToUse *orchestrator.LLMConfig
	if lcm.presetPhaseLLM != nil && lcm.presetPhaseLLM.Provider != "" && lcm.presetPhaseLLM.ModelID != "" {
		// Use preset phase LLM
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: lcm.presetPhaseLLM.Provider,
				ModelID:  lcm.presetPhaseLLM.ModelID,
			},
			Fallbacks: lcm.GetFallbacks(), // Safe: returns nil if orchestratorLLMConfig is nil
			APIKeys:   lcm.GetAPIKeys(),   // Safe: returns nil if orchestratorLLMConfig is nil
		}
		lcm.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM for learning consolidation: %s/%s", lcm.presetPhaseLLM.Provider, lcm.presetPhaseLLM.ModelID))
	} else {
		return nil, fmt.Errorf("no valid LLM configuration found for learning consolidation agent: presetPhaseLLM is empty or invalid")
	}

	// Use workspace tools directly - they already include human_feedback (created by createCustomTools in server.go)
	// No need to add human tools separately as they're already combined in WorkspaceTools
	allTools := lcm.WorkspaceTools
	allExecutors := lcm.WorkspaceToolExecutors

	// Create agent config with the selected LLM config
	config := lcm.CreateStandardAgentConfigWithLLM("learning-consolidation-agent", 100, agents.OutputFormatStructured, llmConfigToUse)

	// Consolidation agent doesn't need MCP servers - uses workspace tools only
	config.ServerNames = []string{mcpclient.NoServers}

	// Code execution mode only applies to execution agents, not learning consolidation agents
	config.UseCodeExecutionMode = false
	lcm.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for learning consolidation agent (only execution agents use MCP tools)"))

	// Large output virtual tools are enabled for consolidation (agent may generate large reports)
	enabled := true
	config.EnableContextOffloading = &enabled

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewWorkflowLearningConsolidationAgent(cfg, logger, tracer, eventBridge, lcm.BaseOrchestrator)
	}

	// Use base orchestrator's CreateAndSetupStandardAgentWithConfig to avoid code duplication
	// This handles initialization, event bridge connection, and tool registration
	// Set overwriteSystemPrompt to true for consolidation agent (replaces default MCP prompt with agent-specific prompt)
	agent, err := lcm.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"learning-consolidation",
		0, 0, // step, iteration
		"learning-consolidation", // stepID (use phase name for phase-only agents)
		createAgentFunc,
		allTools,
		allExecutors,
		true, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup learning consolidation agent: %w", err)
	}

	return agent, nil
}

// ConsolidateLearningsOnly runs only the learning consolidation check (standalone, independent from other phases)
// This is a separate workflow phase that can be run independently
func (lcm *LearningConsolidationManager) ConsolidateLearningsOnly(ctx context.Context, workspacePath string) (string, error) {
	lcm.GetLogger().Info(fmt.Sprintf("🔍 Starting standalone learning consolidation for workspace: %s", workspacePath))

	// Set workspace path
	lcm.SetWorkspacePath(workspacePath)

	// Always consolidate learnings/ folder (unified folder for all learning types)
	allowedPaths := "['learnings/']"
	selectedFolder := "learnings/"
	lcm.GetLogger().Info(fmt.Sprintf("✅ Consolidating learnings/ folder"))

	// Create consolidation agent
	consolidationAgent, err := lcm.createLearningConsolidationAgent(ctx, lcm.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("failed to create learning consolidation agent: %w", err)
	}

	// Prepare template variables with selected folder only
	consolidationTemplateVars := map[string]string{
		"WorkspacePath":  lcm.GetWorkspacePath(),
		"AllowedPaths":   allowedPaths,
		"SelectedFolder": selectedFolder,
		"SessionID":      lcm.sessionID,
		"WorkflowID":     lcm.workflowID,
	}

	// Execute consolidation agent
	lcm.GetLogger().Info(fmt.Sprintf("🔍 Executing learning consolidation agent for folder: %s", selectedFolder))
	result, conversationHistory, err := consolidationAgent.Execute(ctx, consolidationTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("learning consolidation agent execution failed: %w", err)
	}

	lcm.GetLogger().Info(fmt.Sprintf("✅ Learning consolidation completed successfully for folder: %s", selectedFolder))
	lcm.GetLogger().Info(fmt.Sprintf("🔍 Consolidation result: %s", result))

	_ = conversationHistory // Conversation history not used for standalone consolidation

	return result, nil
}

// Execute implements the OrchestratorAgent interface
func (agent *WorkflowLearningConsolidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	selectedFolder := templateVars["SelectedFolder"]

	// Provide default allowed paths if not present
	allowedPaths := templateVars["AllowedPaths"]
	if allowedPaths == "" {
		allowedPaths = "['learnings/']"
	}

	// If SelectedFolder is provided, use it to restrict access to only that folder
	if selectedFolder != "" {
		allowedPaths = fmt.Sprintf("['%s']", selectedFolder)
	}

	// Prepare template variables
	consolidationTemplateVars := map[string]string{
		"WorkspacePath":  workspacePath,
		"AllowedPaths":   allowedPaths,
		"SelectedFolder": selectedFolder,
	}

	// Create template data for consolidation
	templateData := WorkflowLearningConsolidationTemplate{
		WorkspacePath: workspacePath,
		AllowedPaths:  allowedPaths,
	}

	// Generate system prompt and user message separately
	systemPrompt := agent.consolidationSystemPromptProcessor(consolidationTemplateVars)
	userMessage := agent.consolidationUserMessageProcessor(consolidationTemplateVars)

	// Get logger from base agent's MCP agent
	baseAgent := agent.GetBaseAgent()
	var logger loggerv2.Logger
	if baseAgent != nil {
		mcpAgent := baseAgent.Agent()
		if mcpAgent != nil && mcpAgent.Logger != nil {
			logger = mcpAgent.Logger
		}
	}

	// Maximum iterations for consolidation analysis
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
			logger.Info(fmt.Sprintf("🔍 Learning consolidation agent iteration %d/%d", iteration, maxIterations))
		}

		// Create a simple input processor that returns the user message
		inputProcessor := func(map[string]string) string {
			return userMessage
		}

		// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
		result, updatedConversationHistory, err := agent.ExecuteWithTemplateValidation(ctx, consolidationTemplateVars, inputProcessor, currentConversationHistory, templateData, systemPrompt, true)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedConversationHistory

		// After execution, ask if user wants to continue (blocking feedback)
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			if logger != nil {
				logger.Info(fmt.Sprintf("🔍 Learning consolidation agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations))
			}

			// Generate unique request ID
			requestID := fmt.Sprintf("learning_consolidation_continue_%d_%d", iteration, time.Now().UnixNano())

			// Request human feedback (blocking call)
			approved, feedback, err := agent.baseOrchestrator.RequestHumanFeedback(
				ctx,
				requestID,
				fmt.Sprintf("Learning consolidation analysis is complete (iteration %d/%d). Would you like to ask more questions or request additional consolidation?", iteration, maxIterations),
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
					logger.Info(fmt.Sprintf("✅ User approved - learning consolidation complete"))
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
		logger.Info(fmt.Sprintf("🔍 Learning consolidation completed after %d iterations", iteration))
	}

	return currentResult, currentConversationHistory, nil
}

// consolidationSystemPromptProcessor creates the system prompt for learning consolidation
func (agent *WorkflowLearningConsolidationAgent) consolidationSystemPromptProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := consolidationSystemTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing consolidation system prompt template: " + err.Error()
	}
	return result.String()
}

// consolidationUserMessageProcessor creates the user message for learning consolidation
func (agent *WorkflowLearningConsolidationAgent) consolidationUserMessageProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := consolidationUserTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing consolidation user message template: " + err.Error()
	}
	return result.String()
}
