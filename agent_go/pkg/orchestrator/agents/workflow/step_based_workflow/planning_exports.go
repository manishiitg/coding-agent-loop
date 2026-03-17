package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// planningChatSystemTemplate is the planning prompt variant for chat mode.
// Key difference: no human_feedback requirement — user chats directly.
var planningChatSystemTemplate = MustRegisterTemplate("planningChatSystem", `## 🤖 ROLE: Planning Agent
**Task**: Design or refine structured execution plans ('plan.json').
**Context**: Workspace: {{.WorkspacePath}} | Date: {{.CurrentDate}} {{.CurrentTime}}

## ⚠️ PROTOCOL
1. **Conversational**: Discuss proposed changes with the user. Apply changes when they agree.
2. **One Step, One Folder**: Each step has write access ONLY to its own folder ('execution/step-{X}/'). Browser downloads (via Playwright) are automatically saved to 'execution/Downloads/' — agents can reference downloaded files from there.
3. **Verifiable Evidence**: Success criteria MUST require artifacts (files, data counts) that prove work was done—not just status flags.
4. **Stable IDs**: Keep existing 'id' values stable. Only generate new IDs for truly new steps.
5. **Context Flow**: dependencies must reference PRIOR step outputs ('file_name.json', never paths).
6. **No Spawning**: Never replace {{"{{VARIABLE_NAME}}"}} placeholders with values.

---

## 🏗️ STEP DESIGN
- **Regular**: Standard task. 'context_output' is the result file.
- **Decision**: Execute a step, then route based on evidence in context (if_true/if_false).
- **Todo Task**: Manages a dynamic todo list with trackable tasks. Main orchestrator creates/assigns tasks, then delegates to predefined sub-agents (with learning) or generic agent (no learning). Use when: work can be broken into trackable tasks, multiple specialized agents needed, or detailed progress tracking required.
- **Routing**: N-way LLM-based routing. Evaluates a routing_question and selects one of N routes (each with route_id + next_step_id). Two modes: (1) Execute-then-route: has description/success_criteria, executes first then routes; (2) Pure routing: no description, evaluates prior context to pick a route.
- **Human Input**: Asks a question to the user and blocks until they respond. Supports response types: 'text' (free-form), 'yesno' (approve/reject), 'multiple_choice' (pick from options). Can store response in a variable via 'variable_name'. Routes based on response: 'if_yes_next_step_id'/'if_no_next_step_id' for yesno, 'option_routes' for multiple choice, 'next_step_id' as fallback.
- **Human + Routing Pattern**: When the user needs to provide input that determines the workflow path, place a 'human_input' step BEFORE a 'routing' step. The routing step's LLM automatically sees human feedback as CRITICAL context and routes based on the user's answer. Do NOT use a routing step alone when human input is needed — routing steps are LLM-only and never ask the user.

### 🎯 PREFER TODO TASK FOR MULTI-STEP WORK
**Default to todo_task** when a step involves multiple distinct sub-tasks (e.g., "process 3 reports", "handle login + extraction + validation"). Benefits:
- Each sub-agent has **independent learnings** — patterns accumulate separately, so each task improves independently over runs.
- Sub-agents can have **different tools, servers, skills, and LLM configs** — a login sub-agent can use browser tools while a data processing sub-agent uses code execution.
- **Parallel execution** — todo_task supports running sub-agents in parallel (configurable per step).
- **Individual debugging** — each sub-agent can be re-run, analyzed, and optimized independently via the workshop.
- **Granular validation** — each sub-agent has its own validation schema and success criteria.

**When NOT to use todo_task**: Simple steps with a single focused task (one tool call, one output file). These are better as regular steps.

**Rule of thumb**: If you're writing a step description with 3+ distinct actions (e.g., "First do X, then do Y, then do Z"), it should probably be a todo_task with sub-agents for X, Y, and Z instead.

{{if eq .UseKnowledgebase "true"}}### 📁 Persistent Storage (Knowledgebase)
- **knowledgebase/**: Persistent folder at workspace root. Never deleted across runs.
- **How to Use**: Use for global templates, reference data, or configurations shared across ALL runs. Design steps to read from here for persistent context. Use 'knowledgebase/file.ext' in descriptions.
{{end}}

### 📄 JSON FILE STRUCTURE BEST PRACTICES
**CRITICAL**: Keep JSON context output files SMALL (< 100KB). Large JSON files cause parsing failures and performance issues.

**DO**:
- Store structured data in JSON: counts, IDs, status, file references, brief summaries (< 1KB per field)
- For large text content (> 1KB), create a separate markdown file and reference it: {"details_file": "step_1_details.md"}
- Example good structure: {"status": "completed", "count": 5, "files": ["file1.md"], "summary": "Brief summary", "details_file": "step_1_details.md"}

**DON'T**:
- Put large text content directly in JSON fields (descriptions, logs, content > 1KB)
- Create JSON files > 100KB - they will fail to load during pre-validation

### 🔍 Validation Schemas
Every step MUST have a 'validation_schema' to enable fast code-based pre-validation.
**CRITICAL for todo_task steps**: Validation passing is the PRIMARY completion signal. The step completes when validation passes.

**Structure:**
'''json
{
  "validation_schema": {
    "files": [
      {
        "file_name": "output.json",
        "must_exist": true,
        "json_checks": [
          {
            "path": "$.field_name",
            "must_exist": true,
            "value_type": "string",
            "min_length": 1,
            "pattern": "^[a-z]+$"
          }
        ]
      }
    ]
  }
}
'''

**Available JSON checks:**
- 'path': JSONPath to the field (e.g., "$.status", "$.items[0].name", "$.data[*].id")
- 'must_exist': Field must exist (boolean)
- 'value_type': "string", "number", "boolean", "array", "object"
- 'min_length' / 'max_length': For strings and arrays
- 'min_value' / 'max_value': For numbers
- 'pattern': Go regex for string format (e.g., "^\\d{4}-\\d{2}-\\d{2}$")
- 'consistency_check': Compare fields (type: "equals", "greater_than", "less_than", "array_length", "in_array")

---

{{if .VariableNames}}
## 🔑 VARIABLES
{{.VariableNames}}
{{end}}

## 📄 CURRENT PLAN
{{if .ExistingPlanJSON}}{{.ExistingPlanJSON}}{{else}}No plan exists yet. Help the user create one.{{end}}

---

{{if .IsCodeExecutionMode}}
## 🛠️ AVAILABLE TOOLS

### Workflow Tools (use these to modify the plan)

#### Add Steps
- 'add_regular_step' — Add a standard execution step
- 'add_decision_step' — Add an execute-then-route step
- 'add_human_input_step' — Add a step that asks the user a question and blocks for input
- 'add_routing_step' — Add an N-way LLM routing step
- 'add_todo_task_step' — Add a todo-task orchestration step with sub-agents

#### Update Steps
- 'update_regular_step(existing_step_id, ...)' — Update fields of a regular step
- 'update_decision_step(existing_step_id, ...)' — Update a decision step
- 'update_routing_step(existing_step_id, ...)' — Update a routing step
- 'update_human_input_step(existing_step_id, ...)' — Update a human input step
- 'update_todo_task_step(existing_step_id, ...)' — Update a todo task step
- 'update_validation_schema(existing_step_id, validation_schema)' — Update a step's validation schema
- 'update_success_criteria(existing_step_id, success_criteria)' — Update a step's success criteria

#### Delete Steps
- 'delete_plan_steps(step_ids[])' — Delete steps by their IDs

#### Todo Task Route Tools
- 'add_todo_task_route(parent_step_id, new_route)' — Add a sub-agent route to a todo task step
- 'update_todo_task_route(parent_step_id, existing_route_id, ...)' — Update a sub-agent route
- 'delete_todo_task_route(parent_step_id, deleted_route_id)' — Remove a sub-agent route

#### Variable Tools
- 'extract_variables(text)' — Identify hard-coded values (URLs, IDs, credentials) to extract as variables
- 'update_variable(action, existing_variable_name, ...)' — Add, update, or delete a variable in variables.json

### Workspace Tools
- 'execute_shell_command(command)' — Run shell commands using full workspace-relative paths
{{end}}

## 📤 OUTPUT RULES
- **Changes**: Discuss proposed changes with the user → Get agreement → Execute tools.
- **Questions**: Respond conversationally if clarification is needed.
- **Validation**: After any change, verify forward-only context flow and ID stability.

*No placeholders. No duplicate steps. No circular dependencies.*

{{"{{TOOL_STRUCTURE}}"}}`)

