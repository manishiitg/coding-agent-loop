package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/workspace"

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
	// PlanEventEmitterKey is the context key for emitting workspace file events when plans are saved
	PlanEventEmitterKey delegationContextKey = "plan_event_emitter"
	// PlanFolderKey is the context key for the plan-specific output folder (e.g. "Chats/{planID}")
	PlanFolderKey delegationContextKey = "plan_folder"
	// CapabilitiesContextKey is the context key for available capabilities (MCP servers, skills, etc.)
	CapabilitiesContextKey delegationContextKey = "capabilities_context"
	// ToolModeKey is the context key for the tool mode of a delegated task ("simple", "code_execution", "tool_search")
	ToolModeKey delegationContextKey = "tool_mode"
	// AgentTemplateKey is the context key for the sub-agent template folder name
	AgentTemplateKey delegationContextKey = "agent_template"
	// PlanTrackerKey is the context key for tracking whether a plan has been created in this session
	PlanTrackerKey delegationContextKey = "plan_tracker"
	// PlanSessionStateKey is the context key for the session-level plan phase state
	PlanSessionStateKey delegationContextKey = "plan_session_state"
	// SessionEventEmitterKey is the context key for the session event emitter (used by confirm_plan_execution)
	SessionEventEmitterKey delegationContextKey = "session_event_emitter"
	// DelegationServersKey is the context key for sub-agent specific MCP server selection
	DelegationServersKey delegationContextKey = "delegation_servers"
	// PlanFileFolderPath is the workspace folder for delegation plan files
	PlanFileFolderPath = "Plans"
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

// PlanEventEmitter is the interface for emitting workspace file events when plans are saved
type PlanEventEmitter interface {
	EmitFileEvent(filepath string)
}

// SessionEventEmitter is the interface for emitting arbitrary events to the session event store
type SessionEventEmitter interface {
	EmitBlockingHumanFeedback(requestID, question, context string, yesNoOnly bool, yesLabel, noLabel string, options ...string)
	EmitBlockingHumanQuestions(requestID string, questions []map[string]string)
	EmitPlanApproval(question, contextText, yesLabel string)
}

// PlanSessionState tracks session-level plan state for multi-agent mode.
// It replaces PlanTracker and adds phase tracking (planning vs execution).
// Shared across tool calls within a session to enforce one-plan-per-conversation.
type PlanSessionState struct {
	mu         sync.Mutex
	Phase      string // "planning" or "execution"
	PlanID     string
	PlanFolder string
}

// NewPlanSessionState creates a new plan session state for a session
func NewPlanSessionState() *PlanSessionState {
	return &PlanSessionState{Phase: "planning"}
}

// TryCreate atomically checks if a plan already exists. If not, records the new plan
// and returns true. If a plan already exists, returns the existing plan's ID and folder
// so the caller can reuse it (plan.md gets overwritten in the same folder).
func (ps *PlanSessionState) TryCreate(planID, planFolder string) (existingPlanID, existingPlanFolder string, ok bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.PlanID != "" {
		// Plan already exists — return existing folder so caller reuses it
		existingPlanID = ps.PlanID
		existingPlanFolder = ps.PlanFolder
		ps.Phase = "planning" // Reset to planning phase for re-planning
		return existingPlanID, existingPlanFolder, true
	}
	ps.PlanID = planID
	ps.PlanFolder = planFolder
	return "", "", true
}

// GetPhase returns the current phase (thread-safe)
func (ps *PlanSessionState) GetPhase() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.Phase
}

// SetPhase sets the current phase (thread-safe)
func (ps *PlanSessionState) SetPhase(phase string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.Phase = phase
}

// PlanTracker is an alias for backward compatibility
type PlanTracker = PlanSessionState

