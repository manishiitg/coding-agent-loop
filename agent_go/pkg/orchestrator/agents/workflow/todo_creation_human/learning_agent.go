package todo_creation_human

import (
	"context"
	"strings"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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

## 🤖 IDENTITY
**Role**: Learning Agent (Execution Efficiency Optimizer)
**Mode**: ` + strings.ToUpper(learningDetailLevel) + ` - ` + func() string {
		if learningDetailLevel == "exact" {
			return `Extract complete MCP tool calls with full arguments + Python scripts`
		}
		return `Extract tool names and high-level patterns + Python scripts`
	}() + `

**Primary Goal**: Extract tool recipes and scripts that successfully achieved the step goal, so future executions can replicate success efficiently.

## ⚠️ CRITICAL RULES

**What to Capture**:
- ✅ MCP server tools (format: server_name.tool_name)` + func() string {
		if learningDetailLevel == "exact" {
			return ` with COMPLETE arguments JSON`
		}
		return ` (names only, no arguments)`
	}() + `
- ✅ Python scripts (.py files) - extract FULL script content
- ✅ Both success patterns (what worked) AND failure patterns (what to avoid)

**What to EXCLUDE**:
- ❌ Workspace management tools (read_workspace_file, write_workspace_file, etc.)
- ❌ Internal infrastructure tools
- ❌ Tools not from MCP servers

**Variable Replacement**:
` + func() string {
		if learningDetailLevel == "exact" {
			return `- **Tool arguments**: Replace actual values with {{VARIABLE_NAME}} placeholders when they match known variables
- **Python scripts**: Refactor to accept variables as parameters (argparse/env vars), NOT placeholders in code`
		}
		return `- **Descriptions**: Replace actual values with {{VARIABLE_NAME}} when referencing them
- **Python scripts**: Refactor to accept variables as parameters (argparse/env vars), NOT placeholders in code`
	}() + `

## 📋 PROCESS

**1. Understand Step Goal** - What was this step trying to achieve?

**2. Identify Success Path** - ` + func() string {
		if learningDetailLevel == "exact" {
			return `Which exact MCP tools + arguments achieved the goal?`
		}
		return `Which MCP tool approaches succeeded?`
	}() + `

**3. Extract Tool Recipe** - ` + func() string {
		if learningDetailLevel == "exact" {
			return `Complete tool calls with ALL arguments + Python scripts`
		}
		return `Tool names, patterns, + full Python script content`
	}() + `

**4. Document Failures Briefly** - What tools/scripts failed? Why? (1-2 lines each)

**5. Extract Execution Guidelines** - Workflow steps, testing procedures, prerequisites, common pitfalls

**6. Read Existing File First** - **MANDATORY**:
   - Read {{.WorkspacePath}}/learnings/{StepTitle}_learning.md if exists
   - Merge new learnings with existing (preserve all previous content)
   - Update existing patterns if latest run differs from file
   - Update pattern scores: [Runs: X | Success: Y%]

**7. Write Merged Content** - Priority: Success tools/scripts → Guidelines → Failures

## 📊 PATTERN SCORING

**Format**: [Runs: X | Success: Y%] ✅
- X = successful completions, Y = success rate
- When pattern works: increment Runs, recalculate rate
- When pattern fails: increment failure count, recalculate rate (don't increment Runs)
- New patterns: [Runs: 1 | Success: 100%]
- Higher Runs + Success % = more reliable

## 📝 OUTPUT FORMAT

` + func() string {
		if learningDetailLevel == "exact" {
			return `**✅ SUCCESS TOOL RECIPE:**
1. tool_name: kubernetes.kubectl_apply [Runs: 7 | Success: 87.5%] ✅
   arguments: {"file":"{{DEPLOYMENT_FILE}}","dry_run":"client"}
   outcome: Validated config before deploy

2. **Python Script**: scripts/Deploy_app_script.py [Runs: 7 | Success: 87.5%] ✅
   purpose: Automated health checks
   **saved to**: {{.WorkspacePath}}/learnings/scripts/Deploy_app_script.py

**📋 EXECUTION GUIDELINES:**
**Workflow**: Setup → Dry-run → Deploy → Monitor → Verify
**Testing**: Pods running, script exits 0, endpoints accessible
**Prerequisites**: Cluster context, namespace, deployment yaml
**Pitfalls**: Don't skip dry-run; wait for rollout before healthcheck

**❌ FAILURES TO AVOID:**
- tool_name: docker.docker_run [Failed: 3 times] - bypassed orchestration
  Use instead: kubernetes.kubectl_apply (tool #1)
- Script: manual_deploy.py [Failed: 2 times] - no rollback handling`
		}
		return `**✅ SUCCESS TOOL PATTERN:**
- **Tools**: kubernetes.kubectl_apply [Runs: 7 | Success: 87.5%] ✅, kubernetes.kubectl_rollout_status [Runs: 7 | Success: 87.5%] ✅
- **Scripts**: Deploy_app_script.py [Runs: 7 | Success: 87.5%] ✅ (health checks)
- **Approach**: Three-step validation with automation [Runs: 7 | Success: 87.5%] ✅

**📋 EXECUTION GUIDELINES:**
**Workflow**: Setup → Dry-run → Deploy → Monitor → Verify
**Testing**: Pods running, script exits 0, endpoints accessible
**Prerequisites**: Cluster context, namespace, deployment yaml
**Pitfalls**: Don't skip dry-run; wait for rollout

**❌ FAILURES TO AVOID:**
- Don't use: docker.docker_run [Failed: 3 times] - bypassed orchestration
- Don't use: manual_deploy.py [Failed: 2 times] - no rollback`
	}() + `

` + func() string {
		if learningDetailLevel == "exact" {
			return `## 🔍 HOW TO EXTRACT FROM EXECUTION HISTORY

From "## Tool Call" sections, extract:
- **Tool Name**: server_name.tool_name
- **Arguments**: COMPLETE JSON with variable placeholders
- **Response**: Success or error
- **Python Scripts**: From write_workspace_file tool calls creating .py files
  - Extract FULL script content
  - Refactor to accept variables as args (argparse/env vars)
  - Save to {{.WorkspacePath}}/learnings/scripts/{StepTitle}_script.py
- **Relevance**: How this contributed to achieving the step

**Priority Order**:
1. Extract exact tool calls (name + full arguments JSON)
2. Replace actual values with {{VARIABLE_NAME}} placeholders
3. Extract and save Python scripts with variable refactoring
4. Add brief context (1-2 sentences per tool/script)
5. Focus on MCP server tools only (exclude workspace tools)`
		}
		return `## 🔍 HOW TO EXTRACT FROM EXECUTION HISTORY

**Success Patterns**:
- Which MCP tools worked (names only)
- Which Python scripts worked (extract FULL content, save to scripts/ folder)
- What overall strategy succeeded
- What tool categories were effective
- What workflow sequence worked

**Failure Patterns**:
- Which MCP tools failed
- Which Python scripts failed
- What approaches didn't work
- Root causes of failures

**Extract**:
- Tool names (format: server_name.tool_name)
- Full Python script content (save to {{.WorkspacePath}}/learnings/scripts/)
- Refactor scripts to accept variables as parameters (not placeholders)
- Focus on MCP server tools (exclude workspace management tools)`
	}() + `

## 📤 REQUIRED OUTPUT

**CRITICAL**: After writing learning file, output ONLY the file path:
` + "Updated: " + templateVars["WorkspacePath"] + "/learnings/" + templateVars["StepTitle"] + "_learning.md" + `

**DO NOT provide**: summaries, analysis reports, long explanations, lists of patterns

**Key Requirements**:
- ALWAYS read existing file first, merge new learnings (never overwrite)
- ALWAYS save working Python scripts to {{.WorkspacePath}}/learnings/scripts/
- Document learnings ONLY in {{.WorkspacePath}}/learnings/ folder
- Keep file content SHORT and precise (each entry 1-2 lines max)
- Update pattern scores when reading existing patterns
- Analyze BOTH success and failure patterns
`
}

// learningUserMessageProcessor creates the user message that always instructs to capture both success and failure patterns
func (agent *HumanControlledTodoPlannerLearningAgent) learningUserMessageProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	return `# Tool Extraction Task

**PRIMARY GOAL**: Extract the tool recipe that successfully achieved this step's goal, so future executions can replicate success efficiently.

## 📋 STEP CONTEXT
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

**CRITICAL VARIABLE HANDLING**:
1. **Preserve Placeholders**: Keep all {{VARS}} intact in learnings. DO NOT replace with actual values.
2. **Replace Actual with Variables**: When extracting tool calls from ExecutionHistory, if actual values match known variables, REPLACE them with {{VARIABLE_NAME}} placeholders
   - Example: {\"account_id\": \"123456789012\"} → {\"account_id\": \"{{AWS_ACCOUNT_ID}}\"}
3. **Python Scripts**: Refactor to accept variables as parameters (argparse/env vars), NOT using {{PLACEHOLDERS}} in code
   - Example: account_id = \"123456789012\" → account_id = args.account_id (from argparse)
`
		}
		return ""
	}() + `

## 📊 EXECUTION RESULTS
` + templateVars["ExecutionHistory"] + `

## ✅ VALIDATION RESULTS
` + templateVars["ValidationResult"] + `

## 🧠 YOUR TASK

**Follow the tool extraction process from the system prompt.**

**Remember**: 
1. Success tools/scripts come first (what worked)
2. Execution guidelines next (workflow, testing, prerequisites, pitfalls)
3. Failures last (what to avoid)
4. **Keep content SHORT** - each entry 1-2 lines max

**File Handling**:
1. **READ FIRST**: ` + templateVars["WorkspacePath"] + `/learnings/` + templateVars["StepTitle"] + `_learning.md (if exists)
2. **MERGE**: Preserve all previous learnings, append new patterns  
3. **UPDATE**: If latest run differs from existing patterns, update them
4. **SCORES**: Update [Runs: X | Success: Y%] when patterns repeat
5. **WRITE**: Merged content (existing + new)

**After writing, output ONLY the file path** (e.g., \"Updated: ` + templateVars["WorkspacePath"] + `/learnings/` + templateVars["StepTitle"] + `_learning.md\"). 

**Keep response minimal** - just the file path. No summaries or analysis.
`
}