// ---------------------------------------------------------------------------
// Chat-mode system prompt templates for debugger phases
// Key difference from orchestrator versions: no human_feedback requirement,
// conversational style, agent reads files on demand via workspace tools.
// ---------------------------------------------------------------------------

var executionDebuggerChatTemplate = MustRegisterTemplate("executionDebuggerChatSystem", `# Execution Debugger (Chat Mode)

## 🤖 ROLE
You are a **read-only** execution analysis assistant. Help the user understand what happened during workflow execution.

## ⚠️ RULES
1. **Read-Only**: You MUST NOT modify any files. You have no write access or plan modification tools.
2. **Answer Directly**: For general questions, answer from the plan context below.
3. **Read Files Only When Needed**: Only read execution logs if user asks about specific failures or "why did X happen".
4. **Conversational**: Ask follow-up questions if the user's query is ambiguous.

## 📋 CONTEXT
- **Workspace**: {{.WorkspacePath}}
- **Run folder**: Check 'runs/' directory for available iterations. Ask the user which run to analyze if unclear.

### Current Plan
{{if .ExistingPlanJSON}}`+"`"+`json
{{.ExistingPlanJSON}}
`+"`"+`{{else}}No plan provided. Read it from 'planning/plan.json'.{{end}}

## 📁 FILE LOCATIONS
- **Plan file**: '{{.WorkspacePath}}/planning/plan.json'
- **Runs**: '{{.WorkspacePath}}/runs/' — list to find available iterations
- **Execution outputs**: '{{.WorkspacePath}}/runs/{iteration}/execution/step-{X}/'
- **Validation logs**: '{{.WorkspacePath}}/runs/{iteration}/logs/step-{X}/validation-{N}.json'
- **Execution logs**: '{{.WorkspacePath}}/runs/{iteration}/logs/step-{X}/execution/'
- **Progress**: '{{.WorkspacePath}}/runs/{iteration}/execution/steps_done.json'
- **Conditional evaluations**: '{{.WorkspacePath}}/runs/{iteration}/logs/step-{X}/conditional-evaluation.json'
- **Decision evaluations**: '{{.WorkspacePath}}/runs/{iteration}/logs/step-{X}/decision-evaluation.json'
- **Routing evaluations**: '{{.WorkspacePath}}/runs/{iteration}/logs/step-{X}/routing-evaluation.json'
- **Orchestration routing**: '{{.WorkspacePath}}/runs/{iteration}/logs/step-{X}/orchestration-execution.json' (JSONL)
- **Todo task progress**: '{{.WorkspacePath}}/runs/{iteration}/execution/step-{X}/tasks.md'

## 📖 STEP FOLDER NAMING
- Regular steps: 'step-{X}/' (X = 1-based)
- Conditional branches: 'step-{X}-if-true-{idx}/', 'step-{X}-if-false-{idx}/'
- Decision steps: 'step-{X}-decision/'
- Sub-agents: 'step-{X}-sub-agent-{idx}/'
- Generic agents: 'step-{X}-generic-agent-{idx}/'

{{"{{TOOL_STRUCTURE}}"}}`)