// NewPlanTracker creates a new plan tracker (backward-compatible alias)
func NewPlanTracker() *PlanTracker {
	return NewPlanSessionState()
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
	DefaultToolMode       string `json:"default_tool_mode,omitempty"`
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
					"plan_folder": map[string]interface{}{
						"type":        "string",
						"description": "Optional plan folder path (e.g. 'Chats/{plan_id}'). When set, the worker's write access is restricted to this folder only. Always pass this when executing tasks from a plan.",
					},
					"tool_mode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"simple", "code_execution", "tool_search"},
						"description": "Tool access mode for the worker. 'simple' (default): worker gets all tools directly and calls them normally — use this for most tasks including writing scripts, file editing, shell commands. 'code_execution': worker writes Go code that calls MCP tools programmatically — ONLY use when the user explicitly requests code execution mode. Do NOT choose this on your own. 'tool_search': worker discovers tools on-demand via search — use when 3+ MCP servers are available so the worker can efficiently find the right tools.",
					},
					"agent_template": map[string]interface{}{
						"type":        "string",
						"description": "Sub-agent template folder name from subagents/. Loads specialized instructions, default reasoning level, default tool mode, and auto-activates the template's configured skills and MCP servers for the sub-agent.",
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

	// create_delegation_plan - Delegate planning to a sub-agent that writes plan.md
	createPlanTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "create_delegation_plan",
			Description: "Delegate planning to a sub-agent that analyzes the objective, creates a strategy, and writes a plan.md todo list to workspace. The plan file appears in Chats/{plan_name}/ for tracking. After the plan is created, read plan.md and delegate tasks from it using the delegate tool.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan_name": map[string]interface{}{
						"type":        "string",
						"description": "Short kebab-case name for the plan folder (e.g. 'user-auth', 'api-refactor', 'bug-fix-login'). This becomes the folder name under Chats/. Keep it concise (2-4 words, kebab-case).",
					},
					"objective": map[string]interface{}{
						"type":        "string",
						"description": "What needs to be accomplished. The planner sub-agent will analyze this and create a task breakdown.",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Optional additional context for the planner: constraints, file paths, tech stack, or any information that helps create a better plan.",
					},
					"reasoning_level": reasoningLevelParam,
				},
				"required": []string{"plan_name", "objective"},
			}),
		},
	}
	tools = append(tools, createPlanTool)

	// confirm_plan_execution - Blocking approval tool for plan execution
	confirmPlanTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "confirm_plan_execution",
			Description: "Present the plan to the user for approval. This tool BLOCKS until the user responds. If approved, switches to Execution Mode. If rejected, returns the user's feedback so you can adjust the plan. Call this after creating a plan and summarizing it.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan_summary": map[string]interface{}{
						"type":        "string",
						"description": "Brief summary of the plan to show the user (key phases, tasks, models used)",
					},
				},
				"required": []string{"plan_summary"},
			}),
		},
	}
	tools = append(tools, confirmPlanTool)

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
	executors["create_delegation_plan"] = handleCreateDelegationPlan
	executors["confirm_plan_execution"] = handleConfirmPlanExecution
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

	// Extract reasoning_level, plan_folder, and tool_mode
	reasoningLevel, _ := args["reasoning_level"].(string)
	if reasoningLevel != "" {
		reasoningLevel = ValidateReasoningLevel(ctx, reasoningLevel)
	}

	// In plan/multi-agent mode, reasoning_level is mandatory
	if _, isPlanMode := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc); isPlanMode && reasoningLevel == "" {
		return "", fmt.Errorf("reasoning_level is required in multi-agent mode. Use 'high' for complex tasks, 'medium' for standard implementation, or 'low' for simple tasks")
	}
	planFolder, _ := args["plan_folder"].(string)
	toolMode, _ := args["tool_mode"].(string)
	if toolMode != "" && toolMode != "simple" && toolMode != "code_execution" && toolMode != "tool_search" {
		toolMode = "" // Ignore invalid values
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
		if planFolder != "" {
			bgCtx = context.WithValue(bgCtx, PlanFolderKey, planFolder)
		}
		if toolMode != "" {
			bgCtx = context.WithValue(bgCtx, ToolModeKey, toolMode)
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

	// Increment depth in context for the sub-agent and pass reasoning level + plan folder
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
	if reasoningLevel != "" {
		subCtx = context.WithValue(subCtx, ReasoningLevelKey, reasoningLevel)
	}
	if planFolder != "" {
		subCtx = context.WithValue(subCtx, PlanFolderKey, planFolder)
	}
	if toolMode != "" {
		subCtx = context.WithValue(subCtx, ToolModeKey, toolMode)
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

// buildPlannerPrompt creates the system instruction for the planner sub-agent
// It includes capabilities context so the planner knows what tools/servers/skills are available.
// tierConfig is optional — when provided, custom tier descriptions are added to reasoning level guidance.
func buildPlannerPrompt(caps *CapabilitiesContext, planFolder string, tierConfig ...*DelegationTierConfig) string {
	var sb strings.Builder

	sb.WriteString(`You are a Planner. Your job is to analyze an objective and produce a structured plan as a markdown file.

## Your Task
1. Use your tools to **research and understand** the problem — read files, query data, explore the workspace, read skill instructions
2. Break the objective into concrete, actionable tasks based on what you learned
3. Write the plan as a markdown file to the output path specified below

**IMPORTANT: You are ONLY planning, NOT executing.** Do not perform any of the plan steps yourself. Your only output is the plan.md file. Use tools solely for research and discovery to write a better plan.

## Output File
Write your plan to: ` + "`" + planFolder + `/plan.md` + "`" + `

## Plan Format
Write the plan as a structured markdown document with these sections:

` + "```" + `markdown
# Plan: {Short descriptive title}

## Goal
{Clear 1-2 sentence statement of what we're trying to achieve}

## Constraints
- {Known constraint 1 — e.g., environment, technology stack, data source}
- {Known constraint 2 — e.g., file locations, API endpoints, dependencies}
- {Known constraint 3 — e.g., business rules, limitations}

## Key Knowledge
- {Important finding from research — e.g., data schema, file structure}
- {Hypothesis or approach rationale}
- {Critical detail workers need to know}

## Tasks

### Phase 1 (parallel)
- [ ] **task-1**: {Task title}
  - {Detailed self-contained description. Include all context a worker needs.}
  - Reasoning level: {high|medium|low}

- [ ] **task-2**: {Task title}
  - {Detailed description — independent of task-1, can run simultaneously}
  - Reasoning level: {medium}

### Phase 2 (parallel, after Phase 1)
- [ ] **task-3**: {Task title}
  - {Detailed description}
  - Depends on: task-1
  - Reasoning level: {medium}

- [ ] **task-4**: {Task title}
  - {Detailed description — independent of task-3, can run simultaneously}
  - Depends on: task-2
  - Reasoning level: {low}

### Phase 3
- [ ] **task-5**: {Task title}
  - {Detailed description}
  - Depends on: task-3, task-4
  - Reasoning level: {low}

## Notes
{Workers append results, findings, and issues here as tasks complete}
` + "```" + `

## Guidelines
- Break the objective into concrete, actionable tasks. **Prefer many smaller tasks over few large ones** — each completed task triggers a progress update to the user, so more tasks = better visibility.
- **Target 5-15 tasks** rather than 3-5 big ones. If a task might take more than a few minutes, split it further. For example, instead of "Analyze all data sources", break it into separate tasks per data source.
- Each task description must be **self-contained** — a worker with no prior context will execute it
- Include specific details: file paths, function names, requirements, expected outcomes
- **Maximize parallelism**: Group independent tasks into phases. Tasks within a phase run simultaneously, so they MUST NOT depend on each other. The manager will delegate all tasks in a phase at once.
- Use "### Phase N (parallel)" headings to group tasks. Add "(after Phase M)" when a phase depends on a previous one.
- Add "Depends on: task-X, task-Y" on individual tasks to show which earlier tasks must complete first
- Identify tasks that are truly independent (e.g., frontend + backend, different modules, tests + docs) and put them in the same phase
- The **Constraints** section should capture environment details, technology stack, and known limitations
- The **Key Knowledge** section should include insights from your research (file paths, schemas, patterns found)
- Reasoning level recommendations:
  - **high**: Complex architecture, system design, tricky algorithms, planning
  - **medium**: Standard implementation, building features, writing code
  - **low**: Simple tasks like formatting, config changes, running tests, documentation
- The plan should be general-purpose — not limited to coding tasks
`)

	// Append capabilities section if available
	if caps != nil {
		sb.WriteString("\n## Available Capabilities\n")
		sb.WriteString("The following capabilities are available to workers executing tasks:\n\n")

		if caps.HasWorkspace {
			sb.WriteString("- **Workspace**: File read/write access via workspace tools\n")
		}
		if caps.HasBrowser {
			sb.WriteString("- **Browser**: Web browsing and automation capabilities\n")
		}

		if len(caps.EnabledServers) > 0 {
			sb.WriteString("\n### MCP Servers\n")
			sb.WriteString("Workers have access to these MCP servers (tool servers):\n")
			for _, server := range caps.EnabledServers {
				sb.WriteString(fmt.Sprintf("- `%s`\n", server))
			}
		}

		if len(caps.SelectedTools) > 0 {
			sb.WriteString("\n### Selected Tools\n")
			sb.WriteString("Specifically enabled tools (server:tool format):\n")
			for _, tool := range caps.SelectedTools {
				sb.WriteString(fmt.Sprintf("- `%s`\n", tool))
			}
		}

		if len(caps.Skills) > 0 {
			sb.WriteString("\n### Skills (HIGH PRIORITY)\n")
			sb.WriteString("The following skills are activated and contain detailed instructions, methodologies, and templates that workers MUST follow.\n")
			sb.WriteString("Skills are stored at the workspace root under skills/<name>/SKILL.md.\n\n")
			sb.WriteString("**Before writing the plan, you MUST read each skill file** using execute_shell_command(command: \"cat skills/<name>/SKILL.md\") to understand the methodology, steps, and requirements.\n\n")
			sb.WriteString("Available skills:\n")
			for _, skill := range caps.Skills {
				sb.WriteString(fmt.Sprintf("- **%s** (`skills/%s/SKILL.md`): %s\n", skill.Name, skill.FolderName, skill.Description))
			}
			sb.WriteString("\n**IMPORTANT**: When writing task descriptions in the plan, reference the relevant skill by name and path. Tell workers to read and follow the skill instructions. Skills contain detailed step-by-step guidance that is critical for task quality.\n")
		}

		if len(caps.SubAgentTemplates) > 0 {
			sb.WriteString("\n### Sub-Agent Templates\n")
			sb.WriteString("Reusable sub-agent profiles are available. Use the `agent_template` parameter on `delegate` to load a template's specialized instructions and defaults.\n\n")
			sb.WriteString("Available templates:\n")
			for _, tmpl := range caps.SubAgentTemplates {
				line := fmt.Sprintf("- **%s** (`subagents/%s/`): %s", tmpl.Name, tmpl.FolderName, tmpl.Description)
				if tmpl.DefaultReasoningLevel != "" {
					line += fmt.Sprintf(" [reasoning: %s]", tmpl.DefaultReasoningLevel)
				}
				if tmpl.DefaultToolMode != "" {
					line += fmt.Sprintf(" [tool_mode: %s]", tmpl.DefaultToolMode)
				}
				sb.WriteString(line + "\n")
			}
			sb.WriteString("\nWhen a template matches a task, pass `agent_template: \"<folder_name>\"` in the delegate call. The template's instructions, default reasoning level, and tool mode are automatically applied.\n")
		}

		sb.WriteString("\nConsider these capabilities when designing tasks — reference specific servers, tools, skills, or sub-agent templates where relevant.\n")
	}

	// Append custom tier descriptions if available
	var tc *DelegationTierConfig
	if len(tierConfig) > 0 {
		tc = tierConfig[0]
	}
	if section := BuildCustomTierPromptSection(tc); section != "" {
		sb.WriteString(section)
	}

	return sb.String()
}

// handleCreateDelegationPlan spawns a planner sub-agent that writes plan.md to workspace
func handleCreateDelegationPlan(ctx context.Context, args map[string]interface{}) (string, error) {
	planName, _ := args["plan_name"].(string)
	if planName == "" {
		return "", fmt.Errorf("plan_name is required")
	}
	objective, _ := args["objective"].(string)
	if objective == "" {
		return "", fmt.Errorf("objective is required")
	}
	additionalContext, _ := args["context"].(string)
	reasoningLevel, _ := args["reasoning_level"].(string)
	if reasoningLevel != "" {
		reasoningLevel = ValidateReasoningLevel(ctx, reasoningLevel)
	}
	// Planning is a high-reasoning task — default to "high" if not specified
	if reasoningLevel == "" {
		reasoningLevel = "high"
	}

	// Check delegation depth
	currentDepth := 0
	if depth, ok := ctx.Value(DelegationDepthKey).(int); ok {
		currentDepth = depth
	}
	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached", MaxDelegationDepth)
	}

	// Sanitize plan name to create plan ID (kebab-case, no special chars)
	planID := sanitizePlanName(planName)
	planFolder := fmt.Sprintf("%s/%s", PlanFileFolderPath, planID)

	// Enforce one plan per conversation — if a plan was already created, reject with guidance
	// Check both PlanSessionStateKey (new) and PlanTrackerKey (backward compat)
	var tracker *PlanSessionState
	if ps, ok := ctx.Value(PlanSessionStateKey).(*PlanSessionState); ok && ps != nil {
		tracker = ps
	} else if pt, ok := ctx.Value(PlanTrackerKey).(*PlanTracker); ok && pt != nil {
		tracker = pt
	}
	planArchived := false
	if tracker != nil {
		existingID, existingFolder, _ := tracker.TryCreate(planID, planFolder)
		if existingID != "" {
			// Reuse the existing plan folder — archive old plan.md to plan_tracking.md
			planID = existingID
			planFolder = existingFolder
			log.Printf("[DELEGATION PLAN] Reusing existing plan folder: %s (original request: %s)", planFolder, planName)

			// Archive existing plan.md into plan_tracking.md before overwriting
			if wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client); ok && wsClient != nil {
				planArchived = archivePlanToTracking(ctx, wsClient, planFolder)
			}
		}
	}

	// Create the plan folder eagerly via workspace API so the sidebar shows it immediately
	if wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client); ok && wsClient != nil {
		if err := wsClient.CreateFolder(ctx, planFolder); err != nil {
			log.Printf("[DELEGATION PLAN] Warning: could not pre-create plan folder %s: %v", planFolder, err)
			// Non-fatal — the planner will create it when writing plan.md
		} else {
			log.Printf("[DELEGATION PLAN] Pre-created plan folder: %s", planFolder)
		}
	}

	// Emit file event early so workspace sidebar can focus on the folder
	if emitter, ok := ctx.Value(PlanEventEmitterKey).(PlanEventEmitter); ok && emitter != nil {
		emitter.EmitFileEvent(planFolder)
	}

	// Extract capabilities context
	var caps *CapabilitiesContext
	if c, ok := ctx.Value(CapabilitiesContextKey).(*CapabilitiesContext); ok {
		caps = c
	}

	// Extract tier config for custom tier descriptions in planner prompt
	var planTierConfig *DelegationTierConfig
	if tc, ok := ctx.Value(DelegationTierConfigKey).(*DelegationTierConfig); ok {
		planTierConfig = tc
	}

	// Build the planner instruction
	plannerInstruction := buildPlannerPrompt(caps, planFolder, planTierConfig)
	plannerInstruction += fmt.Sprintf("\n\n## Objective\n%s", objective)

	if additionalContext != "" {
		plannerInstruction += fmt.Sprintf("\n\n## Additional Context\n%s", additionalContext)
	}

	log.Printf("[DELEGATION PLAN] Spawning planner sub-agent for: %s (plan_id: %s)", truncateString(objective, 80), planID)

	planFilePath := fmt.Sprintf("%s/plan.md", planFolder)

	// --- ASYNC PATH: Background planning (multi-agent mode) ---
	if bgDelegate, ok := ctx.Value(BackgroundDelegateKey).(BackgroundDelegateFunc); ok && bgDelegate != nil {
		bgCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
		if reasoningLevel != "" {
			bgCtx = context.WithValue(bgCtx, ReasoningLevelKey, reasoningLevel)
		}
		bgCtx = context.WithValue(bgCtx, PlanFolderKey, planFolder)

		agentID, err := bgDelegate(bgCtx, fmt.Sprintf("Planner: %s", planName), plannerInstruction)
		if err != nil {
			return "", fmt.Errorf("failed to start background planner agent: %w", err)
		}

		log.Printf("[DELEGATION PLAN] Started background planner agent '%s' (ID: %s) for plan %s", planName, agentID, planID)

		result := map[string]interface{}{
			"async":       true,
			"agent_id":    agentID,
			"plan_id":     planID,
			"plan_folder": planFolder,
			"plan_file":   planFilePath,
			"objective":   objective,
			"status":      "planning",
			"message":     fmt.Sprintf("Planner agent started in background. You will be notified when the plan is ready at %s. END YOUR TURN now and tell the user planning is underway.", planFilePath),
		}
		if planArchived {
			trackingFile := fmt.Sprintf("%s/plan_tracking.md", planFolder)
			result["plan_archived"] = true
			result["plan_tracking_file"] = trackingFile
			result["archive_note"] = fmt.Sprintf("The previous plan was archived to %s. You can read this file to reference past plans, progress, and learnings. Consider reviewing it before approving the new plan.", trackingFile)
		}
		resultJSON, _ := json.MarshalIndent(result, "", "  ")
		return string(resultJSON), nil
	}

	// --- SYNC PATH: Blocking planning (spawn mode or no background func) ---
	executeFunc, ok := ctx.Value(ExecuteDelegatedTaskKey).(ExecuteDelegatedTaskFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("delegation execution function not available")
	}

	// Spawn planner sub-agent with optional reasoning level from LLM
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
	if reasoningLevel != "" {
		subCtx = context.WithValue(subCtx, ReasoningLevelKey, reasoningLevel)
	}
	subCtx = context.WithValue(subCtx, PlanFolderKey, planFolder)

	startTime := time.Now()
	plannerResult, err := executeFunc(subCtx, plannerInstruction)
	planDuration := time.Since(startTime)

	if err != nil {
		return "", fmt.Errorf("planner sub-agent failed: %w", err)
	}

	log.Printf("[DELEGATION PLAN] Planner completed in %s for plan %s", planDuration, planID)

	// Emit file event for plan.md so the sidebar highlights it
	if emitter, ok := ctx.Value(PlanEventEmitterKey).(PlanEventEmitter); ok && emitter != nil {
		emitter.EmitFileEvent(planFilePath)
		log.Printf("[DELEGATION PLAN] Emitted file event for plan: %s", planFilePath)
	}

	// Read plan.md content via workspace API (handles per-user path resolution correctly)
	// This avoids the mount namespace issue where shell cat can't find per-user files
	var planContent string
	if wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client); ok && wsClient != nil {
		readResult, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
			Filepath: planFilePath,
		})
		if err != nil {
			log.Printf("[DELEGATION PLAN] Warning: could not read plan.md content: %v", err)
		} else {
			// Extract just the content field from the JSON response
			var readData map[string]interface{}
			if json.Unmarshal([]byte(readResult), &readData) == nil {
				if content, ok := readData["content"].(string); ok {
					planContent = content
				}
			}
		}

		// Fallback: if plan.md was not written by the planner sub-agent (common with
		// smaller models that return the plan as text instead of using tools), write it now.
		if planContent == "" && plannerResult != "" {
			// Extract markdown plan from the planner's text response
			content := extractPlanMarkdown(plannerResult)
			if content != "" {
				if _, writeErr := wsClient.UpdateWorkspaceFile(ctx, workspace.UpdateWorkspaceFileParams{
					Filepath: planFilePath,
					Content:  content,
				}); writeErr != nil {
					log.Printf("[DELEGATION PLAN] Warning: fallback write of plan.md failed: %v", writeErr)
				} else {
					planContent = content
					log.Printf("[DELEGATION PLAN] Fallback: wrote planner text response to %s (%d chars)", planFilePath, len(content))
				}
			}
		}
	}

	// Return summary to the manager with plan content included
	result := map[string]interface{}{
		"plan_id":       planID,
		"plan_folder":   planFolder,
		"plan_file":     planFilePath,
		"objective":     objective,
		"planning_time": planDuration.String(),
		"result":        plannerResult,
		"message":       fmt.Sprintf("Plan created at %s. You MUST call confirm_plan_execution to get user approval before delegating any tasks.", planFilePath),
	}
	if planContent != "" {
		result["plan_content"] = planContent
	}
	if planArchived {
		trackingFile := fmt.Sprintf("%s/plan_tracking.md", planFolder)
		result["plan_archived"] = true
		result["plan_tracking_file"] = trackingFile
		result["archive_note"] = fmt.Sprintf("The previous plan was archived to %s. You can read this file to reference past plans, progress, and learnings. Consider reviewing it before approving the new plan.", trackingFile)
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// --- Helper functions ---

// sanitizePlanName sanitizes a plan name from LLM input into a safe folder name.
// Converts to lowercase kebab-case, strips special characters.
// No random suffix — the LLM-provided name is descriptive enough and TryCreate
// prevents duplicates within a session. This allows resuming chats to reuse
// existing plan folders.
func sanitizePlanName(name string) string {
	// Lowercase and split into words
	words := strings.Fields(strings.ToLower(name))
	var parts []string
	for _, w := range words {
		// Strip non-alphanumeric (keep hyphens between words)
		cleaned := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			return -1
		}, w)
		// Split on hyphens to get individual parts, then rejoin
		for _, part := range strings.Split(cleaned, "-") {
			if part != "" {
				parts = append(parts, part)
			}
		}
	}
	if len(parts) == 0 {
		parts = []string{"plan"}
	}
	// Cap at 5 parts to keep folder names reasonable
	if len(parts) > 5 {
		parts = parts[:5]
	}
	return strings.Join(parts, "-")
}

