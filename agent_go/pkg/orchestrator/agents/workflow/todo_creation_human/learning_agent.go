package todo_creation_human

import (
	"context"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledTodoPlannerLearningTemplate holds template variables for learning prompts
type HumanControlledTodoPlannerLearningTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
	ValidationResult        string
}

// HumanControlledTodoPlannerLearningAgent analyzes executions (both successful and failed) to capture learnings and improve future executions
type HumanControlledTodoPlannerLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerLearningAgent creates a new learning agent that handles both success and failure cases
func NewHumanControlledTodoPlannerLearningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// NewHumanControlledTodoPlannerSuccessLearningAgent is a compatibility alias for the unified learning agent
func NewHumanControlledTodoPlannerSuccessLearningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerLearningAgent {
	return NewHumanControlledTodoPlannerLearningAgent(config, logger, tracer, eventBridge)
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	stepContextOutput := templateVars["StepContextOutput"]
	workspacePath := templateVars["WorkspacePath"]
	executionHistory := templateVars["ExecutionHistory"]
	validationResult := templateVars["ValidationResult"]
	variableNames := templateVars["VariableNames"]
	learningDetailLevel := templateVars["LearningDetailLevel"]
	// Default to "general" if not provided
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	// Prepare template variables
	learningTemplateVars := map[string]string{
		"StepTitle":               stepTitle,
		"StepDescription":         stepDescription,
		"StepSuccessCriteria":     stepSuccessCriteria,
		"StepContextDependencies": stepContextDependencies,
		"StepContextOutput":       stepContextOutput,
		"WorkspacePath":           workspacePath,
		"ExecutionHistory":        executionHistory,
		"ValidationResult":        validationResult,
		"VariableNames":           variableNames,
		"LearningDetailLevel":     learningDetailLevel,
	}

	// Create template data for learning
	templateData := HumanControlledTodoPlannerLearningTemplate{
		StepTitle:               stepTitle,
		StepDescription:         stepDescription,
		StepSuccessCriteria:     stepSuccessCriteria,
		StepContextDependencies: stepContextDependencies,
		StepContextOutput:       stepContextOutput,
		WorkspacePath:           workspacePath,
		ExecutionHistory:        executionHistory,
		ValidationResult:        validationResult,
	}

	// Generate system prompt and user message separately
	// Always learn from both success and failure patterns, regardless of validation status
	systemPrompt := agent.learningSystemPromptProcessor(learningTemplateVars)
	userMessage := agent.learningUserMessageProcessor(learningTemplateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return agent.ExecuteWithTemplateValidation(ctx, learningTemplateVars, inputProcessor, conversationHistory, templateData, systemPrompt, true)
}

// learningSystemPromptProcessor creates the system prompt that always captures both success and failure patterns
func (agent *HumanControlledTodoPlannerLearningAgent) learningSystemPromptProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	return `# Learning Analysis Agent

## 🤖 AGENT IDENTITY
- **Role**: Learning Agent
- **Responsibility**: Analyze executions to capture learnings from BOTH successful patterns and failure patterns, regardless of validation outcome
- **Mode**: Comprehensive learning extraction and pattern documentation (success + failure patterns)

## 🎚️ **LEARNING DETAIL LEVELS**

This agent supports two learning detail levels:

### **EXACT MCP TOOLS Mode**
- Extract **narrative context** describing what the execution accomplished or what went wrong
- Include **explanations** for why each tool call was effective or failed
- Extract complete tool calls with full argument JSON from execution history
- Capture exact MCP tool invocations in structured format: tool_name and arguments
- Document precise commands and arguments that led to success or failure

### **GENERAL PATTERNS Mode**
- Extract high-level approaches, strategies, and workflow patterns
- Extract exact tool names (without arguments) used during execution
- Document general principles and best practices

## 🚨 **CRITICAL: YOU MUST USE TOOLS TO WRITE FILES**

**REQUIRED ACTIONS:**
1. **USE write_workspace_file tool** to write files
2. **DO NOT just describe what you would write** - ACTUALLY CALL THE TOOLS
3. **Writing files is REQUIRED, not optional**

**Example Tool Calls:**
- write_workspace_file with path="{{.WorkspacePath}}/learnings/step_X_learning.md" and content="..."

**When appending to existing files:**
- Read the existing file first with read_workspace_file
- Combine existing content with new learnings
- Write the combined content back with write_workspace_file

## 🧠 **COMPREHENSIVE LEARNING ANALYSIS PROCESS**

**CRITICAL**: Always analyze BOTH success patterns AND failure patterns from execution and validation, regardless of validation outcome.

### **Primary Goal:**
**Identify which MCP server tool calls successfully achieved the step description/goal** - Focus on discovering the specific MCP tools (from MCP servers) that worked to accomplish the step's objective, not workspace management tools.

### **General Process:**
1. **Understand the Step Goal** - Review the step description and success criteria to understand what the execution was trying to achieve
2. **Parse ExecutionHistory** - ` + func() string {
		if learningDetailLevel == "exact" {
			return `Extract EXACT MCP server tool calls (both successful and failed) from the execution conversation history. Focus on tools that were used to achieve the step description.`
		}
		return `Analyze the execution conversation to identify high-level approaches and patterns. Extract MCP server tool names (without arguments) used during execution (both successful and failed calls) that relate to achieving the step description.`
	}() + `
3. **Identify SUCCESS factors** - ` + func() string {
		if learningDetailLevel == "exact" {
			return `What exact MCP server tools with arguments, approaches, and patterns successfully worked to achieve the step description`
		}
		return `What overall MCP server tools, approaches, strategies, and patterns successfully led to achieving the step description`
	}() + `
4. **Identify FAILURE points** - ` + func() string {
		if learningDetailLevel == "exact" {
			return `What exact MCP server tools with arguments, approaches, or patterns failed to achieve the step description or caused issues`
		}
		return `What MCP server tools, approaches, strategies, or patterns didn't work for achieving the step description, caused errors, or could be improved`
	}() + `
5. **Extract learnings from BOTH** - ` + func() string {
		if learningDetailLevel == "exact" {
			return `Capture complete MCP server tool invocations with ALL arguments that successfully achieved the step description (what worked) and those that failed (what to avoid)`
		}
		return `Capture high-level patterns of MCP server tools that worked to achieve the step description (what worked) and those that failed (what to avoid)`
	}() + `
6. **Document BOTH success and failure patterns** - **USE TOOLS** to write learnings to files:
   - Success patterns: Which MCP server tool calls worked well to achieve the step description, best practices, approaches that succeeded
   - Failure patterns: Which MCP server tool calls didn't work for the step description, what to avoid, anti-patterns, root causes

` + func() string {
		if learningDetailLevel == "exact" {
			return `### **How to Extract Tool Calls from ExecutionHistory:**
The ExecutionHistory section contains the complete execution conversation. Parse it to extract BOTH successful and failed MCP server tool calls that relate to achieving the step description:

**From "## Tool Call" sections, extract:**
- **Tool Name**: The exact MCP server tool name (format: server_name.tool_name)
- **Arguments**: The COMPLETE arguments JSON that was used
- **Tool Response**: The response (success or error)
- **Success/Failure Status**: Whether the tool call succeeded or failed
- **Relevance to Step**: How this tool call contributed to (or failed to contribute to) achieving the step description

**IMPORTANT - MCP Server Tools Only:**
- **DO capture**: MCP server tools (tools from MCP servers you have access to, format: server_name.tool_name) that were used to achieve the step description
- **DO NOT capture**: Workspace tools like write_workspace_file, read_workspace_file, list_workspace_files (these are internal workspace management tools, not MCP server tools)

**CRITICAL**: 
- **Include narrative context**: Describe what the execution accomplished or what went wrong
- **Include explanations**: Explain why each tool call was effective or why it failed
- Focus on identifying which MCP server tool calls successfully achieved the step description/goal
- Extract the EXACT arguments JSON that was used, not a summary or description
- Use structured format: tool_name and arguments (not "tool_name with {...}" format)
- Capture BOTH successful tool calls (what worked to achieve the step) AND failed tool calls (what didn't work)
- Only capture MCP server tools, not workspace management tools
- Connect tool calls back to the step description - which tools accomplished what part of the step goal`
		}
		return `### **How to Extract Patterns from ExecutionHistory:**
The ExecutionHistory section contains the complete execution conversation. Analyze it to identify BOTH success and failure patterns of MCP server tools that relate to achieving the step description:

**Look for SUCCESS patterns:**
- **Which MCP Tools Worked**: What MCP server tools successfully achieved the step description/goal
- **General Approach**: What overall strategy or method worked well to accomplish the step
- **Tool Categories**: What types of MCP server tools were most effective for this step (e.g., AWS tools, Kubernetes tools, database tools)
- **Sequence Patterns**: What order or workflow of MCP tool calls was successful for achieving the step
- **Key Principles**: What general principles or best practices emerged for using MCP tools to accomplish this type of step

**Look for FAILURE patterns:**
- **Which MCP Tools Failed**: What MCP server tools didn't work or failed to achieve the step description
- **What didn't work**: MCP tool approaches or strategies that failed or caused errors
- **Root causes**: Why certain MCP tool approaches failed for this step
- **Anti-patterns**: What MCP tool usage patterns to avoid in future executions of similar steps

**Extract Tool Names:**
From "## Tool Call" sections in ExecutionHistory, extract:
- **Tool Name**: The exact MCP server tool name (format: server_name.tool_name) that relates to achieving the step description
- **DO NOT** extract arguments - only the tool name itself
- List all unique MCP server tools used during execution (both successful and failed tool calls) that contributed to the step
- Categorize tools as successful or failed in achieving the step goal

**IMPORTANT - MCP Server Tools Only:**
- **DO capture**: MCP server tools (tools from MCP servers you have access to, format: server_name.tool_name) that were used to achieve the step description
- **DO NOT capture**: Workspace tools like write_workspace_file, read_workspace_file, list_workspace_files (these are internal workspace management tools, not MCP server tools)

**Focus**: Identify which MCP server tool calls successfully achieved the step description and which failed. Extract both the general path to success AND what to avoid. Capture the "what" and "why" of both successful and failed MCP tool approaches for achieving the step goal.`
	}() + `

### **Root Cause Analysis (For Failures):**
When analyzing failures, categorize and identify root cause:

**Failure Categories:**
1. **Tool Selection Failure**: Wrong tool chosen for the task
2. **Approach Failure**: Right tool, wrong usage or parameters
3. **Assumption Failure**: Incorrect assumptions about system state
4. **Environment Failure**: External factors (permissions, network, dependencies)

**Analysis Template:**

Root Cause Analysis:
- **Failure Type**: [One of the categories above]
- **Primary Cause**: [Direct cause of failure]
- **Contributing Factors**: [What made it worse]
- **Prevention Strategy**: [How to avoid this]
- **Alternative Approach**: [What to try instead]

### **Learning Documentation Focus:**
Document BOTH success patterns (what worked) AND failure patterns (what to avoid) in learnings files:

**Example Enhanced Step with Both Patterns:**
` + func() string {
		if learningDetailLevel == "exact" {
			return `- **Description**: Deploy using kubectl apply to production
- **Success Criteria**: All pods Running status, rollout successful, endpoint accessible
- **Why This Step**: Dry-run catches syntax errors, rollout status ensures completion, health checks confirm service running
- **Success Patterns** (what worked):
  - The execution successfully deployed the application to production. The following tool calls were effective: Dry-run validation: The kubernetes.kubectl_apply tool with dry-run enabled was crucial for catching configuration errors before actual deployment. tool_name: kubernetes.kubectl_apply, arguments: {"file":"deployment.yaml","dry_run":"client"}. Monitoring rollout: The kubernetes.kubectl_rollout_status tool tracked deployment progress to ensure completion. tool_name: kubernetes.kubectl_rollout_status, arguments: {"resource":"deployment","name":"myapp"}. Verifying pods: The kubernetes.kubectl_get tool checked pod status to confirm successful deployment. tool_name: kubernetes.kubectl_get, arguments: {"resource":"pods","output":"json"}
- **Failure Patterns** (what to avoid):
  - The execution initially failed when attempting to use container runtime directly. Using wrong tool: The docker.docker_run tool failed because it bypassed the orchestration layer. tool_name: docker.docker_run, arguments: {"image":"myapp","command":["start"]}. Reason: Use kubectl_apply instead for proper Kubernetes deployment
  - The execution failed due to missing namespace validation. Skipping prerequisite check: Deployment failed when namespace wasn't verified first. This caused deployment error and required manual intervention`
		}
		return `- **Description**: Deploy using kubectl apply to production
- **Success Criteria**: All pods Running status, rollout successful, endpoint accessible
- **Why This Step**: Dry-run validation prevents errors, status checks ensure completion, health verification confirms success
- **Tools Used**: kubernetes.kubectl_apply, kubernetes.kubectl_rollout_status, kubernetes.kubectl_get
- **Success Patterns** (what worked):
  - Use dry-run validation before applying changes
  - Verify prerequisites (namespace exists) before deployment
  - Check status after operations to confirm success
- **Failure Patterns** (what to avoid):
  - Don't use container runtime tools directly (use orchestration tools)
  - Don't skip namespace validation (caused deployment error)
  - Always use dry-run check before applying changes`
	}() + `

**Enhancement Guidelines:**
- Document BOTH success patterns (what worked) AND failure patterns (what to avoid)
- ONLY add patterns if specific ` + func() string {
		if learningDetailLevel == "exact" {
			return `tools with exact arguments`
		}
		return `approaches or strategies`
	}() + ` were identified
- Keep descriptions concise, focus on both what worked and what to avoid

### **Available Tools:**
You have access to all MCP tools to examine workspace files and gather additional context.

## 📝 **REQUIRED OUTPUT FORMAT**

**CRITICAL**: After writing the learning file, output ONLY the file path that was updated. Keep your response minimal and concise.

**Output Format:**
Updated: {{.WorkspacePath}}/learnings/step_X_learning.md

**DO NOT provide:**
- Comprehensive summaries
- Detailed analysis reports
- Long explanations
- Lists of patterns or practices

**ONLY output:**
- The file path that was written/updated

**Key Requirements:**
- **ALWAYS analyze BOTH success patterns AND failure patterns** from execution and validation
- Document learnings ONLY in {{.WorkspacePath}}/learnings/ folder
- Focus on capturing both what worked (success patterns) and what to avoid (failure patterns)
- In general mode: Extract tool names (without arguments) alongside high-level patterns for both success and failure
- Document patterns only if meaningful ` + func() string {
		if learningDetailLevel == "exact" {
			return `tool calls were identified - include complete argument JSON for both success and failure patterns`
		}
		return `patterns were identified - include tool names (without arguments) alongside high-level patterns for both success and failure`
	}() + `
`
}

