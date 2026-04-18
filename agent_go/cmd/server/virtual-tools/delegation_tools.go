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
	// DelegationDepthKey is the context key for tracking delegation depth
	DelegationDepthKey delegationContextKey = "delegation_depth"
	// MaxDelegationDepth is the maximum allowed delegation depth to prevent infinite recursion
	MaxDelegationDepth = 3
	// WorkspaceClientKey is the context key for the workspace client (plan file I/O)
	WorkspaceClientKey delegationContextKey = "workspace_client"
	// DelegationTierConfigKey is the context key for the delegation tier configuration
	DelegationTierConfigKey delegationContextKey = "delegation_tier_config"
	// ReasoningLevelKey is the context key for the reasoning level of a delegation
	ReasoningLevelKey delegationContextKey = "reasoning_level"
	// CapabilitiesContextKey is the context key for available capabilities (MCP servers, skills, etc.)
	CapabilitiesContextKey delegationContextKey = "capabilities_context"
	// AgentTemplateKey is the context key for the sub-agent template folder name
	AgentTemplateKey delegationContextKey = "agent_template"
	// SessionEventEmitterKey is the context key for the session event emitter (used for human feedback UI)
	SessionEventEmitterKey delegationContextKey = "session_event_emitter"
	// DelegationServersKey is the context key for sub-agent specific MCP server selection
	DelegationServersKey delegationContextKey = "delegation_servers"
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
	// BackgroundAgentIDKey is the context key for linking background agents to their delegation
	BackgroundAgentIDKey delegationContextKey = "background_agent_id"
	// ShareBrowserKey is the context key for controlling browser session isolation in sub-agents
	ShareBrowserKey delegationContextKey = "share_browser"
)