var evaluationBuilderChatTemplate = MustRegisterTemplate("evaluationBuilderChatSystem", `# Evaluation Builder (Chat Mode)

## 🤖 ROLE
You are an evaluation plan designer and debugger. Help the user create, review, and refine evaluation plans — and analyze results from past evaluation runs to improve criteria.

## ⚠️ RULES
1. **Conversational**: Discuss proposed changes with the user. Apply changes when they agree.
2. **Answer Directly**: For general questions, answer from the context below.
3. **Read Files Only When Needed**: Only read logs/files if user asks for deep analysis.
4. **Concrete Criteria**: Evaluation criteria must be specific and file-verifiable.
5. **Scoring**: Use 0-10 scale. Define what constitutes each score range for clarity.

## 📋 CONTEXT
- **Workspace**: {{.WorkspacePath}}

### Execution Plan
{{if .ExecutionPlanJSON}}` + "`" + `json
{{.ExecutionPlanJSON}}
` + "`" + `{{else}}No execution plan found. Read it from 'planning/plan.json'.{{end}}

### Evaluation Plan
{{if .EvaluationPlanJSON}}` + "`" + `json
{{.EvaluationPlanJSON}}
` + "`" + `{{else}}No evaluation plan exists yet. Help the user create one using the evaluation modification tools.{{end}}

{{if .EvaluationReportJSON}}### Latest Evaluation Report
` + "`" + `json
{{.EvaluationReportJSON}}
` + "`" + `{{end}}

## 📁 FILE LOCATIONS
- **Evaluation Plan**: '{{.WorkspacePath}}/evaluation/evaluation_plan.json'
- **Evaluation Reports**: '{{.WorkspacePath}}/evaluation/runs/{runFolder}/evaluation_report.json'
- **Execution outputs**: '{{.WorkspacePath}}/runs/{iteration}/execution/'
- **Learnings**: '{{.WorkspacePath}}/evaluation/learnings/'

## 🏗️ EVAL STEP DESIGN
Each evaluation step checks one execution step's output:
- **step_id**: Which execution step to evaluate
- **evaluation_criteria**: What to check (be specific — reference file names, expected fields, formats)
- **pre_validation**: Optional code-based checks (file existence, JSON schema) that run before LLM scoring
- **scoring**: 0-10 scale with clear rubric for each range

## 📖 ANALYSIS GUIDE (when evaluation report is available)
- **Low Scores (< 5)**: Read 'reasoning' in the report. Check if criteria were too vague or output files were missing.
- **Criteria Issues**: If reasoning says "criteria too vague", make success_criteria more specific with exact file/field references.
- **Missing Evidence**: If reasoning says "file not found", verify the step checks for the correct output file names.
- **Score Inflation**: If all scores are 8-10 but outputs look mediocre, tighten the criteria.

## ⚙️ AGENT EXECUTION MODES
Each evaluation step runs as an agent. Choose the right mode via **update_step_config(step_id, use_code_execution_mode, use_tool_search_mode)**:

- **Simple mode** (default): Agent calls MCP tools directly. Best for straightforward checks (read a file, verify a field).
- **Code Execution mode** (use_code_execution_mode=true): Agent writes Python code to call tools programmatically. **Use when**:
  - The eval step needs to parse/compare multiple files or run data transformations
  - Complex validation logic (e.g., diff two outputs, compute metrics, check row counts)
  - Deterministic checks that benefit from Python (regex, JSON parsing, math)
- **Tool Search mode** (use_tool_search_mode=true): Agent discovers tools dynamically at runtime. Best when the eval step needs to use tools that aren't known at build time.

**Default**: Simple mode works for most eval steps since they typically read outputs and verify criteria.

## 🛠️ TOOLS
### Evaluation Plan
- **add_evaluation_step, update_evaluation_step, delete_evaluation_steps** — Modify the evaluation plan

### Execution & Optimization
- **execute_step(step_id)** — Run a single eval step in background
- **query_step(execution_id)** — Check status of a running step
- **generate_learnings(step_id, guidance?)** — Generate learnings from eval step runs
- **optimize_step(step_id, focus?)** — Analyze and optimize an eval step
- **analyze_step(step_id)** — Get optimization suggestions for an eval step
- **update_step_config(step_id, ...)** — Update eval step config (mode, LLM, learning_mode, etc.)
- **run_full_evaluation(target_run_folder)** — Run ALL eval steps + scoring against a target execution run (e.g., 'iteration-1'). Generates evaluation_report.json.
- **list_runs** — List available execution runs to evaluate
- **list_steps** — List all eval steps with their config

Discuss changes with the user before applying them.

{{"{{TOOL_STRUCTURE}}"}}`)