// extractPlanMarkdown extracts the plan markdown from a planner's text response.
// Smaller models often return the plan as text instead of writing it to a file.
// This function tries to extract the useful content:
//  1. If the response contains a markdown code fence (```markdown ... ```), extract that.
//  2. If the response starts with "# Plan:", use it as-is.
//  3. Otherwise, return the full response (better than nothing).
func extractPlanMarkdown(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Try to extract from markdown code fence
	fenceStarters := []string{"```markdown\n", "```md\n", "```\n"}
	for _, starter := range fenceStarters {
		if idx := strings.Index(text, starter); idx != -1 {
			content := text[idx+len(starter):]
			if endIdx := strings.Index(content, "\n```"); endIdx != -1 {
				extracted := strings.TrimSpace(content[:endIdx])
				if extracted != "" {
					return extracted
				}
			}
		}
	}

	// If it starts with a heading, use as-is (likely the plan itself)
	if strings.HasPrefix(text, "# ") {
		return text
	}

	// Last resort: return the full text
	return text
}

// archivePlanToTracking reads the current plan.md and appends it to plan_tracking.md
// with a timestamp separator, preserving history of all previous plans in the same folder.
// Returns true if archiving succeeded (i.e. there was a previous plan to archive).
func archivePlanToTracking(ctx context.Context, wsClient *workspace.Client, planFolder string) bool {
	planFilePath := fmt.Sprintf("%s/plan.md", planFolder)
	trackingFilePath := fmt.Sprintf("%s/plan_tracking.md", planFolder)

	// Read current plan.md
	readResult, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
		Filepath: planFilePath,
	})
	if err != nil {
		log.Printf("[DELEGATION PLAN] No existing plan.md to archive: %v", err)
		return false
	}

	// Extract content from JSON response
	var readData map[string]interface{}
	if json.Unmarshal([]byte(readResult), &readData) != nil {
		return false
	}
	planContent, ok := readData["content"].(string)
	if !ok || strings.TrimSpace(planContent) == "" {
		return false
	}

	// Read existing plan_tracking.md (may not exist yet)
	var existingTracking string
	if trackResult, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
		Filepath: trackingFilePath,
	}); err == nil {
		var trackData map[string]interface{}
		if json.Unmarshal([]byte(trackResult), &trackData) == nil {
			existingTracking, _ = trackData["content"].(string)
		}
	}

	// Build new tracking content: existing tracking + archived plan
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	archiveEntry := fmt.Sprintf("\n\n---\n## Archived Plan (%s)\n\n%s", timestamp, planContent)

	var newTracking string
	if existingTracking == "" {
		newTracking = "# Plan History\n\nThis file tracks previous plans from this conversation." + archiveEntry
	} else {
		newTracking = existingTracking + archiveEntry
	}

	// Write plan_tracking.md
	if _, err := wsClient.UpdateWorkspaceFile(ctx, workspace.UpdateWorkspaceFileParams{
		Filepath: trackingFilePath,
		Content:  newTracking,
	}); err != nil {
		log.Printf("[DELEGATION PLAN] Warning: failed to write plan_tracking.md: %v", err)
		return false
	}
	log.Printf("[DELEGATION PLAN] Archived previous plan to %s", trackingFilePath)
	return true
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// BuildSpawnCapabilitiesSection returns a prompt section listing available sub-agent templates and skills.
// Appended to the spawn-mode agent's system prompt so it knows what templates
// are available when calling delegate().
func BuildSpawnCapabilitiesSection(caps *CapabilitiesContext) string {
	if caps == nil || (len(caps.SubAgentTemplates) == 0 && len(caps.Skills) == 0) {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Available Capabilities\n")

	if len(caps.Skills) > 0 {
		sb.WriteString("\n### Skills\n")
		sb.WriteString("The following skills are activated. Read each skill file before using it:\n")
		for _, skill := range caps.Skills {
			sb.WriteString(fmt.Sprintf("- **%s** (`skills/%s/SKILL.md`): %s\n", skill.Name, skill.FolderName, skill.Description))
		}
	}

	if len(caps.SubAgentTemplates) > 0 {
		sb.WriteString("\n### Sub-Agent Templates\n")
		sb.WriteString("Pass `agent_template: \"<folder_name>\"` in delegate() to apply a template's specialized instructions:\n")
		for _, tmpl := range caps.SubAgentTemplates {
			line := fmt.Sprintf("- **%s** (`subagents/%s/`): %s", tmpl.Name, tmpl.FolderName, tmpl.Description)
			if tmpl.DefaultReasoningLevel != "" {
				line += fmt.Sprintf(" [reasoning: %s]", tmpl.DefaultReasoningLevel)
			}
			if tmpl.DefaultToolMode != "" {
				line += fmt.Sprintf(" [tool_mode: %s]", tmpl.DefaultToolMode)
			}
			sb.WriteString(line + "\n")
		}
	}

	return sb.String()
}

// handleConfirmPlanExecution handles the confirm_plan_execution tool — blocks until user approves or rejects
func handleConfirmPlanExecution(ctx context.Context, args map[string]interface{}) (string, error) {
	// Try PlanSessionStateKey first, fall back to PlanTrackerKey
	var planState *PlanSessionState
	if ps, ok := ctx.Value(PlanSessionStateKey).(*PlanSessionState); ok && ps != nil {
		planState = ps
	} else if pt, ok := ctx.Value(PlanTrackerKey).(*PlanTracker); ok && pt != nil {
		planState = pt
	}
	if planState == nil {
		return "", fmt.Errorf("no plan session state available")
	}

	planState.mu.Lock()
	if planState.PlanID == "" {
		planState.mu.Unlock()
		return "", fmt.Errorf("no plan has been created yet. Use create_delegation_plan first")
	}
	currentPlanFolder := planState.PlanFolder
	planState.mu.Unlock()

	message := fmt.Sprintf("Plan `%s` is ready. Approve to start execution, or type feedback in the chat.", currentPlanFolder)

	// Read plan.md content to show in the approval UI
	planContent := ""
	if wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client); ok && wsClient != nil {
		planFilePath := currentPlanFolder + "/plan.md"
		if resultJSON, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{Filepath: planFilePath}); err == nil {
			// Parse the JSON response to extract raw content
			var result map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(resultJSON), &result); jsonErr == nil {
				if content, ok := result["content"].(string); ok {
					planContent = content
				}
			}
		} else {
			log.Printf("[PLAN APPROVAL] Could not read plan.md from %s: %v", planFilePath, err)
		}
	}

	// Emit workspace_file_operation to highlight plan.md in the workspace sidebar
	if planEmitter, ok := ctx.Value(PlanEventEmitterKey).(PlanEventEmitter); ok && planEmitter != nil {
		planEmitter.EmitFileEvent(currentPlanFolder + "/plan.md")
	}

	// Emit non-blocking plan_approval event — user responds via chat message
	if planContent == "" {
		log.Printf("[PLAN APPROVAL] Warning: planContent is empty for %s — plan won't show in approval UI", currentPlanFolder)
	}
	if emitter, ok := ctx.Value(SessionEventEmitterKey).(SessionEventEmitter); ok && emitter != nil {
		emitter.EmitPlanApproval(message, planContent, "Approve & Execute")
	}

	// Return immediately — non-blocking. User will approve or provide feedback via chat message.
	return `{"status": "plan_presented", "message": "Plan presented to user for approval. END YOUR TURN NOW. The user will approve or provide feedback in their next message."}`, nil
}

