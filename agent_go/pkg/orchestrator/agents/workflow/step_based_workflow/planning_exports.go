package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
)

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

{{if .IsCodeExecutionMode}}{{"{{TOOL_STRUCTURE}}"}}{{end}}`)

// PhaseChatSystemPrompt generates the system prompt for any chat-compatible phase.
// Dispatches to the correct template based on phaseId.
func PhaseChatSystemPrompt(phaseId string, templateVars map[string]string) string {
	now := time.Now()
	templateData := map[string]interface{}{
		"WorkspacePath":       templateVars["WorkspacePath"],
		"ExistingPlanJSON":    templateVars["ExistingPlanJSON"],
		"VariableNames":       templateVars["VariableNames"],
		"IsCodeExecutionMode": templateVars["IsCodeExecutionMode"],
		"CurrentDate":         now.Format("2006-01-02"),
		"CurrentTime":         now.Format("15:04:05"),
	}

	var tmpl = interactiveWorkshopSystemTemplate // default: workflow-builder template
	switch phaseId {
	case "execution-qa":
		tmpl = executionDebuggerChatTemplate
	case "workflow-builder":
		// Use the full workshop system template (same as orchestrator mode)
		// so the chat agent gets all plan design guidance, optimization tips, etc.
		// PlanJSON is intentionally NOT injected here — the agent reads plan.json
		// via shell command on demand, avoiding a large static injection on every request.
		templateData["RunFolder"] = templateVars["RunFolder"]
		templateData["StepConfigSummary"] = templateVars["StepConfigSummary"]
		templateData["ProgressSummary"] = templateVars["ProgressSummary"]
		templateData["GroupInfo"] = templateVars["GroupInfo"]
		templateData["UseKnowledgebase"] = templateVars["UseKnowledgebase"]
		templateData["CustomInstructions"] = templateVars["CustomInstructions"]
		templateData["StepSummary"] = templateVars["StepSummary"]
		templateData["WorkshopMode"] = templateVars["WorkshopMode"]
		templateData["UnoptimizedSteps"] = templateVars["UnoptimizedSteps"]
		templateData["WorkflowObjective"] = templateVars["WorkflowObjective"]
		templateData["WorkflowSuccessCriteria"] = templateVars["WorkflowSuccessCriteria"]
		templateData["ExecutionMode"] = templateVars["ExecutionMode"]
		templateData["AvailableGroups"] = templateVars["AvailableGroups"]
		wsPath := templateVars["WorkspacePath"]
		templateData["AbsWorkspacePath"] = GetPromptDocsRoot() + "/" + wsPath
		templateData["AbsDocsRoot"] = GetPromptDocsRoot()
		templateData["PlanJSON"] = ""    // Intentionally empty — agent reads plan.json on demand via shell command
		templateData["UserRequest"] = "" // Not applicable in chat mode — user messages come via conversation
		// EvaluationPlanJSON and EvaluationReportJSON are intentionally NOT injected —
		// the agent reads them on demand via execute_shell_command.
		tmpl = interactiveWorkshopSystemTemplate
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		panic(fmt.Sprintf("[FATAL] Phase chat system prompt template failed for phase=%q: %v — this means the LLM will receive no system prompt. Fix the template or templateData.", phaseId, err))
	}
	rendered := result.String()
	// Guard against empty or suspiciously short prompts — the workshop template should be 10K+ chars
	if len(rendered) < 1000 {
		panic(fmt.Sprintf("[FATAL] Phase chat system prompt for phase=%q is only %d chars (expected 10000+). Template likely has missing variables or rendering issues.", phaseId, len(rendered)))
	}
	return rendered
}

// SchedulerCallbacks provides schedule CRUD operations via callbacks from server.go.
// This avoids importing database/scheduler packages in the workshop package.
type SchedulerCallbacks struct {
	ListSchedules   func(ctx context.Context, workspacePath string) (string, error)
	CreateSchedule  func(ctx context.Context, workspacePath, name, cronExpr, timezone string, groupIDs []string, mode string, messages []string, workshopMode string) (string, error)
	UpdateSchedule  func(ctx context.Context, jobID, name, cronExpr, timezone string, groupIDs []string, setGroupIDs bool, enabled *bool, mode string, messages []string, workshopMode string) (string, error)
	DeleteSchedule  func(ctx context.Context, jobID string) error
	TriggerSchedule func(ctx context.Context, jobID string) (string, error)
	GetScheduleRuns func(ctx context.Context, jobID string, limit int) (string, error)
}

// SkillCallbacks provides skill management operations via callbacks from server.go.
type SkillCallbacks struct {
	ListSkills   func(ctx context.Context) (string, error)
	ImportSkill  func(ctx context.Context, githubURL, token string) (string, error)
	DeleteSkill  func(ctx context.Context, folderName string) error
	SearchSkills func(ctx context.Context, query string) (string, error)  // Search skills registry via CLI
	InstallSkill func(ctx context.Context, source string) (string, error) // Install via npx skills add (owner/repo@skill)
}

// LLMToolsCallbacks provides LLM management operations via callbacks from server.go.
// This avoids importing the server package in the workshop package.
type LLMToolsCallbacks struct {
	ListPublishedLLMs  func(ctx context.Context) (string, error)
	ListProviderModels func(ctx context.Context, provider string) (string, error)
	ValidateLLM        func(ctx context.Context, args map[string]interface{}) (string, error)
}

// WorkshopChatSession holds the per-session controller and step registry for interactive
// workshop in chat mode. Create with NewWorkshopChatSession; clean up with Close().
type WorkshopChatSession struct {
	controller             *StepBasedWorkflowOrchestrator
	StepRegistry           *WorkshopStepRegistry
	sessionCtx             context.Context
	cancelFunc             context.CancelFunc
	toolCallQueryFunc      ToolCallQueryFunc
	mainSessionID          string
	config                 *WorkshopConfig // Original config for creating fresh controllers
	schedulerWorkspacePath string
	schedulerFuncs         *SchedulerCallbacks
	skillFuncs             *SkillCallbacks
	llmToolsFuncs          *LLMToolsCallbacks
	listAvailableSecrets   func(ctx context.Context) ([]string, error)
	// workshopNotifier is the base notifier wired to StepRegistry (set at creation time).
	// SetExtraSubAgentNotifier chains a server-side notifier on top of this.
	workshopNotifier     SubAgentNotifier
	executionNotifier     WorkshopExecutionNotifier // optional: notifies server when executions start/complete
	hasPendingCompletions func() bool                // optional: server-level check for queued completions
	hasRunningAgents      func() bool                // optional: server-level check for running background agents
	cancelAllServerAgents func()                     // optional: cancel all running agents in server registry
	listServerAgents      func() []ServerAgentInfo   // optional: list all agents from server registry
	workshopModeOverride  string                    // frontend-selected workshop mode
}

// GetConfig returns the workshop config (for accessing session-aware executors, etc.)
func (s *WorkshopChatSession) GetConfig() *WorkshopConfig {
	return s.config
}

// MainSessionID returns the owning chat session ID for this workshop session.
func (s *WorkshopChatSession) MainSessionID() string {
	return s.mainSessionID
}

// SetExtraSubAgentNotifier chains a server-supplied notifier (e.g. bgAgentRegistry)
// with the workshop's own notifier. Safe to call on every request — always rebuilds
// the chain so there are no duplicates.
func (s *WorkshopChatSession) SetExtraSubAgentNotifier(n SubAgentNotifier) {
	if s.workshopNotifier != nil {
		s.controller.SetSubAgentNotifier(ChainSubAgentNotifiers(s.workshopNotifier, n))
	} else {
		s.controller.SetSubAgentNotifier(n)
	}
}

// SetWorkshopExecutionNotifier sets the notifier that the server layer uses to track
// workshop step/background executions in bgAgentRegistry (keeps frontend polling alive).
func (s *WorkshopChatSession) SetWorkshopExecutionNotifier(n WorkshopExecutionNotifier) {
	s.executionNotifier = n
	s.controller.SetWorkshopExecutionNotifier(n)
}

// SetExecutionStateChecks sets server-level callbacks for querying and controlling background execution state.
func (s *WorkshopChatSession) SetExecutionStateChecks(hasPending, hasRunning func() bool, cancelAll func(), listAgents func() []ServerAgentInfo) {
	s.hasPendingCompletions = hasPending
	s.hasRunningAgents = hasRunning
	s.cancelAllServerAgents = cancelAll
	s.listServerAgents = listAgents
}

// SetWorkshopModeOverride sets the frontend-selected workshop mode.
// This takes priority over auto-detection when building AUTO-NOTIFICATION action hints.
func (s *WorkshopChatSession) SetWorkshopModeOverride(mode string) {
	s.workshopModeOverride = mode
}

func splitWorkshopRunFolderParts(targetRunFolder string) (string, string) {
	targetRunFolder = filepath.ToSlash(strings.TrimSpace(targetRunFolder))
	if targetRunFolder == "" {
		return "", ""
	}
	parts := strings.Split(targetRunFolder, "/")
	iteration := strings.TrimSpace(parts[0])
	group := ""
	if len(parts) >= 2 {
		group = strings.TrimSpace(parts[len(parts)-1])
	}
	return iteration, group
}

func formatWorkshopExecutionName(kind string, targetRunFolder string) string {
	iteration, group := splitWorkshopRunFolderParts(targetRunFolder)
	switch {
	case iteration != "" && group != "":
		return fmt.Sprintf("%s: %s | Group: %s", kind, iteration, group)
	case iteration != "":
		return fmt.Sprintf("%s: %s", kind, iteration)
	default:
		return fmt.Sprintf("%s: %s", kind, targetRunFolder)
	}
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
	CustomTools          []llmtypes.Tool
	CustomToolExecutors  map[string]interface{}
	ToolCategories       map[string]string
	LLMConfig            *orchestrator.LLMConfig
	PresetPhaseLLM       *AgentLLMConfig
	UseKnowledgebase     bool
	LLMAllocationMode    string
	TieredConfig         *TieredLLMConfig
	Logger               loggerv2.Logger
	EventBridge          mcpagent.AgentEventListener
	// Session tracking — needed for MCP connection sharing and session cleanup
	SessionID string
	// Secrets for step execution (merged global + user secrets)
	Secrets []orchestrator.SecretEntry
	// Skills loaded from preset for skill-based step execution
	SelectedSkills []string
	// WorkspaceEnvRef holds the env map reference for session-aware workspace executors.
	// When set, code execution mode uses this to get MCP_API_URL with session scoping.
	WorkspaceEnvRef map[string]string
	// EnabledGroupIDs holds the group IDs selected from the workspace toolbar.
	// When set, the session auto-resolves variable values and run folder for these groups.
	EnabledGroupIDs []string
	// ToolCallQueryFunc provides live tool call query capability for query_step_tools.
	// Set by server.go which has access to the EventStore.
	ToolCallQueryFunc ToolCallQueryFunc
	// IsEvaluationMode when true, the controller uses evaluation/ paths for step_config, learnings, etc.
	IsEvaluationMode bool
	// SchedulerWorkspacePath is the workspace folder path (needed for schedule management)
	SchedulerWorkspacePath string
	// SchedulerFuncs provides callbacks for schedule CRUD operations.
	// Set by server.go which has access to the database and scheduler service.
	SchedulerFuncs *SchedulerCallbacks
	// SkillFuncs provides callbacks for skill import/delete operations.
	// Set by server.go which has access to the workspace API.
	SkillFuncs *SkillCallbacks
	// LLMToolsFuncs provides callbacks for LLM management operations.
	// Set by server.go which has access to provider keys and model metadata.
	LLMToolsFuncs *LLMToolsCallbacks
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
	logger.Info(fmt.Sprintf("[WORKSHOP] Config: tools=%d, executors=%d, categories=%d, codeExec=%v, knowledgebase=%v, llmMode=%s",
		len(cfg.CustomTools), len(cfg.CustomToolExecutors), len(cfg.ToolCategories),
		cfg.UseCodeExecutionMode, cfg.UseKnowledgebase, cfg.LLMAllocationMode))
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
		cfg.MCPConfigPath,
		cfg.LLMConfig,
		100, // maxTurns
		logger,
		nil, // tracer
		cfg.EventBridge,
		cfg.CustomTools,
		cfg.CustomToolExecutors,
		cfg.ToolCategories,
		cfg.PresetPhaseLLM,
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
				merged := MergeGroupWithDefaults(existingManifest, groupValues)
				controller.variableValues = merged
				SyncVariablesToWorkspaceEnv(controller.BaseOrchestrator, merged)
				logger.Info(fmt.Sprintf("[WORKSHOP] Auto-set variable values from toolbar-selected group %q (%d vars, %d after merge with defaults)", groupID, len(groupValues), len(merged)))
			} else {
				logger.Warn(fmt.Sprintf("[WORKSHOP] Toolbar-selected group %q not found in manifest — falling back to base values", groupID))
				vals, loadErr := LoadVariableValues(ctx, controller.BaseOrchestrator, cfg.WorkspacePath, cfg.WorkspacePath)
				if loadErr == nil && vals != nil {
					controller.variableValues = vals
					SyncVariablesToWorkspaceEnv(controller.BaseOrchestrator, vals)
				}
			}
			controller.enabledGroupIDs = cfg.EnabledGroupIDs
		} else if existingManifest.HasGroups() {
			logger.Warn("[WORKSHOP] No toolbar-selected group available — variable group selection is required for workshop context")
		} else {
			logger.Warn("[WORKSHOP] Variables manifest has no groups — group configuration is required for workshop context")
		}
	}

	// Pre-load the plan so list_steps and get_step_prompts work immediately (best-effort).
	// Use a detached context so SSE streaming or other concurrent request activity cannot
	// cancel this short, bounded read. context.WithoutCancel preserves values but drops
	// the cancellation signal.
	if loadErr := controller.LoadPlanForWorkshop(context.WithoutCancel(ctx)); loadErr != nil {
		logger.Warn(fmt.Sprintf("[WORKSHOP] Could not pre-load plan (%v) — will retry on first tool call", loadErr))
	}

	registry := NewWorkshopStepRegistry()
	wsn := &workshopSubAgentNotifier{registry: registry}
	controller.SetSubAgentNotifier(wsn)
	controller.SetWorkshopExecutionContext(sessionCtx, registry)

	return &WorkshopChatSession{
		controller:             controller,
		StepRegistry:           registry,
		sessionCtx:             sessionCtx,
		cancelFunc:             cancelFunc,
		toolCallQueryFunc:      cfg.ToolCallQueryFunc,
		mainSessionID:          cfg.SessionID,
		config:                 cfg,
		schedulerWorkspacePath: cfg.SchedulerWorkspacePath,
		schedulerFuncs:         cfg.SchedulerFuncs,
		skillFuncs:             cfg.SkillFuncs,
		llmToolsFuncs:          cfg.LLMToolsFuncs,
		listAvailableSecrets:   cfg.ListAvailableSecrets,
		workshopNotifier:       wsn,
	}, nil
}

// UpdatePresetLLMConfigs refreshes the controller's preset LLM configs.
// Called when reusing a cached workshop session to pick up any LLM config changes
// the user made in the workflow editor since the session was first created.
func (s *WorkshopChatSession) UpdatePresetLLMConfigs(phaseLLM *AgentLLMConfig) {
	s.controller.presetPhaseLLM = phaseLLM
	if s.config != nil {
		s.config.PresetPhaseLLM = phaseLLM
	}
}

// UpdateTieredConfig refreshes the controller's tiered LLM allocation config.
// Called when reusing a cached workshop session to pick up any tiered config changes
// the user made in the workflow editor since the session was first created.
// Also updates session.config.TieredConfig so run_full_workflow picks up the new config
// when it creates a fresh controller (e.g. after initial manifest read failed due to
// context cancellation).
func (s *WorkshopChatSession) UpdateTieredConfig(tieredConfig *TieredLLMConfig) {
	if tieredConfig != nil {
		orchestratorLLMConfig := s.controller.GetLLMConfig()
		var apiKeys *orchestrator.APIKeys
		if orchestratorLLMConfig != nil {
			apiKeys = orchestratorLLMConfig.APIKeys
		}
		s.controller.tierResolver = NewTierResolver(tieredConfig, apiKeys)
		// Also persist into session config so run_full_workflow (which reads cfg.TieredConfig)
		// uses the refreshed value rather than the stale one from session creation.
		if s.config != nil {
			s.config.TieredConfig = tieredConfig
			s.config.LLMAllocationMode = "tiered"
		}
	} else {
		s.controller.tierResolver = nil
		if s.config != nil {
			s.config.TieredConfig = nil
		}
	}
}

// UpdateAPIKeys refreshes the orchestrator's API keys.
// Called on session reuse to ensure workspace-stored keys are always current.
func (s *WorkshopChatSession) UpdateAPIKeys(apiKeys *orchestrator.APIKeys) {
	llmCfg := s.controller.GetLLMConfig()
	if llmCfg != nil {
		llmCfg.APIKeys = apiKeys
	}
	// Also refresh tier resolver's API keys if active
	if s.controller.tierResolver != nil && s.config != nil && s.config.TieredConfig != nil {
		s.controller.tierResolver = NewTierResolver(s.config.TieredConfig, apiKeys)
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
	useKnowledgebase bool,
	selectedSkills []string, skillsParsed bool,
	secrets []orchestrator.SecretEntry,
) {
	s.controller.SetSelectedServers(selectedServers)
	if toolsParsed {
		s.controller.SetSelectedTools(selectedTools)
	}
	s.controller.SetUseCodeExecutionMode(useCodeExecutionMode)
	s.controller.useKnowledgebase = useKnowledgebase
	if skillsParsed {
		s.controller.SetSelectedSkills(selectedSkills)
	}
	s.controller.SetSecrets(secrets)

	// Sync back to session.config so run_full_workflow / run_full_evaluation /
	// run_full_report (which create fresh controllers from cfg) pick up the latest values.
	if s.config != nil {
		s.config.SelectedServers = selectedServers
		if toolsParsed {
			s.config.SelectedTools = selectedTools
		}
		s.config.UseCodeExecutionMode = useCodeExecutionMode
		s.config.UseKnowledgebase = useKnowledgebase
		s.config.Secrets = append([]orchestrator.SecretEntry(nil), secrets...)
	}
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
			merged := MergeGroupWithDefaults(manifest, groupValues)
			s.controller.variableValues = merged
			s.controller.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Refreshed variable values from group %q (%d vars, %d after merge with defaults)", groupID, len(groupValues), len(merged)))
		} else {
			s.controller.GetLogger().Warn(fmt.Sprintf("[WORKSHOP] Group %q not found in manifest during refresh", groupID))
		}
	} else if manifest != nil && manifest.HasGroups() {
		s.controller.GetLogger().Warn("[WORKSHOP] No selected group during refresh — preserving existing workshop variable values")
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
		controller:             session.controller,
		workshopConfig:         session.config,
		stepRegistry:           session.StepRegistry,
		sessionCtx:             session.sessionCtx,
		toolCallQueryFunc:      session.toolCallQueryFunc,
		mainSessionID:          session.mainSessionID,
		schedulerWorkspacePath: session.schedulerWorkspacePath,
		schedulerFuncs:         session.schedulerFuncs,
		llmToolsFuncs:          session.llmToolsFuncs,
		skillFuncs:             session.skillFuncs,
		listAvailableSecrets:   session.listAvailableSecrets,
		executionNotifier:      session.executionNotifier,
		hasPendingCompletions:  session.hasPendingCompletions,
		hasRunningAgents:       session.hasRunningAgents,
		cancelAllServerAgents:  session.cancelAllServerAgents,
		listServerAgents:       session.listServerAgents,
		workshopModeOverride:   session.workshopModeOverride,
	}
	registerInteractiveWorkshopTools(iwm, mcpAgent, logger)
}

// Close cancels all background goroutines for this workshop session.
func (s *WorkshopChatSession) Close() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	if s.controller != nil {
		s.controller.CloseWorkshopGroupSessions()
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
		"Run the full evaluation pipeline: execute all evaluation steps against a target execution run, then score each step and generate an evaluation report. Evaluation itself runs in the internal iteration-0 sandbox under evaluation/runs/. Runs in background — you will be notified when complete.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Iteration folder name. Defaults to 'iteration-0' if omitted.",
				},
				"group_id": map[string]interface{}{
					"type":        "string",
					"description": "The group/user subfolder within the iteration (e.g., 'saurabh', 'xspaces'). Required for grouped/batch workflows where each group has its own execution folder.",
				},
			},
			"required": []string{"group_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			iteration, _ := args["iteration"].(string)
			if iteration == "" {
				iteration = "iteration-0"
			}
			groupID, _ := args["group_id"].(string)
			if groupID == "" {
				return "group_id is required — evaluation needs a specific group's execution folder (e.g., 'saurabh', 'xspaces')", nil
			}
			targetRunFolder := iteration + "/" + groupID

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
			displayName := formatWorkshopExecutionName("Evaluation", targetRunFolder)
			iterationName, groupName := splitWorkshopRunFolderParts(targetRunFolder)
			if session.executionNotifier != nil {
				session.executionNotifier.OnExecutionStart(WorkshopExecutionStart{ID: execID, Name: displayName, Cancel: cancel})
			}

			go func() {
				var result string
				var execErr error
				execMeta := map[string]string{
					"workshop_mode":  "eval",
					"execution_type": "full-evaluation",
					"run_folder":     targetRunFolder,
				}
				if iterationName != "" {
					execMeta["iteration"] = iterationName
				}
				if groupName != "" {
					execMeta["group_id"] = groupName
					execMeta["group_display_name"] = groupName
				}
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if !skipNotify && session.executionNotifier != nil {
						session.executionNotifier.OnExecutionComplete(execID, displayName, result, execMeta, execErr)
					}
				}()

				// Create a fresh controller for the full evaluation run
				evalController, err := NewStepBasedWorkflowOrchestrator(
					execCtx,
					"", "", 0.7, "simple",
					cfg.SelectedServers,
					cfg.SelectedTools,
					cfg.UseCodeExecutionMode,
					cfg.MCPConfigPath,
					cfg.LLMConfig,
					100,
					logger,
					nil, // tracer
					cfg.EventBridge,
					cfg.CustomTools,
					cfg.CustomToolExecutors,
					cfg.ToolCategories,
					cfg.PresetPhaseLLM,
					cfg.UseKnowledgebase,
					cfg.TieredConfig,
				)
				if err != nil {
					execErr = fmt.Errorf("failed to create evaluation controller: %w", err)
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

				result, execErr = evalController.ExecuteEvaluationOnly(
					execCtx,
					session.controller.GetObjective(),
					cfg.WorkspacePath,
					targetRunFolder,
				)
			}()

			return fmt.Sprintf("Full evaluation started for run %q.\nexecution_id: %q\nThis will execute all evaluation steps and generate a scoring report.\nYou will be automatically notified when it completes.", targetRunFolder, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_full_evaluation tool: %v", err))
	}
}

// RegisterRunFullReportTool registers a run_full_report tool that executes the workflow
// report/output generation against a target execution run. Runs in background.
func RegisterRunFullReportTool(
	mcpAgent *mcpagent.Agent,
	session *WorkshopChatSession,
	logger loggerv2.Logger,
) {
	if err := mcpAgent.RegisterCustomTool(
		"run_full_report",
		"Run the full workflow report generation against a target execution run. Report generation itself runs in the internal iteration-0 sandbox and then publishes the final report artifact back to the requested run. Runs in background and notifies when complete.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Iteration folder name. Defaults to 'iteration-0' if omitted.",
				},
				"group_id": map[string]interface{}{
					"type":        "string",
					"description": "The group/user subfolder within the iteration (e.g., 'saurabh', 'manish'). Required — reports are always group-scoped.",
				},
			},
			"required": []string{"group_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			iteration, _ := args["iteration"].(string)
			if iteration == "" {
				iteration = "iteration-0"
			}
			groupID, _ := args["group_id"].(string)
			if groupID == "" {
				return "group_id is required — reports are always group-scoped, e.g. group_id='manish'", nil
			}
			targetRunFolder := iteration + "/" + groupID

			cfg := session.config
			if cfg == nil {
				return "session config not available — cannot create report controller", nil
			}

			execID := fmt.Sprintf("report-full-%s-%d", strings.ReplaceAll(targetRunFolder, "/", "-"), time.Now().UnixNano())
			execCtx, cancel := context.WithCancel(session.sessionCtx)

			agentSessionID := fmt.Sprintf("workshop-report-%s-%d", strings.ReplaceAll(targetRunFolder, "/", "-"), time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         fmt.Sprintf("full-report-%s", targetRunFolder),
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			session.StepRegistry.Register(exec)
			displayName := formatWorkshopExecutionName("Report", targetRunFolder)
			iterationName, groupName := splitWorkshopRunFolderParts(targetRunFolder)
			if session.executionNotifier != nil {
				session.executionNotifier.OnExecutionStart(WorkshopExecutionStart{ID: execID, Name: displayName, Cancel: cancel})
			}

			go func() {
				var result string
				var execErr error
				eventBridge := session.controller.GetContextAwareBridge()
				execMeta := map[string]string{
					"workshop_mode":  "output",
					"execution_type": "full-report",
					"run_folder":     targetRunFolder,
				}
				if iterationName != "" {
					execMeta["iteration"] = iterationName
				}
				if groupName != "" {
					execMeta["group_id"] = groupName
					execMeta["group_display_name"] = groupName
				}
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if eventBridge != nil {
						endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
							BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
							AgentType:     "workshop-report-execution",
							AgentName:     displayName,
							Success:       execErr == nil && !skipNotify,
							InputData: map[string]string{
								"run_folder":     targetRunFolder,
								"workshop_mode":  "output",
								"execution_type": "report",
							},
						}
						if iterationName != "" {
							endEvent.InputData["iteration"] = iterationName
						}
						if groupName != "" {
							endEvent.InputData["group_id"] = groupName
							endEvent.InputData["group_display_name"] = groupName
						}
						if execErr != nil {
							if skipNotify || execCtx.Err() != nil {
								endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
							} else {
								endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
							}
						} else {
							endEvent.Result = firstNonEmpty(result, "Report generated successfully.")
						}
						eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
							Type:          orchestrator_events.OrchestratorAgentEnd,
							Timestamp:     time.Now(),
							Data:          endEvent,
							CorrelationID: agentSessionID,
						})
					}
					if !skipNotify && session.executionNotifier != nil {
						session.executionNotifier.OnExecutionComplete(execID, displayName, result, execMeta, execErr)
					}
				}()

				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-report-execution",
						AgentName:     displayName,
						InputData: map[string]string{
							"run_folder":     targetRunFolder,
							"workshop_mode":  "output",
							"execution_type": "report",
						},
					}
					if iterationName != "" {
						startEvent.InputData["iteration"] = iterationName
					}
					if groupName != "" {
						startEvent.InputData["group_id"] = groupName
						startEvent.InputData["group_display_name"] = groupName
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				reportController, err := NewStepBasedWorkflowOrchestrator(
					execCtx,
					"", "", 0.7, "simple",
					cfg.SelectedServers,
					cfg.SelectedTools,
					cfg.UseCodeExecutionMode,
					cfg.MCPConfigPath,
					cfg.LLMConfig,
					100,
					logger,
					nil,
					cfg.EventBridge,
					cfg.CustomTools,
					cfg.CustomToolExecutors,
					cfg.ToolCategories,
					cfg.PresetPhaseLLM,
					cfg.UseKnowledgebase,
					cfg.TieredConfig,
				)
				if err != nil {
					execErr = fmt.Errorf("failed to create report controller: %w", err)
					return
				}

				if cfg.SessionID != "" {
					reportController.SetHTTPSessionID(cfg.SessionID)
				}
				if len(cfg.Secrets) > 0 {
					reportController.SetSecrets(cfg.Secrets)
				}
				if cfg.WorkspaceEnvRef != nil {
					reportController.SetWorkspaceEnvRef(cfg.WorkspaceEnvRef)
				}
				if skills := session.controller.GetSelectedSkills(); len(skills) > 0 {
					reportController.SetSelectedSkills(skills)
				}
				if session.controller.GetCdpPort() > 0 {
					reportController.SetCdpPort(session.controller.GetCdpPort())
				}
				reportController.SetExecutionOptions(session.controller.GetExecutionOptions())

				result, execErr = reportController.ExecuteFinalOutputOnly(
					execCtx,
					session.controller.GetObjective(),
					cfg.WorkspacePath,
					targetRunFolder,
				)
				result = firstNonEmpty(strings.TrimSpace(result), "Report generated successfully.")
			}()

			return fmt.Sprintf("Full report generation started for run %q.\nexecution_id: %q\nThis will regenerate the report artifact for that run.\nYou will be automatically notified when it completes.", targetRunFolder, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_full_report tool: %v", err))
	}
}

// workflowProgressBridge wraps an existing event bridge and intercepts step completion
// events to send progress notifications to the main workshop agent via bgAgentRegistry.
type workflowProgressBridge struct {
	inner    mcpagent.AgentEventListener
	session  *WorkshopChatSession
	logger   loggerv2.Logger
	parentID string // parent execution ID for correlation
}

func (b *workflowProgressBridge) HandleEvent(ctx context.Context, event *baseevents.AgentEvent) error {
	// Forward all events to the inner bridge first
	if b.inner != nil {
		if err := b.inner.HandleEvent(ctx, event); err != nil {
			return err
		}
	}

	// Intercept step completion events to send progress notifications
	if event.Type == orchestrator_events.OrchestratorAgentEnd {
		if endEvent, ok := event.Data.(*orchestrator_events.OrchestratorAgentEndEvent); ok {
			// Only notify for execution agent completions (not learning, validation, etc.)
			agentType := endEvent.AgentType
			if agentType == "todo_planner_execution" || agentType == "conditional" || agentType == "todo_task_orchestrator" {
				stepName := endEvent.AgentName
				status := "completed"
				result := endEvent.Result
				if !endEvent.Success {
					status = "failed"
					if endEvent.Error != "" {
						result = endEvent.Error
					}
				}

				// Register a progress notification in bgAgentRegistry
				progressID := fmt.Sprintf("%s-step-%d-%d", b.parentID, endEvent.StepIndex, time.Now().UnixNano())
				progressExec := &WorkshopStepExecution{
					ID:     progressID,
					StepID: fmt.Sprintf("workflow-step-%s", stepName),
					Status: WorkshopStepDone,
					Result: fmt.Sprintf("[Step %d: %s] %s — %s", endEvent.StepIndex, stepName, status, truncateResult(result, 500)),
				}
				if !endEvent.Success {
					progressExec.Status = WorkshopStepFailed
					progressExec.Err = fmt.Errorf("%s", result)
				}
				b.session.StepRegistry.Register(progressExec)

				// Notify so backgroundCompletionLoop picks it up
				if b.session.executionNotifier != nil {
					b.session.executionNotifier.OnExecutionStart(WorkshopExecutionStart{ID: progressID, Name: fmt.Sprintf("step-%s", stepName), Cancel: nil})
					if endEvent.Success {
						b.session.executionNotifier.OnExecutionComplete(progressID, fmt.Sprintf("step-%s", stepName), truncateResult(result, 500), nil, nil)
					} else {
						b.session.executionNotifier.OnExecutionComplete(progressID, fmt.Sprintf("step-%s", stepName), "", nil, fmt.Errorf("%s", truncateResult(result, 500)))
					}
				}

				if b.logger != nil {
					b.logger.Info(fmt.Sprintf("📊 [WORKFLOW_PROGRESS] Step %d '%s' %s", endEvent.StepIndex, stepName, status))
				}
			}
		}
	}

	return nil
}

func (b *workflowProgressBridge) Name() string {
	if b.inner != nil {
		return "workflow-progress-" + b.inner.Name()
	}
	return "workflow-progress"
}

// truncateResult truncates a string for progress notifications
func truncateResult(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RegisterRunFullWorkflowTool registers a run_full_workflow tool that executes the complete
// workflow pipeline (all steps, all enabled groups) in the background. The LLM is notified
// when execution completes. This is the workshop-builder equivalent of the orchestrator-mode
// full execution, but triggered as a tool call.
func RegisterRunFullWorkflowTool(
	mcpAgent *mcpagent.Agent,
	session *WorkshopChatSession,
	logger loggerv2.Logger,
) {
	if err := mcpAgent.RegisterCustomTool(
		"run_full_workflow",
		"Execute the complete workflow: load the plan, resolve variables, and run all steps for a single variable group. Runs in background — you will be notified when complete. Use this to trigger a full end-to-end workflow run. If the plan contains human_input steps, you MUST provide human_inputs with a response for each one — the tool will error if any are missing. For routing steps, you can also pass human_inputs with the user's choice to guide the routing decision.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Iteration folder name. Defaults to 'iteration-0' if omitted. Reuses the folder if it exists, creates it if not.",
				},
				"execution_strategy": map[string]interface{}{
					"type":        "string",
					"description": "Execution strategy. Defaults to 'start_from_beginning_no_human' (fresh run, skip human input steps). Use 'resume_from_step_no_human' to resume from last incomplete step.",
					"enum":        []string{"start_from_beginning_no_human", "resume_from_step_no_human"},
				},
				"group_id": map[string]interface{}{
					"type":        "string",
					"description": "Variable group ID to execute (e.g., 'group-1', 'saurabh'). Required. Only one group runs at a time.",
				},
				"human_inputs": map[string]interface{}{
					"type":        "object",
					"description": "Responses for human_input and routing steps, keyed by step ID. Required if the plan has human_input steps. Also supports routing steps — pass the user's choice so the routing execution agent can use it instead of defaulting. Example: {\"choose-workflow\": \"Option 2\", \"route-workflow\": \"Option 2 - execute tests for ai-workshop\"}. Read the plan to see which human_input and routing steps exist.",
					"additionalProperties": map[string]interface{}{
						"type": "string",
					},
				},
			},
			"required": []string{"group_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			cfg := session.config
			if cfg == nil {
				return "session config not available — cannot create workflow controller", nil
			}

			// Parse parameters — iteration defaults to "iteration-0"
			iteration := "iteration-0"
			if it, ok := args["iteration"].(string); ok && it != "" {
				iteration = it
			}

			strategy := "start_from_beginning_no_human"
			if s, ok := args["execution_strategy"].(string); ok && s != "" {
				strategy = s
			}

			// Single group only — required
			groupID := ""
			if g, ok := args["group_id"].(string); ok && g != "" {
				groupID = g
			}
			if groupID == "" {
				return "group_id is required. Read variables.json to see available groups.", nil
			}
			enabledGroupIDs := []string{groupID}

			// Parse human_inputs (optional map of step_id → response)
			var humanInputs map[string]string
			if hi, ok := args["human_inputs"]; ok && hi != nil {
				if hiMap, ok := hi.(map[string]interface{}); ok {
					humanInputs = make(map[string]string, len(hiMap))
					for k, v := range hiMap {
						if s, ok := v.(string); ok {
							humanInputs[k] = s
						}
					}
				}
			}

			// Validate: if plan has human_input steps, human_inputs must cover them all.
			// Also warn about routing steps that accept human_inputs for user intent.
			if err := session.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v", err), nil
			}
			if session.controller.approvedPlan != nil {
				var missingSteps []string
				var routingStepHints []string
				for _, step := range session.controller.approvedPlan.Steps {
					if step.StepType() == StepTypeHumanInput {
						stepID := step.GetID()
						if _, ok := humanInputs[stepID]; !ok {
							hiStep := step.(*HumanInputPlanStep)
							missingSteps = append(missingSteps, fmt.Sprintf("  - %s (id: %s, question: %q)", hiStep.GetTitle(), stepID, hiStep.Question))
						}
					}
					// Hint about routing steps that have a description (execute-then-route)
					// so the builder knows to pass human_inputs for them too.
					if step.StepType() == StepTypeRouting {
						if routingStep, ok := step.(*RoutingPlanStep); ok && routingStep.Description != "" {
							stepID := step.GetID()
							if _, ok := humanInputs[stepID]; !ok {
								routingStepHints = append(routingStepHints, fmt.Sprintf("  - %s (id: %s) — pass the user's choice so the agent knows what to do", step.GetTitle(), stepID))
							}
						}
					}
				}
				if len(missingSteps) > 0 {
					return fmt.Sprintf("❌ Plan has human_input steps that require responses via human_inputs parameter. Missing:\n%s\n\nProvide human_inputs with a response for each step ID listed above.", strings.Join(missingSteps, "\n")), nil
				}
				if len(routingStepHints) > 0 {
					// Find the first routing step ID for the example
					exampleStepID := "route-step"
					for _, step := range session.controller.approvedPlan.Steps {
						if step.StepType() == StepTypeRouting {
							exampleStepID = step.GetID()
							break
						}
					}
					return fmt.Sprintf("⚠️ Plan has routing steps that need the user's choice via human_inputs. Without it, the routing agent won't know the user's intent. Missing:\n%s\n\nPlease re-call with human_inputs including the routing step. Example: human_inputs: {\"%s\": \"<user's choice here>\"}", strings.Join(routingStepHints, "\n"), exampleStepID), nil
				}
			}

			// Iteration is always provided — reuse the folder (creates if doesn't exist)
			runMode := "use_same_run"

			execID := fmt.Sprintf("workflow-full-%d", time.Now().UnixNano())
			execCtx, cancel := context.WithCancel(session.sessionCtx)

			agentSessionID := fmt.Sprintf("workshop-workflow-full-%d", time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         "full-workflow",
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			session.StepRegistry.Register(exec)

			// Notify workshop execution notifier so frontend keeps polling
			// Include group and iteration in display name so notifications are unambiguous
			workflowDisplayName := "full-workflow"
			if len(enabledGroupIDs) > 0 && iteration != "" {
				workflowDisplayName = fmt.Sprintf("full-workflow [%s / %s]", enabledGroupIDs[0], iteration)
			} else if len(enabledGroupIDs) > 0 {
				workflowDisplayName = fmt.Sprintf("full-workflow [%s]", enabledGroupIDs[0])
			}
			if session.executionNotifier != nil {
				session.executionNotifier.OnExecutionStart(WorkshopExecutionStart{ID: execID, Name: workflowDisplayName, Cancel: cancel})
			}

			go func() {
				var result string
				var execErr error
				eventBridge := session.controller.GetContextAwareBridge()
				execMeta := map[string]string{
					"workshop_mode":  "runner",
					"execution_type": "full-workflow",
				}
				if iteration != "" {
					execMeta["iteration"] = iteration
				}
				if len(enabledGroupIDs) > 0 {
					execMeta["group_id"] = enabledGroupIDs[0]
					execMeta["group_display_name"] = enabledGroupIDs[0]
				}
				defer func() {
					skipNotify := finalizeExecStatus(exec, execCtx, &result, &execErr)
					if eventBridge != nil {
						endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
							BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
							AgentType:     "workshop-workflow-execution",
							AgentName:     "Full Workflow Execution",
							Success:       execErr == nil && !skipNotify,
							InputData: map[string]string{
								"execution_strategy": strategy,
								"execution_type":     "full-workflow",
							},
						}
						if execErr != nil {
							if skipNotify || execCtx.Err() != nil {
								endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
							} else {
								endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
							}
						} else {
							endEvent.Result = firstNonEmpty(result, "Workflow execution completed successfully.")
						}
						eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
							Type:          orchestrator_events.OrchestratorAgentEnd,
							Timestamp:     time.Now(),
							Data:          endEvent,
							CorrelationID: agentSessionID,
						})
					}
					if !skipNotify && session.executionNotifier != nil {
						session.executionNotifier.OnExecutionComplete(execID, "Full Workflow Execution", result, execMeta, execErr)
					}
				}()

				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-workflow-execution",
						AgentName:     "Full Workflow Execution",
						InputData: map[string]string{
							"execution_strategy": strategy,
							"execution_type":     "full-workflow",
						},
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				// Wrap event bridge with progress listener to send per-step notifications
				progressBridge := &workflowProgressBridge{
					inner:    cfg.EventBridge,
					session:  session,
					logger:   logger,
					parentID: execID,
				}

				workflowController, err := NewStepBasedWorkflowOrchestrator(
					execCtx,
					"", "", 0.7, "simple",
					cfg.SelectedServers,
					cfg.SelectedTools,
					cfg.UseCodeExecutionMode,
					cfg.MCPConfigPath,
					cfg.LLMConfig,
					100,
					logger,
					nil,
					progressBridge, // wrapped bridge with per-step notifications
					cfg.CustomTools,
					cfg.CustomToolExecutors,
					cfg.ToolCategories,
					cfg.PresetPhaseLLM,
					cfg.UseKnowledgebase,
					cfg.TieredConfig,
				)
				if err != nil {
					execErr = fmt.Errorf("failed to create workflow controller: %w", err)
					return
				}

				// Wire sub-agent tracking so generic/predefined sub-agents spawned by the
				// runner controller appear in the session's stepRegistry and are visible
				// via list_executions/query_step. Without this, hcpo.subAgentNotifier is
				// nil inside controller_todo_task.go and sub-agent tracking is silently skipped.
				workflowController.SetSubAgentNotifier(&workshopSubAgentNotifier{registry: session.StepRegistry})
				workflowController.SetWorkshopExecutionContext(execCtx, session.StepRegistry)

				// Propagate session context
				if cfg.SessionID != "" {
					workflowController.SetHTTPSessionID(cfg.SessionID)
				}
				if len(cfg.Secrets) > 0 {
					workflowController.SetSecrets(cfg.Secrets)
				}
				if cfg.WorkspaceEnvRef != nil {
					workflowController.SetWorkspaceEnvRef(cfg.WorkspaceEnvRef)
				}
				if skills := session.controller.GetSelectedSkills(); len(skills) > 0 {
					workflowController.SetSelectedSkills(skills)
				}
				if session.controller.GetCdpPort() > 0 {
					workflowController.SetCdpPort(session.controller.GetCdpPort())
				}

				// Set execution options
				execOpts := &ExecutionOptions{
					RunMode:           runMode,
					SelectedRunFolder: iteration,
					ExecutionStrategy: strategy,
					EnabledGroupIDs:   enabledGroupIDs,
					HumanInputs:      humanInputs,
				}
				workflowController.SetExecutionOptions(execOpts)

				result, execErr = workflowController.CreateTodoList(
					execCtx,
					session.controller.GetObjective(),
					cfg.WorkspacePath,
				)
				result = firstNonEmpty(strings.TrimSpace(result), "Workflow execution completed successfully.")
			}()

			groupInfo := ""
			if len(enabledGroupIDs) > 0 {
				groupInfo = fmt.Sprintf("\nGroup: %s", enabledGroupIDs[0])
			}
			iterInfo := "\nIteration: new (auto-created)"
			if iteration != "" {
				iterInfo = fmt.Sprintf("\nIteration: %s (reusing)", iteration)
			}
			return fmt.Sprintf("Full workflow execution started.\nexecution_id: %q\nStrategy: %s%s%s\nAll steps will be executed end-to-end.\nYou will be automatically notified when it completes.", execID, strategy, groupInfo, iterInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_full_workflow tool: %v", err))
	}
}

// RegisterEvaluationValidationTools is the exported wrapper for registering evaluation
// plan validation tools on an MCP agent. Used by server.go for workflow-builder chat sessions.
func RegisterEvaluationValidationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	moveFile func(context.Context, string, string) error,
) error {
	_ = writeFile
	_ = moveFile
	return registerEvaluationValidationTools(mcpAgent, workspacePath, logger, readFile)
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
