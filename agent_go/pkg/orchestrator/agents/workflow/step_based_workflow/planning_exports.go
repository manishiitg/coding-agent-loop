package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
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
2. **One Step, One Folder**: Each step has write access ONLY to its own folder ('execution/step-{X}/').
3. **Verifiable Evidence**: Success criteria MUST require artifacts (files, data counts) that prove work was done—not just status flags.
4. **Stable IDs**: Keep existing 'id' values stable. Only generate new IDs for truly new steps.
5. **Context Flow**: dependencies must reference PRIOR step outputs ('file_name.json', never paths).
6. **No Spawning**: Never replace {{"{{VARIABLE_NAME}}"}} placeholders with values.

---

## 🏗️ STEP DESIGN
- **Regular**: Standard task. 'context_output' is the result file.
- **Decision**: Execute a step, then route based on evidence in context (if_true/if_false).
- **Conditional**: Inspection-only branch (no execution).
- **Todo Task**: Manages a dynamic todo list with trackable tasks. Main orchestrator creates/assigns tasks, then delegates to predefined sub-agents (with learning) or generic agent (no learning). Use when: work can be broken into trackable tasks, multiple specialized agents needed, or detailed progress tracking required.
- **Loop**: Repeat until criteria met (polled progress).
- **Routing**: N-way LLM-based routing. Evaluates a routing_question and selects one of N routes (each with route_id + next_step_id). Two modes: (1) Execute-then-route: has description/success_criteria, executes first then routes; (2) Pure routing: no description, evaluates prior context to pick a route.
- **Human Input**: Asks a question to the user and blocks until they respond. Supports response types: 'text' (free-form), 'yesno' (approve/reject), 'multiple_choice' (pick from options). Can store response in a variable via 'variable_name'. Routes based on response: 'if_yes_next_step_id'/'if_no_next_step_id' for yesno, 'option_routes' for multiple choice, 'next_step_id' as fallback.
- **Human + Routing Pattern**: When the user needs to provide input that determines the workflow path, place a 'human_input' step BEFORE a 'routing' step. The routing step's LLM automatically sees human feedback as CRITICAL context and routes based on the user's answer. Do NOT use a routing step alone when human input is needed — routing steps are LLM-only and never ask the user.

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
- 'add_conditional_step' — Add an if/else branch step (evaluation only, no execution)
- 'add_decision_step' — Add an execute-then-route step
- 'add_loop_step' — Add a step that repeats until a condition is met
- 'add_human_input_step' — Add a step that asks the user a question and blocks for input
- 'add_routing_step' — Add an N-way LLM routing step
- 'add_todo_task_step' — Add a todo-task orchestration step with sub-agents

#### Update Steps
- 'update_regular_step(existing_step_id, ...)' — Update fields of a regular step
- 'update_conditional_step(existing_step_id, ...)' — Update a conditional step
- 'update_decision_step(existing_step_id, ...)' — Update a decision step
- 'update_routing_step(existing_step_id, ...)' — Update a routing step
- 'update_human_input_step(existing_step_id, ...)' — Update a human input step
- 'update_todo_task_step(existing_step_id, ...)' — Update a todo task step
- 'update_validation_schema(existing_step_id, validation_schema)' — Update a step's validation schema
- 'update_success_criteria(existing_step_id, success_criteria)' — Update a step's success criteria

#### Delete Steps
- 'delete_plan_steps(step_ids[])' — Delete steps by their IDs

#### Conditional Branch Tools
- 'convert_step_to_conditional(step_id, condition_question, if_true_steps, if_false_steps)' — Convert a regular step to conditional
- 'convert_conditional_to_regular(step_id)' — Convert a conditional step back to regular
- 'add_branch_steps(parent_step_id, branch_type, new_steps[])' — Add steps to a branch
- 'update_branch_steps(parent_step_id, branch_type, updated_steps[])' — Update steps in a branch
- 'delete_branch_steps(parent_step_id, branch_type, deleted_step_ids[])' — Delete steps from a branch

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

