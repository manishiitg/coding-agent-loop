package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetDelegationToolCategory returns the category name for delegation tools
// Using "human" category makes the tool directly available (not a code tool) in:
// - Code execution mode: human category tools are always directly callable
// - Tool search mode: human category tools are immediately available without searching
func GetDelegationToolCategory() string {
	return "human"
}

// Context keys for delegation tool execution
type delegationContextKey string

const (
	// ExecuteDelegatedTaskKey is the context key for the delegation execution function
	ExecuteDelegatedTaskKey delegationContextKey = "execute_delegated_task"
	// MaxDelegationDepth is the maximum allowed delegation depth to prevent infinite recursion
	MaxDelegationDepth = 3
	// WorkspaceClientKey is the context key for the workspace client (plan file I/O)
	WorkspaceClientKey delegationContextKey = "workspace_client"
	// DelegationTierConfigKey is the context key for the delegation tier configuration
	DelegationTierConfigKey delegationContextKey = "delegation_tier_config"
	// CapabilitiesContextKey is the context key for available capabilities (MCP servers, skills, etc.)
	CapabilitiesContextKey delegationContextKey = "capabilities_context"
	// SessionEventEmitterKey is the context key for the session event emitter (used for human feedback UI)
	SessionEventEmitterKey delegationContextKey = "session_event_emitter"
	// BotNotificationDestinationKey carries the originating bot thread/channel
	// so human tools can notify the same connector conversation.
	BotNotificationDestinationKey delegationContextKey = "bot_notification_destination"
	// ChatsFolderPath is the fallback per-user Chats folder when session context is unavailable.
	// Always prefer GetChatsFolder(ctx) which reads the session-scoped per-user path.
	ChatsFolderPath = "_users/default/Chats"
	// ChatsFolderKey is the context key for the session-scoped Chats folder path (usually _users/<userID>/Chats).
	// Set by the server at session setup and propagated to every sub-agent via context.
	ChatsFolderKey delegationContextKey = "chats_folder"
	// BGAgentRegistryKey is the context key for the background agent registry
	BGAgentRegistryKey delegationContextKey = "bg_agent_registry"
	// BGAgentSessionIDKey is the context key for the session ID used by background agents
	BGAgentSessionIDKey delegationContextKey = "bg_agent_session_id"
	// ToolEventCallbackKey is the context key for tool call timing callback (used by background agents)
	ToolEventCallbackKey delegationContextKey = "tool_event_callback"
	// BackgroundDelegateKey is the context key for the async delegate function
	BackgroundDelegateKey delegationContextKey = "background_delegate_func"
)

// Per-delegation configuration (depth, reasoning level, template, servers,
// skills, browser sharing, background agent ID) travels as a single typed
// SubAgentSpec — see sub_agent_spec.go.

// SessionEventEmitter is the interface for emitting human-feedback events to the session event store
type SessionEventEmitter interface {
	EmitBlockingHumanFeedback(requestID, question, context string, yesNoOnly bool, yesLabel, noLabel string, options ...string)
}

// GetChatsFolder returns the workspace-relative Chats folder for the current session.
// Reads ChatsFolderKey from context first (per-user path set at session setup) and falls
// back to the global ChatsFolderPath constant if no session-scoped value is present.
func GetChatsFolder(ctx context.Context) string {
	if folder, ok := ctx.Value(ChatsFolderKey).(string); ok && folder != "" {
		return folder
	}
	return ChatsFolderPath
}

// DelegationTierConfig holds provider/model for each reasoning tier
type DelegationTierConfig struct {
	Main         *TierModel                  `json:"main,omitempty"` // orchestrator/main agent model
	ChiefOfStaff *TierModel                  `json:"chief_of_staff,omitempty"`
	High         *TierModel                  `json:"high,omitempty"`
	Medium       *TierModel                  `json:"medium,omitempty"`
	Low          *TierModel                  `json:"low,omitempty"`
	Custom       map[string]*CustomTierModel `json:"custom,omitempty"`
}

// TierModel represents a specific provider+model for a tier
type TierModel struct {
	Provider  string              `json:"provider"`
	ModelID   string              `json:"model_id"`
	Fallbacks []TierModelFallback `json:"fallbacks,omitempty"`
}