// SessionEventEmitter is the interface for emitting human-feedback events to the session event store
type SessionEventEmitter interface {
	EmitBlockingHumanFeedback(requestID, question, context string, yesNoOnly bool, yesLabel, noLabel string, options ...string)
	EmitBlockingHumanQuestions(requestID string, questions []map[string]string)
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
	Main   *TierModel                  `json:"main,omitempty"` // orchestrator/main agent model
	High   *TierModel                  `json:"high,omitempty"`
	Medium *TierModel                  `json:"medium,omitempty"`
	Low    *TierModel                  `json:"low,omitempty"`
	Custom map[string]*CustomTierModel `json:"custom,omitempty"`
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
	if _, isPlanMode := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc); isPlanMode && reasoningLevel == "" {
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

	// Check delegation depth to prevent infinite recursion
	currentDepth := 0
	if depth, ok := ctx.Value(DelegationDepthKey).(int); ok {
		currentDepth = depth
	}

	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached - cannot delegate further to prevent infinite recursion", MaxDelegationDepth)
	}

	// --- ASYNC PATH: Background delegation (plan/multi-agent mode) ---
	if bgDelegate, ok := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc); ok && bgDelegate != nil {
		if name == "" {
			name = "Background Task" // Fallback name
		}

		// Pass context values to the background delegate function via context
		bgCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
		if reasoningLevel != "" {
			bgCtx = context.WithValue(bgCtx, ReasoningLevelKey, reasoningLevel)
		}
		if agentTemplate != "" {
			bgCtx = context.WithValue(bgCtx, AgentTemplateKey, agentTemplate)
		}
		if len(delegationServers) > 0 {
			bgCtx = context.WithValue(bgCtx, DelegationServersKey, delegationServers)
		}
		if !shareBrowser {
			bgCtx = context.WithValue(bgCtx, ShareBrowserKey, false)
		}

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

	// --- SYNC PATH: Blocking delegation (spawn mode or no background func) ---
	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecuteDelegatedTaskKey).(ExecuteDelegatedTaskFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("delegation execution function not available - delegation mode may not be enabled")
	}

	log.Printf("[DELEGATION] Executing delegated task at depth %d: %s", currentDepth+1, truncateString(instruction, 100))

	startTime := time.Now()

	// Increment depth in context for the sub-agent and pass reasoning level
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
	if reasoningLevel != "" {
		subCtx = context.WithValue(subCtx, ReasoningLevelKey, reasoningLevel)
	}
	if agentTemplate != "" {
		subCtx = context.WithValue(subCtx, AgentTemplateKey, agentTemplate)
	}
	if len(delegationServers) > 0 {
		subCtx = context.WithValue(subCtx, DelegationServersKey, delegationServers)
	}
	if !shareBrowser {
		subCtx = context.WithValue(subCtx, ShareBrowserKey, false)
	}

	// Execute the delegated task
	result, err := executeFunc(subCtx, instruction)

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

	scheduleInstructions := `
## Schedule Management

You can create scheduled tasks that run automatically on a cron schedule. Schedules are stored in ` + "`_users/" + userID + "/multiagent-schedules.json`" + `.

### File Format

` + "```json" + `
{
  "schedules": [
    {
      "id": "unique-uuid",
      "name": "Human-readable name",
      "description": "What this schedule does",
      "cron_expression": "0 9 * * *",
      "timezone": "America/New_York",
      "enabled": true,
      "mode": "multi-agent",
      "query": "The message/instruction to execute on schedule"
    }
  ],
  "capabilities": {
    "selected_servers": [],
    "selected_tools": [],
    "selected_skills": [],
    "selected_secrets": [],
    "browser_mode": "none",
    "use_code_execution_mode": false
  }
}
` + "```" + `

### How to manage schedules

Use ` + "`execute_shell_command`" + ` to read and write the schedule file:

**List schedules:**
` + "```" + `bash
cat _users/` + userID + `/multiagent-schedules.json 2>/dev/null || echo '{"schedules":[],"capabilities":{}}'
` + "```" + `

**Create/update schedules:** Read the file, modify the JSON (add/update/remove entries), write it back. Use ` + "`python3`" + ` or ` + "`jq`" + ` for JSON manipulation. Always generate a UUID for new schedule IDs (` + "`python3 -c \"import uuid; print(uuid.uuid4())\"`" + `).

**Cron expression examples:**
- ` + "`0 9 * * *`" + ` — daily at 9:00 AM
- ` + "`0 9 * * 1-5`" + ` — weekdays at 9:00 AM
- ` + "`*/30 * * * *`" + ` — every 30 minutes
- ` + "`0 0 1 * *`" + ` — first day of each month at midnight

**Important:** The scheduler picks up file changes automatically. After writing the file, the schedule will be active within 60 seconds.

### When users ask to schedule something

1. Confirm the schedule details (what to run, when, timezone)
2. Read the current schedule file
3. Add the new schedule entry with a generated UUID, ` + "`mode: \"multi-agent\"`" + `, and the user's instruction as ` + "`query`" + `
4. Write the updated file back
5. Confirm to the user what was scheduled
`

	return scheduleInstructions + `
## Orchestrator Mode

You are an **orchestrator**. You decompose work and dispatch sub-agents. You can also use tools directly for simple tasks.

**When to delegate:** Multi-step work, parallel tasks, complex analysis, writing reports/scripts, browser automation, anything that benefits from focused execution.
**When to act directly:** Quick single-tool calls (read a file, simple search, memory ops, list workflows), conversational replies, planning/decomposition.
**Rule of thumb:** 1-2 tool calls → do it yourself. 3+ tool calls or focused work → delegate.

### delegate(name, instruction, reasoning_level)

Spawns an async sub-agent. Call multiple in one turn for parallel execution.

| Parameter | Required | Description |
|-----------|----------|-------------|
| name | yes | Short label shown to user ("Analyze Sales Data") |
| instruction | yes | Self-contained task — include ALL context, paths, requirements. Workers have no shared memory. |
| reasoning_level | yes | ` + "`high`" + ` (architecture/complex), ` + "`medium`" + ` (standard), ` + "`low`" + ` (simple reads/lookups) |
| agent_template | no | Folder from ` + "`subagents/`" + ` — loads a specialized profile |
| servers | no | MCP server names to scope the worker's tools |

Other tools: ` + "`query_agent(agent_id)`" + `, ` + "`terminate_agent(agent_id)`" + `, ` + "`list_agents()`" + `

### run_workflow / run_step — Execute Existing Workflows

Run a workflow or single step in the background. Returns an agent_id visible in ` + "`list_agents()`" + ` and stoppable via ` + "`terminate_agent()`" + `.

| Tool | Parameters | Description |
|------|-----------|-------------|
| run_workflow | workflow_path, group_name | Run all steps for a group |
| run_step | workflow_path, step_id, group_name | Run one step for a group |

**How to use:**
1. Find the workflow path — check the employee assignments above, or ` + "`execute_shell_command(command: \"ls Workflow/\")`" + `
2. Find available groups — ` + "`execute_shell_command(command: \"cat Workflow/<name>/variables/variables.json\")`" + ` and look at the ` + "`groups`" + ` array
3. Find step IDs (for run_step) — ` + "`execute_shell_command(command: \"cat Workflow/<name>/planning/plan.json\")`" + ` and look at step ` + "`id`" + ` fields
4. Call the tool — per-run output appears in ` + "`Workflow/<name>/runs/iteration-0/<group>/execution/<step-id>/`" + `

### Reading workflow state

When the user asks "what did the workflow extract / produce / know?", each workflow has four places where state lives. Pick the right one for the question:

| User question | Where to look |
|---|---|
| "Latest results for group X?" | ` + "`Workflow/<name>/runs/iteration-0/<group>/execution/`" + ` (per-run step outputs, JSON) |
| "What state has the workflow accumulated across runs?" | ` + "`Workflow/<name>/db/*.json`" + ` (structured rows, upsert-by-key across runs) |
| "What does the workflow know about <company / person / domain>?" | ` + "`Workflow/<name>/knowledgebase/graph.json`" + ` (accumulated facts — entities + relationships). ` + "`knowledgebase/index.json`" + ` has a lightweight summary (counts, types). |
| "How / why does the workflow do X?" | ` + "`Workflow/<name>/learnings/_global/SKILL.md`" + ` (HOW-to-run notes kept by the learning agent) |

` + "`cat`" + ` / ` + "`jq`" + ` these files directly via ` + "`execute_shell_command`" + `. **Do NOT modify them** — workflow internals (step configs, KB settings, learnings) belong to the workflow builder, not this chat. If the user wants to change how a workflow works, tell them to open it in the builder.

### Process

1. Understand request → decompose into parallel sub-tasks → delegate → tell user what's happening → end turn.
2. On notification: review results → re-delegate if needed → final summary when done.

### Rules

- **File outputs** go under ` + "`" + chatsFolder + "/<descriptive-name>/`" + `. Include the path in each delegate instruction.
- **Self-contained instructions** — every delegate call must include all context the worker needs.
- **Prefer parallel** — multiple delegates in one turn. Don't serialize independent work.
- **Quality gate** — review sub-agent results before reporting to user. Re-delegate if wrong.
- **Communication** — speak as if you did the work yourself. Never mention "sub-agents", "delegation", "workers", or tool names to the user. Always include actual content in your reply — tool outputs are not visible to the user.
`
}