var evaluationDebuggerChatTemplate = MustRegisterTemplate("evaluationDebuggerChatSystem", `# Evaluation Debugger (Chat Mode)

## 🤖 ROLE
You are an evaluation debugging assistant. Help the user improve their evaluation plan based on execution results.

## ⚠️ RULES
1. **Conversational**: Discuss proposed changes with the user. Apply changes when they agree.
2. **Answer Directly**: For general questions, answer from the context below.
3. **Read Files Only When Needed**: Only read logs/files if user asks for deep debugging.
4. **Concrete Criteria**: When updating evaluation criteria, make them specific and file-verifiable.

## 📋 CONTEXT
- **Workspace**: {{.WorkspacePath}}

### Current Plan
{{if .ExistingPlanJSON}}`+"`"+`json
{{.ExistingPlanJSON}}
`+"`"+`{{else}}No plan provided. Read it from 'planning/plan.json'.{{end}}

## 📁 FILE LOCATIONS
- **Evaluation Plan**: '{{.WorkspacePath}}/evaluation/evaluation_plan.json'
- **Evaluation Reports**: '{{.WorkspacePath}}/evaluation/runs/{runFolder}/evaluation_report.json'
- **Execution outputs**: '{{.WorkspacePath}}/runs/{iteration}/execution/'
- **Learnings**: '{{.WorkspacePath}}/evaluation/learnings/'

## 📖 ANALYSIS GUIDE
- **Low Scores**: Check steps with scores < 50%. Read 'reasoning' in the report.
- **Criteria Issues**: If reasoning says "criteria too vague", update 'success_criteria' in the evaluation plan.
- **Missing Evidence**: If reasoning says "file not found", check if the step checks for the right output files.

## 🛠️ TOOLS
You have evaluation modification tools (update_evaluation_step, add_evaluation_step, delete_evaluation_steps) and workspace tools.
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
	case "evaluation-debugger":
		tmpl = evaluationDebuggerChatTemplate
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

// WorkshopChatSession holds the per-session controller and step registry for interactive
// workshop in chat mode. Create with NewWorkshopChatSession; clean up with Close().
type WorkshopChatSession struct {
	controller   *StepBasedWorkflowOrchestrator
	StepRegistry *WorkshopStepRegistry
	sessionCtx   context.Context
	cancelFunc   context.CancelFunc
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
	PresetExecutionLLM   *AgentLLMConfig
	PresetValidationLLM  *AgentLLMConfig
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
	if cfg.PresetExecutionLLM != nil {
		logger.Info(fmt.Sprintf("[WORKSHOP] presetExecutionLLM=%s/%s", cfg.PresetExecutionLLM.Provider, cfg.PresetExecutionLLM.ModelID))
	}
	if cfg.PresetValidationLLM != nil {
		logger.Info(fmt.Sprintf("[WORKSHOP] presetValidationLLM=%s/%s", cfg.PresetValidationLLM.Provider, cfg.PresetValidationLLM.ModelID))
	}
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
		cfg.PresetExecutionLLM,
		cfg.PresetValidationLLM,
		cfg.PresetLearningLLM,
		cfg.PresetPhaseLLM,
		nil, // presetAnonymizationLLM (deprecated)
		cfg.PresetPlanImprovementLLM,
		cfg.UseKnowledgebase,
		cfg.LLMAllocationMode,
		cfg.TieredConfig,
	)
	if err != nil {
		cancelFunc()
		return nil, fmt.Errorf("failed to create workshop controller: %w", err)
	}

	controller.SetWorkspacePath(cfg.WorkspacePath)

	// Propagate session IDs for MCP connection sharing and session cleanup
	if cfg.SessionID != "" {
		controller.SetMCPSessionID(cfg.SessionID)
		controller.SetHTTPSessionID(cfg.SessionID)
		logger.Debug(fmt.Sprintf("[WORKSHOP] Set MCP/HTTP session ID: %s", cfg.SessionID))
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
		controller:   controller,
		StepRegistry: NewWorkshopStepRegistry(),
		sessionCtx:   sessionCtx,
		cancelFunc:   cancelFunc,
	}, nil
}

// UpdatePresetLLMConfigs refreshes the controller's preset LLM configs.
// Called when reusing a cached workshop session to pick up any LLM config changes
// the user made in the workflow editor since the session was first created.
func (s *WorkshopChatSession) UpdatePresetLLMConfigs(
	executionLLM, validationLLM, learningLLM, phaseLLM, planImprovementLLM *AgentLLMConfig,
) {
	s.controller.presetExecutionLLM = executionLLM
	s.controller.presetValidationLLM = validationLLM
	s.controller.presetLearningLLM = learningLLM
	s.controller.presetPhaseLLM = phaseLLM
	s.controller.presetPlanImprovementLLM = planImprovementLLM
}

// UpdateTieredConfig refreshes the controller's tiered LLM allocation config.
// Called when reusing a cached workshop session to pick up any tiered config changes
// the user made in the workflow editor since the session was first created.
func (s *WorkshopChatSession) UpdateTieredConfig(llmAllocationMode string, tieredConfig *TieredLLMConfig) {
	if llmAllocationMode == "tiered" && tieredConfig != nil {
		orchestratorLLMConfig := s.controller.GetLLMConfig()
		var apiKeys *orchestrator.APIKeys
		if orchestratorLLMConfig != nil {
			apiKeys = orchestratorLLMConfig.APIKeys
		}
		s.controller.tierResolver = NewTierResolver(tieredConfig, apiKeys)
		s.controller.useTieredMode = true
	} else {
		s.controller.tierResolver = nil
		s.controller.useTieredMode = false
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
) {
	iwm := &InteractiveWorkshopManager{
		controller:   session.controller,
		stepRegistry: session.StepRegistry,
		sessionCtx:   session.sessionCtx,
	}
	registerInteractiveWorkshopTools(iwm, mcpAgent, logger)
}

// Close cancels all background goroutines for this workshop session.
func (s *WorkshopChatSession) Close() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
}

// RegisterEvaluationModificationTools is the exported wrapper for registering evaluation
// modification tools on an MCP agent. Used by server.go for evaluation-debugger phase.
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
