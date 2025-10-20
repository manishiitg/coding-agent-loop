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

// TodoPlannerCritiqueTemplate holds template variables for todo planning critique prompts
type TodoPlannerCritiqueTemplate struct {
	Objective         string
	InputData         string // The execution/validation data to critique
	InputPrompt       string // The prompt used to generate the data
	RefinementHistory string
	Iteration         int
	WorkspacePath     string // Added for todo_creation context
}

// TodoPlannerCritiqueAgent extends BaseOrchestratorAgent with todo planning critique functionality
type TodoPlannerCritiqueAgent struct {
	*agents.BaseOrchestratorAgent // ✅ REUSE: All base functionality
}

// NewTodoPlannerCritiqueAgent creates a new todo planner critique agent
func NewTodoPlannerCritiqueAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoPlannerCritiqueAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerCritiqueAgentType, // 🆕 NEW: Agent type
		eventBridge,
	)

	return &TodoPlannerCritiqueAgent{
		BaseOrchestratorAgent: baseAgent, // ✅ REUSE: All base functionality
	}
}

// Execute implements the OrchestratorAgent interface
func (tpca *TodoPlannerCritiqueAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract required parameters
	objective, ok := templateVars["objective"]
	if !ok {
		objective = "No objective provided"
	}

	workspacePath, ok := templateVars["workspace_path"]
	if !ok {
		workspacePath = "No workspace path provided"
	}

	iteration := 1
	if iterStr, ok := templateVars["iteration"]; ok {
		var iter int
		if _, err := fmt.Sscanf(iterStr, "%d", &iter); err == nil {
			iteration = iter
		}
	}

	// Prepare template variables
	critiqueTemplateVars := map[string]string{
		"objective":      objective,
		"iteration":      fmt.Sprintf("%d", iteration),
		"workspace_path": workspacePath,
	}

	// Execute using input processor
	return tpca.ExecuteWithInputProcessor(ctx, critiqueTemplateVars, tpca.todoPlannerCritiqueInputProcessor, conversationHistory)
}

// todoPlannerCritiqueInputProcessor processes inputs specifically for todo planning critique
func (tpca *TodoPlannerCritiqueAgent) todoPlannerCritiqueInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoPlannerCritiqueTemplate{
		Objective:         templateVars["objective"],
		InputData:         templateVars["input_data"],
		InputPrompt:       templateVars["input_prompt"],
		RefinementHistory: templateVars["refinement_history"],
		WorkspacePath:     templateVars["workspace_path"],
		Iteration: func() int {
			var iter int
			if _, err := fmt.Sscanf(templateVars["iteration"], "%d", &iter); err == nil {
				return iter
			}
			return 1
		}(),
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - EVALUATE FINAL TODO LIST SYNTHESIS

**OBJECTIVE**: {{.Objective}}
**ITERATION**: {{.Iteration}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Critique Agent
- **Responsibility**: Evaluate todo list synthesis quality with quantitative scoring
- **Mode**: Analytical (verify synthesis completeness, provide stop signal)

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation/todo.md (final todo to critique)
- {{.WorkspacePath}}/todo_creation/iteration_analysis.md (verify synthesis)
- {{.WorkspacePath}}/todo_creation/planning/plan.md (count iterations)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (verify best methods)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (evidence quality)

**WRITE:**
- Returns critique report directly (no file writes)

**RESTRICTIONS:**
- READ-ONLY agent - does not modify any files
- Must provide quantitative scores and clear STOP_ITERATIONS signal

## 🔍 QUANTITATIVE EVALUATION PROCESS

**1. Count Iteration References:**
- Read todo.md and count references to "Iteration X"
- Minimum required: At least {{.Iteration}} iterations should be referenced
- Use regex_search to count: search for "Iteration [0-9]+" pattern

**2. Verify Best Method Selection:**
- Read execution_results.md to see success rates
- Check if todo.md uses methods with highest success rates
- Cross-reference with validation reports

**3. Calculate Scores (/100):**
- Synthesis Completeness: 40 points
- Method Selection: 30 points
- Actionability: 30 points

` + GetTodoCreationMemoryRequirements() + `

## 📤 Output Format

# Critique Report - Iteration {{.Iteration}}

## 📊 QUANTITATIVE SCORES

**TOTAL SCORE: [X]/100**

### 1. Synthesis Completeness: [X]/40
- **Iteration References**: Found [N] / {{.Iteration}} iterations in todo.md
  - 40pts: All {{.Iteration}} iterations referenced
  - 30pts: 70%+ iterations referenced
  - 20pts: 50%+ iterations referenced
  - 0pts: <50% iterations referenced
- **Actual Score**: [X]/40

### 2. Method Selection: [X]/30
- **Best Method Used**: [Yes/No] - Uses method with highest success rate?
- **Evidence**: Success rate from execution_results.md = [%]
  - 30pts: Uses method with highest success rate + validation
  - 15pts: Uses method from recent iterations only
  - 0pts: Uses unvalidated methods
- **Actual Score**: [X]/30

### 3. Actionability: [X]/30
- **Steps with Complete Fields**: [N] / [Total]
  - 30pts: All steps have: What, How, Success, Dependencies
  - 15pts: Most steps (70%+) have required fields
  - 0pts: Many steps missing critical fields
- **Actual Score**: [X]/30

---

## 🔍 KEY FINDINGS

### Iteration References Found
- Counted [N] iteration references in todo.md
- Expected: At least {{.Iteration}} references
- Status: [PASS/FAIL]

### Method Selection
- Best method from execution_results.md: [Method name, success rate %]
- Method used in todo.md: [Method name]
- Match: [Yes/No]

### Critical Issues
1. [Most critical issue if score < 80]
2. [Second issue]

---

## 💡 RECOMMENDATIONS

**If score >= 80:** Todo list is production-ready.
**If score < 80:** Address these issues before proceeding:
- [Specific improvement needed]
- [Second improvement]

---

## 🚦 STOP SIGNAL

**STOP_ITERATIONS**: [Yes/No]
**Reason**: [One sentence - why stop or continue]

**If Yes (score >= 80):** Todo list synthesis is complete and production-ready.
**If No (score < 80):** [What needs improvement in next iteration]

---

Focus on quantitative verification and clear stop/continue signal.`

	// Parse and execute the template
	tmpl, err := template.New("todoPlannerCritique").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateData)
	if err != nil {
		return fmt.Sprintf("Error executing template: %v", err)
	}

	return result.String()
}
