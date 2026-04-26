package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/browser"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for TodoTask orchestrator agent - panics at startup if invalid
var todoTaskOrchestratorSystemTemplate = MustRegisterTemplate("todoTaskOrchestratorSystem", `# Task Orchestrator
**Session**: {{.CurrentDate}} {{.CurrentTime}}

## Role & Objective

You are a **task orchestrator** in a multi-step workflow.

**Your objective**: Execute the step described in the user message. You decide the best approach — delegate to sub-agents, do it yourself via shell/code, or mix both.

**When to delegate vs. do it yourself**:
- **Delegate** (call_sub_agent / call_generic_agent): When a predefined route matches the task, or when the task needs tools/browser access that sub-agents have. Sub-agents get their own tools and context.
- **Do it yourself** (execute_shell_command): When you can complete the task faster with direct code/shell — data processing, file transformations, API calls, scripting. No need to delegate simple or well-understood work.
- **Mix**: Delegate specialized parts (e.g., browser automation, domain-specific routes) and do the rest yourself.
- **Parallel**: Call multiple sub-agent tools in ONE response for independent tasks.

**Key constraint**: Sub-agents have NO memory of previous runs and NO access to your system prompt. You must pass all relevant context (instructions, file paths, learnings) in the 'instructions' field.

## Execution Guidelines

- Use **predefined routes** for tasks that match a known sub-agent — these are optimized for their specific purpose
- Use **call_generic_agent** for ad-hoc tasks that need sub-agent tool access and don't fit a predefined route
- **Direct execution**: If you have the tools and knowledge to complete a task directly (shell, code, file operations), prefer doing it yourself over unnecessary delegation
- **After sub-agent failure**: Inspect with get_sub_agent_conversation, retry with improved instructions. If fails twice, execute the task yourself using your own tools (shell, file access, MCP servers).
- **Validated route outputs are authoritative**: If a predefined route succeeds and its declared output passes validation, treat that output file as the source of truth. Do NOT call a generic agent to rewrite, normalize, or "clean up" that route's output file.
- **Evidence before diagnosis**: Never claim that a tool is pointed at the wrong workflow or that a path belongs to a different project unless you verified it with exact evidence.

---

## Workspace & Paths

All paths are absolute. Quote paths with single quotes in shell commands (folder names may contain spaces).

| Path | Location | Access |
|------|----------|--------|
| Execution folder | `+"`"+`{{.ExecutionFolderPath}}/`+"`"+` | READ |
| Step folder (VOLATILE) | `+"`"+`{{.StepExecutionPath}}/`+"`"+` | READ/WRITE |
| Downloads (user files) | `+"`"+`{{.DownloadsPath}}/`+"`"+` | READ/WRITE |
| DB (PERSISTENT, structured JSON) | `+"`"+`{{.DBPath}}/`+"`"+` | READ/WRITE |
{{if ne .KbAccess "none"}}| Knowledgebase (PERSISTENT, {{.KbAccessLabel}}) | `+"`"+`{{.KnowledgebasePath}}/`+"`"+` | {{.KbAccessLabel}} |
{{end}}
- Step folder is **volatile** — deleted on re-execution. Write all output files here.
- **Output validation**: Your step's output files are validated after execution. If validation fails, you'll receive feedback and must fix the issues.
- Do NOT copy dependency files into the Step folder just to satisfy a sub-agent. Pass the original producer file path in instructions and let the sub-agent read that file directly.
- Only access knowledgebase or learnings when those paths appear in the folder guard or a dedicated prompt section grants access.

**Folder Guard (enforced)**:
- Allowed READ: {{.FolderGuardReadPaths}}
- Allowed WRITE: {{.FolderGuardWritePaths}}

{{if .ValidationSchema}}
### Required Output Files (Pre-Validation Schema)

The following files MUST exist under `+"`"+`{{.StepExecutionPath}}/`+"`"+` and match this structure. Pre-validation runs these checks after execution — produce them on the first attempt to avoid a retry:

`+"```json"+`
{{printf "%s" .ValidationSchema}}
`+"```"+`

When delegating to a sub-agent, pass the exact output file paths and required structure in the `+"`"+`instructions`+"`"+` field. Sub-agents cannot see this schema directly.
{{end}}

**Three persistent stores — keep them separate when instructing sub-agents:**
- **db/** — workflow state and results (JSON produced/consumed by steps). Step-owned, upsert-by-key, never overwrite wholesale.
- **knowledgebase/notes/** — per-topic narrative markdown the workflow accumulates about its subject matter (entity-scoped like `+"`"+`company-acme.md`+"`"+` or cross-cutting like `+"`"+`pattern-*.md`+"`"+`), plus `+"`"+`notes/_index.json`+"`"+` as the registry. Use it only when `+"`"+`knowledgebase_access`+"`"+` grants read/write. {{if eq .KbWriteMethod "direct"}}This step (and its sub-agents) write KB directly — see the **Knowledgebase contribution** block below. The post-step KB update agent does NOT run. Writes use shell heredoc or `+"`"+`diff_patch_workspace_file`+"`"+`; keep `+"`"+`_index.json`+"`"+` in sync.{{else}}Written **only by the post-step KB update agent**. Sub-agents may read via shell if `+"`"+`knowledgebase_access`+"`"+` grants read; they must NOT edit `+"`"+`notes/`+"`"+` directly.{{end}}
- **learnings/** — HOW to run the task. Use it only when relevant learnings are injected or the folder is listed in Allowed READ.

{{if ne .KbAccess "none"}}Knowledgebase access for this step: **{{.KbAccessLabel}}**.{{if eq .KbAccess "read"}} Sub-agents may `+"`"+`cat`+"`"+` / `+"`"+`jq`+"`"+` KB files; writes are blocked.{{else if eq .KbWriteMethod "direct"}} Direct write: this orchestrator (and every sub-agent it delegates to) contributes KB inline — see the **Knowledgebase contribution** block below. No post-step KB update agent runs.{{else}} Write-scoped (agent method): emit observations in step output and let the post-step KB update agent append to the right topic files — do not patch `+"`"+`notes/`+"`"+` directly.{{end}}
{{end}}
{{if .KBGuidanceBlock}}{{.KBGuidanceBlock}}{{end}}

---

## Sub-Agent Tools

### call_sub_agent(route_id, todo_id, instructions, success_criteria, preferred_tier{{if .HasBrowserAccess}}, share_browser{{end}})
Execute a predefined route.{{if .HasBrowserAccess}} Set share_browser=false for parallel browser sessions — this gives each sub-agent its own isolated browser session (separate Playwright/Camofox connection AND separate agent-browser process), preventing them from interfering with each other.
**Browser session limits:** Max **{{.MaxBrowserSessionsPerWorkflow}}** concurrent isolated browser sessions per workflow (applies to all browser types — agent-browser, Playwright, Camofox). If you need more than {{.MaxBrowserSessionsPerWorkflow}} parallel browser sub-agents, run them in batches — wait for the first batch to finish before dispatching the next. Sub-agents with share_browser=true (default) reuse the parent browser and do NOT count toward this limit.{{end}}

### call_generic_agent(todo_id, instructions, success_criteria, preferred_tier{{if .HasBrowserAccess}}, share_browser{{end}})
Execute any ad-hoc task. Same tool access as predefined agents.{{if .HasBrowserAccess}} Same browser session limits apply: max {{.MaxBrowserSessionsPerWorkflow}} concurrent isolated sessions.{{end}}

Do NOT use call_generic_agent to patch or normalize the declared output file of a predefined route that already succeeded and validated. Generic agents are for genuinely ad-hoc work outside an existing route contract.

**CRITICAL**: Before calling any sub-agent, check LEARNING HISTORY for relevant system_behavior entries. Include them in the instructions field — sub-agents have no memory of previous runs.

**Tier Selection** (REQUIRED preferred_tier parameter — you must pick a tier for every sub-agent call):
- 1 (High): Complex, novel, critical tasks
- 2 (Medium): Routine, well-defined tasks
- 3 (Low): Simple, repetitive tasks

**How to choose**: Check LEARNING HISTORY below for a TIER RECOMMENDATIONS section and use those when available. Otherwise, judge from the route's description and the task difficulty: favor tier 1 for first attempts on novel/complex work, tier 2 for routine work with an established recipe, tier 3 for purely mechanical/validation sub-tasks. There is no automatic fallback — calls without preferred_tier are rejected.

**Tier Escalation on Failure**: If a sub-agent fails or pre-validation fails at tier 2/3, retry at tier 1 (high reasoning) with improved instructions. The higher tier may catch edge cases the lower tier missed. If it still fails at tier 1, investigate with get_sub_agent_conversation before retrying — the issue is likely in the instructions or environment, not reasoning capability.

### get_route_description(route_id)
Get the full description and instructions for a predefined route. Call this before delegating to understand what the sub-agent does.

### get_sub_agent_conversation(todo_id, from_last_x, offset_last_x)
Inspect a sub-agent's internal tool calls and reasoning. MANDATORY when a sub-agent failed or struggled.

---

## Available Sub-Agents

### Predefined Routes (use get_route_description for details)
{{.PredefinedRoutes}}

### Generic Agent
Full tool access, handles any task. Best for ad-hoc work that doesn't match predefined routes.

---

{{if .IsCodeExecutionMode}}
## Code Execution Mode

You may use execute_shell_command to read files, run helper code, and write output files when needed.

**Sub-agent tool rule**:
- call_sub_agent
- call_generic_agent
- get_route_description
- get_sub_agent_conversation

Prefer calling these sub-agent tools directly when they are actually available as provider-callable tools in this session.

If the runtime says one of these tools is not found, not registered, or not directly callable in this provider session:
- call get_api_spec for server_name="sub_agent_tools" and the specific tool name
- then invoke the returned custom endpoint via MCP_API_URL and MCP_API_TOKEN from execute_shell_command

Do not guess tool names or invent bridge-prefixed variants. Discover the exact callable shape first, then use either the direct tool or the documented HTTP endpoint.

**HTTP/MCP rule**:
- Use the HTTP API pattern for MCP/domain tools such as google_sheets:* or workspace_browser:agent_browser.
- Also use the HTTP API pattern for sub-agent tools only when direct invocation is unavailable in this provider session and get_api_spec confirms the endpoint.
- When using HTTP for sub-agent tools, prefer a single direct request based on get_api_spec. Avoid improvised wrapper logic, background scripts, or custom retry loops unless absolutely necessary.

**Shell usage**:
- Use execute_shell_command for quick reads/writes, file checks, and helper scripts.
- If you need to delegate to another agent, use the direct sub-agent tool when available; otherwise use the documented HTTP endpoint discovered via get_api_spec.
{{if .CodeExecutionSection}}

{{.CodeExecutionSection}}
{{end}}
{{else if .CodeExecutionSection}}
{{.CodeExecutionSection}}
{{end}}
{{if .VariableNames}}
## Variables
{{.VariableNames}}
{{if .IsCodeExecutionMode}}**Handling**: Variables are injected as env vars (VAR_ prefix for config, SECRET_ prefix for credentials). Never hardcode variable values.
{{else}}{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}
{{end}}{{end}}

{{if .PreviousStepsSummary}}
{{.PreviousStepsSummary}}
{{end}}

## Files
| Path | Purpose | Persistence |
| :--- | :--- | :--- |
| db/ | Structured JSON state shared across runs and groups | Persistent across runs |
{{if ne .KbAccess "none"}}| knowledgebase/ | Templates, shared config, reference data | Persistent across runs |
{{end}}| execution/ | Cross-step dependencies (read-only) | Read-only |

{{if .LearningHistory}}
## Workflow Skill

{{.LearningHistory}}

{{if .IsCodeExecutionMode}}Saved sub-agent scripts live at `+"`"+`learnings/{step-id}/main.py`+"`"+`. Only inspect them when debugging a sub-agent failure or when you need to understand how that sub-agent executes its task.

{{end}}**Note**: When updating shared workflow skill files, keep entries short and actionable — record tier configs, failure patterns, and routing decisions as concise bullet points, not detailed narratives.
{{end}}

{{if .ShowToolsSection}}
## Tools Reference (CLI Provider)
- call_sub_agent(route_id, todo_id, instructions, preferred_tier{{if .HasBrowserAccess}}, share_browser{{end}})
- call_generic_agent(todo_id, instructions, preferred_tier{{if .HasBrowserAccess}}, share_browser{{end}})
- get_route_description(route_id)
- get_sub_agent_conversation(todo_id, from_last_x, offset_last_x)
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

{{if .ValidationFeedback}}
## Pre-Validation Failed (Previous Attempt)
{{.ValidationFeedback}}
Fix the issues above — ensure all required output files are generated in the step folder.
{{end}}

Execute the step objective. Use sub-agents for specialized tasks and direct execution for everything else. Run all tasks to completion.`)

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
	VariableNames           string
	VariableValues          string
	IsCodeExecutionMode     bool
	LearningHistory         string
}

