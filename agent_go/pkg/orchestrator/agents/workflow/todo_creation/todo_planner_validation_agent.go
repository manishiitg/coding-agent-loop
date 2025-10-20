package todo_creation

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

	"github.com/tmc/langchaingo/llms"
)

// TodoPlannerValidationTemplate holds template variables for validation prompts
type TodoPlannerValidationTemplate struct {
	ExecutionResult string
	WorkspacePath   string
}

// TodoPlannerValidationAgent validates execution results and assesses quality
type TodoPlannerValidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoPlannerValidationAgent creates a new todo planner validation agent
func NewTodoPlannerValidationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoPlannerValidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerValidationAgentType,
		eventBridge,
	)

	return &TodoPlannerValidationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (tpva *TodoPlannerValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract execution result and workspace path from template variables
	// Validation agent is tactical - just validates execution results with evidence
	executionResult := templateVars["ExecutionResult"]
	workspacePath := templateVars["WorkspacePath"]

	// Prepare template variables
	validationTemplateVars := map[string]string{
		"ExecutionResult": executionResult,
		"WorkspacePath":   workspacePath,
	}

	// Execute using input processor
	return tpva.ExecuteWithInputProcessor(ctx, validationTemplateVars, tpva.validationInputProcessor, conversationHistory)
}

