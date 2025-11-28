package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledTodoPlannerAnonymizationTemplate holds template variables for anonymization prompts
type HumanControlledTodoPlannerAnonymizationTemplate struct {
	WorkspacePath string
	VariablesJSON string
	VariableNames string
}

// HumanControlledTodoPlannerAnonymizationAgent scans learnings folder and replaces actual values with variable placeholders
type HumanControlledTodoPlannerAnonymizationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerAnonymizationAgent creates a new anonymization agent
func NewHumanControlledTodoPlannerAnonymizationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerAnonymizationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerAnonymizationAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerAnonymizationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// AnonymizationManager manages anonymization agent creation independently from controller
type AnonymizationManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Anonymization LLM config (optional preset)
	presetAnonymizationLLM *AgentLLMConfig
}

// NewAnonymizationManager creates a new AnonymizationManager
func NewAnonymizationManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetAnonymizationLLM *AgentLLMConfig,
) *AnonymizationManager {
	return &AnonymizationManager{
		BaseOrchestrator:       baseOrchestrator,
		presetAnonymizationLLM: presetAnonymizationLLM,
	}
}

// createAnonymizationAgent creates and sets up an anonymization agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
func (am *AnonymizationManager) createAnonymizationAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from both learnings folders, writes to both learnings folders
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	learningCodeExecPath := fmt.Sprintf("%s/learning_code_exec", workspacePath)

	// Agent has access to both learnings folders (read and write)
	readPaths := []string{learningsPath, learningCodeExecPath}
	writePaths := []string{learningsPath, learningCodeExecPath}
	am.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	am.GetLogger().Infof("🔒 Setting folder guard for anonymization agent - Read paths: %v, Write paths: %v (both learnings folders)", readPaths, writePaths)

	// Determine LLM config: Priority: preset default > orchestrator default
	var llmConfigToUse *orchestrator.LLMConfig
	orchestratorLLMConfig := am.GetLLMConfig()
	if am.presetAnonymizationLLM != nil && am.presetAnonymizationLLM.Provider != "" && am.presetAnonymizationLLM.ModelID != "" {
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              am.presetAnonymizationLLM.Provider,
			ModelID:               am.presetAnonymizationLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,        // Preserve fallback models from orchestrator
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback, // Preserve cross-provider fallback
			APIKeys:               orchestratorLLMConfig.APIKeys,               // Preserve API keys from orchestrator
		}
		am.GetLogger().Infof("🔧 Using preset default anonymization LLM: %s/%s", am.presetAnonymizationLLM.Provider, am.presetAnonymizationLLM.ModelID)
	} else {
		llmConfigToUse = orchestratorLLMConfig
		am.GetLogger().Infof("🔧 Using orchestrator default anonymization LLM: %s/%s", am.GetProvider(), am.GetModel())
	}

	// Use workspace tools directly - they already include human_feedback (created by createCustomTools in server.go)
	// No need to add human tools separately as they're already combined in WorkspaceTools
	allTools := am.WorkspaceTools
	allExecutors := am.WorkspaceToolExecutors

	// Create agent config with the selected LLM config
	config := am.CreateStandardAgentConfigWithLLM("anonymization-agent", 100, agents.OutputFormatStructured, llmConfigToUse)

	// Anonymization agent doesn't need MCP servers - uses workspace tools only
	config.ServerNames = []string{mcpclient.NoServers}

	// Code execution mode only applies to execution agents, not anonymization agents
	config.UseCodeExecutionMode = false
	am.GetLogger().Infof("🔧 Disabling code execution mode for anonymization agent (only execution agents use MCP tools)")

	// Large output virtual tools are enabled for anonymization (agent may generate large reports)

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewHumanControlledTodoPlannerAnonymizationAgent(cfg, logger, tracer, eventBridge)
	}

	// Use base orchestrator's CreateAndSetupStandardAgentWithConfig to avoid code duplication
	// This handles initialization, event bridge connection, and tool registration
	// Set overwriteSystemPrompt to true for anonymization agent (replaces default MCP prompt with agent-specific prompt)
	agent, err := am.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"anonymization",
		0, 0, // step, iteration
		createAgentFunc,
		allTools,
		allExecutors,
		true, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup anonymization agent: %w", err)
	}

	return agent, nil
}

