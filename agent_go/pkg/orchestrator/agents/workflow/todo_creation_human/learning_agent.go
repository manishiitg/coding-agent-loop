package todo_creation_human

import (
	"context"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
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
func (agent *HumanControlledTodoPlannerFailureLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
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

## 📊 **EXECUTION RESULTS**
` + templateVars["ExecutionHistory"] + `

## ✅ **VALIDATION RESULTS**
` + templateVars["ValidationResult"] + `

## 🧠 **YOUR TASK - FAILURE ANALYSIS**

This step execution failed validation. Analyze what went wrong and provide a refined task description for immediate retry.

### **Failure Analysis Process:**
1. **Read current plan** - Examine plan.md to understand the current step
2. **Identify failure points** - What specific issues caused the validation to fail
3. **Analyze root causes** - Why did the execution not meet the success criteria
4. **Generate refined task** - Create an improved task description for retry
5. **Document failure insights** - Write to learnings/failure_analysis.md and learnings/step_X_learning.md

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

### **Plan Improvement Focus:**
Update plan.md with **learnings from the failure** by **enhancing the markdown content**:

**Example of Enhanced Step After Failure Analysis:**

### Step 1: Deploy service
- **Description**: Deploy using kubectl apply to production
- **Success Criteria**: Service is running with all pods healthy (kubectl get pods shows 'Running' status), deployment rolled out successfully (kubectl rollout status returns 'successfully rolled out'), and no error events (kubectl get events shows no errors in last 5m)
- **Why This Step**: This step deploys the application. Previous failure showed that namespace validation is critical before deployment. The timeout on rollout status prevents hanging indefinitely.
- **Context Dependencies**: ../validation/environment_check.md, ../execution/step_1_config.md, ../validation/namespace_verification.md
- **Context Output**: ./execution/step_2_deployment.md
- **Failure Patterns**:
  - Don't use docker.docker_run directly (use kubectl instead - previous failure)
  - Don't skip namespace validation (caused deployment error)
  - Don't apply without dry-run check (YAML syntax errors not caught)
  - Don't skip timeout on rollout status (prevents hanging indefinitely)

**How to Enhance Markdown Plan Based on Failures:**
1. **Description**: Keep concise, focus on core task
2. **Success Criteria**: Add validation checks that would have caught the error, expected outputs with specific values
3. **Why This Step**: Explain what went wrong in the previous attempt and why the new approach should work
4. **Context Dependencies**: Add any missing dependencies that caused the failure
5. **Failure Patterns**: ONLY add this section if you identified specific tools, approaches, or patterns that failed. Include specific MCP server.tool references and exact reasons why they failed.

**Refinement Focus:**
- **Specific Tool Recommendations**: Suggest alternative tools if original failed (integrate into description)
- **Detailed Error Context**: Explain what error occurred and why (integrate into description and why_this_step)
- **Step-by-Step Alternatives**: Provide refined approach with clear alternatives (integrate into description)

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

## Plan Improvement Actions

### Plan Updates Made:
- [Enhanced markdown step descriptions with alternative tools, error explanations, and what to avoid based on failure]
- [Enhanced success criteria with validation checks that would have caught the error]
- [Enhanced why_this_step sections with failure analysis and why new approach should work]
- [Updated context dependencies with missing dependencies that caused failure]
- [Added Failure Patterns section ONLY if specific tools/approaches that failed were identified - include MCP server.tool references and failure reasons]

**NOTE**: Update plan.md file - do NOT create new files or change file structure

### Execution Insights Captured:
- [Successful tools and approaches that worked well]
- [Patterns and best practices discovered]
- [Context dependencies that were missing or incorrect]

---

## 📁 **FILE PERMISSIONS (Failure Learning Agent)**

**READ:**
- planning/plan.md (current markdown plan)
- validation/step_X_validation_report.md (validation results with execution summary)

**WRITE:**
- learnings/failure_analysis.md (append failure patterns and anti-patterns)
- learnings/step_X_learning.md (create detailed failure analysis for this step)
- planning/plan.md (update with improvements based on failure analysis)

**RESTRICTIONS:**
- Learning outputs go to learnings/ folder
- Plan improvements go to planning/plan.md
- Read execution details from validation reports (which contain execution conversation)
- Focus on failure analysis and retry guidance

---

**Important**: 
1. **Focus on failure**: Analyze what went wrong and why validation failed
2. **Provide refined task**: Generate improved task description for immediate retry
3. **Update plan.md**: Improve the markdown plan by enhancing step descriptions, success criteria, and context dependencies
4. **Markdown format**: Update the markdown plan.md file - do NOT create JSON files
5. **Document in learnings/**: Write failure patterns to learnings/failure_analysis.md and step details to learnings/step_X_learning.md
6. **Prevent repetition**: Integrate failure analysis and alternatives directly into the markdown step descriptions
7. **Failure Patterns Section**: ONLY add "- **Failure Patterns**:" section if you identified specific MCP tools, exact commands, or clear patterns that failed. Do NOT add empty or generic patterns.
`
}
