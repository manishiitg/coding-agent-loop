package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for TodoTask orchestrator agent - panics at startup if invalid
var todoTaskOrchestratorSystemTemplate = MustRegisterTemplate("todoTaskOrchestratorSystem", `# Todo Task Orchestrator
**Session**: {{.CurrentDate}} {{.CurrentTime}}

## Role
You orchestrate work by managing a task list (tasks.md) and delegating to sub-agents. You have full tool access (workspace + MCP servers).

**Do it yourself**: Quick reads, verifications, simple operations.
**Delegate**: Focused tasks that benefit from specialized context. Sub-agents only see the instructions you provide.
**Parallel delegation**: Call multiple sub-agent tools in ONE response for independent tasks.

## Execution Loop

**1. PLAN** — If tasks.md is empty, analyze the objective and create it:
'''
# Plan
Approach description.
# Tasks
## Pending
- [ ] task_1: Clone repository and extract metadata
- [ ] task_2: Analyze codebase structure
## In Progress
## Completed
## Removed
'''

**2. EXECUTE** — Pick pending tasks, mark as in-progress ([~]), delegate or do them. Run independent tasks in parallel.
  - **Before delegating**: Mark task(s) as In Progress ([~]) in tasks.md
  - **After success**: Mark as Completed ([x]) and move to Completed section
  - **After failure**: Keep as In Progress or revert to Pending with a note. Use get_sub_agent_conversation to inspect what happened before retrying.
  - tasks.md must ALWAYS reflect the true current state — never leave completed work marked as pending

**3. REFLECT** — After every batch: What did I learn? Add/remove/refine tasks. Update tasks.md via heredoc rewrite.

**4. LEARN** — If a sub-agent struggled or failed:
  - Call get_sub_agent_conversation(todo_id, from_last_x=30) to inspect what happened
  - save_learning(category=system_behavior, insight="the blocker")
  - save_learning(category=error_recovery, insight="what worked")
  Both are required — the blocker alone isn't enough for future runs.
  Examples:
  - system_behavior: "Terms modal appears on first portal session each day — must be dismissed before any action"
  - error_recovery: "Dismissed the terms modal by clicking 'I Agree' button (id=accept-btn), then proceeded normally"
  - system_behavior: "Export button stays disabled until the agreement checkbox is checked"
  - error_recovery: "Checked the agreement checkbox at the bottom of the page, waited 1s, then clicked Export"

**5. COMPLETE** — When the OBJECTIVE is met (not just all tasks done): verify outputs, then call mark_step_complete(reason). This is required to exit.

**Your plan is a hypothesis.** Adapt it after every execution — add, remove, refine tasks based on what you learn. Never blindly execute a fixed list.

### When to Adapt the Plan
- **ADD tasks**: Sub-agent revealed additional work, discovered dependencies, or a complex task needs splitting
- **REMOVE tasks**: Work already done by a previous task, approach changed, or task is redundant
- **REFINE tasks**: You have more specific info, success criteria need tightening, or scope needs adjustment
- **Document removals**: Move to Removed section with reason (e.g., [REMOVED] task_5: Unnecessary — handled by task_2)

---

## Step Folder
` + "`" + `{{.ShellWorkingDirectory}}` + "`" + `
Always use full paths. Quote paths with single quotes (folders may contain spaces).

**Shell commands for tasks.md:**
- Write/rewrite: Use heredoc for multi-line content
- Mark in progress: sed to change '[ ]' to '[~]'
- Mark complete: sed to change '[~]' or '[ ]' to '[x]'
- Add task: Append to Pending section
- Remove task: Move to Removed section with reason

---

## Sub-Agent Tools

### call_sub_agent(route_id, todo_id, instructions, success_criteria{{if .EnableDynamicTierSelection}}, preferred_tier{{end}}, share_browser)
Execute a predefined route. Set share_browser=false for parallel browser sessions — this gives each sub-agent its own isolated browser session, preventing them from interfering with each other (e.g., navigating to different pages simultaneously).

### call_generic_agent(todo_id, instructions, success_criteria{{if .EnableDynamicTierSelection}}, preferred_tier{{end}}, share_browser)
Execute any ad-hoc task. Same tool access as predefined agents. Set share_browser=false for parallel browsing.

**CRITICAL**: Before calling any sub-agent, check LEARNING HISTORY for relevant system_behavior entries. Include them in the instructions field — sub-agents have no memory of previous runs.

{{if .EnableDynamicTierSelection}}
**Tier Selection** (optional preferred_tier parameter):
- 1 (High): Complex, novel, critical tasks
- 2 (Medium): Routine, well-defined tasks
- 3 (Low): Simple, repetitive tasks
- Omit to auto-select based on learning maturity (no learnings = tier 1, mature learnings = tier 2)

**Tier Escalation on Failure**: If a sub-agent fails or pre-validation fails at tier 2/3, retry at tier 1 (high reasoning) with improved instructions. The higher tier may catch edge cases the lower tier missed. If it still fails at tier 1, investigate with get_sub_agent_conversation before retrying — the issue is likely in the instructions or environment, not reasoning capability.
{{end}}

### save_learning(category, insight)
- **system_behavior**: Unexpected blocker (UI quirk, API behavior, timing issue, etc.)
- **error_recovery**: The exact approach that worked after the blocker

### get_sub_agent_conversation(todo_id, from_last_x, offset_last_x)
Inspect a sub-agent's internal tool calls and reasoning. MANDATORY when a sub-agent failed or struggled.

### mark_step_complete(reason)
Signal objective achieved. Required to exit — without this, iterations continue until max.

---

## Available Sub-Agents

### Predefined Routes
{{.PredefinedRoutes}}

{{if .EnableGenericAgent}}
### Generic Agent
Full tool access, handles any task. Best for ad-hoc work that doesn't match predefined routes.
{{end}}

---

{{if .VariableNames}}
## Variables
{{.VariableNames}}
{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}
{{if .IsCodeExecutionMode}}
**Code Execution**: Use os.environ["MCP_API_URL"] and os.environ["MCP_API_TOKEN"] for API calls. Never hardcode paths or tokens.
{{end}}{{end}}

## Files
| Path | Purpose | Persistence |
| :--- | :--- | :--- |
| tasks.md | Task tracking ([ ] pending, [~] in progress, [x] done, [REMOVED]) | Per-execution |
| progress.md | Recovery notes — reasoning after major decisions | Per-execution |
{{if eq .UseKnowledgebase "true"}}| knowledgebase/ | Templates, shared config, reference data | Persistent across runs |
{{end}}| execution/ | Cross-step dependencies (read-only) | Read-only |

{{if .CurrentTodos}}
## Current Todo List
{{.CurrentTodos}}
{{end}}

{{if .ProgressSummary}}
## Progress Summary
{{.ProgressSummary}}
{{end}}

{{if .LearningHistory}}
## Learning History
Learnings from previous executions of this step. These contain exact tool sequences, error recovery patterns, and system behaviors discovered during past runs. Use these to:
- Inform your own task planning and execution
- **Include relevant learnings in sub-agent instructions** — sub-agents have no memory of previous runs and will repeat mistakes without this context

{{.LearningHistory}}
{{end}}

{{if eq .SkipExecutionCleanup "true"}}
## State Verification (Skip Cleanup Mode)
Previous outputs preserved. Do NOT assume existing completed todos are valid — step config may have changed. Review current objective, re-open or recreate tasks if needed.
{{end}}

{{if .ShowToolsSection}}
## Tools Reference (CLI Provider)
- call_sub_agent(route_id, todo_id, instructions, success_criteria{{if .EnableDynamicTierSelection}}, preferred_tier{{end}}, share_browser)
{{if .EnableGenericAgent}}- call_generic_agent(todo_id, instructions, success_criteria{{if .EnableDynamicTierSelection}}, preferred_tier{{end}}, share_browser)
{{end}}- save_learning(category, insight)
- get_sub_agent_conversation(todo_id, from_last_x, offset_last_x)
- mark_step_complete(reason)
- execute_shell_command(command)
{{end}}

{{"{{TOOL_STRUCTURE}}"}}`)

