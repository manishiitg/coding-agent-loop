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

// HumanControlledTodoPlannerWriterTemplate holds template variables for human-controlled todo writing prompts
type HumanControlledTodoPlannerWriterTemplate struct {
	Objective       string
	WorkspacePath   string
	TotalIterations string
}

// HumanControlledTodoPlannerWriterAgent creates optimal todo list based on execution experience in human-controlled mode
type HumanControlledTodoPlannerWriterAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerWriterAgent creates a new human-controlled todo planner writer agent
func NewHumanControlledTodoPlannerWriterAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerWriterAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerWriterAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerWriterAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpwa *HumanControlledTodoPlannerWriterAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract variables from template variables
	// Human-controlled writer - synthesizes from single execution for todo list creation
	objective := templateVars["Objective"]
	workspacePath := templateVars["WorkspacePath"]
	totalIterations := templateVars["TotalIterations"]
	if strings.TrimSpace(totalIterations) == "" {
		totalIterations = "1"
	}

	// Prepare template variables
	writerTemplateVars := map[string]string{
		"Objective":       objective,
		"WorkspacePath":   workspacePath,
		"TotalIterations": totalIterations,
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerWriterTemplate{
		Objective:       objective,
		WorkspacePath:   workspacePath,
		TotalIterations: totalIterations,
	}

	// Execute using template validation
	return hctpwa.ExecuteWithTemplateValidation(ctx, writerTemplateVars, hctpwa.humanControlledWriterInputProcessor, conversationHistory, templateData)
}