// validationInputProcessor processes inputs specifically for execution validation
func (tpva *TodoPlannerValidationAgent) validationInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoPlannerValidationTemplate{
		ExecutionResult: templateVars["ExecutionResult"],
		WorkspacePath:   templateVars["WorkspacePath"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - VALIDATE EXECUTION RESULTS

**EXECUTION RESULT**: {{.ExecutionResult}}
**WORKSPACE**: {{.WorkspacePath}}

**CORE TASK**: Validate execution results with evidence. You are the validation agent - verify that execution claims are backed by evidence.

**⚠️ IMPORTANT**: Validate ONLY what was executed. Don't validate against plans or objectives - just verify the execution claims with evidence.

## 📋 Validation Guidelines
- **Read Previous Work**: Check {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md for context
- **Validate Execution Claims**: Check that execution claims are backed by evidence
- **Verify Claims**: Cross-reference claims with evidence files
- **Detect Tool Failures**: Identify claims that might be based on tool failures
- **Evidence for Critical**: Require evidence only for critical steps (magic numbers, assumptions, failures)
- **Append Validation**: Add your validation results to validation/execution_validation_report.md
- **Stay Focused**: Validate execution results only
- **Build on Memory**: Reference previous validations if relevant

## 💾 Workspace Updates
Create/update file in {{.WorkspacePath}}/todo_creation/validation/:
- **execution_validation_report.md**: Comprehensive validation report

**⚠️ IMPORTANT**: Only modify files within {{.WorkspacePath}}/todo_creation/

## 🔍 Evidence Verification Process

### Use Execution Results ({{.ExecutionResult}}):
- Analyze claims for immediate context
- Review MCP server/tool/arguments claimed to work
- Review steps claimed as completed

### Read Execution Files for Evidence:
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md
- {{.WorkspacePath}}/todo_creation/execution/evidence/

### Verification Steps:
1. Cross-reference claims with evidence files
2. Verify tool calls with MCP server/tool/arguments
3. Validate completion with evidence file existence

` + memory.GetWorkflowMemoryRequirements() + `

## 📤 Output Format

**APPEND to** {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md

---

## 🔍 Validation Report
**Date**: [Current date/time]
**Steps Validated**: [Number]

### Previous Validation Summary
[Briefly summarize from validation report if it exists]
- Previous validation: [Results, issues found]
- Key concerns: [Important findings]

### Current Validation

#### Step 1: [Step Name]
- **Claim**: [What execution claimed]
- **Evidence Found**: [Evidence file/result]
- **Verification Method**: [How verified]
- **Confidence**: [High/Medium/Low]
- **Status**: [VALIDATED/REJECTED/QUESTIONABLE]
- **Notes**: [Any concerns or confirmations]

#### Step 2: [Step Name - if validated]
- **Claim**: [What was claimed]
- **Evidence**: [Evidence found]
- **Status**: [VALIDATED/REJECTED]

### Validation Summary
- **Steps Validated**: [Number confirmed]
- **Steps Rejected**: [Number insufficient evidence]
- **Evidence Quality**: [High/Medium/Low]
- **Key Issues**: [Main validation concerns]

### Cumulative Statistics
- **Total Steps Validated**: [From all validation runs]
- **Total Steps Rejected**: [From all validation runs]
- **Overall Reliability**: [High/Medium/Low]

---

## ⚠️ Critical Assumptions
[Evidence-backed assumptions needing double-check:]

- **Assumption**: [What was assumed]
- **Evidence**: [Supporting evidence]
- **Criticality**: [HIGH/MEDIUM]
- **Impact If Wrong**: [Potential consequences]
- **Double-Check Result**: [Verification outcome]
- **Status**: [CONFIRMED/QUESTIONABLE/INVALIDATED]

## ⚡ Tool Failure Risks
[Assumptions that might be based on tool failures:]

- **Assumption**: [What was assumed]
- **Tool Used**: [MCP tool]
- **Failure Risk**: [HIGH/MEDIUM/LOW]
- **Backup Verification**: [Alternative method used]
- **Status**: [RELIABLE/QUESTIONABLE/UNRELIABLE]

## ❌ Unsupported Claims
[Claims lacking evidence:]

- **Claim**: [What was claimed]
- **Missing Evidence**: [What's needed]
- **Impact**: [Effect on plan validity]
- **Recommendation**: [How to address]

## 🔍 Evidence Quality

### Strong Evidence
- **Step**: [Name]
- **Evidence Files**: [List]
- **Quality**: [High/Medium/Low]
- **Verification**: [Method]

### Weak Evidence
- **Step**: [Name]
- **Issues**: [Problems with evidence]
- **Missing**: [Additional evidence needed]
- **Recommendation**: [How to improve]

## 🚨 Hallucination Detection

### Suspected Fabrications
- **Claim**: [What was claimed]
- **Suspicion Reason**: [Why suspected]
- **Verification Attempt**: [What was tried]
- **Status**: [CONFIRMED/LIKELY/INCONCLUSIVE]

### Fact-Checking Results
- **Claim**: [What was claimed]
- **Tool Used**: [MCP tool for verification]
- **Result**: [What was found]
- **Accuracy**: [ACCURATE/INACCURATE/INCONCLUSIVE]

## 🔧 Technical Validation

### Command Verification
- **Command**: [Command executed]
- **Expected**: [Expected output]
- **Actual**: [Actual output]
- **Status**: [VERIFIED/FAILED/INCONCLUSIVE]

### Web Scraping Validation
- **URL**: [URL accessed]
- **Selectors**: [Selectors used]
- **Data Extracted**: [Data claimed]
- **Evidence**: [Screenshots/files]
- **Status**: [VERIFIED/UNVERIFIED/FAILED]

## ✅ Completed Steps (Validated Only)
[For validated steps only:]

- **Step**: [Name]
- **Completion Date**: [Date]
- **Evidence Files**: [References]
- **Verification**: [Method]
- **Quality**: [High/Medium/Low]
- **Status**: COMPLETED - VALIDATED

## 🚫 Rejected Steps
[Cannot mark as completed:]

- **Step**: [Name]
- **Evidence Issues**: [Problems]
- **Required Evidence**: [What's needed]
- **Status**: REJECTED - INSUFFICIENT EVIDENCE

## 🎯 Critical Issues
1. [Most critical issue with evidence]
2. [Second most critical]
3. [Third most critical]

## ⚠️ Risk Assessment

### Detrimental Assumptions
- **Assumption**: [Critical assumption]
- **Impact**: [How it could derail objective]
- **Evidence Quality**: [Quality level]
- **Verification**: [Status]
- **Risk Level**: [HIGH/MEDIUM/LOW]
- **Mitigation**: [How to address]

### Tool Failure Impact
- **Assumption**: [Tool-dependent assumption]
- **Tool Dependency**: [Which tools]
- **Failure Scenarios**: [What could go wrong]
- **Impact**: [Effect on objective]
- **Mitigation**: [Prevention strategy]

## 💡 Recommendations
- [Address unsupported claims]
- [Improve evidence collection]
- [Verify assumptions before making them]
- [Prevent hallucinations in future iterations]

Focus on evidence-backed validation for reliable plan refinement.`

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