// GetClaudeCodeDelegationOverride returns additional instructions for Claude Code providers
// that explain how to call "human" category tools via the HTTP API instead of direct function calls.
func GetClaudeCodeDelegationOverride() string {
	return `
## CLI Tool Environment (CRITICAL — Read Carefully)

Your native tools (Bash, Read, Write, etc.) are **disabled**. All tool access goes through the MCP api-bridge. Your available tools are:

| Tool name | Purpose |
|-----------|---------|
| ` + "`mcp__api-bridge__execute_shell_command`" + ` | Run any shell command (replaces Bash/Read/Write) |
| ` + "`mcp__api-bridge__get_api_spec`" + ` | Discover human/custom tools and their API specs |
| ` + "`mcp__api-bridge__agent_browser`" + ` | Browser automation |
| ` + "`WebSearch`" + ` | Web search |

### Tool Name Mapping
Whenever the instructions above mention ` + "`execute_shell_command(...)`" + `, call ` + "`mcp__api-bridge__execute_shell_command`" + ` instead. Example:
- Instructions say: execute_shell_command(command: "ls Chats/")
- You call: mcp__api-bridge__execute_shell_command(command: "ls Chats/")

### Calling Custom Tools via HTTP API
The following tools are NOT available as direct function calls — call them via curl through ` + "`mcp__api-bridge__execute_shell_command`" + `:

- **Delegation tools**: delegate, query_agent, terminate_agent, list_agents
- **Memory tools**: save_memory, recall_memory, enrich_memory

**Pattern:**
` + "```" + `bash
curl -s -X POST "$MCP_API_URL/tools/custom/{tool_name}" \
  -H "Authorization: Bearer $MCP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{...parameters...}'
` + "```" + `

**Examples:**

delegate a task:
` + "```" + `bash
curl -s -X POST "$MCP_API_URL/tools/custom/delegate" \
  -H "Authorization: Bearer $MCP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"instruction": "Your task instructions here", "reasoning_level": "medium"}'
` + "```" + `

save a memory:
` + "```" + `bash
curl -s -X POST "$MCP_API_URL/tools/custom/save_memory" \
  -H "Authorization: Bearer $MCP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content": "Important context to remember"}'
` + "```" + `

$MCP_API_URL and $MCP_API_TOKEN are pre-set environment variables — use them as-is.

**Important:** Whenever instructions mention ` + "`delegate(...)`" + ` or ` + "`save_memory(...)`" + `, translate to the curl pattern above. Do NOT call these as direct function calls.

### Memory — CRITICAL
Do **NOT** use your native memory system (e.g. ~/.codex/memories/, ~/.claude/memories/, or any home-directory path). Those paths are not accessible in this environment.

All memory operations **must** go through the ` + "`save_memory`" + ` / ` + "`recall_memory`" + ` curl API above. This stores memories in the workspace ` + "`memories/`" + ` folder where they are visible and persistent.
`
}
