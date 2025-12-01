package todo_creation_human

import (
	"context"
	"fmt"
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
func NewHumanControlledTodoPlannerLearningConsolidationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *HumanControlledTodoPlannerLearningConsolidationAgent {
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

	// Preset LLM config for learning consolidation agent
	presetLearningConsolidationLLM *AgentLLMConfig
}

// NewLearningConsolidationManager creates a new LearningConsolidationManager
func NewLearningConsolidationManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetLearningConsolidationLLM *AgentLLMConfig,
) *LearningConsolidationManager {
	return &LearningConsolidationManager{
		BaseOrchestrator:               baseOrchestrator,
		sessionID:                      sessionID,
		workflowID:                     workflowID,
		presetLearningConsolidationLLM: presetLearningConsolidationLLM,
	}
}

// createLearningConsolidationAgent creates and sets up a learning consolidation agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
// selectedFolder: the folder to consolidate ("learnings/" or "learning_code_exec/")
func (lcm *LearningConsolidationManager) createLearningConsolidationAgent(ctx context.Context, workspacePath string, selectedFolder string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read and write access to selected folder only
	var targetPath string
	if selectedFolder == "learnings/" {
		targetPath = fmt.Sprintf("%s/learnings", workspacePath)
	} else if selectedFolder == "learning_code_exec/" {
		targetPath = fmt.Sprintf("%s/learning_code_exec", workspacePath)
	} else {
		return nil, fmt.Errorf("invalid selected folder: %s (must be 'learnings/' or 'learning_code_exec/')", selectedFolder)
	}

	// Agent has read and write access to only the selected folder
	readPaths := []string{targetPath}
	writePaths := []string{targetPath} // Write access to selected folder for consolidation
	lcm.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	lcm.GetLogger().Infof("🔍 Setting folder guard for learning consolidation agent - Read paths: %v, Write paths: %v (read/write access to %s folder only)", readPaths, writePaths, selectedFolder)

	// Use preset LLM config if available, otherwise fall back to orchestrator default
	orchestratorLLMConfig := lcm.GetLLMConfig()
	var llmConfigToUse *orchestrator.LLMConfig
	if lcm.presetLearningConsolidationLLM != nil && lcm.presetLearningConsolidationLLM.Provider != "" && lcm.presetLearningConsolidationLLM.ModelID != "" {
		// Use preset LLM config
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:       lcm.presetLearningConsolidationLLM.Provider,
			ModelID:        lcm.presetLearningConsolidationLLM.ModelID,
			FallbackModels: orchestratorLLMConfig.FallbackModels,
			APIKeys:        orchestratorLLMConfig.APIKeys,
			Options:        orchestratorLLMConfig.Options,
		}
		lcm.GetLogger().Infof("🔧 Using preset learning consolidation LLM: %s/%s", lcm.presetLearningConsolidationLLM.Provider, lcm.presetLearningConsolidationLLM.ModelID)
	} else {
		// Fall back to orchestrator default
		llmConfigToUse = orchestratorLLMConfig
		lcm.GetLogger().Infof("🔧 Using orchestrator default consolidation LLM: %s/%s", lcm.GetProvider(), lcm.GetModel())
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
	lcm.GetLogger().Infof("🔧 Disabling code execution mode for learning consolidation agent (only execution agents use MCP tools)")

	// Large output virtual tools are enabled for consolidation (agent may generate large reports)
	enabled := true
	config.EnableLargeOutputVirtualTools = &enabled

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
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
		return nil, fmt.Errorf("failed to create and setup learning consolidation agent: %w", err)
	}

	return agent, nil
}