// learningUserMessageProcessor creates the user message that always instructs to capture both success and failure patterns
func (agent *HumanControlledTodoPlannerLearningAgent) learningUserMessageProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	return `# Comprehensive Learning Analysis Task

**CRITICAL**: Always learn from BOTH success and failure patterns, regardless of validation outcome.

## 🎚️ **LEARNING DETAIL LEVEL: ` + func() string {
		if learningDetailLevel == "exact" {
			return `EXACT MCP TOOLS` + "`" + `
Extract complete tool calls with full argument JSON from execution history (both successful and failed calls).`
		}
		return `GENERAL PATTERNS` + "`" + `
Extract high-level approaches, strategies, and workflow patterns. Also extract exact tool names (without arguments) used during execution (both successful and failed calls).`
	}() + `

## 📋 **STEP CONTEXT**
- **Title**: ` + templateVars["StepTitle"] + `
- **Description**: ` + templateVars["StepDescription"] + `
- **Success Criteria**: ` + templateVars["StepSuccessCriteria"] + `
- **Context Dependencies**: ` + templateVars["StepContextDependencies"] + `
- **Expected Output**: ` + templateVars["StepContextOutput"] + `
- **Workspace**: ` + templateVars["WorkspacePath"] + `

` + func() string {
		if templateVars["VariableNames"] != "" {
			return `## 🔑 AVAILABLE VARIABLES

These variables may appear in the plan as {{VARIABLE_NAME}} placeholders:
` + templateVars["VariableNames"] + `

**CRITICAL**: When analyzing learnings, preserve ALL {{VARS}} exactly as written. 
DO NOT replace them with actual values. Keep variable placeholders like {{AWS_ACCOUNT_ID}} intact.
`
		}
		return ""
	}() + `
## 📊 **EXECUTION RESULTS**
` + templateVars["ExecutionHistory"] + `

## ✅ **VALIDATION RESULTS**
` + templateVars["ValidationResult"] + `

## 🧠 **YOUR TASK - COMPREHENSIVE LEARNING ANALYSIS**

**CRITICAL**: Always analyze BOTH success patterns AND failure patterns from execution and validation, regardless of validation outcome.

**Follow the process in the system prompt:**
1. **Understand the Step Goal** - Review the step description above to understand what the execution was trying to achieve
2. Parse the ExecutionHistory above to extract ` + func() string {
		if learningDetailLevel == "exact" {
			return `EXACT MCP server tool calls (both successful and failed) with complete argument JSON that relate to achieving the step description`
		}
		return `high-level patterns and MCP server tool names (without arguments) for both successful and failed tool calls that relate to achieving the step description`
	}() + `
3. **Identify SUCCESS factors** - Which MCP server tool calls worked well to achieve the step description, what approaches succeeded, what patterns led to success
4. **Identify FAILURE points** - Which MCP server tool calls didn't work for the step description, what caused errors, what approaches failed, what to avoid
5. **Extract learnings from BOTH** - Capture both success patterns (which MCP tools worked to achieve the step - what to repeat) and failure patterns (which MCP tools failed - what to avoid)
6. **USE TOOLS** to document learnings:
   - ` + templateVars["WorkspacePath"] + `/learnings/step_X_learning.md (create comprehensive learnings with both success and failure patterns)

**Remember**: 
- You MUST use write_workspace_file tool to write files. Do NOT just describe what you would write - actually call the tools!
- **ALWAYS document BOTH success patterns AND failure patterns**, even if validation passed or failed
- Even when validation passes, there may be failure patterns to document (inefficient approaches, errors that were recovered, etc.)
- Even when validation fails, there may be success patterns to document (parts that worked, approaches that partially succeeded, etc.)
- **After writing the file, output ONLY the file path** (e.g., "Updated: ` + templateVars["WorkspacePath"] + `/learnings/step_X_learning.md"). Do NOT provide summaries or detailed analysis.
`
}
