package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// HumanControlledTodoPlannerValidationTemplate holds template variables for validation prompts
type HumanControlledTodoPlannerValidationTemplate struct {
	StepNumber              string
	TotalSteps              string
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepWhyThisStep         string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
}

// HumanControlledTodoPlannerValidationAgent validates if tasks were completed properly
type HumanControlledTodoPlannerValidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerValidationAgent creates a new human-controlled todo planner validation agent
func NewHumanControlledTodoPlannerValidationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerValidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerValidationAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerValidationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpva *HumanControlledTodoPlannerValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract variables from template variables
	stepNumber := templateVars["StepNumber"]
	totalSteps := templateVars["TotalSteps"]
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepWhyThisStep := templateVars["StepWhyThisStep"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	stepContextOutput := templateVars["StepContextOutput"]
	workspacePath := templateVars["WorkspacePath"]
	executionHistory := templateVars["ExecutionHistory"]

	// Prepare template variables
	validationTemplateVars := map[string]string{
		"StepNumber":              stepNumber,
		"TotalSteps":              totalSteps,
		"StepTitle":               stepTitle,
		"StepDescription":         stepDescription,
		"StepSuccessCriteria":     stepSuccessCriteria,
		"StepWhyThisStep":         stepWhyThisStep,
		"StepContextDependencies": stepContextDependencies,
		"StepContextOutput":       stepContextOutput,
		"WorkspacePath":           workspacePath,
		"ExecutionHistory":        executionHistory,
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerValidationTemplate{
		StepNumber:              stepNumber,
		TotalSteps:              totalSteps,
		StepTitle:               stepTitle,
		StepDescription:         stepDescription,
		StepSuccessCriteria:     stepSuccessCriteria,
		StepWhyThisStep:         stepWhyThisStep,
		StepContextDependencies: stepContextDependencies,
		StepContextOutput:       stepContextOutput,
		WorkspacePath:           workspacePath,
		ExecutionHistory:        executionHistory,
	}

	// Execute using template validation
	return hctpva.ExecuteWithTemplateValidation(ctx, validationTemplateVars, hctpva.humanControlledValidationInputProcessor, conversationHistory, templateData)
}

// humanControlledValidationInputProcessor processes inputs specifically for task completion validation
func (hctpva *HumanControlledTodoPlannerValidationAgent) humanControlledValidationInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerValidationTemplate{
		StepNumber:              templateVars["StepNumber"],
		TotalSteps:              templateVars["TotalSteps"],
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepWhyThisStep:         templateVars["StepWhyThisStep"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		ExecutionHistory:        templateVars["ExecutionHistory"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - VALIDATE STEP {{.StepNumber}} EXECUTION

**STEP**: {{.StepNumber}}/{{.TotalSteps}} - {{.StepTitle}}
**STEP DESCRIPTION**: {{.StepDescription}}
**WORKSPACE**: {{.WorkspacePath}}

### 📋 Complete Step Information
**Success Criteria**: {{.StepSuccessCriteria}}
**Why This Step**: {{.StepWhyThisStep}}
**Context Dependencies**: {{.StepContextDependencies}}
**Context Output**: {{.StepContextOutput}}

### 🔍 Step Context Analysis
**Success Criteria**: Use the success criteria above to verify completion
**Why This Step**: The why this step field explains how this contributes to the objective
**Context Dependencies**: Check if context dependencies files were properly read
**Context Output**: Verify if the context output file was created as specified

**EXECUTION CONVERSATION HISTORY TO VALIDATE**:
{{.ExecutionHistory}}

## 🤖 AGENT IDENTITY
- **Role**: Validation Agent
- **Responsibility**: Verify if step {{.StepNumber}} success criteria was met and execution was completed properly
- **Mode**: Success criteria verification with execution output analysis

## 📁 FILE PERMISSIONS
**READ:**
- **ANALYZE** execution output for evidence of file operations and tool usage
- **VERIFY** any files mentioned in the execution output

**WRITE:**
- **CREATE** {{.WorkspacePath}}/validation/step_{{.StepNumber}}_validation_report.md (step validation report)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/
- Focus on step {{.StepNumber}} execution output analysis - verify claims from execution agent
- Simple validation approach for human-controlled mode
- **Receives step {{.StepNumber}} execution output as input** - validates execution claims

**EXECUTION CONVERSATION HISTORY TO VALIDATE**:
{{.ExecutionHistory}}


## 🔍 VALIDATION PROCESS

**Step {{.StepNumber}}/{{.TotalSteps}} - "{{.StepTitle}}"**

**Your Task**: Validate if the execution conversation history shows that step {{.StepNumber}} was completed successfully by checking if the SUCCESS CRITERIA was met.

**Primary Validation Focus**: **SUCCESS CRITERIA VERIFICATION**
**Success Criteria**: {{.StepSuccessCriteria}}

**Validation Approach**:
1. **Analyze Execution Conversation**: Review the execution conversation history for evidence of successful completion
2. **Verify Success Criteria**: Check if the success criteria "{{.StepSuccessCriteria}}" was actually met based on execution evidence
3. **Tool Usage Analysis**: Analyze what tools were used and their results from the conversation
4. **File Operations Analysis**: Check if files were created/updated as claimed in the conversation
5. **Task Completion**: Ensure the step objective was achieved based on execution results

**Validation Criteria**:
- **PRIMARY**: Success criteria "{{.StepSuccessCriteria}}" must be met based on execution evidence
- Execution conversation must show successful tool usage
- File operations mentioned in execution must be verifiable
- Step {{.StepNumber}} objective must be achieved based on execution results
- No critical errors or failures in execution conversation

**Validation Decision**:
- ✅ **PASS**: If success criteria is met and execution evidence supports completion
- ❌ **FAIL**: If success criteria is not met or execution evidence is insufficient

## 📤 Output Format

**CREATE** {{.WorkspacePath}}/validation/step_{{.StepNumber}}_validation_report.md

---

## 📋 Step {{.StepNumber}} Execution Validation Report
**Date**: [Current date/time]
**Step**: {{.StepNumber}}/{{.TotalSteps}}

### 🎯 Overall Assessment
- **Success Criteria Met**: [✅ YES / ❌ NO] - "{{.StepSuccessCriteria}}"
- **Execution Status**: [COMPLETED/PARTIAL/FAILED/INCOMPLETE]
- **Evidence Quality**: [STRONG/MODERATE/WEAK/INSUFFICIENT]
- **Execution Quality**: [HIGH/MEDIUM/LOW]

### 📝 Step Analysis

#### Success Criteria Verification
- **Success Criteria**: "{{.StepSuccessCriteria}}"
- **Evidence Found**: [Specific evidence from execution conversation that success criteria was met]
- **Verification Method**: [How you verified the success criteria based on execution evidence]
- **Verification Result**: [✅ CONFIRMED / ❌ NOT MET / ⚠️ PARTIAL]

#### Execution Conversation Analysis
- **Tools Used**: [List of tools used by execution agent from conversation]
- **Tool Results**: [Results and outputs from tool usage in conversation]
- **File Operations**: [Files created/updated based on execution conversation]
- **Key Actions**: [Main actions performed by execution agent from conversation]

### ✅ Evidence Found
1. **[Evidence Type]**: [Description] - ✅ CONFIRMED in execution conversation
2. **[Evidence Type]**: [Description] - ✅ CONFIRMED in execution conversation

### ⚠️ Partial Evidence
1. **[Evidence Type]**: [Description] - ⚠️ PARTIAL evidence found
2. **[Evidence Type]**: [Description] - ⚠️ UNCLEAR from execution conversation

### ❌ Missing Evidence
1. **[Evidence Type]**: [Description] - ❌ NO EVIDENCE found
2. **[Evidence Type]**: [Description] - ❌ INSUFFICIENT evidence

### 🔍 Execution Evidence Analysis
**Execution Conversation Summary:**
- [Summary of what the execution agent accomplished from conversation]
- [Key tools used and their results from conversation]
- [Files created or modified from conversation]

**Success Criteria Evidence:**
- [Specific evidence that supports success criteria completion]
- [Any gaps or missing evidence]

**Tool Usage Analysis:**
- [Analysis of tool usage effectiveness from conversation]
- [Results and outputs from tools in conversation]

### 📊 Evidence Summary
- **Success Criteria Evidence**: [STRONG/MODERATE/WEAK/INSUFFICIENT]
- **Tool Usage Evidence**: [COMPREHENSIVE/PARTIAL/MINIMAL/NONE]
- **File Operations Evidence**: [COMPLETE/PARTIAL/MISSING/NONE]
- **Overall Evidence Quality**: [HIGH/MEDIUM/LOW/INSUFFICIENT]

### 💡 Recommendations
**For Step {{.StepNumber}}:**
- [Specific evidence gaps that need to be addressed]
- [Missing execution evidence that should be provided]
- [Areas where execution could be improved]

**For Next Steps:**
- [How step {{.StepNumber}} execution evidence affects subsequent steps]
- [Any execution patterns that should be continued or improved]
- [Adjustments needed for future execution approaches]

---

**Note**: Focus on step {{.StepNumber}} execution conversation analysis. Check if the execution conversation provides sufficient evidence that the success criteria was met. Analyze tool usage, file operations, and execution results to verify completion. Provide clear feedback on what evidence was found versus what was needed for step {{.StepNumber}}.`

	// Parse and execute the template
	tmpl, err := template.New("validation").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing validation template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing validation template: %v", err)
	}

	return result.String()
}
