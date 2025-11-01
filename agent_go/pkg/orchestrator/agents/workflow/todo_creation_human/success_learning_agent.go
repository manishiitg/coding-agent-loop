package todo_creation_human

import (
	"context"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledTodoPlannerSuccessLearningTemplate holds template variables for success learning prompts
type HumanControlledTodoPlannerSuccessLearningTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
	ValidationResult        string
	CurrentObjective        string
}

// HumanControlledTodoPlannerSuccessLearningAgent analyzes successful executions to capture best practices and improve plan.json
type HumanControlledTodoPlannerSuccessLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerSuccessLearningAgent creates a new success learning agent
func NewHumanControlledTodoPlannerSuccessLearningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerSuccessLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerSuccessLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerSuccessLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	stepContextOutput := templateVars["StepContextOutput"]
	workspacePath := templateVars["WorkspacePath"]
	executionHistory := templateVars["ExecutionHistory"]
	validationResult := templateVars["ValidationResult"]
	currentObjective := templateVars["CurrentObjective"]
	variableNames := templateVars["VariableNames"]
	learningDetailLevel := templateVars["LearningDetailLevel"]
	// Default to "general" if not provided
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	// Prepare template variables
	successLearningTemplateVars := map[string]string{
		"StepTitle":               stepTitle,
		"StepDescription":         stepDescription,
		"StepSuccessCriteria":     stepSuccessCriteria,
		"StepContextDependencies": stepContextDependencies,
		"StepContextOutput":       stepContextOutput,
		"WorkspacePath":           workspacePath,
		"ExecutionHistory":        executionHistory,
		"ValidationResult":        validationResult,
		"CurrentObjective":        currentObjective,
		"VariableNames":           variableNames,
		"LearningDetailLevel":     learningDetailLevel,
	}

	// Create template data for success learning
	templateData := HumanControlledTodoPlannerSuccessLearningTemplate{
		StepTitle:               stepTitle,
		StepDescription:         stepDescription,
		StepSuccessCriteria:     stepSuccessCriteria,
		StepContextDependencies: stepContextDependencies,
		StepContextOutput:       stepContextOutput,
		WorkspacePath:           workspacePath,
		ExecutionHistory:        executionHistory,
		ValidationResult:        validationResult,
		CurrentObjective:        currentObjective,
	}

	// Execute using simple text output
	return agent.ExecuteWithTemplateValidation(ctx, successLearningTemplateVars, agent.successLearningInputProcessor, conversationHistory, templateData)
}

// successLearningInputProcessor creates the success learning analysis prompt
func (agent *HumanControlledTodoPlannerSuccessLearningAgent) successLearningInputProcessor(templateVars map[string]string) string {
	return `# Success Learning Analysis Agent

## 🎚️ **LEARNING DETAIL LEVEL: ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `EXACT MCP TOOLS` + "`" + `
Extract complete tool calls with full argument JSON from execution history.`
		}
		return `GENERAL PATTERNS` + "`" + `
Extract high-level approaches, strategies, and workflow patterns.`
	}() + `

## 📋 **STEP CONTEXT**
- **Title**: ` + templateVars["StepTitle"] + `
- **Description**: ` + templateVars["StepDescription"] + `
- **Success Criteria**: ` + templateVars["StepSuccessCriteria"] + `
- **Context Dependencies**: ` + templateVars["StepContextDependencies"] + `
- **Expected Output**: ` + templateVars["StepContextOutput"] + `
- **Workspace**: ` + templateVars["WorkspacePath"] + `

## 🎯 **OBJECTIVE**
` + templateVars["CurrentObjective"] + `

` + func() string {
		if templateVars["VariableNames"] != "" {
			return `## 🔑 AVAILABLE VARIABLES

These variables may appear in the plan as {{VARIABLE_NAME}} placeholders:
` + templateVars["VariableNames"] + `

**CRITICAL**: When updating the plan.md file, preserve ALL {{VARS}} exactly as written. 
DO NOT replace them with actual values. Keep variable placeholders like {{AWS_ACCOUNT_ID}} intact.
The updated plan must maintain variable placeholders, not resolved values.
`
		}
		return ""
	}() + `
## ✅ **SUCCESSFUL EXECUTION RESULTS**
` + templateVars["ExecutionHistory"] + `

## ✅ **VALIDATION RESULTS (SUCCESS)**
` + templateVars["ValidationResult"] + `

## 🧠 **YOUR TASK - SUCCESS ANALYSIS**

This step was executed successfully! Analyze what made it work well and document these learnings (do NOT update plan.md).

### **Success Analysis Process:**
1. **Read current plan** - Examine plan.md to understand the current step (read-only, do not modify)
2. **Parse ExecutionHistory** - ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `Extract EXACT tool calls from the execution conversation history below`
		}
		return `Analyze the execution conversation to identify high-level approaches and patterns`
	}() + `
3. **Identify success factors** - ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `What exact tools with arguments, approaches, and patterns worked best`
		}
		return `What overall approaches, strategies, and patterns led to success`
	}() + `
