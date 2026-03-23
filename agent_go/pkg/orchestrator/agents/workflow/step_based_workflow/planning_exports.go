package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
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
	controller           *StepBasedWorkflowOrchestrator
	StepRegistry         *WorkshopStepRegistry
	sessionCtx           context.Context
	cancelFunc           context.CancelFunc
	toolCallQueryFunc    ToolCallQueryFunc
	mainSessionID        string
	config               *WorkshopConfig // Original config for creating fresh controllers
	presetQueryID        string
	schedulerFuncs       *SchedulerCallbacks
	skillFuncs           *SkillCallbacks
	listAvailableSecrets func(ctx context.Context) ([]string, error)
	// workshopNotifier is the base notifier wired to StepRegistry (set at creation time).
	// SetExtraSubAgentNotifier chains a server-side notifier on top of this.
	workshopNotifier SubAgentNotifier
}

// GetConfig returns the workshop config (for accessing session-aware executors, etc.)
func (s *WorkshopChatSession) GetConfig() *WorkshopConfig {
	return s.config
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
	// PresetQueryID is the preset this workshop belongs to (needed for schedule management)
	PresetQueryID string
	// SchedulerFuncs provides callbacks for schedule CRUD operations.
	// Set by server.go which has access to the database and scheduler service.
	SchedulerFuncs *SchedulerCallbacks
	// SkillFuncs provides callbacks for skill import/delete operations.
	// Set by server.go which has access to the workspace API.
	SkillFuncs *SkillCallbacks
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
				controller.variableValues = groupValues
				SyncVariablesToWorkspaceEnv(controller.BaseOrchestrator, groupValues)
				logger.Info(fmt.Sprintf("[WORKSHOP] Auto-set variable values from toolbar-selected group %q (%d vars)", groupID, len(groupValues)))
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

	return &WorkshopChatSession{
		controller:           controller,
		StepRegistry:         registry,
		sessionCtx:           sessionCtx,
		cancelFunc:           cancelFunc,
		toolCallQueryFunc:    cfg.ToolCallQueryFunc,
		mainSessionID:        cfg.SessionID,
		config:               cfg,
		presetQueryID:        cfg.PresetQueryID,
		schedulerFuncs:       cfg.SchedulerFuncs,
		skillFuncs:           cfg.SkillFuncs,
		listAvailableSecrets: cfg.ListAvailableSecrets,
		workshopNotifier:     wsn,
	}, nil
}

// UpdatePresetLLMConfigs refreshes the controller's preset LLM configs.
// Called when reusing a cached workshop session to pick up any LLM config changes
// the user made in the workflow editor since the session was first created.
func (s *WorkshopChatSession) UpdatePresetLLMConfigs(phaseLLM *AgentLLMConfig) {
	s.controller.presetPhaseLLM = phaseLLM
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
		controller:           session.controller,
		stepRegistry:         session.StepRegistry,
		sessionCtx:           session.sessionCtx,
		toolCallQueryFunc:    session.toolCallQueryFunc,
		mainSessionID:        session.mainSessionID,
		presetQueryID:        session.presetQueryID,
		schedulerFuncs:       session.schedulerFuncs,
		skillFuncs:           session.skillFuncs,
		listAvailableSecrets: session.listAvailableSecrets,
	}
	registerInteractiveWorkshopTools(iwm, mcpAgent, logger)
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
					cfg.PresetPhaseLLM,
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

// RegisterRunFullReportTool registers a run_full_report tool that executes the workflow
// report/output generation against a target execution run. Runs in background.
func RegisterRunFullReportTool(
	mcpAgent *mcpagent.Agent,
	session *WorkshopChatSession,
	logger loggerv2.Logger,
) {
	if err := mcpAgent.RegisterCustomTool(
		"run_full_report",
		"Run the full workflow report generation against a target execution run. Regenerates the final report artifact for that run in background and notifies when complete.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_run_folder": map[string]interface{}{
					"type":        "string",
					"description": "The group-scoped execution run folder to generate a report for (e.g., 'iteration-2/manish').",
				},
			},
			"required": []string{"target_run_folder"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			targetRunFolder, _ := args["target_run_folder"].(string)
			if targetRunFolder == "" {
				return "target_run_folder is required", nil
			}
			if !strings.Contains(targetRunFolder, "/") {
				return "target_run_folder must be group-scoped, e.g. 'iteration-2/group-name'", nil
			}

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

			go func() {
				eventBridge := session.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-report-execution",
						AgentName:     fmt.Sprintf("Report: %s", targetRunFolder),
						InputData: map[string]string{
							"run_folder":     targetRunFolder,
							"workshop_mode":  "output",
							"execution_type": "report",
						},
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
					cfg.UseToolSearchMode,
					cfg.PreDiscoveredTools,
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
					exec.mu.Lock()
					exec.Status = WorkshopStepFailed
					exec.Err = fmt.Errorf("failed to create report controller: %w", err)
					exec.mu.Unlock()
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

				resultText, execErr := reportController.ExecuteFinalOutputOnly(
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
					exec.Result = firstNonEmpty(strings.TrimSpace(resultText), "Report generated successfully.")
				}

				if eventBridge != nil {
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-report-execution",
						AgentName:     fmt.Sprintf("Report: %s", targetRunFolder),
						Success:       execErr == nil,
						InputData: map[string]string{
							"run_folder":     targetRunFolder,
							"workshop_mode":  "output",
							"execution_type": "report",
						},
					}
					if execErr != nil {
						endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
					} else if exec.Result != "" {
						endEvent.Result = exec.Result
					} else {
						endEvent.Result = "Report generated successfully."
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentEnd,
						Timestamp:     time.Now(),
						Data:          endEvent,
						CorrelationID: agentSessionID,
					})
				}
			}()

			return fmt.Sprintf("Full report generation started for run %q.\nexecution_id: %q\nThis will regenerate the report artifact for that run.\nYou will be automatically notified when it completes.", targetRunFolder, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_full_report tool: %v", err))
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
