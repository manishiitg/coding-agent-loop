package todo_creation_human

import (
	"context"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledTodoPlannerFailureLearningTemplate holds template variables for failure learning prompts
type HumanControlledTodoPlannerFailureLearningTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepWhyThisStep         string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
	ValidationResult        string
	CurrentObjective        string
}

// HumanControlledTodoPlannerFailureLearningAgent analyzes failed executions to provide refined task descriptions for retry
type HumanControlledTodoPlannerFailureLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerFailureLearningAgent creates a new failure learning agent
func NewHumanControlledTodoPlannerFailureLearningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerFailureLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerValidationAgentType, // Use validation agent type for now
		eventBridge,
	)

	return &HumanControlledTodoPlannerFailureLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerFailureLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepWhyThisStep := templateVars["StepWhyThisStep"]
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
	failureLearningTemplateVars := map[string]string{
		"StepTitle":               stepTitle,
		"StepDescription":         stepDescription,
		"StepSuccessCriteria":     stepSuccessCriteria,
		"StepWhyThisStep":         stepWhyThisStep,
		"StepContextDependencies": stepContextDependencies,
		"StepContextOutput":       stepContextOutput,
		"WorkspacePath":           workspacePath,
		"ExecutionHistory":        executionHistory,
		"ValidationResult":        validationResult,
		"CurrentObjective":        currentObjective,
		"VariableNames":           variableNames,
		"LearningDetailLevel":     learningDetailLevel,
	}

	// Create template data for failure learning
	templateData := HumanControlledTodoPlannerFailureLearningTemplate{
		StepTitle:               stepTitle,
		StepDescription:         stepDescription,
		StepSuccessCriteria:     stepSuccessCriteria,
		StepWhyThisStep:         stepWhyThisStep,
		StepContextDependencies: stepContextDependencies,
		StepContextOutput:       stepContextOutput,
		WorkspacePath:           workspacePath,
		ExecutionHistory:        executionHistory,
		ValidationResult:        validationResult,
		CurrentObjective:        currentObjective,
	}

	// Execute using simple text output
	return agent.ExecuteWithTemplateValidation(ctx, failureLearningTemplateVars, agent.failureLearningInputProcessor, conversationHistory, templateData)
}

// failureLearningInputProcessor creates the failure learning analysis prompt
func (agent *HumanControlledTodoPlannerFailureLearningAgent) failureLearningInputProcessor(templateVars map[string]string) string {
	return `# Failure Learning Analysis Agent

## 🎚️ **LEARNING DETAIL LEVEL: ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `EXACT MCP TOOLS` + "`" + `
Extract specific failed tool calls with complete argument JSON.`
		}
		return `GENERAL PATTERNS` + "`" + `
Extract high-level failure patterns and approaches that didn't work.`
	}() + `

## 📋 **STEP CONTEXT**
- **Title**: ` + templateVars["StepTitle"] + `
- **Description**: ` + templateVars["StepDescription"] + `
- **Success Criteria**: ` + templateVars["StepSuccessCriteria"] + `
- **Why This Step**: ` + templateVars["StepWhyThisStep"] + `
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
## 📊 **EXECUTION RESULTS**
` + templateVars["ExecutionHistory"] + `

## ✅ **VALIDATION RESULTS**
` + templateVars["ValidationResult"] + `

## 🧠 **YOUR TASK - FAILURE ANALYSIS**

This step execution failed validation. Analyze what went wrong and provide a refined task description for immediate retry.

### **Failure Analysis Process:**
1. **Read current plan** - Examine plan.md to understand the current step (read-only, do not modify)
2. **Identify failure points** - What specific issues caused the validation to fail
3. **Analyze root causes** - Why did the execution not meet the success criteria
4. **Generate refined task** - Create an improved task description for retry (for use in conversation, not plan update)
5. **Document failure insights** - Write to learnings/failure_analysis.md and learnings/step_X_learning.md
6. **DO NOT update plan.md** - Plan updates are handled separately by other agents

### **Root Cause Analysis:**
Categorize the failure and identify root cause:

**Failure Categories**:
1. **Tool Selection Failure**: Wrong tool chosen for the task
2. **Approach Failure**: Right tool, wrong usage or parameters
3. **Assumption Failure**: Incorrect assumptions about system state
4. **Environment Failure**: External factors (permissions, network, dependencies)

**Analysis Template**:

## Root Cause Analysis:
- **Failure Type**: [One of the categories above]
- **Primary Cause**: [Direct cause of failure]
- **Contributing Factors**: [What made it worse]
- **Prevention Strategy**: [How to avoid this]
- **Alternative Approach**: [What to try instead]

### **Learning Documentation Focus:**
Document **learnings from the failure** in learnings files (do NOT update plan.md):

**Example Enhanced Step After Failure:**
` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `- **Description**: Deploy using kubectl apply to production
- **Success Criteria**: All pods Running, rollout successful, no error events
- **Why This Step**: Previous failure showed namespace validation is critical. Timeout prevents hanging.
- **Failure Patterns**:
  - Don't use docker.docker_run with {"image":"myapp","command":["start"]} (use kubectl_apply)
  - Don't skip kubectl_get with {"resource":"namespace"} validation (caused error)
  - Always use kubectl_apply with {"dry_run":"client"} first`
		}
		return `- **Description**: Deploy using kubectl apply to production
- **Success Criteria**: All pods Running, rollout successful, no error events
- **Why This Step**: Previous failure showed namespace validation is critical. Timeout prevents hanging.
- **Failure Patterns**:
  - Don't use container runtime tools directly (use orchestration tools)
  - Don't skip namespace validation (caused deployment error)
  - Always use dry-run check before applying changes`
	}() + `

**Enhancement Guidelines:**
- ONLY add Failure Patterns if specific failures identified
- Explain what went wrong and why new approach should work
- Integrate ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `alternative tools with exact arguments`
		}
		return `alternative approaches and strategies`
	}() + ` into description