// PhaseChatSystemPrompt generates the system prompt for any chat-compatible phase.
// Dispatches to the correct template based on phaseId.
func PhaseChatSystemPrompt(phaseId string, templateVars map[string]string) string {
	now := time.Now()
	templateData := map[string]interface{}{
		"WorkspacePath":    templateVars["WorkspacePath"],
		"ExistingPlanJSON": templateVars["ExistingPlanJSON"],
		"VariableNames":    templateVars["VariableNames"],
		"CurrentDate":      now.Format("2006-01-02"),
		"CurrentTime":      now.Format("15:04:05"),
	}

	var tmpl = planningChatSystemTemplate // default
	switch phaseId {
	case "planning":
		// Planning also needs these extra fields
		templateData["Objective"] = templateVars["Objective"]
		templateData["ExecutionWorkspacePath"] = fmt.Sprintf("%s/execution", templateVars["WorkspacePath"])
		templateData["IsCodeExecutionMode"] = templateVars["IsCodeExecutionMode"] == "true"
		templateData["UseKnowledgebase"] = templateVars["UseKnowledgebase"]
		tmpl = planningChatSystemTemplate
	case "execution-qa":
		tmpl = executionDebuggerChatTemplate
	case "evaluation-builder":
		templateData["EvaluationPlanJSON"] = templateVars["EvaluationPlanJSON"]
		templateData["EvaluationReportJSON"] = templateVars["EvaluationReportJSON"]
		templateData["ExecutionPlanJSON"] = templateVars["ExistingPlanJSON"]
		tmpl = evaluationBuilderChatTemplate
	case "workflow-builder":
		// Use the full workshop system template (same as orchestrator mode)
		// so the chat agent gets all plan design guidance, optimization tips, etc.
		templateData["PlanJSON"] = templateVars["ExistingPlanJSON"]
		templateData["RunFolder"] = templateVars["RunFolder"]
		templateData["StepConfigSummary"] = templateVars["StepConfigSummary"]
		templateData["ProgressSummary"] = templateVars["ProgressSummary"]
		templateData["GroupInfo"] = templateVars["GroupInfo"]
		templateData["UseKnowledgebase"] = templateVars["UseKnowledgebase"]
		templateData["UserRequest"] = "" // Not applicable in chat mode — user messages come via conversation
		templateData["EvaluationPlanJSON"] = templateVars["EvaluationPlanJSON"]
		templateData["EvaluationReportJSON"] = templateVars["EvaluationReportJSON"]
		tmpl = interactiveWorkshopSystemTemplate
	case "human-assisted-execution":
		// Execution-only template — same tools but no optimization/plan-modification guidance
		templateData["PlanJSON"] = templateVars["ExistingPlanJSON"]
		templateData["RunFolder"] = templateVars["RunFolder"]
		templateData["ProgressSummary"] = templateVars["ProgressSummary"]
		templateData["UseKnowledgebase"] = templateVars["UseKnowledgebase"]
		tmpl = humanAssistedExecutionSystemTemplate
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return "Error executing phase chat system prompt template: " + err.Error()
	}
	return result.String()
}

// SchedulerCallbacks provides schedule CRUD operations via callbacks from server.go.
// This avoids importing database/scheduler packages in the workshop package.
type SchedulerCallbacks struct {
	ListSchedules   func(ctx context.Context, presetID string) (string, error)
	CreateSchedule  func(ctx context.Context, presetID, name, cronExpr, timezone string, groupIDs []string) (string, error)
	UpdateSchedule  func(ctx context.Context, jobID, name, cronExpr, timezone string, groupIDs []string, setGroupIDs bool, enabled *bool) (string, error)
	DeleteSchedule  func(ctx context.Context, jobID string) error
	TriggerSchedule func(ctx context.Context, jobID string) (string, error)
	GetScheduleRuns func(ctx context.Context, jobID string, limit int) (string, error)
}

// SkillCallbacks provides skill management operations via callbacks from server.go.
type SkillCallbacks struct {
	ListSkills  func(ctx context.Context) (string, error)
	ImportSkill func(ctx context.Context, githubURL, token string) (string, error)
	DeleteSkill func(ctx context.Context, folderName string) error
}

// WorkshopChatSession holds the per-session controller and step registry for interactive
// workshop in chat mode. Create with NewWorkshopChatSession; clean up with Close().
type WorkshopChatSession struct {
	controller        *StepBasedWorkflowOrchestrator
	StepRegistry      *WorkshopStepRegistry
	sessionCtx        context.Context
	cancelFunc        context.CancelFunc
	toolCallQueryFunc ToolCallQueryFunc
	mainSessionID     string
	config            *WorkshopConfig // Original config for creating fresh controllers
	presetQueryID          string
	schedulerFuncs         *SchedulerCallbacks
	skillFuncs             *SkillCallbacks
	listAvailableSecrets   func(ctx context.Context) ([]string, error)
}

// WorkshopConfig bundles all settings for a workshop session to replicate the
// exact same tool/LLM/browser/image-gen setup as normal workflow execution.
// Built by server.go using the same preset-loading logic as the normal workflow path.
type WorkshopConfig struct {
	WorkspacePath        string
	RunFolder            string
	MCPConfigPath        string
	SelectedServers      []string
	SelectedTools        []string
	UseCodeExecutionMode bool
	UseToolSearchMode    bool
	PreDiscoveredTools   []string
	CustomTools          []llmtypes.Tool
	CustomToolExecutors  map[string]interface{}
	ToolCategories       map[string]string
	LLMConfig            *orchestrator.LLMConfig
	PresetLearningLLM    *AgentLLMConfig
	PresetPhaseLLM       *AgentLLMConfig
	PresetPlanImprovementLLM *AgentLLMConfig
	UseKnowledgebase     bool
	LLMAllocationMode    string
	TieredConfig         *TieredLLMConfig
	Logger               loggerv2.Logger
	EventBridge          mcpagent.AgentEventListener
	// Session tracking — needed for MCP connection sharing and session cleanup
	SessionID            string
	// Secrets for step execution (merged global + user secrets)
	Secrets              []orchestrator.SecretEntry
	// Skills loaded from preset for skill-based step execution
	SelectedSkills       []string
	// WorkspaceEnvRef holds the env map reference for session-aware workspace executors.
	// When set, code execution mode uses this to get MCP_API_URL with session scoping.
	WorkspaceEnvRef      map[string]string
	// EnabledGroupIDs holds the group IDs selected from the workspace toolbar.
	// When set, the session auto-resolves variable values and run folder for these groups.
	EnabledGroupIDs      []string
	// ToolCallQueryFunc provides live tool call query capability for query_step_tools.
	// Set by server.go which has access to the EventStore.
	ToolCallQueryFunc    ToolCallQueryFunc
	// IsEvaluationMode when true, the controller uses evaluation/ paths for step_config, learnings, etc.
	IsEvaluationMode     bool
	// PresetQueryID is the preset this workshop belongs to (needed for schedule management)
	PresetQueryID        string
	// SchedulerFuncs provides callbacks for schedule CRUD operations.
	// Set by server.go which has access to the database and scheduler service.
	SchedulerFuncs       *SchedulerCallbacks
	// SkillFuncs provides callbacks for skill import/delete operations.
	// Set by server.go which has access to the workspace API.
	SkillFuncs           *SkillCallbacks
	// ListAvailableSecrets returns names of all available secrets (global + user-stored).
	// Used by get_workflow_config to show which secrets can be added.
	ListAvailableSecrets func(ctx context.Context) ([]string, error)
}

