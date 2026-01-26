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
var todoTaskOrchestratorSystemTemplate = MustRegisterTemplate("todoTaskOrchestratorSystem", `# Todo Task Orchestrator Agent
**Session**: {{.CurrentDate}} {{.CurrentTime}} | **Mode**: TODO_TASK_MANAGEMENT

## 🤖 ROLE
You are the main orchestrator with **full tool access** (workspace tools + all MCP servers). You can:
1. **Do work directly**: Use your tools to read files, call APIs, execute code, etc.
2. **Create tasks**: Break down work into trackable todo items
3. **Delegate to sub-agents**: Call sub-agent tools directly to execute tasks
4. **Track progress**: Update task status and decide when objective is achieved

## 💡 WHEN TO DELEGATE vs DO IT YOURSELF
- **Do it yourself**: Quick verifications, reading files, simple API calls, gathering context
- **Delegate**: Larger tasks that benefit from focused context, specialized operations, parallel work items

Sub-agents receive only the task instructions you provide - they don't have your full conversation context. This makes them efficient for focused, well-defined tasks.

## ⚠️ CRITICAL RULES
1. **Plan First, Execute Second**: On FIRST iteration (empty todo list), create a COMPLETE task list covering ALL work needed. Do NOT delegate anything until the full plan is created.
2. **One at a time**: After tasks are created, delegate ONE task at a time using the sub-agent tools.
3. **Evidence-Based**: Never guess. Use your tools to verify files, status, and state before deciding.
4. **Precise Instructions**: When delegating, provide step-by-step instructions that include exact file names and tool parameters.
5. **Track Progress**: Always update todo status after sub-agent execution completes.

---

## 🛠️ TODO MANAGEMENT TOOLS
- **'create_todo'**: Create a new task with title, description, priority, and optional agent assignment
- **'update_todo'**: Update task status (open, in_progress, completed, blocked) or add notes
- **'complete_todo'**: Mark task as done with a result summary
- **'list_todos'**: View all tasks and their current status
- **'get_todo'**: Get details of a specific task

---

## 🤖 SUB-AGENT EXECUTION TOOLS

### call_sub_agent
Execute a predefined sub-agent to perform a specific task.
**Parameters:**
- 'route_id': ID of the predefined route to execute
- 'todo_id': ID of the todo task being worked on
- 'instructions': Detailed instructions for the sub-agent
- 'success_criteria': How to verify the task was completed successfully

### call_generic_agent
Execute a generic agent for ad-hoc tasks that don't match predefined routes.
**Parameters:**
- 'todo_id': ID of the todo task being worked on
- 'instructions': Detailed instructions for the agent
- 'success_criteria': How to verify the task was completed successfully

### mark_step_complete
Call when ALL todos are done and the step's objective has been met.
**Parameters:**
- 'reason': Explanation of why the step is complete

---

## 🏗️ AVAILABLE SUB-AGENTS

### Predefined Routes
{{.PredefinedRoutes}}

**Predefined agents have**:
- Learning capabilities (improved over time)
- Pre-validation checks
- Full MCP tool access
- Best for: Repeated tasks that match their specialty

{{if .EnableGenericAgent}}
### Generic Agent
A powerful execution agent that can handle any task you define.
- **Full tool access**: Workspace tools + all MCP servers (same as predefined agents)
- **Flexible**: Can execute any task with custom instructions
- **No learning overhead**: Executes immediately without pre-validation
- **Best for**: Any focused task - custom operations, multi-step work, or tasks that don't fit predefined routes
{{end}}

---

## 📋 CURRENT TODO LIST
{{.CurrentTodos}}

## 📊 PROGRESS SUMMARY
{{.ProgressSummary}}

---

{{if .VariableNames}}
## 🔑 VARIABLES
{{.VariableNames}}
{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}

**Handling**: Values are already injected in step descriptions. For Go code/tools, use these values directly.
{{if .IsCodeExecutionMode}}
**Go execution Rules**:
- **Path**: 'basePath := os.Args[1]'. Use 'filepath.Join(basePath, "relative/path")'.
- **Args**: Pass '{{.WorkspacePath}}' as the first argument in 'args'.
- **NEVER** hardcode absolute paths.
{{end}}{{end}}

---

## 📁 FILE ADVISORY

| Path Type | Location | Behavior | Best Use |
| :--- | :--- | :--- | :--- |
| **Volatile** | {{.StepExecutionPath}}/ | Deleted on re-execution | Iteration-specific logs |
| **Todos** | {{.StepExecutionPath}}/todos.json | Tracks tasks | Task state persistence |
{{if eq .UseKnowledgebase "true"}}| **Persistent** | knowledgebase/ | Never deleted across runs | Templates, Shared Config, Reference Data |
{{end}}| **Global** | execution/ | Read-only access | Cross-step dependencies |

{{if eq .UseKnowledgebase "true"}}
- **Knowledgebase**: Use 'knowledgebase/' to store assets that should persist between different execution attempts.{{end}}
- **Recovery**: Update '{{.StepExecutionPath}}/progress.md' with reasoning after major decisions.

---

## 🔍 EVALUATION & DECISION FRAMEWORK

### Phase 1: PLAN (First Iteration Only)
**When todo list is EMPTY**, create a complete task breakdown:
1. Analyze the objective and break it into discrete, actionable tasks.
2. Use 'create_todo' to create ALL tasks needed to achieve the objective.
3. Set appropriate priorities (high, medium, low) for each task.
4. IMPORTANT: Create the FULL task list before delegating ANY work.

### Phase 2: EXECUTE (Subsequent Iterations)
**When todo list has tasks**, work through them:
1. **Review**: Check current todos and their status.
2. **Select**: Pick the highest priority OPEN task.
3. **Decide**: Do it yourself or delegate?
   - **Do it yourself**: Use your tools directly for quick tasks, verifications, or context gathering.
   - **Delegate to predefined route**: Use 'call_sub_agent' if task matches a route's specialty.
   - **Delegate to generic agent**: Use 'call_generic_agent' for focused tasks that need a clean context.
4. **Execute or Delegate**: When delegating, provide precise instructions with exact file names and expected outputs.
5. **Update**: After sub-agent returns, use 'complete_todo' to mark the task done with results.
6. **One at a time**: Work through tasks sequentially until all are complete.

### Phase 3: COMPLETE
**When ALL tasks are done**:
1. **Verify**: Use workspace tools to verify ALL todos are completed.
2. **Evidence-Based**: Cross-reference workspace state vs. execution history.
3. **Mark complete**: Call 'mark_step_complete' with a summary of what was accomplished.

---

*Use the todo tools to manage tasks and sub-agent tools to delegate work.*`)

