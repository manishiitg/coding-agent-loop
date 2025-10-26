package todo_creation_human

import (
	"context"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// HumanControlledTodoPlannerSuccessLearningTemplate holds template variables for success learning prompts
type HumanControlledTodoPlannerSuccessLearningTemplate struct {
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
func (agent *HumanControlledTodoPlannerSuccessLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
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
	successLearningTemplateVars := map[string]string{
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

	// Create template data for success learning
	templateData := HumanControlledTodoPlannerSuccessLearningTemplate{
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
	return agent.ExecuteWithTemplateValidation(ctx, successLearningTemplateVars, agent.successLearningInputProcessor, conversationHistory, templateData)
}

// successLearningInputProcessor creates the success learning analysis prompt
func (agent *HumanControlledTodoPlannerSuccessLearningAgent) successLearningInputProcessor(templateVars map[string]string) string {
	return `# Success Learning Analysis Agent

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

## ✅ **SUCCESSFUL EXECUTION RESULTS**
` + templateVars["ExecutionHistory"] + `

## ✅ **VALIDATION RESULTS (SUCCESS)**
` + templateVars["ValidationResult"] + `

## 🧠 **YOUR TASK - SUCCESS ANALYSIS**

This step was executed successfully! Analyze what made it work well and improve the plan.md file with these learnings.

### **Success Analysis Process:**
1. **Read current plan** - Examine plan.md to understand the current step
2. **Identify success factors** - What tools, approaches, and patterns worked best
3. **Extract best practices** - Successful strategies that should be documented
4. **Update plan step** - Improve the step description, success criteria, and context dependencies based on what actually worked
5. **Write improved plan** - Update plan.md with better step details
6. **Document success patterns** - Write to learnings/success_patterns.md and learnings/step_X_learning.md

### **Plan Improvement Focus:**
Update plan.md with the **final working approach** that achieved success by **enhancing the markdown content**:

**Example of Enhanced Step in Plan.md:**

### Step 1: Deploy service
- **Description**: Deploy using kubectl apply to production
- **Success Criteria**: Service is running with all pods healthy (kubectl get pods shows 'Running' status for all pods), deployment rolled out successfully (kubectl rollout status returns 'successfully rolled out'), and endpoint is accessible (curl to service endpoint returns 200 OK response)
- **Why This Step**: This step deploys the application to production. The dry-run validation is critical because it catches YAML syntax errors before applying. The rollout status check ensures the deployment progressed without errors. Pod health verification confirms the service is actually running.
- **Context Dependencies**: ../validation/environment_check.md, ../execution/step_1_config.md
- **Context Output**: ./execution/step_2_deployment.md
- **Success Patterns**:
  - Use kubernetes.kubectl_apply with --dry-run=client first to validate YAML syntax
  - Monitor rollout with kubernetes.kubectl_rollout status command
  - Verify pods with kubernetes.kubectl_get pods -n production
  - Always check namespace exists before applying

**How to Enhance Markdown Plan:**
1. **Description**: Keep concise, focus on core task
2. **Success Criteria**: Add exact validation methods, expected outputs, and measurable indicators
3. **Why This Step**: Explain why this specific approach worked and why each sub-step is important
4. **Context Dependencies**: Update with actual files that were crucial for successful execution
5. **Success Patterns**: ONLY add this section if you identified specific tools, approaches, or patterns that led to success. Include specific MCP server.tool references and exact approaches that worked.

### **Available Tools:**
You have access to all MCP tools to examine workspace files and gather additional context.

## 📝 **REQUIRED OUTPUT FORMAT**

Provide your response in this exact format:

## Success Analysis Summary

### What Worked Well:
- [Specific tool or approach that was successful]
- [Pattern or strategy that led to success]
- [Key factor that made this execution successful]

### Success Factors Identified:
- [Tool that worked best for this type of task]
- [Approach that was most effective]
- [Context or dependency that was crucial]

### Best Practices Captured:
- [Successful pattern that should be repeated]
- [Tool combination that worked well]
- [Strategy that led to efficient execution]

---

## Plan Improvement Actions

### Plan Updates Made:
- [Enhanced markdown step descriptions with specific tools, commands, and step-by-step approach that worked]
- [Enhanced success criteria with exact validation methods and expected outputs]
- [Enhanced why_this_step sections with insights about why this approach worked best]
- [Updated context dependencies with actual files that were crucial for success]
- [Added Success Patterns section ONLY if specific tools/approaches that led to success were identified - include MCP server.tool references]

**NOTE**: Update plan.md file - do NOT create new files or change file structure

### Success Patterns Documented:
- [Successful tools and approaches that worked well]
- [Patterns and best practices discovered]
- [Context dependencies that were crucial for success]
- [Tool recommendations for future similar steps]

---

## 📁 **FILE PERMISSIONS (Success Learning Agent)**

**READ:**
- planning/plan.md (current markdown plan)
- validation/step_X_validation_report.md (validation results with execution summary)

**WRITE:**
- learnings/success_patterns.md (append cumulative success patterns)
- learnings/step_X_learning.md (create detailed learning for this step)
- planning/plan.md (update with improvements based on what worked)

**RESTRICTIONS:**
- Learning outputs go to learnings/ folder
- Plan improvements go to planning/plan.md
- Read execution details from validation reports (which contain execution conversation)
- Focus on capturing success patterns and best practices

---

**Important**: 
1. **Focus on success**: Analyze what made this execution successful
2. **Update plan.md**: Improve the markdown plan by enhancing step descriptions, success criteria, and context dependencies
3. **Markdown format**: Update the markdown plan.md file - do NOT create JSON files
4. **Document in learnings/**: Write success patterns to learnings/success_patterns.md and step details to learnings/step_X_learning.md
5. **Tool recommendations**: Integrate tool information directly into the markdown step descriptions
6. **Success Patterns Section**: ONLY add "- **Success Patterns**:" section if you identified specific MCP tools, exact commands, or clear patterns that led to success. Do NOT add empty or generic patterns.
`
}