// NewWorkshopChatSession creates a WorkshopChatSession using the full tool/LLM config
// from server.go — matching the exact same setup as a normal workflow execution.
func NewWorkshopChatSession(ctx context.Context, cfg *WorkshopConfig) (*WorkshopChatSession, error) {
	logger := cfg.Logger
	logger.Info(fmt.Sprintf("[WORKSHOP] NewWorkshopChatSession: workspace=%s, runFolder=%s, servers=%v",
		cfg.WorkspacePath, cfg.RunFolder, cfg.SelectedServers))
	logger.Info(fmt.Sprintf("[WORKSHOP] Config: tools=%d, executors=%d, categories=%d, codeExec=%v, toolSearch=%v, knowledgebase=%v, llmMode=%s",
		len(cfg.CustomTools), len(cfg.CustomToolExecutors), len(cfg.ToolCategories),
		cfg.UseCodeExecutionMode, cfg.UseToolSearchMode, cfg.UseKnowledgebase, cfg.LLMAllocationMode))
	if cfg.PresetPhaseLLM != nil {
		logger.Info(fmt.Sprintf("[WORKSHOP] presetPhaseLLM=%s/%s", cfg.PresetPhaseLLM.Provider, cfg.PresetPhaseLLM.ModelID))
	}
	if cfg.TieredConfig != nil {
		logger.Info(fmt.Sprintf("[WORKSHOP] tiered: T1=%s/%s T2=%s/%s T3=%s/%s",
			cfg.TieredConfig.Tier1.Provider, cfg.TieredConfig.Tier1.ModelID,
			cfg.TieredConfig.Tier2.Provider, cfg.TieredConfig.Tier2.ModelID,
			cfg.TieredConfig.Tier3.Provider, cfg.TieredConfig.Tier3.ModelID))
	}
	// Log tool names for debugging
	toolNames := make([]string, 0, len(cfg.CustomTools))
	for _, t := range cfg.CustomTools {
		if t.Function != nil {
			toolNames = append(toolNames, t.Function.Name)
		}
	}
	logger.Info(fmt.Sprintf("[WORKSHOP] Tool definitions: %v", toolNames))

	sessionCtx, cancelFunc := context.WithCancel(context.Background())

	controller, err := NewStepBasedWorkflowOrchestrator(
		ctx,
		"",       // provider (unused — LLM comes from preset/step config)
		"",       // model (unused)
		0.7,      // temperature
		"simple", // agentMode
		cfg.SelectedServers,
		cfg.SelectedTools,
		cfg.UseCodeExecutionMode,
		cfg.UseToolSearchMode,
		cfg.PreDiscoveredTools,
		cfg.MCPConfigPath,
		cfg.LLMConfig,
		100, // maxTurns
		logger,
		nil, // tracer
		cfg.EventBridge,
		cfg.CustomTools,
		cfg.CustomToolExecutors,
		cfg.ToolCategories,
		nil, // presetValidationLLM (LLM validation removed)
		cfg.PresetLearningLLM,
		cfg.PresetPhaseLLM,
		nil, // presetAnonymizationLLM (deprecated)
		cfg.PresetPlanImprovementLLM,
		cfg.UseKnowledgebase,
		cfg.TieredConfig,
	)
	if err != nil {
		cancelFunc()
		return nil, fmt.Errorf("failed to create workshop controller: %w", err)
	}

	controller.SetWorkspacePath(cfg.WorkspacePath)

	// Set evaluation mode if configured (uses evaluation/ paths for step_config, learnings, etc.)
	if cfg.IsEvaluationMode {
		controller.isEvaluationMode = true
	}

	// Propagate HTTP session ID for chat history, but NOT the MCP session ID.
	//
	// WHY: Each controller creates its own unique MCP session ID (e.g. "session-group-default-group-...")
	// during initialization. This MCP session ID determines which Playwright/browser connection
	// is reused. When a step agent executes, it applies runtime overrides like --output-dir
	// (to redirect downloads to execution/Downloads/) on the MCP connection keyed by this ID.
	//
	// BUG FIX: Previously we called controller.SetMCPSessionID(cfg.SessionID) here, which
	// overwrote the controller's MCP session ID with the chat's session ID. This caused all
	// step agents to share the chat session's Playwright connection — which was created WITHOUT
	// the --output-dir override. Result: downloads went to the browser's default location
	// instead of execution/Downloads/.
	//
	// FIX: Only propagate HTTP session ID (used for chat history / REST endpoints).
	// The controller keeps its own MCP session ID for isolated Playwright connections.
	if cfg.SessionID != "" {
		controller.SetHTTPSessionID(cfg.SessionID)
		logger.Debug(fmt.Sprintf("[WORKSHOP] Session ID propagation: HTTP=%s, MCP=%s (kept separate for Playwright isolation)",
			cfg.SessionID, controller.GetMCPSessionID()))
		logger.Debug(fmt.Sprintf("[WORKSHOP] MCP session %s will get its own Playwright connection with --output-dir override",
			controller.GetMCPSessionID()))
	}

	// Propagate secrets for step execution
	if len(cfg.Secrets) > 0 {
		controller.SetSecrets(cfg.Secrets)
		logger.Debug(fmt.Sprintf("[WORKSHOP] Set %d secrets", len(cfg.Secrets)))
	}

	// Propagate selected skills
	if len(cfg.SelectedSkills) > 0 {
		controller.SetSelectedSkills(cfg.SelectedSkills)
		logger.Debug(fmt.Sprintf("[WORKSHOP] Set %d skills: %v", len(cfg.SelectedSkills), cfg.SelectedSkills))
	}

	// Propagate workspace env ref for code execution mode
	if cfg.WorkspaceEnvRef != nil {
		controller.SetWorkspaceEnvRef(cfg.WorkspaceEnvRef)
		logger.Debug(fmt.Sprintf("[WORKSHOP] Set workspace env ref (MCP_API_URL=%s)", cfg.WorkspaceEnvRef["MCP_API_URL"]))
	}

	// Set run folder if provided. With per-call group_id support, the run folder
	// can also be set on each execute_step call, so it's OK if empty here.
	if cfg.RunFolder != "" {
		controller.SetSelectedRunFolder(cfg.RunFolder)
		logger.Debug(fmt.Sprintf("[WORKSHOP] Run folder set from session init: %s", cfg.RunFolder))
	}

	// Load variables manifest so execute_step can resolve variable values.
	variablesPath := fmt.Sprintf("%s/variables/variables.json", cfg.WorkspacePath)
	_, existingManifest, varErr := controller.variableManager.checkExistingVariables(ctx, variablesPath)
	if varErr != nil {
		logger.Warn(fmt.Sprintf("[WORKSHOP] Failed to check variables: %v — proceeding without", varErr))
	} else if existingManifest != nil {
		controller.variablesManifest = existingManifest
		logger.Debug(fmt.Sprintf("[WORKSHOP] Loaded variables manifest with %d groups", len(existingManifest.Groups)))

		// Auto-set variable values from the enabled group selected in the toolbar.
		// This ensures execute_step always uses the correct group values without
		// requiring the agent to pass group_id on each call.
		if len(cfg.EnabledGroupIDs) > 0 {
			groupID := cfg.EnabledGroupIDs[0] // Use the first selected group
			groupValues := existingManifest.GetVariableValues(groupID)
			if groupValues != nil {
				controller.variableValues = groupValues
				logger.Info(fmt.Sprintf("[WORKSHOP] Auto-set variable values from toolbar-selected group %q (%d vars)", groupID, len(groupValues)))
			} else {
				logger.Warn(fmt.Sprintf("[WORKSHOP] Toolbar-selected group %q not found in manifest — falling back to base values", groupID))
				vals, loadErr := LoadVariableValues(ctx, controller.BaseOrchestrator, cfg.WorkspacePath, cfg.WorkspacePath)
				if loadErr == nil && vals != nil {
					controller.variableValues = vals
				}
			}
			controller.enabledGroupIDs = cfg.EnabledGroupIDs
		} else if existingManifest.HasGroups() {
			// No group selected from toolbar — use first enabled group as default
			enabledGroups := existingManifest.GetEnabledGroups()
			if len(enabledGroups) > 0 {
				controller.variableValues = enabledGroups[0].Values
				controller.enabledGroupIDs = []string{enabledGroups[0].GroupID}
				logger.Info(fmt.Sprintf("[WORKSHOP] Auto-set variable values from first enabled group %q (%d vars)", enabledGroups[0].GroupID, len(enabledGroups[0].Values)))
			}
		} else {
			// No groups — load base variable values
			vals, loadErr := LoadVariableValues(ctx, controller.BaseOrchestrator, cfg.WorkspacePath, cfg.WorkspacePath)
			if loadErr == nil && vals != nil {
				controller.variableValues = vals
				logger.Info(fmt.Sprintf("[WORKSHOP] Loaded %d base variable values (no groups)", len(vals)))
			}
		}
	}

	// Pre-load the plan so list_steps and get_step_prompts work immediately (best-effort).
	if loadErr := controller.LoadPlanForWorkshop(ctx); loadErr != nil {
		logger.Warn(fmt.Sprintf("[WORKSHOP] Could not pre-load plan (%v) — will retry on first tool call", loadErr))
	}

	return &WorkshopChatSession{
		controller:        controller,
		StepRegistry:      NewWorkshopStepRegistry(),
		sessionCtx:        sessionCtx,
		cancelFunc:        cancelFunc,
		toolCallQueryFunc: cfg.ToolCallQueryFunc,
		mainSessionID:     cfg.SessionID,
		config:            cfg,
		presetQueryID:          cfg.PresetQueryID,
		schedulerFuncs:         cfg.SchedulerFuncs,
		skillFuncs:             cfg.SkillFuncs,
		listAvailableSecrets:   cfg.ListAvailableSecrets,
	}, nil
}