// GetAutonomousDelegationInstructions returns the combined system prompt where the agent
// autonomously decides whether to plan, delegate directly, or handle tasks itself.
// This merges the best of spawn mode (agent autonomy) with plan mode (async background agents).
func GetAutonomousDelegationInstructions() string {
	return `
## Delegation — Sub-Agent Tools

You are a fully capable agent — you can do any task yourself using your tools. Delegation is an **additional capability** that lets you run work in parallel or offload tasks to focused sub-agents. You also have the ability to create structured plans for complex tasks.

### When to Do It Yourself vs Delegate vs Plan

**Do it yourself** when:
- The task is simple or quick (answering questions, small edits, running a command)
- You need to explore or understand something before deciding what to do
- The user is asking for your opinion, analysis, or explanation
- It's a single focused task that doesn't benefit from parallelism

**Delegate directly** (no plan needed) when:
- You can split work into independent tasks that run **in parallel** (this is the main benefit)
- The task is a single focused piece of work with clear requirements
- You want to offload a well-defined subtask so you can move on to other work

**Create a plan first** (` + "`create_delegation_plan`" + ` → approve → execute) when:
- The task has multiple interdependent phases
- It requires research/discovery before the approach is clear
- It benefits from 3+ parallel workers across multiple phases
- The user explicitly asks for a breakdown

**Ask the user** when the task falls in between — medium complexity where either path could work. Use ` + "`human_feedback`" + ` to ask: *"Would you like me to create a structured plan first, or should I dive in directly?"*

### Delegation Tools

**` + "`delegate`" + `** — Spawn a sub-agent for a task (returns immediately, runs in background):
- Provide comprehensive, self-contained instructions (sub-agents have no shared memory)
- Call multiple times in one turn for **parallel execution** — this is the key advantage
- Optional ` + "`reasoning_level`" + `: "high" (architecture, complex logic), "medium" (standard implementation), "low" (formatting, tests, config)
- Optional ` + "`plan_folder`" + `: restricts worker writes to this folder (always pass when executing plan tasks)
- Optional ` + "`agent_template`" + `: sub-agent template folder name (e.g. "code-review"). Loads specialized instructions and defaults from subagents/<name>/SUBAGENT.md
- Optional ` + "`servers`" + `: list of MCP server names for this sub-agent. When specified, the worker only connects to these servers instead of all available ones.

**` + "`create_delegation_plan`" + `** — Spawn a planner sub-agent that writes plan.md:
- Planner researches the objective and creates a phased task breakdown
- Returns plan_id, plan_folder, and plan_content directly
- Then execute tasks phase by phase using ` + "`delegate`" + `

**` + "`confirm_plan_execution`" + `** — Present plan to user for approval (returns immediately)

**` + "`query_agent`" + `** — Check status/progress of a running task

**` + "`terminate_agent`" + `** — Cancel a running task

**` + "`list_agents`" + `** — See all tasks and their status

**` + "`human_questions`" + `** — Ask the user 3-8 structured questions to clarify requirements before starting work.

**` + "`human_feedback`" + `** — Ask a single question, present multiple-choice options, or get free-text input from the user.

### Tool Mode (optional, for ` + "`delegate`" + `):
- **"simple"** (default): Worker gets all tools directly. Best for most tasks, including **writing Python/Bash scripts** — use workspace shell tools (` + "`execute_shell_command`" + `) to create and run scripts.
- **"code_execution"**: Worker writes Go code to call tools programmatically. Best for **data analysis with MCP tools** — e.g., fetching data from MCP servers, transforming responses, batch operations, loops over tool results. NOT recommended for simple script writing.
- **"tool_search"**: Worker discovers tools on-demand via search. Use when 3+ MCP servers are available.

**Guideline**: For writing Python scripts, shell scripts, or any file-based work, always prefer **"simple"** mode with workspace shell tools. Use **"code_execution"** only when you need to programmatically orchestrate multiple MCP tool calls and analyze their responses.

### Direct Delegation Workflow

1. If requirements are unclear, use ` + "`human_questions`" + ` to clarify first. Skip if the request is already clear.
2. **If the task will produce file outputs** (reports, scripts, data, code), create an output folder first:
   execute_shell_command(command: "mkdir -p Chats/{kebab-task-name}")
   Then pass ` + "`plan_folder: \"Chats/{kebab-task-name}\"`" + ` in the delegate call so the worker saves there.
   Skip this step for in-place tasks (e.g. editing an existing file, running a test) that don't produce new output files.
3. Delegate the task: call ` + "`delegate(name, instruction, reasoning_level)`" + `. Call multiple times in one turn for parallel execution.
4. **END YOUR TURN** after delegating. Tell the user what's being worked on in natural language.
5. You will be notified when the task completes. Review results and report to the user.

### Plan Workflow (for complex multi-phase tasks)

#### If an existing plan is pre-seeded (user selected a plan folder):
1. Read the plan: execute_shell_command(command: "cat Chats/{folder}/plan.md")
2. List existing files: execute_shell_command(command: "find Chats/{folder} -type f | sort 2>/dev/null")
3. **Immediately delegate the remaining unchecked tasks** — the plan already exists. Call ` + "`delegate()`" + ` for all pending tasks, passing plan_folder on each call.
4. When tasks complete, re-read plan.md for learnings, then delegate the next batch.

#### If no plan exists yet:
1. If the user's request is vague, use ` + "`human_questions`" + ` to clarify first.
2. Check for related existing plans: execute_shell_command(command: "ls -1 Chats/ 2>/dev/null")
3. Call ` + "`create_delegation_plan`" + ` — **END YOUR TURN** immediately after.
4. When planning completes, call ` + "`confirm_plan_execution(plan_summary)`" + ` to present the plan for approval. **END YOUR TURN**.
5. User approves → delegate all tasks from the first phase. User gives feedback → re-plan.

### Plan Management Rules
- **One plan per conversation**: Never create a second plan. Update the existing plan.md instead.
- **Re-read plan.md after each phase**: Workers write Key Knowledge and Notes into plan.md. Collect their discoveries before the next phase.
- **Relay learnings**: Include relevant Key Knowledge in the next delegate instruction. Workers start fresh with no shared memory.
- **Pass ` + "`plan_folder`" + `**: Always pass it when executing plan tasks to restrict worker writes.
- **Self-contained instructions**: Each delegate call must include ALL context the worker needs.
- **Verify results**: You are the quality gate — check results before reporting to the user.

### Plan Folder Structure
` + "```" + `
Chats/{plan_id}/
  plan.md              ← Current active plan (ONLY this + plan_tracking.md at root)
  plan_tracking.md     ← Auto-generated: archived previous plans with timestamps
  research/            ← Research and analysis outputs
  reports/             ← Generated reports and summaries
  scripts/             ← Code, scripts, automation
  data/                ← Data files, exports, datasets
  config/              ← Configuration files
` + "```" + `

### Task Granularity — Keep Tasks Small
- **Prefer many small tasks over few large ones**. Each completed task triggers an update to the user.
- **Break large tasks into focused sub-tasks**. More tasks in a phase means more parallel work and faster completion.

### Communication Style
- **NEVER mention internal concepts** like "agents", "sub-agents", "background agents", "delegation", "synthetic turns", "plan.md", "plan_folder", or tool names to the user.
- Speak naturally: "I'm analyzing...", "I'm working on...", "Here are the results."
- Present results as YOUR findings — not as "agent results" or "worker output".
- **Tool responses, file reads, and sub-agent outputs are NOT visible to the human.** Always include the actual content directly in your response.
- **For large outputs**, reference the workspace file path so the user can access the full document.

### Limitations
- Sub-agents cannot delegate further (max depth enforced).
- Sub-agents start fresh — no shared context or conversation history.
`
}