// humanControlledWriterInputProcessor processes inputs specifically for human-controlled todo list creation
func (hctpwa *HumanControlledTodoPlannerWriterAgent) humanControlledWriterInputProcessor(templateVars map[string]string) string {
	// Create template data
	totalIterationsForTemplate := templateVars["TotalIterations"]
	if strings.TrimSpace(totalIterationsForTemplate) == "" {
		totalIterationsForTemplate = "1"
	}
	templateData := HumanControlledTodoPlannerWriterTemplate{
		Objective:       templateVars["Objective"],
		WorkspacePath:   templateVars["WorkspacePath"],
		TotalIterations: totalIterationsForTemplate,
	}

	// Define the template - structured format matching planner agent for LLM execution
	templateStr := `## 🎯 PRIMARY TASK - CREATE STRUCTURED TODO LIST

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Writer Agent
- **Responsibility**: Create structured, execution-ready todo list based on execution results and learnings
- **Mode**: Structured synthesis (create step-by-step plan format that another LLM can efficiently execute)

## 📁 FILE PERMISSIONS
**READ:**
- planning/plan.md (original plan)
- validation/step_*_validation_report.md (all step validation reports with execution summaries)
- learnings/success_patterns.md (success learning insights)
- learnings/failure_analysis.md (failure patterns to avoid)
- learnings/step_*_learning.md (per-step learning details)

**WRITE:**
- {{.WorkspacePath}}/todo_final.md (final structured todo list - outside todo_creation_human/)

**RESTRICTIONS:**
- Read from planning/, validation/, learnings/ folders
- Validation reports contain execution summaries from execution agent
- Write todo_final.md to workspace root in STRUCTURED format
- Format MUST be parseable and executable by another LLM

## 📋 SYNTHESIS GUIDELINES
- **Read All Execution Data**: Review plan.md, validation reports (which include execution summaries), and all learning files
- **Extract Success Patterns**: From learnings/success_patterns.md - capture what worked, which tools, which approaches
- **Extract Failure Patterns**: From learnings/failure_analysis.md - capture what failed, which tools to avoid, anti-patterns
- **Use Structured Format**: Each step must have: title, description, success_criteria, why_this_step, context_dependencies, context_output, success_patterns, failure_patterns
- **Be Specific**: Include exact MCP server, tool names, and successful approaches
- **Make It Executable**: Another LLM should be able to read this and execute without ambiguity

**Critical Structure Requirements**:
- **title**: Clear, concise step name
- **description**: Detailed what and how, including specific tools and approaches that worked
- **success_criteria**: Measurable completion criteria
- **why_this_step**: Explain purpose and value
- **context_dependencies**: What needs to be done before this step
- **context_output**: What this step produces for subsequent steps
- **success_patterns**: List of approaches/tools that WORKED (from learning reports)
- **failure_patterns**: List of approaches/tools that FAILED (from learning reports)

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**CREATE** {{.WorkspacePath}}/todo_final.md

---

# 📋 Structured Todo List: {{.Objective}}

**Date**: [Current date/time]
**Status**: Ready for Execution
**Based On**: Validated execution results and learning analysis

## 🎯 Objective
{{.Objective}}

## 📊 Execution Summary
**Total Steps Completed**: [X steps]
**Success Rate**: [X% based on validation]
**Key Learnings Applied**: [Brief summary of main insights]

---

## 📝 Step-by-Step Execution Plan

### Step 1: [Step Title]

**Description:**
[Detailed description of what needs to be done. Include specific tools, MCP servers, and approaches that worked during execution. Be explicit about the exact commands, arguments, or methods to use.]

**Success Criteria:**
- [Specific, measurable criterion 1]
- [Specific, measurable criterion 2]
- [Specific, measurable criterion 3]

**Why This Step:**
[Explain the purpose and value of this step. Why is it necessary? What problem does it solve?]

**Context Dependencies:**
- [What must be completed before this step]
- [Any files, data, or setup required]

**Context Output:**
[What this step produces that subsequent steps will need]

**Success Patterns (What Worked):**
- [Specific tool/approach that succeeded: e.g., "Used fileserver.read_file with path argument to read configuration"]
- [Specific MCP server/tool combination that worked]
- [Any successful techniques or methods from execution]
- [Include exact tool names and arguments that proved successful]

**Failure Patterns (What to Avoid):**
- [Specific tool/approach that failed: e.g., "Avoid using grep without proper escaping"]
- [Specific MCP server/tool combination that didn't work]
- [Any unsuccessful techniques or anti-patterns]
- [Include exact scenarios or approaches to avoid]

---

### Step 2: [Step Title]

**Description:**
[Detailed description with specific tools and methods]

**Success Criteria:**
- [Criterion 1]
- [Criterion 2]

**Why This Step:**
[Purpose and value]

**Context Dependencies:**
- [Prerequisites]

**Context Output:**
[What this produces]

**Success Patterns (What Worked):**
- [Specific successful approach 1]
- [Specific successful approach 2]

**Failure Patterns (What to Avoid):**
- [Specific failed approach 1]
- [Specific failed approach 2]

---

### Step 3: [Step Title]

**Description:**
[Detailed description with specific tools and methods]

**Success Criteria:**
- [Criterion 1]
- [Criterion 2]

**Why This Step:**
[Purpose and value]

**Context Dependencies:**
- [Prerequisites]

**Context Output:**
[What this produces]

**Success Patterns (What Worked):**
- [Specific successful approach 1]
- [Specific successful approach 2]

**Failure Patterns (What to Avoid):**
- [Specific failed approach 1]
- [Specific failed approach 2]

---

[Continue with all remaining steps in the same format]

---

## 🎯 Execution Guidelines for Next LLM

### How to Use This Todo List
1. **Read Each Step Sequentially**: Execute steps in order due to dependencies
2. **Check Context Dependencies**: Ensure prerequisites are met before starting each step
3. **Use Success Patterns**: Follow the approaches that worked during validation
4. **Avoid Failure Patterns**: Do not repeat approaches that failed
5. **Verify Success Criteria**: After each step, validate against the success criteria
6. **Capture Context Output**: Ensure each step produces the expected output for subsequent steps

### Key Success Factors
- **Follow Success Patterns Exactly**: The tools and approaches listed have been validated
- **Respect Dependencies**: Context dependencies are critical for successful execution
- **Validate Thoroughly**: Check success criteria after each step
- **Learn from Failures**: Avoid all listed failure patterns

### Recommended Execution Approach
- Execute steps sequentially (Step 1 → Step 2 → Step 3 → ...)
- Validate after each step before proceeding
- If a step fails, review success patterns and try the proven approach
- If uncertain, refer to the "Why This Step" section for context

---

**Note**: This todo list is structured for efficient LLM execution. Each step contains validated approaches (success patterns) and anti-patterns to avoid (failure patterns). Follow the success patterns exactly as they have been tested and validated during execution.`

	// Parse and execute the template
	tmpl, err := template.New("human_controlled_writer").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled writer template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled writer template: %v", err)
	}

	return result.String()
}