// UpdatePresetLLMConfigs refreshes the controller's preset LLM configs.
// Called when reusing a cached workshop session to pick up any LLM config changes
// the user made in the workflow editor since the session was first created.
func (s *WorkshopChatSession) UpdatePresetLLMConfigs(
	learningLLM, phaseLLM, planImprovementLLM *AgentLLMConfig,
) {
	s.controller.presetLearningLLM = learningLLM
	s.controller.presetPhaseLLM = phaseLLM
	s.controller.presetPlanImprovementLLM = planImprovementLLM
}

// UpdateTieredConfig refreshes the controller's tiered LLM allocation config.
// Called when reusing a cached workshop session to pick up any tiered config changes
// the user made in the workflow editor since the session was first created.
func (s *WorkshopChatSession) UpdateTieredConfig(tieredConfig *TieredLLMConfig) {
	if tieredConfig != nil {
		orchestratorLLMConfig := s.controller.GetLLMConfig()
		var apiKeys *orchestrator.APIKeys
		if orchestratorLLMConfig != nil {
			apiKeys = orchestratorLLMConfig.APIKeys
		}
		s.controller.tierResolver = NewTierResolver(tieredConfig, apiKeys)
	} else {
		s.controller.tierResolver = nil
	}
}

// UpdatePresetSettings refreshes non-LLM controller settings from the preset.
// Called when reusing a cached workshop session to pick up any config changes
// the user made in the workflow editor (MCP servers, tools, knowledgebase, etc.).
// The *Parsed flags indicate whether the JSON field was successfully parsed; if false,
// the existing value is kept to avoid clearing settings on parse failure.
func (s *WorkshopChatSession) UpdatePresetSettings(
	selectedServers []string,
	selectedTools []string, toolsParsed bool,
	useCodeExecutionMode bool,
	useToolSearchMode bool,
	preDiscoveredTools []string, preDiscoveredParsed bool,
	useKnowledgebase bool,
	selectedSkills []string, skillsParsed bool,
	secrets []orchestrator.SecretEntry,
) {
	s.controller.SetSelectedServers(selectedServers)
	if toolsParsed {
		s.controller.SetSelectedTools(selectedTools)
	}
	s.controller.SetUseCodeExecutionMode(useCodeExecutionMode)
	s.controller.SetUseToolSearchMode(useToolSearchMode)
	if preDiscoveredParsed {
		s.controller.SetPreDiscoveredTools(preDiscoveredTools)
	}
	s.controller.useKnowledgebase = useKnowledgebase
	if skillsParsed {
		s.controller.SetSelectedSkills(selectedSkills)
	}
	s.controller.SetSecrets(secrets)
}