// ConsolidateLearningsOnly runs only the learning consolidation check (standalone, independent from other phases)
// This is a separate workflow phase that can be run independently
func (lcm *LearningConsolidationManager) ConsolidateLearningsOnly(ctx context.Context, workspacePath string) (string, error) {
	lcm.GetLogger().Infof("🔍 Starting standalone learning consolidation for workspace: %s", workspacePath)

	// Set workspace path
	lcm.SetWorkspacePath(workspacePath)

	// Ask user which folder to consolidate before starting
	folderChoice, err := lcm.requestFolderSelection(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get folder selection: %w", err)
	}

	// Determine allowed paths based on user selection
	var allowedPaths string
	var selectedFolder string
	switch folderChoice {
	case "option0": // learnings/
		allowedPaths = "['learnings/']"
		selectedFolder = "learnings/"
		lcm.GetLogger().Infof("✅ User selected: learnings/ folder")
	case "option1": // learning_code_exec/
		allowedPaths = "['learning_code_exec/']"
		selectedFolder = "learning_code_exec/"
		lcm.GetLogger().Infof("✅ User selected: learning_code_exec/ folder")
	default:
		return "", fmt.Errorf("invalid folder selection: %s", folderChoice)
	}

	// Create consolidation agent with selected folder
	consolidationAgent, err := lcm.createLearningConsolidationAgent(ctx, lcm.GetWorkspacePath(), selectedFolder)
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
	lcm.GetLogger().Infof("🔍 Executing learning consolidation agent for folder: %s", selectedFolder)
	result, conversationHistory, err := consolidationAgent.Execute(ctx, consolidationTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("learning consolidation agent execution failed: %w", err)
	}

	lcm.GetLogger().Infof("✅ Learning consolidation completed successfully for folder: %s", selectedFolder)
	lcm.GetLogger().Infof("🔍 Consolidation result: %s", result)

	_ = conversationHistory // Conversation history not used for standalone consolidation

	return result, nil
}

