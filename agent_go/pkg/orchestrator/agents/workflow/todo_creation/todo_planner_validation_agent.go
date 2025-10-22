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

	"github.com/tmc/langchaingo/llms"
)

// TodoPlannerValidationTemplate holds template variables for validation prompts
type TodoPlannerValidationTemplate struct {
	ExecutionResult string
	WorkspacePath   string
	Iteration       string
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
func (tpva *TodoPlannerValidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract execution result and workspace path from template variables
	// Validation agent is tactical - just validates execution results with evidence
	executionResult := templateVars["ExecutionResult"]
	workspacePath := templateVars["WorkspacePath"]

	// Prepare template variables
	validationTemplateVars := map[string]string{
		"ExecutionResult": executionResult,
		"WorkspacePath":   workspacePath,
		"Iteration":       templateVars["Iteration"],
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
		Iteration:       templateVars["Iteration"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - VALIDATE EXECUTION RESULTS

**EXECUTION RESULT**: {{.ExecutionResult}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Validation Agent
- **Responsibility**: Verify execution claims are backed by evidence files
- **Mode**: Analytical (verify only, do not modify execution state)

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (current iteration)
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md
- {{.WorkspacePath}}/todo_creation/execution/evidence/ (verify evidence exists)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (previous validations)

**WRITE:**
- **APPEND** to {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md

**RESTRICTIONS:**
- READ-ONLY for execution files - never modify execution results
- Only modify files within {{.WorkspacePath}}/todo_creation/

## 🔍 VALIDATION PROCESS

**Confidence Levels:**
- **HIGH**: Strong evidence file exists, matches claim exactly
- **MEDIUM**: Partial evidence or indirect verification
- **LOW**: No evidence file, or claim cannot be verified

**Steps:**
1. Read {{.WorkspacePath}}/todo_creation/execution/execution_results.md
2. For each claimed step completion, check if evidence file exists in evidence/
3. Cross-reference tool outputs with claimed results
4. Assign confidence level (HIGH/MEDIUM/LOW)
5. Mark as VALIDATED (high confidence), QUESTIONABLE (medium), or REJECTED (low)

` + GetTodoCreationMemoryRequirements() + `

## 📤 Output Format

**APPEND to** {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md

---

## 🔍 Validation Report - Iteration {{.Iteration}}
**Date**: [Current date/time]

### 1️⃣ VALIDATED STEPS

#### Step: [Step Name]
- **Claim**: [What execution claimed]
- **Evidence**: [File path in evidence/]
- **Confidence**: HIGH
- **Status**: ✅ VALIDATED

[Repeat for all validated steps]

### 2️⃣ REJECTED STEPS

#### Step: [Step Name]
- **Claim**: [What execution claimed]
- **Evidence Issues**: [Missing file, or mismatch]
- **Required**: [What evidence is needed]
- **Status**: ❌ REJECTED

[Repeat for all rejected steps]

### 3️⃣ CRITICAL ISSUES

**HIGH Priority:**
1. [Most critical validation concern with evidence impact]

**MEDIUM Priority:**
2. [Secondary concern]
3. [Third concern]

**Assumptions Requiring Verification:**
- **Assumption**: [Critical assumption from execution]
- **Evidence Quality**: [LOW/MEDIUM]
- **Risk**: [Impact if wrong]

### 4️⃣ RECOMMENDATIONS

**For Next Iteration:**
- [How to improve evidence collection]
- [What claims need better support]
- [Suggested validation improvements]

**Summary:**
- Validated: [X] steps (HIGH confidence)
- Rejected: [Y] steps (insufficient evidence)
- Overall Quality: [HIGH/MEDIUM/LOW]

---

Focus on clear VALIDATED/REJECTED decisions with confidence levels.`

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