// UpdateEnabledGroupIDs refreshes the toolbar-selected group IDs and reloads variable values.
// Called when reusing a cached workshop session to pick up any group selection changes.
func (s *WorkshopChatSession) UpdateEnabledGroupIDs(ctx context.Context, enabledGroupIDs []string) {
	s.controller.enabledGroupIDs = enabledGroupIDs

	// Reload variables manifest from disk (may have changed since session was created)
	variablesPath := fmt.Sprintf("%s/variables/variables.json", s.controller.GetWorkspacePath())
	_, manifest, err := s.controller.variableManager.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		s.controller.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Failed to reload variables: %v", err))
		return
	}
	if manifest != nil {
		s.controller.variablesManifest = manifest
	}

	// Re-resolve variable values from the selected group
	if manifest != nil && len(enabledGroupIDs) > 0 {
		groupID := enabledGroupIDs[0]
		groupValues := manifest.GetVariableValues(groupID)
		if groupValues != nil {
			s.controller.variableValues = groupValues
			s.controller.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Refreshed variable values from group %q (%d vars)", groupID, len(groupValues)))
		} else {
			s.controller.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Group %q not found in manifest during refresh", groupID))
		}
	} else if manifest != nil && manifest.HasGroups() {
		enabledGroups := manifest.GetEnabledGroups()
		if len(enabledGroups) > 0 {
			s.controller.variableValues = enabledGroups[0].Values
			s.controller.enabledGroupIDs = []string{enabledGroups[0].GroupID}
			s.controller.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Refreshed variable values from first enabled group %q", enabledGroups[0].GroupID))
		}
	}
}

// RegisterWorkshopChatTools registers execute_step, query_step, stop_step, list_steps,
// update_step_config, and get_step_prompts on the given agent using the session's controller.
func RegisterWorkshopChatTools(
	mcpAgent *mcpagent.Agent,
	session *WorkshopChatSession,
	logger loggerv2.Logger,
	fullMode bool,
) {
	iwm := &InteractiveWorkshopManager{
		controller:        session.controller,
		stepRegistry:      session.StepRegistry,
		sessionCtx:        session.sessionCtx,
		toolCallQueryFunc: session.toolCallQueryFunc,
		mainSessionID:     session.mainSessionID,
		presetQueryID:          session.presetQueryID,
		schedulerFuncs:         session.schedulerFuncs,
		skillFuncs:             session.skillFuncs,
		listAvailableSecrets:   session.listAvailableSecrets,
	}
	registerInteractiveWorkshopTools(iwm, mcpAgent, logger, fullMode)
}

// Close cancels all background goroutines for this workshop session.
func (s *WorkshopChatSession) Close() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
}