// requestFolderSelection asks the user to select which folder to consolidate
// Returns: "option0" for learnings/, "option1" for learning_code_exec/
func (lcm *LearningConsolidationManager) requestFolderSelection(ctx context.Context) (string, error) {
	lcm.GetLogger().Infof("🤔 Asking user to select folder for consolidation")

	requestID := fmt.Sprintf("learning_consolidation_folder_selection_%d", time.Now().UnixNano())

	options := []string{
		"learnings/ (MCP tool patterns)",
		"learning_code_exec/ (Code execution patterns)",
	}

	question := "Which folder would you like to consolidate?"
	context := "Learning consolidation will analyze and merge duplicate/similar patterns in the selected folder.\n\n" +
		"**learnings/**: Contains MCP tool patterns and Python scripts\n" +
		"**learning_code_exec/**: Contains Go code execution patterns\n\n" +
		"Please select which folder you want to consolidate."

	choice, err := lcm.RequestMultipleChoiceFeedback(
		ctx,
		requestID,
		question,
		options,
		context,
		lcm.sessionID,
		lcm.workflowID,
	)
	if err != nil {
		return "", fmt.Errorf("failed to get folder selection: %w", err)
	}

	lcm.GetLogger().Infof("✅ User selected folder option: %s", choice)
	return choice, nil
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerLearningConsolidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	selectedFolder := templateVars["SelectedFolder"]

	// Provide default allowed paths if not present
	allowedPaths := templateVars["AllowedPaths"]
	if allowedPaths == "" {
		allowedPaths = "['learnings/', 'learning_code_exec/']"
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
	templateData := HumanControlledTodoPlannerLearningConsolidationTemplate{
		WorkspacePath: workspacePath,
		AllowedPaths:  allowedPaths,
	}

	// Generate system prompt and user message separately
	systemPrompt := agent.consolidationSystemPromptProcessor(consolidationTemplateVars)
	userMessage := agent.consolidationUserMessageProcessor(consolidationTemplateVars)

	// Get logger from base agent's MCP agent
	baseAgent := agent.GetBaseAgent()
	var logger utils.ExtendedLogger
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
			logger.Infof("🔍 Learning consolidation agent iteration %d/%d", iteration, maxIterations)
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
				logger.Infof("🔍 Learning consolidation agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations)
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
					logger.Warnf("⚠️ Failed to get user feedback: %v", err)
				}
				// Continue without blocking if feedback fails
				break
			}

			// If user clicked Approve button, we're done
			if approved {
				if logger != nil {
					logger.Infof("✅ User approved - learning consolidation complete")
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
		logger.Infof("🔍 Learning consolidation completed after %d iterations", iteration)
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

1. **Discover All Learning Files**: Use 'list_workspace_files' to explore ` + selectedFolder + ` folder:
   - If consolidating 'learnings/': List files in 'learnings/' folder (look for *_learning.md files) and 'learnings/scripts/' folder (look for *_script.py files)
   - If consolidating 'learning_code_exec/': List files in 'learning_code_exec/' folder (look for *_learning.md files) and 'learning_code_exec/code/' folder (look for *_code.go files)

2. **Read and Analyze Learning Files**: For each learning file:
   - Read the complete content using 'read_workspace_file'
   - Extract success patterns (tool calls, code patterns, approaches)
   - Extract failure patterns (what to avoid)
   - Extract pattern scores ([Runs: X | Success: Y%] or [Failed: Z times])
   - Identify tool names, arguments, code patterns, and approaches

3. **Identify Consolidation Opportunities**:
   - **Duplicate Patterns**: Same tool calls/approaches appearing in multiple learning files
     - Example: Same tool call pattern in "Step1_learning.md" and "Step2_learning.md"
   - **Similar Patterns**: Overlapping tool usage or same approach with minor variations
     - Example: Same tools but different argument values, or same approach with slight modifications
   - **Outdated Patterns**: Low success rate (<50%) with many failures, or patterns superseded by better ones
     - Example: Pattern with [Runs: 2 | Success: 33%] that has a better alternative with [Runs: 10 | Success: 90%]
   - **Redundant Patterns**: Patterns that are essentially the same but documented differently
     - Example: Two patterns that achieve the same goal using the same tools but written differently
   - **Files to Merge**: Multiple learning files that should be merged into a single file
     - **CRITICAL**: Only merge files within the SAME folder (learnings/ or learning_code_exec/)
     - **NEVER merge across folders**: Do NOT merge files from learnings/ with files from learning_code_exec/
     - Example: Files with >70% similar patterns within the same folder
     - Example: Multiple files with overlapping content in the same folder that would benefit from consolidation
     - Example: General patterns that appear across multiple step files in the same folder (can be extracted to a shared file)

4. **Analyze Pattern Relationships**:
   - **Cross-Step Patterns**: Identify patterns that appear across multiple steps (may indicate general best practices)
   - **Pattern Evolution**: Identify patterns that have been improved over time (keep best version)
   - **Pattern Conflicts**: Identify conflicting patterns (same goal, different approaches - keep best one)
   - **File Similarity**: Calculate similarity between files (percentage of shared patterns)
   - **Merge Candidates**: Identify files that should be merged (>70% similar patterns within the SAME folder)
     - **CRITICAL**: Only consider files in the same folder for merging (learnings/ or learning_code_exec/)
     - **NEVER merge across folders**: learnings/ and learning_code_exec/ are separate and should remain separate

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
     - **CRITICAL FOLDER RULE**: Only merge files within the SAME folder (learnings/ or learning_code_exec/)
     - **NEVER merge across folders**: Do NOT merge files from learnings/ with files from learning_code_exec/
     - **Create Consolidated File**: Use 'write_workspace_file' to create a new consolidated file with merged content (in the same folder as source files)
     - **Merge All Patterns**: Combine all unique patterns from source files into the consolidated file
     - **Combine Scores**: Merge run counts and recalculate success rates for all patterns
     - **Preserve Best Content**: Keep the best descriptions, most detailed patterns, and highest quality content
     - **File Naming**: Use descriptive names like "consolidated_patterns_learning.md" or merge into the most relevant existing file (in the same folder)
     - **Delete Original Files**: After successful merge, use 'delete_workspace_file' to remove original files (ONLY after user explicitly approves deletion)
     - **Cross-Step Patterns**: If patterns appear across multiple steps in the same folder, consider creating a "general_patterns_learning.md" file (in that same folder)
   - **Update Pattern Scores**: When consolidating, properly combine run counts and recalculate success percentages
   - **Preserve Best Patterns**: Always keep the pattern with highest success rate and most runs when consolidating
   - Only modify files that the user explicitly approved for consolidation/merging
   - Be careful to preserve all valuable learnings - only consolidate truly duplicate/redundant patterns

## ⚠️ IMPORTANT RULES
- **MANDATORY HUMAN CONFIRMATION**: You MUST use 'human_feedback' tool to get user approval BEFORE any write/update/delete operations. Never call write tools (update_workspace_file, write_workspace_file, delete_workspace_file, etc.) without first getting explicit confirmation via 'human_feedback'. The 'human_feedback' tool is available in your tool list - use it to pause execution and get user input.
- **Write Access**: You have write access to learnings/ folders and can consolidate learning files, but ONLY after user approval via 'human_feedback'.
- **Restricted Access**: You ONLY have access to these subdirectories: ` + templateVars["AllowedPaths"] + `
   - You CANNOT list the root workspace (folder=".").
   - Always start listing from the allowed subdirectories (e.g., folder="learnings", folder="learning_code_exec").
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
  - Files with >70% similar patterns within the SAME folder should be considered for merging
  - **CRITICAL**: Only merge files within the same folder (learnings/ or learning_code_exec/)
  - **NEVER merge across folders**: learnings/ and learning_code_exec/ are separate and must remain separate
  - Files with mostly duplicate content in the same folder (can merge into single file)
  - Multiple small files in the same folder that could be consolidated into one larger file
  - General patterns appearing in 3+ files in the same folder (extract to shared file in that folder)

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
- **Action**: Merge all three files into learnings/consolidated_aws_patterns_learning.md with:
  - All unique patterns from all three files
  - Combined scores (merge run counts, recalculate success rates)
  - Best descriptions from each file
  - Delete original files after successful merge (with user approval)
- **Note**: Only merge files within the same folder. Do NOT merge learnings/ files with learning_code_exec/ files.

**Example 5 - Cross-Step Pattern Extraction:**
- Pattern "kubernetes.kubectl_apply" appears in 5 different step learning files with similar arguments
- **Action**: Extract to general_patterns_learning.md, remove from individual files, reference in individual files

## 📝 OUTPUT FORMAT
After consolidation, provide a summary of:
- Number of patterns consolidated
- Number of patterns removed
- Number of files merged
- Number of files created (consolidated files)
- Number of files deleted (after merging)
- Number of files updated
- Overall impact on learning structure
`
}

// consolidationUserMessageProcessor creates the user message for learning consolidation
func (agent *HumanControlledTodoPlannerLearningConsolidationAgent) consolidationUserMessageProcessor(templateVars map[string]string) string {
	return `# Learning Consolidation Task

**PRIMARY GOAL**: Analyze all learning files and consolidate duplicate, similar, and outdated patterns to optimize learning structure for better future execution efficiency.

**Context**:
- **Workspace Path**: ` + templateVars["WorkspacePath"] + `
- **Allowed Paths**: ` + templateVars["AllowedPaths"] + `

**YOUR TASKS**:

1. **Discover all learning files**: Use 'list_workspace_files' to list all learning files from both folders:
   - Look for *.md files in learnings/ folder
   - Look for *.py files in learnings/scripts/ folder
   - Look for *.md files in learning_code_exec/ folder
   - Look for *.go files in learning_code_exec/code/ folder

2. **Read and analyze learning files**: For each learning file:
   - Read the complete content using 'read_workspace_file'
   - Extract all success patterns (with their scores [Runs: X | Success: Y%])
   - Extract all failure patterns (with their failure counts [Failed: Z times])
   - Identify tool names, arguments, code patterns, and approaches
   - Note pattern locations (which file, which section)

3. **Identify consolidation opportunities**:
   - **Duplicate patterns**: Same tool calls/approaches appearing in multiple files
   - **Similar patterns**: Overlapping tool usage or same approach with minor variations
   - **Outdated patterns**: Low success rate (<50%) with many failures, or superseded by better patterns
   - **Redundant patterns**: Patterns that are essentially the same but documented differently
   - **Files to merge**: Files with >70% similar patterns within the SAME folder
     - **CRITICAL**: Only merge files within the same folder (learnings/ or learning_code_exec/)
     - **NEVER merge across folders**: Do NOT merge files from learnings/ with files from learning_code_exec/

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
     - **CRITICAL FOLDER RULE**: Only merge files within the SAME folder (learnings/ or learning_code_exec/)
     - **NEVER merge across folders**: Do NOT merge files from learnings/ with files from learning_code_exec/
     - **Create consolidated file**: Use 'write_workspace_file' to create new file with merged content (in the same folder as source files)
     - **Combine all patterns**: Merge all unique patterns from source files, combine scores
     - **Preserve best content**: Keep best descriptions, most detailed patterns, highest quality content
     - **File naming**: Use descriptive names (e.g., "consolidated_patterns_learning.md" or merge into most relevant existing file) in the same folder
     - **Cross-step patterns**: If patterns appear in 3+ files in the same folder, consider creating "general_patterns_learning.md" in that folder
     - **Delete originals**: After successful merge, use 'delete_workspace_file' to remove original files (ONLY if user explicitly approved deletion)
   - **Update pattern scores**: When consolidating, properly combine run counts and recalculate success percentages
   - **Preserve best patterns**: Always keep the pattern with highest success rate and most runs when consolidating
   - Only modify files that the user explicitly approved for consolidation/merging
   - If user says "no" or "don't consolidate", do NOT modify any files

**CRITICAL WORKFLOW RULES**: 
- **MANDATORY**: Always use 'human_feedback' tool BEFORE any write/update/delete operations
- **NEVER** call 'update_workspace_file', 'write_workspace_file', or 'delete_workspace_file' without first getting user approval via 'human_feedback'
- **Preserve valuable learnings**: Only consolidate truly duplicate/redundant/outdated patterns
- **Score merging**: When consolidating, combine run counts and recalculate success rates properly
- **Workflow**: Analyze → Present with 'human_feedback' → Wait for approval → Then consolidate (if approved)
`
}
