package todo_creation_human

import (
	"context"
	"fmt"
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

// HumanControlledTodoPlannerLearningConsolidationTemplate holds template variables for consolidation prompts
type HumanControlledTodoPlannerLearningConsolidationTemplate struct {
	WorkspacePath string
	AllowedPaths  string
}

// HumanControlledTodoPlannerLearningConsolidationAgent consolidates learnings across all learning files
type HumanControlledTodoPlannerLearningConsolidationAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator // Reference to base orchestrator for RequestHumanFeedback
}

// NewHumanControlledTodoPlannerLearningConsolidationAgent creates a new learning consolidation agent
func NewHumanControlledTodoPlannerLearningConsolidationAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *HumanControlledTodoPlannerLearningConsolidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerAnonymizationAgentType, // Reuse anonymization agent type
		eventBridge,
	)

	return &HumanControlledTodoPlannerLearningConsolidationAgent{
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

	// Learning LLM config (primary LLM for learning consolidation agent)
	presetLearningLLM *AgentLLMConfig
}

// NewLearningConsolidationManager creates a new LearningConsolidationManager
func NewLearningConsolidationManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetLearningLLM *AgentLLMConfig,
) *LearningConsolidationManager {
	return &LearningConsolidationManager{
		BaseOrchestrator:  baseOrchestrator,
		sessionID:         sessionID,
		workflowID:        workflowID,
		presetLearningLLM: presetLearningLLM,
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

	// Use preset learning LLM if available, otherwise fall back to orchestrator default
	orchestratorLLMConfig := lcm.GetLLMConfig()
	var llmConfigToUse *orchestrator.LLMConfig
	if lcm.presetLearningLLM != nil && lcm.presetLearningLLM.Provider != "" && lcm.presetLearningLLM.ModelID != "" {
		// Use preset learning LLM
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              lcm.presetLearningLLM.Provider,
			ModelID:               lcm.presetLearningLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback,
			APIKeys:               orchestratorLLMConfig.APIKeys,
		}
		lcm.GetLogger().Info(fmt.Sprintf("🔧 Using preset learning LLM for learning consolidation: %s/%s", lcm.presetLearningLLM.Provider, lcm.presetLearningLLM.ModelID))
	} else {
		// Fall back to orchestrator default
		llmConfigToUse = orchestratorLLMConfig
		lcm.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default consolidation LLM: %s/%s", lcm.GetProvider(), lcm.GetModel()))
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
	config.EnableLargeOutputVirtualTools = &enabled

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewHumanControlledTodoPlannerLearningConsolidationAgent(cfg, logger, tracer, eventBridge, lcm.BaseOrchestrator)
	}

	// Use base orchestrator's CreateAndSetupStandardAgentWithConfig to avoid code duplication
	// This handles initialization, event bridge connection, and tool registration
	// Set overwriteSystemPrompt to true for consolidation agent (replaces default MCP prompt with agent-specific prompt)
	agent, err := lcm.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"learning-consolidation",
		0, 0, // step, iteration
		createAgentFunc,
		allTools,
		allExecutors,
		true, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to create and setup learning consolidation agent: %w", err), nil)
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
		return "", fmt.Errorf(fmt.Sprintf("failed to create learning consolidation agent: %w", err), nil)
	}

	// Prepare template variables with selected folder only
	consolidationTemplateVars := map[string]string{
		"WorkspacePath":            lcm.GetWorkspacePath(),
		"AllowedPaths":             allowedPaths,
		"SelectedFolder":           selectedFolder,
		"SessionID":                lcm.sessionID,
		"WorkflowID":               lcm.workflowID,
		"UseStepSpecificLearnings": "true",
	}

	// Execute consolidation agent
	lcm.GetLogger().Info(fmt.Sprintf("🔍 Executing learning consolidation agent for folder: %s", selectedFolder))
	result, conversationHistory, err := consolidationAgent.Execute(ctx, consolidationTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("learning consolidation agent execution failed: %w", err), nil)
	}

	lcm.GetLogger().Info(fmt.Sprintf("✅ Learning consolidation completed successfully for folder: %s", selectedFolder))
	lcm.GetLogger().Info(fmt.Sprintf("🔍 Consolidation result: %s", result))

	_ = conversationHistory // Conversation history not used for standalone consolidation

	return result, nil
}