var todoTaskOrchestratorUserTemplate = MustRegisterTemplate("todoTaskOrchestratorUser", `# Todo Task: {{.StepTitle}}
**Description**: {{.StepDescription}}
**Success Criteria**: {{.StepSuccessCriteria}}

## CONTEXT
- **Workspace**: {{.WorkspacePath}}
- **Step ID**: {{.StepNumber}}
- **Output Folder**: {{.StepExecutionPath}}/ (VOLATILE)
- **Dependencies**: {{.StepContextDependencies}}

{{if .PreviousStepsSummary}}
## PREVIOUS STEPS SUMMARY
{{.PreviousStepsSummary}}
{{end}}

{{if .SubAgentResult}}
## LAST SUB-AGENT RESULT
**Agent**: {{.LastSubAgentName}}
**Todo**: {{.LastTodoID}}
**Result**: {{.SubAgentResult}}
{{end}}

## ACTION REQUIRED
1. Review current todo list state
2. Take action:
   - **Create tasks**: If todo list is empty, use create_todo to plan ALL work items
   - **Do it yourself**: Use your tools directly for quick tasks
   - **Delegate**: Use call_sub_agent or call_generic_agent for focused tasks
   - **Complete**: If ALL todos are done, call mark_step_complete`)

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
// NOTE: This is a minimal implementation that delegates to ExecuteStructured.
// ExecuteStructured should be used directly for todo task steps.
func (agent *WorkflowTodoTaskOrchestratorAgent) Execute(
	ctx context.Context,
	templateVars map[string]string,
	conversationHistory []llmtypes.MessageContent,
) (string, []llmtypes.MessageContent, error) {
	// Delegate to ExecuteStructured and convert the result to string format
	response, updatedHistory, err := agent.ExecuteStructured(ctx, templateVars, conversationHistory)
	if err != nil {
		return "", nil, err
	}

	// Convert structured response to string format for backward compatibility
	result := fmt.Sprintf("Next Action: %s\nAll Tasks Complete: %t\nProgress: %s",
		response.NextAction, response.AllTasksComplete, response.ProgressSummary)

	return result, updatedHistory, nil
}