4. **Extract learnings** - ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `Capture complete MCP tool invocations with ALL arguments that led to success`
		}
		return `Capture high-level paths to success, general patterns, and strategic approaches`
	}() + `
5. **Document success patterns** - ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `Write EXACT tool calls with arguments to learnings/success_patterns.md and learnings/step_X_learning.md`
		}
		return `Write general success patterns and approaches to learnings/success_patterns.md and learnings/step_X_learning.md`
	}() + `
6. **DO NOT update plan.md** - Plan updates are handled separately by other agents

` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `### **How to Extract Tool Calls from ExecutionHistory:**
The ExecutionHistory section below contains the complete execution conversation. Parse it to extract:

**From "## Tool Call" sections, extract:**
- **Tool Name**: The exact MCP tool (e.g., fileserver.read_file, aws.cli_query)
- **Arguments**: The COMPLETE arguments JSON that was used
- **Tool Response**: The response that confirmed success

**Extraction Format Example:**
From ExecutionHistory:
` + "```" + `
### Tool Call
**Tool Name:** fileserver.read_file
**Arguments:** {"path":"/workspace/config.json","limit":100}
` + "```" + `

Extract to Success Patterns:
` + "```markdown" + `
- fileserver.read_file with {"path":"/workspace/config.json","limit":100}
` + "```" + `

**CRITICAL**: Extract the EXACT arguments JSON that was used, not a summary or description.`
		}
		return `### **How to Extract Success Patterns from ExecutionHistory:**
The ExecutionHistory section below contains the complete execution conversation. Analyze it to identify:

**Look for high-level patterns:**
- **General Approach**: What overall strategy or method led to success
- **Tool Categories**: What types of tools were most effective (e.g., file operations, API calls, database queries)
- **Sequence Patterns**: What order or workflow was most successful
- **Key Principles**: What general principles or best practices emerged

**Example of General Pattern Extraction:**
- Used file system tools to read configuration before making changes
- Validated environment state before deploying
- Used dry-run mode to test before applying changes
- Checked resource status after operations to confirm success

**Focus**: Extract the general path to success, not specific tool arguments. Capture the "what" and "why" of the approach, not the exact "how" with specific parameters.`
	}() + `

### **Learning Documentation Focus:**
Document the **final working approach** that achieved success in learnings files (do NOT update plan.md):

**Example Enhanced Step:**
` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `- **Description**: Deploy using kubectl apply to production
- **Success Criteria**: All pods Running status, rollout successful, endpoint accessible
- **Why This Step**: Dry-run catches syntax errors, rollout status ensures completion, health checks confirm service running
- **Success Patterns**:
  - kubernetes.kubectl_apply with {"file":"deployment.yaml","dry_run":"client"}
  - kubernetes.kubectl_rollout_status with {"resource":"deployment","name":"myapp"}
  - kubernetes.kubectl_get with {"resource":"pods","output":"json"}`
		}
		return `- **Description**: Deploy using kubectl apply to production