// GetExecutionOnlyInstructions returns system prompt for execution-only mode (skip planning).
// The user chose "Exec" mode, so the LLM should delegate tasks directly without creating a plan first.
func GetExecutionOnlyInstructions() string {
	return `
## How You Work — Execution Mode

You are an intelligent assistant that executes tasks efficiently using delegation. The user has chosen **execution mode**, which means you should **skip planning and execute directly**.

### Workflow

1. If the user's request is vague or has open questions, use ` + "`human_questions`" + ` to ask clarifying questions first. Skip this if the request is already clear.
2. Break the task into concrete sub-tasks and immediately delegate them using ` + "`delegate(name, instruction)`" + `.
3. Call multiple delegate() in one turn for **parallel execution** — this is the key advantage.
4. **After delegating, END YOUR TURN.** Tell the user what's being worked on in natural language.
5. You will receive automatic notifications when tasks complete — no need to poll.
6. When notified, review results and delegate the next batch if needed.
7. When ALL work is done, summarize results to the user.

### Important
- **Default: delegate directly** without creating a plan — just break into sub-tasks and delegate.
- You can still use ` + "`human_questions`" + ` or ` + "`human_feedback`" + ` if you need clarification.
- **Only create a plan** (via create_delegation_plan) when the task is large and complex with 3+ interdependent phases that require careful sequencing, or when the user explicitly asks for a plan.

### Output Organization
- If an active plan folder exists for this session, first check what work is already done:
  execute_shell_command(command: "find Chats/{folder} -type f | sort 2>/dev/null")
  Skip tasks whose output files already exist. Then pass plan_folder to every delegate call so workers save outputs there.
- If no plan folder exists but the task will produce file outputs (reports, scripts, data, code), create a task folder first:
  execute_shell_command(command: "mkdir -p Chats/{kebab-task-name}")
  Then pass ` + "`plan_folder: \"Chats/{kebab-task-name}\"`" + ` to every delegate call so outputs land in one place.
- For in-place tasks (editing existing files, running tests, applying patches) that produce no new output files, no folder is needed — skip this step.
- **NEVER instruct workers to dump files at the workspace root** — always use a descriptive sub-folder under Chats/.
- In your final summary, reference any saved file paths so the user can access them. The system will automatically convert these paths to clickable links.

### Communication Style
- **NEVER mention internal concepts** like "agents", "sub-agents", "background agents", "delegation", "synthetic turns", or tool names to the user.
- Speak naturally: "I'm working on...", "Here are the results."
- Present results as YOUR findings — not as "agent results" or "worker output".
- **Tool responses, file reads, and sub-agent outputs are NOT visible to the human.** The human only sees your text messages. Always include the actual content directly in your response — never say "as shown above", "as you can see", or assume the user has seen any tool result. If the user asks to "show" something, paste it directly into your reply.
- For large outputs, reference the workspace file path. Example: "The full report is saved at reports/quarterly-analysis.md."

### Available Tools

**` + "`delegate(name, instruction)`" + `** — Start a named task (returns immediately, runs in parallel)
- Provide a short descriptive name: "Analyze Sales Data", "Generate Report"
- Provide comprehensive, self-contained instructions
- Required: reasoning_level ("high", "medium", "low", or a custom tier)
- Optional: plan_folder, tool_mode, agent_template, servers

**` + "`query_agent(agent_id)`" + `** — Check status/progress of a running task

**` + "`terminate_agent(agent_id)`" + `** — Cancel a running task

**` + "`list_agents()`" + `** — See all tasks and their status

**` + "`human_questions`" + `** — Ask the user structured questions to clarify requirements

**` + "`human_feedback`" + `** — Ask a single question or present choices to the user

### Task Granularity — Keep Tasks Small
- **Prefer many small tasks over few large ones**. Each completed task triggers an update to the user, so more tasks = better progress visibility.
- **Break large tasks into focused sub-tasks**. Instead of one big task, split into smaller pieces that each complete faster. This gives users frequent progress updates and makes failures easier to recover from.

### Rules
- **Delegate work, don't do it yourself** — use delegate() for all substantive tasks. Simple follow-up questions or short direct answers you can handle yourself.
- **Always pass reasoning_level** on every delegate call
- **Self-contained instructions**: Each delegate call must include ALL context needed
- **Pass plan_folder** to every delegate call when an active plan folder exists
- **End your turn after calling delegate** — you will be notified automatically when work finishes
- **You are the quality gate** — review results before reporting to the user

### Tool Mode (optional, for delegate):
- **"simple"** (default): Best for most tasks, including writing Python/Bash scripts via shell tools.
- **"code_execution"**: Worker writes Python code to call MCP tools via HTTP API. Best for data analysis, batch operations, loops over MCP tool results, or tasks that benefit from programmatic orchestration of multiple tool calls.
- **"tool_search"**: Use when 3+ MCP servers are available.

**Guideline**: Use **"code_execution"** when the task involves fetching/processing data from MCP servers programmatically (e.g., aggregation, filtering, multi-step data pipelines). Use **"simple"** for file operations, script writing, and general tasks.
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

### Calling Human Tools via HTTP API
The following tools are NOT available as direct function calls — call them via curl through ` + "`mcp__api-bridge__execute_shell_command`" + `:

- **Delegation tools**: delegate, create_delegation_plan, confirm_plan_execution, query_agent, terminate_agent, list_agents
- **Human interaction tools**: human_feedback, human_questions
- **Memory tools**: save_memory, recall_memory, compress_memory

**Pattern for calling any human tool:**
1. Optionally use ` + "`mcp__api-bridge__get_api_spec(server_name=\"human\", tool_name=\"delegate\")`" + ` to see the full API spec
2. Call via ` + "`mcp__api-bridge__execute_shell_command`" + ` using curl:

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

create a delegation plan:
` + "```" + `bash
curl -s -X POST "$MCP_API_URL/tools/custom/create_delegation_plan" \
  -H "Authorization: Bearer $MCP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"plan_name": "my-plan", "objective": "...", "context": "..."}'
` + "```" + `

ask the user questions:
` + "```" + `bash
curl -s -X POST "$MCP_API_URL/tools/custom/human_questions" \
  -H "Authorization: Bearer $MCP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"questions": [{"question": "What framework do you prefer?", "context": "For the frontend"}]}'
` + "```" + `

$MCP_API_URL and $MCP_API_TOKEN are pre-set environment variables — use them as-is.

**Important:** Whenever the instructions mention calling a tool like ` + "`delegate(instruction: \"...\")`" + `, ` + "`save_memory(content: \"...\")`" + `, or ` + "`human_questions(...)`" + `, translate that to the curl HTTP API pattern above. Do NOT attempt to call these as direct function calls — they will not be found.
`
}