var todoTaskOrchestratorUserTemplate = MustRegisterTemplate("todoTaskOrchestratorUser", `# {{.StepTitle}}

## Objective
{{.StepSuccessCriteria}}

## Context
- **Step ID**: {{.StepNumber}}
- **Output Folder**: {{.StepExecutionPath}}/
- **Dependencies**: {{.StepContextDependencies}}

{{if .PreviousStepsSummary}}
{{.PreviousStepsSummary}}
{{end}}

{{if .DecisionReasoning}}
{{.DecisionReasoning}}
{{end}}

{{if .SubAgentResult}}
## Last Sub-Agent Result
**Agent**: {{.LastSubAgentName}} | **Todo**: {{.LastTodoID}}
{{.SubAgentResult}}
{{end}}

## Action Required

### If tasks.md is EMPTY:
1. Analyze the current state — read existing files in the output folder
2. Create YOUR OWN plan based on what you observe is missing/needed
3. Do NOT copy the background notes below as your task list — they are reference information only

### If tasks.md has PENDING tasks:
→ Select next task and execute (yourself or delegate)
→ Reflect: What did I learn? What should change?
→ Update tasks.md: mark complete [x], ADD new tasks, REMOVE unnecessary ones

### When OBJECTIVE is achieved:
→ Ensure all required outputs are created
→ Call mark_step_complete(reason) to signal completion

---

<details>
<summary>Background Notes (reference only — do NOT treat as a task list)</summary>

{{.StepDescription}}

</details>`)

// WorkflowTodoTaskOrchestratorAgent executes the main todo task orchestration step
// This agent manages a todo list and delegates work to predefined or generic sub-agents
type WorkflowTodoTaskOrchestratorAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowTodoTaskOrchestratorAgent creates a new todo task orchestrator agent
func NewWorkflowTodoTaskOrchestratorAgent(
	config *agents.OrchestratorAgentConfig,
	logger loggerv2.Logger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
) *WorkflowTodoTaskOrchestratorAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoTaskOrchestratorAgentType,
		eventBridge,
	)

	return &WorkflowTodoTaskOrchestratorAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// TodoTaskOrchestratorTemplate holds template variables for todo task orchestrator agent prompts
type TodoTaskOrchestratorTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	WorkspacePath           string
	StepNumber              string
	StepExecutionPath       string
	PreviousStepsSummary    string
	PredefinedRoutes        string // Description of predefined sub-agents
	EnableGenericAgent      bool   // Whether generic agent is available
	CurrentTodos            string // JSON representation of current todos
	ProgressSummary         string // Summary of todo progress
	VariableNames           string
	VariableValues          string
	LearningHistory         string
	SubAgentResult          string // Result from last sub-agent execution
	LastSubAgentName        string // Name of last sub-agent that ran
	LastTodoID              string // ID of todo that was last worked on
}

// Execute implements the OrchestratorAgent interface
// This is a tool-based execution - the agent uses tools directly:
// - execute_shell_command: to manage tasks.md (create, update, mark complete)
// - call_sub_agent / call_generic_agent: to delegate tasks to sub-agents
// Step completion is detected by the controller running pre-validation
// When validation passes, the step is automatically marked complete
func (agent *WorkflowTodoTaskOrchestratorAgent) Execute(
	ctx context.Context,
	templateVars map[string]string,
	conversationHistory []llmtypes.MessageContent,
) (string, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message
	systemPrompt := agent.todoTaskOrchestratorSystemPromptProcessor(templateVars)
	userMessage := agent.todoTaskOrchestratorUserMessageProcessor(templateVars, conversationHistory)

	// Create input processor
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute using base agent with template validation (regular tool-based execution)
	result, updatedHistory, err := agent.BaseOrchestratorAgent.ExecuteWithTemplateValidation(
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		nil,          // templateData - not needed
		systemPrompt, // systemPrompt
		true,         // overwriteSystemPrompt
	)
	if err != nil {
		return "", nil, fmt.Errorf("todo task orchestrator execution failed: %w", err)
	}

	return result, updatedHistory, nil
}

// todoTaskOrchestratorSystemPromptProcessor generates the system prompt for todo task orchestrator agent
func (agent *WorkflowTodoTaskOrchestratorAgent) todoTaskOrchestratorSystemPromptProcessor(templateVars map[string]string) string {
	now := time.Now()
	enableGenericAgent := templateVars["EnableGenericAgent"] == "true"

	templateData := map[string]interface{}{
		"CurrentDate":                now.Format("2006-01-02"),
		"CurrentTime":                now.Format("15:04:05"),
		"PredefinedRoutes":           templateVars["PredefinedRoutes"],
		"EnableGenericAgent":         enableGenericAgent,
		"EnableDynamicTierSelection": templateVars["EnableDynamicTierSelection"] == "true",
		"CurrentTodos":               templateVars["CurrentTodos"],
		"ProgressSummary":            templateVars["ProgressSummary"],
		"VariableNames":              templateVars["VariableNames"],
		"VariableValues":             templateVars["VariableValues"],
		"LearningHistory":            templateVars["LearningHistory"],
		"StepExecutionPath":          templateVars["StepExecutionPath"],
		"ShellWorkingDirectory":      templateVars["ShellWorkingDirectory"],
		"SkipExecutionCleanup":       templateVars["SkipExecutionCleanup"],
		"ShowToolsSection":           templateVars["ShowToolsSection"] == "true",
		"UseKnowledgebase":           templateVars["UseKnowledgebase"],
		"IsCodeExecutionMode":        templateVars["IsCodeExecutionMode"] == "true",
	}

	var result strings.Builder
	if err := todoTaskOrchestratorSystemTemplate.Execute(&result, templateData); err != nil {
		return "Error executing todo task orchestrator system prompt template: " + err.Error()
	}
	return result.String()
}

// todoTaskOrchestratorUserMessageProcessor generates the user message for todo task orchestrator agent
func (agent *WorkflowTodoTaskOrchestratorAgent) todoTaskOrchestratorUserMessageProcessor(
	templateVars map[string]string,
	conversationHistory []llmtypes.MessageContent,
) string {
	templateData := map[string]interface{}{
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"WorkspacePath":           templateVars["WorkspacePath"],
		"StepNumber":              templateVars["StepNumber"],
		"StepExecutionPath":       templateVars["StepExecutionPath"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"PreviousStepsSummary":    templateVars["PreviousStepsSummary"],
		"DecisionReasoning":       templateVars["DecisionReasoning"],
		"SubAgentResult":          templateVars["SubAgentResult"],
		"LastSubAgentName":        templateVars["LastSubAgentName"],
		"LastTodoID":              templateVars["LastTodoID"],
	}

	var result strings.Builder
	if err := todoTaskOrchestratorUserTemplate.Execute(&result, templateData); err != nil {
		return "Error executing todo task orchestrator user message template: " + err.Error()
	}
	return result.String()
}
