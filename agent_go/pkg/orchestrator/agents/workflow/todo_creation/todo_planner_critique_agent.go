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

You are a todo planner critique agent. Evaluate if the FINAL todo list properly synthesizes learnings from ALL iterations.

**OBJECTIVE**: {{.Objective}}
**ITERATION**: {{.Iteration}}
**WORKSPACE**: {{.WorkspacePath}}

## 🎯 Understanding What You're Evaluating

**IMPORTANT - Evaluation Context**:
- 🔄 **10+ Iterations** = Long discovery process with multiple approaches
- 📝 **Planning History** = Multiple "## Iteration X" sections in plan.md
- 🧪 **Execution History** = Multiple "## Iteration X" sections in execution_results.md
- ✅ **Validation History** = Validation reports across iterations
- 📄 **Todo List** = Writer synthesized BEST methods from ALL iterations
- **YOUR JOB** = Verify synthesis is comprehensive and optimal

## 📁 Read Context Files First

Read these files to understand ALL iteration history:

### Planning & Execution History
- {{.WorkspacePath}}/todo_creation/planning/plan.md (READ ALL "## Iteration X" sections)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (READ ALL "## Iteration X" sections)
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md
- {{.WorkspacePath}}/todo_creation/execution/evidence/

### Validation History & Final Todo
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (ALL validations)
- {{.WorkspacePath}}/todo_creation/todo.md (final synthesized todo)

## ❓ Core Evaluation Questions

### 1. Synthesis Quality
- Did Writer read ALL "## Iteration X" sections from plan.md?
- Did Writer read ALL "## Iteration X" sections from execution_results.md?
- Is the todo list based on learnings from ALL iterations (not just latest)?
- Were all approaches tried across iterations properly compared?

### 2. Method Selection
- Did Writer identify methods with HIGHEST success rates across iterations?
- Are selected methods backed by evidence from execution history?
- Were failed methods (from early iterations) properly excluded?
- Is the selection justified by comparing iteration results?

### 3. Objective Coverage
- Does the synthesized todo list cover ALL objective requirements?
- Are there missing elements or gaps?
- Is the scope complete?

### 4. Step Quality
- Are steps using proven MCP tools from successful iterations?
- Are steps clear, specific, and reproducible?
- Do steps reflect learnings from failed iterations (avoiding mistakes)?
- Are dependencies clearly defined?

### 5. Iteration Learning
- Does todo reflect evolution across iterations (improvement over time)?
- Were insights from validation integrated?
- Are there patterns from multiple iterations incorporated?
- Is this better than any single iteration's approach?

` + memory.GetWorkflowMemoryRequirements() + `

## 📤 Output Format
**Return clean markdown only, not JSON**

# Final Todo List Assessment Report - Iteration Synthesis

## 📊 Assessment Scores

### Synthesis Quality: [High/Medium/Low]
- **All Iterations Read**: Did Writer analyze ALL "## Iteration X" sections?
- **Comprehensive Analysis**: [Yes/No - All planning + execution history reviewed?]
- **Iteration Comparison**: Were methods compared across iterations?
- **Confidence**: [High/Medium/Low]

### Method Selection: [High/Medium/Low]
- **Best Methods Selected**: Highest success rates from iterations?
- **Evidence-Backed**: Selection justified by iteration results?
- **Failed Methods Excluded**: Early failed approaches not included?
- **Selection Quality**: [Strong/Moderate/Weak]

### Objective Coverage: [High/Medium/Low]
- **Coverage**: Does todo address all requirements?
- **Missing Elements**: [None/Minor/Major gaps]
- **Scope**: [Complete/Mostly Complete/Incomplete]

### Step Quality: [High/Medium/Low]
- **Proven MCP Tools**: Uses tools from successful iterations?
- **Clarity**: Clear and specific?
- **Reproducibility**: Consistently executable?
- **Learning Integration**: Avoids mistakes from failed iterations?

### Iteration Learning: [High/Medium/Low]
- **Evolution**: Shows improvement over iterations?
- **Pattern Recognition**: Incorporates patterns from multiple iterations?
- **Better Than Single**: Superior to any single iteration?
- **Validation Integration**: Includes validation insights?

## 🔍 Key Findings

### 📚 Iteration History Analysis
- **Total Iterations Analyzed**: [Number from files]
- **Planning Iterations Found**: [Count from plan.md]
- **Execution Iterations Found**: [Count from execution_results.md]
- **Validation Iterations Found**: [Count from validation report]

### 🏆 Method Selection Analysis
- **Iteration 1**: [Method tried, success rate]
- **Iteration 2**: [Method tried, success rate]
- **Iteration X**: [Method tried, success rate]
- **Winner Selected**: [Which iteration's method was chosen for todo?]
- **Correct Choice**: [Yes/No - Is this the best from all iterations?]

### ✅ Strengths
[What aspects are well done]

### ⚠️ Coverage Gaps
[Missing or incomplete aspects]

### 📝 Step Quality Issues
[Steps that are unclear or hard to reproduce]

### 🔧 Optimization Issues
[Claims lacking evidence or suboptimal approaches]

### ❓ Approach Selection Concerns
[If wrong approach was selected or selection not justified]

## 💡 Recommendations

### Priority Improvements
1. [Most critical issue to address]
2. [Second priority]
3. [Third priority]

### Specific Actions
- **Coverage**: [How to address gaps]
- **Quality**: [How to improve steps]
- **Optimization**: [How to verify claims]
- **Execution**: [How to complete gaps]

## ✅ Final Assessment
- **All Iterations Analyzed**: [Yes/No]
- **Best Methods Selected**: [Yes/No - Highest success rates from iterations?]
- **Synthesis Quality**: [High/Medium/Low]
- **Complete Coverage**: [Yes/No]
- **Reproducible Steps**: [Yes/No]
- **Iteration Learning Applied**: [Yes/No]
- **Production Ready**: [Yes/No]
- **Reason**: [Brief explanation - focus on synthesis quality and iteration-based selection]

Focus on verifying comprehensive iteration analysis, optimal method synthesis, and production readiness based on ALL iteration history.`

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