// AnonymizeLearningsOnly runs only the anonymization phase (standalone, independent from other phases)
// This is a separate workflow phase that can be run independently
func (am *AnonymizationManager) AnonymizeLearningsOnly(ctx context.Context, workspacePath string) (string, error) {
	am.GetLogger().Infof("🔒 Starting standalone anonymization for workspace: %s", workspacePath)

	// Set workspace path
	am.SetWorkspacePath(workspacePath)

	// Check if variables.json exists - REQUIRED for anonymization
	variablesPath := fmt.Sprintf("%s/variables/variables.json", am.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := am.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing variables: %w", err)
	}
	if !variablesExist {
		return "", fmt.Errorf("variables.json not found at %s - variable extraction must be run first as a separate phase", variablesPath)
	}

	// Variables exist - use them for anonymization
	am.GetLogger().Infof("✅ Found %d variables for anonymization", len(existingVariablesManifest.Variables))

	// Prepare variables data for template
	var variableNames strings.Builder
	for i, variable := range existingVariablesManifest.Variables {
		if i > 0 {
			variableNames.WriteString("\n")
		}
		variableNames.WriteString(fmt.Sprintf("- **%s**: %s", variable.Name, variable.Description))
	}

	variablesJSONBytes, err := json.MarshalIndent(existingVariablesManifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal variables to JSON: %w", err)
	}

	// Create anonymization agent
	anonymizationAgent, err := am.createAnonymizationAgent(ctx, am.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("failed to create anonymization agent: %w", err)
	}

	// Prepare template variables
	anonymizationTemplateVars := map[string]string{
		"WorkspacePath": am.GetWorkspacePath(),
		"VariablesJSON": string(variablesJSONBytes),
		"VariableNames": variableNames.String(),
	}

	// Execute anonymization agent
	am.GetLogger().Infof("🔒 Executing anonymization agent...")
	result, conversationHistory, err := anonymizationAgent.Execute(ctx, anonymizationTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("anonymization agent execution failed: %w", err)
	}

	am.GetLogger().Infof("✅ Anonymization completed successfully")
	am.GetLogger().Infof("📊 Anonymization result: %s", result)

	_ = conversationHistory // Conversation history not used for standalone anonymization

	return result, nil
}