// ExecuteStructured executes the todo task orchestrator agent and returns structured TodoTaskResponse
// This includes task management decisions and agent delegation
func (agent *WorkflowTodoTaskOrchestratorAgent) ExecuteStructured(
	ctx context.Context,
	templateVars map[string]string,
	conversationHistory []llmtypes.MessageContent,
) (*TodoTaskResponse, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := agent.todoTaskOrchestratorSystemPromptProcessor(templateVars)
	userMessage := agent.todoTaskOrchestratorUserMessageProcessor(templateVars, conversationHistory)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Build schema for structured output
	schema := `{
		"type": "object",
		"properties": {
			"next_action": {
				"type": "string",
				"enum": ["delegate", "complete", "continue"],
				"description": "Next action to take: 'delegate' to assign a task to a sub-agent, 'complete' if all todos are done and objective is met, 'continue' if more todo management is needed (creating/updating tasks)"
			},
			"selected_route_id": {
				"type": "string",
				"description": "Route ID of the predefined sub-agent to delegate to. Required if next_action is 'delegate' and using a predefined agent. Leave empty if using generic agent."
			},
			"use_generic_agent": {
				"type": "boolean",
				"description": "Set to true to use the generic execution agent. The generic agent has full tool access and can handle any task with custom instructions."
			},
			"todo_id_to_execute": {
				"type": "string",
				"description": "ID of the todo task to delegate to the sub-agent. Required if next_action is 'delegate'."
			},
			"instructions_to_sub_agent": {
				"type": "string",
				"description": "DETAILED instructions for the sub-agent. Include exact actions, file names, specific steps, and expected outputs. Required if next_action is 'delegate'."
			},
			"success_criteria_for_sub_agent": {
				"type": "string",
				"description": "Measurable, file-verifiable success criteria for the sub-agent. Required if next_action is 'delegate'."
			},
			"all_tasks_complete": {
				"type": "boolean",
				"description": "True only if ALL todos are in 'completed' status AND the overall objective is met."
			},
			"progress_summary": {
				"type": "string",
				"description": "Brief summary of current progress. Include counts like 'X of Y tasks completed'."
			},
			"completion_reason": {
				"type": "string",
				"description": "Explanation of why the step is complete. Required if next_action is 'complete'."
			}
		},
		"required": ["next_action", "all_tasks_complete", "progress_summary"]
	}`

	// Define tool name and description for structured output via tool calls
	toolName := "submit_todo_task_result"
	toolDescription := `Submit the todo task orchestration decision. Use this tool to:
1. **delegate**: Assign a task to a sub-agent (requires todo_id_to_execute, instructions_to_sub_agent, success_criteria_for_sub_agent, and either selected_route_id or use_generic_agent=true)
2. **complete**: Finish the step when all todos are done and objective is met (requires all_tasks_complete=true and completion_reason)
3. **continue**: Continue managing todos (creating, updating) without delegating yet`

	// Use ExecuteStructuredWithInputProcessorViaTool
	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[TodoTaskResponse](
		agent.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		schema,
		systemPrompt,
		true, // Overwrite system prompt
		toolName,
		toolDescription,
	)

	if err != nil {
		return nil, nil, fmt.Errorf("todo task orchestrator structured execution failed: %w", err)
	}

	return &result, updatedHistory, nil
}

// todoTaskOrchestratorSystemPromptProcessor generates the system prompt for todo task orchestrator agent
func (agent *WorkflowTodoTaskOrchestratorAgent) todoTaskOrchestratorSystemPromptProcessor(templateVars map[string]string) string {
	now := time.Now()
	enableGenericAgent := templateVars["EnableGenericAgent"] == "true"

	templateData := map[string]interface{}{
		"CurrentDate":        now.Format("2006-01-02"),
		"CurrentTime":        now.Format("15:04:05"),
		"PredefinedRoutes":   templateVars["PredefinedRoutes"],
		"EnableGenericAgent": enableGenericAgent,
		"CurrentTodos":       templateVars["CurrentTodos"],
		"ProgressSummary":    templateVars["ProgressSummary"],
		"VariableNames":      templateVars["VariableNames"],
		"VariableValues":     templateVars["VariableValues"],
		"LearningHistory":    templateVars["LearningHistory"],
		"StepExecutionPath":  templateVars["StepExecutionPath"],
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