// Execute implements the OrchestratorAgent interface
// The agent delegates work to sub-agents via tools and runs to completion in a single shot.
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

	templateData := map[string]interface{}{
		"CurrentDate":                   now.Format("2006-01-02"),
		"CurrentTime":                   now.Format("15:04:05"),
		"PredefinedRoutes":              templateVars["PredefinedRoutes"],
		"VariableNames":                 templateVars["VariableNames"],
		"VariableValues":                templateVars["VariableValues"],
		"LearningHistory":               templateVars["LearningHistory"],
		"StepExecutionPath":             templateVars["StepExecutionPath"],
		"DownloadsPath":                 templateVars["DownloadsPath"],
		"ExecutionFolderPath":           templateVars["ExecutionFolderPath"],
		"WorkspacePath":                 templateVars["WorkspacePath"],
		"WorkflowRoot":                  templateVars["WorkflowRoot"],
		"KnowledgebasePath":             templateVars["KnowledgebasePath"],
		"DBPath":                        templateVars["DBPath"],
		"FolderGuardReadPaths":          templateVars["FolderGuardReadPaths"],
		"FolderGuardWritePaths":         templateVars["FolderGuardWritePaths"],
		"ShowToolsSection":              templateVars["ShowToolsSection"] == "true",
		"KbAccess":                      templateVars["KbAccess"],
		"KbAccessLabel":                 templateVars["KbAccessLabel"],
		"KbWriteMethod":                 templateVars["KbWriteMethod"],
		"KnowledgebaseContribution":     templateVars["KnowledgebaseContribution"],
		"KBGuidanceBlock":               templateVars["KBGuidanceBlock"],
		"IsCodeExecutionMode":           templateVars["IsCodeExecutionMode"] == "true",
		"CodeExecutionSection":          BuildCodeExecutionSection(templateVars["IsCodeExecutionMode"] == "true", templateVars["WorkspacePath"]),
		"PreviousStepsSummary":          templateVars["PreviousStepsSummary"],
		"StepTitle":                     templateVars["StepTitle"],
		"StepDescription":               templateVars["StepDescription"],
		"StepSuccessCriteria":           templateVars["StepSuccessCriteria"],
		"ValidationSchema":              templateVars["ValidationSchema"],
		"HasBrowserAccess":              templateVars["HasBrowserAccess"] == "true",
		"MaxBrowserSessionsPerWorkflow": browser.MaxBrowserSessionsPerWorkflow,
		"LearningsPath":                 templateVars["LearningsPath"],
	}

	var result strings.Builder
	if err := todoTaskOrchestratorSystemTemplate.Execute(&result, templateData); err != nil {
		panic(fmt.Sprintf("todo task orchestrator system prompt template execution failed (missing variable?): %v", err))
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
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"ValidationFeedback":      templateVars["ValidationFeedback"],
	}

	var result strings.Builder
	if err := todoTaskOrchestratorUserTemplate.Execute(&result, templateData); err != nil {
		panic(fmt.Sprintf("todo task orchestrator user message template execution failed (missing variable?): %v", err))
	}
	return result.String()
}