// checkExistingVariables checks if variables.json already exists and loads it
func (am *AnonymizationManager) checkExistingVariables(ctx context.Context, variablesPath string) (bool, *VariablesManifest, error) {
	am.GetLogger().Infof("🔍 Checking for existing variables at %s", variablesPath)

	// Use the generic ReadWorkspaceFile function from base orchestrator
	variablesContent, err := am.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			am.GetLogger().Infof("📋 No existing variables found: %v", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing variables: %w", err)
	}

	// Parse JSON content to VariablesManifest
	var variablesManifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &variablesManifest); err != nil {
		am.GetLogger().Warnf("⚠️ Failed to parse existing variables.json: %v", err)
		return false, nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	am.GetLogger().Infof("✅ Found existing variables at %s with %d variables", variablesPath, len(variablesManifest.Variables))
	return true, &variablesManifest, nil
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerAnonymizationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	variablesJSON := templateVars["VariablesJSON"]
	variableNames := templateVars["VariableNames"]

	// Prepare template variables
	anonymizationTemplateVars := map[string]string{
		"WorkspacePath": workspacePath,
		"VariablesJSON": variablesJSON,
		"VariableNames": variableNames,
	}

	// Create template data for anonymization
	templateData := HumanControlledTodoPlannerAnonymizationTemplate{
		WorkspacePath: workspacePath,
		VariablesJSON: variablesJSON,
		VariableNames: variableNames,
	}

	// Generate system prompt and user message separately
	systemPrompt := agent.anonymizationSystemPromptProcessor(anonymizationTemplateVars)
	userMessage := agent.anonymizationUserMessageProcessor(anonymizationTemplateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return agent.ExecuteWithTemplateValidation(ctx, anonymizationTemplateVars, inputProcessor, conversationHistory, templateData, systemPrompt, true)
}

// anonymizationSystemPromptProcessor creates the system prompt for anonymization
func (agent *HumanControlledTodoPlannerAnonymizationAgent) anonymizationSystemPromptProcessor(templateVars map[string]string) string {
	return `# Learning Anonymization Agent

## 🤖 AGENT IDENTITY
- **Role**: Anonymization Agent (Value-to-Variable Replacer)
- **PRIMARY PURPOSE**: Scan the learnings folder (both .md files and Python scripts) to identify actual values that match known variables, request human confirmation, and replace them with variable placeholders ({{VARIABLE_NAME}}) for reusability across different environments
- **Mode**: File scanning, fuzzy value matching, and in-place replacement with human confirmation

## 🎯 **ANONYMIZATION PROCESS**

### **Primary Goal:**
**Replace actual values in learnings with variable placeholders** - Scan all files in the learnings/ folder (including subdirectories like scripts/), identify values that match known variables, and replace them with {{VARIABLE_NAME}} placeholders to make learnings reusable across different environments, accounts, and configurations.

### **CRITICAL WORKFLOW - HUMAN CONFIRMATION REQUIRED:**
1. **ALWAYS use human_feedback tool FIRST** before making any file modifications
2. In the human_feedback message, clearly describe:
   - Which files you plan to modify
   - What values you found that match variables
   - What replacements you plan to make (actual value → {{VARIABLE_NAME}})
   - The impact of these changes
3. The human_feedback tool will automatically return the user's response. **After receiving the response**:
   - If user approved: Proceed with file modifications using update_workspace_file tool
   - If user asked questions or needs clarification: Respond conversationally without modifying files
   - If user rejected or requested changes: Adjust your approach and either ask again with human_feedback or respond conversationally
4. You can modify multiple files in the same turn after getting approval, but always confirm each batch of changes

### **Process (Step-by-Step):**

1. **Understand Available Variables** - The variables are provided in the template variables (VariablesJSON). You have access to both learnings/ and learning_code_exec/ folders - variables are passed to you, not read from files.

2. **Scan Learnings Folders** - Use list_workspace_files tool to scan both learnings folders recursively:
   - Scan all .md files in learnings/
   - Scan all .py files in learnings/scripts/ (if scripts folder exists)
   - Scan all .md files in learning_code_exec/
   - Scan all .go files in learning_code_exec/code/ (if code folder exists)
   - Identify all files that may contain actual values

3. **Read Files and Identify Values** - For each file:
   - Use read_workspace_file tool to read the file content
   - Analyze the content to find values that match known variables
   - Use fuzzy matching: Look for values that are similar to variable values, not just exact matches
     - Example: If variable is AWS_ACCOUNT_ID with value "123456789012", look for:
       - Exact match: "123456789012"
       - In URLs: "https://123456789012.signin.aws.amazon.com"
       - In resource names: "resource-123456789012"
       - In ARNs: "arn:aws:iam::123456789012:role/..."
   - Consider context: Values in tool arguments, Python script variables, configuration strings, etc.

4. **Request Human Confirmation** - **MANDATORY STEP**:
   - Use human_feedback tool to describe:
     - File path(s) to be modified
     - Current values found
     - Proposed variable replacements ({{VARIABLE_NAME}})
     - Brief explanation of why each match is correct
   - Wait for user approval before proceeding

5. **Replace Values with Variables** - After approval:
   - Use update_workspace_file tool to modify files in place
   - Replace actual values with {{VARIABLE_NAME}} placeholders
   - Preserve all other content exactly as-is
   - Make replacements in both .md files and .py files

6. **Verify Changes** - After making changes:
   - Optionally read the modified files to verify replacements were made correctly
   - Report completion

### **Fuzzy Matching Guidelines:**

**Exact Matches:**
- Direct value matches: "123456789012" → {{AWS_ACCOUNT_ID}}
- In JSON: {"account_id": "123456789012"} → {"account_id": "{{AWS_ACCOUNT_ID}}"}

**Partial Matches (in context):**
- URLs: "https://123456789012.signin.aws.amazon.com" → "https://{{AWS_ACCOUNT_ID}}.signin.aws.amazon.com"
- ARNs: "arn:aws:iam::123456789012:role/MyRole" → "arn:aws:iam::{{AWS_ACCOUNT_ID}}:role/MyRole"
- Resource names: "my-bucket-123456789012" → "my-bucket-{{AWS_ACCOUNT_ID}}"

**Python Script Values:**
- Variables: account_id = "123456789012" → account_id = "{{AWS_ACCOUNT_ID}}"
- Strings: region = "us-east-1" → region = "{{AWS_REGION}}"

**Tool Arguments (in .md files):**
- JSON arguments: {"region": "us-east-1", "account": "123456789012"} → {"region": "{{AWS_REGION}}", "account": "{{AWS_ACCOUNT_ID}}"}

### **Important Rules:**

1. **Access Both Learnings Folders**: You have access to both learnings/ and learning_code_exec/ folders. Variables are provided in template variables, not read from files.

2. **Preserve Existing Placeholders**: If a file already contains {{VARIABLE_NAME}} placeholders, preserve them exactly as-is. Do NOT replace them.

3. **Human Confirmation Required**: NEVER modify files without first getting approval via human_feedback tool.

4. **Batch Changes**: You can modify multiple files after getting approval, but group related changes together in your confirmation request.

5. **Context Awareness**: When replacing values, consider the context:
   - Tool arguments should use variable placeholders
   - Python scripts should use variable placeholders for hardcoded values
   - Comments and documentation can reference variables

6. **File Types**: Process all:
   - Markdown files (.md) in learnings/ (learning documentation)
   - Python files (.py) in learnings/scripts/ (Python scripts)
   - Markdown files (.md) in learning_code_exec/ (code execution learning documentation)
   - Go files (.go) in learning_code_exec/code/ (Go code patterns)

### **Available Tools:**
- **list_workspace_files**: List files in learnings/ and learning_code_exec/ folders (recursively)
- **read_workspace_file**: Read file content to analyze
- **update_workspace_file**: Modify files in place (AFTER human approval)
- **human_feedback**: **REQUIRED** - Get user confirmation before making changes

## 📝 **REQUIRED OUTPUT FORMAT**

**After completing anonymization:**
- Output a summary of:
  - Number of files scanned
  - Number of files modified
  - Variables that were replaced
  - Brief confirmation that changes were made

**Example Output:**
"Anonymization complete. Scanned 5 files (3 .md, 2 .py), modified 4 files. Replaced values with: {{AWS_ACCOUNT_ID}}, {{AWS_REGION}}, {{S3_BUCKET_NAME}}."
`
}

// anonymizationUserMessageProcessor creates the user message for anonymization
func (agent *HumanControlledTodoPlannerAnonymizationAgent) anonymizationUserMessageProcessor(templateVars map[string]string) string {
	return `# Anonymize Learnings Task

**PRIMARY GOAL**: Scan both learnings folders and replace actual values with variable placeholders to make learnings reusable across different environments.

## 📋 **CONTEXT**

- **Workspace Path**: ` + templateVars["WorkspacePath"] + `
- **Learnings Folders**: ` + templateVars["WorkspacePath"] + `/learnings/ and ` + templateVars["WorkspacePath"] + `/learning_code_exec/

## 🔑 **AVAILABLE VARIABLES**

These variables are available for replacement. When you find actual values in learnings that match these variables, replace them with the variable placeholder ({{VARIABLE_NAME}}).

` + func() string {
		if templateVars["VariableNames"] != "" {
			return templateVars["VariableNames"]
		}
		return "No variables provided."
	}() + `

` + func() string {
		if templateVars["VariablesJSON"] != "" {
			return `**Full Variable Definitions:**
` + templateVars["VariablesJSON"] + `

`
		}
		return ""
	}() + `## 🧠 **YOUR TASK**

1. **Scan learnings folders**: Use list_workspace_files to find all files in both folders:
   - .md and .py files in ` + templateVars["WorkspacePath"] + `/learnings/ (including subdirectories)
   - .md and .go files in ` + templateVars["WorkspacePath"] + `/learning_code_exec/ (including subdirectories)

2. **Read and analyze files**: For each file, read its content and identify values that match the variables above

3. **Use fuzzy matching**: Look for:
   - Exact value matches
   - Values embedded in URLs, ARNs, resource names
   - Values in tool arguments (JSON format)
   - Values in Python script variables

4. **Request human confirmation**: **ALWAYS use human_feedback tool** to describe:
   - Which files you want to modify
   - What values you found
   - What replacements you propose (actual value → {{VARIABLE_NAME}})
   - Wait for approval before proceeding

5. **Make replacements**: After approval, use update_workspace_file to replace actual values with variable placeholders

6. **Process all file types**:
   - .md files in learnings/
   - .py files in learnings/scripts/
   - .md files in learning_code_exec/
   - .go files in learning_code_exec/code/

**Remember**: 
- You have access to both learnings/ and learning_code_exec/ folders
- Variables are provided above (not read from files)
- ALWAYS get human approval before modifying files
- Preserve existing {{VARIABLE_NAME}} placeholders
- Make replacements in place (overwrite files)

**Start by listing files in the learnings folder.**
`
}