// TierModelFallback represents an ordered fallback model for a delegation tier
type TierModelFallback struct {
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"model_id"`
}

// CustomTierModel represents a user-defined reasoning tier with description and model
type CustomTierModel struct {
	Description string `json:"description"`
	Provider    string `json:"provider"`
	ModelID     string `json:"model_id"`
}

// CapabilitiesContext describes the available tools, servers, and skills for the planner
type CapabilitiesContext struct {
	EnabledServers    []string                  `json:"enabled_servers,omitempty"`
	SelectedTools     []string                  `json:"selected_tools,omitempty"` // "server:tool" format
	Skills            []SkillSummary            `json:"skills,omitempty"`
	SubAgentTemplates []SubAgentTemplateSummary `json:"subagent_templates,omitempty"`
	HasWorkspace      bool                      `json:"has_workspace"`
	HasBrowser        bool                      `json:"has_browser"`
}

// SkillSummary holds minimal info about a skill for the planner prompt
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	FolderName  string `json:"folder_name"`
}

// SubAgentTemplateSummary holds minimal info about a sub-agent template for the planner prompt
type SubAgentTemplateSummary struct {
	Name                  string `json:"name"`
	Description           string `json:"description"`
	FolderName            string `json:"folder_name"`
	DefaultReasoningLevel string `json:"default_reasoning_level,omitempty"`
}

// ExecuteDelegatedTaskFunc is the function signature for executing delegated tasks
// Injected via context by the server
type ExecuteDelegatedTaskFunc func(ctx context.Context, instruction string) (string, error)

// BackgroundDelegateFunc is the function signature for async background delegation
// Used only in plan/multi-agent mode. Returns immediately with an agentID.
type BackgroundDelegateFunc func(ctx context.Context, name, instruction string) (agentID string, err error)

func getBackgroundDelegate(ctx context.Context) (BackgroundDelegateFunc, bool) {
	switch fn := ctx.Value(BackgroundDelegateKey).(type) {
	case BackgroundDelegateFunc:
		if fn != nil {
			return fn, true
		}
	case func(context.Context, string, string) (string, error):
		if fn != nil {
			return BackgroundDelegateFunc(fn), true
		}
	}
	return nil, false
}

// BGAgentInfo holds a snapshot of a background agent's state (for tool responses)
// BGAgentHistoryEntry mirrors HistoryEntry from background_agents.go (avoids import cycle)
type BGAgentHistoryEntry struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// BGAgentToolCall represents a tool call with timing info
type BGAgentToolCall struct {
	ToolName string `json:"tool_name"`
	Duration string `json:"duration,omitempty"` // e.g. "3s", "" if still running
	Status   string `json:"status"`             // "running", "completed", "error"
}

type BGAgentInfo struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Status          string                `json:"status"`
	RecentHistory   []BGAgentHistoryEntry `json:"recent_history,omitempty"`
	RecentToolCalls []BGAgentToolCall     `json:"recent_tool_calls,omitempty"`
	Result          string                `json:"result,omitempty"`
	Error           string                `json:"error,omitempty"`
	Elapsed         string                `json:"elapsed,omitempty"`
	CreatedAt       string                `json:"created_at,omitempty"`
	CompletedAt     string                `json:"completed_at,omitempty"`
}

// BGAgentQuerier is the interface for querying background agent state
type BGAgentQuerier interface {
	QueryAgent(sessionID, agentID string, last, offset int) (*BGAgentInfo, error)
	ListAgents(sessionID string) ([]*BGAgentInfo, error)
	TerminateAgent(sessionID, agentID string) error
}

// BuildReasoningLevelParam builds the reasoning_level parameter definition dynamically,
// including any custom tier slugs from the tier config.
func BuildReasoningLevelParam(tierConfig *DelegationTierConfig) map[string]interface{} {
	enumVals := []string{"high", "medium", "low"}
	desc := "'high' for complex planning/architecture, 'medium' for standard implementation, 'low' for simple tasks like formatting/tests."
	if tierConfig != nil && len(tierConfig.Custom) > 0 {
		for slug, ct := range tierConfig.Custom {
			enumVals = append(enumVals, slug)
			desc += fmt.Sprintf(" '%s': %s.", slug, ct.Description)
		}
	}
	desc += " If not specified, uses the parent agent's model."
	return map[string]interface{}{
		"type": "string", "enum": enumVals, "description": "Optional reasoning tier for this task. " + desc,
	}
}

// ValidateReasoningLevel checks if a reasoning level is valid (built-in or custom tier).
// Returns the level if valid, empty string if not.
func ValidateReasoningLevel(ctx context.Context, level string) string {
	if level == "high" || level == "medium" || level == "low" {
		return level
	}
	// Check if it's a valid custom tier
	if tc, ok := ctx.Value(DelegationTierConfigKey).(*DelegationTierConfig); ok && tc != nil {
		if _, exists := tc.Custom[level]; exists {
			return level
		}
	}
	return "" // invalid
}

// BuildCustomTierPromptSection returns a markdown section describing custom tiers for system prompts.
// Returns empty string if no custom tiers are configured.
func BuildCustomTierPromptSection(tierConfig *DelegationTierConfig) string {
	if tierConfig == nil || len(tierConfig.Custom) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n### Custom Reasoning Tiers\n")
	sb.WriteString("In addition to high/medium/low, these custom tiers are available:\n")
	for slug, ct := range tierConfig.Custom {
		sb.WriteString(fmt.Sprintf("- `%s`: %s\n", slug, ct.Description))
	}
	return sb.String()
}

// CreateDelegationTools creates all delegation virtual tools.
// tierConfig is optional — when provided, custom tier slugs are included in reasoning_level enum.
// requireReasoningLevel — when true, reasoning_level is required on the delegate tool (used in plan/multi-agent mode).
func CreateDelegationTools(tierConfig *DelegationTierConfig, requireReasoningLevel bool) []llmtypes.Tool {
	var tools []llmtypes.Tool

	reasoningLevelParam := BuildReasoningLevelParam(tierConfig)

	// delegate tool - Execute a sub-agent to handle a task
	delegateTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "delegate",
			Description: "Delegate a task to a sub-agent for execution. Provide comprehensive, self-contained instructions.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Short, descriptive name for this agent (shown to user). E.g. 'Research APIs', 'Write tests', 'Fix auth bug'.",
					},
					"instruction": map[string]interface{}{
						"type":        "string",
						"description": "Comprehensive, self-contained instructions for the sub-agent. Include all necessary context, requirements, and expected outcomes.",
					},
					"reasoning_level": reasoningLevelParam,
					"agent_template": map[string]interface{}{
						"type":        "string",
						"description": "Sub-agent template folder name from subagents/. Loads specialized instructions, default reasoning level, and auto-activates the template's configured skills and MCP servers for the sub-agent.",
					},
					"servers": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Optional list of MCP server names for this sub-agent. When specified, the sub-agent only connects to these servers instead of all available ones. Use this to give the worker only the tools it needs, reducing noise and improving efficiency.",
					},
					"share_browser": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the sub-agent shares the parent's browser session (Playwright) or gets an isolated browser. Default: true (shared). Set to false for parallel browsing, different auth contexts, or to avoid state interference.",
					},
					"skills": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Optional list of skill folder names to attach to this sub-agent. Sub-agents start with NO skills by default — if the sub-agent needs a skill, the parent must pass it explicitly here. Use this when the sub-agent's task benefits from a specific skill's instructions (e.g. skills=[\"pdf-extract\"] for a sub-agent that processes PDFs). Do not pass skills the sub-agent does not need; each adds tokens to the sub-agent's system prompt.",
					},
				},
				"required": func() []string {
					if requireReasoningLevel {
						return []string{"name", "instruction", "reasoning_level"}
					}
					return []string{"name", "instruction"}
				}(),
			}),
		},
	}
	tools = append(tools, delegateTool)

	// query_agent - Check status of a background agent
	queryAgentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "query_agent",
			Description: "Check the status of a background agent. For running agents, returns recent conversation history (tool calls & responses). For completed agents, returns the final result. Use 'last' and 'offset' to paginate through history.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_id": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the background agent to query (returned from delegate).",
					},
					"last": map[string]interface{}{
						"type":        "integer",
						"description": "Number of recent history entries to return (default: 2). Use higher values to see more context.",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Skip this many entries from the end (default: 0). E.g. last=3, offset=5 returns entries 5th-to-8th from the end.",
					},
				},
				"required": []string{"agent_id"},
			}),
		},
	}
	tools = append(tools, queryAgentTool)

	// terminate_agent - Cancel a running background agent
	terminateAgentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "terminate_agent",
			Description: "Cancel a running background agent. The agent will be stopped and its status set to 'canceled'.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_id": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the background agent to terminate.",
					},
				},
				"required": []string{"agent_id"},
			}),
		},
	}
	tools = append(tools, terminateAgentTool)

	// list_agents - List all background agents in the session
	listAgentsTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "list_agents",
			Description: "List all background agents in the current session with their name, status, and elapsed time.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}),
		},
	}
	tools = append(tools, listAgentsTool)

	return tools
}

// CreateDelegationToolExecutors creates the execution functions for delegation tools
// Note: These executors require context injection from the server to work
func CreateDelegationToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["delegate"] = handleDelegate
	executors["query_agent"] = handleQueryAgent
	executors["terminate_agent"] = handleTerminateAgent
	executors["list_agents"] = handleListAgents

	return executors
}

// handleDelegate executes a delegated task via sub-agent
// In plan/multi-agent mode with BackgroundDelegateFunc set, delegation is ASYNC.
// In spawn mode (or when BackgroundDelegateFunc is not set), delegation is synchronous.
func handleDelegate(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract name argument (required in plan mode, optional in spawn mode)
	name, _ := args["name"].(string)

	// Extract instruction argument
	instruction, ok := args["instruction"].(string)
	if !ok || instruction == "" {
		return "", fmt.Errorf("instruction is required")
	}

	// Extract reasoning_level
	reasoningLevel, _ := args["reasoning_level"].(string)
	if reasoningLevel != "" {
		reasoningLevel = ValidateReasoningLevel(ctx, reasoningLevel)
	}

	// In multi-agent mode, reasoning_level is mandatory
	if _, isPlanMode := getBackgroundDelegate(ctx); isPlanMode && reasoningLevel == "" {
		return "", fmt.Errorf("reasoning_level is required in multi-agent mode. Use 'high' for complex tasks, 'medium' for standard implementation, or 'low' for simple tasks")
	}
	agentTemplate, _ := args["agent_template"].(string)

	// Extract optional servers array
	var delegationServers []string
	if serversRaw, ok := args["servers"].([]interface{}); ok {
		for _, s := range serversRaw {
			if str, ok := s.(string); ok && str != "" {
				delegationServers = append(delegationServers, str)
			}
		}
	}

	// Extract share_browser param (defaults to true — shared browser)
	shareBrowser := true
	if sb, ok := args["share_browser"].(bool); ok {
		shareBrowser = sb
	}

	// Extract optional skills array — explicit-pass semantics for
	// sub-agents. Parent must list every skill the sub-agent needs;
	// no inheritance from the parent's own attached skills.
	var delegationSkills []string
	if skillsRaw, ok := args["skills"].([]interface{}); ok {
		for _, s := range skillsRaw {
			if str, ok := s.(string); ok && str != "" {
				delegationSkills = append(delegationSkills, str)
			}
		}
	}

	// Check delegation depth to prevent infinite recursion
	currentDepth := SubAgentSpecFromContext(ctx).Depth

	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached - cannot delegate further to prevent infinite recursion", MaxDelegationDepth)
	}

	// The full sub-agent contract travels as one typed spec.
	childSpec := SubAgentSpec{
		Depth:          currentDepth + 1,
		ReasoningLevel: reasoningLevel,
		AgentTemplate:  agentTemplate,
		Servers:        delegationServers,
		Skills:         delegationSkills,
		ShareBrowser:   shareBrowser,
	}

	// --- ASYNC PATH: Background delegation (plan/multi-agent mode) ---
	if bgDelegate, ok := getBackgroundDelegate(ctx); ok {
		if name == "" {
			name = "Background Task" // Fallback name
		}

		bgCtx := WithSubAgentSpec(ctx, childSpec)

		agentID, err := bgDelegate(bgCtx, name, instruction)
		if err != nil {
			return "", fmt.Errorf("failed to start background agent: %w", err)
		}

		log.Printf("[DELEGATION] Started background agent '%s' (ID: %s) at depth %d", name, agentID, currentDepth+1)

		result := map[string]interface{}{
			"async":    true,
			"agent_id": agentID,
			"name":     name,
			"status":   "running",
			"message":  fmt.Sprintf("Background agent '%s' started. You'll be notified when it completes. Use query_agent(agent_id: \"%s\") to check status.", name, agentID),
		}
		resultJSON, _ := json.MarshalIndent(result, "", "  ")
		return string(resultJSON), nil
	}
	log.Printf("[DELEGATION] Background delegate unavailable; using synchronous delegation path")

	// --- SYNC PATH: Blocking delegation (spawn mode or no background func) ---
	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecuteDelegatedTaskKey).(ExecuteDelegatedTaskFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("delegation execution function not available - delegation mode may not be enabled")
	}

	log.Printf("[DELEGATION] Executing delegated task at depth %d: %s", currentDepth+1, truncateString(instruction, 100))

	startTime := time.Now()

	// Execute the delegated task with the child spec in context
	result, err := executeFunc(WithSubAgentSpec(ctx, childSpec), instruction)

	executionTime := time.Since(startTime)

	// Build result
	delegationResult := map[string]interface{}{
		"success":        err == nil,
		"result":         result,
		"execution_time": executionTime.String(),
		"completed_at":   time.Now().Format(time.RFC3339),
		"depth":          currentDepth + 1,
	}

	if err != nil {
		delegationResult["error"] = err.Error()
		log.Printf("[DELEGATION] Delegated task failed at depth %d: %v", currentDepth+1, err)
	} else {
		log.Printf("[DELEGATION] Delegated task completed at depth %d in %s", currentDepth+1, executionTime)
	}

	resultJSON, _ := json.MarshalIndent(delegationResult, "", "  ")
	return string(resultJSON), nil
}

// handleQueryAgent checks the status of a background agent
func handleQueryAgent(ctx context.Context, args map[string]interface{}) (string, error) {
	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}

	// Parse optional pagination params
	last := 2 // default
	if v, ok := args["last"].(float64); ok && v > 0 {
		last = int(v)
	}
	offset := 0
	if v, ok := args["offset"].(float64); ok && v > 0 {
		offset = int(v)
	}

	querier, ok := ctx.Value(BGAgentRegistryKey).(BGAgentQuerier)
	if !ok || querier == nil {
		return "", fmt.Errorf("background agent management not available")
	}

	sessionID, _ := ctx.Value(BGAgentSessionIDKey).(string)
	if sessionID == "" {
		return "", fmt.Errorf("session ID not available")
	}

	info, err := querier.QueryAgent(sessionID, agentID, last, offset)
	if err != nil {
		return "", err
	}

	resultJSON, _ := json.MarshalIndent(info, "", "  ")
	return string(resultJSON), nil
}

// handleTerminateAgent cancels a running background agent
func handleTerminateAgent(ctx context.Context, args map[string]interface{}) (string, error) {
	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}

	querier, ok := ctx.Value(BGAgentRegistryKey).(BGAgentQuerier)
	if !ok || querier == nil {
		return "", fmt.Errorf("background agent management not available")
	}

	sessionID, _ := ctx.Value(BGAgentSessionIDKey).(string)
	if sessionID == "" {
		return "", fmt.Errorf("session ID not available")
	}

	if err := querier.TerminateAgent(sessionID, agentID); err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"agent_id": agentID,
		"status":   "canceled",
		"message":  fmt.Sprintf("Agent %s has been terminated.", agentID),
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleListAgents lists all background agents in the session
func handleListAgents(ctx context.Context, args map[string]interface{}) (string, error) {
	querier, ok := ctx.Value(BGAgentRegistryKey).(BGAgentQuerier)
	if !ok || querier == nil {
		return "", fmt.Errorf("background agent management not available")
	}

	sessionID, _ := ctx.Value(BGAgentSessionIDKey).(string)
	if sessionID == "" {
		return "", fmt.Errorf("session ID not available")
	}

	agents, err := querier.ListAgents(sessionID)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// BuildSpawnCapabilitiesSection returns a prompt section listing available sub-agent templates.
// Skills are already listed by buildSkillPrompt — only sub-agent templates are listed here.
// Appended to the spawn-mode agent's system prompt so it knows what templates
// are available when calling delegate().
func BuildSpawnCapabilitiesSection(caps *CapabilitiesContext) string {
	if caps == nil || len(caps.SubAgentTemplates) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Sub-Agent Templates\n")
	sb.WriteString("Pass `agent_template: \"<folder_name>\"` in delegate() to apply a template's specialized instructions:\n")
	for _, tmpl := range caps.SubAgentTemplates {
		line := fmt.Sprintf("- **%s** (`subagents/%s/`): %s", tmpl.Name, tmpl.FolderName, tmpl.Description)
		if tmpl.DefaultReasoningLevel != "" {
			line += fmt.Sprintf(" [reasoning: %s]", tmpl.DefaultReasoningLevel)
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// GetMultiAgentDelegationInstructions returns the system prompt for multi-agent chat.
// chatsFolder is the session's per-user Chats folder (e.g. "_users/alice/Chats"). Fallback
// to the global "Chats" constant when empty for backwards compatibility.
// Every sub-agent task is delegated in code_execution mode; workers run asynchronously
// and auto-notify the manager when they complete.
func GetMultiAgentDelegationInstructions(chatsFolder string) string {
	return GetMultiAgentDelegationInstructionsWithUser(chatsFolder, "")
}

func GetMultiAgentDelegationInstructionsWithUser(chatsFolder string, userID string) string {
	if chatsFolder == "" {
		chatsFolder = ChatsFolderPath
	}
	if userID == "" {
		userID = "default"
	}

	// Schedule + Secret management used to be ~80 lines of inline detail.
	// Both are rare-path topics: most chat turns do not touch schedules or
	// secrets at all. They moved to templates/system/{schedule-management,
	// secret-management}.md, loaded via get_reference_doc when the user
	// actually asks. Keep brief cheat sheets here so the agent knows the
	// capabilities exist and which doc to load.
	scheduleInstructions := `
## Schedule Management (brief)

Schedules are server-managed in ` + "`_users/" + userID + "/multiagent-schedules.json`" + ` with ` + "`mode: \"multi-agent\"`" + `; tool changes activate immediately.

**When scheduling:** confirm what/when/timezone, call ` + "`list_multiagent_schedules`" + `, then ` + "`create_multiagent_schedule`" + ` / ` + "`update_multiagent_schedule`" + ` / ` + "`delete_multiagent_schedule`" + ` / ` + "`trigger_multiagent_schedule`" + `. Do not edit the JSON directly.

**For formats, cron examples, update/remove flows, call:**
` + "`get_reference_doc(kind=\"schedule-management\")`" + ` before changing schedules.

## Secret Management (brief)

Buckets: **workflow** (scoped to workflow), **user** (reusable), **global** (read-only). Tools: ` + "`list_secrets`" + `, ` + "`set_workflow_secret`" + `, ` + "`delete_workflow_secret`" + `, ` + "`set_user_secret`" + `, ` + "`delete_user_secret`" + `.

**Hard rules:** never echo / print / log a plaintext secret value; acknowledge by name only. ` + "`set_workflow_secret`" + ` / ` + "`set_user_secret`" + ` inject ` + "`$SECRET_<NAME>`" + ` into the shell — usable immediately without config update.

**For full bucket semantics, naming rules, safety rules, call:**
` + "`get_reference_doc(kind=\"secret-management\")`" + ` before any set / delete / attach.
`

	return scheduleInstructions + `
## Your Role — Chief of Staff

You are the user's **chief of staff**. The user's standing work runs as **automations** — workflows under ` + "`Workflow/`" + `, each one **one capability** with its own plan, accumulated experience (` + "`knowledgebase/`" + ` + ` + "`db/`" + `), Pulse verdicts, and track record (` + "`runs/`" + `). Your job is to manage these workflows against the org goals on the user's behalf — read them, monitor them, build on top of what they produce, and report back — and to handle ad-hoc requests yourself by dispatching temporary sub-agents (contractors).

For recurring scheduled Chief of Staff tasks, durable context lives in ` + "`pulse/task.html`" + `. Read the prior entries for the same task before acting, then let the post-run report update capture the new findings. Each workflow's own work and knowledge stays in that workflow's KB/db, not in Chief of Staff chat.

### Org Goals

Org goals live in the local workspace file ` + "`pulse/goals.html`" + ` (docs root + ` + "`/pulse/goals.html`" + `). Org Pulse lives in local ` + "`pulse/org-pulse.html`" + `. Manage workflows against them using Pulse verdicts, reports, db, and run artifacts. Load ` + "`get_reference_doc(kind=\"org-goals\")`" + ` before goal/alignment/performance work, and ` + "`get_reference_doc(kind=\"org-html\")`" + ` before editing. Never WebFetch raw GitHub URLs for these files or reference docs.

Mechanically you are an **orchestrator**: you decompose work and dispatch sub-agents, and you use tools directly for simple tasks.

**When to delegate:** Multi-step work, parallel tasks, complex analysis, writing reports/scripts, browser automation, anything that benefits from focused execution.
**When to act directly:** Quick single-tool calls (read a file, simple search, list workflows), conversational replies, planning/decomposition.
**Rule of thumb:** 1-2 tool calls → do it yourself. 3+ tool calls or focused work → delegate.

### delegate(name, instruction, reasoning_level)

Spawns an async sub-agent. Call multiple in one turn for parallel execution.

| Parameter | Required | Description |
|-----------|----------|-------------|
| name | yes | Short label shown to user ("Analyze Sales Data") |
| instruction | yes | Self-contained task — include ALL context, paths, requirements. Workers do not share hidden context with you. |
| reasoning_level | yes | ` + "`high`" + ` (architecture/complex), ` + "`medium`" + ` (standard), ` + "`low`" + ` (simple reads/lookups) |
| agent_template | no | Folder from ` + "`subagents/`" + ` — loads a specialized profile |
| servers | no | MCP server names to scope the worker's tools |

Other tools: ` + "`query_agent(agent_id)`" + `, ` + "`terminate_agent(agent_id)`" + `, ` + "`list_agents()`" + `

### Workflow Runs

Chief of Staff does **not** run workflows directly right now. The user runs workflows manually from the automation UI when they want execution.

**How to handle workflow execution requests:**
1. Find the workflow path — ` + "`execute_shell_command(command: \"ls Workflow/\")`" + `
2. Find available groups — ` + "`execute_shell_command(command: \"cat Workflow/<name>/variables/variables.json\")`" + ` and look at the ` + "`groups`" + ` array
3. Tell the user which workflow/group to run manually and what context or route choice to use.
4. After the user has run it, inspect the latest output in ` + "`Workflow/<name>/runs/iteration-0/<group>/`" + `.
5. If local ` + "`pulse/goals.html`" + ` exists under the docs root, load ` + "`get_reference_doc(kind=\"org-goals\")`" + ` and produce **Org goal alignment**: goal, workflow/group, status, evidence path, gap, next action. Use the run folder, ` + "`builder/improve.html`" + `, ` + "`reports/`" + `, and ` + "`db/db.sqlite`" + `. Edit local ` + "`pulse/goals.html`" + ` only for concrete scorecard changes after loading ` + "`org-html`" + `; otherwise classify supporting/unaligned.

### Reading workflow state

When the user asks what a workflow produced, knows, or should improve, load ` + "`get_reference_doc(kind=\"file-layout\")`" + ` and ` + "`get_reference_doc(kind=\"stores\")`" + ` for the deep filesystem contract, then inspect the right source:

- **Plan/config:** ` + "`workflow.json`" + `, ` + "`soul/soul.md`" + `, ` + "`planning/plan.json`" + `, ` + "`planning/step_config.json`" + `, ` + "`variables/variables.json`" + `.
- **Reports:** ` + "`reports/report_plan.json`" + ` plus the registered HTML documents under ` + "`db/reports/`" + `; reports read ` + "`db/db.sqlite`" + ` live through ` + "`window.report`" + `.
- **Database:** ` + "`db/README.md`" + ` for table contracts, then ` + "`db/db.sqlite`" + ` schema/tables/rows for accumulated structured state; ` + "`db/assets/`" + ` for durable files referenced by rows or reports.
- **Knowledge:** ` + "`knowledgebase/context/context.md`" + ` for user-supplied runtime context, ` + "`knowledgebase/notes/_index.json`" + ` + selected ` + "`knowledgebase/notes/*.md`" + ` for workflow-discovered narrative knowledge.
- **How-to skill:** ` + "`learnings/_global/SKILL.md`" + ` and relevant ` + "`learnings/<step-id>/main.py`" + ` for scripted steps.
- **Runtime evidence:** latest ` + "`runs/iteration-0/<group>/`" + ` outputs/logs/timing, ` + "`costs/`" + `, Pulse verdicts and Chief recommendation cards in ` + "`builder/improve.html`" + `.
- **External capabilities:** selected workflow skills/servers from ` + "`workflow.json`" + ` and per-step ` + "`enabled_skills`" + ` in ` + "`planning/step_config.json`" + `; installed skills live at workspace-root ` + "`skills/<folder>/SKILL.md`" + `.

Org-level goals live in local ` + "`pulse/goals.html`" + ` and are what you manage against.

Read workflow files with shell tools, but **do not modify workflow internals** from Chief of Staff chat. The narrow exception is recommendation logging: when you find a workflow-specific improvement opportunity, you may edit that workflow's ` + "`builder/improve.html`" + ` to add a newest-first **Chief of Staff recommendation** / **Open finding** card. Make the handoff goal-aligned enough for the workflow builder to act: name the org goal/KPI target or say "supporting/no explicit goal", classify the workflow as ` + "`aligned`" + ` / ` + "`supporting`" + ` / ` + "`unaligned`" + ` / ` + "`unknown-measurement`" + `, cite evidence paths, state the gap, assign priority, propose the builder action (` + "`harden_workflow`" + `, ` + "`replan_workflow_from_results`" + `, eval/report measurement fix, manual review, or no-action watchpoint), and say what KPI/success-criteria impact the change should produce. Do not edit ` + "`workflow.json`" + `, ` + "`planning/`" + `, prompts, config, steps, reports, DB, KB, or learnings from Chief of Staff. If the user wants to change how a workflow works, tell them to open it in the builder. Org-level goals are the other exception: they live outside workflows in local ` + "`pulse/goals.html`" + `.

### notify_user — proactively reach the user

` + "`notify_user(message_for_user)`" + ` pushes a message to the user's connected channels (Slack / WhatsApp / email). Use it when work you started **completes detached from the current turn** and the user is not watching this thread — an async ` + "`delegate`" + ` finished, or a schedule you set fired. In a deployed bot channel it's how you say "done — here's the result" after you've already ended the turn.

- **Don't** use it for your normal reply. When you're answering inline in this conversation, just reply — that text already reaches the user. ` + "`notify_user`" + ` is for the out-of-band ping, not a duplicate of your answer.
- One call fans out to every connected channel. If an email channel is connected the tool also offers ` + "`email_subject`" + ` / ` + "`email_body`" + ` (and ` + "`email_cc`" + ` / ` + "`email_html`" + ` / ` + "`email_attachments`" + `) so the email reads fuller than the terse chat line. It reports back per-channel delivery; if no channel is connected it's a harmless no-op.

### Process

1. Understand request → decompose into parallel sub-tasks → delegate → tell user what's happening → end turn.
2. On notification: review results → re-delegate if needed → final summary when done. If the user has stepped away or asked to be pinged, ` + "`notify_user`" + ` the result.

### Rules

- **File outputs** go under ` + "`" + chatsFolder + "/<descriptive-name>/`" + `. Include the path in each delegate instruction.
- **Self-contained instructions** — every delegate call must include all context the worker needs.
- **Prefer parallel** — multiple delegates in one turn. Don't serialize independent work.
- **Quality gate** — review sub-agent results before reporting to user. Re-delegate if wrong.
- **Communication** — speak as if you did the work yourself. Never mention "sub-agents", "delegation", "workers", or tool names to the user. Always include actual content in your reply — tool outputs are not visible to the user.
`
}

// BuildCLIToolEnvironmentPrompt returns additional instructions for CLI providers
// that explain how to call api-bridge tools and route human/custom tools through HTTP.
func BuildCLIToolEnvironmentPrompt(provider string) string {
	executeTool := "mcp_api-bridge_execute_shell_command"
	specTool := "mcp_api-bridge_get_api_spec"
	browserTool := "mcp_api-bridge_agent_browser"
	if provider == "claude-code" {
		executeTool = "mcp__api-bridge__execute_shell_command"
		specTool = "mcp__api-bridge__get_api_spec"
		browserTool = "mcp__api-bridge__agent_browser"
	}

	return `
## CLI Tool Environment (CRITICAL — Read Carefully)

Your native tools (Bash, Read, Write, etc.) are **disabled**. All tool access goes through the MCP api-bridge. Your available tools are:

| Tool name | Purpose |
|-----------|---------|
| ` + "`" + executeTool + "`" + ` | Run any shell command (replaces Bash/Read/Write) |
| ` + "`" + specTool + "`" + ` | Discover human/custom tools and their API specs |
| ` + "`" + browserTool + "`" + ` | Browser automation |
| ` + "`WebSearch`" + ` | Web search |

### Tool Name Mapping
Whenever the instructions above mention ` + "`execute_shell_command(...)`" + `, call ` + "`" + executeTool + "`" + ` instead. Example:
- Instructions say: execute_shell_command(command: "ls Chats/")
- You call: ` + executeTool + `(command: "ls Chats/")

### Calling Custom Tools via HTTP API
The following tools are NOT available as direct function calls — call them via curl through ` + "`" + executeTool + "`" + `:

- **Delegation tools**: delegate, query_agent, terminate_agent, list_agents
- **Human tools**: notify_user, submit_human_answer
- **LLM config tools**: list_published_llms, list_provider_models, test_llm, save_published_llm, set_provider_auth, list_llm_capabilities, estimate_llm_cost

**Pattern:**
` + "```" + `bash
payload='{...parameters...}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/{tool_name}"
` + "```" + `

**Examples:**

delegate a task:
` + "```" + `bash
payload='{"instruction": "Your task instructions here", "reasoning_level": "medium"}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/delegate"
` + "```" + `

list published chat LLMs:
` + "```" + `bash
payload='{}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/list_published_llms"
` + "```" + `

list provider models:
` + "```" + `bash
payload='{"provider": "claude-code"}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/list_provider_models"
` + "```" + `

notify the user:
` + "```" + `bash
payload='{"message_for_user": "Done"}'
curl -sS --json "$payload" -H "$MCP_AUTH" "$MCP_CUSTOM/notify_user"
` + "```" + `

$MCP_CUSTOM and $MCP_AUTH are pre-set environment variables — use them as-is.

**Important:** Whenever instructions mention ` + "`delegate(...)`" + `, ` + "`notify_user(...)`" + `, or LLM config tools, translate to the curl pattern above. Do NOT call these as direct function calls.

Do **NOT** read or edit ` + "`config/`" + ` files for LLM/provider configuration. Use ` + "`list_published_llms`" + ` for the published set, ` + "`list_provider_models`" + ` for provider-supported models, ` + "`test_llm`" + ` for candidate validation, and ` + "`save_published_llm`" + ` for publishing.

### Durable context
For Chief of Staff scheduled tasks, use ` + "`pulse/task.html`" + ` as the durable task context. Do not create or depend on separate memory files.
`
}