- **Success Criteria**: All pods Running status, rollout successful, endpoint accessible
- **Why This Step**: Dry-run validation prevents errors, status checks ensure completion, health verification confirms success
- **Success Patterns**:
  - Use dry-run validation before applying changes
  - Verify prerequisites (namespace exists) before deployment
  - Check status after operations to confirm success`
	}() + `

**Enhancement Guidelines:**
- ONLY add Success Patterns if specific ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `tools with exact arguments`
		}
		return `approaches or strategies`
	}() + ` were identified
- Keep descriptions concise, focus on what worked
- Integrate learnings directly into existing plan sections

### **Available Tools:**
You have access to all MCP tools to examine workspace files and gather additional context.

## 📝 **REQUIRED OUTPUT FORMAT**

## Success Analysis Summary

### What Worked Well:
- [Key factors that made execution successful]
- [Patterns or strategies that led to success]

### Best Practices Captured:
- ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `[Tool calls with complete arguments: tool_name with {"arg":"value"}]`
		}
		return `[General approaches and workflow patterns]`
	}() + `

---

## Learning Documentation Actions

### Learnings Documented:
- [Success patterns captured: ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `specific tools, commands, and step-by-step approach that worked`
		}
		return `general approaches and strategies that led to success`
	}() + `]
- [Validation methods that worked: ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `exact validation methods and expected outputs`
		}
		return `validation approaches and expected outcomes (high-level)`
	}() + `]
- [Insights about why this approach worked best]
- [Context dependencies that were crucial for success]
- [Success Patterns documented with ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `EXACT tool calls including complete argument JSON from ExecutionHistory`
		}
		return `general patterns and approaches extracted from ExecutionHistory`
	}() + `]

**NOTE**: Document learnings in learnings/ folder files - do NOT update plan.md file

### Success Patterns Documented (` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `EXACT tool calls extracted`
		}
		return `General patterns extracted`
	}() + `):
- ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `[Tool name with complete argument JSON: tool_name with {"arg":"value"}]
- [Tool combinations that worked well with specific configurations]
- [Patterns and best practices discovered from actual execution]
- [Context dependencies that were crucial for success]`
		}
		return `[General approach or strategy that led to success]
- [High-level workflow or sequence that worked well]
- [Key principles or patterns discovered from execution]
- [Context dependencies that were crucial for success]`
	}() + `

---

## 📁 **FILE PERMISSIONS (Success Learning Agent)**

**READ:**
- planning/plan.md (current markdown plan) - path: ` + templateVars["WorkspacePath"] + `/todo_creation_human/planning/plan.md
- validation/step_X_validation_report.md (validation results with execution summary) - path: ` + templateVars["WorkspacePath"] + `/todo_creation_human/validation/step_X_validation_report.md

**WRITE:**
- learnings/success_patterns.md (append cumulative success patterns) - path: ` + templateVars["WorkspacePath"] + `/todo_creation_human/learnings/success_patterns.md
- learnings/step_X_learning.md (create detailed learning for this step) - path: ` + templateVars["WorkspacePath"] + `/todo_creation_human/learnings/step_X_learning.md

**RESTRICTIONS:**
- Learning outputs go to learnings/ folder ONLY
- **DO NOT** update or modify planning/plan.md (plan updates are handled separately)
- **DO NOT** read or write files in execution/ folder (execution agent handles those)
- Read execution details from validation reports (which contain execution conversation)
- Focus on capturing success patterns and best practices
- All file paths must be relative to ` + templateVars["WorkspacePath"] + `/todo_creation_human/

---

**Key Requirements:**
- Analyze what made execution successful and document learnings
- **DO NOT** update planning/plan.md (plan updates are handled separately)
- Document learnings ONLY in learnings/ folder
- Focus on capturing success patterns that can be referenced later
- ONLY add Success Patterns if meaningful ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `tool calls were identified - include complete argument JSON`
		}
		return `patterns were identified - focus on what and why, not exact tools`
	}() + `
`
}