// RegisterRunFullEvaluationTool registers a run_full_evaluation tool that executes all
// evaluation steps and scoring against a target execution run. Runs in background.
func RegisterRunFullEvaluationTool(
	mcpAgent *mcpagent.Agent,
	session *WorkshopChatSession,
	logger loggerv2.Logger,
) {
	if err := mcpAgent.RegisterCustomTool(
		"run_full_evaluation",
		"Run the full evaluation pipeline: execute all evaluation steps against a target execution run, then score each step and generate an evaluation report. Runs in background — you will be notified when complete.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_run_folder": map[string]interface{}{
					"type":        "string",
					"description": "The execution run folder to evaluate (e.g., 'iteration-1'). This is the folder under runs/ whose outputs will be checked.",
				},
			},
			"required": []string{"target_run_folder"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			targetRunFolder, _ := args["target_run_folder"].(string)
			if targetRunFolder == "" {
				return "target_run_folder is required", nil
			}

			cfg := session.config
			if cfg == nil {
				return "session config not available — cannot create evaluation controller", nil
			}

			execID := fmt.Sprintf("eval-full-%s-%d", targetRunFolder, time.Now().UnixNano())
			execCtx, cancel := context.WithCancel(session.sessionCtx)

			// Inject correlation IDs so eval execution events are tagged as sub-agent events.
			// Without this, query_step_tools cannot find tool calls — it matches by correlationID
			// which is only set when ForceCorrelationIDKey is in the context.
			agentSessionID := fmt.Sprintf("workshop-eval-%s-%d", targetRunFolder, time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         fmt.Sprintf("full-eval-%s", targetRunFolder),
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			session.StepRegistry.Register(exec)

			go func() {
				// Create a fresh controller for the full evaluation run
				evalController, err := NewStepBasedWorkflowOrchestrator(
					execCtx,
					"", "", 0.7, "simple",
					cfg.SelectedServers,
					cfg.SelectedTools,
					cfg.UseCodeExecutionMode,
					cfg.UseToolSearchMode,
					cfg.PreDiscoveredTools,
					cfg.MCPConfigPath,
					cfg.LLMConfig,
					100,
					logger,
					nil, // tracer
					cfg.EventBridge,
					cfg.CustomTools,
					cfg.CustomToolExecutors,
					cfg.ToolCategories,
					nil, // presetValidationLLM
					cfg.PresetLearningLLM,
					cfg.PresetPhaseLLM,
					nil, // presetAnonymizationLLM
					cfg.PresetPlanImprovementLLM,
					cfg.UseKnowledgebase,
					cfg.TieredConfig,
				)
				if err != nil {
					exec.mu.Lock()
					exec.Status = WorkshopStepFailed
					exec.Err = fmt.Errorf("failed to create evaluation controller: %w", err)
					exec.mu.Unlock()
					return
				}

				// Propagate HTTP session ID only — do NOT overwrite MCP session ID.
				// Same reasoning as main controller above: eval controller needs its own
				// MCP session ID so its step agents get isolated Playwright connections
				// with correct --output-dir overrides for download path resolution.
				if cfg.SessionID != "" {
					evalController.SetHTTPSessionID(cfg.SessionID)
					logger.Debug(fmt.Sprintf("[WORKSHOP-EVAL] Session ID propagation: HTTP=%s, MCP=%s (kept separate for Playwright isolation)",
						cfg.SessionID, evalController.GetMCPSessionID()))
					logger.Debug(fmt.Sprintf("[WORKSHOP-EVAL] MCP session %s will get its own Playwright connection with --output-dir override",
						evalController.GetMCPSessionID()))
				}
				if len(cfg.Secrets) > 0 {
					evalController.SetSecrets(cfg.Secrets)
				}
				if cfg.WorkspaceEnvRef != nil {
					evalController.SetWorkspaceEnvRef(cfg.WorkspaceEnvRef)
				}

				result, execErr := evalController.ExecuteEvaluationOnly(
					execCtx,
					session.controller.GetObjective(),
					cfg.WorkspacePath,
					targetRunFolder,
				)

				exec.mu.Lock()
				defer exec.mu.Unlock()
				if exec.Status == WorkshopStepCancelled {
					return
				}
				if execErr != nil {
					exec.Status = WorkshopStepFailed
					exec.Err = execErr
				} else {
					exec.Status = WorkshopStepDone
					exec.Result = result
				}
			}()

			return fmt.Sprintf("Full evaluation started for run %q.\nexecution_id: %q\nThis will execute all evaluation steps and generate a scoring report.\nYou will be automatically notified when it completes.", targetRunFolder, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_full_evaluation tool: %v", err))
	}
}

// RegisterEvaluationModificationTools is the exported wrapper for registering evaluation
// modification tools on an MCP agent. Used by server.go for evaluation-builder phase.
func RegisterEvaluationModificationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	moveFile func(context.Context, string, string) error,
) error {
	return registerEvaluationModificationTools(mcpAgent, workspacePath, logger, readFile, writeFile, moveFile)
}

// RegisterPlanModificationTools is the exported wrapper for registering plan modification tools
// on an MCP agent. Used by server.go for workflow phase chat sessions.
func RegisterPlanModificationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	moveFile func(context.Context, string, string) error,
	agentName string,
) error {
	return registerPlanModificationTools(mcpAgent, workspacePath, logger, readFile, writeFile, moveFile, agentName, nil)
}

// PlanningChatSystemPrompt generates the planning system prompt for chat mode.
// Unlike the orchestrator version, this removes the human_feedback requirement
// since the user is chatting directly with the agent.
func PlanningChatSystemPrompt(templateVars map[string]string) string {
	now := time.Now()
	templateData := map[string]interface{}{
		"Objective":              templateVars["Objective"],
		"WorkspacePath":          templateVars["WorkspacePath"],
		"ExecutionWorkspacePath": fmt.Sprintf("%s/execution", templateVars["WorkspacePath"]),
		"ExistingPlanJSON":       templateVars["ExistingPlanJSON"],
		"VariableNames":          templateVars["VariableNames"],
		"IsCodeExecutionMode":    templateVars["IsCodeExecutionMode"] == "true",
		"UseKnowledgebase":       templateVars["UseKnowledgebase"],
		"CurrentDate":            now.Format("2006-01-02"),
		"CurrentTime":            now.Format("15:04:05"),
	}

	var result strings.Builder
	if err := planningChatSystemTemplate.Execute(&result, templateData); err != nil {
		return "Error executing planning chat system prompt template: " + err.Error()
	}
	return result.String()
}

// ReadPlanFromWorkspace reads plan.json from the workspace and returns it as JSON string.
// Returns empty string if plan doesn't exist.
func ReadPlanFromWorkspace(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) string {
	planPath := "planning/plan.json"
	if workspacePath != "" {
		planPath = workspacePath + "/planning/plan.json"
	}
	content, err := readFile(ctx, planPath)
	if err != nil {
		return ""
	}
	// Validate it's valid JSON
	var plan interface{}
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return ""
	}
	return content
}

// ReadVariablesFromWorkspace reads variables.json and returns formatted variable names.
// Returns empty string if variables don't exist.
func ReadVariablesFromWorkspace(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) string {
	varPath := "planning/variables.json"
	if workspacePath != "" {
		varPath = workspacePath + "/planning/variables.json"
	}
	content, err := readFile(ctx, varPath)
	if err != nil {
		return ""
	}

	// Parse the variables manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return ""
	}
	return FormatVariableNames(&manifest)
}
