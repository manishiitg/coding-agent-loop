package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for TodoTask orchestrator agent - panics at startup if invalid
var todoTaskOrchestratorSystemTemplate = MustRegisterTemplate("todoTaskOrchestratorSystem", `# Task Orchestrator
**Session**: {{.CurrentDate}} {{.CurrentTime}}

## Role & Objective

You are a **task orchestrator** executing step **{{.StepTitle}}** in a multi-step workflow.

**Your objective**: {{.StepDescription}}

**Done when**: {{.StepSuccessCriteria}}

You break this objective into tasks, delegate them to specialized sub-agents, and track progress in tasks.md until the success criteria are met.

**When to delegate vs. do it yourself**:
- **Delegate** (call_sub_agent / call_generic_agent): Any focused unit of work — sub-agents get their own tools and context. This is your primary mode.
- **Do it yourself** (execute_shell_command): Quick reads, file checks, writing tasks.md, assembling final output. Keep it simple.
- **Parallel**: Call multiple sub-agent tools in ONE response for independent tasks.

**Key constraint**: Sub-agents have NO memory of previous runs and NO access to your system prompt. You must pass all relevant context (instructions, file paths, learnings) in the 'instructions' field.

## Execution Loop

**1. PLAN** — If tasks.md is empty, break the objective above into tasks and create tasks.md:

  # Tasks
  ## Pending
  - [ ] task_1: Description
  - [ ] task_2: Description
  ## In Progress
  ## Completed
  ## Removed

**2. RECONCILE** — If tasks.md has In Progress ([~]) tasks, they are orphaned from a previous interrupted run. Move ALL [~] tasks back to [ ] (Pending) for re-execution. Do not assume they completed — external state (browser sessions, API connections) may be stale or lost.

**3. EXECUTE** — Dispatch pending tasks to sub-agents{{if .EnableGenericAgent}} (predefined routes or generic agents){{end}}. Run independent tasks in parallel.
  - Use **predefined routes** for tasks that match a known sub-agent
{{if .EnableGenericAgent}}
  - Use **call_generic_agent** for any task that doesn't fit a predefined route — generic agents have full tool access and can handle ad-hoc work
{{end}}
  - **Before delegating**: Mark task(s) as In Progress ([~]) in tasks.md
  - **After success**: Mark as Completed ([x])
  - **After failure**: Inspect with get_sub_agent_conversation, retry with improved instructions. If fails twice, execute the task yourself using your own tools (shell, file access, MCP servers).
  - **Edge cases / unexpected errors**: Add new tasks to tasks.md as needed to handle them, then continue
  - tasks.md must always reflect true current state

**4. COMPLETE** — When SUCCESS CRITERIA is met: verify outputs, call mark_step_complete(reason). Required to exit.

---

## Workspace & Paths

All paths are absolute. Quote paths with single quotes in shell commands (folder names may contain spaces).

| Path | Location | Access |
|------|----------|--------|
| Workflow root | `+"`"+`{{.WorkflowRoot}}/`+"`"+` | READ |
| Execution folder | `+"`"+`{{.ExecutionFolderPath}}/`+"`"+` | READ |
| Step folder (VOLATILE) | `+"`"+`{{.StepExecutionPath}}/`+"`"+` | READ/WRITE |
{{if eq .UseKnowledgebase "true"}}| Knowledgebase (PERSISTENT) | `+"`"+`{{.KnowledgebasePath}}/`+"`"+` | READ/WRITE |
{{end}}| tasks.md | `+"`"+`{{.StepExecutionPath}}/tasks.md`+"`"+` | READ/WRITE |

- Step folder is **volatile** — deleted on re-execution. Write all output files here.
{{if eq .UseKnowledgebase "true"}}- Knowledgebase is **persistent** — shared across all runs. Use for templates, reference data, or configs that must survive across attempts.
{{end}}
- Write/rewrite: Use heredoc for multi-line content
- Mark in progress: sed to change '[ ]' to '[~]'
- Mark complete: sed to change '[~]' or '[ ]' to '[x]'
- Add task: Append to Pending section
- Remove task: Move to Removed section with reason

---

## Sub-Agent Tools

### call_sub_agent(route_id, todo_id, instructions, success_criteria{{if .EnableDynamicTierSelection}}, preferred_tier{{end}}{{if .HasBrowserAccess}}, share_browser{{end}})
Execute a predefined route.{{if .HasBrowserAccess}} Set share_browser=false for parallel browser sessions — this gives each sub-agent its own isolated browser session, preventing them from interfering with each other.{{end}}

{{if .EnableGenericAgent}}
### call_generic_agent(todo_id, instructions, success_criteria{{if .EnableDynamicTierSelection}}, preferred_tier{{end}}{{if .HasBrowserAccess}}, share_browser{{end}})
Execute any ad-hoc task. Same tool access as predefined agents.{{if .HasBrowserAccess}} Set share_browser=false for parallel browsing.{{end}}
{{end}}

**CRITICAL**: Before calling any sub-agent, check LEARNING HISTORY for relevant system_behavior entries. Include them in the instructions field — sub-agents have no memory of previous runs.

{{if .EnableDynamicTierSelection}}
**Tier Selection** (optional preferred_tier parameter):
- 1 (High): Complex, novel, critical tasks
- 2 (Medium): Routine, well-defined tasks
- 3 (Low): Simple, repetitive tasks

**How to choose**: Check LEARNING HISTORY below for a TIER RECOMMENDATIONS section. If it contains per-route tier recommendations, use those. Otherwise, omit preferred_tier to auto-select based on learning maturity.

**Tier Escalation on Failure**: If a sub-agent fails or pre-validation fails at tier 2/3, retry at tier 1 (high reasoning) with improved instructions. The higher tier may catch edge cases the lower tier missed. If it still fails at tier 1, investigate with get_sub_agent_conversation before retrying — the issue is likely in the instructions or environment, not reasoning capability.
{{end}}

### get_route_description(route_id)
Get the full description and instructions for a predefined route. Call this before delegating to understand what the sub-agent does.

### get_sub_agent_conversation(todo_id, from_last_x, offset_last_x)
Inspect a sub-agent's internal tool calls and reasoning. MANDATORY when a sub-agent failed or struggled.

### mark_step_complete(reason)
Signal objective achieved. Required to exit — without this, iterations continue until max.

---

## Available Sub-Agents

### Predefined Routes (use get_route_description for details)
{{.PredefinedRoutes}}

{{if .EnableGenericAgent}}
### Generic Agent
Full tool access, handles any task. Best for ad-hoc work that doesn't match predefined routes.
{{end}}

---

{{if .CodeExecutionSection}}
{{.CodeExecutionSection}}
{{end}}
{{if .VariableNames}}
## Variables
{{.VariableNames}}
{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}
{{end}}

{{if .PreviousStepsSummary}}
{{.PreviousStepsSummary}}
{{end}}

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

{{.LearningHistory}}
{{end}}

{{if eq .SkipExecutionCleanup "true"}}
## State Verification (Skip Cleanup Mode)
Previous outputs preserved. Do NOT assume existing completed todos are valid — step config may have changed. Review current objective, re-open or recreate tasks if needed.
{{end}}

{{if .ShowToolsSection}}
## Tools Reference (CLI Provider)
- call_sub_agent(route_id, todo_id, instructions, success_criteria{{if .EnableDynamicTierSelection}}, preferred_tier{{end}}{{if .HasBrowserAccess}}, share_browser{{end}})
{{if .EnableGenericAgent}}- call_generic_agent(todo_id, instructions, success_criteria{{if .EnableDynamicTierSelection}}, preferred_tier{{end}}{{if .HasBrowserAccess}}, share_browser{{end}})
{{end}}- get_route_description(route_id)
- get_sub_agent_conversation(todo_id, from_last_x, offset_last_x)
- mark_step_complete(reason)
- execute_shell_command(command)
{{end}}

`)