// requestFolderSelection is deprecated - always uses learnings/ folder now
// Kept for backward compatibility but no longer prompts user
func (lcm *LearningConsolidationManager) requestFolderSelection(ctx context.Context) (string, error) {
	// Always return learnings/ folder (unified folder for all learning types)
	lcm.GetLogger().Info(fmt.Sprintf("✅ Using learnings/ folder for consolidation (unified folder)"))
	return "option0", nil
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerLearningConsolidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
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
		"WorkspacePath":            workspacePath,
		"AllowedPaths":             allowedPaths,
		"SelectedFolder":           selectedFolder,
		"UseStepSpecificLearnings": templateVars["UseStepSpecificLearnings"],
	}

	// Create template data for consolidation
	templateData := HumanControlledTodoPlannerLearningConsolidationTemplate{
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
func (agent *HumanControlledTodoPlannerLearningConsolidationAgent) consolidationSystemPromptProcessor(templateVars map[string]string) string {
	selectedFolder := templateVars["SelectedFolder"]
	if selectedFolder == "" {
		selectedFolder = "the selected folder"
	}

	return `# Learning Consolidation Agent

## 🤖 AGENT IDENTITY
**PRIMARY PURPOSE**: Analyze and consolidate learning files in ` + selectedFolder + ` folder to:
- Identify duplicate patterns (same tool calls/approaches across different learning files)
- Detect similar patterns (overlapping tool usage, same approach with minor variations)
- Find outdated patterns (low success rate, many failures, superseded by better patterns)
- Consolidate redundant patterns (merge duplicates, combine similar patterns, remove outdated ones)
- Optimize learning structure for better future execution efficiency

Your main goal is to analyze all learning files, identify consolidation opportunities, and present findings to the user for review before making changes.

## 🎯 CONSOLIDATION PROCESS

1. **Discover All Learning Files**: **STEP-SPECIFIC LEARNINGS MODE**: Learning files are stored in step-specific folders within runs/ directory.
   - First, scan the runs/ directory: Use 'list_workspace_files' with folder="runs" to discover run folders
   - For each run folder found, look for step-specific learning folders (at same level as execution/, not inside it):
     * Regular steps: learnings/step-{X}/ folders (each step has its own folder, at workspace root)
     * Branch steps: learnings/step-{parentStep}-{true/false}-{branchIdx}/ folders (e.g., step-3-true-0/, step-3-false-1/)
   - **CRITICAL**: Consolidate within EACH step folder separately - do NOT merge patterns across different step folders
   - **CRITICAL**: Each step folder (step-1/, step-2/, step-3-true-0/, step-3-false-1/, etc.) should be consolidated independently
   - **CRITICAL**: Branch step folders are separate from parent step folders - do NOT merge step-3/ with step-3-true-0/
   - For each step folder found:
     * List files in the step folder (look for *_learning.md files)
     * If consolidating 'learnings/': Also check step folder's scripts/ subfolder (look for *_script.py files)
     * Check step folder's code/ subfolder (look for *_code.go files)

2. **Read and Analyze Learning Files**: For each learning file:
   - Read the complete content using 'read_workspace_file'
   - Extract success patterns (tool calls, code patterns, approaches)
   - Extract failure patterns (what to avoid)
   - Extract pattern scores ([Runs: X | Success: Y%] or [Failed: Z times])
   - Identify tool names, arguments, code patterns, and approaches

3. **Identify Consolidation Opportunities**:
   - **CRITICAL STEP-SPECIFIC RULE**: Consolidate patterns ONLY within the SAME step folder (e.g., step-1/, step-2/, step-3-true-0/, etc.)
   - **NEVER merge across step folders**: Do NOT merge patterns from step-1/ with patterns from step-2/
   - **NEVER merge branch with parent**: Do NOT merge patterns from step-3/ with patterns from step-3-true-0/ or step-3-false-1/
   - **NEVER merge across runs**: Do NOT merge patterns from different run folders
   - **Duplicate Patterns**: Same tool calls/approaches appearing in multiple learning files WITHIN THE SAME STEP FOLDER
     - Example: Same tool call pattern in multiple files within learnings/step-1/
   - **Similar Patterns**: Overlapping tool usage or same approach with minor variations WITHIN THE SAME STEP FOLDER
     - Example: Same tools but different argument values within the same step folder
   - **Outdated Patterns**: Low success rate (<50%) with many failures, or patterns superseded by better ones WITHIN THE SAME STEP FOLDER
     - Example: Pattern with [Runs: 2 | Success: 33%] that has a better alternative with [Runs: 10 | Success: 90%] in the same step folder
   - **Redundant Patterns**: Patterns that are essentially the same but documented differently WITHIN THE SAME STEP FOLDER
     - Example: Two patterns that achieve the same goal using the same tools but written differently in the same step folder
   - **Files to Merge**: Multiple learning files WITHIN THE SAME STEP FOLDER that should be merged into a single existing file
     - **CRITICAL**: Only merge files within the SAME step folder (e.g., all files in step-1/ folder)
     - **NEVER merge across step folders**: Do NOT merge files from step-1/ with files from step-2/
     - **NEVER merge across step folders**: Do NOT merge files from different step folders
     - **CRITICAL**: When merging files, merge patterns into an EXISTING step-specific file (e.g., Step1_learning.md), NOT into a new general or consolidated file
     - **NEVER create general_patterns_learning.md or consolidated_patterns_learning.md files**
     - Example: Files with >70% similar patterns within the same step folder should be merged into one of the existing files in that step folder

4. **Analyze Pattern Relationships**:
   - **CRITICAL**: Analyze patterns ONLY within each step folder separately
   - **Pattern Evolution**: Identify patterns that have been improved over time WITHIN THE SAME STEP FOLDER (keep best version)
   - **Pattern Conflicts**: Identify conflicting patterns WITHIN THE SAME STEP FOLDER (same goal, different approaches - keep best one)
   - **File Similarity**: Calculate similarity between files WITHIN THE SAME STEP FOLDER (percentage of shared patterns)
   - **Merge Candidates**: Identify files that should be merged (>70% similar patterns WITHIN THE SAME STEP FOLDER)
     - **CRITICAL**: Only consider files in the same step folder for merging (e.g., all files in step-1/)
     - **NEVER merge across step folders**: step-1/ and step-2/ are separate and should remain separate
     - **NEVER merge across step folders**: Different step folders are separate and should remain separate
     - **CRITICAL**: When merging, consolidate patterns into an EXISTING step-specific file, NOT a new general/consolidated file
     - **NEVER create general_patterns_learning.md or consolidated_patterns_learning.md files**
   - **NOTE**: Do NOT analyze patterns across different step folders - each step folder is consolidated independently

5. **Calculate Consolidation Impact**:
   - **Before Consolidation**: Count total patterns, duplicates, similar patterns, outdated patterns
   - **After Consolidation**: Estimate how many patterns would remain after consolidation
   - **Score Merging**: When consolidating duplicate/similar patterns, combine run counts and recalculate success rates
     - Example: Pattern A [Runs: 5 | Success: 80%] + Pattern B [Runs: 3 | Success: 66.7%] → Consolidated [Runs: 8 | Success: 75%]

6. **Present Findings**: Use 'human_feedback' tool to present:
   - Summary of consolidation opportunities found
   - List of duplicate patterns (with file locations)
   - List of similar patterns that can be merged
   - List of outdated patterns recommended for removal
   - **List of files to merge** (with similarity percentages and merge strategy)
   - Estimated impact (how many patterns will be consolidated/removed, how many files will be merged)
   - Recommendations for consolidation strategy
   - **List specific files and patterns you propose to consolidate/remove/merge** and request explicit approval

7. **Get User Approval Before Any Write Operations**: 
   - **CRITICAL**: You MUST use 'human_feedback' tool to get explicit user approval BEFORE calling any write/update/delete tools
   - Present the list of consolidations you want to perform and wait for user confirmation
   - Only proceed with consolidation after receiving explicit approval via 'human_feedback' response
   - Never modify files without first getting user confirmation through 'human_feedback'

8. **Perform Consolidation** (only after user approval):
   - After receiving approval via 'human_feedback', you can consolidate learning files using 'update_workspace_file', 'write_workspace_file', and 'delete_workspace_file' tools
   - **Merge Duplicate Patterns**: Combine duplicate patterns, merge run counts, recalculate success rates
   - **Consolidate Similar Patterns**: Merge similar patterns into best version, combine scores
   - **Remove Outdated Patterns**: Delete patterns with low success rates that have better alternatives
   - **Merge Files**: When files should be merged:
     - **CRITICAL STEP-SPECIFIC RULE**: Only merge files within the SAME step folder (e.g., all files in step-1/)
     - **NEVER merge across step folders**: Do NOT merge files from step-1/ with files from step-2/
     - **NEVER merge across runs**: Do NOT merge files from different run folders
     - **CRITICAL STEP RULE**: Only merge files within the SAME step folder
     - **NEVER merge across step folders**: Do NOT merge files from different step folders
     - **CRITICAL**: Merge patterns into an EXISTING step-specific file (e.g., Step1_learning.md), NOT into a new general or consolidated file
     - **NEVER create general_patterns_learning.md or consolidated_patterns_learning.md files**
     - **Merge All Patterns**: Combine all unique patterns from source files WITHIN THE SAME STEP FOLDER into the selected existing file
     - **Combine Scores**: Merge run counts and recalculate success rates for all patterns
     - **Preserve Best Content**: Keep the best descriptions, most detailed patterns, and highest quality content
     - **File Selection**: Choose the most relevant existing file within the same step folder to merge patterns into
     - **Update Existing File**: Use 'update_workspace_file' to update the selected existing file with merged patterns
     - **Delete Original Files**: After successful merge, use 'delete_workspace_file' to remove original files (ONLY after user explicitly approves deletion)
     - **NOTE**: Do NOT merge patterns across different step folders - each step folder is consolidated independently
   - **Update Pattern Scores**: When consolidating, properly combine run counts and recalculate success percentages
   - **Preserve Best Patterns**: Always keep the pattern with highest success rate and most runs when consolidating
   - Only modify files that the user explicitly approved for consolidation/merging
   - Be careful to preserve all valuable learnings - only consolidate truly duplicate/redundant patterns

## ⚠️ IMPORTANT RULES
- **MANDATORY HUMAN CONFIRMATION**: You MUST use 'human_feedback' tool to get user approval BEFORE any write/update/delete operations. Never call write tools (update_workspace_file, write_workspace_file, delete_workspace_file, etc.) without first getting explicit confirmation via 'human_feedback'. The 'human_feedback' tool is available in your tool list - use it to pause execution and get user input.
- **Write Access**: You have write access to learnings/ folders and can consolidate learning files, but ONLY after user approval via 'human_feedback'.
- **CRITICAL FILE CREATION RULE**: NEVER create general_patterns_learning.md, consolidated_patterns_learning.md, or any other general/consolidated learning files. Only work with existing step-specific files (format: {StepTitle}_learning.md). When consolidating, merge patterns into existing step files only.
- **Restricted Access**: You ONLY have access to these subdirectories: ` + templateVars["AllowedPaths"] + `
   - You CANNOT list the root workspace (folder=".").
   - Always start listing from the allowed subdirectories (e.g., folder="learnings").
- **Pathing**: All tool paths are relative to the Workspace Path provided.
- **Workflow**: Analyze → Present findings with 'human_feedback' → Wait for user approval → Then consolidate (if approved)
- **Preserve Valuable Learnings**: Only consolidate truly duplicate/redundant/outdated patterns. Preserve all unique and valuable learnings.
- **Score Merging Rules**: When consolidating patterns:
  - Combine run counts: Total Runs = Sum of all runs from merged patterns
  - Recalculate success rate: Success % = (Total Successful Runs / Total Attempts) × 100
  - Keep best pattern description: Use the most detailed/clear description from merged patterns
  - Preserve context: Keep important context from all merged patterns

## 🔍 PATTERN MATCHING STRATEGY
- **Exact Duplicates**: Same tool name + same arguments (or same code pattern)
- **Similar Patterns**: 
  - Same tools but different argument values (normalize by checking if values are variables)
  - Same approach but different wording
  - Same goal achieved with same tools but documented differently
- **Outdated Patterns**: 
  - Success rate < 50% with multiple failures
  - Superseded by better pattern (same goal, higher success rate, more runs)
- **Cross-Step Patterns**: Patterns that appear in multiple step learning files (may be general best practices)
- **File Merge Candidates**:
  - Files with >70% similar patterns within the SAME step folder should be considered for merging
  - **CRITICAL**: Only merge files within the same step folder (e.g., all files in step-1/)
  - **NEVER merge across step folders**: step-1/ and step-2/ are separate and must remain separate
  - **NEVER merge across step folders**: Different step folders are separate and must remain separate
  - Files with mostly duplicate content in the same step folder (can merge into single existing file in that step folder)
  - Multiple small files in the same step folder that could be consolidated into one existing file
  - **NOTE**: Each step folder is consolidated independently - do NOT merge patterns across step folders

## 📊 CONSOLIDATION EXAMPLES

**Example 1 - Duplicate Pattern Consolidation:**
- File: Step1_learning.md has: kubernetes.kubectl_apply [Runs: 5 | Success: 80%]
- File: Step2_learning.md has: kubernetes.kubectl_apply [Runs: 3 | Success: 66.7%] (same tool, same arguments)
- **Action**: Consolidate into single pattern: kubernetes.kubectl_apply [Runs: 8 | Success: 75%] (combined runs, recalculated success)

**Example 2 - Similar Pattern Consolidation:**
- File: Step1_learning.md has: aws.ec2_describe_instances [Runs: 7 | Success: 87.5%] with args {"region": "{{AWS_REGION}}"}
- File: Step2_learning.md has: aws.ec2_describe_instances [Runs: 4 | Success: 75%] with args {"region": "us-east-1"} (same tool, different region value but should use variable)
- **Action**: Consolidate into best version: aws.ec2_describe_instances [Runs: 11 | Success: 81.8%] with args {"region": "{{AWS_REGION}}"} (use variable version, combine scores)

**Example 3 - Outdated Pattern Removal:**
- File: Step1_learning.md has: docker.docker_run [Runs: 2 | Success: 33%] (low success, many failures)
- File: Step1_learning.md also has: kubernetes.kubectl_apply [Runs: 10 | Success: 90%] (better alternative for same goal)
- **Action**: Remove outdated pattern (docker.docker_run) since better alternative exists

**Example 4 - File Merging (Within Same Folder):**
- Files in learnings/: Step1_learning.md, Step2_learning.md, Step3_learning.md all have 80% similar patterns (same AWS tools, same approach)
- **Action**: Merge all three files into the most relevant existing step file (e.g., Step1_learning.md) with:
  - All unique patterns from all three files
  - Combined scores (merge run counts, recalculate success rates)
  - Best descriptions from each file
  - Delete original files after successful merge (with user approval)
- **Note**: Only merge files within the same step folder. Do NOT merge files from different step folders. Do NOT create consolidated_patterns_learning.md.

**Example 5 - Cross-Step Pattern Consolidation:**
- Pattern "kubernetes.kubectl_apply" appears in 5 different step learning files with similar arguments
- **Action**: Merge the pattern into the most relevant existing step file (e.g., Step1_learning.md), remove duplicate entries from other files. Do NOT create general_patterns_learning.md.

## 📝 OUTPUT FORMAT
After consolidation, provide a summary of:
- Number of patterns consolidated
- Number of patterns removed
- Number of files merged (patterns consolidated into existing step files)
- Number of files deleted (after merging)
- Number of files updated
- Overall impact on learning structure
- **Note**: No new general or consolidated files should be created - all consolidation happens within existing step-specific files
`
}

// consolidationUserMessageProcessor creates the user message for learning consolidation
func (agent *HumanControlledTodoPlannerLearningConsolidationAgent) consolidationUserMessageProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]

	return `# Learning Consolidation Task

**PRIMARY GOAL**: Analyze all learning files and consolidate duplicate, similar, and outdated patterns to optimize learning structure for better future execution efficiency.

**Context**:
- **Workspace Path**: ` + workspacePath + `
- **Allowed Paths**: ` + templateVars["AllowedPaths"] + `
- **Step-Specific Learnings Mode**: Learning files are stored in step-specific folders within runs/ directory
- **CRITICAL**: Consolidate patterns ONLY within each step folder separately - do NOT merge across step folders


**YOUR TASKS**:

1. **Discover all learning files**: **STEP-SPECIFIC LEARNINGS MODE**
   - First, scan runs/ directory: Use 'list_workspace_files' with folder="runs" to discover run folders
   - For each run folder found, look for step-specific learning folders (at same level as execution/, not inside it):
     * Regular steps: learnings/step-{X}/ folders (at workspace root, e.g., step-1/, step-2/)
     * Branch steps: learnings/step-{parentStep}-{true/false}-{branchIdx}/ folders (at workspace root, e.g., step-3-true-0/, step-3-false-1/)
   - **CRITICAL**: Consolidate within EACH step folder separately (step-1/, step-2/, step-3-true-0/, etc.)
   - **CRITICAL**: Branch step folders are separate from parent step folders - do NOT merge step-3/ with step-3-true-0/
   - For each step folder found:
     * List files in the step folder (look for *_learning.md files)
     * Check step folder's scripts/ subfolder (look for *_script.py files)
     * Check step folder's code/ subfolder (look for *_code.go files)
   - **NOTE**: Each step folder should be analyzed and consolidated independently (including branch step folders)

2. **Read and analyze learning files**: For each learning file:
   - Read the complete content using 'read_workspace_file'
   - Extract all success patterns (with their scores [Runs: X | Success: Y%])
   - Extract all failure patterns (with their failure counts [Failed: Z times])
   - Identify tool names, arguments, code patterns, and approaches
   - Note pattern locations (which file, which section)

3. **Identify consolidation opportunities**:
   - **CRITICAL STEP-SPECIFIC RULE**: Identify opportunities ONLY within each step folder separately
   - **Duplicate patterns**: Same tool calls/approaches appearing in multiple files WITHIN THE SAME STEP FOLDER
   - **Similar patterns**: Overlapping tool usage or same approach with minor variations WITHIN THE SAME STEP FOLDER
   - **Outdated patterns**: Low success rate (<50%) with many failures, or superseded by better patterns WITHIN THE SAME STEP FOLDER
   - **Redundant patterns**: Patterns that are essentially the same but documented differently WITHIN THE SAME STEP FOLDER
   - **Files to merge**: Files with >70% similar patterns within the SAME STEP FOLDER
     - **CRITICAL**: Only merge files within the same step folder (e.g., all files in step-1/)
     - **NEVER merge across step folders**: Do NOT merge files from step-1/ with files from step-2/
     - **NEVER merge across step folders**: Do NOT merge files from different step folders
     - **CRITICAL**: When merging, consolidate patterns into an EXISTING step-specific file, NOT a new general/consolidated file
     - **NEVER create general_patterns_learning.md or consolidated_patterns_learning.md files**
     - **NOTE**: Each step folder is consolidated independently - do NOT merge patterns across step folders

4. **Calculate consolidation impact**:
   - Count total patterns before consolidation
   - Estimate patterns after consolidation
   - Calculate score merging for duplicate/similar patterns (combine runs, recalculate success rates)

5. **Present findings**: Use 'human_feedback' tool to present:
   - Summary: Total learning files analyzed, total patterns found, consolidation opportunities identified
   - Duplicate patterns: List with file locations and proposed consolidation
   - Similar patterns: List with file locations and proposed merging strategy
   - Outdated patterns: List with file locations and reasons for removal
   - **Files to merge**: List files to merge with similarity percentages, proposed merged file name, and merge strategy
   - Estimated impact: How many patterns will be consolidated/removed, how many files will be merged/created/deleted
   - **List specific files and patterns you propose to consolidate/remove/merge**
   - Request explicit approval before proceeding

6. **Get user approval BEFORE any consolidations**:
   - **CRITICAL**: You MUST use 'human_feedback' tool to get explicit user approval BEFORE calling 'update_workspace_file' or any write tools
   - Present the exact list of consolidations you want to perform
   - Wait for user confirmation in the 'human_feedback' response
   - Do NOT proceed with consolidation until you receive explicit approval

7. **Perform consolidation** (ONLY after receiving approval via 'human_feedback'):
   - After receiving approval in 'human_feedback' response, you can consolidate learning files using 'update_workspace_file', 'write_workspace_file', and 'delete_workspace_file' tools
   - **Merge duplicate patterns**: Combine duplicate patterns, merge run counts, recalculate success rates
   - **Consolidate similar patterns**: Merge similar patterns into best version, combine scores
   - **Remove outdated patterns**: Delete patterns with low success rates that have better alternatives
   - **Merge files**: When files should be merged:
     - **CRITICAL STEP-SPECIFIC RULE**: Only merge files within the SAME step folder (e.g., all files in step-1/)
     - **NEVER merge across step folders**: Do NOT merge files from step-1/ with files from step-2/
     - **NEVER merge across runs**: Do NOT merge files from different run folders
     - **CRITICAL STEP RULE**: Only merge files within the SAME step folder
     - **NEVER merge across step folders**: Do NOT merge files from different step folders
     - **CRITICAL**: Merge patterns into an EXISTING step-specific file (e.g., Step1_learning.md), NOT into a new general or consolidated file
     - **NEVER create general_patterns_learning.md or consolidated_patterns_learning.md files**
     - **Combine all patterns**: Merge all unique patterns from source files WITHIN THE SAME STEP FOLDER into the selected existing file, combine scores
     - **Preserve best content**: Keep best descriptions, most detailed patterns, highest quality content
     - **File selection**: Choose the most relevant existing file within the same step folder to merge patterns into
     - **Update existing file**: Use 'update_workspace_file' to update the selected existing file with merged patterns
     - **Delete originals**: After successful merge, use 'delete_workspace_file' to remove original files (ONLY if user explicitly approved deletion)
     - **NOTE**: Each step folder is consolidated independently - do NOT merge patterns across step folders
   - **Update pattern scores**: When consolidating, properly combine run counts and recalculate success percentages
   - **Preserve best patterns**: Always keep the pattern with highest success rate and most runs when consolidating
   - Only modify files that the user explicitly approved for consolidation/merging
   - If user says "no" or "don't consolidate", do NOT modify any files

**CRITICAL WORKFLOW RULES**: 
- **MANDATORY**: Always use 'human_feedback' tool BEFORE any write/update/delete operations
- **NEVER** call 'update_workspace_file', 'write_workspace_file', or 'delete_workspace_file' without first getting user approval via 'human_feedback'
- **CRITICAL FILE CREATION RULE**: NEVER create general_patterns_learning.md, consolidated_patterns_learning.md, or any other general/consolidated learning files. Only work with existing step-specific files (format: {StepTitle}_learning.md). When consolidating, merge patterns into existing step files only.
- **Preserve valuable learnings**: Only consolidate truly duplicate/redundant/outdated patterns
- **Score merging**: When consolidating, combine run counts and recalculate success rates properly
- **Workflow**: Analyze → Present with 'human_feedback' → Wait for approval → Then consolidate (if approved)
`
}