### **Available Tools:**
You have access to all MCP tools to examine workspace files and gather additional context.

## 📝 **REQUIRED OUTPUT FORMAT**

Provide your response in this exact format:

## Refined Task Description

### Refined Task:
[Clear, actionable task description that incorporates learnings from execution and validation results - for immediate retry if validation failed]

### Key Changes:
- [Specific improvement 1 based on learnings]
- [Specific improvement 2 based on learnings]  
- [What to avoid based on failures]

### Learning Analysis:
[Concise analysis of what worked, what failed, and key insights for future execution]

---

## Learning Documentation Actions

### Learnings Documented:
- [Failure patterns captured: ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `alternative tools with exact arguments, error explanations, and what to avoid based on failure`
		}
		return `alternative approaches, error explanations, and what to avoid based on failure`
	}() + `]
- [Validation checks that would have caught the error]
- [Failure analysis insights and why new approach should work]
- [Missing dependencies that caused failure]
- [Failure Patterns documented ONLY if ` + func() string {
		if templateVars["LearningDetailLevel"] == "exact" {
			return `specific tools/approaches that failed were identified - include MCP server.tool references with exact arguments and failure reasons`
		}
		return `clear patterns or approaches that failed were identified - include high-level descriptions of what didn't work and why`
	}() + `]

**NOTE**: Document learnings in learnings/ folder files - do NOT update plan.md file

### Execution Insights Captured:
- [Successful tools and approaches that worked well]
- [Patterns and best practices discovered]
- [Context dependencies that were missing or incorrect]

---

## 📁 **FILE PERMISSIONS (Failure Learning Agent)**

**READ:**
- planning/plan.md (current markdown plan) - path: ` + templateVars["WorkspacePath"] + `/todo_creation_human/planning/plan.md
- validation/step_X_validation_report.md (validation results with execution summary) - path: ` + templateVars["WorkspacePath"] + `/todo_creation_human/validation/step_X_validation_report.md

**WRITE:**
- learnings/failure_analysis.md (append failure patterns and anti-patterns) - path: ` + templateVars["WorkspacePath"] + `/todo_creation_human/learnings/failure_analysis.md
- learnings/step_X_learning.md (create detailed failure analysis for this step) - path: ` + templateVars["WorkspacePath"] + `/todo_creation_human/learnings/step_X_learning.md

**RESTRICTIONS:**
- Learning outputs go to learnings/ folder ONLY
- **DO NOT** update or modify planning/plan.md (plan updates are handled separately)
- **DO NOT** read or write files in execution/ folder (execution agent handles those)
- Read execution details from validation reports (which contain execution conversation)
- Focus on failure analysis and retry guidance
- All file paths must be relative to ` + templateVars["WorkspacePath"] + `/todo_creation_human/

---

**Key Requirements:**
- Analyze failure and provide refined task for immediate retry
- **DO NOT** update planning/plan.md (plan updates are handled separately)
- Document learnings ONLY in learnings/ folder
- ONLY add Failure Patterns if meaningful failures identified
- Focus on documenting what failed and what to avoid for future reference
`
}