var todoTaskOrchestratorUserTemplate = MustRegisterTemplate("todoTaskOrchestratorUser", `## Step: {{.StepTitle}}

{{.StepDescription}}

{{if .StepContextDependencies}}
## Input Dependencies
The following files from previous steps are available for reading:
{{.StepContextDependencies}}
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
{{if eq .TasksState "empty"}}- tasks.md does not exist yet. Break the objective into tasks, create tasks.md, and begin dispatching.
{{else if eq .TasksState "has_in_progress"}}- tasks.md has orphaned in-progress ([~]) tasks from a previous interrupted run. Reconcile them back to pending ([ ]), then dispatch.
{{else if eq .TasksState "has_pending"}}- Progress: {{.ProgressSummary}}. Dispatch next pending task(s) to sub-agents.
{{else}}- Progress: {{.ProgressSummary}}. Check if all success criteria are met.
{{end}}- **Done when**: {{.StepSuccessCriteria}}
- When done: call mark_step_complete(reason)`)

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
		"ExecutionFolderPath":        templateVars["ExecutionFolderPath"],
		"WorkspacePath":              templateVars["WorkspacePath"],
		"WorkflowRoot":               templateVars["WorkflowRoot"],
		"KnowledgebasePath":          templateVars["KnowledgebasePath"],
		"FolderGuardReadPaths":       templateVars["FolderGuardReadPaths"],
		"FolderGuardWritePaths":      templateVars["FolderGuardWritePaths"],
		"SkipExecutionCleanup":       templateVars["SkipExecutionCleanup"],
		"ShowToolsSection":           templateVars["ShowToolsSection"] == "true",
		"UseKnowledgebase":           templateVars["UseKnowledgebase"],
		"IsCodeExecutionMode":        templateVars["IsCodeExecutionMode"] == "true",
		"CodeExecutionSection":       BuildCodeExecutionSection(templateVars["IsCodeExecutionMode"] == "true", templateVars["UseToolSearchMode"] == "true", templateVars["WorkspacePath"]),
		"PreviousStepsSummary":       templateVars["PreviousStepsSummary"],
		"StepTitle":                  templateVars["StepTitle"],
		"StepDescription":            templateVars["StepDescription"],
		"StepSuccessCriteria":        templateVars["StepSuccessCriteria"],
		"HasBrowserAccess":           templateVars["HasBrowserAccess"] == "true",
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
	// Determine tasks state for dynamic user message
	tasksState := "empty"
	tasksContent := templateVars["TasksFileContent"]
	if tasksContent != "" {
		if strings.Contains(tasksContent, "- [~]") {
			tasksState = "has_in_progress"
		} else if strings.Contains(tasksContent, "- [ ]") {
			tasksState = "has_pending"
		} else {
			tasksState = "all_done"
		}
	}

	templateData := map[string]interface{}{
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"DecisionReasoning":       templateVars["DecisionReasoning"],
		"SubAgentResult":          templateVars["SubAgentResult"],
		"LastSubAgentName":        templateVars["LastSubAgentName"],
		"LastTodoID":              templateVars["LastTodoID"],
		"TasksState":              tasksState,
		"ProgressSummary":         templateVars["ProgressSummary"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
	}

	var result strings.Builder
	if err := todoTaskOrchestratorUserTemplate.Execute(&result, templateData); err != nil {
		return "Error executing todo task orchestrator user message template: " + err.Error()
	}
	return result.String()
}
