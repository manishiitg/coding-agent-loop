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
	StepNumber      string
	TotalSteps      string
	StepTitle       string
	StepDescription string
	WorkspacePath   string
	ExecutionOutput string
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
func (hctpva *HumanControlledTodoPlannerValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract variables from template variables
	stepNumber := templateVars["StepNumber"]
	totalSteps := templateVars["TotalSteps"]
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	workspacePath := templateVars["WorkspacePath"]
	executionOutput := templateVars["ExecutionOutput"]

	// Prepare template variables
	validationTemplateVars := map[string]string{
		"StepNumber":      stepNumber,
		"TotalSteps":      totalSteps,
		"StepTitle":       stepTitle,
		"StepDescription": stepDescription,
		"WorkspacePath":   workspacePath,
		"ExecutionOutput": executionOutput,
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerValidationTemplate{
		StepNumber:      stepNumber,
		TotalSteps:      totalSteps,
		StepTitle:       stepTitle,
		StepDescription: stepDescription,
		WorkspacePath:   workspacePath,
		ExecutionOutput: executionOutput,
	}

	// Execute using template validation
	return hctpva.ExecuteWithTemplateValidation(ctx, validationTemplateVars, hctpva.humanControlledValidationInputProcessor, conversationHistory, templateData)
}

// humanControlledValidationInputProcessor processes inputs specifically for task completion validation
func (hctpva *HumanControlledTodoPlannerValidationAgent) humanControlledValidationInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerValidationTemplate{
		StepNumber:      templateVars["StepNumber"],
		TotalSteps:      templateVars["TotalSteps"],
		StepTitle:       templateVars["StepTitle"],
		StepDescription: templateVars["StepDescription"],
		WorkspacePath:   templateVars["WorkspacePath"],
		ExecutionOutput: templateVars["ExecutionOutput"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - VALIDATE STEP {{.StepNumber}} EXECUTION OUTPUT

**STEP**: {{.StepNumber}}/{{.TotalSteps}} - {{.StepTitle}}
**STEP DESCRIPTION**: {{.StepDescription}}
**WORKSPACE**: {{.WorkspacePath}}

**EXECUTION OUTPUT TO VALIDATE**:
{{.ExecutionOutput}}

## 🤖 AGENT IDENTITY
- **Role**: Validation Agent
- **Responsibility**: Check if step {{.StepNumber}} execution output shows proper task completion
- **Mode**: Simple step execution validation (no evidence verification)

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (original plan)
- {{.WorkspacePath}}/todo_creation_human/execution/step_{{.StepNumber}}_execution_results.md (step execution results)
- {{.WorkspacePath}}/todo_creation_human/execution/completed_steps.md (completed steps)
- **VERIFY** files mentioned in step {{.StepNumber}} execution output (file existence and contents)

**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_creation_human/validation/step_{{.StepNumber}}_validation_report.md (step validation report)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation_human/
- Focus on step {{.StepNumber}} file-based validation - verify files claimed by execution agent
- Simple validation approach for human-controlled mode
- **Receives step {{.StepNumber}} execution output as input** - validates file claims from execution

## 🔍 VALIDATION PROCESS

**File-Based Execution Validation:**
1. **Read Original Plan**: Review the plan to understand what was supposed to be accomplished
2. **Analyze Execution Output**: Check the provided execution output above
3. **Extract File Claims**: Identify files that execution agent claims to have created/updated
4. **Verify File Existence**: Check if the claimed files actually exist in the workspace
5. **Validate File Contents**: Read file contents to verify they match execution claims
6. **Compare with Plan**: Assess if the file changes align with the planned step
7. **Provide Feedback**: Give clear assessment of file-based execution quality

**Validation Criteria:**
- **COMPLETED**: All claimed files exist and contain expected content
- **PARTIAL**: Some files exist but content doesn't match claims
- **FAILED**: Files don't exist or execution claims are false
- **INCOMPLETE**: Files exist but are empty or incomplete

## 📤 Output Format

**CREATE** {{.WorkspacePath}}/todo_creation_human/validation/step_{{.StepNumber}}_validation_report.md

---

## 📋 Step {{.StepNumber}} Execution Validation Report
**Date**: [Current date/time]
**Step**: {{.StepNumber}}/{{.TotalSteps}}

### 🎯 Overall Assessment
- **Execution Status**: [COMPLETED/PARTIAL/FAILED/INCOMPLETE]
- **File Verification**: [ALL VERIFIED/PARTIAL VERIFICATION/VERIFICATION FAILED]
- **Execution Quality**: [HIGH/MEDIUM/LOW]

### 📝 Step Analysis

#### Original Plan Summary
- **Planned Step**: [What step {{.StepNumber}} was supposed to accomplish]
- **Expected Files**: [What files should have been created/updated for this step]

#### Execution Output Analysis
- **Files Claimed**: [Files execution agent claims to have created/updated for step {{.StepNumber}}]
- **Actions Claimed**: [What execution agent claims to have done for step {{.StepNumber}}]

### ✅ Verified Files
1. **[File Path]**: [Description] - ✅ EXISTS and matches claims
2. **[File Path]**: [Description] - ✅ EXISTS and matches claims

### ⚠️ Partially Verified Files
1. **[File Path]**: [Description] - ⚠️ EXISTS but content doesn't match claims
2. **[File Path]**: [Description] - ⚠️ EXISTS but is incomplete

### ❌ Failed File Verification
1. **[File Path]**: [Description] - ❌ FILE NOT FOUND
2. **[File Path]**: [Description] - ❌ EMPTY FILE

### 🔍 File Verification Details
**Files Checked for Step {{.StepNumber}}:**
- [List all files that were verified for this step]
- [Include verification results for each file]

**File Contents Analysis:**
- [Summary of file contents and whether they match execution claims for step {{.StepNumber}}]
- [Any discrepancies found between claims and actual files]

### 📊 Verification Summary
- **Total Files Claimed**: [Number]
- **Files Verified**: [Number] ([Percentage]%)
- **Files Partially Verified**: [Number] ([Percentage]%)
- **Files Not Found**: [Number] ([Percentage]%)
- **Files Empty**: [Number] ([Percentage]%)

### 💡 Recommendations
**For Step {{.StepNumber}}:**
- [Specific file issues that need to be addressed for this step]
- [Missing files that should be created for this step]
- [Files that need content updates for this step]

**For Next Steps:**
- [How step {{.StepNumber}} file verification affects subsequent steps]
- [Any file dependencies that need to be resolved]
- [Adjustments needed for future file operations]

---

**Note**: Focus on step {{.StepNumber}} file-based validation. Check if the execution output claims about file creation/updates are accurate by verifying file existence and contents for this specific step. Provide clear feedback on what files were actually created/updated versus what was claimed for step {{.StepNumber}}.`

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
