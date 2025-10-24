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
		agents.TodoPlannerValidationAgentType, // Use validation agent type for now
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

This step was executed successfully! Analyze what made it work well and improve the plan.json file with these learnings.

### **Success Analysis Process:**
1. **Read current plan** - Examine ` + "`plan.json`" + ` to understand the current step
2. **Identify success factors** - What tools, approaches, and patterns worked best
3. **Extract best practices** - Successful strategies that should be documented
4. **Update plan step** - Improve the step description, success criteria, and context dependencies based on what actually worked
5. **Write improved plan** - Update ` + "`plan.json`" + ` with better step details
6. **Document success patterns** - Write to ` + "`learnings/success_patterns.md`" + ` and ` + "`learnings/step_X_learning.md`" + `

### **Plan Improvement Focus:**
Update plan.json with the **final working approach** that achieved success:

**Example of Enhanced Step in Plan.json:**
` + "```" + `json
{
  "title": "Deploy service",
  "description": "Deploy using kubectl apply with --dry-run validation first. Use namespace 'production'. Verify deployment status with kubectl rollout status.",
  "success_criteria": "Service is running with all pods healthy and endpoint accessible",
  "mcp_tools_used": [
    {
      "server": "kubernetes",
      "tool": "kubectl_apply",
      "args": "--dry-run=client -f deployment.yaml"
    },
    {
      "server": "kubernetes", 
      "tool": "kubectl_rollout",
      "args": "status deployment/myapp -n production"
    }
  ],
  "approach_to_follow": "1. Validate with dry-run first, 2. Apply to production, 3. Monitor rollout status, 4. Verify endpoint health",
  "approach_to_avoid": "Don't apply directly without dry-run validation. Don't skip rollout status check. Avoid assuming success without verifying pods.",
  "context_dependencies": ["../validation/environment_check.md", "../execution/step_1_config.md"]
}
` + "```" + `

**Required Documentation**:
1. **mcp_tools_used**: List exact MCP server, tool name, and successful arguments
2. **approach_to_follow**: Step-by-step path that led to success
3. **approach_to_avoid**: What NOT to do (failed attempts, wrong tools, bad patterns)
4. **Enhanced description**: Add specific commands and validation steps that worked

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
- [Description of how the plan.json file was updated with successful execution insights]
- [Specific improvements made to step description based on what worked]
- [Enhanced success criteria based on actual successful validation]
- [Improved context dependencies based on successful execution]

### Success Patterns Documented:
- [Successful tools and approaches that worked well]
- [Patterns and best practices discovered]
- [Context dependencies that were crucial for success]
- [Tool recommendations for future similar steps]

---

## 📁 **FILE PERMISSIONS**

**READ:**
- planning/plan.json (current plan)
- validation/step_X_validation_report.md (validation results with execution summary)

**WRITE TO learnings/ FOLDER ONLY:**
- learnings/success_patterns.md (append cumulative success patterns)
- learnings/step_X_learning.md (create detailed learning for this step)

**WRITE TO planning/ FOLDER:**
- planning/plan.json (update with improvements)

**RESTRICTIONS:**
- All learning outputs MUST go to learnings/ folder
- Read execution details from validation reports (which contain execution conversation)
- Update plan.json in planning/ folder with improvements

---

**Important**: 
1. **Focus on success**: Analyze what made this execution successful
2. **Update plan.json**: Improve the step details based on successful execution learnings
3. **Document in learnings/**: Write success patterns to ` + "`learnings/success_patterns.md`" + ` and step details to ` + "`learnings/step_X_learning.md`" + `
4. **Tool recommendations**: Document exact MCP server, tool name, and arguments that worked`
}
